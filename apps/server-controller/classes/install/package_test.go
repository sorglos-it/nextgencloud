package install

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// empty reports that nothing is left under base: every refusal must remove
// the folder it staged, so a rejected package leaves no byte behind.
func empty(t testing.TB, base string) {
	t.Helper()
	names, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", base, err)
	}
	if len(names) != 0 {
		got := make([]string, 0, len(names))
		for _, e := range names {
			got = append(got, e.Name())
		}
		t.Errorf("after a refusal %q holds %v, want nothing", base, got)
	}
}

// refuses runs f and demands want, and that nothing stayed on disk.
func refuses(t *testing.T, name string, f *fixture, want error) {
	t.Helper()
	p, err := f.run()
	if !errors.Is(err, want) {
		t.Errorf("%s: Accept = %#v, %v; want %v", name, p, err, want)
	}
	empty(t, f.base)
}

// TestAcceptChecksChecksumAndUIHash is the whole good path and the two hashes
// that bind it: the file to the checksum the signed task names, and the
// unpacked UI to the UI hash. It is also the intermediate check every refusal
// below leans on — that the way through to store.Walk is open at all.
func TestAcceptChecksChecksumAndUIHash(t *testing.T) {
	f := catalogFixture(t)
	p, err := f.run()
	if err != nil {
		t.Fatalf("Accept on a good store package: %v", err)
	}
	if p.Manifest == nil || p.Manifest.ID != testApp || p.Manifest.Version != testVer {
		t.Fatalf("manifest = %+v", p.Manifest)
	}
	if !bytes.Equal(p.UI, uiHashOf(t, goodPackage(t))) {
		t.Errorf("UI hash = %x", p.UI)
	}
	if !bytes.Equal(p.Signer, f.pub) {
		t.Errorf("Signer = %x, want the store key %x", p.Signer, f.pub)
	}
	if p.Seq != 7 {
		t.Errorf("Seq = %d, want the catalogue number 7", p.Seq)
	}
	if !bytes.Equal(p.Transfer.SHA256, sha(goodPackage(t))) {
		t.Errorf("Transfer.SHA256 = %x", p.Transfer.SHA256)
	}
	for _, name := range []string{
		filepath.Join(FilesDir, "ui", "index.html"),
		filepath.Join(FilesDir, "ui", "icon.svg"),
		filepath.Join(FilesDir, "services", "amd64.tar"),
		PackageFile,
	} {
		if _, err := os.Stat(filepath.Join(p.Dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(p.Dir, FilesDir, "ui", "index.html"))
	if err != nil || !bytes.Equal(body, []byte("<!doctype html><title>Notizen</title>")) {
		t.Errorf("ui/index.html = %q, %v", body, err)
	}

	// A package whose ui-tar hashes to something else: the catalogue entry
	// and the task both name the UI hash, and each is refused on its own.
	other := buildTar(t, slices.Concat(goodEntries(t, appManifest(testApp, testVer, "")),
		[]entry{{"ui/extra.js", []byte("alert(1)"), tar.TypeReg}}))
	g := catalogFixture(t)
	g.setFile(other) // checksum follows the file, the UI hash in the input does not
	refuses(t, "ui hash of the task", g, ErrVerify)

	g = catalogFixture(t)
	g.setFile(other)
	g.in.UI = uiHashOf(t, other)
	refuses(t, "ui hash of the catalogue entry", g, ErrVerify)

	// Both UI hashes right, so only the file checksum is wrong.
	g = catalogFixture(t)
	g.file = other
	refuses(t, "checksum of the file", g, ErrVerify)

	// The header promises a checksum the signed task did not approve.
	g = catalogFixture(t)
	g.tr.SHA256, g.tr.Size = sha(other), int64(len(other))
	g.file = other
	refuses(t, "checksum of the header against the task", g, ErrVerify)

	// One byte of the file turned over, its length unchanged: only the
	// SHA-256 of what was written catches this one.
	bent := slices.Clone(goodPackage(t))
	bent[len(bent)-600] ^= 1
	g = catalogFixture(t)
	g.file = bent
	refuses(t, "one byte of the file turned over", g, ErrVerify)

	// The checked catalogue entry has to agree with the header: the task and
	// the header could both name a file the store never published.
	g = catalogFixture(t)
	g.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].SHA256 = enc.Bytes(sha(other)) })
	refuses(t, "catalogue entry with another checksum", g, ErrVerify)

	g = catalogFixture(t)
	g.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].Size++ })
	refuses(t, "catalogue entry with another size", g, ErrVerify)

	// The store really published this file and the header names it rightly,
	// yet the signed task approved another one. Only the comparison against
	// the task catches this, so it is the only case that can catch it.
	g = catalogFixture(t)
	g.setFile(other)
	g.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].UI = enc.Bytes(uiHashOf(t, other)) })
	g.in.SHA256, g.in.UI = sha(goodPackage(t)), uiHashOf(t, other)
	refuses(t, "a file the store published but the task did not approve", g, ErrVerify)

	// The mirror of it for the UI hash: the catalogue entry agrees with the
	// file, the task does not.
	g = catalogFixture(t)
	g.setFile(other)
	g.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].UI = enc.Bytes(uiHashOf(t, other)) })
	refuses(t, "ui hash of the task against a catalogue that agrees", g, ErrVerify)

	// And for an uploaded app, where no catalogue entry exists at all.
	g = uploadFixture(t)
	g.setFile(other)
	refuses(t, "ui hash of the task for an uploaded app", g, ErrVerify)
}

