package sqlstore

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/go-sql-driver/mysql"
	"github.com/ncruces/go-sqlite3"
)

// Dialect is everything that differs between the databases behind the same
// SQL: the column types of a migration and the meaning of a driver error.
type Dialect interface {
	// Type returns the column type for a placeholder of a migration, or ""
	// for one it does not know. "opts" may return "".
	Type(placeholder string) string
	// Retry reports whether err is a conflict that rolled the statement
	// back, so the caller may run the whole operation again unchanged: a
	// Galera certification conflict, a lock that waited too long, SQLite's
	// "busy". It is never true for an error that may have committed.
	Retry(err error) bool
	// Duplicate reports whether err is a broken primary key or unique index.
	Duplicate(err error) bool
	// Exists reports whether err says a table, index or column is already
	// there, which a migration that runs a second time after a crash sees.
	Exists(err error) bool
	// Gone reports whether err means the connection failed, so a read may be
	// repeated, possibly on another node.
	Gone(err error) bool
	// DDLInTx reports whether CREATE and ALTER can live in a transaction.
	// MariaDB commits them on its own, SQLite does not.
	DDLInTx() bool
}

// MariaDB is the dialect of MariaDB and MySQL.
var MariaDB Dialect = mariaDialect{}

// SQLite is the dialect of SQLite.
var SQLite Dialect = sqliteDialect{}

type mariaDialect struct{}

func (mariaDialect) Type(p string) string {
	switch p {
	case "key":
		return "VARBINARY(255)"
	case "text":
		return "VARBINARY(1024)"
	case "doc":
		return "MEDIUMBLOB"
	case "data":
		return "LONGBLOB"
	case "int":
		return "BIGINT"
	case "opts":
		// InnoDB is what Galera replicates; DYNAMIC keeps long keys usable.
		// No space in front: a statement carries one between every two of its
		// pieces already, and a second one would only ever be written here.
		return "ENGINE=InnoDB ROW_FORMAT=DYNAMIC"
	}
	return ""
}

// MariaDB error numbers, https://mariadb.com/kb/en/mariadb-error-codes/.
const (
	errDupEntry     = 1062 // duplicate value for a key
	errDupKeyName   = 1061 // index is already there
	errDupColumn    = 1060 // column is already there
	errTableExists  = 1050 // table is already there
	errLockWait     = 1205 // lock wait timeout, rolled back
	errDeadlock     = 1213 // deadlock; Galera reports a certification conflict here
	errWsrepAborted = 1047 // node is not ready to serve, the transaction is gone
)

func mysqlNumber(err error) uint16 {
	var e *mysql.MySQLError
	if errors.As(err, &e) {
		return e.Number
	}
	return 0
}

func (mariaDialect) Retry(err error) bool {
	n := mysqlNumber(err)
	return n == errDeadlock || n == errLockWait || n == errWsrepAborted
}

func (mariaDialect) Duplicate(err error) bool { return mysqlNumber(err) == errDupEntry }

func (mariaDialect) Exists(err error) bool {
	n := mysqlNumber(err)
	return n == errTableExists || n == errDupKeyName || n == errDupColumn
}

func (mariaDialect) Gone(err error) bool {
	// A driver error without a MariaDB number is a broken connection: the
	// server never answered. Only a read is repeated after it. ErrBadConn
	// belongs in front: the driver returns it far more often than
	// ErrInvalidConn, and database/sql hands it on unchanged once its own two
	// tries are spent.
	return err != nil && mysqlNumber(err) == 0 &&
		(errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) ||
			errors.Is(err, mysql.ErrPktSync) || isNetwork(err))
}

func (mariaDialect) DDLInTx() bool { return false }

type sqliteDialect struct{}

func (sqliteDialect) Type(p string) string {
	switch p {
	case "key", "text", "doc", "data":
		return "BLOB"
	case "int":
		return "INTEGER"
	case "opts":
		return ""
	}
	return ""
}

func (sqliteDialect) Retry(err error) bool {
	return errors.Is(err, sqlite3.BUSY) || errors.Is(err, sqlite3.LOCKED)
}

func (sqliteDialect) Duplicate(err error) bool {
	return errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) || errors.Is(err, sqlite3.CONSTRAINT_UNIQUE)
}

// Exists reads the message, and it is the only place in NGC that does: SQLite
// has no error code of its own for "it is already there". A CREATE of a table,
// an index or a view that exists reports the generic SQLITE_ERROR, the same
// code a syntax error carries, so the code alone would step over every broken
// statement in a migration. Both have to hold: the code SQLITE_ERROR and the
// words "already exists" that SQLite writes into exactly this message.
//
// What asks for it: an update replays every migration on a database the
// previous API version is still working on. Under MariaDB each CREATE commits
// on its own and the numbers 1050, 1060 and 1061 say so; SQLite keeps the
// whole file in one transaction, and without this the second run would fail on
// the first table instead of stepping over it.
//
// The one real error this swallows is a NAME COLLISION, and it is measured,
// not feared: "CREATE INDEX ngc_blob_seq ON ngc_blob (id)" on a database that
// already carries that index over other columns answers "index ngc_blob_seq
// already exists", so it is stepped over and the migration is recorded as
// done. SQLite says nothing about the content in that message, so no reading
// of it could tell the two apart. What keeps it out is the rule that a
// migration only ever adds and the lint of task 3, which refuses a name that
// is not in its fixed lists - not this function.
func (sqliteDialect) Exists(err error) bool {
	return errors.Is(err, sqlite3.ERROR) && strings.Contains(err.Error(), "already exists")
}

func (sqliteDialect) Gone(err error) bool { return errors.Is(err, sqlite3.IOERR) }

func (sqliteDialect) DDLInTx() bool { return true }

// isNetwork reports whether err comes from the network below the driver.
//
// What the caller itself gave up on comes first and is refused, however much
// it looks like a network error: context.DeadlineExceeded and a socket
// deadline both satisfy net.Error, and the driver hands the context error
// straight back (packets.go asks mc.canceled before it falls back to
// ErrInvalidConn). Counting those as a broken connection would repeat on
// another node what the caller had already abandoned - retries piling up
// exactly when the database is too slow anyway.
func isNetwork(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, os.ErrDeadlineExceeded) {
		return false
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE)
}
