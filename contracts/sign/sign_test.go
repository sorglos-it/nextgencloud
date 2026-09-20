package sign

import (
	"crypto/ed25519"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func testKey(fill byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = fill + byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func TestSignVerify(t *testing.T) {
	priv := testKey(0)
	pub := priv.Public().(ed25519.PublicKey)
	env, err := Sign(priv, PurposeTask, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, pub, PurposeTask); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := Verify(env, pub, PurposeConfirm); err != ErrBadSignature {
		t.Errorf("other purpose: %v", err)
	}
	other := testKey(100).Public().(ed25519.PublicKey)
	if err := Verify(env, other, PurposeTask); err != ErrWrongKey {
		t.Errorf("other key: %v", err)
	}
	env.Payload = []byte(`{"v":2}`)
	if err := Verify(env, pub, PurposeTask); err != ErrBadSignature {
		t.Errorf("tampered payload: %v", err)
	}
	env.Payload = make([]byte, MaxPayload+1)
	if err := Verify(env, pub, PurposeTask); err != ErrTooLarge {
		t.Errorf("oversized payload: %v", err)
	}
}

func TestSignTooLarge(t *testing.T) {
	if _, err := Sign(testKey(0), PurposeTask, make([]byte, MaxPayload+1)); err != ErrTooLarge {
		t.Fatalf("got %v", err)
	}
}

func TestSignVerifyEmptyPayload(t *testing.T) {
	priv := testKey(0)
	pub := priv.Public().(ed25519.PublicKey)
	env, err := Sign(priv, PurposeTask, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, pub, PurposeTask); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyBadKeyLength(t *testing.T) {
	priv := testKey(0)
	env, err := Sign(priv, PurposeTask, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	short := ed25519.PublicKey{1, 2, 3}
	env.KeyID = KeyID(short) // matches, so the KeyID check doesn't hide the length check below it
	if err := Verify(env, short, PurposeTask); err != ErrBadSignature {
		t.Errorf("short key: %v", err)
	}
}

type signVector struct {
	Seed     enc.Bytes `json:"seed"`
	Public   enc.Bytes `json:"public"`
	Purpose  string    `json:"purpose"`
	Message  enc.Bytes `json:"message"`
	Envelope Envelope  `json:"envelope"`
}

func TestVectors(t *testing.T) {
	priv := testKey(0)
	payload := []byte(`{"v":1,"seq":1}`)
	env, err := Sign(priv, PurposeTrustList, payload)
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, *update, "sign.json", signVector{
		Seed:     priv.Seed(),
		Public:   []byte(priv.Public().(ed25519.PublicKey)),
		Purpose:  PurposeTrustList,
		Message:  Message(PurposeTrustList, payload),
		Envelope: env,
	})
}
