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
	// ErrClosing reports that a CLOSE E_PROTOCOL for this number has already
	// gone out. Whatever still arrives for it is dropped without an answer, and
	// the link stands; FailChannel is the only call that answers it.
	ErrClosing = errors.New("link: CLOSE already sent for this channel")
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

// Allow adds to what the other side may send us; the caller calls it before it
// sends a GRANT, so that both sides count the same, and sends the GRANT only
// when this says yes. The window of an OPEN we send ourselves is counted by
// Next.
//
// False has two causes, and in both of them nothing is added and the GRANT must
// not go out. Either it would lift that allowance above wire.MaxWindow, which
// the contract bounds in both directions, or the credit is zero, which is no
// GRANT at all (wire.ParseGrant refuses one, so the other side would not take
// it either). Both would be our own protocol error, and the other side would
// answer with CLOSE E_PROTOCOL. Without the first bound Take would hold
// nothing, because the side that counts what may arrive is the same side that
// raises the figure.
func (c *Channel) Allow(credit uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if credit == 0 || c.in+uint64(credit) > wire.MaxWindow {
		return false
	}
	c.in += uint64(credit)
	return true
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
//
// A max of zero or less is a table that carries no channel at all. It refuses
// every call, and which error it names depends on the call and not on max: the
// checks that do not look at the table at all answer first, so Open and Next
// still answer ErrProtocol for channel 0, for a window outside
// MinWindow..MaxWindow and for the wrong side, and FailChannel still answers
// ErrProtocol for channel 0. Everything that gets past those answers ErrLimit.
// Refusing is the safe answer and not an unbounded one, so a caller that forgets
// the limit grants no channel rather than every channel.
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
	// At most max-1 of these max candidates are taken, so one of them is free.
	// They are max different numbers: the sequence wraps at most once, and max
	// is far below the range of a uint32.
	for i := 0; i < t.max; i++ {
		num := t.next
		// Numbers run from 1 and wrap, so a long-lived link keeps finding one.
		// The wrap is the one place that could hand out channel 0, which is not
		// a channel at all, so it goes back to 1 and not to 0; num is therefore
		// never 0, and no second check for it is needed.
		t.next++
		if t.next == wire.ControlChannel {
			t.next = 1
		}
		// A number that is still open or closing must survive the wrap: taking
		// it again would replace a live channel in the map, and free's identity
		// check would then never remove either, so the number would be lost for
		// the life of the link.
		if _, taken := t.ch[num]; taken {
			continue
		}
		c := &Channel{Num: num, owner: t, in: uint64(window)}
		t.ch[num] = c
		return c, nil
	}
	return nil, ErrLimit
}

// FailChannel holds the number of a CLOSE E_PROTOCOL this side is about to
// send and notes that CLOSE in the same call: send writes the frame between the two
// halves, and nothing else of this package hands a bare reservation out.
//
// Holding and noting are ONE function on purpose. "Every held number is given
// back once the other side's CLOSE arrives" has no other holder: a reservation
// of its own could be made without ever noting a CLOSE, and the number would
// then count toward the limit for the life of the link. A lint over the callers
// was the weaker holder and is measured to be so -- it reads names and not
// values, and the files on disk and not the build. This one is held by the
// compiler, because reserving alone is not reachable from outside.
//
// A held number carries no credit in either direction: nothing may be sent or
// arrive on it. What the answers mean:
//
//   - ErrProtocol: channel 0, which is always open, carries Control and is
//     never a number of this table, so a protocol error on it ends the link
//     instead of closing a channel. Open refuses it for the same reason.
//   - ErrLimit: the stray frame would make one number too many, which ends the
//     link. Nothing was held and send was not called.
//   - ErrClosing: we already said E_PROTOCOL for this number. send was not
//     called, nothing is noted twice, and the link stands.
//   - the error of send: the frame did not go out, so no CLOSE is noted and the
//     number stays held. That is not a leak this package can mend and it is not
//     one in practice: a CLOSE has a fixed short payload, so the only way its
//     write fails is the socket, and that has ended the link and the table with
//     it (link.go, Write).
func (t *Table) FailChannel(num uint32, send func() error) error {
	if num == wire.ControlChannel {
		return ErrProtocol
	}
	c, err := t.hold(num)
	if err != nil {
		return err
	}
	if c.Closing() {
		return ErrClosing
	}
	if err := send(); err != nil {
		return err
	}
	c.SentClose()
	return nil
}

// hold returns the channel of a number, making a held one when the number is
// free. It is unexported because a caller that could reach it could hold a
// number without ever noting its CLOSE -- see FailChannel.
func (t *Table) hold(num uint32) (*Channel, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.ch[num]; ok {
		return c, nil
	}
	if len(t.ch) >= t.max {
		return nil, ErrLimit
	}
	c := &Channel{Num: num, owner: t}
	t.ch[num] = c
	return c, nil
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
