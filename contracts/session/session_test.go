package session

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

const (
	issued = 1790000000
	now    = issued + 100
)

type fixture struct {
	list   *trust.List
	admin  trusttest.Signer
	client []byte
	proof  proof.Proof
	api    sign.Envelope
}

// newFixture signs everything with the password admin. trusttest.Genesis
// sorts its two admins by ID, which puts the passkey admin ("admin2") at
// Users[0] and the password admin passed in here (trusttest.NewSigner(1),
// "admin") at Users[1] — see trust_test.go's chain comment.
func newFixture(t testing.TB) fixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	sessSig := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	sessDH, _ := trusttest.NodeKeys(0x52)
	client, err := json.Marshal(ClientCert{
		Installation: l.Installation, User: l.Users[1].ID, Nonce: bytes.Repeat([]byte{0x53}, NonceSize),
		Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)), DH: sessDH.Public, Issued: issued, Expires: issued + 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	apiDH, _ := trusttest.NodeKeys(0x54)
	_, nodeSign := trusttest.NodeKeys(0xA1) // key of "api-1" in trusttest.Genesis
	env, err := SignAPI(nodeSign, APICert{Installation: l.Installation, Node: "api-1", Client: CertHash(client),
		DH: apiDH.Public, Issued: issued, Expires: issued + 3600})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{list: l, admin: admin, client: client, proof: admin.Prove(sign.PurposeClientCert, client), api: env}
}

