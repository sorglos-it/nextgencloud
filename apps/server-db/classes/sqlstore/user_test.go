package sqlstore_test

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
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
// the contract this store answers - all of them but log, which answered
// errors.ErrUnsupported then and was red in every run until task 7 built it:
// last_login written as 0,
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
//
// Where the contract counts above come from, because no standing run gave
// them then. dbtest.Run had one call site in the whole tree - memstore's own
// test, against memstore.New() - and nothing under apps/server-db imported
// dbtest at all: no build tag, no environment switch. So go test ./... ran no
// group of the contract against this store, and every count above that names a
// group came from a run made by hand for the measurement and not kept. Read
// them as what a hand-made run answered once, not as a guard that stood then.
//
// Since task 7 a standing one does. TestConformance in classes/sqlite calls
// dbtest.Run on sqlite.Open, so go test ./... drives all eleven groups against
// this store, log among them, and what holds it day to day is this package and
// that run together. The package comment of dbtest - every implementation in
// apps/server-db/ has to pass Run, against MariaDB and SQLite - is true of the
// SQLite half from here on; no MariaDB connection is opened anywhere in this
// repository yet, so the other half is still a promise. shared/ is the
// coordinator's, so nothing there was changed for it.
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
	// whole, not the rollback behind it: the way out taken here answers before
	// any statement of the write has matched a row, so there is nothing for a
	// rollback to take back. Measured - store.write committing instead of
	// rolling back in the error case leaves this line green.
	//
	// It read "no way through UserPut writes before the statement that fails",
	// and that claims more than was counted. Seven of UserPut's eight ways out
	// that answer an error read that way from the source; the eighth - the
	// RowsAffected error right behind an UPDATE that has already run - does not,
	// and where that UPDATE met an existing row it has already changed it by
	// then. Whether a driver can take that path at all is not measured, here or
	// anywhere in this package, so it is named and left open.
	//
	// The rest of that reason is about UserPut and was once written down as
	// though it were about the package, which it is not: OnceUse writes its mark
	// before it reads the row, and there a rollback has work. The same mutation
	// is caught by TestAFailedWriteTakesBackWhatItWrote in once_test.go, which
	// carries the same count of eight, and before that test nothing in this
	// package caught it at all.
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
// statement and leaves it with the values it already had, so the statement
// counts it the way the other counting rule counts it. That much the two are
// the same, and that much is all this fixture needs. They are not the same
// throughout, and nothing here may be read as saying they are: MariaDB does
// touch the row it matched and only leaves it out of the count, while the
// trigger takes it out of the statement altogether. Nothing this test asks
// turns on the difference - it reads the answers of UserPut and the row
// afterwards, and both are the same either way. Every column of ngc_user is
// NOT NULL, so the WHEN compares values and never NULL, where an equality
// would be neither true nor false and the row would be counted after all.
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

