package appstore

// The whole way of a paid app, driven once from end to end with the tool that
// a store operator really uses, and then the cases that decide whether the way
// holds.
//
// Everything here goes through apps/cli/classes, the code behind the three
// subcommands of ngc-store: Pack writes the package, Encrypt seals it with a
// content key, License wraps that key for this installation and signs the
// licence with the store's key. Nothing is minted by hand, because a licence
// a test builds itself measures the test's idea of a licence and not the one
// the tool issues.
//
// What this file does NOT measure, said here so no number of it is read wider
// than it is: no package is fetched over HTTP in any case below, so the line
// of the fixture that lends the client the test server's transport
// (fixture_test.go:252, "Punkt 45") touches nothing here -- a licence is
// judged by signature and not by a TLS chain. And the controller's own half of
// the way is measured in its own package (install), not here.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	cli "github.com/sorglos-it/nextgencloud/apps/cli/classes"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// paidApp is the app these cases pack. It is not the licenceApp of the vectors
// (org.sorglos.terminal), because a manifest may not carry the ID of an NGC
// part at all (store.Manifest.Validate, wire.CoreApp).
const paidApp = "org.example.paid"

// theOtherInstallKey is the public key of an installation that is not this
// one: NodeKeys(0x92) against the fixture's NodeKeys(0x91).
var theOtherInstallKey = func() []byte { dh, _ := trusttest.NodeKeys(0x92); return dh.Public }()

// goods is what the tool produced: the plain package, the sealed one, and the
// paths of the two keys, so a case can sign one more licence with them.
type goods struct {
	dir        string
	plain      []byte // the package as Pack wrote it
	sealed     []byte // the same package as Encrypt sealed it
	sealedPath string
	storeKey   string // the store's Ed25519 seed, base64url in a file
	contentKey string // the content key of the app, base64url in a file
	manifest   *store.Manifest
	sum        []byte // SHA-256 of the plain package, as Pack reports it
}

// build runs ngc-store's own three steps over a freshly written app. The seed
// of the store key is the one the vectors sign with (0x71), so a licence from
// here verifies against the trust list the fixture set up; the content key is
// the tool's own random one, which is the point -- nothing below knows it.
func build(t *testing.T) goods {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "app")
	if err := os.MkdirAll(filepath.Join(src, "ui"), 0o700); err != nil {
		t.Fatalf("app directory: %v", err)
	}
	mf := `{"id":"` + paidApp + `","name":"Kaufapp","version":"1.0.0",` +
		`"publisher":"Sorglos","ui":{"entry":"index.html"}}`
	if err := os.WriteFile(filepath.Join(src, store.ManifestFile), []byte(mf), 0o600); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "ui", "index.html"),
		[]byte("<!doctype html><title>Kaufapp</title>\n"), 0o600); err != nil {
		t.Fatalf("ui file: %v", err)
	}
	plainPath := filepath.Join(dir, "paid.ngc")
	m, sum, err := cli.Pack(src, plainPath)
	if err != nil {
		t.Fatalf("ngc-store pack: %v", err)
	}
	g := goods{dir: dir, manifest: m, sum: sum,
		storeKey:   filepath.Join(dir, "store.key"),
		contentKey: filepath.Join(dir, "content.key"),
		sealedPath: plainPath + cli.EncSuffix,
	}
	// The seed of the vectors' store key, written the way the tool reads it.
	// Not NewStoreKey: that one draws a key nothing in the trust list knows.
	seed := bytes.Repeat([]byte{0x71}, 32)
	if err := os.WriteFile(g.storeKey, []byte(enc.B64.EncodeToString(seed)+"\n"), 0o600); err != nil {
		t.Fatalf("store key file: %v", err)
	}
	if _, err := cli.NewContentKey(g.contentKey); err != nil {
		t.Fatalf("ngc-store content key: %v", err)
	}
	if err := cli.Encrypt(plainPath, g.sealedPath, g.contentKey); err != nil {
		t.Fatalf("ngc-store encrypt: %v", err)
	}
	if g.plain, err = os.ReadFile(plainPath); err != nil {
		t.Fatalf("plain package: %v", err)
	}
	if g.sealed, err = os.ReadFile(g.sealedPath); err != nil {
		t.Fatalf("sealed package: %v", err)
	}
	// The preparation has to have worked before any number below is read: a
	// sealed file that is still the plain one would make every case here pass
	// for the wrong reason.
	if bytes.Equal(g.plain, g.sealed) {
		t.Fatal("encrypt did not change the package")
	}
	if !bytes.HasPrefix(g.sealed, []byte("NGCENC1\n")) {
		t.Fatalf("the sealed package carries no NGCENC1 header: % x", g.sealed[:8])
	}
	if bytes.Contains(g.sealed, []byte("manifest.json")) {
		t.Fatal("the sealed package still carries the name manifest.json in the clear")
	}
	return g
}

