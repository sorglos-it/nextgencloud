package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/updates"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// notServedHere names the actions of ngc.store/controller this API has no
// handler for, and they are two different cases on purpose:
//
//   - delete-data and install are root tasks a shell may open and the
//     controller carries out alone, so the relay takes them (coreapp.open);
//   - stage and license the API alone may open, so a shell's OPEN for them is
//     E_FORBIDDEN before any handler is looked for (core.ShellOpens).
//
// Everything else of ngc.store has to have a handler after Build, or it is
// unreachable from a shell. The list is written out rather than derived, so
// that an action added to the contract cannot quietly join it:
// TestEveryStoreActionIsServedAfterBuild holds every name in here against
// core.All().
var notServedHere = []string{core.ActDeleteData, core.ActInstall, core.ActStage, core.ActLicense}

// TestEveryStoreActionIsServedAfterBuild measures the wiring and not a
// comment: Build has to hand the dispatcher both seams, so that every action
// of ngc.store the API serves itself really has a handler. Before this task
// Deps had no field for either and the answer was E_NOTFOUND for all of them.
//
// Both directions are held. An action that should have a handler and has none
// fails, and an action of notServedHere that HAS one fails too -- a handler
// for ngc.store/controller/install would take a root task away from the
// controller that only it can carry out.
func TestEveryStoreActionIsServedAfterBuild(t *testing.T) {
	srv, _, _ := newServer(t)
	for _, a := range core.All() {
		if a.App != core.AppStore {
			continue
		}
		// Only an action of service controller may be in the list: a
		// controller cannot carry out an action of service api.
		want := !(a.Service == core.ServiceController && slices.Contains(notServedHere, a.Name))
		if got := srv.core.Serves(a.App, a.Service, a.Name); got != want {
			t.Fatalf("ngc.store/%s/%s: served=%v, wanted %v", a.Service, a.Name, got, want)
		}
	}
	// And every name of the list is an action of ngc.store/controller, so a
	// typo in it cannot make the loop above pass for nothing.
	for _, name := range notServedHere {
		if _, ok := core.Lookup(core.AppStore, core.ServiceController, name); !ok {
			t.Fatalf("%q is in the list and is no action of ngc.store/controller", name)
		}
	}
}

// TestTheUpdatesAreWired is four of the six seams task 23 left for this one --
// 1, 2, 3 and 5 -- measured on the server Build really makes. Seam 4 is the
// beat (TestTheBeatOfTheRolloutTurns) and seam 6 the two handlers
// (TestEveryStoreActionIsServedAfterBuild).
//
// The adapter is held by IDENTITY and not by "both are not nil": task 23 asked
// for ONE adapter for appstore.Stores.Send and Updates.Send, and two separate
// ones that both worked would pass any other check.
func TestTheUpdatesAreWired(t *testing.T) {
	srv, _, spool := newServer(t)
	if srv.up == nil {
		t.Fatal("Build made no updates")
	}
	if srv.up.Tunnels == nil {
		t.Fatal("seam 1: Updates.Tunnels is nil, so no node is ever online")
	}
	if srv.up.Send == nil || srv.up.Send != srv.st.Send {
		t.Fatalf("seam 2: the updates have %v and the app store %v -- it has to be ONE adapter",
			srv.up.Send, srv.st.Send)
	}
	if srv.ctl == nil || srv.ctl.OnReport == nil {
		t.Fatal("seam 3: nothing takes the update reports of a controller")
	}
	if srv.up.Spool != spool {
		t.Fatalf("seam 5: the updates spool to %q, not to the data directory %q", srv.up.Spool, spool)
	}
	if srv.up.Stores != srv.st || srv.up.S != srv.State || srv.up.Log == nil {
		t.Fatal("the updates are missing the app store, the state or the log")
	}
	// Seam 4 is the beat, and TestAnUpdateRoundRuns measures it; seam 6 is the
	// two handlers, and TestEveryStoreActionIsServedAfterBuild measures those.
}

