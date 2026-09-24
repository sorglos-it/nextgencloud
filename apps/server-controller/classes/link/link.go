// Package link dials the tunnel of this controller to an API and keeps it
// standing: TLS checked against the pin of that API, Noise_KK inside it, and
// after the handshake one frame per message (packages tunnel, wire, noisep).
//
// Everything that comes in here is hostile, an API of the trust list included:
// the size of a message is bounded before it is read, every payload goes
// through the parser of its contract, and no error and no log line of this
// package quotes anything that came in - this is a program that runs as root.
// A handler is the one way past that: Run passes an error of it on as it is
// and Keep writes it to the log, so a handler that puts something from the
// tunnel into its error puts it into the log of the controller.
//
// A text message, a message that does not decrypt, a frame that does not
// decode and a control message the API may not send end the link at once and
// without an answer - there is nothing to say to a peer that is not the one
// the handshake proved.
package link

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Times of one tunnel.
const (
	// DialTimeout bounds the way in: TLS, the WebSocket handshake, both Noise
	// messages and the two hellos. An API that answers slowly must not hold a
	// controller that has other work. It is not the bound on Dial as a whole:
	// an API that breaks the contract on the way in is told so with a close
	// message, and that message is written and answered under the deadlines of
	// the library (Close), so such a dial may take up to about ten seconds
	// longer - about five against an API that has simply gone quiet, because
	// the waiting for the answering message only begins once ours got out.
	DialTimeout = 30 * time.Second
	// WriteTimeout bounds one message on the way out: a peer that stops
	// reading holds the writer at the front this long and no longer. What is
	// bounded here is the time and not the number - the writers behind it
	// wait on the lock in the meantime, so a peer that went quiet does cost
	// this side one waiting goroutine per writer until the tunnel is over.
	//
	// Half a minute is the bound and not a time a tunnel lives through. From
	// five seconds on, a ping that falls into such a write no longer gets its
	// own frame out and is skipped instead (ping). And when the deadline
	// itself runs out, the library closes the socket hard: it hangs a watcher
	// on the context of every write (vendor .../conn.go:170-181). So this is
	// at the same time the longest a peer may refuse to read before this
	// tunnel is over.
	WriteTimeout = 30 * time.Second
	// minPing is the shortest ping interval this package takes. Only a test
	// sets one at all, and nothing from outside reaches it - but a ticker of a
	// nanosecond on a program that runs as root is a storm and not an
	// interval, so it is refused where every other value of Config is.
	minPing = 10 * time.Millisecond
)

// The errors this package answers with. None of them quotes anything that came
// in, and none of them says which step failed to whoever is at the other end:
// what is on the tunnel is either of this contract or the link is over.
var (
	ErrConfig = errors.New("link: this controller cannot dial or send with these values")
	ErrAPI    = errors.New("link: the API cannot be reached")
	ErrLink   = errors.New("link: the API broke the contract of the tunnel")
)

// Handler answers the channels an API opens. On a tunnel only the API opens
// channels, so every frame on a channel of its own goes here, one at a time
// and in the order it arrived, from the goroutine that runs the tunnel. It
// writes with Tunnel.Send, and an error of it ends the link.
//
// Two things hold for every call of Frame, and whoever writes a handler may
// count on both:
//
//   - f.Payload is good for the length of the call and no longer: the link
//     may use that memory again afterwards. Whoever keeps anything of it
//     copies it first.
//   - The call holds the reading of the whole tunnel, and the tunnel lives on
//     that reading: the pong of the API is picked up there too, and it waits
//     behind every frame that came in before it. What decides is therefore
//     how long the reading is held between a ping and its deadline, not how
//     long one call takes: a queue of short calls costs the tunnel exactly as
//     one long call does. Held over two ping intervals (2 x
//     tunnel.PingInterval, a minute) the tunnel is lost unless this side is
//     writing at the same time - the ping falls at the latest one interval
//     into the hold and then waits one interval for its pong; the link counts
//     as dead although the API is healthy, and Run comes back with an error.
//     The one thing that puts that off: a ping that gave up while a write of
//     this side was running is skipped and the next interval asks again
//     (ping), so a handler that holds the reading and writes at the same time
//     is measured by the intervals in which it did not write. No length of a
//     single call is safe on its own, because the holds add up. What a
//     handler must keep to is therefore not a length but this: give the
//     reading back at once. Whatever cannot be answered on the spot belongs
//     in a goroutine of its own, which answers with Tunnel.Send, and Frame
//     returns without waiting for it. Both ways of losing the tunnel are
//     measured, see TestAHandlerThatHoldsTheReadingCostsTheTunnel.
type Handler interface {
	Frame(t *Tunnel, f wire.Frame) error
}