// licence signs one licence for app over this installation's key with the
// tool. opts carries the term; the installation and the key it is wrapped for
// are named by the caller, so a case can issue one for somebody else.
func (g goods) licence(t *testing.T, app, installation string, key []byte, opts cli.LicenseOpts) []byte {
	t.Helper()
	if opts.Store == "" {
		opts.Store = store.GlobalID
	}
	if opts.Issued == 0 {
		opts.Issued = testNow
	}
	keyFile := g.storeKey
	if opts.ID == "forged" {
		// Another store's key, a key the trust list does not know: the forged
		// signature of case 6. The ID picks it so the caller reads one list of
		// options and not two paths.
		keyFile = filepath.Join(g.dir, "other-store.key")
		if _, err := os.Stat(keyFile); errors.Is(err, os.ErrNotExist) {
			seed := bytes.Repeat([]byte{0x72}, 32)
			if err := os.WriteFile(keyFile, []byte(enc.B64.EncodeToString(seed)+"\n"), 0o600); err != nil {
				t.Fatalf("other store key: %v", err)
			}
		}
	}
	env, err := cli.License(app, installation, keyFile, g.contentKey,
		enc.B64.EncodeToString(key), opts)
	if err != nil {
		t.Fatalf("ngc-store license: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

// pathFixture is the licence fixture with the packed app installed as well, so
// the app of these cases has a row and a store to be judged against.
func pathFixture(t *testing.T) (licFixture, goods) {
	t.Helper()
	lf := newLicenseFixture(t)
	g := build(t)
	lf.putApp(t, paidApp, lf.V.Store, "ctl-1")
	return lf, g
}

// open is the last step of the way: the content key the API unwrapped against
// the sealed package, read back to the plain bytes. It is store.NewDecrypter
// and nothing of this package, because that is what a controller runs.
func open(t *testing.T, g goods, key []byte) []byte {
	t.Helper()
	f, err := os.Open(g.sealedPath)
	if err != nil {
		t.Fatalf("sealed package: %v", err)
	}
	defer f.Close()
	r, err := store.NewDecrypter(f, key)
	if err != nil {
		t.Fatalf("NewDecrypter: %v", err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read the plain package: %v", err)
	}
	return b
}

// TestThePaidWayRunsFromEndToEnd is the way once through, with the numbers.
//
// Tool: pack, encrypt, license. API: TakeLicense, then ContentKey. Controller's
// step, run here because it is what the key is for: the sealed package opens to
// exactly the bytes Pack wrote, and the manifest in it is the app's.
func TestThePaidWayRunsFromEndToEnd(t *testing.T) {
	lf, g := pathFixture(t)
	body := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Expires: testNow + 30*86400})

	res, err := lf.St.TakeLicense(ctx, paidApp, body)
	if err != nil {
		t.Fatalf("TakeLicense: %v", err)
	}
	if res.App != paidApp || res.State != core.LicenseValid {
		t.Fatalf("result = %+v; want app %q state %q", res, paidApp, core.LicenseValid)
	}
	if _, ok := lf.keptLicense(t, paidApp); !ok {
		t.Fatal("the licence was taken and nothing was kept")
	}

	key, err := lf.St.ContentKey(ctx, paidApp)
	if err != nil {
		t.Fatalf("ContentKey: %v", err)
	}
	if len(key) != store.KeySize {
		t.Fatalf("content key = %d bytes; want %d", len(key), store.KeySize)
	}
	plain := open(t, g, key)
	if !bytes.Equal(plain, g.plain) {
		t.Fatalf("the opened package is %d bytes, the packed one %d", len(plain), len(g.plain))
	}
	// The numbers of this run, so a change in them is seen and not guessed at.
	t.Logf("plain=%d sealed=%d licence=%d key=%d manifest=%s %s",
		len(g.plain), len(g.sealed), len(body), len(key), g.manifest.ID, g.manifest.Version)
	if len(g.sealed) <= len(g.plain) {
		t.Errorf("sealed %d is not longer than plain %d", len(g.sealed), len(g.plain))
	}
}

// TestALicenceOfAnotherInstallationIsRefused is case 1 of the six: a licence
// the store issued for somebody else's NGC, with that one's key in it. It is
// refused, and what the admin reads is the one sentence of E_LICENSE.
func TestALicenceOfAnotherInstallationIsRefused(t *testing.T) {
	lf, g := pathFixture(t)
	other := trusttest.ID(0x12)
	if other == lf.V.Installation {
		t.Fatal("the other installation is this one")
	}
	body := g.licence(t, paidApp, other, theOtherInstallKey,
		cli.LicenseOpts{Per: store.PerSite, Expires: testNow + 30*86400})

	_, err := lf.St.TakeLicense(ctx, paidApp, body)
	if !errors.Is(err, ErrLicense) {
		t.Fatalf("TakeLicense of another installation's licence: %v; want ErrLicense", err)
	}
	if _, ok := lf.keptLicense(t, paidApp); ok {
		t.Error("a licence of another installation was kept")
	}
	// Nothing opens the package either: the refusal is not only of the paper.
	if _, err := lf.St.ContentKey(ctx, paidApp); !errors.Is(err, ErrLicense) {
		t.Errorf("ContentKey after the refusal: %v; want ErrLicense", err)
	}
	mustRead(t, err, wire.CodeLicense, "Keine gültige Lizenz.")
}

// TestAnExpiredLicenceStillRunsInsideTheGracePeriod is case 2: the term is
// over, the grace period is not. The app runs -- the content key is still
// handed out -- and the state the admin sees says "grace" and not "valid".
func TestAnExpiredLicenceStillRunsInsideTheGracePeriod(t *testing.T) {
	lf, g := pathFixture(t)
	// Issued well before, expired a day ago, so the default grace of 14 days
	// is still running at testNow. The clock is the fixture's and does not move.
	body := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Issued: testNow - 40*86400, Expires: testNow - 86400})

	res, err := lf.St.TakeLicense(ctx, paidApp, body)
	if err != nil {
		t.Fatalf("TakeLicense inside the grace period: %v", err)
	}
	if res.State != core.LicenseGrace {
		t.Fatalf("state = %q; want %q", res.State, core.LicenseGrace)
	}
	key, err := lf.St.ContentKey(ctx, paidApp)
	if err != nil {
		t.Fatalf("ContentKey inside the grace period: %v; a grace licence runs", err)
	}
	if !bytes.Equal(open(t, g, key), g.plain) {
		t.Error("the package did not open inside the grace period")
	}
	// The intermediate check this case needs: the very same licence is past
	// the grace period once the clock moves on, so the green above is the
	// grace period and not a term that never ended.
	lf.S.Now = func() int64 { return testNow + 20*86400 }
	if _, err := lf.St.ContentKey(ctx, paidApp); !errors.Is(err, ErrLicense) {
		t.Errorf("the same licence 20 days later: %v; want ErrLicense", err)
	}
}

