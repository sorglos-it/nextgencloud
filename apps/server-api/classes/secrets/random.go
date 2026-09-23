package secrets

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// setupAlphabet is the alphabet of a one-time setup code
// (session.ValidSetupCode): capitals and digits without I, O, 0 and 1, which
// people confuse. 32 characters, so every byte of a random draw maps to one
// without bias.
const setupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Bytes returns n random bytes.
func Bytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// ID returns an ID of users, installations and other values trust.ValidID
// checks: 16 random bytes as base64url.
func ID() (string, error) {
	b, err := Bytes(16)
	if err != nil {
		return "", err
	}
	return enc.B64.EncodeToString(b), nil
}

// LogID returns the ID of a log entry (core.ValidLogID): 16 random bytes as
// lower-case hex, so every database collation orders it like Go.
func LogID() (string, error) {
	b, err := Bytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetupCode returns a one-time setup code in its canonical form: 20 characters
// of the alphabet in groups of five joined by '-' (session.ValidSetupCode).
func SetupCode() (string, error) {
	b, err := Bytes(20)
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, 23)
	for i, c := range b {
		if i > 0 && i%5 == 0 {
			out = append(out, '-')
		}
		out = append(out, setupAlphabet[int(c)%len(setupAlphabet)])
	}
	return string(out), nil
}
