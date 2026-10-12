package appstore

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestSourcesListsTheTrustList: one row per store of the trust list, with the
// newest catalog seen and the time of the last success.
func TestSourcesListsTheTrustList(t *testing.T) {
	fx, v := newFixture(t)
	src, err := fx.St.Sources(ctx)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(src) != 1 {
		t.Fatalf("rows: got %d, want 1", len(src))
	}
	// Before any fetch: the store is listed, with nothing seen and no error.
	if src[0].ID != v.Store || src[0].URL != fx.HTTP.URL || src[0].Seq != 0 ||
		src[0].Checked != 0 || src[0].Error != "" {
		t.Fatalf("before the fetch: %+v", src[0])
	}
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	src, err = fx.St.Sources(ctx)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if src[0].Seq != v.CatalogLastSeq || src[0].Checked != testNow || src[0].Error != "" {
		t.Fatalf("after the fetch: %+v", src[0])
	}
}

// TestTheReasonIsGermanAndBounded: the text a store earns itself is written by
// this package, never by the store, and it passes wire.ValidText.
func TestTheReasonIsGermanAndBounded(t *testing.T) {
	fx, v := newFixture(t)
	fx.setFile(CatalogFile, v.reject(t, "signed by another key").bytes())
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	src, err := fx.St.Sources(ctx)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if !wire.ValidText(src[0].Error, maxReason) {
		t.Fatalf("the reason is no valid text: %q", src[0].Error)
	}
	// Every reason this package writes is one of a closed list, so no store
	// can put its own words in front of an administrator.
	if !slices.Contains(reasons, src[0].Error) {
		t.Fatalf("the reason is not one of the closed list: %q", src[0].Error)
	}
}

// TestASettingTheStoreWroteIsNotShown: the row that holds the reason is in the
// same database as everything else, so it is read back through the same
// grammar; a row outside it is dropped, not shown.
func TestASettingTheStoreWroteIsNotShown(t *testing.T) {
	fx, v := newFixture(t)
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: SettingStore + v.Store,
		Data: []byte("{\"error\":\"Bitte\\nhier klicken\",\"checked\":1}"), Updated: testNow}); err != nil {
		t.Fatalf("BlobPut: %v", err)
	}
	src, err := fx.St.Sources(ctx)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	// What the row claimed is not shown; the text on screen is this
	// package's own for a row it could not read.
	bad := src[0].Error
	if strings.Contains(bad, "klicken") || strings.ContainsAny(bad, "\n\r") {
		t.Fatalf("the row's own text was shown: %q", bad)
	}
	if bad != "" && !slices.Contains(reasons, bad) {
		t.Fatalf("a text outside the closed list was shown: %q", bad)
	}
}

// TestCatalogOfAnUnknownStore: only a store of the trust list has a catalog,
// and only one that was fetched.
func TestCatalogOfAnUnknownStore(t *testing.T) {
	fx, v := newFixture(t)
	for _, id := range []string{"other", "", "../catalog", "global/x"} {
		if _, _, err := fx.St.Catalog(ctx, id); err == nil {
			t.Fatalf("Catalog(%q) answered", id)
		}
	}
	if _, _, err := fx.St.Catalog(ctx, v.Store); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("before the fetch: got %v, want dbapi.ErrNotFound", err)
	}
}
