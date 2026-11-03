package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// Every statement of this file, as a constant. Nothing is ever put together
// at run time: the table and column names are the ones the fixed lists in
// names.go carry, everything a caller brings goes in through a placeholder.
const (
	onceInsert = "INSERT INTO ngc_once (kind, token, data, expires, used) VALUES (?, ?, ?, ?, ?)"
	onceUse    = "UPDATE ngc_once SET used = ? WHERE kind = ? AND token = ? AND used = ? AND expires > ?"
	onceRead   = "SELECT data FROM ngc_once WHERE kind = ? AND token = ?"
	onceState  = "SELECT expires FROM ngc_once WHERE kind = ? AND token = ?"
	oncePurge  = "DELETE FROM ngc_once WHERE expires <= ?"

	lockTake = "UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ? AND (owner = ? OR until <= ?)"
	lockRow  = "SELECT owner, until FROM ngc_lock WHERE name = ?"
	// The same read for a database that wants the row held for the write
	// behind it, see Dialect.RowLock. Two constants and not one put
	// together: a statement in this package is always readable off the file.
	lockRowLocked = "SELECT owner, until FROM ngc_lock WHERE name = ? FOR UPDATE"
	lockInsert    = "INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)"
	lockFree      = "DELETE FROM ngc_lock WHERE name = ? AND owner = ?"
)

// The two answers of the second bolt of LockTake. errLockHeld is no mistake:
// it means "somebody else holds it", which the method reports as false, and
// it is an error only so that the transaction rolls the UPDATE back. Without
// that the row would carry this owner and a deadline nobody is using, and on
// a column whose comparison refuses everything it would carry them for good.
// errLockBent is a mistake and is reported: a deadline that is no number
// means row or column are not what the migrations write, and a lock nobody
// can judge is worse than a lock nobody gets.
var (
	errLockHeld = errors.New("sqlstore: the lock is held by another owner")
	errLockBent = errors.New("sqlstore: the deadline of the lock is not a number")
)

// freeForTake judges in Go what lockTake judges in SQL: whether the row that
// is there lets owner take the lock at now - its own row always, a foreign
// one only once its deadline has passed.
//
// prevOwner and prevUntil are the row as the database handed it over, bytes
// and not a number, because what the column really holds is what has to be
// judged: a text where a number belongs is the very case this bolt exists
// for, and a Scan into an int64 would hand that case to the driver instead of
// to this function.
//
// It knows the same difference lockTake knows, and that is not a nicety.
// dbapi.Store promises the same owner may take the lock again and move the
// deadline, rollout.renewLease rests on that, and shared/dbapi/dbtest
// measures it. A bolt that read "the deadline has passed, so the lock is
// lost" would break the renewal of exactly the round that took longer than
// its lease.
func freeForTake(prevOwner, prevUntil []byte, owner string, now int64) (bool, error) {
	if string(prevOwner) == owner {
		return true, nil
	}
	until, err := strconv.ParseInt(string(prevUntil), 10, 64)
	if err != nil {
		return false, errLockBent
	}
	return until <= now, nil
}

// OnceCreate is one INSERT, and that is what makes it atomic: the primary key
// decides. Of concurrent calls for the same kind and value exactly one gets
// the row, and a loser sees a broken key and gives ErrUsed; on Galera a loser
// may see a certification conflict first, repeat the whole transaction (see
// write), and only then find the row.
//
// The bounds stand in front of the work, as everywhere in this store: MariaDB
// refuses a value its column cannot hold and SQLite takes it, so the bound has
// to be here for both to answer the same. data is a {{key}} column like kind
// and token, see sql/0001-core.sql.
func (s *store) OnceCreate(ctx context.Context, kind string, value []byte, data string, expires int64) error {
	if kind == "" || len(value) == 0 || !okKey(kind) || len(value) > MaxKey || !okKey(data) {
		return dbapi.ErrInvalid
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		// blob is what keeps a nil slice out of the driver; here the length
		// above already does, and the call is what the guard in
		// sqlcheck_test.go reads - it follows the shape, not the reasoning.
		_, err := tx.ExecContext(ctx, onceInsert, []byte(kind), blob(value), []byte(data), expires, int64(0))
		if err != nil && s.d.Duplicate(err) {
			return dbapi.ErrUsed
		}
		return err
	})
}

// OnceUse marks the value as used and gives back its data. The UPDATE does
// the deciding, so no SELECT ... FOR UPDATE is needed, which SQLite has not
// got anyway. It changes only a row that is neither used nor expired, so a
// second use matches nothing and then reads why.
//
// That the write comes first is held by TestOnceUseDecidesWithItsWrite: it
// reads this package and refuses a OnceUse that asks the database about the
// value before it writes the mark. It finds this method by its name, and two
// things keep that name here: dbapi.Store asks for it, so a rename does not
// build, and the test says so itself when it finds no OnceUse at all.
func (s *store) OnceUse(ctx context.Context, kind string, value []byte, now int64) (string, error) {
	// A key the columns cannot hold is one OnceCreate refused to write, so it
	// is unknown, and that answer is given here instead of in the database -
	// the same bound in front of the same work as in BlobGet.
	if !okKey(kind) || len(value) > MaxKey {
		return "", dbapi.ErrNotFound
	}
	var data string
	err := s.write(ctx, func(tx *sql.Tx) error {
		data = "" // write may run this again after a conflict
		res, err := tx.ExecContext(ctx, onceUse, int64(1), []byte(kind), blob(value), int64(0), now)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			return tx.QueryRowContext(ctx, onceRead, []byte(kind), blob(value)).Scan(&data)
		}
		// Why it matched nothing. The used column is not read: the UPDATE
		// above asked for a row that is unused and in time, so a row it did
		// not match and that is still in time carries the mark already,
		// whatever number stands in it.
		//
		// Which of the two answers an expired and spent value gets is not in
		// the contract; expired comes first here because memstore, the
		// reference beside this store, answers it that way, and two stores of
		// one contract have to agree.
		var expires int64
		switch err := tx.QueryRowContext(ctx, onceState, []byte(kind), blob(value)).Scan(&expires); {
		case errors.Is(err, sql.ErrNoRows):
			return dbapi.ErrNotFound // nobody ever created it, or a purge freed it
		case err != nil:
			return err
		case expires <= now:
			return dbapi.ErrNotFound // it is there and its time has passed
		default:
			return dbapi.ErrUsed
		}
	})
	if err != nil {
		return "", err
	}
	return data, nil
}

