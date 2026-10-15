package login

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

func TestStartAnswersForAKnownUser(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	if len(c.Nonce) != session.NonceSize || c.Installation != fx.Installation || c.User != fx.PasswordUser {
		t.Fatalf("challenge %+v", c)
	}
	if c.KDF == nil || c.KDF.Validate() != nil {
		t.Fatalf("kdf %+v", c.KDF)
	}
	if c.TOTP || c.PasswordToo {
		t.Fatalf("totp %v passwordToo %v", c.TOTP, c.PasswordToo)
	}
	if c.MaxLifetime != h.S.Cfg.SessionLifetime || c.Now != h.S.Now() {
		t.Fatalf("times %d %d", c.MaxLifetime, c.Now)
	}
	if c.RP.ID == "" {
		t.Fatal("rp missing")
	}
	// Two challenges for the same user have different nonces.
	if other := start(t, h, fx.PasswordName); string(other.Nonce) == string(c.Nonce) {
		t.Fatal("the same nonce twice")
	}
}

func TestStartNamesTheKeyIDOfEveryPasskey(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasskeyName)
	u, _ := fx.List.UserByName(fx.PasskeyName)
	var want int
	for _, k := range u.Keys {
		if k.Kind == proof.KindPasskey {
			want++
		}
	}
	if len(c.Passkeys) != want {
		t.Fatalf("%d passkeys, want %d", len(c.Passkeys), want)
	}
	// The browser takes the key ID from here and derives none of its own, so
	// every entry has to carry both halves (session.PasskeyRef).
	for _, p := range c.Passkeys {
		if p.Validate() != nil {
			t.Fatalf("passkey ref %+v", p)
		}
	}
	// The whole answer has to pass the parser the browser reads it with;
	// anything it refuses would tell the browser something.
	b, _ := json.Marshal(c)
	if _, err := session.ParseLoginChallenge(b); err != nil {
		t.Fatalf("the browser would refuse this answer: %v", err)
	}
}

func TestStartLooksTheSameForAnUnknownName(t *testing.T) {
	h, _ := newFixture(t)
	c := start(t, h, "nobody")
	if len(c.Nonce) != session.NonceSize {
		t.Fatalf("challenge %+v", c)
	}
	if !trust.ValidID(c.User) {
		t.Fatalf("user %q", c.User)
	}
	// The made-up answer passes the browser's parser like a real one.
	b, _ := json.Marshal(c)
	if _, err := session.ParseLoginChallenge(b); err != nil {
		t.Fatalf("the browser would refuse the made-up answer: %v", err)
	}
	// Made up, but stable: a second try must not tell the shell anything new.
	again := start(t, h, "nobody")
	if again.User != c.User || len(again.Passkeys) != len(c.Passkeys) || again.TOTP != c.TOTP {
		t.Fatal("the made-up values changed between two calls")
	}
	if (again.KDF == nil) != (c.KDF == nil) ||
		(c.KDF != nil && string(again.KDF.Salt) != string(c.KDF.Salt)) {
		t.Fatal("the made-up KDF changed between two calls")
	}
	// And they differ from those of another unknown name.
	third := start(t, h, "nothing")
	if third.User == c.User {
		t.Fatal("two unknown names got the same user")
	}
}

