package secrets

import (
	"bytes"
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
}

func TestOpenRejectsDamage(t *testing.T) {
	b := testBox(t)
	sealed, _ := b.Seal([]byte("x"))
	for i := range sealed {
		bad := bytes.Clone(sealed)
		bad[i] ^= 1
		if _, err := b.Open(bad); err == nil {
			t.Fatalf("byte %d flipped and still opened", i)
		}
	}
	if _, err := b.Open(sealed[:len(sealed)-1]); err == nil {
		t.Fatal("truncated value opened")
	}
	if _, err := b.Open(nil); err == nil {
		t.Fatal("empty value opened")
	}
}

func TestNewBoxRejectsShortKey(t *testing.T) {
	if _, err := NewBox(config.NewSecret([]byte("short"))); err == nil {
		t.Fatal("short key accepted")
	}
	if _, err := NewBox(config.Secret{}); err == nil {
		t.Fatal("empty key accepted")
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
