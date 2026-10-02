package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestSocketNeedsASession(t *testing.T) {
	h := newServer(t)
	// Without a cookie the upgrade never happens: 401 with the body of the
	// contract.
	w, r := socketRequest("")
	h.Socket(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d", w.Code)
	}
	var c wire.Close
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	if c.Code != wire.CodeLogin {
		t.Fatalf("code %q", c.Code)
	}
}

func TestSocketCarriesAnOpenAndAnswers(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	f := c.read(t)
	if f.Type != wire.TypeGrant || f.Channel != 1 {
		t.Fatalf("frame %+v", f)
	}
}

func TestSocketEndsOnAForbiddenControlMessage(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	// Only "bye" may come from the browser; anything else ends the link.
	c.writeControl(t, wire.CtlStatus, []byte("{}"))
	wantEnded(t, c)
}

func TestSocketEndsOnBye(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeControl(t, wire.CtlBye, nil)
	wantEnded(t, c)
}

func TestSocketEndsBeyondTheChannelLimit(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	open := openOf()
	for i := 1; i <= wire.MaxSessionChannels+1; i++ {
		// The write of the frame that ends the link may itself fail, which is
		// not the finding: the finding is that the link ended.
		if err := c.writeErr(wire.Frame{Type: wire.TypeOpen, Channel: uint32(i),
			Payload: openPayload(t, open)}); err != nil {
			break
		}
	}
	wantEnded(t, c)
}

// openPayload is the JSON of an OPEN.
func openPayload(t *testing.T, o wire.Open) []byte {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return b
}

// --- the three conditions link.Table puts on its caller ---

// TestGrantRaisesWhatMayArriveBeforeItGoesOut holds the first sentence by its
// effect, and it says WHICH frame was the protocol error. That is the whole
// point: a case that only waits for a CLOSE after two DATA frames cannot tell a
// bound that is one too small from one that is right -- measured, the mutant
// Take(len(f.Payload)+1) survived exactly such a case.
//
// So the allowance is spent in two steps with a round trip between them. After
// the full credit there must be NO close and the link must still answer, which
// channel 2's GRANT says; only the byte after that closes channel 1. A GRANT
// that had gone out without Allow would have allowed nothing and the first frame
// would already have been the error.
//
// The window is not wire.MinWindow here. It is the one case that uses another
// one, so that a bound written as a constant instead of as the OPEN's own window
// has somewhere to fall.
func TestGrantRaisesWhatMayArriveBeforeItGoesOut(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	const window = 2 * wire.MinWindow
	c.writeOpen(t, 1, openWindow(window))
	f := c.read(t)
	credit, handshake, err := wire.ParseGrant(f.Payload)
	if f.Type != wire.TypeGrant || f.Channel != 1 || err != nil || len(handshake) != 0 {
		t.Fatalf("frame %+v credit %d handshake %d err %v", f, credit, len(handshake), err)
	}
	if credit != window {
		t.Fatalf("credit %d, want the window of the OPEN, %d", credit, window)
	}
	// The whole allowance at once, and nothing may follow it.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: make([]byte, credit)})
	c.writeOpen(t, 2, openOf())
	g := c.read(t)
	if g.Type != wire.TypeGrant || g.Channel != 2 {
		t.Fatalf("the full allowance was answered with %+v, want the GRANT of channel 2", g)
	}
	// One byte beyond the allowance, and that one is the protocol error.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte{0}})
	wantClose(t, c, 1, wire.CodeProtocol)
}

