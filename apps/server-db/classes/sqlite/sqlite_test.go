package sqlite

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

var ctx = context.Background()

// testConfig is a fresh, encrypted file in the test's own directory.
func testConfig(t *testing.T, name string) config.DB {
	t.Helper()
	key := bytes.Repeat([]byte{0x5a}, 64) // 64 bytes: AES-256-XTS
	return config.DB{Driver: config.DriverSQLite, Path: filepath.Join(t.TempDir(), name), Key: config.NewSecret(key)}
}

func TestOpenEncrypts(t *testing.T) {
	cfg := testConfig(t, "state.db")
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO probe (a) VALUES (?)", []byte("secret-marker")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(cfg.Path)
	if err != nil || len(raw) < 16 {
		t.Fatalf("read back: %d bytes, %v", len(raw), err)
	}
	if bytes.HasPrefix(raw, []byte("SQLite format 3\x00")) {
		t.Error("the file is a plain SQLite database, so it is not encrypted")
	}
	if bytes.Contains(raw, []byte("secret-marker")) {
		t.Error("the file holds the value in the clear")
	}

	// The same key opens it again.
	db, err = OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	if err := db.QueryRowContext(ctx, "SELECT a FROM probe").Scan(&got); err != nil || string(got) != "secret-marker" {
		t.Errorf("with the right key: %q, %v", got, err)
	}
	db.Close()

	// Another key does not.
	other := cfg
	other.Key = config.NewSecret(bytes.Repeat([]byte{0x5b}, 64))
	db, err = OpenDB(other)
	if err == nil {
		err = db.QueryRowContext(ctx, "SELECT a FROM probe").Scan(&got)
		db.Close()
	}
	if err == nil {
		t.Error("the wrong key opened the file")
	}
}

func TestOpenRefusesBadConfig(t *testing.T) {
	for name, cfg := range map[string]config.DB{
		"no key":       {Driver: config.DriverSQLite, Path: "/tmp/x.db"},
		"short key":    {Driver: config.DriverSQLite, Path: "/tmp/x.db", Key: config.NewSecret(make([]byte, 32))},
		"relative":     {Driver: config.DriverSQLite, Path: "x.db", Key: config.NewSecret(make([]byte, 64))},
		"wrong driver": {Driver: config.DriverMariaDB},
		// A path may not smuggle a second URI parameter into the file name.
		"parameter in the path": {Driver: config.DriverSQLite, Path: "/tmp/x.db?hexkey=00", Key: config.NewSecret(make([]byte, 64))},
	} {
		if db, err := OpenDB(cfg); err == nil {
			db.Close()
			t.Errorf("%s was accepted", name)
		}
	}
	// "/tmp/x.db?hexkey=00" is only absolute where "/" starts a path, so on a
	// developer's machine the case above is refused for being relative and
	// proves nothing about the "?". This one carries the same "?" on a path
	// that is absolute everywhere.
	cfg := testConfig(t, "state.db?hexkey=00")
	if db, err := OpenDB(cfg); err == nil {
		db.Close()
		t.Error("a parameter in an absolute path was accepted")
	}
}

// TestFileURL guards the data source name. Whatever the path holds, the URI
// carries exactly the two parameters we put there and the path comes back
// out of it unchanged: no file name of ours turns into a key, a VFS or a
// lock mode of someone else's choosing.
func TestFileURL(t *testing.T) {
	dir := t.TempDir() // absolute on every machine the tests run on
	for _, name := range []string{"state.db", "a b.db", "a%3Fb.db", "a&b=c.db", "a+b,c;d.db", "Ärger.db"} {
		path := filepath.Join(dir, name)
		dsn, err := fileURL(path)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if !strings.HasPrefix(dsn, "file:") {
			t.Errorf("%q: %q does not start with file:", name, dsn)
		}
		// Read the URI the way SQLite does, not the way net/url does: the
		// file name is everything between "file:" and the first "?",
		// percent-decoded, and the parameters are the rest.
		if n := strings.Count(dsn, "?"); n != 1 {
			t.Errorf("%q: %d question marks in %q", name, n, dsn)
		}
		file, query, _ := strings.Cut(strings.TrimPrefix(dsn, "file:"), "?")
		got, err := url.PathUnescape(file)
		if err != nil || got != path {
			t.Errorf("%q: the URI names %q, %v", path, got, err)
		}
		q, err := url.ParseQuery(query)
		if err != nil || len(q) != 2 || q.Get("vfs") != "xts" || q.Get("_txlock") != "immediate" {
			t.Errorf("%q: parameters %v, %v", name, q, err)
		}
	}
	for label, path := range map[string]string{
		"empty":         "",
		"relative":      "x.db",
		"relative dot":  "./x.db",
		"question mark": filepath.Join(dir, "x.db?hexkey=00"),
		"hash":          filepath.Join(dir, "x.db#hexkey=00"),
		"line feed":     filepath.Join(dir, "x\n.db"),
		"nul":           filepath.Join(dir, "x\x00.db"),
		"delete":        filepath.Join(dir, "x\x7f.db"),
		"too long":      filepath.Join(dir, strings.Repeat("x", config.MaxField)+".db"),
	} {
		if dsn, err := fileURL(path); err == nil {
			t.Errorf("%s was accepted: %q", label, dsn)
		}
	}
}

// TestPragmas checks what every new connection has to carry. The key is not
// among them because it cannot be read back; that it was set first is what
// makes the rest of this work at all, since each of these statements already
// reads the encrypted file.
func TestPragmas(t *testing.T) {
	db, err := OpenDB(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	want := map[string]string{
		"journal_mode": "wal",
		"synchronous":  "1", // normal
		"temp_store":   "2", // memory
		"busy_timeout": "10000",
		"foreign_keys": "1",
	}
	// Hold every connection of the pool at once, so each of them is a new one
	// and each has to have run the callback.
	for i := range maxConns {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		defer conn.Close()
		for pragma, value := range want {
			var got string
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("connection %d, %s: %v", i, pragma, err)
			}
			if got != value {
				t.Errorf("connection %d: %s = %q, want %q", i, pragma, got, value)
			}
		}
	}
}

// TestWritersWait proves that two writers do not trip over each other: with
// _txlock=immediate the second one waits at the start of its transaction,
// and busy_timeout gives it long enough to get its turn.
func TestWritersWait(t *testing.T) {
	db, err := OpenDB(testConfig(t, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (a BLOB NOT NULL, PRIMARY KEY (a))"); err != nil {
		t.Fatal(err)
	}
	const writers = 16
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errs[i] = err
				return
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO probe (a) VALUES (?)", []byte{byte(i)}); err != nil {
				tx.Rollback()
				errs[i] = err
				return
			}
			errs[i] = tx.Commit()
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	var rows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM probe").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != writers {
		t.Errorf("%d rows, want %d", rows, writers)
	}
}
