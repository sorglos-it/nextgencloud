package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

var ctx = context.Background()

// testConfig is a fresh, encrypted file in the test's own directory.
func testConfig(t *testing.T, name string) config.DB {
	t.Helper()
	key := bytes.Repeat([]byte{0x5a}, 64) // 64 bytes: AES-256-XTS
	return config.DB{Driver: config.DriverSQLite, Path: filepath.Join(t.TempDir(), name), Key: config.NewSecret(key)}
}

func TestOpenEncrypts(t *testing.T) {
	cfg := testConfig(t, "state.db")
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO probe (a) VALUES (?)", []byte("secret-marker")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(cfg.Path)
	if err != nil || len(raw) < 16 {
		t.Fatalf("read back: %d bytes, %v", len(raw), err)
	}
	if bytes.HasPrefix(raw, []byte("SQLite format 3\x00")) {
		t.Error("the file is a plain SQLite database, so it is not encrypted")
	}
	if bytes.Contains(raw, []byte("secret-marker")) {
		t.Error("the file holds the value in the clear")
	}

	// The same key opens it again.
	db, err = OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	if err := db.QueryRowContext(ctx, "SELECT a FROM probe").Scan(&got); err != nil || string(got) != "secret-marker" {
		t.Errorf("with the right key: %q, %v", got, err)
	}
	db.Close()

	// Another key does not.
	other := cfg
	other.Key = config.NewSecret(bytes.Repeat([]byte{0x5b}, 64))
	db, err = OpenDB(other)
	if err == nil {
		err = db.QueryRowContext(ctx, "SELECT a FROM probe").Scan(&got)
		db.Close()
	}
	if err == nil {
		t.Error("the wrong key opened the file")
	}
}

func TestOpenRefusesBadConfig(t *testing.T) {
	for name, cfg := range map[string]config.DB{
		"no key":       {Driver: config.DriverSQLite, Path: "/tmp/x.db"},
		"short key":    {Driver: config.DriverSQLite, Path: "/tmp/x.db", Key: config.NewSecret(make([]byte, 32))},
		"relative":     {Driver: config.DriverSQLite, Path: "x.db", Key: config.NewSecret(make([]byte, 64))},
		"wrong driver": {Driver: config.DriverMariaDB},
		// A path may not smuggle a second URI parameter into the file name.
		"parameter in the path": {Driver: config.DriverSQLite, Path: "/tmp/x.db?hexkey=00", Key: config.NewSecret(make([]byte, 64))},
	} {
		if db, err := OpenDB(cfg); err == nil {
			db.Close()
			t.Errorf("%s was accepted", name)
		}
	}
	// "/tmp/x.db?hexkey=00" is only absolute where "/" starts a path, so on a
	// developer's machine the case above is refused for being relative and
	// proves nothing about the "?". This one carries the same "?" on a path
	// that is absolute everywhere.
	cfg := testConfig(t, "state.db?hexkey=00")
	if db, err := OpenDB(cfg); err == nil {
		db.Close()
		t.Error("a parameter in an absolute path was accepted")
	}
}

// TestFileURL guards the data source name. Whatever the path holds, the URI
// carries exactly the two parameters we put there and the path comes back
// out of it unchanged: no file name of ours turns into a key, a VFS or a
// lock mode of someone else's choosing.
func TestFileURL(t *testing.T) {
	dir := t.TempDir() // absolute on every machine the tests run on
	for _, name := range []string{"state.db", "a b.db", "a%3Fb.db", "a&b=c.db", "a+b,c;d.db", "Ärger.db"} {
		path := filepath.Join(dir, name)
		dsn, err := fileURL(path)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if !strings.HasPrefix(dsn, "file:") {
			t.Errorf("%q: %q does not start with file:", name, dsn)
		}
		// Read the URI the way SQLite does, not the way net/url does: the
		// file name is everything between "file:" and the first "?",
		// percent-decoded, and the parameters are the rest.
		if n := strings.Count(dsn, "?"); n != 1 {
			t.Errorf("%q: %d question marks in %q", name, n, dsn)
		}
		file, query, _ := strings.Cut(strings.TrimPrefix(dsn, "file:"), "?")
		got, err := url.PathUnescape(file)
		if err != nil || got != path {
			t.Errorf("%q: the URI names %q, %v", path, got, err)
		}
		q, err := url.ParseQuery(query)
		if err != nil || len(q) != 2 || q.Get("vfs") != "xts" || q.Get("_txlock") != "immediate" {
			t.Errorf("%q: parameters %v, %v", name, q, err)
		}
	}
	// filepath.Join cleans what it builds, so the paths that must stay dirty
	// are put together by hand.
	sep := string(filepath.Separator)
	for label, path := range map[string]string{
		"empty":            "",
		"relative":         "x.db",
		"relative dot":     "./x.db",
		"question mark":    filepath.Join(dir, "x.db?hexkey=00"),
		"hash":             filepath.Join(dir, "x.db#hexkey=00"),
		"line feed":        filepath.Join(dir, "x\n.db"),
		"nul":              filepath.Join(dir, "x\x00.db"),
		"delete":           filepath.Join(dir, "x\x7f.db"),
		"too long":         filepath.Join(dir, strings.Repeat("x", config.MaxField)+".db"),
		"step up":          dir + sep + ".." + sep + "x.db",
		"step here":        dir + sep + "." + sep + "x.db",
		"two separators":   dir + sep + sep + "x.db",
		"separator at end": dir + sep + "x.db" + sep,
	} {
		if dsn, err := fileURL(path); err == nil {
			t.Errorf("%s was accepted: %q", label, dsn)
		}
	}
}

