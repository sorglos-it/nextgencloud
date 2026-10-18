package install

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// The app, the store and the versions every case below starts from.
const (
	testApp   = "org.sorglos.notes"
	testVer   = "1.0.0"
	testStore = "global"
	testNGC   = "1.0.0"
)

type entry struct {
	name string
	body []byte
	kind byte
}

// buildTar writes entries as a tar archive, the shape of an app package.
func buildTar(t testing.TB, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: e.kind}
		if e.kind == tar.TypeSymlink || e.kind == tar.TypeLink {
			h.Linkname, h.Size = "/etc/shadow", 0
		}
		if e.kind == tar.TypeDir {
			h.Mode, h.Size = 0o755, 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.kind == tar.TypeReg {
			if _, err := w.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// appManifest is the manifest of the test app: a UI, an icon and one service,
// so a package needs both ui/ and services/.
func appManifest(id, version, minNGC string) *store.Manifest {
	return &store.Manifest{ID: id, Name: "Notizen", Version: version, Publisher: "Sorglos IT",
		Icon: "icon.svg", UI: &store.UI{Entry: "index.html"},
		Services: []store.Service{{Name: "app", Command: []string{"/usr/bin/notes"}}},
		MinNGC:   minNGC}
}

func manifestEntry(t testing.TB, m *store.Manifest) entry {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return entry{store.ManifestFile, b, tar.TypeReg}
}

// goodEntries is a package Walk accepts, for the manifest m.
func goodEntries(t testing.TB, m *store.Manifest) []entry {
	return []entry{
		manifestEntry(t, m),
		{"ui", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Notizen</title>"), tar.TypeReg},
		{"ui/icon.svg", []byte("<svg/>"), tar.TypeReg},
		{"services/amd64.tar", []byte("image"), tar.TypeReg},
	}
}

func goodPackage(t testing.TB) []byte {
	return buildTar(t, goodEntries(t, appManifest(testApp, testVer, "")))
}

// uiHashOf is the app's UI hash of a plain package (store.PackageUI).
func uiHashOf(t testing.TB, pkg []byte) []byte {
	t.Helper()
	_, h, err := store.PackageUI(bytes.NewReader(pkg), io.Discard)
	if err != nil {
		t.Fatalf("PackageUI: %v", err)
	}
	return h
}

func sha(b []byte) []byte { s := sha256.Sum256(b); return s[:] }

// signer returns a deterministic Ed25519 key, so a case can sign with the
// right one and with a different one.
func signer(fill byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
	return priv.Public().(ed25519.PublicKey), priv
}

// paxRecord formats one PAX record: its length in decimal, including itself.
func paxRecord(k, v string) string {
	s := " " + k + "=" + v + "\n"
	n := len(s) + 1
	for len(strconv.Itoa(n))+len(s) != n {
		n = len(strconv.Itoa(n)) + len(s)
	}
	return strconv.Itoa(n) + s
}

// rawHeader builds a USTAR header block by hand, for entries tar.Writer does
// not write.
func rawHeader(name string, size int64, flag byte) []byte {
	h := make([]byte, 512)
	copy(h[0:100], name)
	copy(h[100:108], "0000644\x00")
	copy(h[108:116], "0000000\x00")
	copy(h[116:124], "0000000\x00")
	copy(h[124:136], fmt.Sprintf("%011o\x00", size))
	copy(h[136:148], "00000000000\x00")
	h[156] = flag
	copy(h[257:265], "ustar\x0000")
	for i := 148; i < 156; i++ {
		h[i] = ' '
	}
	sum := 0
	for _, c := range h {
		sum += int(c)
	}
	copy(h[148:156], fmt.Sprintf("%06o\x00 ", sum))
	return h
}

func pad512(b []byte) []byte { return append(b, make([]byte, (512-len(b)%512)%512)...) }

// sparseEntry claims size bytes and carries one block, its sparse map:
// archive/tar reports it as a regular file of the full logical size and fills
// the holes itself, so a few hundred bytes hand out gigabytes. It follows the
// helper of shared/store's own tests, so the entry here is the one Walk
// refuses there.
func sparseEntry(name string, size int64) []byte {
	pax := paxRecord("GNU.sparse.major", "1") + paxRecord("GNU.sparse.minor", "0") +
		paxRecord("GNU.sparse.name", name) + paxRecord("GNU.sparse.realsize", strconv.FormatInt(size, 10))
	out := append(rawHeader("PaxHeaders/0", int64(len(pax)), tar.TypeXHeader), pad512([]byte(pax))...)
	sparseMap := pad512([]byte("1\n0\n0\n")) // one segment: offset 0, no data
	out = append(out, rawHeader(name, int64(len(sparseMap)), tar.TypeReg)...)
	return append(out, sparseMap...)
}

// endMarker is the end of every archive: two zero blocks.
var endMarker = make([]byte, 1024)

// withoutEnd returns an archive of buildTar without its end marker, so raw
// entries can follow.
func withoutEnd(archive []byte) []byte { return slices.Clone(archive[:len(archive)-len(endMarker)]) }

// catalogOf signs c as a store does.
func catalogOf(t testing.TB, priv ed25519.PrivateKey, c store.Catalog) *sign.Envelope {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, sign.PurposeCatalog, b)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// releaseOf signs r with the release key.
func releaseOf(t testing.TB, priv ed25519.PrivateKey, r store.Release) *sign.Envelope {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, sign.PurposeRelease, b)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// fixture is one call to Accept: the package file, the header and the input,
// each a field a case may bend before it calls run.
type fixture struct {
	t    testing.TB
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	file []byte // what the API sends after the header
	tr   core.Transfer
	in   AcceptInput
	base string
}

// catalogFixture is a store app Accept must accept: a signed catalog of the
// store in the trust list, the package that catalog names, and an input whose
// checksum and UI hash are the ones of that package. Every case below starts
// from it and bends one thing, so what the case measures is that one thing.
func catalogFixture(t testing.TB) *fixture {
	t.Helper()
	pkg := goodPackage(t)
	pub, priv := signer(1)
	f := &fixture{t: t, pub: pub, priv: priv, file: pkg, base: t.TempDir()}
	f.tr = core.Transfer{Store: testStore, Size: int64(len(pkg)), SHA256: sha(pkg)}
	f.tr.Catalog = catalogOf(t, priv, store.Catalog{Store: testStore, Seq: 7, Issued: 1,
		Apps: []store.Entry{{ID: testApp, Name: "Notizen", Publisher: "Sorglos IT",
			Versions: []store.Package{{Version: testVer, URL: "pkg.tar", Size: int64(len(pkg)),
				SHA256: sha(pkg), UI: uiHashOf(t, pkg)}}}}})
	f.in = AcceptInput{Max: store.MaxPackage, List: &trust.List{Stores: []trust.Store{
		{ID: testStore, Name: "Global", URL: "https://store.example.org", Key: enc.Bytes(pub)}}},
		Store: testStore, App: testApp, Version: testVer, SHA256: sha(pkg), UI: uiHashOf(t, pkg),
		NGC: testNGC, Dir: f.base, LastSeq: func(string) uint64 { return 0 }}
	return f
}

// uploadFixture is an app an admin uploaded: no catalog, no release, and the
// signed task alone names the checksum and the UI hash.
func uploadFixture(t testing.TB) *fixture {
	t.Helper()
	f := catalogFixture(t)
	f.tr.Catalog, f.tr.Store = nil, ""
	f.in.Store = core.LocalStore
	return f
}

// setFile puts b in place of the package and carries its checksum and size
// into the header, the catalog and the input, so a case that bends only the
// package measures the package rule and not a stale checksum.
func (f *fixture) setFile(b []byte) *fixture {
	f.t.Helper()
	f.file = b
	f.tr.Size, f.tr.SHA256 = int64(len(b)), sha(b)
	f.in.SHA256 = sha(b)
	if f.tr.Catalog != nil {
		f.withCatalog(func(c *store.Catalog) {
			c.Apps[0].Versions[0].Size, c.Apps[0].Versions[0].SHA256 = int64(len(b)), sha(b)
		})
	}
	return f
}

// withCatalog rewrites the catalog through fn and signs it again.
func (f *fixture) withCatalog(fn func(c *store.Catalog)) *fixture {
	f.t.Helper()
	var c store.Catalog
	if err := json.Unmarshal(f.tr.Catalog.Payload, &c); err != nil {
		f.t.Fatal(err)
	}
	fn(&c)
	f.tr.Catalog = catalogOf(f.t, f.priv, c)
	return f
}

// stream is the bytes Accept reads: the header, then the file.
func (f *fixture) stream() []byte {
	f.t.Helper()
	var buf bytes.Buffer
	if err := core.WriteTransfer(&buf, f.tr); err != nil {
		f.t.Fatalf("WriteTransfer: %v", err)
	}
	return append(buf.Bytes(), f.file...)
}

func (f *fixture) run() (Package, error) {
	f.t.Helper()
	f.in.Reader = bytes.NewReader(f.stream())
	return Accept(f.t.Context(), f.in)
}

// encrypt seals b with key, as a store does for a paid app.
func encrypt(t testing.TB, b, key []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := store.Encrypt(&buf, bytes.NewReader(b), key, rand.Reader); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return buf.Bytes()
}
