// Package defertimer is clean beside it and right: its OnceUse decides with
// the write, and it carries the shape that the rule reads most closely -
// defer s.timer("…")() names its function with a call, so that call is
// settled where the defer stands and is read there. It touches no database,
// and nothing else in this OnceUse speaks before the UPDATE.
//
// It is here because reading the function expression of a defer is the newest
// thing the rule does, and a guard that is red on this everyday shape would be
// switched off - and then it guards nothing.
package defertimer

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

func (s *store) timer(what string) func() { return func() {} }

// OnceUse decides with its write; the timer around it says nothing to the
// database.
func (s *store) OnceUse(ctx any, kind string, value []byte, now int64) (string, error) {
	defer s.timer("OnceUse")()
	r, err := s.db.ExecContext(ctx, onceUse, int64(1), []byte(kind), blob(value), int64(0), now)
	if err != nil {
		return "", err
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return "", failure("the value is spent or past its time")
	}
	var data string
	err = s.db.QueryRowContext(ctx, onceRead, []byte(kind), blob(value)).Scan(&data)
	return data, err
}
