package sqlstore_test

// The write channel: one write of a program at a time, so that a writer who
// cannot get through does not hold the connection the next READ needs.
//
// Why it had to be built is measured and not reasoned - blocked writers
// against the cost of the first read of the same program, on D: on
// 15.10.2026: 3 -> 0.003 s, 4 -> 9.300 s. The pool keeps four connections,
// and a writer waiting for the file lock held the one he waited on.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// write1Seq hands every write of these cases an ID of its own: the primary
// key of ngc_log refuses a second row with the same one, and a case that
// wrote one ID over and over would be measuring the duplicate check instead
// of the lock.
var write1Seq atomic.Uint64

func write1Entry() core.LogEntry {
	return core.LogEntry{
		ID:     idHex(write1Seq.Add(1)),
		Time:   1790000000,
		Kind:   "login",
		Result: "ok",
	}
}

func idHex(n uint64) string {
	const hexdigits = "0123456789abcdef"
	b := make([]byte, 32)
	for i := range b {
		b[i] = '0'
	}
	for i := 31; i >= 0 && n > 0; i-- {
		b[i] = hexdigits[n&0xf]
		n >>= 4
	}
	return string(b)
}

// write1Fixture is one program on one file: its own pool, its own
// connections, its own file locks. It hands back the store, the pool behind
// it (for Stats, which is how many connections are really checked out) and
// the path, so a second program can take the lock of the same file.
func write1Fixture(t *testing.T, d sqlstore.Dialect) (dbapi.Store, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db := write1Open(t, path)
	if _, err := migrate.Apply(ctx, db, sqlstore.SQLite, "write1", 1790000000); err != nil {
		t.Fatal(err)
	}
	s := sqlstore.New(db, d)
	t.Cleanup(func() { s.Close() })
	return s, db, path
}

