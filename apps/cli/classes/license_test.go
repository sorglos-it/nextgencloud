package classes

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// testID is an installation ID in the form trust.ValidID wants: 16 bytes as
// base64url.
func testID(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return enc.B64.EncodeToString(b)
}

// licenseFixture is one test store, one installation and one paid app.
type licenseFixture struct {
	dir            string
	keyFile        string // the store's Ed25519 seed
	contentKeyFile string
	contentKey     []byte
	st             trust.Store // the store as an installation trusts it
	installPriv    *ecdh.PrivateKey
	installKey     string // the installation's X25519 public key, base64url
	installation   string
	app            string
	opts           LicenseOpts
}

func newLicenseFixture(t testing.TB) licenseFixture {
	t.Helper()
	dir := t.TempDir()
	f := licenseFixture{
		dir:            dir,
		keyFile:        filepath.Join(dir, "store.key"),
		contentKeyFile: filepath.Join(dir, "content.key"),
		app:            "org.sorglos.terminal",
		installation:   testID(t),
	}
	priv, err := NewStoreKey(f.keyFile)
	if err != nil {
		t.Fatalf("store key: %v", err)
	}
	f.st = trust.Store{ID: "test", Name: "Test-Store", URL: "https://example.org/store",
		Key: enc.Bytes(priv.Public().(ed25519.PublicKey))}
	if f.contentKey, err = NewContentKey(f.contentKeyFile); err != nil {
		t.Fatalf("content key: %v", err)
	}
	if f.installPriv, err = ecdh.X25519().GenerateKey(rand.Reader); err != nil {
		t.Fatalf("installation key: %v", err)
	}
	f.installKey = enc.B64.EncodeToString(f.installPriv.PublicKey().Bytes())
	f.opts = LicenseOpts{Store: f.st.ID, Per: store.PerSite, Issued: 1_790_000_000}
	return f
}

// sign issues a licence with the fixture's parts.
func (f licenseFixture) sign(t testing.TB) sign.Envelope {
	t.Helper()
	env, err := License(f.app, f.installation, f.keyFile, f.contentKeyFile, f.installKey, f.opts)
	if err != nil {
		t.Fatalf("license: %v", err)
	}
	return env
}

// verifyOK is the intermediate check every refusal below needs: it proves the
// way to the line under test was open for this very envelope.
func (f licenseFixture) verifyOK(t testing.TB, env sign.Envelope) *store.License {
	t.Helper()
	l, err := store.VerifyLicense(env, f.st, f.installation, f.app, f.installPriv.PublicKey().Bytes())
	if err != nil {
		t.Fatalf("the way is not open: %v", err)
	}
	return l
}

func TestLicenseVerifies(t *testing.T) {
	f := newLicenseFixture(t)
	env := f.sign(t)
	l := f.verifyOK(t, env)
	if l.Store != f.st.ID || l.App != f.app || l.Installation != f.installation {
		t.Fatalf("licence %+v", l)
	}
	if l.Per != store.PerSite || l.Users != 0 || l.Issued != f.opts.Issued || l.Expires != 0 {
		t.Fatalf("licence %+v", l)
	}
	if len(l.ID) == 0 {
		t.Fatal("the licence has no ID")
	}
	if l.State(f.opts.Issued+86400) != store.LicenseValid {
		t.Fatal("a bought licence does not run")
	}
	// The envelope names the key that signed it, and the signature is one
	// for a licence and for nothing else.
	if env.KeyID != sign.KeyID(f.st.Key) {
		t.Fatalf("key ID %q", env.KeyID)
	}
	if err := sign.Verify(env, ed25519.PublicKey(f.st.Key), sign.PurposeCatalog); err == nil {
		t.Fatal("the signature of a licence verifies as a catalog")
	}
}

func TestLicenseCarriesTheContentKeyForThisInstallationOnly(t *testing.T) {
	f := newLicenseFixture(t)
	l := f.verifyOK(t, f.sign(t))
	key, err := store.UnwrapKey(l.Key, f.installPriv, f.app)
	if err != nil || !bytes.Equal(key, f.contentKey) {
		t.Fatalf("unwrap: %v", err)
	}
	// Another installation's private key does not open it, and neither does
	// the right key under another app's name: the app is the AEAD's
	// additional data, so a licence for a cheap app cannot unlock an
	// expensive one.
	other, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnwrapKey(l.Key, other, f.app); !errors.Is(err, store.ErrEncrypted) {
		t.Fatalf("another installation opened the key: %v", err)
	}
	if _, err := store.UnwrapKey(l.Key, f.installPriv, "org.sorglos.calculator"); !errors.Is(err, store.ErrEncrypted) {
		t.Fatalf("the key opened under another app: %v", err)
	}
	// And the content key is nowhere in the signed bytes in the clear.
	for _, s := range keyShapes(f.contentKey) {
		if bytes.Contains(env2bytes(f.sign(t)), []byte(s)) {
			t.Fatal("the content key stands in the licence in the clear")
		}
	}
}

