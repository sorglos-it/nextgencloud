package classes

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// ReleaseFile and ReleaseKeyFile are the two names a release directory
// carries beside the downloaded files, the same ones
// shared/testdata/release/verify.sh reads: the signed release as a
// sign.Envelope, and the release key as a PEM "PUBLIC KEY". Check and the
// script must look at the same directory, so the names are one constant
// each and no literal.
const (
	ReleaseFile    = "release.json"
	ReleaseKeyFile = "release-key.pem"
)

// ErrNoReleaseFile reports a directory that holds no file Release can name a
// part and an architecture for. The message names the file names it looked
// for: whoever builds their first release had to read releaseParts to learn
// them, and a directory with one wrong name looks exactly like an empty one.
var ErrNoReleaseFile = errors.New("ngc-store: the directory holds no release file; the names are " +
	strings.Join(ReleaseFileNames(), ", "))

// ErrVersion reports a version that is not the contract's
// (store.ValidVersion): MAJOR.MINOR.PATCH, three numbers without leading
// zeros. A four-digit number of this house's own numbering ("0.0.1.0") is
// the one that keeps arriving here.
var ErrVersion = errors.New("ngc-store: the version must be MAJOR.MINOR.PATCH, three numbers " +
	"without leading zeros (for example 1.1.0)")

// releaseExt is the extension of a release file. verify.sh checks a file by
// the name the signed release gives it, so the extension matters only here,
// where Release decides which files of a directory it lists.
const releaseExt = ".tar"

// releaseParts are the files a release lists, in the order they are listed.
// The order is fixed here and not read from the directory: a release's
// files are a known, closed set (store.Release.Validate allows no other
// part and no other architecture), and a fixed order makes two runs over
// the same files give the same payload.
var releaseParts = []struct{ part, arch string }{
	{store.ReleaseWeb, "all"},
	{store.ReleaseAPI, "amd64"},
	{store.ReleaseAPI, "arm64"},
	{store.ReleaseController, "amd64"},
	{store.ReleaseController, "arm64"},
}

// ReleaseOpts describe the release to sign.
type ReleaseOpts struct {
	Version string // store.ValidVersion
	Seq     uint64 // higher than the last one in the directory, else ErrSeq
	Issued  int64  // Unix seconds, must be > 0
	Notes   string // what changed, shown to admins; may be empty
	BaseURL string // where the files lie; empty writes names beside release.json
}

// Release reads the files of dir, names each one its part and architecture
// after its file name ("ngc-web-all.tar" is store.ReleaseWeb for "all",
// "ngc-api-amd64.tar" is store.ReleaseAPI for "amd64") and returns a signed
// release. A file name that names no part is ignored, so a directory may
// hold a catalog, a key or notes beside the files; a directory that names
// none at all is ErrNoReleaseFile.
//
// Release builds no character grammar for addresses, for the same reason
// Catalog builds none (Punkt 25 of docs/vertraege-v1.2.md): the address it
// composes from BaseURL is judged by store.Release.Validate, the rule every
// reader applies through store.VerifyRelease, so an address this tool
// writes is one an installation fetches. Its one rule of its own is the one
// Catalog has, and it is about the shape of the base and not about its
// characters: a base that already carries a query gets no file name
// appended, because "…?token=x/ngc-web-all.tar" names no file on any
// server.
//
// Nothing of a file's checksum depends on when it was built: the size and
// the SHA-256 listed here are those of the bytes on the disk, read in one
// pass (sizeAndSum). The release itself is not byte-identical between two
// runs and must not be: it carries seq and issued.
//
// Release writes no file. Whatever it refuses leaves nothing behind,
// because the caller only ever sees an envelope or an error; the subcommand
// in Run writes the envelope, and only after Release returned one.
func Release(dir, keyFile string, opts ReleaseOpts) (sign.Envelope, error) {
	priv, err := readStoreKey(keyFile)
	if err != nil {
		return sign.Envelope{}, err
	}
	defer clear(priv)
	pub := priv.Public().(ed25519.PublicKey)
	// Before anything is read: a sequence number that does not rise is the
	// rollback a reader refuses (store.ErrRollback), and the only one who
	// can still fix it is the person at the keyboard. This is the stricter
	// rule of Task 28 — the contract still accepts an equal number, writing
	// one again is a mistake — and it is kept the same here on purpose, so
	// that one tool does not have two ideas of what a sequence number is.
	// The version is asked here and not left to r.Validate at the end: the
	// contract answers every defect of a release with one sentinel, and a
	// run that said "store: invalid release" to a version of four digits
	// cost time on 16.10.2026. The rule itself stays the contract's
	// (store.ValidVersion); this only says which rule was broken.
	if !store.ValidVersion(opts.Version) {
		return sign.Envelope{}, fmt.Errorf("%w: %q", ErrVersion, opts.Version)
	}
	last, err := lastReleaseSeq(dir, pub)
	if err != nil {
		return sign.Envelope{}, err
	}
	if opts.Seq <= last {
		return sign.Envelope{}, ErrSeq
	}
	files := make([]store.File, 0, len(releaseParts))
	for _, p := range releaseParts {
		name := p.part + "-" + p.arch + releaseExt
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return sign.Envelope{}, err
		}
		size, sum, err := sizeAndSum(path)
		if err != nil {
			return sign.Envelope{}, err
		}
		url, err := downloadURL(name, opts.BaseURL)
		if err != nil {
			return sign.Envelope{}, err
		}
		files = append(files, store.File{Part: p.part, Arch: p.arch, URL: url, Size: size, SHA256: enc.Bytes(sum)})
	}
	if len(files) == 0 {
		return sign.Envelope{}, ErrNoReleaseFile
	}
	r := store.Release{Seq: opts.Seq, Issued: opts.Issued, Version: opts.Version, Notes: opts.Notes, Files: files}
	if err := r.Validate(); err != nil {
		return sign.Envelope{}, err
	}
	// encoding/json, not core.EncodeResult: a release is no answer of the
	// API. store.Release has no field that could become null — Notes and the
	// lists that may be empty carry omitempty — and the round trip through
	// store.VerifyRelease is measured (TestReleaseVerifies).
	payload, err := json.Marshal(r)
	if err != nil {
		return sign.Envelope{}, err
	}
	return sign.Sign(priv, sign.PurposeRelease, payload)
}

