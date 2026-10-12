package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestCallAnswersAndCloses(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe,
		Window: wire.MinWindow})
	// A call-style channel: first a GRANT, then the result as DATA, then EOF
	// and CLOSE ok.
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("first frame %+v", f)
	}
	body := c.collect(t)
	if len(body) == 0 {
		t.Fatal("no result")
	}
	if cl := c.closed(t); cl.Status != wire.StatusOK {
		t.Fatalf("close %+v", cl)
	}
}

func TestUnknownActionIsNotFound(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: "nothing",
		Window: wire.MinWindow})
	if cl := c.closed(t); cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
}

func TestResultBeyondTheLimitCloses(t *testing.T) {
	fx := newFixture(t)
	fx.D.Register(core.AppDesktop, core.ServiceAPI, core.ActMe,
		func(_ context.Context, _ *Call) ([]byte, error) {
			return make([]byte, wire.MaxCallResult+1), nil
		})
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe,
		Window: wire.MinWindow})
	if cl := c.closed(t); cl.Code != wire.CodeTooLarge {
		t.Fatalf("close %+v", cl)
	}
}

// TestInputOfTheLimitIsRead is the open-way check of the case below: at
// exactly MaxIn the channel is served, so an E_TOOLARGE one byte further is
// the bound and not a wall in front of it (docs/arbeitsweise.md, trap one).
func TestInputOfTheLimitIsRead(t *testing.T) {
	fx := newFixture(t)
	action, ok := core.Lookup(core.AppUsers, core.ServiceAPI, core.ActTrust)
	if !ok {
		t.Fatal("no action ngc.users/api/trust")
	}
	var got int
	fx.D.Register(core.AppUsers, core.ServiceAPI, core.ActTrust,
		func(_ context.Context, rq *Call) ([]byte, error) {
			b := make([]byte, action.MaxIn+1)
			n, _ := rq.In.Read(b)
			got = n
			return []byte(`{}`), nil
		})
	c := fx.OpenAdmin(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI, Action: core.ActTrust,
		Window: wire.MinWindow})
	c.sendBytes(t, int(action.MaxIn))
	if cl := c.closed(t); cl.Status != wire.StatusOK {
		t.Fatalf("close %+v", cl)
	}
	if int64(got) != action.MaxIn {
		t.Fatalf("the handler read %d of %d bytes", got, action.MaxIn)
	}
}

func TestInputBeyondMaxInCloses(t *testing.T) {
	fx := newFixture(t)
	action, ok := core.Lookup(core.AppUsers, core.ServiceAPI, core.ActTrust)
	if !ok {
		t.Fatal("no action ngc.users/api/trust")
	}
	fx.D.Register(core.AppUsers, core.ServiceAPI, core.ActTrust,
		func(_ context.Context, _ *Call) ([]byte, error) { return []byte(`{}`), nil })
	c := fx.OpenAdmin(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI, Action: core.ActTrust,
		Window: wire.MinWindow})
	c.sendBytes(t, int(action.MaxIn)+1)
	if cl := c.closed(t); cl.Code != wire.CodeTooLarge {
		t.Fatalf("close %+v", cl)
	}
}

// TestAnActionWithoutAHandlerIsNotFound holds the second E_NOTFOUND of open:
// the action is in the contract, but this dispatcher serves no handler for it.
// Without this case a dispatcher that answered E_INTERNAL there would pass.
//
// The action it names has to be one THIS dispatcher registers nothing for, and
// that is where it had to move. It was ngc.users/list until task 16 gave that
// one a handler, then ngc.store/sources, which stopped meaning anything the
// moment the app store's handlers were registered -- every action of the
// contract has a handler or a relay now. So what it points at is
// ngc.store/sources on a dispatcher built WITHOUT the app store
// (Deps.Store nil), which is a state of its own and not a hole: a seam nobody
// wired says E_NOTFOUND, not E_INTERNAL, and nothing of it blames the
// installation.
//
// The control is the point of the second half: the SAME action on a dispatcher
// with the seam set answers a result. Without it this case would stay green
// for a dispatcher that answered E_NOTFOUND to everything.
func TestAnActionWithoutAHandlerIsNotFound(t *testing.T) {
	fx := newFixture(t)
	c := fx.OpenAdmin(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI, Action: core.ActSources,
		Window: wire.MinWindow})
	if cl := c.closed(t); cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
	// The same for the other seam, because the two are registered apart: a
	// dispatcher with the app store and without the updates serves sources and
	// not ngc.store/updates.
	c2 := fx.OpenAdmin(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActUpdates, Window: wire.MinWindow})
	if cl := c2.closed(t); cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
	with := newStoreFixture(t)
	c3 := with.OpenAdmin(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActSources, Window: wire.MinWindow})
	c3.result(t)
	if !with.Store.saw(core.ActSources) {
		t.Fatal("the seam was wired and the app store was not asked")
	}
	c4 := with.OpenAdmin(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActUpdates, Window: wire.MinWindow})
	c4.result(t)
	if !with.Up.saw(core.ActUpdates) {
		t.Fatal("the seam was wired and the updates were not asked")
	}
}

