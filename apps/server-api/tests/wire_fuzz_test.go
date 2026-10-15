// Package tests holds fuzz targets for contract functions in shared/ that
// read foreign bytes and had no target of their own. They live here and not
// next to the contract because shared/ is frozen: a new file under shared/
// changes its tree hash and is therefore a change. Reading from shared/ is
// not.
package tests

import (
	"runtime"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// FuzzWireParseGrant feeds a GRANT payload: 4 bytes of credit and nothing, or
// the Noise message 2 behind it. Accepted credit must be inside the window and
// the handshake must be the one size the contract names -- a GRANT that passes
// with credit 0 or a stray length is a finding on its own.
func FuzzWireParseGrant(f *testing.F) {
	f.Add(wire.Grant(1, nil))                                      // valid
	f.Add(wire.Grant(1, make([]byte, wire.HandshakeSize)))         // valid, with handshake
	f.Add(wire.Grant(1, make([]byte, wire.HandshakeSize-1))[:4+1]) // a hair off

	f.Fuzz(func(t *testing.T, p []byte) {
		credit, hs, err := wire.ParseGrant(p)
		if err != nil {
			if credit != 0 || hs != nil {
				t.Fatalf("refused %d bytes but returned %d, %d bytes", len(p), credit, len(hs))
			}
			return
		}
		if credit == 0 || credit > wire.MaxWindow {
			t.Fatalf("accepted credit %d", credit)
		}
		if len(hs) != 0 && len(hs) != wire.HandshakeSize {
			t.Fatalf("accepted handshake of %d bytes", len(hs))
		}
		if len(p) != 4+len(hs) {
			t.Fatalf("%d bytes in, %d bytes of handshake out", len(p), len(hs))
		}
	})
}

// FuzzWireParseOpen feeds an OPEN payload as a browser sends it. What is
// pinned is the contract's own rule set: an accepted OPEN must survive
// Validate a second time, and its fields must be the ones the grammar allows.
// Re-validating is the oracle -- ParseOpen skips the full params scan on
// purpose (the budget has already seen them), so a payload that passes the
// short way and fails the long one is a real difference.
func FuzzWireParseOpen(f *testing.F) {
	f.Add([]byte(`{"app":"ngc.users","service":"a","action":"b","window":65536}`))
	f.Add([]byte(`{"app":"ngc.users","service":"a","action":"b","window":0}`))
	f.Add([]byte("{}"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, p []byte) {
		o, err := wire.ParseOpen(p)
		if err != nil {
			return
		}
		if err := o.Validate(); err != nil {
			t.Fatalf("parsed %d bytes, then Validate: %v", len(p), err)
		}
		if !wire.ValidAppID(o.App) || !wire.ValidName(o.Service) || !wire.ValidName(o.Action) {
			t.Fatalf("accepted app %q service %q action %q", o.App, o.Service, o.Action)
		}
		if o.Window < wire.MinWindow || o.Window > wire.MaxWindow {
			t.Fatalf("accepted window %d", o.Window)
		}
		if !wire.ValidParams(o.Params) {
			t.Fatalf("accepted params of %d bytes", len(o.Params))
		}
		if o.Task != nil && len(o.Params) != 0 {
			t.Fatalf("task and %d bytes of own params", len(o.Params))
		}
	})
}

// FuzzWireParseClose feeds a CLOSE payload. An accepted CLOSE must survive
// Validate again, and a status error must carry a code the contract knows:
// a close reason nobody can read is how a channel ends without a reason.
func FuzzWireParseClose(f *testing.F) {
	f.Add([]byte(`{"status":"ok"}`))
	f.Add([]byte(`{"status":"ok","code":"E_LIMIT"}`))
	f.Add([]byte("{}"))

	f.Fuzz(func(t *testing.T, p []byte) {
		c, err := wire.ParseClose(p)
		if err != nil {
			return
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("parsed %d bytes, then Validate: %v", len(p), err)
		}
		if c.Status == wire.StatusOK && c.Code != "" {
			t.Fatalf("ok with code %q", c.Code)
		}
	})
}

// FuzzWireParseControl feeds a control message. Two things are pinned: the
// type must be one of the six the contract names, and -- because the body
// stays raw and decoding a raw body costs up to about twenty times its size
// (wire.controlBudget) -- what one message is allowed to allocate.
//
// The ceiling is deliberately far above the documented factor so that only a
// real blow-up trips it: an input that asks for memory out of proportion to
// its own size is a finding even without a crash.
func FuzzWireParseControl(f *testing.F) {
	f.Add([]byte(`{"t":"hello"}`))
	f.Add([]byte(`{"t":"hell"}`))
	f.Add([]byte("{}"))

	f.Fuzz(func(t *testing.T, p []byte) {
		before := heapAlloc()
		c, err := wire.ParseControl(p)
		grew := heapAlloc() - before
		if err == nil {
			switch c.T {
			case wire.CtlHello, wire.CtlTrust, wire.CtlNotify, wire.CtlStatus, wire.CtlUpdate, wire.CtlBye:
			default:
				t.Fatalf("accepted type %q", c.T)
			}
		}
		// 64 KiB of slack covers the fixed cost of a decoder for the smallest
		// inputs; above that the input pays for itself a hundred times over.
		if limit := uint64(100*len(p)) + 64<<10; grew > limit {
			t.Fatalf("%d bytes in, %d bytes of heap (limit %d)", len(p), grew, limit)
		}
	})
}

// heapAlloc reports the bytes this process has taken from the heap so far.
// It only ever grows, so a difference of two readings is what ran in between
// -- including what has already been collected again, which is the point: a
// parser that asks for four gibibytes and frees them is still a parser that
// asked for four gibibytes.
func heapAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}
