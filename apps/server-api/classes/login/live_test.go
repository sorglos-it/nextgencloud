package login

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func TestLiveAndLogout(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	live, ok := h.Live(ctx, r)
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
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("the session survived the logout")
	}
}

func TestLiveEndsAfterIdleAndLifetime(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	h.S.Now = func() int64 { return testNow + h.S.Cfg.IdleTimeout + 1 }
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("an idle session stayed alive")
	}
	cookie = loginOnce(t, h, fx)
	r = httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	base := h.S.Now()
	h.S.Now = func() int64 { return base + h.S.Cfg.SessionLifetime + 1 }
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("a session outlived its lifetime")
	}
}

func TestLiveRefusesABadCookie(t *testing.T) {
	h, _ := newFixture(t)
	for _, value := range []string{"", "not base64url!", "AAAA"} {
		r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
		if value != "" {
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: value})
		}
		if _, ok := h.Live(ctx, r); ok {
			t.Fatalf("cookie %q was taken", value)
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
		if _, ok := h.Live(ctx, r); !ok {
			t.Fatalf("the session was gone after %d seconds", at)
		}
	}
	h.S.Now = func() int64 { return testNow + 601 }
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("a session outlived a lifetime the installation had shortened")
	}
	raw, err := enc.B64.DecodeString(cookie)
	if err != nil {
		t.Fatalf("cookie: %v", err)
	}
	if _, err := h.S.DB.SessionGet(ctx, session.TokenHash(raw)); !isNotFound(err) {
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
// Live knows, and the one nothing held to a test before: with the error of
// session.VerifyClient dropped, every other test of this package stayed green.
//
// Both halves are asserted, because the two are different promises: the
// request is refused, and the row is gone. A session whose user an admin took
// out of the list must not lie around until its own expiry.
func TestLiveEndsASessionTheTrustListNoLongerHolds(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := getCookie(session.PathTrust, cookie)
	if _, ok := h.Live(ctx, r); !ok {
		t.Fatal("the session was not open before the user was withdrawn")
	}
	withdrawPasswordUser(t, h, fx)
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("a session of a user the trust list no longer holds stayed open")
	}
	raw, err := enc.B64.DecodeString(cookie)
	if err != nil {
		t.Fatalf("cookie: %v", err)
	}
	if _, err := h.S.DB.SessionGet(ctx, session.TokenHash(raw)); !isNotFound(err) {
		t.Fatalf("the row of the withdrawn user's session is still there: %v", err)
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
	raw, err := enc.B64.DecodeString(cookie)
	if err != nil {
		t.Fatalf("cookie: %v", err)
	}
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
	if _, err := h.S.DB.SessionGet(ctx, session.TokenHash(raw)); err != nil {
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
			if _, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); !ok {
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
	if _, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); !ok {
		t.Fatal("the open session did not survive the housekeeping")
	}
}
