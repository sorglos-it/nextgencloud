package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestFailUsesTheContract(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, wire.CodeLogin)
	if w.Code != session.HTTPStatus(wire.CodeLogin) {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control %q", got)
	}
	var c wire.Close
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("body: %v", err)
	}
	if c.Status != wire.StatusError || c.Code != wire.CodeLogin || c.Message != wire.CodeLogin.Message() {
		t.Fatalf("body %+v", c)
	}
}

func TestFailRetryTellsWhen(t *testing.T) {
	w := httptest.NewRecorder()
	FailRetry(w, wire.CodeLocked, 42)
	if w.Code != 429 || w.Header().Get("Retry-After") != "42" {
		t.Fatalf("status %d retry %q", w.Code, w.Header().Get("Retry-After"))
	}
}

func TestBodyChecksTypeAndSize(t *testing.T) {
	good := httptest.NewRequest(http.MethodPost, session.PathLoginStart, strings.NewReader(`{"name":"a"}`))
	good.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if b, ok := Body(w, good); !ok || string(b) != `{"name":"a"}` {
		t.Fatalf("good body: %q %v", b, ok)
	}

	wrong := httptest.NewRequest(http.MethodPost, session.PathLoginStart, strings.NewReader(`{}`))
	wrong.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	if _, ok := Body(w, wrong); ok || w.Code != 400 {
		t.Fatalf("wrong type: %v %d", ok, w.Code)
	}

	big := httptest.NewRequest(http.MethodPost, session.PathLoginStart,
		strings.NewReader(strings.Repeat("x", session.MaxRequest+1)))
	big.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	if _, ok := Body(w, big); ok || w.Code != 413 {
		t.Fatalf("large body: %v %d", ok, w.Code)
	}
}

func TestCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetCookie(w, "abc")
	got := w.Header().Get("Set-Cookie")
	want := session.CookieName + "=abc; " + session.CookieAttributes
	if got != want {
		t.Fatalf("cookie %q", got)
	}
	w = httptest.NewRecorder()
	ClearCookie(w)
	if got := w.Header().Get("Set-Cookie"); !strings.Contains(got, "Max-Age=0") {
		t.Fatalf("clearing cookie %q", got)
	}
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.Header.Set("Cookie", session.CookieName+"=abc")
	if v, ok := Cookie(r); !ok || v != "abc" {
		t.Fatalf("read cookie %q %v", v, ok)
	}
}

func TestMuxAnswersEveryPathAndMethod(t *testing.T) {
	seen := map[string]bool{}
	mark := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { seen[name] = true; NoContent(w) }
	}
	mux := Routes{Setup: mark("setup"), Login: mark("login"), Finish: mark("finish"),
		Logout: mark("logout"), Trust: mark("trust"), Socket: mark("socket")}.Mux()
	cases := []struct {
		method, path, name string
	}{
		{http.MethodGet, session.PathSetup, "setup"},
		{http.MethodPost, session.PathSetup, "setup"},
		{http.MethodPost, session.PathLoginStart, "login"},
		{http.MethodPost, session.PathLoginFinish, "finish"},
		{http.MethodPost, session.PathLogout, "logout"},
		{http.MethodGet, session.PathTrust, "trust"},
		{http.MethodGet, session.PathSocket, "socket"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if !seen[c.name] {
			t.Fatalf("%s %s did not reach %s", c.method, c.path, c.name)
		}
		seen[c.name] = false
	}
	// A method the contract does not list is a protocol error, and an unknown
	// path is not found; neither ever reaches a handler.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, session.PathSetup, nil))
	if w.Code != 400 {
		t.Fatalf("DELETE answered %d", w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil))
	if w.Code != 404 {
		t.Fatalf("unknown path answered %d", w.Code)
	}
}

// tornBody stands for a body that ends before it is read: a client that hangs
// up, or a Content-Length that promises more than it sends.
type tornBody struct{}

func (tornBody) Read([]byte) (int, error) { return 0, errors.New("torn off") }