func TestStartTakesTheSamePathForAnUnknownName(t *testing.T) {
	h, fx := newFixture(t)
	// 1. Nothing outside tells the two apart: same status, same headers, and a
	//    shape the installation really gives out. session.Challenge draws the
	//    made-up shape from the list, so it is one of the shapes the list's own
	//    users produce — not necessarily this one user's.
	known, unknown := raw(t, h, fx.PasswordName), raw(t, h, "nobody")
	if known.Code != unknown.Code {
		t.Fatalf("status %d vs %d", known.Code, unknown.Code)
	}
	if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
		t.Fatalf("headers %v vs %v", known.Header(), unknown.Header())
	}
	real := map[string]bool{}
	for _, u := range fx.List.Users {
		real[fields(t, raw(t, h, u.Name))] = true
	}
	if !real[fields(t, unknown)] {
		t.Fatalf("made-up shape %q, real shapes %v", fields(t, unknown), real)
	}
	// 2. Nothing inside either: login/start writes no log entry for either
	//    name, and it locks neither. Only login/finish counts failures, so
	//    asking for a name must never hold it against anybody.
	entries, err := h.S.DB.LogQuery(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(entries) != 0 {
		t.Fatalf("login/start logged %d entries (%v)", len(entries), err)
	}
	for _, name := range []string{fx.PasswordName, "nobody"} {
		if wait, _ := lock.Check(ctx, h.S.DB, name, h.S.Now()); wait != 0 {
			t.Fatalf("%q was locked by asking", name)
		}
	}
	// 3. Time used to stand here, measured over 25 requests. On a clock that
	//    resolves a millisecond that is 25 zeroes, and "0 > 2*0" is false, so
	//    the comparison passed whatever the handler did. What the two names do
	//    inside now stands in TestStartMakesTheSameDatabaseCallsForEveryName,
	//    which needs no clock at all, and the time in
	//    TestStartTakesTheSameTimeForAnUnknownName, which measures batches a
	//    clock can see.
}

// TestStartMakesTheSameDatabaseCallsForEveryName is the clock-free half of
// "the same path": the calls a request makes, in order. Two names may now
// differ in NOTHING -- not in which call is made, not in how often, and not
// in whether a row is asked for by an ID no name has.
//
// It used to allow one difference, the nonce row kept only for a real user,
// and that one call was why the failure of it had to be swallowed (point 50).
// Since the made-up challenge keeps a nonce too, under an ID of nobody's, the
// lists are equal and the failure can be answered. This test is the floor
// that answer stands on: let the one call come back and
// TestStartSaysComeBackWhenTheDatabaseIsBusy turns into the oracle it shuts.
func TestStartMakesTheSameDatabaseCallsForEveryName(t *testing.T) {
	h, fx := newFixture(t)
	rec := &callLog{Store: h.S.DB}
	h.S.DB = rec
	// One request first: it counts the TOTP share, which is then kept for
	// shareTTL, so it belongs to no name in particular.
	raw(t, h, "warmup")
	rec.reset()
	raw(t, h, fx.PasswordName)
	known := rec.list()
	rec.reset()
	raw(t, h, "nobody")
	unknown := rec.list()

	if !slices.Equal(known, unknown) {
		t.Fatalf("known %v, unknown %v", known, unknown)
	}
	// And the nonce write is in there for both, so the equality above is two
	// paths that both keep a nonce and not two that both skip it.
	if last := known[len(known)-1]; last != "OnceCreate "+dbapi.OnceNonce {
		t.Fatalf("the last call of either name is %q, want the nonce write", last)
	}
	for _, call := range slices.Concat(known, unknown) {
		if call == "UserGet(empty id)" {
			t.Fatalf("a row was asked for by an empty ID: %v", known)
		}
		if call == "LogAdd" {
			t.Fatal("login/start wrote a log entry")
		}
	}
}

// TestStartAnswersTheSameWhenTheNonceCannotBeStored holds the one call a known
// name makes more to the same rule as the rest: a database that reads but
// cannot write -- a full disk, a read replica, a locked table -- must not turn
// that one call into an answer of its own. It would be a perfect oracle, with
// one request per name and no measuring at all.
//
// The login fails later with E_LOGIN, which is the answer a wrong password
// gets too, so nothing is lost by answering here.
func TestStartAnswersTheSameWhenTheNonceCannotBeStored(t *testing.T) {
	h, fx := newFixture(t)
	h.S.DB = noOnce{Store: h.S.DB}
	known, unknown := raw(t, h, fx.PasswordName), raw(t, h, "nobody")
	if known.Code != 200 || unknown.Code != 200 {
		t.Fatalf("known %d, unknown %d: %s", known.Code, unknown.Code, known.Body)
	}
	if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
		t.Fatalf("headers %v vs %v", known.Header(), unknown.Header())
	}
	for _, w := range []*httptest.ResponseRecorder{known, unknown} {
		if _, err := session.ParseLoginChallenge(w.Body.Bytes()); err != nil {
			t.Fatalf("the browser would refuse this answer: %v", err)
		}
	}
}

