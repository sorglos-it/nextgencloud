package sqlstore

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/ncruces/go-sqlite3"
)

// TestDeciders guards the four readers of a driver error. They decide whether
// an operation runs a second time, so a wrong answer is either a write that
// silently disappears or a retry storm against a database that is already in
// trouble. None of it needs a running database.
//
// The SQLite cases carry the error codes themselves instead of a
// *sqlite3.Error: that type has only unexported fields, and building a real
// one would need the wasm driver, which belongs to the SQLite part. Both take
// the same road through errors.Is, because (*sqlite3.Error).Is compares
// exactly these codes.
func TestDeciders(t *testing.T) {
	cases := []struct {
		name                           string
		d                              Dialect
		err                            error
		retry, duplicate, exists, gone bool
	}{
		// MariaDB: the numbers that mean "run it again".
		{"deadlock or Galera conflict", MariaDB, &mysql.MySQLError{Number: errDeadlock}, true, false, false, false},
		{"lock wait timeout", MariaDB, &mysql.MySQLError{Number: errLockWait}, true, false, false, false},
		{"node not ready", MariaDB, &mysql.MySQLError{Number: errWsrepAborted}, true, false, false, false},
		{"wrapped deadlock", MariaDB, fmt.Errorf("insert: %w", &mysql.MySQLError{Number: errDeadlock}), true, false, false, false},

		// MariaDB: a broken key, and the three "already there" of a migration.
		{"duplicate entry", MariaDB, &mysql.MySQLError{Number: errDupEntry}, false, true, false, false},
		{"table exists", MariaDB, &mysql.MySQLError{Number: errTableExists}, false, false, true, false},
		{"index exists", MariaDB, &mysql.MySQLError{Number: errDupKeyName}, false, false, true, false},
		{"column exists", MariaDB, &mysql.MySQLError{Number: errDupColumn}, false, false, true, false},
		{"unknown table", MariaDB, &mysql.MySQLError{Number: 1146}, false, false, false, false},

		// MariaDB: the connection died under us. ErrBadConn is the one the
		// driver returns most often, and database/sql hands it on unchanged
		// after its own two tries.
		{"bad connection", MariaDB, driver.ErrBadConn, false, false, false, true},
		{"wrapped bad connection", MariaDB, fmt.Errorf("query: %w", driver.ErrBadConn), false, false, false, true},
		{"invalid connection", MariaDB, mysql.ErrInvalidConn, false, false, false, true},
		{"packets out of sync", MariaDB, mysql.ErrPktSync, false, false, false, true},
		{"connection reset", MariaDB, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, false, false, false, true},
		{"connection refused", MariaDB, fmt.Errorf("dial: %w", syscall.ECONNREFUSED), false, false, false, true},
		{"broken pipe", MariaDB, fmt.Errorf("write: %w", syscall.EPIPE), false, false, false, true},
		{"server hung up", MariaDB, io.EOF, false, false, false, true},

		// MariaDB: what the caller itself gave up on is never "gone". The
		// driver hands the context error straight back (packets.go asks
		// mc.canceled before it falls back to ErrInvalidConn), and both it and
		// a socket deadline satisfy net.Error.
		{"deadline of the caller", MariaDB, context.DeadlineExceeded, false, false, false, false},
		{"canceled by the caller", MariaDB, context.Canceled, false, false, false, false},
		{"wrapped deadline of the caller", MariaDB, fmt.Errorf("query: %w", context.DeadlineExceeded), false, false, false, false},
		{"socket deadline", MariaDB, os.ErrDeadlineExceeded, false, false, false, false},
		{"socket deadline in an OpError", MariaDB, &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, false, false, false, false},
		{"no error", MariaDB, nil, false, false, false, false},

		// SQLite: busy and locked are the two worth repeating.
		{"busy", SQLite, sqlite3.BUSY, true, false, false, false},
		{"locked", SQLite, sqlite3.LOCKED, true, false, false, false},
		{"wrapped busy snapshot", SQLite, fmt.Errorf("step: %w", sqlite3.BUSY_SNAPSHOT), true, false, false, false},

		// SQLite: a broken key. A plain CONSTRAINT says nothing about which.
		{"primary key", SQLite, sqlite3.CONSTRAINT_PRIMARYKEY, false, true, false, false},
		{"unique index", SQLite, sqlite3.CONSTRAINT_UNIQUE, false, true, false, false},
		{"not null", SQLite, sqlite3.CONSTRAINT_NOTNULL, false, false, false, false},
		{"plain constraint", SQLite, sqlite3.CONSTRAINT, false, false, false, false},

		// SQLite: only an I/O error may be read again. A malformed file does
		// not heal by trying once more, so it is deliberately not "gone".
		{"disk error", SQLite, sqlite3.IOERR, false, false, false, true},
		{"wrapped read error", SQLite, fmt.Errorf("read: %w", sqlite3.IOERR_READ), false, false, false, true},
		{"corrupt database", SQLite, sqlite3.CORRUPT, false, false, false, false},
		{"not a database", SQLite, sqlite3.NOTADB, false, false, false, false},
		{"deadline of the caller", SQLite, context.DeadlineExceeded, false, false, false, false},
		{"no error", SQLite, nil, false, false, false, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%T/%s", c.d, c.name), func(t *testing.T) {
			if got := c.d.Retry(c.err); got != c.retry {
				t.Errorf("Retry = %v, want %v", got, c.retry)
			}
			if got := c.d.Duplicate(c.err); got != c.duplicate {
				t.Errorf("Duplicate = %v, want %v", got, c.duplicate)
			}
			if got := c.d.Exists(c.err); got != c.exists {
				t.Errorf("Exists = %v, want %v", got, c.exists)
			}
			if got := c.d.Gone(c.err); got != c.gone {
				t.Errorf("Gone = %v, want %v", got, c.gone)
			}
		})
	}
}

// TestDDLInTx holds the one difference the migrations build on: MariaDB
// commits CREATE and ALTER on its own, SQLite keeps them in the transaction.
func TestDDLInTx(t *testing.T) {
	if MariaDB.DDLInTx() {
		t.Error("MariaDB: DDL does not stay in a transaction")
	}
	if !SQLite.DDLInTx() {
		t.Error("SQLite: DDL stays in the transaction")
	}
}
