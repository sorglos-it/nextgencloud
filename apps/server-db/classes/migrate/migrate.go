// Package migrate applies the numbered migrations of apps/server-db/sql. They
// belong to the version of ngc-api and travel inside its release file: one
// controller with role db runs them before the new API starts, and the
// installation runs them once.
package migrate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	migrations "github.com/sorglos-it/nextgencloud/apps/server-db/sql"
)

// File is one migration: its number, its file name and its statements in
// order.
type File struct {
	Version    int64
	Name       string
	Statements []string
}

// List reads every migration and checks it against the rules. A migration
// that breaks one is an error, so a wrong file never reaches a database.
func List() ([]File, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || path.Ext(name) != ".sql" {
			continue
		}
		// The bound comes before the work on the file past it, not after.
		if len(out) == maxFiles {
			return nil, fmt.Errorf("migrate: there are more than %d migrations", maxFiles)
		}
		number, _, ok := strings.Cut(name, "-")
		version, err := strconv.ParseInt(number, 10, 64)
		if !ok || err != nil || version < 1 || len(number) != 4 {
			return nil, fmt.Errorf("migrate: %q is not named 0001-<name>.sql", name)
		}
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		stmts, err := split(string(body))
		if err != nil {
			return nil, fmt.Errorf("migrate: %s: %w", name, err)
		}
		out = append(out, File{Version: version, Name: name, Statements: stmts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, f := range out {
		if f.Version != int64(i+1) {
			return nil, fmt.Errorf("migrate: migration %d is missing", i+1)
		}
	}
	return out, nil
}

// maxFile is the most one migration may be long, maxStatements the most it
// may carry, and maxFiles the most there may be. The migrations are built
// into the release file, so none of this comes from outside; a bound belongs
// on every count all the same. 0001 is the whole schema of NGC and carries
// fourteen statements.
const (
	maxFile       = 1 << 20
	maxStatements = 256
	maxFiles      = 999
)

// split cuts a migration into statements and gives back the form each one
// runs in. A statement ends at a semicolon; the rules refuse quotes, so a
// semicolon can never sit inside a value. A line that starts with -- is a
// comment.
//
// What comes back is not the text of the file: it is what checkStatement made
// of it, written back out of the pieces it measured. That is what Apply runs,
// so that nothing the rules did not read can reach a database.
func split(body string) ([]string, error) {
	if len(body) > maxFile {
		return nil, fmt.Errorf("a migration is %d bytes long, %d are allowed", len(body), maxFile)
	}
	var keep []string
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "--") {
			keep = append(keep, line)
		}
	}
	var out []string
	for _, raw := range strings.Split(strings.Join(keep, "\n"), ";") {
		stmt := strings.Join(strings.Fields(raw), " ")
		if stmt == "" {
			continue
		}
		// The bound comes before the work on the statement past it, not
		// after.
		if len(out) == maxStatements {
			return nil, fmt.Errorf("a migration carries more than %d statements", maxStatements)
		}
		form, err := checkStatement(stmt, fromMigration)
		if err != nil {
			// The number says which statement of the file it is: what the
			// database is given is the form a statement runs in and no longer
			// the text of the file, so a message that quotes it cannot be
			// searched for in the .sql any more.
			return nil, fmt.Errorf("statement %d: %w", len(out)+1, err)
		}
		out = append(out, form)
	}
	return out, nil
}

// lockName is the row of ngc_lock that keeps two servers from migrating at
// the same time, and lockFor is how long one run may hold it. maxNow is the
// latest time a run may be given: a unix second far past any real date, and
// low enough that now+lockFor can never wrap around into the past. A bound
// belongs on every value that comes in, the time as much as the owner.
const (
	lockName = "migrate"
	lockFor  = 900     // seconds
	maxNow   = 1 << 40 // seconds since 1970, the year 36812
)