// TestAcceptRefusesUnsafePaths drives the four shapes that would write
// outside the target folder or hand out more bytes than the archive carries.
// Each archive differs from the good one of TestAcceptChecksChecksumAndUIHash
// by exactly one entry, and that case proves the way to store.Walk is open.
func TestAcceptRefusesUnsafePaths(t *testing.T) {
	good := goodEntries(t, appManifest(testApp, testVer, ""))
	cases := map[string][]byte{
		"parent": buildTar(t, append(slices.Clone(good), entry{"ui/../../etc/cron.d/x", []byte("x"), tar.TypeReg})),
		"absolute": buildTar(t, append(slices.Clone(good),
			entry{"/etc/passwd", []byte("root::0:0::/:/bin/sh"), tar.TypeReg})),
		"symlink":  buildTar(t, append(slices.Clone(good), entry{"ui/link", nil, tar.TypeSymlink})),
		"hardlink": buildTar(t, append(slices.Clone(good), entry{"ui/hard", nil, tar.TypeLink})),
		"sparse": slices.Concat(withoutEnd(buildTar(t, good)),
			sparseEntry("ui/hole.bin", 4096), endMarker),
	}
	for name, pkg := range cases {
		f := catalogFixture(t)
		f.setFile(pkg)
		f.in.UI = uiHashOf(t, goodPackage(t)) // never reached: Walk refuses first
		refuses(t, name, f, store.ErrPackage)
	}
}

// TestAcceptRefusesTooManyDirectories nails both sides of store.MaxDirs:
// exactly MaxDirs directories pass, one more is refused. "ui" and the
// "services" the image implies are two of them.
func TestAcceptRefusesTooManyDirectories(t *testing.T) {
	pack := func(extra int) []byte {
		entries := slices.Clone(goodEntries(t, appManifest(testApp, testVer, "")))
		for i := 0; i < extra; i++ {
			entries = append(entries, entry{fmt.Sprintf("ui/d%d", i), nil, tar.TypeDir})
		}
		return buildTar(t, entries)
	}
	at := pack(store.MaxDirs - 2)
	f := catalogFixture(t)
	f.setFile(at)
	f.in.UI = uiHashOf(t, at)
	if _, err := f.run(); err != nil {
		t.Fatalf("exactly store.MaxDirs directories: %v, want accepted", err)
	}

	over := pack(store.MaxDirs - 1)
	g := catalogFixture(t)
	g.setFile(over)
	g.in.UI = uiHashOf(t, at) // never reached
	refuses(t, "one directory over store.MaxDirs", g, store.ErrPackage)
}

