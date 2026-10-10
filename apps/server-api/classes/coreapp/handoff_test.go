package coreapp

import (
	"bytes"
	"encoding/json"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestTheRawOpenBytesTravelToTheRelay is the measurement behind the raw []byte
// of Relay.Forward. The OPEN payload is written by hand with a SPACE after a
// colon -- the JSON encoder of Go never writes one -- so a dispatcher that
// handed the relay a re-encoded OPEN instead of the bytes that arrived would
// be caught here and nowhere else.
//
// The counter-control stands in the same case, for the reason task 17 named:
// without it the case would also be green for a dispatcher that re-encodes,
// if the re-encoding happened to match. It asserts that json.Marshal of the
// PARSED open is NOT the same byte string, so the two really are different
// things.
func TestTheRawOpenBytesTravelToTheRelay(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse, appY+"/"+appAct)
	e2e := make([]byte, wire.HandshakeSize)
	for i := range e2e {
		e2e[i] = 0x42
	}
	enc, err := json.Marshal(struct {
		E2E []byte `json:"e2e"`
	}{e2e})
	if err != nil {
		t.Fatal(err)
	}
	e2eField := string(enc[len(`{"e2e":`) : len(enc)-1])
	// The space after the colon of "window" is the whole point.
	raw := []byte(`{"app":"` + appY + `","service":"` + appSvc + `","action":"` + appAct +
		`","window": 4096,"e2e":` + e2eField + `}`)
	o, err := wire.ParseOpen(raw)
	if err != nil {
		t.Fatalf("the hand-written OPEN is not one: %v", err)
	}
	again, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(again, raw) {
		t.Fatal("the encoder produced the same bytes, so this case could not tell the two apart")
	}
	c := fx.OpenRaw(t, raw, wire.MinWindow)
	r := fx.Relay.forwarded(t)
	if !bytes.Equal(r.Raw, raw) {
		t.Fatalf("the relay got %q, the browser sent %q", r.Raw, raw)
	}
	c.closed(t)
}

// TestTwoOpensInFlightKeepTheirOwnBytes holds that two channels opened one
// behind the other each get the bytes of their own OPEN.
//
// What it does NOT hold, and the mutant that proved it: the copy in
// browser.channel. A mutant that hands the relay f.Payload instead of
// bytes.Clone(f.Payload) stays ALIVE through the whole package. The reason is
// measured and not guessed -- today every frame's payload is a fresh
// allocation: coder/websocket hands Read a new slice per message, noisep.Open
// calls Decrypt with a nil destination, which appends into a new slice, and
// wire.Decode only cuts a window out of that. So nothing is reused and no case
// can see a copy that is missing.
//
// The copy stays anyway, because what it defends against is a change one layer
// down (a read buffer that IS reused), not a bug that exists now. That is the
// same limit task 17 named for Forwarded.Raw, and it is said here rather than
// dressed up as a measurement.
func TestTwoOpensInFlightKeepTheirOwnBytes(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse, appY+"/"+appAct)
	first := openBytes(t, appY)
	c1 := fx.OpenRaw(t, first, wire.MinWindow)
	c2 := fx.OpenRaw(t, openBytes(t, appY), wire.MinWindow)
	a := fx.Relay.forwarded(t)
	b := fx.Relay.forwarded(t)
	if !bytes.Equal(a.Raw, first) || !bytes.Equal(b.Raw, first) {
		t.Fatalf("one of the two carries other bytes:\n%q\n%q\n%q", a.Raw, b.Raw, first)
	}
	c1.closed(t)
	c2.closed(t)
}

// openBytes is an app OPEN as a byte string, with the end-to-end handshake a
// non-core channel must carry.
func openBytes(t *testing.T, app string) []byte {
	t.Helper()
	o := wire.Open{App: app, Service: appSvc, Action: appAct, Window: wire.MinWindow,
		E2E: make([]byte, wire.HandshakeSize)}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
