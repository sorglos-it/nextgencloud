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

// TestOneWriteAndOneDelete: what the INSERT writes and what the DELETE
// removes, with nothing in between to cover either up.
//
// Half the INSERT was unguarded, and a second write is what covered it.
// TestUserNameIsUnique puts the same user twice, and the second put is an
// UPDATE that sets every column: it repairs whatever the INSERT got wrong
// before the test reads the row. Measured on this package and on every group
// of the contract - last_login written as 0, cert and proof the wrong way
// round and created written as 0 passed all of it, and roles written empty
// passed everything but dbtest's aliasing group. Here every row is written
// once and read back column for column.
//
// The delete is the same hole from the other side: nothing anywhere removed a
// session that was there. dbtest.testSessions deletes after its purge already
// took the row, TestAnEmptyTokenIsNobodysSession deletes with a token no row
// carries, and both ask nothing but err == nil - so "DELETE ... AND 1 = 0"
// passed the package and the contract as well. Here the session is there, and
// it has to be gone afterwards.
func TestOneWriteAndOneDelete(t *testing.T) {
	s := open(t)
	u := dbapi.User{ID: "u-admin", Name: "admin", Roles: []string{"admin", "user"},
		TOTP: []byte("secret"), Locked: 1790000600, Failures: 3, LastLogin: 1790000200}
	if err := s.UserPut(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := s.UserGet(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID || got.Name != u.Name || strings.Join(got.Roles, ",") != "admin,user" ||
		string(got.TOTP) != "secret" || got.Locked != u.Locked || got.Failures != u.Failures ||
		got.LastLogin != u.LastLogin {
		t.Errorf("one write left %+v, want %+v", got, u)
	}

	// Every byte slice of the session differs from every other, so a column
	// read or written in the wrong place cannot pass for its neighbour.
	token := bytes.Repeat([]byte{0x11}, 32)
	v := dbapi.Session{Token: token, User: "u-admin", Cert: []byte("cert"), Proof: []byte("proof"),
		APIKey: []byte("api-key"), APICert: []byte("api-cert"),
		Created: 1790000001, Expires: 1790003602, Seen: 1790000003}
	if err := s.SessionPut(ctx, v); err != nil {
		t.Fatal(err)
	}
	gotV, err := s.SessionGet(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotV.Token, v.Token) || gotV.User != v.User || !bytes.Equal(gotV.Cert, v.Cert) ||
		!bytes.Equal(gotV.Proof, v.Proof) || !bytes.Equal(gotV.APIKey, v.APIKey) ||
		!bytes.Equal(gotV.APICert, v.APICert) || gotV.Created != v.Created ||
		gotV.Expires != v.Expires || gotV.Seen != v.Seen {
		t.Errorf("one write left %+v, want %+v", gotV, v)
	}

	p := dbapi.Pending{Name: "web", Key: bytes.Repeat([]byte{0xC1}, 32), Since: 1790000100}
	if err := s.PendingPut(ctx, p); err != nil {
		t.Fatal(err)
	}
	list, err := s.PendingList(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if !bytes.Equal(list[0].Key, p.Key) || list[0].Name != p.Name || list[0].Since != p.Since {
		t.Errorf("one write left %+v, want %+v", list[0], p)
	}

	// The session is there, so a delete that names it has to take it.
	if err := s.SessionDelete(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionGet(ctx, token); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after delete: %v, want ErrNotFound", err)
	}
}

// TestADuplicateNameIsErrInvalid: dbapi promises that the same name for
// another ID gives ErrInvalid, and the store owes that answer however the
// name turns out to be taken - by the SELECT of UserPut or by the unique
// index ngc_user_name behind it. Without the translation the caller gets the
// driver's "UNIQUE constraint failed" and errors.Is finds nothing in it.
//
// The race that reaches the index - two writers past the SELECT together - is
// not measurable on SQLite, so the shape is made instead of waited for: a row
// whose name is taken and whose ID is empty. UserPut refuses to write such a
// row, so it is planted with SQL of the test's own, the way
// TestAUserRowNobodyOfOursWrote plants its column. The SELECT then reads an
// empty ID and lets the write through, exactly as it would for a writer that
// was still one statement behind, and the INSERT walks into the index.
func TestADuplicateNameIsErrInvalid(t *testing.T) {
	s, db := openDB(t)
	if _, err := db.ExecContext(ctx, "INSERT INTO ngc_user (id, name, roles, totp, locked, failures, "+
		"last_login) VALUES (?, ?, ?, ?, ?, ?, ?)", []byte(""), []byte("admin"), []byte(""),
		[]byte(""), int64(0), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("the index answered with %v, want ErrInvalid", err)
	}
	// The way in front is still the way in front: where the SELECT sees the
	// name, it answers before a write runs at all, and the translation behind
	// it leaves that untouched. Both halves have to give the same error,
	// otherwise a caller could tell them apart.
	if err := s.UserPut(ctx, dbapi.User{ID: "u-other", Name: "chef"}); err != nil {
		t.Fatalf("a free name: %v", err)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-evil", Name: "chef"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("the SELECT answered with %v, want ErrInvalid", err)
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
