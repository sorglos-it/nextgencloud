package sqlstore

import (
	"context"
	"database/sql"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// The six reading statements are constants of their own instead of one with
// switches inside: each one can use an index, and none is put together.
// Order is always (ts, id) descending. The id column is binary, so the
// database compares the IDs byte for byte, and because an ID is 32 lower-case
// hex characters that is the order Go gives them as well - what core.LogEntry
// says the hex is for, so that no collation of the five databases reorders a
// page.
//
// Six, counted against the constants below - logAll, logBefore, logTime,
// logUser, logUserBefore, logUserTime, with logCols their shared head and
// logInsert the write. The plan of this task wrote "four" over the same six,
// and a count in a comment is read by whoever comes after and not recounted.
const (
	logInsert = "INSERT INTO ngc_log (id, ts, user_id, kind, app, action, node, result_code, detail) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
	logCols       = "SELECT id, ts, user_id, kind, app, action, node, result_code, detail FROM ngc_log "
	logAll        = logCols + "ORDER BY ts DESC, id DESC LIMIT ?"
	logBefore     = logCols + "WHERE ts < ? OR (ts = ? AND id < ?) ORDER BY ts DESC, id DESC LIMIT ?"
	logTime       = logCols + "WHERE ts < ? ORDER BY ts DESC, id DESC LIMIT ?"
	logUser       = logCols + "WHERE user_id = ? ORDER BY ts DESC, id DESC LIMIT ?"
	logUserBefore = logCols + "WHERE user_id = ? AND (ts < ? OR (ts = ? AND id < ?)) " +
		"ORDER BY ts DESC, id DESC LIMIT ?"
	logUserTime = logCols + "WHERE user_id = ? AND ts < ? ORDER BY ts DESC, id DESC LIMIT ?"
)

// LogAdd keeps one entry. The ID comes from the API and is the primary key,
// so the same ID twice is ErrUsed and nothing is written twice. That one
// translation covers the whole statement because ngc_log carries no unique
// index besides that key - ngc_log_ts and ngc_log_user are plain ones - so a
// duplicate here can only be the ID, and unlike UserPut this method has no
// second index its answer would have to tell apart.
//
// It has the shape OnceCreate has, and with it one window, read off the source
// and not measured: write repeats the whole transaction when the database says
// it was rolled back, and that includes a Commit that comes back retryable. If
// such a Commit had in fact landed, the INSERT of the next round meets its own
// primary key and this method answers ErrUsed for a row it wrote itself. The
// comment at write describes the window; how reachable it is on either database
// is unmeasured, and nothing of this task made it wider.
func (s *store) LogAdd(ctx context.Context, e core.LogEntry) error {
	if !core.ValidLogID(e.ID) || e.Kind == "" || e.Time <= 0 || !okKey(e.User) || !okKey(e.Kind) ||
		!okKey(e.App) || !okKey(e.Action) || !okKey(e.Node) || !okKey(e.Result) || !okText(e.Detail) {
		return dbapi.ErrInvalid
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, logInsert, []byte(e.ID), e.Time, []byte(e.User), []byte(e.Kind),
			[]byte(e.App), []byte(e.Action), []byte(e.Node), []byte(e.Result), []byte(e.Detail))
		if err != nil && s.d.Duplicate(err) {
			return dbapi.ErrUsed
		}
		return err
	})
}

// LogQuery returns a page, newest first. The cursor is (BeforeTime,
// BeforeID): with an ID the page starts inside that second, without one the
// whole second is left out, and without a time it starts at the newest entry.
//
// A Limit above MaxPage is refused and not quietly cut short. Cutting it would
// be the worse answer of the two: a caller who asks for 1000 and is handed 500
// cannot tell that from "there are only 500", and would stop paging with rows
// still in the table. Refusing says what happened, and the cursor above is the
// way to the rest.
//
// The line is drawn twice on purpose, and neither copy is spare. The contract
// draws it at the API border - core.LogQuery.Validate refuses a Limit above
// 500, which is where a number from outside is stopped - and this store draws
// it again for the callers that never pass that border: everything inside NGC
// that holds a dbapi.Store and builds a core.LogQuery itself. What the number
// reaches here is "LIMIT ?" and, on the way back, one slice holding the whole
// answer; a layer that takes its caller's word for that size is one mistake
// away from a memory problem. So whoever finds the two some day: the one in
// core guards the outside, this one guards us, and deleting either leaves a
// hole the other does not cover.
//
// Which of the six statements runs is decided here, and each arm hands its
// own one to logPage with the values that belong to it. The plan of this task
// put the chosen statement and its values into two variables first and gave
// those to the driver; that shape is refused by the guard in sqlcheck_test.go
// - measured, "the statement is the variable stmt, and that is no constant"
// and "the guard cannot see which values stand in args: it is written in 6
// places" - because a name written in six places names no one statement that
// can be read off the file. Handing both to a helper that never writes to
// them is the shape put already has, and the guard follows a parameter back
// to every call site.
func (s *store) LogQuery(ctx context.Context, q core.LogQuery) ([]core.LogEntry, error) {
	if q.Limit < 1 || q.Limit > MaxPage || q.BeforeTime < 0 ||
		(q.BeforeID != "" && (!core.ValidLogID(q.BeforeID) || q.BeforeTime == 0)) || !okKey(q.User) {
		return nil, dbapi.ErrInvalid
	}
	switch {
	case q.User != "" && q.BeforeID != "":
		return s.logPage(ctx, logUserBefore,
			[]any{[]byte(q.User), q.BeforeTime, q.BeforeTime, []byte(q.BeforeID), int64(q.Limit)})
	case q.User != "" && q.BeforeTime != 0:
		return s.logPage(ctx, logUserTime, []any{[]byte(q.User), q.BeforeTime, int64(q.Limit)})
	case q.User != "":
		return s.logPage(ctx, logUser, []any{[]byte(q.User), int64(q.Limit)})
	case q.BeforeID != "":
		return s.logPage(ctx, logBefore, []any{q.BeforeTime, q.BeforeTime, []byte(q.BeforeID), int64(q.Limit)})
	case q.BeforeTime != 0:
		return s.logPage(ctx, logTime, []any{q.BeforeTime, int64(q.Limit)})
	}
	return s.logPage(ctx, logAll, []any{int64(q.Limit)})
}

// logPage runs one of the reading statements and builds the page. It writes
// to neither of its two parameters, so both lead back to the arm of LogQuery
// that handed them over.
func (s *store) logPage(ctx context.Context, stmt string, args []any) ([]core.LogEntry, error) {
	var out []core.LogEntry
	err := s.query(ctx, func() error {
		out = nil
		rows, err := s.rdb.QueryContext(ctx, stmt, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e core.LogEntry
			if err := rows.Scan(&e.ID, &e.Time, &e.User, &e.Kind, &e.App, &e.Action, &e.Node,
				&e.Result, &e.Detail); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
