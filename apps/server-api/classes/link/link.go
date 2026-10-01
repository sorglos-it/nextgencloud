// Package link carries the two links of the API: the session to a browser and
// the tunnel to a controller. Both are a WebSocket with Noise_KK inside: the
// first binary message is Noise message 1 of the other side, the second is the
// API's message 2, both without a payload and exactly wire.HandshakeSize bytes.
// After that every binary message is one Noise transport message whose
// plaintext is exactly one frame. A text message, a frame that does not decode
// or a failed decryption ends the link (package wire).
package link

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrLink reports a message that breaks the rules of the link. Accept answers
// it for a Noise message 2 it cannot have the library produce as well, which
// takes a fault inside that library: in Noise_KK that message always ends the
// handshake and hands over both cipher states. A message 2 that cannot be sent
// is a different thing and gives the real error back.
var ErrLink = errors.New("link: malformed message")

// ErrOrigins reports a list of allowed origins that Accept will not work with.
// It names the entry by its number, never the value, and it stands before the
// upgrade: a misconfigured server does not become a WebSocket at all.
var ErrOrigins = errors.New("link: unusable origin")

// randReader is the source of the ephemeral Noise keys.
var randReader = rand.Reader

// The two deadlines of a link. Both are policy, not protocol: no byte on the
// line depends on them, so a change here is no change to a contract.
const (
	// HandshakeTimeout bounds the whole Noise handshake, from the connection
	// being ours to the finished transport. Nothing else bounds it: Hijack
	// takes the server's own deadlines off the connection (net/http), and the
	// request context only ends when the handler returns, so a client that
	// upgrades and then says nothing would hold a goroutine and a file handle
	// for good, before any login. The price: a human on a very bad line can
	// be cut off after ten seconds.
	//
	// Measured is the half that reads message 1. For the half that writes
	// message 2 the deadline is in the code and unmeasured: forty-eight bytes
	// block no socket, so the only way to make that write run out of time is to
	// let the clock expire between the read and the write — a case that races a
	// timer, green on one machine and red on the next. A named gap is worth more
	// than a case that teaches the next reader to explain away a red run.
	HandshakeTimeout = 10 * time.Second

	// CloseWait bounds how long a refused peer may take to answer the close
	// frame. A peer that just broke the rules of the link has earned less
	// patience than one that ends a link in order, which Close still gives the
	// library's own wait: measured, a refusal held the reader five seconds
	// before this, so whoever opens many refused links held a reader for each
	// of them that long. Five is what that wait costs in practice, not its
	// ceiling — closeBounded says what the ceiling is.
	CloseWait = 1 * time.Second

	// FirstFrameTimeout bounds the window from the finished Noise connection to
	// the first frame the link hands up. It closes the hole the handshake
	// deadline leaves one step later: whoever carries Noise message 1 can
	// replay it (see shared/noisep), so a connection can be had without a key —
	// measured, the same recorded bytes got a link twice — and after that only
	// this window stands between a replay and a goroutine plus a file handle
	// held for good. An honest client sends its first frame at once; whoever
	// says nothing after the handshake cannot form frames at all. Nothing
	// bounds the quiet time after that first frame: a session that is logged in
	// may sit still for as long as the browser keeps the socket open.
	//
	// It is wall clock from Accept, so it bounds the caller as well as the peer:
	// measured with a window of 100 ms, a caller that was busy for 300 ms lost a
	// frame that was already on the line, and the link with it. Whoever wires a
	// link up therefore starts reading at once — the window is no grace period
	// for the handler.
	FirstFrameTimeout = 30 * time.Second
)

// Options describe one link.
type Options struct {
	Subprotocol string      // session.Subprotocol or tunnel.Subprotocol
	Prologue    string      // noisep.PrologueSession or noisep.PrologueTunnel
	Static      noise.DHKey // the API's key pair for this link
	Peer        []byte      // the other side's static public key
	// Origins are the allowed Origin values (scheme://host). They are added to,
	// not instead of, two things the library always lets through: an Origin
	// equal to the request host (accept.go:239, compared before any pattern)
	// and a request with no Origin header at all (accept.go:228), which is how
	// a controller reaches the tunnel. So an empty list allows nothing but
	// those two, and a non-empty one cannot take them away. CheckOrigins reads
	// the list before the upgrade.
	//
	// trust.ValidOrigin is deliberately NOT used here, and the reason that
	// carries is the bare host form: the library matches a value without "://"
	// against the host alone and documents that as its normal case, while
	// ValidOrigin takes nothing but "https://" and a host — measured, it says
	// false to "ngc.example" and to "ngc.example:8443". It also refuses
	// http://localhost:8101, which would take a development machine with it,
	// although no contract path can carry that value here today: config.API has
	// no field for origins, the API never reads web.json, and the only source
	// this package can see is trust.List.RP.Origins, over which
	// trust.List.validateUsers already runs ValidOrigin
	// (shared/trust/trust.go:621) — those are the passkey origins, so that
	// check guards another list, not this one. Adding it here would refuse the
	// form the library asks for and buy nothing, so do not "fix" it. What is
	// left for this package is the one thing no grammar can see, that the values
	// end up in a matcher: CheckOrigins.
	Origins []string
	// Handshake is the time the whole Noise handshake may take, from the
	// connection being ours to the finished transport; zero or less means
	// HandshakeTimeout. A test sets it to milliseconds instead of waiting.
	Handshake time.Duration
	// FirstFrame is the time from the finished handshake to the first frame;
	// zero or less means FirstFrameTimeout.
	FirstFrame time.Duration
}