// TestPastTheGracePeriodNothingRunsAnyMore is case 3: term and grace period
// both over. The licence is still taken -- it is genuine paper -- but no
// content key is handed out, so no controller can start the app's services.
func TestPastTheGracePeriodNothingRunsAnyMore(t *testing.T) {
	lf, g := pathFixture(t)
	body := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Issued: testNow - 60*86400,
			Expires: testNow - 20*86400, Grace: 14 * 86400})

	res, err := lf.St.TakeLicense(ctx, paidApp, body)
	if err != nil {
		t.Fatalf("TakeLicense past the grace period: %v", err)
	}
	if res.State != core.LicenseExpired {
		t.Fatalf("state = %q; want %q", res.State, core.LicenseExpired)
	}
	_, err = lf.St.ContentKey(ctx, paidApp)
	if !errors.Is(err, ErrLicense) {
		t.Fatalf("ContentKey past the grace period: %v; want ErrLicense", err)
	}
	mustRead(t, err, wire.CodeLicense, "Keine gültige Lizenz.")
	// And the seat count says no as well, without an error: UsersAllowed
	// answers false for a licence past its grace period.
	if _, allowed, err := lf.St.UsersAllowed(ctx, paidApp); err != nil || allowed {
		t.Errorf("UsersAllowed past the grace period = %v, %v; want false, nil", allowed, err)
	}
}

