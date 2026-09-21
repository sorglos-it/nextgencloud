package proof

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"flag"
	"math/big"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

var testRP = RP{ID: "ngc.example.org", Origins: []string{"https://ngc.example.org"}}

// authenticator is a software passkey for tests.
type authenticator struct {
	key    UserKey
	signFn func(digest, signed []byte) []byte
}

func newAuthenticator(t *testing.T, alg string) authenticator {
	t.Helper()
	return newAuthenticatorBits(t, alg, 2048)
}

// newAuthenticatorBits is newAuthenticator with the size of an RSA modulus
// given, so a vector can carry a key that is too short.
func newAuthenticatorBits(t *testing.T, alg string, rsaBits int) authenticator {
	t.Helper()
	var (
		pub    any
		signFn func(digest, signed []byte) []byte
	)
	switch alg {
	case AlgES256:
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub = &priv.PublicKey
		signFn = func(digest, _ []byte) []byte {
			s, err := ecdsa.SignASN1(rand.Reader, priv, digest)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
	case AlgEdDSA:
		p, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub = p
		signFn = func(_, signed []byte) []byte { return ed25519.Sign(priv, signed) }
	case AlgRS256:
		priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
		if err != nil {
			t.Fatal(err)
		}
		pub = &priv.PublicKey
		signFn = func(digest, _ []byte) []byte {
			s, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
	}
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return authenticator{
		key:    UserKey{ID: sign.KeyID(spki), Kind: KindPasskey, Alg: alg, Public: spki, CredID: []byte("credential-1")},
		signFn: signFn,
	}
}

// assertion is what an authenticator puts into one WebAuthn assertion.
// assertionFor returns a valid one; a negative case changes one field.
type assertion struct {
	typ         string // type in the client data
	origin      string
	crossOrigin bool
	challenge   []byte
	rpID        string // its SHA-256 starts the authenticator data
	flags       byte
	authDataLen int
}

func assertionFor(purpose string, payload []byte) assertion {
	return assertion{typ: "webauthn.get", origin: testRP.Origins[0], challenge: Challenge(purpose, payload),
		rpID: testRP.ID, flags: flagUP | flagUV, authDataLen: minAuthData}
}

// assert signs as with a's key, over authenticator data and the hash of the
// client data as a real authenticator does, so a vector breaks only the rule
// its field was changed for.
func (a authenticator) assert(as assertion) Proof {
	cd, _ := json.Marshal(map[string]any{
		"type":        as.typ,
		"challenge":   enc.B64.EncodeToString(as.challenge),
		"origin":      as.origin,
		"crossOrigin": as.crossOrigin,
	})
	rpHash := sha256.Sum256([]byte(as.rpID))
	ad := make([]byte, as.authDataLen)
	copy(ad, append(rpHash[:], as.flags, 0, 0, 0, 7)) // signature counter 7
	cdHash := sha256.Sum256(cd)
	signed := append(append([]byte{}, ad...), cdHash[:]...)
	digest := sha256.Sum256(signed)
	return Proof{Kind: KindPasskey, KeyID: a.key.ID, Sig: a.signFn(digest[:], signed), AuthData: ad, ClientData: cd}
}

func (a authenticator) prove(rp RP, purpose string, payload []byte, flags byte, origin string) Proof {
	as := assertionFor(purpose, payload)
	as.rpID, as.flags, as.origin = rp.ID, flags, origin
	return a.assert(as)
}

func TestPasskeyAlgorithms(t *testing.T) {
	for _, alg := range []string{AlgES256, AlgEdDSA, AlgRS256} {
		a := newAuthenticator(t, alg)
		if err := a.key.Validate(); err != nil {
			t.Fatalf("%s: validate: %v", alg, err)
		}
		p := a.prove(testRP, sign.PurposeClientCert, []byte("statement"), flagUP|flagUV, testRP.Origins[0])
		if err := Verify(p, a.key, testRP, sign.PurposeClientCert, []byte("statement")); err != nil {
			t.Errorf("%s: %v", alg, err)
		}
	}
}

func TestPasskeyRejects(t *testing.T) {
	a := newAuthenticator(t, AlgES256)
	payload := []byte("statement")
	cases := []struct {
		name string
		p    Proof
		want error
	}{
		{"no user verification", a.prove(testRP, sign.PurposeClientCert, payload, flagUP, testRP.Origins[0]), ErrNoUV},
		{"foreign origin", a.prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, "https://evil.example"), ErrClientData},
		{"other purpose", a.prove(testRP, sign.PurposeTask, payload, flagUP|flagUV, testRP.Origins[0]), ErrClientData},
		{"other rp", a.prove(RP{ID: "evil.example", Origins: testRP.Origins}, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0]), ErrAuthData},
	}
	for _, c := range cases {
		if err := Verify(c.p, a.key, testRP, sign.PurposeClientCert, payload); err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	good := a.prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0])
	good.Sig[len(good.Sig)-1] ^= 1
	if err := Verify(good, a.key, testRP, sign.PurposeClientCert, payload); err != ErrSignature {
		t.Errorf("tampered signature: %v", err)
	}
}