// withoutTheNameIndex gives back the store with the unique index
// ngc_user_name dropped, and measures that it really is gone before anything
// rests on that.
//
// Why a test takes the index away at all. With it in place the promise "a
// name belongs to one ID" is kept whatever UserPut does about it, so nothing
// a caller can ask this store tells the check in the transaction apart from
// the index behind it. That is measured and not feared: on aa718c6 the whole
// check was made unreachable, once by wrapping it in a condition UserPut has
// already refused above (len(u.ID) == 0) and once by asking the question and
// throwing its answer away, and with the index in place the race below stayed
// green against both, 400 rounds of 32 writers each, three runs apiece.
//
// So the fixture is the same kind as countChangedRows above: SQL of the
// test's own, on a database of its own, that takes away one of two things
// which answer alike, so the other one has to answer alone. What the two
// tests below hold is what this store's own code keeps, not what its schema
// keeps - the schema is held by TestUserNameIsUnique and by the migration
// lint, and nothing here touches the database any other test meets.
//
// The proof runs at the top, at every namedRows the test asks, and once more
// from a t.Cleanup, and the last two of those places are each the correction
// of a fixture that had one fewer. A proof at the top says the index was gone
// when the store was handed over; it says nothing about the run that follows.
// Measured on 2a21a26, where it ran once: one line behind it,
// CREATE UNIQUE INDEX ngc_user_name ON ngc_user (name), left the package 142/0
// green with that proof passing - and with the index answering again, both
// tests below held nothing at all. On that drift the whole refusal deleted
// (case false: in place of the condition) was 142/0 too, where it is 140/2
// without it. On this tree that same drift is 141/2 in both tests, and both of
// the runs added since catch it, each naming the index: the proof at every
// namedRows reports first, from the test body, and the cleanup run reports
// after it.
//
// All three runs are the one function nameIndexGone, not copies of a proof
// that could drift apart, and it plants and removes its own two rows: every
// row the test wrote is left where it is.
//
// Why the proof in the body, and what was written here before it. The cleanup
// run does not catch a t.Cleanup registered after this one that drops the
// index again: cleanups run last-registered-first, so that one goes before
// this proof, and with the index standing the whole run the package was 143/0
// green. What used to stand here was that no further proof closes that, the
// reason given being the order itself - a third proof beaten by a cleanup
// registered after the third, a proof at every namedRows by one registered
// after that.
//
// Half of that is true and half of it was never measured. A proof registered
// as a cleanup can be beaten by a later cleanup; a proof in the test body
// cannot be touched by any cleanup at all, because cleanups run after the body
// has finished. The proof at every namedRows is in the body. Measured on this
// tree against exactly that drift - the index made again here and a t.Cleanup
// registered after this one dropping it - 141/2 in both fences, each naming
// the index, where without the body proof it is 143/0 green. The whole repair
// costs about 0.4 s: measured over three runs of the three tests it touches,
// one run took 2.87 s before it and 3.24 s after. Both numbers are one run and
// not the three added up.
//
// So the sentence that ended the search was the error, not the shape it turned
// down. What a test cannot do is make itself immune to arbitrary changes in
// the file it is written in; that is still true, and it is no licence to leave
// a shape unmeasured because an argument says it would not hold.
func withoutTheNameIndex(t *testing.T) (dbapi.Store, fixture) {
	t.Helper()
	s, db := openDB(t)
	f := fixture{db: db, standing: nameIndexGone}
	if _, err := db.ExecContext(ctx, "DROP INDEX ngc_user_name"); err != nil {
		t.Fatal(err)
	}
	if err := nameIndexGone(t, db); err != nil {
		t.Fatalf("two rows under one name were refused before this test began, so the index is "+
			"still there and this test would hold nothing: %v", err)
	}
	t.Cleanup(func() {
		if err := nameIndexGone(t, db); err != nil {
			t.Errorf("two rows under one name were refused after this test was done, so "+
				"ngc_user_name stood again: something between the proof above and here put it "+
				"back, and while it stood this test held nothing - it is the index that "+
				"refused, not the check in UserPut: %v", err)
		}
	})
	return s, f
}

// fixture is what withoutTheNameIndex and throughAViewWithoutAnID hand back
// beside the store: the database behind it, and that fixture's own proof that
// it is still standing.
//
// The two travel together so that counting rows cannot be written without
// re-proving the fixture the rows are counted in, and so that no test can pair
// itself with the other fixture's proof. That is why namedRows takes this and
// not a bare *sql.DB, and it is enough, because every use of the database in
// the three tests these two fixtures carry is a namedRows.
type fixture struct {
	db       *sql.DB
	standing func(*testing.T, *sql.DB) error
}

