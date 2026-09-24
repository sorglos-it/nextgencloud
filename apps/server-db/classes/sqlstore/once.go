package sqlstore

import (
	"context"
	"database/sql"
	"errors"

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

	lockTake   = "UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ? AND (owner = ? OR until <= ?)"
	lockInsert = "INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)"
	lockFree   = "DELETE FROM ngc_lock WHERE name = ? AND owner = ?"
)

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
// value before it writes the mark. It finds this method by its name, and
// nothing but dbapi.Store keeps that name here.
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
		res, err := tx.ExecContext(ctx, lockTake, []byte(owner), until, []byte(name), []byte(owner), now)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
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
