package migraterun_test

// The runner, measured through its own two calls and not through
// migrate.Apply below it. Everything here runs on the encrypted SQLite file
// that apps/server-db really opens.
//
// NOT MEASURABLE here: that MariaDB answers a replayed migration the way the
// runner expects. This machine has no MariaDB and no Galera. What is measured
// of that side is our numbers only - 1050, 1060 and 1061 in sqlstore's
// TestDeciders - and a test over a number is not a test over a behaviour.

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migraterun"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

var ctx = context.Background()

func fileConfig(t *testing.T) config.DB {
	t.Helper()
	return config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "state.db"),
		Key:    config.NewSecret(bytes.Repeat([]byte{5}, 64)),
	}
}

// TestRunTwice: the first run applies everything, the second finds nothing to
// do. That is what a controller sees when an update is installed again.
func TestRunTwice(t *testing.T) {
	cfg := fileConfig(t)
	need, err := migraterun.Needed(ctx, cfg)
	if err != nil || !need {
		t.Fatalf("an empty database needs migrating: %v, %v", need, err)
	}
	n, err := migraterun.Run(ctx, cfg, "ctl-1", 1790000000)
	if err != nil || n == 0 {
		t.Fatalf("first run: %d, %v", n, err)
	}
	if n, err := migraterun.Run(ctx, cfg, "ctl-1", 1790000100); err != nil || n != 0 {
		t.Errorf("second run: %d, %v", n, err)
	}
	if need, err := migraterun.Needed(ctx, cfg); err != nil || need {
		t.Errorf("after the run nothing is missing: %v, %v", need, err)
	}
}

// TestPreviousVersionKeepsWorking is the promise of 4.8 through the runner:
// a migration runs while the API of the previous version is still serving.
// The store here stands in for that API - it was opened before the migration
// and has to keep reading and writing across it, without a reconnect.
func TestPreviousVersionKeepsWorking(t *testing.T) {
	cfg := fileConfig(t)
	if _, err := migraterun.Run(ctx, cfg, "ctl-1", 1790000000); err != nil {
		t.Fatal(err)
	}
	old, err := sqlite.Open(cfg, "api-1", 1790000000)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	put := func(id string) error {
		return old.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: id, Seq: 1, Data: []byte(id)})
	}
	if err := put("before"); err != nil {
		t.Fatal(err)
	}
	// Every migration again, with the record of them removed: that replays
	// exactly what an update does to a database that is being used.
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM ngc_schema WHERE version > ?", int64(0)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := migraterun.Run(ctx, cfg, "ctl-1", 1790000200); err != nil {
		t.Fatalf("the migration failed on a database in use: %v", err)
	}
	if err := put("after"); err != nil {
		t.Errorf("the previous version could not write after the migration: %v", err)
	}
	if b, err := old.BlobGet(ctx, dbapi.BlobTrust, "before"); err != nil || string(b.Data) != "before" {
		t.Errorf("the previous version lost its row: %+v, %v", b, err)
	}
}

// TestNeededDoesNotCallABrokenDatabaseBehind is the one branch of Needed that
// is worth a case of its own. migrate.Level fails on a database that never
// saw a migration AND on one that cannot be read at all, and "yes, migrate"
// to the second would hand a broken database to the migrations and lose the
// real reason on the way.
//
// Measured with the wrong key on a file that is a database: the connection
// does not answer, so the error comes back instead of "true".
func TestNeededDoesNotCallABrokenDatabaseBehind(t *testing.T) {
	cfg := fileConfig(t)
	if _, err := migraterun.Run(ctx, cfg, "ctl-1", 1790000000); err != nil {
		t.Fatal(err)
	}
	wrong := cfg
	wrong.Key = config.NewSecret(bytes.Repeat([]byte{6}, 64))
	need, err := migraterun.Needed(ctx, wrong)
	if err == nil {
		t.Fatalf("a database that cannot be read came back as need = %v", need)
	}
	if need {
		t.Error("a database that cannot be read must not be reported as behind")
	}
	if !strings.Contains(err.Error(), "does not answer") {
		t.Errorf("the message does not say that the database is silent: %v", err)
	}
}

// TestAnUnknownDriverIsNamed: config.DB.Validate refuses such a
// configuration already, so this is the second bolt. Both calls have to hold
// it - Needed opens the database as Run does.
func TestAnUnknownDriverIsNamed(t *testing.T) {
	cfg := config.DB{Driver: "postgres", Path: "/tmp/x.db"}
	if _, err := migraterun.Run(ctx, cfg, "ctl-1", 1790000000); err == nil ||
		!strings.Contains(err.Error(), `"postgres"`) {
		t.Errorf("Run: %v", err)
	}
	if _, err := migraterun.Needed(ctx, cfg); err == nil ||
		!strings.Contains(err.Error(), `"postgres"`) {
		t.Errorf("Needed: %v", err)
	}
}