// nameIndexGone is the whole proof that ngc_user_name is not there: two rows
// under one name are written, and its own two rows are taken away again. It
// gives back what the database said to the first of the two that was refused.
//
// Two rows and not a look into sqlite_master, because the question is whether
// anything refuses a second row under a taken name - an index under another
// name or a trigger doing the same work would answer that question and not the
// other one.
//
// The name and the two IDs are rolled fresh on every call, and that is the
// whole of it: a proof that asks with a name written in the source asks a
// question whoever writes the source already knows. Measured on this tree with
// the fixed name it used to carry, one line after the DROP:
//
//	CREATE UNIQUE INDEX ngc_user_name2 ON ngc_user (name) WHERE name <> x'7477696365'
//
// x'7477696365' is "twice", the name this proof used to ask with, as a BLOB
// because the column holds one. Both runs of the proof came through honestly
// and passed, the index stood the whole run for every other name, and the
// package was 143/0 green with both fences below holding nothing. A proof at
// every namedRows does not see it either - measured, 143/0, because it asks
// with the same literal. It is not the number of proofs that was wrong, it is
// the name they ask with.
//
// The IDs are rolled for the same reason and it is the same measurement: the
// predicate written over id instead, WHERE id NOT IN (x'752d6f6e65',
// x'752d74776f') - "u-one" and "u-two", the IDs this proof used to plant - is
// 143/0 green with the name rolled but the IDs left standing. With both rolled
// each of the two lines is 141/2, caught at the top of the fixture.
//
// What this does not close, and why no further fence is written for it.
// Rolling the values does not make these two rows unfindable, because it is
// not their values that tell them from the rest of the table - it is their
// shape, and the shape stands fixed in the source above. The name is always 26
// bytes, both IDs are always 30, and each ID opens with one of the two
// four-byte prefixes written there.
// Every one of those is a handle: a partial index built on any one of them
// spares this proof and guards every name these tests write. Measured on this
// tree, one line after the DROP in withoutTheNameIndex, each on its own:
//
//	CREATE UNIQUE INDEX ngc_user_name2 ON ngc_user (name)
//	    WHERE substr(id,1,4) NOT IN (x'6f6e652d', x'74776f2d')
//	CREATE UNIQUE INDEX ngc_user_name2 ON ngc_user (name) WHERE length(name) <> 26
//	CREATE UNIQUE INDEX ngc_user_name2 ON ngc_user (name) WHERE length(id) <> 30
//
// x'6f6e652d' and x'74776f2d' are "one-" and "two-" as BLOBs, because the
// column holds one. All three are 143/0 green, with all three runs of this
// proof coming through. Beside the third of them the whole refusal deleted
// (case false: in place of the condition in UserPut) is 143/0 too, where that
// deletion alone is 141/2 - so under any of the three both fences below hold
// nothing at all. The same shape carries against the other fixture: built on
// ngc_user_rows, either predicate over id leaves the mutant
// TestAFailedQuestionIsNotAFreeName is for - other = "" in place of return err
// - 143/0 green, where alone it is 142/1.
//
// More rolling does not help, and that is why this is written down instead of
// answered. A name of random length still has a length. These two rows have to
// be findable by this proof, or it could not take them away again, and they
// have to be tellable from every other row, or they would not be its rows at
// all. Every property that does that telling is a handle for a predicate that
// spares them. So no fence is built here and none is owed: what this measures
// is the boundary itself, that this proof cannot immunise itself against a
// change to the file it stands in. That sentence was already written at
// withoutTheNameIndex; these three lines are the number under it.
//
// The other five columns are the weaker case of the same thing and stay as
// they were: they are written with the values below, and a predicate over one
// of them cannot pick out these rows and nothing else - locked = 0 holds for
// most rows the tests here write - so it spares this proof only by sparing
// much of the table with it, which is the narrowed-question shape already
// named at the fences.
//
// It removes its own two rows by ID and leaves every other row alone, so it
// can run again at every namedRows and once more when the test is done,
// without touching what the test wrote.
func nameIndexGone(t *testing.T, db *sql.DB) error {
	t.Helper()
	name := rand.Text()
	ids := []string{"one-" + name, "two-" + name}
	var refused error
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, "INSERT INTO ngc_user (id, name, roles, totp, locked, "+
			"failures, last_login) VALUES (?, ?, ?, ?, ?, ?, ?)", []byte(id), []byte(name),
			[]byte(""), []byte(""), int64(0), int64(0), int64(0)); err != nil && refused == nil {
			refused = err
		}
	}
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, "DELETE FROM ngc_user WHERE id = ?", []byte(id)); err != nil {
			t.Error(err)
		}
	}
	return refused
}

