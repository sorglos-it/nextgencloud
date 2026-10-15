package appstore

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// seenRT records how much time the NETWORK call is given. It delegates to the
// fixture's own transport, so the fetch really happens and this is not a case
// that measures a refusal before the request.
//
// It asks for the time LEFT and not for whether a deadline exists, and that is
// a correction a first version of this case needed: net/http puts
// Client.Timeout on the request context itself, so a fetch always has one.
// What tells the two candidates apart is the figure -- the client's measured
// 60 s, or dbwait.RoundWait at half of it.
type seenRT struct {
	base http.RoundTripper
	mu   sync.Mutex
	hits int
	left time.Duration
}

func (s *seenRT) RoundTrip(r *http.Request) (*http.Response, error) {
	rest := time.Duration(1 << 62)
	if dl, ok := r.Context().Deadline(); ok {
		rest = time.Until(dl)
	}
	s.mu.Lock()
	s.hits++
	if s.hits == 1 || rest < s.left {
		s.left = rest
	}
	s.mu.Unlock()
	return s.base.RoundTrip(r)
}

// seenDB records the deadline of the context a DATABASE call runs under.
type seenDB struct {
	dbapi.Store
	mu    sync.Mutex
	calls int
	noneA int
	worst time.Duration
}

func (d *seenDB) saw(ctx context.Context) {
	dl, ok := ctx.Deadline()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if !ok {
		d.noneA++
		return
	}
	if left := time.Until(dl); left > d.worst {
		d.worst = left
	}
}

func (d *seenDB) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	d.saw(ctx)
	return d.Store.BlobGet(ctx, kind, id)
}

func (d *seenDB) BlobPut(ctx context.Context, b dbapi.Blob) error {
	d.saw(ctx)
	return d.Store.BlobPut(ctx, b)
}

func (d *seenDB) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	d.saw(ctx)
	return d.Store.BlobFrom(ctx, kind, seq)
}

// TestRefreshBoundsTheDatabaseAndNotTheFetch is the shape of the decision, in
// numbers rather than in a comment: a deadline PER PART.
//
// Measured on 15.10.2026 before the change: a blocked database call ran into
// the measurement's own cap of 20,003 s (no bound of ours at all), while a
// store that never answers ends at 60,001 s, which is Stores.Client's own
// Timeout. One deadline over the whole round could only be wrong in one of
// the two directions, so there is one per part and this case says which got
// which.
func TestRefreshBoundsTheDatabaseAndNotTheFetch(t *testing.T) {
	fx, _ := newFixture(t)
	rt := &seenRT{base: fx.HTTP.Client().Transport}
	fx.St.Client = &http.Client{Timeout: fx.St.Client.Timeout,
		CheckRedirect: fx.St.Client.CheckRedirect, Transport: rt}
	db := &seenDB{Store: fx.S.DB}
	fx.S.DB = db

	// No deadline goes in: this is the round's own context (server.Server.hour
	// hands its context on unchanged), so everything measured below was put on
	// by Refresh itself.
	if err := fx.St.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// The intermediate checks: both parts were really reached, so a verdict
	// below is about the deadline and not about a step that never ran.
	if rt.hits == 0 {
		t.Fatal("no request was made, so this case measures an earlier wall")
	}
	if db.calls == 0 {
		t.Fatal("no database call was made, so this case measures an earlier wall")
	}

	// The fetch keeps the client's own measured bound and gets none of ours.
	if rt.left <= dbwait.RoundWait {
		t.Fatalf("the fetch was given %v, which is not the client's own measured %v: a "+
			"deadline of ours is over it, and thirty seconds would cut a fetch doing "+
			"nothing wrong at half the bound the client already holds",
			rt.left, fx.St.Client.Timeout)
	}
	// Every database call carries one, and it is RoundWait and not something
	// larger that happens to exist.
	if db.noneA != 0 {
		t.Fatalf("%d of %d database calls carry no deadline at all; without one a busy "+
			"database holds this round for the measured 80,6 s", db.noneA, db.calls)
	}
	if db.worst > dbwait.RoundWait {
		t.Fatalf("a database call had %v left, more than dbwait.RoundWait (%v)",
			db.worst, dbwait.RoundWait)
	}
}

// stuckForget blocks the LAST database call of Refresh, the one forget makes.
// Measured before the change that is the silent one: its error was dropped
// (`_ = st.forget(...)`) and Refresh answered nil, so the round that called it
// logged reason=none for a database nobody could reach.
type stuckForget struct {
	dbapi.Store
	calls int
}

func (s *stuckForget) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	s.calls++
	<-ctx.Done()
	return nil, context.Cause(ctx)
}

// TestRefreshHandsTheDatabaseFailureBack: a store that fails is still none of
// the round's business, but a database that does not answer is -- the round's
// log line is the whole of what it can say.
func TestRefreshHandsTheDatabaseFailureBack(t *testing.T) {
	fx, _ := newFixture(t)
	st := &stuckForget{Store: fx.S.DB}
	fx.S.DB = st

	start := time.Now()
	err := fx.St.Refresh(context.Background())
	took := time.Since(start)

	if st.calls == 0 {
		t.Fatal("forget made no read, so this case measures an earlier wall")
	}
	if err == nil {
		t.Fatal("Refresh answered nil for a database that never came free, so the round " +
			"that calls it logs reason=none")
	}
	if !dbwait.Busy(err) {
		t.Fatalf("Refresh handed back %v, which is not dbwait.ErrBusy, so no caller can "+
			"tell a busy database from one that is away", err)
	}
	if took > 2*dbwait.RoundWait {
		t.Fatalf("Refresh took %v against a deadline of %v", took, dbwait.RoundWait)
	}
}