// TestConnectorHidesTheKey guards the house rule that a secret never reaches
// a log, not even by accident. The connector carries the key of the file for
// the lifetime of the pool, so whatever printing it may happen - a %v of a
// connection that would not open, say - has to show nothing of the key.
func TestConnectorHidesTheKey(t *testing.T) {
	hex := strings.Repeat("5a", keySize)
	c := connector{hexkey: config.NewSecretText(hex)}
	for _, printed := range []string{
		fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c),
		fmt.Sprintf("%s", c.hexkey), fmt.Sprint(c.hexkey),
	} {
		if strings.Contains(printed, "5a5a") {
			t.Errorf("the key shows in %q", printed)
		}
	}
	// It is still the key, not a copy that lost something.
	if c.hexkey.Text() != hex {
		t.Error("the connector does not hand the key on unchanged")
	}
	// And the check is not vacuous: the same field as a plain string, which
	// is what stood here before, shows the key at the first %v.
	type plain struct{ hexkey string }
	if !strings.Contains(fmt.Sprintf("%v", plain{hex}), "5a5a") {
		t.Error("the check itself does not catch a key in plain sight")
	}
}

// TestPragmas checks what every new connection has to carry. The key is not
// among them because it cannot be read back; that it was set first is what
// makes the rest of this work at all, since each of these statements already
// reads the encrypted file.
func TestPragmas(t *testing.T) {
	db, err := OpenDB(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	want := map[string]string{
		"journal_mode": "wal",
		"synchronous":  "1", // normal
		"temp_store":   "2", // memory
		"busy_timeout": "10000",
		"foreign_keys": "1",
	}
	// Hold every connection of the pool at once, so each of them is a new one
	// and each has to have run the callback.
	for i := range maxOpenConns {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		defer conn.Close()
		for pragma, value := range want {
			var got string
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("connection %d, %s: %v", i, pragma, err)
			}
			if got != value {
				t.Errorf("connection %d: %s = %q, want %q", i, pragma, got, value)
			}
		}
	}
}

// TestPoolKeepsOneConnection pins the shape of the pool: four connections at
// once, but only one of them stays. Every connection runs a SQLite build of
// its own, and its memory - up to maxMemory after one large blob - comes back
// only when the connection closes.
func TestPoolKeepsOneConnection(t *testing.T) {
	db, err := OpenDB(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := db.Stats().MaxOpenConnections; got != maxOpenConns {
		t.Errorf("%d connections at once, want %d", got, maxOpenConns)
	}
	conns := make([]*sql.Conn, maxOpenConns)
	for i := range conns {
		if conns[i], err = db.Conn(ctx); err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
	}
	if got := db.Stats().OpenConnections; got != maxOpenConns {
		t.Errorf("%d connections open, want %d", got, maxOpenConns)
	}
	for i, conn := range conns {
		if err := conn.Close(); err != nil {
			t.Errorf("closing %d: %v", i, err)
		}
	}
	if st := db.Stats(); st.Idle > maxIdleConns || st.OpenConnections > maxIdleConns {
		t.Errorf("%d open and %d idle afterwards; at most %d may stay",
			st.OpenConnections, st.Idle, maxIdleConns)
	}
}

// TestLargestBlob writes and reads back a blob of the largest size the store
// allows. It is the only proof that the memory of the SQLite build carries
// one: the driver runs SQLite in WebAssembly with a bound of its own, and a
// connection that runs past that bound does not return an error - it panics.
// The test takes a few seconds and a few hundred MiB; that is the price of
// the proof.
func TestLargestBlob(t *testing.T) {
	db, err := OpenDB(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (id INTEGER NOT NULL, a BLOB NOT NULL, PRIMARY KEY (id))"); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0x5a}, sqlstore.MaxData)
	// Both ends carry a mark, so a blob that comes back short or shifted is
	// not hidden by the sameness of the filling.
	copy(data, "head")
	copy(data[len(data)-4:], "tail")
	if _, err := db.ExecContext(ctx, "INSERT INTO probe (id, a) VALUES (?, ?)", 1, data); err != nil {
		t.Fatalf("writing %d bytes: %v", len(data), err)
	}
	var got []byte
	if err := db.QueryRowContext(ctx, "SELECT a FROM probe WHERE id = ?", 1).Scan(&got); err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("%d bytes came back, want %d", len(got), len(data))
	}
	if !bytes.Equal(got, data) {
		t.Error("the blob came back changed")
	}
}

// TestWritersWait proves that two writers do not trip over each other: with
// _txlock=immediate the second one waits at the start of its transaction,
// and busy_timeout gives it long enough to get its turn.
func TestWritersWait(t *testing.T) {
	db, err := OpenDB(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	const writers = 16
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errs[i] = err
				return
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO probe (a) VALUES (?)", []byte{byte(i)}); err != nil {
				tx.Rollback()
				errs[i] = err
				return
			}
			errs[i] = tx.Commit()
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	var rows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM probe").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != writers {
		t.Errorf("%d rows, want %d", rows, writers)
	}
}