func TestBodyTellsTornFromTooLarge(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, session.PathLoginStart, tornBody{})
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if _, ok := Body(w, r); ok || w.Code != 400 {
		t.Fatalf("torn body: %v %d", ok, w.Code)
	}
	// Twice the limit is the other side of the same read: there the answer
	// has to name the size.
	big := httptest.NewRequest(http.MethodPost, session.PathLoginStart,
		strings.NewReader(strings.Repeat("x", 2*session.MaxRequest)))
	big.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	if _, ok := Body(w, big); ok || w.Code != 413 {
		t.Fatalf("twice the limit: %v %d", ok, w.Code)
	}
}

func TestCookieTurnsAwayWhatIsNoToken(t *testing.T) {
	// A quoted value comes back without its quotes, so a space or a comma
	// inside it would go on into the Set-Cookie header of the next answer and
	// come back from there as something else.
	for _, header := range []string{
		session.CookieName + "=" + strings.Repeat("a", 4*session.TokenSize),
		session.CookieName + `=" a"`,
		session.CookieName + `="a,b"`,
		session.CookieName + "=ä",
	} {
		r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
		r.Header.Set("Cookie", header)
		if v, ok := Cookie(r); ok {
			t.Fatalf("took %q as the token", v)
		}
	}
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	if _, ok := Cookie(r); ok {
		t.Fatal("took a missing cookie")
	}
	// A whole token is what the API itself hands out, and it has to pass.
	token := enc.B64.EncodeToString(bytes.Repeat([]byte{7}, session.TokenSize))
	r = httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.Header.Set("Cookie", session.CookieName+"="+token)
	if v, ok := Cookie(r); !ok || v != token {
		t.Fatalf("turned away a whole token: %q %v", v, ok)
	}
}

func TestFaultOfTheState(t *testing.T) {
	cases := []struct {
		err        error
		take, read wire.Code
	}{
		// The number comes out of the request on both paths, never out of a
		// row, so it is a wrong request and not a damaged database.
		{state.ErrBadVersion, wire.CodeProtocol, wire.CodeProtocol},
		// A second chain under the same number is only ever handed in.
		{state.ErrFork, wire.CodeVerify, wire.CodeInternal},
		// Handed in it is another installation's list; read it is our own
		// version 1 against our own ID.
		{state.ErrInstallation, wire.CodeVerify, wire.CodeInternal},
		// Handed in it is a replayed or skipping version; read it is a
		// number above the newest, which the contract answers 404.
		{state.ErrOldVersion, wire.CodeVerify, wire.CodeNotFound},
		{state.ErrNoTrust, wire.CodeNotFound, wire.CodeNotFound},
		{state.ErrBusy, wire.CodeInternal, wire.CodeInternal},
		{state.ErrSetting, wire.CodeInternal, wire.CodeInternal},
		{errors.New("some other fault"), wire.CodeInternal, wire.CodeInternal},
	}
	for _, c := range cases {
		if got := TakeFault(c.err); got != c.take {
			t.Fatalf("take %v: %q instead of %q", c.err, got, c.take)
		}
		if got := ReadFault(c.err); got != c.read {
			t.Fatalf("read %v: %q instead of %q", c.err, got, c.read)
		}
		// Every code must carry a message, or the answer would be a CLOSE
		// payload the shell refuses.
		for _, code := range []wire.Code{c.take, c.read} {
			if err := wire.Error(code).Validate(); err != nil {
				t.Fatalf("code %q is no CLOSE payload: %v", code, err)
			}
		}
	}
	// A wrapped error keeps its code: the state hands its errors on through
	// callers that add their own words.
	if got := TakeFault(fmt.Errorf("add version: %w", state.ErrFork)); got != wire.CodeVerify {
		t.Fatalf("wrapped fork: %q", got)
	}
	if got := ReadFault(fmt.Errorf("chain: %w", state.ErrOldVersion)); got != wire.CodeNotFound {
		t.Fatalf("wrapped old version: %q", got)
	}
}
