package sqlite_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// backupCtx is the context of the tests in this file. The package's other
// external test file carries none, and a package-level name of its own keeps
// the two from ever colliding.
var backupCtx = context.Background()

// backupKey is the key of the files below. 0xab repeated gives a hex key of
// "abab…", which TestBackupKeepsTheKeyOutOfTheError looks for.
func backupConfig(dir, name string) config.DB {
	return config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(dir, name),
		Key:    config.NewSecret(bytes.Repeat([]byte{0xab}, 64)),
	}
}

// writers and blobSize make the write load of TestBackupHoldsUnderWriteLoad.
//
// The size is what makes the load bite. SQLite in WAL mode writes into the
// -wal file and touches the database file only at a checkpoint, so a plain
// copy of a file that is merely being written to would look fine. What tears
// a copy is a checkpoint folding the WAL back into the file while the bytes
// are read, and a checkpoint starts by itself after 1000 pages (4 MiB) of
// WAL. Eight writers of 1 MiB each fill that in four rounds, so checkpoints
// run all the way through the copying.
const (
	writers  = 8
	blobSize = 1 << 20
)

// payload is the content of one blob: a stream that depends on the writer and
// on the sequence number, over the whole blob and not only in a header. A
// copy that caught half of one version of a row and half of another therefore
// shows it wherever the two halves meet, not just at the front.
func payload(w int, seq uint64) []byte {
	b := make([]byte, blobSize)
	x := uint64(w+1)<<32 | seq | 1
	for i := 0; i < len(b); i += 8 {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		binary.LittleEndian.PutUint64(b[i:], x)
	}
	return b
}

// TestBackupHoldsUnderWriteLoad is the promise "a copy that is consistent in
// itself". Eight writers keep writing 1 MiB rows for the whole test, and
// every copy taken while they run has to be a database again: it opens, it
// passes integrity_check, it holds a row of every writer, and every row in it
// is exactly one version of that row and not a mixture of two.
//
// Without the write lock the test is not vacuous: a copy taken while a
// checkpoint moves pages is torn, which was measured by taking the lock out
// (see the report).
func TestBackupHoldsUnderWriteLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := backupConfig(dir, "state.db")
	s, err := sqlite.Open(cfg, "test", 1790000000)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A value in the clear, so the copy can be checked for holding it.
	if err := s.BlobPut(backupCtx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "marker", Data: []byte("secret-marker")}); err != nil {
		t.Fatal(err)
	}

	var (
		stop    atomic.Bool
		commits atomic.Int64
		first   sync.WaitGroup // every writer has written once
		wg      sync.WaitGroup
	)
	first.Add(writers)
	errs := make([]error, writers)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			once := false
			for seq := uint64(1); !stop.Load(); seq++ {
				b := dbapi.Blob{Kind: dbapi.BlobTrust, ID: fmt.Sprint(w), Seq: seq, Data: payload(w, seq)}
				if err := s.BlobPut(backupCtx, b); err != nil {
					errs[w] = err
					break
				}
				commits.Add(1)
				if !once {
					once = true
					first.Done()
				}
			}
			if !once {
				first.Done() // a writer that failed must not hang the test
			}
		}()
	}
	first.Wait()

	const rounds = 8
	for round := range rounds {
		target := filepath.Join(dir, fmt.Sprintf("backup-%d.db", round))
		if err := sqlite.Backup(cfg, target); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		checkCopy(t, cfg, target, round)
	}
	stop.Store(true)
	wg.Wait()
	for w, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", w, err)
		}
	}
	n := commits.Load()
	t.Logf("%d copies while %d rows of %d bytes were written", rounds, n, blobSize)
	// A load that wrote almost nothing would make every copy above trivially
	// fine. Four full WAL rounds (16 MiB) are what brings the checkpoints the
	// copies have to survive.
	if n < 4*writers*2 {
		t.Errorf("only %d rows were written; the load is too small to prove anything", n)
	}
}

