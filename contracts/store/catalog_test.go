package store

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"runtime"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

const issued = 1790000000

var (
	storeKey   = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	releaseKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize))
	global     = trust.Store{ID: GlobalID, Name: "NGC Store", URL: "https://store.ngc.test", Key: enc.Bytes(storeKey.Public().(ed25519.PublicKey))}
	install, _ = trusttest.NodeKeys(0x91) // X25519 key pair of the installation
)

func signJSON(t testing.TB, priv ed25519.PrivateKey, purpose string, v any) sign.Envelope {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, purpose, b)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func testCatalog() Catalog {
	sum := bytes.Repeat([]byte{0x01}, 32)
	return Catalog{Store: GlobalID, Seq: 5, Issued: issued, Apps: []Entry{
		{ID: "org.sorglos.calculator", Name: "Rechner", Publisher: "Sorglos IT",
			Versions: []Package{{Version: "1.0.0", URL: "org.sorglos.calculator-1.0.0.ngc", Size: 12345, SHA256: sum}}},
		{ID: "org.sorglos.terminal", Name: "Terminal", Publisher: "Sorglos IT",
			Price: &Price{Model: PriceSubscription, Per: PerSite, Amount: 499, Currency: "EUR", Period: PeriodMonth, Shop: "https://store.ngc.test/buy/terminal"},
			Versions: []Package{
				{Version: "1.0.0", URL: "https://cdn.ngc.test/terminal-1.0.0.ngc", Size: 5000000, SHA256: sum, Encrypted: true},
				{Version: "1.1.0", URL: "https://cdn.ngc.test/terminal-1.1.0.ngc", Size: 5100000, SHA256: sum, Encrypted: true},
			}},
	}}
}

func testLicense() License {
	return License{ID: "L-1", Store: GlobalID, App: "org.sorglos.terminal", Installation: trusttest.ID(0x11),
		InstallKey: install.Public, Per: PerUser, Users: 5, Issued: issued, Expires: issued + 30*86400,
		Key: &WrappedKey{Ephemeral: bytes.Repeat([]byte{2}, 32), Nonce: bytes.Repeat([]byte{3}, 12), Sealed: bytes.Repeat([]byte{4}, 48)}}
}

func testRelease() Release {
	sum := bytes.Repeat([]byte{0x05}, 32)
	f := func(part, arch string) File {
		return File{Part: part, Arch: arch, URL: "https://releases.ngc.test/1.1.0/" + part + "-" + arch + ".tar", Size: 1000, SHA256: sum}
	}
	return Release{Seq: 3, Issued: issued, Version: "1.1.0", Notes: "Fehler behoben.", Files: []File{
		f(ReleaseWeb, "all"), f(ReleaseAPI, "amd64"), f(ReleaseAPI, "arm64"), f(ReleaseController, "amd64"), f(ReleaseController, "arm64"),
	}}
}

func TestCatalog(t *testing.T) {
	env := signJSON(t, storeKey, sign.PurposeCatalog, testCatalog())
	if c, err := VerifyCatalog(env, global, 5); err != nil || len(c.Apps) != 2 {
		t.Fatalf("valid: %v", err)
	}
	if _, err := VerifyCatalog(env, global, 6); err != ErrRollback {
		t.Errorf("older catalog: %v", err)
	}
	evil := testCatalog()
	evil.Store = "evil"
	if _, err := VerifyCatalog(signJSON(t, storeKey, sign.PurposeCatalog, evil), global, 0); err != ErrCatalog {
		t.Errorf("other store: %v", err)
	}
	other := global
	other.Key = enc.Bytes(releaseKey.Public().(ed25519.PublicKey))
	if _, err := VerifyCatalog(env, other, 0); err != sign.ErrWrongKey {
		t.Errorf("other key: %v", err)
	}
	needs := testCatalog()
	needs.Apps[1].Versions[1].Needs = []string{NeedNetwork, NeedRoot}
	if c, err := VerifyCatalog(signJSON(t, storeKey, sign.PurposeCatalog, needs), global, 0); err != nil || len(c.Apps[1].Versions[1].Needs) != 2 {
		t.Errorf("both needs: %v", err)
	}
}