// namedRows is how many rows of ngc_user carry that name, asked of the
// database and not of the store: what a writer was told and what stands there
// are two different things, and both tests below want both.
//
// It proves the fixture is still standing before it counts, and that is the
// one place a proof cannot be reached from a t.Cleanup, because cleanups run
// after the body. The reasoning and the numbers are at withoutTheNameIndex.
// The proof plants rows of its own under a name nobody can name in advance, so
// it can never be the name being counted here.
func namedRows(t *testing.T, f fixture, name string) int {
	t.Helper()
	if err := f.standing(t, f.db); err != nil {
		t.Fatalf("the fixture this test runs in was not standing when its rows were counted, so "+
			"from here on it held nothing: %v", err)
	}
	var n int
	if err := f.db.QueryRowContext(ctx, "SELECT count(*) FROM ngc_user WHERE name = ?",
		[]byte(name)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// renameOntoTaken asks UserPut for the rename path - an ID this table already
// carries, asking for a name another row holds - and asks first that both
// halves of that are really so.
//
// It takes one value and asks its questions of that same value, because the
// dependency has to be carried by the code and not by a comment beside it.
// Measured on d2b34b4, where the comment was all there was: a UserDelete of
// u-admin above the rename, and a fresh ID in the rename itself, each left the
// package 142/0 green while the check that only refuses new IDs lived again.
// Through here each of those is red on the spot.
//
// One value and not two questions beside a write, because two can drift apart:
// measured then, with the questions written out at the call site and a fresh
// ID put into the write alone, the package stayed green and that check lived
// again. That one is not re-measured on this tree, so it carries no count.
// Here there is nothing to put it into - the questions and the write take the
// one argument - so that form cannot be written at all.
//
// Nothing holds that this call stays here, and that is a decision and not an
// oversight. Writing s.UserPut straight again leaves the package 143/0 green,
// measured on this tree, and the refusal itself stays caught; what goes is the
// two ways above going quiet once more. No question put to the store can see
// that, because what would be lost is not behaviour of the store but two
// questions this file asked.
//
// Two guards over this file's own source could, and neither is built. One
// names this function by hand: red on a rename or a move that changes nothing,
// and whoever took the call out would take the guard with it. The other is the
// form the reasoning here used to miss - a general check for unexported
// identifiers nobody uses, staticcheck's U1000 and its like, which names
// nothing by hand, survives a rename and cannot be carried off with the call.
// It is not measured: this repository has no linter, so whether it would catch
// this is an expectation and not a number. Naming it is what this paragraph
// owes; building a linter for one function is not.
func renameOntoTaken(t *testing.T, s dbapi.Store, u dbapi.User) error {
	t.Helper()
	if have, err := s.UserGet(ctx, u.ID); err != nil || have.Name == u.Name {
		t.Fatalf("%s has to carry a row of its own under another name here, or this is no"+
			" rename: %+v, %v", u.ID, have, err)
	}
	if have, err := s.UserByName(ctx, u.Name); err != nil || have.ID == u.ID {
		t.Fatalf("another ID has to hold %q here, or this asks for a free name: %+v, %v",
			u.Name, have, err)
	}
	return s.UserPut(ctx, u)
}

// TestTheCheckAloneRefusesATakenName: one writer after the other, with the
// index gone. A second ID may not be told it has a name that is taken, and it
// may not have it in the database either.
//
// This is the behaviour behind TestTheNameCheckSitsInTheTransaction, which
// can only read the source. A guard over the syntax tree cannot say that a
// statement runs and that its answer gates the write - that is a property of
// the run, not of the shape - and three ways past it were found in three
// rounds, the last two of them these: the check wrapped in a condition that
// can never hold, and the question asked with its answer thrown away. Both
// compile, both run, and on aa718c6 both left all 139 tests of this package
// green, the guard included, and every group of the contract with them. Here
// each of them is red at the second writer, at once, in five runs of five:
// "the second writer was told <nil>, want ErrInvalid", and two rows carrying
// the name.
//
// It costs no race to see it. Two writers one after the other are two
// writers, and a check whose answer never reaches the write lets the second
// one through whether or not anything ran at the same time.
//
// The put of "chef" and the one behind it are here so that a store which
// simply refuses everything cannot pass: renaming has to free the name again.
//
// No UserPut before renameOntoTaken renames onto a taken name, and that is not
// the same as saying they all drive the insert path, which is what stood here
// and was wrong. Measured with a probe on each statement UserPut can run: two
// of them reach the INSERT, one is refused by the check before any statement
// runs, and UserPut(u-admin, "chef") reaches the UPDATE - it is itself
// already the rename path, it just renames onto a name that is free, which the
// check lets through. What none of them asks is an ID this table already
// carries, asking for a name another row holds.
//
// A check that rejects only where no row yet carries u.ID answers every one of
// them exactly as UserPut does, guard included, and on 94e4809 it compiled,
// ran, and left all 142 tests of this package green. u-admin already carries a
// row by then - renamed to "chef" two statements above, eight lines of source
// - so asking there for u-evil's "admin" is that path, and it is red under
// that check at once: the rename comes back <nil> and two rows carry "admin".
//
// renameOntoTaken is what makes that line the rename path and not a comment
// saying so; what it asks and what was measured without it stand there.
//
// Every writer here carries either something or nothing in every field but ID
// and Name, and never the same one twice. u-evil carries roles, a TOTP secret,
// a lock, failures and a last login; u-admin carries the zero value of each.
// That is not decoration and may not be tidied away. Both refusals below would
// otherwise stand between rows alike in every column, and the refusal could
// then be hung on any one of them with nothing going red. Measured, four of
// those, each green across all 142 tests of the tree they were measured on
// while both writers were empty, and red once they were not: len(u.Roles) == 0,
// len(u.TOTP) == 0 and u.LastLogin == 0 added to the rejection, and
// "AND locked = 0" added to the question itself, which does not touch the Go
// code at all. Re-measured here, where the package is 143: all four are 141/2,
// red in this test and in the race beside it. The two refusals sit on opposite
// sides - at the second writer the user asking is the full one and the row
// holding the name is the empty one, at the rename it is the other way round -
// so a condition either way round drops one of them.
// TestOnlyOneWriterGetsAName carries the same thing, on alternate rounds
// because both sides of its refusals are racers; each of the two catches all
// four on its own, measured with the other one left out of the run.
//
// One shape this test does not reach, and what used to stand here about it was
// wrong. A question that fails reading as "the name is free" - other = "" in
// place of return err where the Scan answers an error that is not
// sql.ErrNoRows - is green here, and this comment said it would take a driver
// of the test's own, on two grounds. Both grounds hold: every column of
// ngc_user is NOT NULL, so no row a test can plant makes a scan of id into a
// string fail, and SQLite has no trigger on SELECT. Neither of them says what
// the name ngc_user has to stand for, and the conclusion drawn from them did.
// A view whose id column reads NULL, with INSTEAD-OF triggers carrying the
// write through to the table behind it, makes that Scan fail without taking a
// column from the INSERT or the UPDATE. TestAFailedQuestionIsNotAFreeName is
// that fixture, and the mutant is red there: the second writer is told <nil>
// and two rows carry the name.
//
// What this test and TestOnlyOneWriterGetsAName hold, and what they do not.
// They hold the property for the rows they write and the conditions they run:
// with ngc_user_name gone, a writer asking for a name another ID holds is
// refused, and one row carries that name. A change that narrows the check to
// something these two do not write, or hangs it on something they do not vary,
// they do not see. Two of those are measured on this tree, each on its own,
// each green across all 143 with both fences run:
//
//   - Narrowed by a column. Every name here is short and every ID is short, so
//     AND length(name) < 10 in userIDOf, && len(other) > 3 and && len(u.ID) >= 4
//     on the refusal are all green. The first of them, measured: a user named
//     "administrator" is told <nil> and two rows carry that name. (A fourth,
//     AND length(id) < 10, is red now - not through these two but through the
//     view of TestAFailedQuestionIsNotAFreeName, where that column is NULL and
//     the narrowed question matches nothing.)
//   - Hung on the dialect. && s.d.DDLInTx() is true on SQLite and false on
//     MariaDB: green here, and green in every group of the contract this store
//     answers. Measured against a dialect that wraps SQLite and answers
//     DDLInTx false: the second writer is told <nil> and two rows carry
//     "admin". The check is then dead exactly on the database the comment at
//     UserPut names it for.
//
// Both counts in this comment that name a group of the contract - the one on
// aa718c6 above and the one in the dialect line - came by hand, before task 7
// gave dbtest.Run a second call site and go test ./... a standing run of the
// contract against this store. The note at TestOneWriteAndOneDelete says it in
// full.
//
// Neither is left open for want of another round. Twelve ways were found in
// nine, each in a shape the round before had not thought of - hung on a
// field, the question narrowed, hung on the dialect, the fixture undermined,
// the fixture's repair undone by the order cleanups run in, the fixture's
// proof spared by name from an index that guarded everything else, and that
// same index spared instead by the shape of what the proof plants.
//
// The tenth and the eleventh are closed in the shape they were measured in, by
// a proof in the test body and by a proof that rolls its name and its two IDs
// fresh; both are set out at withoutTheNameIndex and nameIndexGone. The
// twelfth is not closed and nothing here is built for it. Its measurement is
// at nameIndexGone: a partial index that spares just the rows that proof
// plants - by their fixed prefix, or by the length of the name, or by the
// length of the ID - leaves both fences holding nothing, 143/0 even with the
// refusal deleted whole. No further rolling reaches it, because the proof's
// rows have to be tellable from every other row to be its rows at all, and
// whatever tells them carries the predicate that spares them.
//
// A test cannot make itself immune to arbitrary changes in the source it
// measures. That is the nature of the thing and not a weakness of these two,
// and the twelfth way is that sentence with a number under it rather than a
// new gap. What stands written here is what was measured; a sentence promising
// more than that is worse than none, because the next reader stops looking -
// and so is a sentence turning a shape down unmeasured, which is how the tenth
// stayed open a round longer than it had to.
func TestTheCheckAloneRefusesATakenName(t *testing.T) {
	s, f := withoutTheNameIndex(t)
	// evil carries something in every field but ID and Name, u-admin the zero
	// value of each. Four ways past the check live or die on that; see above.
	evil := dbapi.User{ID: "u-evil", Name: "admin", Roles: []string{"user"},
		TOTP: []byte("secret"), Locked: 1790000600, Failures: 3, LastLogin: 1790000200}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UserPut(ctx, evil); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("the second writer was told %v, want ErrInvalid", err)
	}
	if n := namedRows(t, f, "admin"); n != 1 {
		t.Errorf("%d rows carry the name admin, want 1", n)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "chef"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UserPut(ctx, evil); err != nil {
		t.Errorf("the freed name stayed taken: %v", err)
	}
	// The rename path, asked through renameOntoTaken so that the state it rests
	// on is asked of the same value the write is given.
	if err := renameOntoTaken(t, s, dbapi.User{ID: "u-admin", Name: "admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("the rename onto a taken name was told %v, want ErrInvalid", err)
	}
	if n := namedRows(t, f, "admin"); n != 1 {
		t.Errorf("%d rows carry the name admin after the refused rename, want 1", n)
	}
}

// TestOnlyOneWriterGetsAName: of writers racing for one free name exactly one
// is told he got it, and exactly one row carries it. With the index gone, so
// what holds that is the place of the check and nothing else.
//
// What this one holds and the test above does not: a check written in front
// of the write but asked outside the transaction. Lifted out of the literal
// and put to s.db - the pool, the last committed state - UserPut still
// refuses a second writer who comes afterwards, so the test above stays
// green; racing, every writer whose question ran before the first commit sees
// a free name and takes it. Measured against exactly that shape: red in five
// runs of five at GOMAXPROCS 1, 2 and 3, and inside the first rounds from 8
// processors upwards, with three to fifteen writers told they had the name.
// With 400 rounds of 32 it is five of five that low; with 200 it was two of
// three at two and at three processors, which is what the 400 are for.
// Against the store as it stands: green in five runs at full width and at 1,
// 2 and 4 processors, about 2.7 seconds.
//
// What it does not hold, said plainly. A check asked of s.db while standing
// inside the transaction does not answer wrong here; it takes a second
// connection of the pool while the transaction holds the only writer -
// maxOpenConns is 4 against 32 racers - and this test then does not fail, it
// runs into the timeout of go test (measured against a 45 s bound: "test
// timed out"). That shape is the one the guard sees in the source at no cost.
// Nor does this test hold anything while the index stands: with it, both
// unreachable-check shapes above came through here green.
//
// The racers of every other round carry something in every field but ID and
// Name, and the rounds between carry the zero value of each. Same reason as at
// TestTheCheckAloneRefusesATakenName, which says it at length: a refusal
// standing between rows alike in every column can be hung on any one of them
// with nothing going red. Here both sides of a refusal are racers, so one
// round cannot have a full asker and an empty holder at once - which of the
// 32 wins is not ours to say - and the two shapes are put on alternate rounds
// instead. That is measured and not argued. With every racer empty, as they
// stood, all four ways past the check left this test green, while their four
// opposites - the same conditions turned round - were red; every racer full
// would have swapped exactly that, which is why neither shape alone will do.
// Alternating, all eight are red here, each on its own, with the test beside
// it left out of the run.
//
// And what nothing on SQLite holds: whether MariaDB or Galera would let two
// transactions past this check together. SQLite gives one writer the database
// at a time, so what is measured here is that the check is inside that one
// writer's transaction, not that a multi-master engine certifies the two
// apart.
//
// What this test holds is the place of the check for the names it races and
// the shapes it writes - short names, short IDs, every field either full or
// zero. A check narrowed to something outside that, or hung on something this
// test does not vary, stands untouched here: measured, AND length(name) < 10
// in userIDOf, && len(other) > 3 and && len(u.ID) >= 4 on the refusal, and
// && s.d.DDLInTx(), each green across all 143 with both fences run, and the
// last of them dead on MariaDB, which is the database it is written for. The
// four are set out at TestTheCheckAloneRefusesATakenName, with what each one
// lets through. They are named and not closed: twelve ways in nine rounds,
// each a shape the round before had not thought of, the last three of them
// against the fixture rather than the check. The tenth and the eleventh are
// closed in the shape they were measured in; the twelfth is not, and nothing
// here is built for it - a partial index that spares just the rows the
// fixture's proof plants leaves both fences holding nothing, measured at
// nameIndexGone. No test immunises itself against arbitrary changes to the
// source it measures. The promise here is the one that was measured.
func TestOnlyOneWriterGetsAName(t *testing.T) {
	const racers, rounds = 32, 400
	s, f := withoutTheNameIndex(t)
	fullRounds := 0
	for round := range rounds {
		name := "n" + strconv.Itoa(round)
		full := round%2 == 0
		if full {
			fullRounds++
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, racers)
		for i := range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				u := dbapi.User{
					ID:   "u" + strconv.Itoa(round) + "-" + strconv.Itoa(i),
					Name: name,
				}
				if full {
					u.Roles, u.TOTP = []string{"user"}, []byte("secret")
					u.Locked, u.Failures, u.LastLogin = 1790000600, 3, 1790000200
				}
				<-start
				errs[i] = s.UserPut(ctx, u)
			}()
		}
		close(start)
		wg.Wait()
		ok, invalid, other := 0, 0, 0
		for i := range racers {
			switch {
			case errs[i] == nil:
				ok++
			case errors.Is(errs[i], dbapi.ErrInvalid):
				invalid++
			default:
				other++
				if other <= 2 {
					t.Logf("round %d, racer %d: %v", round, i, errs[i])
				}
			}
		}
		if rows := namedRows(t, f, name); ok != 1 || rows != 1 {
			t.Fatalf("round %d: %d writers were told they got %q and %d rows carry it - "+
				"want 1 and 1; %d got ErrInvalid, %d something else",
				round, ok, name, rows, invalid, other)
		}
	}
	// Both shapes have to have raced, or half the eight above go quiet. Nothing
	// this test asks of the store can say that, so it is counted here: measured,
	// full := true and full := false each left this test green without it.
	if fullRounds != rounds/2 {
		t.Errorf("%d of %d rounds raced writers carrying something in every field, want %d - "+
			"with one shape alone four of the eight ways past the check are green here",
			fullRounds, rounds, rounds/2)
	}
}

// throughAViewWithoutAnID puts a view in front of ngc_user whose id column
// reads NULL, with INSTEAD-OF triggers carrying the INSERT and the UPDATE
// through to the table behind it, and takes the unique index away as
// withoutTheNameIndex does.
//
// What it is for. The question UserPut asks in front of its write - SELECT id
// FROM ngc_user WHERE name = ? - reads that one column into a Go string, and a
// NULL cannot be read into one: the question fails where it used to answer.
// The write does not, because a view replaces the column that is read without
// taking one away from the INSERT and the UPDATE behind it.
//
// This corrects a sentence that stood at TestTheCheckAloneRefusesATakenName
// and was wrong. It said this shape would take a driver of the test's own, on
// two grounds that are both true - every column of ngc_user is NOT NULL, so no
// row a test can plant makes that scan fail, and SQLite has no trigger on
// SELECT - and the conclusion drawn from them was not. Neither ground says
// anything about what the name ngc_user has to stand for.
//
// The index has to go with it. With the index in place the INSERT meets
// ngc_user_name in the table behind the view, and UserPut comes back with
// ErrInvalid after all, for the wrong reason. Measured: with the index left
// standing it is 142/1 whether the mutant is there or not, because this
// fixture's own proof refuses to start - two rows under one name are refused,
// which is exactly what it asks about. Without the index it is 143/0 clean and
// 142/1 with the mutant. What used to stand here, 143/0 with the index, was
// never measurable on this tree: the proof above sits in the way of it.
//
// SQLite only, like countChangedRows above: it is a shape made for one
// question, on a database of its own, and nothing else in this package meets
// it.
func throughAViewWithoutAnID(t *testing.T) (dbapi.Store, fixture) {
	t.Helper()
	s, db := openDB(t)
	f := fixture{db: db, standing: theViewAnswers}
	for _, stmt := range []string{
		"DROP INDEX ngc_user_name",
		"ALTER TABLE ngc_user RENAME TO ngc_user_rows",
		"CREATE VIEW ngc_user (id, name, roles, totp, locked, failures, last_login) AS " +
			"SELECT NULL, name, roles, totp, locked, failures, last_login FROM ngc_user_rows",
		"CREATE TRIGGER ngc_user_insert INSTEAD OF INSERT ON ngc_user BEGIN " +
			"INSERT INTO ngc_user_rows (id, name, roles, totp, locked, failures, last_login) VALUES " +
			"(NEW.id, NEW.name, NEW.roles, NEW.totp, NEW.locked, NEW.failures, NEW.last_login); END",
		"CREATE TRIGGER ngc_user_update INSTEAD OF UPDATE ON ngc_user BEGIN " +
			"UPDATE ngc_user_rows SET name = NEW.name, roles = NEW.roles, totp = NEW.totp, " +
			"locked = NEW.locked, failures = NEW.failures, last_login = NEW.last_login " +
			"WHERE name = OLD.name; END",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := theViewAnswers(t, db); err != nil {
		t.Fatalf("before this test began: %v", err)
	}
	t.Cleanup(func() {
		if err := theViewAnswers(t, db); err != nil {
			t.Errorf("after this test was done, so it did not stand while the test ran and this "+
				"test held nothing: %v", err)
		}
	})
	return s, f
}

// theViewAnswers is the whole proof that the fixture above is standing, and it
// is both halves of it at once: two rows go in under one name, so nothing
// refuses a duplicate name any more, and the question UserPut asks about that
// name comes back with something that cannot be read into a string, which is
// the one thing this fixture is for. It takes its own two rows away again and
// leaves every other row where it is.
//
// It runs at the top, at every namedRows and from a t.Cleanup, for the reason
// written out at withoutTheNameIndex: a proof that runs once at the top proves
// something about the setup and nothing about the run, and only the run in the
// body is out of reach of a later cleanup.
//
// Its name and its two IDs are rolled fresh on every call, for the reason
// written out at nameIndexGone: a proof that asks with a name from the source
// asks a question whoever writes the source already knows, and a partial index
// sparing exactly that name lets the proof pass while it guards every other.
//
// The bound on that is written out at nameIndexGone too, and it is measured
// against this fixture as well: what is rolled here is the values and not the
// shape, so a partial index on ngc_user_rows keyed on the shape of these two
// rows - substr(id,1,4) NOT IN (x'6f6e652d', x'74776f2d'), or length(id) <> 30
// - lets this proof through while it guards the rest, and the mutant the test
// below is for goes green with it, 143/0 where alone it is 142/1.
func theViewAnswers(t *testing.T, db *sql.DB) error {
	t.Helper()
	name := rand.Text()
	ids := []string{"one-" + name, "two-" + name}
	var bad error
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, "INSERT INTO ngc_user (id, name, roles, totp, locked, "+
			"failures, last_login) VALUES (?, ?, ?, ?, ?, ?, ?)", []byte(id), []byte(name),
			[]byte(""), []byte(""), int64(0), int64(0), int64(0)); err != nil && bad == nil {
			bad = fmt.Errorf("two rows under one name were refused, so something still keeps the "+
				"name unique and the refusal below need not be UserPut's: %w", err)
		}
	}
	var other string
	err := db.QueryRowContext(ctx, "SELECT id FROM ngc_user WHERE name = ?", []byte(name)).Scan(&other)
	if err == nil && bad == nil {
		bad = fmt.Errorf("the question about a taken name answered %q, so the view is not in "+
			"front of ngc_user and nothing here makes that question fail", other)
	}
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, "DELETE FROM ngc_user_rows WHERE id = ?", []byte(id)); err != nil {
			t.Error(err)
		}
	}
	return bad
}

