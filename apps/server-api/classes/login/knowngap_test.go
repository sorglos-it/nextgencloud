package login

import (
	json "encoding/json/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// The tests in this file hold gaps, not promises. Every one of them asserts
// what the code does today and says beside it that today is wrong. They are
// the receipt for point 33b of docs/vertraege-v1.2.md, which closes with
// point 6 of that list: a way to advance an anchor that lives outside the
// database. Until that exists, state.checkGap cannot hold against whoever
// writes the rows, and the comments at state.checkGap and
// state.checkInstallation say so.
//
// A test here going red is good news and not a regression. It means the gap
// it fences is shut, and then two things are owed: the comment beside the
// code it names has to be corrected, and the test itself has to go or turn
// into the promise it was standing in for. Without these, the honest comments
// of today rot into false ones the day somebody fixes this, and nobody finds
// out -- a comment rots, a checked-in test does not.
//
// What they cost the attacker is two questions and not one, because they are
// not all the same damage. The first two below put an older list back in
// force: the session of a user an admin withdrew is open again, on a list that
// still holds him. That is the very damage
// TestAWithdrawalSurvivesTheRowsBeingDropped fences, so the two prices
// compare -- eleven statements each against its ten.
//
// The third is a different damage and no price of it belongs in that
// subtraction. A warm node deciding on a list the store has lost puts nothing
// back in force: it goes on deciding on the newest version there ever was.
// Measured on the very twelve-version chain of the two above, with version 2
// withdrawing the user: drop the row of version 12 alone and Refresh still
// answers version 12 with no error -- and the withdrawn user's session is
// refused all the same, E_LOGIN, because version 12 is the list that
// withdrew him. Drop all twelve and it is the same answer. On that
// installation there is no number of statements at all that buys an attacker
// what the two above buy for eleven.
//
// So no number stands here for the third. What it is, is written out at
// TestKnownGap_AWarmNodeDecidesOnAListTheStoreLost, which measures it where
// it can be seen: a user no version ever withdrew, and a node answering
// ok=true out of a version the store can no longer show. On that
// three-version installation one statement reaches it -- dropping the row of
// version 3 alone leaves Refresh answering version 3 with no error and Live
// saying ok=true, measured -- and that one statement and the eleven above
// buy two different things.

// withdrawnAtTwelve builds the installation these gaps are measured on:
// version 2 takes the logged-in password user out of the trust list, and ten
// more versions follow, so version 12 is the newest. It returns the cookie of
// that user's session, which is open and must not be.
func withdrawnAtTwelve(t *testing.T, h *Handler, fx fixture) string {
	t.Helper()
	cookie := loginOnce(t, h, fx)
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
	return cookie
}

// coldNode is a second API of the same installation with an empty cache: the
// node that walks the chain up from the bottom, and the one every gap here is
// asked of.
func coldNode(t *testing.T, h *Handler) *Handler {
	t.Helper()
	c := New(state.New(h.S.Cfg, h.S.DB, h.S.Box, h.S.Pin))
	c.S.Now = h.S.Now
	return c
}

// dropTrustRows deletes the trust rows from lo to hi, which is all the writer
// of the database has to do.
func dropTrustRows(t *testing.T, h *Handler, lo, hi int) {
	t.Helper()
	for seq := lo; seq <= hi; seq++ {
		if err := h.S.DB.BlobDelete(ctx, dbapi.BlobTrust, strconv.Itoa(seq)); err != nil {
			t.Fatalf("drop row %d: %v", seq, err)
		}
	}
}

// TestKnownGap_TruncatingTheChainRollsItBack fences the first of the two forms
// state.checkGap cannot see: the chain is cut instead of holed.
//
// checkGap looks above a number the store does not hold. Rows 2 to 11 gone
// leaves version 12 standing above the hole, and that is caught. Rows 2 to 12
// gone -- one statement more -- leaves nothing above at all, and the end of a
// cut chain looks from below exactly like the end of a whole one. The cold
// node reads version 1 as the newest and takes the session of a user an admin
// withdrew ten versions ago.
//
// This is a gap and not the behaviour we want. It closes with point 6 of
// docs/vertraege-v1.2.md (an anchor outside the database, advanced). If this
// test goes red the gap is shut: correct the comment at state.checkGap, which
// names this form, and retire this test.
func TestKnownGap_TruncatingTheChainRollsItBack(t *testing.T) {
	h, fx := newFixture(t)
	cookie := withdrawnAtTwelve(t, h, fx)
	dropTrustRows(t, h, 2, 12)

	cold := coldNode(t, h)
	v, err := cold.S.Refresh(ctx)
	if err != nil || v.List == nil || v.List.Seq != 1 {
		t.Fatalf("the gap is shut -- Refresh now answers version %v, %v. "+
			"Correct the comment at state.checkGap and retire this test", v.List, err)
	}
	live, code, ok := coldNode(t, h).Live(ctx, getCookie(session.PathTrust, cookie))
	if !ok {
		t.Fatalf("the gap is shut -- the cold node refused the withdrawn user with %q. "+
			"Correct the comment at state.checkGap and retire this test", code)
	}
	// Said out loud, because this is the damage and not a detail: the session
	// is open again, on the list that still holds the withdrawn user.
	if live.Trust.List.Seq != 1 {
		t.Fatalf("the gap is shut -- the rolled-back version was %d, not 1. "+
			"Correct the comment at state.checkGap and retire this test", live.Trust.List.Seq)
	}
	if !slices.ContainsFunc(live.Trust.List.Users, func(u trust.User) bool { return u.ID == fx.PasswordUser }) {
		t.Fatal("version 1 no longer holds the withdrawn user, so this test is not measuring the rollback")
	}
}

// TestKnownGap_LoweringTheTopRowsSeqRollsItBack fences the second form, and it
// is the one that shows why no check inside the database can hold.
//
// The row of version 12 is left exactly where it is -- same ID, same bytes,
// same signature -- and only its Seq is lowered. Seq is not something the
// store knows about a trust row: it is a field the writer hands in, and
// dbapi.BlobPut lays it down as given. So the one query checkGap asks,
// BlobFrom above the missing number, answers with nothing, and the cold node
// reads version 1 as the newest again.
//
// shared/dbapi/dbtest does not stand in the way of this: it writes the trust
// rows with Seq equal to the ID itself and then checks that BlobFrom filters
// and sorts by Seq, so what it pins is the query and never which Seq a trust
// row must carry. memstore passes dbtest in full and is what this test runs
// on.
//
// This is a gap and not the behaviour we want. It closes with point 6 of
// docs/vertraege-v1.2.md (an anchor outside the database, advanced). If this
// test goes red the gap is shut: correct the comment at state.checkGap, which
// names this form, and retire this test.
func TestKnownGap_LoweringTheTopRowsSeqRollsItBack(t *testing.T) {
	h, fx := newFixture(t)
	cookie := withdrawnAtTwelve(t, h, fx)
	dropTrustRows(t, h, 2, 11)

	row, err := h.S.DB.BlobGet(ctx, dbapi.BlobTrust, "12")
	if err != nil {
		t.Fatalf("read row 12: %v", err)
	}
	row.Seq = 0
	if err := h.S.DB.BlobPut(ctx, row); err != nil {
		t.Fatalf("write row 12 back with a lowered Seq: %v", err)
	}
	// The row is untouched as a document: whoever reads it still reads
	// version 12. Only the field the query sorts on was moved.
	back, err := h.S.DB.BlobGet(ctx, dbapi.BlobTrust, "12")
	if err != nil {
		t.Fatalf("read row 12 back: %v", err)
	}
	sg, err := trust.ParseSigned(back.Data)
	if err != nil {
		t.Fatalf("row 12 no longer parses, so this test is not measuring the lowered Seq: %v", err)
	}
	var l trust.List
	if err := json.Unmarshal(sg.Payload, &l); err != nil || l.Seq != 12 {
		t.Fatalf("row 12 does not read as version 12 (%d), so this test measures something else: %v", l.Seq, err)
	}
	if above, err := h.S.DB.BlobFrom(ctx, dbapi.BlobTrust, 2); err != nil || len(above) != 0 {
		t.Fatalf("the store now answers %d row(s) above the hole (%v) -- if that is a fix, "+
			"correct the comment at state.checkGap and retire this test", len(above), err)
	}

	cold := coldNode(t, h)
	v, err := cold.S.Refresh(ctx)
	if err != nil || v.List == nil || v.List.Seq != 1 {
		t.Fatalf("the gap is shut -- Refresh now answers version %v, %v. "+
			"Correct the comment at state.checkGap and retire this test", v.List, err)
	}
	live, code, ok := coldNode(t, h).Live(ctx, getCookie(session.PathTrust, cookie))
	if !ok {
		t.Fatalf("the gap is shut -- the cold node refused the withdrawn user with %q. "+
			"Correct the comment at state.checkGap and retire this test", code)
	}
	if live.Trust.List.Seq != 1 {
		t.Fatalf("the gap is shut -- the rolled-back version was %d, not 1. "+
			"Correct the comment at state.checkGap and retire this test", live.Trust.List.Seq)
	}
}

// TestKnownGap_AWarmNodeDecidesOnAListTheStoreLost fences the half of N3 that
// stayed open when the answers were made to agree.
//
// state.Refresh asks only what lies above the cached version, never for the
// cached version itself. So a node that holds one decides on it even after
// every trust row of the installation is gone: Refresh hands back the cached
// version and no error at all. What answers 500 for GET session.PathTrust is
// the step after, state.Chain, which goes to the rows for the versions it is
// to send -- so the status is right and the decision is not. A caller of Live
// that does not go on to Chain, which is the WebSocket login/live.go names,
// decides a session on a list the store no longer holds.
//
// This is a gap and not the behaviour we want. It closes the same way as the
// two above, with point 6 of docs/vertraege-v1.2.md, because a node that
// checked its cached version against an anchor outside the database would see
// this too. If this test goes red, correct the note at the Refresh call in
// login/live.go, the one beside state.ErrSetting there, the one at the 204 in
// login/trust.go, and retire it.
func TestKnownGap_AWarmNodeDecidesOnAListTheStoreLost(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	prev := fx.Signed
	for i := 2; i <= 3; i++ {
		prev = fx.Passkey.Sign(t, trusttest.Next(t, prev))
		if _, err := h.S.AddVersion(ctx, prev); err != nil {
			t.Fatalf("version %d: %v", i, err)
		}
	}
	// The node is warm: it wrote these versions and holds version 3.
	dropTrustRows(t, h, 1, 3)

	v, err := h.S.Refresh(ctx)
	if err != nil || v.List == nil || v.List.Seq != 3 {
		t.Fatalf("the gap is shut -- Refresh now answers %v, %v. "+
			"Correct the note at the Refresh call in login/live.go, the one beside "+
			"state.ErrSetting there, the one at the 204 in login/trust.go, and retire "+
			"this test", v.List, err)
	}
	// And this is why the answer of GET /trust looks right anyway: the step
	// after this one does go to the rows, and finds them gone.
	if _, err := h.S.Chain(ctx, v, 1); err == nil {
		t.Fatal("state.Chain answered without the rows, so the 500 of GET /trust no longer comes " +
			"from there. Correct the note at the Refresh call in login/live.go, the one " +
			"beside state.ErrSetting there, the one at the 204 in login/trust.go, and " +
			"retire this test")
	}
	// The gap itself, and not the reassurance above: a caller of Live that
	// does not go on to Chain sees none of that. It decides the session live,
	// on a list the store has already lost.
	if _, _, ok := h.Live(ctx, getCookie(session.PathTrust, cookie)); !ok {
		t.Fatal("the gap is shut -- the warm node refused a session on the list the store lost. " +
			"Correct the note at the Refresh call in login/live.go, the one beside " +
			"state.ErrSetting there, the one at the 204 in login/trust.go, and retire " +
			"this test")
	}
}
