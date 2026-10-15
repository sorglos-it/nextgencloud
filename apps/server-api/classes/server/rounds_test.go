package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// safeBuf is a bytes.Buffer a case may read while something else logs into
// it, and it is a measurement and not caution: a case that runs Server.rounds
// on a goroutine and waits for a line reads the buffer while that goroutine
// writes it, and -race reported exactly that (3 DATA RACE in
// TestTheBeatGivesEachRoundItsOwnLock, 15.10.2026, slog's handler growing the
// buffer against the case's String). The cases that call a round in line never
// touched it, which is why a plain buffer stood here for as long as they were
// the only ones.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// logging catches what a round writes. A background round has nobody to
// answer, so the log line is the WHOLE of what it can say -- which is why a
// round without a reason in it is the same fault the handlers had before
// point 49, only with no header to make up for it.
func logging(t *testing.T) *safeBuf {
	t.Helper()
	buf := &safeBuf{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

// stuckStore is the database of the measurement: a second program holds the
// file, so every call waits. What it hands back when the wait ends is the
// CAUSE of the context (sqlstore.cause, point 49), so a round that sets a
// deadline of dbwait sees dbwait.ErrBusy and one that sets none sees the
// case's own cap.
type stuckStore struct {
	dbapi.Store
	calls atomic.Int64
}

func (s *stuckStore) hold(ctx context.Context) error {
	s.calls.Add(1)
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	// The cap belongs to the case and not to the thing under test: a round
	// with no deadline waits the measured 80,6 s of point 48, and a case that
	// sits that long is no assurance -- it has to end and say what it waited
	// for. roundCap is well under that figure and well over any deadline of
	// dbwait, so whichever ends the wait is unambiguous.
	case <-time.After(roundCap):
		return errTestNoDeadline
	}
}

// roundCap is the case's own patience. It stands above dbwait.RoundWait so a
// deadline of the code always wins the race, and below the 80,6 s a store
// call really takes without one, so a round that sets none is caught here and
// not after a minute and twenty seconds.
var roundCap = dbwait.RoundWait + 10*time.Second

var errTestNoDeadline = errNoDeadline{}

type errNoDeadline struct{}

func (errNoDeadline) Error() string { return "the round set no deadline at all" }

func (s *stuckStore) SessionPurge(ctx context.Context, now int64) error {
	return s.hold(ctx)
}

// TestTheHousekeepingRoundBoundsTheDatabase is the assurance of the deadline.
// Measured before: the round handed s.minute the server's own context, which
// has no deadline at all, so one busy database held the round for the 80,6 s
// of point 48 -- and because minute and hour shared s.round then, that hang
// dropped the ticks of BOTH of them. They hold a lock each now
// (TestTheRoundsDoNotShareALock and TestTheBeatGivesEachRoundItsOwnLock),
// so this round's hang is this round's alone.
//
// What a round may do that a user path may not is come back later: the next
// tick is a minute away, so giving up and letting the beat try again is the
// whole cost. That is why this deadline is its own figure and not UserWait.
func TestTheHousekeepingRoundBoundsTheDatabase(t *testing.T) {
	srv, db, _ := newServer(t)
	stuck := &stuckStore{Store: db}
	srv.State.DB = stuck

	start := time.Now()
	srv.minute(ctx)
	took := time.Since(start)

	// The intermediate check: the round really reached the blocked call, so a
	// short time below is the deadline and not a round that did nothing.
	if stuck.calls.Load() == 0 {
		t.Fatal("the round made no store call, so this case measures an earlier wall")
	}
	if took >= roundCap {
		t.Fatalf("the round waited %v, so it carries no deadline; without one a busy "+
			"database holds it for the measured 80,6 s", took)
	}
	if took > 2*dbwait.RoundWait {
		t.Fatalf("the round took %v against a deadline of %v", took, dbwait.RoundWait)
	}
}

// TestAStuckRoundSaysWhyInTheLog is the second half: a round has no caller to
// answer, so if the reason is not in the log it is nowhere. Before the change
// the line read "housekeeping failed" with the bare context error beside it,
// which says neither busy nor away.
func TestAStuckRoundSaysWhyInTheLog(t *testing.T) {
	srv, db, _ := newServer(t)
	buf := logging(t)
	srv.State.DB = &stuckStore{Store: db}

	srv.minute(ctx)

	got := buf.String()
	if !strings.Contains(got, "db-busy") {
		t.Fatalf("the round's log names no reason: %q", got)
	}
	// And the reason is a word of ours and not the standard library's, which
	// is the difference between "the database is busy" and a line an admin
	// cannot act on.
	if strings.Contains(got, "context deadline exceeded") {
		t.Fatalf("the bare context error is in the line: %q", got)
	}
}

// bareStore is the store the comment on roundReason claims to be right for:
// one that lets the deadline through as the standard library writes it, the
// bare context.DeadlineExceeded with no cause of ours in it. apps/server-db
// was taught to translate (point 49, 11 return points), so this is not that
// store -- it is every other implementation of dbapi.Store, memstore
// included, and the claim that asking the CONTEXT covers them is held here
// instead of being asserted in a comment.
type bareStore struct {
	dbapi.Store
}

func (bareStore) SessionPurge(ctx context.Context, now int64) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestARoundNamesBusyEvenForAStoreThatDoesNotTranslate is the half of
// roundReason a handler cannot have: the round holds the context it set, so it
// can ask the cause at the source.
func TestARoundNamesBusyEvenForAStoreThatDoesNotTranslate(t *testing.T) {
	srv, db, _ := newServer(t)
	buf := logging(t)
	srv.State.DB = bareStore{Store: db}

	srv.minute(ctx)

	got := buf.String()
	if !strings.Contains(got, "reason=db-busy") {
		t.Fatalf("a bare context deadline was not named as busy: %q", got)
	}
}

// TestARoundWithNoTrustListYetIsNoDatabaseFault is the honesty of the last
// arm, and it is the one a fix writes wrong most easily: the commonest error
// of this round is state.ErrNoTrust -- "no trust list yet", the normal state
// of an installation whose setup is still open. A reason that called it a
// database fault would put a fault in the log where there is none.
func TestARoundWithNoTrustListYetIsNoDatabaseFault(t *testing.T) {
	// newServer builds over an empty database, which is exactly that state:
	// the trust read fails and nothing is wrong.
	srv, _, _ := newServer(t)
	buf := logging(t)

	srv.minute(ctx)

	got := buf.String()
	// The intermediate check: the line this case is about was really written.
	if !strings.Contains(got, "trust list could not be read") {
		t.Fatalf("the trust read did not fail, so this case measures nothing: %q", got)
	}
	if strings.Contains(got, "db-unreachable") || strings.Contains(got, "db-busy") {
		t.Fatalf("a missing trust list is logged as a database fault: %q", got)
	}
	if !strings.Contains(got, "reason=other") {
		t.Fatalf("the line carries no reason of its own: %q", got)
	}
}

// TestTheRoundDeadlineFitsBetweenTheBeatAndTheRetryChain holds the FIGURE and
// not only the fact, the way point 49 holds UserWait: a deadline nobody sized
// would pass a check that only asks whether there is one.
//
// Three bounds, each with its measurement:
//
//   - under the beat (Minute, one minute), so a round that cannot get the
//     database frees it before the next tick is due instead of dropping it;
//   - over one busy_timeout (10 s, sqlite.go:27), so a round that loses one
//     race still finishes its work;
//   - under the full retry chain (80,6 s, point 48), because waiting it out is
//     exactly what a repeating round does not have to do.
func TestTheRoundDeadlineFitsBetweenTheBeatAndTheRetryChain(t *testing.T) {
	const busyTimeout = 10 * time.Second
	const retryChain = 80*time.Second + 600*time.Millisecond
	if dbwait.RoundWait >= time.Minute {
		t.Fatalf("RoundWait = %v, not under the minute beat", dbwait.RoundWait)
	}
	if dbwait.RoundWait <= busyTimeout {
		t.Fatalf("RoundWait = %v, not over one busy_timeout of %v", dbwait.RoundWait, busyTimeout)
	}
	if dbwait.RoundWait >= retryChain {
		t.Fatalf("RoundWait = %v, not under the retry chain of %v", dbwait.RoundWait, retryChain)
	}
	// And it is longer than the user deadline: nobody waits at a screen for a
	// round, so the one thing it may spend more of is time.
	if dbwait.RoundWait <= dbwait.UserWait {
		t.Fatalf("RoundWait %v is not above UserWait %v", dbwait.RoundWait, dbwait.UserWait)
	}
}

// blobStuck is the database of the CATALOG round's measurement: the first
// thing appstore.Refresh does is read the trust list, and that read is a
// BlobGet (state.readBlock). A store that holds it holds the round.
type blobStuck struct {
	dbapi.Store
	calls atomic.Int64
}

func (s *blobStuck) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	s.calls.Add(1)
	select {
	case <-ctx.Done():
		return dbapi.Blob{}, context.Cause(ctx)
	case <-time.After(roundCap):
		return dbapi.Blob{}, errTestNoDeadline
	}
}

