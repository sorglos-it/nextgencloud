package sqlite

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// fillForJournal makes the file carry a journal of its own at the moment
// freeze copies it: a table and a row, with one connection left open so that
// closing the pool does not take the -wal file away again. It answers that
// connection's pool, which the test keeps open for the whole run.
func fillForJournal(t *testing.T, cfg config.DB) *sql.DB {
	t.Helper()
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE TABLE t (a INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO t (a) VALUES (1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	st, err := os.Stat(cfg.Path + "-wal")
	if err != nil || st.Size() == 0 {
		t.Fatalf("the file carries no journal, so fold would not run at all: %v", err)
	}
	return db
}

// TestBackupStopsWhenTheJournalOfTheCopyCannotBeFolded holds the last link of
// the chain named at fold: that the replay of the journal over the copy
// really FINISHED is said by nothing but the two comparisons on the answer of
// PRAGMA wal_checkpoint. Until 12.10.2026 nothing made that answer come back
// - the copy is a file nothing else has a connection to - so the check rested
// on nobody and taking it out left every case green.
//
// The condition is HERE MADE, not hoped for: the seam betweenCopyAndFold
// opens the copy with the same key and holds a read transaction on it, which
// is exactly what a TRUNCATE checkpoint cannot get past. It then has to be
// Backup that refuses, and no backup may be left under the target's name.
func TestBackupStopsWhenTheJournalOfTheCopyCannotBeFolded(t *testing.T) {
	cfg := testConfig(t, "state.db")
	_ = fillForJournal(t, cfg)
	target := cfg.Path + ".backup"

	var opened bool
	// The busy deadline on a short clock: the TRUNCATE checkpoint below runs
	// into the read lock this seam holds, and it waits this deadline out
	// before it answers "busy". The wall is the same one, only the clock is
	// shorter - measured 18.10.2026 on D:: 10,08 s with the default of ten
	// seconds, 0,30 s here, same assertions.
	err := backup(cfg, target, Options{BusyTimeout: shortBusy}, func(tmp string) {
		copyCfg := cfg
		copyCfg.Path = tmp
		db, err := OpenDB(copyCfg)
		if err != nil {
			t.Errorf("opening the copy: %v", err)
			return
		}
		t.Cleanup(func() { _ = db.Close() })
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Errorf("read transaction on the copy: %v", err)
			return
		}
		if _, err := tx.QueryContext(ctx, "SELECT a FROM t"); err != nil {
			t.Errorf("reading the copy: %v", err)
			return
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		opened = true
	})
	if !opened {
		t.Fatal("the seam never got to hold the copy, so this case measured the wrong thing")
	}
	if err == nil {
		t.Fatal("the backup was handed out although the journal of the copy could not be folded")
	}
	// "blieben im Journal" and not just "Journal": the first version of this
	// line asked for "Journal" alone and PASSED against the mutant with the
	// two comparisons taken out, because the temporary directory of a test is
	// named after the test and this test's name carries the word. The mutant
	// fails one wall later, at os.Rename, over the copy this test is holding
	// open - a later wall, and it would have gone through as a catch.
	if !strings.Contains(err.Error(), "blieben im Journal") {
		t.Errorf("the error is not the one fold gives for a journal that stayed: %v", err)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Error("a backup is lying under the target's name although Backup refused")
	}
	// What the ".part" files do afterwards is NOT asked here, and that is a
	// limit of this case and not a finding: this test is still holding the
	// copy open, and Windows does not remove an open file - measured, all
	// three names stayed. removeCopy drops os.Remove's errors on purpose, and
	// the name that is left over carries ".part", which is never handed out
	// as a backup. The one thing that matters is the line above: nothing lies
	// under the target's own name.
}

// TestFreezeRefusesAFileThatIsNotInWalMode is Kl-1 of the review of
// 10.10.2026: freeze looks for the journal under the one name SQLite gives it
// in WAL mode and under no other, and what held that was a sentence about the
// connector two files away.
//
// The mode is switched back on the connection freeze is given, which is the
// only way to reach the case at all - every connection the connector hands
// out is put into WAL mode on the spot. So the question freeze asks is
// measured here, and the connector's PRAGMA is what the question is about.
func TestFreezeRefusesAFileThatIsNotInWalMode(t *testing.T) {
	cfg := testConfig(t, "state.db")
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA journal_mode=delete"); err != nil {
		t.Fatalf("switching the mode: %v", err)
	}
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("reading the mode back: %v", err)
	}
	if strings.EqualFold(mode, "wal") {
		t.Fatalf("the mode is still %q, so this case would measure nothing", mode)
	}

	tmp := cfg.Path + ".backup.part"
	journal, err := freeze(ctx, conn, cfg.Path, tmp)
	t.Cleanup(func() { removeCopy(tmp) })
	if err == nil {
		t.Fatalf("freeze copied a file in mode %q and reported journal=%v", mode, journal)
	}
	if !strings.Contains(err.Error(), "Journalmodus") {
		t.Errorf("the error does not name the mode: %v", err)
	}
}
