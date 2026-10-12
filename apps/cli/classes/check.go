package classes

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// StoreKeyFile is the public half of the store's signing key in a store
// directory, as a PEM "PUBLIC KEY" — the same shape as ReleaseKeyFile, and
// read with the same function. The secret seed is never in the directory a
// store is served from; it lies wherever its owner keeps it and reaches
// ngc-store as a file of its own (Catalog, License).
const StoreKeyFile = "store-key.pem"

// CheckStore judges a whole store directory: the signed catalog in
// CatalogFile against the key in StoreKeyFile, and then the catalog against
// the files that lie there. It returns nil for a store that holds, else an
// error whose text is the reason; Run prints "store ok" or
// "store wrong: <reason>".
//
// This is the tool with which whoever runs a store notices that it is
// broken BEFORE an installation notices. An installation that downloads
// from a broken store sees one of two things: a catalog its API refuses, or
// a package whose checksum does not match — and in the second case it has
// already spent the bandwidth. Every reason below is a defect that an
// installation would hit later, named here while it is still cheap.
//
// storeID is the ID the catalog must carry (trust.Store.ID). It is a
// parameter and not read from the catalog on purpose: a catalog that claims
// another store's ID is a defect, and a check that took the ID from the
// document it is checking could never see it.
//
// lastSeq is the newest sequence number readers have already seen; 0 while
// that is unknown. A catalog below it is the rollback every reader refuses
// (store.ErrRollback).
//
// CheckStore writes nothing into dir.
func CheckStore(dir, storeID string, lastSeq uint64) error {
	keyPEM, err := os.ReadFile(filepath.Join(dir, StoreKeyFile))
	if err != nil {
		return fmt.Errorf("store key unreadable: %s", StoreKeyFile)
	}
	key, err := publicKeyPEM(keyPEM)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, CatalogFile))
	if err != nil {
		return fmt.Errorf("catalog unreadable: %s", CatalogFile)
	}
	env, err := sign.ParseEnvelope(b, sign.MaxPayload)
	if err != nil {
		return errors.New("no payload, or payload not base64url, or signature not base64url")
	}
	c, err := verifiedCatalog(env, key, storeID, lastSeq)
	if err != nil {
		return err
	}
	listed := map[string]bool{}
	for _, e := range c.Apps {
		for _, p := range e.Versions {
			name, err := checkVersion(dir, e, p)
			if err != nil {
				return fmt.Errorf("%s %s: %w", e.ID, p.Version, err)
			}
			listed[strings.TrimSuffix(name, EncSuffix)] = true
		}
	}
	return checkUnlisted(dir, listed)
}

// verifiedCatalog is the catalog of env, checked the way an installation
// checks it (store.VerifyCatalog), with the three ways it can fail told
// apart so that each gets its own reason.
//
// The signature is asked first and on its own (sign.Verify), because a
// catalog signed with another key says nothing at all and every later
// question would be about a document nobody wrote. Only after that does the
// difference between "no valid catalog" and "of another store" exist, and it
// is found by asking the contract a second time with the ID the document
// itself claims: if that passes, the ID was the whole difference.
//
// That second reading is for the message alone. The decision is always the
// first call with the ID the caller named; nothing that only the second call
// accepts is ever accepted.
func verifiedCatalog(env sign.Envelope, key ed25519.PublicKey, storeID string, lastSeq uint64) (*store.Catalog, error) {
	if err := sign.Verify(env, key, sign.PurposeCatalog); err != nil {
		return nil, errors.New("catalog does not verify")
	}
	st := trust.Store{ID: storeID, Key: enc.Bytes(key)}
	c, err := store.VerifyCatalog(env, st, lastSeq)
	if err == nil {
		return c, nil
	}
	if errors.Is(err, store.ErrRollback) {
		return nil, fmt.Errorf("catalog rolled back: below the sequence number %d already seen", lastSeq)
	}
	// encoding/json and not enc.Budget: this reads one string out of a
	// payload that is already refused and already bounded in length by
	// sign.ParseEnvelope, to put a name in a message. A strict decoder
	// would be the right tool for an input that may be accepted; nothing
	// here can be accepted.
	var claim struct {
		Store string `json:"store"`
	}
	if json.Unmarshal(env.Payload, &claim) == nil && claim.Store != storeID {
		st.ID = claim.Store
		if _, err := store.VerifyCatalog(env, st, lastSeq); err == nil {
			return nil, fmt.Errorf("catalog of another store: %q, not %q", claim.Store, storeID)
		}
	}
	return nil, errors.New("catalog is no valid catalog")
}