// TestStartSaysSoWhenTheNonceCannotBeKept: swallowing the failure keeps the
// answer alike, but it must not make the failure invisible. Every login then
// fails with E_LOGIN and nothing outside says why, where a 503 used to. The
// line is said once per window and names nothing of the request, so it is
// neither floodable nor an answer to whoever reads the log.
func TestStartSaysSoWhenTheNonceCannotBeKept(t *testing.T) {
	h, fx := newFixture(t)
	now := testNow
	h.S.Now = func() int64 { return now }
	h.S.DB = noOnce{Store: h.S.DB}
	log := capturedLog(t)
	for range 20 {
		raw(t, h, fx.PasswordName)
		raw(t, h, "nobody")
	}
	if lines := strings.Count(log.String(), "\n"); lines != 1 {
		t.Fatalf("%d lines for 40 requests, want 1:\n%s", lines, log)
	}
	if s := log.String(); strings.Contains(s, fx.PasswordName) || strings.Contains(s, fx.PasswordUser) {
		t.Fatalf("the line names the request: %s", s)
	}
	// Nor does it carry the database's own error. The only values a driver
	// could quote back from that statement are the nonce and the user ID, and
	// a nonce in the server log is what the standing rule keeps out of it.
	if s := log.String(); strings.Contains(s, noOnceError) {
		t.Fatalf("the line carries the error of the database: %s", s)
	}
	// One second before the window is over it is still quiet.
	now = testNow + nonceWarnTTL - 1
	raw(t, h, fx.PasswordName)
	if lines := strings.Count(log.String(), "\n"); lines != 1 {
		t.Fatalf("%d lines after %d seconds, want 1", lines, nonceWarnTTL-1)
	}
	// On the second itself it is said again, so a failure that lasts does not
	// fall silent after the first minute.
	now = testNow + nonceWarnTTL
	raw(t, h, fx.PasswordName)
	if lines := strings.Count(log.String(), "\n"); lines != 2 {
		t.Fatalf("%d lines after %d seconds, want 2:\n%s", lines, nonceWarnTTL, log)
	}
}

// TestStartAnswersWhenTheRowCannotBeRead moves the hole of K-2 one door
// further and shuts it there. What dbapi.Store.UserByName gives for a name
// nobody has is not in the contract: it carries no doc comment, and
// dbapi/dbtest pins it only for a name that exists. A store that answers
// anything but dbapi.ErrNotFound would tell the two names apart again, so no
// error of that call becomes an answer of its own.
func TestStartAnswersWhenTheRowCannotBeRead(t *testing.T) {
	h, fx := newFixture(t)
	h.S.DB = oddMiss{Store: h.S.DB}
	known, unknown := raw(t, h, fx.PasswordName), raw(t, h, "nobody")
	if known.Code != 200 || unknown.Code != 200 {
		t.Fatalf("known %d, unknown %d: %s", known.Code, unknown.Code, unknown.Body)
	}
	if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
		t.Fatalf("headers %v vs %v", known.Header(), unknown.Header())
	}
	for _, w := range []*httptest.ResponseRecorder{known, unknown} {
		if _, err := session.ParseLoginChallenge(w.Body.Bytes()); err != nil {
			t.Fatalf("the browser would refuse this answer: %v", err)
		}
	}
}