// downloadURL is the address of name under base: the name alone when base is
// empty (a path relative to release.json, store.ValidPath), else base and
// the name. See Release on why there is no character rule here.
func downloadURL(name, base string) (string, error) {
	if base == "" {
		return name, nil
	}
	if strings.Contains(base, "?") {
		return "", ErrPack
	}
	return strings.TrimSuffix(base, "/") + "/" + name, nil
}

// lastReleaseSeq is the sequence number of the release already in dir, or 0
// while there is none. It reads it the way an installation does: the
// signature against this key, then the contract's parser.
//
// A release that lies there and cannot be read this way is ErrSeq, not a 0:
// it says nothing about which sequence numbers readers have already seen,
// and going on would risk signing one of them again.
func lastReleaseSeq(dir string, pub ed25519.PublicKey) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(dir, ReleaseFile))
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
	r, err := store.VerifyRelease(env, pub, 0)
	if err != nil {
		return 0, ErrSeq
	}
	return r.Seq, nil
}

// downloadName is the name a signed address gives the file beside
// release.json or catalog.json: the last segment of the address. It must
// start with a letter or a digit and hold only letters, digits and . _ -,
// the rule shared/testdata/release/verify.sh applies with the tools of a
// shell.
//
// This is not a second grammar for addresses — the address itself is judged
// by the contract (store.Release.Validate, store.Catalog.Validate) before
// anything here looks at it, and Punkt 25 of docs/vertraege-v1.2.md says
// why no caller outside shared/ writes one of those. It is the rule for a
// file name in the directory, and it is narrower than the address: it keeps
// out the names that would be read as an option ("-x", "--help"), the ones
// a shell or a directory listing hides ("."), and the empty name an address
// ending in "/" leaves.
var downloadName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// nameOf is the file name of url, or an error naming the reason the way
// verify.sh names it.
func nameOf(url string) (string, error) {
	name := url
	if i := strings.LastIndexByte(url, '/'); i >= 0 {
		name = url[i+1:]
	}
	if !downloadName.MatchString(name) {
		return "", fmt.Errorf("file name not allowed: %s", name)
	}
	return name, nil
}

