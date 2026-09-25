package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// TestKnownGap_ARestartedNodeReopensTheSetupDoor holds a gap and not a
// promise. It asserts what the code does today and says here that today is
// wrong. It is the receipt for point 33b of docs/vertraege-v1.2.md, which
// closes with point 6 of that list: a way to advance an anchor that lives
// outside the database. The comment at fault() in setup.go carries the same
// line, and state.checkGap carries it for the trust rows.
//
// Going red here is good news and not a regression: it means the door is shut
// again, and then the comment at fault() has to be corrected and this test
// retired. A comment rots, a checked-in test does not.
//
// The damage, one statement: node A runs the first setup and holds version 1
// in its cache, so it keeps answering 404 whether the row is there or not.
// The row of version 1 is dropped. Node B starts afterwards with an empty
// cache, finds no trust list, and offers the first setup again.
//
// It is not a difference of status, which is why it belongs beside the other
// three and not in a note. The installation ID is a row of its own and was
// not touched, so node B offers the setup for the installation that stood --
// and takes a version 1 signed by a key nobody of that installation ever
// held. After that the installation runs on a stranger's trust list with his
// admin in it. Whoever can drop one row can take the installation, and no
// status this handler picks changes that.
func TestKnownGap_ARestartedNodeReopensTheSetupDoor(t *testing.T) {
	a := newHandler(t)
	codeBytes, _ := os.ReadFile(a.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	a.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), code)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	w = httptest.NewRecorder()
	a.Serve(w, post(firstVersion(t, info, trusttest.NewSigner(3), code)))
	if w.Code != 204 {
		t.Fatalf("the first setup did not go through: %d %s", w.Code, w.Body)
	}
	stood, err := a.S.Installation(ctx)
	if err != nil {
		t.Fatalf("installation: %v", err)
	}

	// One statement of whoever can write the database.
	if err := a.S.DB.BlobDelete(ctx, dbapi.BlobTrust, "1"); err != nil {
		t.Fatalf("drop row 1: %v", err)
	}

	// Node A kept its cache, so it still says the setup is over.
	wa := httptest.NewRecorder()
	a.Serve(wa, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if wa.Code != 404 {
		t.Fatalf("node A answered %d, so this test is no longer measuring the split", wa.Code)
	}

	// Node B shares the database and starts now: empty cache, and a start
	// without a trust list makes a setup code of its own.
	s := state.New(a.S.Cfg, a.S.DB, a.S.Box, a.S.Pin)
	s.Now = a.S.Now
	b := &Handler{S: s, CodeFile: filepath.Join(t.TempDir(), "setup-code")}
	if _, err := b.EnsureCode(); err != nil {
		t.Fatalf("node B ensure code: %v", err)
	}
	bCodeBytes, _ := os.ReadFile(b.CodeFile)
	bCode := strings.TrimSpace(string(bCodeBytes))

	wb := httptest.NewRecorder()
	b.Serve(wb, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if wb.Code != 200 {
		t.Fatalf("the gap is shut -- the restarted node answered %d instead of offering the setup. "+
			"Correct the comment at fault() in setup.go and retire this test", wb.Code)
	}
	bInfo, err := session.ParseSetupOffer(wb.Body.Bytes(), bCode)
	if err != nil {
		t.Fatalf("node B offer: %v", err)
	}
	if bInfo.Installation != stood {
		t.Fatalf("the gap is shut -- node B offered %q instead of reusing the installation that "+
			"stood (%q), so it no longer hands a fresh cache the stored ID for free. Correct the "+
			"comment at fault() in setup.go and retire this test", bInfo.Installation, stood)
	}

	// The takeover itself: a key of nobody's signs version 1 for that
	// installation, and it is taken.
	stranger := trusttest.NewSigner(9)
	wb = httptest.NewRecorder()
	b.Serve(wb, post(firstVersion(t, bInfo, stranger, bCode)))
	if wb.Code != 204 {
		t.Fatalf("the gap is shut -- the restarted node refused a stranger's version 1 with %d. "+
			"Correct the comment at fault() in setup.go and retire this test", wb.Code)
	}
	v, err := b.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust after the takeover: %v", err)
	}
	if v.List.Seq != 1 || len(v.List.Users) != 1 {
		t.Fatalf("expected the stranger's version 1 with his one admin, got version %d with %d user(s)",
			v.List.Seq, len(v.List.Users))
	}
}
