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
// wrong is E_INTERNAL. Three parties can be at fault here, not two: the API
// sits between the shell and us, and the doc of tunnel.Opened lets it "put
// any OPEN here". Which value carries which blame was measured, not reasoned
// out, and one of the four carries none at all:
//
//   - ErrChannel is this side. Both ways to it mean the channel handed over
//     has no end-to-end side at all: no session — access refuses an app
//     channel that has none — or a message 1 of another size, which
//     wire.ParseOpen lets through for no app OPEN.
//   - ErrEnded and ErrMessage are either side, told apart by the method they
//     came out of: out of Open the shell did it, out of Data or End this
//     side did.
//   - ErrHandshake says nothing about whose fault it was. Message 1 fails to
//     open the same way for causes of all three parties. Ours: a static key
//     that is no pair, or one that is not what our node ID stands for, or a
//     wrong node ID in the prologue. The shell's: a damaged message 1, or an
//     old copy of our key. The API's: an OPEN meant for another controller
//     put on this one — from here that is bit for bit our own wrong node ID,
//     with our configuration right. End it with E_INTERNAL. That names our
//     own right and the shell's and the API's wrong, and it is that way
//     round on purpose: the other one sends whoever runs this server looking
//     at the user.
//     Ours are catchable — just not here. A static key whose public half
//     derives from its private one (curve25519) catches the pair; a public
//     half equal to what our node ID carries in the trust list
//     (trust.Node.Noise) catches the key that is someone else's and the node
//     ID that is not ours. Neither check reads anything the channel brings:
//     their input is the key pair, our node ID and the trust list, and those
//     three are one process's configuration. A controller that has them
//     wrong has them wrong for every channel, not for this one. So their
//     place is where the own identity and each new version of the trust list
//     are loaded — refuse to start there, rather than pay an X25519 step per
//     channel at the most expensive point of the channel setup.
//     TestAcceptCannotSayWhoseFaultTheHandshakeWas holds three of the causes
//     on this one value; whoever tells them apart in Accept fails there.
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
// run at once. Sent reads what Data writes, so it belongs to the writing
// side: ask it in the goroutine that writes, or once that one has stopped.
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
//
// o must be the tunnel.Opened access.Check decided on, not one parsed a
// second time — and its two halves are bound in different ways. access
// decides on app, service and action (core.Lookup) and on the task with its
// confirm (task.Check); the params reach no decision of that package, the
// word does not occur in it, and access.go is all of it. What binds the
// params is this prologue: "Only two things bind it to what the shell sent",
// says the doc of tunnel.Opened — this prologue, which hashes params and
// task, and the signature of the task. So a second Opened would bind the
// channel to a task access never saw, and to params nothing else in this
// controller ever looks at. Inside here they are read once, but across the
// package boundary it is the caller's to keep, the way access.Request.List
// is.
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
	// Dead branch 1 of 4 in this package, measured: a panic in its place
	// leaves every test green. For Noise_KK without a PSK NewHandshakeState
	// answers with no error at all (state.go:317 of the vendored
	// github.com/flynn/noise), so this branch is what keeps the package off
	// a foreign library's word.
	if err != nil {
		return nil, nil, ErrHandshake
	}
	// At exactly HandshakeSize bytes the payload read here is empty, so
	// there is none to check and none to act on.
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		return nil, nil, ErrHandshake
	}
	msg2, cs1, cs2, err := hs.WriteMessage(nil, nil)
	// Dead branch 2 of 4 is the size half: dropping it leaves every test
	// green. It stays as a belt beside the braces, but it proves nothing.
	// What carries instead: after message 2 the pattern of Noise_KK is used
	// up, so WriteMessage hands out both cipher states, and a nil one would
	// panic in every round trip the tests run.
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
	// Dead branch 3 of 4, measured: wire.Close holds two string types, a
	// string and a *int, and json.Marshal has nothing to fail on in those.
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
	// Dead branch 4 of 4, measured: wire.E2E above holds a data body under
	// wire.MaxE2EPlaintext and an end under a CLOSE payload, both below
	// noisep.MaxPlaintext, so the size half of Seal has nothing left to
	// refuse. What remains of it is the nonce run out after 2^64 messages
	// (vendor/github.com/flynn/noise/state.go:56) — no test goes there.
	if err != nil {
		return nil, ErrMessage
	}
	return ct, nil
}
