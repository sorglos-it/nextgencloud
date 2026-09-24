package login

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// setUsers makes the database hold exactly users rows, of which the first
// withTOTP hold a TOTP secret. The rows of the fixture go, so every case says
// its own number and none inherits one.
func setUsers(t *testing.T, h *Handler, users, withTOTP int) {
	t.Helper()
	old, err := h.S.DB.UserList(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, u := range old {
		if err := h.S.DB.UserDelete(ctx, u.ID); err != nil {
			t.Fatalf("delete %s: %v", u.ID, err)
		}
	}
	for i := range users {
		u := dbapi.User{ID: trusttest.ID(byte(0x40 + i)), Name: "u" + strconv.Itoa(i)}
		if i < withTOTP {
			u.TOTP = bytes.Repeat([]byte{byte(i + 1)}, 20)
		}
		if err := h.S.DB.UserPut(ctx, u); err != nil {
			t.Fatalf("user %d: %v", i, err)
		}
	}
}

func TestTOTPShareCountsTheUsers(t *testing.T) {
	for _, c := range []struct {
		name        string
		users, totp int
		want        int
	}{
		{"one of four holds TOTP", 4, 1, 25},
		{"nobody holds TOTP", 4, 0, 0},
		{"everybody holds TOTP", 4, 4, 100},
		{"no users at all", 0, 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, _ := newFixture(t)
			setUsers(t, h, c.users, c.totp)
			if got := h.totpShare(ctx); got != c.want {
				t.Fatalf("share %d, want %d", got, c.want)
			}
		})
	}
}

// TestTOTPShareIsKeptForAWhile nails both sides of the keep time: one second
// before it is over the counted number still stands, and on the second itself
// the users are counted again.
func TestTOTPShareIsKeptForAWhile(t *testing.T) {
	h, _ := newFixture(t)
	now := testNow
	h.S.Now = func() int64 { return now }
	setUsers(t, h, 4, 1)
	if got := h.totpShare(ctx); got != 25 {
		t.Fatalf("first count %d, want 25", got)
	}
	setUsers(t, h, 4, 4)
	now = testNow + shareTTL - 1
	if got := h.totpShare(ctx); got != 25 {
		t.Fatalf("counted again after %d seconds: %d", shareTTL-1, got)
	}
	now = testNow + shareTTL
	if got := h.totpShare(ctx); got != 100 {
		t.Fatalf("still the kept number after %d seconds: %d", shareTTL, got)
	}
}

// brokenList is a database whose UserList fails and whose every other call
// works, so a login/start runs through it to the end. It counts the calls: a
// count that failed must be tried again as seldom as one that worked, or a
// database that is down is asked once per login instead of once per shareTTL.
type brokenList struct {
	dbapi.Store
	calls int
	ok    bool // count the users instead of failing
}

func (b *brokenList) UserList(ctx context.Context) ([]dbapi.User, error) {
	b.calls++
	if b.ok {
		return b.Store.UserList(ctx)
	}
	return nil, errors.New("memstore: the database is gone")
}

// TestTOTPShareAsksTheDatabaseOncePerKeepTime holds the failed count to the
// same keep time as the one that worked. Otherwise every login walks the user
// table again -- and on a real database that is not an error that comes back
// at once but a connection that has to time out first, once per login.
func TestTOTPShareAsksTheDatabaseOncePerKeepTime(t *testing.T) {
	h, _ := newFixture(t)
	now := testNow
	h.S.Now = func() int64 { return now }
	db := &brokenList{Store: h.S.DB, ok: true}
	h.S.DB = db
	for range 100 {
		h.totpShare(ctx)
	}
	if db.calls != 1 {
		t.Fatalf("a database that answers was asked %d times for 100 logins, want 1", db.calls)
	}
	// The keep time is over and the database is gone: one try, not a hundred.
	db.ok, now = false, testNow+shareTTL
	for range 100 {
		h.totpShare(ctx)
	}
	if db.calls != 2 {
		t.Fatalf("a database that is gone was asked %d times for 100 logins, want 2", db.calls)
	}
	// And a try that failed does not shut the counting off either: once the
	// keep time is over again, it is tried again.
	now = testNow + 2*shareTTL
	h.totpShare(ctx)
	if db.calls != 3 {
		t.Fatalf("%d calls once the keep time was over again, want 3", db.calls)
	}
}

