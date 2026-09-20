package trust_test

import (
	"crypto/ecdsa"
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
	admin   = trusttest.NewSigner(1)
	alice   = trusttest.NewSigner(2)
	mallory = trusttest.NewSigner(3)
)

func anchor(t *testing.T, s trust.Signed) trust.Anchor {
	t.Helper()
	l, err := trust.Parse(s.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return trust.Anchor{Seq: l.Seq, Hash: trust.Hash(s)}
}

// chain returns version 1 (genesis), version 2 (the admin adds alice) and
// version 3 (alice adds a second key of her own, without an admin).
func chain(t *testing.T) []trust.Signed {
	g := admin.Sign(t, trusttest.Genesis(admin))
	l2 := trusttest.Next(t, g)
	l2.Roles = append(l2.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	l2.Users = append(l2.Users, trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"}, Keys: []proof.UserKey{alice.Key}})
	v2 := admin.Sign(t, l2)
	l3 := trusttest.Next(t, v2)
	l3.Users[1].Keys = append(l3.Users[1].Keys, trusttest.NewSigner(4).Key)
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

type vector struct {
	Name   string         `json:"name"`
	Anchor trust.Anchor   `json:"anchor"`
	Chain  []trust.Signed `json:"chain"`
	Valid  bool           `json:"valid"`
	Seq    uint64         `json:"seq,omitempty"`
}

func vectors(t *testing.T) []vector {
	c := chain(t)
	g, v2 := c[0], c[1]

	l, err := trust.Parse(v2.Payload)
	if err != nil {
		t.Fatal(err)
	}
	l.Users[1].Roles = []string{trust.RoleAdmin, "user"}
	b, _ := json.Marshal(l)
	tampered := trust.Signed{Payload: b, Proof: v2.Proof}

	l3 := trusttest.Next(t, v2)
	l3.Users[1].Roles = []string{trust.RoleAdmin, "user"}
	selfPromoted := alice.Sign(t, l3)

	foreign := mallory.Sign(t, trusttest.Next(t, g))
	gap := admin.Sign(t, trusttest.Next(t, v2))

	return []vector{
		{"genesis", anchor(t, g), c[:1], true, 1},
		{"admin adds a user, user adds an own key", anchor(t, g), c, true, 3},
		{"anchor in the middle", anchor(t, v2), c[1:], true, 3},
		{"wrong anchor", anchor(t, v2), c[:1], false, 0},
		{"missing version", anchor(t, g), []trust.Signed{g, gap}, false, 0},
		{"old version again", anchor(t, g), []trust.Signed{g, v2, v2}, false, 0},
		{"tampered", anchor(t, g), []trust.Signed{g, tampered}, false, 0},
		{"user makes herself admin", anchor(t, g), []trust.Signed{g, v2, selfPromoted}, false, 0},
		{"unknown signer", anchor(t, g), []trust.Signed{g, foreign}, false, 0},
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
			l.Users[0].Roles = []string{"user"}
		},
		"unknown role": func(l *trust.List) { l.Users[0].Roles = []string{trust.RoleAdmin, "ghost"} },
		"duplicate key": func(l *trust.List) {
			l.Users = append(l.Users, trust.User{ID: trusttest.ID(0x22), Name: "bob", Roles: []string{trust.RoleAdmin}, Keys: l.Users[0].Keys})
		},
		"unsorted nodes":                    func(l *trust.List) { l.Nodes[0], l.Nodes[1] = l.Nodes[1], l.Nodes[0] },
		"passkey without rp":                func(l *trust.List) { l.Users[0].Keys = append(l.Users[0].Keys, pk) },
		"password key in passkey-only role": func(l *trust.List) { l.Roles[0].PasskeyOnly = true },
		"bad list entry":                    func(l *trust.List) { l.Roles[0].Black = []string{"everything"} },
		"api without url":                   func(l *trust.List) { l.Nodes[0].URL = "" },
		"genesis with prev":                 func(l *trust.List) { l.Prev = make([]byte, 32) },
	}
	for name, mutate := range cases {
		l := trusttest.Genesis(admin)
		mutate(l)
		if err := l.Validate(); err != trust.ErrInvalid {
			t.Errorf("%s: got %v", name, err)
		}
	}
	l := trusttest.Genesis(admin)
	l.RP = proof.RP{ID: "ngc.example.org", Origins: []string{"https://ngc.example.org"}}
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
