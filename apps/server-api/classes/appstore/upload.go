package appstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// Upload reads an app package an admin sent (ngc.store/upload): it walks it
// safely (store.Walk through store.PackageUI), keeps the canonical ui-tar and
// answers what it read. The shell computes the same values from the file it
// sent and refuses a mismatch, so nothing in this answer has to be trusted.
//
// The package is read once and in one pass: store.MaxPackage is 4 GiB, and
// neither the checksum nor the walk may ask for it as a whole. The checksum
// runs beside the walk (io.TeeReader), and what the walk leaves unread is
// drained afterwards -- it belongs to the file the shell hashed, so the two
// sums only agree if every byte went through this one. The ui-tar is held,
// because it is what is kept: store bounds the files that go into it at
// store.MaxUITotal (64 MiB), and its own headers at 512 bytes apiece.
//
// Nothing is written before the whole package has been read and its length is
// known, so a package that is refused leaves no row behind (FuzzUpload).
func (st *Stores) Upload(ctx context.Context, r io.Reader) (core.UploadResult, error) {
	max := st.uploadLimit()
	sum := sha256.New()
	// max+1 and not max: one byte over the bound is what tells it is over,
	// and nothing beyond that is read at all.
	count := &counted{r: io.TeeReader(io.LimitReader(r, max+1), sum)}
	var ui bytes.Buffer
	m, uiHash, err := store.PackageUI(count, &ui)
	if err != nil {
		return core.UploadResult{}, err
	}
	// Discarded, not hashed again: the TeeReader above already puts every
	// byte that is read here into sum.
	if _, err := io.Copy(io.Discard, count); err != nil {
		return core.UploadResult{}, ErrStore
	}
	if count.n > max {
		return core.UploadResult{}, ErrStore
	}
	// m.ID and m.Version come from store.ParseManifest (wire.ValidAppID,
	// store.ValidVersion), so neither can carry the "@" that separates them
	// in the row's ID.
	if err := st.KeepUI(ctx, m.ID, m.Version, ui.Bytes()); err != nil {
		return core.UploadResult{}, err
	}
	return core.UploadResult{ID: m.ID, Version: m.Version, SHA256: enc.Bytes(sum.Sum(nil)),
		UI: enc.Bytes(uiHash)}, nil
}

// uploadLimit is the length one upload may have. store.MaxPackage is the last
// word: MaxUpload exists so a test need not send 4 GiB, and a value above the
// contract's bound -- or none at all -- is not taken from it
// (TestTheUploadBoundIsTheContractsOwn).
func (st *Stores) uploadLimit() int64 {
	if st.MaxUpload <= 0 || st.MaxUpload > store.MaxPackage {
		return store.MaxPackage
	}
	return st.MaxUpload
}

// counted is a reader that remembers how much came through it. The length of
// an upload is not known in advance: there is no header to believe, and a
// Content-Length would be the uploader's word.
type counted struct {
	r io.Reader
	n int64
}

func (c *counted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
