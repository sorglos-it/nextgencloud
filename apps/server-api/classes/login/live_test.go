package login

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
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

// TestAWithdrawalSurvivesTheRowsBeingDropped is the withdrawal above, put back
// in force by whoever can write the database -- done past the width
// state.checkGap caught before, and caught now.
//
// It holds one form and not the property. Two others cost one statement more
// than this test does and are open: cutting the chain above the hole, and
// lowering the Seq of the row above it. Both were built and both go through,
// so neither can be asserted refused here the way this one is -- what a test
// of them would pin is today's answer, which is that the session comes back.
// state.checkGap says why that is not a fault of this package to mend.
//
// Version 2 takes the user out, ten more versions follow, and then the rows 2
// to 11 are dropped. Version 12 still stands, so nothing is missing at the top
// and no node that is up to date notices anything. A node with an empty cache
// reads version 1, misses 2 and finds nothing in the numbers above it, so it
// takes version 1 for the newest: the list that still holds the user. The
// session of a user an admin withdrew ten versions ago is open again, on every
// node that was restarted since.
//
// It is asked of a cold node, because that is the one that walks the chain up
// from the bottom. The node that wrote the versions holds the newest in its
// cache and asks only above it, where nothing is missing.
func TestAWithdrawalSurvivesTheRowsBeingDropped(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := getCookie(session.PathTrust, cookie)
	l := trusttest.Next(t, fx.Signed)
	l.Users = slices.DeleteFunc(l.Users, func(u trust.User) bool { return u.ID == fx.PasswordUser })
	prev := fx.Passkey.Sign(t, l)
	if _, err := h.S.AddVersion(ctx, prev); err != nil {
		t.Fatalf("version 2 without the user: %v", err)
	}
	for i := 3; i <= 12; i++ {
		prev = fx.Passkey.Sign(t, trusttest.Next(t, prev))
		if _, err := h.S.AddVersion(ctx, prev); err != nil {
			t.Fatalf("version %d: %v", i, err)
		}
	}
	for seq := 2; seq <= 11; seq++ {
		if err := h.S.DB.BlobDelete(ctx, dbapi.BlobTrust, strconv.Itoa(seq)); err != nil {
			t.Fatalf("drop row %d: %v", seq, err)
		}
	}
	cold := New(state.New(h.S.Cfg, h.S.DB, h.S.Box, h.S.Pin))
	cold.S.Now = h.S.Now
	live, code, ok := cold.Live(ctx, r)
	if ok {
		t.Fatalf("the cold node took the session of a withdrawn user and read version %d as the newest",
			live.Trust.List.Seq)
	}
	// The rows contradict each other, which is the installation's damage and
	// not the session's: E_LOGIN here would send the user to the login screen
	// for it, and 404 would tell the shell there is nothing newer to fetch.
	if code != wire.CodeInternal {
		t.Fatalf("the cold node answered %q", code)
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
// installation to the login screen.
//
// GET session.PathTrust is the path that is asked. The logout used to read its
// answer out of this same call and no longer does -- it takes sessionRow, so
// of the five steps below it takes one. For two of the other four,
// TestLogoutEndsASessionTheRestOfLiveCannotRead holds it to the 204.
//
// The five steps below are every one Live takes that can fail without the
// session saying no. Three of them are the store not answering, which is over
// when it answers again: E_UNREACHABLE. Two are this API's own row and its own
// key, which a retry does not mend: E_INTERNAL.
//
// They are not every step that can fail over a row of this API's own: row.Cert
// is a sixth, inside session.VerifyClient, and it is answered the other way
// round on purpose -- E_LOGIN, and the row goes. A certificate that no longer
// reads is a session that can never be proved again, so ending it mends
// itself, where a proof that no longer reads leaves a row nothing can use.
// TestLogoutEndsASessionTheRestOfLiveCannotRead is what keeps that difference
// from costing its owner the session.
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
			w := httptest.NewRecorder()
			h.Trust(w, getCookie(session.PathTrust+"?from=1", cookie))
			if w.Code != c.status {
				t.Fatalf("GET %s answered %d, want %d", session.PathTrust, w.Code, c.status)
			}
			cl, err := wire.ParseClose(w.Body.Bytes())
			if err != nil || cl.Code != c.code {
				t.Fatalf("GET %s answered %q (%v), want %q", session.PathTrust, cl.Code, err, c.code)
			}
			// None of it was an answer about the session, so the row stays.
			if _, err := plain.SessionGet(ctx, token); err != nil {
				t.Fatalf("the session was ended over a step that failed: %v", err)
			}
		})
	}
}