// TestStartNeverAsksForAUserWithAnEmptyID pins what dbapi/dbtest leaves open.
// A name nobody has has no ID, and a lookup by that empty ID is a call the
// contract never promised an answer for: UserPut already refuses it with
// dbapi.ErrInvalid, and apps/server-db -- a stub today -- may do the same for
// UserGet. Then every unknown name would answer 503 and every known one 200.
// The name is looked up by the name, which is the one form both have.
func TestStartNeverAsksForAUserWithAnEmptyID(t *testing.T) {
	h, fx := newFixture(t)
	h.S.DB = strictID{Store: h.S.DB}
	known, unknown := raw(t, h, fx.PasswordName), raw(t, h, "nobody")
	if known.Code != 200 || unknown.Code != 200 {
		t.Fatalf("known %d, unknown %d: %s", known.Code, unknown.Code, unknown.Body)
	}
	if _, err := session.ParseLoginChallenge(unknown.Body.Bytes()); err != nil {
		t.Fatalf("the browser would refuse the made-up answer: %v", err)
	}
}

func TestStartLocksAfterFailures(t *testing.T) {
	h, _ := newFixture(t)
	// Only login/finish counts failures; start itself must stay answerable so
	// the shell can retry after a lock has passed. A locked name is refused.
	for i := 0; i < 3; i++ {
		failLogin(t, h, "anna")
	}
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"anna"}`))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d retry %q", w.Code, w.Header().Get("Retry-After"))
	}
}

func TestStartRefusesABadName(t *testing.T) {
	h, _ := newFixture(t)
	for _, body := range []string{`{"name":""}`, `{"name":"Anna"}`, `{"name":"a b"}`, `{}`, `{"name":1}`} {
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, body))
		if w.Code != 400 {
			t.Fatalf("%s answered %d", body, w.Code)
		}
	}
}

// start runs login/start and returns the challenge.
func start(t testing.TB, h *Handler, name string) session.LoginChallenge {
	t.Helper()
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+name+`"}`))
	if w.Code != 200 {
		t.Fatalf("start %q answered %d: %s", name, w.Code, w.Body)
	}
	var c session.LoginChallenge
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	return c
}

// raw runs login/start and hands back the whole answer, headers and all.
func raw(t *testing.T, h *Handler, name string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+name+`"}`))
	return w
}

// fields lists the field names of an answer in order, so two answers can be
// compared without comparing the values, which differ by design.
func fields(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("answer: %v", err)
	}
	return strings.Join(slices.Sorted(maps.Keys(m)), ",")
}

// TestStartTakesTheSameTimeForAnUnknownName is the half that needs a clock.
// The factor of two is generous on purpose -- this test may not become flaky on
// a loaded machine; it catches a shortcut that was put back in, not a few
// percent. The null control says what the machine's own spread is, so the test
// can tell "the two names are alike" from "this machine cannot tell anything
// apart today" -- and timed refuses a batch the clock did not see, and one that
// was not answered, neither of which the 25 single requests that stood here did.
//
// The null control is held to the same factor as the comparison itself. Here it
// stayed within about a tenth where the factor allows a doubling, which is what
// makes the factor usable; whoever ever draws the factor tighter has to draw it
// tighter for the null control first, or the test starts reporting the machine
// as a finding.
func TestStartTakesTheSameTimeForAnUnknownName(t *testing.T) {
	h, fx := newFixture(t)
	known, unknown, again := timeThem(t, h, fx.PasswordName, "nobody", fx.PasswordName, 5)
	// The null control first: one name against itself, measured the same way
	// and in the same batches.
	if known > 2*again || again > 2*known {
		t.Fatalf("the same name twice: %v and %v -- this machine cannot measure a factor of two today", known, again)
	}
	if known > 2*unknown || unknown > 2*known {
		t.Fatalf("known %v, unknown %v (the same name twice: %v and %v)", known, unknown, known, again)
	}
}

// timedFloor is the shortest batch this test believes. Below it the clock, not
// the handler, is what was measured, and no comparison of such numbers means
// anything.
const timedFloor = 5 * time.Millisecond

