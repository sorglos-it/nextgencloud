// Package prepared is good code that has to stay silent: a statement made
// once and run many times. A guard that goes red here would be switched off,
// and then it guards nothing.
package prepared

type result struct{}

type row struct{}

type conn struct{}

func (conn) ExecContext(ctx any, stmt string, args ...any) (result, error) {
	return result{}, nil
}

func (conn) QueryContext(ctx any, stmt string, args ...any) (*row, error) { return nil, nil }

func (conn) QueryRowContext(ctx any, stmt string, args ...any) *row { return nil }

// ready stands for sql.Stmt: its Exec and ExecContext carry no statement.
type ready struct{}

func (ready) ExecContext(ctx any, args ...any) (result, error) { return result{}, nil }

func (conn) PrepareContext(ctx any, stmt string) (ready, error) { return ready{}, nil }

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

// PutMany is the textbook form for a row written many times: the statement is
// made once, and the calls after it carry values only. sql.Stmt has Exec and
// ExecContext without a statement of their own, so a guard that always takes
// the first argument for the statement would call the first value a statement
// built at run time. This package has to stay silent.
func (s *store) PutMany(ctx any, kind string, ids []string, data []byte) error {
	st, err := s.db.PrepareContext(ctx, blobInsert)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := st.ExecContext(ctx, []byte(kind), []byte(id), int64(0), blob(data)); err != nil {
			return err
		}
	}
	return nil
}
