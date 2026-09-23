package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// proofUserKey names the type of trust.User.Keys, which comes from package
// proof, so the version below reads like the one the shell builds.
type proofUserKey = proof.UserKey

var ctx = context.Background()

// newHandler returns a handler on a fresh store with a fixed clock and a
// one-time code of its own. It takes a testing.TB so that a fuzz target,
// which has a *testing.F, builds one per run.
func newHandler(t testing.TB) *Handler {
	t.Helper()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	_, signKey := trusttest.NodeKeys(1)
	noiseKey, _ := trusttest.NodeKeys(2)
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.ngc.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noiseKey.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey: config.NewSecret(make([]byte, 32)),
		// Its own key next to SecretKey; config.API.Validate asks for both
		// with 32 bytes, so a configuration without it is none.
		ChallengeKey: config.NewSecret(make([]byte, 32)),
		ReleaseKey:   enc.Bytes(make([]byte, 32)), StoreKey: enc.Bytes(make([]byte, 32)),
		StoreURL: "https://store.ngc.test", SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, make([]byte, 32))
	s.Now = func() int64 { return 1790000000 }
	h := &Handler{S: s, CodeFile: filepath.Join(t.TempDir(), "setup-code")}
	if _, err := h.EnsureCode(); err != nil {
		t.Fatalf("ensure code: %v", err)
	}
	return h
}

func TestOfferIsSealedWithTheCode(t *testing.T) {
	h := newHandler(t)
	code, err := os.ReadFile(h.CodeFile)
	if err != nil || !session.ValidSetupCode(strings.TrimSpace(string(code))) {
		t.Fatalf("code file %q: %v", code, err)
	}
	// Windows knows no such mode: there Go reports 0666 for every file it can
	// write, as the checks in shared/config and apitls say.
	if info, _ := os.Stat(h.CodeFile); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("code file mode %v", info.Mode().Perm())
	}
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 200 {
		t.Fatalf("offer status %d", w.Code)
	}
	info, err := session.ParseSetupOffer(w.Body.Bytes(), strings.TrimSpace(string(code)))
	if err != nil {
		t.Fatalf("parse offer: %v", err)
	}
	if len(info.Nodes) != 1 || info.Nodes[0].ID != "api-1" || info.Nodes[0].URL != h.S.Cfg.URL {
		t.Fatalf("nodes %+v", info.Nodes)
	}
	if info.Store == nil || info.Store.ID != trust.StoreGlobal || info.Store.URL != h.S.Cfg.StoreURL {
		t.Fatalf("store %+v", info.Store)
	}
	// Another code opens nothing: the MAC is checked before info is read.
	if _, err := session.ParseSetupOffer(w.Body.Bytes(), "AAAAA-BBBBB-CCCCC-DDDDD"); err == nil {
		t.Fatal("offer accepted with another code")
	}
}

func TestSetupTakesVersionOneOnlyOnce(t *testing.T) {
	h := newHandler(t)
	codeBytes, _ := os.ReadFile(h.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), code)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}

	admin := trusttest.NewSigner(3)
	body := firstVersion(t, info, admin, code)
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 204 {
		t.Fatalf("setup status %d body %s", w.Code, w.Body)
	}
	if _, err := os.Stat(h.CodeFile); !os.IsNotExist(err) {
		t.Fatal("the code file survived the setup")
	}
	v, err := h.S.Trust(ctx)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("trust after setup: %v", err)
	}
	// Once there is an admin both methods answer E_NOTFOUND.
	w = httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 404 {
		t.Fatalf("second offer status %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 404 {
		t.Fatalf("second setup status %d", w.Code)
	}
}

