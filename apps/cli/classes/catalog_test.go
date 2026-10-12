package classes

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// testSeed is the Ed25519 seed the key file of these cases carries. It is a
// test key and nothing else signs with it; the case that checks that no
// secret escapes looks for exactly these bytes.
var testSeed = bytes.Repeat([]byte{0x5a}, ed25519.SeedSize)

// testPrice is the price file of a paid app.
const testPrice = `{"model":"buy","per":"site","amount":1500,"currency":"EUR",` +
	`"shop":"https://shop.example.com/term"}`

// signingKey writes a key file and returns its path and the public key.
func signingKey(t *testing.T, seed []byte) (string, ed25519.PublicKey) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store.key")
	write(t, path, []byte(enc.B64.EncodeToString(seed)+"\n"))
	return path, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
}

// packInto lays down an app with id and version and packs it into dir. extra
// goes into the manifest as further members. It returns the package's path.
func packInto(t *testing.T, dir, id, version, extra string) string {
	t.Helper()
	src := t.TempDir()
	manifest := fmt.Sprintf(`{"id":%q,"name":"App %s","version":%q,"publisher":"Sorglos IT",`+
		`"description":"Eine Test-App","ui":{"entry":"index.html"}%s}`, id, id, version, extra)
	write(t, filepath.Join(src, "manifest.json"), []byte(manifest))
	write(t, filepath.Join(src, "ui", "index.html"), []byte("<!doctype html><title>"+id+" "+version+"</title>"))
	out := filepath.Join(dir, id+"-"+version+".ngc")
	if _, _, err := Pack(src, out); err != nil {
		t.Fatalf("pack %s %s: %v", id, version, err)
	}
	return out
}

// opts are the options of a catalog of the store "teststore".
func opts(seq uint64, base string) CatalogOpts {
	return CatalogOpts{Store: "teststore", Seq: seq, Issued: 1760000000, BaseURL: base}
}

// readBack checks the envelope the way an installation does — the signature
// against pub, the store's ID and the contract's parser — and returns the
// catalog. Every case reads its result through store.VerifyCatalog, never
// through a reader of its own: what this tool writes has to pass the check
// the API runs, not one written beside it.
func readBack(t *testing.T, env sign.Envelope, pub ed25519.PublicKey, lastSeq uint64) *store.Catalog {
	t.Helper()
	c, err := store.VerifyCatalog(env, trust.Store{ID: "teststore", Key: enc.Bytes(pub)}, lastSeq)
	if err != nil {
		t.Fatalf("store.VerifyCatalog: %v", err)
	}
	return c
}

func fileSum(t *testing.T, path string) []byte {
	t.Helper()
	h := sha256.Sum256(read(t, path))
	return h[:]
}

// uiHash is the ui hash of a package as the contract computes it.
func uiHash(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, h, err := store.PackageUI(f, io.Discard)
	if err != nil {
		t.Fatalf("package ui of %s: %v", path, err)
	}
	return h
}

func TestCatalogListsEveryPackage(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	calc1 := packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	calc2 := packInto(t, dir, "org.sorglos.calculator", "1.2.0", "")
	term := packInto(t, dir, "org.sorglos.terminal", "0.9.0", "")
	env, err := Catalog(dir, key, opts(1, ""))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	c := readBack(t, env, pub, 0)
	if c.Store != "teststore" || c.Seq != 1 || c.Issued != 1760000000 || len(c.Apps) != 2 {
		t.Fatalf("catalog: %+v", c)
	}
	// Apps sorted by ID, versions ascending: the contract's own order.
	if c.Apps[0].ID != "org.sorglos.calculator" || c.Apps[1].ID != "org.sorglos.terminal" {
		t.Fatalf("apps: %s %s", c.Apps[0].ID, c.Apps[1].ID)
	}
	e := c.Apps[0]
	if e.Name != "App org.sorglos.calculator" || e.Publisher != "Sorglos IT" ||
		e.Description != "Eine Test-App" || e.Price != nil || len(e.Versions) != 2 {
		t.Fatalf("entry: %+v", e)
	}
	if e.Versions[0].Version != "1.0.0" || e.Versions[1].Version != "1.2.0" {
		t.Fatalf("versions: %+v", e.Versions)
	}
	for i, path := range []string{calc1, calc2} {
		p := e.Versions[i]
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.URL != filepath.Base(path) || p.Size != info.Size() || !bytes.Equal(p.SHA256, fileSum(t, path)) {
			t.Fatalf("version %d: %+v", i, p)
		}
		if !bytes.Equal(p.UI, uiHash(t, path)) {
			t.Fatalf("ui of version %d: %x", i, p.UI)
		}
		if p.Encrypted || p.MinNGC != "" || len(p.Needs) != 0 {
			t.Fatalf("version %d carries more than it should: %+v", i, p)
		}
	}
	if c.Apps[1].Versions[0].URL != filepath.Base(term) {
		t.Fatalf("terminal url: %s", c.Apps[1].Versions[0].URL)
	}
}

