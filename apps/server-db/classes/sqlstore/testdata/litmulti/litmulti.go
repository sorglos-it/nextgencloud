// Package litmulti is clean beside it and wrong: its OnceUse asks the
// database about the value before it writes the mark. The read stands in a
// literal given the name read, and read is called in front of the write. A
// token without a value is looked up by its kind alone, and only the data is
// read there - the other arm reads the mark and the time as well.
//
// The guard says nothing about it, and that is a gap, not a verdict. litOf
// follows a name to its literal only where the name was written in exactly
// one place; here it is written in two, litOf gives up because it cannot say
// which of the two bodies runs, nobody else looks into either, and the read
// comes through without a word. Written in one place this same read is
// caught, which is what twostep holds.
//
// The gap is older than the change of 134470a: it was silent before that
// commit and after it, so that change neither made it nor mended it.
//
// Its line in the table says what the guard does, not that this store is
// right. The line goes red on the day the guard follows a name written more
// than once, and then somebody reads here why it was let through, instead of
// taking the new red for a mistake.
package litmulti

type result struct{}

type row struct{}

type conn struct{}

func (conn) ExecContext(ctx any, stmt string, args ...any) (result, error) {
	return result{}, nil
}

func (conn) QueryContext(ctx any, stmt string, args ...any) (*row, error) { return nil, nil }

func (conn) QueryRowContext(ctx any, stmt string, args ...any) *row { return nil }

func (result) RowsAffected() (int64, error) { return 0, nil }

func (r *row) Scan(dest ...any) error { return nil }

// failure is an error without an import.
type failure string

func (f failure) Error() string { return string(f) }

const (
	onceInsert = "INSERT INTO ngc_once (kind, token, data, expires, used) VALUES (?, ?, ?, ?, ?)"
	onceUse    = "UPDATE ngc_once SET used = ? WHERE kind = ? AND token = ? AND used = ? AND expires > ?"
	onceRead   = "SELECT data FROM ngc_once WHERE kind = ? AND token = ?"
	onceCheck  = "SELECT used, expires, data FROM ngc_once WHERE kind = ? AND token = ?"
	onceMark   = "UPDATE ngc_once SET used = ? WHERE kind = ? AND token = ?"
	onceCount  = "SELECT count(*) FROM ngc_once WHERE kind = ?"
	oncePurge  = "DELETE FROM ngc_once WHERE expires <= ?"

	lockFree  = "DELETE FROM ngc_lock WHERE name = ? AND owner = ?"
	lockList  = "SELECT name, owner FROM ngc_lock WHERE until > ?"
	lockOwner = "SELECT owner FROM ngc_lock WHERE name = ?"
)

type store struct {
	db conn
}

func blob(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

func (s *store) exec(ctx any, stmt string, args ...any) error {
	_, err := s.db.ExecContext(ctx, stmt, args...)
	return err
}

func (s *store) OnceCreate(ctx any, kind string, value []byte, data string, expires int64) error {
	return s.exec(ctx, onceInsert, []byte(kind), blob(value), []byte(data), expires, int64(0))
}

func (s *store) OncePurge(ctx any, now int64) error { return s.exec(ctx, oncePurge, now) }

func (s *store) OnceCount(ctx any, kind string) *row {
	return s.db.QueryRowContext(ctx, onceCount, []byte(kind))
}

func (s *store) LockFree(ctx any, name, owner string) error {
	return s.exec(ctx, lockFree, []byte(name), []byte(owner))
}

func (s *store) LockList(ctx any, now int64) (*row, error) {
	return s.db.QueryContext(ctx, lockList, now)
}

func (s *store) LockOwner(ctx any, name string) *row {
	return s.db.QueryRowContext(ctx, lockOwner, []byte(name))
}

// OnceUse is the hole: read is written in two places, one in each arm of the
// if, so the guard cannot say which body runs and reads neither. Both of them
// read, and both run before the write.
func (s *store) OnceUse(ctx any, kind string, value []byte, now int64) (string, error) {
	var used, expires int64
	var data string
	var read func()
	if len(value) == 0 {
		read = func() {
			s.db.QueryRowContext(ctx, onceRead, []byte(kind), blob(value)).Scan(&data)
		}
	} else {
		read = func() {
			s.db.QueryRowContext(ctx, onceCheck, []byte(kind), blob(value)).Scan(&used, &expires, &data)
		}
	}
	read()
	if used != 0 || expires <= now {
		return "", failure("the value is spent or past its time")
	}
	if err := s.exec(ctx, onceMark, int64(1), []byte(kind), blob(value)); err != nil {
		return "", err
	}
	return data, nil
}
