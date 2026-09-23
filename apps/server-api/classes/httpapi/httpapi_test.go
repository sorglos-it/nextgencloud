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
	"time"

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
		// The damage of our own rows is never the caller's: whoever hears
		// "signature invalid" would sign again over a broken database.
		{state.ErrDamaged, wire.CodeInternal, wire.CodeInternal},
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
		if got := Fault(c.err, FromRequest); got != c.take {
			t.Fatalf("request %v: %q instead of %q", c.err, got, c.take)
		}
		if got := Fault(c.err, FromRows); got != c.read {
			t.Fatalf("rows %v: %q instead of %q", c.err, got, c.read)
		}
		// An origin nobody named must never be the one that answers 403: the
		// mistake would be silent and it would send the shell looking for a
		// fault of its own.
		if got := Fault(c.err, Origin("")); got == wire.CodeVerify {
			t.Fatalf("%v answers %q without an origin", c.err, got)
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
	if got := Fault(fmt.Errorf("add version: %w", state.ErrFork), FromRequest); got != wire.CodeVerify {
		t.Fatalf("wrapped fork: %q", got)
	}
	if got := Fault(fmt.Errorf("chain: %w", state.ErrOldVersion), FromRows); got != wire.CodeNotFound {
		t.Fatalf("wrapped old version: %q", got)
	}
}

// everyRoute wires all six routes to one handler, so a test can see whether
// any of them was reached.
func everyRoute(h http.HandlerFunc) Routes {
	return Routes{Setup: h, Login: h, Finish: h, Logout: h, Trust: h, Socket: h}
}

// TestMuxAnswersAnUncleanPathItself proves that no answer of this package is
// left to Go's ServeMux. It cleans a path of its own accord and answers the
// difference with a redirect whose HTML body gives the query string of the
// request back, without Cache-Control: no-store, without nosniff and without
// a CLOSE payload; "*" of OPTIONS * it answers with a bare 400. Both go past
// everything this package promises, and the first one quotes the input.
func TestMuxAnswersAnUncleanPathItself(t *testing.T) {
	reached := false
	mux := everyRoute(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		NoContent(w)
	}).Mux()
	for _, target := range []string{
		"//" + strings.TrimPrefix(session.PathSetup, "/") + "?code=secret",
		"/api/v1/./setup",
		"/api/v1/trust/../setup",
		"/api/v1/setup/",
		"*",
	} {
		method := http.MethodGet
		if target == "*" {
			method = http.MethodOptions
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, target, nil))
		if reached {
			t.Fatalf("%q reached a handler", target)
		}
		if w.Code != 404 {
			t.Fatalf("%q answered %d", target, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%q: cache control %q", target, got)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%q: nosniff %q", target, got)
		}
		if w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("%q gave the request back: %q %q", target, w.Header().Get("Location"), w.Body)
		}
		c, err := wire.ParseClose(w.Body.Bytes())
		if err != nil || c.Code != wire.CodeNotFound {
			t.Fatalf("%q: body %q (%v)", target, w.Body, err)
		}
	}
	// A path that is already clean still reaches its handler.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if !reached || w.Code != 204 {
		t.Fatalf("a clean path answered %d, reached %v", w.Code, reached)
	}
}

// TestMuxWithoutHandlers proves that a route nobody wired answers instead of
// panicking. It is a mistake of this program, so it is E_INTERNAL -- and the
// connection stays whole.
func TestMuxWithoutHandlers(t *testing.T) {
	mux := Routes{}.Mux()
	for _, path := range []string{session.PathSetup, session.PathLoginStart, session.PathLoginFinish,
		session.PathLogout, session.PathTrust, session.PathSocket} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != 500 {
			t.Fatalf("%s answered %d", path, w.Code)
		}
		if _, err := wire.ParseClose(w.Body.Bytes()); err != nil {
			t.Fatalf("%s: body %q (%v)", path, w.Body, err)
		}
	}
}

// deadlineWriter is a ResponseWriter that can set a read deadline and
// remembers it; http.NewResponseController finds the method on the writer.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (d *deadlineWriter) SetReadDeadline(t time.Time) error { d.deadline = t; return nil }

// TestBodySetsAReadDeadline proves that the limit on the time is there before
// the first byte is read. session.MaxRequest bounds how much a body brings,
// nothing else bounds how long it may take: a sender that drips one byte a
// minute holds a goroutine of this API for as long as it likes.
func TestBodySetsAReadDeadline(t *testing.T) {
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodPost, session.PathLoginStart, strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	before := time.Now()
	if _, ok := Body(w, r); !ok {
		t.Fatalf("good body refused with %d", w.Code)
	}
	if w.deadline.IsZero() {
		t.Fatal("read the body without a deadline")
	}
	if w.deadline.Before(before) || w.deadline.After(time.Now().Add(readLimit)) {
		t.Fatalf("deadline %v, now %v, limit %v", w.deadline, before, readLimit)
	}
}

