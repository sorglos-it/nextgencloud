// Package literal is the second way the guard before this one was measured
// through: the database call sits in a function literal at a package var, and
// a guard that reads only function declarations never comes past it.
package literal

type result struct{}

type row struct{}

type conn struct{}

func (conn) ExecContext(ctx any, stmt string, args ...any) (result, error) {
	return result{}, nil
}

func (conn) QueryContext(ctx any, stmt string, args ...any) (*row, error) { return nil, nil }

func (conn) QueryRowContext(ctx any, stmt string, args ...any) *row { return nil }

const (
	blobSelect = "SELECT seq, data FROM ngc_blob WHERE kind = ? AND id = ?"
	blobUpdate = "UPDATE ngc_blob SET seq = ?, data = ? WHERE kind = ? AND id = ?"
	blobInsert = "INSERT INTO ngc_blob (kind, id, seq, data) VALUES (?, ?, ?, ?)"
	blobFrom   = "SELECT id, seq FROM ngc_blob WHERE kind = ? AND seq >= ?"
	blobDelete = "DELETE FROM ngc_blob WHERE kind = ? AND id = ?"
	blobCount  = "SELECT count(*) FROM ngc_blob WHERE kind = ?"
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

func (s *store) put(ctx any, update string, updateArgs []any, insert string, insertArgs []any) error {
	if _, err := s.db.ExecContext(ctx, update, updateArgs...); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, insert, insertArgs...)
	return err
}

func (s *store) Put(ctx any, kind, id string, seq int64, data []byte) error {
	return s.put(ctx,
		blobUpdate, []any{seq, blob(data), []byte(kind), []byte(id)},
		blobInsert, []any{[]byte(kind), []byte(id), seq, blob(data)})
}

func (s *store) Get(ctx any, kind, id string) *row {
	return s.db.QueryRowContext(ctx, blobSelect, []byte(kind), []byte(id))
}

func (s *store) Count(ctx any, kind string) *row {
	return s.db.QueryRowContext(ctx, blobCount, []byte(kind))
}

func (s *store) From(ctx any, kind string, seq int64) (*row, error) {
	return s.db.QueryContext(ctx, blobFrom, []byte(kind), seq)
}

func (s *store) Delete(ctx any, kind, id string) error {
	return s.exec(ctx, blobDelete, []byte(kind), []byte(id))
}

// Erase is a function literal at a package var. A guard that walks only the
// bodies of function declarations never reads it, and the statement below is
// put together at run time and goes straight to the driver.
var Erase = func(ctx any, s *store, kind string) error {
	stmt := "DELETE FROM ngc_blob WHERE kind = '" + kind + "'"
	_, err := s.db.ExecContext(ctx, stmt)
	return err
}
