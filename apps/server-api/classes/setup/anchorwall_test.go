package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// pinnedSeq is the version a trust-anchor.json of a paired installation names
// here, and pinnedHash the 32 bytes beside it. The hash is never compared in
// these cases, and that is the finding and not a shortcut: the pin decides
// where the chain is read FROM (state.refreshLocked, "case pinned: from =
// anchor.Seq"), so with no row at pinnedSeq nothing is ever read that could be
// held against the hash. config.parseAnchor asks for 32 bytes, so the file
// needs them to be a file at all.
const pinnedSeq = 3

var pinnedHash = func() []byte {
	h := make([]byte, 32)
	for i := range h {
		h[i] = byte(i + 1)
	}
	return h
}()

// anchorAt gives the handler an anchor path of this run and writes seq into
// it; seq 0 writes nothing, and then the path is one that exists nowhere. It
// is always set, so that no case below depends on whether the machine it runs
// on carries config.AnchorFile.
func anchorAt(t testing.TB, h *Handler, seq uint64) {
	t.Helper()
	h.S.AnchorFile = filepath.Join(t.TempDir(), "trust-anchor.json")
	if seq == 0 {
		if _, err := os.Stat(h.S.AnchorFile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the control needs a path with no anchor on it: %v", err)
		}
		return
	}
	if err := config.SaveAnchor(h.S.AnchorFile, trust.Anchor{Seq: seq, Hash: pinnedHash}); err != nil {
		t.Fatalf("SaveAnchor: %v", err)
	}
	if a, err := config.LoadAnchor(h.S.AnchorFile); err != nil || a.Seq != seq {
		t.Fatalf("the anchor this case needs is not on disk: %v %v", a.Seq, err)
	}
}

// openSetup walks the first half of the documented way back: the one-time
// code off disk and the GET that offers the setup. It fails the case when the
// door is shut, because every case here is about what happens AFTER it opened.
func openSetup(t testing.TB, h *Handler) (session.SetupInfo, string) {
	t.Helper()
	codeBytes, err := os.ReadFile(h.CodeFile)
	if err != nil {
		t.Fatalf("code file: %v", err)
	}
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 200 {
		t.Fatalf("the setup did not open: %d %s", w.Code, w.Body)
	}
	info, err := session.ParseSetupOffer(w.Body.Bytes(), code)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	return info, code
}

// coldNode is a second API on the same database with an empty cache and the
// same anchor file: a restart, or another node. The cold read is what every
// case here measures, because the node that took version 1 holds it in its
// own cache (state.AddVersion sets s.cur) and sees no wall at all.
func coldNode(t testing.TB, h *Handler) *Handler {
	t.Helper()
	s := state.New(h.S.Cfg, h.S.DB, h.S.Box, h.S.Pin)
	s.Now = h.S.Now
	s.AnchorFile = h.S.AnchorFile
	return &Handler{S: s, CodeFile: filepath.Join(t.TempDir(), "setup-code")}
}

// TestAnchorWall_WithoutAnAnchorTheWayBackGoesThrough is the intermediate
// check of the case below, and it is not optional: without it a refusal there
// could be any other wall on the same path -- a code file, a MAC, a list
// trust.Open would not take. Here the way back is walked to the end on the
// same database, the same handler and the same signature, with nothing on the
// anchor path. This is what docs/vertraege-v1.2.md point 10 promises: a
// restored database with no trust list opens the setup again, and what the
// setup takes is what the installation then runs on -- cold, after a restart,
// not only in the cache of the node that took it.
func TestAnchorWall_WithoutAnAnchorTheWayBackGoesThrough(t *testing.T) {
	h := newHandler(t)
	anchorAt(t, h, 0)
	info, code := openSetup(t, h)
	admin := trusttest.NewSigner(3)
	w := httptest.NewRecorder()
	h.Serve(w, post(firstVersion(t, info, admin, code)))
	if w.Code != 204 {
		t.Fatalf("version 1 was refused with no anchor: %d %s", w.Code, w.Body)
	}
	cold := coldNode(t, h)
	v, err := cold.S.Trust(ctx)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("a cold node does not open the list the setup just took: %v %v", v.List, err)
	}
	// And the door is shut behind it, which is the other half of point 10.
	wc := httptest.NewRecorder()
	cold.Serve(wc, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if wc.Code != 404 {
		t.Fatalf("the setup stayed open after version 1: %d", wc.Code)
	}
}

