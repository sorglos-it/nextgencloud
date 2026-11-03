package mariadb_test

import (
	"database/sql"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/mariadb"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// The MariaDB half of the measurement of 03.11.2026
// (docs/schloss-ablauf-riegel.md). The SQLite half is in
// apps/server-db/classes/sqlite/lockdeadline_test.go, and it is the one that
// shows a column type changing an answer.
//
// Here nothing changes: measured against 11.8.6-MariaDB-0+deb13u1, BIGINT,
// VARCHAR(64) and VARBINARY(64) all answered right, with the deadline written
// as a number and written as text - twelve rows, twelve times right, because
// MariaDB converts for the comparison. So on this database the bent column is
// NOT PRODUCIBLE as a mistake, and that sentence is the reason this file
// exists instead of being left out: a measurement that is missing leaves
// nothing behind to read, while a test that passes for a named reason does.
//
// The cases below are therefore not "the bent column breaks it" but "the bent
// column does not": the same three questions as on SQLite, and all three come
// out the way they come out on a whole column.
const (
	lockNow   = int64(1790000000)
	lockUntil = lockNow + 600
)

// bentLock gives the migrate account and a store, with ngc_lock.until changed
// to the named column type. The migrate account is the only one allowed to
// change a column - the account of the API may not, and TestAccountRights
// holds that - so the DDL goes through that handle and the questions through
// the store of the API, exactly as in the installation.
func bentLock(t *testing.T, typ string) (dbapi.Store, *sql.DB) {
	t.Helper()
	s := fresh(t) // applies the migrations and empties every table
	admin, err := mariadb.OpenDB(clusterConfig(t, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// The column goes back, or every later test in this package would
		// run against a schema this one bent.
		if _, err := admin.ExecContext(ctx, "ALTER TABLE ngc_lock MODIFY until BIGINT NOT NULL"); err != nil {
			t.Errorf("die Spalte zurueckbauen: %v", err)
		}
		admin.Close()
	})
	if _, err := admin.ExecContext(ctx, "ALTER TABLE ngc_lock MODIFY until "+typ+" NOT NULL"); err != nil {
		t.Fatalf("die Spalte verbiegen: %v", err)
	}
	// The rig says what it did. MariaDB echoes the type back in its own
	// spelling - bigint(20) for BIGINT - so the check is on the name in
	// front of the bracket and not on the whole word.
	var got string
	if err := admin.QueryRowContext(ctx,
		"SELECT COLUMN_TYPE FROM information_schema.columns WHERE table_schema = DATABASE() "+
			"AND table_name = 'ngc_lock' AND column_name = 'until'").Scan(&got); err != nil {
		t.Fatal(err)
	}
	t.Logf("ngc_lock.until ist %q", got)
	return s, admin
}

// holdRow puts a row of its own into ngc_lock, the deadline as given: a
// number for the ordinary case, a string for the case a bent column allows.
func holdRow(t *testing.T, db *sql.DB, owner string, until any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "DELETE FROM ngc_lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)",
		[]byte(dbapi.LockUpdate), []byte(owner), until); err != nil {
		t.Fatal(err)
	}
}

// askTheThree asks the three questions that decide whether a deadline is a
// deadline, and every one of them is a control of the other two: refused
// while it runs, taken over once it has passed, renewed by its own owner.
// Without the middle one a rig that refuses everything would pass.
func askTheThree(t *testing.T, s dbapi.Store, db *sql.DB, running, passed any) {
	t.Helper()
	holdRow(t, db, "api-1", running)
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || ok {
		t.Errorf("foreign owner while the deadline runs: %v, %v, want false", ok, err)
	}
	holdRow(t, db, "api-1", passed)
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || !ok {
		t.Errorf("foreign owner after the deadline: %v, %v, want true", ok, err)
	}
	holdRow(t, db, "api-1", running)
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", lockNow, lockUntil+60); err != nil || !ok {
		t.Errorf("the owner renews: %v, %v, want true", ok, err)
	}
}

// TestLockTakeOnAWholeColumn is the control: the column the migrations write,
// the deadline as a number.
func TestLockTakeOnAWholeColumn(t *testing.T) {
	s, db := bentLock(t, "BIGINT")
	askTheThree(t, s, db, lockUntil, lockNow-1)
}

// TestLockTakeOnABentColumnAnswersTheSame: VARCHAR and VARBINARY, and the
// deadline written as text on top of that - the two things that turn the
// comparison on SQLite. MariaDB converts, so all three questions answer as
// they do on BIGINT. This is the test that says "not producible here", and it
// says it by passing.
func TestLockTakeOnABentColumnAnswersTheSame(t *testing.T) {
	for _, typ := range []string{"VARCHAR(64)", "VARBINARY(64)"} {
		t.Run(typ, func(t *testing.T) {
			s, db := bentLock(t, typ)
			askTheThree(t, s, db, "1790000600", "1789999999")
		})
	}
}

// TestLockTakeOnATextColumnDoesNotOrderByDigits is the other half of the
// SQLite finding: there a TEXT column compares by digits, so a deadline of
// nine digits reads as later than one of ten. Here it does not, and the
// deadline that has long passed is taken over.
func TestLockTakeOnATextColumnDoesNotOrderByDigits(t *testing.T) {
	s, db := bentLock(t, "VARCHAR(64)")
	holdRow(t, db, "api-1", "999999999")
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", lockNow, lockUntil); err != nil || !ok {
		t.Errorf("nine digits, deadline long passed: %v, %v, want true", ok, err)
	}
}
