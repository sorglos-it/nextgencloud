package relay

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The fakes below stand in for package browser and package controllers. They
// keep the real link.Table of each hop, so the credit and the channel numbers
// of a case are counted by the code that counts them in production; only the
// socket is replaced.
//
// What they record, they copy, and that is not laziness about identity: a real
// Write seals the frame and is done with the slice when it returns, so the
// relay may reuse its buffer right after -- measured here, a recorder that
// kept the slice read every chunk of a transfer as the last one. The
// byte-for-byte cases therefore compare content, which is what the contract
// asks for; they say nothing about which array the bytes lived in.

// newRelay returns a relay whose wait for a controller's CLOSE is short: a
// case that ends a channel without the controller answering would otherwise
// pay CloseDrain. TestDrainIsBoundedByDefault holds the constant itself.
func newRelay() *Relay { return &Relay{Drain: 50 * time.Millisecond} }

// ev is one thing a fake was asked to do, in the order it was asked.
type ev struct {
	kind    string // "grant", "data", "eof", "close", "closecode", "open", "write"
	num     uint32
	payload []byte
	credit  uint32
	hs      []byte
	cl      wire.Close
	code    wire.Code
}

// recorder is the shared half of both fakes.
type recorder struct {
	mu   sync.Mutex
	evs  []ev
	seen chan ev // every event, so a case can wait for one instead of sleeping
}

func newRecorder() *recorder { return &recorder{seen: make(chan ev, 256)} }

func (r *recorder) add(e ev) {
	e.payload, e.hs = bytes.Clone(e.payload), bytes.Clone(e.hs)
	r.mu.Lock()
	r.evs = append(r.evs, e)
	r.mu.Unlock()
	select {
	case r.seen <- e:
	default:
	}
}

// events returns what happened so far.
func (r *recorder) events() []ev {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ev(nil), r.evs...)
}

// kinds returns the kinds of what happened, which is what an order is read off.
func (r *recorder) kinds() []string {
	out := []string{}
	for _, e := range r.events() {
		out = append(out, e.kind)
	}
	return out
}

// await waits for an event of a kind. It fails the case rather than hanging,
// because a relay that never gets there is the bug the case is looking for.
func (r *recorder) await(t *testing.T, kind string) ev {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		// Whatever already happened counts: the event may be in the slice
		// before this call.
		for _, e := range r.events() {
			if e.kind == kind {
				return e
			}
		}
		select {
		case <-r.seen:
		case <-deadline:
			t.Fatalf("no %q within 5 s; got %v", kind, r.kinds())
		}
	}
}

// errFake is what a fake answers for a frame it must not take: more DATA than
// the credit allows, or a GRANT beyond the window.
var errFake = errors.New("relay test: the fake refused")

// fakeShell is the browser side of a forwarded channel.
type fakeShell struct {
	*recorder
	tbl *link.Table
	in  chan wire.Frame

	// noCredit lets Data through without taking credit. A case that measures
	// a limit on the result would otherwise stall on the window first, and the
	// credit has cases of its own.
	noCredit bool
}

func newShell() *fakeShell {
	return &fakeShell{recorder: newRecorder(),
		tbl: link.NewTable(wire.MaxSessionChannels, false), in: make(chan wire.Frame, 64)}
}

// channel is the shell's channel number with the window the browser granted.
func (s *fakeShell) channel(t *testing.T) *link.Channel {
	t.Helper()
	c, err := s.tbl.Open(7, wire.DefaultWindow)
	if err != nil {
		t.Fatalf("shell channel: %v", err)
	}
	return c
}

func (s *fakeShell) GrantRaw(_ context.Context, c *link.Channel, credit uint32, hs []byte) error {
	if !c.Allow(credit) {
		return errFake
	}
	s.add(ev{kind: "grant", num: c.Num, credit: credit, hs: hs})
	return nil
}

func (s *fakeShell) Data(_ context.Context, c *link.Channel, p []byte) error {
	if !s.noCredit && !c.Spend(len(p)) {
		return errFake
	}
	s.add(ev{kind: "data", num: c.Num, payload: p})
	return nil
}

func (s *fakeShell) EOF(_ context.Context, c *link.Channel) error {
	s.add(ev{kind: "eof", num: c.Num})
	return nil
}

func (s *fakeShell) CloseWith(_ context.Context, c *link.Channel, cl wire.Close) error {
	s.add(ev{kind: "close", num: c.Num, cl: cl})
	c.SentClose()
	return nil
}

func (s *fakeShell) Close(_ context.Context, code wire.Code, num uint32) error {
	s.add(ev{kind: "closecode", num: num, code: code})
	if c, ok := s.tbl.Get(num); ok {
		c.SentClose()
	}
	return nil
}

func (s *fakeShell) Next(ctx context.Context, _ *link.Channel) (wire.Frame, bool) {
	select {
	case f, ok := <-s.in:
		return f, ok
	case <-ctx.Done():
		return wire.Frame{}, false
	}
}

// send puts a frame of the browser into the buffer the relay reads.
func (s *fakeShell) send(f wire.Frame) { s.in <- f }

// fakeHop is the controller side of a forwarded channel.
type fakeHop struct {
	*recorder
	tbl *link.Table
	in  chan wire.Frame
}

func newHop(max int) *fakeHop {
	return &fakeHop{recorder: newRecorder(),
		tbl: link.NewTable(max, true), in: make(chan wire.Frame, 256)}
}

func (h *fakeHop) Table() *link.Table { return h.tbl }

func (h *fakeHop) Write(_ context.Context, f wire.Frame) error {
	kind := map[wire.Type]string{wire.TypeOpen: "open", wire.TypeData: "data",
		wire.TypeEOF: "eof", wire.TypeClose: "close", wire.TypeGrant: "grant"}[f.Type]
	h.add(ev{kind: kind, num: f.Channel, payload: f.Payload})
	return nil
}

func (h *fakeHop) Grant(_ context.Context, c *link.Channel, credit uint32) error {
	if !c.Allow(credit) {
		return errFake
	}
	h.add(ev{kind: "grant", num: c.Num, credit: credit})
	return nil
}

func (h *fakeHop) Next(ctx context.Context, _ *link.Channel) (wire.Frame, bool) {
	select {
	case f, ok := <-h.in:
		return f, ok
	case <-ctx.Done():
		return wire.Frame{}, false
	}
}

// send puts a frame of the controller into the buffer the relay reads.
func (h *fakeHop) send(f wire.Frame) { h.in <- f }

// grant builds the GRANT payload a controller sends.
func grant(credit uint32, hs []byte) wire.Frame {
	return wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(credit, hs)}
}

// handshake is a Noise message 2 of the right size; its bytes are only
// compared, never used as cryptography.
func handshake() []byte {
	hs := make([]byte, wire.HandshakeSize)
	for i := range hs {
		hs[i] = byte(i + 1)
	}
	return hs
}