// TestAcceptDecryptsAPaidApp checks the content key of a bought app: the
// right one opens the package, a wrong one is store.ErrEncrypted, and the key
// never appears in an error.
func TestAcceptDecryptsAPaidApp(t *testing.T) {
	key := bytes.Repeat([]byte{0xA7}, store.KeySize)
	plain := goodPackage(t)
	sealed := encrypt(t, plain, key)
	price := &store.Price{Model: store.PriceBuy, Per: store.PerSite, Amount: 499,
		Currency: "EUR", Shop: "https://store.example.org/shop"}
	paid := func(t *testing.T) *fixture {
		f := catalogFixture(t)
		f.setFile(sealed) // checksum and size of the sealed file, as the store delivers it
		f.withCatalog(func(c *store.Catalog) {
			c.Apps[0].Price = price
			c.Apps[0].Versions[0].Encrypted = true
			c.Apps[0].Versions[0].UI = uiHashOf(t, plain)
		})
		f.in.UI = uiHashOf(t, plain)
		// A license goes with a key; its signature is the caller's business
		// (store.VerifyLicenseFor), so here it only has to have the right form.
		env, err := sign.Sign(f.priv, sign.PurposeLicense, bytes.Repeat([]byte{1}, 64))
		if err != nil {
			t.Fatal(err)
		}
		f.tr.License, f.tr.Key = &env, enc.Bytes(key)
		return f
	}

	f := paid(t)
	p, err := f.run()
	if err != nil {
		t.Fatalf("Accept on an encrypted package with the right key: %v", err)
	}
	if p.Manifest == nil || p.Manifest.ID != testApp {
		t.Fatalf("manifest = %+v", p.Manifest)
	}
	if !bytes.Equal(p.UI, uiHashOf(t, plain)) {
		t.Errorf("UI hash = %x", p.UI)
	}

	g := paid(t)
	wrong := bytes.Repeat([]byte{0xA8}, store.KeySize)
	g.tr.Key = enc.Bytes(wrong)
	gp, err := g.run()
	if !errors.Is(err, store.ErrEncrypted) {
		t.Errorf("wrong content key: %#v, %v; want %v", gp, err, store.ErrEncrypted)
	}
	if err != nil && bytes.Contains([]byte(err.Error()), wrong) {
		t.Error("the error text carries the content key")
	}
	empty(t, g.base)

	// The plain package where the catalogue says encrypted, and the other way
	// round: both are refused before anything is unpacked.
	h := paid(t)
	h.setFile(plain)
	h.withCatalog(func(c *store.Catalog) {
		c.Apps[0].Price, c.Apps[0].Versions[0].Encrypted = price, true
		c.Apps[0].Versions[0].UI = uiHashOf(t, plain)
	})
	refuses(t, "plain file for a paid app", h, store.ErrEncrypted)

	i := catalogFixture(t)
	i.setFile(sealed)
	refuses(t, "sealed file without a key", i, store.ErrPackage)
}

// TestAcceptRefusesTooNewMinNGC covers minNgc on both sides: in the
// catalogue entry, where it is checked before a byte is written, and in the
// manifest, which is the only place an uploaded app states it.
func TestAcceptRefusesTooNewMinNGC(t *testing.T) {
	f := catalogFixture(t)
	f.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].MinNGC = "2.0.0" })
	refuses(t, "catalogue minNgc 2.0.0 against NGC 1.0.0", f, ErrVerify)

	// 0.0.0-dev is no release version, so it suits no minNgc at all.
	g := catalogFixture(t)
	g.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].MinNGC = "1.0.0" })
	g.in.NGC = "0.0.0-dev"
	refuses(t, "a development build against minNgc 1.0.0", g, ErrVerify)

	// The same minNgc that this NGC does support must pass, or the case above
	// would prove nothing but that minNgc is read.
	h := catalogFixture(t)
	h.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].MinNGC = "1.0.0" })
	if _, err := h.run(); err != nil {
		t.Errorf("minNgc 1.0.0 against NGC 1.0.0: %v, want accepted", err)
	}

	// An uploaded app has no catalogue entry: its manifest is the only source.
	pkg := buildTar(t, goodEntries(t, appManifest(testApp, testVer, "2.0.0")))
	i := uploadFixture(t)
	i.setFile(pkg)
	i.in.UI = uiHashOf(t, pkg)
	refuses(t, "manifest minNgc 2.0.0 against NGC 1.0.0", i, ErrVerify)

	ok := buildTar(t, goodEntries(t, appManifest(testApp, testVer, "1.0.0")))
	j := uploadFixture(t)
	j.setFile(ok)
	j.in.UI = uiHashOf(t, ok)
	if _, err := j.run(); err != nil {
		t.Errorf("manifest minNgc 1.0.0 against NGC 1.0.0: %v, want accepted", err)
	}
}