// TestCatalogSortsByTheContractNotByFileName: the catalog's order is apps by
// ID and versions ascending (store.Catalog.Validate). Here the file names run
// against both, so an order taken from the directory cannot pass.
func TestCatalogSortsByTheContractNotByFileName(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	for _, f := range []struct{ file, id, version string }{
		{"aaa", "org.sorglos.zebra", "1.0.0"},
		{"bbb", "org.sorglos.alpha", "2.0.0"},
		{"ccc", "org.sorglos.alpha", "1.0.0"},
	} {
		src := t.TempDir()
		write(t, filepath.Join(src, "manifest.json"), []byte(fmt.Sprintf(
			`{"id":%q,"name":"App","version":%q,"publisher":"Sorglos IT","ui":{"entry":"index.html"}}`,
			f.id, f.version)))
		write(t, filepath.Join(src, "ui", "index.html"), []byte("<!doctype html><title>"+f.file+"</title>"))
		if _, _, err := Pack(src, filepath.Join(dir, f.file+".ngc")); err != nil {
			t.Fatal(err)
		}
	}
	env, err := Catalog(dir, key, opts(1, ""))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	c := readBack(t, env, pub, 0)
	if len(c.Apps) != 2 || c.Apps[0].ID != "org.sorglos.alpha" || c.Apps[1].ID != "org.sorglos.zebra" {
		t.Fatalf("apps: %+v", c.Apps)
	}
	v := c.Apps[0].Versions
	if len(v) != 2 || v[0].Version != "1.0.0" || v[1].Version != "2.0.0" ||
		v[0].URL != "ccc.ngc" || v[1].URL != "bbb.ngc" {
		t.Fatalf("versions: %+v", v)
	}
}

