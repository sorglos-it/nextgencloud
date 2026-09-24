package login

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestLiveAndLogout(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	live, _, ok := h.Live(ctx, r)
	if !ok || live.Verified.Cert().User != fx.PasswordUser {
		t.Fatalf("live %v %+v", ok, live)
	}
	if len(live.APIKey) != 32 {
		t.Fatalf("api key of %d bytes", len(live.APIKey))
	}

	w := httptest.NewRecorder()
	h.Logout(w, jsonPostCookie(session.PathLogout, cookie))
	if w.Code != 204 {
		t.Fatalf("logout %d", w.Code)
	}
	if _, code, ok := h.Live(ctx, r); ok || code != wire.CodeLogin {
		t.Fatalf("after the logout the session answered %q (open %v)", code, ok)
	}
}

func TestLiveEndsAfterIdleAndLifetime(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	h.S.Now = func() int64 { return testNow + h.S.Cfg.IdleTimeout + 1 }
	if _, code, ok := h.Live(ctx, r); ok || code != wire.CodeLogin {
		t.Fatalf("an idle session answered %q (open %v)", code, ok)
	}
	cookie = loginOnce(t, h, fx)
	r = httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	base := h.S.Now()
	h.S.Now = func() int64 { return base + h.S.Cfg.SessionLifetime + 1 }
	if _, code, ok := h.Live(ctx, r); ok || code != wire.CodeLogin {
		t.Fatalf("a session past its lifetime answered %q (open %v)", code, ok)
	}
}

func TestLiveRefusesABadCookie(t *testing.T) {
	h, _ := newFixture(t)
	for _, value := range []string{"", "not base64url!", "AAAA"} {
		r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
		if value != "" {
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: value})
		}
		if _, code, ok := h.Live(ctx, r); ok || code != wire.CodeLogin {
			t.Fatalf("cookie %q answered %q (taken %v)", value, code, ok)
		}
	}
}

// TestLiveEndsASessionTheConfigNoLongerAllows reaches the one of the three
// ends the test above cannot. login/finish takes no certificate reaching
// further than the lifetime configured at the login, so while that setting
// stands, a session past SessionLifetime is past row.Expires too and the first
// of the three answers it: the second half of TestLiveEndsAfterIdleAndLifetime
// is green with the lifetime and without it. The lifetime is for the
// installation that shortens it, and this is that installation.
//
// The session is asked for twice on the way, so that the idle time is never
// what ends it: Seen moves with every answered request, and the last step is
// well inside the idle time and past the lifetime.
func TestLiveEndsASessionTheConfigNoLongerAllows(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := getCookie(session.PathTrust, cookie)
	h.S.Cfg.SessionLifetime, h.S.Cfg.IdleTimeout = 600, 300
	for _, at := range []int64{200, 400} {
		h.S.Now = func() int64 { return testNow + at }
		if _, _, ok := h.Live(ctx, r); !ok {
			t.Fatalf("the session was gone after %d seconds", at)
		}
	}
	h.S.Now = func() int64 { return testNow + 601 }
	if _, code, ok := h.Live(ctx, r); ok || code != wire.CodeLogin {
		t.Fatalf("a session outlived a shortened lifetime: %q (open %v)", code, ok)
	}
	if _, err := h.S.DB.SessionGet(ctx, tokenOf(t, cookie)); !isNotFound(err) {
		t.Fatalf("the row of the ended session is still there: %v", err)
	}
}

// withdrawPasswordUser signs the version after fx.Signed with the user of
// loginOnce taken out of it. The passkey admin signs: he is an admin of
// version 1 and stays in version 2, so the chain is whole and the one thing
// that changed is that the list no longer holds the user who is logged in.
func withdrawPasswordUser(t *testing.T, h *Handler, fx fixture) {
	t.Helper()
	l := trusttest.Next(t, fx.Signed)
	l.Users = slices.DeleteFunc(l.Users, func(u trust.User) bool { return u.ID == fx.PasswordUser })
	if _, err := h.S.AddVersion(ctx, fx.Passkey.Sign(t, l)); err != nil {
		t.Fatalf("version 2 without the user: %v", err)
	}
}