// Config is what a tunnel needs. API and Noise come from controller.json;
// Hello is asked afresh for every dial, because the version, the roles and the
// newest trust list version of this server change while it runs.
type Config struct {
	API   config.APIRef
	Noise noise.DHKey
	Hello func() tunnel.Hello
	// Handler answers the channels the API opens. Without one nothing here
	// serves channels, and every channel is refused as not reachable.
	Handler Handler
	Log     *slog.Logger // nil: slog.Default()
	Now     func() int64 // nil: time.Now().Unix()
	// Sleep waits between two attempts of Keep. Only a test sets it, to count
	// the pauses instead of waiting them out.
	Sleep func(time.Duration)
	// Ping is how long the tunnel waits before it asks the API again whether
	// it is still there, and how long it then waits for the answer. Only a
	// test sets it, to sit out a ping in a fraction of a second; without it
	// the interval of the contract holds, and anything under minPing is
	// refused (check). It is a value of this package, not one of the
	// contract: tunnel.PingInterval stays what goes on the wire.
	Ping time.Duration
}

// check refuses values this controller cannot dial with, before anything goes
// out: the address of the API by the grammar of the contract, both keys and
// the pin by their length, and a hello there has to be.
func (c Config) check() error {
	if !trust.ValidTunnelURL(c.API.URL) || len(c.API.Key) != 32 || len(c.API.TLS) != sha256.Size ||
		len(c.Noise.Public) != 32 || len(c.Noise.Private) != 32 || c.Hello == nil {
		return ErrConfig
	}
	// Zero is the interval of the contract; anything under the floor is a
	// ticker storm on a program that runs as root.
	if c.Ping != 0 && c.Ping < minPing {
		return ErrConfig
	}
	return nil
}

func (c Config) logger() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

func (c Config) now() int64 {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().Unix()
}

// every is the time between two pings and the time the tunnel then waits for
// the pong.
func (c Config) every() time.Duration {
	if c.Ping > 0 {
		return c.Ping
	}
	return tunnel.PingInterval * time.Second
}

