// Package noisep fixes how NGC uses the Noise protocol framework: always
// Noise_KK_25519_AESGCM_SHA256, for three uses told apart by the prologue:
// session (browser -> API; static keys ClientCert.DH and APICert.DH), tunnel
// (controller -> API; keys from the trust list, before that from the pairing
// code) and end to end (shell -> controller; ClientCert.DH and the
// controller's key from the trust list). Messages 1 and 2 carry an empty
// payload in all three uses, so each is exactly wire.HandshakeSize (48) bytes;
// a non-empty payload is a protocol error. Session and tunnel send them as the
// first two binary WebSocket messages, an end-to-end channel in Open.E2E and
// the first GRANT (package wire). Whoever carries message 1 can replay it, so
// nothing is acted on before the initiator's first transport message has
// decrypted. A failed Open ends the link, or on an end-to-end channel the
// channel, as cut off.
package noisep

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"

	"github.com/flynn/noise"
)

// Suite is X25519, AES-256-GCM and SHA-256.
var Suite = noise.NewCipherSuite(noise.DH25519, noise.CipherAESGCM, noise.HashSHA256)

// Prologues and sizes.
const (
	PrologueSession = "ngc/session/v1" // browser ↔ API, the browser initiates
	PrologueTunnel  = "ngc/tunnel/v1"  // controller ↔ API, the controller initiates
	PrologueE2E     = "ngc/e2e/v1"     // shell ↔ controller per channel, the shell initiates; see E2EPrologue
	MaxMessage      = 65535
	TagSize         = 16
	MaxPlaintext    = MaxMessage - TagSize
)

// ErrTooLarge reports a message above the Noise limit.
var ErrTooLarge = errors.New("noisep: message too large")

// E2EPrologue binds an end-to-end handshake to exactly one channel:
//
//	"ngc/e2e/v1" 0x00 controllerID 0x00 app 0x00 service 0x00 action 0x00 SHA-256(params) SHA-256(task)
//
// (each SHA-256 as 32 raw bytes, one after the other). params and task are
// the values of the OPEN's params and task members exactly as the shell wrote
// them into its OPEN payload, each from its opening '{' to its closing '}';
// nil for a member the OPEN does not have, which hashes as the empty string.
// An OPEN with a task has no params of its own, so the task carries them;
// binding its bytes keeps whoever relays the OPEN from swapping in, adding or
// dropping a task, even another valid one of the same user. The controller
// takes both from tunnel.VerifyOpen: Opened.Open.Params and
// Opened.Open.TaskBytes(), which keep the bytes as they arrived.
func E2EPrologue(controllerID, app, service, action string, params, task []byte) []byte {
	p := sha256.Sum256(params)
	t := sha256.Sum256(task)
	b := []byte(PrologueE2E)
	for _, s := range []string{controllerID, app, service, action} {
		b = append(b, 0)
		b = append(b, s...)
	}
	b = append(b, 0)
	b = append(b, p[:]...)
	return append(b, t[:]...)
}

// Keypair creates an X25519 key pair. rng is crypto/rand.Reader outside tests.
func Keypair(rng io.Reader) (noise.DHKey, error) { return Suite.GenerateKeypair(rng) }

// Handshake starts a Noise_KK handshake: static is the own key pair, peer the
// static public key of the other side.
func Handshake(initiator bool, prologue []byte, static noise.DHKey, peer []byte, rng io.Reader) (*noise.HandshakeState, error) {
	return newHandshake(initiator, prologue, static, peer, rng, noise.DHKey{})
}

// newHandshake allows a fixed ephemeral key for test vectors: the
// noise.HandshakeState always draws a fresh e from its Random source when
// writing a message (Config.EphemeralKeypair only ever feeds a pre-message
// hash, which Noise_KK has none of), so a non-zero eph replaces rng with a
// reader over eph.Private. X25519 clamping is idempotent, so the keypair
// GenerateKeypair then derives from those bytes is eph itself.
func newHandshake(initiator bool, prologue []byte, static noise.DHKey, peer []byte, rng io.Reader, eph noise.DHKey) (*noise.HandshakeState, error) {
	if len(eph.Private) > 0 {
		rng = bytes.NewReader(eph.Private)
	}
	return noise.NewHandshakeState(noise.Config{
		CipherSuite:      Suite,
		Random:           rng,
		Pattern:          noise.HandshakeKK,
		Initiator:        initiator,
		Prologue:         prologue,
		StaticKeypair:    static,
		PeerStatic:       peer,
		EphemeralKeypair: eph,
	})
}

// Transport encrypts and decrypts messages after the handshake.
type Transport struct {
	send, recv *noise.CipherState
}

// NewTransport sorts the cipher states the handshake returned: cs1 carries
// initiator → responder, cs2 responder → initiator.
func NewTransport(initiator bool, cs1, cs2 *noise.CipherState) *Transport {
	if initiator {
		return &Transport{send: cs1, recv: cs2}
	}
	return &Transport{send: cs2, recv: cs1}
}

// Seal encrypts one message.
func (t *Transport) Seal(plaintext []byte) ([]byte, error) {
	if len(plaintext) > MaxPlaintext {
		return nil, ErrTooLarge
	}
	return t.send.Encrypt(nil, nil, plaintext)
}

// Open decrypts one message.
func (t *Transport) Open(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) > MaxMessage {
		return nil, ErrTooLarge
	}
	return t.recv.Decrypt(nil, nil, ciphertext)
}