// TestCatalogComputesUIBeforeEncryption: for a paid app the ui hash is the
// one of the plain package, while size and SHA-256 are those of the
// encrypted file — that is what an installation downloads and checks.
//
// The control runs first on the very same directory: without the .enc and
// the price the app is free and its ui hash is the one below. So what the
// case then measures is the work of those two files, not an earlier rule.
func TestCatalogComputesUIBeforeEncryption(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	plain := packInto(t, dir, "org.sorglos.terminal", "1.0.0", "")
	want := uiHash(t, plain)

	free, err := Catalog(dir, key, opts(1, ""))
	if err != nil {
		t.Fatalf("catalog of the plain package: %v", err)
	}
	if p := readBack(t, free, pub, 0).Apps[0].Versions[0]; p.Encrypted || !bytes.Equal(p.UI, want) {
		t.Fatalf("control: %+v", p)
	}

	// The encrypted file stands for what Task 27 writes: other bytes, another
	// length. Only its size and checksum are read here, so a placeholder
	// measures exactly what has to be measured.
	sealed := plain + EncSuffix
	write(t, sealed, append([]byte("sealed:"), read(t, plain)...))
	write(t, strings.TrimSuffix(plain, ".ngc")+PriceSuffix, []byte(testPrice))

	env, err := Catalog(dir, key, opts(2, ""))
	if err != nil {
		t.Fatalf("catalog of the paid app: %v", err)
	}
	c := readBack(t, env, pub, 0)
	e := c.Apps[0]
	if e.Price == nil || e.Price.Model != store.PriceBuy || e.Price.Amount != 1500 {
		t.Fatalf("price: %+v", e.Price)
	}
	p := e.Versions[0]
	info, err := os.Stat(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Encrypted || p.URL != filepath.Base(sealed) || p.Size != info.Size() ||
		!bytes.Equal(p.SHA256, fileSum(t, sealed)) {
		t.Fatalf("encrypted version: %+v", p)
	}
	if !bytes.Equal(p.UI, want) {
		t.Fatalf("ui hash came from the encrypted file: %x want %x", p.UI, want)
	}
	// And the plain package's own checksum is nowhere in the entry: an
	// installation downloads the sealed file.
	if bytes.Equal(p.SHA256, fileSum(t, plain)) {
		t.Fatal("the checksum of the plain package was listed")
	}
}

func TestCatalogTakesMinNGCFromTheManifest(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	packInto(t, dir, "org.sorglos.calculator", "2.0.0", `,"minNgc":"1.4.0","needs":["network"]`)
	// Control on the same directory: without the two manifest members the
	// entry carries neither.
	packInto(t, dir, "org.sorglos.editor", "2.0.0", "")
	env, err := Catalog(dir, key, opts(3, ""))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	c := readBack(t, env, pub, 0)
	p := c.Apps[0].Versions[0]
	if p.MinNGC != "1.4.0" || len(p.Needs) != 1 || p.Needs[0] != store.NeedNetwork {
		t.Fatalf("calculator: %+v", p)
	}
	if q := c.Apps[1].Versions[0]; q.MinNGC != "" || len(q.Needs) != 0 {
		t.Fatalf("editor: %+v", q)
	}
}

// TestCatalogVerifies runs the result through the checks a store has to
// survive on the other side (Task 19): the right key, the right purpose, the
// store's own ID, and a sequence number that does not go back.
func TestCatalogVerifies(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	env, err := Catalog(dir, key, opts(6, "https://store.example.com/pkg"))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	me := trust.Store{ID: "teststore", Key: enc.Bytes(pub)}
	// Accepted at exactly the sequence number already seen.
	if _, err := store.VerifyCatalog(env, me, 6); err != nil {
		t.Fatalf("seq 6: %v", err)
	}
	c := readBack(t, env, pub, 0)
	// The signed download address passed store's own grammar (validURL);
	// this tool has no second one.
	if c.Apps[0].Versions[0].URL != "https://store.example.com/pkg/org.sorglos.calculator-1.0.0.ngc" {
		t.Fatalf("url: %s", c.Apps[0].Versions[0].URL)
	}
	// A reader that has already seen 7 calls this catalog a rollback.
	if _, err := store.VerifyCatalog(env, me, 7); !errors.Is(err, store.ErrRollback) {
		t.Fatalf("seq 7: want ErrRollback, got %v", err)
	}
	// Another key.
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	if _, err := store.VerifyCatalog(env, trust.Store{ID: "teststore", Key: enc.Bytes(other)}, 0); !errors.Is(err, sign.ErrWrongKey) {
		t.Fatalf("foreign key: want ErrWrongKey, got %v", err)
	}
	// A catalog that claims to hold the apps of another store.
	if _, err := store.VerifyCatalog(env, trust.Store{ID: "otherstore", Key: enc.Bytes(pub)}, 0); !errors.Is(err, store.ErrCatalog) {
		t.Fatalf("other store: want ErrCatalog, got %v", err)
	}
	// A bent signature.
	bent := env
	bent.Sig = bytes.Clone(env.Sig)
	bent.Sig[0] ^= 1
	if _, err := store.VerifyCatalog(bent, me, 0); !errors.Is(err, sign.ErrBadSignature) {
		t.Fatalf("bent signature: want ErrBadSignature, got %v", err)
	}
	// A signature of another purpose never verifies as a catalog.
	wrong, err := sign.Sign(ed25519.NewKeyFromSeed(testSeed), sign.PurposeLicense, env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyCatalog(wrong, me, 0); !errors.Is(err, sign.ErrBadSignature) {
		t.Fatalf("other purpose: want ErrBadSignature, got %v", err)
	}
	// And the envelope survives the way it travels: JSON, strict parsing,
	// then the same check.
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	again, err := sign.ParseEnvelope(b, sign.MaxPayload)
	if err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	if _, err := store.VerifyCatalog(again, me, 0); err != nil {
		t.Fatalf("after the round trip: %v", err)
	}
}

// TestCatalogRefusesTooManyVersions: store.MaxVersions versions of one app
// are the most a catalog holds. One more is an error and not a catalog with
// the first hundred in it.
func TestCatalogRefusesTooManyVersions(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	for i := 0; i < store.MaxVersions; i++ {
		packInto(t, dir, "org.sorglos.calculator", fmt.Sprintf("1.0.%d", i), "")
	}
	// Both sides of the border: exactly store.MaxVersions still signs.
	env, err := Catalog(dir, key, opts(1, ""))
	if err != nil {
		t.Fatalf("%d versions: %v", store.MaxVersions, err)
	}
	if n := len(readBack(t, env, pub, 0).Apps[0].Versions); n != store.MaxVersions {
		t.Fatalf("versions: %d", n)
	}
	packInto(t, dir, "org.sorglos.calculator", fmt.Sprintf("1.0.%d", store.MaxVersions), "")
	if _, err := Catalog(dir, key, opts(2, "")); !errors.Is(err, store.ErrCatalog) {
		t.Fatalf("one version too many: want ErrCatalog, got %v", err)
	}
	// And nothing was written: the directory holds no catalog, so the next
	// run does not find a sequence number this one never signed.
	if _, err := os.Stat(filepath.Join(dir, CatalogFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a catalog was left behind: %v", err)
	}
}

// TestAssembleRefusesTooManyVersionsInTotal: store.MaxCatalogVersions
// versions of all apps together are the limit. It cannot be driven through
// Catalog — 30001 versions need at least 301 apps of 100 versions, and that
// many packages are not written in a test — so it is measured one layer
// down, on the function that builds and checks the catalog. What Catalog
// adds on top of it (reading the directory) is measured by the case above.
func TestAssembleRefusesTooManyVersionsInTotal(t *testing.T) {
	build := func(total int) []appVersion {
		var out []appVersion
		for i := 0; total > 0; i++ {
			id := fmt.Sprintf("org.sorglos.a%04d", i)
			for v := 0; v < store.MaxVersions && total > 0; v++ {
				out = append(out, appVersion{
					entry: store.Entry{ID: id, Name: "App", Publisher: "Sorglos IT"},
					pkg: store.Package{Version: fmt.Sprintf("1.0.%d", v), URL: "a.ngc", Size: 1,
						SHA256: bytes.Repeat([]byte{2}, 32), UI: bytes.Repeat([]byte{3}, 32)},
				})
				total--
			}
		}
		return out
	}
	if _, err := assemble(opts(1, ""), build(store.MaxCatalogVersions)); err != nil {
		t.Fatalf("%d versions: %v", store.MaxCatalogVersions, err)
	}
	if _, err := assemble(opts(1, ""), build(store.MaxCatalogVersions+1)); !errors.Is(err, store.ErrCatalog) {
		t.Fatalf("one version too many: want ErrCatalog, got %v", err)
	}
}

// TestCatalogRefusesASequenceThatDoesNotRise is what this tool does against
// a rolled-back sequence number: a catalog signed with a number a reader has
// already seen is store.ErrRollback at every installation, and nobody there
// can fix it. So the number has to rise, and it is checked against the
// catalog that lies in the directory.
func TestCatalogRefusesASequenceThatDoesNotRise(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	// Without a catalog in the directory any number above 0 is fine, 0 is not
	// (the contract refuses seq 0 as well).
	if _, err := Catalog(dir, key, opts(0, "")); !errors.Is(err, ErrSeq) {
		t.Fatalf("seq 0: want ErrSeq, got %v", err)
	}
	if code := Run([]string{"catalog", dir, key, "teststore", "7"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("writing seq 7: exit %d", code)
	}
	stored := readStored(t, dir, pub)
	if stored.Seq != 7 {
		t.Fatalf("stored seq: %d", stored.Seq)
	}
	// Both sides of the border: 7 and 6 are refused, 8 goes through.
	for _, seq := range []uint64{6, 7} {
		if _, err := Catalog(dir, key, opts(seq, "")); !errors.Is(err, ErrSeq) {
			t.Fatalf("seq %d: want ErrSeq, got %v", seq, err)
		}
	}
	if _, err := Catalog(dir, key, opts(8, "")); err != nil {
		t.Fatalf("seq 8: %v", err)
	}
	// The catalog in the directory is still the one of seq 7: a refused run
	// changes nothing.
	if again := readStored(t, dir, pub); again.Seq != 7 {
		t.Fatalf("stored seq after the refusals: %d", again.Seq)
	}
	// A catalog that cannot be read with this key says nothing about the
	// numbers readers have seen, so it is a refusal and not a 0.
	for _, what := range []string{"not json", `{"payload":"AAAA","kid":"AAAAAAAAAAAAAAAAAAAAAA","sig":"AA"}`} {
		write(t, filepath.Join(dir, CatalogFile), []byte(what))
		if _, err := Catalog(dir, key, opts(9, "")); !errors.Is(err, ErrSeq) {
			t.Fatalf("broken catalog %q: want ErrSeq, got %v", what, err)
		}
	}
	// Signed, valid — but with another key: also no answer about the
	// sequence numbers of this store.
	otherKey, _ := signingKey(t, bytes.Repeat([]byte{7}, ed25519.SeedSize))
	os.Remove(filepath.Join(dir, CatalogFile))
	if code := Run([]string{"catalog", dir, otherKey, "teststore", "40"}, io.Discard, io.Discard); code != 0 {
		t.Fatal("the other key could not write a catalog")
	}
	if _, err := Catalog(dir, key, opts(41, "")); !errors.Is(err, ErrSeq) {
		t.Fatalf("catalog of another key: want ErrSeq, got %v", err)
	}
}

// readStored reads the catalog that lies in dir the way an installation does.
func readStored(t *testing.T, dir string, pub ed25519.PublicKey) *store.Catalog {
	t.Helper()
	env, err := sign.ParseEnvelope(read(t, filepath.Join(dir, CatalogFile)), sign.MaxPayload)
	if err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	return readBack(t, env, pub, 0)
}

// TestCatalogRefusesAnAddressOutsideTheGrammar: the addresses a catalog
// carries are judged by store's own grammar, the one store.VerifyCatalog
// applies. This tool writes no grammar of its own, so the case proves the
// refusal comes from there — and the control shows the way to it was open.
func TestCatalogRefusesAnAddressOutsideTheGrammar(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	// Control: a base the grammar allows signs, on this very directory.
	env, err := Catalog(dir, key, opts(1, "https://store.example.com/p"))
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	readBack(t, env, pub, 0)
	for _, base := range []string{
		"http://store.example.com/p",                           // no TLS
		"https://store.example.com/p#frag",                     // a fragment says nothing to a server
		"https://store.example.com/a b",                        // a space
		"https://store.example.com/a<b",                        // breaks out of an HTML attribute
		"https://user@store.example.com",                       // user info
		"https://store.example.com:0",                          // port 0
		"ftp://store.example.com/p",                            // not an address of a catalog at all
		"https://" + strings.Repeat("a", 600) + ".example.com", // longer than store's maxURL
	} {
		if _, err := Catalog(dir, key, opts(1, base)); !errors.Is(err, store.ErrCatalog) {
			t.Fatalf("base %q: want ErrCatalog, got %v", base, err)
		}
	}
	// And the one case the grammar cannot catch, because the address stays
	// inside it: a base that already carries a query. That refusal is this
	// tool's own rule about the shape of a base.
	if _, err := Catalog(dir, key, opts(1, "https://store.example.com/p?token=abc")); !errors.Is(err, ErrPack) {
		t.Fatalf("base with a query: want ErrPack, got %v", err)
	}
}

// TestCatalogTakesTheMetadataOfTheNewestVersion: a rename or a new price
// arrives with a new version, and that is the version a store shows.
func TestCatalogTakesTheMetadataOfTheNewestVersion(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	// The file names make the older version the one read first (sorted
	// glob), so the newest has to win over the order on disk.
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	src := t.TempDir()
	write(t, filepath.Join(src, "manifest.json"), []byte(`{"id":"org.sorglos.calculator","name":"Rechner",`+
		`"version":"2.0.0","publisher":"Sorglos IT","ui":{"entry":"index.html"}}`))
	write(t, filepath.Join(src, "ui", "index.html"), []byte("<!doctype html><title>2</title>"))
	if _, _, err := Pack(src, filepath.Join(dir, "org.sorglos.calculator-2.0.0.ngc")); err != nil {
		t.Fatal(err)
	}
	env, err := Catalog(dir, key, opts(1, ""))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	e := readBack(t, env, pub, 0).Apps[0]
	if e.Name != "Rechner" || e.Description != "" || len(e.Versions) != 2 {
		t.Fatalf("entry: %+v", e)
	}
}

// TestCatalogRefusesAFileThatIsNoPackage: whatever lies in the directory
// under *.ngc has to be a package. A refusal here leaves no catalog.
func TestCatalogRefusesAFileThatIsNoPackage(t *testing.T) {
	dir := t.TempDir()
	key, _ := signingKey(t, testSeed)
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	write(t, filepath.Join(dir, "broken.ngc"), []byte("no tar at all"))
	if _, err := Catalog(dir, key, opts(1, "")); err == nil {
		t.Fatal("a file that is no package was listed")
	}
	if _, err := os.Stat(filepath.Join(dir, CatalogFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a catalog was left behind: %v", err)
	}
}

// TestCatalogRefusesABrokenKeyFile: a key file that is no 32-byte seed is
// ErrPack, and no byte of it reaches the error.
func TestCatalogRefusesABrokenKeyFile(t *testing.T) {
	dir := t.TempDir()
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	for _, body := range []string{"", "short", enc.B64.EncodeToString(bytes.Repeat([]byte{9}, 31)), "***"} {
		path := filepath.Join(t.TempDir(), "store.key")
		write(t, path, []byte(body))
		_, err := Catalog(dir, path, opts(1, ""))
		if !errors.Is(err, ErrPack) {
			t.Fatalf("key %q: want ErrPack, got %v", body, err)
		}
		if body != "" && strings.Contains(err.Error(), body) {
			t.Fatalf("the error quotes the key file: %v", err)
		}
	}
}

// TestCatalogTellsNoSecret: the seed of the signing key appears in no
// message of the tool, in none of its three shapes (plain, base64url, hex),
// and the call itself carries only the path of the key file, never the key.
// Measured against the output of a run that worked and of one that failed.
func TestCatalogTellsNoSecret(t *testing.T) {
	dir := t.TempDir()
	key, _ := signingKey(t, testSeed)
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	shapes := []string{string(testSeed), enc.B64.EncodeToString(testSeed),
		hex.EncodeToString(testSeed), strings.ToUpper(hex.EncodeToString(testSeed))}
	runs := [][]string{
		{"catalog", dir, key, "teststore", "1"},                // works
		{"catalog", dir, key, "teststore", "1"},                // refused: the sequence number does not rise
		{"catalog", dir, key, "teststore", "nonumber"},         // refused: no number at all
		{"catalog", filepath.Join(dir, "gone"), key, "s", "2"}, // refused: no such directory
	}
	for i, args := range runs {
		var out, errOut bytes.Buffer
		Run(args, &out, &errOut)
		for _, where := range []struct {
			name string
			text string
		}{{"stdout", out.String()}, {"stderr", errOut.String()}, {"the call", strings.Join(args, " ")}} {
			for _, s := range shapes {
				if strings.Contains(where.text, s) {
					t.Fatalf("run %d: the seed is in %s", i, where.name)
				}
			}
		}
		// The control: the output of a run says something at all, so the
		// case is not green because nothing was printed.
		if out.Len()+errOut.Len() == 0 {
			t.Fatalf("run %d printed nothing", i)
		}
	}
}

// TestRunWritesTheCatalog: the subcommand writes the signed catalog beside
// the packages and a second run with the same number refuses, leaving the
// first catalog untouched.
func TestRunWritesTheCatalog(t *testing.T) {
	dir := t.TempDir()
	key, pub := signingKey(t, testSeed)
	packInto(t, dir, "org.sorglos.calculator", "1.0.0", "")
	var out, errOut bytes.Buffer
	if code := Run([]string{"catalog", dir, key, "teststore", "5", "https://store.example.com/p"},
		&out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "teststore 5") {
		t.Fatalf("stdout: %q", out.String())
	}
	c := readStored(t, dir, pub)
	if c.Seq != 5 || c.Issued <= 0 || len(c.Apps) != 1 {
		t.Fatalf("catalog: %+v", c)
	}
	first := read(t, filepath.Join(dir, CatalogFile))
	errOut.Reset()
	if code := Run([]string{"catalog", dir, key, "teststore", "5"}, io.Discard, &errOut); code != 1 {
		t.Fatalf("the second run with seq 5: exit %d", code)
	}
	if !bytes.Equal(first, read(t, filepath.Join(dir, CatalogFile))) {
		t.Fatal("the refused run changed the catalog")
	}
	// No leftover of the atomic write.
	if _, err := os.Stat(filepath.Join(dir, CatalogFile+".new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a half-written catalog was left behind: %v", err)
	}
	// A wrong call is exit 2 and prints the usage.
	errOut.Reset()
	if code := Run([]string{"catalog", dir}, io.Discard, &errOut); code != 2 || !strings.Contains(errOut.String(), "ngc-store catalog") {
		t.Fatalf("wrong call: exit %d %q", code, errOut.String())
	}
}