func TestVerify(t *testing.T) {
	f := newFixture(t)
	c, err := VerifyClient(f.client, f.proof, f.list, now)
	if err != nil || c.User != f.list.Users[1].ID {
		t.Fatalf("client: %v", err)
	}
	a, err := VerifyAPI(f.api, f.list, f.client, now)
	if err != nil || a.Node != "api-1" {
		t.Fatalf("api: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	f := newFixture(t)
	if _, err := VerifyClient(f.client, f.proof, f.list, issued+3600); err != ErrExpired {
		t.Errorf("expired: %v", err)
	}
	if _, err := VerifyClient(f.client, f.proof, f.list, issued-MaxSkew-1); err != ErrExpired {
		t.Errorf("not yet valid: %v", err)
	}
	stranger := trusttest.NewSigner(9)
	if _, err := VerifyClient(f.client, stranger.Prove(sign.PurposeClientCert, f.client), f.list, now); err != ErrUser {
		t.Errorf("foreign key: %v", err)
	}
	// "foreign key" above only covers the unknown-key half of the owner
	// check. Here the signing key is genuinely listed (f.admin owns
	// Users[1]), but the certificate claims to be Users[0] (the passkey
	// admin) instead: still ErrUser.
	var cu ClientCert
	if err := json.Unmarshal(f.client, &cu); err != nil {
		t.Fatal(err)
	}
	cu.User = f.list.Users[0].ID
	otherUser, _ := json.Marshal(cu)
	if _, err := VerifyClient(otherUser, f.admin.Prove(sign.PurposeClientCert, otherUser), f.list, now); err != ErrUser {
		t.Errorf("cross-user: %v", err)
	}
	if _, err := VerifyClient(f.client, f.admin.Prove(sign.PurposeTask, f.client), f.list, now); err != proof.ErrSignature {
		t.Errorf("other purpose: %v", err)
	}
	var c ClientCert
	if err := json.Unmarshal(f.client, &c); err != nil {
		t.Fatal(err)
	}
	c.Expires = c.Issued + MaxLifetime + 1
	long, _ := json.Marshal(c)
	if _, err := VerifyClient(long, f.admin.Prove(sign.PurposeClientCert, long), f.list, now); err != ErrCert {
		t.Errorf("lifetime: %v", err)
	}
	if _, err := VerifyAPI(f.api, f.list, long, now); err != ErrCert {
		t.Errorf("api cert for another client: %v", err)
	}
	var a APICert
	if err := json.Unmarshal(f.api.Payload, &a); err != nil {
		t.Fatal(err)
	}
	_, rogue := trusttest.NodeKeys(0x77)
	env, _ := SignAPI(rogue, a)
	if _, err := VerifyAPI(env, f.list, f.client, now); err != ErrCert {
		t.Errorf("rogue api: %v", err)
	}

	// Each case changes exactly one field of a freshly signed API
	// certificate and re-signs it, so the signature check does not fire
	// before the field check under test does.
	_, nodeSign := trusttest.NodeKeys(0xA1) // key of "api-1" in trusttest.Genesis
	apiCase := func(mutate func(*APICert), want error) {
		t.Helper()
		var m APICert
		if err := json.Unmarshal(f.api.Payload, &m); err != nil {
			t.Fatal(err)
		}
		mutate(&m)
		env, err := SignAPI(nodeSign, m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyAPI(env, f.list, f.client, now); err != want {
			t.Errorf("got %v, want %v", err, want)
		}
	}
	apiCase(func(m *APICert) { m.Installation = trusttest.ID(0x12) }, ErrCert)
	apiCase(func(m *APICert) { m.Node = "api-2" }, ErrCert)
	apiCase(func(m *APICert) { m.DH = bytes.Repeat([]byte{0x99}, 31) }, ErrCert)
	apiCase(func(m *APICert) { m.Expires = m.Issued + MaxLifetime + 1 }, ErrCert)
	apiCase(func(m *APICert) { m.Expires = now }, ErrExpired)
	apiCase(func(m *APICert) { m.Issued = now + MaxSkew + 1 }, ErrExpired)
}

func TestCodes(t *testing.T) {
	for _, s := range []string{"ABCDE-FGHJK-LMNPQ-RSTUV", "23456-789AB-CDEFG-HJKLM"} {
		if !ValidSetupCode(s) {
			t.Errorf("%s rejected", s)
		}
	}
	for _, s := range []string{"ABCDE-FGHJK-LMNPQ", "abcde-fghjk-lmnpq-rstuv", "ABCDE-FGHJK-LMNPQ-RSTU0", "ABCDE FGHJK LMNPQ RSTUV"} {
		if ValidSetupCode(s) {
			t.Errorf("%s accepted", s)
		}
	}
	if !ValidTOTP("012345") || ValidTOTP("12345") || ValidTOTP("12345a") || ValidTOTP("١٢٣٤٥٦") {
		t.Error("TOTP format")
	}
}

type vector struct {
	Trust  trust.Signed  `json:"trust"`
	Now    int64         `json:"now"`
	Client enc.Bytes     `json:"client"`
	Proof  proof.Proof   `json:"proof"`
	API    sign.Envelope `json:"api"`
}

func TestVectors(t *testing.T) {
	f := newFixture(t)
	golden.Check(t, *update, "session.json", vector{Trust: f.admin.Sign(t, f.list), Now: now, Client: f.client, Proof: f.proof, API: f.api})
}

func FuzzVerifyClient(f *testing.F) {
	fx := newFixture(f)
	f.Add(fx.client)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = VerifyClient(b, fx.proof, fx.list, now)
	})
}

// FuzzVerifyAPI signs the fuzzed bytes as the envelope payload with the real
// node key, as FuzzOpen (contracts/trust) fuzzes chain bytes rather than
// arbitrary top-level input, so fuzzing exercises APICert's JSON decoding
// and field checks instead of mostly failing the signature check.
func FuzzVerifyAPI(f *testing.F) {
	fx := newFixture(f)
	_, nodeSign := trusttest.NodeKeys(0xA1) // key of "api-1" in trusttest.Genesis
	f.Add([]byte(fx.api.Payload))
	f.Fuzz(func(t *testing.T, b []byte) {
		env, err := sign.Sign(nodeSign, sign.PurposeAPICert, b)
		if err != nil {
			return
		}
		_, _ = VerifyAPI(env, fx.list, fx.client, now)
	})
}