func testKDF() KDF {
	salt := make([]byte, SaltSize)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	return KDF{Alg: KDFArgon2id, Salt: salt, MemKiB: DefaultMemKiB, Time: DefaultTime, Par: DefaultPar}
}

func passwordKey(t *testing.T, password string) (ed25519.PrivateKey, UserKey) {
	t.Helper()
	k := testKDF()
	priv, err := DeriveKey([]byte(password), k)
	if err != nil {
		t.Fatal(err)
	}
	pub := []byte(priv.Public().(ed25519.PublicKey))
	return priv, UserKey{ID: sign.KeyID(pub), Kind: KindPassword, Alg: AlgEdDSA, Public: pub, KDF: &k}
}

func TestPasswordKey(t *testing.T) {
	priv, key := passwordKey(t, "Heimnetz-2026")
	if err := key.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := []byte("statement")
	p := Proof{Kind: KindPassword, KeyID: key.ID, Sig: ed25519.Sign(priv, sign.Message(sign.PurposeClientCert, payload))}
	if err := Verify(p, key, testRP, sign.PurposeClientCert, payload); err != nil {
		t.Fatal(err)
	}
	if err := Verify(p, key, testRP, sign.PurposeTask, payload); err != ErrSignature {
		t.Errorf("other purpose: %v", err)
	}
	weak := testKDF()
	weak.MemKiB = 1024
	if _, err := DeriveKey([]byte("x"), weak); err != ErrKDF {
		t.Errorf("weak KDF: %v", err)
	}
}

func TestVerifyKeyIDMismatch(t *testing.T) {
	priv1, key1 := passwordKey(t, "Heimnetz-2026")
	_, key2 := passwordKey(t, "Käse&Brot")
	payload := []byte("statement")
	// A genuine signature by key1, but the UserKey handed to Verify claims
	// key2's ID while still carrying key1's Public — a foreign key answering
	// for another key's identity. Must be rejected before the signature (which
	// would otherwise genuinely verify) is even checked.
	sig := ed25519.Sign(priv1, sign.Message(sign.PurposeClientCert, payload))
	relabeled := key1
	relabeled.ID = key2.ID
	p := Proof{Kind: KindPassword, KeyID: key2.ID, Sig: sig}
	if err := Verify(p, relabeled, testRP, sign.PurposeClientCert, payload); err != ErrKey {
		t.Errorf("key ID mismatch: got %v, want %v", err, ErrKey)
	}
}

func TestUserKeyPublicTooLarge(t *testing.T) {
	a := newAuthenticator(t, AlgES256)
	oversized := make([]byte, MaxPublic+1)
	key := a.key
	key.Public = oversized
	key.ID = sign.KeyID(oversized) // keep ID self-consistent: isolate the size check
	if err := key.Validate(); err != ErrKey {
		t.Errorf("oversized public key: got %v, want %v", err, ErrKey)
	}
}