// offeredReader hands its bytes over and remembers how much room it was
// offered on the first read and how often it was read at all.
type offeredReader struct {
	r     *strings.Reader
	first int
	reads int
}

func (o *offeredReader) Read(p []byte) (int, error) {
	if o.reads == 0 {
		o.first = len(p)
	}
	o.reads++
	return o.r.Read(p)
}

// TestBodyMakesItsRoomOnce proves the room for the whole body stands before
// the first byte is read, instead of a buffer growing under it. The reader is
// offered the whole body at once and hands it over at once, so two reads are
// all it takes: one that fills, one that finds the end. io.ReadAll offers 512
// bytes first and moves to a new, bigger piece a dozen times over, then
// copies everything into a right-sized slice -- about twice the limit
// allocated and every byte moved again, for every body.
//
// What the reader is offered, how often it is read and the room that comes
// back are the promise itself. They say the same on every platform and under
// the race detector, which no count of allocated bytes does.
func TestBodyMakesItsRoomOnce(t *testing.T) {
	const most = session.MaxRequest + 1 + bytes.MinRead
	// A body of nothing is read once, every other one twice.
	for _, size := range []int{0, 12, 1 << 10, session.MaxRequest} {
		src := &offeredReader{r: strings.NewReader(strings.Repeat("x", size))}
		r := httptest.NewRequest(http.MethodPost, session.PathLoginStart, src)
		// net/http hands no more body over than Content-Length says, so this
		// is what the room is made for.
		r.ContentLength = int64(size)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		b, ok := Body(w, r)
		if !ok || len(b) != size {
			t.Fatalf("body of %d: %d %v", size, len(b), ok)
		}
		if src.first < size {
			t.Errorf("a body of %d bytes was first read into room for %d", size, src.first)
		}
		if src.reads > 2 {
			t.Errorf("a body of %d bytes handed over at once took %d reads", size, src.reads)
		}
		if cap(b) > size+bytes.MinRead || cap(b) > most {
			t.Errorf("a body of %d bytes came back with room for %d", size, cap(b))
		}
	}
	// A request that does not know its own length (chunked) says -1, and then
	// the room is the limit -- once.
	src := &offeredReader{r: strings.NewReader(strings.Repeat("x", session.MaxRequest))}
	r := httptest.NewRequest(http.MethodPost, session.PathLoginStart, src)
	r.ContentLength = -1
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b, ok := Body(w, r)
	if !ok || len(b) != session.MaxRequest {
		t.Fatalf("body without a length: %d %v", len(b), ok)
	}
	if src.first < session.MaxRequest || src.reads > 2 || cap(b) > most {
		t.Fatalf("body without a length: room for %d at the first read, %d reads, %d in the end",
			src.first, src.reads, cap(b))
	}
}

// TestCookieTurnsAwayASecondSession proves that a second cookie of this name
// is no session at all. r.Cookie hands the first one back, and a neighbouring
// host or another path of the same host may set one: it would be taken as
// this API's session without a word.
func TestCookieTurnsAwayASecondSession(t *testing.T) {
	token := enc.B64.EncodeToString(bytes.Repeat([]byte{7}, session.TokenSize))
	other := enc.B64.EncodeToString(bytes.Repeat([]byte{8}, session.TokenSize))
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.Header.Set("Cookie", session.CookieName+"="+token+"; "+session.CookieName+"="+other)
	if v, ok := Cookie(r); ok {
		t.Fatalf("took %q out of two in one header", v)
	}
	// A browser may send them in two headers just as well.
	r = httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.Header.Add("Cookie", session.CookieName+"="+token)
	r.Header.Add("Cookie", "other=x; "+session.CookieName+"="+other)
	if v, ok := Cookie(r); ok {
		t.Fatalf("took %q out of two headers", v)
	}
	// One session among other cookies stays one.
	r = httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.Header.Set("Cookie", "other=x; "+session.CookieName+"="+token+"; more=y")
	if v, ok := Cookie(r); !ok || v != token {
		t.Fatalf("turned away the one session: %q %v", v, ok)
	}
}

// TestCookieIsWrittenOnce proves one answer never carries two Set-Cookie
// lines: with two of them the browser keeps whichever it reads last, and
// which that is would decide whether a session is set or deleted.
func TestCookieIsWrittenOnce(t *testing.T) {
	w := httptest.NewRecorder()
	SetCookie(w, "abc")
	ClearCookie(w)
	set := w.Header().Values("Set-Cookie")
	if len(set) != 1 || !strings.Contains(set[0], "Max-Age=0") {
		t.Fatalf("Set-Cookie %q", set)
	}
	w = httptest.NewRecorder()
	ClearCookie(w)
	SetCookie(w, "abc")
	set = w.Header().Values("Set-Cookie")
	if len(set) != 1 || strings.Contains(set[0], "Max-Age=0") {
		t.Fatalf("Set-Cookie %q", set)
	}
}
