package classes

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrSeq reports a sequence number that would not be higher than the one of
// the catalog already in the directory. A reader refuses a catalog whose seq
// is below the newest it has seen (store.ErrRollback); writing such a one is
// a mistake of whoever runs the store, so it is caught here instead of at
// every installation.
var ErrSeq = errors.New("ngc-store: the catalog needs a higher sequence number than the last one")

// CatalogFile is the name ngc-store reads the last catalog of a directory
// under and writes the new one to.
const CatalogFile = "catalog.json"

// EncSuffix is the encrypted form of a paid app beside the plain package. A
// package that has one is offered as encrypted: its size and SHA-256 are the
// ones of this file, because that is what an installation downloads.
//
// Task 27 builds the encrypt subcommand that writes it; still to be checked
// is that the two agree on the name. Measured here only against a
// placeholder file of other bytes and another length, which is all that size
// and checksum need.
const EncSuffix = ".enc"

// PriceSuffix names the price of a paid app: "app.ngc" is paid if
// "app.price.json" lies beside it and holds a store.Price. The manifest
// carries no price — the same package may be sold by several stores at
// different prices — and a catalog entry of an encrypted package without a
// price is no valid catalog (store.Entry.valid ties Encrypted to Price).
const PriceSuffix = ".price.json"

// priceBudget bounds a price file while it is decoded (enc.Budget): a price
// has no array.
var priceBudget = enc.Budget{Size: 4096}

// CatalogOpts describe the catalog to write.
type CatalogOpts struct {
	Store   string // the store's ID (wire.ValidName)
	Seq     uint64 // higher than the last one, else readers refuse it
	Issued  int64
	BaseURL string // where the packages lie; empty writes paths beside the catalog
}

// Catalog reads every package in dir and returns a signed catalog. The ui
// hash of a version is store.PackageUI over the plain package, computed
// before a paid app is encrypted; minNgc, the needs and the app's name come
// from the manifest.
//
// Catalog builds no character grammar for addresses. A URL it composes from
// BaseURL is judged by store.Catalog.Validate, the same rule a reader
// applies through store.VerifyCatalog (store's validURL), so an address this
// tool writes is an address an installation fetches — and one outside the
// grammar is refused here instead of at every installation. It has exactly
// one rule of its own about an address, and it is about the shape of the
// base, not about its characters: a base that already carries a query gets
// no file name appended (entryOf).
//
// Nothing of a package's checksum depends on when it was packed: Pack writes
// a fixed modification time, so the SHA-256 and the ui hash listed here
// describe the package's content. The catalog itself is not byte-identical
// between two runs, and must not be: it carries seq and issued.
//
// Catalog writes no file at all. Whatever it refuses — a sequence number
// that does not rise, a *.ngc that is no package, an address outside the
// grammar, more versions than a catalog holds — leaves nothing behind,
// because the caller only ever sees an envelope or an error. The subcommand
// in Run writes the envelope, and only after Catalog returned one.
func Catalog(dir, keyFile string, opts CatalogOpts) (sign.Envelope, error) {
	priv, err := readSigningKey(keyFile)
	if err != nil {
		return sign.Envelope{}, err
	}
	pub := priv.Public().(ed25519.PublicKey)
	// Before anything is read: a sequence number that does not rise is the
	// rollback a reader refuses, and the only one who can still fix it is the
	// person at the keyboard.
	last, err := lastSeq(dir, pub, opts.Store)
	if err != nil {
		return sign.Envelope{}, err
	}
	if opts.Seq <= last {
		return sign.Envelope{}, ErrSeq
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.ngc"))
	if err != nil {
		return sign.Envelope{}, err
	}
	slices.Sort(names)
	versions := make([]appVersion, 0, len(names))
	for _, name := range names {
		v, err := entryOf(name, opts.BaseURL)
		if err != nil {
			return sign.Envelope{}, err
		}
		versions = append(versions, v)
	}
	c, err := assemble(opts, versions)
	if err != nil {
		return sign.Envelope{}, err
	}
	// encoding/json, not core.EncodeResult: a catalog is no answer of the API.
	// store.Catalog has no field that could become null — the lists that may
	// be empty carry omitempty — and the round trip through
	// store.VerifyCatalog is measured (TestCatalogVerifies).
	payload, err := json.Marshal(c)
	if err != nil {
		return sign.Envelope{}, err
	}
	return sign.Sign(priv, sign.PurposeCatalog, payload)
}

// appVersion is one package: the app it belongs to and the version itself.
type appVersion struct {
	entry store.Entry // without Versions
	pkg   store.Package
}

// assemble groups the versions by app, sorts both lists as the contract
// demands and checks the result. An app's name, publisher, description and
// price are the ones of its newest version: that is the version a store
// shows, and a rename or a new price arrives with it.
func assemble(opts CatalogOpts, versions []appVersion) (store.Catalog, error) {
	c := store.Catalog{Store: opts.Store, Seq: opts.Seq, Issued: opts.Issued}
	at := map[string]int{}
	// The highest version seen per app. store.Newer is false whenever either
	// side is no version, and that includes the empty string: the first
	// version of an app is therefore taken by the explicit test for "" below,
	// never by a comparison against it.
	top := map[string]string{}
	for _, v := range versions {
		i, ok := at[v.entry.ID]
		if !ok {
			at[v.entry.ID] = len(c.Apps)
			c.Apps = append(c.Apps, v.entry)
			i = len(c.Apps) - 1
		} else if store.Newer(v.pkg.Version, top[v.entry.ID]) {
			kept := c.Apps[i].Versions
			c.Apps[i] = v.entry
			c.Apps[i].Versions = kept
		}
		if store.Newer(v.pkg.Version, top[v.entry.ID]) || top[v.entry.ID] == "" {
			top[v.entry.ID] = v.pkg.Version
		}
		c.Apps[i].Versions = append(c.Apps[i].Versions, v.pkg)
	}
	for i := range c.Apps {
		// Versions ascend and apps are sorted by ID: store.Catalog.Validate
		// refuses anything else.
		slices.SortFunc(c.Apps[i].Versions, func(a, b store.Package) int {
			switch {
			case store.Newer(b.Version, a.Version):
				return -1
			case store.Newer(a.Version, b.Version):
				return 1
			}
			return 0
		})
	}
	slices.SortFunc(c.Apps, func(a, b store.Entry) int { return strings.Compare(a.ID, b.ID) })
	if err := c.Validate(); err != nil {
		return store.Catalog{}, err
	}
	return c, nil
}

// entryOf reads one package: its manifest and the hash of its ui-tar from the
// plain package, its size and SHA-256 from the file an installation
// downloads — the encrypted one where there is one.
func entryOf(path, base string) (appVersion, error) {
	m, ui, err := packageUI(path)
	if err != nil {
		return appVersion{}, err
	}
	download := path
	price, err := readPrice(path)
	if err != nil {
		return appVersion{}, err
	}
	if _, err := os.Stat(path + EncSuffix); err == nil {
		download = path + EncSuffix
	} else if !errors.Is(err, fs.ErrNotExist) {
		return appVersion{}, err
	}
	size, sum, err := sizeAndSum(download)
	if err != nil {
		return appVersion{}, err
	}
	url := filepath.Base(download)
	if base != "" {
		// A base that already carries a query gets no file name appended:
		// "…?token=x/app.ngc" is still inside store's character grammar, so
		// the contract cannot catch it, and it names no file on any server.
		// This is a rule about the shape of the base, not a second grammar
		// for addresses.
		if strings.Contains(base, "?") {
			return appVersion{}, ErrPack
		}
		url = strings.TrimSuffix(base, "/") + "/" + url
	}
	p := store.Package{Version: m.Version, URL: url, Size: size, SHA256: enc.Bytes(sum),
		UI: enc.Bytes(ui), Encrypted: download != path, Needs: m.Needs, MinNGC: m.MinNGC}
	e := store.Entry{ID: m.ID, Name: m.Name, Publisher: m.Publisher, Description: m.Description, Price: price}
	return appVersion{entry: e, pkg: p}, nil
}

// packageUI reads the manifest and the ui hash of the plain package. The hash
// is store.PackageUI's, over the unencrypted package: the shell runs an app's
// UI only if its ui-tar has exactly this hash, so it has to be taken here,
// before a paid app is encrypted.
func packageUI(path string) (*store.Manifest, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return store.PackageUI(f, io.Discard)
}

// sizeAndSum returns the size and the SHA-256 of a file as it is downloaded.
// The size comes from the bytes that were hashed, not from the directory
// entry: a file that grows or shrinks while it is read would otherwise get a
// size no installation can check its download against.
func sizeAndSum(path string) (int64, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, nil, err
	}
	return n, h.Sum(nil), nil
}

