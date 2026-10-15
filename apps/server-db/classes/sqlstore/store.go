package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"time"

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
			return cause(ctx, ctx.Err())
		}
		var tx *sql.Tx
		if tx, err = s.db.BeginTx(ctx, nil); err != nil {
			if s.d.Retry(err) || s.d.Gone(err) {
				continue
			}
			return cause(ctx, err)
		}
		if err = fn(tx); err != nil {
			tx.Rollback()
			if s.d.Retry(err) {
				continue
			}
			return cause(ctx, err)
		}
		if err = tx.Commit(); err == nil {
			return nil
		}
		if !s.d.Retry(err) {
			return cause(ctx, err)
		}
	}
	return cause(ctx, err)
}

// cause hands the caller back the reason HE named for his deadline instead of
// the bare context.DeadlineExceeded of the standard library, and nothing else:
// an error that is not the context ending passes through untouched.
//
// It is needed because the context error reaches a caller from two different
// places and only one of them is this file. The loops above return ctx.Err()
// themselves when the pause between two tries is cut short -- but
// database/sql returns the very same error out of BeginTx, ExecContext and
// Query the moment the context is done, and that one never passes a line of
// ours. Without this, which of the two happened would decide whether the
// caller's reason survives, and the caller cannot see the difference.
//
// It asks ctx.Err() and not the error alone, so an error that merely LOOKS
// like a cancellation -- a driver that wraps context.Canceled into something
// of its own, a row that holds the words -- is not turned into the caller's
// reason while his context is still open.
func cause(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return err
	}
	if c := context.Cause(ctx); c != nil {
		return c
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
			return 0, cause(ctx, ctx.Err())
		}
		var res sql.Result
		if res, err = s.db.ExecContext(ctx, stmt, args...); err != nil {
			if s.d.Retry(err) {
				continue
			}
			return 0, cause(ctx, err)
		}
		return res.RowsAffected()
	}
	return 0, cause(ctx, err)
}

// query runs a read. A read may also be repeated when the connection failed,
// because it changes nothing: database/sql then takes another connection, and
// with MariaDB another node.
func (s *store) query(ctx context.Context, fn func() error) error {
	var err error
	for try := range maxTries {
		if try > 0 && !wait(ctx, try) {
			return cause(ctx, ctx.Err())
		}
		if err = fn(); err == nil || (!s.d.Retry(err) && !s.d.Gone(err)) {
			return cause(ctx, err)
		}
	}
	return cause(ctx, err)
}

// put writes a row that may or may not be there. Neither MERGE nor ON
// DUPLICATE KEY is portable, so it is UPDATE first and INSERT when nothing
// matched. That writing the same values again counts as a hit and does not
// turn into an INSERT is the driver's word and not this method's: the driver
// has to count the rows the UPDATE matched and not the ones it really
// changed, which on MariaDB is the ClientFoundRows flag of the DSN. No
// MariaDB connection is opened anywhere in this repository yet, so nothing
// sets it today, and whoever writes that DSN has to - the same debt LockTake
// names.
//
// Whoever writes it owes something before it, and this is the place that says
// so, because after the connection stands it is too late. UserPut had this
// very shape and stopped resting on the count: it asks the database whether a
// row carries this key instead of reading it off n, and so it answers the
// same under either counting rule, which
// TestUserPutAnswersTheSameHoweverRowsAreCounted holds by running both. put
// can be given the same. The key it would ask with is already here - each of
// the four callers names it in its WHERE, at the end of updateArgs, one
// argument or two - and what is missing is only that this signature does not
// say which of those arguments it is. That is the shape of today's
// parameters, not an obstacle in the thing, and the sentence that stood here
// before - that put does not know its key - said the second when it meant the
// first.
//
// It has not been given the same because nobody has been asked for it: put
// carries BlobPut and AppPut as well, which belong to the tasks before this
// one, and a change here is a change to all four. Until somebody is asked,
// this is what stands: on a driver that counts changed rows, writing a row
// again byte for byte comes back from here as "the row is neither there nor
// addable" - the UPDATE finds nothing to change, the INSERT behind it breaks
// on the primary key, and the second round finds nothing again. The review of
// round 4 measured that, with a trigger that turns the counting round, for
// all four callers - SessionPut, PendingPut, BlobPut, AppPut - against <nil>
// without the trigger. TestBlobRoundTrip promises the opposite in as many
// words, "Writing the very same row again must work", and SQLite cannot show
// the break because it counts every row its UPDATE went through.
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