// TestTheTrustListDecidesTheOrigin holds the Origin check of the socket, which no
// other case touches: the shell is served from the origins of the trust list, and
// a page of another site gets no link even with a good cookie. Measured on the
// version before it: Origins set to nil survived every case of this package.
//
// The library lets two things through whatever the list says -- an Origin equal
// to the request host, and a request with no Origin header at all (link.Options)
// -- so both halves here carry an Origin of a site that is not the test server's
// host.
func TestTheTrustListDecidesTheOrigin(t *testing.T) {
	h := newServer(t)
	want := h.Fx.List.RP.Origins
	if len(want) != 1 {
		t.Fatalf("the fixture's trust list carries %d origins", len(want))
	}
	// The origin of the trust list is allowed, and the link really runs.
	c, err := dialOrigin(t, h, want[0])
	if err != nil {
		t.Fatalf("the origin of the trust list was refused: %v", err)
	}
	c.writeOpen(t, 1, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// Any other origin gets no upgrade at all.
	if _, err := dialOrigin(t, h, "https://evil.example.com"); err == nil {
		t.Fatal("a page of another site got a link")
	}
}

// TestTheBrowsersGrantRaisesWhatWeMaySend is the other direction of the window,
// and it is the only case that measures what a GRANT of the browser does. The
// OPEN grants what we may send; its GRANT raises that, and nothing else does.
// Measured without it: Grant swapped for Allow in feed survived, because no case
// ever sent anything.
// The window here is three times the smallest one, and that is the point of the
// number: what we may send has to come from the OPEN and not from a constant.
// Measured with wire.MinWindow in both places, the mutant that opens every
// channel with Table.Open(num, wire.MinWindow) survived.
func TestTheBrowsersGrantRaisesWhatWeMaySend(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	const window = 3 * wire.MinWindow
	c.writeOpen(t, 1, openWindow(window))
	o := firstOpened(t, h)
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// The window of the OPEN is what we may send, and not a byte more.
	if err := o.s.SendData(ctx, o.c, make([]byte, window)); err != nil {
		t.Fatalf("the window of the OPEN (%d bytes): %v", window, err)
	}
	if err := o.s.SendData(ctx, o.c, []byte{0}); !errors.Is(err, ErrCredit) {
		t.Fatalf("one byte beyond the window answered %v", err)
	}
	if f := c.read(t); f.Type != wire.TypeData || f.Channel != 1 || len(f.Payload) != window {
		t.Fatalf("frame %+v with %d bytes", f, len(f.Payload))
	}
	// Now the browser grants more, and exactly that much may go out again.
	c.write(t, wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(wire.MinWindow, nil)})
	// The reader is past the GRANT once it has answered the OPEN of channel 2.
	c.writeOpen(t, 2, openOf())
	firstOpened(t, h)
	if err := o.s.SendData(ctx, o.c, make([]byte, wire.MinWindow)); err != nil {
		t.Fatalf("after the browser's GRANT: %v", err)
	}
	if err := o.s.SendData(ctx, o.c, []byte{0}); !errors.Is(err, ErrCredit) {
		t.Fatalf("the GRANT raised more than it said: %v", err)
	}
}

