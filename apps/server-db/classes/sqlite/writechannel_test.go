package sqlite

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// The assurance of the one write channel: however many writers of this
// process are waiting for the file lock, a READ of this process does not wait
// for them.
//
// Measured 13.10.2026, which is why this file exists. With one pool of four
// connections a waiting writer held one of them, so four waiting writers
// owned the pool and the next read waited behind them: 2 blocked writers ->
// 0.007 s for the first read, 3 -> 0.007 s, 4 -> 9.5 s, on C: and on Z: (NAS)
// alike. The limit was not "so and so many users", it was the number four.
//
// The lock is held by a REAL second process (os.Args[0] re-executed), because
// SQLite's write lock is a lock on the file and a second handle in this
// process would not prove it holds across processes.
//
// The deadline below is not the measured number: a read that goes through
// takes about 60 us here, a read that waits takes the 10 s of busy_timeout.
// One second is a hundred times the one and a tenth of the other, so it
// separates the two cases without the case having to be fast.
const (
	blockedWriters = 4
	readDeadline   = time.Second
	helperHold     = 20 * time.Second
	helperEnvPath  = "NGC_TEST_BLOCK_PATH"
	helperEnvKey   = "NGC_TEST_BLOCK_KEY"
)

func TestAReadDoesNotWaitBehindThisProcessesBlockedWriters(t *testing.T) {
	cfg := testConfig(t, "state.db")
	st, err := Open(cfg, "test", 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	if err := st.UserPut(ctx, dbapi.User{ID: "u1", Name: "u1"}); err != nil {
		t.Fatalf("UserPut: %v", err)
	}

	// A second process takes the write lock and keeps it.
	cmd := exec.Command(os.Args[0], "-test.run=^TestBlockHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(),
		helperEnvPath+"="+cfg.Path,
		helperEnvKey+"="+hex.EncodeToString(cfg.Key.Bytes()))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	if err := waitForLine(out, "ngc-lock-held"); err != nil {
		t.Fatalf("the helper never took the lock: %v", err)
	}

	// blockedWriters writers of THIS process. Each one waits for the lock the
	// helper holds, and that is the point: it waits inside busy_timeout.
	var inflight atomic.Int64
	var wg sync.WaitGroup
	for i := range blockedWriters {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inflight.Add(1)
			defer inflight.Add(-1)
			st.LogAdd(ctx, core.LogEntry{
				ID: fmt.Sprintf("%032x", i), Time: 2, Kind: "probe", Result: "ok"})
		}(i)
	}
	// Setup check: all of them have to be in flight, or the case measures an
	// idle pool and passes for the wrong reason.
	deadline := time.Now().Add(5 * time.Second)
	for inflight.Load() < blockedWriters {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d writers are in flight - nothing is blocked",
				inflight.Load(), blockedWriters)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // reached the lock, not just the goroutine

	start := time.Now()
	u, err := st.UserGet(ctx, "u1")
	took := time.Since(start)
	// Setup check: the read has to have really happened. A read that returns
	// nothing would be fast for the wrong reason.
	if err != nil || u.Name != "u1" {
		t.Fatalf("the read did not happen: name=%q err=%v", u.Name, err)
	}
	if n := inflight.Load(); n != blockedWriters {
		t.Fatalf("%d of %d writers were no longer blocked at the read - the lock did not hold",
			n, blockedWriters)
	}
	if took > readDeadline {
		t.Fatalf("the read waited %v behind %d blocked writers, more than %v",
			took, blockedWriters, readDeadline)
	}
	cmd.Process.Kill()
	wg.Wait()
}

