package appstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// uiKey is the ID of the rows of one version: the ui-tar (dbapi.BlobUI) and
// the evidence it was installed with (dbapi.BlobApproval). Both are named the
// same way, because the shell asks for them together and must get the pair of
// one version.
func uiKey(app, version string) string { return app + "@" + version }

// KeepUI keeps the canonical ui-tar of one version; ngc.desktop/ui sends
// exactly these bytes, and the shell compares their SHA-256 against the proof
// of ngc.desktop/ui-proof. Nothing is formatted, trimmed or re-packed here:
// one byte changed and that comparison fails, which is the point.
func (st *Stores) KeepUI(ctx context.Context, app, version string, ui []byte) error {
	return st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobUI, ID: uiKey(app, version),
		Data: ui, Updated: st.S.Now()})
}

// KeepProof keeps the evidence a version was installed with
// (ngc.desktop/ui-proof): the catalog of its store for a store app, the
// activate task with the client certificate and the user's proof for an
// uploaded one (core.LocalStore).
//
// It is verified against the trust list before it is written, so a proof that
// does not hold never becomes a row at all. Proof checks it again on the way
// out, because what holds today holds only against today's list.
//
// The parser in between is not what refuses, as far as it was measured: with
// it taken out, no case of shared/testdata/uiproof.json changes its answer --
// core.VerifyUIProof checks the form a second time itself, and the bytes here
// come from a Go value and not from a wire, so no unknown field or duplicate
// can be among them. What it does hold is the other property: the row that is
// written is a row core.ParseUIProof reads back, which is what
// ngc.desktop/ui-proof needs of it, and that row outlives this call
// (TestKeepProofTakesOnlyWhatHolds reads every kept row back).
func (st *Stores) KeepProof(ctx context.Context, p core.UIProof) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	parsed, err := core.ParseUIProof(data)
	if err != nil {
		return err
	}
	v, err := st.S.Trust(ctx)
	if err != nil {
		return err
	}
	if _, err := core.VerifyUIProof(parsed, parsed.App, parsed.Version, v.List); err != nil {
		return err
	}
	return st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobApproval,
		ID: uiKey(parsed.App, parsed.Version), Data: data, Updated: st.S.Now()})
}

// Proof answers ngc.desktop/ui-proof: the evidence kept for one version, as
// far as it still holds.
//
// It is verified again here, against the trust list as it reads today. The row
// was verified when it was written, but a key or a store an admin has since
// withdrawn would otherwise go on vouching for a UI out of a database -- and a
// database is not where trust lives. The shell checks it a third time against
// its own copy of the list, so it never has to believe this API at all.
//
// Where the ui-tar of that version is here as well, the hash the proof vouches
// for must be the hash of those bytes: the two rows travel to the shell as a
// pair, and handing out evidence that does not fit them is handing out a
// mismatch the shell can only refuse. A version whose ui-tar is not here --
// before it is written, or where it is served from elsewhere -- keeps its
// proof.
func (st *Stores) Proof(ctx context.Context, app, version string) (core.UIProof, error) {
	b, err := st.S.DB.BlobGet(ctx, dbapi.BlobApproval, uiKey(app, version))
	if err != nil {
		return core.UIProof{}, err
	}
	p, err := core.ParseUIProof(b.Data)
	if err != nil {
		return core.UIProof{}, err
	}
	v, err := st.S.Trust(ctx)
	if err != nil {
		return core.UIProof{}, err
	}
	ui, err := core.VerifyUIProof(p, app, version, v.List)
	if err != nil {
		return core.UIProof{}, err
	}
	kept, err := st.S.DB.BlobGet(ctx, dbapi.BlobUI, uiKey(app, version))
	if errors.Is(err, dbapi.ErrNotFound) {
		return p, nil
	}
	if err != nil {
		return core.UIProof{}, err
	}
	// hmac.Equal and not bytes.Equal: the comparison takes the same time
	// whatever the bytes are, as everywhere else a hash is compared here.
	sum := sha256.Sum256(kept.Data)
	if !hmac.Equal(sum[:], ui) {
		return core.UIProof{}, core.ErrUIProof
	}
	return p, nil
}
