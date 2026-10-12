// Package valueslice is good code that has to stay silent: the values in a
// variable of their own, handed on through a second helper, and put together
// by a function that gives a row back.
package valueslice

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

// Rewrite writes the row of values into a variable of its own first. For a
// row of ten columns - Session and User in the tasks to come - that is the
// near form, and writing every value out at every call site instead would be
// the worse code.
func (s *store) Rewrite(ctx any, kind, id string, seq int64, data []byte) error {
	vals := []any{seq, blob(data), []byte(kind), []byte(id)}
	return s.exec(ctx, blobUpdate, vals...)
}

// Erase goes through a second level of helper, and the values come out of a
// function that puts the row together.
func (s *store) Erase(ctx any, kind, id string) error {
	return s.write(ctx, blobDelete, rowOf(kind, id))
}

func (s *store) write(ctx any, stmt string, vals []any) error {
	return s.exec(ctx, stmt, vals...)
}

func rowOf(kind, id string) []any {
	return []any{[]byte(kind), []byte(id)}
}
