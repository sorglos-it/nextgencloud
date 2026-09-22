package store

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite shared/testdata")

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
	ui := bytes.Repeat([]byte{0x06}, 32)
	return Catalog{Store: GlobalID, Seq: 5, Issued: issued, Apps: []Entry{
		{ID: "org.sorglos.calculator", Name: "Rechner", Publisher: "Sorglos IT",
			Versions: []Package{{Version: "1.0.0", URL: "org.sorglos.calculator-1.0.0.ngc", Size: 12345, SHA256: sum, UI: ui}}},
		{ID: "org.sorglos.terminal", Name: "Terminal", Publisher: "Sorglos IT",
			Price: &Price{Model: PriceSubscription, Per: PerSite, Amount: 499, Currency: "EUR", Period: PeriodMonth, Shop: "https://store.ngc.test/buy/terminal"},
			Versions: []Package{
				{Version: "1.0.0", URL: "https://cdn.ngc.test/terminal-1.0.0.ngc", Size: 5000000, SHA256: sum, UI: ui, Encrypted: true},
				{Version: "1.1.0", URL: "https://cdn.ngc.test/terminal-1.1.0.ngc", Size: 5100000, SHA256: sum, UI: ui, Encrypted: true},
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
		"version without ui":  func(c *Catalog) { c.Apps[0].Versions[0].UI = nil },
		"more versions than MaxCatalogVersions": func(c *Catalog) {
			c.Apps = nil
			for i := range MaxCatalogVersions/MaxVersions + 1 {
				e := Entry{ID: fmt.Sprintf("org.example.a%05d", i), Name: "App", Publisher: "P"}
				for j := range MaxVersions {
					e.Versions = append(e.Versions, Package{Version: fmt.Sprintf("1.0.%d", j), URL: "a.ngc", Size: 1,
						SHA256: make([]byte, 32), UI: make([]byte, 32)})
				}
				c.Apps = append(c.Apps, e)
			}
		},
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

// catalogShape is a catalog of apps apps, each with fields app and versions
// times version, all inside the budget but not valid.
func catalogShape(apps int, app string, versions int, version string) []byte {
	var b strings.Builder
	b.WriteString(`{"store":"global","seq":1,"issued":1,"apps":[`)
	for i := range apps {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{` + app + `"versions":[`)
		for j := range versions {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(version)
		}
		b.WriteString(`]}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// TestCatalogDecodeCost decodes the costliest shapes of catalog the budget
// lets through: every one costs less than 32 MiB, and one version more than
// MaxCatalogVersions is refused before anything is built.
func TestCatalogDecodeCost(t *testing.T) {
	perApp := MaxCatalogVersions / MaxApps
	// filled builds a shape whose string of every app or version is as long
	// as the payload allows; slots is how many such strings it has.
	filled := func(slots int, shape func(s string) []byte) []byte {
		n := (sign.MaxPayload - len(shape(""))) / slots
		return shape(strings.Repeat("x", n))
	}
	cases := []struct {
		name string
		doc  []byte
	}{
		{"most apps, each with a price and versions with two needs", catalogShape(MaxApps, `"price":{},`, perApp, `{"needs":["",""]}`)},
		{"fewest apps, full lists of versions with two needs", catalogShape(MaxCatalogVersions/MaxVersions, ``, MaxVersions, `{"needs":["",""]}`)},
		{"most apps with long descriptions", filled(MaxApps, func(s string) []byte {
			return catalogShape(MaxApps, `"description":"`+s+`",`, perApp, `{"needs":["",""]}`)
		})},
		{"versions with long URLs", filled(MaxCatalogVersions, func(s string) []byte {
			return catalogShape(MaxCatalogVersions/MaxVersions, ``, MaxVersions, `{"url":"`+s+`","needs":["",""]}`)
		})},
		{"versions with every field", catalogShape(MaxCatalogVersions/MaxVersions, ``, MaxVersions,
			`{"version":"","url":"","size":0,"sha256":"","ui":"","needs":["",""],"minNgc":""}`)},
	}
	for _, c := range cases {
		var got Catalog
		var err error
		n := allocated(func() { err = catalogBudget.Decode(c.doc, &got) })
		t.Logf("%s: %d bytes, %.1f MiB allocated", c.name, len(c.doc), float64(n)/(1<<20))
		if err != nil || len(c.doc) > sign.MaxPayload {
			t.Errorf("%s: %d bytes, not inside the budget: %v", c.name, len(c.doc), err)
		}
		if n > 32<<20 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
	}
	over := catalogShape(MaxCatalogVersions/MaxVersions+1, ``, MaxVersions, `{}`)
	var err error
	if n := allocated(func() { _, err = parseCatalog(over) }); n > 64<<10 || err != ErrCatalog {
		t.Errorf("one app of versions too many: %d bytes allocated, %v", n, err)
	}
}

// catalogReject is a catalog that VerifyCatalog(catalog, global, lastSeq)
// must refuse, for the reason its name gives.
type catalogReject struct {
	Name    string        `json:"name"`
	Catalog sign.Envelope `json:"catalog"`
	LastSeq uint64        `json:"lastSeq"`
	Valid   bool          `json:"valid"`
	want    error         // the error VerifyCatalog must return; not in the file
	bound   bool          // a valid catalog, refused only for the store it names; not in the file
}

// catalogRejects changes one thing of testCatalog per case and signs it with
// the store key for catalogs, unless the name says otherwise. Every lastSeq
// is 0, which refuses no seq, except where the name says otherwise.
func catalogRejects(t testing.TB) []catalogReject {
	signed := func(change func(c *Catalog)) sign.Envelope {
		c := testCatalog()
		change(&c)
		return signJSON(t, storeKey, sign.PurposeCatalog, c)
	}
	// Without its ui member, as a store that does not know it would write
	// the version.
	noUI := testCatalog()
	noUI.Apps[0].Versions[0].UI = nil
	b, err := json.Marshal(noUI)
	if err != nil || bytes.Count(b, []byte(`"ui":""`)) != 1 {
		t.Fatalf("%s, %v", b, err)
	}
	withoutUI, err := sign.Sign(storeKey, sign.PurposeCatalog, bytes.Replace(b, []byte(`,"ui":""`), nil, 1))
	if err != nil {
		t.Fatal(err)
	}
	return []catalogReject{
		{Name: "signed by another key", Catalog: signJSON(t, releaseKey, sign.PurposeCatalog, testCatalog()), want: sign.ErrWrongKey},
		{Name: "signed for purpose license", Catalog: signJSON(t, storeKey, sign.PurposeLicense, testCatalog()), want: sign.ErrBadSignature},
		{Name: "catalog of another store", Catalog: signed(func(c *Catalog) { c.Store = "other" }), want: ErrCatalog, bound: true},
		{Name: "older than the last one seen", Catalog: signJSON(t, storeKey, sign.PurposeCatalog, testCatalog()), LastSeq: 6, want: ErrRollback},
		{Name: "core app", Catalog: signed(func(c *Catalog) { c.Apps[0].ID = "ngc.calculator" }), want: ErrCatalog},
		// Ascending as text, not as versions: 1.9.0 is older than 1.10.0.
		{Name: "versions not ascending", Catalog: signed(func(c *Catalog) {
			c.Apps[1].Versions[0].Version, c.Apps[1].Versions[1].Version = "1.10.0", "1.9.0"
		}), want: ErrCatalog},
		{Name: "version without ui", Catalog: withoutUI, want: ErrCatalog},
		{Name: "ui of 31 bytes", Catalog: signed(func(c *Catalog) { c.Apps[0].Versions[0].UI = c.Apps[0].Versions[0].UI[:31] }), want: ErrCatalog},
		{Name: "plain http url", Catalog: signed(func(c *Catalog) { c.Apps[1].Versions[0].URL = "http://cdn.ngc.test/terminal-1.0.0.ngc" }), want: ErrCatalog},
		{Name: "encrypted without a price", Catalog: signed(func(c *Catalog) { c.Apps[0].Versions[0].Encrypted = true }), want: ErrCatalog},
	}
}

// licenseReject is a license that VerifyLicense(license, global,
// installation, installKey) must refuse, for the reason its name gives.
type licenseReject struct {
	Name    string        `json:"name"`
	License sign.Envelope `json:"license"`
	Valid   bool          `json:"valid"`
	want    error         // the error VerifyLicense must return; not in the file
	bound   bool          // a valid license, refused only for whom it names; not in the file
}

// licenseRejects changes one thing of testLicense per case and signs it
// with the store key for licenses.
func licenseRejects(t testing.TB) []licenseReject {
	signed := func(change func(l *License)) sign.Envelope {
		l := testLicense()
		change(&l)
		return signJSON(t, storeKey, sign.PurposeLicense, l)
	}
	other, _ := trusttest.NodeKeys(0x92)
	return []licenseReject{
		{Name: "other installation", License: signed(func(l *License) { l.Installation = trusttest.ID(0x12) }), want: ErrLicense, bound: true},
		{Name: "other install key", License: signed(func(l *License) { l.InstallKey = other.Public }), want: ErrLicense, bound: true},
		{Name: "other store", License: signed(func(l *License) { l.Store = "other" }), want: ErrLicense, bound: true},
		{Name: "per user without users", License: signed(func(l *License) { l.Users = 0 }), want: ErrLicense},
		{Name: "grace over 90 days", License: signed(func(l *License) { l.Grace = MaxGrace + 1 }), want: ErrLicense},
	}
}

// releaseReject is a release that VerifyRelease(release, releaseKey, lastSeq)
// must refuse, for the reason its name gives.
type releaseReject struct {
	Name    string        `json:"name"`
	Release sign.Envelope `json:"release"`
	LastSeq uint64        `json:"lastSeq"`
	Valid   bool          `json:"valid"`
	want    error         // the error VerifyRelease must return; not in the file
}

// releaseRejects changes one thing of testRelease per case and signs it with
// the release key for releases, unless the name says otherwise. Every
// lastSeq is 0, except where the name says otherwise.
func releaseRejects(t testing.TB) []releaseReject {
	signed := func(change func(r *Release)) sign.Envelope {
		r := testRelease()
		change(&r)
		return signJSON(t, releaseKey, sign.PurposeRelease, r)
	}
	return []releaseReject{
		{Name: "signed by the store key", Release: signJSON(t, storeKey, sign.PurposeRelease, testRelease()), want: sign.ErrWrongKey},
		{Name: "signed for purpose catalog", Release: signJSON(t, releaseKey, sign.PurposeCatalog, testRelease()), want: sign.ErrBadSignature},
		{Name: "old part name web", Release: signed(func(r *Release) { r.Files[0].Part = "web" }), want: ErrRelease},
		// A second file, not the same one twice.
		{Name: "two files for ngc-api amd64", Release: signed(func(r *Release) {
			twin := r.Files[1]
			twin.URL, twin.SHA256 = "https://releases.ngc.test/1.1.0/ngc-api-amd64-b.tar", bytes.Repeat([]byte{0x07}, 32)
			r.Files = slices.Insert(r.Files, 2, twin)
		}), want: ErrRelease},
		{Name: "older than the last one seen", Release: signJSON(t, releaseKey, sign.PurposeRelease, testRelease()), LastSeq: 4, want: ErrRollback},
	}
}

type vector struct {
	StoreKey       enc.Bytes       `json:"storeKey"`
	Catalog        sign.Envelope   `json:"catalog"`
	CatalogRejects []catalogReject `json:"catalogRejects"`
	Installation   string          `json:"installation"`
	InstallKey     enc.Bytes       `json:"installKey"`
	License        sign.Envelope   `json:"license"`
	LicenseRejects []licenseReject `json:"licenseRejects"`
	ReleaseKey     enc.Bytes       `json:"releaseKey"`
	Release        sign.Envelope   `json:"release"`
	ReleaseRejects []releaseReject `json:"releaseRejects"`
}

// TestVectors writes store.json: a catalog, a license and a release that
// verify, and rejects of each that are signed correctly but for their own
// rule. It checks every reject fails for the reason its name gives: one
// refused for its signature carries the valid payload, one refused for its
// seq is the valid document, and one refused for whom it names is valid on
// its own.
func TestVectors(t *testing.T) {
	releasePub := releaseKey.Public().(ed25519.PublicKey)
	v := vector{
		StoreKey:       global.Key,
		Catalog:        signJSON(t, storeKey, sign.PurposeCatalog, testCatalog()),
		CatalogRejects: catalogRejects(t),
		Installation:   trusttest.ID(0x11),
		InstallKey:     install.Public,
		License:        signJSON(t, storeKey, sign.PurposeLicense, testLicense()),
		LicenseRejects: licenseRejects(t),
		ReleaseKey:     enc.Bytes(releasePub),
		Release:        signJSON(t, releaseKey, sign.PurposeRelease, testRelease()),
		ReleaseRejects: releaseRejects(t),
	}
	if _, err := VerifyCatalog(v.Catalog, global, 0); err != nil {
		t.Errorf("catalog: %v", err)
	}
	if _, err := VerifyLicense(v.License, global, v.Installation, v.InstallKey); err != nil {
		t.Errorf("license: %v", err)
	}
	if _, err := VerifyRelease(v.Release, releasePub, 0); err != nil {
		t.Errorf("release: %v", err)
	}
	// only checks that r, refused with err, breaks no rule but the one err
	// names: valid is the valid document, parses whether r's payload is one.
	only := func(name string, r, valid sign.Envelope, err error, bound, parses bool) {
		t.Helper()
		switch {
		case err == sign.ErrWrongKey || err == sign.ErrBadSignature:
			if !bytes.Equal(r.Payload, valid.Payload) {
				t.Errorf("%s: refused for its signature, but its payload differs", name)
			}
		case err == ErrRollback:
			if !reflect.DeepEqual(r, valid) {
				t.Errorf("%s: refused for its seq, but not the valid document", name)
			}
		case parses != bound:
			t.Errorf("%s: payload parses %v, bound %v", name, parses, bound)
		}
	}
	for _, r := range v.CatalogRejects {
		if _, err := VerifyCatalog(r.Catalog, global, r.LastSeq); err != r.want || r.Valid {
			t.Errorf("%s: got %v, want %v", r.Name, err, r.want)
		}
		_, err := parseCatalog(r.Catalog.Payload)
		only(r.Name, r.Catalog, v.Catalog, r.want, r.bound, err == nil)
	}
	for _, r := range v.LicenseRejects {
		if _, err := VerifyLicense(r.License, global, v.Installation, v.InstallKey); err != r.want || r.Valid {
			t.Errorf("%s: got %v, want %v", r.Name, err, r.want)
		}
		_, err := parseLicense(r.License.Payload)
		only(r.Name, r.License, v.License, r.want, r.bound, err == nil)
	}
	for _, r := range v.ReleaseRejects {
		if _, err := VerifyRelease(r.Release, releasePub, r.LastSeq); err != r.want || r.Valid {
			t.Errorf("%s: got %v, want %v", r.Name, err, r.want)
		}
		_, err := parseRelease(r.Release.Payload)
		only(r.Name, r.Release, v.Release, r.want, false, err == nil)
	}
	golden.Check(t, *update, "store.json", v)
}

// releaseFile is the one file of the release vector for install scripts.
var releaseFile = []byte("NGC web test file\n")

// TestReleaseScriptVector writes what verify.sh, the reference for the ulh
// scripts, checks: the release key as PEM, the file and the signed release,
// each compared byte for byte.
func TestReleaseScriptVector(t *testing.T) {
	der, err := x509.MarshalPKIXPublicKey(releaseKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	sum := sha256.Sum256(releaseFile)
	env := signJSON(t, releaseKey, sign.PurposeRelease, Release{Seq: 3, Issued: issued, Version: "1.1.0",
		Files: []File{{Part: ReleaseWeb, Arch: "all", URL: "ngc-web-all.tar", Size: int64(len(releaseFile)), SHA256: sum[:]}}})
	release, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	release = append(release, '\n')

	// The key parsed back from the PEM verifies the release.
	block, rest := pem.Decode(keyPEM)
	if block == nil || block.Type != "PUBLIC KEY" || len(rest) != 0 {
		t.Fatalf("PEM %q", keyPEM)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("key of type %T", pub)
	}
	r, err := VerifyRelease(env, key, 0)
	if err != nil || len(r.Files) != 1 || r.Files[0].Size != 18 {
		t.Fatalf("release: %+v, %v", r, err)
	}
	// verify.sh builds the signed message itself.
	script, err := os.ReadFile(filepath.Join(golden.Dir, "release", "verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := string(sign.Message(sign.PurposeRelease, nil))
	if want := `printf '` + strings.TrimSuffix(prefix, "\x00") + `\0'`; !strings.HasSuffix(prefix, "\x00") || !bytes.Contains(script, []byte(want)) {
		t.Errorf("verify.sh does not build the message %q", prefix)
	}

	golden.CheckBytes(t, *update, "release/release-key.pem", keyPEM)
	golden.CheckBytes(t, *update, "release/ngc-web-all.tar", releaseFile)
	golden.CheckBytes(t, *update, "release/release.json", release)
}

// TestReleaseScript runs verify.sh on the release vector and on releases made
// for one case each. It needs a POSIX shell and the tools of the ulh scripts,
// jq among them; the Linux servers have them, and there the test must pass
// with the real jq. Elsewhere a stand-in for the three filters of verify.sh
// can take jq's place on PATH.
func TestReleaseScript(t *testing.T) {
	for _, tool := range []string{"sh", "openssl", "jq", "sha256sum", "od", "base64", "tr", "cut", "wc", "mktemp"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s missing", tool)
		}
	}
	vector := map[string][]byte{}
	for _, name := range []string{"release.json", "release-key.pem", "ngc-web-all.tar", "verify.sh"} {
		b, err := os.ReadFile(filepath.Join(golden.Dir, "release", name))
		if err != nil {
			t.Fatal(err)
		}
		vector[name] = b
	}
	script := filepath.Join(t.TempDir(), "verify.sh")
	if err := os.WriteFile(script, vector["verify.sh"], 0o644); err != nil {
		t.Fatal(err)
	}
	checksum := func(b []byte) enc.Bytes { s := sha256.Sum256(b); return s[:] }
	api := []byte("NGC api test file\n")
	web := File{Part: ReleaseWeb, Arch: "all", URL: "ngc-web-all.tar", Size: int64(len(releaseFile)), SHA256: checksum(releaseFile)}
	apiAMD := File{Part: ReleaseAPI, Arch: "amd64", URL: "https://example.org/1.1.0/ngc-api-amd64.tar", Size: int64(len(api)), SHA256: checksum(api)}
	apiARM := File{Part: ReleaseAPI, Arch: "arm64", URL: "https://example.org/1.1.0/ngc-api-arm64.tar", Size: 1, SHA256: checksum([]byte("x"))}
	webAt := func(url string) File { f := web; f.URL = url; return f }
	payload := func(files ...File) []byte {
		b, err := json.Marshal(Release{Seq: 3, Issued: issued, Version: "1.1.0", Files: files})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// release is release.json with payload signed by the release key, after
	// change if set.
	release := func(payload []byte, change func(env map[string]string)) []byte {
		env, err := sign.Sign(releaseKey, sign.PurposeRelease, payload)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{"payload": enc.B64.EncodeToString(env.Payload), "kid": env.KeyID, "sig": enc.B64.EncodeToString(env.Sig)}
		if change != nil {
			change(m)
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return append(b, '\n')
	}
	// dir is a release directory: release.json, the release key and files.
	dir := func(releaseJSON []byte, files map[string][]byte) map[string][]byte {
		d := map[string][]byte{"release.json": releaseJSON, "release-key.pem": vector["release-key.pem"]}
		for name, b := range files {
			d[name] = b
		}
		return d
	}
	webOnly := map[string][]byte{"ngc-web-all.tar": releaseFile}
	otherKey := func() []byte {
		der, err := x509.MarshalPKIXPublicKey(storeKey.Public())
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	}()
	withKey := func(d map[string][]byte, key []byte) map[string][]byte { d["release-key.pem"] = key; return d }
	unusedBit := func(m map[string]string) { // sets an unused bit of the signature's last letter
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
		s := m["sig"]
		m["sig"] = s[:len(s)-1] + string(alphabet[strings.IndexByte(alphabet, s[len(s)-1])|1])
	}
	webArgs := []string{ReleaseWeb, "all"}

	cases := []struct {
		name  string
		files map[string][]byte
		args  []string
		why   string // what verify.sh says of a release it refuses; "" if it passes
	}{
		{"release vector", map[string][]byte{"release.json": vector["release.json"], "release-key.pem": vector["release-key.pem"],
			"ngc-web-all.tar": vector["ngc-web-all.tar"]}, webArgs, ""},
		{"url of https", dir(release(payload(webAt("https://example.org/1.1.0/ngc-web-all.tar")), nil), webOnly), webArgs, ""},
		{"url with a directory", dir(release(payload(webAt("sub/ngc-web-all.tar")), nil), webOnly), webArgs, ""},
		{"files of other parts missing", dir(release(payload(web, apiAMD, apiARM), nil), map[string][]byte{"ngc-api-amd64.tar": api}),
			[]string{ReleaseAPI, "amd64"}, ""},
		{"file of another part changed", dir(release(payload(web, apiAMD), nil), map[string][]byte{"ngc-web-all.tar": releaseFile,
			"ngc-api-amd64.tar": []byte("changed")}), webArgs, ""},
		{"files not listed", dir(release(payload(web), nil), map[string][]byte{"ngc-web-all.tar": releaseFile, "evil.sh": []byte("rm -rf /"),
			"payload.json": []byte("{}"), "sub/ngc-web-all.tar": []byte("other")}), webArgs, ""},
		{"file named like a work file", dir(release(payload(webAt("payload.json")), nil), map[string][]byte{"payload.json": releaseFile}), webArgs, ""},

		{"tampered payload", dir(release(payload(web), func(m map[string]string) {
			p, _ := enc.B64.DecodeString(m["payload"])
			m["payload"] = enc.B64.EncodeToString(bytes.Replace(p, []byte(`"1.1.0"`), []byte(`"9.9.9"`), 1))
		}), webOnly), webArgs, "signature does not verify"},
		{"wrong key", withKey(dir(release(payload(web), nil), webOnly), otherKey), webArgs, "signature does not verify"},
		{"changed signature", dir(release(payload(web), func(m map[string]string) {
			s, _ := enc.B64.DecodeString(m["sig"])
			s[0] ^= 1
			m["sig"] = enc.B64.EncodeToString(s)
		}), webOnly), webArgs, "signature does not verify"},
		{"signature with an unused bit set", dir(release(payload(web), unusedBit), webOnly), webArgs, "signature not base64url"},
		{"payload with padding", dir(release(payload(web), func(m map[string]string) { m["payload"] += "=" }), webOnly), webArgs, "payload not base64url"},
		// jq -r prints such a text with a second newline, and $(...) drops
		// newlines at the end and NUL bytes: what remains is valid base64url.
		{"payload ending in a newline", dir(release(payload(web), func(m map[string]string) { m["payload"] += "\n" }), webOnly), webArgs, "payload not base64url"},
		{"signature ending in a newline", dir(release(payload(web), func(m map[string]string) { m["sig"] += "\n" }), webOnly), webArgs, "signature not base64url"},
		{"payload with a NUL byte", dir(release(payload(web), func(m map[string]string) { m["payload"] += "\x00" }), webOnly), webArgs, "payload not base64url"},
		{"no payload", dir(release(payload(web), func(m map[string]string) { delete(m, "payload") }), webOnly), webArgs, "no payload"},
		{"wrong hash", dir(release(payload(web), nil), map[string][]byte{"ngc-web-all.tar": []byte("NGC web test filE\n")}), webArgs, "checksum wrong"},
		{"bytes appended", dir(release(payload(web), nil), map[string][]byte{"ngc-web-all.tar": append(slices.Clone(releaseFile), 'x')}), webArgs, "checksum wrong"},
		{"wrong size", dir(release(payload(func() File { f := web; f.Size++; return f }()), nil), webOnly), webArgs, "size wrong"},
		{"missing file", dir(release(payload(web), nil), nil), webArgs, "file missing"},
		{"file only in the url's directory", dir(release(payload(webAt("sub/ngc-web-all.tar")), nil),
			map[string][]byte{"sub/ngc-web-all.tar": releaseFile}), webArgs, "file missing"},
		{"files null", dir(release(payload(), nil), webOnly), webArgs, "no single valid entry"},
		{"files empty", dir(release(payload([]File{}...), nil), webOnly), webArgs, "no single valid entry"},
		{"part not listed", dir(release(payload(web), nil), webOnly), []string{ReleaseController, "amd64"}, "no single valid entry"},
		{"two entries for the part", dir(release(payload(web, web), nil), webOnly), webArgs, "no single valid entry"},
		{"size as a string", dir(release([]byte(strings.Replace(string(payload(web)), `"size":18`, `"size":"18"`, 1)), nil), webOnly), webArgs, "no single valid entry"},
		{"name with a leading dash", dir(release(payload(webAt("-x.tar")), nil), map[string][]byte{"-x.tar": releaseFile}), webArgs, "file name not allowed"},
		{"name --help", dir(release(payload(webAt("https://example.org/--help")), nil), map[string][]byte{"--help": releaseFile}), webArgs, "file name not allowed"},
		{"hidden name", dir(release(payload(webAt(".x.tar")), nil), map[string][]byte{".x.tar": releaseFile}), webArgs, "file name not allowed"},
		{"url ending in a slash", dir(release(payload(webAt("https://example.org/")), nil), webOnly), webArgs, "file name not allowed"},
		{"no part and arch", dir(release(payload(web), nil), webOnly), nil, "usage"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d, tmp := t.TempDir(), t.TempDir()
			for name, b := range c.files {
				p := filepath.Join(d, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("sh", append([]string{filepath.ToSlash(script), "."}, c.args...)...)
			cmd.Dir = d
			cmd.Env = append(os.Environ(), "TMPDIR="+filepath.ToSlash(tmp))
			out, err := cmd.CombinedOutput()
			if c.why == "" && (err != nil || string(out) != "release ok\n") ||
				c.why != "" && (err == nil || strings.Contains(string(out), "release ok") || !strings.Contains(string(out), c.why)) {
				t.Errorf("%q, %v", out, err)
			}
			// Nothing is written into the release directory, and the work
			// directory is gone.
			left := 0
			err = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
				if err == nil && !e.IsDir() {
					left++
				}
				return err
			})
			if err != nil || left != len(c.files) {
				t.Errorf("%d files in the release directory, %v", left, err)
			}
			if work, err := os.ReadDir(tmp); err != nil || len(work) != 0 {
				t.Errorf("left in TMPDIR: %v, %v", work, err)
			}
		})
	}
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