// TestAReportOfAControllerReachesTheRows is seam 3 all the way through, and
// not at its two ends: a report comes in the way a controller sends one --
// through the hook Build set -- and comes out in the rows ngc.store/updates
// answers. The hook takes a tunnel and Report takes a node, so this is also
// what holds that the wrapper passes the tunnel's node and not something else.
func TestAReportOfAControllerReachesTheRows(t *testing.T) {
	srv, db, _ := newServer(t)
	signedIn(t, db)
	if _, err := srv.State.Refresh(ctx); err != nil {
		t.Fatalf("trust: %v", err)
	}
	// The controller of trusttest.Genesis. A row for trust.AutoSystem exists
	// for every controller of the trust list, with or without a tunnel, which
	// is why this needs no tunnel at all.
	const node = "ctl-1"
	rows, err := srv.up.Rows(ctx)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	// The control first: without a report the row carries no state, so a green
	// case cannot mean "the state was always there".
	if r, ok := rowOf(rows, trust.AutoSystem, node); !ok {
		t.Fatalf("no row for %s on %s: %+v", trust.AutoSystem, node, rows)
	} else if r.State != "" {
		t.Fatalf("the row already says %q before any report", r.State)
	}
	srv.ctl.OnReport(ctx, &controllers.Tunnel{Node: node},
		store.UpdateReport{Target: trust.AutoSystem, Version: "1.2.3", State: store.UpdateInstalled})
	rows, err = srv.up.Rows(ctx)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	r, ok := rowOf(rows, trust.AutoSystem, node)
	if !ok || r.State != store.UpdateInstalled {
		t.Fatalf("the report did not reach the rows: %+v", r)
	}
	// And the other direction: a report of ANOTHER node must not colour this
	// row, which is what a wrapper passing a fixed node would do.
	srv.ctl.OnReport(ctx, &controllers.Tunnel{Node: "ctl-9"},
		store.UpdateReport{Target: trust.AutoSystem, Version: "9.9.9", State: store.UpdateFailed})
	rows, err = srv.up.Rows(ctx)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if r, _ := rowOf(rows, trust.AutoSystem, node); r.State != store.UpdateInstalled {
		t.Fatalf("a report of another node changed this row to %q", r.State)
	}
}

// rowOf finds the row of one target on one node.
func rowOf(rows []core.UpdateInfo, target, node string) (core.UpdateInfo, bool) {
	for _, r := range rows {
		if r.Target == target && r.Node == node {
			return r, true
		}
	}
	return core.UpdateInfo{}, false
}

// TestTheBeatOfTheRolloutTurns is seam 4: who calls Rollout, how often, and
// what happens to a tick that finds a round still running. The beat is set to
// milliseconds, as the two housekeeping rounds already are, and the round
// itself is a probe: with a release, a store and a controller behind it the
// case would measure those and not the beat.
//
// Three things at once, and each of them is the other's control:
//
//   - the beat turns at all (more than one round ran);
//   - a tick that arrives during a round is DROPPED and not queued, and no
//     round ever runs beside another: the probe holds the first round open for
//     about twenty-five tick lengths, and exactly one round is inside at any
//     time. How often the ticker really fired in that span is not measured
//     here -- what is measured is that at most one round followed the span. This is what a lock used to claim to hold and could
//     not (the loop is the only caller and calls synchronously); the mutant it
//     really stands against is one goroutine per tick;
//   - updates.ErrNotDriving is no failure: the probe answers it and the rounds
//     go on, which is the normal case of two API servers.
func TestTheBeatOfTheRolloutTurns(t *testing.T) {
	srv, _, _ := newServer(t)
	oldHour := Hour
	Hour = 2 * time.Millisecond
	t.Cleanup(func() { Hour = oldHour })

	var mu sync.Mutex
	inside, maxInside, rounds := 0, 0, 0
	release := make(chan struct{})
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer close(done)
		srv.rollouts(ctx, func(context.Context) error {
			mu.Lock()
			inside++
			rounds++
			n, r := inside, rounds
			maxInside = max(maxInside, n)
			mu.Unlock()
			if r == 1 {
				// The first round stays in while the ticker fires again and
				// again: without that no tick would ever arrive during a
				// round, and the property would be measured by nothing.
				<-release
			}
			mu.Lock()
			inside--
			mu.Unlock()
			return updates.ErrNotDriving
		})
	}()
	// Give the ticker time to fire several times into the held round.
	time.Sleep(50 * time.Millisecond)
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		r := rounds
		mu.Unlock()
		if r >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if rounds < 3 {
		t.Fatalf("%d rounds ran -- the beat does not turn, or ErrNotDriving stopped it", rounds)
	}
	if maxInside != 1 {
		t.Fatalf("%d rounds were inside at once; a tick must be dropped, not queued", maxInside)
	}
	// And the ticks that arrived during the held round were DROPPED: an
	// upper bound, which is what this check is. It does NOT say the ticker
	// fired while the round was held -- a beat nothing ever interrupted would
	// pass it too. That part rests on 50 ms against a tick of 2 ms and is
	// arithmetic, not a measurement; the comment above used to claim it as
	// one.
	if rounds > 10 {
		t.Fatalf("%d rounds ran -- about 25 ticks were missed and at most one "+
			"catch-up round may follow them", rounds)
	}
}