// Check judges the file of one part and one architecture in the release
// directory dir the way shared/testdata/release/verify.sh judges it, so the
// shell script of a Linux server and this tool say the same thing about the
// same directory. It returns nil for a release that holds, else an error
// whose text is the reason — Run prints "release ok" or
// "release wrong: <reason>", the two lines the script prints.
//
// What it checks, in this order, because an order of questions tells the
// reasons apart where a single exit status cannot: the signed release
// against the key in ReleaseKeyFile, then exactly one entry for part and
// arch, then the file name the entry's address gives, then that the file
// lies there, then its SHA-256, then its size.
//
// Only this one file is checked and only it may be installed afterwards:
// files of other parts and files the release does not list are ignored,
// whatever lies in dir. Check writes nothing into dir.
//
// Check is stricter than the script in one place, and that is measured in
// TestCheckMatchesTheScript: the whole release has to be a valid
// store.Release, not only the one entry asked for. The two agree on the
// verdict of all 31 cases of shared/store's TestReleaseScript and on the
// reason of 30 of them; the one that differs is an address whose last
// segment begins with a dot, which store.ValidPath already refuses, so the
// file name is never reached. Which way round that is, is not a matter of
// taste: a release that is not a valid release is refused whole.
func Check(dir, part, arch string) error {
	if part == "" || arch == "" {
		return errors.New("usage: a part and an architecture name the file to check")
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, ReleaseKeyFile))
	if err != nil {
		return fmt.Errorf("release key unreadable: %s", ReleaseKeyFile)
	}
	key, err := publicKeyPEM(keyPEM)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, ReleaseFile))
	if err != nil {
		return fmt.Errorf("release unreadable: %s", ReleaseFile)
	}
	env, err := sign.ParseEnvelope(b, sign.MaxPayload)
	if err != nil {
		// sign.ParseEnvelope answers one sentinel for a missing payload, a
		// payload that is no base64url and a signature that is no base64url
		// alike. The script tells those three apart. This reason names all
		// three instead of guessing which one it was — so it holds each of
		// the script's three sentences inside it, and a reader of either
		// output is told the same thing, the script more narrowly.
		return errors.New("no payload, or payload not base64url, or signature not base64url")
	}
	r, err := store.VerifyRelease(env, key, 0)
	if err != nil {
		if errors.Is(err, store.ErrRelease) {
			return errors.New("no single valid entry: the release is no valid release")
		}
		return errors.New("signature does not verify")
	}
	var entry *store.File
	for i := range r.Files {
		if r.Files[i].Part == part && r.Files[i].Arch == arch {
			if entry != nil {
				// store.Release.Validate already refuses two entries for one
				// part and architecture, so this is unreachable through
				// VerifyRelease. It stays because the reason belongs to the
				// question, not to the one function that happens to answer
				// it first today.
				return fmt.Errorf("no single valid entry for %s %s", part, arch)
			}
			entry = &r.Files[i]
		}
	}
	if entry == nil {
		// Three mistakes that looked the same from here and are not the
		// same for whoever made them: a part that is no part at all
		// ("web" for store.ReleaseWeb, measured on 17.10.2026 as the
		// reason an afternoon went), a part without this architecture, and
		// a part this release simply does not carry. The tool knows the
		// closed set (releaseParts) and the release it just read, so it
		// names both. The sentence keeps "no single valid entry for <part>
		// <arch>" in front, because that is the script's wording and what
		// TestCheckMatchesTheScript holds the two to.
		if !KnownPart(part) {
			return fmt.Errorf("no single valid entry for %s %s: %q is no part of a release — the parts are: %s",
				part, arch, part, PartList())
		}
		if _, ok := ReleaseFileName(part, arch); !ok {
			return fmt.Errorf("no single valid entry for %s %s: %s has no architecture %q — the parts are: %s",
				part, arch, part, arch, PartList())
		}
		return fmt.Errorf("no single valid entry for %s %s: this release lists %s",
			part, arch, listedParts(r))
	}
	name, err := nameOf(entry.URL)
	if err != nil {
		return err
	}
	return checkFile(dir, name, entry.Size, entry.SHA256)
}

// checkFile is the last three questions of Check and of CheckStore: the file
// lies there, its SHA-256 is sum, its size is size. The size comes from the
// bytes that were hashed, not from the directory entry, for the reason
// sizeAndSum gives.
//
// The order matters and is the script's: a missing file is named before a
// checksum, and the checksum before the size, so that a truncated download
// is reported as a wrong checksum and not as a wrong size — a wrong size is
// then a release that lists a size that does not belong to these bytes.
func checkFile(dir, name string, size int64, sum []byte) error {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("file missing: %s", name)
	}
	got, gotSum, err := sizeAndSum(path)
	if err != nil {
		return fmt.Errorf("file unreadable: %s", name)
	}
	if !equalSum(gotSum, sum) {
		return fmt.Errorf("checksum wrong: %s", name)
	}
	if got != size {
		return fmt.Errorf("size wrong: %s", name)
	}
	return nil
}

// equalSum compares two checksums. Neither is a secret — they are in a
// signed, public document — so there is nothing here to compare in constant
// time.
func equalSum(a, b []byte) bool {
	if len(a) != len(b) || len(a) != sha256.Size {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// publicKeyPEM reads an Ed25519 public key from a PEM "PUBLIC KEY" block,
// the file verify.sh hands to openssl. Nothing secret passes through here —
// a public key is public — so the error may name what was wrong with it.
func publicKeyPEM(b []byte) (ed25519.PublicKey, error) {
	block, rest := pem.Decode(b)
	if block == nil || block.Type != "PUBLIC KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("key is no PEM PUBLIC KEY block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("key is no public key")
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is no Ed25519 key but %T", pub)
	}
	return key, nil
}

// PublicKeyPEM is the counterpart: the public half of a signing key as the
// PEM file Check and verify.sh read. Only the public half ever reaches it —
// it takes an ed25519.PublicKey, so a seed cannot be passed in by mistake.
func PublicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}
