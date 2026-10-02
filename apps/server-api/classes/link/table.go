package link

import (
	"errors"
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var (
	// ErrLimit reports a channel number beyond the limit of the link; the
	// caller ends the link, because a peer that counts along never reaches it.
	ErrLimit = errors.New("link: channel limit reached")
	// ErrProtocol reports an OPEN for a number that is still open, an OPEN from
	// the side that does not open channels, an OPEN for channel 0 and a window
	// outside MinWindow..MaxWindow. The answer is CLOSE E_PROTOCOL for that
	// number; a frame for a number that is neither open nor closing is the same
	// error, but the caller sees it as Get reporting false.
	ErrProtocol = errors.New("link: protocol error on a channel")
)

// Channel is one open or closing channel number with the credit of both
// directions. Every method may be called from the reading and the writing side
// at once, so the counters sit behind one lock each channel.
type Channel struct {
	Num uint32

	owner *Table // set once, before the channel leaves the table

	mu        sync.Mutex
	out       uint64 // bytes we may still send
	in        uint64 // bytes the other side may still send
	sentClose bool
	recvClose bool
}

// SendAllowed reports whether n more bytes of DATA payload fit into what the
// other side granted. It does not take them; Spend does that, so that a caller
// can ask before it builds a frame it may not send.
func (c *Channel) SendAllowed(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return n >= 0 && uint64(n) <= c.out
}

// Spend takes n bytes off our allowance; false when there is not enough, which
// is our own mistake, not the other side's.
func (c *Channel) Spend(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < 0 || uint64(n) > c.out {
		return false
	}
	c.out -= uint64(n)
	return true
}

// Grant adds credit the other side sent us; false when it would lift the
// allowance above wire.MaxWindow, which closes the channel with E_PROTOCOL.
// A credit of zero is refused for the same reason: wire.ParseGrant does not
// let one through, so seeing one means the frame did not come that way.
func (c *Channel) Grant(credit uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if credit == 0 || c.out+uint64(credit) > wire.MaxWindow {
		return false
	}
	c.out += uint64(credit)
	return true
}

// Take counts n bytes of DATA that arrived; false when more came than we
// granted, which closes the channel with E_PROTOCOL.
func (c *Channel) Take(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < 0 || uint64(n) > c.in {
		return false
	}
	c.in -= uint64(n)
	return true
}

// Allow adds to what the other side may send us. The caller calls it whenever
// it sends a GRANT, so that both sides count the same; the window of an OPEN we
// send ourselves is counted by Next.
func (c *Channel) Allow(credit uint32) {
	c.mu.Lock()
	c.in += uint64(credit)
	c.mu.Unlock()
}

// Closing reports whether we have already sent a CLOSE. Frames that still
// arrive for such a number are dropped without an answer.
func (c *Channel) Closing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sentClose
}

// SentClose notes that we sent a CLOSE for this channel.
func (c *Channel) SentClose() {
	c.mu.Lock()
	c.sentClose = true
	done := c.recvClose
	c.mu.Unlock()
	if done {
		c.owner.free(c)
	}
}

// RecvClose notes that the other side's CLOSE arrived. It counts even when it
// came in after our own CLOSE, among frames that are otherwise dropped.
func (c *Channel) RecvClose() {
	c.mu.Lock()
	c.recvClose = true
	done := c.sentClose
	c.mu.Unlock()
	if done {
		c.owner.free(c)
	}
}

// Table holds the channel numbers of one link.
type Table struct {
	max    int
	opener bool // we pick the numbers: the browser on a session link, the API on a tunnel

	mu   sync.Mutex
	ch   map[uint32]*Channel
	next uint32
}

// NewTable returns a table for at most max numbers that are not free
// (wire.MaxSessionChannels or wire.MaxTunnelChannels). opener says whether this
// side picks the numbers; only one of the two sides does.
func NewTable(max int, opener bool) *Table {
	return &Table{max: max, opener: opener, ch: map[uint32]*Channel{}, next: 1}
}

// Open records an OPEN that arrived, with the window the opener granted us.
// That window is what we may send; what may arrive is nothing until we grant it
// ourselves with Allow.
func (t *Table) Open(num, window uint32) (*Channel, error) {
	if num == wire.ControlChannel || t.opener || window < wire.MinWindow || window > wire.MaxWindow {
		return nil, ErrProtocol
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.ch[num]; ok {
		return nil, ErrProtocol
	}
	if len(t.ch) >= t.max {
		return nil, ErrLimit
	}
	c := &Channel{Num: num, owner: t, out: uint64(window)}
	t.ch[num] = c
	return c, nil
}

// Next picks the next free number for a channel we open, with the window we
// grant the other side. We may send nothing on it until its first GRANT.
func (t *Table) Next(window uint32) (*Channel, error) {
	if !t.opener || window < wire.MinWindow || window > wire.MaxWindow {
		return nil, ErrProtocol
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.ch) >= t.max {
		return nil, ErrLimit
	}
	// At most max-1 numbers are taken here, so max+1 candidates always hold a
	// free one, even when one of them is channel 0 after a wrap.
	for i := 0; i <= t.max; i++ {
		num := t.next
		// Numbers run from 1 and wrap, so a long-lived link keeps finding one.
		t.next++
		if t.next == 0 {
			t.next = 1
		}
		if num == wire.ControlChannel {
			continue
		}
		if _, taken := t.ch[num]; taken {
			continue
		}
		c := &Channel{Num: num, owner: t, in: uint64(window)}
		t.ch[num] = c
		return c, nil
	}
	return nil, ErrLimit
}

// Reserve holds a number whose CLOSE E_PROTOCOL we are about to send, so that
// it counts toward the limit until the other side's CLOSE arrives. It carries
// no credit in either direction: nothing may be sent or arrive on it. ErrLimit
// means the stray frame would make one number too many, which ends the link.
func (t *Table) Reserve(num uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.ch[num]; ok {
		return nil
	}
	if len(t.ch) >= t.max {
		return ErrLimit
	}
	t.ch[num] = &Channel{Num: num, owner: t}
	return nil
}

// Get returns a channel that is open or closing. False means the number is
// free, so a frame for it is a protocol error.
func (t *Table) Get(num uint32) (*Channel, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.ch[num]
	return c, ok
}

// Count returns how many numbers are not free.
func (t *Table) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.ch)
}

// free removes a number once a CLOSE for it was both sent and received. It
// compares the channel it holds, because the number may already belong to a new
// channel: a late call from the old one must not take its successor's number.
func (t *Table) free(c *Channel) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur, ok := t.ch[c.Num]; ok && cur == c {
		delete(t.ch, c.Num)
	}
}
