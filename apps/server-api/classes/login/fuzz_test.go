package login

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// startSeeds returns the bodies FuzzStartBody begins with: two names the trust
// list holds, one it does not, and five shapes the parser turns away. Without
// a body of the right shape among them nothing would ever reach the answer,
// and a green run would say nothing about the path behind the parser.
func startSeeds(fx fixture) [][]byte {
	return [][]byte{
		[]byte(`{"name":"` + fx.PasswordName + `"}`),
		[]byte(`{"name":"` + fx.PasskeyName + `"}`),
		[]byte(`{"name":"nobody"}`),
		[]byte(`{"name":""}`),
		[]byte(`{"name":"Anna"}`),
		[]byte(`{"name":1}`),
		[]byte(`{}`),
		[]byte("null"),
	}
}

// settings returns every setting row with its contents. The counting of failed
// logins keeps its buckets among them, so this is what says whether login/start
// laid a lock on anybody -- by their contents and not by their number, because
// a second failure in a bucket that already has a row writes that row and adds
// none.
func settings(h *Handler) (map[string]string, error) {
	blobs, err := h.S.DB.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(blobs))
	for _, b := range blobs {
		out[b.ID] = string(b.Data)
	}
	return out, nil
}

// shape lists the field names of an answer, sorted, so two answers can be
// held against each other without their values, which differ by design.
func shape(b []byte) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return strings.Join(slices.Sorted(maps.Keys(m)), ",")
}

// realShapes are the shapes the users of the list really produce. Every answer
// of login/start has to be one of them, for a name nobody has too.
func realShapes(tb testing.TB, h *Handler, fx fixture) map[string]bool {
	tb.Helper()
	out := map[string]bool{}
	for _, u := range fx.List.Users {
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+u.Name+`"}`))
		if w.Code != 200 {
			tb.Fatalf("%q answered %d", u.Name, w.Code)
		}
		out[shape(w.Body.Bytes())] = true
	}
	return out
}

// TestStartSeedsAreAnswered is what makes a green fuzz run mean something: the
// seeds are not refusals alone. Three of them are bodies the parser reads, and
// each is answered with a challenge the browser's own parser takes -- a target
// that only ever counted refusals would stay green with a handler that refuses
// every name there is.
func TestStartSeedsAreAnswered(t *testing.T) {
	h, fx := newFixture(t)
	answered := 0
	for _, body := range startSeeds(fx) {
		if _, err := session.ParseLoginStart(body); err != nil {
			continue
		}
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, string(body)))
		if w.Code != 200 {
			t.Fatalf("a body of the right shape answered %d: %q", w.Code, body)
		}
		if _, err := session.ParseLoginChallenge(w.Body.Bytes()); err != nil {
			t.Fatalf("the browser would refuse the answer to %q: %v", body, err)
		}
		answered++
	}
	if answered < 3 {
		t.Fatalf("%d seeds were answered with a challenge, want at least 3", answered)
	}
}

// FuzzStartBody feeds POST session.PathLoginStart with everything. Whatever
// comes, the answer is a status of the contract, a body the parser reads is
// answered with a challenge of a shape this installation really gives out, and
// nothing is left behind: login/start writes no log entry and no setting, so
// asking for a name never locks it.
func FuzzStartBody(f *testing.F) {
	h, fx := newFixture(f)
	for _, body := range startSeeds(fx) {
		f.Add(body)
	}
	shapes := realShapes(f, h, fx)
	// The settings the fixture left behind: only the installation ID. The
	// counting of failed logins writes its rows here too, so anything else
	// here afterwards is a lock login/start laid on somebody. Their contents
	// are held too and not only their number: a failure counted into a bucket
	// that already has a row changes that row and adds none.
	before, err := settings(h)
	if err != nil {
		f.Fatalf("settings: %v", err)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, string(body)))
		switch w.Code {
		case 200, 400, 413, 429, 500, 503:
		default:
			t.Fatalf("status %d for %q", w.Code, body)
		}
		req, err := session.ParseLoginStart(body)
		switch {
		case len(body) > session.MaxRequest:
			if w.Code != 413 {
				t.Fatalf("a body of %d bytes answered %d", len(body), w.Code)
			}
		case err == nil:
			if w.Code != 200 {
				t.Fatalf("a body the parser reads answered %d: %q", w.Code, body)
			}
			if _, err := session.ParseLoginChallenge(w.Body.Bytes()); err != nil {
				t.Fatalf("the browser would refuse the answer to %q: %v", body, err)
			}
			if !shapes[shape(w.Body.Bytes())] {
				t.Fatalf("%q was answered with the shape %q, which no user of the list gives",
					body, shape(w.Body.Bytes()))
			}
			// Asking for a name is no failed attempt, whichever name it is.
			if wait, err := lock.Check(ctx, h.S.DB, req.Name, h.S.Now()); wait != 0 || err != nil {
				t.Fatalf("%q was locked by asking: %d (%v)", req.Name, wait, err)
			}
		default:
			if w.Code != 400 {
				t.Fatalf("a body the parser refuses answered %d: %q", w.Code, body)
			}
		}
		entries, err := h.S.DB.LogQuery(ctx, core.LogQuery{Limit: 1})
		if err != nil || len(entries) != 0 {
			t.Fatalf("login/start logged %d entries (%v)", len(entries), err)
		}
		now, err := settings(h)
		if err != nil || !maps.Equal(now, before) {
			t.Fatalf("settings after %q are %v, were %v (%v)", body, now, before, err)
		}
	})
}

