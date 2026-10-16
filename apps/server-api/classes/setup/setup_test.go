package setup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	// OPTIONS and a method of nobody's are in the list too: they take the
	// same branch, and a branch no test walks is one nobody notices losing.
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch,
		http.MethodHead, http.MethodOptions, "SETUP"} {
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
//
// It also proves that the start is told. This is the state a start cut short
// between making the file and writing it leaves behind, and O_EXCL keeps
// every later start from writing a code: without a word of its own the admin
// sees nothing but 404 for good and never learns why.
func TestEnsureCodeSaysNoCodeWhenTheFileHoldsNone(t *testing.T) {
	h := newHandler(t)
	const junk = "not a code\n"
	if err := os.WriteFile(h.CodeFile, []byte(junk), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	ok, err := h.EnsureCode()
	if ok || !errors.Is(err, ErrNoCode) {
		t.Fatalf("a file that holds no code answered %v %v", ok, err)
	}
	// An empty file is the same case, and it is the one a crash leaves.
	if err := os.WriteFile(h.CodeFile, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ok, err := h.EnsureCode(); ok || !errors.Is(err, ErrNoCode) {
		t.Fatalf("an empty file answered %v %v", ok, err)
	}
	if err := os.WriteFile(h.CodeFile, []byte(junk), 0o600); err != nil {
		t.Fatalf("write: %v", err)
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

// TestTheLongTermKeysAreCopiedOnce proves this API's private keys are read
// out of the configuration once and the public ones held. The offer answers
// whoever asks, with no login in front of it, and config.Secret.Bytes hands
// out a fresh copy of the key each time: a handler that reads them per
// request lets anybody strew as many copies of the long-term keys through
// memory, core dump and swap file as he sends requests -- in the one state
// where the installation has no admin and nothing else stands in the way.
func TestTheLongTermKeysAreCopiedOnce(t *testing.T) {
	h := newHandler(t)
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 200 {
		t.Fatalf("offer status %d", w.Code)
	}
	first := bytes.Clone(w.Body.Bytes())
	// Other keys in the configuration change nothing about the answer, since
	// no request reads them again. One that did would name these instead --
	// and the signing key here is not even the size of a seed.
	noiseKey, _ := trusttest.NodeKeys(7)
	h.S.Cfg.NoiseKey = config.NewSecret(noiseKey.Private)
	h.S.Cfg.SignKey = config.NewSecret(make([]byte, 5))
	w = httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 200 {
		t.Fatalf("the second offer answered %d", w.Code)
	}
	if !bytes.Equal(first, w.Body.Bytes()) {
		t.Fatal("the second offer names other keys: the private ones were read again")
	}
}

// errNoPut is what noPut answers: a database that does not answer, met where
// a wrong attempt is counted.
var errNoPut = errors.New("setup_test: the database does not answer")

// noPut is a store that cannot write a lock row and works otherwise. The
// prefix is the one package lock gives its rows.
type noPut struct{ dbapi.Store }

func (n noPut) BlobPut(ctx context.Context, b dbapi.Blob) error {
	if b.Kind == dbapi.BlobSetting && strings.HasPrefix(b.ID, "lock.") {
		return errNoPut
	}
	return n.Store.BlobPut(ctx, b)
}

// noRead answers every read of a trust row with the very error noPut answers
// a write with: one fault of one database, met at the step that reads instead
// of at the one that writes.
type noRead struct{ dbapi.Store }

func (n noRead) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if kind == dbapi.BlobTrust {
		return dbapi.Blob{}, errNoPut
	}
	return n.Store.BlobGet(ctx, kind, id)
}

// badLock hands out a row of the counting that nothing can read. The store
// answers; what it answers with is wrong, which is the other kind of fault and
// the one httpapi.Fault cannot see -- lock.ErrLock is package lock's word, not
// package state's, and every error Fault does not know reads there as a store
// that did not answer.
type badLock struct{ dbapi.Store }

func (n badLock) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if kind == dbapi.BlobSetting && strings.HasPrefix(id, "lock.") {
		// Seq and Updated are what keep it from reading as a row past its keep
		// time, which would be no row at all (classes/lock, expired).
		return dbapi.Blob{Kind: kind, ID: id, Seq: 1 << 62, Data: []byte("not a lock row"),
			Updated: 1 << 40}, nil
	}
	return n.Store.BlobGet(ctx, kind, id)
}

// TestADamagedRowOfTheCountingIsNoOutage proves the handler answers a lock row
// that does not read as what it is: broken, and mended in the database. E_
// UNREACHABLE would send the admin away to wait for a database that is there
// and answering.
func TestADamagedRowOfTheCountingIsNoOutage(t *testing.T) {
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
	h.S.DB = badLock{Store: h.S.DB}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 500 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	c, err := wire.ParseClose(w.Body.Bytes())
	if err != nil || c.Code != wire.CodeInternal {
		t.Fatalf("answered %q: %v", c.Code, err)
	}
}

// TestOneFaultOfTheDatabaseIsOneStatus proves the handler answers one fault
// with one status. The same error of the same database was E_UNREACHABLE
// where the wait is counted and E_INTERNAL where the rows behind the trust
// list are read; two statuses for one fault tell the far side two different
// things about one installation.
//
// The one status is E_INTERNAL. A write that was refused is an answer of the
// store, not a silence, and what refused it -- a full disk, a read-only mount,
// a right that is missing, a constraint -- is not over when the database
// answers again. 503 is the contract's word for a database that cannot be
// reached (shared/README.md), and it sends the admin of a first setup into a
// wait that nothing ends. The status is read here and not only the code,
// because one of the two has been wrong before while the other was right.
func TestOneFaultOfTheDatabaseIsOneStatus(t *testing.T) {
	h := newHandler(t)
	codeBytes, _ := os.ReadFile(h.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), code)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	// A version sealed with another code: the answer to it is counted, and
	// that is the write which fails here.
	body := firstVersion(t, info, trusttest.NewSigner(3), "AAAAA-BBBBB-CCCCC-DDDDD")
	h.S.DB = noPut{Store: h.S.DB}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	c, err := wire.ParseClose(w.Body.Bytes())
	if err != nil || c.Code != wire.CodeInternal {
		t.Fatalf("status %d body %s, wanted %s: %v", w.Code, w.Body, wire.CodeInternal, err)
	}
	if w.Code != 500 {
		t.Fatalf("status %d", w.Code)
	}
	// The same error of the same database at the other step: the first thing
	// every request of this handler does is read the trust rows.
	other := newHandler(t)
	other.S.DB = noRead{Store: other.S.DB}
	w = httptest.NewRecorder()
	other.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	c, err = wire.ParseClose(w.Body.Bytes())
	if err != nil || c.Code != wire.CodeInternal || w.Code != 500 {
		t.Fatalf("the read answered %d %s: %v", w.Code, w.Body, err)
	}
}

// TestAnInstallationThatStandsIsAnsweredAsDone proves the answer follows the
// rows and not the tidying up behind them. Once version 1 is in the rows the
// setup is over, and every request after it answers 404: an error here would
// send the admin back to a door that is already shut, and nothing would tell
// him that his own step went through.
func TestAnInstallationThatStandsIsAnsweredAsDone(t *testing.T) {
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
	// The code file is held from two sides, so that removing it fails on
	// either kind of system: Windows keeps a file that is open, and Unix
	// keeps the names in a directory nobody may write in.
	open, err := os.Open(h.CodeFile)
	if err != nil {
		t.Fatalf("hold the file open: %v", err)
	}
	defer open.Close()
	dir := filepath.Dir(h.CodeFile)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod the directory: %v", err)
	}
	defer os.Chmod(dir, 0o700)
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 204 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if _, err := h.S.Trust(ctx); err != nil {
		t.Fatalf("version 1 is not in the rows: %v", err)
	}
	// Nothing is open any more either way: the setup is over because the rows
	// say so, and the file is read only while there is no version.
	w = httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 404 {
		t.Fatalf("the offer after the setup answered %d", w.Code)
	}
	if _, err := os.Stat(h.CodeFile); err != nil {
		t.Skip("the code file went after all: nothing was held here")
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

// TestACodeFileNobodyCanReadIsNotOneThatHoldsNoCode proves the two cases are
// told apart. ErrNoCode says the file holds no code, and the way out of it is
// to remove the file so the next start makes a new one. A file that cannot be
// read is the other case: it may hold exactly the code the admin was given,
// and whoever is told to remove it throws that code away over a fault of the
// rights or of the disk.
func TestACodeFileNobodyCanReadIsNotOneThatHoldsNoCode(t *testing.T) {
	h := newHandler(t)
	if err := os.Remove(h.CodeFile); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// A directory under the name is a file every system opens and none
	// reads, so this holds on Windows as it does on Linux.
	if err := os.Mkdir(h.CodeFile, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := h.readCode()
	if err == nil || errors.Is(err, ErrNoCode) {
		t.Fatalf("a file nobody can read answered %v", err)
	}
	if err := os.Remove(h.CodeFile); err != nil {
		t.Fatalf("remove the directory: %v", err)
	}
	// The other case, and the only one to remove: it is there and holds no
	// code anybody could have been given.
	if err := os.WriteFile(h.CodeFile, []byte("not a code\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := h.readCode(); !errors.Is(err, ErrNoCode) {
		t.Fatalf("a file that holds no code answered %v", err)
	}
	// Neither of them opens a setup: what cannot be read is no code either.
	if _, ok := h.code(); ok {
		t.Fatal("a file that holds no code was taken for one")
	}
}

// TestACodeFileThatWillNotGoIsSaidWithoutTheCode proves the admin is told
// when the code file outlives the setup. Until the next start a valid code
// lies on the disk: put a database without a trust list back in that window
// and EnsureCode opens the setup again with the old code instead of a fresh
// one. The word about it never carries the code itself -- it goes to the log,
// and the code never does.
func TestACodeFileThatWillNotGoIsSaidWithoutTheCode(t *testing.T) {
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
	// The file is held from two sides, so that removing it fails on either
	// kind of system: Windows keeps a file that is open, and Unix keeps the
	// names in a directory nobody may write in.
	open, err := os.Open(h.CodeFile)
	if err != nil {
		t.Fatalf("hold the file open: %v", err)
	}
	defer open.Close()
	dir := filepath.Dir(h.CodeFile)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod the directory: %v", err)
	}
	defer os.Chmod(dir, 0o700)
	var said bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&said, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 204 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if _, err := os.Stat(h.CodeFile); err != nil {
		t.Skip("the code file went after all: nothing was held here")
	}
	if said.Len() == 0 {
		t.Fatal("the code file outlived the setup and nothing was said")
	}
	if strings.Contains(said.String(), code) {
		t.Fatalf("the code itself was said: %s", said.String())
	}
	if !strings.Contains(said.String(), h.CodeFile) {
		t.Fatalf("what was said names no file: %s", said.String())
	}
}

// TestTheSigningKeyIsClearedAfterUse proves no copy of the signing key is
// left lying. An ed25519.PrivateKey is 64 bytes whose first 32 are the seed,
// so a copy of it is the signing key of this node itself, and this is the one
// place that makes one.
func TestTheSigningKeyIsClearedAfterUse(t *testing.T) {
	_, signKey := trusttest.NodeKeys(1)
	sign := ed25519.NewKeyFromSeed(signKey.Seed())
	want := sign.Public().(ed25519.PublicKey)
	pub := signPublic(sign)
	if !bytes.Equal(pub, want) {
		t.Fatalf("public key %x, wanted %x", pub, want)
	}
	for _, b := range sign {
		if b != 0 {
			t.Fatalf("the private signing key was left in memory: %x", []byte(sign))
		}
	}
}

// TestTwoStartsAtOnceBothComeUp is the race two real ngc-api on one data
// directory run into, measured over HTTP on 13.10.2026: both were started in
// the same moment, one of them wrote the code file and the other found it
// there and EMPTY -- O_EXCL creates the name before the code is in it -- read
// ErrNoCode out of that and REFUSED TO START ("der Einmal-Code für die
// Ersteinrichtung lässt sich nicht anlegen"), while nothing was broken.
//
// The deliberate case stays what it is: a file that really holds no code is
// still ErrNoCode (TestEnsureCodeSaysNoCodeWhenTheFileHoldsNone). What this
// case asks is that the name never EXISTS half written, so that the loser of
// the race reads the winner's code instead of a window.
func TestTwoStartsAtOnceBothComeUp(t *testing.T) {
	const rounds = 40
	for i := 0; i < rounds; i++ {
		shared := filepath.Join(t.TempDir(), "setup-code")
		a, b := newHandler(t), newHandler(t)
		// Each handler made a code of its own; what is measured is one file
		// two starts meet, so both are pointed at a name that holds nothing.
		for _, h := range []*Handler{a, b} {
			_ = os.Remove(h.CodeFile)
			h.CodeFile = shared
		}
		errs := make([]error, 2)
		oks := make([]bool, 2)
		var wg sync.WaitGroup
		gate := make(chan struct{})
		for j, h := range []*Handler{a, b} {
			wg.Add(1)
			go func(j int, h *Handler) {
				defer wg.Done()
				<-gate
				oks[j], errs[j] = h.EnsureCode()
			}(j, h)
		}
		close(gate)
		wg.Wait()
		for j := range errs {
			if errs[j] != nil || !oks[j] {
				t.Fatalf("round %d: start %d did not come up: ok=%v err=%v", i, j, oks[j], errs[j])
			}
		}
		// And both hold the SAME code: of two starts at once only one makes
		// one, which is what the hard link at the call site promises.
		code, err := os.ReadFile(shared)
		if err != nil || !session.ValidSetupCode(strings.TrimSpace(string(code))) {
			t.Fatalf("round %d: the file holds %q: %v", i, code, err)
		}
	}
}

// TestTheCodeFileNeverExistsHalfWritten holds the invariant the race of
// TestTwoStartsAtOnceBothComeUp rests on, and holds it without a second
// start: while a start is making the code, the NAME of the code file never
// exists carrying anything but a code.
//
// It is the invariant and not the race, because a second start only SEES the
// window -- whether it sees it depends on how the two are scheduled, and a
// case that depends on that measures the scheduler. Here one goroutine makes
// the code and the other reads the name as fast as it can, so the window is
// caught wherever it is.
func TestTheCodeFileNeverExistsHalfWritten(t *testing.T) {
	const rounds = 20
	// What the watcher managed to read, over all rounds. It is summed and not
	// asked per round on purpose: measured on 16.10.2026, a per-round check
	// went red in 1 of 30 runs because in round 0 the watcher had not been
	// scheduled once before the round was over -- an assertion about the
	// scheduler, not about the code. Summed over the rounds there is no such
	// race: 0 red in 30 runs.
	var seen int64
	for i := 0; i < rounds; i++ {
		h := newHandler(t)
		_ = os.Remove(h.CodeFile)
		done := make(chan error, 1)
		stop := make(chan struct{})
		var bad atomic.Int64
		var reads atomic.Int64
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				b, err := os.ReadFile(h.CodeFile)
				if err != nil {
					continue // not there yet is not a window
				}
				reads.Add(1)
				if !session.ValidSetupCode(strings.TrimSpace(string(b))) {
					bad.Add(1)
				}
			}
		}()
		go func() {
			_, err := h.EnsureCode()
			done <- err
		}()
		if err := <-done; err != nil {
			close(stop)
			t.Fatalf("round %d: EnsureCode: %v", i, err)
		}
		// One read after the call has returned, so the watcher has seen the
		// finished file at least once: a round that read nothing at all would
		// be a green that measured nothing.
		b, err := os.ReadFile(h.CodeFile)
		close(stop)
		if err != nil || !session.ValidSetupCode(strings.TrimSpace(string(b))) {
			t.Fatalf("round %d: the finished file holds %q: %v", i, b, err)
		}
		if n := bad.Load(); n > 0 {
			t.Fatalf("round %d: the name existed %d times without a code in it (of %d reads)",
				i, n, reads.Load())
		}
		seen += reads.Load()
	}
	// And the watcher really looked at least once. Without this the case is
	// green when the watcher reads NOTHING AT ALL: the read after each call
	// above is the TEST's own and says nothing about the watcher, so bad
	// stays 0 and the window is never looked for. Measured on 16.10.2026
	// with a watcher mutated to read nothing: exit 0, 0 FAIL -- the check
	// rode on a counter nobody asked about.
	if seen == 0 {
		t.Fatalf("the watcher read the file in none of %d rounds, so no window was measured", rounds)
	}
}
