package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrClosed says that the channel is over: this side has closed it, the CLOSE
// of the API has arrived, or the tunnel under it is gone. Nothing of a channel
// works after it, and nothing more will come.
var ErrClosed = errors.New("link: the channel is closed")

// Handler answers the channels an API opens. Only the API opens channels on a
// tunnel, so every channel here comes from there, and Open is called once for
// each of them, in a goroutine of its own.
//
// Three things hold for every call, and whoever writes a handler may count on
// all three:
//
//   - The call does not hold the reading of the tunnel. It may take as long as
//     the work takes; the frames of every other channel go on arriving.
//   - When Open comes back the channel is closed with exactly one CLOSE: the
//     one the handler sent itself (Channel.Close), or one this package writes
//   - status ok for a handler that came back with nil, E_INTERNAL for one
//     that came back with an error. That error ends its channel and nothing
//     else; it goes into the log of this controller, so a handler that puts
//     something of the tunnel into its error puts it into that log. The one
//     exception: if this hop has already answered a protocol error on that
//     number, the CLOSE says E_PROTOCOL, whatever the handler wanted. The
//     error of the handler still goes into the log.
//   - ctx ends with the channel: with the CLOSE of this side, with the CLOSE
//     of the API, and with the tunnel. A handler that waits on anything waits
//     on it too.
//
// What o says about the user, and what it does not: this package reads the
// OPEN only as far as it must to run the channel (tunnel.ParseOpen), because
// it holds no trust list. **o.Session is therefore always nil here** - not
// because the channel has no user, but because nobody has looked yet. Whoever
// authorises takes Channel.OpenPayload and checks it himself
// (tunnel.VerifyOpen); o.Session alone would say "channel without a user",
// and those are allowed for core.WithoutUser only.
//
// What a handler has to keep to: it writes through its Channel and nowhere
// else. Every write goes through the one write lock of the tunnel (Tunnel.Send),
// so a write that comes to a stand costs this tunnel its heartbeat for as long
// as it stands - the ping of the tunnel is skipped while a write of this side
// is running. That is bounded by WriteTimeout and does not cost the tunnel its
// life, but it does leave it unwatched for that while.
type Handler interface {
	Open(ctx context.Context, ch *Channel, o tunnel.Opened) error
}

// Inbound is one thing that arrived on a channel: DATA with its bytes, or an
// EOF of the API. A CLOSE is none of it - it ends the channel, and Recv says
// so with ErrClosed.
//
// They come as they came in. This hop counts the credit and nothing else: that
// the API sends its EOF once, that no DATA follows it, and that it sends
// nothing at all after its own CLOSE are rules of the contract this side does
// not enforce. A second EOF, DATA behind one, and whatever came in after the
// CLOSE of the API are handed on like everything else. The last of those is
// not a protocol error either: that is a frame for a number "that is neither
// open nor closing (never opened, or free again)", and a number whose CLOSE is
// in while the CLOSE of this side is still owed is neither of the two. Whoever
// reads a channel decides what to make of it.
type Inbound struct {
	Kind wire.Type // wire.TypeData or wire.TypeEOF
	Data []byte    // the payload of a DATA; it belongs to whoever received it
}

// The two answers this package writes by itself. They are marshalled once:
// what is sent here has to be a CLOSE payload of the contract, and
// Frame.Encode refuses an empty one.
var (
	closeOK, _          = json.Marshal(wire.Close{Status: wire.StatusOK})
	closeProtocol, _    = json.Marshal(wire.Error(wire.CodeProtocol))
	closeUnreachable, _ = json.Marshal(wire.Error(wire.CodeUnreachable))
	closeInternal, _    = json.Marshal(wire.Error(wire.CodeInternal))
)