// checkCopy opens the copy with the same key and reads it as it is: no
// migration, no repair.
func checkCopy(t *testing.T, cfg config.DB, target string, round int) {
	t.Helper()
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("round %d: %v", round, err)
	}
	if bytes.HasPrefix(raw, []byte("SQLite format 3\x00")) || bytes.Contains(raw, []byte("secret-marker")) {
		t.Errorf("round %d: the backup is not encrypted", round)
	}
	copyCfg := cfg
	copyCfg.Path = target
	db, err := sqlite.OpenDB(copyCfg)
	if err != nil {
		t.Fatalf("round %d: the copy does not open: %v", round, err)
	}
	defer db.Close()
	var state string
	if err := db.QueryRowContext(backupCtx, "PRAGMA integrity_check").Scan(&state); err != nil {
		t.Fatalf("round %d: integrity_check: %v", round, err)
	}
	if state != "ok" {
		t.Errorf("round %d: integrity_check says %q", round, state)
	}
	store := sqlite.New(db)
	blobs, err := store.BlobFrom(backupCtx, dbapi.BlobTrust, 0)
	if err != nil {
		t.Fatalf("round %d: reading the rows back: %v", round, err)
	}
	if len(blobs) != writers {
		t.Errorf("round %d: %d rows, want %d", round, len(blobs), writers)
	}
	for _, b := range blobs {
		var w int
		if _, err := fmt.Sscan(b.ID, &w); err != nil {
			t.Errorf("round %d: row %q: %v", round, b.ID, err)
			continue
		}
		want := payload(w, b.Seq)
		if len(b.Data) != len(want) {
			t.Errorf("round %d: row %q at %d is %d bytes, want %d", round, b.ID, b.Seq, len(b.Data), len(want))
			continue
		}
		if !bytes.Equal(b.Data, want) {
			t.Errorf("round %d: row %q at %d is a mixture of two versions", round, b.ID, b.Seq)
		}
	}
	if marker, err := store.BlobGet(backupCtx, dbapi.BlobSetting, "marker"); err != nil || string(marker.Data) != "secret-marker" {
		t.Errorf("round %d: the copy lost the row: %+v, %v", round, marker, err)
	}
}

