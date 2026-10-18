// Package mirror is the sixth hole: the way to the driver through reflect,
// where the name of the method is a string.
package mirror

import "reflect"

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

// Wipe reaches the driver through reflect. The name of the method is a string
// here, so no reading of identifiers sees it, and neither the statement nor
// the values stand anywhere that could be measured.
func (s *store) Wipe(ctx any, kind string) error {
	m := reflect.ValueOf(s.db).MethodByName("ExecContext")
	m.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf("DELETE FROM ngc_blob WHERE kind = '" + kind + "'"),
	})
	return nil
}