// TestAcceptRefusesAForeignSourceAndARolledBackCatalog covers where the
// catalogue comes from: only a store of this installation's own trust list,
// only the store the signed task names, and never a catalogue older than the
// newest one already seen from it.
func TestAcceptRefusesAForeignSourceAndARolledBackCatalog(t *testing.T) {
	// A store that is not in the trust list at all.
	f := catalogFixture(t)
	f.in.List = &trust.List{}
	refuses(t, "store not in the trust list", f, ErrSource)

	// In the list, but with another key: the catalogue was signed by someone
	// whose key an admin never confirmed.
	g := catalogFixture(t)
	otherPub, _ := signer(9)
	g.in.List = &trust.List{Stores: []trust.Store{{ID: testStore, Name: "Global",
		URL: "https://store.example.org", Key: enc.Bytes(otherPub)}}}
	refuses(t, "store with another key", g, sign.ErrWrongKey)

	// Another store of the same list, which did sign the catalogue it sends:
	// the task named "global", so this is refused all the same.
	otherPub2, otherPriv := signer(5)
	h := catalogFixture(t)
	h.priv = otherPriv
	h.tr.Store = "other"
	h.withCatalog(func(c *store.Catalog) { c.Store = "other" })
	h.in.List = &trust.List{Stores: []trust.Store{
		{ID: testStore, Name: "Global", URL: "https://store.example.org", Key: enc.Bytes(h.pub)},
		{ID: "other", Name: "Andere", URL: "https://other.example.org", Key: enc.Bytes(otherPub2)},
	}}
	refuses(t, "a different store than the task names", h, ErrSource)

	// A catalogue that names a store other than the one whose key signed it.
	i := catalogFixture(t)
	i.withCatalog(func(c *store.Catalog) { c.Store = "other" })
	refuses(t, "catalogue of another store under this key", i, store.ErrCatalog)

	// Rolled back: the installation has already seen number 8.
	j := catalogFixture(t)
	j.in.LastSeq = func(st string) uint64 {
		if st != testStore {
			t.Errorf("LastSeq asked for %q, want %q", st, testStore)
		}
		return 8
	}
	refuses(t, "catalogue number 7 after 8", j, store.ErrRollback)

	// The same number again is not a rollback: an installation may be handed
	// the catalogue it already has.
	k := catalogFixture(t)
	k.in.LastSeq = func(string) uint64 { return 7 }
	if _, err := k.run(); err != nil {
		t.Errorf("catalogue number 7 after 7: %v, want accepted", err)
	}

	// The app and the version must stand in the catalogue.
	l := catalogFixture(t)
	l.in.App = "org.sorglos.other"
	refuses(t, "an app the catalogue does not list", l, ErrSource)

	m := catalogFixture(t)
	m.in.Version = "1.0.1"
	refuses(t, "a version the catalogue does not list", m, ErrSource)

	// An uploaded app may not claim to come from a store, and a store app may
	// not claim to be an upload.
	n := uploadFixture(t)
	n.in.Store = testStore
	refuses(t, "an upload that names a store", n, ErrSource)

	o := catalogFixture(t)
	o.in.Store = core.LocalStore
	refuses(t, "a store app that names the local store", o, ErrSource)
}