// Channel is one channel of a tunnel. Everything of it may be used from any
// goroutine at the same time: the frames of one write do not interleave with
// those of another, and a write that is waiting for credit holds nothing that a
// Grant of the other direction needs - a handler that reads and writes at once
// can always grant, which is what gets its own write going again.
type Channel struct {
	c      *channels
	num    uint32
	open   []byte // the OPEN payload as it arrived
	ctx    context.Context
	cancel context.CancelFunc

	// smu holds one write at a time, so that the pieces of two do not come
	// apart on the wire. It is held while a write waits for credit; wmu is
	// not, so waiting for credit holds up no GRANT of the other direction.
	smu sync.Mutex
	// wmu is the write lock of this channel: one frame at a time, and the gate
	// the CLOSE closes. Whoever writes something other than that CLOSE takes
	// it, looks under mu whether the CLOSE of this side is claimed, and only
	// then writes; the CLOSE itself takes it after its claim and needs no such
	// look, because it holds the claim. Either way nothing of this channel goes
	// out behind it.
	//
	// The order of the locks of this package, all of it: smu, then wmu, then
	// c.mu (of the channels), then mu of one channel - and wmu before the write
	// lock of the tunnel. The edge that matters for a deadlock is wmu -> c.mu:
	// it is only ever taken that way round, by the write of the CLOSE, and
	// route takes c.mu -> mu and no wmu at all. mu is never held while writing.
	wmu sync.Mutex

	mu    sync.Mutex
	queue []Inbound
	bytes uint64 // payload bytes waiting in queue
	// in is the credit this side granted and the API has not spent, out the
	// credit the API granted and this side has not spent. Both are bytes of
	// DATA payload, both are at most wire.MaxWindow.
	in  uint64
	out uint64
	// eof says that this side sent its EOF, first that it sent its first
	// GRANT, closing that it claimed the one CLOSE of this side, closed that
	// the CLOSE is written, and peer that the CLOSE of the API arrived. A
	// number is free again only with closed and peer together.
	eof     bool
	first   bool
	closing bool
	closed  bool
	peer    bool
	// answer is what the claimed CLOSE of this side will say, and it is there
	// only as long as the write is still owed: whoever takes it writes it, and
	// only he. So the first claim decides the payload and a second caller can
	// neither write a second CLOSE nor change what the first one says.
	answer []byte
	// data wakes Recv, credit wakes the writers. One waiting place each, so a wake
	// meant for the one is never taken by the other; both hold one token and
	// are refilled by whoever finds more left.
	data   chan struct{}
	credit chan struct{}
}

// Num is the number of the channel.
func (ch *Channel) Num() uint32 { return ch.num }

// OpenPayload is the OPEN payload of the API as it arrived, kept for as long
// as the channel lives. This hop reads no more of it than it must to run the
// channel (tunnel.ParseOpen): who opened the channel is checked by whoever
// holds the trust list, and tunnel.VerifyOpen wants exactly these bytes. They
// belong to the channel; whoever changes them changes what a later check sees.
func (ch *Channel) OpenPayload() []byte { return ch.open }

// Window is how many bytes of DATA this side may still send before the API
// grants more: what the OPEN granted plus every GRANT since, less what went
// out. The writers wait for it by themselves; this is for whoever wants to know.
func (ch *Channel) Window() uint32 {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return uint32(ch.out)
}

// Recv gives the next thing that arrived on the channel. It waits until
// something is there, until ctx ends, or until the channel is over: ErrClosed.
// Everything that is waiting is handed out before that end - what the API sent
// before its CLOSE and what it sent after it (see Inbound) - so nothing that
// was taken is lost.
func (ch *Channel) Recv(ctx context.Context) (Inbound, error) {
	for {
		ch.mu.Lock()
		if len(ch.queue) > 0 {
			in := ch.queue[0]
			ch.queue[0] = Inbound{}
			ch.queue = ch.queue[1:]
			ch.bytes -= uint64(len(in.Data))
			more := len(ch.queue) > 0
			ch.mu.Unlock()
			if more {
				wake(ch.data)
			}
			return in, nil
		}
		over := ch.closing || ch.peer
		ch.mu.Unlock()
		if over {
			return Inbound{}, ErrClosed
		}
		select {
		case <-ch.data:
		case <-ch.ctx.Done():
			return Inbound{}, ErrClosed
		case <-ctx.Done():
			// The channel ends the context of its handler, so at the end of a
			// channel both of these are ready at once and the runtime takes
			// whichever it likes. What happened is the channel: whoever waits
			// here gets one answer for it and not two by turns.
			if ch.ctx.Err() != nil {
				return Inbound{}, ErrClosed
			}
			return Inbound{}, ctx.Err()
		}
	}
}

