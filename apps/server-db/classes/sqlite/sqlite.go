// Package sqlite opens the encrypted SQLite file of NGC: the database of a
// controller, and the one that stands in for MariaDB when everything runs on
// one Pi. The file is encrypted with AES-256-XTS, so a copy of it is worth
// nothing without the key from the configuration.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/ncruces/go-sqlite3"
	sqlitedriver "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/vfs/xts" // registers the encrypting VFS "xts"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// DefaultBusyTimeout is how long a writer waits for another one before it
// gives up with "busy"; the store repeats such an operation itself. Every
// part of NGC opens with this one - OpenDB and Backup take no options - and
// TestOpenDBSetsThePragmasOfTheFile holds the number against the file.
const DefaultBusyTimeout = 10 * time.Second

// Options are the knobs of one pool that are not part of the configuration.
// The zero Options is what NGC runs with, so OpenDB(cfg) and
// OpenDBWith(cfg, Options{}) open the same file in the same way.
//
// BusyTimeout exists because the deadline is the one thing a MEASUREMENT has
// to be able to shorten. Three cases wait this deadline out on purpose - the
// sentence an admin reads when a neighbour holds the file (Punkt 48: 10 s
// times eight tries is 80,6 s) - and they used to take ten seconds each
// because the number sat in a const with no way in from outside. They now
// wait out the SAME deadline on a shorter clock: what they assert is the
// sentence and the refusal, not the length of the wait, and the length of the
// production wait is asserted in one place, against this constant.
//
// It is a deadline and not a switch: zero means DefaultBusyTimeout, and a
// negative one is refused rather than turned into "do not wait at all" - a
// pool that gives up immediately is a different file than the one NGC opens,
// and nothing should reach it by leaving a field empty the wrong way.
type Options struct {
	BusyTimeout time.Duration
}

// busyMillis is what the PRAGMA is given. SQLite takes whole milliseconds, so
// a deadline below one millisecond would silently become "do not wait": it is
// refused with the same sentence as a negative one.
func (o Options) busyMillis() (int64, error) {
	d := o.BusyTimeout
	if d == 0 {
		d = DefaultBusyTimeout
	}
	if d < time.Millisecond {
		return 0, fmt.Errorf("sqlite: busy timeout %v, want at least 1ms or zero for the default of %v",
			o.BusyTimeout, DefaultBusyTimeout)
	}
	return d.Milliseconds(), nil
}

// keySize is the key of the file: 64 bytes are AES-256-XTS, two AES-256 keys
// (config.DB.Key carries exactly this many).
const keySize = 64

// The two pools of one file: writes get ONE connection, reads get four. Only
// one connection of either stays idle, because each connection runs a SQLite
// build of its own and its memory comes back only when the connection closes
// - with several kept open, the largest blob each of them ever carried would
// stay paid for until the program ends, on a Pi the most expensive thing in
// here. An idle connection above the first is therefore dropped after a
// minute.
//
// That sum is per POOL, and while Backup runs there are FOUR of them on the
// same machine: the caller's write handle, the caller's read handle, the one
// Backup opens on the database, and the one fold opens on the copy (Kl-4 of
// the review of 10.10.2026 counted three, when the caller had one). Decided
// and named rather than changed: maxMemory below is a ceiling and not a
// reservation, the two backup pools live for the length of one backup, and
// each of the three besides the write handle holds one connection that reads
// and binds no blob - so what they really cost is nothing like four times the
// worst case of one. NOT MEASURED: how much the four hold together at the
// peak of a backup. Whoever has a controller measures it; until then this is
// the honest shape of the sentence, not a number.
//
// WHY THE SPLIT, MEASURED 13.10.2026 (point 48, built as point 49): with one
// pool of four, four was also the number at which the process blocked itself.
// A writer that waits for the file lock HOLDS its connection, so four waiting
// writers owned all four connections and the next query of the process - any
// query, a read as well - waited behind them: 2 blocked writers -> 0.007 s
// for the first read, 3 -> 0.007 s, 4 -> 9.5 s, on C: and on Z: (NAS) alike.
// With the split it is 0.006 s at 2, at 4 and at 192.
//
// What fixed that is the SEPARATE READ HANDLE, and not the one below: a
// throwaway mutation that puts writeConns back to 4 and leaves the split
// standing keeps the read fast (0.006 s at 192 blocked writers); one that
// takes the split away and restores the four brings 9.69 s back exactly.
// Measured, because point 48 had laid it down the other way round.
//
// writeConns = 1 is right for a second reason, not for that one: a SQLite
// file has exactly ONE writer, so a second write connection can only wait,
// and waiting in front of the handle inside Go is cheaper than waiting in
// busy_timeout. Measured as throughput of one process: 16 threads went from
// 404/s to 625/s on C: and from 241/s to 530/s on Z:.
const (
	writeConns   = 1
	readConns    = 4
	maxIdleConns = 1
	idleTimeout  = 60 * time.Second
)

