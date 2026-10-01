package migrate_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

// The tests of package migrate run on the encrypted file that
// apps/server-db really opens, and this is where that opener comes from.
// Package migrate itself may not import sqlite: sqlite imports migrate, and
// an import back from the package under test is an import cycle to go test.
// This file is in the external test package, where the same import is a plain
// dependency - migrate_test imports sqlite imports migrate, and nothing
// closes the ring.
//
// Both test packages are linked into one binary and every init runs before
// the first test, so openFile has the opener by the time it is called. If
// this file goes, openFile says so and stops instead of opening a file of its
// own.
func init() { migrate.OpenEncrypted = sqlite.OpenDB }

// TestTheHookOpensAnEncryptedFile measures what the comment at OpenEncrypted
// used to only assert.
//
// The import proved it by itself: a test that called sqlite.OpenDB could not
// get its file from anywhere else. The hook cannot, and that is the notch this
// detour costs - openFile's nil check says only that somebody set an opener,
// not which one, and a second test file whose name sorts after this one would
// overwrite it in its own init. migrate's tests would then measure an
// unencrypted file in silence, which is exactly the hole the comment warns
// about and did not close.
//
// So the property is asked instead of the identity: this goes through the hook
// and not through sqlite.OpenDB, writes a marker, closes, and reads the bytes
// off the disk. A plain opener fails it. Comparing function pointers would
// catch the same swap, but it would say nothing about whether the opener it
// names still encrypts.
func TestTheHookOpensAnEncryptedFile(t *testing.T) {
	if migrate.OpenEncrypted == nil {
		t.Fatal("OpenEncrypted is not set, so there is nothing to measure here")
	}
	path := filepath.Join(t.TempDir(), "state.db")
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   path,
		Key:    config.NewSecret(bytes.Repeat([]byte{7}, 64)), // 64 bytes: AES-256-XTS
	}
	db, err := migrate.OpenEncrypted(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "hook-marker-in-the-clear"
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO probe (a) VALUES (?)", []byte(marker)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 16 {
		t.Fatalf("read back: %d bytes, %v", len(raw), err)
	}
	// The header of an unencrypted file, without its closing NUL byte: enough
	// to tell the two apart, and no escape in the source.
	if bytes.HasPrefix(raw, []byte("SQLite format 3")) {
		t.Error("the hook opened a plain SQLite file, so migrate's tests would not measure the encrypting opener")
	}
	if bytes.Contains(raw, []byte(marker)) {
		t.Error("the file holds the value in the clear")
	}
}