// TestLiveEndsASessionTheTrustListNoLongerHolds is the third of the three ends
// Live knows, and the one nothing held to a test before. With the error of
// session.VerifyClient dropped, two tests of this package fall, and both were
// laid in the round that closed this hole: this one, and
// TestLiveSeesAWithdrawalAnotherAPIWrote two commits later.
//
// Both halves are asserted, because the two are different promises: the
// request is refused, and the row is gone. A session whose user an admin took
// out of the list must not lie around until its own expiry.
func TestLiveEndsASessionTheTrustListNoLongerHolds(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := getCookie(session.PathTrust, cookie)
	if _, _, ok := h.Live(ctx, r); !ok {
		t.Fatal("the session was not open before the user was withdrawn")
	}
	withdrawPasswordUser(t, h, fx)
	if _, code, ok := h.Live(ctx, r); ok || code != wire.CodeLogin {
		t.Fatalf("a session of a withdrawn user answered %q (open %v)", code, ok)
	}
	if _, err := h.S.DB.SessionGet(ctx, tokenOf(t, cookie)); !isNotFound(err) {
		t.Fatalf("the row of the withdrawn user's session is still there: %v", err)
	}
}

// TestLiveSeesAWithdrawalAnotherAPIWrote holds the order of the two steps: the
// trust list is brought up to date before the session is decided on, not
// after. It takes a second API of the installation to see it at all, because
// the one that writes a version has the new list in its cache already.
//
// The second API shares the database and keeps a cache of its own. It takes
// the session once, which fills that cache, and only then does the admin
// withdraw the user on the first API. Reading the cache instead of the
// database leaves the second API answering out of a list that is one version
// behind -- it keeps the session open and hands out that very list.
func TestLiveSeesAWithdrawalAnotherAPIWrote(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	other := New(state.New(h.S.Cfg, h.S.DB, h.S.Box, h.S.Pin))
	other.S.Now = h.S.Now
	r := getCookie(session.PathTrust, cookie)
	if _, _, ok := other.Live(ctx, r); !ok {
		t.Fatal("the second API refused a session that is open")
	}
	withdrawPasswordUser(t, h, fx)
	if _, code, ok := other.Live(ctx, r); ok || code != wire.CodeLogin {
		t.Fatalf("the second API kept a withdrawn user's session: %q (open %v)", code, ok)
	}
}

// damageProof writes the session's stored proof back as something no parser
// reads. The row is this API's own, so nothing about it is the caller's fault.
func damageProof(t *testing.T, h *Handler, cookie string) {
	t.Helper()
	row, err := h.S.DB.SessionGet(ctx, tokenOf(t, cookie))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	row.Proof = []byte("{")
	if err := h.S.DB.SessionPut(ctx, row); err != nil {
		t.Fatalf("damaged proof: %v", err)
	}
}

// tokenOf is the token of a cookie value, as the database keeps it.
func tokenOf(t *testing.T, cookie string) []byte {
	t.Helper()
	raw, err := enc.B64.DecodeString(cookie)
	if err != nil {
		t.Fatalf("cookie: %v", err)
	}
	return session.TokenHash(raw)
}