// TestBackupDoesNotCreateTheDatabase holds the one thing a copy must never
// do: invent its source. OpenDB creates the file when it is missing, so a
// mistyped path would otherwise produce a fresh, empty database and a backup
// of nothing - and that backup, played back, is an erased installation.
func TestBackupDoesNotCreateTheDatabase(t *testing.T) {
	dir := t.TempDir()
	cfg := backupConfig(dir, "state.db")
	target := filepath.Join(dir, "backup.db")
	if err := sqlite.Backup(cfg, target); err == nil {
		t.Error("a missing database was backed up")
	}
	if _, err := os.Stat(cfg.Path); !os.IsNotExist(err) {
		t.Errorf("Backup made the database it was asked to copy: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("Backup left a target behind: %v", err)
	}

	// What the refusal above protects against, measured instead of claimed:
	// opening the file is what creates it.
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(backupCtx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(cfg.Path)
	if err != nil {
		t.Fatalf("opening the file did not create it: %v", err)
	}
	t.Logf("opening the missing file created %d bytes", st.Size())

	// And the way to that refusal is open: the same configuration and the
	// same target go through once the file is there, so the error above was
	// about the missing file and not about something earlier.
	if err := sqlite.Backup(cfg, target); err != nil {
		t.Fatalf("the way to the check is blocked: %v", err)
	}
}

// TestBackupRefusesTheDatabaseItself: a target that names the database would
// write the file while it is read.
func TestBackupRefusesTheDatabaseItself(t *testing.T) {
	dir := t.TempDir()
	cfg := backupConfig(dir, "state.db")
	s, err := sqlite.Open(cfg, "test", 1790000000)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BlobPut(backupCtx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "marker", Data: []byte("secret-marker")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{cfg.Path, ""} {
		if err := sqlite.Backup(cfg, target); err == nil {
			t.Errorf("target %q was accepted", target)
		}
	}
	after, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the refused call changed the database")
	}
	// The way to the check is open: a target beside it goes through.
	if err := sqlite.Backup(cfg, filepath.Join(dir, "backup.db")); err != nil {
		t.Fatalf("the way to the check is blocked: %v", err)
	}
}

// TestBackupBringsNoSchema is the negative half of the promise: the copy is
// the bytes of the encrypted file, and Backup is not a second door into the
// database. It never reads a row, never applies a migration and never adds a
// table - so a file without any schema of ours is copied as it is, byte for
// byte, and the copy carries no table it did not have.
//
// This is the guard against the obvious rewrite: whoever reaches for
// sqlite.Open or for SQLite's own backup API instead of the bytes brings the
// migrations with it, and ngc_schema appears in both files.
func TestBackupBringsNoSchema(t *testing.T) {
	dir := t.TempDir()
	cfg := backupConfig(dir, "state.db")
	db, err := sqlite.OpenDB(cfg) // no migrations: OpenDB creates no table
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(backupCtx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(backupCtx, "INSERT INTO probe (a) VALUES (?)", []byte("secret-marker")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "backup.db")
	if err := sqlite.Backup(cfg, target); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, copied) {
		t.Errorf("the copy is %d bytes against %d of the file: it is not a copy of the file",
			len(copied), len(source))
	}
	for label, path := range map[string]string{"the database": cfg.Path, "the copy": target} {
		open := cfg
		open.Path = path
		db, err := sqlite.OpenDB(open)
		if err != nil {
			t.Fatalf("%s does not open: %v", label, err)
		}
		var tables string
		if err := db.QueryRowContext(backupCtx,
			"SELECT group_concat(name) FROM sqlite_master WHERE type = 'table' ORDER BY name").Scan(&tables); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if tables != "probe" {
			t.Errorf("%s holds the tables %q, want only probe", label, tables)
		}
		var got []byte
		if err := db.QueryRowContext(backupCtx, "SELECT a FROM probe").Scan(&got); err != nil || string(got) != "secret-marker" {
			t.Errorf("%s lost the row: %q, %v", label, got, err)
		}
		db.Close()
	}
}

// TestBackupKeepsTheKeyOutOfTheError: the key of the file sits in the
// configuration Backup is handed, and an error text that names the path is
// the shortest way to lose it.
func TestBackupKeepsTheKeyOutOfTheError(t *testing.T) {
	dir := t.TempDir()
	cfg := backupConfig(dir, "state.db")
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(backupCtx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	hexkey := strings.Repeat("ab", 64)
	// Three ways in: a target in a directory that is not there, a target
	// that is the database, and a database that is not there.
	for _, c := range []struct {
		label  string
		cfg    config.DB
		target string
	}{
		{"missing directory", cfg, filepath.Join(dir, "nowhere", "backup.db")},
		{"target is the database", cfg, cfg.Path},
		{"missing database", backupConfig(dir, "gone.db"), filepath.Join(dir, "backup.db")},
	} {
		err := sqlite.Backup(c.cfg, c.target)
		if err == nil {
			t.Errorf("%s: no error", c.label)
			continue
		}
		if strings.Contains(err.Error(), hexkey) || strings.Contains(err.Error(), "abab") ||
			strings.Contains(err.Error(), string(bytes.Repeat([]byte{0xab}, 8))) {
			t.Errorf("%s: the key shows in %q", c.label, err)
		}
	}
	// And the check is not vacuous: it does find such a key when one is in
	// the text.
	if !strings.Contains(fmt.Errorf("sqlite: %s", hexkey).Error(), "abab") {
		t.Error("the check itself does not catch a key in plain sight")
	}
}
