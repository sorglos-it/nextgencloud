package server

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/updates"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
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
