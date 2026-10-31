package join

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/coreapp"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"

	"github.com/sorglos-it/nextgencloud/shared/core"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestJoinCodeFitsTheTrustList holds that a fresh code passes
// tunnel.JoinCode.Check against the newest version this API verified, and that
// the one-time value behind it is the SHA-256 of the secret in the code.
func TestJoinCodeFitsTheTrustList(t *testing.T) {
	j, fx := newJoin(t)
	code, expires, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("new code: %v", err)
	}
	if err := code.Check(fx.Version); err != nil {
		t.Fatalf("the code does not fit the list this API verified: %v", err)
	}
	if expires != testNow+tunnel.PairLifetime {
		t.Fatalf("expires %d, want %d", expires, testNow+tunnel.PairLifetime)
	}
	// The code has to survive the way it travels: as text through the ulh
	// answer file and back.
	back, err := tunnel.ParseJoinCode(code.String())
	if err != nil {
		t.Fatalf("parse back: %v", err)
	}
	if back.Check(fx.Version) != nil || !bytes.Equal(back.Secret, code.Secret) {
		t.Fatal("the code does not survive being written down and read back")
	}
	// What the database keeps is the hash, with the secret as its data: that
	// is what Accept checks a MAC against.
	h := sha256.Sum256(code.Secret)
	data, err := j.S.DB.OnceUse(ctx, dbapi.OnceJoin, h[:], testNow)
	if err != nil {
		t.Fatalf("the hash of the secret is not a one-time value: %v", err)
	}
	if data != enc.B64.EncodeToString(code.Secret) {
		t.Fatal("the stored data is not the secret of the code")
	}
	// A second code must not reuse the secret of the first.
	other, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("second code: %v", err)
	}
	if bytes.Equal(other.Secret, code.Secret) {
		t.Fatal("two codes carry the same secret")
	}
}

// TestAcceptKeepsTheRequestOnce holds the whole accepting side: a request with
// a correct tunnel.JoinMAC is kept as dbapi.BlobJoin, the secret is spent
// exactly once, and Waiting shows the row with the same tunnel.JoinFingerprint
// the new server's ulh script prints.
func TestAcceptKeepsTheRequestOnce(t *testing.T) {
	j, fx := newJoin(t)
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("new code: %v", err)
	}
	// Nothing waits before the first request: otherwise "one row" below would
	// also be true of a row somebody else wrote.
	before, err := j.Waiting(ctx)
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("%d rows waiting before any request", len(before))
	}
	req := fx.request(code.Secret)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	h := sha256.Sum256(code.Secret)
	hash := enc.B64.EncodeToString(h[:])
	w := httptest.NewRecorder()
	j.Accept(w, post(hash, body))
	if w.Code != http.StatusNoContent {
		t.Fatalf("accept answered %d, want %d (%s)", w.Code, http.StatusNoContent, w.Body.String())
	}
	// The request lies in the database as a blob of kind join, under the node
	// ID it asks for.
	b, err := j.S.DB.BlobGet(ctx, dbapi.BlobJoin, req.Node)
	if err != nil {
		t.Fatalf("the request was not kept: %v", err)
	}
	kept, err := tunnel.ParseJoinRequest(b.Data)
	if err != nil {
		t.Fatalf("the kept row does not parse: %v", err)
	}
	if kept.Node != req.Node || !bytes.Equal(kept.Noise, req.Noise) || !bytes.Equal(kept.Sign, req.Sign) ||
		!bytes.Equal(kept.TLS, req.TLS) || kept.URL != req.URL {
		t.Fatal("the kept row is not what was asked for")
	}
	// Waiting shows exactly it, with the fingerprint the admin compares.
	rows, err := j.Waiting(ctx)
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows waiting, want 1", len(rows))
	}
	want := tunnel.JoinFingerprint(req.Node, req.Noise, req.Sign, req.TLS, req.URL)
	got := tunnel.JoinFingerprint(rows[0].Node, rows[0].Noise, rows[0].Sign, rows[0].TLS, rows[0].URL)
	if got != want {
		t.Fatalf("fingerprint %q, want %q", got, want)
	}
	if rows[0].Since != testNow {
		t.Fatalf("since %d, want %d", rows[0].Since, testNow)
	}
	// The same secret a second time is refused, and no second row appears.
	w2 := httptest.NewRecorder()
	j.Accept(w2, post(hash, body))
	if w2.Code == http.StatusNoContent {
		t.Fatal("the same join secret was taken twice")
	}
	rows, err = j.Waiting(ctx)
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows after the replay, want 1", len(rows))
	}
}