// TestAFailedQuestionIsNotAFreeName: the question in front of the write
// answers three ways - a row, no row, or an error - and the third one may not
// be taken for the second. A store that reads "I could not ask" as "the name
// is free" writes the second row and tells its writer he got the name.
//
// With the index gone and the view in front, UserPut's SELECT cannot read what
// it is given. Measured, other = "" in place of return err where the Scan
// answers an error that is not sql.ErrNoRows: the second writer is told <nil>
// and two rows carry "admin", where the store as it stands hands the driver's
// error on and one row carries it. Until this test that mutant was green
// across the whole package, and written down at
// TestTheCheckAloneRefusesATakenName as a shape nothing here could reach.
//
// What is asked of the answer is that there is one, not which one it is. The
// error the driver gives for a NULL it cannot read is the fixture's, not the
// store's, and the store owes here only that it does not swallow it. What one
// row and not two says is the same thing from the database's side, and it is
// the half that cannot be argued with.
//
// The other half is open, and this is what it costs. A store that swallows the
// driver's error and answers one of its own passes here: return dbapi.ErrInvalid
// in place of return err is 143/0 green, measured. The caller is then told "the
// name belongs to another ID" where the truth is that the question could not be
// asked, and errors.Is finds nothing of the failure in it. Holding that would
// mean asking for the fixture's own error by its text, which is the fixture
// speaking and not the store, so it is written down instead of built.
func TestAFailedQuestionIsNotAFreeName(t *testing.T) {
	s, f := throughAViewWithoutAnID(t)
	if err := s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "admin"}); err != nil {
		t.Fatal(err)
	}
	if n := namedRows(t, f, "admin"); n != 1 {
		t.Fatalf("%d rows carry the name admin before the second writer, want 1", n)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-evil", Name: "admin"}); err == nil {
		t.Error("the second writer was told <nil>, want the error the question came back with")
	}
	if n := namedRows(t, f, "admin"); n != 1 {
		t.Errorf("%d rows carry the name admin, want 1", n)
	}
}
