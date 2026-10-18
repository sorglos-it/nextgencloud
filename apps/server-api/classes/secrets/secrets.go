// Package secrets encrypts the values the API keeps in the database (TOTP
// secrets, session keys, the installation's private key) and makes the random
// values it hands out. The key is config.API.SecretKey, which every API of an
// installation shares, so a dump of the database or a backup is worthless on
// its own.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// MaxSecret is the largest plaintext Seal takes. Everything sealed here is a
// key, a code or a TOTP secret of a few dozen bytes, so the limit is far out
// of the way; it is there so that nothing coming from outside can ever run
// into Seal unchecked.
const MaxSecret = 1 << 20

// ErrSealed reports a value that is damaged or was sealed with another key.
var ErrSealed = errors.New("secrets: sealed value damaged or wrong key")

// ErrKey reports a key that is not 32 bytes. It is its own error, never
// ErrSealed: a caller that skips a damaged row on ErrSealed would otherwise
// skip an installation that is set up wrong just as quietly.
var ErrKey = errors.New("secrets: key is not 32 bytes")

// ErrSize reports a size outside the limits, MaxSecret or MaxBytes. Only our
// own code decides these sizes, so a size that misses tells of a mistake in
// the API, never of a value from outside.
var ErrSize = errors.New("secrets: size out of range")

// Box seals and opens values with the installation's secret key.
type Box struct{ aead cipher.AEAD }

// NewBox returns a Box for the 32-byte key of config.API.SecretKey.
func NewBox(key config.Secret) (*Box, error) {
	k := key.Bytes()
	if len(k) != 32 {
		return nil, ErrKey
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, ErrKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrKey
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain, at most MaxSecret bytes: a fresh nonce, then the
// sealed value. The nonce is prefixed, so a sealed value carries everything
// needed to open it but the key. Its length comes from the AEAD itself, the
// only source in this package: a second one would drift apart from it in
// silence the day the AEAD changes.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	if len(plain) > MaxSecret {
		return nil, ErrSize
	}
	nonce, err := Bytes(b.aead.NonceSize())
	if err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open decrypts a value of Seal. Every failure is ErrSealed alone: the error
// never quotes the value. The nonce is read with the length the AEAD names,
// the same source Seal writes with, so guard, slice and nonce agree.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n+b.aead.Overhead() {
		return nil, ErrSealed
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, ErrSealed
	}
	return plain, nil
}