// env2bytes is everything an envelope carries, as bytes to search in.
func env2bytes(env sign.Envelope) []byte {
	b := append([]byte{}, env.Payload...)
	b = append(b, env.KeyID...)
	return append(b, env.Sig...)
}

func TestALicenceForAnotherInstallationIsRefused(t *testing.T) {
	f := newLicenseFixture(t)
	env := f.sign(t)
	f.verifyOK(t, env) // the way is open
	if _, err := store.VerifyLicense(env, f.st, testID(t), f.app,
		f.installPriv.PublicKey().Bytes()); !errors.Is(err, store.ErrLicense) {
		t.Fatalf("another installation: %v", err)
	}
	// The same refusal without the installation's key, the way a controller
	// checks.
	if _, err := store.VerifyLicenseFor(env, f.st, testID(t), f.app); !errors.Is(err, store.ErrLicense) {
		t.Fatalf("another installation, controller: %v", err)
	}
}

func TestALicenceForAnotherAppIsRefused(t *testing.T) {
	f := newLicenseFixture(t)
	env := f.sign(t)
	f.verifyOK(t, env)
	if _, err := store.VerifyLicense(env, f.st, f.installation, "org.sorglos.calculator",
		f.installPriv.PublicKey().Bytes()); !errors.Is(err, store.ErrLicense) {
		t.Fatalf("another app: %v", err)
	}
}

func TestALicenceForAnotherInstallationKeyIsRefused(t *testing.T) {
	f := newLicenseFixture(t)
	env := f.sign(t)
	f.verifyOK(t, env)
	other, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyLicense(env, f.st, f.installation, f.app,
		other.PublicKey().Bytes()); !errors.Is(err, store.ErrLicense) {
		t.Fatalf("another installation key: %v", err)
	}
}

func TestALicenceOfAnotherStoreIsRefused(t *testing.T) {
	f := newLicenseFixture(t)
	env := f.sign(t)
	f.verifyOK(t, env)
	// Another key with the same store ID: the signature does not belong to
	// the key the installation trusts.
	foreign, err := NewStoreKey(filepath.Join(f.dir, "foreign.key"))
	if err != nil {
		t.Fatal(err)
	}
	st := f.st
	st.Key = enc.Bytes(foreign.Public().(ed25519.PublicKey))
	if _, err := store.VerifyLicense(env, st, f.installation, f.app,
		f.installPriv.PublicKey().Bytes()); err == nil {
		t.Fatal("a foreign key verified the licence")
	}
	// A licence signed by the foreign key, with the right store ID in it,
	// does not pass as the trusted store's licence either.
	fEnv, err := License(f.app, f.installation, filepath.Join(f.dir, "foreign.key"),
		f.contentKeyFile, f.installKey, f.opts)
	if err != nil {
		t.Fatalf("foreign licence: %v", err)
	}
	if _, err := store.VerifyLicense(fEnv, f.st, f.installation, f.app,
		f.installPriv.PublicKey().Bytes()); err == nil {
		t.Fatal("a licence of a foreign key was accepted")
	}
	// Intermediate check: that same foreign licence is sound in itself, so
	// the refusal is about the key and not about a malformed licence.
	st2 := f.st
	st2.Key = enc.Bytes(foreign.Public().(ed25519.PublicKey))
	if _, err := store.VerifyLicense(fEnv, st2, f.installation, f.app,
		f.installPriv.PublicKey().Bytes()); err != nil {
		t.Fatalf("the foreign licence is not sound: %v", err)
	}
	// And a licence that names another store is refused although the
	// signature is the trusted store's own.
	opts := f.opts
	opts.Store = "anderer"
	wrongStore, err := License(f.app, f.installation, f.keyFile, f.contentKeyFile, f.installKey, opts)
	if err != nil {
		t.Fatalf("licence of another store: %v", err)
	}
	if _, err := store.VerifyLicense(wrongStore, f.st, f.installation, f.app,
		f.installPriv.PublicKey().Bytes()); !errors.Is(err, store.ErrLicense) {
		t.Fatalf("another store ID: %v", err)
	}
}