func TestVerifyPublicTooLarge(t *testing.T) {
	// TestUserKeyPublicTooLarge only exercises Validate's own explicit MaxPublic
	// check, which short-circuits before Validate ever calls parsePublic. This
	// test proves the second enforcement point — inside parsePublic itself —
	// independently, since that's the one protecting Verify when a caller skips
	// Validate (the whole reason for enforcing the cap in both places).
	a := newAuthenticator(t, AlgES256)
	payload := []byte("statement")
	p := a.prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0])
	oversized := make([]byte, MaxPublic+1)
	key := a.key
	key.Public = oversized
	key.ID = sign.KeyID(oversized) // keep ID self-consistent: isolate the size check
	p.KeyID = key.ID
	if err := Verify(p, key, testRP, sign.PurposeClientCert, payload); err != ErrKey {
		t.Errorf("oversized public key via Verify: got %v, want %v", err, ErrKey)
	}
}

func TestVerifySignatureTooLarge(t *testing.T) {
	priv, key := passwordKey(t, "Heimnetz-2026")
	payload := []byte("statement")
	sig := ed25519.Sign(priv, sign.Message(sign.PurposeClientCert, payload))
	p := Proof{Kind: KindPassword, KeyID: key.ID, Sig: append(sig, make([]byte, MaxSig)...)}
	if err := Verify(p, key, testRP, sign.PurposeClientCert, payload); err != ErrSignature {
		t.Errorf("oversized signature: got %v, want %v", err, ErrSignature)
	}
}

func TestUserKeyRSAModulusTooLarge(t *testing.T) {
	n := new(big.Int).Lsh(big.NewInt(1), MaxRSABits) // BitLen() == MaxRSABits+1
	spki, err := x509.MarshalPKIXPublicKey(&rsa.PublicKey{N: n, E: 65537})
	if err != nil {
		t.Fatal(err)
	}
	key := UserKey{ID: sign.KeyID(spki), Kind: KindPasskey, Alg: AlgRS256, Public: spki, CredID: []byte("credential-1")}
	if err := key.Validate(); err != ErrKey {
		t.Errorf("oversized RSA modulus: got %v, want %v", err, ErrKey)
	}
}

func TestProofValidate(t *testing.T) {
	payload := []byte("statement")
	// A password proof straight from an Ed25519 key: Validate looks at the
	// structure only, so the slow Argon2id derivation is not needed.
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	password := Proof{Kind: KindPassword, KeyID: sign.KeyID(priv.Public().(ed25519.PublicKey)),
		Sig: ed25519.Sign(priv, sign.Message(sign.PurposeClientCert, payload))}
	passkey := newAuthenticator(t, AlgES256).prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0])
	cases := []struct {
		name   string
		base   Proof
		mutate func(p *Proof)
		valid  bool
	}{
		{"valid password proof", password, func(*Proof) {}, true},
		{"valid passkey proof", passkey, func(*Proof) {}, true},
		{"unknown kind", password, func(p *Proof) { p.Kind = "otp" }, false},
		{"key ID not 16 bytes", password, func(p *Proof) { p.KeyID = enc.B64.EncodeToString(make([]byte, 15)) }, false},
		{"empty signature", password, func(p *Proof) { p.Sig = nil }, false},
		{"signature of MaxSig bytes", password, func(p *Proof) { p.Sig = make([]byte, MaxSig) }, true},
		{"signature over MaxSig", password, func(p *Proof) { p.Sig = make([]byte, MaxSig+1) }, false},
		{"password proof with authData", password, func(p *Proof) { p.AuthData = passkey.AuthData }, false},
		{"password proof with clientData", password, func(p *Proof) { p.ClientData = passkey.ClientData }, false},
		{"passkey authData of 36 bytes", passkey, func(p *Proof) { p.AuthData = p.AuthData[:36] }, false},
		{"passkey authData of MaxAuthData bytes", passkey, func(p *Proof) { p.AuthData = make([]byte, MaxAuthData) }, true},
		{"passkey authData over MaxAuthData", passkey, func(p *Proof) { p.AuthData = make([]byte, MaxAuthData+1) }, false},
		{"passkey without clientData", passkey, func(p *Proof) { p.ClientData = nil }, false},
		{"passkey clientData of MaxClientData bytes", passkey, func(p *Proof) { p.ClientData = make([]byte, MaxClientData) }, true},
		{"passkey clientData over MaxClientData", passkey, func(p *Proof) { p.ClientData = make([]byte, MaxClientData+1) }, false},
	}
	for _, c := range cases {
		p := c.base
		c.mutate(&p)
		if err := p.Validate(); (err == nil) != c.valid || (err != nil && err != ErrProof) {
			t.Errorf("%s: valid=%v, got %v", c.name, c.valid, err)
		}
	}
}