// maxMemory bounds the memory of the SQLite build of one connection. The
// driver runs SQLite in WebAssembly, and a connection that runs past the
// bound does not return an error, it panics; the bound is a ceiling, not a
// reservation, so a connection that stays small stays small.
//
// 512 MiB is sqlstore.MaxData (80 MiB, the largest blob) with room for the
// page cache, the journal and the copy a statement makes while it binds the
// blob. The driver's own default is 4096 pages (256 MiB) on a 64-bit machine
// and 512 pages (32 MiB) on a 32-bit one - the largest blob would not fit
// through a 32-bit build at all, which is one more reason NGC builds only
// linux/amd64 and linux/arm64.
const maxMemory = 512 << 20

// OpenDB opens the file named by cfg, encrypted with cfg.Key. It does not
// create any table: the schema comes from the migrations.
//
// The handle carries ONE connection, which is what a writer needs: every
// caller of OpenDB writes, or reads pages for a backup. Whoever wants reads
// beside the writes takes OpenRW.
func OpenDB(cfg config.DB) (*sql.DB, error) { return OpenDBWith(cfg, Options{}) }

// OpenDBWith is OpenDB with the knobs of Options. Nothing in NGC calls it
// with a non-zero Options; what it is for is a measurement that has to wait
// out the busy deadline without waiting ten seconds for it.
//
// The two live together and neither gives way: the handle carries one write
// connection (see writeConns), and Options shortens the deadline that one
// connection waits on. A measurement of the split needs BOTH - the queue in
// front of the write handle is bounded by one chain of the OPEN database
// (sqlstore.takeTurn reads the busy_timeout pragma back off it), so shortening
// the deadline here shortens the bound there as well, and a measurement of
// the queue does not have to sit out eight ten-second tries either.
func OpenDBWith(cfg config.DB, opt Options) (*sql.DB, error) {
	busy, err := opt.busyMillis()
	if err != nil {
		return nil, err
	}
	if cfg.Driver != config.DriverSQLite {
		return nil, fmt.Errorf("sqlite: driver %q, want %q", cfg.Driver, config.DriverSQLite)
	}
	key := cfg.Key.Bytes()
	if len(key) != keySize {
		clear(key)
		return nil, errors.New("sqlite: the key must be 64 bytes for AES-256-XTS")
	}
	// The key is the only value that ever reaches SQL as text instead of as a
	// placeholder, because PRAGMA takes no placeholder. hex.EncodeToString of
	// 64 bytes is always 128 characters of 0-9 and a-f, so there is nothing in
	// it that could end the string or add a statement. It travels on as a
	// SecretText: printing, logging or encoding it shows "[secret]".
	hexkey := config.NewSecretText(hex.EncodeToString(key))
	clear(key) // the copy of the raw key is done with; the hex one the pool keeps
	dsn, err := fileURL(cfg.Path)
	if err != nil {
		return nil, err
	}
	inner, err := (&sqlitedriver.SQLite{}).OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector{inner: inner, hexkey: hexkey, busy: busy})
	db.SetMaxOpenConns(writeConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxIdleTime(idleTimeout)
	return db, nil
}

// OpenRW opens the file twice: write carries one connection and every write,
// read carries readConns connections and every read. Both are pools on the
// same file with the same key.
//
// This is the shape of the whole change of point 48, and it is two sentences:
// a SQLite file has one writer, so a second write connection can only wait;
// and a connection that waits is a connection a read cannot have. Giving the
// writes one connection and the reads their own handle separates the two
// waits. The caller hands both to sqlstore.NewSplit.
//
// The read handle can write - it is the same connector, and asking SQLite for
// a read-only connection would also refuse the PRAGMAs the file needs. That
// nothing writes through it is this package's doing, not SQLite's: Open is
// the only caller, and it gives the handle to NewSplit, which uses it for
// queries only.
func OpenRW(cfg config.DB) (write, read *sql.DB, err error) {
	return OpenRWWith(cfg, Options{})
}