// TestTheCatalogRoundBoundsTheDatabase is the assurance Server.hour did not
// have. Measured on 15.10.2026 before the change, against a blocked database:
// appstore.Refresh ran into the measurement's own cap of 20,003 s, because no
// deadline of ours bounded a single database call on that path -- in operation
// that is the 80,6 s of point 48.
//
// The deadline is NOT on the round: its fetch is bounded by the store client's
// own measured 60 s and thirty seconds over the round would cut a fetch doing
// nothing wrong. It is on each database call inside Refresh, which is why this
// case measures the round and the one below measures which part got which.
func TestTheCatalogRoundBoundsTheDatabase(t *testing.T) {
	srv, db, _ := newServer(t)
	stuck := &blobStuck{Store: db}
	srv.State.DB = stuck

	start := time.Now()
	srv.hour(ctx)
	took := time.Since(start)

	// The intermediate check: the round really reached the blocked call, so a
	// short time below is the deadline and not a round that did nothing.
	if stuck.calls.Load() == 0 {
		t.Fatal("the catalog round made no store call, so this case measures an earlier wall")
	}
	if took >= roundCap {
		t.Fatalf("the catalog round waited %v, so no database call on its path carries a "+
			"deadline; without one a busy database holds it for the measured 80,6 s", took)
	}
	if took > 2*dbwait.RoundWait {
		t.Fatalf("the catalog round took %v against a deadline of %v", took, dbwait.RoundWait)
	}
}

