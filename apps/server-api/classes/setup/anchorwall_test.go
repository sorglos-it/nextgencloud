package setup

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
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

// TestAnchorWall_ARestoredDatabaseComesBack measures the third way of point
// 43 of docs/vertraege-v1.2.md. The installation was paired once, so
// trust-anchor.json stands above 1 (node.pair writes it, state.advanceAnchor
// moves it up). The database that comes back from the backup holds no trust
// list. Point 10 of the contract says the setup opens again -- and before the
// third way it opened into a wall:
//
//	the setup opens                        200
//	version 1 is signed and stored         204
//	a cold node reads from the pin         state.ErrNoTrust  <- the wall
//	the setup opens again                  200
//	and the second version 1 is refused    the row is taken
//
// Measured on 18.10.2026 before the change: exactly this, with the pin left
// at 3. The wall closed behind the admin -- his version 1 lay in the database
// and no further setup could mend it.
//
// What the third way adds is one line of take: a setup that got through to
// the end -- MAC of the local one-time code, signature, AddVersion -- reseals
// the pin onto the version it just stored. Not the sight of an empty
// database, which would hand the pin to anybody who can empty one (answer B
// of point 43, measured there as no answer at all).
func TestAnchorWall_ARestoredDatabaseComesBack(t *testing.T) {
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
	// The node that took it sees no wall even without the reseal: its cache
	// holds the version. This is why the way back has to be measured cold,
	// and it is said here rather than left to the reader.
	if v, err := h.S.Trust(ctx); err != nil || v.List.Seq != 1 {
		t.Fatalf("the taking node does not hold version 1: %v %v", v.List, err)
	}

	// 3.: the pin now names the version that is there. This is the new line,
	// and it is a step DOWN from 3 to 1 -- state.advanceAnchor would never
	// write it, which is why take asks for a reseal and not a move up.
	a, err := config.LoadAnchor(h.S.AnchorFile)
	if err != nil || a.Seq != 1 {
		t.Fatalf("the pin stands at %v, want 1: %v", a.Seq, err)
	}
	if !bytes.Equal(a.Hash, versionHash(t, h)) {
		t.Fatalf("the pin names other bytes than the version that was stored")
	}

	// 4.: the wall is gone. A restart, or any second node, runs on version 1.
	cold := coldNode(t, h)
	v, err := cold.S.Trust(ctx)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("a cold node does not open the list the setup took: %v %v", v.List, err)
	}
	// 5.: and the door is shut behind it, which is the other half of point 10.
	wc := httptest.NewRecorder()
	cold.Serve(wc, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if wc.Code != 404 {
		t.Fatalf("the setup stayed open after version 1: %d", wc.Code)
	}
}

// TestAnchorWall_TheResealedPinStillRefusesATruncatedChain is the half that
// decided the shape of the line in take. Point 43 names two ways of freeing
// the pin, and both open the way back: removing the file, and resealing it
// onto version 1. Measured on 18.10.2026, side by side on this case:
//
//	removed   cold node on version 1, pin gone, versions 2 and 3 added,
//	          top row deleted -> the cold node runs on version 2, silently
//	resealed  cold node on version 1, pin at 1, versions 2 and 3 added,
//	          pin follows to 3, top row deleted -> state.ErrNoTrust
//
// So removing the file gives the chain cut at the TOP back to whoever can
// write the database -- the one form state.checkGap names and cannot catch --
// and nothing in this repository ever writes the file again except a pairing.
// Resealing costs nothing against that and keeps the guard, so take reseals.
//
// This case is the second half: after the way back, the pin is a pin again.
func TestAnchorWall_TheResealedPinStillRefusesATruncatedChain(t *testing.T) {
	h := newHandler(t)
	anchorAt(t, h, pinnedSeq)
	admin := trusttest.NewSigner(3)
	info, code := openSetup(t, h)
	raw := firstVersion(t, info, admin, code)
	first, err := session.ParseSetup(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	w := httptest.NewRecorder()
	h.Serve(w, post(raw))
	if w.Code != 204 {
		t.Fatalf("the setup refused version 1 under the pin: %d %s", w.Code, w.Body)
	}
	// The installation runs on: two more versions of the same admin, which
	// the pin follows by itself (state.advanceAnchor).
	second := admin.Sign(t, trusttest.Next(t, first.Trust))
	if _, err := h.S.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	third := admin.Sign(t, trusttest.Next(t, second))
	if _, err := h.S.AddVersion(ctx, third); err != nil {
		t.Fatalf("version 3: %v", err)
	}
	// Intermediate check: the pin stands at the newest version. Without it
	// the refusal below could be any other wall.
	if a, err := config.LoadAnchor(h.S.AnchorFile); err != nil || a.Seq != 3 {
		t.Fatalf("the pin stands at %v, want 3: %v", a.Seq, err)
	}
	// Intermediate check two: a cold node opens the whole chain under it.
	if v, err := coldNode(t, h).S.Trust(ctx); err != nil || v.List.Seq != 3 {
		t.Fatalf("the chain does not open under the resealed pin: %v %v", v.List, err)
	}
	// The attack: the top row away, the rest whole.
	if err := h.S.DB.BlobDelete(ctx, dbapi.BlobTrust, "3"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if v, err := coldNode(t, h).S.Trust(ctx); !errors.Is(err, state.ErrNoTrust) {
		t.Fatalf("the truncated chain opened at %v: %v", v.List, err)
	}
	// Control: the same rows, and no anchor at all -- which is what removing
	// the file would leave behind. Then the older list IS put back in force,
	// without a word, and that is the whole reason take reseals.
	if err := os.Remove(h.S.AnchorFile); err != nil {
		t.Fatalf("remove: %v", err)
	}
	v, err := coldNode(t, h).S.Trust(ctx)
	if err != nil || v.List.Seq != 2 {
		t.Fatalf("control: with no anchor the cold node answered %v: %v", v.List, err)
	}
}

// versionHash is the hash of the one trust version this installation holds,
// read back out of the state rather than recomputed from the test's own
// bytes.
func versionHash(t testing.TB, h *Handler) []byte {
	t.Helper()
	v, err := h.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	return v.Hash
}