// TestARoundThatIsNotDrivingIsNoFailure holds the sentence of
// updates.ErrNotDriving on the real updates: another API holds
// dbapi.LockUpdate, so a round of ours does nothing at all and says which of
// the two it is. Without it a caller could treat the normal case of two API
// servers as a fault.
func TestARoundThatIsNotDrivingIsNoFailure(t *testing.T) {
	srv, db, _ := newServer(t)
	signedIn(t, db)
	if _, err := srv.State.Refresh(ctx); err != nil {
		t.Fatalf("trust: %v", err)
	}
	now := srv.State.Now()
	ok, err := db.LockTake(ctx, dbapi.LockUpdate, "api-9", now, now+600)
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	if err := srv.up.Rollout(ctx); !errors.Is(err, updates.ErrNotDriving) {
		t.Fatalf("a round that is not driving answered %v", err)
	}
	// The control: with the lock free the same round gets further -- it fails
	// on the release this installation has not got, which is a different
	// error. Without this half the case would pass for a Rollout that always
	// answered ErrNotDriving.
	if err := db.LockFree(ctx, dbapi.LockUpdate, "api-9"); err != nil {
		t.Fatalf("free: %v", err)
	}
	if err := srv.up.Rollout(ctx); err == nil || errors.Is(err, updates.ErrNotDriving) {
		t.Fatalf("with the lock free the round answered %v", err)
	}
}

// TestABrokenRoundDoesNotStopTheBeat is the decision of the per-round catch in
// Server.once, and it is the one place the two kinds of work part ways. A
// channel that panics ENDS -- there is a browser waiting, and it gets
// E_INTERNAL. A background round has nobody waiting, so ending it would be the
// silent failure of V1.2 point 50: housekeeping that stopped keeps expired
// sessions logged in and leaves the trust list as it was, while the API goes on
// answering as though both had been done. So the catch sits around ONE round
// and the NEXT tick runs the round again.
//
// Measured directly on once, because that is the seam the two housekeeping
// rounds share: minute and hour both go through it.
func TestABrokenRoundDoesNotStopTheBeat(t *testing.T) {
	srv, _, _ := newServer(t)
	before := guard.Panics()
	srv.once(func() { panic("a programming error in a background round") })
	if got := guard.Panics(); got != before+1 {
		t.Fatalf("the counter went %d -> %d; the round's panic must count once", before, got)
	}
	// The lock is free again and the next round runs. Both halves at once: a
	// catch that kept the lock would hang here just as a missing catch would
	// have killed the binary above.
	ran := false
	srv.once(func() { ran = true })
	if !ran {
		t.Fatal("the round after the broken one never ran -- the beat stopped")
	}
}

// TestABrokenRolloutDoesNotStopTheBeat is the same decision for the update
// round, which has a goroutine and a beat of its own (Server.rollouts): a
// rollout that broke off may not take the hour with it, or an installation
// would quietly stop updating itself. The next hour asks the same nodes again,
// which is what a rollout is built to survive.
func TestABrokenRolloutDoesNotStopTheBeat(t *testing.T) {
	srv, _, _ := newServer(t)
	before := guard.Panics()
	oldHour := Hour
	Hour = 2 * time.Millisecond
	t.Cleanup(func() { Hour = oldHour })

	var mu sync.Mutex
	rounds := 0
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.rollouts(ctx, func(context.Context) error {
			mu.Lock()
			rounds++
			r := rounds
			mu.Unlock()
			if r == 1 {
				panic("a programming error in the update round")
			}
			return updates.ErrNotDriving
		})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		r := rounds
		mu.Unlock()
		if r >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if rounds < 3 {
		t.Fatalf("%d rounds ran after the broken one -- the beat stopped with it", rounds)
	}
	if got := guard.Panics(); got != before+1 {
		t.Fatalf("the counter went %d -> %d; the broken round must count once", before, got)
	}
}