// threeVersions is an installation whose trust list has three versions, with a
// session of the password user open. Three is the fewest that lets a hole sit
// in the middle of the chain, which is where the two nodes below meet it at
// different steps.
func threeVersions(t *testing.T) (*Handler, string) {
	t.Helper()
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	second := fx.Passkey.Sign(t, trusttest.Next(t, fx.Signed))
	if _, err := h.S.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	if _, err := h.S.AddVersion(ctx, fx.Passkey.Sign(t, trusttest.Next(t, second))); err != nil {
		t.Fatalf("version 3: %v", err)
	}
	return h, cookie
}

// TestOneFaultIsOneAnswerOnEveryNode holds the two faults below to one answer
// each, whichever node of the installation is asked.
//
// A version missing from the trust rows is met at two different steps. A node
// with nothing cached walks into it while it brings the list up to date
// (state.Refresh, inside Live): it reads version 1, misses 2 and finds 3 above
// it. A node that is up to date already walks past it there -- it asks only
// above its newest -- and meets it when it reads the rows out for the answer
// (state.Chain). Two steps, and before this two answers for the one hole: 503
// on the cold node, 500 on the warm one. Whoever read that learned which node
// he had reached, not what was wrong with the installation.
//
// The second fault is the other class, and the one httpapi.Fault has to keep
// apart from the first: a store that answers nothing is over when it answers
// again, which is E_UNREACHABLE, and it says so on both nodes. Without that
// arm the one place that picks the code would answer a store that is down like
// a database that is broken.
//
// The third case is the one that stood here as the fault no arm could even
// out: every trust row gone at once. The warm node still holds a list and
// finds its row missing (E_INTERNAL); the cold node found no list at all and
// said state.ErrNoTrust, which is 404 -- the very answer the contract's table
// keeps for a from above the newest version, so the shell was told it was
// ahead of a server that had lost everything.
//
// The sentence that stood here said the contract has a query for neither the
// newest number the store holds nor an anchor outside it. Both halves were
// wrong, and the difference between a limit and an excuse is exactly that:
// dbapi.BlobFrom is the first, and state.checkGap asks it now; trust.Anchor
// with config.LoadAnchor and trust-anchor.json is the second. Neither was
// missing.
//
// What is not wired up is the file, and only the file: config.LoadAnchor,
// config.SaveAnchor and config.AnchorFile have no caller in this project
// outside shared/config, and trust-anchor.json is written on the web machine.
// The type is wired up everywhere -- state.refreshLocked and state.AddVersion
// build a trust.Anchor every time they open a chain, out of the rows
// themselves, and session and tunnel carry one too. That is why an anchor that
// comes from outside is a small change
// later and not a new mechanism, and it is the only thing that would hold
// against a writer of the database (state.checkGap says which forms get past
// what is there now).
//
// What tells the cold node apart needed neither of them in the end, and it
// sits in Live: login/finish asks for the trust list before it writes a
// session row, so a request that came this far with a session proves a list
// stood once.
//
// What this test holds is the answer, and only the answer. The two nodes do
// not agree on what they decided: on the warm node state.Refresh still hands
// Live the cached version and no error, and the 500 comes out of state.Chain
// one step further on. This test asks for a chain, so it sees that step. The
// note at that read in live.go says what a caller who does not see it gets.
func TestOneFaultIsOneAnswerOnEveryNode(t *testing.T) {
	for _, c := range []struct {
		name   string
		damage func(t *testing.T, h *Handler)
		status int
		code   wire.Code
	}{
		{"a version is missing from the rows", func(t *testing.T, h *Handler) {
			if err := h.S.DB.BlobDelete(ctx, dbapi.BlobTrust, "2"); err != nil {
				t.Fatalf("the hole: %v", err)
			}
		}, 500, wire.CodeInternal},
		{"the store answers no trust row at all", func(_ *testing.T, h *Handler) {
			h.S.DB = noTrust{h.S.DB}
		}, 503, wire.CodeUnreachable},
		{"every trust row is gone", func(t *testing.T, h *Handler) {
			for _, id := range []string{"1", "2", "3"} {
				if err := h.S.DB.BlobDelete(ctx, dbapi.BlobTrust, id); err != nil {
					t.Fatalf("drop row %s: %v", id, err)
				}
			}
		}, 500, wire.CodeInternal},
	} {
		t.Run(c.name, func(t *testing.T) {
			warm, cookie := threeVersions(t)
			c.damage(t, warm)
			// The cold node is built after the damage and shares the store, so
			// it meets the same installation with an empty cache.
			cold := New(state.New(warm.S.Cfg, warm.S.DB, warm.S.Box, warm.S.Pin))
			cold.S.Now = warm.S.Now
			for _, node := range []struct {
				what string
				h    *Handler
			}{{"the warm node", warm}, {"the cold node", cold}} {
				w := httptest.NewRecorder()
				node.h.Trust(w, getCookie(session.PathTrust+"?from=1", cookie))
				if w.Code != c.status {
					t.Fatalf("%s answered %d %s, want %d", node.what, w.Code, w.Body, c.status)
				}
				cl, err := wire.ParseClose(w.Body.Bytes())
				if err != nil || cl.Code != c.code {
					t.Fatalf("%s answered %q (%v), want %q", node.what, cl.Code, err, c.code)
				}
			}
		})
	}
}