// Tunnel is one link to one API.
type Tunnel struct {
	cfg  Config
	c    *websocket.Conn
	tr   *noisep.Transport
	peer tunnel.Hello
	// life ends with the tunnel and with nothing else: it is what every read
	// and every write of this link waits on, so that closing it brings every
	// goroutine of it back. The context of the dial bounds the way in alone.
	life context.Context
	stop context.CancelFunc
	mu   sync.Mutex // one writer at a time
	once sync.Once
	ran  atomic.Bool // Run has begun
	dead atomic.Bool // the ping found the link gone, and not through this side
	// closing says that this side is ending the tunnel, Close as fail. It is
	// set before the close message goes out, so whoever afterwards finds the
	// link broken - the reader in Run, the ping - knows it was this side that
	// broke it and not the API. The life of the tunnel cannot say that: it
	// ends after the close message, and until then an orderly end looks like
	// a link that fell apart.
	closing atomic.Bool
	// writing counts the writes of this side that are running: it goes up
	// just before t.c.Write and down when write returns. That is less than
	// "the bytes of this side that stand in the socket", and the difference
	// matters: once the kernel has taken the bytes into the send buffer,
	// Write comes back and this falls to 0 while those bytes are still on
	// their way out.
	//
	// That gap was measured, in the tests of this package and by accident:
	// with about two and a half megabytes of ours lying in the send buffer
	// and writing at 0, the link was called dead inside nineteen seconds
	// although the API answered every ping it saw. Which of the two it was -
	// a ping frame queued behind that backlog, or a ping that could not put
	// its own frame into a full buffer - was not told apart, and for this
	// count it makes no difference: neither is a write of ours running. So
	// what is caught here is a write that is standing, not a socket that is
	// full.
	//
	// Only write raises it, around t.c.Write and nothing else, and mu above
	// keeps it at one - it is a number and not a flag because a number cannot
	// be set twice by mistake. What it is for: the ping frame goes through
	// the same lock of the library as every message of ours, so a ping that
	// gave up while one of them was in t.c.Write gave up over this side and
	// says nothing about the API (ping).
	//
	// One holder of that lock this count does not see: the read loop of the
	// library answers a ping of the API itself, out of the goroutine of Run,
	// with a pong over the same lock and under a deadline of five seconds
	// (vendor .../read.go:303, :323, write.go:277). Two things can come of
	// that, and only the first one a count could ever step in front of:
	//
	//   - A ping of ours loses the lock to that pong and finds writing == 0,
	//     so the link is called dead although the API is healthy. Narrow: it
	//     takes a ping of the API to fall into those seconds.
	//   - A write of ours holds the lock longer than those five seconds - it
	//     may hold it up to WriteTimeout - and then the read loop does not
	//     get the lock for the pong at all (conn.go:286-292). handleControl
	//     gives that back as an error, and the error is the end of the
	//     reading: read.go:202 and :208 -> reader (:382-384) -> Conn.Read ->
	//     message, where every error of Conn.Read becomes ErrAPI -> Run,
	//     which hands it out because this side was not the one closing. The
	//     tunnel is gone at once. No bookkeeping helps here, because it is
	//     not our ping that fails.
	//
	// What sets both off is a ping of the API. Nothing in this tree sends
	// one - t.c.Ping in ping below is the only ping in the whole program -
	// and the contract names an interval (tunnel.PingInterval) without saying
	// who asks, so an API that pings is allowed and plausible, but that it
	// happens is not shown anywhere. Closing the first case would need a
	// second count this package has no hook for: the library offers a
	// callback before the pong (OnPingReceived) and none after it. The second
	// case cannot be closed here at all.
	writing atomic.Int64
	// since is taken once the Noise session stands, so how long a tunnel
	// stood is measured from just before the two hellos.
	since int64
}

// Dial opens the tunnel to the API of cfg: TLS against its pin, the WebSocket
// under the subprotocol of the contract, Noise_KK as the initiator, then this
// controller's hello and the API's. It comes back with a tunnel whose peer has
// said hello; not a single channel is taken before that (Run).
func Dial(ctx context.Context, cfg Config) (*Tunnel, error) {
	if err := cfg.check(); err != nil {
		return nil, err
	}
	name, ok := host(cfg.API.URL)
	if !ok {
		return nil, ErrConfig
	}
	dial, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	// The pin stands in for the chain and the names, so the name is only what
	// SNI carries to a server with several certificates.
	tlsCfg := tunnel.PinnedTLS(cfg.API.TLS)
	tlsCfg.ServerName = name
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
		// A redirect would carry the handshake somewhere else, perhaps in
		// clear text. The answer is taken for what it is, and only the 101 of
		// the address in the configuration opens a tunnel.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// The connection of a tunnel is taken out of the client at the handshake;
	// what a failed attempt left lying around is closed here.
	defer client.CloseIdleConnections()
	c, _, err := websocket.Dial(dial, cfg.API.URL+"?key="+url.QueryEscape(tunnel.Fingerprint(cfg.Noise.Public)),
		&websocket.DialOptions{HTTPClient: client, Subprotocols: []string{tunnel.Subprotocol}})
	if err != nil {
		return nil, ErrAPI
	}
	// A server that does not speak the subprotocol of the tunnel is not the
	// other end of one; the library lets an answer without it pass.
	if c.Subprotocol() != tunnel.Subprotocol {
		c.Close(websocket.StatusProtocolError, "")
		return nil, ErrLink
	}
	// Before the first read: a message of this contract is one frame with the
	// tag of its Noise message and not a byte more.
	c.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	tr, err := handshake(dial, c, cfg)
	if err != nil {
		return nil, err
	}
	t := &Tunnel{cfg: cfg, c: c, tr: tr, since: cfg.now()}
	t.life, t.stop = context.WithCancel(context.WithoutCancel(ctx))
	if err := t.control(dial, wire.CtlHello, cfg.Hello()); err != nil {
		t.Close()
		return nil, err
	}
	peer, err := t.readHello(dial)
	if err != nil {
		t.Close()
		return nil, err
	}
	t.peer = peer
	return t, nil
}