func TestSetupRefusesAForeignVersionAndLocks(t *testing.T) {
	h := newHandler(t)
	codeBytes, _ := os.ReadFile(h.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, _ := session.ParseSetupOffer(w.Body.Bytes(), code)
	admin := trusttest.NewSigner(3)
	// A man in the middle who does not hold the code cannot make the MAC.
	body := firstVersion(t, info, admin, "AAAAA-BBBBB-CCCCC-DDDDD")
	for i := 0; i < 3; i++ {
		w = httptest.NewRecorder()
		h.Serve(w, post(body))
		if w.Code != 403 && w.Code != 429 {
			t.Fatalf("attempt %d answered %d", i, w.Code)
		}
	}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("no lock: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	// Body and status say the same thing, as every answer of the contract does.
	c, err := wire.ParseClose(w.Body.Bytes())
	if err != nil || c.Code != wire.CodeLocked {
		t.Fatalf("body %s: %v", w.Body, err)
	}
	if _, err := h.S.Trust(ctx); err == nil {
		t.Fatal("a foreign version was taken")
	}
}

// TestDamagedRowsNeverAskForAnotherSignature proves that rows of our own
// which contradict each other are answered as the installation's fault. The
// admin would sign a fresh version over a database no version he can sign
// will mend, and E_VERIFY is what would send him off to do it.
func TestDamagedRowsNeverAskForAnotherSignature(t *testing.T) {
	h := newHandler(t)
	// Version 1 of another installation, in the row where ours belongs.
	other := trusttest.NewSigner(4)
	l := &trust.List{
		Seq: 1, Issued: 1790000000, Installation: trusttest.ID(9),
		Users: []trust.User{{ID: trusttest.ID(1), Name: "admin", Roles: []string{trust.RoleAdmin},
			Keys: []proofUserKey{other.Key}}},
		Roles: []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelCritical}},
	}
	row, err := json.Marshal(other.Sign(t, l))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := h.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: row}); err != nil {
		t.Fatalf("put: %v", err)
	}
	for _, r := range []*http.Request{httptest.NewRequest(http.MethodGet, session.PathSetup, nil), post([]byte("{}"))} {
		w := httptest.NewRecorder()
		h.Serve(w, r)
		if w.Code != 500 {
			t.Fatalf("%s answered %d", r.Method, w.Code)
		}
		c, err := wire.ParseClose(w.Body.Bytes())
		if err != nil || c.Code != wire.CodeInternal {
			t.Fatalf("%s body %s: %v", r.Method, w.Body, err)
		}
	}
}

// TestAStepAnotherHoldsIsNoBadSignature proves that a step this API cannot
// take right now is never answered as a fault of the caller: his version and
// his signature are good, and E_VERIFY would send him off to sign them again.
// It is no wrong attempt either, so it must not count against the wait.
func TestAStepAnotherHoldsIsNoBadSignature(t *testing.T) {
	h := newHandler(t)
	codeBytes, _ := os.ReadFile(h.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), code)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	body := firstVersion(t, info, trusttest.NewSigner(3), code)
	// "trust" is the name package state takes for this step; while another
	// owner holds it, AddVersion answers state.ErrBusy.
	now := h.S.Now()
	if ok, err := h.S.DB.LockTake(ctx, "trust", "api-2", now, now+30); err != nil || !ok {
		t.Fatalf("take the step: %v", err)
	}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 500 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	c, err := wire.ParseClose(w.Body.Bytes())
	if err != nil || c.Code != wire.CodeInternal {
		t.Fatalf("body %s: %v", w.Body, err)
	}
	// Once the step is free the same request goes through: nothing was
	// counted against it, and the code is still the one that counts.
	if err := h.S.DB.LockFree(ctx, "trust", "api-2"); err != nil {
		t.Fatalf("free the step: %v", err)
	}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 204 {
		t.Fatalf("status once the step was free: %d body %s", w.Code, w.Body)
	}
}