// finishSeeds returns the bodies FuzzFinish begins with, one per place a login
// can be refused. They are built without asking the handler for a challenge, so
// they are the same bytes in every run and a corpus keeps its meaning; the
// nonce is one nobody ever handed out, so not one of them can make a session.
//
//	0  everything the API asks for and only the nonce wrong -- it runs through
//	   the proof, the password of the role and the code, and is refused at the
//	   last check of all
//	1  the same with a wrong code
//	2  the same without the password the role asks for
//	3  the same with a password proof by a key nobody holds
//	4  a proof by a key nobody holds, which is the shape a wrong password
//	   really has: the browser derives another key from it, and no list knows
//	   that one
//	5  a body the parser reads and session.VerifyClient refuses as a
//	   certificate
//	6..8  shapes the parser turns away
//
// Without 0 among them a green run would say nothing about the path behind the
// parser, and that path is where a session is made.
func finishSeeds(tb testing.TB, h *Handler, fx fixture, passkey, password trusttest.Signer, code string) [][]byte {
	tb.Helper()
	c := session.LoginChallenge{
		User: fx.PasskeyUser, Nonce: madeUpNonce,
		Now: testNow, MaxLifetime: h.S.Cfg.SessionLifetime,
	}
	nobody := trusttest.NewSigner(200)
	return [][]byte{
		finishWith(tb, fx, passkey, &password, c, code),
		finishWith(tb, fx, passkey, &password, c, "000000"),
		finishBody(tb, fx, passkey, c, code),
		finishWith(tb, fx, passkey, &nobody, c, code),
		finishBody(tb, fx, nobody, c, code),
		[]byte(`{"cert":"AAAA","proof":{"kind":"password","kid":"AAAAAAAAAAAAAAAAAAAAAA","sig":"AA"}}`),
		[]byte(`{"cert":"","proof":{}}`),
		[]byte(`{}`),
		[]byte("null"),
	}
}

