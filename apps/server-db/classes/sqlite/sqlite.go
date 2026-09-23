// Package sqlite opens the encrypted SQLite file of NGC: the database of a
// controller, and the one that stands in for MariaDB when everything runs on
// one Pi. The file is encrypted with AES-256-XTS, so a copy of it is worth
// nothing without the key from the configuration.
package sqlite

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/ncruces/go-sqlite3"
	sqlitedriver "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/vfs/xts" // registers the encrypting VFS "xts"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// busyTimeout is how long a writer waits for another one before it gives up
// with "busy"; the store repeats such an operation itself.
const busyTimeout = 10000 // milliseconds

// keySize is the key of the file: 64 bytes are AES-256-XTS, two AES-256 keys
// (config.DB.Key carries exactly this many).
const keySize = 64

// maxConns is how many connections the pool keeps. A handful is plenty for
// one controller or one Pi, and it keeps the memory of the SQLite build
// small. Writers take the file lock one after another; busy_timeout and the
// store's repeat handle the wait.
const maxConns = 4

// OpenDB opens the file named by cfg, encrypted with cfg.Key. It does not
// create any table: the schema comes from the migrations.
func OpenDB(cfg config.DB) (*sql.DB, error) {
	if cfg.Driver != config.DriverSQLite {
		return nil, fmt.Errorf("sqlite: driver %q, want %q", cfg.Driver, config.DriverSQLite)
	}
	key := cfg.Key.Bytes()
	if len(key) != keySize {
		return nil, errors.New("sqlite: the key must be 64 bytes for AES-256-XTS")
	}
	// The key is the only value that ever reaches SQL as text instead of as a
	// placeholder, because PRAGMA takes no placeholder. hex.EncodeToString of
	// 64 bytes is always 128 characters of 0-9 and a-f, so there is nothing in
	// it that could end the string or add a statement.
	hexkey := hex.EncodeToString(key)
	clear(key) // the copy of the raw key is done with; the hex one the pool keeps
	dsn, err := fileURL(cfg.Path)
	if err != nil {
		return nil, err
	}
	db, err := sqlitedriver.Open(dsn, func(c *sqlite3.Conn) error {
		// Order matters: the key first, because everything after it reads the
		// file. Temporary files stay in memory so that no part of the data
		// lands on disk with a random key instead of ours.
		for _, pragma := range []string{
			"PRAGMA hexkey='" + hexkey + "'",
			fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout),
			"PRAGMA journal_mode=wal",
			"PRAGMA synchronous=normal",
			"PRAGMA temp_store=memory",
			"PRAGMA foreign_keys=on",
		} {
			if err := c.Exec(pragma); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	return db, nil
}

// fileURL turns the path of the configuration into the data source name.
// Building it with net/url escapes the path, so a path that holds a "?" can
// never add a URI parameter of its own - "?hexkey=00" would otherwise put a
// key of the attacker's choosing next to ours. validPath refuses such a path
// outright, because a file of that name is not what the configuration meant
// either.
//
// _txlock=immediate makes a write transaction take the lock when it begins.
// Otherwise SQLite takes it at the first write, and two transactions that
// both read first block each other instead of waiting.
//
// The URI keeps no authority ("file:/dir/x.db", not "file:///dir/x.db"):
// SQLite reads both alike, and only this form also carries the path of a
// developer's machine, where an absolute path starts with a drive letter
// that would otherwise be read as the authority.
func fileURL(path string) (string, error) {
	if !validPath(path) {
		return "", errors.New("sqlite: the path must be an absolute file name")
	}
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "vfs=xts&_txlock=immediate"}
	return u.String(), nil
}

// validPath reports whether path names a file and nothing else. The
// configuration asks for an absolute path (config.DB.Validate: it starts with
// "/"), which on the targets linux/amd64 and linux/arm64 is exactly what
// filepath.IsAbs says; on a developer's machine it lets the tests run with
// that machine's own absolute paths.
//
// "?" and "#" open the query and the fragment of an SQLite URI, and a control
// character - NUL above all - ends a name inside SQLite. net/url escapes all
// of them, so none of them could add a parameter of its own, but each would
// silently turn into a file of a different name than the configuration names.
func validPath(path string) bool {
	if !filepath.IsAbs(path) || len(path) > config.MaxField {
		return false
	}
	for _, c := range []byte(path) {
		if c == '?' || c == '#' || c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}
