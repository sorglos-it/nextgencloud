package secrets

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	b, err := NewBox(config.NewSecret(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	return b
}

func TestSealOpen(t *testing.T) {
	b := testBox(t)
	plain := []byte("totp secret")
	sealed, err := b.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("sealed value holds the plaintext")
	}
	back, err := b.Open(sealed)
	if err != nil || !bytes.Equal(back, plain) {
		t.Fatalf("open: %v %q", err, back)
	}
	// Two seals of the same value differ: the nonce is fresh every time, so a
	// database dump never shows which rows hold the same secret.
	again, _ := b.Seal(plain)
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals are equal")
	}
	// An empty value seals too: nonce and tag alone, the shortest sealed
	// value there is, which is exactly the length Open lets through.
	empty, err := b.Seal(nil)
	if err != nil {
		t.Fatalf("seal nil: %v", err)
	}
	back, err = b.Open(empty)
	if err != nil || len(back) != 0 {
		t.Fatalf("open nil: %v %q", err, back)
	}
	if _, err := b.Seal(make([]byte, MaxSecret+1)); !errors.Is(err, ErrSize) {
		t.Fatalf("a plaintext beyond MaxSecret was sealed: %v", err)
	}
}

func TestOpenRejectsDamage(t *testing.T) {
	b := testBox(t)
	sealed, _ := b.Seal([]byte("x"))
	for i := range sealed {
		bad := bytes.Clone(sealed)
		bad[i] ^= 1
		if _, err := b.Open(bad); !errors.Is(err, ErrSealed) {
			t.Fatalf("byte %d flipped and Open answered %v", i, err)
		}
	}
	if _, err := b.Open(sealed[:len(sealed)-1]); !errors.Is(err, ErrSealed) {
		t.Fatalf("truncated value answered %v", err)
	}
	if _, err := b.Open(nil); !errors.Is(err, ErrSealed) {
		t.Fatalf("empty value answered %v", err)
	}
	// The other half of the ErrSealed promise: an undamaged value under
	// another key. A backup opens with nothing but its own installation's key.
	other, err := NewBox(config.NewSecret(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	if _, err := other.Open(sealed); !errors.Is(err, ErrSealed) {
		t.Fatalf("another key opened the value: %v", err)
	}
}

// TestNewBoxRejectsShortKey pins ErrKey, not just "some error": a caller that
// skips a damaged row on ErrSealed must not silently skip an installation
// whose key is wrong.
func TestNewBoxRejectsShortKey(t *testing.T) {
	if _, err := NewBox(config.NewSecret([]byte("short"))); !errors.Is(err, ErrKey) {
		t.Fatalf("short key answered %v", err)
	}
	if _, err := NewBox(config.Secret{}); !errors.Is(err, ErrKey) {
		t.Fatalf("empty key answered %v", err)
	}
	if errors.Is(ErrKey, ErrSealed) || errors.Is(ErrSealed, ErrKey) {
		t.Fatal("ErrKey and ErrSealed cannot be told apart")
	}
}

func TestBytes(t *testing.T) {
	b, err := Bytes(32)
	if err != nil || len(b) != 32 {
		t.Fatalf("bytes: %v, %d bytes", err, len(b))
	}
	if other, _ := Bytes(32); bytes.Equal(b, other) {
		t.Fatal("two draws are equal")
	}
	for _, n := range []int{0, -1, MaxBytes + 1} {
		if _, err := Bytes(n); !errors.Is(err, ErrSize) {
			t.Fatalf("Bytes(%d) answered %v", n, err)
		}
	}
}

func TestRandomValues(t *testing.T) {
	id, err := ID()
	if err != nil || !trust.ValidID(id) {
		t.Fatalf("id %q: %v", id, err)
	}
	log, err := LogID()
	if err != nil || !core.ValidLogID(log) {
		t.Fatalf("log id %q: %v", log, err)
	}
	code, err := SetupCode()
	if err != nil || !session.ValidSetupCode(code) {
		t.Fatalf("setup code %q: %v", code, err)
	}
	other, _ := SetupCode()
	if code == other {
		t.Fatal("two setup codes are equal")
	}
}