// --- The first caller of rights.Check -------------------------------------
//
// rights.Check promises that no unsigned part of the OPEN moves the decision.
// That promise rests on this package resolving the fields of rights.Request,
// and the three cases below are what measures it.

// TestTheResolvedAppDecidesAndNotTheOpen is the promise itself: the OPEN names
// appX, the database answers the row of appY for it, and the rights of appY
// are what open the channel. A dispatcher that passed the OPEN's app ID into
// rights.Request would refuse here.
func TestTheResolvedAppDecidesAndNotTheOpen(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Lying.swapFrom, fx.Lying.swapTo = appX, appY
	// Only the resolved app is granted; nothing of appX is.
	fx.Grant(t, appY+"/"+core.RightUse, appY+"/"+appAct)
	c := fx.Open(t, wire.Open{App: appX, Service: appSvc, Action: appAct,
		Window: wire.MinWindow, E2E: make([]byte, wire.HandshakeSize)})
	r := fx.Relay.forwarded(t)
	if r.Open.App != appX {
		t.Fatalf("the relay was handed %q, not the OPEN", r.Open.App)
	}
	if r.Node != ctlNode {
		t.Fatalf("node %q", r.Node)
	}
	if cl := c.closed(t); cl.Code != wire.CodeUnreachable {
		t.Fatalf("close %+v", cl)
	}
}

// TestTheRightsOfTheClaimedAppDoNotOpenIt is the other control of the same
// pair: the same lying OPEN, but now only appX is granted. The decision
// follows the resolved row, so the channel is refused although the OPEN's own
// app carries every right. One of the two must die, one must live.
func TestTheRightsOfTheClaimedAppDoNotOpenIt(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Lying.swapFrom, fx.Lying.swapTo = appX, appY
	fx.Grant(t, appX+"/"+core.RightUse, appX+"/"+appAct)
	c := fx.Open(t, wire.Open{App: appX, Service: appSvc, Action: appAct,
		Window: wire.MinWindow, E2E: make([]byte, wire.HandshakeSize)})
	if cl := c.closed(t); cl.Code != wire.CodeForbidden {
		t.Fatalf("close %+v", cl)
	}
	select {
	case r := <-fx.Relay.ch:
		t.Fatalf("the channel was forwarded anyway: %+v", r)
	default:
	}
}

// TestTheNodeOfTheTaskIsTheNodeThatIsChecked is the case the plan's
// nodeOfTask asks for: the node of a root task comes out of the task in the
// OPEN, which is signed, and trust.MayRoot is checked against exactly that
// node. Here the task names the controller the admin may run root tasks on,
// so the channel opens and the relay is handed that node.
func TestTheNodeOfTheTaskIsTheNodeThatIsChecked(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	tk := fx.Task(t, task.Task{ID: taskID(0x61), Node: ctlNode, App: core.AppStore,
		Service: core.ServiceController, Action: core.ActDeactivate,
		Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Window: wire.MinWindow, Task: tk})
	r := fx.Relay.forwarded(t)
	if r.Node != ctlNode {
		t.Fatalf("node %q, not the one the task names", r.Node)
	}
	if r.Task == nil || r.Task.Node != ctlNode {
		t.Fatalf("task %+v", r.Task)
	}
	if cl := c.closed(t); cl.Code != wire.CodeUnreachable {
		t.Fatalf("close %+v", cl)
	}
}

// TestATaskOnAnotherNodeIsRefused is the control of the case above: the same
// signed task on a node the trust list does not know. task.Check compares the
// node the API resolved against the signed one, so a nodeOfTask that answered
// "" or a fixed node would be seen here.
func TestATaskOnAnotherNodeIsRefused(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	tk := fx.Task(t, task.Task{ID: taskID(0x62), Node: "ctl-9", App: core.AppStore,
		Service: core.ServiceController, Action: core.ActDeactivate,
		Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Window: wire.MinWindow, Task: tk})
	if cl := c.closed(t); cl.Code != wire.CodeForbidden {
		t.Fatalf("close %+v", cl)
	}
}