// handshake carries out Noise_KK as the initiator: message 1 without a
// payload, message 2 read and taken only at exactly wire.HandshakeSize bytes.
// It closes the connection on every failure.
func handshake(ctx context.Context, c *websocket.Conn, cfg Config) (*noisep.Transport, error) {
	hs, err := noisep.Handshake(true, []byte(noisep.PrologueTunnel), cfg.Noise, cfg.API.Key, rand.Reader)
	if err != nil {
		c.Close(websocket.StatusInternalError, "")
		return nil, ErrConfig
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil || len(msg1) != wire.HandshakeSize {
		c.Close(websocket.StatusInternalError, "")
		return nil, ErrConfig
	}
	if err := c.Write(ctx, websocket.MessageBinary, msg1); err != nil {
		c.CloseNow()
		return nil, ErrAPI
	}
	typ, msg2, err := c.Read(ctx)
	if err != nil {
		c.CloseNow()
		return nil, ErrAPI
	}
	if typ != websocket.MessageBinary || len(msg2) != wire.HandshakeSize {
		c.Close(websocket.StatusProtocolError, "")
		return nil, ErrLink
	}
	// Messages 1 and 2 carry an empty payload in every use of Noise in NGC, so
	// a payload here is a protocol error and nothing to read.
	payload, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil || len(payload) != 0 || cs1 == nil || cs2 == nil {
		c.Close(websocket.StatusProtocolError, "")
		return nil, ErrLink
	}
	return noisep.NewTransport(true, cs1, cs2), nil
}

// readHello reads the hello of the API, the first transport message it sends.
// The contract of the sender is taken as it is: every part also understands
// the one before its own, and what a version can be asked for is the question
// of whoever uses the tunnel, not of the link itself.
func (t *Tunnel) readHello(ctx context.Context) (tunnel.Hello, error) {
	plain, err := t.message(ctx)
	if err != nil {
		return tunnel.Hello{}, err
	}
	f, ctl, err := takeMessage(plain)
	if err != nil {
		t.fail()
		return tunnel.Hello{}, err
	}
	if f.Channel != wire.ControlChannel || ctl.T != wire.CtlHello {
		t.fail()
		return tunnel.Hello{}, ErrLink
	}
	h, err := tunnel.ParseHello(ctl.B)
	if err != nil {
		t.fail()
		return tunnel.Hello{}, ErrLink
	}
	return h, nil
}

// Peer is the hello the API sent. It is read before the first channel and
// stands for the whole life of the tunnel; the roles are copied, so no caller
// holds the list of another.
func (t *Tunnel) Peer() tunnel.Hello {
	h := t.peer
	h.Roles = slices.Clone(h.Roles)
	return h
}

// Run reads the tunnel until it ends. Every binary message is exactly one
// Noise transport message whose plaintext is exactly one frame; channel 0
// carries the control messages the API may send, every other channel goes to
// the handler. It comes back with nil when the API said goodbye or the tunnel
// was closed, and with an error when the API broke the contract or the link
// fell apart.
func (t *Tunnel) Run(ctx context.Context) error {
	// One reader at a time: two would take the messages of one Noise session
	// apart between them. This stands before the close below, so a second
	// caller does not end the tunnel of the first.
	if !t.ran.CompareAndSwap(false, true) {
		return ErrConfig
	}
	defer t.Close()
	// The context of the caller ends the tunnel; from then on the reading
	// below comes back at once.
	go func() {
		select {
		case <-ctx.Done():
			t.Close()
		case <-t.life.Done():
		}
	}()
	go t.ping()
	for {
		plain, err := t.message(t.life)
		if err != nil {
			// A link this side ended because the other one broke the contract
			// says so, whatever the socket did afterwards, and a link the
			// ping found gone is gone and not closed: whoever keeps the
			// tunnel should see both and dial again. Everything else is an
			// orderly end exactly when this side closed the tunnel - and that
			// is closing and not the life of it, which ends only after the
			// close message has gone out.
			if errors.Is(err, ErrLink) || t.dead.Load() || !t.closing.Load() {
				return err
			}
			return nil
		}
		f, ctl, err := takeMessage(plain)
		if err != nil {
			t.fail()
			return err
		}
		if f.Channel == wire.ControlChannel {
			// The hello comes once, before Run; of the two types the API may
			// send on a tunnel only the goodbye is left here.
			if ctl.T != wire.CtlBye {
				t.fail()
				return ErrLink
			}
			return nil
		}
		if t.cfg.Handler == nil {
			// Nothing here serves channels, so the API is told so for this
			// one and the tunnel stays: its status and its hello are worth
			// more than the channel.
			if err := t.Send(wire.Frame{Type: wire.TypeClose, Channel: f.Channel, Payload: closeUnreachable}); err != nil {
				return err
			}
			continue
		}
		if err := t.cfg.Handler.Frame(t, f); err != nil {
			t.fail()
			return err
		}
	}
}

// closeUnreachable is the answer to a channel nothing here serves: the CLOSE
// payload of the contract with the German text of its code. A fixed value
// marshals or this package sends nothing at all - Frame.Encode refuses an
// empty CLOSE payload.
var closeUnreachable, _ = json.Marshal(wire.Error(wire.CodeUnreachable))

// message reads one transport message and gives back its plaintext. A text
// message and a message that does not decrypt end the link at once and without
// an answer.
func (t *Tunnel) message(ctx context.Context) ([]byte, error) {
	typ, data, err := t.c.Read(ctx)
	if err != nil {
		return nil, ErrAPI
	}
	if typ != websocket.MessageBinary {
		t.fail()
		return nil, ErrLink
	}
	plain, err := t.tr.Open(data)
	if err != nil {
		t.fail()
		return nil, ErrLink
	}
	return plain, nil
}

// takeMessage checks one decrypted message: it is exactly one frame, and a
// frame on channel 0 is exactly one control message the API may send on a
// tunnel. Everything else is a reason to end the link. The payload of the
// frame points into plain.
func takeMessage(plain []byte) (wire.Frame, wire.Control, error) {
	f, err := wire.Decode(plain)
	if err != nil {
		return wire.Frame{}, wire.Control{}, ErrLink
	}
	if f.Channel != wire.ControlChannel {
		return f, wire.Control{}, nil
	}
	if f.Type != wire.TypeData {
		return wire.Frame{}, wire.Control{}, ErrLink
	}
	ctl, err := wire.ParseControl(f.Payload)
	if err != nil || !wire.ControlAllowed(wire.LinkTunnel, true, ctl.T) {
		return wire.Frame{}, wire.Control{}, ErrLink
	}
	return f, ctl, nil
}

// Send writes one frame of a channel. The writers are serialized: status
// messages, answers and channels write at the same time, and the counter of
// the Noise session takes no concurrency - two messages sealed out of order
// would not decrypt at the other end.
//
// Channel 0 is no channel. What goes there goes through Control, which sends
// only what a controller may send on a tunnel: a handler that wrote its own
// control message here would have the API end the link over it.
//
// A write that comes to a stand in the socket - a peer that has stopped
// reading - takes the ping of this tunnel the lock it needs for its own
// frame. Such a ping is skipped and asked again one interval later (ping),
// so it does not cost the tunnel its life - but it does cost it its
// heartbeat for as long as the write stands: for that while nothing here
// would notice a peer that went quiet, and a write that stands for the whole
// of WriteTimeout leaves the tunnel that long unwatched. WriteTimeout is
// also the bound of the write itself.
func (t *Tunnel) Send(f wire.Frame) error {
	if f.Channel == wire.ControlChannel {
		return ErrConfig
	}
	ctx, cancel := context.WithTimeout(t.life, WriteTimeout)
	defer cancel()
	return t.write(ctx, f)
}

// write seals one frame and puts it on the link.
func (t *Tunnel) write(ctx context.Context, f wire.Frame) error {
	b, err := f.Encode()
	if err != nil {
		return ErrConfig
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	msg, err := t.tr.Seal(b)
	if err != nil {
		return ErrConfig
	}
	// Raised around the write and nothing else: what the ping asks is whether
	// a write of ours was running and holding the lock of the library, not
	// whether a message was on its way through this package - and not, for
	// that matter, whether bytes of ours are still in the send buffer of the
	// socket, which this cannot say (writing). Lowered by defer, not by the
	// next statement: a panic in the library would otherwise leave the count
	// up for good, and a count that is up for good silences the ping of this
	// tunnel for ever - WriteTimeout does not catch that either, because no
	// write is running any more. It runs before mu is given up, so nobody
	// sees a free lock and a write of ours still counted as running.
	t.writing.Add(1)
	defer t.writing.Add(-1)
	err = t.c.Write(ctx, websocket.MessageBinary, msg)
	if err != nil {
		return ErrAPI
	}
	return nil
}

// Control sends one control message on channel 0. Only what a controller may
// send on a tunnel goes out (wire.ControlAllowed): a type the API would have
// to end the link over is refused here.
func (t *Tunnel) Control(typ string, body any) error {
	if !wire.ControlAllowed(wire.LinkTunnel, false, typ) {
		return ErrConfig
	}
	ctx, cancel := context.WithTimeout(t.life, WriteTimeout)
	defer cancel()
	return t.control(ctx, typ, body)
}

// control sends one control message within ctx, the hello of the dial
// included.
func (t *Tunnel) control(ctx context.Context, typ string, body any) error {
	var raw json.RawMessage
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return ErrConfig
		}
		// A body that writes itself as null is none. A typed nil pointer is
		// not nil once it sits in an interface, so the value decides and not
		// the interface: "b":null is no message of this contract, and the API
		// would end the tunnel over it (wire.ParseControl).
		if !bytes.Equal(b, []byte("null")) {
			raw = b
		}
	}
	p, err := json.Marshal(wire.Control{T: typ, B: raw})
	if err != nil {
		return ErrConfig
	}
	return t.write(ctx, wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: p})
}