// TestBlockHelperProcess is the second process. It is a test so that the test
// binary can be the helper; without the environment variable it does nothing.
func TestBlockHelperProcess(t *testing.T) {
	path := os.Getenv(helperEnvPath)
	if path == "" {
		t.Skip("not the helper process")
	}
	key, err := hex.DecodeString(os.Getenv(helperEnvKey))
	if err != nil || len(key) != keySize {
		// Not Skipf: a helper that cannot do its job must say so loudly, or
		// the case above measures a lock nobody holds.
		t.Fatalf("helper: the key is not %d bytes: %v", keySize, err)
	}
	db, err := OpenDB(config.DB{Driver: config.DriverSQLite, Path: path, Key: config.NewSecret(key)})
	if err != nil {
		t.Fatalf("helper: OpenDB: %v", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("helper: BeginTx: %v", err)
	}
	defer tx.Rollback()
	// _txlock=immediate took the lock at BEGIN; a real write proves it is
	// held, so the line below is not a guess.
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO ngc_log (id, ts, user_id, kind, app, action, node, result_code, detail) "+
			"VALUES (?,?,?,?,?,?,?,?,?)",
		[]byte("ffffffffffffffffffffffffffffffff"), int64(1), []byte(""), []byte("probe"),
		[]byte(""), []byte(""), []byte(""), []byte("ok"), []byte("")); err != nil {
		t.Fatalf("helper: the lock is not held, nothing was written: %v", err)
	}
	fmt.Println("ngc-lock-held")
	os.Stdout.Sync()
	time.Sleep(helperHold)
}

// waitForLine reads lines until want shows up, or gives up.
func waitForLine(r interface{ Read([]byte) (int, error) }, want string) error {
	br := bufio.NewReader(r)
	for i := 0; i < 200; i++ {
		line, err := br.ReadString('\n')
		if len(line) > 0 && trimLine(line) == want {
			return nil
		}
		if err != nil {
			return fmt.Errorf("after %d lines: %v", i, err)
		}
	}
	return fmt.Errorf("%s never came", strconv.Quote(want))
}

func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// TestTheWriteHandleHasOneConnection pins the other half of the shape: the
// handle that writes carries exactly one connection, so a writer that waits
// waits in Go and not on a connection somebody else needs.
func TestTheWriteHandleHasOneConnection(t *testing.T) {
	write, read, err := OpenRW(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	defer read.Close()
	if got := write.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("the write handle has %d connections, want 1", got)
	}
	if got := read.Stats().MaxOpenConnections; got != readConns {
		t.Errorf("the read handle has %d connections, want %d", got, readConns)
	}
	// The two are different handles, not the same one twice: the whole point
	// is that a busy write handle leaves the read handle alone.
	if write == read {
		t.Error("write and read are the same handle")
	}
}

// TestTheStoreReadsThroughTheReadHandle proves which handle a read uses, and
// it does so without looking at the code: it takes the ONE connection of the
// write handle and holds it, then reads. If the read went through the write
// handle it would wait for the connection; through the read handle it goes.
func TestTheStoreReadsThroughTheReadHandle(t *testing.T) {
	cfg := testConfig(t, "state.db")
	write, read, err := OpenRW(cfg)
	if err != nil {
		t.Fatal(err)
	}
	st := NewSplit(write, read)
	defer st.Close()
	if _, err := migrate.Apply(ctx, write, sqlstore.SQLite, "test", 1); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if err := st.UserPut(ctx, dbapi.User{ID: "u1", Name: "u1"}); err != nil {
		t.Fatalf("UserPut: %v", err)
	}
	conn, err := write.Conn(ctx)
	if err != nil {
		t.Fatalf("taking the only write connection: %v", err)
	}
	defer conn.Close()
	// Setup check: the write handle really has nothing left to give.
	if st := write.Stats(); st.OpenConnections != 1 || st.InUse != 1 {
		t.Fatalf("the write handle has %d open and %d in use, want 1 and 1",
			st.OpenConnections, st.InUse)
	}
	done := make(chan error, 1)
	go func() {
		u, err := st.UserGet(ctx, "u1")
		if err == nil && u.Name != "u1" {
			err = fmt.Errorf("the read gave name %q", u.Name)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the read failed: %v", err)
		}
	case <-time.After(readDeadline):
		t.Fatalf("the read waited more than %v for the write handle's connection", readDeadline)
	}
}
