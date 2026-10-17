package state

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// stateWithAnApp makes a database that holds something, so a copy of it can
// be told from a copy of an empty file. It gives the configuration back.
func stateWithAnApp(t *testing.T) config.DB {
	t.Helper()
	cfg := dbOf(filepath.Join(t.TempDir(), "state.db"), 64)
	st, err := Open(cfg, testOwner, testNow)
	if err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	app := dbapi.App{ID: "org.sorglos.notes", Version: "1.0.0", Store: "global"}
	if err := st.PutApp(context.Background(), app); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	st.Close()
	return cfg
}

// TestBackupRefusesAKeyThatIsNoKey holds the same name Open earns: a key of
// the wrong length is this server's configuration, not a broken file, and the
// answer has to say which of the two it is. Without this, a wrong key reaches
// the store and comes back as the store's own unnamed error.
func TestBackupRefusesAKeyThatIsNoKey(t *testing.T) {
	dir := t.TempDir()
	cfg := dbOf(filepath.Join(dir, "state.db"), 32)
	target := filepath.Join(dir, "sicherung.db")
	err := Backup(cfg, target)
	if !errors.Is(err, ErrKey) {
		t.Fatalf("Backup with a 32-byte key = %v, want %v", err, ErrKey)
	}
	// And nothing was written: a file under the name of a backup that is not
	// one is worse than no file, because a plan keeps it.
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("a wrong key left a backup behind: %v", err)
	}
}

// TestBackupRefusesAnotherDriver is the other half of Open's pair: a
// controller's own database is this one file format, and a configuration that
// names MariaDB must not be copied as though it were a file of this machine.
func TestBackupRefusesAnotherDriver(t *testing.T) {
	// The database has to EXIST for this case to measure the driver at all,
	// and that is the whole lesson of it: with no file there, the copier
	// refuses the call by itself ("die Datenbank ist nicht da"), every
	// assertion below passes, and a mutant that takes the driver check out
	// stays alive -- measured, m4 of this round. A case that is turned away
	// before the line it means measures the earlier wall.
	cfg := stateWithAnApp(t)
	target := filepath.Join(filepath.Dir(cfg.Path), "sicherung.db")
	// The intermediate check: with the driver this controller really has,
	// this same call goes through. So what refuses it below is the driver.
	if err := Backup(cfg, target); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	cfg.Driver = config.DriverMariaDB
	err := Backup(cfg, target)
	if err == nil {
		t.Fatalf("a MariaDB configuration was copied as a local file")
	}
	// And the answer names the program that CAN do it: an admin of a cluster
	// who reads only "no" looks for a bug instead of running ngc-db.
	if !strings.Contains(err.Error(), "ngc-db") {
		t.Fatalf("the answer does not name the program that can do it: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("a backup was left behind: %v", err)
	}
}

// TestBackupWritesAFileThatStandsOnItsOwn is the measurement that matters:
// the copy is there, it is encrypted, it carries the same bytes as the
// database, and nothing of SQLite's is left beside it.
func TestBackupWritesAFileThatStandsOnItsOwn(t *testing.T) {
	cfg := stateWithAnApp(t)
	target := filepath.Join(filepath.Dir(cfg.Path), "sicherung.db")
	if err := Backup(cfg, target); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	copied, err := os.Stat(target)
	if err != nil || copied.Size() == 0 {
		t.Fatalf("the backup %q: %v", target, err)
	}
	for _, beside := range []string{target + "-wal", target + "-shm"} {
		if _, err := os.Stat(beside); !os.IsNotExist(err) {
			t.Fatalf("%q stayed beside the backup: %v", beside, err)
		}
	}
	head, err := os.ReadFile(target)
	if err != nil || len(head) < 16 {
		t.Fatalf("the backup could not be read: %v", err)
	}
	if bytes.HasPrefix(head, []byte("SQLite format 3")) {
		t.Fatalf("the backup is a plain SQLite file, so it is not encrypted")
	}
}

// TestABackupOfThisControllerOpensAgain is the whole point of a backup and
// the one thing none of the cases above says: the copy can be OPENED with the
// key of this server, and the app that was in the database is in it.
//
// It goes through Open and not through a reader of its own, so what is
// measured is the way the controller itself takes after a restore.
func TestABackupOfThisControllerOpensAgain(t *testing.T) {
	ctx := context.Background()
	cfg := stateWithAnApp(t)
	target := filepath.Join(filepath.Dir(cfg.Path), "sicherung.db")
	if err := Backup(cfg, target); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	// The database is thrown away, exactly as a disk failure throws it away,
	// and the copy takes its place under its name.
	for _, p := range []string{cfg.Path, cfg.Path + "-wal", cfg.Path + "-shm"} {
		os.Remove(p)
	}
	if err := os.Rename(target, cfg.Path); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	back, err := Open(cfg, testOwner, testNow)
	if err != nil {
		t.Fatalf("the backup did not open again: %v", err)
	}
	defer back.Close()
	apps, err := back.Apps(ctx)
	if err != nil {
		t.Fatalf("Apps: %v", err)
	}
	var found bool
	for _, a := range apps {
		if a.ID == "org.sorglos.notes" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the app of this server is not in the restored database: %+v", apps)
	}
}
