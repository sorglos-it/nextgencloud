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
// What makes the test not vacuous is the JOURNAL, not the lock, and the
// earlier version of this sentence had it the wrong way round. Measured
// 10.10.2026: with the write lock taken out this test stays green (mutant m1,
// code 0); with the journal left out of the copy it tears and the test fails
// (mutant m4). A checkpoint really does move pages into the file while it is
// read - the file changed in 7 of 8 windows with the lock held - and what
// repairs that is replaying the journal over the copy.
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

// seeded makes a database with one row in it and closes it again, so a case
// below can compare the bytes before and after a refused call.
func seeded(t *testing.T, cfg config.DB) []byte {
	t.Helper()
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
	raw, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestBackupRefusesEveryNameOfTheDatabase is the guard that comparing two
// strings is not. Measured on 10.10.2026 before it was built: cfg.Path goes
// through validPath, the target went through nothing, so another spelling of
// the same path ("/dir/./state.db", "/dir/sub/../state.db") was not equal to
// it and ran all the way to os.Rename ON THE DATABASE. What refused it there
// was Windows. On linux/amd64 and linux/arm64, the only targets NGC builds
// for, os.Rename would have replaced the running database.
//
// Every case therefore asks for the refusal of this package, named by its own
// words - not merely for "an error", which is what the operating system
// happened to give. And after every case the bytes of the database have to be
// the ones from before it.
//
// Each case gets a directory and a database of its own: a case that destroyed
// the file would otherwise decide the next one.
func TestBackupRefusesEveryNameOfTheDatabase(t *testing.T) {
	// name -> how the target is built from the database, and the words the
	// refusal has to carry.
	for _, c := range []struct {
		label string
		db    string
		make  func(dir, path string) string
		want  string
	}{
		{"a dot segment in the middle", "state.db", func(dir, path string) string {
			return filepath.Join(dir, ".") + string(filepath.Separator) + "." + string(filepath.Separator) + "state.db"
		}, "absoluter, einfacher Dateiname"},
		{"a parent segment walking back", "state.db", func(dir, path string) string {
			return filepath.Join(dir, "sub") + string(filepath.Separator) + ".." + string(filepath.Separator) + "state.db"
		}, "absoluter, einfacher Dateiname"},
		{"the journal of the database", "state.db", func(dir, path string) string {
			return path + "-wal"
		}, "eigenen Dateinamen"},
		{"the shared-memory file", "state.db", func(dir, path string) string {
			return path + "-shm"
		}, "eigenen Dateinamen"},
		// The copy is written under target + ".part" before the rename, so a
		// database whose own name ends in ".part" is hit by a target that
		// looks harmless.
		{"the name the copy carries while it is written", "state.db.part", func(dir, path string) string {
			return filepath.Join(dir, "state.db")
		}, "eigenen Dateinamen"},
		{"a name that is not absolute", "state.db", func(dir, path string) string {
			return "backup.db"
		}, "absoluter, einfacher Dateiname"},
	} {
		t.Run(c.label, func(t *testing.T) {
			dir := t.TempDir()
			cfg := backupConfig(dir, c.db)
			before := seeded(t, cfg)
			target := c.make(dir, cfg.Path)
			err := sqlite.Backup(cfg, target)
			if err == nil {
				t.Fatalf("target %q was accepted", target)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("target %q is refused by something else: %v", target, err)
			}
			after, readErr := os.ReadFile(cfg.Path)
			if readErr != nil {
				t.Fatalf("the database is gone after the refused call: %v", readErr)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("the refused call changed the database: %d bytes against %d", len(after), len(before))
			}
			// The way to the check is open: a clean name beside it goes
			// through on the very same database.
			if err := sqlite.Backup(cfg, filepath.Join(dir, "backup.db")); err != nil {
				t.Fatalf("the way to the check is blocked: %v", err)
			}
		})
	}
}

// TestBackupRefusesALinkAsItsTarget is the form that is not a spelling of the
// path at all: a target that is a symbolic link to the database. Following it
// would have to answer where it points at the moment of the write, which is
// not the moment it was looked at, so the link is refused outright.
//
// It says out loud when it cannot measure. Creating a symbolic link needs a
// privilege on Windows that a normal account does not have; where that is the
// case this case reports it instead of passing quietly, because a guard
// nobody ever ran is not a guard.
func TestBackupRefusesALinkAsItsTarget(t *testing.T) {
	dir := t.TempDir()
	cfg := backupConfig(dir, "state.db")
	before := seeded(t, cfg)
	link := filepath.Join(dir, "backup.db")
	if err := os.Symlink(cfg.Path, link); err != nil {
		t.Skipf("NOT MEASURED HERE: this machine does not let a test make a symbolic link (%v). "+
			"On the targets linux/amd64 and linux/arm64 it runs; whoever has one runs this case there.", err)
	}
	err := sqlite.Backup(cfg, link)
	if err == nil {
		t.Fatal("a link to the database was accepted as a target")
	}
	if !strings.Contains(err.Error(), "Verweis") {
		t.Errorf("the link is refused by something else: %v", err)
	}
	after, readErr := os.ReadFile(cfg.Path)
	if readErr != nil {
		t.Fatalf("the database is gone after the refused call: %v", readErr)
	}
	if !bytes.Equal(before, after) {
		t.Error("the refused call changed the database through the link")
	}
	// The way to the check is open: the same name without the link goes
	// through.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Backup(cfg, link); err != nil {
		t.Fatalf("the way to the check is blocked: %v", err)
	}
}

// TestBackupRefusesAnotherNameOfTheSameFile is the form that carries no mark
// at all: a hard link. It is a plain file to Lstat, it is spelled nothing like
// the database, and writing through it writes the database. os.SameFile is
// what sees it, and a hard link is the one alias this machine can really make,
// so this is where that question is measured.
func TestBackupRefusesAnotherNameOfTheSameFile(t *testing.T) {
	dir := t.TempDir()
	cfg := backupConfig(dir, "state.db")
	before := seeded(t, cfg)
	alias := filepath.Join(dir, "backup.db")
	if err := os.Link(cfg.Path, alias); err != nil {
		t.Skipf("NOT MEASURED HERE: this machine does not let a test make a hard link (%v)", err)
	}
	err := sqlite.Backup(cfg, alias)
	if err == nil {
		t.Fatal("another name of the database was accepted as a target")
	}
	if !strings.Contains(err.Error(), "dieselbe Datei") {
		t.Errorf("the alias is refused by something else: %v", err)
	}
	after, readErr := os.ReadFile(cfg.Path)
	if readErr != nil {
		t.Fatalf("the database is gone after the refused call: %v", readErr)
	}
	if !bytes.Equal(before, after) {
		t.Error("the refused call changed the database through its other name")
	}
	// The way to the check is open: the same name, no longer a second name of
	// the database, goes through.
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Backup(cfg, alias); err != nil {
		t.Fatalf("the way to the check is blocked: %v", err)
	}
}
