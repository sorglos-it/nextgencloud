package secrets

import (
	"bytes"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// FuzzOpen feeds Open with everything: it must answer ErrSealed and never
// panic, whatever comes out of the database.
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
		if _, err := b.Open(in); err != nil && err != ErrSealed {
			t.Fatalf("open answered %v instead of ErrSealed", err)
		}
	})
}