// TestAStuckCatalogRoundSaysWhyInTheLog is the other half, and it was the
// worse one: measured before the change, Refresh dropped the error of its
// last database call (`_ = st.forget(...)`) and answered nil, so this round
// logged nothing at all -- not a wrong reason, no line. A background round
// has nobody to answer, so that was eighty seconds of waiting and silence.
func TestAStuckCatalogRoundSaysWhyInTheLog(t *testing.T) {
	srv, db, _ := newServer(t)
	buf := logging(t)
	srv.State.DB = &blobStuck{Store: db}

	srv.hour(ctx)

	got := buf.String()
	if !strings.Contains(got, "catalogs could not be fetched") {
		t.Fatalf("the catalog round wrote no line at all for a database nobody can reach: %q", got)
	}
	if !strings.Contains(got, "reason=db-busy") {
		t.Fatalf("the catalog round's log names no reason: %q", got)
	}
	if strings.Contains(got, "context deadline exceeded") {
		t.Fatalf("the bare context error is in the line: %q", got)
	}
}

// TestTheRoundsDoNotShareALock is the decoupling, and the number behind it is
// the one that makes it necessary: the catalog round's fetch is bounded by
// appstore's http.Client alone, measured 60,001 s PER REQUEST on 15.10.2026,
// so a trust list with four unreachable stores holds that round for four
// minutes. Under one shared lock every minute tick in them was dropped, and
// the minute round is what logs expired sessions out.
//
// This is the case TestOnlyOneRoundAtATime cannot be: that one holds "a round
// does not run twice", this one holds "the other round is not the same round".
func TestTheRoundsDoNotShareALock(t *testing.T) {
	srv := &Server{}
	inside := make(chan struct{})
	release := make(chan struct{})
	go srv.once(&srv.hourly, func() { close(inside); <-release })
	<-inside
	defer close(release)

	ran := false
	srv.once(&srv.round, func() { ran = true })
	if !ran {
		t.Fatal("the housekeeping tick was dropped although only the catalog round was busy")
	}
	// The intermediate check, so this cannot pass against a once that locks
	// nothing: the busy round's OWN tick is still dropped.
	second := false
	srv.once(&srv.hourly, func() { second = true })
	if second {
		t.Fatal("a second catalog round started on top of the first")
	}
}

// TestTheBeatGivesEachRoundItsOwnLock is the half TestTheRoundsDoNotShareALock
// cannot hold: that one calls once with the lock it means, so it says nothing
// about which lock Server.rounds HANDS IN. Measured: a mutant that dispatches
// the hourly tick on s.round again passed that case and was only caught here.
//
// It holds the HOUSEKEEPING lock and waits for the catalog round, not the
// other way round, because that is the direction the mutant breaks: with one
// shared lock a held s.round drops every hourly tick.
func TestTheBeatGivesEachRoundItsOwnLock(t *testing.T) {
	srv, _, _ := newServer(t)
	buf := logging(t)
	oldM, oldH := Minute, Hour
	// The housekeeping beat stays out of the way: this case is about the
	// hourly tick, and a minute round in it would write lines of its own.
	Minute, Hour = time.Hour, 5*time.Millisecond
	t.Cleanup(func() { Minute, Hour = oldM, oldH })

	srv.round.Lock()
	defer srv.round.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); srv.rounds(ctx) }()

	// The catalog round over an empty database fails with state.ErrNoTrust and
	// says so; that line is how this case sees it ran at all.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), "catalogs could not be fetched") {
		if time.Now().After(deadline) {
			t.Fatalf("no catalog round ran in 10 s while the housekeeping lock was held, "+
				"so the hourly tick is dispatched on that same lock: %q", buf.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}
