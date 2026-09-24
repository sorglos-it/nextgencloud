// Package sentence is clean beside it, and it is right: its OnceUse decides
// with the write, which is what the rule on that method asks. What it carries
// besides is everything that reads like a statement in front of the write and
// is none - an ordinary message that opens with the word select, one of those
// in a helper of its own, a reading statement named early and handed over
// late, that same one standing first in a parameter list and running second,
// and the read itself in a defer.
//
// Nothing here has a hole, and that is the point: a guard that is red on any
// of this is red on good code, and a guard that is red on good code is
// switched off - and then it guards nothing at all.
package sentence

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
	onceCount  = "SELECT count(*) FROM ngc_once WHERE kind = ?"
	oncePurge  = "DELETE FROM ngc_once WHERE expires <= ?"
	lockFree   = "DELETE FROM ngc_lock WHERE name = ? AND owner = ?"
	lockList   = "SELECT name, owner FROM ngc_lock WHERE until > ?"
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

// okKind carries a sentence that opens the way a statement does, in a file of
// its own and far from any database - the same place limits.go stands in.
func okKind(kind string) bool {
	const why = "select a kind from the fixed list, or the values are not found again"
	if kind == "" {
		return false
	}
	_ = why
	return true
}

func (s *store) exec(ctx any, stmt string, args ...any) error {
	_, err := s.db.ExecContext(ctx, stmt, args...)
	return err
}

// markUsed is given both statements, the reading one first in the list, and
// hands them to the database the other way round: the write decides, and the
// read runs in a defer, so it runs after it and only when a row really
// changed.
func (s *store) markUsed(ctx any, read, write string, kind string, value []byte, writeArgs []any) (data string, took bool, err error) {
	defer func() {
		if err != nil || !took {
			return
		}
		err = s.db.QueryRowContext(ctx, read, []byte(kind), blob(value)).Scan(&data)
	}()
	res, err := s.db.ExecContext(ctx, write, writeArgs...)
	if err != nil {
		return "", false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", false, err
	}
	return "", n > 0, nil
}

func (s *store) OnceCreate(ctx any, kind string, value []byte, data string, expires int64) error {
	return s.exec(ctx, onceInsert, []byte(kind), blob(value), []byte(data), expires, int64(0))
}

// OnceUse decides with its write: one UPDATE that matches only a row still
// unused and still in time, and the count of rows it changed says whether
// this caller is the one that spent it.
func (s *store) OnceUse(ctx any, kind string, value []byte, now int64) (string, error) {
	const spent = "select a value that is still in time, because this one is not"
	const back = onceRead // named here, handed to the database below
	if !okKind(kind) {
		return "", failure(spent)
	}
	data, took, err := s.markUsed(ctx, back, onceUse, kind, value,
		[]any{int64(1), []byte(kind), blob(value), int64(0), now})
	if err != nil {
		return "", err
	}
	if !took {
		return "", failure(spent)
	}
	return data, nil
}

func (s *store) OncePurge(ctx any, now int64) error {
	return s.exec(ctx, oncePurge, now)
}

func (s *store) OnceCount(ctx any, kind string) *row {
	return s.db.QueryRowContext(ctx, onceCount, []byte(kind))
}

func (s *store) LockFree(ctx any, name, owner string) error {
	return s.exec(ctx, lockFree, []byte(name), []byte(owner))
}

func (s *store) LockList(ctx any, now int64) (*row, error) {
	return s.db.QueryContext(ctx, lockList, now)
}
