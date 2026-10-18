package dbopen

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// registerBackuper puts b in place for one test and takes it out again.
func registerBackuper(t *testing.T, b Backuper) {
	t.Helper()
	RegisterBackuper(b)
	t.Cleanup(func() { RegisterBackuper(nil) })
}

// TestBackupWithoutBackuperIsItsOwnError holds the one thing that must not
// happen: a program built without the database part answering "the backup
// worked" and writing nothing. It also holds that the answer is NOT
// ErrNoOpener -- a program may open a database and still carry no way to copy
// it, and an admin who reads the wrong sentence looks in the wrong place.
func TestBackupWithoutBackuperIsItsOwnError(t *testing.T) {
	registerBackuper(t, nil)
	err := Backup(context.Background(), config.DB{}, "/tmp/backup.db")
	if !errors.Is(err, ErrNoBackuper) {
		t.Fatalf("without a backuper: %v", err)
	}
	if errors.Is(err, ErrNoOpener) {
		t.Fatalf("the missing backuper was reported as a missing opener")
	}
}

// TestBackupHandsTargetAndConfigurationOn is what the seam is for: the target
// the admin named and the configuration behind it have to arrive unchanged.
// A backup written to a path nobody asked for, or taken of a database nobody
// named, is the same accident from two sides.
func TestBackupHandsTargetAndConfigurationOn(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 64)
	want := config.DB{Driver: config.DriverSQLite, Path: "/var/lib/ngc/api/ngc.db", Key: config.NewSecret(key)}
	const target = "/mnt/stick/ngc-2026-10-17.db"
	var got config.DB
	var gotTarget string
	registerBackuper(t, func(_ context.Context, cfg config.DB, tg string) error {
		got, gotTarget = cfg, tg
		return nil
	})
	if err := Backup(context.Background(), want, target); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if gotTarget != target {
		t.Fatalf("the target became %q", gotTarget)
	}
	if got.Driver != want.Driver || got.Path != want.Path {
		t.Fatalf("the backuper saw %+v", got)
	}
	// The key is the whole of why a backup can be opened again, so it is
	// compared as bytes and not by the "[secret]" a Secret prints.
	if !bytes.Equal(got.Key.Bytes(), key) {
		t.Fatalf("the key of the database did not reach the backuper")
	}
}

// TestBackupHandsTheErrorBackBare keeps the sentence of the part that failed.
// The driver says which step gave way -- the write lock, the journal mode, the
// target -- and a wrapper of this file would put a second sentence in front of
// the one an admin has to act on.
func TestBackupHandsTheErrorBackBare(t *testing.T) {
	want := errors.New("sqlite: ein anderes Programm schreibt gerade in die Datenbank")
	registerBackuper(t, func(context.Context, config.DB, string) error { return want })
	err := Backup(context.Background(), config.DB{}, "/tmp/b.db")
	if err != want {
		t.Fatalf("the error of the part did not come back bare: %v", err)
	}
}