// OncePurge removes every value that expired before or at now.
func (s *store) OncePurge(ctx context.Context, now int64) error {
	_, err := s.exec(ctx, oncePurge, now)
	return err
}

// LockTake takes the lock unless somebody else holds it and their time has
// not passed. The UPDATE decides in one step - it changes the row only while
// the lock is free or already ours - so two owners can never both win. When
// it matches nothing the row may simply not be there yet; the INSERT tells
// the two apart, and a broken key means the row is there after all and
// somebody else holds it.
//
// The deadline is judged a SECOND time, in Go, over the row the UPDATE
// overwrote (freeForTake). The UPDATE is untouched by that: it still decides
// alone who writes, and the bolt only decides whether that write is kept.
// Why it is there although the SQL already says it: every database compares
// differently, and a comparison that is not numeric does not fail, it says
// the opposite. Measured on 03.11.2026 against SQLite and MariaDB 11.8.6
// (docs/schloss-ablauf-riegel.md): with the "until <= ?" of today a bent
// column always refuses too MUCH, so this bolt cannot fire on it - it is
// aimed at the line that reads more naturally and would be written some day,
// "until > ?" for "held". With that line and without this bolt a second
// owner gets the lock on both databases, at a perfectly correct column; with
// it, he does not.
//
// That the same owner may take it again with the very same time rests on the
// driver counting matched rows and not changed ones, the same as put. On
// MariaDB that is the ClientFoundRows flag of the DSN; no MariaDB connection
// is opened anywhere in this repository yet, so nothing sets it today, and
// whoever writes that DSN has to. Without it this method gives false where it
// should give true, and SQLite never shows it: it counts every row its UPDATE
// went through.
func (s *store) LockTake(ctx context.Context, name, owner string, now, until int64) (bool, error) {
	if name == "" || owner == "" || until <= now || !okKey(name) || !okText(owner) {
		return false, dbapi.ErrInvalid
	}
	took := false
	err := s.write(ctx, func(tx *sql.Tx) error {
		took = false
		// The row the UPDATE is about to overwrite, read in the same
		// transaction, so that what Go judges is what the UPDATE acted on.
		// It is read BEFORE the UPDATE because afterwards it is gone, and
		// judged AFTER it, because only a hit has to be justified.
		//
		// That a neighbour may write between the two cannot turn a refusal
		// into a take, and that rests on one property of the deadlines: no
		// writer of ours ever lowers one. LockTake refuses an until at or
		// below now and the migration runner writes now+lockFor, so while
		// now stands still a row of ours cannot go from "held" to "expired".
		// A neighbour can therefore only make the UPDATE match LESS, and a
		// row it changed under us ends in n == 0, where this bolt is not
		// asked at all. A deadline lowered from outside - a row changed by
		// hand - can make this bolt refuse a take the UPDATE allowed, which
		// is one lock not taken and the safe of the two mistakes.
		var prevOwner, prevUntil []byte
		var err error
		hadRow := true
		if s.d.RowLock() {
			err = tx.QueryRowContext(ctx, lockRowLocked, []byte(name)).Scan(&prevOwner, &prevUntil)
		} else {
			err = tx.QueryRowContext(ctx, lockRow, []byte(name)).Scan(&prevOwner, &prevUntil)
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			hadRow = false
		case err != nil:
			return err
		}
		res, err := tx.ExecContext(ctx, lockTake, []byte(owner), until, []byte(name), []byte(owner), now)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			// The second bolt. The UPDATE has already written, so a refusal
			// here has to take the write with it: the error rolls the
			// transaction back, and the row stands as it stood.
			if hadRow {
				free, err := freeForTake(prevOwner, prevUntil, owner, now)
				if err != nil {
					return err
				}
				if !free {
					return errLockHeld
				}
			}
			took = true
			return nil
		}
		if _, err := tx.ExecContext(ctx, lockInsert, []byte(name), []byte(owner), until); err != nil {
			if s.d.Duplicate(err) {
				return nil // the row is there and somebody else holds it
			}
			return err
		}
		took = true
		return nil
	})
	if errors.Is(err, errLockHeld) {
		// The one error that is an answer: the bolt refused, the UPDATE is
		// rolled back, and the caller hears what he would have heard from a
		// database whose comparison had held.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return took, nil
}

// LockFree gives the lock back; another owner is ignored, because the DELETE
// names both. A name or owner the columns cannot hold names no row of ours,
// and giving back a lock that is not there is no error - the same answer
// BlobDelete gives, and again without asking the database.
func (s *store) LockFree(ctx context.Context, name, owner string) error {
	if !okKey(name) || !okText(owner) {
		return nil
	}
	_, err := s.exec(ctx, lockFree, []byte(name), []byte(owner))
	return err
}