// TestARootTaskWithoutATaskAsksForConfirmation is the open-way check in front
// of the two node cases: without a task the same action answers E_CONFIRM, so
// their answers are about the node and not about a wall before it.
func TestARootTaskWithoutATaskAsksForConfirmation(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Window: wire.MinWindow,
		Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	if cl := c.closed(t); cl.Code != wire.CodeConfirm {
		t.Fatalf("close %+v", cl)
	}
}

// --- The log ---------------------------------------------------------------

// TestEveryChannelIsLogged holds the sentence of the design (line 630 to 632):
// every channel that opens is written down, with the code it closed with.
func TestEveryChannelIsLogged(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe,
		Window: wire.MinWindow})
	c.result(t)
	entry := fx.waitEntry(t, core.AppDesktop, core.ActMe)
	if entry.User != fx.AdminID || entry.Result != "ok" {
		t.Fatalf("entry %+v", entry)
	}
}

func TestARefusedChannelIsLoggedWithItsCode(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: "nothing",
		Window: wire.MinWindow})
	if cl := c.closed(t); cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
	entry := fx.waitEntry(t, core.AppDesktop, "nothing")
	if entry.Result != string(wire.CodeNotFound) {
		t.Fatalf("entry %+v", entry)
	}
}

// TestADroppedFieldDoesNotStopTheChannel is the other half of ErrDropped: it
// is a report, not a refusal. The channel of the case above closed with
// E_NOTFOUND because the action is unknown -- not with the code of a log that
// complained. Here the action is unknown AND the handler would have answered,
// and what is held is that the dispatcher does not turn ErrDropped into a code
// of its own.
func TestADroppedFieldDoesNotStopTheChannel(t *testing.T) {
	fx := newFixture(t)
	// An action name of the contract whose entry the log keeps whole, so the
	// comparison is against a channel that ran.
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActApps,
		Window: wire.MinWindow})
	body := c.result(t)
	if len(body) == 0 {
		t.Fatal("no result")
	}
	// The entry of that channel is written after its CLOSE, so it is waited
	// for here: counting before and after without it would count a race.
	fx.waitEntry(t, core.AppDesktop, core.ActApps)
	// And the report itself: the log answers ErrDropped for an action outside
	// the grammar, and still writes.
	before := len(fx.logEntries(t))
	err := fx.Log.Channel(ctx, fx.AdminID, core.AppDesktop, "not a name", "", "")
	if !errors.Is(err, audit.ErrDropped) {
		t.Fatalf("the log did not report a dropped field: %v", err)
	}
	if after := len(fx.logEntries(t)); after != before+1 {
		t.Fatalf("%d entries before, %d after", before, after)
	}
}

// TestTheKindOfAChannelEntryIsNeverInvented holds the one exception of package
// audit: a kind outside core.Log* is refused and writes nothing. The
// dispatcher must therefore use the entry points that are there, and this case
// says what it would cost not to.
func TestTheKindOfAChannelEntryIsNeverInvented(t *testing.T) {
	fx := newFixture(t)
	before := len(fx.logEntries(t))
	err := fx.Log.Note(ctx, "channel-open", fx.AdminID, core.AppDesktop, "", "")
	if !errors.Is(err, audit.ErrEntry) {
		t.Fatalf("an invented kind was taken: %v", err)
	}
	if after := len(fx.logEntries(t)); after != before {
		t.Fatalf("%d entries before, %d after", before, after)
	}
}

// TestASignedTaskIsLoggedWithItsSignature holds that the signature reaches the
// log as base64url: anything else is dropped by audit.Task, and the task would
// stand in the log without the proof of who ordered it.
func TestASignedTaskIsLoggedWithItsSignature(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	tk := fx.Task(t, task.Task{ID: taskID(0x63), Node: ctlNode, App: core.AppStore,
		Service: core.ServiceController, Action: core.ActDeactivate,
		Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Window: wire.MinWindow, Task: tk})
	fx.Relay.forwarded(t)
	c.closed(t)
	entries := fx.logEntries(t)
	var detail string
	for _, e := range entries {
		if e.Kind == core.LogTask {
			detail = e.Detail
		}
	}
	if detail == "" {
		t.Fatalf("no task entry: %+v", entries)
	}
	if detail == audit.Dropped {
		t.Fatal("the signature was dropped; it did not reach the log as base64url")
	}
}

// taskID is the fixed ID of a task of a case.
func taskID(fill byte) []byte {
	b := make([]byte, task.IDSize)
	for i := range b {
		b[i] = fill
	}
	return b
}

// channelEntry finds the channel entry of an app and action.
func channelEntry(entries []core.LogEntry, app, action string) (core.LogEntry, bool) {
	for _, e := range entries {
		if e.Kind == core.LogChannel && e.App == app && e.Action == action {
			return e, true
		}
	}
	return core.LogEntry{}, false
}