// TestOneUserTooManyIsRefused is case 4: a licence for two users, three users
// holding the right "<app>/use". The count is answered, the permission is not.
func TestOneUserTooManyIsRefused(t *testing.T) {
	lf, g := pathFixture(t)
	body := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerUser, Users: 2, Expires: testNow + 30*86400})
	if _, err := lf.St.TakeLicense(ctx, paidApp, body); err != nil {
		t.Fatalf("TakeLicense of a per-user licence: %v", err)
	}

	// The intermediate check: two users fit, so what fails below is the third
	// one and not the per-user licence as such. It needs a fixture of its own,
	// because giveUse writes one new version of the trust list and two of them
	// would add the same role twice.
	lf2, g2 := pathFixture(t)
	if _, err := lf2.St.TakeLicense(ctx, paidApp,
		g2.licence(t, paidApp, lf2.V.Installation, installPair.Public,
			cli.LicenseOpts{Per: store.PerUser, Users: 2, Expires: testNow + 30*86400})); err != nil {
		t.Fatalf("TakeLicense in the control fixture: %v", err)
	}
	lf2.giveUse(t, paidApp, 2)
	if n, allowed, err := lf2.St.UsersAllowed(ctx, paidApp); err != nil || n != 2 || !allowed {
		t.Fatalf("two users on a licence for two = %d, %v, %v; want 2, true, nil", n, allowed, err)
	}

	lf.giveUse(t, paidApp, 3)
	n, allowed, err := lf.St.UsersAllowed(ctx, paidApp)
	if err != nil {
		t.Fatalf("UsersAllowed with three users: %v", err)
	}
	if n != 3 || allowed {
		t.Fatalf("three users on a licence for two = %d, %v; want 3, false", n, allowed)
	}
	// What the admin reads when a role is saved that would take the third
	// seat: the dispatcher turns "not allowed" into E_LICENSE (coreapp
	// seatsAllow), the same sentence as every other refusal of this file.
	if wire.CodeLicense.Message() != "Keine gültige Lizenz." {
		t.Fatalf("E_LICENSE reads %q", wire.CodeLicense.Message())
	}
	// And the content key is NOT withheld for a full seat count: the app the
	// paying admin installed keeps running, the extra user is what is refused.
	if _, err := lf.St.ContentKey(ctx, paidApp); err != nil {
		t.Errorf("ContentKey with one user too many: %v; the licence itself holds", err)
	}
}

