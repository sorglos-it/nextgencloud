package classes

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// TestTestStore plays through the success criterion of apps/cli/CLAUDE.md:
// "ngc-store erzeugt einen Test-Store, aus dem sich eine Kauf-App
// freischalten lässt." Two apps are packed, one of them encrypted, the
// catalog is signed, a licence is issued, and everything is then read the
// way the API reads it — through shared/store, never through a reader
// written beside it.
//
// The encrypted form is written to path+EncSuffix ("app.ngc.enc"), not to
// "app.enc": that is the name Encrypt writes and the name Catalog looks the
// entry up under, and with the other one the catalog would never find it.
func TestTestStore(t *testing.T) {
	dir := t.TempDir()
	// 1. Two apps: one free, one to be bought.
	free := packApp(t, dir, "org.sorglos.calculator", "1.0.0")
	paid := packApp(t, dir, "org.sorglos.terminal", "1.0.0")

	// 2. The paid app is encrypted, and it gets a price: a catalog entry of
	// an encrypted package without a price is no valid catalog.
	contentKey := encryptPaid(t, dir, paid)
	write(t, strings.TrimSuffix(paid, ".ngc")+PriceSuffix, []byte(paidPrice))

	// 3. The catalog is signed.
	keyFile := filepath.Join(t.TempDir(), "store.key")
	storeKey, err := NewStoreKey(keyFile)
	if err != nil {
		t.Fatalf("store key: %v", err)
	}
	st := trust.Store{ID: "test", Name: "Test-Store", URL: "https://store.ngc.test",
		Key: enc.Bytes(storeKey.Public().(ed25519.PublicKey))}
	env, err := Catalog(dir, keyFile, CatalogOpts{Store: st.ID, Seq: 1, Issued: 1_790_000_000})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	// 4. A test licence for one installation.
	installPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("installation key: %v", err)
	}
	installation := testID(t)
	lic, err := License("org.sorglos.terminal", installation, keyFile,
		filepath.Join(dir, "content.key"), enc.B64.EncodeToString(installPriv.PublicKey().Bytes()),
		LicenseOpts{Per: store.PerSite, Issued: 1_790_000_000, Store: st.ID, ID: "test-1"})
	if err != nil {
		t.Fatalf("license: %v", err)
	}

	// 5. Read like the API does: the catalog, the licence and the content key.
	c, err := store.VerifyCatalog(env, st, 0)
	if err != nil {
		t.Fatalf("catalog does not verify: %v", err)
	}
	if len(c.Apps) != 2 {
		t.Fatalf("%d apps in the catalog, want 2", len(c.Apps))
	}
	paidEntry := c.Apps[1]
	if paidEntry.ID != "org.sorglos.terminal" || paidEntry.Price == nil || !paidEntry.Versions[0].Encrypted {
		t.Fatalf("the paid app in the catalog: %+v", paidEntry)
	}
	if name := paidEntry.Versions[0].URL; name != filepath.Base(paid)+EncSuffix {
		t.Errorf("the catalog names %q, want the encrypted form %q", name, filepath.Base(paid)+EncSuffix)
	}
	if freeEntry := c.Apps[0]; freeEntry.ID != "org.sorglos.calculator" || freeEntry.Price != nil ||
		freeEntry.Versions[0].Encrypted || freeEntry.Versions[0].URL != filepath.Base(free) {
		t.Errorf("the free app in the catalog: %+v", freeEntry)
	}
	l, err := store.VerifyLicense(lic, st, installation, "org.sorglos.terminal", installPriv.PublicKey().Bytes())
	if err != nil {
		t.Fatalf("license does not verify: %v", err)
	}
	key, err := store.UnwrapKey(l.Key, installPriv, "org.sorglos.terminal")
	if err != nil || !bytes.Equal(key, contentKey) {
		t.Fatalf("content key: %v", err)
	}

	// 6. And a foreign installation does not get at it.
	other, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnwrapKey(l.Key, other, "org.sorglos.terminal"); err == nil {
		t.Fatal("a foreign installation opened the app")
	}

	// 7. The store the API just read is one CheckStore takes — the signed
	// catalog and the files on the disk say the same thing.
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, CatalogFile), b)
	pem, err := PublicKeyPEM(storeKey.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, StoreKeyFile), pem)
	if err := CheckStore(dir, st.ID, 1); err != nil {
		t.Fatalf("the store that was just used does not pass the check: %v", err)
	}
}

