package session

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var update = flag.Bool("update", false, "rewrite shared/testdata")

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
	_, api2Pin := trusttest.APITLS(0xE2, "api-2.test")
	l.Nodes = []trust.Node{l.Nodes[0], {ID: "api-2", Kind: trust.NodeAPI, Name: "API 2", Noise: api2DH.Public,
		Sign: enc.Bytes(api2Sign.Public().(ed25519.PublicKey)), URL: "wss://api-2.test:7443/tunnel", TLS: api2Pin}, l.Nodes[1]}
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

// testSetupCode is the one-time code of the setup tests and vectors;
// otherSetupCode is another valid one.
const (
	testSetupCode  = "ABCDE-FGHJK-MNPQR-STUVW"
	otherSetupCode = "23456-789AB-CDEFG-HJKLM"
)

// setupList returns version 1 as the setup makes it from setupInfo:
// trusttest.Genesis without the controller, which only a pairing adds later.
func setupList(admin trusttest.Signer) *trust.List {
	l := trusttest.Genesis(admin)
	l.Nodes = l.Nodes[:1] // api-1
	return l
}

// setupInfo is what the API offers for setupList: its installation, its API
// node, the global store and the release key NodeKeys(ReleaseFill), which
// Genesis's standing approval for ngc-controller names.
func setupInfo() SetupInfo {
	l := setupList(trusttest.NewSigner(1))
	_, release := trusttest.NodeKeys(trusttest.ReleaseFill)
	global := l.Stores[0]
	return SetupInfo{Installation: l.Installation, Nodes: l.Nodes, Now: issued,
		ReleaseKey: enc.Bytes(release.Public().(ed25519.PublicKey)), Store: &global}
}

// api2Node returns a second API node, api-2 (NodeKeys(0xA2), TLS pin of
// APITLS(0xE2, "api-2.test")), which sorts after api-1.
func api2Node() trust.Node {
	dh, sig := trusttest.NodeKeys(0xA2)
	_, pin := trusttest.APITLS(0xE2, "api-2.test")
	return trust.Node{ID: "api-2", Kind: trust.NodeAPI, Name: "API 2", Noise: dh.Public,
		Sign: enc.Bytes(sig.Public().(ed25519.PublicKey)), URL: "wss://api-2.test:7443/tunnel", TLS: pin}
}

// versionOne builds version 1 from info as the package comment says the shell
// does: the admins of setupList, info's installation, time, API nodes and
// store, and standing approvals for the three NGC parts with info's release
// key and for system.
func versionOne(info SetupInfo) *trust.List {
	l := setupList(trusttest.NewSigner(1))
	l.Installation, l.Issued, l.Nodes, l.Stores = info.Installation, info.Now, info.Nodes, nil
	if info.Store != nil {
		l.Stores = []trust.Store{*info.Store}
	}
	l.Auto = []trust.Auto{{Target: trust.AutoAPI, Key: info.ReleaseKey}, {Target: trust.AutoController, Key: info.ReleaseKey},
		{Target: trust.AutoWeb, Key: info.ReleaseKey}, {Target: trust.AutoSystem}}
	return l
}

// controllerLike returns api-1 of setupInfo as a node of kind controller
// with the ID ctl-1. It has no root lists, so the budget of the info lets it
// through, and of trust.ValidAPINode it breaks only the kind.
func controllerLike() trust.Node {
	n := setupInfo().Nodes[0]
	n.ID, n.Kind = "ctl-1", trust.NodeController
	return n
}

// setupRequest returns the Setup for s with its MAC made with code.
func setupRequest(s trust.Signed, code string) Setup {
	return Setup{MAC: SetupMAC(code, s.Payload), Trust: s}
}