// memProbe is the size of the file TestCopyFileHoldsOnlyAPieceOfTheFile
// copies. It has to be far larger than copyBuf and far larger than the bound
// the case allows, or the two readings - a piece at a time against the whole
// file - would not be told apart. The journal of a database under load was
// measured at 169 MB on 03.10.2026; 64 MiB is enough to show the difference
// without making every test run pay for it.
const memProbe = 64 << 20

// digest reads a file in pieces and returns its hash. It is here so that the
// case can say "the copy is the file" without holding either of them in
// memory - a test that allocated the file to check the copy would measure its
// own doing.
func digest(t *testing.T, path string) ([]byte, int64) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.CopyBuffer(h, f, make([]byte, copyBuf))
	if err != nil {
		t.Fatal(err)
	}
	return h.Sum(nil), n
}

// TestCopyFileHoldsOnlyAPieceOfTheFile is the bound a controller with 512 MB
// of RAM lives on. Backup used to read the file and the journal whole
// (os.ReadFile) and write them whole (os.WriteFile), which goes around the
// 512 MiB this package allows one connection and grows with the installation:
// 9,5 MB + 39,8 MB were measured on 03.10.2026, and the journal alone grew to
// 169 MB in twelve seconds under eight writers.
//
// What is measured is the allocation of one copy, not the time: TotalAlloc
// counts every byte handed out, so reading a file whole shows up in it
// however quickly the garbage collector takes it back again. The bound is
// 4 MiB against a 64 MiB file, so the two readings are sixteen times apart
// and no page cache or allocator rounding can bridge that.
func TestCopyFileHoldsOnlyAPieceOfTheFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.bin")
	dst := filepath.Join(dir, "big.copy")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	// Written in pieces for the same reason the copy is: a test that built
	// the whole file in memory would be the first thing to die on a Pi.
	piece := make([]byte, 1<<20)
	for i := range piece {
		piece[i] = byte(i * 7)
	}
	for written := 0; written < memProbe; written += len(piece) {
		if _, err := f.Write(piece); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// The measurement checks its own setup: a file that did not reach its
	// size would make any bound below look kept.
	if _, n := digest(t, src); n != memProbe {
		t.Fatalf("the probe file is %d bytes, want %d", n, memProbe)
	}

	buf := make([]byte, copyBuf)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	had, err := copyFile(src, dst, buf)
	runtime.ReadMemStats(&after)
	if err != nil || !had {
		t.Fatalf("copyFile: %v, %v", had, err)
	}
	grew := after.TotalAlloc - before.TotalAlloc
	t.Logf("copying %d bytes handed out %d bytes", memProbe, grew)
	const bound = 4 << 20
	if grew > bound {
		t.Errorf("copying %d bytes handed out %d bytes, more than the %d allowed: the file is being held whole",
			memProbe, grew, bound)
	}
	// And it is a copy, not a short read that happens to allocate little.
	wantSum, wantSize := digest(t, src)
	gotSum, gotSize := digest(t, dst)
	if gotSize != wantSize || !bytes.Equal(gotSum, wantSum) {
		t.Errorf("the copy is %d bytes against %d, hashes equal: %v", gotSize, wantSize, bytes.Equal(gotSum, wantSum))
	}
}
