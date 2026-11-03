package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// The measurement of 03.11.2026 (docs/schloss-ablauf-riegel.md) asked what
// the deadline of the rollout lock really rests on, and the answer was: on
// the DIRECTION of one SQL comparison. These tests are that measurement, put
// where it runs by itself instead of in a program somebody has to find again.
//
// What they hold is the behaviour of SQLite on a column the migrations do NOT
// write, because that is the case the comparison answers differently from
// MariaDB: "until <= ?" is not numeric any more, and SQLite orders every text
// behind every number. The direction of today's statement makes that refuse
// too much and never too little, so no second owner gets the lock - and that
// is exactly what has to stay true when somebody reaches for this statement
// again.
//
// The same file for MariaDB is apps/server-db/classes/mariadb; there every
// bent column answers right, and why that is no reason to leave it out stands
// in that file.
const (
	lockNow   = int64(1790000000)
	lockUntil = lockNow + 600
)

// lockCtx is this file's own context: the one in sqlite_test.go belongs to
// the internal test package and is not in reach here.
var lockCtx = context.Background()

// bentStore gives a store whose ngc_lock.until carries the named column type.
// The schema comes from the migrations first, so what is bent here is one
// column of a real schema and not a hand-built table.
func bentStore(t *testing.T, typ string) (dbapi.Store, *sql.DB) {
	t.Helper()
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "state.db"),
		Key:    config.NewSecret(bytes.Repeat([]byte{9}, 64)),
	}
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := migrate.Apply(lockCtx, db, sqlstore.SQLite, "test", lockNow); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"DROP TABLE ngc_lock",
		"CREATE TABLE ngc_lock ( name BLOB NOT NULL , owner BLOB NOT NULL , until " + typ +
			" NOT NULL , PRIMARY KEY ( name ) )",
	} {
		if _, err := db.ExecContext(lockCtx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// The rig says what it did, not that it tried: a test that measured the
	// wrong column type would measure the ordinary case and pass.
	var got string
	if err := db.QueryRowContext(lockCtx,
		"SELECT type FROM pragma_table_info('ngc_lock') WHERE name = 'until'").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != typ {
		t.Fatalf("ngc_lock.until is %q, want %q", got, typ)
	}
	return sqlite.New(db), db
}

// hold puts a row of its own into ngc_lock: the deadline as text, which is
// what a bent column can hold and a whole column type means nothing without.
func hold(t *testing.T, db *sql.DB, owner string, until any) {
	t.Helper()
	if _, err := db.ExecContext(lockCtx, "DELETE FROM ngc_lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(lockCtx,
		"INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)",
		[]byte(dbapi.LockUpdate), []byte(owner), until); err != nil {
		t.Fatal(err)
	}
}

// TestLockTakeOnAWholeColumn is the control, and no test here means anything
// without it: on the column the migrations write, a foreign owner is refused
// while the deadline runs and gets the lock once it has passed. A rig that
// cannot show the second half is not measuring the deadline.
func TestLockTakeOnAWholeColumn(t *testing.T) {
	s, db := bentStore(t, "INTEGER")
	hold(t, db, "api-1", lockUntil)
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || ok {
		t.Errorf("foreign owner while the deadline runs: %v, %v, want false", ok, err)
	}
	hold(t, db, "api-1", lockNow-1)
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || !ok {
		t.Errorf("foreign owner after the deadline: %v, %v, want true", ok, err)
	}
	hold(t, db, "api-1", lockUntil)
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-1", lockNow, lockUntil+60); err != nil || !ok {
		t.Errorf("the owner renews: %v, %v, want true", ok, err)
	}
}

// TestLockTakeOnABentColumnStillRefuses is the measured case. In SQLite the
// other two columns of ngc_lock are BLOB already, so a migration that wrote
// "until {{text}}" instead of "until {{int}}" is one word away - and a BLOB
// column that carries text makes "until <= ?" false for every number,
// because SQLite orders text behind numbers and converts nothing.
//
// The lock holds: a second owner is refused. What the bent column costs is
// not safety but availability, and the second half of this test says it out
// loud - even a deadline that has LONG passed is not taken over any more, so
// the lock jams for good instead of for 600 seconds. That is measured, not
// feared, and it is written down here so that a change which "fixes" the
// jamming is seen for what it is: it moves this comparison, and then the
// bolt in Go is the only thing left.
func TestLockTakeOnABentColumnStillRefuses(t *testing.T) {
	s, db := bentStore(t, "BLOB")
	hold(t, db, "api-1", "1790000600")
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || ok {
		t.Errorf("foreign owner while the deadline runs: %v, %v, want false", ok, err)
	}
	hold(t, db, "api-1", "1789999999")
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || ok {
		t.Errorf("foreign owner after the deadline: %v, %v, want false - the bent column jams the lock", ok, err)
	}
	// The owner himself still gets through, because the statement asks for
	// his name as well and that half of it never touches the deadline. So a
	// bent column stops the takeover and not the renewal.
	hold(t, db, "api-1", "1790000600")
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-1", lockNow, lockUntil+60); err != nil || !ok {
		t.Errorf("the owner renews: %v, %v, want true", ok, err)
	}
}

// TestLockTakeOnATextColumnOrdersByDigits is the second of the two causes the
// measurement separated, and the one that is easy to read the wrong way. A
// TEXT column does make SQLite compare, but as text: the answer is right only
// while both numbers have as many digits. With 1790000600 against 1790000000
// it comes out right; with a deadline of fewer digits it does not.
//
// Unix seconds all have ten digits until the year 2286, so today this cause
// cannot reach the lock. That is a property of the numbers and not a bolt,
// and this test is where that sentence is kept honest.
func TestLockTakeOnATextColumnOrdersByDigits(t *testing.T) {
	s, db := bentStore(t, "TEXT")
	hold(t, db, "api-1", "1790000600")
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || ok {
		t.Errorf("ten digits, deadline running: %v, %v, want false", ok, err)
	}
	hold(t, db, "api-1", "1789999999")
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || !ok {
		t.Errorf("ten digits, deadline passed: %v, %v, want true", ok, err)
	}
	// Nine digits: the text comparison puts "999999999" after "1790000000",
	// so the deadline reads as running although the number is long gone. The
	// lock is refused - too much again, never too little.
	hold(t, db, "api-1", "999999999")
	if ok, err := s.LockTake(lockCtx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || ok {
		t.Errorf("nine digits, deadline long passed: %v, %v, want false - text orders by digits", ok, err)
	}
}