// CheckOrigins reads the list of allowed origins. It is exported so that
// whoever fills Options can run it once at startup, because Accept runs it per
// request: measured, a list with one dead entry beside a good one answers a
// request that carries no Origin header — one the library would have allowed —
// with no upgrade at all, and it does so at the first request rather than at
// boot. Refusing is the right half to keep (an allow-list that half works is
// worse than none), so the remedy is to ask at startup.
//
// The library takes every value as a path.Match pattern, so three kinds of entry
// are refused, each measured against that matcher: an empty one, which matches
// nothing and says nothing; one with a wildcard, because a list of allowed
// origins that a "*" or a "?" fits is no list ("*" matches the origin host
// evil.example.com); and one that does not match itself, which is every value
// the matcher cannot read and every bracketed IPv6 host, since brackets are a
// character class there — such an entry would quietly never let anyone through.
// The count follows the contract, which gives a trust list at most
// trust.MaxOrigins of them.
func CheckOrigins(origins []string) error {
	if len(origins) > trust.MaxOrigins {
		return fmt.Errorf("%w: %d entries", ErrOrigins, len(origins))
	}
	for i, o := range origins {
		if o == "" || strings.ContainsAny(o, "*?") {
			return fmt.Errorf("%w: entry %d", ErrOrigins, i)
		}
		// The library lowercases both sides before it matches, so the
		// self-test reads the value exactly as the matcher will.
		if ok, err := path.Match(strings.ToLower(o), strings.ToLower(o)); err != nil || !ok {
			return fmt.Errorf("%w: entry %d", ErrOrigins, i)
		}
	}
	return nil
}

// handshake is the deadline for this link's handshake.
func (o Options) handshake() time.Duration {
	if o.Handshake <= 0 {
		return HandshakeTimeout
	}
	return o.Handshake
}

// firstFrame is the deadline for this link's first frame.
func (o Options) firstFrame() time.Duration {
	if o.FirstFrame <= 0 {
		return FirstFrameTimeout
	}
	return o.FirstFrame
}

// Link is one open link.
type Link struct {
	conn *websocket.Conn
	tr   *noisep.Transport

	// first is when the first frame is due, zero once one has arrived. Only
	// the reader touches it, and a link has exactly one reader.
	first time.Time

	// ended says that a broken rule or a failed read has already closed the
	// link, so Close has nothing left to do. Close may come from another
	// goroutine than the reader, so this is atomic.
	ended atomic.Bool

	// One writer at a time: several goroutines relay onto one link, and a
	// Noise transport message must not be split or interleaved. Sealing is
	// inside the lock as well, because each message takes the next nonce of
	// the cipher state: sealed in one order and sent in another, they would
	// not decrypt.
	wmu sync.Mutex
}

// Accept upgrades the request and runs the Noise handshake as responder.
func Accept(w http.ResponseWriter, r *http.Request, o Options) (*Link, error) {
	// Before the upgrade, so a list that is not a list never guards anything.
	// Nothing is written to w on this way out; the caller answers the request.
	if err := CheckOrigins(o.Origins); err != nil {
		return nil, err
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{o.Subprotocol},
		OriginPatterns: o.Origins,
	})
	if err != nil {
		return nil, err
	}
	// A client that did not ask for our subprotocol speaks something else.
	if conn.Subprotocol() != o.Subprotocol {
		closeBounded(conn, websocket.StatusPolicyViolation, "subprotocol")
		return nil, ErrLink
	}
	// The clock on the handshake starts here, where the connection becomes
	// ours; r.Context() alone would only end with the handler.
	hctx, cancel := context.WithTimeout(r.Context(), o.handshake())
	defer cancel()
	// One frame at most, plus the Noise tag: nothing larger is ever valid, and
	// the limit keeps a flood from filling memory before it is refused.
	conn.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	hs, err := noisep.Handshake(false, []byte(o.Prologue), o.Static, o.Peer, randReader)
	if err != nil {
		closeBounded(conn, websocket.StatusInternalError, "handshake")
		return nil, err
	}
	// One word for every way the handshake can end, the deadline included: a
	// close frame never says how far the other side got.
	kind, msg1, err := conn.Read(hctx)
	if err != nil || kind != websocket.MessageBinary || len(msg1) != wire.HandshakeSize {
		closeBounded(conn, websocket.StatusPolicyViolation, "handshake")
		return nil, ErrLink
	}
	payload, _, _, err := hs.ReadMessage(nil, msg1)
	if err != nil || len(payload) != 0 {
		// Messages 1 and 2 carry no payload in all three uses of noisep.
		closeBounded(conn, websocket.StatusPolicyViolation, "handshake")
		return nil, ErrLink
	}
	msg2, cs1, cs2, err := hs.WriteMessage(nil, nil)
	if err != nil || cs1 == nil || cs2 == nil {
		closeBounded(conn, websocket.StatusInternalError, "handshake")
		return nil, ErrLink
	}
	if err := conn.Write(hctx, websocket.MessageBinary, msg2); err != nil {
		closeBounded(conn, websocket.StatusInternalError, "handshake")
		return nil, err
	}
	return &Link{conn: conn, tr: noisep.NewTransport(false, cs1, cs2),
		first: time.Now().Add(o.firstFrame())}, nil
}

