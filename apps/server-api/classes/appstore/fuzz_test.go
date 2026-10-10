package appstore

import (
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
