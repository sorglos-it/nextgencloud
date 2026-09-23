// Package migrate applies the numbered migrations of apps/server-db/sql. They
// belong to the version of ngc-api and travel inside its release file: one
// controller with role db runs them before the new API starts, and the
// installation runs them once.
package migrate

import (
	"context"
	"database/sql"
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

// split cuts a migration into statements. A statement ends at a semicolon;
// checkStatement refuses quotes, so a semicolon can never sit inside a value.
// A line that starts with -- is a comment.
func split(body string) ([]string, error) {
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
		if err := checkStatement(stmt); err != nil {
			return nil, err
		}
		out = append(out, stmt)
	}
	return out, nil
}

// lockName is the row of ngc_lock that keeps two servers from migrating at
// the same time, and lockFor is how long one run may hold it.
const (
	lockName = "migrate"
	lockFor  = 900 // seconds
)

// The runner's own two tables. They are not in 0001, because the runner needs
// them before the first migration can run; they keep the rules of a migration
// all the same. ngc_lock is the table dbapi.LockTake uses later, so the two
// never disagree about who holds a lock.
const (
	createSchema = "CREATE TABLE ngc_schema (version {{int}} NOT NULL, step {{int}} NOT NULL, " +
		"done {{int}} NOT NULL, applied {{int}} NOT NULL, PRIMARY KEY (version)){{opts}}"
	createLock = "CREATE TABLE ngc_lock (name {{key}} NOT NULL, owner {{text}} NOT NULL, " +
		"until {{int}} NOT NULL, PRIMARY KEY (name)){{opts}}"
	probeSchema = "SELECT version FROM ngc_schema WHERE version = ?"
	probeLock   = "SELECT owner FROM ngc_lock WHERE name = ?"
	selectState = "SELECT step, done FROM ngc_schema WHERE version = ?"
	selectLevel = "SELECT version FROM ngc_schema WHERE done = ? ORDER BY version DESC"
	insertState = "INSERT INTO ngc_schema (version, step, done, applied) VALUES (?, ?, ?, ?)"
	updateStep  = "UPDATE ngc_schema SET step = ? WHERE version = ?"
	updateDone  = "UPDATE ngc_schema SET step = ?, done = ?, applied = ? WHERE version = ?"
	takeLock    = "UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ? AND (owner = ? OR until <= ?)"
	insertLock  = "INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)"
	freeLock    = "DELETE FROM ngc_lock WHERE name = ? AND owner = ?"
)

// Apply runs every migration that has not run yet and returns how many it
// ran. owner names who runs it (a node ID) and shows up in the lock, so an
// admin can see which server is migrating.
func Apply(ctx context.Context, db *sql.DB, d sqlstore.Dialect, owner string, now int64) (int, error) {
	// The owner goes into a {{text}} column, so it is bounded here instead of
	// being cut short by the database.
	if owner == "" || len(owner) > sqlstore.MaxText {
		return 0, fmt.Errorf("migrate: the owner has to be 1 to %d bytes", sqlstore.MaxText)
	}
	files, err := List()
	if err != nil {
		return 0, err
	}
	if err := bootstrap(ctx, db, d); err != nil {
		return 0, err
	}
	taken, err := take(ctx, db, owner, now)
	if err != nil {
		return 0, err
	}
	if !taken {
		return 0, errors.New("migrate: another server is running the migrations")
	}
	defer func() {
		if _, err := db.ExecContext(ctx, freeLock, []byte(lockName), []byte(owner)); err != nil {
			slog.Warn("cannot give the lock of the migrations back", "error", err)
		}
	}()
	done := 0
	for _, f := range files {
		ran, err := applyOne(ctx, db, d, f, now)
		if err != nil {
			return done, fmt.Errorf("migrate: %s: %w", f.Name, err)
		}
		if ran {
			done++
		}
	}
	return done, nil
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
		stmt, err := sqlstore.Fill(d, pair.create)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil && !d.Exists(err) {
			return err
		}
	}
	return nil
}

// take takes the migration lock: the row and the rule of dbapi.LockTake, so
// the runner and the store never disagree about who holds one. It reports
// false while somebody else holds it and their time has not passed.
func take(ctx context.Context, db *sql.DB, owner string, now int64) (bool, error) {
	res, err := db.ExecContext(ctx, takeLock, []byte(owner), now+lockFor, []byte(lockName), []byte(owner), now)
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
	// Nothing changed: either the lock is held by somebody else, or there is
	// no row yet. The INSERT tells the two apart. MariaDB counts changed rows
	// and not matched ones, so an owner who takes their own lock again
	// without moving the time lands here too; Apply always moves it.
	if _, err := db.ExecContext(ctx, insertLock, []byte(lockName), []byte(owner), now+lockFor); err != nil {
		return false, nil
	}
	return true, nil
}

// applyOne runs one migration. SQLite keeps its DDL inside a transaction, so
// the whole file goes in at once. MariaDB commits every CREATE on its own,
// so each statement is recorded as it succeeds and a crash picks up there; a
// statement that ran but was not recorded fails with "already there", which
// the dialect knows and the runner steps over.
func applyOne(ctx context.Context, db *sql.DB, d sqlstore.Dialect, f File, now int64) (bool, error) {
	step, done, err := state(ctx, db, f.Version)
	if err != nil {
		return false, err
	}
	if done {
		return false, nil
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
		for _, raw := range f.Statements {
			stmt, err := sqlstore.Fill(d, raw)
			if err != nil {
				return false, err
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return false, err
			}
		}
		if _, err := tx.ExecContext(ctx, updateDone, int64(len(f.Statements)), int64(1), now, f.Version); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	for i, raw := range f.Statements[step:] {
		stmt, err := sqlstore.Fill(d, raw)
		if err != nil {
			return false, err
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil && !d.Exists(err) {
			return false, err
		}
		if _, err := db.ExecContext(ctx, updateStep, step+int64(i)+1, f.Version); err != nil {
			return false, err
		}
	}
	_, err = db.ExecContext(ctx, updateDone, int64(len(f.Statements)), int64(1), now, f.Version)
	return true, err
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
