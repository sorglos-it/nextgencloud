package classes

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// Errors of signing a licence. Like the key errors in crypt.go they are
// sentinels, so no message can carry a seed, a private key or a content key.
var (
	// ErrSignKey reports a file that is no Ed25519 seed of 32 bytes as base64url.
	ErrSignKey = errors.New("ngc-store: the file is no signing key")
	// ErrInstallKey reports an installation key that is not 32 bytes of
	// base64url. A key of the right shape that X25519 itself refuses — all
	// zeroes, for instance — comes back as store.ErrEncrypted from
	// store.WrapKey instead; both are refusals, and neither is this one.
	ErrInstallKey = errors.New("ngc-store: the installation key is no X25519 key")
)

// licenseIDSize is how many random bytes a licence ID carries when the
// caller names none: 16 bytes, 22 characters of base64url, within the 64 the
// contract allows (store.License.Validate).
const licenseIDSize = 16

// LicenseOpts are the parts of a licence that are not the app, the
// installation or a key. Issued must be set: nothing here reads the clock,
// so the same options give the same licence apart from the random ID and the
// random one-time key of the wrapping. Who signs reads the time — in
// ngc-store that is Run.
type LicenseOpts struct {
	Per     string // store.PerSite (the default when empty) or store.PerUser
	Users   int    // per user only: how many users may get the right "use"
	Issued  int64  // Unix seconds, must be > 0
	Expires int64  // subscription; 0 for a bought app
	Grace   int64  // 0 means store.GraceDefault
	Store   string // the store's ID; empty is refused, see below
	ID      string // empty draws a random one
}

// License signs a licence for one installation of one app, for a test store.
//
// keyFile holds the store's Ed25519 seed, contentKeyFile the content key of
// the app (the one Encrypt used), and installKey the X25519 public key of
// the installation as base64url — a public key, so it may stand on the
// command line; the two secrets are files, never arguments, because
// arguments stand in the process list for every other user to read.
//
// The content key is put into the licence with store.WrapKey, encrypted for
// installKey and bound to the app's ID: only that installation can open it
// (store.UnwrapKey, with its private key), and a licence for a cheap app
// cannot unlock an expensive one. The result is checked with
// store.License.Validate before it is signed, so nothing malformed ever
// carries a signature, and signed with sign.PurposeLicense, so it verifies
// as nothing else.
//
// An empty Store is refused by Validate instead of being filled in with
// store.GlobalID: defaulting to the global store would make a test store's
// licence claim to come from the one store every installation trusts out of
// the box.
func License(app, installation, keyFile, contentKeyFile, installKey string, opts LicenseOpts) (sign.Envelope, error) {
	priv, err := readStoreKey(keyFile)
	if err != nil {
		return sign.Envelope{}, err
	}
	defer clear(priv)
	contentKey, err := readContentKey(contentKeyFile)
	if err != nil {
		return sign.Envelope{}, err
	}
	defer clear(contentKey)
	pub, err := enc.B64.DecodeString(strings.TrimSpace(installKey))
	if err != nil || len(pub) != 32 {
		return sign.Envelope{}, ErrInstallKey
	}
	id := opts.ID
	if id == "" {
		raw := make([]byte, licenseIDSize)
		if _, err := rand.Read(raw); err != nil {
			return sign.Envelope{}, err
		}
		id = enc.B64.EncodeToString(raw)
	}
	per := opts.Per
	if per == "" {
		per = store.PerSite
	}
	wrapped, err := store.WrapKey(contentKey, pub, app, rand.Reader)
	if err != nil {
		return sign.Envelope{}, err
	}
	l := store.License{
		ID:           id,
		Store:        opts.Store,
		App:          app,
		Installation: installation,
		InstallKey:   enc.Bytes(pub),
		Per:          per,
		Users:        opts.Users,
		Issued:       opts.Issued,
		Expires:      opts.Expires,
		Grace:        opts.Grace,
		Key:          wrapped,
	}
	if err := l.Validate(); err != nil {
		return sign.Envelope{}, err
	}
	// encoding/json, not core.EncodeResult: a licence is a signed document,
	// not an answer of the API. store.License has no member that could
	// become null — the only pointer, Key, is non-nil after Validate.
	payload, err := json.Marshal(l)
	if err != nil {
		return sign.Envelope{}, err
	}
	return sign.Sign(priv, sign.PurposeLicense, payload)
}

// readStoreKey reads the Ed25519 seed of a store, 32 bytes as base64url, and
// returns the private key made from it.
//
// It is deliberately not the readSigningKey of Task 28's catalog.go (branch
// api-t28): two functions of the same name in one package do not compile,
// and that branch is not merged yet. Both read the same file format, and
// this one is the stricter of the two — it stops reading after maxKeyFile
// bytes, answers with a sentinel instead of the error of os.ReadFile or of
// the decoder, and overwrites the text it read. When both branches are in,
// the two should become one function, and it should be this one.
//
// Every failure is ErrSignKey: see readContentKey for why no error of the
// decoder is passed on.
func readStoreKey(path string) (ed25519.PrivateKey, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrKeyMissing
		}
		return nil, ErrSignKey
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxKeyFile+1))
	if err != nil || len(raw) > maxKeyFile {
		return nil, ErrSignKey
	}
	defer clear(raw)
	seed, err := enc.B64.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, ErrSignKey
	}
	defer clear(seed)
	return ed25519.NewKeyFromSeed(seed), nil
}

// NewStoreKey writes a new Ed25519 seed to path and returns the private key.
// It is the same file shape and the same care as NewContentKey: O_EXCL and
// 0600, no overwriting, nothing printed.
func NewStoreKey(path string) (ed25519.PrivateKey, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	defer clear(seed)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrKeyExists
		}
		return nil, err
	}
	if _, err := io.WriteString(f, enc.B64.EncodeToString(seed)+"\n"); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
