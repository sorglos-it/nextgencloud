package classes

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// storeFixture is one test store that holds: a free app, a paid app with its
// encrypted form and its price, a signed catalog and the public half of the
// signing key.
type storeFixture struct {
	dir     string
	id      string
	keyFile string
	pub     ed25519.PublicKey
	free    string // the free app's package, a path
	paid    string // the paid app's plain package, a path
}

// paidPrice is the price file of the paid app of these cases.
const paidPrice = `{"model":"buy","per":"site","amount":2500,"currency":"EUR",` +
	`"shop":"https://shop.example.org/terminal"}`

// newStore builds a store that CheckStore accepts. Every case below starts
// from a fresh copy of it, breaks exactly one thing and reads what CheckStore
// then says — and each one first checks the unbroken copy, so that a refusal
// is about the break and not about an earlier wall (docs: messen.md).
func newStore(t testing.TB) storeFixture {
	t.Helper()
	dir := t.TempDir()
	f := storeFixture{dir: dir, id: "teststore", keyFile: filepath.Join(t.TempDir(), "store.key")}
	write(t, f.keyFile, []byte(enc.B64.EncodeToString(testSeed)+"\n"))
	f.pub = ed25519.NewKeyFromSeed(testSeed).Public().(ed25519.PublicKey)
	f.free = packApp(t, dir, "org.sorglos.calculator", "1.0.0")
	f.paid = packApp(t, dir, "org.sorglos.terminal", "1.0.0")
	encryptPaid(t, dir, f.paid)
	write(t, strings.TrimSuffix(f.paid, ".ngc")+PriceSuffix, []byte(paidPrice))
	f.sign(t, 1)
	return f
}

// sign writes catalog.json and store-key.pem into the store.
func (f storeFixture) sign(t testing.TB, seq uint64) {
	t.Helper()
	env, err := Catalog(f.dir, f.keyFile, CatalogOpts{Store: f.id, Seq: seq, Issued: 1_790_000_000})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.dir, CatalogFile), b)
	pem, err := PublicKeyPEM(f.pub)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.dir, StoreKeyFile), pem)
}

// packApp lays down an app and packs it into dir, like packInto but with the
// name a store serves: "<id>-<version>.ngc".
func packApp(t testing.TB, dir, id, version string) string {
	t.Helper()
	src := t.TempDir()
	manifest := `{"id":"` + id + `","name":"App ` + id + `","version":"` + version + `",` +
		`"publisher":"Sorglos IT","description":"Eine Test-App","ui":{"entry":"index.html"}}`
	write(t, filepath.Join(src, "manifest.json"), []byte(manifest))
	write(t, filepath.Join(src, "ui", "index.html"), []byte("<!doctype html><title>"+id+" "+version+"</title>"))
	out := filepath.Join(dir, id+"-"+version+".ngc")
	if _, _, err := Pack(src, out); err != nil {
		t.Fatalf("pack %s %s: %v", id, version, err)
	}
	return out
}

// encryptPaid seals a package with a content key of its own, under the name
// the catalog looks for (path + EncSuffix, not "app.enc").
func encryptPaid(t testing.TB, dir, path string) []byte {
	t.Helper()
	keyFile := filepath.Join(dir, "content.key")
	key, err := NewContentKey(keyFile)
	if err != nil {
		t.Fatalf("content key: %v", err)
	}
	if err := Encrypt(path, path+EncSuffix, keyFile); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return key
}

// copyStore copies the store into a directory of its own, so that a case
// that breaks it leaves every other case untouched.
func copyStore(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	if err := filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	}); err != nil {
		t.Fatalf("copy the store: %v", err)
	}
	// The copy itself is checked: a case that measured against half a store
	// would measure nothing.
	if err := CheckStore(dst, "teststore", 0); err != nil {
		t.Fatalf("the copy is already broken: %v", err)
	}
	return dst
}