// TestTheHousekeepingRoundSaysTheCountOfCaughtErrors holds the WIRING, which is
// the half a case in package guard cannot reach: guard.Report writes the line,
// but a Report nobody calls is the number nobody reads -- which is what
// Panics() was until this round called it.
//
// The round is run by hand here. The beat around it is held by
// TestTheBeatOfTheRolloutTurns and TestABrokenRoundDoesNotStopTheBeat; what is
// measured here is that the minute round reaches guard at all.
func TestTheHousekeepingRoundSaysTheCountOfCaughtErrors(t *testing.T) {
	srv, _, _ := newServer(t)
	var said bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&said, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	// A round of its own first, so whatever the cases before this one counted
	// is already said and this case measures only its own panic.
	srv.minute(ctx)
	said.Reset()

	srv.once(func() { panic("a programming error in a background round") })
	total := guard.Panics()
	said.Reset() // the ERROR line of the catch is not what is measured here

	srv.minute(ctx)
	line := said.String()
	if !strings.Contains(line, "level=WARN") || !strings.Contains(line, "panics=") {
		t.Fatalf("the housekeeping round said nothing about the caught error: %q", line)
	}
	if !strings.Contains(line, "new=1") {
		t.Fatalf("the round did not say how many are new: %q", line)
	}
	// The number is the one guard holds, not one this round counted again.
	if !strings.Contains(line, "panics="+strconv.FormatUint(total, 10)) {
		t.Fatalf("the round says another total than guard.Panics() = %d: %q", total, line)
	}
	// And the round that follows is silent, so a defect does not turn into a
	// line a minute for the life of the process.
	said.Reset()
	srv.minute(ctx)
	if strings.Contains(said.String(), "panics=") {
		t.Fatalf("the same caught error was said twice: %q", said.String())
	}
}

// trustFlaky is a store whose trust rows can be made unreachable, so a case
// can measure the minute round against a fault that comes, goes and comes
// back with the SAME words. Only BlobGet of the trust rows is touched: that is
// the one call State.Refresh makes to read a version.
type trustFlaky struct {
	dbapi.Store
	broken atomic.Bool
}

// errTrustAway is the one fault trustFlaky makes, by value, so a case can tell
// the second coming of the same fault from a different one.
var errTrustAway = errors.New("the trust rows could not be reached")

func (f *trustFlaky) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if f.broken.Load() && kind == dbapi.BlobTrust {
		return dbapi.Blob{}, errTrustAway
	}
	return f.Store.BlobGet(ctx, kind, id)
}

// TestTheTrustLineIsSaidOnChangeAndNotEveryMinute nails the line down that was
// measured on the small plant on 18.10.2026: ngc-api had been up 54 minutes
// and had written "trust list could not be read" 54 times, all with the same
// cause, "state: no trust list yet" -- the normal state of an installation
// that nobody has paired yet.
//
// Two halves, and the second is the one that keeps the quiet honest:
//
//   - the same fault, round after round, is said ONCE. Otherwise the round
//     makes 1440 lines a day about a state nobody can act on, and the lines
//     that do mean something are in between them.
//   - a fault that CHANGED, and the same fault after a round that worked, are
//     said again. A database that locks, frees and locks again is two events,
//     and a memory that only ever said "already told you" would hide the
//     second one for the life of the process.
func TestTheTrustLineIsSaidOnChangeAndNotEveryMinute(t *testing.T) {
	db := &trustFlaky{Store: memstore.New()}
	withStore(t, db)
	withDirs(t)
	srv, err := Build(ctx, newConfig(t), "9.9.9")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var said bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&said, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	// The fixture has to be the lie-free one: without a trust list the round
	// must really fail here, or everything below measures a round that had
	// nothing to say anyway.
	if _, err := srv.State.Refresh(ctx); err == nil {
		t.Fatal("the fixture already has a trust list -- this case would measure a silence it did not cause")
	}

	round := func() string {
		said.Reset()
		srv.minute(ctx)
		return said.String()
	}

	first := round()
	if !strings.Contains(first, "trust list could not be read") {
		t.Fatalf("the first round said nothing about the trust list: %q", first)
	}
	if !strings.Contains(first, "no trust list yet") {
		t.Fatalf("the first round did not name the cause: %q", first)
	}
	if line := round(); strings.Contains(line, "trust list could not be read") {
		t.Fatalf("the same cause was said a second time -- a line a minute for a state that does not change: %q", line)
	}

	// A DIFFERENT cause is a new thing to say.
	db.broken.Store(true)
	changed := round()
	if !strings.Contains(changed, errTrustAway.Error()) {
		t.Fatalf("a changed cause was swallowed: %q", changed)
	}
	if line := round(); strings.Contains(line, "trust list could not be read") {
		t.Fatalf("the changed cause was said twice as well: %q", line)
	}

	// It recovers: a round that reads the list says nothing, and it forgets.
	signedIn(t, db.Store)
	db.broken.Store(false)
	if line := round(); strings.Contains(line, "trust list could not be read") {
		t.Fatalf("the round that READ the list still complained: %q", line)
	}
	if _, err := srv.State.Refresh(ctx); err != nil {
		t.Fatalf("the recovery did not take: %v", err)
	}

	// And the same cause, after that good round, is said AGAIN. This is the
	// half a plain "said it once" flag cannot do.
	db.broken.Store(true)
	again := round()
	if !strings.Contains(again, errTrustAway.Error()) {
		t.Fatalf("the fault came back after a good round and was not said again: %q", again)
	}
}