// TestAStepLiveCouldNotTakeIsNoLogout holds the answer apart from E_LOGIN. The
// contract keeps that one for a session that is missing or has run out
// (shared/README.md), so a step of Live that could not be taken must not carry
// it: a database that hiccups would otherwise send every user of the
// installation to the login screen, and take the logout that would end the
// session properly away in the same breath. Both paths of this package are
// asked, because both read the answer out of the same call.
//
// The five steps below are every one Live takes that can fail without the
// session being the reason. Three of them are the store not answering, which
// is over when it answers again: E_UNREACHABLE. Two are this API's own row and
// its own key, which a retry does not mend: E_INTERNAL.
func TestAStepLiveCouldNotTakeIsNoLogout(t *testing.T) {
	for _, c := range []struct {
		name   string
		take   func(t *testing.T, h *Handler, cookie string)
		status int
		code   wire.Code
	}{
		{"the session row cannot be read", func(_ *testing.T, h *Handler, _ string) {
			h.S.DB = noSession{h.S.DB}
		}, 503, wire.CodeUnreachable},
		{"the trust list cannot be read", func(_ *testing.T, h *Handler, _ string) {
			h.S.DB = noTrust{h.S.DB}
		}, 503, wire.CodeUnreachable},
		{"the session cannot be touched", func(_ *testing.T, h *Handler, _ string) {
			h.S.DB = noTouch{h.S.DB}
		}, 503, wire.CodeUnreachable},
		{"the stored proof is damaged", damageProof, 500, wire.CodeInternal},
		{"the session key does not open", func(t *testing.T, h *Handler, _ string) {
			box, err := secrets.NewBox(config.NewSecret(bytes.Repeat([]byte{3}, 32)))
			if err != nil {
				t.Fatalf("box: %v", err)
			}
			h.S.Box = box
		}, 500, wire.CodeInternal},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fx := newFixture(t)
			cookie := loginOnce(t, h, fx)
			token := tokenOf(t, cookie)
			// The store of the fixture, kept before a double takes its place:
			// the row is asked for at the end through the one that answers.
			plain := h.S.DB
			c.take(t, h, cookie)
			for _, call := range []struct {
				what string
				run  func(w *httptest.ResponseRecorder)
			}{
				{"GET " + session.PathTrust, func(w *httptest.ResponseRecorder) {
					h.Trust(w, getCookie(session.PathTrust+"?from=1", cookie))
				}},
				{"POST " + session.PathLogout, func(w *httptest.ResponseRecorder) {
					h.Logout(w, jsonPostCookie(session.PathLogout, cookie))
				}},
			} {
				w := httptest.NewRecorder()
				call.run(w)
				if w.Code != c.status {
					t.Fatalf("%s answered %d, want %d", call.what, w.Code, c.status)
				}
				cl, err := wire.ParseClose(w.Body.Bytes())
				if err != nil || cl.Code != c.code {
					t.Fatalf("%s answered %q (%v), want %q", call.what, cl.Code, err, c.code)
				}
			}
			// None of it was an answer about the session, so the row stays.
			if _, err := plain.SessionGet(ctx, token); err != nil {
				t.Fatalf("the session was ended over a step that failed: %v", err)
			}
		})
	}
}

// TestADamagedProofCostsNoLookAtTheList holds the order of two steps of Live:
// the stored proof is read before the trust list is. Reading the proof is one
// json.Unmarshal and one Validate over a row this API wrote itself; reading
// the list is a walk of the trust rows, and what it brings back is wanted
// first by session.VerifyClient, which stands below both. Either order refuses
// the same session, so the order shows in nothing but the cost -- and the cost
// is the whole of why it is this way round.
func TestADamagedProofCostsNoLookAtTheList(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	damageProof(t, h, cookie)
	c := &trustReads{Store: h.S.DB}
	h.S.DB = c
	if _, code, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); ok || code != wire.CodeInternal {
		t.Fatalf("a damaged proof answered %q (open %v)", code, ok)
	}
	if got := c.take(); got != 0 {
		t.Fatalf("a damaged proof cost %d reads of the trust list", got)
	}
}

// lockRows counts the rows the counting of failed logins keeps. They are
// settings like any other, and classes/lock names its two spaces nowhere a
// test can read them -- so they are counted the other way round: the fixture
// and a login that works write one setting between them, the installation ID,
// and every further setting of these tests is a row of that counting.
func lockRows(t *testing.T, h *Handler) int {
	t.Helper()
	all, err := settings(h)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if _, ok := all[dbapi.SettingInstallation]; !ok {
		t.Fatal("the installation ID is no longer a setting: this count no longer means what it says")
	}
	return len(all) - 1
}