// ping asks the API every tunnel.PingInterval seconds whether it is still
// there and waits one interval for the answer. It is the only thing that
// notices a peer which went quiet without the socket saying so, and a tunnel
// nobody notices is gone would take every channel on it with it.
//
// Two ways a ping can fail are of this side's own making, and neither may be
// laid at the API's door:
//
//   - This side is closing the tunnel, so the socket was taken away under the
//     ping (closing).
//   - A write of this side is running and holding the lock (writing). The
//     ping frame goes through the same lock of the library as every message
//     (writeFrameMu, vendor .../write.go:289), and the library gives that
//     lock up after five seconds for a control frame
//     (write.go:276-277) while a write of ours may hold it up to
//     WriteTimeout. The ping then never got its frame out - the API does not
//     even see it - so there is nothing it could answer.
//
// The first ends the ping, the second skips it: the tunnel is none the worse
// for a ping it never got to ask, and the next interval asks again. What is left
// makes the link dead: a ping that did get out and got no pong within the
// interval, or a socket that broke under it. Both mean the same thing, and
// that is what dead says - this tunnel can no longer be vouched for, whatever
// the socket still claims.
//
// Two things this does not cover, both set off by a ping of the API, which
// nothing in this tree sends today (writing has them in full):
//
//   - The read loop of the library answers such a ping over the same lock.
//     A ping of ours that loses the lock to it finds writing == 0 and makes
//     the link dead although the API is healthy.
//   - If a write of ours holds that lock longer than the five seconds the
//     pong has, the pong is not written at all, and that error ends the
//     reading and with it Run. The tunnel is gone at once, and skipping a
//     ping would not have helped: it is not our ping that fails there.
//
// What this count does say is bounded too: it is up while t.c.Write runs and
// not while bytes of ours stand in the send buffer, so a ping whose frame
// went in behind a backlog can still be called a peer that went quiet
// (writing).
func (t *Tunnel) ping() {
	tick := time.NewTicker(t.cfg.every())
	defer tick.Stop()
	for {
		select {
		case <-t.life.Done():
			return
		case <-tick.C:
		}
		ctx, cancel := context.WithTimeout(t.life, t.cfg.every())
		err := t.c.Ping(ctx)
		// Read before anything else runs: what counts is whether a write of
		// ours was running when the ping gave up, not whether one ran at
		// some point while it waited.
		mine := t.writing.Load() > 0
		cancel()
		if err == nil {
			continue
		}
		if t.closing.Load() {
			return
		}
		if mine {
			continue
		}
		t.dead.Store(true)
		t.Close()
		return
	}
}

