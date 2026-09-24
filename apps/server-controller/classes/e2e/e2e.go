// Package e2e runs the end-to-end channel between a shell and this
// controller: the Noise_KK handshake the shell began in its OPEN, and the
// inner messages that travel encrypted inside the channel's DATA
// (shared/README.md, section "Ende zu Ende"). Only apps other than the core
// apps have one; a core app's DATA runs in the link's own Noise session.
//
// Nothing here decides whether a channel may run — access does that — and
// nothing here sends: Accept hands back the Noise message 2 the caller puts
// into its first GRANT, and every method hands back the bytes of one frame.
package e2e

import (
	"crypto/rand"
	"encoding/json"
	"errors"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Every failure answers with one of these alone: what a ciphertext or an
// inner message held says nothing that belongs in the log of this server.
// All of them end the channel, and which CLOSE it ends with is the caller's
// to pick — what the shell did wrong is E_PROTOCOL, what this controller did
// wrong is E_INTERNAL. ErrEnded is both, told apart by where it came from:
// out of Open the shell wrote after its own end, out of Data or End this
// side did.
var (
	ErrChannel   = errors.New("e2e: channel carries no end-to-end handshake")
	ErrHandshake = errors.New("e2e: end-to-end handshake failed")
	ErrEnded     = errors.New("e2e: nothing follows an end")
	ErrMessage   = errors.New("e2e: malformed end-to-end message")
)

// Session is the end-to-end channel of one OPEN. Open reads what the shell
// sends, Data and End write what the service answers. The two directions
// touch different fields — Open only peerEnded and the receiving cipher
// state, Data and End only ended, sent and the sending one — so one goroutine
// may read while another writes. Two that write, or two that read, must not
// run at once.
type Session struct {
	tr *noisep.Transport
	// ended and peerEnded carry the one rule of the inner messages that wire
	// cannot keep on its own, because it needs the state of the channel:
	// after its own end a side sends nothing more.
	ended     bool
	peerEnded bool
	sent      int64
}

// Accept answers the handshake in the OPEN of o and returns the session and
// the Noise message 2 for the first GRANT. static is this controller's own
// Noise key pair, nodeID its node in the trust list: the prologue binds both
// ends to this one channel of this one controller.
func Accept(o tunnel.Opened, nodeID string, static noise.DHKey) (*Session, []byte, error) {
	// The peer key is the dh of the client certificate, and nothing else
	// names it. An app channel always carries one — access refuses one
	// without a user — so no session here means the caller handed over a
	// channel that has no end-to-end side at all.
	if o.Session == nil {
		return nil, nil, ErrChannel
	}
	// Message 1 as the OPEN carried it, taken once: what the size is checked
	// on is what the handshake then reads. Noise_KK message 1 without a
	// payload is exactly HandshakeSize bytes — the ephemeral key and the tag
	// leave no room for one — and a longer message is one with a payload,
	// which Noise would read and hand out and the contract forbids.
	msg1 := o.Open.E2E
	if len(msg1) != wire.HandshakeSize {
		return nil, nil, ErrChannel
	}
	// Params and task go into the prologue as the OPEN carried them
	// (tunnel.VerifyOpen keeps those bytes): hashing anything re-encoded
	// would bind the channel to something the shell never wrote, and the
	// shell's own prologue would no longer match.
	prologue := noisep.E2EPrologue(nodeID, o.Open.App, o.Open.Service, o.Open.Action,
		o.Open.Params, o.Open.TaskBytes())
	hs, err := noisep.Handshake(false, prologue, static, o.Session.Cert().DH, rand.Reader)
	if err != nil {
		return nil, nil, ErrHandshake
	}
	// At exactly HandshakeSize bytes the payload read here is empty, so
	// there is none to check and none to act on.
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		return nil, nil, ErrHandshake
	}
	msg2, cs1, cs2, err := hs.WriteMessage(nil, nil)
	if err != nil || len(msg2) != wire.HandshakeSize {
		return nil, nil, ErrHandshake
	}
	return &Session{tr: noisep.NewTransport(false, cs1, cs2)}, msg2, nil
}

// Open reads one DATA payload of the shell: the Noise transport message is
// decrypted and its plaintext read as one inner message. After the shell's
// own end nothing more of it may come, and one that does is refused.
func (s *Session) Open(ciphertext []byte) (wire.E2EMessage, error) {
	if s.peerEnded {
		return wire.E2EMessage{}, ErrEnded
	}
	plain, err := s.tr.Open(ciphertext)
	if err != nil {
		return wire.E2EMessage{}, ErrMessage
	}
	m, err := wire.ParseE2E(plain, false)
	if err != nil {
		return wire.E2EMessage{}, ErrMessage
	}
	if m.Kind == wire.E2EEnd {
		s.peerEnded = true
	}
	return m, nil
}

// Data seals one piece of the service's output. It bounds one inner message
// and no more: how much output an action may give is wire.MaxCallResult, and
// the caller holds that against Sent, because only the caller knows whether
// the action is a stream (store.Action.Stream).
func (s *Session) Data(b []byte) ([]byte, error) {
	ct, err := s.seal(wire.E2EData, b)
	if err != nil {
		return nil, err
	}
	s.sent += int64(len(b))
	return ct, nil
}

// End seals the outcome and closes this side: Data and End are over after it.
// The output of the service still follows the shell's end, but nothing
// follows this one.
func (s *Session) End(cl wire.Close) ([]byte, error) {
	body, err := json.Marshal(cl)
	if err != nil {
		return nil, ErrMessage
	}
	ct, err := s.seal(wire.E2EEnd, body)
	if err != nil {
		return nil, err
	}
	s.ended = true
	return ct, nil
}

// Sent is how many bytes of output went out so far: what Data took, not what
// the inner messages cost around it.
func (s *Session) Sent() int64 { return s.sent }

// seal builds one inner message and encrypts it. wire.E2E bounds the
// plaintext by wire.MaxE2EPlaintext and reads the outcome of an end as a
// CLOSE payload, so nothing malformed reaches the wire.
func (s *Session) seal(kind byte, body []byte) ([]byte, error) {
	if s.ended {
		return nil, ErrEnded
	}
	plain, err := wire.E2E(kind, body)
	if err != nil {
		return nil, ErrMessage
	}
	ct, err := s.tr.Seal(plain)
	if err != nil {
		return nil, ErrMessage
	}
	return ct, nil
}
