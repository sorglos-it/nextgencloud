package install

// The order of the two steps of a paid app on a controller: decrypting the
// package, and judging the licence that came with it.
//
// Reading the code answers it (Accept at activate.go, the licence check after
// it), but a reading is not a measurement, and the sentence matters enough to
// be nailed down: it decides whether a controller that holds a licence it must
// refuse has written the app's files to its disk first.
//
// What is measured here is Accept alone, which is the step that decrypts. The
// licence check sits in Manager.Activate behind it and is not called from
// here; that it exists and what it refuses is measured in activate_test.go.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// TestTheKeyOpensThePackageBeforeTheLicenceIsJudged is the order, measured.
//
// The envelope handed to Accept carries a payload that is no licence at all
// and a signature of a key the trust list does not know -- so
// store.VerifyLicenseFor refuses it, and the case shows that it does. Accept
// nonetheless decrypts the package and writes its files out, because Accept
// never looks at the licence: the content key is reason enough.
//
// The security statement that makes that acceptable, and it is the reason this
// case is written as a measurement and not as a finding: the content key only
// ever reaches a controller when the API has already judged the licence
// (appstore buildTransfer -> licenseFrom -> store.VerifyLicense, and
// contentKeyFrom, which refuses a licence past its grace period). The
// controller's own check is a second wall behind that one, and this case
// measures where that second wall stands: behind the decryption, not in front
// of it.
func TestTheKeyOpensThePackageBeforeTheLicenceIsJudged(t *testing.T) {
	key := bytes.Repeat([]byte{0xA7}, store.KeySize)
	plain := goodPackage(t)
	sealed := encrypt(t, plain, key)

	price := &store.Price{Model: store.PriceBuy, Per: store.PerSite, Amount: 499,
		Currency: "EUR", Shop: "https://store.example.org/shop"}
	f := catalogFixture(t)
	f.setFile(sealed)
	f.withCatalog(func(c *store.Catalog) {
		c.Apps[0].Price = price
		c.Apps[0].Versions[0].Encrypted = true
		c.Apps[0].Versions[0].UI = uiHashOf(t, plain)
	})
	f.in.UI = uiHashOf(t, plain)

	// A licence envelope no reader can accept: another key signs it, and the
	// payload is not even a licence.
	_, foreign := signer(9)
	env, err := sign.Sign(foreign, sign.PurposeLicense, bytes.Repeat([]byte{1}, 64))
	if err != nil {
		t.Fatalf("sign the foreign envelope: %v", err)
	}
	f.tr.License, f.tr.Key = &env, enc.Bytes(key)

	// The intermediate check, and it is what makes the rest a measurement: the
	// envelope really is one a controller refuses. Without it the case below
	// could be green because the licence was fine all along.
	st := trust.Store{ID: testStore, Name: "Global", URL: "https://store.example.org", Key: enc.Bytes(f.pub)}
	if _, err := store.VerifyLicenseFor(env, st, "EREREREREREREREREREREQ", testApp); err == nil {
		t.Fatal("the envelope of this case verifies as a licence; then nothing below is measured")
	}

	pkg, err := f.run()
	if err != nil {
		t.Fatalf("Accept with a refusable licence: %v", err)
	}
	// Decrypted: the manifest was read out of the sealed file, which is only
	// possible through the content key.
	if pkg.Manifest == nil || pkg.Manifest.ID != testApp {
		t.Fatalf("manifest = %+v; the package was not opened", pkg.Manifest)
	}
	// And unpacked: the files are on this disk, before anything judged the
	// paper that came with them.
	entry := filepath.Join(pkg.Dir, FilesDir, "ui", "index.html")
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("the files of the app are not on disk: %v", err)
	}
	t.Logf("sealed=%d plain=%d files written before the licence was judged", len(sealed), len(plain))

	// The counter-measurement of the same run: without the key nothing opens.
	// So what the case above showed is the key at work and not a package that
	// was never sealed.
	g := catalogFixture(t)
	g.setFile(sealed)
	g.withCatalog(func(c *store.Catalog) {
		c.Apps[0].Price = price
		c.Apps[0].Versions[0].Encrypted = true
		c.Apps[0].Versions[0].UI = uiHashOf(t, plain)
	})
	g.in.UI = uiHashOf(t, plain)
	// The licence stays and the key is a wrong one: core.Transfer refuses a
	// licence without any key at all, so the control bends the key instead of
	// removing it, which measures the same thing -- that the key is what
	// opened the package above.
	wrong := bytes.Repeat([]byte{0xA8}, store.KeySize)
	g.tr.License, g.tr.Key = &env, enc.Bytes(wrong)
	if _, err := g.run(); !errors.Is(err, store.ErrEncrypted) {
		t.Fatalf("a sealed package with the wrong key: %v; want ErrEncrypted", err)
	}
}

// TestNoContentKeyReachesTheStagedFiles holds the other half of the question:
// the key is not written anywhere a reader of this machine would find it. It
// is an absence, so it names the places it looked: the staged directory with
// everything under it, and the package file itself.
func TestNoContentKeyReachesTheStagedFiles(t *testing.T) {
	key := bytes.Repeat([]byte{0xA7}, store.KeySize)
	plain := goodPackage(t)
	sealed := encrypt(t, plain, key)

	price := &store.Price{Model: store.PriceBuy, Per: store.PerSite, Amount: 499,
		Currency: "EUR", Shop: "https://store.example.org/shop"}
	f := catalogFixture(t)
	f.setFile(sealed)
	f.withCatalog(func(c *store.Catalog) {
		c.Apps[0].Price = price
		c.Apps[0].Versions[0].Encrypted = true
		c.Apps[0].Versions[0].UI = uiHashOf(t, plain)
	})
	f.in.UI = uiHashOf(t, plain)
	env, err := sign.Sign(f.priv, sign.PurposeLicense, bytes.Repeat([]byte{1}, 64))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	f.tr.License, f.tr.Key = &env, enc.Bytes(key)

	pkg, err := f.run()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	// The plain package must be there, or this case looked at an empty tree.
	if pkg.Manifest == nil {
		t.Fatal("the package was not opened; nothing below is measured")
	}
	looked := 0
	if err := filepath.WalkDir(pkg.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		looked++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, key) {
			// The name of the file, never its bytes: a failing case lands in a
			// report and a chat.
			t.Errorf("the content key stands in %s", filepath.Base(path))
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the staged directory: %v", err)
	}
	if looked == 0 {
		t.Fatal("no file was read; the absence is not measured")
	}
	t.Logf("looked at %d staged files under %s", looked, FilesDir)

	// The control of the same shape: a value that IS in those files is found
	// by the very same search, so a green run above is not a search that
	// cannot find anything. The needle is read off the disk, so it cannot be
	// a guess about what the package carries.
	ui, err := os.ReadFile(filepath.Join(pkg.Dir, FilesDir, "ui", "index.html"))
	if err != nil || len(ui) < 8 {
		t.Fatalf("the unpacked ui file: %v, %d bytes", err, len(ui))
	}
	needle := ui[:8]
	found := false
	_ = filepath.WalkDir(pkg.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err == nil && bytes.Contains(b, needle) {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("the search did not even find the bytes of a staged file; it finds nothing")
	}
}

// used so the import of core is not idle when a case above is edited away.
var _ = core.LocalStore
