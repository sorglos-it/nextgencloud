package migrate

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

// openTwice opens the SAME encrypted file twice, the way two programs on one
// Pi do it: the API and the controller each have a pool of their own on one
// file. openFile cannot be used for this - it makes a TempDir of its own on
// every call, so two calls are two files.
func openTwice(t *testing.T) (*sql.DB, *sql.DB) {
	t.Helper()
	if OpenEncrypted == nil {
		t.Fatal("OpenEncrypted is not set; encrypted_test.go is what sets it")
	}
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "state.db"),
		Key:    config.NewSecret(bytes.Repeat([]byte{7}, 64)),
	}
	open := func() *sql.DB {
		db, err := OpenEncrypted(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	return open(), open()
}

// TestApplySaysWhoHoldsTheFile measures the sentence an admin reads when the
// migrations cannot start because ANOTHER PROGRAM is writing to the file -
// not because another run holds the migration lock (TestApplyWhileLocked
// measures that one, and it has a sentence of its own).
//
// Measured at the running pair on 13.10.2026: a second process whose write
// transaction stands for longer than busy_timeout makes Apply give up after
// 10.014 s, and what came out was the driver's bare "sqlite3: database is
// locked" - no "migrate:", no step, nothing that says it waited ten seconds
// or that a neighbour is writing. On one Pi, where the API and the controller
// both open one file and each migrates at its own start, that is the sentence
// the admin gets at a failed start.
//
// The test takes about ten seconds on purpose: busy_timeout is what is being
// waited out, and OpenDB sets it to 10 000 ms with no way in from outside.
// Shortening it here would measure a different file than the one NGC opens.
func TestApplySaysWhoHoldsTheFile(t *testing.T) {
	writer, runner := openTwice(t)
	if err := bootstrap(ctx, writer, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	// The neighbour takes the write lock of the file and keeps it. _txlock=
	// immediate means BeginTx takes it right here, not at the first write.
	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, insertLock, []byte("other"), []byte("neighbour"), int64(1790009999)); err != nil {
		t.Fatal(err)
	}

	n, err := Apply(ctx, runner, sqlstore.SQLite, "starting-up", 1790000001)
	if err == nil {
		t.Fatalf("the migrations ran while another program held the file: %d", n)
	}
	if n != 0 {
		t.Errorf("%d migrations ran", n)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "migrate: ") {
		t.Errorf("the error does not say which part failed: %q", msg)
	}
	if !strings.Contains(msg, "another program is writing") {
		t.Errorf("the error does not say that a neighbour holds the file: %q", msg)
	}
	// And it must not read like the other refusal: that one means a run of
	// ours holds the migration lock, and the cure is a different one.
	if strings.Contains(msg, "another server is running the migrations") {
		t.Errorf("a busy file reads like a held migration lock: %q", msg)
	}
}

// TestApplyNamesTheStepOfAnOrdinaryFailure is the other arm of busyOr: an
// error that is NOT "busy" still has to say which step gave up, and it must
// not carry the sentence about a neighbour - whoever reads "try again" for a
// closed pool looks in the wrong place. A closed database is the cheapest
// non-retryable failure there is, and it fails in the same two steps.
func TestApplyNamesTheStepOfAnOrdinaryFailure(t *testing.T) {
	db := openFile(t)
	if err := bootstrap(ctx, db, sqlstore.SQLite); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	n, err := Apply(ctx, db, sqlstore.SQLite, "starting-up", 1790000001)
	if err == nil {
		t.Fatalf("the migrations ran on a closed database: %d", n)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "migrate: ") {
		t.Errorf("the error does not say which part failed: %q", msg)
	}
	if !strings.Contains(msg, "the tables of the runner") && !strings.Contains(msg, "the lock of the migrations") {
		t.Errorf("the error names no step: %q", msg)
	}
	if strings.Contains(msg, "another program is writing") {
		t.Errorf("an ordinary failure reads like a busy file: %q", msg)
	}
}
