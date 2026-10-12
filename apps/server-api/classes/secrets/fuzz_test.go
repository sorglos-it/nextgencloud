package secrets

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// FuzzOpen feeds Open with everything: it must never panic, and every FAILURE
// must be ErrSealed and nothing else.
//
// Not "it must answer ErrSealed", which is what this line used to say and what
// the body does not hold: success is allowed, and has to be -- the first seed
// is a sealed value this box really opens. What the body refuses is a failure
// of any other shape.
func FuzzOpen(f *testing.F) {
	b, err := NewBox(config.NewSecret(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		f.Fatalf("new box: %v", err)
	}
	sealed, err := b.Seal([]byte("seed"))
	if err != nil {
		f.Fatalf("seal: %v", err)
	}
	f.Add(sealed)
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0}, 28))
	f.Fuzz(func(t *testing.T, in []byte) {
		if _, err := b.Open(in); err != nil && !errors.Is(err, ErrSealed) {
			t.Fatalf("open answered %v instead of ErrSealed", err)
		}
	})
}