// Whole is one payload that has to arrive as exactly one DATA frame, and it is
// an interface so that the compiler can tell it from a stream of bytes:
// whatever satisfies it cannot be handed to SendStream, because that one takes
// []byte and nothing else. e2e.Sealed is the one thing in this build that
// satisfies it, and that is the whole point - a sealed inner message in two
// frames decrypts in neither of them.
//
// What is left open and is meant to be: WholePayload is exported, so whoever
// really wants the bytes can take them out and stream them. That is then a
// plain act and reads like one, the way a conversion between two named types
// does. Go has nothing stronger to offer here: a type whose bytes nobody can
// reach could not be checked by the tests of the package that makes it.
type Whole interface {
	// WholePayload is the one payload, in one piece.
	WholePayload() []byte
}

// SendStream puts b on the channel as DATA, in frames of at most
// wire.MaxPayload bytes. It sends no more than the API granted and waits for
// the rest, so a handler with more to say than the other end wants stands here
// and not in the memory of this controller. It comes back when everything is
// out, or with ErrClosed when the channel ended first.
//
// It is called SendStream because it FRAGMENTS: how many frames one b becomes
// is the credit of the moment, which is the timing of the other side. For a
// stream of bytes that is right and is what the contract asks for on a
// core-app channel ("a result is all DATA up to the other side's EOF", wire).
// For a payload that has to arrive whole it is fatal and it does not look
// fatal: the other side reports a broken cipher, it happens on one machine and
// not on the next, and a test case written for it kept the fault in only 70 %
// of its runs (measured 02. and 03.10.2026). Such a payload goes to SendWhole,
// and Whole is how the compiler keeps the two apart.
func (ch *Channel) SendStream(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	ch.smu.Lock()
	defer ch.smu.Unlock()
	for len(b) > 0 {
		n, err := ch.reserve(len(b))
		if err != nil {
			return err
		}
		if err := ch.write(wire.Frame{Type: wire.TypeData, Channel: ch.num, Payload: b[:n]}); err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// SendWhole puts b on the channel as ONE DATA frame, and waits until the other
// side has granted all of it at once.
//
// That is what an end-to-end payload needs and what SendStream cannot give: "Every
// DATA payload is then one Noise transport message" (wire, "End to end"), and
// SendStream breaks one b into as many frames as the credit of the moment
// allows. A
// Noise message that arrives in two frames decrypts in neither of them, and
// how the credit falls is the other side timing, so the fault turns up on one
// machine and not on the next. Measured on 02.10.2026: a result of 8 MiB went
// out whole on Windows and arrived in pieces in WSL - "cipher: message
// authentication failed" at the shell, with a frame of 488 bytes in the middle
// of a run of full ones.
//
// w must fit one frame (wire.MaxPayload) and, to come off at all, the window
// the other side gave. ErrConfig for a w that is too long for a frame.
//
// ctx is what bounds the wait for that credit, and it is not optional comfort:
// the whole of this call holds the send lock of the channel, so a peer that
// keeps back less than one payload of credit and then stands still would hold
// every later write of this channel too - including the one that ends it. The
// caller therefore hands in the clock its own work hangs on, and a wait that
// runs out comes back with that context error while the channel stays usable
// for whoever writes next. Without ctx the only ways out are enough credit and
// the end of the channel, and neither of those is a clock.
func (ch *Channel) SendWhole(ctx context.Context, w Whole) error {
	b := w.WholePayload()
	if len(b) == 0 {
		return nil
	}
	if len(b) > wire.MaxPayload {
		return ErrConfig
	}
	ch.smu.Lock()
	defer ch.smu.Unlock()
	if err := ch.reserveAll(ctx, len(b)); err != nil {
		return err
	}
	return ch.write(wire.Frame{Type: wire.TypeData, Channel: ch.num, Payload: b})
}

// reserveAll takes credit for want bytes, all of it or none, and waits until
// there is that much, until ctx ends or until the channel does. Every wake
// re-reads the whole allowance, so two GRANT that were coalesced into one wake
// are still counted together.
func (ch *Channel) reserveAll(ctx context.Context, want int) error {
	for {
		ch.mu.Lock()
		switch {
		case ch.closing || ch.peer:
			ch.mu.Unlock()
			return ErrClosed
		case ch.eof:
			// After its own EOF a side sends no more data.
			ch.mu.Unlock()
			return ErrConfig
		}
		if ch.out >= uint64(want) {
			ch.out -= uint64(want)
			ch.mu.Unlock()
			return nil
		}
		ch.mu.Unlock()
		select {
		case <-ch.credit:
		case <-ch.ctx.Done():
			return ErrClosed
		case <-ctx.Done():
			// Both are ready at once at the end of a channel, and then it is
			// the channel that happened - the same answer Recv gives there.
			if ch.ctx.Err() != nil {
				return ErrClosed
			}
			return ctx.Err()
		}
	}
}

// write puts one frame of this channel on the tunnel, unless the CLOSE of this
// side is claimed. Taking the write lock and looking at that in one go is what
// makes the CLOSE the last frame of this side on this number: a frame that got
// here first is written before it, one that comes later is not written at all.
func (ch *Channel) write(f wire.Frame) error {
	ch.wmu.Lock()
	defer ch.wmu.Unlock()
	ch.mu.Lock()
	over := ch.closing || ch.peer
	ch.mu.Unlock()
	if over {
		return ErrClosed
	}
	return ch.c.send(f)
}

// reserve takes credit for up to want bytes off the allowance and waits until
// there is some. It holds no write lock while it waits, so a handler that is
// waiting for credit can still grant in the other direction.
func (ch *Channel) reserve(want int) (int, error) {
	for {
		ch.mu.Lock()
		switch {
		case ch.closing || ch.peer:
			ch.mu.Unlock()
			return 0, ErrClosed
		case ch.eof:
			// After its own EOF a side sends no more data.
			ch.mu.Unlock()
			return 0, ErrConfig
		}
		n := uint64(want)
		if n > ch.out {
			n = ch.out
		}
		if n > wire.MaxPayload {
			n = wire.MaxPayload
		}
		if n > 0 {
			ch.out -= n
			ch.mu.Unlock()
			return int(n), nil
		}
		ch.mu.Unlock()
		select {
		case <-ch.credit:
		case <-ch.ctx.Done():
			return 0, ErrClosed
		}
	}
}

// EOF says that this side has nothing more to send. It may be sent once, and
// only before the CLOSE of this side.
func (ch *Channel) EOF() error {
	ch.smu.Lock()
	defer ch.smu.Unlock()
	ch.mu.Lock()
	switch {
	case ch.closing || ch.peer:
		ch.mu.Unlock()
		return ErrClosed
	case ch.eof:
		ch.mu.Unlock()
		return ErrConfig
	}
	ch.eof = true
	ch.mu.Unlock()
	return ch.write(wire.Frame{Type: wire.TypeEOF, Channel: ch.num})
}

// Grant gives the API credit for credit bytes of DATA payload. The first GRANT
// of a channel is at least wire.MinWindow and carries Noise message 2 on an
// end-to-end channel (handshake, wire.HandshakeSize bytes); every later one
// carries none. What is granted and not yet spent never goes above
// wire.MaxWindow. Values the contract does not allow are refused here and not
// sent: they would cost the channel at the other end.
func (ch *Channel) Grant(credit uint32, handshake []byte) error {
	// A GRANT of nothing is none of the contract (wire.ParseGrant), and a
	// handshake is Noise message 2 or nothing. What is above the window is not
	// asked here: the allowance below catches it, whatever this side already
	// granted.
	if credit == 0 || (len(handshake) != 0 && len(handshake) != wire.HandshakeSize) {
		return ErrConfig
	}
	// The write lock around the look and the write together: between finding
	// the CLOSE of this side unclaimed and writing, that CLOSE may not slip in
	// - and it cannot, because writing it takes this lock too. (The CLOSE is
	// claimed and its write taken before that lock, under mu; it needs no look
	// of its own, it holds the claim.)
	ch.wmu.Lock()
	defer ch.wmu.Unlock()
	ch.mu.Lock()
	switch {
	case ch.closing || ch.peer:
		ch.mu.Unlock()
		return ErrClosed
	case !ch.first && credit < wire.MinWindow:
		ch.mu.Unlock()
		return ErrConfig
	case ch.first && len(handshake) != 0:
		ch.mu.Unlock()
		return ErrConfig
	case ch.in+uint64(credit) > wire.MaxWindow:
		ch.mu.Unlock()
		return ErrConfig
	}
	// Booked before the frame goes out: the API may send the moment it has
	// read the GRANT, and what it then sends is counted against this.
	ch.in += uint64(credit)
	ch.first = true
	ch.mu.Unlock()
	return ch.c.send(wire.Frame{Type: wire.TypeGrant, Channel: ch.num, Payload: wire.Grant(credit, handshake)})
}

// Close ends the channel with cl. A channel gets exactly one CLOSE, and the
// first claim on it says what that CLOSE reads: a Close that comes after this
// hop has already answered a protocol error for the channel sends no cl of its
// own, and neither does a second Close or the end of the handler. What comes
// back is the error of the write if this call was the one that wrote, and nil
// otherwise. A cl that is none of the contract is refused and nothing is sent
// - the channel stays open and the end of the handler closes it.
func (ch *Channel) Close(cl wire.Close) error {
	if cl.Validate() != nil {
		return ErrConfig
	}
	p, err := json.Marshal(cl)
	if err != nil {
		return ErrConfig
	}
	return ch.c.close(ch, p)
}

// wake puts a token into a waiting place, or leaves the one that is there.
func wake(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// channels is the state of every channel number of one tunnel: open, closing
// (the CLOSE of this side is out) or reserved because a stray frame had to be
// answered. A number is in here until it is free, and how many are in here is
// what the limit of the contract counts.
//
// The rules are those of shared/README.md, "Öffnen, Schließen, Freigabe":
//
//   - Only the API opens. An OPEN for a number that is free opens a channel,
//     an OPEN for one that is open is a protocol error, and an OPEN for one
//     this side has closed is a frame after one's own CLOSE like any other.
//   - Every other frame for a number that is neither open nor closing is a
//     protocol error: the answer is CLOSE E_PROTOCOL for that number, and the
//     number stays taken until the CLOSE of the API for it has arrived.
//   - A frame that would take one number more than wire.MaxTunnelChannels ends
//     the link - an OPEN beyond it, and a stray frame whose CLOSE E_PROTOCOL
//     would be that number too. A stray CLOSE is not one of them: it brings
//     the CLOSE of the API with it, so its number is free again as soon as the
//     answer is out and it never is one number too many.
//   - After the CLOSE of this side everything is dropped without an answer.
//     The CLOSE of the API among it still counts and frees the number.
type channels struct {
	life context.Context
	h    Handler
	send func(wire.Frame) error
	log  *slog.Logger
	mu   sync.Mutex
	m    map[uint32]*Channel
}

func newChannels(life context.Context, h Handler, send func(wire.Frame) error, log *slog.Logger) *channels {
	return &channels{life: life, h: h, send: send, log: log, m: make(map[uint32]*Channel)}
}

// frame is the seam Run hands its frames to. The tunnel is the one this
// machinery writes to already, so only the frame counts here.
func (c *channels) frame(_ *Tunnel, f wire.Frame) error { return c.take(f) }

// what says what has to happen once the locks are given up again. Nothing here
// writes while it holds a lock: a write can come to a stand for WriteTimeout,
// and a lock held that long would hold every channel of the tunnel with it.
type what int

const (
	nothing   what = iota
	start          // a channel of its own begins
	answering      // the CLOSE route claimed for the number goes out
)

// take takes one frame of a channel. It is called from the goroutine that runs
// the tunnel, one frame at a time and in the order they arrived. What a channel
// does with its frames happens in the goroutine of that channel, so a handler
// never holds this one.
//
// One thing here does hold it, and it has to: the answer to a frame that opens
// or ends nothing - CLOSE E_PROTOCOL, CLOSE E_UNREACHABLE - is written from
// right here, so that it is out before the next frame is taken (a test can
// count on that, and so can the API). What it says was settled in route, under
// the lock of that channel and in the same breath as the decision to answer,
// so the end of a handler that this frame woke cannot put its own status in
// its place. That costs at most two waits of WriteTimeout: one for the write
// lock of that channel, if a write of it is standing in the socket, and one for
// the write itself. The ping of the tunnel is skipped while a write of this
// side runs, so the link lives through it (see Tunnel.Send), but for that
// while nothing here would notice a peer that went quiet.
//
// The error of that write is not passed on: it means the socket is gone or the
// deadline ran out, and in both cases the library has already torn the socket
// down, so the very next read of Run comes back with an error and ends the
// tunnel there. Ending it from here would take the answer away from every
// other channel on it for nothing.
func (c *channels) take(f wire.Frame) error {
	ch, o, do, err := c.route(f)
	if err != nil {
		return err
	}
	switch do {
	case start:
		go c.run(ch, o)
	case answering:
		c.finish(ch)
	}
	return nil
}

// route brings one frame to its number and says what is left to do. Every
// answer it decides on it claims on the spot, in the same breath as the
// decision and under the lock of that channel: what take then writes is
// settled, whoever else is ending the channel at the same moment.
func (c *channels) route(f wire.Frame) (*Channel, tunnel.Opened, what, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, known := c.m[f.Channel]
	if !known {
		// A number that is free. It is taken from here on, whatever the frame
		// was: an OPEN opens a channel, everything else is answered and the
		// number waits for the CLOSE of the API.
		//
		// The limit ends the link for a frame that would make one number too
		// many, and a stray CLOSE is none of those: the CLOSE of the API for
		// that number is already in, so the number is free again as soon as
		// the answer below is written - before the next frame is taken. It
		// holds the number for no longer than this one call.
		if f.Type != wire.TypeClose && len(c.m) >= wire.MaxTunnelChannels {
			return nil, tunnel.Opened{}, nothing, broke(ReasonChannels)
		}
		ch = c.hold(f.Channel)
		if f.Type != wire.TypeOpen {
			// A CLOSE here is a protocol error like every other stray frame -
			// and it is the CLOSE of the API for that number all the same. The
			// API answers no CLOSE with a second one, so counting it is what
			// gives the number back once the answer of this side is out;
			// without it the number would be lost for the life of the tunnel.
			ch.peer = f.Type == wire.TypeClose
			ch.claim(closeProtocol)
			return ch, tunnel.Opened{}, answering, nil
		}
		if c.h == nil {
			ch.claim(closeUnreachable)
			return ch, tunnel.Opened{}, answering, nil
		}
		o, inner, err := tunnel.ParseOpen(f.Payload)
		if err != nil {
			// An OPEN this hop cannot read is no channel: without its window
			// nothing could be sent on it either.
			ch.claim(closeProtocol)
			return ch, tunnel.Opened{}, answering, nil
		}
		ch.open = bytes.Clone(f.Payload)
		ch.out = uint64(o.Window)
		return ch, tunnel.Opened{Open: inner, Window: o.Window}, start, nil
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.closing {
		// After the CLOSE of this side nothing is answered. The CLOSE of the
		// API is the one frame that still counts.
		if f.Type == wire.TypeClose {
			ch.peer = true
			if ch.closed {
				c.drop(ch)
			}
		}
		return nil, tunnel.Opened{}, nothing, nil
	}
	switch f.Type {
	case wire.TypeOpen:
		// A number that is still open.
		ch.claimLocked(closeProtocol)
		return ch, tunnel.Opened{}, answering, nil
	case wire.TypeData:
		n := uint64(len(f.Payload))
		if n > ch.in || !ch.room() {
			ch.claimLocked(closeProtocol)
			return ch, tunnel.Opened{}, answering, nil
		}
		ch.in -= n
		ch.bytes += n
		ch.queue = append(ch.queue, Inbound{Kind: wire.TypeData, Data: bytes.Clone(f.Payload)})
		wake(ch.data)
	case wire.TypeEOF:
		if !ch.room() {
			ch.claimLocked(closeProtocol)
			return ch, tunnel.Opened{}, answering, nil
		}
		ch.queue = append(ch.queue, Inbound{Kind: wire.TypeEOF})
		wake(ch.data)
	case wire.TypeGrant:
		credit, handshake, err := wire.ParseGrant(f.Payload)
		// Noise message 2 travels in the first GRANT of the side that accepted
		// the channel, and that is this side: a handshake from the API is one
		// in a GRANT it does not belong in.
		if err != nil || len(handshake) != 0 || ch.out+uint64(credit) > wire.MaxWindow {
			ch.claimLocked(closeProtocol)
			return ch, tunnel.Opened{}, answering, nil
		}
		ch.out += uint64(credit)
		wake(ch.credit)
	case wire.TypeClose:
		// A CLOSE counts as received whatever stands in it: the API sends no
		// second one, so a number whose CLOSE this side threw away would never
		// come back. What does not parse is answered with the protocol error
		// all the same.
		//
		// This cancel wakes the handler, and the end of a handler writes the
		// CLOSE of this side too - but it has to come through mu to claim it,
		// and mu is held here until route is through. So the claim below is
		// the first one and E_PROTOCOL is what goes out.
		ch.peer = true
		ch.cancel()
		if _, err := wire.ParseClose(f.Payload); err != nil {
			ch.claimLocked(closeProtocol)
			return ch, tunnel.Opened{}, answering, nil
		}
		// The channel is over; the end of its handler writes the one CLOSE
		// this side owes for it.
	}
	return nil, tunnel.Opened{}, nothing, nil
}

// queueCost is what one waiting frame costs this side in bookkeeping: the size
// of an Inbound, measured in TestAChannelQueuesNoMoreThanItsCreditPaysFor.
const queueCost = 32

// room says whether one more frame may wait for the handler. A DATA frame with
// an empty payload costs no credit, and an EOF costs none either, so the credit
// in bytes alone does not bound what may pile up in front of a handler that
// does not read. The memory does: a waiting frame costs queueCost bytes
// whatever it carries, and this side keeps no more of them than the credit it
// granted would pay for at that price - so a byte of credit is at most two
// bytes of memory here, not thirty-three. On top of that come two frames: an
// empty DATA and an EOF are both of the contract before this side has granted
// anything at all, and those two are taken.
//
// It is a rule of this controller and not of the contract, which counts bytes
// and says nothing about the number of frames: a peer that splits its credit
// into pieces smaller than queueCost bytes is closed with E_PROTOCOL here and
// might not be somewhere else. With the smallest window that is 130 waiting
// frames.
func (ch *Channel) room() bool { return uint64(len(ch.queue)) < (ch.in+ch.bytes)/queueCost+2 }

// hold puts a number into the state of this tunnel. The caller holds mu.
func (c *channels) hold(num uint32) *Channel {
	ctx, cancel := context.WithCancel(c.life)
	ch := &Channel{c: c, num: num, ctx: ctx, cancel: cancel,
		data: make(chan struct{}, 1), credit: make(chan struct{}, 1)}
	c.m[num] = ch
	return ch
}

// drop makes a number free again. The caller holds mu, and both closes are in.
func (c *channels) drop(ch *Channel) {
	if c.m[ch.num] == ch {
		delete(c.m, ch.num)
	}
}

// run is the goroutine of one channel: the handler, and after it the one CLOSE
// of this side.
//
// take starts it with go, so this is a goroutine of its own and a panic in it
// is final -- nothing above a goroutine can catch one. Until the catch in
// handle below, ONE programming error in ONE handler of ONE channel ended the
// whole ngc-controller: every other channel of this tunnel, every other
// tunnel, and with them the shells, the terminals and the service calls of
// every user of this installation. That is V1.2 point 38 on this side of the
// link; the API side was built first (package guard), and this controller is
// where the app channels, the pseudo terminals and the container processes
// really are.
//
// The catch is in TWO places for the reason the API gives: handle holds the
// work, so the channel still gets its CLOSE and its line, and this one holds
// the little that comes after it, so a defect in the closing cannot do what
// the work can no longer do.
func (c *channels) run(ch *Channel, o tunnel.Opened) {
	defer func() { _ = guard.HitTo(c.log, recover(), "link.close", ch.num) }()
	c.close(ch, c.handle(ch, o))
}

// handle is the handler of one channel with the catch of that channel around
// it. It answers the payload the one CLOSE of this side carries.
//
// A panic is a bug of ours and never a refusal, so it ends the channel the way
// a handler that answered an error ends it: closeInternal, which is E_INTERNAL
// over the link. It is deliberately NOT a quiet death -- a channel that
// vanishes without a word leaves the API, and behind it the browser, waiting
// for an answer that is never coming, and that is a false answer of its own
// (V1.2 point 50).
func (c *channels) handle(ch *Channel, o tunnel.Opened) (p []byte) {
	defer func() {
		if guard.HitTo(c.log, recover(), "link.run", ch.num) {
			p = closeInternal
		}
	}()
	if err := c.h.Open(ch.ctx, ch, o); err != nil {
		// The error of a handler ends its channel and not the link. It goes
		// into the log of this controller as it is: whoever writes a handler
		// decides what stands in it.
		c.log.Warn("channel failed", "channel", ch.num, "err", err)
		return closeInternal
	}
	return closeOK
}

// claimLocked takes the one CLOSE this side owes for the channel and fixes
// what it will say. Only the first claim counts, and it counts from the moment
// it is made under mu - that is what keeps an answer this hop decided on from
// being overwritten by a handler that is ending at the same moment. The caller
// holds mu.
func (ch *Channel) claimLocked(payload []byte) {
	if ch.closing {
		return
	}
	ch.closing = true
	ch.answer = payload
}

// claim is claimLocked for a caller who does not hold mu.
func (ch *Channel) claim(payload []byte) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.claimLocked(payload)
}

// close claims the one CLOSE of this side for ch with payload and writes it.
// It may be called more than once and from anywhere: the first claim says what
// the CLOSE reads, and whoever gets to the write writes that one - a later
// caller writes nothing of its own.
func (c *channels) close(ch *Channel, payload []byte) error {
	ch.claim(payload)
	return c.finish(ch)
}

// finish writes the CLOSE that was claimed for ch, and frees the number once
// the CLOSE of the API is in as well. It writes nothing if there is no claim
// yet or if the write of one is already taken, so every claim goes out exactly
// once however often this is called.
func (c *channels) finish(ch *Channel) error {
	ch.mu.Lock()
	// Taking the payload out is taking the write: whoever finds it there is
	// the one who writes, and everybody after him finds nothing.
	payload := ch.answer
	ch.answer = nil
	ch.mu.Unlock()
	if payload == nil {
		return nil
	}
	// Everything of this channel is over before the CLOSE goes out: whoever
	// waits for credit or for something to read comes back with ErrClosed
	// instead of writing behind it, and the write lock of the channel is taken
	// for the one frame that is still owed. So the CLOSE is the last frame of
	// this side on this number, whatever was on its way when it was claimed -
	// what that costs is the wait for a write that is standing, which is
	// bounded by WriteTimeout like every other.
	ch.cancel()
	ch.wmu.Lock()
	defer ch.wmu.Unlock()
	err := c.send(wire.Frame{Type: wire.TypeClose, Channel: ch.num, Payload: payload})
	// The number stays taken until the CLOSE is really out. Freeing it earlier
	// would let a new channel of the API take the number while the CLOSE of
	// the old one is still on its way.
	ch.mu.Lock()
	ch.closed = true
	peer := ch.peer
	ch.mu.Unlock()
	if peer {
		c.mu.Lock()
		c.drop(ch)
		c.mu.Unlock()
	}
	return err
}
