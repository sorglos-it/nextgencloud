package appstore

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// FuzzCatalogAnswer feeds whatever a store may answer into the whole intake:
// the size limit, sign.ParseEnvelope and store.VerifyCatalog. The one thing it
// pins is that nothing but the real catalog is ever kept - a crash or a stored
// catalog under forged bytes is a finding either way.
func FuzzCatalogAnswer(f *testing.F) {
	f.Add([]byte("{}"))
	f.Add([]byte(""))
	f.Add([]byte(`{"payload":"AA","kid":"AAAAAAAAAAAAAAAAAAAAAA","sig":"AA"}`))
	v := loadVectors(&testing.T{})
	f.Add(v.Catalog.bytes())
	for _, r := range v.CatalogRejects {
		f.Add(r.Catalog.bytes())
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		fx, v := newFixture(t)
		fx.setFile(CatalogFile, body)
		err := fx.St.refreshOne(ctx, fx.theStore(t))
		kept := fx.keptSeq(t, v.Store)
		if err == nil && kept != v.CatalogLastSeq {
			t.Fatalf("taken with Seq %d", kept)
		}
		if err != nil && kept != 0 {
			t.Fatalf("refused with %v, yet Seq %d was kept", err, kept)
		}
		if err == nil {
			if _, _, cerr := fx.St.Catalog(ctx, v.Store); cerr != nil {
				t.Fatalf("taken, yet Catalog says %v", cerr)
			}
		} else if _, gerr := fx.S.DB.BlobGet(ctx, dbapi.BlobCatalog, v.Store); gerr == nil {
			t.Fatal("refused, yet a catalog row exists")
		}
	})
}

// FuzzReleaseAnswer does the same for the release file, whose key comes from
// the configuration and not from the trust list.
func FuzzReleaseAnswer(f *testing.F) {
	f.Add([]byte("{}"))
	v := loadVectors(&testing.T{})
	f.Add(v.Release.bytes())
	f.Add(v.Catalog.bytes())
	f.Fuzz(func(t *testing.T, body []byte) {
		fx, v := newFixture(t)
		fx.setFile(ReleaseFile, body)
		r, _, err := fx.St.Release(ctx)
		if err == nil && r.Seq != v.ReleaseLastSeq {
			t.Fatalf("taken with Seq %d", r.Seq)
		}
		if err != nil && r != nil {
			t.Fatalf("refused with %v, yet a release came back", err)
		}
	})
}

// FuzzUpload feeds Upload with anything that might be a package: it must
// answer an error, never panic and never leave a ui-tar behind. The seeds are
// the three packages of packui.json, so the corpus starts at something that
// walks all the way through and the mutations have a shape to break.
func FuzzUpload(f *testing.F) {
	for _, c := range loadUploadVectors(&testing.T{}) {
		f.Add([]byte(c.Package))
	}
	f.Add([]byte{})
	f.Add(make([]byte, 1024)) // an empty archive: the end marker alone
	f.Fuzz(func(t *testing.T, pkg []byte) {
		st, s := newStores(t)
		res, err := st.Upload(ctx, bytes.NewReader(pkg))
		blobs, ferr := s.DB.BlobFrom(ctx, dbapi.BlobUI, 0)
		if ferr != nil {
			t.Fatalf("BlobFrom: %v", ferr)
		}
		if err != nil {
			if len(blobs) != 0 {
				t.Fatalf("a refused package left %d ui-tars behind", len(blobs))
			}
			return
		}
		if len(res.SHA256) != sha256.Size || len(res.UI) != sha256.Size {
			t.Fatalf("result %+v", res)
		}
		// What came back has to be what was kept, under the ID of the
		// manifest: a result naming one version while another one's bytes
		// lie in the database is what the shell cannot see.
		if len(blobs) != 1 || blobs[0].ID != res.ID+"@"+res.Version {
			t.Fatalf("%d ui-tars, first %q, result %q", len(blobs), blobs[0].ID, res.ID+"@"+res.Version)
		}
		sum := sha256.Sum256(blobs[0].Data)
		if !bytes.Equal(sum[:], res.UI) {
			t.Fatalf("the kept ui-tar is not the one the result names")
		}
	})
}