// readPrice reads the price beside a package, or nil where there is none.
// Strict JSON within a budget, as every input from outside
// (enc.Budget.Decode); the price itself is checked by
// store.Catalog.Validate, which is what a reader checks it with.
func readPrice(pkg string) (*store.Price, error) {
	b, err := os.ReadFile(strings.TrimSuffix(pkg, ".ngc") + PriceSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p store.Price
	if priceBudget.Decode(b, &p) != nil {
		return nil, ErrPack
	}
	return &p, nil
}

// lastSeq is the sequence number of the catalog already in dir, or 0 while
// there is none. It reads it the way an installation does: the signature
// against this key and the store's ID, then the contract's parser.
//
// A catalog that lies there and cannot be read this way is ErrSeq, not a 0:
// it says nothing about which sequence numbers readers have already seen,
// and going on would risk signing one of them again.
func lastSeq(dir string, pub ed25519.PublicKey, storeID string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(dir, CatalogFile))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	env, err := sign.ParseEnvelope(b, sign.MaxPayload)
	if err != nil {
		return 0, ErrSeq
	}
	c, err := store.VerifyCatalog(env, trust.Store{ID: storeID, Key: enc.Bytes(pub)}, 0)
	if err != nil {
		return 0, ErrSeq
	}
	return c.Seq, nil
}

// readSigningKey reads an Ed25519 seed of 32 bytes as base64url. Neither the
// seed nor the content of the file ever reaches an error or a message: a
// broken key file is ErrPack, the same refusal as any unusable input
// (measured: TestCatalogRefusesABrokenKeyFile, TestCatalogTellsNoSecret).
// The key reaches ngc-store as a file, never as an argument, so no process
// list carries it either.
func readSigningKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := enc.B64.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, ErrPack
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
