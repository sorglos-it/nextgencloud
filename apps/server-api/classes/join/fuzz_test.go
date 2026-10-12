package join

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// FuzzJoinRequest sends arbitrary bodies and arbitrary query values to Accept
// and holds that nothing panics and that no dbapi.BlobJoin ever comes out of
// them: the secret of this API's code is random, so no input the fuzzer can
// write carries a MAC that holds.
//
// Because that is a statement about a wall, every case also measures that the
// wall is the MAC and not something earlier: afterwards the correctly signed
// request of a second code IS taken and DOES leave a row. Without that
// control a handler that refuses everything would pass.
func FuzzJoinRequest(f *testing.F) {
	j, fx := newJoin(f)
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		f.Fatalf("new code: %v", err)
	}
	good, err := json.Marshal(fx.request(code.Secret))
	if err != nil {
		f.Fatalf("body: %v", err)
	}
	h := sha256.Sum256(code.Secret)
	f.Add(enc.B64.EncodeToString(h[:]), good)
	f.Add("", []byte(""))
	f.Add(" ", []byte(""))
	f.Add("!!", []byte(`{"node":"api-2"}`))
	f.Add(enc.B64.EncodeToString(h[:]), []byte("{}"))
	f.Add(enc.B64.EncodeToString(h[:]),
		[]byte(`{"node":"api-2","noise":"AAAA","sign":"AAAA","tls":"AAAA","url":"wss://a.test:7443/tunnel","mac":"AAAA"}`))

	f.Fuzz(func(t *testing.T, query string, body []byte) {
		// Each case gets its own API, so one case cannot spend the secret of
		// the next, and "no row" is about this body alone.
		j, fx := newJoin(t)
		if _, _, err := j.NewJoinCode(ctx); err != nil {
			t.Fatalf("new code: %v", err)
		}
		w := httptest.NewRecorder()
		j.Accept(w, post(query, body))
		if w.Code == http.StatusNoContent {
			t.Fatalf("Accept answered 204 for a body nobody signed: %q %q", query, body)
		}
		rows, err := j.Waiting(ctx)
		if err != nil {
			t.Fatalf("waiting: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("%d rows were kept for a body nobody signed: %q %q", len(rows), query, body)
		}
		// The control: the way to a row is open.
		second, _, err := j.NewJoinCode(ctx)
		if err != nil {
			t.Fatalf("second code: %v", err)
		}
		ok, err := json.Marshal(fx.request(second.Secret))
		if err != nil {
			t.Fatalf("body: %v", err)
		}
		sh := sha256.Sum256(second.Secret)
		w2 := httptest.NewRecorder()
		j.Accept(w2, post(enc.B64.EncodeToString(sh[:]), ok))
		if w2.Code != http.StatusNoContent {
			t.Fatalf("the correct request is refused, so this case measures nothing: %d", w2.Code)
		}
		rows, err = j.Waiting(ctx)
		if err != nil {
			t.Fatalf("waiting: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("%d rows after the correct request, want 1", len(rows))
		}
	})
}
