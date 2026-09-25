package sqlstore_test

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
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
// before the test reads the row. Measured against the test files as they
// stood before this test was written, on this package and on every group of
// the contract this store answers - all of them but log, which is
// ErrUnsupported until task 7 and red in every run: last_login written as 0,
// cert and proof the wrong way round and created written as 0 turned none of
// them red, and roles written empty turned none of them red but dbtest's
// aliasing group. Here every row is written once and read back column for
// column.
//
// The delete is the same hole from the other side: nothing anywhere removed a
// session that was there. dbtest.testSessions deletes after its purge already
// took the row, TestAnEmptyTokenIsNobodysSession deletes with a token no row
// carries, and both ask nothing but err == nil - so "DELETE ... AND 1 = 0"
// turned nothing in this package red and no group of the contract either. Here
// the session is there, and it has to be gone afterwards.
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
// was still one statement behind.
//
// Three ways give that answer, and all three are walked here: the SELECT, the
// INSERT into the index, and the UPDATE renaming into it. They are one test
// because a caller may not tell them apart - it asks errors.Is and gets one
// word.
//
// Two of the three this test holds on its own: take the translation off the
// UPDATE, or the answer off the second round behind the INSERT, and it goes
// red. That is what UserPut translating at each statement is for; with one
// translation around both, either of the two could go and nothing here would
// notice. The third, the SELECT in front, it does not hold - the index gives
// the same word, so the whole check can be deleted with this test green.
// TestTheNameCheckSitsInTheTransaction is what holds that one, by reading
// where it stands.
func TestADuplicateNameIsErrInvalid(t *testing.T) {
	s, db := openDB(t)
	if _, err := db.ExecContext(ctx, "INSERT INTO ngc_user (id, name, roles, totp, locked, failures, "+
		"last_login) VALUES (?, ?, ?, ?, ?, ?, ?)", []byte(""), []byte("admin"), []byte(""),
		[]byte(""), int64(0), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	// No row carries "u-admin", so the INSERT runs and meets the index. The
	// second round of the write then finds no row of that ID either, which is
	// what says the index meant the name and not the primary key.
	if err := s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("the INSERT answered with %v, want ErrInvalid", err)
	}
	// The way in front: where the SELECT sees the name, it answers before a
	// write runs at all. Nothing here can tell that from the two behind it -
	// that is the point of the contract - so this only walks the way.
	if err := s.UserPut(ctx, dbapi.User{ID: "u-other", Name: "chef"}); err != nil {
		t.Fatalf("a free name: %v", err)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-evil", Name: "chef"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("the SELECT answered with %v, want ErrInvalid", err)
	}
	// The third way: u-other is there, so the rename is an UPDATE, and it
	// walks into the same index. Nothing but the name can stop that
	// statement, so it is the one place that answers this half.
	if err := s.UserPut(ctx, dbapi.User{ID: "u-other", Name: "admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("the UPDATE answered with %v, want ErrInvalid", err)
	}
	// The failed rename wrote nothing. That is the one statement being refused
	// whole, not the rollback behind it: no way through UserPut writes before
	// the statement that fails, so there is never anything for a rollback to
	// take back. Measured - store.write committing instead of rolling back in
	// the error case leaves this line green, and everything else in this
	// package with it.
	if got, err := s.UserGet(ctx, "u-other"); err != nil || got.Name != "chef" {
		t.Errorf("after the refused rename: %+v, %v", got, err)
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

// countChangedRows makes SQLite count the way MariaDB counts without the
// ClientFoundRows flag of its DSN: an UPDATE that finds a row it has nothing
// to change in affects no row. SQLite counts every row its UPDATE went
// through, so without this the two counting rules cannot be told apart here at
// all - and they have to be, because nothing in this repository opens a
// MariaDB connection yet and so nobody sets that flag today.
//
// RAISE(IGNORE) in a BEFORE UPDATE trigger drops that one row from the
// statement and leaves it with the values it already had, which is exactly
// what the other counting rule does. Every column of ngc_user is NOT NULL, so
// the WHEN compares values and never NULL, where an equality would be neither
// true nor false and the row would be counted after all.
const countChangedRows = `CREATE TRIGGER ngc_user_counts_changed_rows BEFORE UPDATE ON ngc_user
FOR EACH ROW WHEN NEW.name = OLD.name AND NEW.roles = OLD.roles AND NEW.totp = OLD.totp
 AND NEW.locked = OLD.locked AND NEW.failures = OLD.failures AND NEW.last_login = OLD.last_login
BEGIN SELECT RAISE(IGNORE); END`

// TestUserPutAnswersTheSameHoweverRowsAreCounted: what UserPut answers may not
// hang on whether the driver counts the rows an UPDATE matched or only the
// ones it really changed. Both are in use - it is the ClientFoundRows flag of
// the MariaDB DSN, which no connection in this repository sets today - and a
// method whose answer turns on it answers differently on two databases that
// are both doing what they promise.
//
// It is the same script twice, once on each counting rule, and what it holds
// is that the two runs say the same things in the same order. Whether those
// things are the right ones is held elsewhere: TestUserNameIsUnique and
// TestADuplicateNameIsErrInvalid ask that of the run with rows matched, which
// is the one SQLite gives on its own.
//
// The line that needs the second run is the second put of the same user, byte
// for byte. With rows counted as changed the UPDATE finds nothing to change,
// the INSERT behind it breaks on the primary key, and a valid repeated write
// used to come back as ErrInvalid: the contract's word for "the name belongs
// to another ID", for a user putting himself back exactly as he was.
//
// The fixture is measured before the script runs. A trigger that did not fire
// would leave both runs on one and the same database, and this test would be
// green while holding nothing.
func TestUserPutAnswersTheSameHoweverRowsAreCounted(t *testing.T) {
	admin := dbapi.User{ID: "u-admin", Name: "admin", Roles: []string{"admin", "user"},
		TOTP: []byte("secret"), Locked: 1790000600, Failures: 3, LastLogin: 1790000200}
	var said [2][]string
	for run, changedRows := range []bool{false, true} {
		counting := "with rows matched"
		if changedRows {
			counting = "with rows changed"
		}
		s, db := openDB(t)
		if changedRows {
			if _, err := db.ExecContext(ctx, countChangedRows); err != nil {
				t.Fatalf("%s: the trigger that turns the counting round: %v", counting, err)
			}
		}
		say := func(what string, err error) { said[run] = append(said[run], what+": "+fmt.Sprint(err)) }
		say("a user nobody wrote yet", s.UserPut(ctx, admin))

		want := int64(1)
		if changedRows {
			want = 0
		}
		if n := countedRows(t, db, admin, admin.LastLogin); n != want {
			t.Fatalf("%s: an UPDATE with the values already in the row counted %d rows, want %d; "+
				"this run does not count the way it says it does", counting, n, want)
		}
		if n := countedRows(t, db, admin, admin.LastLogin+1); n != 1 {
			t.Fatalf("%s: an UPDATE that really changes the row counted %d rows, want 1", counting, n)
		}
		if n := countedRows(t, db, admin, admin.LastLogin); n != 1 {
			t.Fatalf("%s: an UPDATE that changes the row back counted %d rows, want 1", counting, n)
		}

		say("the same user again, byte for byte", s.UserPut(ctx, admin))
		chef := admin
		chef.Name = "chef"
		say("the same user under a new name", s.UserPut(ctx, chef))
		say("that user again, byte for byte", s.UserPut(ctx, chef))
		say("another ID on the name he now has", s.UserPut(ctx, dbapi.User{ID: "u-evil", Name: "chef"}))

		// A name the SELECT of UserPut cannot see, so the INSERT walks into
		// the index: the row carries an empty ID, which this store never
		// writes, the way TestADuplicateNameIsErrInvalid plants it.
		if _, err := db.ExecContext(ctx, "INSERT INTO ngc_user (id, name, roles, totp, locked, "+
			"failures, last_login) VALUES (?, ?, ?, ?, ?, ?, ?)", []byte(""), []byte("taken"),
			[]byte(""), []byte(""), int64(0), int64(0), int64(0)); err != nil {
			t.Fatal(err)
		}
		say("a name only the index sees", s.UserPut(ctx, dbapi.User{ID: "u-new", Name: "taken"}))

		got, err := s.UserGet(ctx, admin.ID)
		say(fmt.Sprintf("the row of %s is %q %v %q %d %d %d", admin.ID, got.Name, got.Roles,
			got.TOTP, got.Locked, got.Failures, got.LastLogin), err)
	}
	for i, line := range said[0] {
		other := "nothing"
		if i < len(said[1]) {
			other = said[1][i]
		}
		if line != other {
			t.Errorf("the counting changed the answer\n  with rows matched: %s\n  with rows changed: %s",
				line, other)
		}
	}
	if len(said[1]) != len(said[0]) {
		t.Errorf("with rows matched %d answers were taken and with rows changed %d",
			len(said[0]), len(said[1]))
	}
}

// countedRows writes the row of u with SQL of the test's own and gives back
// how many rows the database says it affected. It sets every column of
// ngc_user, the way the UPDATE of UserPut does, so what the database counts
// here is what it counts there.
func countedRows(t *testing.T, db *sql.DB, u dbapi.User, lastLogin int64) int64 {
	t.Helper()
	res, err := db.ExecContext(ctx, "UPDATE ngc_user SET name = ?, roles = ?, totp = ?, locked = ?, "+
		"failures = ?, last_login = ? WHERE id = ?", []byte(u.Name), []byte(strings.Join(u.Roles, "\n")),
		u.TOTP, u.Locked, int64(u.Failures), lastLogin, []byte(u.ID))
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	return n
}
