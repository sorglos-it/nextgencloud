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
