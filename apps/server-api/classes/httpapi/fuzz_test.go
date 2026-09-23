package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// methods are the methods FuzzBody picks from. They cannot be fuzzed as text:
// httptest.NewRequest builds a request line out of the method and panics on
// anything that is none.
var methods = []string{http.MethodPost, http.MethodGet, http.MethodPut, http.MethodDelete,
	http.MethodPatch, http.MethodOptions, http.MethodHead}

// tornAfter hands the bytes of r over and then breaks off, as a client does
// that hangs up or promises more in Content-Length than it sends.
type tornAfter struct{ r *bytes.Reader }

func (t tornAfter) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil {
		return n, errors.New("torn off")
	}
	return n, nil
}

// FuzzBody feeds Body with any method, any Content-Type and any body, whole
// or broken off in the middle. What it takes is the body unchanged and within
// session.MaxRequest, and it says so without writing a word; what it refuses
// is answered by the contract: 400 or 413, Cache-Control no-store and a CLOSE
// payload wire.ParseClose reads, whose code carries exactly that status. A
// body that breaks off is never taken, and never called too large.
func FuzzBody(f *testing.F) {
	f.Add(0, "application/json", []byte(`{"name":"a"}`), false)
	f.Add(0, "application/json; charset=utf-8", []byte(`{}`), false)
	f.Add(0, "text/plain", []byte(`{}`), false)
	f.Add(1, "", []byte{}, false)
	f.Add(0, "application/json", bytes.Repeat([]byte("x"), session.MaxRequest+1), false)
	// A body that ends before it is read: the only way into the 400 of a read
	// that failed for a reason other than the size.
	f.Add(0, "application/json", []byte(`{"name":`), true)
	f.Add(0, "application/json", []byte{}, true)
	f.Add(0, "application/json", bytes.Repeat([]byte("x"), session.MaxRequest+1), true)
	f.Fuzz(func(t *testing.T, which int, ctype string, body []byte, torn bool) {
		var src io.Reader = bytes.NewReader(body)
		if torn {
			src = tornAfter{r: bytes.NewReader(body)}
		}
		method := methods[((which%len(methods))+len(methods))%len(methods)]
		r := httptest.NewRequest(method, session.PathLoginStart, src)
		r.Header.Set("Content-Type", ctype)
		w := httptest.NewRecorder()
		b, ok := Body(w, r)
		if ok {
			if torn {
				t.Fatal("took a body that broke off")
			}
			if !bytes.Equal(b, body) {
				t.Fatalf("took %d bytes instead of %d", len(b), len(body))
			}
			if len(b) > session.MaxRequest {
				t.Fatalf("took %d bytes, more than the limit", len(b))
			}
			if w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "" {
				t.Fatal("answered although it took the body")
			}
			return
		}
		if w.Code != 400 && w.Code != 413 {
			t.Fatalf("refused with status %d", w.Code)
		}
		// Only the size is 413: telling a sender it sent too much would send
		// it away to shorten something that was never too long.
		if torn && len(body) <= session.MaxRequest && w.Code != 400 {
			t.Fatalf("a body that broke off answered %d", w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("cache control %q", got)
		}
		c, err := wire.ParseClose(w.Body.Bytes())
		if err != nil {
			t.Fatalf("body is no CLOSE payload: %v", err)
		}
		if c.Status != wire.StatusError || session.HTTPStatus(c.Code) != w.Code {
			t.Fatalf("code %q with status %d", c.Code, w.Code)
		}
	})
}

// FuzzCookie feeds Cookie with any cookie header. A value it takes must be
// the only one of its name in the header and one SetCookie writes back
// unchanged and without an attribute of its own: that is what keeps a value
// out of the header's grammar.
func FuzzCookie(f *testing.F) {
	f.Add(session.CookieName + "=abc")
	f.Add(session.CookieName + "=")
	f.Add("other=abc; " + session.CookieName + "=xy")
	f.Add(session.CookieName + "=a; Domain=example.org")
	f.Add(session.CookieName + "=" + strings.Repeat("a", 4*session.TokenSize))
	// Found by fuzzing: a quoted value loses its quotes and keeps the space.
	f.Add(session.CookieName + `=" "`)
	// A second cookie of the same name, as a neighbouring host or another
	// path of this host sets it.
	f.Add(session.CookieName + "=abc; " + session.CookieName + "=xyz")
	f.Fuzz(func(t *testing.T, header string) {
		r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
		r.Header.Set("Cookie", header)
		v, ok := Cookie(r)
		if !ok {
			return
		}
		if v == "" || len(v) > maxCookie {
			t.Fatalf("took a value of %d characters", len(v))
		}
		if n := len(r.CookiesNamed(session.CookieName)); n != 1 {
			t.Fatalf("took one of %d cookies of that name", n)
		}
		w := httptest.NewRecorder()
		SetCookie(w, v)
		set := w.Header().Values("Set-Cookie")
		if len(set) != 1 {
			t.Fatalf("value wrote %d cookies", len(set))
		}
		c, err := http.ParseSetCookie(set[0])
		if err != nil {
			t.Fatalf("value broke the cookie: %v", err)
		}
		if c.Name != session.CookieName || c.Value != v {
			t.Fatalf("wrote %q=%q", c.Name, c.Value)
		}
		if c.Path != "/api/" || c.Domain != "" || c.MaxAge != 0 || !c.Secure || !c.HttpOnly ||
			c.SameSite != http.SameSiteStrictMode {
			t.Fatalf("value changed the attributes: %q", set[0])
		}
	})
}
