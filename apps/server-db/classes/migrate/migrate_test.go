package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

var ctx = context.Background()

// OpenEncrypted opens the encrypted file these tests run on. It is the real
// opener of apps/server-db, sqlite.OpenDB, and encrypted_test.go beside this
// file hands it over: that file is in the external test package, which may
// import sqlite, while this one may not. sqlite imports this package since
// task 7 - sqlite.Open applies the migrations - and a package under test that
// imports something importing it back is an import cycle to go test, measured:
// "imports ... sqlite from migrate_test.go imports ... migrate from store.go:
// import cycle not allowed in test".
//
// Going through the real opener is the point. Opening the file here with the
// driver instead would leave these tests measuring a database nobody runs,
// and the hexkey PRAGMA could change in sqlite without a test here noticing.
var OpenEncrypted func(config.DB) (*sql.DB, error)

// OpenEncryptedBusy is the same opener with the busy deadline of the pool
// given, and it is handed over by the same file for the same reason. Only
// TestApplySaysWhoHoldsTheFile uses it: that case waits the deadline out on
// purpose, and the production one is ten seconds (sqlite.DefaultBusyTimeout,
// Punkt 48). It goes through the same sqlite.OpenDBWith the server would, so
// what it measures is still the file NGC opens - with a shorter clock on the
// one number that case is not about.
var OpenEncryptedBusy func(config.DB, time.Duration) (*sql.DB, error)

// openFile gives every test its own encrypted, empty file.
func openFile(t *testing.T) *sql.DB {
	t.Helper()
	if OpenEncrypted == nil {
		t.Fatal("OpenEncrypted is not set; encrypted_test.go is what sets it")
	}
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "state.db"),
		Key:    config.NewSecret(bytes.Repeat([]byte{7}, 64)),
	}
	db, err := OpenEncrypted(cfg)
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

// atType gets a test into the middle of a run without a second process:
// sqlstore.Fill asks the dialect for every column type, so do runs while the
// runner is filling the nth one. Like stepwise it records every statement on
// its own, the way MariaDB does.
type atType struct {
	sqlstore.Dialect
	nth   int
	calls int
	do    func()
}

func (*atType) DDLInTx() bool { return false }

func (d *atType) Type(name string) string {
	d.calls++
	if d.calls == d.nth {
		d.do()
	}
	return d.Dialect.Type(name)
}

// noDuplicate stands for a database that answers the INSERT of the lock with
// something that is not a duplicate key - a connection that broke, say.
type noDuplicate struct{ sqlstore.Dialect }

func (noDuplicate) Duplicate(error) bool { return false }

// spy passes every question on to the database and keeps the last one, so a
// test can see what hold asks and with how many values.
type spy struct {
	db    *sql.DB
	query string
	args  []any
}

func (s *spy) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	s.query, s.args = query, args
	return s.db.QueryRowContext(ctx, query, args...)
}