func TestCheckStoreTakesAStoreThatHolds(t *testing.T) {
	f := newStore(t)
	if err := CheckStore(f.dir, f.id, 0); err != nil {
		t.Fatalf("a store that holds: %v", err)
	}
	// And the sequence number it carries is not below itself.
	if err := CheckStore(f.dir, f.id, 1); err != nil {
		t.Errorf("seq 1 against seq 1: %v", err)
	}
}

// TestCheckStoreNamesTheReason is the case this subcommand exists for. Each
// entry breaks one thing in a store that held a moment before, and the
// reason CheckStore names has to be the reason of THAT break — not just any
// refusal. A test that only asked "does it fail" would pass for a check
// that answered "wrong" to everything.
func TestCheckStoreNamesTheReason(t *testing.T) {
	good := newStore(t)
	// A store with a second version of the free app, so that "the catalog
	// names a version that is not there" can be built without touching the
	// first one.
	twoVersions := newStore(t)
	packApp(t, twoVersions.dir, "org.sorglos.calculator", "1.1.0")
	twoVersions.sign(t, 2)

	cases := []struct {
		name   string
		from   string // the store to copy; empty means good
		id     string // the ID to check against; empty means "teststore"
		seq    uint64
		break_ func(t *testing.T, dir string)
		want   string
	}{
		{name: "a file of the catalog is gone", want: "org.sorglos.calculator 1.0.0: file missing: org.sorglos.calculator-1.0.0.ngc",
			break_: func(t *testing.T, dir string) { remove(t, dir, "org.sorglos.calculator-1.0.0.ngc") }},
		{name: "a file was changed", want: "org.sorglos.calculator 1.0.0: checksum wrong: org.sorglos.calculator-1.0.0.ngc",
			break_: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, "org.sorglos.calculator-1.0.0.ngc"), []byte("das ist kein Paket mehr\n"))
			}},
		{name: "the catalog names a version that is not there", from: twoVersions.dir,
			want:   "org.sorglos.calculator 1.1.0: file missing: org.sorglos.calculator-1.1.0.ngc",
			break_: func(t *testing.T, dir string) { remove(t, dir, "org.sorglos.calculator-1.1.0.ngc") }},
		{name: "the encrypted version is gone", want: "org.sorglos.terminal 1.0.0: encrypted version missing: org.sorglos.terminal-1.0.0.ngc.enc",
			break_: func(t *testing.T, dir string) { remove(t, dir, "org.sorglos.terminal-1.0.0.ngc.enc") }},
		{name: "the plain package behind a paid app is gone",
			want:   "org.sorglos.terminal 1.0.0: plain package missing: org.sorglos.terminal-1.0.0.ngc",
			break_: func(t *testing.T, dir string) { remove(t, dir, "org.sorglos.terminal-1.0.0.ngc") }},
		{name: "the price is gone", want: "org.sorglos.terminal 1.0.0: price missing: org.sorglos.terminal-1.0.0.price.json",
			break_: func(t *testing.T, dir string) { remove(t, dir, "org.sorglos.terminal-1.0.0.price.json") }},
		{name: "the price is no price", want: "org.sorglos.terminal 1.0.0: price unreadable: org.sorglos.terminal-1.0.0.price.json",
			break_: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, "org.sorglos.terminal-1.0.0.price.json"), []byte("{\"model\":}"))
			}},
		{name: "the sequence number was turned back", seq: 5,
			want:   "catalog rolled back: below the sequence number 5 already seen",
			break_: func(t *testing.T, dir string) {}},
		{name: "another key signed the catalog", want: "catalog does not verify",
			break_: func(t *testing.T, dir string) {
				other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x77}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
				pem, err := PublicKeyPEM(other)
				if err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(dir, StoreKeyFile), pem)
			}},
		{name: "the catalog is of another store", id: "anderer-store",
			want:   `catalog of another store: "teststore", not "anderer-store"`,
			break_: func(t *testing.T, dir string) {}},
		{name: "the catalog was tampered with", want: "catalog does not verify",
			break_: func(t *testing.T, dir string) {
				b := read(t, filepath.Join(dir, CatalogFile))
				var m map[string]string
				if json.Unmarshal(b, &m) != nil {
					t.Fatal("the catalog is no envelope")
				}
				p, err := enc.B64.DecodeString(m["payload"])
				if err != nil {
					t.Fatal(err)
				}
				m["payload"] = enc.B64.EncodeToString(bytes.Replace(p, []byte(`"seq":1`), []byte(`"seq":9`), 1))
				out, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(dir, CatalogFile), out)
			}},
		{name: "the catalog is gone", want: "catalog unreadable: catalog.json",
			break_: func(t *testing.T, dir string) { remove(t, dir, CatalogFile) }},
		{name: "the key is gone", want: "store key unreadable: store-key.pem",
			break_: func(t *testing.T, dir string) { remove(t, dir, StoreKeyFile) }},
		{name: "the key is no key", want: "key is no PEM PUBLIC KEY block",
			break_: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, StoreKeyFile), []byte("kein PEM\n"))
			}},
		{name: "a package was packed after the catalog was signed", want: "package not in the catalog: org.sorglos.editor-1.0.0.ngc",
			break_: func(t *testing.T, dir string) { packApp(t, dir, "org.sorglos.editor", "1.0.0") }},
		{name: "the plain package behind a paid app was replaced",
			want: "org.sorglos.terminal 1.0.0: ui hash wrong: org.sorglos.terminal-1.0.0.ngc",
			break_: func(t *testing.T, dir string) {
				// Another valid package of the same app and version, with
				// another UI: the encrypted form still matches its checksum,
				// so only the ui hash can see this.
				src := t.TempDir()
				write(t, filepath.Join(src, "manifest.json"), []byte(`{"id":"org.sorglos.terminal",`+
					`"name":"App org.sorglos.terminal","version":"1.0.0","publisher":"Sorglos IT",`+
					`"description":"Eine Test-App","ui":{"entry":"index.html"}}`))
				write(t, filepath.Join(src, "ui", "index.html"), []byte("<!doctype html><title>etwas anderes</title>"))
				if _, _, err := Pack(src, filepath.Join(dir, "org.sorglos.terminal-1.0.0.ngc")); err != nil {
					t.Fatalf("pack: %v", err)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			from := c.from
			if from == "" {
				from = good.dir
			}
			// copyStore checks the copy before it is broken: the
			// intermediate check that the way to the line under test is open.
			dir := copyStore(t, from)
			id := c.id
			if id == "" {
				id = "teststore"
			}
			c.break_(t, dir)
			err := CheckStore(dir, id, c.seq)
			if err == nil {
				t.Fatalf("the store is broken and CheckStore takes it, want %q", c.want)
			}
			if err.Error() != c.want {
				t.Errorf("CheckStore says %q, want %q", err, c.want)
			}
		})
	}
}

