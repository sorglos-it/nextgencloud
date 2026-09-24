package login

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func TestTrustNeedsASessionAndPages(t *testing.T) {
	h, fx := newFixture(t)
	w := httptest.NewRecorder()
	h.Trust(w, httptest.NewRequest(http.MethodGet, session.PathTrust+"?from=1", nil))
	if w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
	cookie := loginOnce(t, h, fx)

	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=1", cookie))
	if w.Code != 200 {
		t.Fatalf("from=1: %d", w.Code)
	}
	var chain session.TrustChain
	if err := json.Unmarshal(w.Body.Bytes(), &chain); err != nil || len(chain.Versions) != 1 {
		t.Fatalf("chain: %v %d", err, len(chain.Versions))
	}
	if _, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain.Versions[0])}, chain.Versions); err != nil {
		t.Fatalf("chain does not verify: %v", err)
	}

	// One past the newest is 204, anything beyond is 404.
	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=2", cookie))
	if w.Code != 204 {
		t.Fatalf("from=2: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=3", cookie))
	if w.Code != 404 {
		t.Fatalf("from=3: %d", w.Code)
	}
	// A from the contract refuses is a protocol error.
	for _, bad := range []string{"", "0", "01", "-1", "x", "9007199254740992"} {
		w = httptest.NewRecorder()
		h.Trust(w, getCookie(session.PathTrust+"?from="+bad, cookie))
		if w.Code != 400 {
			t.Fatalf("from=%q answered %d", bad, w.Code)
		}
	}

	// A new version shows up at once.
	second := fx.Passkey.Sign(t, trusttest.Next(t, fx.Signed))
	if _, err := h.S.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=2", cookie))
	if w.Code != 200 {
		t.Fatalf("new version: %d", w.Code)
	}
}

// TestTrustLooksAtTheListOnce holds this path to one look at the trust rows.
// Live brings the list up to date before it decides on the session, and the
// answer is built from that same version: fetching it a second time inside
// state.Chain would walk the rows above the newest a second time, and it would
// answer out of a list the session was never checked against.
//
// What that second walk costs is not the stored versions read again -- a
// refresh that is up to date reads none of them. Measured on this fixture,
// which holds one version: nine reads, rows 2 to 10, every one of them a row
// that is not there (state.gapProbes). Nine reads that find nothing are what
// the trust rows cost per look here, and paying them twice was the whole of
// it.
//
// One look is counted rather than written down: what it costs is state's
// business (gapProbes), and what this test is about is that it happens once.
func TestTrustLooksAtTheListOnce(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	c := &trustReads{Store: h.S.DB}
	h.S.DB = c
	if _, _, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); !ok {
		t.Fatal("the session was refused")
	}
	once := c.take()
	if once == 0 {
		t.Fatal("Live read no row of the trust list at all")
	}
	w := httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=2", cookie))
	if w.Code != 204 {
		t.Fatalf("from=2: %d", w.Code)
	}
	if got := c.take(); got != once {
		t.Fatalf("the nothing-new cost %d reads, want the %d of one look", got, once)
	}
	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=1", cookie))
	if w.Code != 200 {
		t.Fatalf("from=1: %d", w.Code)
	}
	// On top of the one look comes the one row the answer itself carries.
	if got := c.take(); got != once+1 {
		t.Fatalf("the chain cost %d reads, want %d", got, once+1)
	}
}