// timeBatch runs runs requests for name and returns how long they took
// altogether and the status of the first. It fails nothing itself, so a test
// can hold it against a name that is refused; timed is where the two answers
// are judged.
func timeBatch(h *Handler, name string, runs int) (time.Duration, int) {
	first := 0
	start := time.Now()
	for i := range runs {
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+name+`"}`))
		if i == 0 {
			first = w.Code
		}
	}
	return time.Since(start), first
}

// timed runs timedRuns requests for name and returns what one of them took.
// Two things are refused: a batch that was not answered, and one the clock did
// not see. Without the first, a handler that refuses both names quickly would
// be compared as though it had answered -- and if it were quick enough, the
// floor would then blame the clock for it.
func timed(t *testing.T, h *Handler, name string) time.Duration {
	t.Helper()
	d, code := timeBatch(h, name, timedRuns)
	if code != http.StatusOK {
		t.Fatalf("%q answered %d: what follows would be the time of a refusal, not of an answer", name, code)
	}
	if d < timedFloor {
		t.Fatalf("%d requests for %q took %v: the clock did not see them", timedRuns, name, d)
	}
	return d / timedRuns
}

// TestTimedSeesAnAnswerThatIsNotOne holds timeBatch to handing the status back.
// A locked name answers 429 in a fraction of the time a challenge takes, so a
// measurement that does not look at the answer would compare two refusals and
// call them alike.
func TestTimedSeesAnAnswerThatIsNotOne(t *testing.T) {
	h, _ := newFixture(t)
	for range 3 {
		failLogin(t, h, "anna")
	}
	if _, code := timeBatch(h, "anna", 8); code != http.StatusTooManyRequests {
		t.Fatalf("a locked name answered %d", code)
	}
	if _, code := timeBatch(h, "anna", 0); code != 0 {
		t.Fatalf("a batch of no requests answered %d", code)
	}
}

// timeThem measures the three names in turn, batches times each, and returns
// the median batch of each. Turn by turn, so a machine that gets slower or
// faster while the test runs does so for all three; the median drops the one
// slow batch a busy machine throws in, which an average would not.
func timeThem(t *testing.T, h *Handler, a, b, c string, batches int) (time.Duration, time.Duration, time.Duration) {
	t.Helper()
	da, db, dc := make([]time.Duration, batches), make([]time.Duration, batches), make([]time.Duration, batches)
	for i := range batches {
		da[i], db[i], dc[i] = timed(t, h, a), timed(t, h, b), timed(t, h, c)
	}
	slices.Sort(da)
	slices.Sort(db)
	slices.Sort(dc)
	return da[batches/2], db[batches/2], dc[batches/2]
}

