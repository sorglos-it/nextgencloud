package trust_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

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
// accident (see checkSigner's genesis parameter).
func genesisByNonAdmin(t *testing.T) trust.Signed {
	t.Helper()
	g := trusttest.Genesis(admin)
	g.Roles = append(g.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	g.Users = append(g.Users, trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"}, Keys: []proof.UserKey{alice.Key}})
	return alice.Sign(t, g)
}

// TestGenesisSignerMustBeAdmin proves the version-1 admin check does not
// depend on prev and next being the same pointer: a listed non-admin is
// refused even though she is a real, verifiable signer with a real key.
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
	}
}

func TestVectors(t *testing.T) {
	vs := vectors(t)
	for _, v := range vs {
		got, err := trust.Open(v.Anchor, v.Chain)
		if (err == nil) != v.Valid || (v.Valid && got.List.Seq != v.Seq) {
			t.Errorf("%s: want valid=%v seq=%d, got %v", v.Name, v.Valid, v.Seq, err)
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
		"AutoSystem with a key":             func(l *trust.List) { l.Auto[1].Key = make([]byte, ed25519.PublicKeySize) },
		"valid target without a key":        func(l *trust.List) { l.Auto[0].Key = nil },
		"unknown target":                    func(l *trust.List) { l.Auto = append(l.Auto, trust.Auto{Target: "unknown-target"}) },
	}
	for name, mutate := range cases {
		l := trusttest.Genesis(admin)
		mutate(l)
		if err := l.Validate(); err != trust.ErrInvalid {
			t.Errorf("%s: got %v", name, err)
		}
	}
	l := trusttest.Genesis(admin)
	l.Users[0].Keys = append(l.Users[0].Keys, pk)
	if err := l.Validate(); err != nil {
		t.Errorf("passkey with rp: %v", err)
	}
}

func TestPolicy(t *testing.T) {
	l := trusttest.Genesis(admin)
	l.Roles = []trust.Role{
		{Name: trust.RoleAdmin, Level: trust.LevelCritical, White: []string{"app/ngc.store/update"}},
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
		{"admin, whitelisted", adminID, trust.Op{App: "ngc.store", Action: "update", Root: true, Critical: true}, false},
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
