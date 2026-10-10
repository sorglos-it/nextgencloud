package browser

import (
	"context"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The seam a dispatcher and the relay work over: frames come out of the
// channel's queue (link.Channel.Next), never off the socket. Who may read
// what, in one sentence: Server.run is the only code that reads the socket,
// and everyone else reads the queue.
//
// The sentence itself is held by the compiler and not by these cases:
// Session.link and Session.table are unexported, so no package outside this
// one has anything to read a socket with, and link.Table.Deliver is a method
// of the table, so nobody outside can put a frame into a queue either. What a
// case CAN measure is the effect that makes the sentence worth holding, and
// that is what the first one below does.

// TestANextThatRunsOutOfTimeLeavesTheLinkStanding is that effect. A read of
// coder/websocket with a deadline ends the whole link instead of the read
// (link.go, Read), so a dispatcher that read the socket with a clock of its
// own would take the session of every other channel down with it. Here the
// wait of one channel runs out and a second channel is served afterwards on
// the same link.
func TestANextThatRunsOutOfTimeLeavesTheLinkStanding(t *testing.T) {
	h := newServer(t)
	p := dialSession(t, h)
	p.writeOpen(t, 1, openOf())
	first := firstOpened(t, h)
	if f := p.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("first frame %+v", f)
	}
	wctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if f, ok := first.s.Next(wctx, first.c); ok {
		t.Fatalf("a frame came out of nowhere: %+v", f)
	}
	// The link is still there, and that is the whole point.
	p.writeOpen(t, 3, openOf())
	second := firstOpened(t, h)
	if f := p.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("the second channel got %+v", f)
	}
	if second.c.Num != 3 {
		t.Fatalf("channel %d", second.c.Num)
	}
}

// TestTheShellsFramesComeOutOfTheQueue holds the delivery: what the browser
// sends on a channel is what Next hands out, in order, and a GRANT of the
// browser is not among it -- it is counted and dropped, so a peer cannot fill
// a channel's queue with frames that cost no credit.
func TestTheShellsFramesComeOutOfTheQueue(t *testing.T) {
	h := newServer(t)
	p := dialSession(t, h)
	p.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := p.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("first frame %+v", f)
	}
	p.write(t, wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(wire.MinWindow, nil)})
	p.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("eins")})
	p.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("zwei")})
	p.write(t, wire.Frame{Type: wire.TypeEOF, Channel: 1})
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	for _, want := range []string{"eins", "zwei"} {
		f, ok := o.s.Next(rctx, o.c)
		if !ok || f.Type != wire.TypeData || string(f.Payload) != want {
			t.Fatalf("wanted DATA %q, got %+v %v", want, f, ok)
		}
	}
	f, ok := o.s.Next(rctx, o.c)
	if !ok || f.Type != wire.TypeEOF {
		t.Fatalf("wanted EOF, got %+v %v", f, ok)
	}
}

// TestReadInputStopsAtTheBound is the bound of a FlowIn channel, measured with
// its open way next to it: at the bound the input is complete, one byte past
// it the answer is E_TOOLARGE. Without the first half the second would also be
// green for a bound that refuses everything.
func TestReadInputStopsAtTheBound(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		code wire.Code
	}{
		{"at the bound", 16, ""},
		{"one byte past it", 17, wire.CodeTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newServer(t)
			p := dialSession(t, h)
			p.writeOpen(t, 1, openOf())
			o := firstOpened(t, h)
			if f := p.read(t); f.Type != wire.TypeGrant {
				t.Fatalf("first frame %+v", f)
			}
			p.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: make([]byte, tc.size)})
			p.write(t, wire.Frame{Type: wire.TypeEOF, Channel: 1})
			rctx, cancel := context.WithTimeout(ctx, readWait)
			defer cancel()
			body, code := o.s.ReadInput(rctx, o.c, 16)
			if code != tc.code {
				t.Fatalf("code %q, wanted %q", code, tc.code)
			}
			if tc.code == "" && len(body) != tc.size {
				t.Fatalf("%d bytes of input, wanted %d", len(body), tc.size)
			}
		})
	}
}

// TestReadInputGivesTheCreditBack holds that an upload larger than one window
// does not stall: every payload that was kept is granted again.
func TestReadInputGivesTheCreditBack(t *testing.T) {
	h := newServer(t)
	p := dialSession(t, h)
	p.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := p.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("first frame %+v", f)
	}
	go func() {
		rctx, cancel := context.WithTimeout(ctx, readWait)
		defer cancel()
		_, _ = o.s.ReadInput(rctx, o.c, 1<<20)
	}()
	p.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("acht byt")})
	f := p.read(t)
	if f.Type != wire.TypeGrant {
		t.Fatalf("no GRANT came back: %+v", f)
	}
	credit, _, err := wire.ParseGrant(f.Payload)
	if err != nil || credit != 8 {
		t.Fatalf("credit %d (%v), wanted the 8 bytes that were kept", credit, err)
	}
	p.write(t, wire.Frame{Type: wire.TypeEOF, Channel: 1})
}
