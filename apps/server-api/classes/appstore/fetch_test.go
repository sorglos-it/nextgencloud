package appstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// TestRefreshVerifiesAndKeeps is the open way every attack below has to walk:
// the fetch happens, the signature verifies against the key of the trust list
// and the envelope is kept under the catalog's own Seq.
func TestRefreshVerifiesAndKeeps(t *testing.T) {
	fx, v := newFixture(t)
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if fx.hits() == 0 {
		t.Fatal("the store saw no request: nothing was fetched")
	}
	if got := fx.keptSeq(t, v.Store); got != v.CatalogLastSeq {
		t.Fatalf("kept Seq: got %d, want %d", got, v.CatalogLastSeq)
	}
	b, err := fx.S.DB.BlobGet(ctx, dbapi.BlobCatalog, v.Store)
	if err != nil {
		t.Fatalf("BlobGet: %v", err)
	}
	var kept sign.Envelope
	if err := json.Unmarshal(b.Data, &kept); err != nil {
		t.Fatalf("kept envelope: %v", err)
	}
	if !bytes.Equal(kept.Payload, v.Catalog.envelope(t).Payload) {
		t.Fatal("the kept payload is not the one the store served")
	}
	// Catalog hands the stored envelope back, so the shell checks it itself.
	c, env, err := fx.St.Catalog(ctx, v.Store)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if c.Seq != v.CatalogLastSeq || !bytes.Equal(env.Payload, kept.Payload) {
		t.Fatalf("Catalog: seq %d, payload equal %v", c.Seq, bytes.Equal(env.Payload, kept.Payload))
	}
}

