package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// maxTries is how often an operation is repeated after a conflict that rolled
// it back, and pause is how long the first wait is; it doubles, with a little
// noise so that two servers do not keep colliding in step.
const (
	maxTries = 8
	pause    = 2 * time.Millisecond
)

type store struct {
	db *sql.DB
	d  Dialect
}

// New returns dbapi.Store on an open database. It creates nothing: the schema
// comes from the migrations.
func New(db *sql.DB, d Dialect) dbapi.Store { return &store{db: db, d: d} }

func (s *store) Close() error { return s.db.Close() }

// write runs fn in one transaction and repeats the whole of it while the
// database says the transaction was rolled back: a Galera certification
// conflict (two nodes wrote the same row at once), a lock that waited too
// long, SQLite's "busy". An error that may have been committed is never
// repeated, because repeating it could turn a success into ErrUsed.
func (s *store) write(ctx context.Context, fn func(tx *sql.Tx) error) error {
	var err error
	for try := range maxTries {
		if try > 0 && !wait(ctx, try) {
			return ctx.Err()
		}
		var tx *sql.Tx
		if tx, err = s.db.BeginTx(ctx, nil); err != nil {
			if s.d.Retry(err) || s.d.Gone(err) {
				continue
			}
			return err
		}
		if err = fn(tx); err != nil {
			tx.Rollback()
			if s.d.Retry(err) {
				continue
			}
			return err
		}
		if err = tx.Commit(); err == nil {
			return nil
		}
		if !s.d.Retry(err) {
			return err
		}
	}
	return err
}

// exec runs one statement, which needs no transaction of its own, and returns
// how many rows it matched. It repeats the statement on the same conflicts as
// write.
func (s *store) exec(ctx context.Context, stmt string, args ...any) (int64, error) {
	var err error
	for try := range maxTries {
		if try > 0 && !wait(ctx, try) {
			return 0, ctx.Err()
		}
		var res sql.Result
		if res, err = s.db.ExecContext(ctx, stmt, args...); err != nil {
			if s.d.Retry(err) {
				continue
			}
			return 0, err
		}
		return res.RowsAffected()
	}
	return 0, err
}

// query runs a read. A read may also be repeated when the connection failed,
// because it changes nothing: database/sql then takes another connection, and
// with MariaDB another node.
func (s *store) query(ctx context.Context, fn func() error) error {
	var err error
	for try := range maxTries {
		if try > 0 && !wait(ctx, try) {
			return ctx.Err()
		}
		if err = fn(); err == nil || (!s.d.Retry(err) && !s.d.Gone(err)) {
			return err
		}
	}
	return err
}

// put writes a row that may or may not be there. Neither MERGE nor ON
// DUPLICATE KEY is portable, so it is UPDATE first and INSERT when nothing
// matched. The driver counts matched rows, not changed ones (MariaDB gets
// ClientFoundRows for it), so writing the same values again counts as a hit
// and does not turn into an INSERT.
func (s *store) put(ctx context.Context, update string, updateArgs []any, insert string, insertArgs []any) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		for round := range 2 {
			res, err := tx.ExecContext(ctx, update, updateArgs...)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n > 0 {
				return nil
			}
			if round == 1 {
				return errors.New("sqlstore: the row is neither there nor addable")
			}
			if _, err := tx.ExecContext(ctx, insert, insertArgs...); err != nil {
				if !s.d.Duplicate(err) {
					return err
				}
				continue // somebody was faster: update that row instead
			}
			return nil
		}
		return nil
	})
}

// wait sleeps before the next try and reports whether the context is still
// alive.
func wait(ctx context.Context, try int) bool {
	d := pause << min(try, 6)
	d += time.Duration(rand.Int64N(int64(d)))
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Not written yet: Log... (task 7). Each line goes away with its task;
// Once... and Lock... went with task 5 and stand in once.go now, User...,
// Session... and Pending... with task 6 and stand in user.go. No test takes
// the answer of one of them for a final one; the single test that calls them,
// TestTheRestIsNotWrittenYet, holds nothing but that they are still
// provisional, and it is meant to go red the moment one of them is built -
// that is what takes a finished method out of the list below instead of
// leaving it lying here.

func (s *store) LogAdd(context.Context, core.LogEntry) error { return errors.ErrUnsupported }

func (s *store) LogQuery(context.Context, core.LogQuery) ([]core.LogEntry, error) {
	return nil, errors.ErrUnsupported
}
