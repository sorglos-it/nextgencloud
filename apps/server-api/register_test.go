package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

// The hole these cases hold shut, measured on 12.10.2026 and the reason this
// file exists: classes/dbopen declared Register and RegisterMigrator, and no
// line of production code anywhere called either -- only tests did. So
// server.Build ran into dbopen.ErrNoOpener and ngc-api could not start at
// all, while every part of it compiled and every test was green. The seam is
// the blank import of apps/server-db/classes/register in main.go, and these
// cases fail without it.
//
// They sit in package main on purpose. The registration is a property of THIS
// PROGRAM and of nothing below it: a case inside classes/register could pass
// while the program that has to carry it leaves the import out, which is
// precisely the shape of the finding.

// dbFile is a database the cases open for real. The path is this machine's
// own absolute one, which is what sqlite.validPath asks for; config.DB's own
// Validate wants a leading "/" and is not on this way, since nothing here
// reads an api.json.
func dbFile(t *testing.T) config.DB {
	t.Helper()
	key := make([]byte, 64)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "ngc.db"),
		Key:    config.NewSecret(key),
	}
}

// TestTheDatabasePartIsRegistered proves the opener is really in place in this
// program: a store comes back and the file is on disk afterwards. Without the
// blank import this is ErrNoOpener.
func TestTheDatabasePartIsRegistered(t *testing.T) {
	cfg := dbFile(t)
	store, err := dbopen.Open(context.Background(), cfg, dbopen.OwnerOf("api-1"))
	if err != nil {
		t.Fatalf("dbopen.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if store == nil {
		t.Fatalf("an open without a store and without an error")
	}
	// Not the error and not the type: the file. sqlite.Open migrates at every
	// open, so a database that is there is the whole of what "it ran" means.
	st, err := os.Stat(cfg.Path)
	if err != nil || st.Size() == 0 {
		t.Fatalf("the database file of %q: %v", cfg.Path, err)
	}
}

// TestTheMigrationsAreRegistered is the same for ngc-api migrate: the count
// it prints is what an admin reads, so the case reads it too.
func TestTheMigrationsAreRegistered(t *testing.T) {
	var out bytes.Buffer
	if err := dbopen.Migrate(context.Background(), dbFile(t), dbopen.OwnerOf("api-1"), &out); err != nil {
		t.Fatalf("dbopen.Migrate: %v", err)
	}
	// A fresh file gets every migration there is, so the number is above
	// zero; the exact one would be a case over the count of the files in
	// apps/server-db/sql and would break with each new one.
	got := strings.TrimSpace(out.String())
	if !strings.HasSuffix(got, "Migrationen eingespielt.") && !strings.HasSuffix(got, "Migration eingespielt.") {
		t.Fatalf("migrate said %q", got)
	}
	if strings.HasPrefix(got, "0 ") {
		t.Fatalf("migrate played nothing into a fresh database: %q", got)
	}
}

// TestADriverNobodyRegisteredIsNamed is the other half of the seam, and it
// measures a shape rather than a text: a driver this program has no opener
// for comes back as an error that names the driver -- not as a nil store the
// first query dereferences, and not as a panic. This repository has no
// recover in production code (Verträge V1.2, Punkt 38), so a panic here would
// be the whole process.
func TestADriverNobodyRegisteredIsNamed(t *testing.T) {
	cfg := dbFile(t)
	cfg.Driver = "postgres"
	store, err := dbopen.Open(context.Background(), cfg, dbopen.OwnerOf("api-1"))
	if err == nil {
		t.Fatalf("an unknown driver opened a store")
	}
	if store != nil {
		t.Fatalf("an error and a store at once")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("the error does not name the driver: %v", err)
	}
	// And the file was never touched, so nothing of a wrong line in api.json
	// is left behind.
	if _, err := os.Stat(cfg.Path); !os.IsNotExist(err) {
		t.Fatalf("a file although no driver opened: %v", err)
	}
}

// TestTheOwnerNamesThisProgramAndThisNode nails the form down. It is read by
// a human out of ngc_lock, and the controller's counterpart is
// "ngc-controller/<name>[/<node>]"; the API has no name field, so the node is
// the whole of what identifies it.
func TestTheOwnerNamesThisProgramAndThisNode(t *testing.T) {
	if got := dbopen.OwnerOf("api-1"); got != "ngc-api/api-1" {
		t.Fatalf("OwnerOf: %q", got)
	}
	if dbopen.Owner != "ngc-api" {
		t.Fatalf("Owner: %q", dbopen.Owner)
	}
}

// TestTheBackupIsRegistered is the same seam again for "ngc-api backup", and
// it is the one of the three that had no caller at all until this round:
// sqlite.Backup was built, measured and tested, and no program could reach
// it. A case that only asked dbopen.Backup for a nil error would have passed
// against a registration that writes nothing, so this one asks the file
// system.
func TestTheBackupIsRegistered(t *testing.T) {
	cfg := dbFile(t)
	// Something has to be there before it can be copied: Backup refuses a
	// database that is not on disk, exactly so that a backup of an empty
	// file cannot be played back over a running installation.
	store, err := dbopen.Open(context.Background(), cfg, dbopen.OwnerOf("api-1"))
	if err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	store.Close()
	target := filepath.Join(filepath.Dir(cfg.Path), "sicherung.db")
	if err := dbopen.Backup(context.Background(), cfg, target); err != nil {
		t.Fatalf("dbopen.Backup: %v", err)
	}
	st, err := os.Stat(target)
	if err != nil || st.Size() == 0 {
		t.Fatalf("the backup file %q: %v", target, err)
	}
	// The copy stands on its own: no journal and no shared-memory file beside
	// it, or what was handed out is half a database.
	for _, beside := range []string{target + "-wal", target + "-shm"} {
		if _, err := os.Stat(beside); !os.IsNotExist(err) {
			t.Fatalf("%q stayed beside the backup: %v", beside, err)
		}
	}
	// And it is ENCRYPTED, which is the sentence backupRun prints to the
	// admin. Measured and not read: the first sixteen bytes of a plain
	// SQLite file are "SQLite format 3", and a copy that showed them would
	// mean the key never reached the copier.
	head, err := os.ReadFile(target)
	if err != nil || len(head) < 16 {
		t.Fatalf("the backup could not be read: %v", err)
	}
	if bytes.HasPrefix(head, []byte("SQLite format 3")) {
		t.Fatalf("the backup is a plain SQLite file, so it is not encrypted")
	}
	// The other way round, so this is not a case that would pass on any file
	// at all: the ORIGINAL is encrypted too, and the two have the same size.
	// A copy of a different length is not this database.
	orig, err := os.Stat(cfg.Path)
	if err != nil {
		t.Fatalf("stat of the database: %v", err)
	}
	if orig.Size() != st.Size() {
		t.Fatalf("the backup is %d bytes, the database %d", st.Size(), orig.Size())
	}
}

// TestABackupOfAClusterNamesTheProgramThatCanDoIt holds the second half of
// the registration: a MariaDB installation keeps its data on other machines,
// and the one answer that must never come back here is a nil error with no
// file written. An admin would then believe this installation has backups.
func TestABackupOfAClusterNamesTheProgramThatCanDoIt(t *testing.T) {
	cfg := dbFile(t)
	cfg.Driver = config.DriverMariaDB
	target := filepath.Join(filepath.Dir(cfg.Path), "sicherung.db")
	err := dbopen.Backup(context.Background(), cfg, target)
	if err == nil {
		t.Fatalf("a cluster reported a backup that nothing wrote")
	}
	if !strings.Contains(err.Error(), "ngc-db") {
		t.Fatalf("the error does not name the program that can do it: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("a file although nothing was copied: %v", err)
	}
}