// fail ends the link at once and without an answer: what came in was not from
// the other end of the handshake, or not of this contract. The API is told
// which of the two by the status of the close message and by nothing else, so
// that message goes out before anything here is torn down (Close).
//
// One thing can take that status away: a Close of the caller at the same
// moment. Whichever of the two reaches the library first writes its own
// message, and the other one then only waits for it - so an API that broke
// the contract while its controller was shutting down can see the normal
// closure of that shutdown instead. Nothing here sends anything afterwards to
// put it right, and nothing may: the peer is not the one the handshake
// proved.
func (t *Tunnel) fail() {
	t.closing.Store(true)
	t.c.Close(websocket.StatusProtocolError, "")
	t.stop()
}

// Close ends the tunnel. It may be called more than once and from any
// goroutine; whoever runs the tunnel comes back out of Run.
//
// It blocks, against a peer that has gone quiet for up to about ten seconds:
// the library writes the close message under a deadline of five seconds
// (vendor .../close.go:185) and waits five more for the answering one
// (close.go:199). The second wait only begins once the first one worked
// (close.go:157-161), so a peer that neither reads nor answers costs about
// five seconds; the ten are the peer that takes almost the whole first
// deadline to read the message and then never answers. Both deadlines are the
// library's own and run on context.Background(), so no context of the caller
// shortens them - neither the one given to Dial or Run nor one of whoever
// calls Close.
//
// What comes after no longer hangs on the peer: the socket is torn down, and
// the goroutines of the library are waited for under a further bound of
// fifteen seconds (close.go:231). One case is the exception: fail closes
// outside the once above, so Close and fail can meet, and whichever of the
// two reaches the library second finds it already closing and then does
// nothing but sit out those fifteen seconds (close.go:102-107) - what it
// waits for there is the other one, which is still standing in front of the
// peer.
//
// It also gives an error back, and that error has three sources: the close
// message did not get out or no answer came, the socket did not tear down
// cleanly, or the goroutines did not come back within the fifteen seconds.
// The tunnel is over in each of them, and every caller in this package
// therefore drops it.
func (t *Tunnel) Close() error {
	var err error
	t.once.Do(func() {
		// The close message goes out first and the life of the tunnel ends
		// after it. The other way round the message is lost: the library
		// hangs a watcher on the context of every read and write, and that
		// watcher closes the socket hard the moment the context ends - a
		// close message written afterwards has nothing left to go through.
		// So the order here is what lets the API tell a controller that shut
		// down from one that fell over, and closing is what lets Run tell the
		// same thing apart on this side.
		t.closing.Store(true)
		err = t.c.Close(websocket.StatusNormalClosure, "")
		t.stop()
	})
	return err
}