// TestAForgedAndATamperedLicenceAreBothRefused is cases 5 and 6: another
// store's signature, and one field changed under a signature that was real.
func TestAForgedAndATamperedLicenceAreBothRefused(t *testing.T) {
	lf, g := pathFixture(t)

	// Forged: the same licence, signed with a key the trust list does not know.
	forged := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{ID: "forged", Per: store.PerSite, Expires: testNow + 30*86400})
	_, err := lf.St.TakeLicense(ctx, paidApp, forged)
	if !errors.Is(err, ErrLicense) {
		t.Fatalf("a forged licence: %v; want ErrLicense", err)
	}
	mustRead(t, err, wire.CodeLicense, "Keine gültige Lizenz.")

	// Tampered: the genuine licence, with one field of the payload rewritten
	// and the signature left as it was. The field is the term, which is what
	// somebody would change.
	good := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Expires: testNow + 30*86400})
	tampered := rewrite(t, good, func(l map[string]any) { l["expires"] = float64(testNow + 3000*86400) })
	if bytes.Equal(good, tampered) {
		t.Fatal("the tampering changed nothing")
	}
	_, err = lf.St.TakeLicense(ctx, paidApp, tampered)
	if !errors.Is(err, ErrLicense) {
		t.Fatalf("a tampered licence: %v; want ErrLicense", err)
	}
	if _, ok := lf.keptLicense(t, paidApp); ok {
		t.Error("a forged or tampered licence was kept")
	}
	// The intermediate check for both: the untouched licence of the same run
	// goes through, so what the two refusals measure is the signature and not
	// a wall in front of it.
	if _, err := lf.St.TakeLicense(ctx, paidApp, good); err != nil {
		t.Fatalf("the untouched licence of the same run: %v", err)
	}
}

// rewrite changes the payload of a signed envelope and leaves the signature
// alone: that is what a tampered licence is.
func rewrite(t *testing.T, body []byte, change func(map[string]any)) []byte {
	t.Helper()
	var env sign.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	var l map[string]any
	if err := json.Unmarshal(env.Payload, &l); err != nil {
		t.Fatalf("payload: %v", err)
	}
	change(l)
	payload, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	env.Payload = payload
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

// mustRead holds the code and the sentence a refusal reaches the admin with:
// appstore.CodeFor maps the error, wire.Text carries the words. A measurement
// of what the user reads, not of what the function returned.
func mustRead(t *testing.T, err error, want wire.Code, sentence string) {
	t.Helper()
	if got := CodeFor(err); got != want {
		t.Errorf("CodeFor(%v) = %q; want %q", err, got, want)
	}
	if got := want.Message(); got != sentence {
		t.Errorf("%s reads %q; want %q", want, got, sentence)
	}
}

// TestNoContentKeyIsStoredInTheClear is the absence the security question
// asks about, measured over the key the TOOL drew -- not the fixed one of
// license_test.go, so nothing here can be green because the needle was never
// in the room.
//
// The places it looks, named because that is what an absence is made of: the
// licence row in the database (dbapi.BlobLicense), the catalogue row of the
// store (dbapi.BlobCatalog), the two settings rows the path writes
// (SettingInstallation, SettingInstallKey), every entry of the audit log, and
// the text of the error of a refused licence. Each is searched for the key as
// raw bytes, as base64url and as hex, upper and lower (containsSecret).
func TestNoContentKeyIsStoredInTheClear(t *testing.T) {
	lf, g := pathFixture(t)
	body := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Expires: testNow + 30*86400})
	if _, err := lf.St.TakeLicense(ctx, paidApp, body); err != nil {
		t.Fatalf("TakeLicense: %v", err)
	}
	key, err := lf.St.ContentKey(ctx, paidApp)
	if err != nil {
		t.Fatalf("ContentKey: %v", err)
	}
	// The needle has to be the real one: the key the API unwrapped must be the
	// one the tool wrote into its key file, or this case searches for a value
	// that was never a secret of this run.
	raw, err := os.ReadFile(g.contentKey)
	if err != nil {
		t.Fatalf("the tool's key file: %v", err)
	}
	fromFile, err := enc.B64.DecodeString(string(bytes.TrimSpace(raw)))
	if err != nil || !bytes.Equal(fromFile, key) {
		t.Fatalf("the unwrapped key is not the one ngc-store wrote: %v", err)
	}

	rows := []struct {
		name string
		kind string
		id   string
	}{
		{"the licence row", dbapi.BlobLicense, paidApp},
		{"the catalogue row", dbapi.BlobCatalog, lf.V.Store},
		{"the installation row", dbapi.BlobSetting, dbapi.SettingInstallation},
		{"the installation key row", dbapi.BlobSetting, dbapi.SettingInstallKey},
	}
	seen := 0
	for _, r := range rows {
		b, err := lf.S.DB.BlobGet(ctx, r.kind, r.id)
		if errors.Is(err, dbapi.ErrNotFound) {
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		seen++
		// The row AND what is encoded inside it: a licence row is an envelope
		// whose payload is base64url, so a key inside it would not be found by
		// a search over the row's bytes alone. The haystack is therefore the
		// row plus every layer this path can peel off it.
		if containsSecret(haystack(b.Data), key) {
			t.Errorf("the content key stands in %s", r.name)
		}
	}
	// The licence row above must be one of the rows that were there, or the
	// search ran over an empty database.
	if seen == 0 {
		t.Fatal("no row was read; the absence is not measured")
	}
	if _, ok := lf.keptLicense(t, paidApp); !ok {
		t.Fatal("no licence row: the one row that carries the wrapped key is missing")
	}
	t.Logf("looked at %d rows of the database", seen)

	// The control of the same search over the same bytes: the WRAPPED key is
	// in the licence row, and containsSecret finds it. So a green run above is
	// the key's absence and not a search that cannot find anything.
	l, _, _, err := lf.St.License(ctx, paidApp)
	if err != nil || l.Key == nil {
		t.Fatalf("read the licence back: %v", err)
	}
	row, _ := lf.keptLicense(t, paidApp)
	if !containsSecret(haystack(row.Data), l.Key.Sealed) {
		t.Fatal("the search does not even find the sealed key that is in the row; it finds nothing")
	}

	// The log, over the real key, and an error of a refusal.
	entries, err := lf.St.Log.Query(ctx, core.LogQuery{Limit: 500})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no log entry at all: this half would measure nothing")
	}
	all, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if containsSecret(string(all), key) {
		t.Error("the content key is in the log")
	}
	forged := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{ID: "forged", Per: store.PerSite, Expires: testNow + 30*86400})
	_, err = lf.St.TakeLicense(ctx, paidApp, forged)
	if err == nil {
		t.Fatal("the forged licence was taken")
	}
	if containsSecret(err.Error(), key) || containsSecret(err.Error(), installPair.Private) {
		t.Errorf("the error of a refusal carries a secret: %q", err)
	}
}