// Read returns the next frame; every failure ends the link — a broken rule of
// the link and a read the library could not finish alike. Exactly one goroutine
// reads a link: the nonce of the cipher state fixes the order of the messages,
// so a second reader would decrypt out of turn. A ctx that ends a Read ends the
// link with it, so it is no poll timer.
func (l *Link) Read(ctx context.Context) (wire.Frame, error) {
	if !l.first.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, l.first)
		defer cancel()
	}
	kind, msg, err := l.conn.Read(ctx)
	if err != nil {
		// For a message over the read limit the library writes its close frame
		// and leaves the connection standing (write.go:428), and after any
		// broken read the place in the stream is unknown. Measured without this
		// line: the caller held a link that could no longer write and paid
		// 5,0008 s in Close to be rid of the socket.
		l.drop()
		return wire.Frame{}, err
	}
	if kind != websocket.MessageBinary {
		l.fail("binary only")
		return wire.Frame{}, ErrLink
	}
	plain, err := l.tr.Open(msg)
	if err != nil {
		l.fail("decrypt")
		return wire.Frame{}, ErrLink
	}
	f, err := wire.Decode(plain)
	if err != nil {
		l.fail("frame")
		return wire.Frame{}, ErrLink
	}
	l.first = time.Time{}
	return f, nil
}

// Write seals and sends one frame. Its second error cannot be reached today and
// is kept anyway: Encode allows wire.MaxPayload bytes, so the largest frame is
// wire.MaxFrame (65519) and that is exactly noisep.MaxPlaintext, the most Seal
// takes. The two limits come from two packages, and only their equality makes
// the branch dead — nothing in this file would notice if one of them moved.
func (l *Link) Write(ctx context.Context, f wire.Frame) error {
	raw, err := f.Encode()
	if err != nil {
		return err
	}
	l.wmu.Lock()
	defer l.wmu.Unlock()
	sealed, err := l.tr.Seal(raw)
	if err != nil {
		return err
	}
	return l.conn.Write(ctx, websocket.MessageBinary, sealed)
}

// Close ends the link in order and waits for the peer's answer to the close
// frame, which a refusal does not: CloseWait says why. Once a broken rule or a
// failed read has ended the link, Close has nothing left to do and says so at
// once: measured before that, the plain `defer l.Close(…)` of a caller paid
// 3,9995 s on top of the second that Read had already paid, because the second close
// waits in waitGoroutines for the goroutine of the first (close.go:102-108).
func (l *Link) Close(code websocket.StatusCode, reason string) error {
	if l.ended.Load() {
		return nil
	}
	return l.conn.Close(code, reason)
}

// fail ends the link on a broken message; the reason is fixed text, never
// anything from the message.
func (l *Link) fail(reason string) {
	if l.ended.Swap(true) {
		return
	}
	closeBounded(l.conn, websocket.StatusProtocolError, reason)
}

// drop ends the link without a close frame: after a read the library could not
// finish there is nothing left to say that it has not said itself, and nobody to
// wait for. CloseNow is the only way that really drops the socket, and the swap
// keeps it from meeting a close of ours that is already in flight — it would
// then wait in waitGoroutines for up to fifteen seconds instead of dropping
// anything (close.go:132-146, :231).
func (l *Link) drop() {
	if l.ended.Swap(true) {
		return
	}
	_ = l.conn.CloseNow()
}

// closeBounded sends the close frame and gives the peer at most CloseWait to
// answer it. The frame is worth sending — a browser learns that it was refused
// and not that the network died — but the answer is not worth waiting five
// seconds for from someone who just broke the rules. The wait runs in its own
// goroutine because websocket.Close takes no context and the library hands out
// no way to drop the connection at the bound: closing is swapped once, so a
// CloseNow after it only waits again, and the step that drops the socket is
// unexported (vendor/github.com/coder/websocket/close.go). That goroutine ends
// on its own, but not inside five seconds: the library allows five for writing
// the close frame (close.go:185) and five more for the answer (:199), with
// waitGoroutines on top (:231). The reader is free after CloseWait either way.
func closeBounded(conn *websocket.Conn, code websocket.StatusCode, reason string) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = conn.Close(code, reason)
	}()
	t := time.NewTimer(CloseWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
}