// remove deletes one file of a store copy and makes sure it was there: a
// break that broke nothing would leave the case measuring a store that
// holds.
func remove(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s is not there, so this case breaks nothing: %v", name, err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}
}

// TestRunChecksAStore is the subcommand end to end: the flag tells the two
// calls of check apart, a store that holds prints "store ok", and a broken
// one names its reason on stderr and returns 1.
func TestRunChecksAStore(t *testing.T) {
	f := newStore(t)
	var out, errOut bytes.Buffer
	if code := Run([]string{"check", "-store", f.id, f.dir}, &out, &errOut); code != 0 || out.String() != "store ok\n" {
		t.Fatalf("check -store: %d, %q, %s", code, out.String(), errOut.String())
	}
	remove(t, f.dir, "org.sorglos.terminal-1.0.0.price.json")
	out.Reset()
	errOut.Reset()
	code := Run([]string{"check", "-store", f.id, f.dir}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "store wrong: org.sorglos.terminal 1.0.0: price missing:") {
		t.Errorf("check of a broken store: %d, %q, %q", code, out.String(), errOut.String())
	}
	// Without the flag the same arguments are a release check and not a
	// store check, so a store ID that looks like a part cannot be mistaken
	// for one.
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"check", f.dir}, &out, &errOut); code != 2 {
		t.Errorf("check without the flag and without part and arch: %d", code)
	}
}