// runBytes is how much chance goes into the owner of the lock behind the node
// ID, and maxOwner is the longest node ID that leaves room for it: node ID,
// one slash and 2*runBytes hex characters together stay inside a {{text}}
// column.
const (
	runBytes = 16
	maxOwner = sqlstore.MaxText - 2*runBytes - 1
)

// The runner's own two tables. They are not in 0001, because the runner needs
// them before the first migration can run; they keep the rules of a migration
// all the same, and they go through fill like every other statement, so the
// program and not only a test holds them to the rules. That is also why they
// stand here with a space between every piece: that is the form a statement
// runs in, see write. ngc_lock is the table dbapi.LockTake uses later: same
// row, same columns, same deadline, so the two never disagree about who holds
// a lock. The runner is stricter in one point, see take.
const (
	createSchema = "CREATE TABLE ngc_schema ( version {{int}} NOT NULL , step {{int}} NOT NULL , " +
		"done {{int}} NOT NULL , applied {{int}} NOT NULL , PRIMARY KEY ( version ) ) {{opts}}"
	createLock = "CREATE TABLE ngc_lock ( name {{key}} NOT NULL , owner {{text}} NOT NULL , " +
		"until {{int}} NOT NULL , PRIMARY KEY ( name ) ) {{opts}}"
	probeSchema = "SELECT version FROM ngc_schema WHERE version = ?"
	probeLock   = "SELECT owner FROM ngc_lock WHERE name = ?"
	// The owner is compared in the database, not read out of it: a column
	// holds whatever was written into it, and nothing unbounded belongs in
	// the program.
	holdLock    = "SELECT until FROM ngc_lock WHERE name = ? AND owner = ?"
	selectUntil = "SELECT until FROM ngc_lock WHERE name = ?"
	selectState = "SELECT step, done FROM ngc_schema WHERE version = ?"
	selectLevel = "SELECT version FROM ngc_schema WHERE done = ? ORDER BY version DESC"
	insertState = "INSERT INTO ngc_schema (version, step, done, applied) VALUES (?, ?, ?, ?)"
	updateStep  = "UPDATE ngc_schema SET step = ? WHERE version = ?"
	updateDone  = "UPDATE ngc_schema SET step = ?, done = ?, applied = ? WHERE version = ?"
	takeLock    = "UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ? AND until <= ?"
	insertLock  = "INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)"
	freeLock    = "DELETE FROM ngc_lock WHERE name = ? AND owner = ?"
)

// errLockLost says that the run no longer holds the migration lock. It is the
// one answer to every way that can happen: the time ran out and another
// server took over, or somebody deleted the row.
var errLockLost = errors.New("the lock of the migrations is no longer ours: another server has taken it over")

// querier is what hold needs of a database or of a transaction. Inside a
// transaction it asks through the transaction, so the answer comes from the
// same connection that is writing.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Apply runs every migration that has not run yet and returns how many it
// ran. owner names who runs it (a node ID); what goes into the lock is that
// name and a part of its own for this one run, so an admin sees which server
// is migrating and no two runs are ever taken for one.
func Apply(ctx context.Context, db *sql.DB, d sqlstore.Dialect, owner string, now int64) (int, error) {
	// The name and the run part behind it go into a {{text}} column together,
	// so both are bounded here instead of being cut short by the database.
	if owner == "" || len(owner) > maxOwner {
		return 0, fmt.Errorf("migrate: the owner has to be 1 to %d bytes", maxOwner)
	}
	if now < 0 || now > maxNow {
		return 0, fmt.Errorf("migrate: the time has to be 0 to %d", int64(maxNow))
	}
	files, err := List()
	if err != nil {
		return 0, err
	}
	if err := bootstrap(ctx, db, d); err != nil {
		return 0, err
	}
	run, err := runOwner(owner)
	if err != nil {
		return 0, err
	}
	taken, err := take(ctx, db, d, run, now)
	if err != nil {
		return 0, err
	}
	if !taken {
		// Both reasons are named, because the runner cannot tell them apart,
		// and with them how long the wait is: whoever installs NGC is no
		// expert, and "in 340 seconds" is something to act on.
		return 0, fmt.Errorf("migrate: the lock is held: another server is running the migrations, "+
			"or a run died before it gave the lock back - %s", freeIn(ctx, db, now))
	}
	defer func() {
		// The lock goes back over a context of its own: where the run was cut
		// short, the DELETE would be cut short with it and the lock would
		// stand for the rest of lockFor, for no reason.
		if _, err := db.ExecContext(context.WithoutCancel(ctx), freeLock, []byte(lockName), []byte(run)); err != nil {
			slog.Warn("cannot give the lock of the migrations back", "error", err)
		}
	}()
	done := 0
	for _, f := range files {
		// The lock has a deadline, so before every migration the runner makes
		// sure it is still this run's.
		if err := hold(ctx, db, run, now); err != nil {
			return done, fmt.Errorf("migrate: %w", err)
		}
		ran, err := applyOne(ctx, db, d, f, run, now)
		if err != nil {
			return done, fmt.Errorf("migrate: %s: %w", f.Name, err)
		}
		if ran {
			done++
		}
	}
	return done, nil
}

