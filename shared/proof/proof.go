// Package proof verifies that a user approved a payload: with the key the
// browser derives from the password (password key) or with a passkey.
package proof

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"golang.org/x/crypto/argon2"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
)

// Kind tells password keys and passkeys apart.
type Kind string

const (
	KindPassword Kind = "password"
	KindPasskey  Kind = "passkey"
)

// Signature algorithms.
const (
	AlgEdDSA = "EdDSA"
	AlgES256 = "ES256"
	AlgRS256 = "RS256"
)

// Argon2id parameters for the password key.
const (
	KDFArgon2id   = "argon2id"
	SaltSize      = 16
	DefaultMemKiB = 65536
	DefaultTime   = 3
	DefaultPar    = 1
	MinMemKiB     = 19456
	MaxMemKiB     = 1 << 20
)

// KDF describes how the browser derives the password key:
//
//	seed = Argon2id(password, Salt, Time, MemKiB, Par, 32 bytes)
//	key  = Ed25519 private key from seed
//
// The password is the UTF-8 encoding of the typed text after Unicode NFC
// normalization (JavaScript: password.normalize("NFC")).
type KDF struct {
	Alg    string    `json:"alg"`
	Salt   enc.Bytes `json:"salt"`
	MemKiB uint32    `json:"m"`
	Time   uint32    `json:"t"`
	Par    uint8     `json:"p"`
}

var (
	ErrKDF         = errors.New("proof: KDF parameters out of range")
	ErrKey         = errors.New("proof: malformed user key")
	ErrProof       = errors.New("proof: malformed proof")
	ErrKeyMismatch = errors.New("proof: proof names another key")
	ErrSignature   = errors.New("proof: bad signature")
	ErrClientData  = errors.New("proof: client data does not match")
	ErrAuthData    = errors.New("proof: authenticator data does not match")
	ErrNoUV        = errors.New("proof: user verification (PIN or biometrics) missing")
)

// Validate checks the parameters against the allowed ranges.
func (k KDF) Validate() error {
	if k.Alg != KDFArgon2id || len(k.Salt) != SaltSize ||
		k.MemKiB < MinMemKiB || k.MemKiB > MaxMemKiB ||
		k.Time < 1 || k.Time > 10 || k.Par < 1 || k.Par > 4 {
		return ErrKDF
	}
	return nil
}

// DeriveKey derives the password key. password must already be NFC-normalized UTF-8.
func DeriveKey(password []byte, k KDF) (ed25519.PrivateKey, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	seed := argon2.IDKey(password, k.Salt, k.Time, k.MemKiB, k.Par, ed25519.SeedSize)
	return ed25519.NewKeyFromSeed(seed), nil
}

// Size limits. They are generous for all three allowed algorithms (RSA-4096
// is the largest) but bound the cost of parsing an attacker-supplied key or
// signature before any expensive cryptography runs.
const (
	MaxPublic     = 1024 // SubjectPublicKeyInfo, enough for RSA-4096
	MaxSig        = 1024
	MaxRSABits    = 4096
	MaxAuthData   = 4096
	MaxClientData = 4096
	MaxCredID     = 1023 // longest credential ID an authenticator may hand out (WebAuthn, CTAP2)
)

// minAuthData is the fixed start of WebAuthn authenticator data: SHA-256 of
// the RP ID, one byte of flags and the 4-byte signature counter.
const minAuthData = 37

// UserKey is a key of a user as listed in the trust list.
type UserKey struct {
	ID     string    `json:"id"`
	Kind   Kind      `json:"kind"`
	Alg    string    `json:"alg"`
	Public enc.Bytes `json:"pub"`              // password: raw Ed25519 key; passkey: SubjectPublicKeyInfo (DER)
	CredID enc.Bytes `json:"credId,omitempty"` // passkey only
	KDF    *KDF      `json:"kdf,omitempty"`    // password only
}

// Validate checks the structure of k.
func (k UserKey) Validate() error {
	if k.ID != sign.KeyID(k.Public) {
		return ErrKey
	}
	switch k.Kind {
	case KindPassword:
		if k.Alg != AlgEdDSA || len(k.Public) != ed25519.PublicKeySize || k.KDF == nil || len(k.CredID) != 0 {
			return ErrKey
		}
		return k.KDF.Validate()
	case KindPasskey:
		if len(k.CredID) == 0 || len(k.CredID) > MaxCredID || len(k.Public) > MaxPublic || k.KDF != nil {
			return ErrKey
		}
		_, err := parsePublic(k)
		return err
	}
	return ErrKey
}

// Proof shows that the holder of a user key approved a payload.
type Proof struct {
	Kind  Kind   `json:"kind"`
	KeyID string `json:"kid"`
	// Password keys: Ed25519 signature over sign.Message(purpose, payload).
	// Passkeys: the WebAuthn assertion signature.
	Sig enc.Bytes `json:"sig"`
	// Passkeys only. The challenge in ClientData is Challenge(purpose, payload).
	AuthData   enc.Bytes `json:"authData,omitempty"`
	ClientData enc.Bytes `json:"clientData,omitempty"`
}