// TestAcceptRefusesABadSignature bends one byte of the signature and of the
// payload on both sources, and runs the release path against the wrong key.
func TestAcceptRefusesABadSignature(t *testing.T) {
	f := catalogFixture(t)
	f.tr.Catalog.Sig[0] ^= 1
	refuses(t, "catalogue signature", f, sign.ErrBadSignature)

	g := catalogFixture(t)
	g.tr.Catalog.Payload[len(g.tr.Catalog.Payload)-3] ^= 1
	refuses(t, "catalogue payload", g, sign.ErrBadSignature)

	relPub, relPriv := signer(3)
	release := func(t *testing.T) *fixture {
		f := catalogFixture(t)
		f.tr.Catalog, f.tr.Store = nil, ""
		f.in.Store, f.in.App, f.in.ReleaseKey = "", store.ReleaseController, relPub
		f.tr.Release = releaseOf(t, relPriv, store.Release{Seq: 4, Issued: 1, Version: testVer,
			Files: []store.File{{Part: store.ReleaseController, Arch: "amd64", URL: "ngc-controller",
				Size: int64(len(f.file)), SHA256: enc.Bytes(sha(f.file))}}})
		return f
	}

	h := release(t)
	if p, err := h.run(); err != nil || p.Seq != 4 || p.Manifest != nil {
		t.Fatalf("a signed release: %#v, %v; want the staged file at number 4", p, err)
	}

	i := release(t)
	wrongPub, _ := signer(4)
	i.in.ReleaseKey = wrongPub
	refuses(t, "release under a foreign key", i, sign.ErrWrongKey)

	j := release(t)
	j.in.LastSeq = func(string) uint64 { return 5 }
	refuses(t, "release number 4 after 5", j, store.ErrRollback)

	k := release(t)
	k.in.Version = "1.0.1"
	refuses(t, "a release version the task did not ask for", k, ErrSource)

	l := release(t)
	l.in.App = store.ReleaseAPI
	refuses(t, "a part the release does not list", l, ErrSource)

	m := release(t)
	m.in.ReleaseKey = nil
	refuses(t, "a release without a key to check it with", m, ErrSource)

	// Two sources at once cannot even be written as a header, so the rule
	// holds one step earlier than Accept; this pins that it holds at all.
	n := catalogFixture(t)
	n.tr.Release = releaseOf(t, relPriv, store.Release{Seq: 4, Issued: 1, Version: testVer,
		Files: []store.File{{Part: store.ReleaseWeb, Arch: "all", URL: "x", Size: 1,
			SHA256: enc.Bytes(sha(nil))}}})
	if err := core.WriteTransfer(io.Discard, n.tr); !errors.Is(err, core.ErrTransfer) {
		t.Errorf("a header with a catalogue and a release: %v, want %v", err, core.ErrTransfer)
	}
}

// TestAcceptRefusesTooLarge covers the bounds on the file itself: the size
// the header promises against in.Max, and the bytes that actually arrive
// against that promise, in both directions.
func TestAcceptRefusesTooLarge(t *testing.T) {
	f := catalogFixture(t)
	f.in.Max = f.tr.Size - 1
	refuses(t, "a file larger than in.Max", f, ErrVerify)

	// Exactly in.Max passes, so the case above measures the bound and not the
	// mere presence of a comparison.
	g := catalogFixture(t)
	g.in.Max = g.tr.Size
	if _, err := g.run(); err != nil {
		t.Errorf("a file of exactly in.Max bytes: %v, want accepted", err)
	}

	// More bytes than the header promised: the extra must not be read, and
	// the checksum of the promised prefix must not save it.
	h := catalogFixture(t)
	h.file = append(slices.Clone(h.file), bytes.Repeat([]byte{0}, 2048)...)
	refuses(t, "more bytes than the header promised", h, ErrVerify)

	// Fewer bytes: a package cut short in transit still parses as a shorter
	// archive, so only the size and the checksum catch it.
	i := catalogFixture(t)
	i.file = i.file[:len(i.file)-1024]
	refuses(t, "a package cut short", i, ErrVerify)
}