// checkVersion judges one version of one app in the catalog against the
// files in dir and returns the file name the entry's address gives.
//
// The order of the questions is the order of the answers: a name that is no
// name, then the encrypted form, then the file itself with its checksum and
// size (checkFile, the same function Check uses so that a store and a
// release are judged by one rule), then the plain package behind the ui
// hash, then the price file. An order is what tells reasons apart; a single
// exit status would not (docs: messen.md).
func checkVersion(dir string, e store.Entry, p store.Package) (string, error) {
	name, err := nameOf(p.URL)
	if err != nil {
		return "", err
	}
	plain := strings.TrimSuffix(name, EncSuffix)
	if p.Encrypted != (name != plain) {
		if p.Encrypted {
			return name, fmt.Errorf("entry says encrypted but names a plain package: %s", name)
		}
		return name, fmt.Errorf("entry says free but names an encrypted package: %s", name)
	}
	// A paid app whose encrypted form is gone while the package it was made
	// from still lies there: checkFile would call it a missing file, which is
	// true but tells the owner nothing about what to do. The one thing to do
	// is encrypt it again.
	if p.Encrypted {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			if _, err := os.Stat(filepath.Join(dir, plain)); err == nil {
				return name, fmt.Errorf("encrypted version missing: %s", name)
			}
		}
	}
	if err := checkFile(dir, name, p.Size, p.SHA256); err != nil {
		return name, err
	}
	// The ui hash is taken over the plain package, before encryption
	// (store.PackageUI), so it can only be checked against that file. For a
	// free app it is the file that was just checked; for a paid one it is the
	// package beside the encrypted form, and a store that no longer has it
	// cannot sign its next catalog.
	if _, err := os.Stat(filepath.Join(dir, plain)); err != nil {
		return name, fmt.Errorf("plain package missing: %s", plain)
	}
	_, ui, err := packageUI(filepath.Join(dir, plain))
	if err != nil {
		return name, fmt.Errorf("plain package is no package: %s", plain)
	}
	if !equalSum(ui, p.UI) {
		return name, fmt.Errorf("ui hash wrong: %s", plain)
	}
	// A paid entry carries a price, and store.Catalog.Validate ties the two
	// together, so the catalog cannot be missing one. The file beside the
	// package can be: delete it and the catalog in the directory stays
	// valid, while the next one signed from this directory would carry no
	// price and be refused by every reader (Encrypted != (Price != nil)).
	// That is the defect this question finds.
	if e.Price != nil {
		priceFile := strings.TrimSuffix(plain, ".ngc") + PriceSuffix
		price, err := readPrice(filepath.Join(dir, plain))
		if err != nil {
			return name, fmt.Errorf("price unreadable: %s", priceFile)
		}
		if price == nil {
			return name, fmt.Errorf("price missing: %s", priceFile)
		}
	}
	return name, nil
}

// checkUnlisted finds the packages in dir the catalog does not name. Such a
// package is not a danger — no installation ever asks for it — but it is
// always a mistake: either the catalog was signed before it was packed, in
// which case nobody can install it, or it should not be there at all. The
// owner has to decide which, so it is a reason and not a hint.
func checkUnlisted(dir string, listed map[string]bool) error {
	names, err := filepath.Glob(filepath.Join(dir, "*.ngc"))
	if err != nil {
		return err
	}
	slices.Sort(names)
	for _, path := range names {
		if name := filepath.Base(path); !listed[name] {
			return fmt.Errorf("package not in the catalog: %s", name)
		}
	}
	return nil
}