// TestTestStoreThroughTheSubcommands is the same store once more, built only
// through Run: nothing is called directly, so the five subcommands have to
// agree about file names, key files and the order they are run in. This is
// the path whoever runs a test store actually walks.
func TestTestStoreThroughTheSubcommands(t *testing.T) {
	dir := t.TempDir()
	src := t.TempDir()
	write(t, filepath.Join(src, "manifest.json"), []byte(`{"id":"org.sorglos.terminal","name":"Terminal",`+
		`"version":"1.0.0","publisher":"Sorglos IT","description":"Eine Test-App","ui":{"entry":"index.html"}}`))
	write(t, filepath.Join(src, "ui", "index.html"), []byte("<!doctype html><title>Terminal</title>"))
	pkg := filepath.Join(dir, "org.sorglos.terminal-1.0.0.ngc")
	contentKeyFile := filepath.Join(dir, "content.key")
	storeKeyFile := filepath.Join(t.TempDir(), "store.key")
	write(t, strings.TrimSuffix(pkg, ".ngc")+PriceSuffix, []byte(paidPrice))

	installPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	installation := testID(t)
	var out, errOut bytes.Buffer
	run := func(args ...string) string {
		t.Helper()
		out.Reset()
		errOut.Reset()
		if code := Run(args, &out, &errOut); code != 0 {
			t.Fatalf("ngc-store %v: %d, %s", args, code, errOut.String())
		}
		return out.String()
	}
	run("pack", src, pkg)
	run("encrypt", pkg, contentKeyFile)
	// The signing key has to exist before "catalog" is called. Measured
	// here, and it is an inconsistency of the tool and not of this case:
	// "encrypt", "license" and "release" all create a missing key file and
	// say so, "catalog" alone fails with the error of os.Open. It belongs to
	// catalog.go (Task 28), so it is reported and not changed here.
	if _, err := NewStoreKey(storeKeyFile); err != nil {
		t.Fatalf("store key: %v", err)
	}
	run("catalog", dir, storeKeyFile, "test", "1")
	licOut := run("license", "-per", "site", "-issued", "1790000000", "-id", "test-1",
		"org.sorglos.terminal", installation, storeKeyFile, contentKeyFile,
		enc.B64.EncodeToString(installPriv.PublicKey().Bytes()), "test")

	// The licence is the last line of what the subcommand printed: the first
	// lines are the new key file and its public half.
	lines := strings.Split(strings.TrimSpace(licOut), "\n")
	var lic sign.Envelope
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lic); err != nil {
		t.Fatalf("the licence is no envelope: %v", err)
	}
	priv, err := readStoreKey(storeKeyFile)
	if err != nil {
		t.Fatalf("store key: %v", err)
	}
	st := trust.Store{ID: "test", Name: "Test-Store", URL: "https://store.ngc.test",
		Key: enc.Bytes(priv.Public().(ed25519.PublicKey))}
	l, err := store.VerifyLicense(lic, st, installation, "org.sorglos.terminal", installPriv.PublicKey().Bytes())
	if err != nil {
		t.Fatalf("license does not verify: %v", err)
	}
	key, err := store.UnwrapKey(l.Key, installPriv, "org.sorglos.terminal")
	if err != nil {
		t.Fatalf("content key: %v", err)
	}
	// The key the licence carries is the one the package was encrypted with:
	// measured by decrypting, not by comparing it with itself.
	if want, err := readContentKey(contentKeyFile); err != nil || !bytes.Equal(key, want) {
		t.Fatalf("the licence carries another content key: %v", err)
	}

	// And the store checks out through the subcommand, once the public half
	// of the key lies beside the catalog.
	pem, err := PublicKeyPEM(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, StoreKeyFile), pem)
	if got := run("check", "-store", "test", dir); got != "store ok\n" {
		t.Errorf("check -store: %q", got)
	}
}