func write1Open(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sqlite.OpenDB(config.DB{
		Driver: config.DriverSQLite,
		Path:   path,
		Key:    config.NewSecret(bytes.Repeat([]byte{9}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// write1HoldLock is the foreign writer: a second program that holds the write
// lock of the file until the case lets go. BEGIN IMMEDIATE takes the lock the
// moment it begins, and a lock that was never taken is a case that measures
// nothing - so it is checked and not assumed.
func write1HoldLock(t *testing.T, path string) {
	t.Helper()
	db := write1Open(t, path)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("the foreign lock was not taken, so this case measures nothing: %v", err)
	}
	t.Cleanup(func() {
		conn.ExecContext(context.Background(), "ROLLBACK")
		conn.Close()
		db.Close()
	})
}

// write1Blocked starts n writers that cannot get through and waits until they
// have all reached the database. It gives back the highest number of
// connections it saw checked out while they were waiting.
//
// The count comes from db.Stats of the real pool and not from a number of
// ours, so the case cannot drift away from the pool it is about.
func write1Blocked(t *testing.T, s dbapi.Store, db *sql.DB, n int) int {
	t.Helper()
	var launched atomic.Int64
	// The writers go away when the case is over, and they go away by having
	// their context cancelled: waiting out a deadline instead would make
	// every case here cost that deadline.
	wctx, cancelWriters := context.WithCancel(context.Background())
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			launched.Add(1)
			s.LogAdd(wctx, write1Entry())
			<-stop
		}()
	}
	t.Cleanup(func() {
		cancelWriters()
		close(stop)
		wg.Wait()
	})

	// Let every writer reach the driver, and watch the pool while they do.
	peak := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if in := db.Stats().InUse; in > peak {
			peak = in
		}
		if launched.Load() == int64(n) && peak >= 1 {
			// Keep watching a moment longer: the last writer may still be on
			// his way into the pool, and a peak read too early would be the
			// case measuring its own haste.
			time.Sleep(300 * time.Millisecond)
			if in := db.Stats().InUse; in > peak {
				peak = in
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := launched.Load(); got != int64(n) {
		t.Fatalf("only %d of %d writers started; nothing measured", got, n)
	}
	if peak < 1 {
		t.Fatal("no connection was ever checked out, so no writer was blocked; nothing measured")
	}
	return peak
}

// TestABlockedWriteDoesNotHoldTheReadsOfItsProgram is the assurance of this
// change. As many writers as the pool has connections cannot get through, and
// a read of the same program still answers at once.
//
// Red before the change, at this very line: the read waited 9.3 s for a
// connection and the two-second deadline below ended it. The deadline is two
// seconds against a measured 9.3, so it separates the two states by a factor
// of four and does not sit on the edge of either.
func TestABlockedWriteDoesNotHoldTheReadsOfItsProgram(t *testing.T) {
	s, db, path := write1Fixture(t, sqlstore.SQLite)
	write1HoldLock(t, path)

	// As many blocked writers as the pool has connections - asked of the pool
	// itself, so that the case cannot test a number against the same number.
	full := db.Stats().MaxOpenConnections
	if full < 2 {
		t.Fatalf("the pool reports %d connections; this case needs at least two", full)
	}
	write1Blocked(t, s, db, full)

	rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := s.BlobGet(rctx, dbapi.BlobSetting, "nothing")
	el := time.Since(start)
	if !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("a read with %d writers blocked took %v and answered %v, want %v",
			full, el, err, dbapi.ErrNotFound)
	}
}

// TestOnlyOneWriteOfThisProgramHoldsAConnection says the same thing from the
// side of the pool, because the case above would also pass if the pool simply
// had more connections. Red before the change: the peak was 4.
func TestOnlyOneWriteOfThisProgramHoldsAConnection(t *testing.T) {
	s, db, path := write1Fixture(t, sqlstore.SQLite)
	write1HoldLock(t, path)

	full := db.Stats().MaxOpenConnections
	peak := write1Blocked(t, s, db, full+4) // more writers than the pool has room for
	if peak != 1 {
		t.Fatalf("%d connections were checked out at once; one write at a time means 1", peak)
	}
}

// TestAWriteWithoutATransactionTakesItsTurnToo covers the OTHER writing way
// of this store. write runs a transaction; exec runs one statement that needs
// none - a DELETE, a purge, a touch of a session - and it writes just as
// much. Without this case the turn would be measured on one of the two ways
// only, and a change that forgot exec would look green.
func TestAWriteWithoutATransactionTakesItsTurnToo(t *testing.T) {
	s, db, path := write1Fixture(t, sqlstore.SQLite)
	write1HoldLock(t, path)

	full := db.Stats().MaxOpenConnections
	var launched atomic.Int64
	wctx, cancelWriters := context.WithCancel(context.Background())
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := range full + 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			launched.Add(1)
			// BlobDelete goes through exec, not through write.
			s.BlobDelete(wctx, dbapi.BlobSetting, "nothing-"+idHex(uint64(i)))
			<-stop
		}(i)
	}
	defer func() {
		cancelWriters()
		close(stop)
		wg.Wait()
	}()

	peak := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if in := db.Stats().InUse; in > peak {
			peak = in
		}
		if launched.Load() == int64(full+4) && peak >= 1 {
			time.Sleep(300 * time.Millisecond)
			if in := db.Stats().InUse; in > peak {
				peak = in
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := launched.Load(); got != int64(full+4) {
		t.Fatalf("only %d of %d writers started; nothing measured", got, full+4)
	}
	if peak != 1 {
		t.Fatalf("%d connections were checked out at once by statements without a transaction; "+
			"one write at a time means 1", peak)
	}
}

// TestMariaDBKeepsItsParallelPool is point 3 of this task as a case and not as
// a sentence: for MariaDB the four connections are four real writers on a
// server that handles them, and the write channel must not reach that path.
//
// It runs on the same file and the same statements as the two cases above -
// every statement sqlstore writes is the same text for both databases, the
// dialect only answers for column types and for the meaning of an error - so
// the ONLY difference between this case and the one above is the dialect.
// That is what makes it a measurement of the dialect and not of a second
// setup.
//
// It is green before the change as well, and that is the point: it dies the
// moment the channel is turned on for everybody.
func TestMariaDBKeepsItsParallelPool(t *testing.T) {
	s, db, path := write1Fixture(t, sqlstore.MariaDB)
	write1HoldLock(t, path)

	full := db.Stats().MaxOpenConnections
	peak := write1Blocked(t, s, db, full+4)
	if peak <= 1 {
		t.Fatalf("MariaDB saw %d connections checked out at once; its pool must stay parallel", peak)
	}
}

// TestAMigrationGetsThroughWhileWritersWork is point 5: migrate.Apply has ONE
// deadline of ten seconds and no repeat of its own, so it is the most fragile
// caller on the file. Here nobody holds a foreign lock - the writers are the
// load, and they really write.
func TestAMigrationGetsThroughWhileWritersWork(t *testing.T) {
	s, db, _ := write1Fixture(t, sqlstore.SQLite)

	var wrote atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range db.Stats().MaxOpenConnections {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := s.LogAdd(context.Background(), write1Entry()); err == nil {
					wrote.Add(1)
				}
			}
		}()
	}
	defer func() {
		close(stop)
		wg.Wait()
	}()
	time.Sleep(300 * time.Millisecond)
	if wrote.Load() == 0 {
		t.Fatal("not one write got through, so the migration below meets no load; nothing measured")
	}

	// Apply answers with the number of migrations it RAN, so zero is the
	// right answer here - the schema of the fixture is already there. What
	// this case is about is that it gets through at all: bootstrap and take
	// go over db.ExecContext with the one deadline of ten seconds and no
	// repeat of their own, so under load they are the first thing to break.
	start := time.Now()
	if _, err := migrate.Apply(context.Background(), db, sqlstore.SQLite, "write1", 1790000000); err != nil {
		t.Fatalf("a migration under %d writers after %v: %v", db.Stats().MaxOpenConnections,
			time.Since(start), err)
	}
	// A second run has to get through as well, which is how this case sees
	// that the first one gave the lock back instead of leaving it standing:
	// a held lock is the one error Apply names in as many words.
	if _, err := migrate.Apply(context.Background(), db, sqlstore.SQLite, "write1", 1790000000); err != nil {
		t.Fatalf("the second migration under load: %v", err)
	}
}