// TestAnchorWall_ARestoredDatabaseEndsInAWall measures point 43 of
// docs/vertraege-v1.2.md, which was read out of the code and not measured.
// The installation was paired once, so trust-anchor.json stands above 1
// (node.pair writes it, state.advanceAnchor moves it up). The database that
// comes back from the backup holds no trust list. Point 10 of the contract
// says the setup opens again, and it does -- and then:
//
//	the setup opens                        measured here, 200
//	version 1 is signed and stored         measured here, 204
//	a cold node reads from the pin         measured here, state.ErrNoTrust
//	the setup opens again                  measured here, 200
//	and the second version 1 is refused    measured here, the row is taken
//
// So the way back is open and ends in a wall, and the wall closes behind the
// admin: the row of version 1 is in the database, the pin still names 3, and
// no further setup can mend it. Nothing in this repository removes the file
// (grep: only config.SaveAnchor and node.pair write it, nothing deletes it;
// tools/recovery-*.sh of branch recovery does not mention it), so the wall
// stands as the code stands.
func TestAnchorWall_ARestoredDatabaseEndsInAWall(t *testing.T) {
	h := newHandler(t)
	anchorAt(t, h, pinnedSeq)
	admin := trusttest.NewSigner(3)

	// 1. and 2.: the door opens and version 1 goes in.
	info, code := openSetup(t, h)
	w := httptest.NewRecorder()
	h.Serve(w, post(firstVersion(t, info, admin, code)))
	if w.Code != 204 {
		t.Fatalf("the setup refused version 1 under the pin: %d %s", w.Code, w.Body)
	}
	// The node that took it sees no wall: its cache holds the version. This
	// is why the wall has to be measured cold, and it is said here rather
	// than left to the reader.
	if v, err := h.S.Trust(ctx); err != nil || v.List.Seq != 1 {
		t.Fatalf("the taking node does not hold version 1: %v %v", v.List, err)
	}
	// The pin was not moved up by it either: advanceAnchor is forward only.
	if a, err := config.LoadAnchor(h.S.AnchorFile); err != nil || a.Seq != pinnedSeq {
		t.Fatalf("the pin stands at %v, want %d: %v", a.Seq, pinnedSeq, err)
	}

	// 3.: the wall. A restart, or any second node.
	cold := coldNode(t, h)
	if v, err := cold.S.Trust(ctx); !errors.Is(err, state.ErrNoTrust) {
		t.Fatalf("the cold node opened %v instead of answering ErrNoTrust: %v", v.List, err)
	}

	// 4. and 5.: and it does not open a second time either. The cold node
	// offers the setup again -- which is the damage, not the mending -- and
	// the version 1 it would take is refused, because the row is there.
	if _, err := cold.EnsureCode(); err != nil {
		t.Fatalf("the cold node could not make a code: %v", err)
	}
	info2, code2 := openSetup(t, cold)
	w2 := httptest.NewRecorder()
	cold.Serve(w2, post(firstVersion(t, info2, admin, code2)))
	if w2.Code == 204 {
		t.Fatalf("the second setup went through, so this case no longer measures the wall")
	}
	// Still ErrNoTrust afterwards: the installation runs on nothing.
	if v, err := cold.S.Trust(ctx); !errors.Is(err, state.ErrNoTrust) {
		t.Fatalf("after the second setup the cold node holds %v: %v", v.List, err)
	}
}
