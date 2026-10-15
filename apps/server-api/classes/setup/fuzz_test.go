package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// setupSeeds returns the bodies FuzzSetupBody starts from. The first is
// version 1 as the shell builds it, sealed with a code that is not this
// installation's: without a body of the right shape nothing would ever get
// past the parser, and a green run would say nothing about the path behind
// it. No seed and no mutation of one can carry a MAC that fits, because every
// handler the target builds draws a code of its own.
func setupSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	h := newHandler(tb)
	codeBytes, err := os.ReadFile(h.CodeFile)
	if err != nil {
		tb.Fatalf("code: %v", err)
	}
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), strings.TrimSpace(string(codeBytes)))
	if err != nil {
		tb.Fatalf("offer: %v", err)
	}
	return [][]byte{
		firstVersion(tb, info, trusttest.NewSigner(3), "AAAAA-BBBBB-CCCCC-DDDDD"),
		[]byte(`{"mac":"","trust":{"payload":"","proof":{}}}`),
		[]byte("{}"),
		[]byte("null"),
	}
}

// TestSetupSeedsGetPastTheParser is what makes a green fuzz run mean
// something: at least one seed is a body session.ParseSetup reads, and it
// really travels to the MAC behind it, where it is turned away with E_VERIFY.
func TestSetupSeedsGetPastTheParser(t *testing.T) {
	past := 0
	for _, body := range setupSeeds(t) {
		if _, err := session.ParseSetup(body); err != nil {
			continue
		}
		past++
		h := newHandler(t)
		w := httptest.NewRecorder()
		h.Serve(w, post(body))
		if w.Code != 403 {
			t.Fatalf("a body of the right shape answered %d", w.Code)
		}
	}
	if past < 1 {
		t.Fatal("no seed is a body the parser reads: the target would never reach the MAC")
	}
}

// FuzzSetupBody feeds POST session.PathSetup with everything: the API must
// answer a status of the contract and never panic or take a version.
func FuzzSetupBody(f *testing.F) {
	for _, body := range setupSeeds(f) {
		f.Add(body)
	}
	// One handler for the whole run: making one writes a file and syncs it,
	// which would be almost all of every run. Nothing carries over from one
	// body to the next but the wait, which is cleared below -- a body that
	// changed anything else would have been taken, and that fails the run.
	h := newHandler(f)
	f.Fuzz(func(t *testing.T, body []byte) {
		// Every body is a first attempt, as it is on a fresh installation.
		if err := lock.Clear(ctx, h.S.DB, lockName); err != nil {
			t.Fatalf("clear the wait: %v", err)
		}
		w := httptest.NewRecorder()
		h.Serve(w, post(body))
		switch w.Code {
		case 204, 400, 403, 404, 413, 429, 500, 503:
		default:
			t.Fatalf("status %d for %q", w.Code, body)
		}
		if w.Code == 204 {
			t.Fatalf("a fuzzed body was taken as version 1: %q", body)
		}
		// The answer names the step that turned the body away and nothing
		// else: too much is E_TOOLARGE, a body the parser refuses is
		// E_PROTOCOL, and one it reads goes on to the MAC, which nobody
		// without the code can make.
		_, err := session.ParseSetup(body)
		switch {
		case len(body) > session.MaxRequest:
			if w.Code != 413 {
				t.Fatalf("a body of %d bytes answered %d", len(body), w.Code)
			}
		case err == nil:
			if w.Code != 403 {
				t.Fatalf("a body the parser reads answered %d", w.Code)
			}
		default:
			if w.Code != 400 {
				t.Fatalf("a body the parser refuses answered %d", w.Code)
			}
		}
		// Nothing is left behind either: no version of the trust list, and
		// the code is still the one that counts.
		if _, err := h.S.Trust(ctx); !errors.Is(err, state.ErrNoTrust) {
			t.Fatalf("trust after a fuzzed body: %v", err)
		}
		if _, ok := h.code(); !ok {
			t.Fatal("the code file did not survive a fuzzed body")
		}
	})
}
