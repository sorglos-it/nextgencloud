package link

import (
	"bytes"
	"context"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// MaxQueuedFrames bounds how many frames of one channel may wait between the
// link's single reader and whoever serves that channel.
//
// Why a bound of its own, next to the credit: DATA is counted against what we
// granted (Take), so the BYTES waiting here are already bounded by the window
// of the channel. The COUNT is not -- a DATA frame may carry an empty payload
// and costs no credit at all, and a peer that sent nothing but those would
// grow this queue without limit. Beyond the bound Deliver answers false and
// the reader answers CLOSE E_PROTOCOL, which is the same answer it gives for
// more DATA than granted.
//
// The number is roomy on purpose: a peer with a full window of 1 MiB and
// payloads of a kilobyte has a thousand frames in flight, which still fits.
// Whoever lowers it should know that it is then a limit a well-behaved shell
// can reach.
const MaxQueuedFrames = 2048

// Deliver puts a frame of a numbered channel into that channel's queue, where
// whoever serves the channel takes it with Channel.Next. It answers false for
// a number that is not open, for a queue that is full and after Hangup; in all
// three the caller answers CLOSE E_PROTOCOL or ends the link, and nothing is
// kept.
//
// It is a method of Table and NOT of Channel, and that is what holds the one
// sentence this whole seam rests on: a dispatcher is handed a *Channel and
// never the Table (both browser.Session and controllers.Tunnel keep theirs
// unexported), so a dispatcher can READ frames and cannot put one in. Only the
// link's own single reader, which lives in the package that owns the table,
// can say that something arrived.
//
// The payload is copied. wire.Frame.Payload points into the buffer the link
// reads into, so a frame that waited here without a copy would change under
// whoever read it next (TestDeliverCopiesThePayload).
func (t *Table) Deliver(num uint32, f wire.Frame) bool {
	t.mu.Lock()
	c, ok := t.ch[num]
	gone := t.gone
	t.mu.Unlock()
	if !ok || gone {
		return false
	}
	f.Payload = bytes.Clone(f.Payload)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) >= MaxQueuedFrames {
		return false
	}
	c.queue = append(c.queue, f)
	// Buffered with one place and never waited on: the reader of a link must
	// not be held up by a channel nobody is reading at this moment.
	select {
	case c.woke <- struct{}{}:
	default:
	}
	return true
}

// Hangup says that the link is gone. Everyone waiting in Next hears false at
// once instead of waiting out their own clock, and nothing more is taken.
// Frames that arrived before it are still handed out: a hangup ends the
// waiting, not what already came.
func (t *Table) Hangup() {
	t.mu.Lock()
	t.gone = true
	list := make([]*Channel, 0, len(t.ch))
	for _, c := range t.ch {
		list = append(list, c)
	}
	t.mu.Unlock()
	for _, c := range list {
		c.mu.Lock()
		c.gone = true
		c.mu.Unlock()
		select {
		case c.woke <- struct{}{}:
		default:
		}
	}
}

// Next returns the next frame of the channel. False means that nothing more
// will come: the link is gone (Table.Hangup) or ctx ended -- which is the
// bound of this wait, and the caller's to set.
//
// This is where a frame is read, and the only place: no caller of this package
// reads the socket. A deadline on a read of coder/websocket ends the whole
// link rather than the read (link.go, Read), so a wait with a clock has to
// happen here, behind the one reader.
func (c *Channel) Next(ctx context.Context) (wire.Frame, bool) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			f := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return f, true
		}
		gone := c.gone
		c.mu.Unlock()
		if gone {
			return wire.Frame{}, false
		}
		select {
		case <-c.woke:
		case <-ctx.Done():
			return wire.Frame{}, false
		}
	}
}

// Queued returns how many frames of the channel are waiting. It is for a
// caller that wants to know whether it is behind, and for the cases that
// measure the bound.
func (c *Channel) Queued() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.queue)
}
