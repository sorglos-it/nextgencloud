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
// The two OPENs have to DIFFER for that, and they did not: both came out of
// the same helper and were byte-identical, and both were held against the
// first one. "Each kept its own bytes" then reads exactly like "both got the
// bytes of channel 1", which is the fault the case exists to catch. The
// end-to-end handshake of each OPEN is filled with its own byte now, and the
// two byte strings are held against the pair as a set -- each arrives exactly
// once. Measured on the way: with the two told apart, holding the FIRST
// forwarded channel against the first OPEN fails -- two goroutines hand them
// over and the order is nobody's promise. That is a fault of a case that
// assumes an order, not of the dispatcher, and it could not show while both
// OPENs were the same bytes.
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
	first := openBytes(t, appY, 0x11)
	second := openBytes(t, appY, 0x22)
	// The case checks its own setup: two OPENs that are the same byte string
	// would make the two comparisons below one comparison.
	if bytes.Equal(first, second) {
		t.Fatal("the two OPENs are the same bytes, so this case tells nothing apart")
	}
	c1 := fx.OpenRaw(t, first, wire.MinWindow)
	c2 := fx.OpenRaw(t, second, wire.MinWindow)
	// Which of the two reaches the relay first is not this case's business --
	// two goroutines hand them over -- so the two byte strings are compared
	// as a SET: each OPEN arrives once and neither arrives twice. That is
	// what "each kept its own bytes" means and all of it. The ORDER of the
	// two is not measured here and is not a promise of this layer.
	got := [][]byte{fx.Relay.forwarded(t).Raw, fx.Relay.forwarded(t).Raw}
	for _, want := range [][]byte{first, second} {
		n := 0
		for _, g := range got {
			if bytes.Equal(g, want) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%q arrived %d times, of %q and %q", want, n, got[0], got[1])
		}
	}
	c1.closed(t)
	c2.closed(t)
}

// openBytes is an app OPEN as a byte string, with the end-to-end handshake a
// non-core channel must carry. fill is the byte that handshake is made of, so
// two OPENs of the same app can be told apart.
func openBytes(t *testing.T, app string, fill byte) []byte {
	t.Helper()
	e2e := make([]byte, wire.HandshakeSize)
	for i := range e2e {
		e2e[i] = fill
	}
	o := wire.Open{App: app, Service: appSvc, Action: appAct, Window: wire.MinWindow, E2E: e2e}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
