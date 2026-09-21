package session

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"runtime"
	"strings"
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
	// carol's role asks for the password too; she holds a passkey and a
	// password key.
	carol                       string
	carolPasskey, carolPassword trusttest.Signer
}

// newFixture signs everything with the password admin. trusttest.Genesis
// sorts its two admins by ID, which puts the passkey admin ("admin2") at
// Users[0] and the password admin passed in here (trusttest.NewSigner(1),
// "admin") at Users[1] — see trust_test.go's chain comment. On top of
// Genesis the list has a second API node, api-2, so that a certificate naming
// it is refused for naming another node than its signer and not for naming
// an unknown one; and carol (Users[2]) in the role "careful", which asks for
// the password too.
func newFixture(t testing.TB) fixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	api2DH, api2Sign := trusttest.NodeKeys(0xA2)
	l.Nodes = []trust.Node{l.Nodes[0], {ID: "api-2", Kind: trust.NodeAPI, Name: "API 2", Noise: api2DH.Public,
		Sign: enc.Bytes(api2Sign.Public().(ed25519.PublicKey)), URL: "wss://api-2.test:7443/tunnel"}, l.Nodes[1]}
	carol, carolPasskey, carolPassword := trusttest.ID(0x23), trusttest.NewPasskey(0x63), trusttest.NewSigner(0x64)
	l.Roles = append(l.Roles, trust.Role{Name: "careful", Level: trust.LevelCritical, PasswordToo: true})
	l.Users = append(l.Users, trust.User{ID: carol, Name: "carol", Roles: []string{"careful"},
		Keys: []proof.UserKey{carolPasskey.Key, carolPassword.Key}})
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
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
	return fixture{list: l, admin: admin, client: client, proof: admin.Prove(sign.PurposeClientCert, client), api: env,
		carol: carol, carolPasskey: carolPasskey, carolPassword: carolPassword}
}

