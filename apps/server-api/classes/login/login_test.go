package login

import (
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// TestUserByNameLooksAtEveryUser pins the one thing userByName does that
// trust.List.UserByName does not: it runs to the end of the list, so its time
// does not follow where a name stands or whether it stands there at all. No
// version of a trust list holds two users of one name (trust.List.Validate),
// and that is the point: a second one is the only way to tell a walk that
// stops at the first match from one that does not without a clock, and a clock
// would make this test flaky long before it made it true.
func TestUserByNameLooksAtEveryUser(t *testing.T) {
	l := &trust.List{Users: []trust.User{
		{ID: trusttest.ID(0x31), Name: "anna"},
		{ID: trusttest.ID(0x32), Name: "anna"},
	}}
	u, ok := userByName(l, "anna")
	if !ok || u.ID != trusttest.ID(0x32) {
		t.Fatalf("userByName stopped at %q (%v): it does not look at every user", u.ID, ok)
	}
	if _, ok := userByName(l, "nobody"); ok {
		t.Fatal("a name nobody has was found")
	}
	// The same list through trust.List.UserByName stops at the first match,
	// which is what this walk is here to avoid. The day that changes, the walk
	// here can go.
	if first, _ := l.UserByName("anna"); first.ID != trusttest.ID(0x31) {
		t.Fatalf("trust.List.UserByName no longer stops at the first match (%q)", first.ID)
	}
}

// TestStartAnswersRightUpToTheLock nails the near side of the free tries that
// TestStartLocksAfterFailures nails the far side of: two failed logins leave
// the name answerable, the third shuts it. Without this a lock that shut after
// the first failure would pass just as well -- and every typo would cost the
// shell a wait.
func TestStartAnswersRightUpToTheLock(t *testing.T) {
	h, _ := newFixture(t)
	failLogin(t, h, "anna")
	failLogin(t, h, "anna")
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"anna"}`))
	if w.Code != 200 {
		t.Fatalf("two failures answered %d, want an answer", w.Code)
	}
	failLogin(t, h, "anna")
	w = httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"anna"}`))
	if w.Code != 429 {
		t.Fatalf("three failures answered %d, want 429", w.Code)
	}
}