func TestATamperedLicenceIsRefused(t *testing.T) {
	f := newLicenseFixture(t)
	for _, c := range []struct{ name, where string }{{"payload", "payload"}, {"signature", "sig"}} {
		env := f.sign(t)
		f.verifyOK(t, env) // the way is open for exactly this envelope
		switch c.where {
		case "payload":
			env.Payload[len(env.Payload)/2] ^= 1
		case "sig":
			env.Sig[0] ^= 1
		}
		if _, err := store.VerifyLicense(env, f.st, f.installation, f.app,
			f.installPriv.PublicKey().Bytes()); !errors.Is(err, sign.ErrBadSignature) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// A wrapped key swapped for another installation's: the signature
	// catches it, because the key is inside the signed payload.
	env := f.sign(t)
	f.verifyOK(t, env)
	other, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	foreignKey, err := store.WrapKey(f.contentKey, other.PublicKey().Bytes(), f.app, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	l := parseLicencePayload(t, env)
	if bytes.Equal(l.Key.Sealed, foreignKey.Sealed) {
		t.Fatal("the two wrappings are the same: nothing was measured")
	}
	l.Key = foreignKey
	payload, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(payload, env.Payload) {
		t.Fatal("the swap did not happen: nothing was measured")
	}
	env.Payload = payload
	if _, err := store.VerifyLicense(env, f.st, f.installation, f.app,
		f.installPriv.PublicKey().Bytes()); !errors.Is(err, sign.ErrBadSignature) {
		t.Fatalf("swapped key: %v", err)
	}
}

// parseLicencePayload reads the licence out of a signed envelope. The test
// does this itself: shared/store keeps its parser private, and these bytes
// come from License in the same run, not from outside.
func parseLicencePayload(t testing.TB, env sign.Envelope) *store.License {
	t.Helper()
	var l store.License
	if err := json.Unmarshal(env.Payload, &l); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &l
}

func TestLicenseRefusesWhatTheContractRefuses(t *testing.T) {
	f := newLicenseFixture(t)
	// Intermediate check first: the unchanged options give a licence, so
	// every refusal below is about the one thing that was changed.
	f.sign(t)
	cases := []struct {
		name string
		opts func(*LicenseOpts)
		app  string
		want error
	}{
		{"no store", func(o *LicenseOpts) { o.Store = "" }, f.app, store.ErrLicense},
		{"no issue time", func(o *LicenseOpts) { o.Issued = 0 }, f.app, store.ErrLicense},
		{"unknown per", func(o *LicenseOpts) { o.Per = "everyone" }, f.app, store.ErrLicense},
		{"per site with users", func(o *LicenseOpts) { o.Users = 5 }, f.app, store.ErrLicense},
		{"per user without users", func(o *LicenseOpts) { o.Per = store.PerUser }, f.app, store.ErrLicense},
		{"too many users", func(o *LicenseOpts) { o.Per, o.Users = store.PerUser, 100001 }, f.app, store.ErrLicense},
		{"expires before it was issued", func(o *LicenseOpts) { o.Expires = o.Issued - 1 }, f.app, store.ErrLicense},
		{"grace too long", func(o *LicenseOpts) { o.Grace = store.MaxGrace + 1 }, f.app, store.ErrLicense},
		{"negative grace", func(o *LicenseOpts) { o.Grace = -1 }, f.app, store.ErrLicense},
		{"an ID no contract allows", func(o *LicenseOpts) { o.ID = "nicht erlaubt!" }, f.app, store.ErrLicense},
		{"no app", func(o *LicenseOpts) {}, "", store.ErrLicense},
		{"an app ID no contract allows", func(o *LicenseOpts) {}, "org..sorglos", store.ErrLicense},
	}
	for _, c := range cases {
		opts := f.opts
		c.opts(&opts)
		if _, err := License(c.app, f.installation, f.keyFile, f.contentKeyFile,
			f.installKey, opts); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// A bad installation ID is refused as well.
	if _, err := License(f.app, "nicht erlaubt!", f.keyFile, f.contentKeyFile,
		f.installKey, f.opts); !errors.Is(err, store.ErrLicense) {
		t.Errorf("installation: %v", err)
	}
}

func TestLicenseRefusesABadInstallationKey(t *testing.T) {
	f := newLicenseFixture(t)
	f.sign(t) // the way is open
	for _, bad := range []string{"", "nicht base64url!!", enc.B64.EncodeToString(make([]byte, 31)),
		enc.B64.EncodeToString(make([]byte, 33))} {
		if _, err := License(f.app, f.installation, f.keyFile, f.contentKeyFile,
			bad, f.opts); !errors.Is(err, ErrInstallKey) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// All zeroes is 32 bytes and base64url, so it passes ErrInstallKey — but
	// X25519 refuses it, and store.WrapKey says so.
	if _, err := License(f.app, f.installation, f.keyFile, f.contentKeyFile,
		enc.B64.EncodeToString(make([]byte, 32)), f.opts); err == nil {
		t.Error("the all-zero key was wrapped for")
	}
}

func TestLicenseRefusesAFileThatIsNoSigningKey(t *testing.T) {
	f := newLicenseFixture(t)
	f.sign(t)
	cases := []struct {
		name string
		body []byte
		want error
	}{
		{"missing", nil, ErrKeyMissing},
		{"empty", []byte(""), ErrSignKey},
		{"not base64url", []byte("++++"), ErrSignKey},
		{"too short", []byte(enc.B64.EncodeToString(make([]byte, 31))), ErrSignKey},
	}
	for _, c := range cases {
		path := filepath.Join(f.dir, "bad-"+c.name+".key")
		if c.body != nil {
			write(t, path, c.body)
		}
		if _, err := License(f.app, f.installation, path, f.contentKeyFile,
			f.installKey, f.opts); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// The content key file is read the same way and refused the same way.
	if _, err := License(f.app, f.installation, f.keyFile,
		filepath.Join(f.dir, "gone.key"), f.installKey, f.opts); !errors.Is(err, ErrKeyMissing) {
		t.Errorf("content key: %v", err)
	}
}

func TestLicensePerUserAndSubscription(t *testing.T) {
	f := newLicenseFixture(t)
	opts := f.opts
	opts.Per, opts.Users = store.PerUser, 25
	opts.Expires = opts.Issued + 30*86400
	opts.Grace = 7 * 86400
	env, err := License(f.app, f.installation, f.keyFile, f.contentKeyFile, f.installKey, opts)
	if err != nil {
		t.Fatalf("license: %v", err)
	}
	l := f.verifyOK(t, env)
	if l.Per != store.PerUser || l.Users != 25 || l.Expires != opts.Expires || l.Grace != opts.Grace {
		t.Fatalf("licence %+v", l)
	}
	// Both sides of the two borders, so neither is only checked against
	// itself: valid one second before it expires, in the grace period one
	// second after, and over one second after the grace ends.
	if l.State(opts.Expires-1) != store.LicenseValid {
		t.Error("expired too early")
	}
	if l.State(opts.Expires) != store.LicenseGrace {
		t.Error("no grace at the moment it expires")
	}
	if l.State(opts.Expires+opts.Grace-1) != store.LicenseGrace {
		t.Error("the grace ends too early")
	}
	if l.State(opts.Expires+opts.Grace) != store.LicenseExpired {
		t.Error("the grace does not end")
	}
}

func TestLicenseDrawsAFreshIDAndAFreshWrapping(t *testing.T) {
	f := newLicenseFixture(t)
	a, b := f.verifyOK(t, f.sign(t)), f.verifyOK(t, f.sign(t))
	if a.ID == b.ID {
		t.Fatal("two licences got the same ID")
	}
	if bytes.Equal(a.Key.Ephemeral, b.Key.Ephemeral) || bytes.Equal(a.Key.Nonce, b.Key.Nonce) {
		t.Fatal("the one-time key of the wrapping repeated")
	}
	// A named ID is taken as it is, so a test store can be reproduced.
	opts := f.opts
	opts.ID = "test-0001"
	c := f.verifyOK(t, func() sign.Envelope {
		env, err := License(f.app, f.installation, f.keyFile, f.contentKeyFile, f.installKey, opts)
		if err != nil {
			t.Fatalf("license: %v", err)
		}
		return env
	}())
	if c.ID != "test-0001" {
		t.Fatalf("ID %q", c.ID)
	}
}