// TestAcceptRefusesAForeignMAC holds that a request whose MAC was made with
// another secret, or bound to another API key or another URL, is refused and
// keeps nothing.
func TestAcceptRefusesAForeignMAC(t *testing.T) {
	cases := []struct {
		name string
		// mac builds the MAC the request travels with; secret is the real one
		// of the code, so a case can get everything but one value right.
		mac func(fx fixture, secret []byte) []byte
	}{
		{"a secret nobody issued", func(fx fixture, secret []byte) []byte {
			return tunnel.JoinMAC(bytes.Repeat([]byte{1}, tunnel.SecretSize), "api-2",
				fx.NewKeys.Public, fx.NewSign, fx.NewTLS, fx.APIPub, "wss://api-2.test:7443/tunnel")
		}},
		{"bound to another API key", func(fx fixture, secret []byte) []byte {
			return tunnel.JoinMAC(secret, "api-2", fx.NewKeys.Public, fx.NewSign, fx.NewTLS,
				bytes.Repeat([]byte{2}, 32), "wss://api-2.test:7443/tunnel")
		}},
		{"over another URL than the one asked for", func(fx fixture, secret []byte) []byte {
			return tunnel.JoinMAC(secret, "api-2", fx.NewKeys.Public, fx.NewSign, fx.NewTLS,
				fx.APIPub, "wss://evil.test:7443/tunnel")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Each sub-case gets its own API: a secret spent by the one before
			// would make the next refuse for the wrong reason.
			j, fx := newJoin(t)
			code, _, err := j.NewJoinCode(ctx)
			if err != nil {
				t.Fatalf("new code: %v", err)
			}
			good := fx.request(code.Secret)
			req := good
			req.MAC = enc.Bytes(c.mac(fx, code.Secret))
			if bytes.Equal(req.MAC, good.MAC) {
				t.Fatal("the MAC of this case is the correct one, so it measures nothing")
			}
			body, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("body: %v", err)
			}
			h := sha256.Sum256(code.Secret)
			w := httptest.NewRecorder()
			j.Accept(w, post(enc.B64.EncodeToString(h[:]), body))
			if w.Code == http.StatusNoContent {
				t.Fatal("a foreign MAC was taken")
			}
			if _, err := j.S.DB.BlobGet(ctx, dbapi.BlobJoin, "api-2"); !errors.Is(err, dbapi.ErrNotFound) {
				t.Fatalf("a row was kept anyway: %v", err)
			}
			rows, err := j.Waiting(ctx)
			if err != nil {
				t.Fatalf("waiting: %v", err)
			}
			if len(rows) != 0 {
				t.Fatalf("%d rows waiting, want 0", len(rows))
			}
		})
	}
}

// TestAcceptRefusesABrokenRequest holds that a body the contract's parser does
// not take, and a query that is no hash, are refused without a row — and that
// none of them spends the secret, which the control at the end measures.
func TestAcceptRefusesABrokenRequest(t *testing.T) {
	j, fx := newJoin(t)
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("new code: %v", err)
	}
	h := sha256.Sum256(code.Secret)
	hash := enc.B64.EncodeToString(h[:])
	good, err := json.Marshal(fx.request(code.Secret))
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	cases := []struct {
		name string
		hash string
		body []byte
	}{
		{"no JSON at all", hash, []byte("{")},
		{"a node ID the contract refuses", hash, []byte(`{"node":"a b"}`)},
		{"no secret in the query", "", good},
		{"a query that is no base64url", "!!!", good},
		{"a query of the wrong length", enc.B64.EncodeToString([]byte("short")), good},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			j.Accept(w, post(c.hash, c.body))
			if w.Code == http.StatusNoContent {
				t.Fatal("a broken request was taken")
			}
			rows, err := j.Waiting(ctx)
			if err != nil {
				t.Fatalf("waiting: %v", err)
			}
			if len(rows) != 0 {
				t.Fatalf("%d rows waiting, want 0", len(rows))
			}
		})
	}
	// The control: the secret is still unspent, so none of the cases above
	// reached the one-time value.
	w := httptest.NewRecorder()
	j.Accept(w, post(hash, good))
	if w.Code != http.StatusNoContent {
		t.Fatalf("the good request after the broken ones answered %d: %s", w.Code, w.Body.String())
	}
}

