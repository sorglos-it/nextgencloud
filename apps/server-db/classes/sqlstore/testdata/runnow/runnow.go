// Package runnow is clean beside it and wrong: its OnceUse asks the database
// about the value before it writes the mark. The read stands in a literal
// handed to runNow, runNow calls that literal where it stands and gives back
// a function that does nothing - so the read really is the first statement
// this method gives the database, and what the defer calls at the end does
// nothing at all.
//
// The guard says nothing about it, and that is a gap, not a verdict. It is
// the price of deferwrap being silent: without reading runNow the guard
// cannot tell it from wrap, which only hands the literal back for the defer
// to call. Both shapes were refused until 134470a and both turned silent in
// that one commit, so closing the false alarm on the right store paid for it
// with this hole here.
//
// Its line in the table says what the guard does, not that this store is
// right. The line goes red on the day the guard catches this shape again,
// and then somebody reads here why it was let through, instead of taking the
// new red for a mistake.
package runnow

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

// runNow calls the function it is handed where it stands, and hands back one
// that does nothing. It is the mirror of wrap in deferwrap, and from outside
// the two are the same shape.
func runNow(f func()) func() { f(); return func() {} }

// OnceUse is the hole: runNow works the read out at the defer, so data is
// filled before the UPDATE decides anything, and two callers reading the same
// unused row both get the value.
func (s *store) OnceUse(ctx any, kind string, value []byte, now int64) (data string, err error) {
	defer runNow(func() {
		s.db.QueryRowContext(ctx, onceRead, []byte(kind), blob(value)).Scan(&data)
	})()
	r, err := s.db.ExecContext(ctx, onceUse, int64(1), []byte(kind), blob(value), int64(0), now)
	if err != nil {
		return "", err
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return "", failure("the value is spent or past its time")
	}
	return data, nil
}
