package sqlstore_test

// The measuring bench of point 48 and of the write channel that answers it.
// It is not part of the normal set: every mode here costs seconds, some of
// them the full ten of busyTimeout, and it writes to a drive the caller
// names so that a number can say which drive it belongs to.
//
// Run it like this (PowerShell), one mode at a time:
//
//	$env:NGC_POOL_MEASURE="block"; $env:NGC_POOL_DIR="D:\claude\m"
//	go test -count=1 -buildvcs=false -v -timeout 30m -run TestPoolMeasure ./apps/server-db/classes/sqlstore
//
// Modes: "block" (blocked writers against the cost of the first read),
// "rate" (throughput), "migrate" (migrate.Apply against blocked writers).

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// logSeq hands every write of this bench an ID of its own. core.ValidLogID
// asks for 32 lower-case hex characters, and the primary key of the table
// refuses a second row with the same one -- a bench that wrote one ID over
// and over would measure the duplicate check, not the lock.
var logSeq atomic.Uint64

func logEntry() core.LogEntry {
	return core.LogEntry{
		ID:     fmt.Sprintf("%032x", logSeq.Add(1)),
		Time:   1790000000,
		Kind:   "login",
		Result: "ok",
	}
}

// measureDir is the directory the files of a run live in. It has to be named,
// because a number of this bench is worth nothing without the drive it ran
// on.
func measureDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("NGC_POOL_DIR")
	if dir == "" {
		t.Fatal("NGC_POOL_DIR is not set: a number of this bench belongs to one drive")
	}
	d := filepath.Join(dir, "run-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

// measureOpen opens one store of its own on path. As far as the file is
// concerned every store here is a program of its own: its own pool, its own
// connections, its own file locks.
func measureOpen(t *testing.T, path string, apply bool) (dbapi.Store, *sql.DB) {
	t.Helper()
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   path,
		Key:    config.NewSecret(bytes.Repeat([]byte{7}, 64)),
	}
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if apply {
		if _, err := migrate.Apply(ctx, db, sqlstore.SQLite, "measure", 1790000000); err != nil {
			t.Fatal(err)
		}
	}
	s := sqlstore.New(db, sqlstore.SQLite)
	return s, db
}

// holdLock takes the write lock of the file on a pool of its own and keeps it
// until the returned function runs. It is the foreign writer of point 48, and
// it is real: BEGIN IMMEDIATE takes the lock the moment it begins, which is
// what _txlock=immediate does for the pool as well.
//
// It checks its own work before the bench names a number: a lock that was
// never taken would let every writer below run through, and the run would
// look like a fast one instead of a broken one.
func holdLock(t *testing.T, path string) func() {
	t.Helper()
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   path,
		Key:    config.NewSecret(bytes.Repeat([]byte{7}, 64)),
	}
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("the foreign lock was not taken, so nothing below is measured: %v", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			conn.ExecContext(context.Background(), "ROLLBACK")
			conn.Close()
			db.Close()
		})
	}
}

func TestPoolMeasure(t *testing.T) {
	mode := os.Getenv("NGC_POOL_MEASURE")
	if mode == "" {
		t.Skipf("NGC_POOL_MEASURE is not set: this is the measuring bench, not a case")
	}
	switch mode {
	case "block":
		measureBlock(t)
	case "rate":
		measureRate(t)
	case "migrate":
		measureMigrate(t)
	default:
		t.Fatalf("unknown mode %q", mode)
	}
}

// measureBlock is the series of point 48: N writers that cannot get through,
// against the cost of the FIRST READ of the same program.
func measureBlock(t *testing.T) {
	dir := measureDir(t)
	fmt.Printf("dir=%s\n", dir)
	for _, n := range []int{1, 2, 3, 4, 5, 6, 8, 16, 32, 64, 192} {
		path := filepath.Join(dir, fmt.Sprintf("block-%02d.db", n))
		s, db := measureOpen(t, path, true)
		release := holdLock(t, path)

		var launched atomic.Int64
		var wg sync.WaitGroup
		stop := make(chan struct{})
		for i := range n {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				launched.Add(1)
				wctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
				defer cancel()
				s.LogAdd(wctx, logEntry())
				<-stop
			}(i)
		}
		// Give every writer the time to reach the driver and block there.
		time.Sleep(700 * time.Millisecond)

		// The bench checks its own preparation before it names a number.
		if got := launched.Load(); got != int64(n) {
			t.Fatalf("n=%d: only %d writers started; nothing measured", n, got)
		}
		inUse := db.Stats().InUse
		if inUse < 1 {
			t.Fatalf("n=%d: no connection is in use, so no writer is blocked; nothing measured", n)
		}

		rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
		start := time.Now()
		_, err := s.BlobGet(rctx, dbapi.BlobSetting, "nothing")
		read := time.Since(start)
		rcancel()
		ok := errors.Is(err, dbapi.ErrNotFound)

		fmt.Printf("n=%2d read=%8.3fs inuse=%d notfound=%v err=%v\n", n, read.Seconds(), inUse, ok, err)
		close(stop)
		release()
		wg.Wait()
		s.Close()
	}
}

// measureRate is what the change costs in throughput: writes nobody blocks,
// N at a time.
func measureRate(t *testing.T) {
	dir := measureDir(t)
	fmt.Printf("dir=%s\n", dir)
	for _, n := range []int{1, 4, 16, 64, 192} {
		path := filepath.Join(dir, fmt.Sprintf("rate-%03d.db", n))
		s, _ := measureOpen(t, path, true)
		var done atomic.Int64
		var wg sync.WaitGroup
		deadline := time.Now().Add(5 * time.Second)
		start := time.Now()
		for i := range n {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				for time.Now().Before(deadline) {
					if err := s.LogAdd(context.Background(), logEntry()); err != nil {
						return
					}
					done.Add(1)
				}
			}(i)
		}
		wg.Wait()
		el := time.Since(start)
		if done.Load() == 0 {
			t.Fatalf("n=%d: not one write got through; nothing measured", n)
		}
		fmt.Printf("n=%3d writes=%6d in %5.2fs -> %7.1f/s\n",
			n, done.Load(), el.Seconds(), float64(done.Load())/el.Seconds())
		s.Close()
	}
}

// measureMigrate asks whether a migration still gets through while N writers
// of the same program are at work. migrate.Apply has ONE deadline of ten
// seconds and no repeat of its own, so it is the most fragile caller there is.
func measureMigrate(t *testing.T) {
	dir := measureDir(t)
	fmt.Printf("dir=%s\n", dir)
	for _, n := range []int{2, 4, 8} {
		path := filepath.Join(dir, fmt.Sprintf("mig-%02d.db", n))
		s, db := measureOpen(t, path, true)

		var launched atomic.Int64
		var wg sync.WaitGroup
		stop := make(chan struct{})
		for i := range n {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				launched.Add(1)
				for {
					select {
					case <-stop:
						return
					default:
					}
					s.LogAdd(context.Background(), logEntry())
				}
			}(i)
		}
		time.Sleep(700 * time.Millisecond)
		if got := launched.Load(); got != int64(n) {
			t.Fatalf("n=%d: only %d writers started; nothing measured", n, got)
		}

		start := time.Now()
		ver, err := migrate.Apply(context.Background(), db, sqlstore.SQLite, "measure", 1790000000)
		el := time.Since(start)
		fmt.Printf("n=%2d migrate=%7.3fs ver=%d inuse=%d err=%v\n",
			n, el.Seconds(), ver, db.Stats().InUse, err)
		close(stop)
		wg.Wait()
		s.Close()
	}
}