// runOwner puts a part of its own behind the node ID, new for every run. The
// lock belongs to one run, not to one machine: two processes on a node carry
// one node ID, and with that alone in the row the second one would look like
// the first - it could take the lock over while the first is still writing,
// and neither of them could ever notice. runBytes bytes from crypto/rand tell
// two runs apart past any doubt, and hex keeps the row readable for whoever
// looks at it.
//
// Whoever wants the node ID back out of the owner: do not cut at the last
// slash. A node ID may carry one itself, and the runner cuts nothing back -
// the run part is always the last 2*runBytes characters, so the node ID is
// everything in front of the slash before them. Apply takes a node ID of at
// most maxOwner bytes (991) for exactly this reason: node ID, slash and run
// part together stay inside sqlstore.MaxText.
func runOwner(node string) (string, error) {
	b := make([]byte, runBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("migrate: no random number for the owner of the lock: %w", err)
	}
	return node + "/" + hex.EncodeToString(b), nil
}

// freeIn says in how long the lock is free again. The deadline comes out of
// the database, so it is measured before it is used: only one that a run of
// ours could have written - later than now, at most lockFor seconds away -
// gives a number. A row that is gone again or carries anything else names the
// rule instead, because a number nobody should trust is worse than none.
func freeIn(ctx context.Context, db *sql.DB, now int64) string {
	var until int64
	if err := db.QueryRowContext(ctx, selectUntil, []byte(lockName)).Scan(&until); err == nil &&
		until > now && until <= now+lockFor {
		return "it is free again in " + seconds(until-now)
	}
	return "it is free again in at most " + seconds(lockFor)
}