// ownerInTheLock runs the migrations under a node ID and gives back what
// stood in the lock row while they were running.
func ownerInTheLock(t *testing.T, node string) string {
	t.Helper()
	firstStatementHasAType(t)
	db := openFile(t)
	d := &atType{Dialect: sqlstore.SQLite}
	if err := bootstrap(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	var got []byte
	// The first type of the first statement: the run is under way and the
	// lock is taken. Which statement that is does not matter, so nothing here
	// changes when 0001 does.
	d.calls, d.nth = 0, 1
	d.do = func() {
		if err := db.QueryRowContext(ctx, probeLock, []byte(lockName)).Scan(&got); err != nil {
			t.Error(err)
		}
	}
	if _, err := Apply(ctx, db, d, node, 1790000000); err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// TestTheLockCarriesARunOfItsOwn: the lock belongs to one run, not to one
// machine. Two processes on a node carry one node ID, so the node ID alone
// would make the second one look like the first: it could take the lock over
// while the first is still writing, and neither of them could ever notice.
func TestTheLockCarriesARunOfItsOwn(t *testing.T) {
	a, b := ownerInTheLock(t, "node"), ownerInTheLock(t, "node")
	if !strings.HasPrefix(a, "node") {
		t.Errorf("the lock does not say which node is migrating: %q", a)
	}
	if a == "node" {
		t.Fatalf("the lock holds the node ID itself, so two runs of one node cannot be told apart: %q", a)
	}
	if a == b {
		t.Errorf("two runs of one node wrote the same owner: %q", a)
	}
	if len(a) > sqlstore.MaxText {
		t.Errorf("the owner is %d bytes, the column takes %d", len(a), sqlstore.MaxText)
	}
}

// TestApplyStopsWhenTheSameNameTakesOver is the same story as
// TestApplyStopsWhenTheLockChangesHands, told with the name the run carries
// itself: a second process on the same node. It is the one that used to go
// wrong - the run kept writing, recorded the migration as done and deleted
// the lock of the process that had taken over.
func TestApplyStopsWhenTheSameNameTakesOver(t *testing.T) {
	firstStatementHasAType(t)
	db := openFile(t)
	d := &atType{Dialect: sqlstore.SQLite}
	if err := bootstrap(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	d.calls, d.nth = 0, 1
	d.do = func() {
		// What a second process under the same node ID leaves behind when it
		// takes the lock over after lockFor seconds.
		if _, err := db.ExecContext(ctx, "UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ?",
			[]byte("node"), int64(1790009999), []byte(lockName)); err != nil {
			t.Error(err)
		}
	}
	if n, err := Apply(ctx, db, d, "node", 1790000000); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("the run carried on although another process of the node had taken over: %d, %v", n, err)
	}
	if level, err := Level(ctx, db); err != nil || level != 0 {
		t.Errorf("the migration was recorded as done: level %d, %v", level, err)
	}
	// The row belongs to the one who took over, and a run that lost the lock
	// gives back nothing.
	var owner []byte
	if err := db.QueryRowContext(ctx, probeLock, []byte(lockName)).Scan(&owner); err != nil {
		t.Fatalf("the run deleted the lock of the process that had taken over: %v", err)
	}
	if string(owner) != "node" {
		t.Errorf("the lock now belongs to %q", owner)
	}
}

// TestApplyNamesTheTimeLeft: whoever installs NGC is no expert. "in 340
// seconds" is something to act on, "up to 900 seconds" is a shrug.
func TestApplyNamesTheTimeLeft(t *testing.T) {
	db := openFile(t)
	if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	const now = 1790000000
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte("someone-else"), int64(now+340)); err != nil {
		t.Fatal(err)
	}
	_, err := Apply(ctx, db, sqlstore.SQLite, "test", now)
	if err == nil {
		t.Fatal("the run went on while the lock was held")
	}
	if !strings.Contains(err.Error(), "340 seconds") {
		t.Errorf("the message does not say how long is left: %v", err)
	}
	// A deadline out of the database is measured before it is used: one that
	// no run of ours could have written names the rule instead of a number
	// nobody should trust.
	for name, until := range map[string]int64{
		"a deadline one second past what a run may hold": now + lockFor + 1,
		"a deadline past every date":                     1 << 62,
	} {
		if _, err := db.ExecContext(ctx, "UPDATE ngc_lock SET until = ? WHERE name = ?", until, []byte(lockName)); err != nil {
			t.Fatal(err)
		}
		_, err := Apply(ctx, db, sqlstore.SQLite, "test", now)
		if err == nil || !strings.Contains(err.Error(), "at most 900 seconds") {
			t.Errorf("%s was passed on: %v", name, err)
		}
	}
}

// TestHoldReadsNoOwnerOutOfTheDatabase: an owner column holds whatever
// somebody wrote into it, and SQLite enforces no length. So hold asks whether
// the row is still its own and lets the database compare - then nothing of
// any length comes into the program, whatever stands there.
func TestHoldReadsNoOwnerOutOfTheDatabase(t *testing.T) {
	db := openFile(t)
	if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", 2*sqlstore.MaxText)
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte(huge), int64(1790009999)); err != nil {
		t.Fatal(err)
	}
	s := &spy{db: db}
	if err := hold(ctx, s, "test", 1790000000); !errors.Is(err, errLockLost) {
		t.Errorf("a lock that is not ours was taken as ours: %v", err)
	}
	// What is checked is the property, not how many values the query happens
	// to bind: the owner is compared in the database and never read out of it.
	// A question that grows another condition stays right; one that starts
	// reading the column does not.
	if strings.Contains(strings.ToLower(s.query), "select owner") || !strings.Contains(s.query, "owner = ?") {
		t.Errorf("hold does not let the database compare the owner: %q", s.query)
	}
}

