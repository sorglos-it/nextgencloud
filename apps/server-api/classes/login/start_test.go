package login

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/core"
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
	// 3. Time does not tell them apart either: session.Challenge walks the
	//    whole list and builds the made-up answer both ways, so the medians of
	//    many runs stay within a factor of two. The factor is generous on
	//    purpose — this test may not become flaky on a loaded machine; it only
	//    catches an early return that was put back in.
	a, b := median(t, h, fx.PasswordName), median(t, h, "nobody")
	if a > 2*b || b > 2*a {
		t.Fatalf("known %v, unknown %v", a, b)
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
func start(t *testing.T, h *Handler, name string) session.LoginChallenge {
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

// median times 25 runs and returns the middle one. The median ignores the one
// slow run a busy machine throws in, which an average would not.
func median(t *testing.T, h *Handler, name string) time.Duration {
	t.Helper()
	d := make([]time.Duration, 25)
	for i := range d {
		start := time.Now()
		raw(t, h, name)
		d[i] = time.Since(start)
	}
	slices.Sort(d)
	return d[len(d)/2]
}

// jsonPost builds a POST with the right content type.
func jsonPost(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