// TestTheDispatcherRunsBesideTheReader holds the "go" in front of
// srv.Open.OpenChannel. A link has exactly one reader, so a dispatcher called in
// line would hold it: no further frame of any channel would be read while it
// runs. Measured without this case: taking the "go" away left every case of this
// package green.
//
// The dispatcher here waits at a gate, so while it is in there the reader must
// still have read the SECOND OPEN -- the table holds two numbers although no
// dispatcher has finished. Called in line, the reader would sit in the first
// dispatcher and the table would hold one.
//
// The waiting runs inside within, so a reader that never gets there falls by name
// instead of hanging.
func TestTheDispatcherRunsBesideTheReader(t *testing.T) {
	h := newServer(t)
	gate := make(chan struct{})
	h.Grant = func(ctx context.Context, s *Session, c *link.Channel, o wire.Open) {
		<-gate
		_ = s.SendGrant(ctx, c, o.Window)
	}
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	c.writeOpen(t, 2, openOf())
	open := 0
	within(t, "waiting for the reader to take both OPENs", func() {
		for {
			n := 0
			h.Srv.Sessions.Each(func(s *Session) { n = s.table.Count() })
			if n == 2 {
				open = n
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
	if open != 2 {
		t.Fatalf("the reader took %d of the two OPENs while a dispatcher was running", open)
	}
	// Now let both dispatchers go; both GRANTs arrive.
	close(gate)
	seen := map[uint32]bool{}
	for len(seen) < 2 {
		f := c.read(t)
		if f.Type != wire.TypeGrant {
			t.Fatalf("frame %+v", f)
		}
		seen[f.Channel] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("GRANTs for %v", seen)
	}
	firstOpened(t, h)
	firstOpened(t, h)
}

// TestWithinNamesAPanicInsteadOfEndingTheRun is the self-test of within's second
// half. A panic in a goroutine that is not the case's own ends the whole test
// binary, and then no case says anything at all -- measured, Each over a nil entry
// did that in 0,37 s. within catches it, so the case can fall by name; this one
// shows the catching works, using a panic of its own rather than a broken
// registry.
func TestWithinNamesAPanicInsteadOfEndingTheRun(t *testing.T) {
	var got string
	inner := &testing.T{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// within calls t.Fatalf, which ends ITS goroutine, so the check runs in
		// one of its own and reads the outcome afterwards.
		defer func() { _ = recover() }()
		within(inner, "a helper", func() { panic("boom") })
		got = "within came back as if nothing had happened"
	}()
	<-done
	if got != "" {
		t.Fatal(got)
	}
	if !inner.Failed() {
		t.Fatal("within let a panic through without failing the case")
	}
}

// TestCloseCarriesTheCodeItWasGiven is the one case that closes a channel with a
// code other than E_PROTOCOL. Without it, Close could write a fixed E_PROTOCOL
// and no case would notice -- measured, that mutant survived.
func TestCloseCarriesTheCodeItWasGiven(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	if err := o.s.Close(ctx, wire.CodeLimit, 1); err != nil {
		t.Fatalf("close: %v", err)
	}
	wantClose(t, c, 1, wire.CodeLimit)
	// And the number is noted as closing, which is what Close owes Reserve.
	ch, ok := o.s.table.Get(1)
	if !ok || !ch.Closing() {
		t.Fatalf("channel 1 after our CLOSE: open %v", ok)
	}
}

// TestReserveBeyondTheLimitEndsTheLink is the other way the limit is reached: not
// one OPEN too many, but a stray frame whose CLOSE E_PROTOCOL would take the one
// number that is left. The contract says the link ends (shared/README.md).
func TestReserveBeyondTheLimitEndsTheLink(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	for i := 1; i <= wire.MaxSessionChannels; i++ {
		c.writeOpen(t, uint32(i), openOf())
	}
	for i := 1; i <= wire.MaxSessionChannels; i++ {
		firstOpened(t, h)
	}
	// Every number is taken, so this one cannot be reserved.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: wire.MaxSessionChannels + 1, Payload: []byte("x")})
	wantEnded(t, c)
}

// TestEofFromTheBrowserKeepsTheChannel: EOF says the browser will send no more
// data on that channel. It is no protocol error and it closes nothing.
func TestEofFromTheBrowserKeepsTheChannel(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	c.write(t, wire.Frame{Type: wire.TypeEOF, Channel: 1})
	// The link answers the next OPEN, so it is past the EOF and still running.
	c.writeOpen(t, 2, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant || f.Channel != 2 {
		t.Fatalf("after the EOF: %+v", f)
	}
	ch, ok := o.s.table.Get(1)
	if !ok || ch.Closing() {
		t.Fatalf("channel 1 after the EOF: open %v", ok)
	}
}

// TestTrustAndNotifyFromTheBrowserEndTheLink: those two go from the API to the
// shell only (wire.ControlAllowed), so from the browser they are a broken rule.
func TestTrustAndNotifyFromTheBrowserEndTheLink(t *testing.T) {
	for _, kind := range []string{wire.CtlTrust, wire.CtlNotify} {
		t.Run(kind, func(t *testing.T) {
			h := newServer(t)
			c := dialSession(t, h)
			c.writeControl(t, kind, []byte(`{}`))
			wantEnded(t, c)
		})
	}
}

// TestHintAllReachesEveryShell: a new version of the trust list is told to every
// open shell at once.
func TestHintAllReachesEveryShell(t *testing.T) {
	h := newServer(t)
	a := dialSession(t, h)
	b := dialSession(t, h)
	// Both links are running once each has answered an OPEN.
	a.writeOpen(t, 1, openOf())
	b.writeOpen(t, 1, openOf())
	firstOpened(t, h)
	firstOpened(t, h)
	if n := registryCount(t, h.Srv.Sessions); n != 2 {
		t.Fatalf("open sessions %d", n)
	}
	within(t, "HintAll", func() { h.Srv.Sessions.HintAll(ctx, 7) })
	for i, c := range []*peer{a, b} {
		got := wantControl(t, c, wire.CtlTrust)
		var hint session.TrustHint
		if err := json.Unmarshal(got.B, &hint); err != nil {
			t.Fatalf("shell %d: hint %q: %v", i, got.B, err)
		}
		if hint.Seq != 7 {
			t.Fatalf("shell %d: seq %d", i, hint.Seq)
		}
	}
}

// TestNoGrantGoesOutWhenAllowSaysNo holds the second sentence: Allow says false
// for a credit of zero and for one that would lift the allowance above
// wire.MaxWindow, and in both cases nothing reaches the browser. A GRANT that
// did reach it would be answered with CLOSE E_PROTOCOL -- our own protocol
// error.
func TestNoGrantGoesOutWhenAllowSaysNo(t *testing.T) {
	h := newServer(t)
	grantNothing(t, h)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if err := o.s.SendGrant(ctx, o.c, 0); !errors.Is(err, ErrGrant) {
		t.Fatalf("a credit of zero answered %v", err)
	}
	if err := o.s.SendGrant(ctx, o.c, wire.MaxWindow+1); !errors.Is(err, ErrGrant) {
		t.Fatalf("a credit above the window answered %v", err)
	}
	// The next frame the browser sees must be the first GRANT that was allowed,
	// not one of the two that were refused.
	if err := o.s.SendGrant(ctx, o.c, wire.MinWindow); err != nil {
		t.Fatalf("a credit inside the window: %v", err)
	}
	f := c.read(t)
	if f.Type != wire.TypeGrant || f.Channel != 1 || !bytes.Equal(f.Payload, wire.Grant(wire.MinWindow, nil)) {
		t.Fatalf("frame %+v payload %v", f, f.Payload)
	}
}

// TestProtocolErrorGivesTheNumberBackAfterTheBrowsersClose holds the third
// sentence by its effect. The number of a stray frame counts toward the limit
// while the CLOSE E_PROTOCOL is unanswered, is answered exactly once however
// many strays follow, and is free again once the browser's CLOSE arrives --
// which only happens because SentClose was noted beside RecvClose.
func TestProtocolErrorGivesTheNumberBackAfterTheBrowsersClose(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// A DATA frame for a number that is neither open nor closing.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 2, Payload: []byte("x")})
	wantClose(t, c, 2, wire.CodeProtocol)
	if n := o.s.table.Count(); n != 2 {
		t.Fatalf("the reserved number does not count: %d", n)
	}
	// A second stray for the same number is dropped without an answer, so the
	// next frame is the GRANT of channel 3 and not a second CLOSE.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 2, Payload: []byte("x")})
	c.writeOpen(t, 3, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant || f.Channel != 3 {
		t.Fatalf("a second stray was answered: %+v", f)
	}
	// Now the browser's CLOSE. Channel 4 after it says when the reader is past
	// it, so nothing here waits on a clock.
	body, err := json.Marshal(wire.Error(wire.CodeProtocol))
	if err != nil {
		t.Fatalf("close body: %v", err)
	}
	c.write(t, wire.Frame{Type: wire.TypeClose, Channel: 2, Payload: body})
	c.writeOpen(t, 4, openOf())
	<-h.Opened // channel 3
	<-h.Opened // channel 4
	if _, ok := o.s.table.Get(2); ok {
		t.Fatal("the number is still held after both CLOSEs")
	}
	if n := o.s.table.Count(); n != 3 {
		t.Fatalf("open numbers %d, want 1, 3 and 4", n)
	}
}

// --- what the browser may not do on a channel ---

func TestGrantFromTheBrowserMayCarryNoHandshake(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// Noise message 2 travels only in the first GRANT of the side that accepted
	// the channel, never from the opener.
	c.write(t, wire.Frame{Type: wire.TypeGrant, Channel: 1,
		Payload: wire.Grant(wire.MinWindow, make([]byte, wire.HandshakeSize))})
	wantClose(t, c, 1, wire.CodeProtocol)
}

func TestGrantFromTheBrowserMayNotLiftAboveTheWindow(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// The OPEN already granted MinWindow, so the whole window on top of it is
	// one too many.
	c.write(t, wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(wire.MaxWindow, nil)})
	wantClose(t, c, 1, wire.CodeProtocol)
}

func TestOpenForANumberThatIsOpenIsAProtocolError(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	c.write(t, wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: openPayload(t, openOf())})
	wantClose(t, c, 1, wire.CodeProtocol)
}

// TestUnknownFrameTypeOnAChannelIsAProtocolError calls feed directly, because no
// browser can reach that arm: wire.Frame.check refuses every type outside 1..5,
// so wire.Decode never hands one up and link.Read cannot produce the frame this
// case builds. The arm stays all the same -- feed must not depend on another
// package's list of types staying what it is -- and the CLOSE is what says it
// spoke.
func TestUnknownFrameTypeOnAChannelIsAProtocolError(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	f := wire.Frame{Type: wire.Type(99), Channel: 1}
	if _, err := f.Encode(); err == nil {
		t.Fatal("a frame of an unknown type can be encoded after all")
	}
	// The channel is closed, the link lives on, so feed says true.
	if !o.s.feed(ctx, o.c, f) {
		t.Fatal("an unknown frame type ended the whole link")
	}
	wantClose(t, c, 1, wire.CodeProtocol)
}

func TestOpenWithABrokenPayloadIsAProtocolError(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.write(t, wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: []byte("{")})
	wantClose(t, c, 1, wire.CodeProtocol)
}

// TestControlChannelTakesOnlyData holds the type check of channel 0, and it
// calls control directly because no browser can reach it: wire.Frame.check
// refuses OPEN, EOF, CLOSE and GRANT on channel 0 outright, so such a frame
// cannot be encoded and link.Read never hands one up. Measured: Encode answers
// wire: malformed frame. The check stays all the same -- it is this package's
// own statement that channel 0 carries control messages as DATA and nothing
// else, and it must not depend on a bound of another package staying where it
// is -- and this case is the only caller that can make it speak.
func TestControlChannelTakesOnlyData(t *testing.T) {
	h := newServer(t)
	for _, typ := range []wire.Type{wire.TypeOpen, wire.TypeEOF, wire.TypeClose, wire.TypeGrant} {
		f := wire.Frame{Type: typ, Channel: wire.ControlChannel}
		if _, err := f.Encode(); err == nil {
			t.Fatalf("type %d on channel 0 can be encoded after all", typ)
		}
		if h.Srv.control(ctx, &Session{}, f) {
			t.Fatalf("type %d was taken on channel 0", typ)
		}
	}
}

// --- the registry ---

func TestRegistryHoldsASessionWhileItRuns(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	firstOpened(t, h)
	if n := registryCount(t, h.Srv.Sessions); n != 1 {
		t.Fatalf("open sessions %d", n)
	}
	// A notice reaches the open shell through the registry.
	within(t, "Broadcast", func() {
		h.Srv.Sessions.Broadcast(ctx, wire.CtlNotify, core.Notice{Level: "info", Text: "Hallo"})
	})
	wantControl(t, c, wire.CtlNotify)
	c.close()
	// The link ends and the session is forgotten. Every call into the registry
	// runs in a goroutine of its own and the case only waits (within): the
	// registry's lock is taken by Count as well as by Each, so a lock that is not
	// given back would block the case itself, and a deadline behind the blocking
	// call is never reached. Measured on the mutant that forgets Each's Unlock
	// before this: the case fell in 10 s and took the whole package run with it
	// all the same, because the NEXT case called Count without a guard and hung
	// until the package timeout.
	within(t, "waiting for the session to be forgotten", func() {
		for h.Srv.Sessions.Count() != 0 {
			time.Sleep(time.Millisecond)
		}
	})
}

// TestRegistryAddsAndRemoves goes through within for the same reason the case
// above does: Add, Remove, Count and Each all take the one lock, so any of them
// can hang, and a hanging case deletes the result of every other case in the
// package.
func TestRegistryAddsAndRemoves(t *testing.T) {
	r := NewRegistry()
	a, b := &Session{}, &Session{}
	within(t, "Add", func() { r.Add(a); r.Add(b) })
	if n := registryCount(t, r); n != 2 {
		t.Fatalf("count %d", n)
	}
	seen := 0
	within(t, "Each", func() { r.Each(func(*Session) { seen++ }) })
	if seen != 2 {
		t.Fatalf("Each saw %d", seen)
	}
	within(t, "Remove", func() { r.Remove(a) })
	if n := registryCount(t, r); n != 1 {
		t.Fatalf("count after remove %d", n)
	}
}

// --- helpers of the cases ---

// wantControl reads until a control message of a kind arrives and returns it.
func wantControl(t *testing.T, p *peer, kind string) wire.Control {
	t.Helper()
	for {
		f, err := p.readErr()
		if err != nil {
			t.Fatalf("no control message %q: %v", kind, err)
		}
		if f.Channel != wire.ControlChannel {
			continue
		}
		var c wire.Control
		if err := json.Unmarshal(f.Payload, &c); err != nil {
			t.Fatalf("control %q: %v", f.Payload, err)
		}
		if c.T != kind {
			t.Fatalf("control %q, want %q", c.T, kind)
		}
		return c
	}
}

// wantClose reads until a CLOSE for a channel arrives and checks its code.
// Frames of other channels in between are the GRANTs of channels this case
// opened and are skipped.
func wantClose(t *testing.T, p *peer, num uint32, code wire.Code) {
	t.Helper()
	for {
		f, err := p.readErr()
		if err != nil {
			t.Fatalf("no CLOSE for channel %d: %v", num, err)
		}
		if f.Channel != num {
			continue
		}
		if f.Type != wire.TypeClose {
			t.Fatalf("channel %d answered %+v", num, f)
		}
		c, err := wire.ParseClose(f.Payload)
		if err != nil {
			t.Fatalf("close payload: %v", err)
		}
		if c.Code != code {
			t.Fatalf("code %q, want %q", c.Code, code)
		}
		return
	}
}

// TestSocketRefusesABrokenAPICertificate holds the one answer of Socket that no
// browser can bring about: a session row whose API certificate this API cannot
// read. The handshake could only fail, so the request is refused before the
// upgrade, with E_INTERNAL -- the damage is this API's own row.
func TestSocketRefusesABrokenAPICertificate(t *testing.T) {
	h := newServer(t)
	cookie, _ := loginOnce(t, h)
	raw, err := enc.B64.DecodeString(cookie)
	if err != nil {
		t.Fatalf("cookie: %v", err)
	}
	row, err := h.Srv.S.DB.SessionGet(ctx, session.TokenHash(raw))
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	row.APICert = []byte("{")
	if err := h.Srv.S.DB.SessionPut(ctx, row); err != nil {
		t.Fatalf("session put: %v", err)
	}
	w, r := socketRequest(cookie)
	h.Socket(w, r)
	if w.Code != 500 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var c wire.Close
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	if c.Code != wire.CodeInternal {
		t.Fatalf("code %q", c.Code)
	}
}
