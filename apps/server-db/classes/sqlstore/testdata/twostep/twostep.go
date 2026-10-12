// Package twostep is clean beside it with one hole cut in: its OnceUse asks
// the database what the state of the value is, decides in Go, and only then
// writes the mark. Two callers read the same unused row and both hand the
// one-time value out - a login taken twice, a task run twice. Every statement
// is a constant and every value comes from blob, so the four other rules of
// the guard have nothing to say here; the hole is the order alone.
//
// It is the shape that runs without a transaction. The one inside a
// transaction sits in the same hole, and the note beside the guard says why
// no run of this repository can tell that one apart.
package twostep

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

const (
	onceInsert = "INSERT INTO ngc_once (kind, token, data, expires, used) VALUES (?, ?, ?, ?, ?)"
	onceCheck  = "SELECT used, expires, data FROM ngc_once WHERE kind = ? AND token = ?"
	onceMark   = "UPDATE ngc_once SET used = ? WHERE kind = ? AND token = ?"
	oncePurge  = "DELETE FROM ngc_once WHERE expires <= ?"

	lockTake   = "UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ? AND (owner = ? OR until <= ?)"
	lockInsert = "INSERT INTO ngc_lock (name, owner, until) VALUES (?, ?, ?)"
	lockFree   = "DELETE FROM ngc_lock WHERE name = ? AND owner = ?"
	lockList   = "SELECT name, owner FROM ngc_lock WHERE until > ?"
	onceCount  = "SELECT count(*) FROM ngc_once WHERE kind = ?"
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

// OnceUse is the hole: it reads the row, decides here, and writes after.
func (s *store) OnceUse(ctx any, kind string, value []byte, now int64) (string, error) {
	var used, expires int64
	var data string
	if err := s.db.QueryRowContext(ctx, onceCheck, []byte(kind), blob(value)).Scan(&used, &expires, &data); err != nil {
		return "", err
	}
	if expires <= now || used != 0 {
		return "", nil
	}
	if err := s.exec(ctx, onceMark, int64(1), []byte(kind), blob(value)); err != nil {
		return "", err
	}
	return data, nil
}

func (s *store) OncePurge(ctx any, now int64) error {
	return s.exec(ctx, oncePurge, now)
}

func (s *store) LockTake(ctx any, name, owner string, now, until int64) (bool, error) {
	r, err := s.db.ExecContext(ctx, lockTake, []byte(owner), until, []byte(name), []byte(owner), now)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if _, err := s.db.ExecContext(ctx, lockInsert, []byte(name), []byte(owner), until); err != nil {
		return false, err
	}
	return true, nil
}

func (s *store) LockFree(ctx any, name, owner string) error {
	return s.exec(ctx, lockFree, []byte(name), []byte(owner))
}

func (s *store) OnceCount(ctx any, kind string) *row {
	return s.db.QueryRowContext(ctx, onceCount, []byte(kind))
}

func (s *store) LockList(ctx any, now int64) (*row, error) {
	return s.db.QueryContext(ctx, lockList, now)
}
