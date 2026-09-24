package sqlstore_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// TestUserNameIsUnique: a name belongs to one ID. The check has to happen in
// the same transaction as the write, otherwise two APIs could both take it.
func TestUserNameIsUnique(t *testing.T) {
	s := open(t)
	admin := dbapi.User{ID: "u-admin", Name: "admin", Roles: []string{"admin", "user"}, TOTP: []byte("secret")}
	if err := s.UserPut(ctx, admin); err != nil {
		t.Fatal(err)
	}
	// The same user again keeps his name.
	if err := s.UserPut(ctx, admin); err != nil {
		t.Fatalf("the same user again: %v", err)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-evil", Name: "admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("name taken: %v", err)
	}
	got, err := s.UserByName(ctx, "admin")
	if err != nil || got.ID != "u-admin" || string(got.TOTP) != "secret" || len(got.Roles) != 2 {
		t.Fatalf("by name: %+v, %v", got, err)
	}
	if _, err := s.UserByName(ctx, "ADMIN"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Error("the name was matched in a different case")
	}
	// Renaming frees the old name.
	admin.Name = "chef"
	if err := s.UserPut(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-other", Name: "admin"}); err != nil {
		t.Errorf("the freed name stayed taken: %v", err)
	}
}

// TestUserListOrder: by name, byte for byte, exactly as Go compares strings.
func TestUserListOrder(t *testing.T) {
	s := open(t)
	for _, name := range []string{"Zoe", "alice", "Bob", "ärger"} {
		if err := s.UserPut(ctx, dbapi.User{ID: "u-" + name, Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.UserList(ctx)
	if err != nil || len(list) != 4 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	want := []string{"Bob", "Zoe", "alice", "ärger"}
	for i, u := range list {
		if u.Name != want[i] {
			t.Fatalf("order: %v, want %v", []string{list[0].Name, list[1].Name, list[2].Name, list[3].Name}, want)
		}
	}
}

func TestSessionAndPending(t *testing.T) {
	s := open(t)
	token := bytes.Repeat([]byte{0x11}, 32)
	v := dbapi.Session{Token: token, User: "u-admin", Cert: []byte(`{"user":"u-admin"}`), Proof: []byte(`{}`),
		APIKey: bytes.Repeat([]byte{2}, 48), APICert: []byte(`{}`), Created: 1790000000, Expires: 1790003600, Seen: 1790000000}
	if err := s.SessionPut(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := s.SessionTouch(ctx, token, 1790000500); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionGet(ctx, token)
	if err != nil || got.Seen != 1790000500 || !bytes.Equal(got.APIKey, v.APIKey) || !bytes.Equal(got.Token, token) {
		t.Fatalf("get: %+v, %v", got, err)
	}
	if err := s.SessionTouch(ctx, []byte("nope"), 1790000500); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("touching an unknown session: %v", err)
	}
	if err := s.SessionPurge(ctx, 1790001000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionGet(ctx, token); err != nil {
		t.Errorf("purge removed a session that is still valid: %v", err)
	}

	k1, k2 := bytes.Repeat([]byte{0xC1}, 32), bytes.Repeat([]byte{0xC2}, 32)
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "dienste", Key: k1, Since: 1790000100}); err != nil {
		t.Fatal(err)
	}
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "web", Key: k2, Since: 1790000000}); err != nil {
		t.Fatal(err)
	}
	list, err := s.PendingList(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "web" {
		t.Fatalf("list: %+v, %v", list, err)
	}
}

// TestAUserRowNobodyOfOursWrote: the roles column is a value from outside
// like any other. A column longer than joinList ever writes would become a
// list of any size, so the way back out refuses it - what appDone does for
// App.Grants, scanUser does for User.Roles. The row is planted with SQL of
// the test's own, because the store has no way of writing one.
func TestAUserRowNobodyOfOursWrote(t *testing.T) {
	s, db := openDB(t)
	roles := strings.Repeat("admin\n", sqlstore.MaxList+1) + "admin"
	if _, err := db.ExecContext(ctx, "INSERT INTO ngc_user (id, name, roles, totp, locked, failures, "+
		"last_login) VALUES (?, ?, ?, ?, ?, ?, ?)", []byte("u-admin"), []byte("admin"), []byte(roles),
		[]byte(""), int64(0), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	if u, err := s.UserGet(ctx, "u-admin"); err == nil {
		t.Errorf("UserGet took %d roles out of one column", len(u.Roles))
	}
	if u, err := s.UserByName(ctx, "admin"); err == nil {
		t.Errorf("UserByName took %d roles out of one column", len(u.Roles))
	}
	if list, err := s.UserList(ctx); err == nil {
		t.Errorf("UserList took the same row: %+v", list)
	}
}

// TestAnEmptyTokenIsNobodysSession: every token and key goes to the driver
// through blob, so a nil one arrives as an empty one. That is allowed to
// change no answer, and it changes none because no row carries an empty token
// or an empty key: SessionPut and PendingPut refuse both, and that half is
// measured here too - without it the empty token would be a key like any
// other and this test would say nothing.
func TestAnEmptyTokenIsNobodysSession(t *testing.T) {
	s := open(t)
	if err := s.SessionPut(ctx, dbapi.Session{User: "u-admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("a session without a token: %v, want ErrInvalid", err)
	}
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "web"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("a controller without a key: %v, want ErrInvalid", err)
	}
	// A session that is there does not answer to the empty token either.
	token := bytes.Repeat([]byte{0x11}, 32)
	if err := s.SessionPut(ctx, dbapi.Session{Token: token, User: "u-admin", Expires: 1790003600}); err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string][]byte{"nil": nil, "empty": {}} {
		if _, err := s.SessionGet(ctx, token); !errors.Is(err, dbapi.ErrNotFound) {
			t.Errorf("SessionGet on a %s token: %v, want ErrNotFound", name, err)
		}
		if err := s.SessionTouch(ctx, token, 1790000500); !errors.Is(err, dbapi.ErrNotFound) {
			t.Errorf("SessionTouch on a %s token: %v, want ErrNotFound", name, err)
		}
		if err := s.SessionDelete(ctx, token); err != nil {
			t.Errorf("SessionDelete on a %s token: %v", name, err)
		}
		if err := s.PendingDelete(ctx, token); err != nil {
			t.Errorf("PendingDelete on a %s key: %v", name, err)
		}
	}
	if _, err := s.SessionGet(ctx, token); err != nil {
		t.Errorf("the session was removed by a delete that named no token: %v", err)
	}
}