func TestVerifySetup(t *testing.T) {
	admin := trusttest.NewSigner(1)
	info := setupInfo()
	releaseKey := info.ReleaseKey
	_, other := trusttest.NodeKeys(0xD2)
	otherKey := enc.Bytes(other.Public().(ed25519.PublicKey))
	signed := func(mutate func(l *trust.List)) trust.Signed {
		l := setupList(admin)
		mutate(l)
		return admin.Sign(t, l)
	}
	good := signed(func(*trust.List) {})
	parts := signed(func(l *trust.List) {
		l.Auto = []trust.Auto{{Target: trust.AutoAPI, Key: releaseKey}, {Target: trust.AutoController, Key: releaseKey},
			{Target: trust.AutoWeb, Key: releaseKey}, {Target: trust.AutoSystem}}
	})
	alice := trusttest.NewSigner(2)
	withAlice := setupList(admin)
	withAlice.Roles = append(withAlice.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	withAlice.Users = append(withAlice.Users,
		trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"}, Keys: []proof.UserKey{alice.Key}})
	notAList := trust.Signed{Payload: []byte("not a trust list"), Proof: good.Proof}
	cases := []struct {
		name string
		s    Setup
		code string
		want error
	}{
		{"version 1 as offered", setupRequest(good, testSetupCode), testSetupCode, nil},
		{"global store switched off", setupRequest(signed(func(l *trust.List) { l.Stores = nil }), testSetupCode), testSetupCode, nil},
		{"approvals for the NGC parts and system", setupRequest(parts, testSetupCode), testSetupCode, nil},
		{"no standing approvals", setupRequest(signed(func(l *trust.List) { l.Auto = nil }), testSetupCode), testSetupCode, nil},

		// The code: checked before anything is decoded.
		{"MAC with another code", setupRequest(good, otherSetupCode), testSetupCode, ErrSetup},
		{"MAC over other bytes", Setup{MAC: SetupMAC(testSetupCode, parts.Payload), Trust: good}, testSetupCode, ErrSetup},
		{"offer MAC instead of the setup MAC", Setup{MAC: OfferMAC(testSetupCode, good.Payload), Trust: good}, testSetupCode, ErrSetup},
		{"no MAC", Setup{Trust: good}, testSetupCode, ErrSetup},
		{"code as typed, not canonical", setupRequest(good, "abcde fghjk mnpqr stuvw"), "abcde fghjk mnpqr stuvw", ErrSetup},
		{"payload that is no list, MAC wrong", setupRequest(notAList, otherSetupCode), testSetupCode, ErrSetup},
		{"payload that is no list, MAC right", setupRequest(notAList, testSetupCode), testSetupCode, trust.ErrInvalid},

		// The version against the offer.
		{"other installation", setupRequest(signed(func(l *trust.List) { l.Installation = trusttest.ID(0x12) }), testSetupCode), testSetupCode, ErrSetup},
		{"API node missing", setupRequest(signed(func(l *trust.List) { l.Nodes = nil }), testSetupCode), testSetupCode, ErrSetup},
		{"API node with another URL", setupRequest(signed(func(l *trust.List) { l.Nodes[0].URL = "wss://api-9.test:7443/tunnel" }), testSetupCode), testSetupCode, ErrSetup},
		{"API node with another TLS pin", setupRequest(signed(func(l *trust.List) { l.Nodes[0].TLS = bytes.Repeat([]byte{9}, 32) }), testSetupCode), testSetupCode, ErrSetup},
		{"global store with another key", setupRequest(signed(func(l *trust.List) { l.Stores[0].Key = bytes.Repeat([]byte{9}, 32) }), testSetupCode), testSetupCode, ErrSetup},
		{"extra store", setupRequest(signed(func(l *trust.List) {
			l.Stores = append(l.Stores, trust.Store{ID: "other", Name: "Other", URL: "https://other.example", Key: bytes.Repeat([]byte{9}, 32)})
		}), testSetupCode), testSetupCode, ErrSetup},
		{"signed by a non-admin", setupRequest(alice.Sign(t, withAlice), testSetupCode), testSetupCode, trust.ErrSigner},
		{"payload is version 2", setupRequest(admin.Sign(t, trusttest.Next(t, good)), testSetupCode), testSetupCode, trust.ErrAnchor},

		// Standing approvals: the NGC parts with the offered release key, and system.
		{"standing approval for an app", setupRequest(signed(func(l *trust.List) {
			l.Auto = []trust.Auto{l.Auto[0], {Target: "org.sorglos.terminal", Key: releaseKey}, l.Auto[1]}
		}), testSetupCode), testSetupCode, ErrSetup},
		{"standing approval with another key", setupRequest(signed(func(l *trust.List) { l.Auto[0].Key = otherKey }), testSetupCode), testSetupCode, ErrSetup},
		{"ngc-web with another key", setupRequest(signed(func(l *trust.List) {
			l.Auto = []trust.Auto{l.Auto[0], {Target: trust.AutoWeb, Key: otherKey}, l.Auto[1]}
		}), testSetupCode), testSetupCode, ErrSetup},
	}
	for _, c := range cases {
		v, err := VerifySetup(c.s, info, c.code)
		if err != c.want || (err == nil && (v.List.Seq != 1 || !bytes.Equal(v.Hash, trust.Hash(c.s.Trust)))) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	noStore := info
	noStore.Store = nil
	if _, err := VerifySetup(setupRequest(good, testSetupCode), noStore, testSetupCode); err != ErrSetup {
		t.Errorf("store the API did not offer: got %v, want %v", err, ErrSetup)
	}
	noRelease := info
	noRelease.ReleaseKey = nil
	if _, err := VerifySetup(setupRequest(good, testSetupCode), noRelease, testSetupCode); err != ErrSetup {
		t.Errorf("release key the API did not offer: got %v, want %v", err, ErrSetup)
	}
}

func TestNormalizeSetupCode(t *testing.T) {
	for _, c := range []struct{ typed, code string }{
		{"ABCDE-FGHJK-MNPQR-STUVW", testSetupCode},
		{"abcde fghjk mnpqr stuvw", testSetupCode},
		{"ABCDEFGHJKMNPQRSTUVW", testSetupCode},
		{"\tabcde-FGHJK mnpqr--stuvw ", testSetupCode},
		{"A-B-C-D-E-F-G-H-J-K-M-N-P-Q-R-S-T-U-V-W", testSetupCode},
		{"23456 789ab cdefg hjklm", otherSetupCode},
		{"ABCDE-FGHJK-MNPQR-STUV1", ""},
		{"ABCDE-FGHJK-MNPQR-STUV0", ""},
		{"abcde-fghjk-mnpqr-stuvo", ""},
		{"abcde-fghjk-mnpqr-stuvi", ""},
		{"ABCDEFGHJKMNPQRSTUVWX", ""}, // 21 characters
		{"ABCDEFGHJKMNPQRSTUV", ""},   // 19
		{"", ""},
		{" - ", ""},
		{"ABCDE-FGHJK-MNPQR-STUVW\n", ""},              // only spaces, tabs and '-' fall away
		{"ABCDE_FGHJK_MNPQR_STUVW", ""},                // no other separator
		{"ABCDE\u00a0FGHJK\u00a0MNPQR\u00a0STUVW", ""}, // no-break space
		{"\uff21BCDE-FGHJK-MNPQR-STUVW", ""},           // full-width A
		{"\u00e4bcde-fghjk-mnpqr-stuvw", ""},           // a with umlaut
	} {
		got, ok := NormalizeSetupCode(c.typed)
		if ok != (c.code != "") || got != c.code || (ok && !ValidSetupCode(got)) {
			t.Errorf("%q: got %q, %v, want %q", c.typed, got, ok, c.code)
		}
	}
}

func TestSetupMACs(t *testing.T) {
	x := []byte(`{"seq":1}`)
	mac := func(key, label string, b []byte) []byte {
		m := hmac.New(sha256.New, []byte(key))
		m.Write([]byte(label))
		m.Write([]byte{0})
		m.Write(b)
		return m.Sum(nil)
	}
	offer, setup := OfferMAC(testSetupCode, x), SetupMAC(testSetupCode, x)
	if !bytes.Equal(offer, mac(testSetupCode, "ngc/setup-offer/v1", x)) || !bytes.Equal(setup, mac(testSetupCode, "ngc/setup/v1", x)) {
		t.Error("MAC not as documented")
	}
	if len(offer) != 32 || bytes.Equal(offer, setup) {
		t.Error("the labels do not keep the two MACs apart")
	}
	// The key is the canonical code, so both sides normalize what was typed
	// before they compute a MAC.
	if bytes.Equal(OfferMAC("abcde fghjk mnpqr stuvw", x), offer) || bytes.Equal(OfferMAC(otherSetupCode, x), offer) {
		t.Error("the MAC does not depend on the exact code")
	}
	if bytes.Equal(OfferMAC(testSetupCode, append(bytes.Clone(x), ' ')), offer) {
		t.Error("the MAC does not cover the exact bytes")
	}
}

func TestParseSetupOffer(t *testing.T) {
	info := setupInfo()
	infoJSON := mustJSON(t, info)
	offer := func(b, mac []byte) []byte { return mustJSON(t, SetupOffer{Info: b, MAC: mac}) }
	// withMAC wraps b with a MAC that holds, so only the rule under test fails.
	withMAC := func(b []byte) []byte { return offer(b, OfferMAC(testSetupCode, b)) }
	withInfo := func(mutate func(*SetupInfo)) []byte {
		i := setupInfo()
		mutate(&i)
		return withMAC(mustJSON(t, i))
	}
	padded := func(b []byte, n int) []byte { return append(bytes.Clone(b), bytes.Repeat([]byte(" "), n-len(b))...) }
	apiNodes := func(n int) []trust.Node {
		nodes := make([]trust.Node, n)
		for i := range nodes {
			k := bytes.Repeat([]byte{byte(0x40 + i)}, 32)
			nodes[i] = trust.Node{ID: fmt.Sprintf("api-%02d", i+1), Kind: trust.NodeAPI, Name: fmt.Sprintf("API %d", i+1),
				Noise: k, Sign: k, URL: fmt.Sprintf("wss://api-%d.test:7443/tunnel", i+1), TLS: k}
		}
		return nodes
	}
	ctl := controllerLike()
	asAPI := ctl
	asAPI.Kind = trust.NodeAPI
	if trust.ValidAPINode(ctl) || !trust.ValidAPINode(asAPI) {
		t.Fatal("the node of kind controller must break the kind rule and no other")
	}
	notJSON := bytes.Repeat([]byte("x"), MaxRequest)
	cases := []struct {
		name string
		body []byte
		code string
		want error
	}{
		{"valid", withMAC(infoJSON), testSetupCode, nil},
		{"without a store", withInfo(func(i *SetupInfo) { i.Store = nil }), testSetupCode, nil},
		{"MaxSetupNodes API nodes", withInfo(func(i *SetupInfo) { i.Nodes = apiNodes(MaxSetupNodes) }), testSetupCode, nil},
		{"two API nodes in order", withInfo(func(i *SetupInfo) { i.Nodes = append(i.Nodes, api2Node()) }), testSetupCode, nil},
		{"store name of 64 bytes", withInfo(func(i *SetupInfo) { i.Store.Name = strings.Repeat("S", 64) }), testSetupCode, nil},
		{"store URL of 256 bytes", withInfo(func(i *SetupInfo) { i.Store.URL = "https://" + strings.Repeat("s", 256-8) }), testSetupCode, nil},
		{"info of MaxRequest bytes", withMAC(padded(infoJSON, MaxRequest)), testSetupCode, nil},

		// The answer around the info.
		{"not JSON", []byte("offer"), testSetupCode, ErrMessage},
		{"an array", []byte(`[]`), testSetupCode, ErrMessage},
		{"unknown member", withMember(withMAC(infoJSON), `"code":"`+testSetupCode+`"`), testSetupCode, ErrMessage},
		{"duplicate member", withMember(withMAC(infoJSON), `"mac":"`+enc.B64.EncodeToString(OfferMAC(testSetupCode, infoJSON))+`"`), testSetupCode, ErrMessage},
		{"larger than MaxSetupOffer", padded(withMAC(infoJSON), MaxSetupOffer+1), testSetupCode, ErrMessage},
		{"empty info", offer(nil, OfferMAC(testSetupCode, nil)), testSetupCode, ErrMessage},
		{"info over MaxRequest", withMAC(padded(infoJSON, MaxRequest+1)), testSetupCode, ErrMessage},
		{"MAC of 31 bytes", offer(infoJSON, OfferMAC(testSetupCode, infoJSON)[:31]), testSetupCode, ErrMessage},
		{"no MAC", offer(infoJSON, nil), testSetupCode, ErrMessage},

		// The MAC, checked before the info is decoded.
		{"MAC with another code", offer(infoJSON, OfferMAC(otherSetupCode, infoJSON)), testSetupCode, ErrSetup},
		{"MAC over other bytes", offer(infoJSON, OfferMAC(testSetupCode, padded(infoJSON, len(infoJSON)+1))), testSetupCode, ErrSetup},
		{"setup MAC instead of the offer MAC", offer(infoJSON, SetupMAC(testSetupCode, infoJSON)), testSetupCode, ErrSetup},
		{"code as typed, not canonical", withMAC(infoJSON), "abcde fghjk mnpqr stuvw", ErrSetup},
		{"64 KiB of info that is not JSON, MAC wrong", offer(notJSON, OfferMAC(testSetupCode, infoJSON)), testSetupCode, ErrSetup},
		{"64 KiB of info that is not JSON, MAC right", withMAC(notJSON), testSetupCode, ErrMessage},

		// The info, under a MAC that holds.
		{"info with an unknown member", withMAC(withMember(infoJSON, `"admin":"x"`)), testSetupCode, ErrMessage},
		{"info as an array", withMAC([]byte(`[]`)), testSetupCode, ErrMessage},
		{"installation not an ID", withInfo(func(i *SetupInfo) { i.Installation = "installation" }), testSetupCode, ErrMessage},
		{"no nodes", withInfo(func(i *SetupInfo) { i.Nodes = []trust.Node{} }), testSetupCode, ErrMessage},
		{"more than MaxSetupNodes nodes", withInfo(func(i *SetupInfo) { i.Nodes = apiNodes(MaxSetupNodes + 1) }), testSetupCode, ErrMessage},
		{"a controller node", withInfo(func(i *SetupInfo) { i.Nodes = append(i.Nodes, ctl) }), testSetupCode, ErrMessage},
		{"API node without a TLS pin", withInfo(func(i *SetupInfo) { i.Nodes[0].TLS = nil }), testSetupCode, ErrMessage},
		{"now 0", withInfo(func(i *SetupInfo) { i.Now = 0 }), testSetupCode, ErrMessage},
		{"no release key", withInfo(func(i *SetupInfo) { i.ReleaseKey = nil }), testSetupCode, ErrMessage},
		{"release key of 31 bytes", withInfo(func(i *SetupInfo) { i.ReleaseKey = i.ReleaseKey[:31] }), testSetupCode, ErrMessage},
		{"store other than global", withInfo(func(i *SetupInfo) { i.Store.ID = "other" }), testSetupCode, ErrMessage},
		{"store key of 31 bytes", withInfo(func(i *SetupInfo) { i.Store.Key = i.Store.Key[:31] }), testSetupCode, ErrMessage},
		// What version 1 takes as offered must keep the rules of the trust list.
		{"nodes out of order", withInfo(func(i *SetupInfo) { i.Nodes = []trust.Node{api2Node(), i.Nodes[0]} }), testSetupCode, ErrMessage},
		{"a node twice", withInfo(func(i *SetupInfo) { i.Nodes = append(i.Nodes, i.Nodes[0]) }), testSetupCode, ErrMessage},
		{"store without a name", withInfo(func(i *SetupInfo) { i.Store.Name = "" }), testSetupCode, ErrMessage},
		{"store name with a line feed", withInfo(func(i *SetupInfo) { i.Store.Name = "NGC\nStore" }), testSetupCode, ErrMessage},
		{"store name of 65 bytes", withInfo(func(i *SetupInfo) { i.Store.Name = strings.Repeat("S", 65) }), testSetupCode, ErrMessage},
		{"store URL with http", withInfo(func(i *SetupInfo) { i.Store.URL = "http://store.ngc.example" }), testSetupCode, ErrMessage},
		{"store URL of 257 bytes", withInfo(func(i *SetupInfo) { i.Store.URL = "https://" + strings.Repeat("s", 257-8) }), testSetupCode, ErrMessage},
	}
	for _, c := range cases {
		got, err := ParseSetupOffer(c.body, c.code)
		if err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
		if err != nil {
			continue
		}
		if got.Installation != info.Installation || len(got.ReleaseKey) != 32 {
			t.Errorf("%s: got %+v", c.name, got)
		}
		// Accepted means that version 1 built from it validates.
		if err := versionOne(got).Validate(); err != nil {
			t.Errorf("%s: version 1 from the offer: %v", c.name, err)
		}
	}
	if got, err := ParseSetupOffer(withMAC(infoJSON), testSetupCode); err != nil || !sameJSON(got, info) {
		t.Errorf("valid offer reads back as %+v, %v", got, err)
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
	base := setupRequest(admin.Sign(t, setupList(admin)), testSetupCode)
	setup := func(mutate func(*Setup)) []byte {
		s := base
		mutate(&s)
		return mustJSON(t, s)
	}
	checkParse(t, func(b []byte) error { _, err := ParseSetup(b); return err }, []parseCase{
		{"valid", setup(func(*Setup) {}), true},
		// ParseSetup checks the form; only VerifySetup knows the code.
		{"MAC with another code", setup(func(s *Setup) { s.MAC = SetupMAC(otherSetupCode, s.Trust.Payload) }), true},
		{"MAC of 31 bytes", setup(func(s *Setup) { s.MAC = s.MAC[:31] }), false},
		{"no MAC", setup(func(s *Setup) { s.MAC = nil }), false},
		{"the old shape, with the code", withMember(mustJSON(t, struct {
			Trust trust.Signed `json:"trust"`
		}{base.Trust}), `"code":"`+testSetupCode+`"`), false},
		{"empty payload", setup(func(s *Setup) { s.Trust.Payload = nil }), false},
		{"malformed proof", setup(func(s *Setup) { s.Trust.Proof.KeyID = "admin" }), false},
		{"unknown field", withMember(setup(func(*Setup) {}), `"admin":"x"`), false},
		{"larger than MaxRequest", append(setup(func(*Setup) {}), bytes.Repeat([]byte(" "), MaxRequest)...), false},
	})
}

func TestHTTPStatus(t *testing.T) {
	for c, want := range map[wire.Code]int{
		wire.CodeProtocol:    400,
		wire.CodeLogin:       401,
		wire.CodeForbidden:   403,
		wire.CodeVerify:      403,
		wire.CodeNotFound:    404,
		wire.CodeTooLarge:    413,
		wire.CodeLocked:      429,
		wire.CodeInternal:    500,
		wire.CodeUnreachable: 503,
		// Codes of channels, which have no answer of their own before the
		// WebSocket, and codes no one defined.
		wire.CodeExit:    500,
		wire.CodeTimeout: 500,
		wire.CodeLimit:   500,
		wire.CodeConfirm: 500,
		wire.CodeLicense: 500,
		"E_UNKNOWN":      500,
		"":               500,
	} {
		if got := HTTPStatus(c); got != want {
			t.Errorf("%q: got %d, want %d", c, got, want)
		}
	}
}

func TestParseFrom(t *testing.T) {
	for s, want := range map[string]uint64{"1": 1, "10": 10, "4711": 4711, "9007199254740991": 9007199254740991} {
		if got, err := ParseFrom(s); err != nil || got != want {
			t.Errorf("%q: got %d, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "0", "01", "+1", "-1", "1e3", "1.0", "9007199254740992", "99999999999999999", " 1", "1 ", "0x1", "\u0661"} {
		if got, err := ParseFrom(s); err != ErrMessage {
			t.Errorf("%q: got %d, %v", s, got, err)
		}
	}
}

// TestVerifyApproval: a past approval needs everything a session needs but
// the time.
func TestVerifyApproval(t *testing.T) {
	f := newFixture(t)
	c := f.clientCert(t)
	const years = 5 * 365 * 86400
	c.Issued, c.Expires = issued-years, issued-years+3600
	old := mustJSON(t, c)
	oldProof := f.admin.Prove(sign.PurposeClientCert, old)
	if _, err := VerifyClient(old, oldProof, f.list, now); err != ErrExpired {
		t.Fatalf("VerifyClient on a certificate that expired years ago: got %v, want %v", err, ErrExpired)
	}
	got, err := VerifyApproval(old, oldProof, f.list)
	if err != nil || got.User != f.list.Users[1].ID || got.Expires != c.Expires || !bytes.Equal(got.Sig, c.Sig) {
		t.Fatalf("certificate that expired years ago: %+v, %v", got, err)
	}
	carol := f.clientFor(t, f.carol)
	otherUser := f.clientFor(t, f.list.Users[0].ID)
	otherInstallation := c
	otherInstallation.Installation = trusttest.ID(0x12)
	otherInst := mustJSON(t, otherInstallation)
	cases := []struct {
		name   string
		client []byte
		p      proof.Proof
		want   error
	}{
		{"passkey for a password-too role", carol, f.carolPasskey.Prove(sign.PurposeClientCert, carol), nil},
		{"another user's key", otherUser, f.admin.Prove(sign.PurposeClientCert, otherUser), ErrUser},
		{"an unknown key", old, trusttest.NewSigner(9).Prove(sign.PurposeClientCert, old), ErrUser},
		{"a password key for a password-too role", carol, f.carolPassword.Prove(sign.PurposeClientCert, carol), ErrUser},
		{"another installation", otherInst, f.admin.Prove(sign.PurposeClientCert, otherInst), ErrCert},
		{"a proof for another purpose", old, f.admin.Prove(sign.PurposeTask, old), proof.ErrSignature},
		{"a duplicate member", withMember(old, `"inst":"`+c.Installation+`"`), oldProof, ErrCert},
	}
	for _, tc := range cases {
		if _, err := VerifyApproval(tc.client, tc.p, f.list); err != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
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

// typedCode is what someone typed and the canonical code
// NormalizeSetupCode makes of it, empty when it refuses.
type typedCode struct {
	Typed string `json:"typed"`
	Code  string `json:"code"`
}

// setupOffer is a body of GET PathSetup, checked with ParseSetupOffer and the
// vector's code.
type setupOffer struct {
	Name  string    `json:"name"`
	Offer enc.Bytes `json:"offer"`
	Valid bool      `json:"valid"`
	want  error     // the error ParseSetupOffer must return; not in the file
}

// setupPost is a body of POST PathSetup, checked with ParseSetup and
// VerifySetup against the info of the offer "valid" and the vector's code.
type setupPost struct {
	Name    string    `json:"name"`
	Request enc.Bytes `json:"request"`
	Valid   bool      `json:"valid"`
	want    error     // the error VerifySetup must return; not in the file
}

type setupVector struct {
	Code     string       `json:"code"`
	Typed    []typedCode  `json:"typed"`
	Offers   []setupOffer `json:"offers"`
	Requests []setupPost  `json:"requests"`
}

// setupVectors: version 1 is setupList, the offer setupInfo. Each invalid
// case breaks one rule; every version in a request is signed by the admin.
func setupVectors(t testing.TB) setupVector {
	admin := trusttest.NewSigner(1)
	infoJSON := mustJSON(t, setupInfo())
	offer := func(info []byte, code string) []byte {
		return mustJSON(t, SetupOffer{Info: info, MAC: OfferMAC(code, info)})
	}
	infoWith := func(mutate func(*SetupInfo)) []byte {
		i := setupInfo()
		mutate(&i)
		return mustJSON(t, i)
	}
	later := infoWith(func(i *SetupInfo) { i.Now++ })
	// Without the member, not with an empty one.
	noRelease := bytes.Replace(infoWith(func(i *SetupInfo) { i.ReleaseKey = nil }), []byte(`"releaseKey":"",`), nil, 1)
	if bytes.Contains(noRelease, []byte("releaseKey")) {
		t.Fatalf("release key still in %s", noRelease)
	}

	releaseKey := setupInfo().ReleaseKey
	signed := func(mutate func(l *trust.List)) trust.Signed {
		l := setupList(admin)
		mutate(l)
		return admin.Sign(t, l)
	}
	good := signed(func(*trust.List) {})
	parts := signed(func(l *trust.List) {
		l.Auto = []trust.Auto{{Target: trust.AutoAPI, Key: releaseKey}, {Target: trust.AutoController, Key: releaseKey},
			{Target: trust.AutoWeb, Key: releaseKey}, {Target: trust.AutoSystem}}
	})
	_, otherStore := trusttest.NodeKeys(0xB2)
	_, otherRelease := trusttest.NodeKeys(0xD2)
	post := func(s trust.Signed, code string) []byte { return mustJSON(t, setupRequest(s, code)) }

	return setupVector{
		Code: testSetupCode,
		Typed: []typedCode{
			{"abcde fghjk mnpqr stuvw", testSetupCode},
			{"ABCDEFGHJKMNPQRSTUVW", testSetupCode},
			{"ABCDE-FGHJK-MNPQR-STUV1", ""},
			{"abcde-fghjk-mnpqr-stuvo", ""},
			{"ABCDEFGHJKMNPQRSTUVWX", ""},
		},
		Offers: []setupOffer{
			{Name: "valid", Offer: offer(infoJSON, testSetupCode), Valid: true},
			{Name: "MAC with another code", Offer: offer(infoJSON, otherSetupCode), want: ErrSetup},
			{Name: "info changed after the MAC", Offer: mustJSON(t, SetupOffer{Info: later, MAC: OfferMAC(testSetupCode, infoJSON)}), want: ErrSetup},
			// Both reach trust.ValidAPINode past the budget, each breaking one of its rules.
			{Name: "info with a controller node", Offer: offer(infoWith(func(i *SetupInfo) { i.Nodes = append(i.Nodes, controllerLike()) }), testSetupCode), want: ErrMessage},
			{Name: "API node without a TLS pin", Offer: offer(infoWith(func(i *SetupInfo) { i.Nodes[0].TLS = nil }), testSetupCode), want: ErrMessage},
			{Name: "info without a release key", Offer: offer(noRelease, testSetupCode), want: ErrMessage},
			{Name: "info with a store other than global", Offer: offer(infoWith(func(i *SetupInfo) { i.Store.ID = "other" }), testSetupCode), want: ErrMessage},
			// Version 1 takes nodes and store as offered, so they keep the
			// rules of the trust list (trust.List.Validate).
			{Name: "two API nodes in order", Offer: offer(infoWith(func(i *SetupInfo) { i.Nodes = append(i.Nodes, api2Node()) }), testSetupCode), Valid: true},
			{Name: "info with its nodes out of order", Offer: offer(infoWith(func(i *SetupInfo) { i.Nodes = []trust.Node{api2Node(), i.Nodes[0]} }), testSetupCode), want: ErrMessage},
			{Name: "info with a node twice", Offer: offer(infoWith(func(i *SetupInfo) { i.Nodes = append(i.Nodes, i.Nodes[0]) }), testSetupCode), want: ErrMessage},
			{Name: "info with a store name that is no text", Offer: offer(infoWith(func(i *SetupInfo) { i.Store.Name = "NGC\nStore" }), testSetupCode), want: ErrMessage},
			{Name: "info with a store name of 65 bytes", Offer: offer(infoWith(func(i *SetupInfo) { i.Store.Name = strings.Repeat("S", 65) }), testSetupCode), want: ErrMessage},
			{Name: "info with a store URL of plain http", Offer: offer(infoWith(func(i *SetupInfo) { i.Store.URL = "http://store.ngc.example" }), testSetupCode), want: ErrMessage},
			{Name: "info with a store URL of 257 bytes", Offer: offer(infoWith(func(i *SetupInfo) { i.Store.URL = "https://" + strings.Repeat("s", 257-8) }), testSetupCode), want: ErrMessage},
		},
		Requests: []setupPost{
			{Name: "valid", Request: post(good, testSetupCode), Valid: true},
			{Name: "approvals for the NGC parts and system", Request: post(parts, testSetupCode), Valid: true},
			{Name: "MAC with another code", Request: post(good, otherSetupCode), want: ErrSetup},
			{Name: "MAC over other bytes", Request: mustJSON(t, Setup{MAC: SetupMAC(testSetupCode, parts.Payload), Trust: good}), want: ErrSetup},
			{Name: "store the API did not offer", Request: post(signed(func(l *trust.List) {
				l.Stores = []trust.Store{{ID: "other", Name: "Other Store", URL: "https://store.other.example",
					Key: enc.Bytes(otherStore.Public().(ed25519.PublicKey))}}
			}), testSetupCode), want: ErrSetup},
			{Name: "standing approval for an app", Request: post(signed(func(l *trust.List) {
				l.Auto = []trust.Auto{l.Auto[0], {Target: "org.sorglos.terminal", Key: releaseKey}, l.Auto[1]}
			}), testSetupCode), want: ErrSetup},
			{Name: "standing approval with another key", Request: post(signed(func(l *trust.List) {
				l.Auto[0].Key = enc.Bytes(otherRelease.Public().(ed25519.PublicKey))
			}), testSetupCode), want: ErrSetup},
		},
	}
}

// checkSetupVector holds the setup vector against the Go functions, as the
// web interface holds it against its own.
func checkSetupVector(t *testing.T, v setupVector) {
	t.Helper()
	for _, c := range v.Typed {
		if got, ok := NormalizeSetupCode(c.Typed); got != c.Code || ok != (c.Code != "") {
			t.Errorf("typed %q: got %q, %v, want %q", c.Typed, got, ok, c.Code)
		}
	}
	var info SetupInfo
	for _, c := range v.Offers {
		got, err := ParseSetupOffer(c.Offer, v.Code)
		if (err == nil) != c.Valid || err != c.want {
			t.Errorf("offer %s: got %v, want %v", c.Name, err, c.want)
		}
		if c.Name == "valid" {
			info = got
		}
	}
	for _, c := range v.Requests {
		s, err := ParseSetup(c.Request)
		if err != nil {
			t.Errorf("request %s: %v", c.Name, err)
			continue
		}
		if _, err := VerifySetup(s, info, v.Code); (err == nil) != c.Valid || err != c.want {
			t.Errorf("request %s: got %v, want %v", c.Name, err, c.want)
		}
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
	Setup         setupVector    `json:"setup"`
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
	setup := setupVectors(t)
	checkSetupVector(t, setup)
	// Every offer refused for its info passes the budget of the info, so a
	// parser with the budget but without the rules behind it fails the vector;
	// every accepted offer makes a version 1 that validates.
	for _, c := range setup.Offers {
		switch {
		case c.Valid:
			info, err := ParseSetupOffer(c.Offer, setup.Code)
			if err == nil {
				err = versionOne(info).Validate()
			}
			if err != nil {
				t.Errorf("offer %s: version 1 from it: %v", c.Name, err)
			}
		case c.want == ErrMessage:
			var o SetupOffer
			var info SetupInfo
			err := json.Unmarshal(c.Offer, &o)
			if err == nil {
				err = infoBudget.Decode(o.Info, &info)
			}
			if err != nil {
				t.Errorf("offer %s is refused before its rules: %v", c.Name, err)
			}
		}
	}
	golden.Check(t, *update, "session.json", vector{Trust: f.admin.Sign(t, f.list), Now: now, Client: f.client, Proof: f.proof, API: f.api,
		APIRejects: rejects, ClientRejects: clients, Setup: setup})
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
		{"ParseSetupOffer", hostile(`{"info":[`, `]}`, MaxSetupOffer),
			func(b []byte) error { _, err := ParseSetupOffer(b, testSetupCode); return err }, ErrMessage},
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
// node key, as FuzzOpen (shared/trust) fuzzes chain bytes rather than
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
	f.Add(mustJSON(f, setupRequest(admin.Sign(f, setupList(admin)), testSetupCode)))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseSetup(b)
	})
}

// FuzzParseSetupOffer reads the fuzzed bytes twice. As a whole answer: what
// ParseSetupOffer accepts carries a MAC that holds for the code. As the Info
// of an answer with a MAC that holds, so the fuzzer reaches the checks behind
// the MAC: what it accepts keeps the rules of the info, and version 1 built
// from it validates.
func FuzzParseSetupOffer(f *testing.F) {
	info := mustJSON(f, setupInfo())
	f.Add(mustJSON(f, SetupOffer{Info: info, MAC: OfferMAC(testSetupCode, info)}))
	f.Add(mustJSON(f, SetupOffer{Info: info, MAC: OfferMAC(otherSetupCode, info)}))
	f.Add(info)
	for _, c := range setupVectors(f).Offers {
		var o SetupOffer
		if json.Unmarshal(c.Offer, &o) == nil {
			f.Add([]byte(o.Info))
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if _, err := ParseSetupOffer(b, testSetupCode); err == nil {
			var o SetupOffer
			if err := json.Unmarshal(b, &o); err != nil {
				t.Fatalf("accepted %q, which does not decode again: %v", b, err)
			}
			if !hmac.Equal(o.MAC, OfferMAC(testSetupCode, o.Info)) {
				t.Fatalf("accepted %q without a MAC that holds", b)
			}
		}
		if len(b) == 0 || len(b) > MaxRequest {
			return
		}
		got, err := ParseSetupOffer(mustJSON(t, SetupOffer{Info: b, MAC: OfferMAC(testSetupCode, b)}), testSetupCode)
		if err == ErrSetup {
			t.Fatalf("MAC that holds refused for %q", b)
		}
		if err != nil {
			return
		}
		if len(got.Nodes) > MaxSetupNodes || (got.Store != nil && got.Store.ID != trust.StoreGlobal) {
			t.Fatalf("accepted %+v", got)
		}
		for _, n := range got.Nodes {
			if !trust.ValidAPINode(n) {
				t.Fatalf("accepted node %+v", n)
			}
		}
		if err := versionOne(got).Validate(); err != nil {
			t.Fatalf("accepted %+v, from which version 1 does not validate: %v", got, err)
		}
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
