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
//
// The same holds for the replayed ADD COLUMN below: MariaDB is documented to
// answer 1060, which mariaDialect.Exists already reads, but NOTHING HERE HAS
// MEASURED IT. Whoever has a server runs this and reports the number:
//
//	mysql -e 'CREATE DATABASE ngc_probe;
//	  CREATE TABLE ngc_probe.ngc_blob (kind VARBINARY(255) NOT NULL);
//	  ALTER TABLE ngc_probe.ngc_blob ADD COLUMN seq BIGINT;
//	  ALTER TABLE ngc_probe.ngc_blob ADD COLUMN seq BIGINT;'
//
// The last statement has to report ERROR 1060, not 1050 or 1061 - any other
// number is a finding, because the runner would then stop on the second run
// of the first migration that adds a column.

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

// columnMigration is the first migration that adds a column: the form every
// migration after 0001 will have, and the one 0001 itself does not carry
// (grep -c ALTER on it is 0). The statements go through the rules first, so
// what runs here is what a real migration would run and not a string this
// test made up.
func columnMigration(t *testing.T) File {
	t.Helper()
	var stmts []string
	for _, s := range []string{
		"CREATE TABLE ngc_blob ( kind {{key}} NOT NULL , PRIMARY KEY ( kind ) ) {{opts}}",
		"ALTER TABLE ngc_blob ADD COLUMN seq {{int}}",
	} {
		form, err := checkStatement(s, fromMigration)
		if err != nil {
			t.Fatalf("the rules refuse %q: %v", s, err)
		}
		stmts = append(stmts, form)
	}
	return File{Version: 99, Name: "0099-a-column.sql", Statements: stmts}
}

// TestAColumnMigrationSurvivesASecondRun is design 4.8 for the form the
// replay above does not reach. The case next to it plays CREATE statements
// again; this one plays an ADD COLUMN again, which is what every migration
// after 0001 is going to be, and SQLite answers it with words of its own.
//
// The record is removed between the two runs, the way an update meets a
// database the previous API version is still working on: whatever is already
// there is what this run would have made, so the second run has to step over
// it and finish.
func TestAColumnMigrationSurvivesASecondRun(t *testing.T) {
	db := encryptedAt(t, filepath.Join(t.TempDir(), "state.db"))
	if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	// applyOne looks at the lock before it records anything, so the run has
	// to hold one like any other.
	const owner = "test/0"
	if taken, err := take(ctx, db, sqlstore.SQLite, owner, 1790000000); err != nil || !taken {
		t.Fatalf("take: %v, %v", taken, err)
	}
	f := columnMigration(t)
	if _, err := applyOne(ctx, db, sqlstore.SQLite, f, owner, 1790000000); err != nil {
		t.Fatalf("the first run already failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM ngc_schema WHERE version = ?", f.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := applyOne(ctx, db, sqlstore.SQLite, f, owner, 1790000000); err != nil {
		t.Errorf("the migration cannot be played a second time: %v", err)
	}
	// The other shape of the same record, and the reason the SQLite branch of
	// applyOne may go on ignoring step: a row that says some statements ran.
	// Nothing in that branch ever writes step - the whole file is one
	// transaction, and a run that died rolled it back - so a row like this
	// only ever comes from somewhere else, and the branch replays from the
	// first statement. That has to end well, and with the ADD COLUMN covered
	// it does.
	if _, err := db.ExecContext(ctx, "UPDATE ngc_schema SET step = ?, done = ? WHERE version = ?",
		int64(1), int64(0), f.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := applyOne(ctx, db, sqlstore.SQLite, f, owner, 1790000000); err != nil {
		t.Errorf("a record that says one statement ran stops the migration: %v", err)
	}

	// And the same record on a database where that statement really did not
	// run. This is the case that decides the question instead of arguing it:
	// cutting the list at step would skip the CREATE TABLE here and the ADD
	// COLUMN would fail on a table that is not there. Playing from the start
	// is the only reading that works for a record the SQLite branch did not
	// write itself.
	fresh := encryptedAt(t, filepath.Join(t.TempDir(), "fresh.db"))
	if err := bootstrap(ctx, fresh, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	if taken, err := take(ctx, fresh, sqlstore.SQLite, owner, 1790000000); err != nil || !taken {
		t.Fatalf("take: %v, %v", taken, err)
	}
	if _, err := fresh.ExecContext(ctx, insertState, f.Version, int64(1), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := applyOne(ctx, fresh, sqlstore.SQLite, f, owner, 1790000000); err != nil {
		t.Errorf("a record that counts a statement the database never saw stops the migration: %v", err)
	}
}

// TestSQLiteSaysDuplicateColumnNameAndTheDialectReadsIt is the measurement
// under the other half of sqliteDialect.Exists. The case at the top of this
// file measures what SQLite writes for a CREATE that ran twice; a replayed
// ALTER TABLE ... ADD COLUMN writes something else, and the words of a CREATE
// do not cover it.
//
// It holds the narrowness in the same breath: an ALTER TABLE that is simply
// wrong carries SQLITE_ERROR as well and must not pass as "already there".
func TestSQLiteSaysDuplicateColumnNameAndTheDialectReadsIt(t *testing.T) {
	db := encryptedAt(t, filepath.Join(t.TempDir(), "state.db"))
	if _, err := db.ExecContext(ctx, "CREATE TABLE ngc_blob ( kind BLOB NOT NULL )"); err != nil {
		t.Fatal(err)
	}
	const add = "ALTER TABLE ngc_blob ADD COLUMN seq INTEGER"
	if _, err := db.ExecContext(ctx, add); err != nil {
		t.Fatalf("the first ALTER already failed: %v", err)
	}
	same, err := db.ExecContext(ctx, add)
	if err == nil {
		t.Fatalf("the second ALTER went through (%v), so there is no error to read", same)
	}
	if !sqlstore.SQLite.Exists(err) {
		t.Errorf("the dialect does not read %q as \"already there\"", err)
	}
	if sqlstore.SQLite.Retry(err) || sqlstore.SQLite.Duplicate(err) {
		t.Errorf("another reader claimed %q as well", err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE ngc_blob ADD COLUMN this is not SQL"); err == nil {
		t.Fatal("the broken ALTER went through")
	} else if sqlstore.SQLite.Exists(err) {
		t.Errorf("a broken ALTER passes as \"already there\": %v", err)
	}

	// The weakness of reading a message, measured instead of feared: a column
	// of the same name and ANOTHER TYPE answers word for word the same, so no
	// reading of the message could tell the two apart. What keeps that out is
	// the rule that a migration only ever adds, not this function - the same
	// standing as the name collision of CREATE INDEX. The comment at
	// sqliteDialect.Exists says so; this is where it is measured.
	other := encryptedAt(t, filepath.Join(t.TempDir(), "other.db"))
	if _, err := other.ExecContext(ctx, "CREATE TABLE ngc_blob ( kind BLOB NOT NULL , seq BLOB )"); err != nil {
		t.Fatal(err)
	}
	_, typeErr := other.ExecContext(ctx, add)
	if typeErr == nil {
		t.Fatal("a column of the same name and another type went through")
	}
	if typeErr.Error() != err.Error() {
		t.Errorf("SQLite tells the two apart after all: %q against %q", typeErr, err)
	}
}
