package sqlite

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/core"
)

// TestTheTurnBoundIsOneChainOfTheOpenDatabase asks the OPEN DATABASE for its
// busy_timeout and checks the bound against that, not against
// DefaultBusyTimeout in this package. Both numbers are written out besides,
// because a bound that is only compared with the constant it came from is not
// checked at all: a mutation of DefaultBusyTimeout alone has to be visible
// here.
func TestTheTurnBoundIsOneChainOfTheOpenDatabase(t *testing.T) {
	write, read, err := OpenRW(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	defer read.Close()

	var ms int64
	if err := write.QueryRow("PRAGMA busy_timeout").Scan(&ms); err != nil {
		t.Fatalf("the open database does not say its busy_timeout: %v", err)
	}
	if ms != 10000 {
		t.Errorf("the open database waits %d ms for a lock, want 10000", ms)
	}
	if DefaultBusyTimeout != 10*time.Second {
		t.Errorf("DefaultBusyTimeout is %v, and the measurement of the bound is 10s", DefaultBusyTimeout)
	}
	// Eight tries of ten seconds: the figure dbwait.StartWait's ninety
	// seconds are built on. Written out, not computed from maxTries, which
	// this package cannot see anyway.
	if got := time.Duration(ms) * time.Millisecond * 8; got != 80*time.Second {
		t.Errorf("one chain of this database is %v, want 1m20s", got)
	}
}

// TestASecondWriterIsNotMadeToWaitAChainPerWriter is the case the measurement
// of 16.10.2026 asked for: against a lock that never comes free, three writers
// without a deadline of their own used to take about one chain EACH (190 s,
// 220 s, 240 s measured, 2 of 2), because the write handle of one connection
// is itself an unbounded queue.
//
// It cannot run against the real chain - that is four minutes - so it runs
// against a database whose own busy_timeout is short, and the bound follows
// the database because sqlstore reads it from there. That is the point of
// reading it from the open file instead of writing it down: the same code
// under a small number behaves like the real one under the big number.
//
// What is held: with n writers the last one is NOT n chains late. The bound is
// two chains, his wait plus the chain of whoever holds the turn.
func TestASecondWriterIsNotMadeToWaitAChainPerWriter(t *testing.T) {
	cfg := testConfig(t, "state.db")
	// The schema first, with nothing holding the file: Open migrates, and
	// migrate has no retry of its own.
	first, err := Open(cfg, "case", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	// A short busy_timeout for this one file, so one chain is well under a
	// second. It comes through Options, which is what Options is for: the
	// deadline is the one thing a measurement has to be able to shorten, and
	// EVERY connection of both handles gets it, because the connector runs
	// the PRAGMA on each one. Setting it with a bare Exec would reach only
	// whichever connection served that statement.
	const turnBusy = 25 * time.Millisecond
	write, readH, err := OpenRWWith(cfg, Options{BusyTimeout: turnBusy})
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	defer readH.Close()
	var ms int64
	if err := write.QueryRow("PRAGMA busy_timeout").Scan(&ms); err != nil || ms != turnBusy.Milliseconds() {
		t.Fatalf("the short timeout did not take: %d ms, %v", ms, err)
	}
	chain := time.Duration(ms) * time.Millisecond * 8 // 200 ms

	st := sqlstore.NewSplit(write, readH, sqlstore.SQLite)

	// Somebody else holds the write lock of the file for the whole case. A
	// second handle of this process will do: SQLite locks the FILE, not the
	// process.
	locker, err := OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	tx, err := locker.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("CREATE TABLE IF NOT EXISTS turncase (a INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// The tightness probe: with that lock held, one write of the store has to
	// fail. If it goes through, the lock is not held and the numbers below
	// would be about nothing.
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelProbe()
	if err := st.LogAdd(probeCtx, logEntry(90001)); err == nil {
		t.Fatal("a write went through while the file lock was held, so this case measures nothing")
	}

	const n = 4
	took := make([]time.Duration, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	t0 := time.Now()
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := time.Now()
			errs[i] = st.LogAdd(context.Background(), logEntry(90100+i))
			took[i] = time.Since(s)
		}(i)
	}
	wg.Wait()
	all := time.Since(t0)

	// Every one of them has to end: either with the turn that never came, or
	// with the database saying it is locked. None may succeed.
	turns := 0
	for i := range n {
		if errs[i] == nil {
			t.Fatalf("writer %d wrote although the file was locked", i+1)
		}
		if errors.Is(errs[i], sqlstore.ErrTurn) {
			turns++
		}
	}
	if turns == 0 {
		t.Errorf("not one of the %d writers hit the bound; the queue was not bounded", n)
	}
	// Two chains, with room for the scheduler. Without the bound this is n
	// chains, which at n=4 is twice as much as the line below allows.
	if limit := 2*chain + 2*time.Second; all > limit {
		t.Errorf("the %d writers took %v, want at most %v (two chains of %v plus slack)",
			n, all, limit, chain)
	}
	for i := range n {
		if took[i] > 2*chain+2*time.Second {
			t.Errorf("writer %d waited %v, more than two chains", i+1, took[i])
		}
	}
}

// TestTheTurnIsGivenBackSoLaterWritesGoThrough holds the other half: the bound
// must not leave the turn taken. Without a free lock every write goes through,
// one after the other, and the error of the bound never appears.
func TestTheTurnIsGivenBackSoLaterWritesGoThrough(t *testing.T) {
	st, err := Open(testConfig(t, "state.db"), "case", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := range 12 {
		if err := st.LogAdd(context.Background(), logEntry(91000+i)); err != nil {
			t.Fatalf("write %d of 12 failed, so a turn was not given back: %v", i+1, err)
		}
	}
	// And the same through the other path, exec rather than write.
	if err := st.SessionPurge(context.Background(), time.Now().Unix()); err != nil {
		t.Fatalf("a statement without a transaction failed: %v", err)
	}
}

// logEntry is one row with an ID of 32 lower-case hex characters.
func logEntry(i int) core.LogEntry {
	h := "0123456789abcdef"
	var b strings.Builder
	for s := 0; s < 32; s++ {
		b.WriteByte(h[(i>>(uint(s)%8*4))&0xf])
	}
	return core.LogEntry{ID: b.String(), Time: int64(i) + 1, Kind: "probe", Result: "ok"}
}