// resign reads the catalog of a store copy, lets change rewrite it and signs
// it again with the store's key. It is how a forged store is built: one that
// is signed, valid by the contract and still wrong about the directory it
// lies in. Nothing but a forgery can produce these two cases — Catalog takes
// "encrypted" from whether the encrypted file is there, so the two can never
// disagree in a catalog this tool wrote.
func resign(t *testing.T, dir, keyFile string, change func(c *store.Catalog)) {
	t.Helper()
	b := read(t, filepath.Join(dir, CatalogFile))
	env, err := sign.ParseEnvelope(b, sign.MaxPayload)
	if err != nil {
		t.Fatalf("the catalog is no envelope: %v", err)
	}
	var c store.Catalog
	if err := json.Unmarshal(env.Payload, &c); err != nil {
		t.Fatalf("the payload is no catalog: %v", err)
	}
	// The two addresses are copied as strings, not as the struct: Apps is a
	// slice, so a copy of the Catalog still points at the same entries and
	// would show the change as if it had always been there.
	before := []string{c.Apps[0].Versions[0].URL, c.Apps[1].Versions[0].URL}
	change(&c)
	if c.Apps[0].Versions[0].URL == before[0] && c.Apps[1].Versions[0].URL == before[1] {
		t.Fatal("the change changed nothing, so this case measures nothing")
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("the forged catalog is no valid catalog, so it would be refused earlier: %v", err)
	}
	payload, err := json.Marshal(&c)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := readStoreKey(keyFile)
	if err != nil {
		t.Fatalf("store key: %v", err)
	}
	out, err := sign.Sign(priv, sign.PurposeCatalog, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, CatalogFile), raw)
}

// TestCheckStoreTellsTheEncryptedFormFromThePlainOne is the pair of reasons
// no honest catalog can carry: Catalog writes "encrypted" exactly when the
// encrypted file lies beside the package, so the entry and the name always
// agree. A forged one can disagree, and then the entry promises an
// installation one thing and the directory holds another: a paid app served
// in the clear, or a free one nobody can open.
func TestCheckStoreTellsTheEncryptedFormFromThePlainOne(t *testing.T) {
	good := newStore(t)
	for _, c := range []struct {
		name   string
		change func(c *store.Catalog)
		want   string
	}{
		{"encrypted but names the plain package", func(c *store.Catalog) {
			c.Apps[1].Versions[0].URL = "org.sorglos.terminal-1.0.0.ngc"
		}, "org.sorglos.terminal 1.0.0: entry says encrypted but names a plain package: org.sorglos.terminal-1.0.0.ngc"},
		{"free but names the encrypted package", func(c *store.Catalog) {
			c.Apps[0].Versions[0].URL = "org.sorglos.terminal-1.0.0.ngc.enc"
		}, "org.sorglos.calculator 1.0.0: entry says free but names an encrypted package: org.sorglos.terminal-1.0.0.ngc.enc"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// copyStore checks the copy first: the way to the line under
			// test is open before the forgery.
			dir := copyStore(t, good.dir)
			resign(t, dir, good.keyFile, c.change)
			err := CheckStore(dir, good.id, 0)
			if err == nil {
				t.Fatalf("the forged store passes, want %q", c.want)
			}
			if err.Error() != c.want {
				t.Errorf("CheckStore says %q, want %q", err, c.want)
			}
		})
	}
}
