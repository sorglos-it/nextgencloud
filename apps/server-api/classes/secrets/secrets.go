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

// nonceSize is the standard nonce of AES-GCM; it is prefixed to every sealed
// value, so a value carries everything needed to open it but the key.
const nonceSize = 12

// ErrSealed reports a value that is damaged or was sealed with another key.
var ErrSealed = errors.New("secrets: sealed value damaged or wrong key")

// Box seals and opens values with the installation's secret key.
type Box struct{ aead cipher.AEAD }

// NewBox returns a Box for the 32-byte key of config.API.SecretKey.
func NewBox(key config.Secret) (*Box, error) {
	k := key.Bytes()
	if len(k) != 32 {
		return nil, ErrSealed
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, ErrSealed
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrSealed
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain: a fresh nonce, then the sealed value.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	nonce, err := Bytes(nonceSize)
	if err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open decrypts a value of Seal. Every failure is ErrSealed alone: the error
// never quotes the value.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	if len(sealed) < nonceSize+b.aead.Overhead() {
		return nil, ErrSealed
	}
	plain, err := b.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return nil, ErrSealed
	}
	return plain, nil
}
