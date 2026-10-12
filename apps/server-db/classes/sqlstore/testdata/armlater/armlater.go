// Package armlater is clean beside it and right: its OnceUse decides with
// the write. The literal that reads the row is handed to add before the
// UPDATE, and add only puts it away - the collected functions run from run,
// after the write has decided.
//
// The guard refuses it all the same, and the refusal is wrong. Outside a
// defer or a go a literal handed to a call as an argument is read as if it
// ran at that call; whether the called function runs it is its own business
// and is not read here. So the guard names this read as the first statement
// the method gives the database, and says the store is wrong when it is not.
//
// The false alarm is older than the change of 134470a: it was there before
// that commit and after it, so that change neither made it nor mended it.
// Only the held-back rebuild of argLits - one walk instead of two - can mend
// it. Until then it stands here, awake and measured, and litarg is the hole
// that rests on the same strict direction: give the direction up and litarg
// goes through.
//
// Its line in the table says what the guard does, not that this store is
// wrong. The line goes green on the day that rebuild lands, and then
// somebody reads here that this store was right the whole time.
package armlater

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

// later collects functions and keeps them until run is called.
type later struct {
	fns []func()
}

// add puts the function away. It does not call it.
func (l *later) add(f func()) { l.fns = append(l.fns, f) }

// run calls what was put away, in the order it came in.
func (l *later) run() {
	for _, f := range l.fns {
		f()
	}
}

// OnceUse decides with its write. The read is handed over before the UPDATE
// and runs after it, from run.
func (s *store) OnceUse(ctx any, kind string, value []byte, now int64) (data string, err error) {
	var l later
	l.add(func() {
		s.db.QueryRowContext(ctx, onceRead, []byte(kind), blob(value)).Scan(&data)
	})
	r, err := s.db.ExecContext(ctx, onceUse, int64(1), []byte(kind), blob(value), int64(0), now)
	if err != nil {
		return "", err
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return "", failure("the value is spent or past its time")
	}
	l.run()
	return data, nil
}