// OpenRWWith is OpenRW with the knobs of Options, and it is OpenDBWith's
// reason for existing carried to the split: both handles get the SAME
// deadline, because a measurement that shortens one and not the other would
// be measuring two different files.
//
// Nothing in NGC calls it with a non-zero Options.
func OpenRWWith(cfg config.DB, opt Options) (write, read *sql.DB, err error) {
	if write, err = OpenDBWith(cfg, opt); err != nil {
		return nil, nil, err
	}
	if read, err = OpenDBWith(cfg, opt); err != nil {
		write.Close()
		return nil, nil, err
	}
	read.SetMaxOpenConns(readConns)
	return write, read, nil
}

// connector opens every connection of the pool. It wraps the driver's own
// connector for two things the driver cannot be told from outside: the bound
// on the memory of the SQLite build, and the PRAGMAs the file needs.
//
// The bound has to sit here. It is read from the context that opens the
// connection, and database/sql opens connections from a context of its own -
// a context handed to a query comes far too late.
type connector struct {
	inner  driver.Connector
	hexkey config.SecretText
	// busy is the busy_timeout of every connection of this pool, in
	// milliseconds. It is set once, when the pool is opened, and read by
	// every connection the pool makes afterwards.
	busy int64
}

// Driver implements database/sql/driver.Connector.
func (c connector) Driver() driver.Driver { return c.inner.Driver() }

// Connect implements database/sql/driver.Connector.
func (c connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(sqlite3.WithMaxMemory(ctx, maxMemory))
	if err != nil {
		return nil, err
	}
	if err := c.prepare(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// prepare runs on the new connection what the file needs. Order matters: the
// key first, because everything after it reads the file. Temporary files stay
// in memory so that no part of the data lands on disk with a random key
// instead of ours.
func (c connector) prepare(ctx context.Context, conn driver.Conn) error {
	raw, ok := conn.(sqlitedriver.Conn)
	if !ok {
		return errors.New("sqlite: the driver did not hand out a SQLite connection")
	}
	s := raw.Raw()
	// As the driver does it while it connects: whoever gave up waiting stops
	// these statements too, and the connection is free again afterwards.
	if old := s.SetInterrupt(ctx); old != ctx {
		defer s.SetInterrupt(old)
	}
	for _, pragma := range []string{
		"PRAGMA hexkey='" + c.hexkey.Text() + "'",
		fmt.Sprintf("PRAGMA busy_timeout=%d", c.busy),
		"PRAGMA journal_mode=wal",
		"PRAGMA synchronous=normal",
		"PRAGMA temp_store=memory",
		"PRAGMA foreign_keys=on",
	} {
		if err := s.Exec(pragma); err != nil {
			return err
		}
	}
	return nil
}

// fileURL turns the path of the configuration into the data source name.
// Building it with net/url escapes the path, so a path that holds a "?" can
// never add a URI parameter of its own - "?hexkey=00" would otherwise put a
// key of the attacker's choosing next to ours, and the VFS reads a parameter
// before it reads our PRAGMA. validPath refuses such a path on top of that,
// so that the one thing standing between a file name and a foreign key is
// never the escaping alone.
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
		return "", errors.New("sqlite: the path must be an absolute, clean file name")
	}
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "vfs=xts&_txlock=immediate"}
	return u.String(), nil
}

// validPath reports whether path names a file and nothing else. The
// configuration asks for an absolute path (config.DB.Validate: it starts with
// "/"), which on the targets linux/amd64 and linux/arm64 is exactly what
// filepath.IsAbs says; on a developer's machine it lets the tests run with
// that machine's own absolute paths. It also has to be the short form of
// itself, so that no "/var/lib/ngc/../../etc/x.db" walks out of the place the
// installation gave the database.
//
// "?" and "#" open the query and the fragment of an SQLite URI. Escaped, they
// come back out of the URI as part of the file name, so the file would be
// exactly the one the configuration names - but a path that carries URI
// syntax is not what a configuration of ours means, and the next hand that
// builds a DSN by pasting strings together would hand over our key with it.
// A control character is worse: NUL ends a name inside SQLite, so that path
// would open a file nobody named.
func validPath(path string) bool {
	if !filepath.IsAbs(path) || len(path) > config.MaxField || path != filepath.Clean(path) {
		return false
	}
	for _, c := range []byte(path) {
		if c == '?' || c == '#' || c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}