// Keep dials again and again until ctx ends. The pause doubles from
// tunnel.ReconnectMin to tunnel.ReconnectMax and carries up to 20 % jitter, so
// that seven controllers do not all return in the same second.
func Keep(ctx context.Context, cfg Config) {
	log := cfg.logger()
	sleep := cfg.Sleep
	if sleep == nil {
		// Waiting ends with the context as well: a controller that is being
		// stopped does not sit out a minute first.
		sleep = func(d time.Duration) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
			}
		}
	}
	pause := tunnel.ReconnectMin * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		t, err := Dial(ctx, cfg)
		if err == nil {
			err = t.Run(ctx)
			t.Close()
			stood := cfg.now() - t.since
			log.Info("tunnel ended", "api", cfg.API.URL, "seconds", stood)
			// Only a tunnel that really stood begins again at the shortest
			// pause: what ended that one was not an API that is away. A dial
			// that got through is no proof of it - an API that plays TLS,
			// Noise and hello and then drops the tunnel at once would hold
			// this controller at about a second per attempt for as long as it
			// likes, and the growing pause is the only bound there is on that
			// way.
			//
			// What the threshold buys is a rate and nothing more: an API that
			// wants this controller back every second has to keep the tunnel
			// standing a ping interval of real time first, so it gets one
			// attempt per interval instead of one per second. It is no proof
			// that a ping was answered - the ping of a tunnel only starts with
			// Run and first goes out a whole interval later, so a tunnel can
			// pass this line before a single pong has come back. Reading since
			// after the hellos would not make that true either; the honest
			// word for what stands here is the rate.
			if stood >= tunnel.PingInterval {
				pause = tunnel.ReconnectMin * time.Second
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warn("tunnel failed", "api", cfg.API.URL, "err", err)
		}
		sleep(jitter(pause))
		pause = grow(pause)
	}
}

