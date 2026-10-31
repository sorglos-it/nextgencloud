package coreapp

import (
	"encoding/json"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestARefusedChannelWritesItsReasonAndSaysItOnTheScreen is the finding of
// 19.-21.10.2026 measured from end to end, through the real socket, the real
// dispatcher and the real log: a refused channel used to leave a line with its
// code and nothing else, and the person read the one German sentence the shell
// says for that code.
//
// The action here is one the contract does not have, so core.Lookup refuses
// it. Two things are held of the same refusal:
//
//   - the log line carries the sentence (reason.ActionUnknown), and
//   - the CLOSE carries it too, because this one may go to an operator.
//
// The second is the way to the screen. The shell shows a message of the server
// where it differs from what it would say for the code itself (apps/web,
// net/channel.js, PR #82); E_NOTFOUND reads "Nicht gefunden." there, and this
// sentence is not that.
func TestARefusedChannelWritesItsReasonAndSaysItOnTheScreen(t *testing.T) {
	fx := newFixture(t)
	const unknown = "kein-vertrag" // a name wire.ValidName takes, so nothing is dropped
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: unknown,
		Window: wire.MinWindow})
	cl := c.closed(t)
	if cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
	if cl.Message != string(reason.ActionUnknown) {
		t.Fatalf("the screen reads %q, want %q", cl.Message, reason.ActionUnknown)
	}
	e := fx.waitEntry(t, core.AppDesktop, unknown)
	if e.Result != string(wire.CodeNotFound) {
		t.Fatalf("entry %+v", e)
	}
	if e.Detail != string(reason.ActionUnknown) {
		t.Fatalf("the log line reads %q, want %q", e.Detail, reason.ActionUnknown)
	}
}

// TestTheScreenIsNotToldWhetherAnAppIsInstalled is the boundary between the
// two readers, measured at the one place it matters. An app channel for an app
// that is not installed answers exactly like a missing right, on purpose
// (package rights, Check) -- and the sentence that would tell the two apart
// goes into the log and NOT into the CLOSE.
//
// What the CLOSE carries instead is the German text of the code, which is what
// it carried before there were reasons at all, so nothing a shell reads got
// worse by this.
func TestTheScreenIsNotToldWhetherAnAppIsInstalled(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: appX, Service: appSvc, Action: appAct,
		Window: wire.MinWindow, E2E: make([]byte, wire.HandshakeSize)})
	cl := c.closed(t)
	if cl.Code != wire.CodeForbidden {
		t.Fatalf("close %+v", cl)
	}
	if cl.Message != wire.CodeForbidden.Message() {
		t.Fatalf("the screen reads %q, want the text of the code %q",
			cl.Message, wire.CodeForbidden.Message())
	}
	if cl.Message == string(reason.AppUnknown) {
		t.Fatal("the screen was told whether the app is installed")
	}
	e := fx.waitEntry(t, appX, appAct)
	if e.Detail != string(reason.AppUnknown) {
		t.Fatalf("the log line reads %q, want %q", e.Detail, reason.AppUnknown)
	}
}

// TestAHandlerRefusalCarriesItsReasonTooHolds the other way a channel is
// refused: not by a check before the handler, but by the handler itself. The
// action here needs a parameter, the OPEN brings none that fits, and
// core.ParseParams says no -- a refusal that used to reach the log as
// E_PROTOCOL alone.
func TestAHandlerRefusalCarriesItsReasonToo(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
		Action: core.ActTOTPConfirm, Window: wire.MinWindow})
	cl := c.closed(t)
	if cl.Code != wire.CodeProtocol {
		t.Fatalf("close %+v", cl)
	}
	if cl.Message != string(reason.Params) {
		t.Fatalf("the screen reads %q, want %q", cl.Message, reason.Params)
	}
	e := fx.waitEntry(t, core.AppDesktop, core.ActTOTPConfirm)
	if e.Detail != string(reason.Params) {
		t.Fatalf("the log line reads %q, want %q", e.Detail, reason.Params)
	}
}