// firstStatementHasAType guards the tests that stop the runner at the first
// Dialect.Type call. That call is in the first statement of the first
// migration only while that statement carries a {{...}}: a CREATE INDEX at
// the top of 0001 would move the spot without a word, and the tests would go
// red with a message about something else entirely.
func firstStatementHasAType(t *testing.T) {
	t.Helper()
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files[0].Statements) < 2 {
		t.Fatal("the first migration holds one statement, so there is no middle to stop in")
	}
	if !strings.Contains(files[0].Statements[0], "{{") {
		t.Fatal("the first statement of the first migration carries no {{...}}, so the first Dialect.Type call " +
			"is no longer inside it - point the tests at the statement that carries one")
	}
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

// TestRunnerTables: the two tables the runner brings along keep the rules of
// a migration, so nothing about ngc_schema and ngc_lock is less portable or
// less checked than the rest of the schema.
func TestRunnerTables(t *testing.T) {
	for _, stmt := range []string{createSchema, createLock} {
		form, err := checkStatement(stmt, fromRunner)
		if err != nil {
			t.Errorf("%.40q: %v", stmt, err)
			continue
		}
		// They stand written the way a statement runs, so bootstrap can send
		// them through the same door as a migration - and a later hand that
		// writes one of them differently finds out here and not in a database.
		if form != stmt {
			t.Errorf("the statement does not stand in the form it runs in: %q instead of %q", stmt, form)
		}
		for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
			if _, err := fill(d, stmt, fromRunner); err != nil {
				t.Errorf("%.40q: %v", stmt, err)
			}
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
	} else if string(owner) != "test" {
		t.Errorf("the lock %q now belongs to %q", dbapi.LockUpdate, owner)
	}
	// Its own lock the runner does give back, so the next server is not kept
	// waiting for the rest of the 900 seconds.
	if err := db.QueryRowContext(ctx, probeLock, []byte(lockName)).Scan(&owner); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("the lock of the migrations is still there: %q, %v", owner, err)
	}
}

// TestApplyWhileLockedBySameOwner: a second process under the same node ID is
// a second process. Whoever holds the migration lock holds it until their
// time has passed, and the runner never hands DDL to two servers at once
// because the name on the row happens to match.
func TestApplyWhileLockedBySameOwner(t *testing.T) {
	db := openFile(t)
	if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte("one"), int64(1790009999)); err != nil {
		t.Fatal(err)
	}
	if n, err := Apply(ctx, db, sqlstore.SQLite, "one", 1790000001); err == nil {
		t.Fatalf("a second run under the same name migrated: %d", n)
	} else if !strings.Contains(err.Error(), "another server") {
		t.Errorf("the message does not say why: %v", err)
	}
	if level, err := Level(ctx, db); err != nil || level != 0 {
		t.Errorf("it migrated anyway: level %d, %v", level, err)
	}
}