// TestPurgeFreesOnlyWhatIsPastItsTime holds the housekeeping round to both
// halves: everything past its time goes, and nothing else does. The three
// kinds are asked for one by one, because Purge stops at the first failure and
// a single count would not say which of them ran.
//
// The one-time value that ran out a moment ago is the room of session.MaxSkew:
// it is past its own expiry and has to survive all the same, or an API whose
// clock runs behind would find a nonce gone that it still accepts.
func TestPurgeFreesOnlyWhatIsPastItsTime(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	day := int64(24 * 3600)
	old := testNow - day
	stale := session.TokenHash([]byte("a session of yesterday"))
	if err := h.S.DB.SessionPut(ctx, dbapi.Session{Token: stale, User: fx.PasswordUser,
		Cert: []byte("{}"), Proof: []byte("{}"), Created: old, Expires: old + 1, Seen: old}); err != nil {
		t.Fatalf("stale session: %v", err)
	}
	for _, v := range []struct {
		value   string
		expires int64
	}{
		{"gone", old + 1},       // ran out yesterday
		{"recent", testNow - 1}, // ran out a moment ago: within the skew
		{"kept", testNow + day}, // runs until tomorrow
	} {
		if err := h.S.DB.OnceCreate(ctx, dbapi.OnceNonce, []byte(v.value), fx.PasswordUser, v.expires); err != nil {
			t.Fatalf("once %s: %v", v.value, err)
		}
	}
	if err := lock.Fail(ctx, h.S.DB, "a name that failed yesterday", old); err != nil {
		t.Fatalf("old failure: %v", err)
	}
	before := lockRows(t, h)
	if err := lock.Fail(ctx, h.S.DB, "a name that failed just now", testNow); err != nil {
		t.Fatalf("fresh failure: %v", err)
	}
	if lockRows(t, h) != before+1 {
		t.Skip("the two names share a bucket, so this run cannot tell the two rows apart")
	}

	if err := h.Purge(ctx); err != nil {
		t.Fatalf("purge: %v", err)
	}

	if _, err := h.S.DB.SessionGet(ctx, stale); !isNotFound(err) {
		t.Fatalf("the session of yesterday is still there: %v", err)
	}
	if _, err := h.S.DB.SessionGet(ctx, tokenOf(t, cookie)); err != nil {
		t.Fatalf("the open session was purged: %v", err)
	}
	if _, err := h.S.DB.OnceUse(ctx, dbapi.OnceNonce, []byte("gone"), testNow); !isNotFound(err) {
		t.Fatalf("the one-time value of yesterday is still there: %v", err)
	}
	if _, err := h.S.DB.OnceUse(ctx, dbapi.OnceNonce, []byte("recent"), testNow-2); err != nil {
		t.Fatalf("a one-time value inside session.MaxSkew was freed: %v", err)
	}
	if _, err := h.S.DB.OnceUse(ctx, dbapi.OnceNonce, []byte("kept"), testNow); err != nil {
		t.Fatalf("the one-time value of tomorrow was freed: %v", err)
	}
	if got := lockRows(t, h); got != 1 {
		t.Fatalf("%d rows of the counting are left, want the one counted just now", got)
	}
}

// TestPurgeRunsBesideRequests puts the housekeeping round next to the requests
// it shares a database with. Purge is the first thing this package means to
// run on a timer, so it is the first that runs while requests are being
// answered -- and it holds the counting of classes/lock over a walk of every
// setting, which every failed login wants for itself.
//
// Without goroutines somewhere in this package, a run under -race says nothing
// about any of that: it reports races it sees, and it sees only what ran. The
// outcome is deterministic all the same, which is why it can be asserted: no
// session of this fixture is past its time, so no order of the calls below may
// end the one that is open.
func TestPurgeRunsBesideRequests(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	const each = 25
	var wg sync.WaitGroup
	fail := make(chan error, each)
	for range each {
		wg.Add(4)
		go func() { defer wg.Done(); fail <- h.Purge(ctx) }()
		go func() {
			defer wg.Done()
			if _, _, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); !ok {
				fail <- errors.New("an open session was refused while Purge ran")
			}
		}()
		go func() {
			defer wg.Done()
			h.Trust(httptest.NewRecorder(), getCookie(session.PathTrust+"?from=1", cookie))
		}()
		// A login that fails counts one failure, which is the one thing that
		// wants the lock Purge holds over its walk.
		go func() {
			defer wg.Done()
			h.Finish(httptest.NewRecorder(), jsonPost(session.PathLoginFinish, `{"cert":"AAAA","proof":{"kind":"password","kid":"AAAAAAAAAAAAAAAAAAAAAA","sig":"AA"}}`))
		}()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatalf("beside the requests: %v", err)
		}
	}
	if _, _, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); !ok {
		t.Fatal("the open session did not survive the housekeeping")
	}
}
