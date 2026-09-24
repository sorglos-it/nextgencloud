package login

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

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
// works, so a login/start runs through it to the end.
type brokenList struct{ dbapi.Store }

func (brokenList) UserList(context.Context) ([]dbapi.User, error) {
	return nil, errors.New("memstore: the database is gone")
}

func TestTOTPShareSurvivesADatabaseFailure(t *testing.T) {
	// Nothing counted yet: the share is 0, and login/start answers all the
	// same -- a made-up answer has to be built for every name, however the
	// database is doing.
	h, fx := newFixture(t)
	h.S.DB = brokenList{Store: h.S.DB}
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
	h2.S.DB = brokenList{Store: h2.S.DB}
	now = testNow + shareTTL
	if got := h2.totpShare(ctx); got != 25 {
		t.Fatalf("share after a failed count %d, want the kept 25", got)
	}
}
