package coreapp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// panicSecret is what a panic value carries by accident. Every case below
// looks for this string in the log, so a line that started printing the value
// shows up as a failure.
const panicSecret = "NGC-TEST-SECRET-0123456789"

// catchLog puts a log of its own in place of the default one for one case.
func catchLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// TestAPanickingHandlerEndsOnlyItsOwnChannel is the case of V1.2 point 38.
//
// What it measures is not that a panic is caught -- that is the easy half --
// but that the NEIGHBOURS live: a second channel on the SAME link, opened
// after the crash, is served and answers. Before the catch in
// (*Dispatcher).serve this case did not fail, it ended the test BINARY: the
// handler runs in a goroutine of browser.(*Server).channel, and a panic in a
// goroutine is final. That is the whole point of the contract point -- one
// channel of one user took every session of every user with it.
//
// It also holds the other half, the one point 50 is about: the channel does
// not vanish quietly. It ends with a code, so the browser is told, and the
// audit entry of the channel carries that code, so the defect is in the
// database and not only in a log file.
func TestAPanickingHandlerEndsOnlyItsOwnChannel(t *testing.T) {
	buf := catchLog(t)
	fx := newFixture(t)
	before := guard.Panics()
	fx.D.Register(core.AppDesktop, core.ServiceAPI, core.ActMe,
		func(_ context.Context, _ *Call) ([]byte, error) {
			panic(fmt.Errorf("the bearer token is %s", panicSecret))
		})
	var neighbour bool
	fx.D.Register(core.AppUsers, core.ServiceAPI, core.ActTrust,
		func(_ context.Context, _ *Call) ([]byte, error) {
			neighbour = true
			return []byte(`{"ok":true}`), nil
		})

	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe,
		Window: wire.MinWindow})
	if cl := c.closed(t); cl.Code != wire.CodeInternal {
		t.Fatalf("the channel of the panicking handler closed %+v, want %s", cl, wire.CodeInternal)
	}

	// The neighbour, on the same link and after the crash. This is the
	// assertion; everything above it is the setup.
	c2 := fx.OpenAdmin(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
		Action: core.ActTrust, Window: wire.MinWindow})
	c2.eof(t)
	if body := string(c2.result(t)); body != `{"ok":true}` {
		t.Fatalf("the neighbour channel answered %q", body)
	}
	if !neighbour {
		t.Fatal("the neighbour's handler never ran")
	}

	// Four ways the defect shows, and none of them is "it worked".
	if got := guard.Panics(); got != before+1 {
		t.Fatalf("the counter says %d catches, want %d", got-before, 1)
	}
	if e := fx.waitEntry(t, core.AppDesktop, core.ActMe); e.Result != string(wire.CodeInternal) {
		t.Fatalf("the audit entry of the crashed channel says %q", e.Result)
	}
	line := buf.String()
	if !strings.Contains(line, "level=ERROR") || !strings.Contains(line, "site=coreapp.open") {
		t.Fatalf("no crash line of the dispatcher: %q", line)
	}
	if !strings.Contains(line, "where=coreapp/panic_test.go:") {
		t.Fatalf("the line does not name the place that panicked: %q", line)
	}
	// And what is NOT in it. A crash line that printed the panic value would
	// hand on whatever the panicking caller had in hand.
	if strings.Contains(line, panicSecret) || strings.Contains(line, "bearer token") {
		t.Fatalf("the value of the panic reached the log: %q", line)
	}
}

// TestAPanicInAnAppChannelLeavesTheCoreChannelsAlone is the same measurement
// for the other way through open: an app channel goes to the relay, so the
// panic happens somewhere else entirely (forward, not run).
func TestAPanicInAnAppChannelLeavesTheCoreChannelsAlone(t *testing.T) {
	_ = catchLog(t)
	fx := newFixture(t)
	fx.PutApp(t, appX, true)
	fx.Grant(t, appX+"/"+core.RightUse, appX+"/"+appAct)
	// The relay of the fixture panics instead of forwarding.
	fx.Relay.panics = true
	fx.D.Register(core.AppUsers, core.ServiceAPI, core.ActTrust,
		func(_ context.Context, _ *Call) ([]byte, error) { return []byte(`{}`), nil })

	// E2E: an app channel carries the handshake of the end-to-end session, and
	// an OPEN without it is a protocol error long before the relay.
	c := fx.Open(t, wire.Open{App: appX, Service: appSvc, Action: appAct,
		Window: wire.MinWindow, E2E: make([]byte, wire.HandshakeSize)})
	if cl := c.closed(t); cl.Code != wire.CodeInternal {
		t.Fatalf("the app channel closed %+v, want %s", cl, wire.CodeInternal)
	}
	c2 := fx.OpenAdmin(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
		Action: core.ActTrust, Window: wire.MinWindow})
	c2.eof(t)
	if body := string(c2.result(t)); body != `{}` {
		t.Fatalf("the core channel after the crash answered %q", body)
	}
}
