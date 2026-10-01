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
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrLink reports a message that breaks the rules of the link. Accept answers
// it for a Noise message 2 it cannot write as well, which takes a fault inside
// the library: in Noise_KK that message always ends the handshake and hands
// over both cipher states.
var ErrLink = errors.New("link: malformed message")

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
	HandshakeTimeout = 10 * time.Second

	// CloseWait bounds how long a refused peer may take to answer the close
	// frame. A peer that just broke the rules of the link has earned less
	// patience than one that ends a link in order, which Close still gives
	// the five seconds of websocket.Close: measured, a refusal held the reader
	// those five seconds, so whoever opens many refused links holds a reader
	// for each of them that long.
	CloseWait = 1 * time.Second
)

// Options describe one link.
type Options struct {
	Subprotocol string      // session.Subprotocol or tunnel.Subprotocol
	Prologue    string      // noisep.PrologueSession or noisep.PrologueTunnel
	Static      noise.DHKey // the API's key pair for this link
	Peer        []byte      // the other side's static public key
	// Origins are the allowed Origin values (scheme://host); empty allows
	// only the request host, and a request without an Origin header is
	// allowed either way, which is how a controller reaches the tunnel. The
	// WebSocket library reads each value as a path.Match pattern, so only
	// values trust.ValidOrigin accepts may be passed: it takes "https://"
	// and a host with an optional port and nothing else, while a pattern
	// with a "*" would match every origin host.
	Origins []string
	// Handshake is the time the whole Noise handshake may take, from the
	// connection being ours to the finished transport; zero means
	// HandshakeTimeout. A test sets it to milliseconds instead of waiting.
	Handshake time.Duration
}

// handshake is the deadline for this link's handshake.
func (o Options) handshake() time.Duration {
	if o.Handshake <= 0 {
		return HandshakeTimeout
	}
	return o.Handshake
}

// Link is one open link.
type Link struct {
	conn *websocket.Conn
	tr   *noisep.Transport

	// One writer at a time: several goroutines relay onto one link, and a
	// Noise transport message must not be split or interleaved. Sealing is
	// inside the lock as well, because each message takes the next nonce of
	// the cipher state: sealed in one order and sent in another, they would
	// not decrypt.
	wmu sync.Mutex
}

// Accept upgrades the request and runs the Noise handshake as responder.
func Accept(w http.ResponseWriter, r *http.Request, o Options) (*Link, error) {
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
	return &Link{conn: conn, tr: noisep.NewTransport(false, cs1, cs2)}, nil
}

// Read returns the next frame; every failure ends the link. Exactly one
// goroutine reads a link: the nonce of the cipher state fixes the order of the
// messages, so a second reader would decrypt out of turn.
func (l *Link) Read(ctx context.Context) (wire.Frame, error) {
	kind, msg, err := l.conn.Read(ctx)
	if err != nil {
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
	return f, nil
}

// Write seals and sends one frame.
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
// frame, which a refusal does not: CloseWait says why.
func (l *Link) Close(code websocket.StatusCode, reason string) error {
	return l.conn.Close(code, reason)
}

// fail ends the link on a broken message; the reason is fixed text, never
// anything from the message.
func (l *Link) fail(reason string) {
	closeBounded(l.conn, websocket.StatusProtocolError, reason)
}

// closeBounded sends the close frame and gives the peer at most CloseWait to
// answer it. The frame is worth sending — a browser learns that it was refused
// and not that the network died — but the answer is not worth waiting five
// seconds for from someone who just broke the rules. The wait runs in its own
// goroutine because websocket.Close takes no context and the library hands out
// no way to drop the connection at the bound: closing is swapped once, so a
// CloseNow after it only waits again, and the step that drops the socket is
// unexported (vendor/github.com/coder/websocket/close.go). The library ends the
// connection within its own five seconds; the reader is free after CloseWait.
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