// slowList holds its caller inside UserList until it is let go, and then hands
// back users if it was given any -- the picture the database had when the call
// began, which is what a slow call brings back.
type slowList struct {
	dbapi.Store
	users   []dbapi.User
	entered chan struct{}
	release chan struct{}
}

func (s *slowList) UserList(ctx context.Context) ([]dbapi.User, error) {
	s.entered <- struct{}{}
	<-s.release
	if s.users != nil {
		return s.users, nil
	}
	return s.Store.UserList(ctx)
}

// TestTOTPShareKeepsTheYoungerCount: the lock holds only while the turn is
// taken, so a count that lasts longer than shareTTL runs on while the next one
// takes its turn. The older one must not put its number over the younger one's
// -- the kept share would otherwise be a number from one count with the time of
// another, and it would go backwards.
func TestTOTPShareKeepsTheYoungerCount(t *testing.T) {
	h, _ := newFixture(t)
	now := testNow
	h.S.Now = func() int64 { return now }
	setUsers(t, h, 4, 0)
	stale := []dbapi.User{{ID: trusttest.ID(0x50), Name: "old"}}
	slow := &slowList{Store: h.S.DB, users: stale,
		entered: make(chan struct{}), release: make(chan struct{})}
	h.S.DB = slow
	older := make(chan int, 1)
	go func() { older <- h.totpShare(ctx) }()
	<-slow.entered // the older count took the turn at testNow and hangs

	// The keep time passes, the users change, and a second login counts them.
	now = testNow + shareTTL
	h.S.DB = slow.Store
	setUsers(t, h, 4, 4)
	if got := h.totpShare(ctx); got != 100 {
		t.Fatalf("the younger count gave %d, want 100", got)
	}
	// Now the older one comes back with 0 of one user.
	close(slow.release)
	if got := <-older; got != 0 {
		t.Fatalf("the older count gave %d, want 0", got)
	}
	if got := h.totpShare(ctx); got != 100 {
		t.Fatalf("the older count wrote over the younger one: %d, want 100", got)
	}
}

// TestTOTPShareDoesNotHoldUpOtherLogins: the count runs outside the lock, so a
// database that answers slowly holds up the one login that counts and no other.
// The lock is held only long enough to take the turn, which is what keeps it at
// one count per shareTTL.
func TestTOTPShareDoesNotHoldUpOtherLogins(t *testing.T) {
	h, _ := newFixture(t)
	slow := &slowList{Store: h.S.DB, entered: make(chan struct{}), release: make(chan struct{})}
	h.S.DB = slow
	defer close(slow.release)
	counting := make(chan int, 1)
	go func() { counting <- h.totpShare(ctx) }()
	<-slow.entered // the first login is inside the database now
	second := make(chan int, 1)
	go func() { second <- h.totpShare(ctx) }()
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("a second login waited behind the count")
	}
}

func TestTOTPShareSurvivesADatabaseFailure(t *testing.T) {
	// Nothing counted yet: the share is 0, and login/start answers all the
	// same -- a made-up answer has to be built for every name, however the
	// database is doing.
	h, fx := newFixture(t)
	h.S.DB = &brokenList{Store: h.S.DB}
	if got := h.totpShare(ctx); got != 0 {
		t.Fatalf("share without a count %d, want 0", got)
	}
	if c := start(t, h, fx.PasswordName); c.User != fx.PasswordUser {
		t.Fatalf("login/start answered %+v", c)
	}
	if c := start(t, h, "nobody"); !trust.ValidID(c.User) {
		t.Fatalf("login/start answered %+v for an unknown name", c)
	}

	// Counted before: the last number stands, even once its keep time is over.
	h2, _ := newFixture(t)
	now := testNow
	h2.S.Now = func() int64 { return now }
	setUsers(t, h2, 4, 1)
	if got := h2.totpShare(ctx); got != 25 {
		t.Fatalf("first count %d, want 25", got)
	}
	h2.S.DB = &brokenList{Store: h2.S.DB}
	now = testNow + shareTTL
	if got := h2.totpShare(ctx); got != 25 {
		t.Fatalf("share after a failed count %d, want the kept 25", got)
	}
}