// TestAHandlerThatRefusesNamesItsCheck walks the OTHER way a refusal is made:
// by a handler, through failWhy, behind the checks of the dispatcher. Both
// rows end in the same code on purpose -- appOf answers E_NOTFOUND whether the
// app is not installed or the user may not use it, which is what tells a user
// without the right nothing about the installation -- and the LOG tells the
// two apart.
//
// The two readers are measured in one case, because that is where they differ:
// the sentence about the right goes onto the screen, the sentence about the
// installation does not.
func TestAHandlerThatRefusesNamesItsCheck(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	bob := nonAdminSession(t, fx)
	// An installed app the user may not use: the handler got as far as the
	// right, which is what the row is about.
	c := bob.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActUI,
		Window: wire.MinWindow, Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	cl := c.closed(t)
	if cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
	if cl.Message != string(reason.NoRight) {
		t.Fatalf("the screen reads %q, want %q", cl.Message, reason.NoRight)
	}
	e := fx.waitEntry(t, core.AppDesktop, core.ActUI)
	if e.Detail != string(reason.NoRight) {
		t.Fatalf("the log line reads %q, want %q", e.Detail, reason.NoRight)
	}

	// And an app that is not in the database at all: same code, other
	// sentence, and this one stays off the screen.
	gone := newFixture(t)
	c2 := gone.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActUI,
		Window: wire.MinWindow, Params: json.RawMessage(`{"app":"` + appX + `"}`)})
	cl2 := c2.closed(t)
	if cl2.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl2)
	}
	if cl2.Message != wire.CodeNotFound.Message() {
		t.Fatalf("the screen reads %q, want the text of the code %q",
			cl2.Message, wire.CodeNotFound.Message())
	}
	e2 := gone.waitEntry(t, core.AppDesktop, core.ActUI)
	if e2.Detail != string(reason.AppUnknown) {
		t.Fatalf("the log line reads %q, want %q", e2.Detail, reason.AppUnknown)
	}
}

// TestAChannelThatRanLeavesNoReason is the control of the three cases above:
// the same path, the same log, and a channel that opened carries no sentence
// at all. Without it, a mutant that wrote one reason into every line would
// pass all three.
func TestAChannelThatRanLeavesNoReason(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActApps,
		Window: wire.MinWindow})
	if len(c.result(t)) == 0 {
		t.Fatal("no result")
	}
	e := fx.waitEntry(t, core.AppDesktop, core.ActApps)
	if e.Result != "ok" || e.Detail != "" {
		t.Fatalf("a channel that ran carries %+v", e)
	}
}

// TestEveryRefusalOfTheseHandlersNamesACheck walks the refusals a case can
// reach without a store seam and holds that none of them is a code alone. It
// is the sentence of this branch as a list: "a code alone is no reason".
func TestEveryRefusalOfTheseHandlersNamesACheck(t *testing.T) {
	for _, c := range []struct {
		name string
		open wire.Open
		code wire.Code
		want reason.Reason
	}{
		{"an action the contract does not have",
			wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: "kein-vertrag"},
			wire.CodeNotFound, reason.ActionUnknown},
		{"an action of ngc.store with no seam wired",
			wire.Open{App: core.AppStore, Service: core.ServiceAPI, Action: core.ActInstalled},
			wire.CodeNotFound, reason.ActionUnserved},
		{"a call whose parameters do not fit",
			wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActTOTPOff},
			wire.CodeProtocol, reason.Params},
		{"an app the installation does not have",
			wire.Open{App: appX, Service: appSvc, Action: appAct, E2E: make([]byte, wire.HandshakeSize)},
			wire.CodeForbidden, reason.AppUnknown},
	} {
		fx := newFixture(t)
		o := c.open
		o.Window = wire.MinWindow
		ch := fx.Open(t, o)
		if cl := ch.closed(t); cl.Code != c.code {
			t.Errorf("%s: close %+v, want %s", c.name, cl, c.code)
			continue
		}
		e := fx.waitEntry(t, o.App, o.Action)
		if e.Detail == "" {
			t.Errorf("%s: %s and no reason in the log", c.name, c.code)
			continue
		}
		if !reason.Known(reason.Reason(e.Detail)) {
			t.Errorf("%s: %q is no sentence of the list", c.name, e.Detail)
		}
		if e.Detail != string(c.want) {
			t.Errorf("%s: the log says %q, want %q", c.name, e.Detail, c.want)
		}
	}
}
