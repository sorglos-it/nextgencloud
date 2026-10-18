package state

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// The clock of every case here. A fixed number, so what goes into the
// migration lock is of the case and not of the machine.
const testNow = int64(1_790_000_000)

// testOwner is what these cases write into the lock.
const testOwner = "ngc-controller/test"

// dbOf is the configuration of one controller database.
func dbOf(path string, keyLen int) config.DB {
	return config.DB{
		Driver: config.DriverSQLite,
		Path:   path,
		Key:    config.NewSecret(bytes.Repeat([]byte{9}, keyLen)),
	}
}

// AES-256-XTS needs 64 bytes: everything else is a mistake, and it is answered
// before a file is opened at all - a half-opened database would leave a file
// on disk for a server that cannot come up.
func TestOpenRefusesAKeyThatIsNoKey(t *testing.T) {
	for _, n := range []int{0, 1, 16, 32, 63, 65, 128} {
		path := filepath.Join(t.TempDir(), "state.db")
		st, err := Open(dbOf(path, n), testOwner, testNow)
		if err == nil {
			st.Close()
			t.Errorf("a key of %d bytes was taken", n)
			continue
		}
		if n != 64 && !strings.Contains(err.Error(), "64") {
			t.Errorf("a key of %d bytes was refused with %q, which does not say how long a key is", n, err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("a key of %d bytes left a file behind", n)
		}
	}
}

// A key of the wrong length is answered with a NAME, so a caller can tell
// "the key of this server is wrong" from "the database of this server is
// broken". That name is the whole reason there is a check in Open in front of
// the one the store does itself.
func TestOpenNamesAKeyThatIsNoKey(t *testing.T) {
	_, err := Open(dbOf(filepath.Join(t.TempDir(), "state.db"), 32), testOwner, testNow)
	if !errors.Is(err, ErrKey) {
		t.Fatalf("Open with a 32-byte key = %v, want %v", err, ErrKey)
	}
	// And a key that IS one does not get that answer, so the case is not
	// measuring a function that always says the same thing.
	st, err := Open(dbOf(filepath.Join(t.TempDir(), "state.db"), 64), testOwner, testNow)
	if err != nil {
		t.Fatalf("Open with a 64-byte key = %v", err)
	}
	st.Close()
}

// The driver is the one this controller's own database is: a configuration
// that names another is refused rather than opened as something else.
func TestOpenRefusesAnotherDriver(t *testing.T) {
	cfg := dbOf(filepath.Join(t.TempDir(), "state.db"), 64)
	cfg.Driver = config.DriverMariaDB
	if st, err := Open(cfg, testOwner, testNow); err == nil {
		st.Close()
		t.Fatal("a MariaDB configuration opened a local SQLite")
	}
}

// What a restart means for this store: the rows are there again. This is the
// first of the three states of continuous operation, and it goes through the
// real file - a store that kept its rows in memory would pass everything else
// of this package and fail here.
func TestTheStateSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	app := dbapi.App{ID: "org.sorglos.notes", Version: "1.0.0", Store: "global",
		Node: "ctl-1", Active: true, Manifest: []byte(`{}`)}

	first, err := Open(dbOf(path, 64), testOwner, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.PutApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// The second start of the same program over the same file.
	second, err := Open(dbOf(path, 64), testOwner, testNow+1)
	if err != nil {
		t.Fatalf("the second start did not open the file: %v", err)
	}
	t.Cleanup(func() { second.Close() })
	got, err := second.App(ctx, app.ID)
	if err != nil {
		t.Fatalf("the row of the app is gone after a restart: %v", err)
	}
	if got.ID != app.ID || got.Version != app.Version || !got.Active {
		t.Fatalf("the row came back as %+v, want %+v", got, app)
	}
	// And the migrations of the second start ran over a schema that was
	// already there: a second run that tried to create the tables again
	// would have failed above.
	apps, err := second.Apps(ctx)
	if err != nil || len(apps) != 1 {
		t.Fatalf("Apps = %d rows, %v, want 1 row", len(apps), err)
	}
}

// A crash in the middle of a write leaves no half row: the file is opened
// again afterwards and either has the row or has it not, and the store reads
// to the end either way.
//
// What is NOT claimed here: that a power cut cannot lose the last write. It
// can - that is what the journal mode decides, and it is of the store and not
// of this file. What is measured is the thing a controller depends on: the
// file is still READABLE after a process died with it open, and the rows
// before the one that was being written are all there.
func TestAFileLeftBehindByADeadProcessStaysReadable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	// A store that is never closed, which is what a process that died with
	// the file open leaves behind.
	first, err := Open(dbOf(path, 64), testOwner, testNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"org.sorglos.a", "org.sorglos.b"} {
		if err := first.PutApp(ctx, dbapi.App{ID: id, Version: "1.0.0", Store: "global",
			Node: "ctl-1", Active: true, Manifest: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	// No Close: the process this stands for did not get that far.
	again, err := Open(dbOf(path, 64), testOwner, testNow+1)
	if err != nil {
		t.Fatalf("the file of a process that did not close it did not open: %v", err)
	}
	t.Cleanup(func() { again.Close(); first.Close() })
	apps, err := again.Apps(ctx)
	if err != nil {
		t.Fatalf("the file of a process that did not close it is not readable: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("%d rows came back, want 2", len(apps))
	}
}

// TestOpenHelperProcess is the second process of the case above. It does
// nothing unless it was started as one, so a normal run of this package skips
// it in a fraction of a second.
func TestOpenHelperProcess(t *testing.T) {
	if os.Getenv("NGC_STATE_HELPER") == "" {
		t.Skip("not the second process of TestASecondControllerOnTheSameFileIsTurnedAway")
	}
	st, err := Open(dbOf(os.Getenv("NGC_STATE_HELPER"), 64), "ngc-controller/second", testNow)
	if err != nil {
		// The one line the parent reads. It goes to stdout, because the test
		// framework writes its own lines there too and the parent looks for
		// this one.
		os.Stdout.WriteString("HELPER-FAILED: " + err.Error() + "\n")
		return
	}
	st.Close()
	os.Stdout.WriteString("HELPER-OPENED\n")
}

// What happens when a SECOND controller process opens the same file. This is
// the measurement the comment of sqlite.Open only asserted: it says the
// migration lock keeps two callers apart, and "it holds according to the
// comment" is no measurement.
//
// Two halves, and they say different things:
//
//  1. While another owner HOLDS the lock, Open comes back with an error that
//     names it. A second controller does not wait, does not migrate and does
//     not come up. That is deterministic and is the first half below.
//  2. One after the other, both come up - which is the restart case and has
//     to work, or a controller could never be restarted.
//
// The second process is a real one (os.Args[0] re-executed), not a second
// goroutine: what is in question is two PROCESSES on one file.
func TestASecondControllerOnTheSameFileIsTurnedAway(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	// The schema has to be there before the lock row can be held.
	first, err := Open(dbOf(path, 64), testOwner, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Sequentially: the second process comes up. The lock was given back.
	cmd := exec.Command(os.Args[0], "-test.run=^TestOpenHelperProcess$")
	cmd.Env = append(os.Environ(), "NGC_STATE_HELPER="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the second process failed to run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "HELPER-OPENED") {
		t.Fatalf("a second controller could not open the file after the first closed it:\n%s", out)
	}

	// Now with the lock really held by somebody else, over the store's own
	// driver and not through a second grammar.
	db, err := sqlite.OpenDB(dbOf(path, 64))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The row is INSERTed and not updated: a run that gave the lock back
	// deleted its row (migrate.freeLock is a DELETE), so after the clean
	// first open above there is none to update. Measured: an UPDATE changes
	// 0 rows here, and a case that did not check its own preparation would
	// then have measured the open of a FREE file and looked like a result.
	//
	// until far in the future: migrate takes the lock only where
	// "until <= now" (migrate.takeLock).
	res, err := db.Exec("INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)",
		[]byte("migrate"), []byte("ngc-controller/somebody-else"), testNow+100000)
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	// The preparation of this case is checked before anything is measured: a
	// case that had not taken the lock would measure the open of a free file
	// and look like a result.
	if n != 1 {
		t.Fatalf("the lock row was not taken: %d rows written, want 1", n)
	}
	cmd = exec.Command(os.Args[0], "-test.run=^TestOpenHelperProcess$")
	cmd.Env = append(os.Environ(), "NGC_STATE_HELPER="+path)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the second process failed to run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "HELPER-FAILED") {
		t.Fatalf("a second controller came up while another owner held the migration lock:\n%s", out)
	}
	// And the answer names the lock, so whoever reads it on a server knows
	// what to look at.
	if !strings.Contains(string(out), "lock") {
		t.Fatalf("the refusal does not name the lock:\n%s", out)
	}
	t.Logf("measured: a second controller process meets the held lock and is refused with: %s",
		strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
}

// Several opens of the same file at once, in one process. It is not an
// assertion about which of them wins - that is a race by nature - but a
// measurement of what the losers get: nobody may come up over a schema that
// another one is still migrating, and nobody may get a store that is broken.
//
// What it holds: at least one comes up, and every one that does not says so
// with an error. A second open that quietly gave back an unmigrated store
// would be the finding this case is here for.
func TestManyOpensAtOnceEitherComeUpOrSayWhyNot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	const tries = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		up   int
		errs []error
	)
	start := make(chan struct{})
	for range tries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			st, err := Open(dbOf(path, 64), testOwner, testNow)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			up++
			// A store that came up has to be usable at once: that is what
			// says the schema was there and not half migrated.
			if _, err := st.Apps(ctx); err != nil {
				errs = append(errs, err)
				t.Errorf("a store that came up cannot be read: %v", err)
			}
			st.Close()
		}()
	}
	close(start)
	wg.Wait()
	if up == 0 {
		t.Fatalf("not one of %d opens came up: %v", tries, errs)
	}
	if up+len(errs) != tries {
		t.Fatalf("%d came up and %d failed out of %d", up, len(errs), tries)
	}
	t.Logf("measured: of %d opens of one file at once, %d came up and %d were refused", tries, up, len(errs))
}
