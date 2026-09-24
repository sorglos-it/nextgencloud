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
	// counting of failed logins writes its rows here too, so a row more than
	// this is a lock login/start laid on somebody.
	before, err := h.S.DB.BlobFrom(ctx, dbapi.BlobSetting, 0)
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
		now, err := h.S.DB.BlobFrom(ctx, dbapi.BlobSetting, 0)
		if err != nil || len(now) != len(before) {
			t.Fatalf("%d settings after %q, were %d (%v)", len(now), body, len(before), err)
		}
	})
}
