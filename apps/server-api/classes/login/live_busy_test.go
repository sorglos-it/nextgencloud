package login

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// busySession is a store whose three session calls cannot get through because
// another program holds the database -- the state of the INSTALLATION of
// point 48, carrying dbwait.ErrBusy the way package sqlstore hands the named
// cause of the deadline back (point 49).
type busySession struct{ dbapi.Store }

func (busySession) SessionGet(context.Context, []byte) (dbapi.Session, error) {
	return dbapi.Session{}, fmt.Errorf("sqlstore: the read did not get through: %w", dbwait.ErrBusy)
}

func (busySession) SessionDelete(context.Context, []byte) error {
	return fmt.Errorf("sqlstore: the write did not get through: %w", dbwait.ErrBusy)
}

// strangerCookie is a token of the right shape this API never handed out. It
// is what the far side of every "does this one exist" question looks like on
// the session paths, and the answer to it has to be the answer a real cookie
// gets.
func strangerCookie(t *testing.T) string {
	t.Helper()
	b := make([]byte, session.TokenSize)
	for i := range b {
		b[i] = byte(i)
	}
	return enc.B64.EncodeToString(b)
}

// TestTrustSaysComeBackWhenTheDatabaseIsBusy is the assurance of this change,
// at the route a session really walks.
//
// Live hands its caller a code and no error (login/live.go:313 before this
// change), so GET session.PathTrust and the WebSocket both end in
// httpapi.Fail and cannot set a Retry-After. The route answers 503 either
// way, which is the one thing a client cannot read: "come back in a moment"
// and "the service is away" are the same answer to it. That is the whole of
// what is measured here -- the header, not the code. The code stays
// E_UNREACHABLE because a word of its own lies in frozen
// shared/wire/wire.go:450-462 (BLOCKED, P9).
func TestTrustSaysComeBackWhenTheDatabaseIsBusy(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	h.S.DB = busySession{Store: h.S.DB}
	log := capturedLog(t)
	w := httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=1", cookie))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Retry-After"); got != strconv.Itoa(httpapi.RetryBusy) {
		t.Errorf("Retry-After %q, want %d", got, httpapi.RetryBusy)
	}
	if s := log.String(); !strings.Contains(s, "reason=db-busy") {
		t.Errorf("the log does not name the reason: %s", s)
	}
}

// TestLogoutSaysComeBackWhenTheDatabaseIsBusy is the same assurance at the
// second of the two paths that read a session row.
func TestLogoutSaysComeBackWhenTheDatabaseIsBusy(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	h.S.DB = busySession{Store: h.S.DB}
	w := httptest.NewRecorder()
	h.Logout(w, jsonPostCookie(session.PathLogout, cookie))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Retry-After"); got != strconv.Itoa(httpapi.RetryBusy) {
		t.Errorf("Retry-After %q, want %d", got, httpapi.RetryBusy)
	}
}

// TestABusySessionPathAnswersTheSameForEveryCookie holds the half that the
// answer above must not buy: a header that a cookie this API handed out gets
// and a made-up one does not would be an oracle for session tokens, and the
// route that reads the most rows of the installation is the wrong place to
// open one.
//
// It is numbers and not an argument, the way
// TestStartMakesTheSameDatabaseCallsForEveryName is: status, every header with
// every value (maps.EqualFunc) and the body, for both paths and both cookies.
// The reason it can hold at all is the same reason login/start may answer busy
// out loud -- busy is a state of the installation and not of the row that was
// asked for, so the read fails before it ever reaches a row.
func TestABusySessionPathAnswersTheSameForEveryCookie(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	stranger := strangerCookie(t)
	h.S.DB = busySession{Store: h.S.DB}
	for _, route := range []struct {
		name string
		call func(w http.ResponseWriter, c string)
	}{
		{"trust", func(w http.ResponseWriter, c string) {
			h.Trust(w, getCookie(session.PathTrust+"?from=1", c))
		}},
		{"logout", func(w http.ResponseWriter, c string) {
			h.Logout(w, jsonPostCookie(session.PathLogout, c))
		}},
	} {
		known, unknown := httptest.NewRecorder(), httptest.NewRecorder()
		route.call(known, cookie)
		route.call(unknown, stranger)
		if known.Code != unknown.Code {
			t.Errorf("%s: status %d vs %d", route.name, known.Code, unknown.Code)
		}
		if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
			t.Errorf("%s: headers %v vs %v", route.name, known.Header(), unknown.Header())
		}
		if known.Body.String() != unknown.Body.String() {
			t.Errorf("%s: bodies %q vs %q", route.name, known.Body, unknown.Body)
		}
	}
}

