// Package httpapi holds what every answer of the API before the WebSocket
// shares: the status and body of an error (session.HTTPStatus, wire.Error),
// Cache-Control: no-store on every answer, the check of Content-Type, of the
// size of a body and of how long it may take, and the session cookie.
// shared/README.md, "Vor dem WebSocket", is the contract.
//
// Every answer comes from here, the answers to a request that reaches no
// route included: Routes.Mux puts a guard in front of Go's ServeMux, which
// would otherwise answer some requests itself in a shape of its own.
package httpapi

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// maxCookie is the length of a session token: the base64url of
// session.TokenSize random bytes, four characters for every three bytes and
// no padding (enc.B64). A longer value is no token of this API, so it is
// turned away before it costs a look in the database.
const maxCookie = (session.TokenSize*8 + 5) / 6

// readLimit is how long a body may take to arrive. session.MaxRequest bounds
// how much comes, this bounds how long it may trickle: without it one sender
// that drips a byte a minute holds a goroutine of this API for as long as it
// likes, and Body is the one place every body goes through. MaxRequest in
// fifteen seconds is about 35 kbit/s, far under what any link the shell
// reaches the API over delivers -- and short enough that a connection which
// only drips is gone again quickly. The limits of the whole server
// (ReadTimeout, ReadHeaderTimeout, MaxHeaderBytes) belong to the http.Server,
// which this package does not build.
const readLimit = 15 * time.Second

// Answer writes one answer with its status and body.
func Answer(w http.ResponseWriter, status int, body []byte) {
	h := w.Header()
	// No answer of the API may sit in a cache: they all hold session state.
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// NoContent answers 204.
func NoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// Fail answers an error: the status of the contract and the CLOSE payload as
// body, so the shell reads every failure the same way. The contracts are read
// through enc, which is encoding/json/v2, so they are written with it too.
func Fail(w http.ResponseWriter, code wire.Code) {
	b, err := json.Marshal(wire.Error(code))
	if err != nil {
		// wire.Error is a fixed struct of strings, so this cannot happen; the
		// answer stays one the shell can read all the same -- and it gets the
		// status of the code it now names, not of the one that failed, so
		// body and status never say two different things.
		code = wire.CodeInternal
		b = []byte(`{"status":"error","code":"E_INTERNAL"}`)
	}
	Answer(w, session.HTTPStatus(code), b)
}

// FailRetry answers an error and says in seconds when to try again.
func FailRetry(w http.ResponseWriter, code wire.Code, retryAfter int64) {
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
	Fail(w, code)
}

// Body reads the JSON body of a request: Content-Type application/json, at
// most session.MaxRequest bytes and at most readLimit long. It answers itself
// on failure and then reports false.
func Body(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		Fail(w, wire.CodeProtocol)
		return nil, false
	}
	// Both limits stand before the first byte is read. A ResponseWriter that
	// cannot set a deadline says so with http.ErrNotSupported, and that is no
	// reason to turn a request away.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(readLimit))
	// One byte over the limit is enough to tell a body that is too large from
	// one that just fits.
	limit := int64(session.MaxRequest) + 1
	// The room is made once, for what the request says it brings and never for
	// more than the limit. io.ReadAll would start at 512 bytes and double a
	// buffer of its own up to about twice the limit, copying everything it
	// has each time. bytes.Buffer asks for bytes.MinRead more room before
	// every read, so that much is added here once and nothing grows again.
	room := limit
	if n := r.ContentLength; n >= 0 && n < room {
		room = n
	}
	var buf bytes.Buffer
	buf.Grow(int(room) + bytes.MinRead)
	_, err = buf.ReadFrom(http.MaxBytesReader(w, r.Body, limit))
	b := buf.Bytes()
	if err != nil {
		// Only the size is E_TOOLARGE. A body that breaks off in the middle
		// is a broken request, and telling the sender it sent too much would
		// send it away to shorten something that was never too long.
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			Fail(w, wire.CodeTooLarge)
		} else {
			Fail(w, wire.CodeProtocol)
		}
		return nil, false
	}
	if len(b) > session.MaxRequest {
		Fail(w, wire.CodeTooLarge)
		return nil, false
	}
	return b, true
}

// SetCookie sets the session cookie with the attributes of the contract.
// The value is this API's own token, base64url and therefore without the
// separators of the header's grammar, so it cannot bring an attribute of its
// own along; Cookie turns away anything else before it gets here. It is Set
// and not Add: one answer that both sets and deletes the cookie would leave
// the browser to pick, and which line it keeps would decide whether the
// session lives.
func SetCookie(w http.ResponseWriter, value string) {
	w.Header().Set("Set-Cookie", session.CookieName+"="+value+"; "+session.CookieAttributes)
}

// ClearCookie deletes the session cookie.
func ClearCookie(w http.ResponseWriter) {
	w.Header().Set("Set-Cookie", session.CookieName+"=; "+session.CookieAttributes+"; Max-Age=0")
}

// Cookie returns the value of the session cookie. There has to be exactly one
// of that name: r.Cookie hands the first of several back, and a neighbouring
// host or another path of this host can set a second one, which would then be
// taken as this API's session without a word.
//
// Only what could be a token of this API passes: the base64url of at most
// session.TokenSize bytes. The cookie header is wider than that -- it carries
// a space or a comma, and it hands a quoted value back without its quotes --
// and such a value would go on into the Set-Cookie header of the next answer,
// where it no longer means to every reader what it meant here: Go's own
// ParseSetCookie hands a value of one space back as an empty one.
func Cookie(r *http.Request) (string, bool) {
	all := r.CookiesNamed(session.CookieName)
	if len(all) != 1 {
		return "", false
	}
	v := all[0].Value
	if v == "" || len(v) > maxCookie {
		return "", false
	}
	// enc.B64 is the only base64url of the contracts and it alone checks the
	// alphabet; encoding/base64 would skip CR and LF.
	if _, err := enc.B64.DecodeString(v); err != nil {
		return "", false
	}
	return v, true
}
