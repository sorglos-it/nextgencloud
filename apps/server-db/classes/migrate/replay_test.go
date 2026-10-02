package migrate

// The promise of design 4.8: a migration runs while the API of the previous
// version is still serving. What makes it keepable is the rule of task 3 -
// a migration only ever adds - so a migration played a second time finds
// everything already there. MariaDB says so with its own error numbers;
// SQLite has no number for it and the dialect reads the message, and these
// cases are what holds that one exception.
//
// No cluster and no MariaDB exist on this machine, so everything here is
// measured on the encrypted SQLite file. What MariaDB really answers to a
// replayed CREATE is NOT MEASURABLE here; only its numbers are, in
// sqlstore's TestDeciders.

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// encryptedAt opens the real encrypted file at path, as often as a case
// wants: two pools on one file are what an update really has - the previous
// API version holds one, ngc-api migrate opens its own.
func encryptedAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	if OpenEncrypted == nil {
		t.Fatal("OpenEncrypted is not set; encrypted_test.go is what sets it")
	}
	db, err := OpenEncrypted(config.DB{
		Driver: config.DriverSQLite,
		Path:   path,
		Key:    config.NewSecret(bytes.Repeat([]byte{5}, 64)),
	})
	if err != nil {
		t.Fatalf("open %s: %v", filepath.Base(path), err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestSQLiteSaysAlreadyExistsAndTheDialectReadsIt is the measurement under
// sqliteDialect.Exists. That one reads the message of the error, which
// nothing else in NGC does, so the message has to be measured against the
// real driver instead of written down: a CREATE TABLE that runs twice on the
// file apps/server-db really opens.
//
// It also holds the narrowness. A statement that is simply wrong carries the
// same error code, SQLITE_ERROR, and must not pass as "already there" - that
// is the whole risk of reading a message, and it is checked here, not
// assumed.
func TestSQLiteSaysAlreadyExistsAndTheDialectReadsIt(t *testing.T) {
	db := encryptedAt(t, filepath.Join(t.TempDir(), "state.db"))
	const create = "CREATE TABLE ngc_probe ( id INTEGER NOT NULL , PRIMARY KEY (id) )"
	if _, err := db.ExecContext(ctx, create); err != nil {
		t.Fatalf("the first CREATE already failed: %v", err)
	}
	_, err := db.ExecContext(ctx, create)
	if err == nil {
		t.Fatal("the second CREATE went through, so there is no error to read")
	}
	if !sqlstore.SQLite.Exists(err) {
		t.Errorf("the dialect does not read %q as \"already there\"", err)
	}
	// Neither of the other two readers may claim it: Retry would run a
	// migration again for no reason, Duplicate would call it a broken key.
	if sqlstore.SQLite.Retry(err) || sqlstore.SQLite.Duplicate(err) {
		t.Errorf("another reader claimed %q as well", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE ngc_probe2 ( this is not SQL )"); err == nil {
		t.Fatal("the broken statement went through")
	} else if sqlstore.SQLite.Exists(err) {
		t.Errorf("a broken statement passes as \"already there\": %v", err)
	}
}

// TestAMigrationReplayedOnADatabaseInUseKeepsTheData is design 4.8 itself.
// The store here stands in for the API of the previous version: it was opened
// before the migration, it wrote a row before it, and it has to keep reading
// and writing across it without reconnecting.
//
// The record of the migrations is removed and every migration played again,
// which is what an update does to a database that is being used - only
// harder, because a real update plays migrations that are new, and this one
// plays the whole schema of NGC onto itself.
func TestAMigrationReplayedOnADatabaseInUseKeepsTheData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	old := encryptedAt(t, path)
	if _, err := Apply(ctx, old, sqlstore.SQLite, "api-1", 1790000000); err != nil {
		t.Fatal(err)
	}
	store := sqlstore.New(old, sqlstore.SQLite)
	put := func(id string) error {
		return store.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: id, Seq: 1, Data: []byte(id)})
	}
	if err := put("before"); err != nil {
		t.Fatal(err)
	}

	// The second pool is the one ngc-api migrate opens for itself.
	fresh := encryptedAt(t, path)
	if _, err := fresh.ExecContext(ctx, "DELETE FROM ngc_schema WHERE version > ?", int64(0)); err != nil {
		t.Fatal(err)
	}
	n, err := Apply(ctx, fresh, sqlstore.SQLite, "ctl-1", 1790000200)
	if err != nil {
		t.Fatalf("the migration failed on a database in use: %v", err)
	}
	if n == 0 {
		t.Fatal("nothing was played again, so nothing was measured")
	}

	if err := put("after"); err != nil {
		t.Errorf("the previous version could not write after the migration: %v", err)
	}
	if b, err := store.BlobGet(ctx, dbapi.BlobTrust, "before"); err != nil || string(b.Data) != "before" {
		t.Errorf("the previous version lost its row: %+v, %v", b, err)
	}
}