func TestCatalogRejects(t *testing.T) {
	cases := map[string]func(c *Catalog){
		"unsorted":            func(c *Catalog) { c.Apps[0], c.Apps[1] = c.Apps[1], c.Apps[0] },
		"paid but open":       func(c *Catalog) { c.Apps[1].Versions[0].Encrypted = false },
		"free but encrypted":  func(c *Catalog) { c.Apps[0].Versions[0].Encrypted = true },
		"versions not rising": func(c *Catalog) { c.Apps[1].Versions[1].Version = "0.9.0" },
		"short checksum":      func(c *Catalog) { c.Apps[0].Versions[0].SHA256 = []byte{1} },
		"subscription period": func(c *Catalog) { c.Apps[1].Price.Period = "" },
		"url with ..":         func(c *Catalog) { c.Apps[0].Versions[0].URL = "../secret.ngc" },
		"plain http":          func(c *Catalog) { c.Apps[1].Versions[0].URL = "http://cdn.ngc.test/x.ngc" },
		"core app":            func(c *Catalog) { c.Apps[0].ID = "ngc.calculator" },
		"no versions":         func(c *Catalog) { c.Apps[0].Versions = nil },
	}
	for name, mutate := range cases {
		c := testCatalog()
		mutate(&c)
		if err := c.Validate(); err != ErrCatalog {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestEmptyCatalog: a store without apps is legitimate; its catalog leaves
// the list out instead of sending null, and verifies.
func TestEmptyCatalog(t *testing.T) {
	c := Catalog{Store: GlobalID, Seq: 1, Issued: issued}
	b, err := json.Marshal(c)
	if err != nil || string(b) != `{"store":"global","seq":1,"issued":1790000000}` {
		t.Fatalf("got %s, %v", b, err)
	}
	got, err := VerifyCatalog(signJSON(t, storeKey, sign.PurposeCatalog, c), global, 0)
	if err != nil || len(got.Apps) != 0 {
		t.Errorf("empty catalog: %+v, %v", got, err)
	}
	if _, err := parseCatalog([]byte(`{"store":"global","seq":1,"issued":1790000000,"apps":[]}`)); err != nil {
		t.Errorf("empty list: %v", err)
	}
	if _, err := parseCatalog([]byte(`{"store":"global","seq":1,"issued":1790000000,"apps":null}`)); err != ErrCatalog {
		t.Errorf("null: %v", err)
	}
}

func TestLicense(t *testing.T) {
	env := signJSON(t, storeKey, sign.PurposeLicense, testLicense())
	l, err := VerifyLicense(env, global, trusttest.ID(0x11), install.Public)
	if err != nil {
		t.Fatalf("valid: %v", err)
	}
	if _, err := VerifyLicense(env, global, trusttest.ID(0x12), install.Public); err != ErrLicense {
		t.Errorf("other installation: %v", err)
	}
	if _, err := VerifyLicense(env, global, trusttest.ID(0x11), bytes.Repeat([]byte{9}, 32)); err != ErrLicense {
		t.Errorf("other installation key: %v", err)
	}
	for _, c := range []struct {
		now  int64
		want LicenseState
	}{
		{issued + 86400, LicenseValid},
		{l.Expires, LicenseGrace},
		{l.Expires + GraceDefault - 1, LicenseGrace},
		{l.Expires + GraceDefault, LicenseExpired},
	} {
		if got := l.State(c.now); got != c.want {
			t.Errorf("State(%d) = %v, want %v", c.now, got, c.want)
		}
	}
	bought := testLicense()
	bought.Expires = 0
	if bought.State(issued+100*365*86400) != LicenseValid {
		t.Error("a bought license never expires")
	}
	noUsers := testLicense()
	noUsers.Users = 0
	if noUsers.Validate() != ErrLicense {
		t.Error("per-user license without user count accepted")
	}
	noKey := testLicense()
	noKey.Key = nil
	if noKey.Validate() != ErrLicense {
		t.Error("license without key accepted")
	}
	b, err := json.Marshal(noKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseLicense(b); err != ErrLicense {
		t.Errorf("license with key null: %v", err)
	}
}

func TestRelease(t *testing.T) {
	env := signJSON(t, releaseKey, sign.PurposeRelease, testRelease())
	pub := releaseKey.Public().(ed25519.PublicKey)
	if _, err := VerifyRelease(env, pub, 3); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if _, err := VerifyRelease(env, pub, 4); err != ErrRollback {
		t.Errorf("older release: %v", err)
	}
	for name, mutate := range map[string]func(r *Release){
		"duplicate file":           func(r *Release) { r.Files = append(r.Files, r.Files[1]) },
		"unknown part":             func(r *Release) { r.Files[0].Part = "db" },
		"web per arch":             func(r *Release) { r.Files[0].Arch = "amd64" },
		"version":                  func(r *Release) { r.Version = "1.1" },
		"no files":                 func(r *Release) { r.Files = nil },
		"old part name web":        func(r *Release) { r.Files[0].Part = "web" },
		"old part name api":        func(r *Release) { r.Files[1].Part = "api" },
		"old part name controller": func(r *Release) { r.Files[3].Part = "controller" },
		"component name db":        func(r *Release) { r.Files[1].Part = "ngc-db" },
	} {
		r := testRelease()
		mutate(&r)
		if err := r.Validate(); err != ErrRelease {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestUpdates(t *testing.T) {
	for _, target := range []string{trust.AutoWeb, trust.AutoSystem, "org.sorglos.terminal"} {
		if !ValidTarget(target) {
			t.Errorf("%s rejected", target)
		}
	}
	for _, target := range []string{"ngc-db", "ngc.store", ""} {
		if ValidTarget(target) {
			t.Errorf("%q accepted", target)
		}
	}
	r, _ := json.Marshal(UpdateReport{Target: trust.AutoAPI, Version: "1.1.0", State: UpdateRolledBack, Message: "API startete nicht, alte Version wiederhergestellt."})
	if _, err := ParseUpdateReport(r); err != nil {
		t.Errorf("report: %v", err)
	}
	for _, bad := range []string{`{"target":"ngc-api","state":"exploded"}`, `{"target":"ngc-api","state":"failed","message":null}`,
		`{"target":"ngc-api","state":"failed","version":"1.1"}`, `{"target":"ngc.store","state":"failed"}`,
		`{"target":"ngc-api","state":"failed","message":"a\nb"}`, `{"target":"ngc-api","state":"failed"} {}`} {
		if _, err := ParseUpdateReport([]byte(bad)); err != ErrUpdate {
			t.Errorf("bad report %s: %v", bad, err)
		}
	}
	l := trusttest.Genesis(trusttest.NewSigner(1))
	l.Auto = []trust.Auto{{Target: trust.AutoController, Key: enc.Bytes(releaseKey.Public().(ed25519.PublicKey))}, {Target: trust.AutoSystem}}
	pub := releaseKey.Public().(ed25519.PublicKey)
	if !Approved(l, trust.AutoController, pub) || Approved(l, trust.AutoAPI, pub) ||
		Approved(l, trust.AutoController, storeKey.Public().(ed25519.PublicKey)) || !Approved(l, trust.AutoSystem, nil) {
		t.Error("standing approvals")
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

// members returns prefix, then as many members "xN":0 as fit, then suffix:
// exactly size bytes.
func members(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + `"x0":0`)
	for i := 1; b.Len()+len(`,"x`+strconv.Itoa(i)+`":0`)+len(suffix) <= size; i++ {
		b.WriteString(`,"x` + strconv.Itoa(i) + `":0`)
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParsersBoundAllocation feeds each parser a document of its full size
// with more elements than its contract allows, or an array or object where
// it allows none: refusing it must allocate next to nothing.
func TestParsersBoundAllocation(t *testing.T) {
	catalog := func(b []byte) error { _, err := parseCatalog(b); return err }
	license := func(b []byte) error { _, err := parseLicense(b); return err }
	release := func(b []byte) error { _, err := parseRelease(b); return err }
	report := func(b []byte) error { _, err := ParseUpdateReport(b); return err }
	cases := []struct {
		name  string
		in    []byte
		parse func([]byte) error
		want  error
	}{
		{"catalog, apps", hostile(`{"apps":[`, `{}`, `]}`, sign.MaxPayload), catalog, ErrCatalog},
		{"catalog, versions", hostile(`{"apps":[{"versions":[`, `{}`, `]}]}`, sign.MaxPayload), catalog, ErrCatalog},
		{"catalog, needs", hostile(`{"apps":[{"versions":[{"needs":[`, `""`, `]}]}]}`, sign.MaxPayload), catalog, ErrCatalog},
		{"catalog, members of the price", members(`{"apps":[{"price":{`, `}}]}`, sign.MaxPayload), catalog, ErrCatalog},
		{"license, an array", hostile(`{"key":{"eph":[`, `{}`, `]}}`, MaxLicense), license, ErrLicense},
		{"license, members", members(`{"key":{`, `}}`, MaxLicense), license, ErrLicense},
		{"release, files", hostile(`{"files":[`, `{}`, `]}`, sign.MaxPayload), release, ErrRelease},
		{"release, an array", hostile(`{"notes":[`, `{}`, `]}`, sign.MaxPayload), release, ErrRelease},
		{"update report, an array", hostile(`{"target":[`, `{}`, `]}`, 4096), report, ErrUpdate},
	}
	for _, c := range cases {
		var err error
		n := allocated(func() { err = c.parse(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

type vector struct {
	StoreKey   enc.Bytes     `json:"storeKey"`
	Catalog    sign.Envelope `json:"catalog"`
	License    sign.Envelope `json:"license"`
	ReleaseKey enc.Bytes     `json:"releaseKey"`
	Release    sign.Envelope `json:"release"`
}

func TestVectors(t *testing.T) {
	golden.Check(t, *update, "store.json", vector{
		StoreKey:   global.Key,
		Catalog:    signJSON(t, storeKey, sign.PurposeCatalog, testCatalog()),
		License:    signJSON(t, storeKey, sign.PurposeLicense, testLicense()),
		ReleaseKey: enc.Bytes(releaseKey.Public().(ed25519.PublicKey)),
		Release:    signJSON(t, releaseKey, sign.PurposeRelease, testRelease()),
	})
}

func FuzzCatalog(f *testing.F) {
	b, _ := json.Marshal(testCatalog())
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseCatalog(b)
		_, _ = parseLicense(b)
		_, _ = parseRelease(b)
		_, _ = ParseUpdateReport(b)
	})
}