// jsonPost builds a POST with the right content type.
func jsonPost(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// TestStartSaysComeBackWhenTheDatabaseIsBusy is the assurance of this change,
// and it is the half the swallow of K-2 got wrong.
//
// Swallowing every failure of the one write keeps the two names alike, which
// is right, but it pays for it with a lie to the person at the screen: the
// login that follows fails with E_LOGIN, the browser reads "Anmeldung
// fehlgeschlagen.", and that is the sentence a WRONG PASSWORD gets. The user
// is told something about his own password that is not true, and he will go
// and change it.
//
// "Busy" is the one write failure that may be said out loud, and the reason is
// not that it is the most common: it is a state of the INSTALLATION -- another
// program holds the file (point 48) -- and never of the row that was handed
// in. So it cannot fall differently for the two names, and an answer built on
// it is no oracle. Every other failure of that write stays swallowed, because
// a store that refuses THIS row and takes the other one would tell the names
// apart again (TestStartAnswersTheSameWhenTheNonceCannotBeStored holds that
// half, with an error that is not busy).
//
// It only works because the write is made for EVERY name now: a known name
// and an unknown one both keep a nonce, so both meet the busy database and
// both hear the same thing. That is what TestStartMakesTheSameDatabaseCalls-
// ForEveryName holds above, and without it this test would BE the oracle it
// is meant to shut.
//
// E_UNREACHABLE and not a word of its own: there is none. A code E_BUSY lies
// in frozen shared/wire/wire.go:450-462 -- BLOCKED, P9 of
// docs/vertraege-v1-3-vorschlag.md. What is left without it is the header and
// the reason in the log, both of which point 49 built.
func TestStartSaysComeBackWhenTheDatabaseIsBusy(t *testing.T) {
	h, fx := newFixture(t)
	h.S.DB = busyOnce{Store: h.S.DB}
	log := capturedLog(t)
	known, unknown := raw(t, h, fx.PasswordName), raw(t, h, "nobody")
	for _, c := range []struct {
		name string
		w    *httptest.ResponseRecorder
	}{{"known", known}, {"unknown", unknown}} {
		if c.w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503: %s", c.name, c.w.Code, c.w.Body)
		}
		if got := c.w.Header().Get("Retry-After"); got != strconv.Itoa(httpapi.RetryBusy) {
			t.Errorf("%s: Retry-After %q, want %d", c.name, got, httpapi.RetryBusy)
		}
	}
	// The two answers are the same answer, down to the headers and the body:
	// whatever this path says about a busy database, it says it without
	// saying which of the two names was asked for.
	if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
		t.Errorf("headers %v vs %v", known.Header(), unknown.Header())
	}
	if known.Body.String() != unknown.Body.String() {
		t.Errorf("bodies %q vs %q", known.Body, unknown.Body)
	}
	// The log says busy and not away, which is the whole of the difference a
	// client that reads only the code cannot see -- and it names nothing of
	// the request.
	if s := log.String(); !strings.Contains(s, "reason=db-busy") {
		t.Errorf("the log does not name the reason: %s", s)
	}
	if s := log.String(); strings.Contains(s, fx.PasswordName) || strings.Contains(s, "nobody") {
		t.Errorf("the log names the request: %s", s)
	}
}

// TestStartKeepsTheMadeUpNonceUnderAnIDNobodyHas is the price of the write
// above, and it has to be paid in the open: a name nobody has now keeps a
// nonce too, and that row must not be a nonce for somebody.
//
// It is kept under a fresh secrets.ID -- the same shape a real user ID has, so
// the rows of the two names do not tell them apart inside the database either,
// and no ID a user has. login/finish holds the nonce's ID against the user of
// the certificate it has already verified against the trust list
// (finish.go, OnceUse), so a nonce under an ID of nobody's denies like a nonce
// that was never kept at all.
func TestStartKeepsTheMadeUpNonceUnderAnIDNobodyHas(t *testing.T) {
	h, fx := newFixture(t)
	see := &seeOnce{Store: h.S.DB}
	h.S.DB = see
	if w := raw(t, h, "nobody"); w.Code != 200 {
		t.Fatalf("unknown name: %d %s", w.Code, w.Body)
	}
	got := see.written()
	if len(got) != 1 {
		t.Fatalf("%d nonce rows for one unknown name, want 1: %v", len(got), got)
	}
	// Not empty: an empty ID is what the first version of this path asked
	// rows by, and it is the shape that tells the names apart on a store
	// that refuses it (K-2).
	if got[0] == "" {
		t.Fatal("the made-up nonce was kept under an empty ID")
	}
	v, err := h.S.Trust(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range v.List.Users {
		if u.ID == got[0] {
			t.Fatalf("the made-up nonce was kept under the ID of user %q", u.Name)
		}
	}
	// And it looks like one: same length as the ID of a user that exists, so
	// a row of a made-up challenge is not the odd one out in the table.
	real, known := userByName(v.List, fx.PasswordName)
	if !known {
		t.Fatal("the fixture has no password user")
	}
	if len(got[0]) != len(real.ID) {
		t.Fatalf("the made-up ID is %d characters, a real one %d", len(got[0]), len(real.ID))
	}
}