// TestWaitingSkipsABrokenRow holds that a stored row which no longer parses
// never reaches the shell as something to sign.
func TestWaitingSkipsABrokenRow(t *testing.T) {
	j, _ := newJoin(t)
	if err := j.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobJoin, ID: "api-9",
		Data: []byte(`{"node":"api-9"}`), Updated: testNow}); err != nil {
		t.Fatalf("put: %v", err)
	}
	rows, err := j.Waiting(ctx)
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("%d rows waiting, want 0", len(rows))
	}
}

// TestKeysOnlyForAListedNode holds the exchange of the shared keys: before the
// admin signed the new server in, PathKeys gives nothing; afterwards exactly
// the two keys every API of the installation shares.
func TestKeysOnlyForAListedNode(t *testing.T) {
	j, fx := newJoin(t)
	// Before the signature there is nothing to fetch.
	if _, _, err := dialKeys(t, fx, fx.NewKeys, tunnel.Fingerprint(fx.NewKeys.Public)); err == nil {
		t.Fatal("the keys were handed out before the new server was listed")
	}
	listTheNewServer(t, j, fx)
	tr, ws, err := dialKeys(t, fx, fx.NewKeys, tunnel.Fingerprint(fx.NewKeys.Public))
	if err != nil {
		t.Fatalf("dial after being listed: %v", err)
	}
	f := readFrame(t, tr, ws)
	if f.Type != wire.TypeData || f.Channel != wire.ControlChannel {
		t.Fatalf("frame type %d on channel %d", f.Type, f.Channel)
	}
	var got sharedKeys
	if err := json.Unmarshal(f.Payload, &got); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if !bytes.Equal(got.SecretKey, testSecretKey) {
		t.Fatal("the secret key that arrived is not this installation's")
	}
	if !bytes.Equal(got.ChallengeKey, testChallengeKey) {
		t.Fatal("the challenge key that arrived is not this installation's")
	}
	// Nothing else travels: the private Noise and signing keys of this API
	// stay here.
	if bytes.Contains(f.Payload, []byte(enc.B64.EncodeToString(j.S.Cfg.NoiseKey.Bytes()))) ||
		bytes.Contains(f.Payload, []byte(enc.B64.EncodeToString(j.S.Cfg.SignKey.Bytes()))) {
		t.Fatal("a private key of this API travelled with the shared keys")
	}
}

// TestKeysRefusesAnUnlistedKey holds which keys get no answer even while an
// api-2 stands in the list: a stranger's, this API's own, the controller's,
// an empty fingerprint, and a fingerprint that names a listed key while the
// handshake runs with another one.
func TestKeysRefusesAnUnlistedKey(t *testing.T) {
	j, fx := newJoin(t)
	listTheNewServer(t, j, fx)
	// The control that the way is open at all: the listed key is answered.
	if _, _, err := dialKeys(t, fx, fx.NewKeys, tunnel.Fingerprint(fx.NewKeys.Public)); err != nil {
		t.Fatalf("the listed key is refused, so this case measures nothing: %v", err)
	}
	stranger, _ := trusttest.NodeKeys(0xF5)
	own, _ := trusttest.NodeKeys(0xA1)
	ctl, _ := trusttest.NodeKeys(0xC1)
	cases := []struct {
		name        string
		keys        noise.DHKey
		fingerprint string
	}{
		{"a key nobody listed", stranger, tunnel.Fingerprint(stranger.Public)},
		{"this API's own key", own, tunnel.Fingerprint(own.Public)},
		{"the controller's key", ctl, tunnel.Fingerprint(ctl.Public)},
		{"an empty fingerprint", fx.NewKeys, ""},
		{"another key under the listed fingerprint", stranger, tunnel.Fingerprint(fx.NewKeys.Public)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := dialKeys(t, fx, c.keys, c.fingerprint); err == nil {
				t.Fatal("the keys were handed out")
			}
		})
	}
}

