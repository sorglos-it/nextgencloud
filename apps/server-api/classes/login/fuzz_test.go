package login

import (
	"encoding/json"
	"maps"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
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

// finishSeeds returns the bodies FuzzFinish begins with. The first two are
// real: a certificate of a user of this installation, the proof over exactly
// its bytes and everything else the API asks for -- only the nonce is one
// nobody ever handed out, so they run through every check and are refused at
// the last one. Without such a body among the seeds a green run would say
// nothing about the path behind the parser, and that path is where a session
// is made. The third carries a key the trust list does not hold, which is the
// shape a wrong password really has: the browser derives another key from it,
// and no list knows that one. The rest are shapes the parser turns away.
//
// They are built without asking the handler for a challenge, so they are the
// same bytes in every run and a corpus keeps its meaning.
func finishSeeds(tb testing.TB, h *Handler, fx fixture) [][]byte {
	tb.Helper()
	c := session.LoginChallenge{
		User: fx.PasswordUser, Nonce: madeUpNonce,
		Now: testNow, MaxLifetime: h.S.Cfg.SessionLifetime,
	}
	return [][]byte{
		finishBody(tb, fx, fx.Password, c, ""),
		finishBody(tb, fx, fx.Password, c, "000000"),
		finishBody(tb, fx, trusttest.NewSigner(200), c, ""),
		[]byte(`{"cert":"AAAA","proof":{"kind":"password","kid":"AAAAAAAAAAAAAAAAAAAAAA","sig":"AA"}}`),
		[]byte(`{"cert":"","proof":{}}`),
		[]byte(`{}`),
		[]byte("null"),
	}
}

// TestFinishSeedsReachTheLastCheck is what makes a green FuzzFinish mean
// something, and it is measured over the seeds and not over a run: a fuzzer
// steers where the coverage is, and the coverage of this handler lies in the
// ways it refuses. So the share of bodies that get through to the far end is
// counted here, on a fixed list nobody steers.
//
// At least one seed has to reach the nonce, which is the last check of all:
// it is behind the proof, behind the lifetime, behind the password of a role
// that asks for one and behind TOTP. At least one has to be turned away by the
// parser, or the target would never try the shape the browser really sends.
// And none of them makes a session.
func TestFinishSeedsReachTheLastCheck(t *testing.T) {
	h, fx := newFixture(t)
	seeds := finishSeeds(t, h, fx)
	watch := &sessionWatch{Store: h.S.DB}
	rec := &callLog{Store: watch}
	h.S.DB = rec
	deep, refused := 0, 0
	for _, body := range seeds {
		clearCounting(t, h)
		rec.reset()
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
		switch w.Code {
		case 400:
			refused++
		case 401:
			if slices.Contains(rec.list(), "OnceUse "+dbapi.OnceNonce) {
				deep++
			}
		default:
			t.Fatalf("a seed answered %d: %q", w.Code, body)
		}
	}
	if deep == 0 {
		t.Fatalf("no seed of %d reached the nonce: the target only ever tries the door", len(seeds))
	}
	if refused == 0 {
		t.Fatal("no seed was turned away by the parser")
	}
	// The two checks in the middle hang on the installation and not on the
	// body: v.List.PasswordToo and len(row.TOTP) > 0 are what Finish asks
	// before it runs session.VerifyPassword and checkTOTP. Where no user of
	// the fixture answers them, no seed and no fuzzed body can ever enter
	// either -- they are code the whole run walks past, and counting seeds
	// says nothing about them.
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
	h, fx := newFixture(f)
	watch := &sessionWatch{Store: h.S.DB}
	h.S.DB = watch
	for _, body := range finishSeeds(f, h, fx) {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		// The counting of failed logins is put back to nothing before every
		// body. Without it the third body of one bucket shuts it, and from
		// there every following one is answered 429 in front of the whole
		// login -- the run would be spent at the door this target is about.
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