// busyLock is a store whose counting rows cannot be read because another
// program holds the database. Only the counting: everything else answers, so
// the case reaches the step it means and does not measure an earlier wall.
type busyLock struct{ dbapi.Store }

func (b busyLock) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if strings.HasPrefix(id, "lock.") || strings.HasPrefix(id, "lockkey.") {
		return dbapi.Blob{}, fmt.Errorf("sqlstore: the read did not get through: %w", dbwait.ErrBusy)
	}
	return b.Store.BlobGet(ctx, kind, id)
}

// TestFinishSaysComeBackWhenTheDatabaseIsBusy is the third of the paths point
// 52 left open. login/finish wrote its E_UNREACHABLE down itself and answered
// with httpapi.Fail, so none of its five store failures could carry the
// header either.
//
// The step measured is the counting of failed logins, which is the first of
// the five a request really reaches -- and the one a key the trust list does
// not hold reaches too, which is why the second half below can hold.
func TestFinishSaysComeBackWhenTheDatabaseIsBusy(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	h.S.DB = busyLock{Store: h.S.DB}
	// Die Zaehler von httpapi sind Zustand des Prozesses, und die Zeile hat
	// seit der Bremse ein Fenster: ohne dieses Zuruecksetzen misst der Fall
	// die Stille eines frueheren.
	httpapi.ResetCountsForTest()
	log := capturedLog(t)
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, fx.Password, c, ""))))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Retry-After"); got != strconv.Itoa(httpapi.RetryBusy) {
		t.Errorf("Retry-After %q, want %d", got, httpapi.RetryBusy)
	}
	if s := log.String(); !strings.Contains(s, "reason=db-busy") {
		t.Errorf("the log does not name the reason: %s", s)
	}
}

// TestABusyFinishAnswersTheSameForEveryKey is the half that must not be
// bought: the counting is the one of the five steps that picks its row by
// whether the trust list holds the key (lock.Check against lock.CheckKey), so
// it is the one place a new oracle could open. Numbers and not an argument --
// status, every header with every value, body -- for a key the list holds and
// one it does not.
func TestABusyFinishAnswersTheSameForEveryKey(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	stranger := start(t, h, fx.PasswordName)
	h.S.DB = busyLock{Store: h.S.DB}
	known := httptest.NewRecorder()
	h.Finish(known, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, fx.Password, c, ""))))
	// A signer the trust list has never seen: its key ID names nobody, so
	// login/finish counts against the key and not against a name.
	unknown := httptest.NewRecorder()
	h.Finish(unknown, jsonPost(session.PathLoginFinish,
		string(finishBody(t, fx, trusttest.NewSigner(42), stranger, ""))))
	if known.Code != unknown.Code {
		t.Errorf("status %d vs %d: %s / %s", known.Code, unknown.Code, known.Body, unknown.Body)
	}
	if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
		t.Errorf("headers %v vs %v", known.Header(), unknown.Header())
	}
	if known.Body.String() != unknown.Body.String() {
		t.Errorf("bodies %q vs %q", known.Body, unknown.Body)
	}
}

// TestStartAnswersTheSameWhenTheCountingIsBusy is the fourth path, and the
// step is the one login/start reads FIRST -- the counting of failed logins,
// before the trust list is asked anything. Point 52 gave this route the busy
// answer for the WRITE of the nonce; the two reads before it still answered a
// bare 503.
//
// Both halves in one case, because here they are the same measurement: the
// header is there, and it is there for a name that exists and one that does
// not, down to every header value and the body. The counting keeps its rows
// per bucket and not per name, so the read costs the same for both -- and so
// does its failure.
func TestStartAnswersTheSameWhenTheCountingIsBusy(t *testing.T) {
	h, fx := newFixture(t)
	h.S.DB = busyLock{Store: h.S.DB}
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
	if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
		t.Errorf("headers %v vs %v", known.Header(), unknown.Header())
	}
	if known.Body.String() != unknown.Body.String() {
		t.Errorf("bodies %q vs %q", known.Body, unknown.Body)
	}
}