// haystack is a stored row with its layers peeled off: the bytes as they lie,
// and the payload of the envelope in it where it is one. Without the second
// layer a search over a licence row would find nothing at all, because a
// payload is base64url and a key inside it would be hidden by the encoding
// rather than by the wrapping.
func haystack(data []byte) string {
	out := string(data)
	var env sign.Envelope
	if json.Unmarshal(data, &env) == nil && len(env.Payload) > 0 {
		out += "\n" + string(env.Payload)
	}
	return out
}

// TestOnlyTheInstallationIDRefusesThisOne is the sharpened half of case 1, and
// it exists because the first one was not enough: a licence for another
// installation normally carries that other installation's key as well, and
// then it is the KEY check of store.VerifyLicense that refuses it, not the
// installation. Measured: the mutant that drops the installation check from
// shared/store/catalog.go verifyLicense LIVED against that case.
//
// This one names another installation and wraps the content key for THIS
// installation's key, so every other check passes and only the installation
// can refuse it. The same mutant dies against it.
func TestOnlyTheInstallationIDRefusesThisOne(t *testing.T) {
	lf, g := pathFixture(t)
	other := trusttest.ID(0x12)
	if other == lf.V.Installation {
		t.Fatal("the other installation is this one")
	}
	// The key is ours, the installation is not.
	body := g.licence(t, paidApp, other, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Expires: testNow + 30*86400})
	if _, err := lf.St.TakeLicense(ctx, paidApp, body); !errors.Is(err, ErrLicense) {
		t.Fatalf("a licence for another installation, wrapped for our key: %v; want ErrLicense", err)
	}
	if _, ok := lf.keptLicense(t, paidApp); ok {
		t.Error("it was kept")
	}
	mustRead(t, ErrLicense, wire.CodeLicense, "Keine gültige Lizenz.")

	// The intermediate check: the very same licence with OUR installation in
	// it goes through. So the refusal above is the installation field and
	// nothing standing in front of it.
	ours := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Expires: testNow + 30*86400})
	if _, err := lf.St.TakeLicense(ctx, paidApp, ours); err != nil {
		t.Fatalf("the same licence for this installation: %v", err)
	}
}