// TestFinishSeedsReachTheLastCheck is what makes a green FuzzFinish mean
// something, and it is measured over the seeds and not over a run: a fuzzer
// steers where the coverage is, and the coverage of this handler lies in the
// ways it refuses.
//
// How far a body came is read off the calls it made. UserGet is behind the
// proof, the lifetime and the password of the role, and the row it fetches
// carries a TOTP secret -- so a body that reaches it had its password taken and
// its code looked at. OnceUse is behind the code as well. That makes the three
// stages tellable apart without a clock:
//
//   - seed 0 reaches the nonce: password and code were right
//   - seed 1 stops between the two: checkTOTP ran and refused the code
//   - seeds 2 and 3 stop in front of both, and seed 3 carries a password proof
//     -- only session.VerifyPassword can have turned it away
func TestFinishSeedsReachTheLastCheck(t *testing.T) {
	h, fx, passkey, password, code := everything(t)
	// Said plainly first, because it is the fixture and not the seeds that
	// decides it: v.List.PasswordToo and len(row.TOTP) > 0 are what Finish
	// asks before session.VerifyPassword and checkTOTP, and both hang on the
	// installation. Where no user answers them, no body the fuzzer can build
	// enters either -- whatever the seeds below then seem to measure.
	v, err := h.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	asked, secrets := 0, 0
	for _, u := range v.List.Users {
		if v.List.PasswordToo(u.ID) {
			asked++
		}
		row, err := h.S.DB.UserGet(ctx, u.ID)
		if err != nil {
			t.Fatalf("user %s: %v", u.ID, err)
		}
		if len(row.TOTP) > 0 {
			secrets++
		}
	}
	if asked == 0 {
		t.Fatal("no role of this installation asks for the password too: session.VerifyPassword is code no body reaches")
	}
	if secrets == 0 {
		t.Fatal("no user of this installation holds a TOTP secret: checkTOTP is code no body reaches")
	}
	seeds := finishSeeds(t, h, fx, passkey, password, code)
	watch := &sessionWatch{Store: h.S.DB}
	rec := &callLog{Store: watch}
	h.S.DB = rec
	// how far each seed came: "" in front of the row, "user" at the code,
	// "nonce" at the last check, "parser" not even past the parser.
	got := make([]string, len(seeds))
	for i, body := range seeds {
		clearCounting(t, h)
		rec.reset()
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
		calls := rec.list()
		switch {
		case w.Code == 400:
			got[i] = "parser"
		case w.Code != 401:
			t.Fatalf("seed %d answered %d: %q", i, w.Code, body)
		case slices.Contains(calls, "OnceUse "+dbapi.OnceNonce):
			got[i] = "nonce"
		case slices.Contains(calls, "UserGet"):
			got[i] = "user"
		}
	}
	want := []string{"nonce", "user", "", "", "", "", "parser", "parser", "parser"}
	if !slices.Equal(got, want) {
		t.Fatalf("the seeds reached %v, want %v", got, want)
	}
	// And a share, not a single one: most of the list has to get past the
	// parser, or the target would spend its run on shapes the browser never
	// sends.
	past := 0
	for _, g := range got {
		if g != "parser" {
			past++
		}
	}
	if past*2 <= len(seeds) {
		t.Fatalf("%d of %d seeds got past the parser", past, len(seeds))
	}
	if watch.made() {
		t.Fatal("a seed made a session")
	}
}

// FuzzFinish feeds POST session.PathLoginFinish with everything. Whatever
// comes, the answer is a status of the contract, no cookie leaves the handler
// and no session row is written. The last two are the canary, and they are
// asked after every single body: a body that made a session would be caught by
// the very next one too, however the run went.
func FuzzFinish(f *testing.F) {
	h, fx, passkey, password, code := everything(f)
	watch := &sessionWatch{Store: h.S.DB}
	h.S.DB = watch
	for _, body := range finishSeeds(f, h, fx, passkey, password, code) {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		// The counting of failed logins is put back to nothing before every
		// body. Without it the third body of one row shuts it, and from there
		// every following one is answered 429 in front of the whole login --
		// the run would be spent at the door this target is about.
		clearCounting(t, h)
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
		switch w.Code {
		case 400, 401, 413, 429, 500, 503:
		default:
			t.Fatalf("status %d for %q", w.Code, body)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatalf("a fuzzed body got a cookie: %q", body)
		}
		if watch.made() {
			t.Fatalf("a fuzzed body made a session: %q", body)
		}
	})
}

// trustSeeds returns the pairs FuzzTrustRequest begins with: the cookie of an
// open session against every number the contract answers differently, and
// three cookies that are no session at all. The cookie is the one thing the
// fuzzer cannot find by itself -- a token is session.TokenSize random bytes --
// so without it among the seeds the run would never reach an answer with a
// chain in it, and everything behind the door would go unvisited.
//
// The last pair but one is the one that shows the order of the two steps in
// Trust: no cookie and a number the contract refuses. Only where both are
// wrong at once does it matter which of them is looked at first, and the
// answer says which it was.
func trustSeeds(cookie string) []struct{ cookie, from string } {
	return []struct{ cookie, from string }{
		{cookie, "1"},                // the whole chain
		{cookie, "2"},                // one past the newest: nothing new
		{cookie, "3"},                // beyond that: no such version
		{cookie, "0"},                // no version has that number
		{cookie, ""},                 // from missing
		{cookie, "x"},                // no number
		{cookie, "9007199254740992"}, // past what a browser counts exactly
		{"", "1"},                    // no cookie
		{"", "x"},                    // no cookie and no number either
		{"AAAA", "1"},                // a cookie that is no token
	}
}

// trustRequest builds GET session.PathTrust?from=... with cookie as the value
// of the session cookie, and with no cookie at all for the empty string. The
// cookie goes into the header as it stands, so httpapi.Cookie sees what a
// browser or a neighbour would really send.
func trustRequest(cookie, from string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.URL.RawQuery = "from=" + url.QueryEscape(from)
	if cookie != "" {
		r.Header.Set("Cookie", session.CookieName+"="+cookie)
	}
	return r
}