// TestApplyStopsWhenTheLockChangesHands: the lock has a time limit, so a run
// that takes longer than lockFor can find it in somebody else's hands in the
// middle. From there on the runner touches nothing: it stops with a message
// instead of writing DDL and a record next to the server that took over.
func TestApplyStopsWhenTheLockChangesHands(t *testing.T) {
	firstStatementHasAType(t)
	db := openFile(t)
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	d := &atType{Dialect: sqlstore.SQLite}
	// The runner's own tables first: from here on the counter is the
	// migration's own, and Apply finds them and asks for no type of its own.
	if err := bootstrap(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	// The first type of the first statement, so the takeover happens in the
	// middle of the run whatever 0001 holds. A number taken from the file
	// would quietly test another spot as soon as the file changes.
	d.calls, d.nth = 0, 1
	d.do = func() {
		// What a takeover after lockFor seconds leaves behind.
		if _, err := db.ExecContext(ctx, "UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ?",
			[]byte("somebody-else"), int64(1790009999), []byte(lockName)); err != nil {
			t.Error(err)
		}
	}
	n, err := Apply(ctx, db, d, "test", 1790000000)
	if err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("the run carried on without the lock: %d, %v", n, err)
	}
	if level, err := Level(ctx, db); err != nil || level != 0 {
		t.Errorf("the migration was recorded as done: level %d, %v", level, err)
	}
	// It stopped at the statement it was in instead of running the file to
	// the end: one of them is recorded, and the file holds more.
	var step, done int64
	if err := db.QueryRowContext(ctx, selectState, files[0].Version).Scan(&step, &done); err != nil {
		t.Fatal(err)
	}
	if step != 1 || done != 0 {
		t.Errorf("the record says %d of %d statements, done = %d", step, len(files[0].Statements), done)
	}
}

// TestApplyGivesTheLockBackAfterACancel: a run that is cut short still gives
// its lock back. Over the context of the run the DELETE would be cut short
// too, and the lock would sit there for the rest of the 900 seconds.
func TestApplyGivesTheLockBackAfterACancel(t *testing.T) {
	firstStatementHasAType(t)
	db := openFile(t)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d := &atType{Dialect: sqlstore.SQLite, do: cancel}
	if err := bootstrap(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	// Cut short in the middle of the first statement, so this too hangs on
	// the run and not on the shape of 0001.
	d.calls, d.nth = 0, 1
	if _, err := Apply(runCtx, db, d, "test", 1790000000); err == nil {
		t.Fatal("the cut short run reported success")
	}
	var owner []byte
	if err := db.QueryRowContext(ctx, probeLock, []byte(lockName)).Scan(&owner); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("the lock is still held by %q: %v", owner, err)
	}
}

// TestApplyRefusesANonsenseRecord: step comes out of the database and is used
// to cut the list of statements. A number larger than the file - a migration
// shortened afterwards, a row changed by hand - is a mistake to report, not a
// reason to take the program down.
func TestApplyRefusesANonsenseRecord(t *testing.T) {
	db := openFile(t)
	d := &stepwise{Dialect: sqlstore.SQLite}
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	f := files[0]
	if _, err := db.ExecContext(ctx, insertState, f.Version, int64(len(f.Statements)+1), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, d, "test", 1790000000); err == nil {
		t.Fatal("a record past the end of the file was taken")
	} else if !strings.Contains(err.Error(), "the migration changed") {
		t.Errorf("the message does not say what is wrong: %v", err)
	}
}

// TestTakeReportsItsOwnError: a lock the runner cannot write is not the same
// as a lock somebody else holds. The first is the runner's own problem and
// has to be readable; reading it as "busy" hides a broken database behind a
// message about another server.
func TestTakeReportsItsOwnError(t *testing.T) {
	db := openFile(t)
	d := noDuplicate{Dialect: sqlstore.SQLite}
	if err := bootstrap(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte("someone-else"), int64(1790009999)); err != nil {
		t.Fatal(err)
	}
	_, err := Apply(ctx, db, d, "test", 1790000001)
	if err == nil {
		t.Fatal("the run went on")
	}
	if strings.Contains(err.Error(), "another server") {
		t.Errorf("the error of the database was reported as a held lock: %v", err)
	}
}

