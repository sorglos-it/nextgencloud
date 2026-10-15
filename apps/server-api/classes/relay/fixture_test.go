package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The fakes below stand in for package browser and package controllers. Only
// the socket is replaced: each hop keeps a real link.Table, the credit of a
// case is counted by the code that counts it in production, and an arriving
// frame goes through the real per-channel queue (link.Table.Deliver,
// link.Channel.Next).
//
// That matters more than it looks. Since the seam of task 15 the two readers
// count what ARRIVES -- browser.Session.feed and controllers.Tunnel.frame --
// and the relay must not count it again. So send mirrors those two functions
// frame for frame, and a case that measures the window measures the same
// counting twice over: once here, once in link.Channel.
//
// What the fakes record, they copy. A real Write seals the frame and is done
// with the slice when it returns, so the relay may reuse its buffer right
// after -- measured here, a recorder that kept the slice read every chunk of a
// transfer as the last one. The byte-for-byte cases therefore compare content,
// which is what the contract asks for; they say nothing about which array the
// bytes lived in.

// ev is one thing a fake was asked to do, in the order it was asked.
type ev struct {
	kind    string // "grant", "data", "eof", "close", "closecode", "open"
	num     uint32
	payload []byte
	credit  uint32
	hs      []byte
	cl      wire.Close
	code    wire.Code
}

// newRelay returns a relay whose wait for a controller's CLOSE is short: a
// case that ends a channel without the controller answering would otherwise
// pay CloseDrain. TestDrainIsBoundedByDefault holds the constant itself.
func newRelay() *Relay { return &Relay{Drain: 50 * time.Millisecond} }

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
// the credit allows, or a GRANT beyond the window. The two real seams answer
// browser.ErrCredit and controllers.ErrGrant there.
var errFake = errors.New("relay test: credit or window refused the frame")

// fakeShell is the browser side of a forwarded channel (package browser).
type fakeShell struct {
	*recorder
	tbl *link.Table

	// noCredit lets SendData through without taking credit. A case that
	// measures a limit on the result would otherwise stall on the window
	// first, and the credit has cases of its own.
	noCredit bool
}

func newShell() *fakeShell {
	return &fakeShell{recorder: newRecorder(), tbl: link.NewTable(wire.MaxSessionChannels, false)}
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

func (s *fakeShell) SendGrant(_ context.Context, c *link.Channel, credit uint32, hs []byte) error {
	if !c.Allow(credit) {
		return errFake
	}
	s.add(ev{kind: "grant", num: c.Num, credit: credit, hs: hs})
	return nil
}

func (s *fakeShell) SendData(_ context.Context, c *link.Channel, p []byte) error {
	if !s.noCredit && !c.Spend(len(p)) {
		return errFake
	}
	s.add(ev{kind: "data", num: c.Num, payload: p})
	return nil
}

func (s *fakeShell) SendEOF(_ context.Context, c *link.Channel) error {
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

func (s *fakeShell) Next(ctx context.Context, c *link.Channel) (wire.Frame, bool) {
	return c.Next(ctx)
}

// send is what browser.Session.feed does with a frame of the browser: DATA is
// counted against what we granted, a GRANT is counted and then DROPPED, and
// what is left waits in the queue of the channel. Anything it refuses would
// have ended the session link in production, so it is the case's own mistake
// and says so.
//
// Errorf and not Fatalf: two cases send from a goroutine of their own, and
// Fatalf there ends the wrong goroutine.
func (s *fakeShell) send(t *testing.T, c *link.Channel, f wire.Frame) {
	t.Helper()
	switch f.Type {
	case wire.TypeData:
		if !c.Take(len(f.Payload)) {
			t.Errorf("the browser sent %d bytes it had no credit for", len(f.Payload))
			return
		}
	case wire.TypeGrant:
		credit, hs, err := wire.ParseGrant(f.Payload)
		if err != nil || len(hs) != 0 || !c.Grant(credit) {
			t.Errorf("GRANT of the browser refused: %v", err)
			return
		}
		// Counted and dropped: a GRANT says nothing the relay has to see, and
		// queueing it would be a way to fill a queue with frames that cost no
		// credit at all.
		return
	}
	if !s.tbl.Deliver(f.Channel, f) {
		t.Error("the queue of the shell channel refused the frame")
		return
	}
	if f.Type == wire.TypeClose {
		c.RecvClose()
	}
}

// fakeHop is the controller side of a forwarded channel (package controllers).
type fakeHop struct {
	*recorder
	tbl *link.Table
}

func newHop(max int) *fakeHop {
	return &fakeHop{recorder: newRecorder(), tbl: link.NewTable(max, true)}
}

func (h *fakeHop) Open(window uint32) (*link.Channel, error) { return h.tbl.Next(window) }

func (h *fakeHop) Get(num uint32) (*link.Channel, bool) { return h.tbl.Get(num) }

func (h *fakeHop) SendOpen(_ context.Context, c *link.Channel, payload []byte) error {
	h.add(ev{kind: "open", num: c.Num, payload: payload})
	return nil
}

func (h *fakeHop) SendData(_ context.Context, c *link.Channel, payload []byte) error {
	if !c.Spend(len(payload)) {
		return errFake
	}
	h.add(ev{kind: "data", num: c.Num, payload: payload})
	return nil
}

func (h *fakeHop) SendEOF(_ context.Context, c *link.Channel) error {
	h.add(ev{kind: "eof", num: c.Num})
	return nil
}

func (h *fakeHop) SendClose(_ context.Context, c *link.Channel, cl wire.Close) error {
	payload, err := json.Marshal(cl)
	if err != nil {
		return err
	}
	h.add(ev{kind: "close", num: c.Num, payload: payload, cl: cl})
	c.SentClose()
	return nil
}

func (h *fakeHop) Grant(_ context.Context, c *link.Channel, credit uint32) error {
	if !c.Allow(credit) {
		return errFake
	}
	h.add(ev{kind: "grant", num: c.Num, credit: credit})
	return nil
}

func (h *fakeHop) Next(ctx context.Context, c *link.Channel) (wire.Frame, bool) {
	return c.Next(ctx)
}

// send is what controllers.Tunnel.frame does with a frame of the controller:
// DATA is counted against what we granted, a GRANT is counted AND queued
// (its payload may carry Noise message 2), and the rest waits in the queue.
//
// It takes no *testing.T and answers nothing, because several cases send from
// a goroutine of their own. A frame it cannot count is dropped, which is what
// a case then sees: the relay hears nothing and the case fails on its own
// wait.
func (h *fakeHop) send(f wire.Frame) {
	c, ok := h.tbl.Get(f.Channel)
	if !ok {
		return
	}
	switch f.Type {
	case wire.TypeData:
		if !c.Take(len(f.Payload)) {
			return
		}
	case wire.TypeGrant:
		credit, _, err := wire.ParseGrant(f.Payload)
		if err != nil || !c.Grant(credit) {
			return
		}
	}
	if !h.tbl.Deliver(f.Channel, f) {
		return
	}
	if f.Type == wire.TypeClose {
		c.RecvClose()
	}
}

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