// TestSetupAnswersOnlyItsTwoMethods proves the handler stands on its own: the
// setup runs under POST and under nothing else, whether the router in front
// sorts the methods or not. The body is the right one, so a handler that took
// every other method for a POST would set the installation up here.
func TestSetupAnswersOnlyItsTwoMethods(t *testing.T) {
	h := newHandler(t)
	codeBytes, _ := os.ReadFile(h.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), code)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	body := firstVersion(t, info, trusttest.NewSigner(3), code)
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead} {
		r := httptest.NewRequest(m, session.PathSetup, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Serve(w, r)
		if w.Code != 400 {
			t.Fatalf("%s answered %d", m, w.Code)
		}
		if _, err := h.S.Trust(ctx); err == nil {
			t.Fatalf("%s set the installation up", m)
		}
	}
}

// TestEnsureCodeSaysNoCodeWhenTheFileHoldsNone proves that what stands in the
// file decides and not that a file is there: one that holds no code opens no
// setup, and it is never written over either, whatever put it there.
func TestEnsureCodeSaysNoCodeWhenTheFileHoldsNone(t *testing.T) {
	h := newHandler(t)
	const junk = "not a code\n"
	if err := os.WriteFile(h.CodeFile, []byte(junk), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	ok, err := h.EnsureCode()
	if err != nil || ok {
		t.Fatalf("a file that holds no code was taken for one: %v %v", ok, err)
	}
	if b, err := os.ReadFile(h.CodeFile); err != nil || string(b) != junk {
		t.Fatalf("the file was written over with %q: %v", b, err)
	}
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 404 {
		t.Fatalf("offer status %d", w.Code)
	}
}

// TestEnsureCodeKeepsTheCodeItMade proves that a second start hands out no
// second code: the one the admin was given stays the one that counts.
func TestEnsureCodeKeepsTheCodeItMade(t *testing.T) {
	h := newHandler(t)
	first, err := os.ReadFile(h.CodeFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	ok, err := h.EnsureCode()
	if err != nil || !ok {
		t.Fatalf("second start: %v %v", ok, err)
	}
	again, err := os.ReadFile(h.CodeFile)
	if err != nil || string(again) != string(first) {
		t.Fatalf("the code was made afresh: %v", err)
	}
	// Once an admin exists the file goes, so nothing on disk still opens a
	// setup that is over.
	h2 := newHandler(t)
	codeBytes, _ := os.ReadFile(h2.CodeFile)
	w := httptest.NewRecorder()
	h2.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), strings.TrimSpace(string(codeBytes)))
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	w = httptest.NewRecorder()
	h2.Serve(w, post(firstVersion(t, info, trusttest.NewSigner(3), strings.TrimSpace(string(codeBytes)))))
	if w.Code != 204 {
		t.Fatalf("setup status %d", w.Code)
	}
	if ok, err := h2.EnsureCode(); err != nil || ok {
		t.Fatalf("a code after the setup: %v %v", ok, err)
	}
	if _, err := os.Stat(h2.CodeFile); !os.IsNotExist(err) {
		t.Fatalf("the code file is still there: %v", err)
	}
}

// post builds a POST of the setup with the right content type.
func post(body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, session.PathSetup, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// firstVersion builds version 1 the way the shell does and seals it with mac.
func firstVersion(t testing.TB, info session.SetupInfo, admin trusttest.Signer, mac string) []byte {
	t.Helper()
	l := &trust.List{
		Seq: 1, Issued: info.Now, Installation: info.Installation,
		Users: []trust.User{{ID: trusttest.ID(1), Name: "admin", Roles: []string{trust.RoleAdmin},
			Keys: []proofUserKey{admin.Key}[0:1]}},
		Roles:  []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelCritical}},
		Nodes:  info.Nodes,
		Stores: []trust.Store{*info.Store},
		// Sorted by target, as trust.List.Validate asks: "system" comes last.
		Auto: []trust.Auto{
			{Target: trust.AutoAPI, Key: info.ReleaseKey},
			{Target: trust.AutoController, Key: info.ReleaseKey},
			{Target: trust.AutoWeb, Key: info.ReleaseKey},
			{Target: trust.AutoSystem},
		},
	}
	signed := admin.Sign(t, l)
	b, err := json.Marshal(session.Setup{MAC: session.SetupMAC(mac, signed.Payload), Trust: signed})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
