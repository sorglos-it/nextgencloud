// Package sign defines envelopes for documents signed with Ed25519 keys held
// by NGC components: API nodes, stores and the release key.
package sign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
)

// Purposes. A signature always covers Message(purpose, payload), so a
// signature made for one purpose never verifies for another.
const (
	PurposeClientCert = "clientcert/v1"
	PurposeAPICert    = "apicert/v1"
	PurposeTrustList  = "trustlist/v1"
	PurposeTask       = "task/v1"
	PurposeConfirm    = "confirm/v1"
	PurposeCatalog    = "catalog/v1"
	PurposeLicense    = "license/v1"
	PurposeRelease    = "release/v1"
)

// MaxPayload is the largest payload an envelope may carry.
const MaxPayload = 4 << 20

// Envelope is a document signed with an Ed25519 key. Verify the signature
// before parsing Payload.
type Envelope struct {
	Payload enc.Bytes `json:"payload"`
	KeyID   string    `json:"kid"`
	Sig     enc.Bytes `json:"sig"`
}

var (
	ErrBadSignature = errors.New("sign: bad signature")
	ErrWrongKey     = errors.New("sign: envelope names a different key")
	ErrTooLarge     = errors.New("sign: payload too large")
)

// Message returns the bytes a signature covers: "ngc/" || purpose || 0x00 || payload.
func Message(purpose string, payload []byte) []byte {
	m := make([]byte, 0, len("ngc/")+len(purpose)+1+len(payload))
	m = append(m, "ngc/"...)
	m = append(m, purpose...)
	m = append(m, 0)
	return append(m, payload...)
}

// KeyID identifies a public key: base64url of the first 16 bytes of its SHA-256.
func KeyID(pub []byte) string {
	h := sha256.Sum256(pub)
	return enc.B64.EncodeToString(h[:16])
}

// Sign signs payload for purpose.
func Sign(priv ed25519.PrivateKey, purpose string, payload []byte) (Envelope, error) {
	if len(payload) > MaxPayload {
		return Envelope{}, ErrTooLarge
	}
	pub := priv.Public().(ed25519.PublicKey)
	return Envelope{
		Payload: payload,
		KeyID:   KeyID(pub),
		Sig:     ed25519.Sign(priv, Message(purpose, payload)),
	}, nil
}

// Verify checks env against pub for purpose.
func Verify(env Envelope, pub ed25519.PublicKey, purpose string) error {
	if len(env.Payload) > MaxPayload {
		return ErrTooLarge
	}
	if env.KeyID != KeyID(pub) {
		return ErrWrongKey
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, Message(purpose, env.Payload), env.Sig) {
		return ErrBadSignature
	}
	return nil
}