// Aufgabe 16 left coreapp.JoinCodes as an interface because this package did
// not exist yet. This is the obligation it wrote down, checked by the
// compiler: coreapp does not import this package, so the check can live here.
var _ coreapp.JoinCodes = (*Join)(nil)

// ---- the log of a join ------------------------------------------------------
//
// The same two statements as in controllers/pair_test.go, measured on the
// other of the two doors: an attempt that SPENT a code leaves exactly one
// line that names the check, and an attempt that spent none leaves none.
// Every case makes its own server, so no one-time value of one reaches
// another (messen.md).

// joinLog reads the whole log of j, newest first.
func joinLog(t *testing.T, j *Join) []core.LogEntry {
	t.Helper()
	got, err := j.S.DB.LogQuery(ctx, core.LogQuery{Limit: 50})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	return got
}

func TestABurntJoinCodeLeavesOneLineThatNamesTheCheck(t *testing.T) {
	j, fx := newJoin(t)
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("new code: %v", err)
	}
	if got := joinLog(t, j); len(got) != 0 {
		t.Fatalf("the log was not empty before the attempt: %+v", got)
	}
	body, err := json.Marshal(fx.request([]byte("wrong secret!!!!")))
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	h := sha256.Sum256(code.Secret)
	w := httptest.NewRecorder()
	j.Accept(w, post(enc.B64.EncodeToString(h[:]), body))
	if w.Code == http.StatusNoContent {
		t.Fatal("a foreign MAC was taken")
	}
	got := joinLog(t, j)
	if len(got) != 1 {
		t.Fatalf("a burnt code left %d lines, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.Kind != core.LogTrust || e.Action != audit.ActionJoin ||
		e.Result != string(wire.CodeVerify) || e.Detail != string(reason.JoinProof) {
		t.Fatalf("line %+v", e)
	}
	if e.User != audit.UserSystem || e.Node != "" || e.App != "" {
		t.Fatalf("the line names somebody or something of the attempt: %+v", e)
	}
}

func TestAJoinThatGotThroughLeavesALineToo(t *testing.T) {
	j, fx := newJoin(t)
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("new code: %v", err)
	}
	body, err := json.Marshal(fx.request(code.Secret))
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	h := sha256.Sum256(code.Secret)
	w := httptest.NewRecorder()
	j.Accept(w, post(enc.B64.EncodeToString(h[:]), body))
	if w.Code != http.StatusNoContent {
		t.Fatalf("accept answered %d (%s)", w.Code, w.Body)
	}
	got := joinLog(t, j)
	if len(got) != 1 || got[0].Result != "ok" || got[0].Detail != "" ||
		got[0].Action != audit.ActionJoin {
		t.Fatalf("a join that got through left %+v", got)
	}
}

func TestAJoinAttemptThatSpendsNoCodeLeavesNoLine(t *testing.T) {
	j, fx := newJoin(t)
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("new code: %v", err)
	}
	body, err := json.Marshal(fx.request(code.Secret))
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	// A hash that was never a secret here: nothing is spent, so nothing is
	// written -- see audit.Log.Pairing for why that line is not wanted.
	stranger := sha256.Sum256([]byte("never a secret of this installation"))
	w := httptest.NewRecorder()
	j.Accept(w, post(enc.B64.EncodeToString(stranger[:]), body))
	if w.Code == http.StatusNoContent {
		t.Fatal("an unknown hash was taken")
	}
	if got := joinLog(t, j); len(got) != 0 {
		t.Fatalf("an attempt that spent nothing left %+v", got)
	}
	// The way to a line is open for this server all the same.
	h := sha256.Sum256(code.Secret)
	w = httptest.NewRecorder()
	j.Accept(w, post(enc.B64.EncodeToString(h[:]), body))
	if w.Code != http.StatusNoContent {
		t.Fatalf("the reachability check answered %d", w.Code)
	}
	if got := joinLog(t, j); len(got) != 1 {
		t.Fatalf("the reachability check left %d lines", len(got))
	}
}
