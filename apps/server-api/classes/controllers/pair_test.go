package controllers

import (
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
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

// ---- the log of a pairing ---------------------------------------------------
//
// What has to be true of the code below, and what each case points its counter
// at (messen.md, "Eine Vorrichtungsprüfung, die nur der Mutant erfüllt"):
//
//   - An attempt that spent a code leaves EXACTLY ONE line, and that line says
//     which check said no -- not a code, a name or a hash.
//   - An attempt that spent NO code leaves NO line, because that one is
//     unbounded: see audit.Log.Pairing.
//
// Every case makes its own server, so no counter of one carries into another
// (messen.md: cases over one-time values must be separated).

// pairLog reads the whole log of srv, newest first.
func pairLog(t *testing.T, srv *Server) []core.LogEntry {
	t.Helper()
	got, err := srv.S.DB.LogQuery(ctx, core.LogQuery{Limit: 50})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	return got
}

func TestABurntPairingCodeLeavesOneLineThatNamesTheCheck(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	// The way to the line is open: before the attempt there is nothing.
	if got := pairLog(t, srv); len(got) != 0 {
		t.Fatalf("the log was not empty before the attempt: %+v", got)
	}
	ctrl, _ := trusttest.NodeKeys(21)
	w := httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), pairBody(t, code, ctrl.Public, []byte("wrong secret!!!!"))))
	if w.Code != 403 {
		t.Fatalf("pair answered %d", w.Code)
	}
	got := pairLog(t, srv)
	if len(got) != 1 {
		t.Fatalf("a burnt code left %d lines, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.Kind != core.LogTrust || e.Action != audit.ActionPair ||
		e.Result != string(wire.CodeVerify) || e.Detail != string(reason.PairProof) {
		t.Fatalf("line %+v", e)
	}
	// Nobody was signed in, so the machine owns the line -- and the name the
	// request carried ("services-1") is nowhere in it.
	if e.User != audit.UserSystem {
		t.Fatalf("user %q", e.User)
	}
	if e.Node != "" || e.App != "" {
		t.Fatalf("the line carries a value of the attempt: %+v", e)
	}
}

func TestAPairingThatGotThroughLeavesALineToo(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(22)
	w := httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), pairBody(t, code, ctrl.Public, code.Secret)))
	if w.Code != 204 {
		t.Fatalf("pair %d %s", w.Code, w.Body)
	}
	got := pairLog(t, srv)
	// Why the good case is counted at all: a log that holds only failures
	// cannot tell "none happened" from "nothing is written" -- which is the
	// very reading M3 had to leave open.
	if len(got) != 1 || got[0].Result != "ok" || got[0].Detail != "" {
		t.Fatalf("a pairing that got through left %+v", got)
	}
	if got[0].Action != audit.ActionPair {
		t.Fatalf("action %q", got[0].Action)
	}
}

func TestAnAttemptThatSpendsNoCodeLeavesNoLine(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(23)
	body := pairBody(t, code, ctrl.Public, code.Secret)
	// A hash that was never a secret of this installation. Nothing is spent,
	// so nothing is written: this line is one anybody on the network could
	// order without limit.
	h := sha256.Sum256([]byte("never a secret of this installation"))
	path := tunnel.PathPair + "?secret=" + enc.B64.EncodeToString(h[:])
	w := httptest.NewRecorder()
	srv.Pair(w, post(path, body))
	if w.Code != 403 {
		t.Fatalf("pair answered %d", w.Code)
	}
	if got := pairLog(t, srv); len(got) != 0 {
		t.Fatalf("an attempt that spent nothing left %+v", got)
	}
	// And the proof that the way to a line is open at all for this server:
	// the same request with the real hash writes one.
	w = httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), body))
	if w.Code != 204 {
		t.Fatalf("the reachability check answered %d", w.Code)
	}
	if got := pairLog(t, srv); len(got) != 1 {
		t.Fatalf("the reachability check left %d lines", len(got))
	}
}
