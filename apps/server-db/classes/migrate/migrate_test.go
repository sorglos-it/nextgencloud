package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

var ctx = context.Background()

// openFile gives every test its own encrypted, empty file.
func openFile(t *testing.T) *sql.DB {
	t.Helper()
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "state.db"),
		Key:    config.NewSecret(bytes.Repeat([]byte{7}, 64)),
	}
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// stepwise is the other kind of database on SQLite: one where DDL cannot live
// in a transaction, so every statement stands and is recorded on its own.
// That is how MariaDB runs a migration, and this is what tests that path
// here. existed counts the statements the runner found already there, so a
// test can tell "stepped over one" from "ran the whole file again".
type stepwise struct {
	sqlstore.Dialect
	existed int
}

func (*stepwise) DDLInTx() bool { return false }

// Exists: SQLite has no error number of its own for "already there", so the
// text decides. It only has to hold for the statements of a migration.
func (d *stepwise) Exists(err error) bool {
	if err != nil && strings.Contains(err.Error(), "already exists") {
		d.existed++
		return true
	}
	return false
}

// tablesEmpty checks that every listed table is there and holds no row.
// ngc_schema is the exception: it holds what ran.
func tablesEmpty(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range sqlstore.Tables {
		var count int64
		// The name comes from the fixed list, never from outside.
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Errorf("table %s: %v", table, err)
		} else if table != "ngc_schema" && count != 0 {
			t.Errorf("table %s holds %d rows", table, count)
		}
	}
}

func TestApply(t *testing.T) {
	db := openFile(t)
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	n, err := Apply(ctx, db, sqlstore.SQLite, "test", 1790000000)
	if err != nil || n != len(files) {
		t.Fatalf("first run: %d migrations, %v; want %d", n, err, len(files))
	}
	level, err := Level(ctx, db)
	if err != nil || level != files[len(files)-1].Version {
		t.Fatalf("level: %d, %v", level, err)
	}
	// Every listed table is there and empty.
	tablesEmpty(t, db)
	// The runner shares ngc_lock with dbapi, so a lock that is not its own
	// has to survive the run, even under the same owner.
	if _, err := db.ExecContext(ctx, insertLock, []byte(dbapi.LockUpdate), []byte("test"), int64(1790009999)); err != nil {
		t.Fatal(err)
	}
	// A second run does nothing and does not fail.
	if n, err := Apply(ctx, db, sqlstore.SQLite, "test", 1790000100); err != nil || n != 0 {
		t.Errorf("second run: %d, %v", n, err)
	}
	var owner []byte
	if err := db.QueryRowContext(ctx, probeLock, []byte(dbapi.LockUpdate)).Scan(&owner); err != nil {
		t.Errorf("the runner took the lock %q of somebody else with it: %v", dbapi.LockUpdate, err)
	}
}

// TestApplyStepwise runs the same migrations the way a database without DDL
// in a transaction runs them: statement by statement, each one recorded.
func TestApplyStepwise(t *testing.T) {
	db := openFile(t)
	d := &stepwise{Dialect: sqlstore.SQLite}
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	n, err := Apply(ctx, db, d, "test", 1790000000)
	if err != nil || n != len(files) {
		t.Fatalf("first run: %d migrations, %v; want %d", n, err, len(files))
	}
	if d.existed != 0 {
		t.Errorf("%d statements were already there on an empty database", d.existed)
	}
	tablesEmpty(t, db)
	if n, err := Apply(ctx, db, d, "test", 1790000100); err != nil || n != 0 {
		t.Errorf("second run: %d, %v", n, err)
	}
}

// TestApplyResumes: a crash in the middle of a migration leaves a database
// the next run finishes. Three statements are already in, only two of them
// recorded - that is what a crash between a statement and its record leaves
// behind. The runner picks up at the record and steps over exactly that one
// statement instead of running the whole file again.
func TestApplyResumes(t *testing.T) {
	db := openFile(t)
	d := &stepwise{Dialect: sqlstore.SQLite}
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if err := bootstrap(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	for _, raw := range f.Statements[:3] {
		stmt, err := sqlstore.Fill(d, raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, insertState, f.Version, int64(2), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	n, err := Apply(ctx, db, d, "test", 1790000200)
	if err != nil || n != 1 {
		t.Fatalf("resumed run: %d migrations, %v", n, err)
	}
	if d.existed != 1 {
		t.Errorf("%d statements were already there; want the one that ran without being recorded", d.existed)
	}
	tablesEmpty(t, db)
	if level, err := Level(ctx, db); err != nil || level != f.Version {
		t.Errorf("level: %d, %v", level, err)
	}
}

// TestApplyWhileLocked: only one server migrates at a time. A second one
// stops with a message instead of racing. Nothing has run on this database,
// so there is work to do and the lock is the only thing that can stop it.
func TestApplyWhileLocked(t *testing.T) {
	db := openFile(t)
	if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	// Somebody else holds the lock and their time has not passed.
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte("someone-else"), int64(1790009999)); err != nil {
		t.Fatal(err)
	}
	n, err := Apply(ctx, db, sqlstore.SQLite, "two", 1790000001)
	if err == nil || !strings.Contains(err.Error(), "another server") {
		t.Fatalf("a second migration ran while the lock was held: %d, %v", n, err)
	}
	if n != 0 {
		t.Errorf("%d migrations ran", n)
	}
	if level, err := Level(ctx, db); err != nil || level != 0 {
		t.Errorf("the locked out server migrated anyway: level %d, %v", level, err)
	}
	// A run that never got the lock does not give it back either.
	var owner []byte
	if err := db.QueryRowContext(ctx, probeLock, []byte(lockName)).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if string(owner) != "someone-else" {
		t.Errorf("the lock now belongs to %q", owner)
	}
}

// TestApplyAfterCrashedOwner: a run that died holding the lock does not block
// every other server for good. Until lockFor seconds have passed nobody else
// gets in; after that the next server takes the lock over and migrates.
func TestApplyAfterCrashedOwner(t *testing.T) {
	db := openFile(t)
	if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	const died = 1790000000
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte("dead"), int64(died+lockFor)); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, sqlstore.SQLite, "next", died+lockFor-1); err == nil {
		t.Error("the lock was taken over a second too early")
	}
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	n, err := Apply(ctx, db, sqlstore.SQLite, "next", died+lockFor)
	if err != nil || n != len(files) {
		t.Fatalf("after the time ran out: %d migrations, %v; want %d", n, err, len(files))
	}
	tablesEmpty(t, db)
}
