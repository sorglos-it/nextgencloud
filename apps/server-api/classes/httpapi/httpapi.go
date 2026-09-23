// Package httpapi holds what every answer of the API before the WebSocket
// shares: the status and body of an error (session.HTTPStatus, wire.Error),
// Cache-Control: no-store on every answer, the check of Content-Type and the
// size of a body, and the session cookie. shared/README.md, "Vor dem
// WebSocket", is the contract.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// maxCookie is the length of a session token: the base64url of
// session.TokenSize random bytes, four characters for every three bytes and
// no padding (enc.B64). A longer value is no token of this API, so it is
// turned away before it costs a look in the database.
const maxCookie = (session.TokenSize*8 + 5) / 6

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
// body, so the shell reads every failure the same way.
func Fail(w http.ResponseWriter, code wire.Code) {
	b, err := json.Marshal(wire.Error(code))
	if err != nil {
		// wire.Error is a fixed struct of strings, so this cannot happen; the
		// answer stays a valid one all the same.
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
// most session.MaxRequest bytes. It answers itself on failure and then
// reports false.
func Body(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		Fail(w, wire.CodeProtocol)
		return nil, false
	}
	// One byte over the limit is enough to tell a body that is too large from
	// one that just fits.
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, session.MaxRequest+1))
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
// own along; Cookie turns away anything else before it gets here.
func SetCookie(w http.ResponseWriter, value string) {
	w.Header().Add("Set-Cookie", session.CookieName+"="+value+"; "+session.CookieAttributes)
}

// ClearCookie deletes the session cookie.
func ClearCookie(w http.ResponseWriter) {
	w.Header().Add("Set-Cookie", session.CookieName+"=; "+session.CookieAttributes+"; Max-Age=0")
}

// Cookie returns the value of the session cookie. Only what could be a token
// of this API passes: the base64url of at most session.TokenSize bytes. The
// cookie header is wider than that -- it carries a space or a comma, and it
// hands a quoted value back without its quotes -- and such a value would go
// on into the Set-Cookie header of the next answer, where it no longer means
// to every reader what it meant here: Go's own ParseSetCookie hands a value
// of one space back as an empty one.
func Cookie(r *http.Request) (string, bool) {
	c, err := r.Cookie(session.CookieName)
	if err != nil || c.Value == "" || len(c.Value) > maxCookie {
		return "", false
	}
	// enc.B64 is the only base64url of the contracts and it alone checks the
	// alphabet; encoding/base64 would skip CR and LF.
	if _, err := enc.B64.DecodeString(c.Value); err != nil {
		return "", false
	}
	return c.Value, true
}