// TestRefuseAForeignSignature: the answer is well-formed and signed, only not
// with the key the trust list names for this store.
func TestRefuseAForeignSignature(t *testing.T) {
	fx, v := newFixture(t)
	fx.setFile(CatalogFile, v.reject(t, "signed by another key").bytes())
	before := fx.hits()
	err := fx.St.refreshOne(ctx, fx.theStore(t))
	if fx.hits() == before {
		t.Fatal("refused before the request: this measures an earlier wall")
	}
	if !errors.Is(err, sign.ErrWrongKey) {
		t.Fatalf("got %v, want sign.ErrWrongKey", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 0 {
		t.Fatalf("a catalog was kept all the same (Seq %d)", got)
	}
}

// TestRefuseABrokenSignature: the right key, the right purpose, a signature
// that does not hold.
func TestRefuseABrokenSignature(t *testing.T) {
	fx, v := newFixture(t)
	env := v.Catalog.envelope(t)
	env.Sig[0] ^= 0x01
	broken, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	fx.setFile(CatalogFile, broken)
	before := fx.hits()
	err = fx.St.refreshOne(ctx, fx.theStore(t))
	if fx.hits() == before {
		t.Fatal("refused before the request: this measures an earlier wall")
	}
	if !errors.Is(err, sign.ErrBadSignature) {
		t.Fatalf("got %v, want sign.ErrBadSignature", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 0 {
		t.Fatalf("a catalog was kept all the same (Seq %d)", got)
	}
}

// TestRefuseASignatureOfAnotherPurpose: the store's own key, over the same
// bytes, signed as a licence. sign.Message binds the purpose, so it must not
// pass as a catalog.
func TestRefuseASignatureOfAnotherPurpose(t *testing.T) {
	fx, v := newFixture(t)
	fx.setFile(CatalogFile, v.reject(t, "signed for purpose license").bytes())
	before := fx.hits()
	err := fx.St.refreshOne(ctx, fx.theStore(t))
	if fx.hits() == before {
		t.Fatal("refused before the request: this measures an earlier wall")
	}
	if !errors.Is(err, sign.ErrBadSignature) {
		t.Fatalf("got %v, want sign.ErrBadSignature", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 0 {
		t.Fatalf("a catalog was kept all the same (Seq %d)", got)
	}
}

// TestRefuseAnOlderCatalog is the rollback: the store's own valid, correctly
// signed catalog, served after a newer one was already kept. Only the Seq
// tells them apart, so this is the one case a signature check alone passes.
func TestRefuseAnOlderCatalog(t *testing.T) {
	fx, v := newFixture(t)
	// A newer catalog is on record first. The way to the Seq check is shown
	// open by the same envelope verifying while nothing newer is stored.
	if err := fx.St.refreshOne(ctx, fx.theStore(t)); err != nil {
		t.Fatalf("the newer catalog did not verify, so no Seq check is reached: %v", err)
	}
	newer := v.CatalogLastSeq + 1
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobCatalog, ID: v.Store, Seq: newer,
		Data: []byte(`{"payload":"AA","kid":"x","sig":"y"}`), Updated: testNow}); err != nil {
		t.Fatalf("BlobPut: %v", err)
	}
	before := fx.hits()
	err := fx.St.refreshOne(ctx, fx.theStore(t))
	if fx.hits() == before {
		t.Fatal("refused before the request: this measures an earlier wall")
	}
	if !errors.Is(err, store.ErrRollback) {
		t.Fatalf("got %v, want store.ErrRollback", err)
	}
	if got := fx.keptSeq(t, v.Store); got != newer {
		t.Fatalf("the older catalog replaced the newer one: Seq %d, want %d", got, newer)
	}
}

// TestRefreshKeepsTheLastGoodCatalog: a store that answers badly must not cost
// the installation the catalog it already has, and must not stop Refresh.
func TestRefreshKeepsTheLastGoodCatalog(t *testing.T) {
	fx, v := newFixture(t)
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	fx.setFile(CatalogFile, v.reject(t, "signed by another key").bytes())
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh over a bad store: got %v, want nil", err)
	}
	if got := fx.keptSeq(t, v.Store); got != v.CatalogLastSeq {
		t.Fatalf("kept Seq: got %d, want %d", got, v.CatalogLastSeq)
	}
	// The reason is on record for ngc.store/sources, in German.
	src, err := fx.St.Sources(ctx)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(src) != 1 || src[0].Error == "" {
		t.Fatalf("Sources: %+v", src)
	}
	if strings.ContainsAny(src[0].Error, "\n\r") {
		t.Fatalf("the reason holds a line break: %q", src[0].Error)
	}
}

// TestRefuseACatalogOfAnotherStore: correctly signed by this store's key, but
// it names another store, so its apps would be attributed to that one.
func TestRefuseACatalogOfAnotherStore(t *testing.T) {
	fx, v := newFixture(t)
	fx.setFile(CatalogFile, v.reject(t, "catalog of another store").bytes())
	before := fx.hits()
	err := fx.St.refreshOne(ctx, fx.theStore(t))
	if fx.hits() == before {
		t.Fatal("refused before the request: this measures an earlier wall")
	}
	if !errors.Is(err, store.ErrCatalog) {
		t.Fatalf("got %v, want store.ErrCatalog", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 0 {
		t.Fatalf("a catalog was kept all the same (Seq %d)", got)
	}
}

// wantEnvelope is the bound written out, not read off maxEnvelope: a case that
// sizes its answer by the constant it means to test grows with it and catches
// a limit that was merely moved (measured with the mutant "size-2mb": it
// passed every case until this number stood on its own).
const wantEnvelope = sign.MaxPayload/3*4 + 1<<10

// TestMaxEnvelopeIsTheBase64OfTheLargestPayload pins the bound itself, so
// moving it is a change somebody has to mean.
func TestMaxEnvelopeIsTheBase64OfTheLargestPayload(t *testing.T) {
	if maxEnvelope != int64(wantEnvelope) {
		t.Fatalf("maxEnvelope: got %d, want %d", maxEnvelope, wantEnvelope)
	}
}

// TestRefuseAnOversizedAnswer: more than an envelope may hold is cut off, and
// what is over the limit is never read into memory.
func TestRefuseAnOversizedAnswer(t *testing.T) {
	fx, v := newFixture(t)
	fx.setFile(CatalogFile, bytes.Repeat([]byte("A"), wantEnvelope+1<<20))
	before := fx.hits()
	err := fx.St.refreshOne(ctx, fx.theStore(t))
	if fx.hits() == before {
		t.Fatal("refused before the request: this measures an earlier wall")
	}
	if !errors.Is(err, ErrStore) {
		t.Fatalf("got %v, want ErrStore", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 0 {
		t.Fatalf("a catalog was kept all the same (Seq %d)", got)
	}
	// An answer of exactly the limit is read and only then refused as no
	// envelope, so the limit and not the fetch is what refused the one above.
	fx.setFile(CatalogFile, bytes.Repeat([]byte("A"), wantEnvelope))
	if err := fx.St.refreshOne(ctx, fx.theStore(t)); !errors.Is(err, sign.ErrEnvelope) {
		t.Fatalf("an answer of exactly maxEnvelope: got %v, want sign.ErrEnvelope", err)
	}
}

// TestRefuseAnAddressOutsideTheGrammar: a store whose URL no trust list would
// carry is not fetched at all. trust.Parse already refuses such a list, so
// this wall is the second one; it is measured on a trust.Store handed in
// directly, because nothing signed can reach the first.
func TestRefuseAnAddressOutsideTheGrammar(t *testing.T) {
	fx, v := newFixture(t)
	good := fx.theStore(t)
	// The way is shown open with the very same store value, changing only the
	// address.
	if err := fx.St.refreshOne(ctx, good); err != nil {
		t.Fatalf("the unchanged store does not verify, so no address check is reached: %v", err)
	}
	if err := fx.S.DB.BlobDelete(ctx, dbapi.BlobCatalog, v.Store); err != nil {
		t.Fatalf("BlobDelete: %v", err)
	}
	for _, bad := range []string{
		"http://" + strings.TrimPrefix(fx.HTTP.URL, "https://"),      // plain http
		fx.HTTP.URL + "/../etc",                                      // a step up
		fx.HTTP.URL + "/?x=1",                                        // a query
		fx.HTTP.URL + "/a#b",                                         // a fragment
		strings.Replace(fx.HTTP.URL, "https://", "https://user@", 1), // user info
		fx.HTTP.URL + "/",                                            // a trailing slash
		"https://127.0.0.1:0",                                        // port 0
		"file:///etc/passwd",                                         // another scheme
		"https://127.0.0.1:65536",                                    // no port
		"https://" + strings.ToUpper("Store.Ngc.Example"),            // upper case
	} {
		s := good
		s.URL = bad
		before := fx.hits()
		if err := fx.St.refreshOne(ctx, s); !errors.Is(err, ErrStore) {
			t.Fatalf("%q: got %v, want ErrStore", bad, err)
		}
		if fx.hits() != before {
			t.Fatalf("%q was fetched before it was refused", bad)
		}
		if got := fx.keptSeq(t, v.Store); got != 0 {
			t.Fatalf("%q: a catalog was kept (Seq %d)", bad, got)
		}
	}
	if !trust.ValidStoreURL(good.URL) {
		t.Fatalf("the fixture's own address is outside the grammar: %q", good.URL)
	}
}

// TestRefuseAStatusOtherThanOK: a store that answers 500 or redirects
// elsewhere has not served a catalog.
func TestRefuseAStatusOtherThanOK(t *testing.T) {
	fx, v := newFixture(t)
	for _, code := range []int{http.StatusFound, http.StatusInternalServerError, http.StatusNoContent} {
		fx.setStatus(code)
		if err := fx.St.refreshOne(ctx, fx.theStore(t)); !errors.Is(err, ErrStore) {
			t.Fatalf("status %d: got %v, want ErrStore", code, err)
		}
		if got := fx.keptSeq(t, v.Store); got != 0 {
			t.Fatalf("status %d: a catalog was kept (Seq %d)", code, got)
		}
	}
}

// TestTheClientFollowsNothing pins what New builds, not what a case overrides:
// a minute at most, no redirect, no proxy out of the environment.
func TestTheClientFollowsNothing(t *testing.T) {
	st := New(nil)
	if st.Client.Timeout.Seconds() != 60 {
		t.Fatalf("timeout: %v", st.Client.Timeout)
	}
	if st.Client.CheckRedirect == nil {
		t.Fatal("the client follows redirects")
	}
	if err := st.Client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect: %v", err)
	}
	tr, ok := st.Client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport: %T", st.Client.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("the client takes a proxy out of the environment")
	}
}

// TestRelease checks the release of the global store against the release key
// of the configuration, not against the store's key.
func TestRelease(t *testing.T) {
	fx, v := newFixture(t)
	r, _, err := fx.St.Release(ctx)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if r.Seq != v.ReleaseLastSeq {
		t.Fatalf("Seq: got %d, want %d", r.Seq, v.ReleaseLastSeq)
	}
	// A release signed with the store's key is not a release.
	fx.setFile(ReleaseFile, v.Catalog.bytes())
	if _, _, err := fx.St.Release(ctx); err == nil {
		t.Fatal("a catalog passed as a release")
	}
}

// TestRefuseAnOlderRelease is the rollback of the other document: a release
// older than the one on record must not put an older NGC back in reach.
func TestRefuseAnOlderRelease(t *testing.T) {
	fx, v := newFixture(t)
	if _, _, err := fx.St.Release(ctx); err != nil {
		t.Fatalf("the release does not verify, so no Seq check is reached: %v", err)
	}
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: SettingRelease,
		Seq: v.ReleaseLastSeq + 1, Data: []byte("{}"), Updated: testNow}); err != nil {
		t.Fatalf("BlobPut: %v", err)
	}
	if _, _, err := fx.St.Release(ctx); !errors.Is(err, store.ErrRollback) {
		t.Fatalf("got %v, want store.ErrRollback", err)
	}
}

// TestDownloadChecksSizeAndChecksum: longer, shorter or different is an error,
// and the caller is told before it uses what it wrote.
func TestDownloadChecksSizeAndChecksum(t *testing.T) {
	fx, _ := newFixture(t)
	body := []byte("ein Paket")
	sum := sha256.Sum256(body)
	fx.setFile("app.ngc", body)
	url := fx.HTTP.URL + "/app.ngc"

	var w bytes.Buffer
	if err := fx.St.Download(ctx, url, int64(len(body)), sum[:], &w); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(w.Bytes(), body) {
		t.Fatal("what was written is not what was served")
	}

	for _, c := range []struct {
		name string
		size int64
		sum  []byte
	}{
		{"shorter than announced", int64(len(body)) + 1, sum[:]},
		{"longer than announced", int64(len(body)) - 1, sum[:]},
		{"another checksum", int64(len(body)), bytes.Repeat([]byte{0x5a}, sha256.Size)},
		{"no size", 0, sum[:]},
		{"a checksum of 31 bytes", int64(len(body)), sum[:31]},
		{"larger than a package may be", store.MaxPackage + 1, sum[:]},
	} {
		w.Reset()
		if err := fx.St.Download(ctx, url, c.size, c.sum, &w); !errors.Is(err, ErrStore) {
			t.Fatalf("%s: got %v, want ErrStore", c.name, err)
		}
	}
	// An address outside the grammar is not fetched.
	before := fx.hits()
	w.Reset()
	if err := fx.St.Download(ctx, "http://127.0.0.1:1/x", int64(len(body)), sum[:], &w); !errors.Is(err, ErrStore) {
		t.Fatalf("plain http: got %v, want ErrStore", err)
	}
	if fx.hits() != before {
		t.Fatal("a plain http address was fetched")
	}
}