type kdfVector struct {
	Password string    `json:"password"`
	KDF      KDF       `json:"kdf"`
	Seed     enc.Bytes `json:"seed"`
	Public   enc.Bytes `json:"public"`
}

func TestKDFVectors(t *testing.T) {
	var vs []kdfVector
	for _, pw := range []string{"Heimnetz-2026", "Käse&Brot"} {
		priv, _ := passwordKey(t, pw)
		vs = append(vs, kdfVector{Password: pw, KDF: testKDF(), Seed: priv.Seed(), Public: []byte(priv.Public().(ed25519.PublicKey))})
	}
	golden.Check(t, *update, "proof_kdf.json", vs)
}

type proofVector struct {
	Name    string    `json:"name"`
	Purpose string    `json:"purpose"`
	Payload enc.Bytes `json:"payload"`
	Key     UserKey   `json:"key"`
	Proof   Proof     `json:"proof"`
	Valid   bool      `json:"valid"`
}

type proofVectors struct {
	RP     RP            `json:"rp"`
	Proofs []proofVector `json:"proofs"`
}

// proofErrors names the error each invalid vector must fail with, so that a
// vector cannot pass by failing for another rule than the one it breaks.
var proofErrors = map[string]error{
	"passkey ES256 without PIN":                    ErrNoUV,
	"passkey EdDSA without PIN":                    ErrNoUV,
	"passkey RS256 without PIN":                    ErrNoUV,
	"passkey ES256, challenge for another purpose": ErrClientData,
	"passkey ES256, challenge for another payload": ErrClientData,
	"passkey ES256, foreign origin":                ErrClientData,
	"passkey ES256, cross-origin":                  ErrClientData,
	"passkey ES256, foreign rpIdHash":              ErrAuthData,
	"passkey ES256, type webauthn.create":          ErrClientData,
	"passkey ES256, user not present":              ErrNoUV,
	"passkey ES256, key ID mismatch":               ErrKeyMismatch,
	"passkey ES256, flipped signature bit":         ErrSignature,
	"passkey ES256, authData of 36 bytes":          ErrAuthData,
	"passkey RS256, 1024-bit key":                  ErrKey,
	"password key, signature for another purpose":  ErrSignature,
	"password key, proof claims kind passkey":      ErrKeyMismatch,
}

// Passkey test keys are random and ECDSA signatures are randomized, so this
// golden file is written once with -update and afterwards only verified.
func TestProofVectors(t *testing.T) {
	if *update {
		writeProofVectors(t)
	}
	var vs proofVectors
	golden.Load(t, "proof.json", &vs)
	for _, v := range vs.Proofs {
		err := Verify(v.Proof, v.Key, vs.RP, v.Purpose, v.Payload)
		if (err == nil) != v.Valid {
			t.Errorf("%s: valid=%v, got %v", v.Name, v.Valid, err)
		}
		if v.Valid {
			if err := v.Proof.Validate(); err != nil {
				t.Errorf("%s: structure: %v", v.Name, err)
			}
		} else if want, ok := proofErrors[v.Name]; !ok || err != want {
			t.Errorf("%s: got %v, want %v", v.Name, err, want)
		}
	}
}

