package login

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
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
