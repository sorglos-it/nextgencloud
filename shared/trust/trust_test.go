package trust_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"flag"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite shared/testdata")

var (
	admin        = trusttest.NewSigner(1)
	alice        = trusttest.NewSigner(2)
	mallory      = trusttest.NewSigner(3)
	passkeyAdmin = trusttest.NewPasskey(trusttest.GenesisPasskeyFill)
)

func anchor(t testing.TB, s trust.Signed) trust.Anchor {
	t.Helper()
	l, err := trust.Parse(s.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return trust.Anchor{Seq: l.Seq, Hash: trust.Hash(s)}
}

// chain returns version 1 (genesis: admin2 with a passkey and admin with a
// password key), version 2 (admin adds alice) and version 3 (alice adds a
// second key of her own, without an admin). Genesis's Users are
// [admin2, admin] (sorted by ID), so alice lands at index 2 once added.
func chain(t testing.TB) []trust.Signed {
	g := admin.Sign(t, trusttest.Genesis(admin))
	l2 := trusttest.Next(t, g)
	l2.Roles = append(l2.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	l2.Users = append(l2.Users, trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"}, Keys: []proof.UserKey{alice.Key}})
	v2 := admin.Sign(t, l2)
	l3 := trusttest.Next(t, v2)
	l3.Users[2].Keys = append(l3.Users[2].Keys, trusttest.NewSigner(4).Key)
	return []trust.Signed{g, v2, alice.Sign(t, l3)}
}

func TestChain(t *testing.T) {
	c := chain(t)
	v, err := trust.Open(anchor(t, c[0]), c)
	if err != nil || v.List.Seq != 3 {
		t.Fatalf("open: %v", err)
	}
	v1, err := trust.Open(anchor(t, c[0]), c[:1])
	if err != nil {
		t.Fatal(err)
	}
	if v, err = trust.Advance(v1, c[1:]); err != nil || v.List.Seq != 3 {
		t.Fatalf("advance: %v", err)
	}
}

// TestAdvanceZeroVersion proves Advance refuses a zero Version cleanly
// instead of panicking on the nil *List it would otherwise dereference.
func TestAdvanceZeroVersion(t *testing.T) {
	g := admin.Sign(t, trusttest.Genesis(admin))
	if _, err := trust.Advance(trust.Version{}, []trust.Signed{g}); err != trust.ErrChain {
		t.Fatalf("zero version: got %v, want %v", err, trust.ErrChain)
	}
}

// genesisByNonAdmin returns a version-1 list where alice, a listed
// non-admin, is the only available signer. Used to prove that Open's
// version-1 special case checks admin-ness explicitly rather than by
// accident (a signer who only changed her own keys may sign a later
// version, but never version 1).
func genesisByNonAdmin(t *testing.T) trust.Signed {
	t.Helper()
	g := trusttest.Genesis(admin)
	g.Roles = append(g.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	g.Users = append(g.Users, trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"}, Keys: []proof.UserKey{alice.Key}})
	return alice.Sign(t, g)
}

// TestGenesisSignerMustBeAdmin proves the version-1 admin check: a listed
// non-admin is refused even though she is a real, verifiable signer with a
// real key.
func TestGenesisSignerMustBeAdmin(t *testing.T) {
	s := genesisByNonAdmin(t)
	if _, err := trust.Open(anchor(t, s), []trust.Signed{s}); err != trust.ErrSigner {
		t.Fatalf("genesis by non-admin: got %v, want %v", err, trust.ErrSigner)
	}
}

type vector struct {
	Name   string         `json:"name"`
	Anchor trust.Anchor   `json:"anchor"`
	Chain  []trust.Signed `json:"chain"`
	Valid  bool           `json:"valid"`
	Seq    uint64         `json:"seq,omitempty"`
}

func vectors(t *testing.T) []vector {
	c := chain(t)
	g, v2, v3 := c[0], c[1], c[2]

	// A version the passkey admin signs, so the vector exercises a real
	// passkey proof end to end (RP check included), not just password keys.
	v4 := passkeyAdmin.Sign(t, trusttest.Next(t, v3))
	full := []trust.Signed{g, v2, v3, v4}

	l, err := trust.Parse(v2.Payload)
	if err != nil {
		t.Fatal(err)
	}
	l.Users[2].Roles = []string{trust.RoleAdmin, "user"}
	b, _ := json.Marshal(l)
	tampered := trust.Signed{Payload: b, Proof: v2.Proof}

	l3 := trusttest.Next(t, v2)
	l3.Users[2].Roles = []string{trust.RoleAdmin, "user"}
	selfPromoted := alice.Sign(t, l3)

	foreign := mallory.Sign(t, trusttest.Next(t, g))
	gap := admin.Sign(t, trusttest.Next(t, v2))

	// Each of the next three breaks exactly one rule, with everything else
	// (including the sequence number) correct, so a chain that only checks
	// Seq cannot pass them by accident.
	wrongPrev := trusttest.Next(t, g)
	h := trust.Hash(g)
	h[0] ^= 0xFF
	wrongPrev.Prev = h
	badPrev := admin.Sign(t, wrongPrev)

	wrongInstall := trusttest.Next(t, g)
	wrongInstall.Installation = trusttest.ID(0x12)
	badInstall := admin.Sign(t, wrongInstall)

	noAdmin := trusttest.Next(t, g)
	noAdmin.Roles = append(noAdmin.Roles, trust.Role{Name: "user", Level: trust.LevelOff})
	for i := range noAdmin.Users {
		noAdmin.Users[i].Roles = []string{"user"}
	}
	badSuccessor := admin.Sign(t, noAdmin)

	nonAdminGenesis := genesisByNonAdmin(t)

	// Key changes: a user may change only her own keys, a new admin cannot
	// vouch for herself, and a replaced key signs nothing after the version
	// that replaced it.
	otherKey := trusttest.Next(t, v2)
	otherKey.Users[1].Keys = []proof.UserKey{trusttest.NewSigner(9).Key}
	otherUsersKey := alice.Sign(t, otherKey)

	withMallory := trusttest.Next(t, g)
	withMallory.Users = append(withMallory.Users,
		trust.User{ID: trusttest.ID(0x30), Name: "mallory", Roles: []string{trust.RoleAdmin}, Keys: []proof.UserKey{mallory.Key}})
	slices.SortFunc(withMallory.Users, func(a, b trust.User) int { return strings.Compare(a.ID, b.ID) })
	selfAdded := mallory.Sign(t, withMallory)

	newKey := trusttest.Next(t, g)
	newKey.Users[1].Keys = []proof.UserKey{trusttest.NewSigner(8).Key}
	replaced := admin.Sign(t, newKey)
	oldKey := admin.Sign(t, trusttest.Next(t, replaced))

	// A member of a password-too role needs a passkey and a password key;
	// with either missing she could never log in.
	carolWith := func(keys ...proof.UserKey) trust.Signed {
		l := trusttest.Next(t, g)
		withCarol(keys...)(l)
		return admin.Sign(t, l)
	}
	carolPasskey, carolPassword := trusttest.NewPasskey(0x63).Key, trusttest.NewSigner(0x64).Key
	carolBoth := carolWith(carolPasskey, carolPassword)
	carolNoPasskey := carolWith(carolPassword)
	carolNoPassword := carolWith(carolPasskey)

	// Controllers and Apache trust an API only with its pin, so an API node
	// needs one; nobody dials a controller, so a controller has none.
	noPin := trusttest.Next(t, g)
	noPin.Nodes[0].TLS = nil
	apiWithoutPin := admin.Sign(t, noPin)
	ctlPin := trusttest.Next(t, g)
	_, ctlPin.Nodes[1].TLS = trusttest.APITLS(0xC1, "ctl-1.test")
	controllerWithPin := admin.Sign(t, ctlPin)

	// Addresses land in the configuration of Apache and of the database
	// driver, so a version whose addresses break the grammar is no version,
	// even with a correct signature.
	feedURL := trusttest.Next(t, g)
	feedURL.Nodes[0].URL += "\x0a"
	apiURLWithFeed := admin.Sign(t, feedURL)
	slashStore := trusttest.Next(t, g)
	slashStore.Stores[0].URL += "/"
	storeURLWithSlash := admin.Sign(t, slashStore)

	return []vector{
		{"genesis", anchor(t, g), c[:1], true, 1},
		{"admin adds a user, user adds an own key, passkey admin re-signs", anchor(t, g), full, true, 4},
		{"anchor in the middle", anchor(t, v2), full[1:], true, 4},
		{"wrong anchor", anchor(t, v2), c[:1], false, 0},
		{"missing version", anchor(t, g), []trust.Signed{g, gap}, false, 0},
		{"old version again", anchor(t, g), []trust.Signed{g, v2, v2}, false, 0},
		{"tampered", anchor(t, g), []trust.Signed{g, tampered}, false, 0},
		{"user makes herself admin", anchor(t, g), []trust.Signed{g, v2, selfPromoted}, false, 0},
		{"unknown signer", anchor(t, g), []trust.Signed{g, foreign}, false, 0},
		{"wrong prev hash", anchor(t, g), []trust.Signed{g, badPrev}, false, 0},
		{"wrong installation", anchor(t, g), []trust.Signed{g, badInstall}, false, 0},
		{"successor without an admin", anchor(t, g), []trust.Signed{g, badSuccessor}, false, 0},
		{"genesis signed by a non-admin", anchor(t, nonAdminGenesis), []trust.Signed{nonAdminGenesis}, false, 0},
		{"user changes another user's key", anchor(t, g), []trust.Signed{g, v2, otherUsersKey}, false, 0},
		{"new admin signs her own addition", anchor(t, g), []trust.Signed{g, selfAdded}, false, 0},
		{"admin replaces the key that signs the version", anchor(t, g), []trust.Signed{g, replaced}, true, 2},
		{"old key signs after it was replaced", anchor(t, g), []trust.Signed{g, replaced, oldKey}, false, 0},
		{"password-too member with a passkey and a password key", anchor(t, g), []trust.Signed{g, carolBoth}, true, 2},
		{"password-too member without a passkey", anchor(t, g), []trust.Signed{g, carolNoPasskey}, false, 0},
		{"password-too member without a password key", anchor(t, g), []trust.Signed{g, carolNoPassword}, false, 0},
		{"API node without a TLS pin", anchor(t, g), []trust.Signed{g, apiWithoutPin}, false, 0},
		{"controller with a TLS pin", anchor(t, g), []trust.Signed{g, controllerWithPin}, false, 0},
		{"API node with a line feed in its URL", anchor(t, g), []trust.Signed{g, apiURLWithFeed}, false, 0},
		{"store URL with a trailing slash", anchor(t, g), []trust.Signed{g, storeURLWithSlash}, false, 0},
	}
}

// vectorErrors names the error each invalid vector must fail with, so that a
// vector cannot pass by failing for another rule than the one it breaks.
var vectorErrors = map[string]error{
	"wrong anchor":                               trust.ErrAnchor,
	"missing version":                            trust.ErrChain,
	"old version again":                          trust.ErrChain,
	"tampered":                                   proof.ErrSignature,
	"user makes herself admin":                   trust.ErrSigner,
	"unknown signer":                             trust.ErrSigner,
	"wrong prev hash":                            trust.ErrChain,
	"wrong installation":                         trust.ErrChain,
	"successor without an admin":                 trust.ErrInvalid,
	"genesis signed by a non-admin":              trust.ErrSigner,
	"user changes another user's key":            trust.ErrSigner,
	"new admin signs her own addition":           trust.ErrSigner,
	"old key signs after it was replaced":        trust.ErrSigner,
	"password-too member without a passkey":      trust.ErrInvalid,
	"password-too member without a password key": trust.ErrInvalid,
	"API node without a TLS pin":                 trust.ErrInvalid,
	"controller with a TLS pin":                  trust.ErrInvalid,
	"API node with a line feed in its URL":       trust.ErrInvalid,
	"store URL with a trailing slash":            trust.ErrInvalid,
}

func TestVectors(t *testing.T) {
	vs := vectors(t)
	for _, v := range vs {
		got, err := trust.Open(v.Anchor, v.Chain)
		if (err == nil) != v.Valid || (v.Valid && got.List.Seq != v.Seq) {
			t.Errorf("%s: want valid=%v seq=%d, got %v", v.Name, v.Valid, v.Seq, err)
		}
		if want, ok := vectorErrors[v.Name]; !v.Valid && (!ok || err != want) {
			t.Errorf("%s: got %v, want %v", v.Name, err, want)
		}
	}
	golden.Check(t, *update, "trust.json", vs)
}

func passkey(t *testing.T) proof.UserKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	spki, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return proof.UserKey{ID: sign.KeyID(spki), Kind: proof.KindPasskey, Alg: proof.AlgES256, Public: spki, CredID: []byte("stick-1")}
}

func TestValidate(t *testing.T) {
	if err := trusttest.Genesis(admin).Validate(); err != nil {
		t.Fatal(err)
	}
	pk := passkey(t)
	cases := map[string]func(l *trust.List){
		"no admin": func(l *trust.List) {
			l.Roles = append(l.Roles, trust.Role{Name: "user", Level: trust.LevelOff})
			for i := range l.Users {
				l.Users[i].Roles = []string{"user"}
			}
		},
		"unknown role": func(l *trust.List) { l.Users[0].Roles = []string{trust.RoleAdmin, "ghost"} },
		"duplicate key": func(l *trust.List) {
			l.Users = append(l.Users, trust.User{ID: trusttest.ID(0x22), Name: "bob", Roles: []string{trust.RoleAdmin}, Keys: l.Users[0].Keys})
		},
		"unsorted nodes": func(l *trust.List) { l.Nodes[0], l.Nodes[1] = l.Nodes[1], l.Nodes[0] },
		"passkey without rp": func(l *trust.List) {
			l.RP = proof.RP{}
			l.Users[0].Keys = append(l.Users[0].Keys, pk)
		},
		"password key in passkey-only role": func(l *trust.List) { l.Roles[0].PasskeyOnly = true },
		"bad list entry":                    func(l *trust.List) { l.Roles[0].Black = []string{"everything"} },
		"api without url":                   func(l *trust.List) { l.Nodes[0].URL = "" },
		"genesis with prev":                 func(l *trust.List) { l.Prev = make([]byte, 32) },
		"store without https":               func(l *trust.List) { l.Stores[0].URL = "http://store.ngc.example" },
		// An origin reaches web.json and the browser compares it as text, so it
		// is scheme, host and port and nothing else (trust.ValidOrigin).
		"origin with a path":         func(l *trust.List) { l.RP.Origins = []string{"https://ngc.example.org/x"} },
		"origin with a line feed":    func(l *trust.List) { l.RP.Origins = []string{"https://ngc.example.org\x0a"} },
		"origin in upper case":       func(l *trust.List) { l.RP.Origins = []string{"https://NGC.example.org"} },
		"AutoSystem with a key":      func(l *trust.List) { l.Auto[1].Key = make([]byte, ed25519.PublicKeySize) },
		"valid target without a key": func(l *trust.List) { l.Auto[0].Key = nil },
		"unknown target":             func(l *trust.List) { l.Auto = append(l.Auto, trust.Auto{Target: "unknown-target"}) },
		"store named local":          func(l *trust.List) { l.Stores[0].ID = trust.StoreLocal },
		"core app as standing approval target": func(l *trust.List) {
			l.Auto = []trust.Auto{l.Auto[0], {Target: "ngc.store", Key: make([]byte, ed25519.PublicKeySize)}, l.Auto[1]}
		},
		"passkey-only role that also asks for the password": func(l *trust.List) {
			l.Roles = append(l.Roles, trust.Role{Name: "strict", Level: trust.LevelCritical, PasskeyOnly: true, PasswordToo: true})
		},
		// admin2 holds only a passkey, so she also breaks the rule below that
		// a password-too member needs a password key: no user can be in both
		// kinds of role, whatever her keys.
		"user in a passkey-only and a password-too role": func(l *trust.List) {
			l.Roles = append(l.Roles, trust.Role{Name: "passkey-only", Level: trust.LevelCritical, PasskeyOnly: true},
				trust.Role{Name: "password-too", Level: trust.LevelCritical, PasswordToo: true})
			l.Users[0].Roles = []string{trust.RoleAdmin, "passkey-only", "password-too"}
		},
		"password-too member with only a password key": withCarol(trusttest.NewSigner(0x64).Key),
		"password-too member with only a passkey":      withCarol(pk),
		// The lock-out of the review: the password admin holds no passkey,
		// the passkey admin no password key.
		"admin role that asks for the password too": func(l *trust.List) { l.Roles[0].PasswordToo = true },
		"API node without a TLS pin":                func(l *trust.List) { l.Nodes[0].TLS = nil },
		"API node with a pin of 31 bytes":           func(l *trust.List) { l.Nodes[0].TLS = l.Nodes[0].TLS[:31] },
		"controller with a TLS pin":                 func(l *trust.List) { l.Nodes[1].TLS = bytes.Repeat([]byte{0xC1}, 32) },
	}
	for name, mutate := range cases {
		l := trusttest.Genesis(admin)
		mutate(l)
		if err := l.Validate(); err != trust.ErrInvalid {
			t.Errorf("%s: got %v", name, err)
		}
	}
	// Twins of cases above that keep every rule: each must pass, so the case
	// it mirrors fails for its own reason and not for a side effect of its
	// mutation. Genesis itself is the twin of "store named local", "API node
	// without a TLS pin" and "controller with a TLS pin".
	twins := map[string]func(l *trust.List){
		// A pin is a hash: any 32 bytes are one.
		"API node with another pin of 32 bytes": func(l *trust.List) { l.Nodes[0].TLS = bytes.Repeat([]byte{0x77}, 32) },
		"passkey with rp":                       func(l *trust.List) { l.Users[0].Keys = append(l.Users[0].Keys, pk) },
		"origin with a port":                    func(l *trust.List) { l.RP.Origins = []string{"https://ngc.example.org:8443"} },
		"app as standing approval target": func(l *trust.List) {
			l.Auto = []trust.Auto{l.Auto[0], {Target: "org.sorglos.terminal", Key: make([]byte, ed25519.PublicKeySize)}, l.Auto[1]}
		},
		"passkey-only role": func(l *trust.List) {
			l.Roles = append(l.Roles, trust.Role{Name: "strict", Level: trust.LevelCritical, PasskeyOnly: true})
		},
		"password-too role": func(l *trust.List) {
			l.Roles = append(l.Roles, trust.Role{Name: "strict", Level: trust.LevelCritical, PasswordToo: true})
		},
		"user in a passkey-only role while a password-too role exists": func(l *trust.List) {
			l.Roles = append(l.Roles, trust.Role{Name: "passkey-only", Level: trust.LevelCritical, PasskeyOnly: true},
				trust.Role{Name: "password-too", Level: trust.LevelCritical, PasswordToo: true})
			l.Users[0].Roles = []string{trust.RoleAdmin, "passkey-only"}
		},
		"password-too member with a passkey and a password key": withCarol(pk, trusttest.NewSigner(0x64).Key),
	}
	for name, mutate := range twins {
		l := trusttest.Genesis(admin)
		mutate(l)
		if err := l.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestTLSPin: the pin is SHA-256 of the certificate's SubjectPublicKeyInfo,
// which for an Ed25519 key is what x509.MarshalPKIXPublicKey writes. It names
// the key, not the certificate.
func TestTLSPin(t *testing.T) {
	cert, pin := trusttest.APITLS(0xE1, "api-1.test")
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	if got := trust.TLSPin(leaf.RawSubjectPublicKeyInfo); !bytes.Equal(got, want[:]) || !bytes.Equal(pin, want[:]) {
		t.Errorf("pin %x and APITLS's %x, want %x", got, pin, want)
	}
	spki, err := x509.MarshalPKIXPublicKey(cert.PrivateKey.(ed25519.PrivateKey).Public())
	if err != nil || !bytes.Equal(spki, leaf.RawSubjectPublicKeyInfo) {
		t.Errorf("SubjectPublicKeyInfo %x, key as PKIX %x, %v", leaf.RawSubjectPublicKeyInfo, spki, err)
	}
	if leaf.DNSNames[0] != "api-1.test" || leaf.SerialNumber.Int64() != 1 {
		t.Errorf("certificate for %v, serial %v", leaf.DNSNames, leaf.SerialNumber)
	}
	again, _ := trusttest.APITLS(0xE1, "api-1.test")
	_, sameKey := trusttest.APITLS(0xE1, "other.test")
	_, otherKey := trusttest.APITLS(0xE2, "api-1.test")
	if !bytes.Equal(again.Certificate[0], cert.Certificate[0]) {
		t.Error("APITLS made other bytes for the same fill and host")
	}
	if !bytes.Equal(sameKey, pin) || bytes.Equal(otherKey, pin) {
		t.Error("the pin follows the certificate instead of the key")
	}
}

// TestValidAPINode breaks one rule per case, starting from api-1 of Genesis.
func TestValidAPINode(t *testing.T) {
	api := trusttest.Genesis(admin).Nodes[0]
	if !trust.ValidAPINode(api) {
		t.Fatal("api-1 of Genesis refused")
	}
	host := func(n int) string { return "wss://" + trusttest.Host(n-18) + ":7443/tunnel" }
	for name, c := range map[string]struct {
		mutate func(n *trust.Node)
		valid  bool
	}{
		"URL of 256 bytes":             {func(n *trust.Node) { n.URL = host(256) }, true},
		"URL with an IPv6 host":        {func(n *trust.Node) { n.URL = "wss://[2001:db8::1]:7443/tunnel" }, true},
		"URL without a port":           {func(n *trust.Node) { n.URL = "wss://api-1.test/tunnel" }, false},
		"URL with a line feed":         {func(n *trust.Node) { n.URL += "\x0a" }, false},
		"name of 64 bytes of umlauts":  {func(n *trust.Node) { n.Name = strings.Repeat("\u00e4", 32) }, true},
		"kind controller":              {func(n *trust.Node) { n.Kind = trust.NodeController }, false},
		"ID not a name":                {func(n *trust.Node) { n.ID = "API 1" }, false},
		"no name":                      {func(n *trust.Node) { n.Name = "" }, false},
		"name with a line feed":        {func(n *trust.Node) { n.Name = "API\n1" }, false},
		"name of 65 bytes":             {func(n *trust.Node) { n.Name = strings.Repeat("a", 65) }, false},
		"Noise key of 31 bytes":        {func(n *trust.Node) { n.Noise = n.Noise[:31] }, false},
		"no signing key":               {func(n *trust.Node) { n.Sign = nil }, false},
		"URL with https":               {func(n *trust.Node) { n.URL = "https://api-1.test:7443/tunnel" }, false},
		"URL of 257 bytes":             {func(n *trust.Node) { n.URL = host(257) }, false},
		"no TLS pin":                   {func(n *trust.Node) { n.TLS = nil }, false},
		"TLS pin of 33 bytes":          {func(n *trust.Node) { n.TLS = append(slices.Clone(n.TLS), 1) }, false},
		"root user":                    {func(n *trust.Node) { n.RootUsers = []string{trusttest.ID(0x21)} }, false},
		"root role":                    {func(n *trust.Node) { n.RootRoles = []string{trust.RoleAdmin} }, false},
		"empty root lists are no list": {func(n *trust.Node) { n.RootUsers, n.RootRoles = []string{}, []string{} }, true},
	} {
		n := api
		c.mutate(&n)
		if got := trust.ValidAPINode(n); got != c.valid {
			t.Errorf("%s: got %v, want %v", name, got, c.valid)
		}
	}
}

// TestValidStore breaks one rule per case, starting from the global store of
// Genesis.
func TestValidStore(t *testing.T) {
	global := trusttest.Genesis(admin).Stores[0]
	if !trust.ValidStore(global) {
		t.Fatal("global store of Genesis refused")
	}
	url := func(n int) string { return "https://" + trusttest.Host(n-8) }
	for name, c := range map[string]struct {
		mutate func(s *trust.Store)
		valid  bool
	}{
		"another ID":                {func(s *trust.Store) { s.ID = "other" }, true},
		"URL of 256 bytes":          {func(s *trust.Store) { s.URL = url(256) }, true},
		"URL with a path":           {func(s *trust.Store) { s.URL = "https://store.ngc.example/ngc" }, true},
		"name of 64 bytes":          {func(s *trust.Store) { s.Name = strings.Repeat("S", 64) }, true},
		"URL with a trailing slash": {func(s *trust.Store) { s.URL += "/" }, false},
		"URL with user info":        {func(s *trust.Store) { s.URL = "https://user@store.ngc.example" }, false},
		"ID local":                  {func(s *trust.Store) { s.ID = trust.StoreLocal }, false},
		"ID not a name":             {func(s *trust.Store) { s.ID = "Global" }, false},
		"no name":                   {func(s *trust.Store) { s.Name = "" }, false},
		"name with a line feed":     {func(s *trust.Store) { s.Name = "NGC\nStore" }, false},
		"name of 65 bytes":          {func(s *trust.Store) { s.Name = strings.Repeat("S", 65) }, false},
		"URL with http":             {func(s *trust.Store) { s.URL = "http://store.ngc.example" }, false},
		"URL of 257 bytes":          {func(s *trust.Store) { s.URL = url(257) }, false},
		"key of 31 bytes":           {func(s *trust.Store) { s.Key = s.Key[:31] }, false},
	} {
		s := global
		c.mutate(&s)
		if got := trust.ValidStore(s); got != c.valid {
			t.Errorf("%s: got %v, want %v", name, got, c.valid)
		}
		l := trusttest.Genesis(admin)
		l.Stores = []trust.Store{s}
		if err := l.Validate(); (err == nil) != c.valid {
			t.Errorf("%s: Validate gives %v", name, err)
		}
	}
}

// TestValidID: an ID has exactly one spelling, so a line break or padding
// around it, which encoding/base64 alone would skip, makes it invalid.
func TestValidID(t *testing.T) {
	id := trusttest.ID(0x11)
	if !trust.ValidID(id) {
		t.Fatalf("%s rejected", id)
	}
	for _, s := range []string{id + "\n", "\r\n" + id, id[:11] + "\n" + id[11:], id + "=", id[:21]} {
		if trust.ValidID(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

// withCarol returns a change that adds the role "careful", which asks for
// the password too, and carol in it with keys.
func withCarol(keys ...proof.UserKey) func(l *trust.List) {
	return func(l *trust.List) {
		l.Roles = append(l.Roles, trust.Role{Name: "careful", Level: trust.LevelCritical, PasswordToo: true})
		l.Users = append(l.Users, trust.User{ID: trusttest.ID(0x23), Name: "carol", Roles: []string{"careful"}, Keys: keys})
	}
}

func TestPasswordToo(t *testing.T) {
	l := trusttest.Genesis(admin)
	l.Roles = append(l.Roles, trust.Role{Name: "careful", Level: trust.LevelCritical, PasswordToo: true})
	bob := trusttest.ID(0x22)
	l.Users = append(l.Users, trust.User{ID: bob, Name: "bob", Roles: []string{"careful"},
		Keys: []proof.UserKey{passkey(t), trusttest.NewSigner(5).Key}})
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	if !l.PasswordToo(bob) {
		t.Error("user in a password-too role: false")
	}
	if l.PasswordToo(l.Users[0].ID) || l.PasswordToo(l.Users[1].ID) {
		t.Error("admins without a password-too role: true")
	}
	if l.PasswordToo(trusttest.ID(0x99)) {
		t.Error("unknown user: true")
	}
}

func TestPolicy(t *testing.T) {
	l := trusttest.Genesis(admin)
	l.Roles = []trust.Role{
		{Name: trust.RoleAdmin, Level: trust.LevelCritical, White: []string{"app/ngc.store/install"}},
		{Name: "ops", Level: trust.LevelHardcore},
		{Name: "user", Level: trust.LevelOff, Black: []string{"app/org.sorglos.terminal"}},
	}
	bob, carol := trusttest.ID(0x22), trusttest.ID(0x23)
	l.Users = append(l.Users,
		trust.User{ID: bob, Name: "bob", Roles: []string{"user"}, Keys: []proof.UserKey{trusttest.NewSigner(5).Key}},
		trust.User{ID: carol, Name: "carol", Roles: []string{"ops", "user"}, Keys: []proof.UserKey{trusttest.NewSigner(6).Key}})
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	adminID := l.Users[0].ID
	shell := trust.Op{App: "org.sorglos.terminal", Action: "shell", Node: "ctl-1", Changes: true}
	calc := trust.Op{App: "org.sorglos.calculator", Action: "save", Node: "ctl-1", Changes: true}
	cases := []struct {
		name string
		user string
		op   trust.Op
		want bool
	}{
		{"admin, normal channel", adminID, shell, false},
		{"admin, critical", adminID, trust.Op{App: "ngc.store", Action: "upload", Critical: true}, true},
		{"admin, whitelisted", adminID, trust.Op{App: "ngc.store", Action: "install", Root: true, Critical: true}, false},
		{"user, blacklisted", bob, shell, true},
		{"user, level off", bob, calc, false},
		{"strictest role wins", carol, calc, true},
		{"unknown user", trusttest.ID(0x99), calc, true},
	}
	for _, c := range cases {
		if got := l.NeedsConfirm(c.user, c.op); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	if !l.MayRoot(adminID, "ctl-1") || l.MayRoot(bob, "ctl-1") || l.MayRoot(adminID, "api-1") {
		t.Error("root permissions wrong")
	}
}

// allocated returns how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// hostile returns prefix, then elem as often as fits, then suffix: exactly
// size bytes.
func hostile(prefix, elem, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + elem)
	for b.Len()+1+len(elem)+len(suffix) <= size {
		b.WriteString("," + elem)
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParseBoundsAllocation feeds Parse the review's probe, a payload of the
// full size with more array elements than Validate allows, before any
// signature is checked: refusing it must allocate next to nothing.
func TestParseBoundsAllocation(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"nodes", hostile(`{"nodes":[`, `{}`, `]}`, sign.MaxPayload)},
		{"users", hostile(`{"users":[`, `{}`, `]}`, sign.MaxPayload)},
		{"keys of one user", hostile(`{"users":[{"keys":[`, `{}`, `]}]}`, sign.MaxPayload)},
		{"roles of one user", hostile(`{"users":[{"roles":[`, `""`, `]}]}`, sign.MaxPayload)},
		{"an array no list has", hostile(`{"rp":{"id":[`, `{}`, `]}}`, sign.MaxPayload)},
	}
	for _, c := range cases {
		var err error
		if n := allocated(func() { _, err = trust.Parse(c.in) }); n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != trust.ErrInvalid {
			t.Errorf("%s: got %v, want %v", c.name, err, trust.ErrInvalid)
		}
	}
}

// TestSignatureBeforeDecoding: a payload within every budget can still cost
// tens of MiB to decode (10000 users, each with 16 empty keys and 114 empty
// role names). Open and Advance check the hash or the proof first, so such a
// payload that nobody listed signed costs no more than hashing it.
func TestSignatureBeforeDecoding(t *testing.T) {
	user := `{"keys":[` + strings.Repeat(`{},`, 15) + `{}],"roles":[` + strings.Repeat(`"",`, 113) + `""]}`
	payload := []byte(`{"users":[` + strings.Repeat(user+",", trust.MaxUsers-1) + user + `]}`)
	if len(payload) > sign.MaxPayload {
		t.Fatalf("payload of %d bytes", len(payload))
	}
	g := admin.Sign(t, trusttest.Genesis(admin))
	v1, err := trust.Open(anchor(t, g), []trust.Signed{g})
	if err != nil {
		t.Fatal(err)
	}
	unsigned := trust.Signed{Payload: payload, Proof: mallory.Prove(sign.PurposeTrustList, payload)}
	var errAdvance, errOpen error
	if n := allocated(func() { _, errAdvance = trust.Advance(v1, []trust.Signed{unsigned}) }); n > 2*uint64(len(payload)) {
		t.Errorf("Advance: %d bytes allocated for a %d-byte payload", n, len(payload))
	}
	if errAdvance != trust.ErrSigner {
		t.Errorf("Advance: got %v, want %v", errAdvance, trust.ErrSigner)
	}
	if n := allocated(func() { _, errOpen = trust.Open(anchor(t, g), []trust.Signed{unsigned}) }); n > 64<<10 {
		t.Errorf("Open: %d bytes allocated", n)
	}
	if errOpen != trust.ErrAnchor {
		t.Errorf("Open: got %v, want %v", errOpen, trust.ErrAnchor)
	}
}

// signedSize is the budget ParseSigned documents: the base64url of
// sign.MaxPayload bytes plus 64 KiB.
var signedSize = enc.B64.EncodedLen(sign.MaxPayload) + 64<<10

func TestParseSigned(t *testing.T) {
	g := admin.Sign(t, trusttest.Genesis(admin))
	good, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	got, err := trust.ParseSigned(good)
	if err != nil {
		t.Fatalf("valid version: %v", err)
	}
	if _, err := trust.Open(anchor(t, g), []trust.Signed{got}); err != nil {
		t.Errorf("open the parsed version: %v", err)
	}
	byPasskey, err := json.Marshal(passkeyAdmin.Sign(t, trusttest.Genesis(admin)))
	if err != nil {
		t.Fatal(err)
	}
	proofJSON, err := json.Marshal(g.Proof)
	if err != nil {
		t.Fatal(err)
	}
	payload := `"payload":"` + enc.B64.EncodeToString(g.Payload) + `"`
	prf := `"proof":` + string(proofJSON)
	doc := func(members ...string) []byte { return []byte("{" + strings.Join(members, ",") + "}") }
	for name, in := range map[string][]byte{
		"members in another order": doc(prf, payload),
		"proof by a passkey":       byPasskey,
	} {
		if _, err := trust.ParseSigned(in); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	cases := map[string][]byte{
		"empty payload":                doc(`"payload":""`, prf),
		"no payload":                   doc(prf),
		"no proof":                     doc(payload),
		"proof of an unknown kind":     doc(payload, `"proof":{"kind":"x","kid":"`+g.Proof.KeyID+`","sig":"AQ"}`),
		"password proof with authData": doc(payload, `"proof":{"kind":"password","kid":"`+g.Proof.KeyID+`","sig":"AQ","authData":"AQ"}`),
		"payload over sign.MaxPayload": doc(`"payload":"`+enc.B64.EncodeToString(make([]byte, sign.MaxPayload+1))+`"`, prf),
		"unknown member":               doc(payload, prf, `"x":1`),
		"duplicate member":             doc(payload, payload, prf),
		"member name in another case":  doc(`"Payload"`+payload[len(`"payload"`):], prf),
		"null":                         doc(payload, `"proof":null`),
		"trailing data":                append(bytes.Clone(good), `{}`...),
		"not an object":                []byte(`[]`),
		"over the size of its budget":  append(bytes.Clone(good), bytes.Repeat([]byte(" "), signedSize)...),
	}
	for name, in := range cases {
		if _, err := trust.ParseSigned(in); err != trust.ErrInvalid {
			t.Errorf("%s: got %v, want %v", name, err, trust.ErrInvalid)
		}
	}
}

// TestParseSignedBoundsAllocation feeds ParseSigned documents of the full size
// of its budget, in shapes the budget refuses: refusing them must allocate
// next to nothing.
func TestParseSignedBoundsAllocation(t *testing.T) {
	nested := strings.Repeat(`{"a":`, 40) + `1` + strings.Repeat(`}`, 40)
	cases := []struct {
		name string
		in   []byte
	}{
		{"payload as an array", hostile(`{"payload":[`, `{}`, `]}`, signedSize)},
		{"signature as an array", hostile(`{"proof":{"sig":[`, `{}`, `]}}`, signedSize)},
		{"too many members", hostile(`{`, `"x":1`, `}`, signedSize)},
		{"nested too deep", append(bytes.Repeat([]byte(" "), signedSize-len(nested)), nested...)},
		{"a member name too long", []byte(`{"` + strings.Repeat("a", signedSize-6) + `":1}`)},
	}
	for _, c := range cases {
		if len(c.in) != signedSize {
			t.Fatalf("%s: %d bytes, want %d", c.name, len(c.in), signedSize)
		}
		var err error
		n := allocated(func() { _, err = trust.ParseSigned(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != trust.ErrInvalid {
			t.Errorf("%s: got %v, want %v", c.name, err, trust.ErrInvalid)
		}
	}
}

func FuzzParseSigned(f *testing.F) {
	b, err := json.Marshal(admin.Sign(f, trusttest.Genesis(admin)))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := trust.ParseSigned(b)
		if err != nil {
			return
		}
		// What ParseSigned accepts, it accepts again as encoding/json writes
		// it, with the same payload.
		again, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		back, err := trust.ParseSigned(again)
		if err != nil || !bytes.Equal(back.Payload, s.Payload) || back.Proof.KeyID != s.Proof.KeyID {
			t.Fatalf("%s does not read back: %v", again, err)
		}
	})
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(admin.Sign(f, trusttest.Genesis(admin)).Payload))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = trust.Parse(b)
	})
}

// FuzzOpen fuzzes the chain-verification path, not just Parse: arbitrary
// bytes are decoded as a []trust.Signed (a decode failure is skipped, since
// that is a wire-level concern, not a trust-level one) and handed to Open
// against a fixed anchor. It must never panic, however malformed the chain.
func FuzzOpen(f *testing.F) {
	c := chain(f)
	a := anchor(f, c[0])
	seed, err := json.Marshal(c)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Fuzz(func(t *testing.T, b []byte) {
		var got []trust.Signed
		if json.Unmarshal(b, &got) != nil {
			return
		}
		_, _ = trust.Open(a, got)
	})
}
