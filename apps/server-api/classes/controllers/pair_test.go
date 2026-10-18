package controllers

import (
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

func TestPairCodeMatchesTheTrustList(t *testing.T) {
	srv, fx := newServer(t)
	code, expires, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if expires <= testNow || expires > testNow+tunnel.PairLifetime {
		t.Fatalf("expires %d", expires)
	}
	// The shell shows a code only when it fits its newest verified version.
	if err := code.Check(fx.Version); err != nil {
		t.Fatalf("code does not fit the trust list: %v", err)
	}
	parsed, err := tunnel.ParsePairCode(code.String())
	if err != nil || parsed.URL != srv.S.Cfg.URL {
		t.Fatalf("parse: %v %+v", err, parsed)
	}
}

func TestPairAcceptsOnceAndListsThePending(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(20)
	body := pairBody(t, code, ctrl.Public, code.Secret)
	w := httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), body))
	if w.Code != 204 {
		t.Fatalf("pair %d %s", w.Code, w.Body)
	}
	list, err := srv.S.DB.PendingList(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "services-1" {
		t.Fatalf("pending %v: %v", list, err)
	}
	// The secret holds exactly once.
	w = httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), body))
	if w.Code != 403 {
		t.Fatalf("second pairing answered %d", w.Code)
	}
}

func TestPairRefusesAForeignMAC(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(20)
	body := pairBody(t, code, ctrl.Public, []byte("wrong secret!!!!"))
	w := httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), body))
	if w.Code != 403 {
		t.Fatalf("status %d", w.Code)
	}
	if list, _ := srv.S.DB.PendingList(ctx); len(list) != 0 {
		t.Fatalf("a foreign request was kept: %v", list)
	}
}

// TestPairNeedsTheHashOfASecret holds the query: without it, or with something
// that is no hash of this API's, nothing is listed.
func TestPairNeedsTheHashOfASecret(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(20)
	body := pairBody(t, code, ctrl.Public, code.Secret)
	other := sha256.Sum256([]byte("a secret this API never made"))
	for _, c := range []struct {
		name, query string
		want        int
	}{
		{"no query at all", "", 400},
		{"not base64url", "???", 400},
		{"the right length but no secret of ours", enc.B64.EncodeToString(other[:]), 403},
		{"base64url of too few bytes", enc.B64.EncodeToString([]byte("short")), 400},
	} {
		w := httptest.NewRecorder()
		srv.Pair(w, post(tunnel.PathPair+"?secret="+c.query, body))
		if w.Code != c.want {
			t.Errorf("%s answered %d, want %d", c.name, w.Code, c.want)
		}
	}
	if list, _ := srv.S.DB.PendingList(ctx); len(list) != 0 {
		t.Fatalf("something was listed without a secret: %v", list)
	}
	// And the secret itself was not spent by any of them, so the real request
	// still works: a refusal before the one-time value must cost nothing.
	w := httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), body))
	if w.Code != 204 {
		t.Fatalf("the real request answered %d %s", w.Code, w.Body)
	}
}

// TestPairSpendsTheSecretBeforeTheMAC holds the order of the handler, which is
// a decision and not an accident: a wrong MAC costs the attempt, so whoever
// holds the hash of a code can burn it but cannot guess at the MAC for a day.
func TestPairSpendsTheSecretBeforeTheMAC(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(20)
	w := httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), pairBody(t, code, ctrl.Public, []byte("wrong secret!!!!"))))
	if w.Code != 403 {
		t.Fatalf("the wrong MAC answered %d", w.Code)
	}
	// The right request now finds nothing left to spend.
	w = httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), pairBody(t, code, ctrl.Public, code.Secret)))
	if w.Code != 403 {
		t.Fatalf("the secret survived a wrong MAC: %d", w.Code)
	}
	if list, _ := srv.S.DB.PendingList(ctx); len(list) != 0 {
		t.Fatalf("pending %v", list)
	}
}

// pairPath is the path a controller calls: the hash of the secret in the query.
func pairPath(code tunnel.PairCode) string {
	h := sha256.Sum256(code.Secret)
	return tunnel.PathPair + "?secret=" + enc.B64.EncodeToString(h[:])
}

// pairBody is a pairing request for a controller key, with the MAC made from
// the secret handed in -- the right one or another.
func pairBody(t *testing.T, code tunnel.PairCode, ctrlKey, secret []byte) string {
	t.Helper()
	req := tunnel.PairRequest{Name: "services-1", Noise: enc.Bytes(ctrlKey),
		MAC: enc.Bytes(tunnel.PairMAC(secret, ctrlKey, code.Key))}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

var _ = dbapi.Pending{}
var _ = trust.Version{}