// TestLogoutEndsASessionTheRestOfLiveCannotRead holds the logout to the one
// thing it is for: ending the session. Removing the row of one's own cookie
// wants neither the trust list nor a proof that still reads back, so neither
// may stand in its way -- and both did. A damaged proof answered 500 and an
// unreadable list 503, the row stayed, and its owner had no way at all to end
// a session that was his.
//
// The two damages stand in one test because they are one rule. Whoever changes
// the logout so that it reads either of them again has to make both of these
// go red, and there is nowhere in between.
//
// The last case is the one step of Live the logout does still take, and the
// reason it cannot simply delete and answer 204: a store that will not say
// whether there is a row leaves it nothing to end, and E_LOGIN would tell the
// shell the one thing about the session that is not so.
func TestLogoutEndsASessionTheRestOfLiveCannotRead(t *testing.T) {
	for _, c := range []struct {
		name   string
		damage func(t *testing.T, h *Handler, cookie string)
		status int
		code   wire.Code // empty where the answer is the 204, which has no body
	}{
		{"the stored proof is damaged", damageProof, 204, ""},
		{"the trust list cannot be read", func(_ *testing.T, h *Handler, _ string) {
			h.S.DB = noTrust{h.S.DB}
		}, 204, ""},
		{"the session row cannot be read", func(_ *testing.T, h *Handler, _ string) {
			h.S.DB = noSession{h.S.DB}
		}, 503, wire.CodeUnreachable},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fx := newFixture(t)
			cookie := loginOnce(t, h, fx)
			token := tokenOf(t, cookie)
			// The store of the fixture, kept before a double takes its place:
			// the row is asked for at the end through the one that answers.
			plain := h.S.DB
			c.damage(t, h, cookie)
			w := httptest.NewRecorder()
			h.Logout(w, jsonPostCookie(session.PathLogout, cookie))
			if w.Code != c.status {
				t.Fatalf("the logout answered %d %s, want %d", w.Code, w.Body, c.status)
			}
			if c.status != 204 {
				cl, err := wire.ParseClose(w.Body.Bytes())
				if err != nil || cl.Code != c.code {
					t.Fatalf("the logout answered %q (%v), want %q", cl.Code, err, c.code)
				}
				// Nothing was said about the session, so the row stays.
				if _, err := plain.SessionGet(ctx, token); err != nil {
					t.Fatalf("the session was ended over a step that failed: %v", err)
				}
				return
			}
			if _, err := plain.SessionGet(ctx, token); !isNotFound(err) {
				t.Fatalf("the session is still there after the logout: %v", err)
			}
			// The cookie goes with the row and not before it: a cookie left
			// behind names a session nobody can reach to end.
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Value != "" {
				t.Fatalf("cookies after the logout: %+v", cookies)
			}
		})
	}
}

// TestADamagedProofCostsNoLookAtTheList holds the order of two steps of Live:
// the stored proof is read before the trust list is. Reading the proof is one
// json.Unmarshal and one Validate over a row this API wrote itself; reading
// the list is a walk of the trust rows, and what it brings back is wanted
// first by session.VerifyClient, which stands below both. The cost is why it
// is this way round.
//
// Either order refuses the same session, and while only one of the two is
// wrong the order shows in nothing but the cost. Where both are wrong at once
// it decides the answer as well, which is the second half below: the step that
// runs first is the one that names the fault. That is not a reason of its own
// to read the proof first -- it only means the cheap step gets to speak, and
// the proof is the one this API wrote itself.
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
	// A damaged proof and a store that will not hand out the list. With the
	// proof read first the answer is about the proof; the other way round it
	// would call the database away over a row that is broken, and the
	// database would be mended by waiting, which this is not.
	h.S.DB = noTrust{h.S.DB}
	if _, code, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); ok || code != wire.CodeInternal {
		t.Fatalf("a damaged proof beside a list nobody can read answered %q (open %v)", code, ok)
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