// TestTrustSeedsReachTheAnswers is what makes a green FuzzTrustRequest mean
// something. It is measured over the fixed seeds and never over a run: a
// fuzzer steers towards coverage, and the coverage of this handler lies in its
// refusals, so a share counted while it runs is a share it pushes down.
//
// The bound is the answers themselves and not a count: every status the
// contract gives this path has to come out of the seeds -- the chain, the
// nothing-new, the two refusals of a number and the missing session. A seed
// list that lost the cookie would fail here and not somewhere far away.
func TestTrustSeedsReachTheAnswers(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	seen := map[int]bool{}
	for _, s := range trustSeeds(cookie) {
		w := httptest.NewRecorder()
		h.Trust(w, trustRequest(s.cookie, s.from))
		seen[w.Code] = true
	}
	for _, want := range []int{200, 204, 400, 401, 404} {
		if !seen[want] {
			t.Fatalf("no seed is answered %d; the seeds reach %v", want, slices.Sorted(maps.Keys(seen)))
		}
	}
}

// FuzzTrustRequest feeds GET session.PathTrust with any cookie and any from.
// Every input is asked the same four questions, and they are asked after it
// and not counted over the run: the answer is one of the contract's, its body
// is what that status carries, nothing was counted against anybody, and
// nothing was written into the log. Where a chain came out, it is read back
// with the shell's own parser and verified from its own anchor.
//
// What a run really spends its time on, said plainly: a token is
// session.TokenSize random bytes, so all but the seeded cookie are refused at
// the door, and the chain itself is reached by the seeds at the start of every
// run rather than by the fuzzer. The door is what is being fuzzed there --
// httpapi.Cookie, session.ParseFrom and the status the pair of them lead to --
// and TestTrustSeedsReachTheAnswers is what says the room behind it was
// entered at all.
func FuzzTrustRequest(f *testing.F) {
	h, fx := newFixture(f)
	cookie := loginOnce(f, h, fx)
	for _, s := range trustSeeds(cookie) {
		f.Add(s.cookie, s.from)
	}
	before, err := settings(h)
	if err != nil {
		f.Fatalf("settings: %v", err)
	}
	f.Fuzz(func(t *testing.T, cookie, from string) {
		w := httptest.NewRecorder()
		h.Trust(w, trustRequest(cookie, from))
		switch w.Code {
		case 200, 204, 400, 401, 404, 500, 503:
		default:
			t.Fatalf("status %d for cookie %q from %q", w.Code, cookie, from)
		}
		// Without a cookie there is no session, whatever from says, and the
		// contract has one answer for that. This is the whole of what the
		// order of the two steps in Trust shows from the outside: read the
		// number first and a request that brings neither is told which of its
		// two faults was looked at, which is one word more than a stranger
		// gets today.
		if cookie == "" && w.Code != 401 {
			t.Fatalf("no cookie and from %q answered %d, not 401", from, w.Code)
		}
		switch w.Code {
		case 200:
			c, err := session.ParseTrustChain(w.Body.Bytes())
			if err != nil {
				t.Fatalf("the shell would refuse the chain for from %q: %v", from, err)
			}
			n, err := session.ParseFrom(from)
			if err != nil {
				t.Fatalf("a chain was given for a from the contract refuses: %q", from)
			}
			if _, err := trust.Open(trust.Anchor{Seq: n, Hash: trust.Hash(c.Versions[0])}, c.Versions); err != nil {
				t.Fatalf("the chain for from %q does not verify: %v", from, err)
			}
		case 204:
			if w.Body.Len() != 0 {
				t.Fatalf("204 carried %d bytes", w.Body.Len())
			}
		default:
			cl, err := wire.ParseClose(w.Body.Bytes())
			if err != nil {
				t.Fatalf("status %d is no CLOSE payload: %v", w.Code, err)
			}
			if cl.Status != wire.StatusError || session.HTTPStatus(cl.Code) != w.Code {
				t.Fatalf("code %q with status %d", cl.Code, w.Code)
			}
		}
		// Asking for the trust list is no failed login, whatever the cookie
		// says: the counting keeps its buckets among the settings.
		now, err := settings(h)
		if err != nil || !maps.Equal(now, before) {
			t.Fatalf("settings after cookie %q from %q are %v, were %v (%v)", cookie, from, now, before, err)
		}
		entries, err := h.S.DB.LogQuery(ctx, core.LogQuery{Limit: 1})
		if err != nil || len(entries) != 0 {
			t.Fatalf("the trust list logged %d entries (%v)", len(entries), err)
		}
	})
}