// TestAcceptRefusesAnotherAppInTheManifest closes the hole an uploaded app
// would otherwise leave: there no catalogue binds the file to a name, so only
// the manifest says which app this is, and it must be the one the signed task
// approved.
func TestAcceptRefusesAnotherAppInTheManifest(t *testing.T) {
	cases := map[string]*store.Manifest{
		"another app":     appManifest("org.sorglos.other", testVer, ""),
		"another version": appManifest(testApp, "9.9.9", ""),
	}
	for name, m := range cases {
		pkg := buildTar(t, goodEntries(t, m))
		f := uploadFixture(t)
		f.setFile(pkg)
		f.in.UI = uiHashOf(t, pkg)
		refuses(t, name, f, ErrVerify)
	}
}

// TestAcceptRefusesAMissingInput runs the fields a caller could leave unset.
// A zero is the one thing the compiler never holds, so each one gets a case.
func TestAcceptRefusesAMissingInput(t *testing.T) {
	cases := map[string]func(in *AcceptInput){
		"no reader":  func(in *AcceptInput) { in.Reader = nil },
		"no max":     func(in *AcceptInput) { in.Max = 0 },
		"max over":   func(in *AcceptInput) { in.Max = store.MaxPackage + 1 },
		"no list":    func(in *AcceptInput) { in.List = nil },
		"no app":     func(in *AcceptInput) { in.App = "" },
		"no version": func(in *AcceptInput) { in.Version = "" },
		"short sha":  func(in *AcceptInput) { in.SHA256 = in.SHA256[:31] },
		"short ui":   func(in *AcceptInput) { in.UI = in.UI[:31] },
		"no dir":     func(in *AcceptInput) { in.Dir = "" },
		"no lastSeq": func(in *AcceptInput) { in.LastSeq = nil },
	}
	for name, bend := range cases {
		f := catalogFixture(t)
		bend(&f.in)
		if name == "no reader" {
			if p, err := Accept(t.Context(), f.in); !errors.Is(err, ErrVerify) {
				t.Errorf("%s: %#v, %v; want %v", name, p, err, ErrVerify)
			}
			empty(t, f.base)
			continue
		}
		refuses(t, name, f, ErrVerify)
	}
}

// TestAcceptRefusesABrokenHeader pins that a header Accept cannot read never
// reaches the disk, whatever the rest of the stream holds.
func TestAcceptRefusesABrokenHeader(t *testing.T) {
	for name, stream := range map[string][]byte{
		"nothing":                      nil,
		"length not followed by bytes": {0, 0, 0, 8},
		"not json":                     {0, 0, 0, 4, 'x', 'y', 'z', 'w'},
	} {
		f := catalogFixture(t)
		f.in.Reader = bytes.NewReader(stream)
		p, err := Accept(t.Context(), f.in)
		if !errors.Is(err, core.ErrTransfer) {
			t.Errorf("%s: %#v, %v; want %v", name, p, err, core.ErrTransfer)
		}
		empty(t, f.base)
	}
}

// TestAcceptStagesInAFreshFolder pins that two accepted packages never share
// a folder, so one install cannot read or overwrite the other's files.
func TestAcceptStagesInAFreshFolder(t *testing.T) {
	base := t.TempDir()
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		f := catalogFixture(t)
		f.in.Dir, f.base = base, base
		p, err := f.run()
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if seen[p.Dir] {
			t.Fatalf("run %d staged in %q again", i, p.Dir)
		}
		seen[p.Dir] = true
		if filepath.Dir(p.Dir) != base {
			t.Errorf("staged in %q, want a folder under %q", p.Dir, base)
		}
	}
}

func FuzzAcceptHeader(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = core.ReadTransfer(bytes.NewReader(b)) })
}

func FuzzAcceptPackage(f *testing.F) {
	f.Add(goodPackage(f))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = store.Walk(bytes.NewReader(b), func(*store.Manifest, store.PackFile, io.Reader) error { return nil })
	})
}