func writeProofVectors(t *testing.T) {
	payload := []byte(`{"v":1}`)
	vs := proofVectors{RP: testRP}
	auth := map[string]authenticator{}
	for _, alg := range []string{AlgES256, AlgEdDSA, AlgRS256} {
		a := newAuthenticator(t, alg)
		auth[alg] = a
		vs.Proofs = append(vs.Proofs,
			proofVector{Name: "passkey " + alg, Purpose: sign.PurposeClientCert, Payload: payload, Key: a.key,
				Proof: a.prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0]), Valid: true},
			proofVector{Name: "passkey " + alg + " without PIN", Purpose: sign.PurposeClientCert, Payload: payload, Key: a.key,
				Proof: a.prove(testRP, sign.PurposeClientCert, payload, flagUP, testRP.Origins[0]), Valid: false})
	}
	priv, key := passwordKey(t, "Heimnetz-2026")
	passwordSig := ed25519.Sign(priv, sign.Message(sign.PurposeClientCert, payload))
	vs.Proofs = append(vs.Proofs, proofVector{Name: "password key", Purpose: sign.PurposeClientCert, Payload: payload, Key: key,
		Proof: Proof{Kind: KindPassword, KeyID: key.ID, Sig: passwordSig}, Valid: true})

	// Each case below starts from a valid proof over the same purpose and
	// payload and breaks exactly one rule (proofErrors says which).
	invalid := func(name string, key UserKey, p Proof) {
		vs.Proofs = append(vs.Proofs, proofVector{Name: name, Purpose: sign.PurposeClientCert, Payload: payload, Key: key, Proof: p})
	}
	es := auth[AlgES256]
	with := func(change func(as *assertion)) Proof {
		as := assertionFor(sign.PurposeClientCert, payload)
		change(&as)
		return es.assert(as)
	}
	invalid("passkey ES256, challenge for another purpose", es.key,
		with(func(as *assertion) { as.challenge = Challenge(sign.PurposeTask, payload) }))
	invalid("passkey ES256, challenge for another payload", es.key,
		with(func(as *assertion) { as.challenge = Challenge(sign.PurposeClientCert, []byte(`{"v":2}`)) }))
	invalid("passkey ES256, foreign origin", es.key, with(func(as *assertion) { as.origin = "https://evil.example" }))
	invalid("passkey ES256, cross-origin", es.key, with(func(as *assertion) { as.crossOrigin = true }))
	invalid("passkey ES256, foreign rpIdHash", es.key, with(func(as *assertion) { as.rpID = "evil.example" }))
	invalid("passkey ES256, type webauthn.create", es.key, with(func(as *assertion) { as.typ = "webauthn.create" }))
	invalid("passkey ES256, user not present", es.key, with(func(as *assertion) { as.flags = flagUV }))
	mismatch := with(func(*assertion) {})
	mismatch.KeyID = auth[AlgEdDSA].key.ID
	invalid("passkey ES256, key ID mismatch", es.key, mismatch)
	flipped := with(func(*assertion) {})
	flipped.Sig[len(flipped.Sig)-1] ^= 1
	invalid("passkey ES256, flipped signature bit", es.key, flipped)
	invalid("passkey ES256, authData of 36 bytes", es.key, with(func(as *assertion) { as.authDataLen = 36 }))
	weak := newAuthenticatorBits(t, AlgRS256, 1024)
	invalid("passkey RS256, 1024-bit key", weak.key, weak.assert(assertionFor(sign.PurposeClientCert, payload)))
	invalid("password key, signature for another purpose", key,
		Proof{Kind: KindPassword, KeyID: key.ID, Sig: ed25519.Sign(priv, sign.Message(sign.PurposeTask, payload))})
	invalid("password key, proof claims kind passkey", key, Proof{Kind: KindPasskey, KeyID: key.ID, Sig: passwordSig})
	golden.Check(t, true, "proof.json", vs)
}

func FuzzVerifyPasskey(f *testing.F) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		f.Fatal(err)
	}
	key := UserKey{ID: sign.KeyID(spki), Kind: KindPasskey, Alg: AlgES256, Public: spki, CredID: []byte("c")}
	f.Add([]byte(`{"type":"webauthn.get"}`), make([]byte, 37), []byte("sig"))
	f.Fuzz(func(t *testing.T, clientData, authData, sig []byte) {
		_ = Verify(Proof{Kind: KindPasskey, KeyID: key.ID, Sig: sig, AuthData: authData, ClientData: clientData},
			key, testRP, sign.PurposeClientCert, []byte("p"))
	})
}