// TestAFutureIssueTimeDoesNotLockOutLaterLicences is the trap of "Punkt 46"
// measured for a licence, and the wall that now stops it. Only a NEWER licence
// replaces the one held (TakeLicense, cur.Issued >= l.Issued), and Issued is a
// number the STORE writes: store.License.Validate asks only l.Issued <= 0
// (shared/store/catalog.go:321, frozen). So one licence issued far ahead --
// from a store whose clock runs fast, or taken while this machine's own clock
// was stepped forward -- used to make every correct licence afterwards a
// no-op, and the admin was told the one held was newer.
//
// Until 18.10.2026 this case asserted exactly that lock and was green. The
// wall does not belong in frozen shared/ (which sees no clock) but at the one
// place a licence enters, so it is in TakeLicense, with session.MaxSkew as the
// tolerance. The case therefore now asserts the opposite, and that it fell on
// the old code is the red-before of the mend.
func TestAFutureIssueTimeDoesNotLockOutLaterLicences(t *testing.T) {
	lf, g := pathFixture(t)
	const year = 365 * 86400

	// A licence of the store's own key, issued ten years from now: no forgery,
	// just a wrong clock somewhere.
	future := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{Per: store.PerSite, Issued: testNow + 10*year,
			Expires: testNow + 11*year})
	if _, err := lf.St.TakeLicense(ctx, paidApp, future); !errors.Is(err, ErrLicense) {
		t.Fatalf("a licence issued ten years from now: got %v, want ErrLicense", err)
	}
	// Nothing of it is stored, so nothing is in the way.
	if row, ok := lf.keptLicense(t, paidApp); ok {
		t.Fatalf("the licence from the future was stored, row at %d", row.Seq)
	}

	// The licence that was really bought gets in, and is what the row holds.
	real := g.licence(t, paidApp, lf.V.Installation, installPair.Public,
		cli.LicenseOpts{ID: "L-REAL", Per: store.PerSite, Issued: testNow,
			Expires: testNow + 30*86400})
	res, err := lf.St.TakeLicense(ctx, paidApp, real)
	if err != nil {
		t.Fatalf("the licence of today: %v", err)
	}
	if res.State != core.LicenseValid {
		t.Errorf("state = %q, want %q", res.State, core.LicenseValid)
	}
	row, ok := lf.keptLicense(t, paidApp)
	if !ok {
		t.Fatal("no licence row")
	}
	if row.Seq != uint64(testNow) {
		t.Fatalf("the kept licence is from %d, want today's %d", row.Seq, testNow)
	}

	// The control: the same real licence in an installation that never saw the
	// future one is kept just the same. So the row above shows the licence and
	// not an accident of this fixture.
	lf2, g2 := pathFixture(t)
	real2 := g2.licence(t, paidApp, lf2.V.Installation, installPair.Public,
		cli.LicenseOpts{ID: "L-REAL", Per: store.PerSite, Issued: testNow,
			Expires: testNow + 30*86400})
	if _, err := lf2.St.TakeLicense(ctx, paidApp, real2); err != nil {
		t.Fatalf("the same licence without one in the way: %v", err)
	}
	row2, ok := lf2.keptLicense(t, paidApp)
	if !ok || row2.Seq != uint64(testNow) {
		t.Fatalf("control: kept %v, seq %d; want it kept at %d", ok, row2.Seq, testNow)
	}
}