// grow doubles the pause up to tunnel.ReconnectMax. An API that is away for an
// hour is asked about once a minute and not once a second, and a controller
// that waits a minute is back within about one when it returns. About: jitter
// puts up to a fifth on top afterwards, so the longest wait is 72 seconds -
// tunnel.ReconnectMax is the step this stops at, not a hard bound on the
// waiting, and nothing hangs on 60 seconds as one.
func grow(d time.Duration) time.Duration {
	if d *= 2; d > tunnel.ReconnectMax*time.Second {
		return tunnel.ReconnectMax * time.Second
	}
	return d
}

// jitter adds up to a fifth of d. Nothing secret hangs on it, so the fast
// generator of the standard library does: what it is good for is that the
// pause of every controller is its own.
func jitter(d time.Duration) time.Duration {
	return d + time.Duration(mrand.Int64N(int64(d)/5+1))
}

// host names the API in the TLS handshake. The address went through
// trust.ValidTunnelURL in check already; it is asked again here so that this
// stays true of every way into this function.
func host(u string) (string, bool) {
	if !trust.ValidTunnelURL(u) {
		return "", false
	}
	h, _, err := net.SplitHostPort(strings.TrimSuffix(strings.TrimPrefix(u, "wss://"), "/tunnel"))
	if err != nil {
		return "", false
	}
	return h, true
}