// clientCert decodes the fixture's client certificate for a test to change.
func (f fixture) clientCert(t testing.TB) ClientCert {
	t.Helper()
	var c ClientCert
	if err := json.Unmarshal(f.client, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// clientFor returns the fixture's client certificate for another user.
func (f fixture) clientFor(t testing.TB, user string) []byte {
	t.Helper()
	c := f.clientCert(t)
	c.User = user
	return mustJSON(t, c)
}

// resignAPI returns the fixture's API certificate with one change, signed
// again with the key of api-1, so the signature check does not fire before
// the check under test does.
func (f fixture) resignAPI(t testing.TB, mutate func(*APICert)) sign.Envelope {
	t.Helper()
	var m APICert
	if err := json.Unmarshal(f.api.Payload, &m); err != nil {
		t.Fatal(err)
	}
	mutate(&m)
	_, nodeSign := trusttest.NodeKeys(0xA1) // key of "api-1" in trusttest.Genesis
	env, err := SignAPI(nodeSign, m)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestVerify(t *testing.T) {
	f := newFixture(t)
	v, err := VerifyClient(f.client, f.proof, f.list, now)
	if err != nil || v.Cert().User != f.list.Users[1].ID || !bytes.Equal(v.Payload(), f.client) {
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
	// The API certificate is bound to these exact bytes, so only the strict
	// decoding of the client certificate can refuse its duplicate member.
	dup := withMember(f.client, `"inst":"`+f.list.Installation+`"`)
	if _, err := VerifyAPI(f.resignAPI(t, func(m *APICert) { m.Client = CertHash(dup) }), f.list, dup, now); err != ErrCert {
		t.Errorf("client certificate with a duplicate member: %v", err)
	}

	// Each case changes exactly one field of a freshly signed API
	// certificate and re-signs it, so the signature check does not fire
	// before the field check under test does.
	clientExpires := f.clientCert(t).Expires
	apiCase := func(name string, mutate func(*APICert), want error) {
		t.Helper()
		if _, err := VerifyAPI(f.resignAPI(t, mutate), f.list, f.client, now); err != want {
			t.Errorf("%s: got %v, want %v", name, err, want)
		}
	}
	apiCase("other installation", func(m *APICert) { m.Installation = trusttest.ID(0x12) }, ErrCert)
	// api-2 is an API node of the list too, but api-1 signed.
	apiCase("other node name", func(m *APICert) { m.Node = "api-2" }, ErrCert)
	apiCase("DH key of 31 bytes", func(m *APICert) { m.DH = bytes.Repeat([]byte{0x99}, 31) }, ErrCert)
	// Moving Issued back instead of Expires on keeps the certificate within
	// the client certificate's lifetime, so only MaxLifetime is broken.
	apiCase("longer than MaxLifetime", func(m *APICert) { m.Issued = m.Expires - MaxLifetime - 1 }, ErrCert)
	apiCase("outlives the client certificate", func(m *APICert) { m.Expires = clientExpires + 1 }, ErrCert)
	apiCase("expired at now", func(m *APICert) { m.Expires = now }, ErrExpired)
	apiCase("not yet valid at now", func(m *APICert) { m.Issued = now + MaxSkew + 1 }, ErrExpired)
}

func TestVerifyPassword(t *testing.T) {
	f := newFixture(t)
	// admin2 (Users[0]) logs in with her passkey and holds a password key too.
	passkey := trusttest.NewPasskey(trusttest.GenesisPasskeyFill)
	password := trusttest.NewSigner(0x62)
	f.list.Users[0].Keys = append(f.list.Users[0].Keys, password.Key)
	if err := f.list.Validate(); err != nil {
		t.Fatal(err)
	}
	c := f.clientCert(t)
	c.User = f.list.Users[0].ID
	client, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	v, err := VerifyClient(client, passkey.Prove(sign.PurposeClientCert, client), f.list, now)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		p    proof.Proof
		want error
	}{
		{"password key of the user", password.Prove(sign.PurposeClientCert, client), nil},
		{"passkey instead of a password key", passkey.Prove(sign.PurposeClientCert, client), ErrUser},
		{"password key of another user", f.admin.Prove(sign.PurposeClientCert, client), ErrUser},
		{"other purpose", password.Prove(sign.PurposeTask, client), proof.ErrSignature},
	}
	for _, tc := range cases {
		if err := VerifyPassword(v, tc.p, f.list); err != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

// TestPasswordTooNeedsPasskey: for a role that asks for the password too,
// the password key is the second proof, never the only one.
func TestPasswordTooNeedsPasskey(t *testing.T) {
	f := newFixture(t)
	client := f.clientFor(t, f.carol)
	byPasskey := f.carolPasskey.Prove(sign.PurposeClientCert, client)
	byPassword := f.carolPassword.Prove(sign.PurposeClientCert, client)
	v, err := VerifyClient(client, byPasskey, f.list, now)
	if err != nil {
		t.Fatalf("passkey, then password: %v", err)
	}
	if err := VerifyPassword(v, byPassword, f.list); err != nil {
		t.Errorf("passkey, then password: %v", err)
	}
	if _, err := VerifyClient(client, byPassword, f.list, now); err != ErrUser {
		t.Errorf("password key alone for a password-too role: got %v, want %v", err, ErrUser)
	}
	// The probe of the review: the password proof twice. It parses, but the
	// first proof is refused.
	body := mustJSON(t, LoginFinish{Cert: client, Proof: byPassword, Password: &byPassword})
	lf, err := ParseLoginFinish(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyClient(lf.Cert, lf.Proof, f.list, now); err != ErrUser {
		t.Errorf("password proof as both proofs: got %v, want %v", err, ErrUser)
	}
}

func TestVerifySetup(t *testing.T) {
	admin := trusttest.NewSigner(1)
	genesis := trusttest.Genesis(admin)
	global := genesis.Stores[0]
	info := SetupInfo{Installation: genesis.Installation, Nodes: genesis.Nodes, Now: issued, Store: &global}
	setup := func(s trust.Signed) Setup { return Setup{Code: "ABCDE-FGHJK-LMNPQ-RSTUV", Trust: s} }
	signed := func(mutate func(l *trust.List)) trust.Signed {
		l := trusttest.Genesis(admin)
		mutate(l)
		return admin.Sign(t, l)
	}
	good := signed(func(*trust.List) {})
	alice := trusttest.NewSigner(2)
	withAlice := trusttest.Genesis(admin)
	withAlice.Roles = append(withAlice.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	withAlice.Users = append(withAlice.Users,
		trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"}, Keys: []proof.UserKey{alice.Key}})
	cases := []struct {
		name string
		s    trust.Signed
		want error
	}{
		{"genesis as offered", good, nil},
		{"global store switched off", signed(func(l *trust.List) { l.Stores = nil }), nil},
		{"other installation", signed(func(l *trust.List) { l.Installation = trusttest.ID(0x12) }), ErrSetup},
		{"API node missing", signed(func(l *trust.List) { l.Nodes = l.Nodes[1:] }), ErrSetup},
		{"API node with another URL", signed(func(l *trust.List) { l.Nodes[0].URL = "wss://api-9.test:7443/tunnel" }), ErrSetup},
		{"global store with another key", signed(func(l *trust.List) { l.Stores[0].Key = bytes.Repeat([]byte{9}, 32) }), ErrSetup},
		{"extra store", signed(func(l *trust.List) {
			l.Stores = append(l.Stores, trust.Store{ID: "other", Name: "Other", URL: "https://other.example", Key: bytes.Repeat([]byte{9}, 32)})
		}), ErrSetup},
		{"signed by a non-admin", alice.Sign(t, withAlice), trust.ErrSigner},
		{"payload is version 2", admin.Sign(t, trusttest.Next(t, good)), trust.ErrAnchor},
	}
	for _, c := range cases {
		v, err := VerifySetup(setup(c.s), info)
		if err != c.want || (err == nil && (v.List.Seq != 1 || !bytes.Equal(v.Hash, trust.Hash(c.s)))) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	noStore := info
	noStore.Store = nil
	if _, err := VerifySetup(setup(good), noStore); err != ErrSetup {
		t.Errorf("store the API did not offer: got %v, want %v", err, ErrSetup)
	}
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

// withMember appends a member to the JSON object b, for cases a Go struct
// cannot produce: unknown and duplicate members.
func withMember(b []byte, member string) []byte {
	out := append([]byte{}, b[:len(b)-1]...)
	return append(append(out, ','), member+"}"...)
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// parseCase is one body for a parser: valid, or breaking exactly one rule.
type parseCase struct {
	name  string
	body  []byte
	valid bool
}

func checkParse(t *testing.T, parse func([]byte) error, cases []parseCase) {
	t.Helper()
	for _, c := range cases {
		err := parse(c.body)
		if (err == nil) != c.valid || (err != nil && err != ErrMessage) {
			t.Errorf("%s: valid=%v, got %v", c.name, c.valid, err)
		}
	}
}

func TestParseLoginStart(t *testing.T) {
	start := func(name string) []byte { return mustJSON(t, LoginStart{Name: name}) }
	checkParse(t, func(b []byte) error { _, err := ParseLoginStart(b); return err }, []parseCase{
		{"valid", start("alice.m-2_x"), true},
		{"name of 64 characters", start(strings.Repeat("a", 64)), true},
		{"empty name", start(""), false},
		{"name of 65 characters", start(strings.Repeat("a", 65)), false},
		{"capital letter", start("Alice"), false},
		{"first character a dot", start(".alice"), false},
		{"unknown field", withMember(start("alice"), `"pw":"x"`), false},
		{"duplicate field", withMember(start("alice"), `"name":"bob"`), false},
		{"larger than MaxRequest", append(start("alice"), bytes.Repeat([]byte(" "), MaxRequest)...), false},
	})
}

func TestParseLoginFinish(t *testing.T) {
	f := newFixture(t)
	passkey := trusttest.NewPasskey(trusttest.GenesisPasskeyFill).Prove(sign.PurposeClientCert, f.client)
	password := f.proof
	base := LoginFinish{Cert: f.client, Proof: passkey}
	finish := func(mutate func(*LoginFinish)) []byte {
		lf := base
		mutate(&lf)
		return mustJSON(t, lf)
	}
	checkParse(t, func(b []byte) error { _, err := ParseLoginFinish(b); return err }, []parseCase{
		{"valid", finish(func(*LoginFinish) {}), true},
		{"valid with password and TOTP", finish(func(lf *LoginFinish) { lf.Password = &password; lf.TOTP = "012345" }), true},
		{"cert of MaxCert bytes", finish(func(lf *LoginFinish) { lf.Cert = make([]byte, MaxCert) }), true},
		{"empty cert", finish(func(lf *LoginFinish) { lf.Cert = nil }), false},
		{"cert over MaxCert", finish(func(lf *LoginFinish) { lf.Cert = make([]byte, MaxCert+1) }), false},
		{"malformed proof", finish(func(lf *LoginFinish) { lf.Proof.Sig = nil }), false},
		{"password proof by a passkey", finish(func(lf *LoginFinish) { lf.Password = &passkey }), false},
		{"malformed password proof", finish(func(lf *LoginFinish) {
			p := password
			p.Sig = nil
			lf.Password = &p
		}), false},
		{"TOTP of five digits", finish(func(lf *LoginFinish) { lf.TOTP = "01234" }), false},
		{"unknown field", withMember(finish(func(*LoginFinish) {}), `"remember":true`), false},
		{"duplicate field", withMember(finish(func(*LoginFinish) {}), `"cert":"AA"`), false},
		{"field name in other case", withMember(finish(func(*LoginFinish) {}), `"TOTP":"012345"`), false},
	})
}

func TestParseSetup(t *testing.T) {
	admin := trusttest.NewSigner(1)
	base := Setup{Code: "ABCDE-FGHJK-LMNPQ-RSTUV", Trust: admin.Sign(t, trusttest.Genesis(admin))}
	setup := func(mutate func(*Setup)) []byte {
		s := base
		mutate(&s)
		return mustJSON(t, s)
	}
	checkParse(t, func(b []byte) error { _, err := ParseSetup(b); return err }, []parseCase{
		{"valid", setup(func(*Setup) {}), true},
		{"code in lower case", setup(func(s *Setup) { s.Code = strings.ToLower(s.Code) }), false},
		{"empty payload", setup(func(s *Setup) { s.Trust.Payload = nil }), false},
		{"malformed proof", setup(func(s *Setup) { s.Trust.Proof.KeyID = "admin" }), false},
		{"unknown field", withMember(setup(func(*Setup) {}), `"admin":"x"`), false},
		{"larger than MaxRequest", append(setup(func(*Setup) {}), bytes.Repeat([]byte(" "), MaxRequest)...), false},
	})
}

func TestParseTrustChain(t *testing.T) {
	admin := trusttest.NewSigner(1)
	g := admin.Sign(t, trusttest.Genesis(admin))
	v2 := admin.Sign(t, trusttest.Next(t, g))
	chain := func(mutate func(*TrustChain)) []byte {
		c := TrustChain{Versions: []trust.Signed{g, v2}}
		mutate(&c)
		return mustJSON(t, c)
	}
	repeat := func(n int) []trust.Signed {
		vs := make([]trust.Signed, n)
		for i := range vs {
			vs[i] = g
		}
		return vs
	}
	checkParse(t, func(b []byte) error { _, err := ParseTrustChain(b); return err }, []parseCase{
		{"valid", chain(func(*TrustChain) {}), true},
		{"valid with more", chain(func(c *TrustChain) { c.More = true }), true},
		{"MaxChainVersions versions", chain(func(c *TrustChain) { c.Versions = repeat(MaxChainVersions) }), true},
		{"no versions", chain(func(c *TrustChain) { c.Versions = []trust.Signed{} }), false},
		{"versions null", chain(func(c *TrustChain) { c.Versions = nil }), false},
		{"more than MaxChainVersions", chain(func(c *TrustChain) { c.Versions = repeat(MaxChainVersions + 1) }), false},
		{"empty payload", chain(func(c *TrustChain) { c.Versions[1].Payload = nil }), false},
		{"payload over sign.MaxPayload", chain(func(c *TrustChain) { c.Versions[1].Payload = make([]byte, sign.MaxPayload+1) }), false},
		{"malformed proof", chain(func(c *TrustChain) { c.Versions[1].Proof.Kind = "otp" }), false},
		{"unknown field", withMember(chain(func(*TrustChain) {}), `"from":1`), false},
		{"larger than MaxChain", append(chain(func(*TrustChain) {}), bytes.Repeat([]byte(" "), MaxChain)...), false},
	})
}

func TestParseTrustHint(t *testing.T) {
	checkParse(t, func(b []byte) error { _, err := ParseTrustHint(b); return err }, []parseCase{
		{"valid", []byte(`{"seq":4}`), true},
		{"largest seq", []byte(`{"seq":9007199254740991}`), true},
		{"seq beyond MaxSafeInteger", []byte(`{"seq":9007199254740992}`), false},
		{"seq 0", []byte(`{"seq":0}`), false},
		{"no seq", []byte(`{}`), false},
		{"negative seq", []byte(`{"seq":-1}`), false},
		{"unknown field", []byte(`{"seq":4,"hash":"AA"}`), false},
		{"64 bytes", append([]byte(`{"seq":4}`), bytes.Repeat([]byte(" "), 55)...), true},
		{"larger than 64 bytes", append([]byte(`{"seq":4}`), bytes.Repeat([]byte(" "), 56)...), false},
	})
}

// apiReject is an API certificate VerifyAPI must refuse, for the reason its
// name gives.
type apiReject struct {
	Name  string        `json:"name"`
	API   sign.Envelope `json:"api"`
	Valid bool          `json:"valid"`
	want  error         // the error VerifyAPI must return; not in the file
}

// apiRejects changes one thing of the fixture's API certificate per case and
// signs it again with the key of api-1, unless the name says otherwise.
func apiRejects(t testing.TB, f fixture) []apiReject {
	_, rogue := trusttest.NodeKeys(0x77)
	byRogue, err := sign.Sign(rogue, sign.PurposeAPICert, f.api.Payload)
	if err != nil {
		t.Fatal(err)
	}
	_, nodeSign := trusttest.NodeKeys(0xA1) // key of "api-1" in trusttest.Genesis
	otherPurpose, err := sign.Sign(nodeSign, sign.PurposeClientCert, f.api.Payload)
	if err != nil {
		t.Fatal(err)
	}
	c := f.clientCert(t)
	other := c
	other.Nonce = bytes.Repeat([]byte{0x55}, NonceSize)
	otherClient := mustJSON(t, other)
	return []apiReject{
		{Name: "signed by a key that is no API node", API: byRogue, want: ErrCert},
		{Name: "signed for another purpose", API: otherPurpose, want: sign.ErrBadSignature},
		{Name: "other installation", API: f.resignAPI(t, func(m *APICert) { m.Installation = trusttest.ID(0x12) }), want: ErrCert},
		{Name: "other node name", API: f.resignAPI(t, func(m *APICert) { m.Node = "api-2" }), want: ErrCert},
		{Name: "bound to another client certificate", API: f.resignAPI(t, func(m *APICert) { m.Client = CertHash(otherClient) }), want: ErrCert},
		{Name: "DH key of 31 bytes", API: f.resignAPI(t, func(m *APICert) { m.DH = m.DH[:31] }), want: ErrCert},
		{Name: "longer than MaxLifetime", API: f.resignAPI(t, func(m *APICert) { m.Issued = m.Expires - MaxLifetime - 1 }), want: ErrCert},
		{Name: "outlives the client certificate", API: f.resignAPI(t, func(m *APICert) { m.Expires = c.Expires + 1 }), want: ErrCert},
		{Name: "expired at now", API: f.resignAPI(t, func(m *APICert) { m.Expires = now }), want: ErrExpired},
		{Name: "not yet valid at now", API: f.resignAPI(t, func(m *APICert) { m.Issued = now + MaxSkew + 1 }), want: ErrExpired},
	}
}

// clientReject is a client certificate with a proof that VerifyClient must
// refuse against the vector's trust list at its now, for the reason its name
// gives.
type clientReject struct {
	Name   string      `json:"name"`
	Client enc.Bytes   `json:"client"`
	Proof  proof.Proof `json:"proof"`
	Valid  bool        `json:"valid"`
	want   error       // the error VerifyClient must return; not in the file
}

// clientRejects: carol's role asks for the password too, so her certificate
// signed by her password key alone is refused, while the same certificate
// signed by her passkey passes (TestPasswordTooNeedsPasskey).
func clientRejects(t testing.TB, f fixture) []clientReject {
	client := f.clientFor(t, f.carol)
	return []clientReject{
		{Name: "password key alone for a password-too role", Client: client,
			Proof: f.carolPassword.Prove(sign.PurposeClientCert, client), want: ErrUser},
	}
}

type vector struct {
	Trust         trust.Signed   `json:"trust"`
	Now           int64          `json:"now"`
	Client        enc.Bytes      `json:"client"`
	Proof         proof.Proof    `json:"proof"`
	API           sign.Envelope  `json:"api"`
	APIRejects    []apiReject    `json:"apiRejects"`
	ClientRejects []clientReject `json:"clientRejects"`
}

func TestVectors(t *testing.T) {
	f := newFixture(t)
	rejects := apiRejects(t, f)
	for _, r := range rejects {
		if _, err := VerifyAPI(r.API, f.list, f.client, now); err != r.want {
			t.Errorf("%s: got %v, want %v", r.Name, err, r.want)
		}
	}
	clients := clientRejects(t, f)
	for _, r := range clients {
		if _, err := VerifyClient(r.Client, r.Proof, f.list, now); err != r.want {
			t.Errorf("%s: got %v, want %v", r.Name, err, r.want)
		}
	}
	golden.Check(t, *update, "session.json", vector{Trust: f.admin.Sign(t, f.list), Now: now, Client: f.client, Proof: f.proof, API: f.api,
		APIRejects: rejects, ClientRejects: clients})
}

// allocated returns how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// hostile returns prefix, then as many empty objects as fit, then suffix:
// exactly size bytes, the shape of the review's probes.
func hostile(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + "{}")
	for b.Len()+len(",{}")+len(suffix) <= size {
		b.WriteString(",{}")
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParsersBoundAllocation feeds each parser an array of empty objects at
// its full size limit, where its contract allows no array or a short one:
// refusing it must allocate next to nothing.
func TestParsersBoundAllocation(t *testing.T) {
	f := newFixture(t)
	_, nodeSign := trusttest.NodeKeys(0xA1) // key of "api-1" in trusttest.Genesis
	apiEnv, err := sign.Sign(nodeSign, sign.PurposeAPICert, hostile(`{"client":[`, `]}`, MaxCert))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		in    []byte
		parse func([]byte) error
		want  error
	}{
		{"ParseLoginStart", hostile(`{"name":[`, `]}`, MaxRequest),
			func(b []byte) error { _, err := ParseLoginStart(b); return err }, ErrMessage},
		{"ParseLoginFinish", hostile(`{"proof":{"sig":[`, `]}}`, MaxRequest),
			func(b []byte) error { _, err := ParseLoginFinish(b); return err }, ErrMessage},
		{"ParseSetup", hostile(`{"trust":{"payload":[`, `]}}`, MaxRequest),
			func(b []byte) error { _, err := ParseSetup(b); return err }, ErrMessage},
		{"ParseTrustChain", hostile(`{"versions":[`, `]}`, MaxChain),
			func(b []byte) error { _, err := ParseTrustChain(b); return err }, ErrMessage},
		{"ParseTrustHint", hostile(`{"seq":[`, `]}`, maxTrustHint),
			func(b []byte) error { _, err := ParseTrustHint(b); return err }, ErrMessage},
		{"VerifyClient", hostile(`{"inst":[`, `]}`, MaxCert),
			func(b []byte) error { _, err := VerifyClient(b, f.proof, f.list, now); return err }, ErrCert},
		{"VerifyAPI, client certificate", hostile(`{"dh":[`, `]}`, MaxCert),
			func(b []byte) error { _, err := VerifyAPI(f.api, f.list, b, now); return err }, ErrCert},
		{"VerifyAPI, API certificate", nil,
			func([]byte) error { _, err := VerifyAPI(apiEnv, f.list, f.client, now); return err }, ErrCert},
	}
	for _, c := range cases {
		var err error
		n := allocated(func() { err = c.parse(c.in) })
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
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

// FuzzParseLoginFinish also covers ParseLoginStart, the other login body.
func FuzzParseLoginFinish(f *testing.F) {
	fx := newFixture(f)
	f.Add(mustJSON(f, LoginFinish{Cert: fx.client, Proof: fx.proof, Password: &fx.proof, TOTP: "012345"}))
	f.Add(mustJSON(f, LoginStart{Name: "alice"}))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseLoginFinish(b)
		_, _ = ParseLoginStart(b)
	})
}

func FuzzParseSetup(f *testing.F) {
	admin := trusttest.NewSigner(1)
	f.Add(mustJSON(f, Setup{Code: "ABCDE-FGHJK-LMNPQ-RSTUV", Trust: admin.Sign(f, trusttest.Genesis(admin))}))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseSetup(b)
	})
}

// FuzzParseTrustChain also covers ParseTrustHint, the hint that leads to a
// TrustChain request.
func FuzzParseTrustChain(f *testing.F) {
	admin := trusttest.NewSigner(1)
	f.Add(mustJSON(f, TrustChain{Versions: []trust.Signed{admin.Sign(f, trusttest.Genesis(admin))}, More: true}))
	f.Add([]byte(`{"seq":4}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseTrustChain(b)
		_, _ = ParseTrustHint(b)
	})
}
