package link

import (
	"context"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// bctx is the context of the cases here; they cancel their own where it is
// the point of the case.
var bctx = context.Background()

// The buffer of a channel is the one place a frame of a numbered channel waits
// between the link's single reader and whoever serves that channel. Two
// packages fill it (browser, controllers) and two read it (coreapp, relay), so
// it lives here and not in either of them.

func TestNextHandsOutWhatWasDelivered(t *testing.T) {
	tb := NewTable(4, false)
	c, err := tb.Open(1, wire.MinWindow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !tb.Deliver(1, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("abc")}) {
		t.Fatal("the frame was not taken")
	}
	if !tb.Deliver(1, wire.Frame{Type: wire.TypeEOF, Channel: 1}) {
		t.Fatal("the EOF was not taken")
	}
	f, ok := c.Next(bctx)
	if !ok || f.Type != wire.TypeData || string(f.Payload) != "abc" {
		t.Fatalf("first %+v %v", f, ok)
	}
	f, ok = c.Next(bctx)
	if !ok || f.Type != wire.TypeEOF {
		t.Fatalf("second %+v %v", f, ok)
	}
}

// TestDeliverToANumberThatIsNotOpenIsRefused: the buffer belongs to a channel,
// and a frame for a number that is free has no channel to wait in.
func TestDeliverToANumberThatIsNotOpenIsRefused(t *testing.T) {
	tb := NewTable(4, false)
	if tb.Deliver(7, wire.Frame{Type: wire.TypeEOF, Channel: 7}) {
		t.Fatal("a frame for a free number was taken")
	}
}

// TestNextWaitsForTheNextFrame holds that Next is a wait and not a poll: the
// frame arrives while the reader of the case is already waiting.
func TestNextWaitsForTheNextFrame(t *testing.T) {
	tb := NewTable(4, false)
	c, _ := tb.Open(1, wire.MinWindow)
	go func() {
		time.Sleep(20 * time.Millisecond)
		tb.Deliver(1, wire.Frame{Type: wire.TypeEOF, Channel: 1})
	}()
	f, ok := c.Next(bctx)
	if !ok || f.Type != wire.TypeEOF {
		t.Fatalf("%+v %v", f, ok)
	}
}

// TestNextEndsWithTheContext: every wait of this project has a bound, and the
// bound of this one is the caller's context.
func TestNextEndsWithTheContext(t *testing.T) {
	tb := NewTable(4, false)
	c, _ := tb.Open(1, wire.MinWindow)
	ctx, cancel := context.WithTimeout(bctx, 20*time.Millisecond)
	defer cancel()
	if f, ok := c.Next(ctx); ok {
		t.Fatalf("a frame came out of nowhere: %+v", f)
	}
}

// TestHangupWakesEveryWaiter is what a link that ended has to do: whoever
// waits for a frame hears false at once instead of waiting out its own clock.
func TestHangupWakesEveryWaiter(t *testing.T) {
	tb := NewTable(4, false)
	c, _ := tb.Open(1, wire.MinWindow)
	done := make(chan bool, 1)
	go func() {
		_, ok := c.Next(bctx)
		done <- ok
	}()
	time.Sleep(10 * time.Millisecond)
	tb.Hangup()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a frame came after the hangup")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter was not woken")
	}
}

// TestFramesDeliveredBeforeTheHangupAreStillRead: a hangup ends the waiting,
// not what already arrived. Otherwise a channel whose last DATA and EOF came
// in the same instant as the link's end would lose them.
func TestFramesDeliveredBeforeTheHangupAreStillRead(t *testing.T) {
	tb := NewTable(4, false)
	c, _ := tb.Open(1, wire.MinWindow)
	tb.Deliver(1, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("x")})
	tb.Hangup()
	f, ok := c.Next(bctx)
	if !ok || string(f.Payload) != "x" {
		t.Fatalf("%+v %v", f, ok)
	}
	if _, ok := c.Next(bctx); ok {
		t.Fatal("more than what was delivered came out")
	}
}

// TestDeliverAfterTheHangupIsRefused: once the link is gone nothing more
// arrives, and a caller that still tries hears so.
func TestDeliverAfterTheHangupIsRefused(t *testing.T) {
	tb := NewTable(4, false)
	tb.Open(1, wire.MinWindow)
	tb.Hangup()
	if tb.Deliver(1, wire.Frame{Type: wire.TypeEOF, Channel: 1}) {
		t.Fatal("a frame after the hangup was taken")
	}
}

// TestTheQueueOfAChannelIsBounded is the limit the credit does not give. DATA
// is counted against what we granted, so the BYTES in the queue are bounded
// anyway -- but a DATA frame may be empty and costs no credit, so the COUNT
// needs a bound of its own. Beyond it Deliver says false, and the caller
// answers E_PROTOCOL.
func TestTheQueueOfAChannelIsBounded(t *testing.T) {
	tb := NewTable(4, false)
	tb.Open(1, wire.MinWindow)
	for i := 0; i < MaxQueuedFrames; i++ {
		if !tb.Deliver(1, wire.Frame{Type: wire.TypeData, Channel: 1}) {
			t.Fatalf("frame %d of %d was refused", i, MaxQueuedFrames)
		}
	}
	if tb.Deliver(1, wire.Frame{Type: wire.TypeData, Channel: 1}) {
		t.Fatalf("frame %d was taken although the bound is %d", MaxQueuedFrames+1, MaxQueuedFrames)
	}
	// And the bound is a queue and not a wall: once one frame is read, one
	// more fits.
	if _, ok := tb.Get(1); !ok {
		t.Fatal("the channel is gone")
	}
	c, _ := tb.Get(1)
	if _, ok := c.Next(bctx); !ok {
		t.Fatal("nothing to read")
	}
	if !tb.Deliver(1, wire.Frame{Type: wire.TypeData, Channel: 1}) {
		t.Fatal("after reading one frame no further one fitted")
	}
}

// TestDeliverCopiesThePayload is the one that keeps a measured bug out:
// wire.Frame.Payload points into the buffer the link reads into, so a frame
// that waits in the queue without a copy of its own changes under whoever
// reads it next.
func TestDeliverCopiesThePayload(t *testing.T) {
	tb := NewTable(4, false)
	c, _ := tb.Open(1, wire.MinWindow)
	buf := []byte("first")
	tb.Deliver(1, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: buf})
	copy(buf, "XXXXX") // the link reads the next frame into the same bytes
	f, ok := c.Next(bctx)
	if !ok {
		t.Fatal("nothing came")
	}
	if string(f.Payload) != "first" {
		t.Fatalf("the payload changed under the queue: %q", f.Payload)
	}
}
