package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
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
	if len(s.args) != 2 || strings.Contains(strings.ToLower(s.query), "select owner") {
		t.Errorf("hold carries the owner into the program instead of letting the database compare it: %q, %d values",
			s.query, len(s.args))
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
		if err := checkStatement(stmt); err != nil {
			t.Errorf("%.40q: %v", stmt, err)
		}
		for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
			if _, err := sqlstore.Fill(d, stmt); err != nil {
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
	db := openFile(t)
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files[0].Statements) < 2 {
		t.Fatal("the first migration holds one statement, so there is no middle to stop in")
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