// seconds writes a number of seconds the way it is read aloud.
func seconds(n int64) string {
	if n == 1 {
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", n)
}

// hold reports an error while the migration lock is not this run's own any
// more. now stands still for the whole run - it comes in, the runner has no
// clock of its own - so the deadline cannot be moved; what can be done is to
// notice that somebody else took over, and to stop before writing DDL next to
// them.
//
// The owner is compared by the database. That way the answer is one number
// however long the row is: a column takes whatever was written into it, and
// SQLite enforces no length at all.
//
// The deadline is looked at too, although this run can never fail on it: its
// own row says now+lockFor out of the same standing-still now. What the
// second test does is refuse a row that carries this run's name and a
// deadline this run did not write - nothing writes one today - so that a
// deadline that has passed is never read as a lock in hand, whoever wrote it.
func hold(ctx context.Context, q querier, owner string, now int64) error {
	var until int64
	switch err := q.QueryRowContext(ctx, holdLock, []byte(lockName), []byte(owner)).Scan(&until); {
	case errors.Is(err, sql.ErrNoRows):
		return errLockLost
	case err != nil:
		return err
	}
	if until <= now {
		return errLockLost
	}
	return nil
}

// bootstrap makes sure the runner's own tables are there. It probes first,
// because CREATE TABLE IF NOT EXISTS is not portable: a probe that works
// means the table is there.
func bootstrap(ctx context.Context, db *sql.DB, d sqlstore.Dialect) error {
	for _, pair := range []struct {
		probe, create string
		arg           any
	}{
		{probeSchema, createSchema, int64(0)},
		{probeLock, createLock, []byte(lockName)},
	} {
		rows, err := db.QueryContext(ctx, pair.probe, pair.arg)
		if err == nil {
			rows.Close()
			continue
		}
		// The same door every statement of a migration goes through: measured
		// again here, and what runs has to be what was measured. The one
		// difference is who is knocking - these two statements are the only
		// ones that may name ngc_schema and ngc_lock.
		stmt, err := fill(d, pair.create, fromRunner)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil && !d.Exists(err) {
			return err
		}
	}
	return nil
}

// take takes the migration lock: the row of dbapi.LockTake and its deadline,
// so the runner and the store never disagree about who holds one. It reports
// false while the lock is held and the time has not passed.
//
// Held means held, whoever holds it: unlike dbapi.LockTake the runner does
// not let the same owner take a lock that is still running. Two processes
// under one node ID are two processes - and each of them carries a run of its
// own in the name anyway (runOwner), so neither can ever present the other's
// - and this is the lock that keeps two servers from writing DDL at the same
// time, the one place where taking it twice is worse than waiting. What that
// costs is a node that restarts inside the 900 seconds and has to wait them
// out, with a message that says so; the runner cannot tell that restart from
// a second process, and waiting is the safe reading of the two.
func take(ctx context.Context, db *sql.DB, d sqlstore.Dialect, owner string, now int64) (bool, error) {
	res, err := db.ExecContext(ctx, takeLock, []byte(owner), now+lockFor, []byte(lockName), now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	// Nothing changed: either the lock is held and the time has not passed,
	// or there is no row yet. The INSERT tells the two apart - a duplicate
	// means the row is there. Every other error is the runner's own and is
	// reported, because a connection that broke must not read as "somebody
	// else is migrating".
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte(owner), now+lockFor); err != nil {
		if !d.Duplicate(err) {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// applyOne runs one migration. SQLite keeps its DDL inside a transaction, so
// the whole file goes in at once. MariaDB commits every CREATE on its own,
// so each statement is recorded as it succeeds and a crash picks up there; a
// statement that ran but was not recorded fails with "already there", which
// the dialect knows and the runner steps over - but only over an object that
// is the one the statement makes, which is asked where the database can say
// so (stepOver, already.go) and assumed where it cannot.
func applyOne(ctx context.Context, db *sql.DB, d sqlstore.Dialect, f File, owner string, now int64) (bool, error) {
	step, done, err := state(ctx, db, f.Version)
	if err != nil {
		return false, err
	}
	if done {
		return false, nil
	}
	// step comes out of the database and cuts the list of statements below,
	// so it is bounded like anything else that comes in. A number past the
	// end means file and record do not belong together - a migration
	// shortened afterwards, a row changed by hand - and that is a mistake to
	// report, not a reason to take the program down.
	if step < 0 || step > int64(len(f.Statements)) {
		return false, fmt.Errorf("the record says %d of %d statements ran: the migration changed after it was applied",
			step, len(f.Statements))
	}
	if step == 0 {
		if _, err := db.ExecContext(ctx, insertState, f.Version, int64(0), int64(0), int64(0)); err != nil && !d.Duplicate(err) {
			return false, err
		}
	}
	if d.DDLInTx() {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		defer tx.Rollback()
		for i, raw := range f.Statements {
			stmt, err := fill(d, raw, fromMigration)
			if err != nil {
				return false, at(i, f, err)
			}
			// A statement that is already there is stepped over, the same
			// as in the MariaDB branch below - but only once the database has
			// said that what is already there is what this statement makes.
			// "A migration only ever adds, so whatever is there is what this
			// run would have made" was a sentence about the FILE, and W-1 of
			// the review of 10.10.2026 measured what a state of the DATABASE
			// does to it: an index of the same name over another column let
			// the run write done=1 over a schema no migration made. stepOver
			// asks instead of assuming; what it can and cannot hold stands at
			// its own line. d.Exists stays as narrow as it was - for SQLite
			// the generic error code AND the words SQLite writes for this one
			// case - so a migration that is really broken still stops here.
			//
			// This branch reads every statement and not f.Statements[step:]
			// as the one below does, and that is not an oversight: nothing in
			// this branch ever writes updateStep, and the whole file is one
			// transaction, so a run that died here rolled back and left step
			// where it was. A row that says some statements ran can therefore
			// only come from somewhere else - a hand, or the other branch -
			// and the honest answer to it is to play the file from the start,
			// because stepping into the middle of it would skip statements
			// this database may never have seen. What makes that safe is the
			// step-over above, for ADD COLUMN as much as for CREATE;
			// migrate's TestAColumnMigrationSurvivesASecondRun measures both
			// shapes of the record. A slice expression here would promise a
			// resume that one transaction does not have.
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				if !d.Exists(err) {
					return false, at(i, f, err)
				}
				// Asked through the transaction: the objects this run has
				// already made are visible on this connection alone.
				if err := stepOver(ctx, tx, d, stmt); err != nil {
					return false, at(i, f, err)
				}
			}
		}
		// Inside the transaction, so the record of "done" and the check that
		// the lock is still ours stand or fall together.
		if err := hold(ctx, tx, owner, now); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, updateDone, int64(len(f.Statements)), int64(1), now, f.Version); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	for i, raw := range f.Statements[step:] {
		// Here every statement stands on its own, so the lock is looked at
		// before each one: a run that lost it stops where it is, and what it
		// wrote so far is recorded for whoever took over.
		if err := hold(ctx, db, owner, now); err != nil {
			return false, err
		}
		stmt, err := fill(d, raw, fromMigration)
		if err != nil {
			return false, at(int(step)+i, f, err)
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if !d.Exists(err) {
				return false, at(int(step)+i, f, err)
			}
			if err := stepOver(ctx, db, d, stmt); err != nil {
				return false, at(int(step)+i, f, err)
			}
		}
		if _, err := db.ExecContext(ctx, updateStep, step+int64(i)+1, f.Version); err != nil {
			return false, err
		}
	}
	if err := hold(ctx, db, owner, now); err != nil {
		return false, err
	}
	_, err = db.ExecContext(ctx, updateDone, int64(len(f.Statements)), int64(1), now, f.Version)
	return true, err
}

// at says which statement of the migration went wrong, counted from one. The
// database is given the form a statement runs in and no longer the text of
// the file, so an error that quotes it cannot be searched for in the .sql;
// the running number is what points the author at the right line, and it is
// the same number the record in ngc_schema counts in.
func at(i int, f File, err error) error {
	return fmt.Errorf("statement %d of %d: %w", i+1, len(f.Statements), err)
}

// state returns how far a migration got: how many statements are recorded and
// whether it finished.
func state(ctx context.Context, db *sql.DB, version int64) (int64, bool, error) {
	var step, done int64
	switch err := db.QueryRowContext(ctx, selectState, version).Scan(&step, &done); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, err
	}
	return step, done == 1, nil
}

// Level returns the highest migration that finished, 0 while none did. It
// takes the first row instead of asking for one: LIMIT is not portable.
func Level(ctx context.Context, db *sql.DB) (int64, error) {
	rows, err := db.QueryContext(ctx, selectLevel, int64(1))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var version int64
	if err := rows.Scan(&version); err != nil {
		return 0, err
	}
	return version, rows.Err()
}