// Validate checks the structure of p before any cryptography runs:
// Kind is password or passkey; KeyID is base64url of 16 bytes; Sig has
// 1..MaxSig bytes; a password proof has no AuthData and no ClientData; a
// passkey proof has 37..MaxAuthData bytes of AuthData and
// 1..MaxClientData bytes of ClientData.
func (p Proof) Validate() error {
	if !ValidKeyID(p.KeyID) {
		return ErrProof
	}
	if len(p.Sig) == 0 || len(p.Sig) > MaxSig {
		return ErrProof
	}
	switch p.Kind {
	case KindPassword:
		if len(p.AuthData) != 0 || len(p.ClientData) != 0 {
			return ErrProof
		}
	case KindPasskey:
		if len(p.AuthData) < minAuthData || len(p.AuthData) > MaxAuthData ||
			len(p.ClientData) == 0 || len(p.ClientData) > MaxClientData {
			return ErrProof
		}
	default:
		return ErrProof
	}
	return nil
}

// ValidKeyID reports whether s names a user key: base64url of 16 bytes, as
// sign.KeyID writes it. Proof.Validate and everything else that carries a key
// ID (session.PasskeyRef) ask this one function, so a key ID means the same
// wherever it travels.
func ValidKeyID(s string) bool {
	// The length check comes first so an oversized key ID is never decoded.
	if len(s) != enc.B64.EncodedLen(sign.KeyIDSize) {
		return false
	}
	id, err := enc.B64.DecodeString(s)
	return err == nil && len(id) == sign.KeyIDSize
}

// RP names the relying party for passkeys.
type RP struct {
	ID      string   `json:"id"`                // e.g. "ngc.example.org"
	Origins []string `json:"origins,omitempty"` // e.g. ["https://ngc.example.org"]
}

// MaxRPID is the longest relying party ID, the length of a DNS name.
const MaxRPID = 253

var rpIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// ValidRPID reports whether s is a relying party ID: a DNS name in lower case
// of at most MaxRPID bytes, without scheme, port or path, as WebAuthn asks.
// config.Web, trust.List.Validate and session.ParseLoginChallenge ask this one
// function, so what the browser hands to navigator.credentials.get is the same
// name everywhere and never free text.
func ValidRPID(s string) bool {
	return s != "" && len(s) <= MaxRPID && rpIDPattern.MatchString(s)
}

// Challenge returns the WebAuthn challenge for a payload.
func Challenge(purpose string, payload []byte) []byte {
	h := sha256.Sum256(sign.Message(purpose, payload))
	return h[:]
}

// Verify checks that p is a proof by key over payload for purpose. key must
// be self-consistent: ID is the hash of Public.
func Verify(p Proof, key UserKey, rp RP, purpose string, payload []byte) error {
	if key.ID != sign.KeyID(key.Public) {
		return ErrKey
	}
	if p.KeyID != key.ID || p.Kind != key.Kind {
		return ErrKeyMismatch
	}
	if len(p.Sig) > MaxSig {
		return ErrSignature
	}
	switch key.Kind {
	case KindPassword:
		if key.Alg != AlgEdDSA || len(key.Public) != ed25519.PublicKeySize {
			return ErrKey
		}
		if !ed25519.Verify(ed25519.PublicKey(key.Public), sign.Message(purpose, payload), p.Sig) {
			return ErrSignature
		}
		return nil
	case KindPasskey:
		return verifyAssertion(p, key, rp, Challenge(purpose, payload))
	}
	return ErrKey
}

type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

const (
	flagUP = 0x01 // user present
	flagUV = 0x04 // user verified (PIN or biometrics)
)

func verifyAssertion(p Proof, key UserKey, rp RP, challenge []byte) error {
	var cd clientData
	if len(p.ClientData) > MaxClientData || json.Unmarshal(p.ClientData, &cd) != nil {
		return ErrClientData
	}
	if cd.Type != "webauthn.get" || cd.CrossOrigin ||
		cd.Challenge != enc.B64.EncodeToString(challenge) || !slices.Contains(rp.Origins, cd.Origin) {
		return ErrClientData
	}
	if len(p.AuthData) < minAuthData || len(p.AuthData) > MaxAuthData {
		return ErrAuthData
	}
	rpHash := sha256.Sum256([]byte(rp.ID))
	if !bytes.Equal(p.AuthData[:32], rpHash[:]) {
		return ErrAuthData
	}
	if flags := p.AuthData[32]; flags&flagUP == 0 || flags&flagUV == 0 {
		return ErrNoUV
	}
	cdHash := sha256.Sum256(p.ClientData)
	signed := make([]byte, 0, len(p.AuthData)+len(cdHash))
	signed = append(append(signed, p.AuthData...), cdHash[:]...)
	pub, err := parsePublic(key)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(signed)
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(k, digest[:], p.Sig) {
			return ErrSignature
		}
	case ed25519.PublicKey:
		if !ed25519.Verify(k, signed, p.Sig) {
			return ErrSignature
		}
	case *rsa.PublicKey:
		if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], p.Sig) != nil {
			return ErrSignature
		}
	default:
		return ErrKey
	}
	return nil
}

func parsePublic(k UserKey) (crypto.PublicKey, error) {
	if len(k.Public) > MaxPublic {
		return nil, ErrKey
	}
	pub, err := x509.ParsePKIXPublicKey(k.Public)
	if err != nil {
		return nil, ErrKey
	}
	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Alg != AlgES256 || key.Curve != elliptic.P256() {
			return nil, ErrKey
		}
	case ed25519.PublicKey:
		if k.Alg != AlgEdDSA {
			return nil, ErrKey
		}
	case *rsa.PublicKey:
		if k.Alg != AlgRS256 || key.N.BitLen() < 2048 || key.N.BitLen() > MaxRSABits {
			return nil, ErrKey
		}
	default:
		return nil, ErrKey
	}
	return pub, nil
}