// TestApplyNeedsATime: now is bounded like every other value that comes in,
// so now+lockFor can never wrap around into the past.
func TestApplyNeedsATime(t *testing.T) {
	// The bound of migrate.go, written out once more: a bound that moves has
	// to be a decision, not an accident.
	const latest = 1 << 40
	db := openFile(t)
	for name, now := range map[string]int64{
		"a time before the epoch": -1,
		"a time past every date":  latest + 1,
	} {
		if _, err := Apply(ctx, db, sqlstore.SQLite, "test", now); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := Apply(ctx, db, sqlstore.SQLite, "test", latest); err != nil {
		t.Errorf("the latest time was refused: %v", err)
	}
}

// TestApplyNeedsAnOwner: what goes into the column is the node ID and the run
// part behind it, so the bound counts both and is measured before the
// database sees anything.
func TestApplyNeedsAnOwner(t *testing.T) {
	db := openFile(t)
	for name, owner := range map[string]string{
		"no owner":          "",
		"an owner too long": strings.Repeat("n", maxOwner+1),
	} {
		if _, err := Apply(ctx, db, sqlstore.SQLite, owner, 1790000000); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// The longest node ID is still good, and what it writes into the lock
	// stays inside the column.
	if _, err := Apply(ctx, db, sqlstore.SQLite, strings.Repeat("n", maxOwner), 1790000000); err != nil {
		t.Errorf("the longest owner was refused: %v", err)
	}
	if got := ownerInTheLock(t, strings.Repeat("n", maxOwner)); len(got) > sqlstore.MaxText {
		t.Errorf("the owner of the longest node is %d bytes, the column takes %d", len(got), sqlstore.MaxText)
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

// schemaRow is one thing the database wrote down: what it is, what it is
// called and which table it belongs to.
type schemaRow struct{ kind, name, of string }

// schemaOf reads what SQLite really made of the statements it was given.
// Left out is only SQLite's own bookkeeping, and it is named piece by piece:
// sqlite_stat% comes from ANALYZE, sqlite_sequence from AUTOINCREMENT.
//
// "sqlite_%" was too wide. It also hid every sqlite_autoindex_*, the name
// SQLite gives the index it makes for a PRIMARY KEY - so the one measurement
// that trusts no rule of ours was blind to exactly the indexes no statement
// had written out by name.
func schemaOf(t *testing.T, db *sql.DB) []schemaRow {
	t.Helper()
	rows, err := db.QueryContext(ctx,
		"SELECT type, name, tbl_name FROM sqlite_master WHERE name NOT LIKE 'sqlite_stat%' AND name <> 'sqlite_sequence'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []schemaRow
	for rows.Next() {
		var r schemaRow
		if err := rows.Scan(&r.kind, &r.name, &r.of); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// unlistedInDB asks the database what came into being and gives back every
// name that no fixed list carries: a table, an index or a column. This is the
// one measurement that does not trust the rules at all - it reads back what
// SQLite really made of the statements it was given.
func unlistedInDB(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var bad []string
	var tables []string
	for _, r := range schemaOf(t, db) {
		switch r.kind {
		case "table":
			if !slices.Contains(sqlstore.Tables, r.name) {
				bad = append(bad, "table "+r.name)
				continue
			}
			tables = append(tables, r.name)
		case "index":
			// An index either carries the name of its table in front, the way
			// a migration has to write it, or it is the one SQLite made for a
			// PRIMARY KEY and named itself: sqlite_autoindex_<table>_<n>.
			// That one belongs to the table of the statement that made it, so
			// it is as listed as the table is.
			if !slices.Contains(sqlstore.Tables, r.of) ||
				!(strings.HasPrefix(r.name, r.of+"_") || strings.HasPrefix(r.name, "sqlite_autoindex_"+r.of+"_")) {
				bad = append(bad, "index "+r.name)
			}
		default:
			bad = append(bad, r.kind+" "+r.name)
		}
	}
	// The type a column was given is a name the database wrote down too, so
	// it is looked up the same way: it has to be one the dialect can make.
	types := []string{}
	for _, p := range sqlstore.Placeholders {
		if typ := strings.TrimSpace(sqlstore.SQLite.Type(p)); typ != "" {
			types = append(types, typ)
		}
	}
	for _, table := range tables {
		cols, err := db.QueryContext(ctx, "SELECT name, type FROM pragma_table_info(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var name, typ string
			if err := cols.Scan(&name, &typ); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(sqlstore.Columns[table], name) {
				bad = append(bad, "column "+table+"."+name)
			} else if !slices.Contains(types, typ) {
				bad = append(bad, "column "+table+"."+name+" of type "+typ)
			}
		}
		cols.Close()
	}
	return bad
}

// runOnSQLite runs finished SQL on a database of its own and reports what the
// database made of it. A statement the database refuses creates nothing, so
// its error is no finding here - what is looked for is what came into being.
//
// The dialect is given, because a dialect that answers something else than a
// column type is one of the things measured here: SQLite runs whatever it is
// handed, and afterwards the database is asked what came of it.
func runOnSQLite(t *testing.T, d sqlstore.Dialect, stmts []string, withTables bool) []string {
	t.Helper()
	db := openFile(t)
	if withTables {
		// The smallest form of every listed table, so that an ALTER TABLE has
		// something to add to. Only listed names are written.
		for _, table := range sqlstore.Tables {
			first := sqlstore.Columns[table][0]
			if _, err := db.ExecContext(ctx,
				"CREATE TABLE "+table+" ("+first+" BLOB NOT NULL)"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range stmts {
		sql, err := sqlstore.Fill(d, s)
		if err != nil {
			continue
		}
		_, _ = db.ExecContext(ctx, sql)
	}
	return unlistedInDB(t, db)
}

// TestNothingUnlistedIsEverCreated measures against the database and not
// against the guard. Whatever the rules let through is run on a real SQLite,
// and then the database is asked what is there: every table, every index and
// every column has to be a name one of the fixed lists carries. This is where
// a rule that reads a statement differently from the way the database reads
// it shows up, whatever the rule believes about it.
//
// Every body is run twice, each time on an empty file: once alone, so that a
// CREATE TABLE really creates, and once on a database that already carries
// the listed tables, so that an ALTER TABLE really adds.
func TestNothingUnlistedIsEverCreated(t *testing.T) {
	// the schema itself first: it has to come out clean
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	var schema []string
	for _, f := range files {
		schema = append(schema, f.Statements...)
	}
	if bad := runOnSQLite(t, sqlstore.SQLite, schema, false); len(bad) > 0 {
		t.Errorf("the schema itself creates %v", bad)
	}
	for name, body := range glued {
		stmts, err := split(body)
		if err != nil {
			continue
		}
		t.Errorf("%s was accepted: %q", name, body)
		for _, withTables := range []bool{false, true} {
			if bad := runOnSQLite(t, sqlstore.SQLite, stmts, withTables); len(bad) > 0 {
				t.Errorf("   the database now carries %v", bad)
			}
		}
	}
}

// TestAnAcceptedAlterRunsOnATableThatHoldsRows measures the rule about ALTER
// TABLE against a real database and not against the guard. A migration only
// ever meets tables that hold rows, so whatever the rules let an ALTER TABLE
// say has to run on one.
func TestAnAcceptedAlterRunsOnATableThatHoldsRows(t *testing.T) {
	for _, s := range []string{
		"ALTER TABLE ngc_blob ADD COLUMN id {{key}}",
		"ALTER TABLE ngc_blob ADD id {{key}}",
		"ALTER TABLE ngc_blob ADD COLUMN id {{key}} NOT NULL",
		"ALTER TABLE ngc_blob ADD id {{key}} NOT NULL",
	} {
		form, err := checkStatement(s, fromMigration)
		if err != nil {
			// Refused by the rules: nothing of it ever reaches a database.
			continue
		}
		sql, err := fill(sqlstore.SQLite, form, fromMigration)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		db := blobWithARow(t)
		if _, err := db.ExecContext(ctx, sql); err != nil {
			t.Errorf("the rules accept %q, and a real SQLite refuses it on a table that holds rows: %v", s, err)
		}
	}
}

// TestANotNullColumnPartsTheTwoDatabases is the measurement the rule above rests on,
// written down so that nobody has to take it on trust. On a table that holds
// rows SQLite refuses the column outright; on an empty one it takes it and
// every INSERT afterwards fails. MariaDB runs both without a word, because it
// fills in a default of its own. So a test on an empty database goes green
// and the real upgrade breaks - which is why NOT NULL is out of the ALTER
// form.
func TestANotNullColumnPartsTheTwoDatabases(t *testing.T) {
	db := blobWithARow(t)
	if _, err := db.ExecContext(ctx, "ALTER TABLE ngc_blob ADD id BLOB NOT NULL"); err == nil {
		t.Error("SQLite took a NOT NULL column on a table that holds rows")
	} else {
		t.Logf("a table that holds rows: %v", err)
	}
	// The same statement on an empty table: it runs, and the table is
	// unusable afterwards.
	empty := openFile(t)
	if _, err := empty.ExecContext(ctx, "CREATE TABLE ngc_blob (kind BLOB NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := empty.ExecContext(ctx, "ALTER TABLE ngc_blob ADD id BLOB NOT NULL"); err != nil {
		t.Errorf("an empty table: %v", err)
	}
	if _, err := empty.ExecContext(ctx, "INSERT INTO ngc_blob (kind) VALUES (?)", []byte("x")); err == nil {
		t.Error("a row went into the table, so the half-done column costs nothing after all")
	} else {
		t.Logf("an empty table, and then a row: %v", err)
	}
}

// blobWithARow gives a database with the smallest ngc_blob there is and one
// row in it, the way every table a migration meets carries rows. Only listed
// names are written.
func blobWithARow(t *testing.T) *sql.DB {
	t.Helper()
	db := openFile(t)
	if _, err := db.ExecContext(ctx, "CREATE TABLE ngc_blob (kind BLOB NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO ngc_blob (kind) VALUES (?)", []byte("x")); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestTheMessageSaysWhichStatement: the database is given the form a
// statement runs in and no longer the text of the file, so an error that
// quotes it cannot be searched for in the .sql any more. The running number
// of the statement is what points the author at the right line.
func TestTheMessageSaysWhichStatement(t *testing.T) {
	table, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL, PRIMARY KEY (kind)){{opts}}", fromMigration)
	if err != nil {
		t.Fatal(err)
	}
	index, err := checkStatement("CREATE INDEX ngc_blob_seq ON ngc_blob (kind)", fromMigration)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		stmts []string
		want  string
	}{
		// A statement that never went through the rules: fill refuses it.
		{"one the rules never wrote", []string{table, index,
			"CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}"}, "statement 3 of 3"},
		// One the database itself refuses: an index before its table.
		//
		// It used to be the same CREATE TABLE twice, and that no longer
		// refuses anything: since the migrations have to survive being played
		// again on a database in use, applyOne steps over a statement that is
		// already there in the SQLite branch as well. An index whose table
		// does not exist yet is refused for a reason that is not "already
		// there", which is what this case needs - and the lint catches almost
		// everything else before the database ever sees it.
		{"one the database refuses", []string{index, table}, "statement 1 of 2"},
	} {
		db := openFile(t)
		if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
			t.Fatal(err)
		}
		f := File{Version: 99, Name: "0099-made-up.sql", Statements: c.stmts}
		_, err := applyOne(ctx, db, sqlstore.SQLite, f, "test", 1790000000)
		if err == nil {
			t.Errorf("%s ran", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the message does not say %q: %v", c.name, c.want, err)
		}
	}
}

// TestTheOracleSeesEveryIndex: unlistedInDB is the one measurement that
// trusts no rule of ours, so it has to see everything the database wrote
// down. It began by leaving out everything called sqlite_%, and that is the
// name SQLite gives the index it makes for a PRIMARY KEY - so exactly the
// indexes nobody wrote out by hand were the ones it could not see. Now only
// SQLite's own bookkeeping is left out, named piece by piece, and the whole
// schema of NGC still comes back with nothing unlisted in it.
func TestTheOracleSeesEveryIndex(t *testing.T) {
	db := openFile(t)
	if _, err := Apply(ctx, db, sqlstore.SQLite, "test", 1790000000); err != nil {
		t.Fatal(err)
	}
	// PRIMARY KEY (kind, id) of ngc_blob: SQLite makes an index for it and
	// names it itself.
	const autoindex = "sqlite_autoindex_ngc_blob_1"
	found := false
	for _, r := range schemaOf(t, db) {
		if r.name == autoindex {
			found = r.kind == "index" && r.of == "ngc_blob"
		}
	}
	if !found {
		t.Errorf("the oracle does not see %s, so it cannot see any index a statement did not name", autoindex)
	}
	if bad := unlistedInDB(t, db); len(bad) != 0 {
		t.Errorf("the schema of NGC carries something unlisted: %v", bad)
	}
}
