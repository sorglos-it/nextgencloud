package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// FuzzBody feeds Body with any Content-Type and any body. What it takes is
// the body unchanged and within session.MaxRequest, and it says so without
// writing a word; what it refuses is answered by the contract: 400 or 413,
// Cache-Control no-store and a CLOSE payload wire.ParseClose reads, whose
// code carries exactly that status.
func FuzzBody(f *testing.F) {
	f.Add("application/json", []byte(`{"name":"a"}`))
	f.Add("application/json; charset=utf-8", []byte(`{}`))
	f.Add("text/plain", []byte(`{}`))
	f.Add("", []byte{})
	f.Add("application/json", bytes.Repeat([]byte("x"), session.MaxRequest+1))
	f.Fuzz(func(t *testing.T, ctype string, body []byte) {
		r := httptest.NewRequest(http.MethodPost, session.PathLoginStart, bytes.NewReader(body))
		r.Header.Set("Content-Type", ctype)
		w := httptest.NewRecorder()
		b, ok := Body(w, r)
		if ok {
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
// one SetCookie writes back unchanged and without an attribute of its own:
// that is what keeps a value out of the header's grammar.
func FuzzCookie(f *testing.F) {
	f.Add(session.CookieName + "=abc")
	f.Add(session.CookieName + "=")
	f.Add("other=abc; " + session.CookieName + "=xy")
	f.Add(session.CookieName + "=a; Domain=example.org")
	f.Add(session.CookieName + "=" + strings.Repeat("a", 4*session.TokenSize))
	// Found by fuzzing: a quoted value loses its quotes and keeps the space.
	f.Add(session.CookieName + `=" "`)
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
