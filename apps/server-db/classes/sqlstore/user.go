package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

const (
	userByID   = "SELECT id, name, roles, totp, locked, failures, last_login FROM ngc_user WHERE id = ?"
	userByName = "SELECT id, name, roles, totp, locked, failures, last_login FROM ngc_user WHERE name = ?"
	userIDOf   = "SELECT id FROM ngc_user WHERE name = ?"
	userUpdate = "UPDATE ngc_user SET name = ?, roles = ?, totp = ?, locked = ?, failures = ?, last_login = ? WHERE id = ?"
	userInsert = "INSERT INTO ngc_user (id, name, roles, totp, locked, failures, last_login) VALUES (?, ?, ?, ?, ?, ?, ?)"
	userList   = "SELECT id, name, roles, totp, locked, failures, last_login FROM ngc_user ORDER BY name"
	userDelete = "DELETE FROM ngc_user WHERE id = ?"

	sessionSelect = "SELECT token, user_id, cert, proof, api_key, api_cert, created, expires, seen FROM ngc_session WHERE token = ?"
	sessionUpdate = "UPDATE ngc_session SET user_id = ?, cert = ?, proof = ?, api_key = ?, api_cert = ?, " +
		"created = ?, expires = ?, seen = ? WHERE token = ?"
	sessionInsert = "INSERT INTO ngc_session (token, user_id, cert, proof, api_key, api_cert, created, expires, seen) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
	sessionTouch  = "UPDATE ngc_session SET seen = ? WHERE token = ?"
	sessionDelete = "DELETE FROM ngc_session WHERE token = ?"
	sessionPurge  = "DELETE FROM ngc_session WHERE expires <= ?"

	pendingUpdate = "UPDATE ngc_pending SET name = ?, since = ? WHERE pubkey = ?"
	pendingInsert = "INSERT INTO ngc_pending (pubkey, name, since) VALUES (?, ?, ?)"
	pendingList   = "SELECT pubkey, name, since FROM ngc_pending ORDER BY since, name"
	pendingDelete = "DELETE FROM ngc_pending WHERE pubkey = ?"
)

// scanUser reads one row into a User. The roles column goes back out through
// readList and not through splitList, the same way appDone reads App.Grants:
// a column of the store's own database is a value from outside like any
// other, and a roles column longer than joinList ever writes would otherwise
// become a list of any size. TestAUserRowNobodyOfOursWrote plants such a row.
//
// UserGet, UserByName and UserList read the same row, so they read it through
// this one function and can never drift apart.
func scanUser(scan func(...any) error) (dbapi.User, error) {
	var u dbapi.User
	var roles string
	if err := scan(&u.ID, &u.Name, &roles, &u.TOTP, &u.Locked, &u.Failures, &u.LastLogin); err != nil {
		return u, err
	}
	list, err := readList(roles)
	u.Roles = list
	return u, err
}

// UserPut writes a user. The name belongs to one ID, and the check for that
// sits in the same transaction as the write, so two APIs cannot both take a
// free name: on Galera one of them loses the certification and repeats, and
// then it sees the name.
func (s *store) UserPut(ctx context.Context, u dbapi.User) error {
	roles, ok := joinList(u.Roles)
	if u.ID == "" || u.Name == "" || !okKey(u.ID) || !okKey(u.Name) || !ok || !okDoc(u.TOTP) {
		return dbapi.ErrInvalid
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		var other string
		switch err := tx.QueryRowContext(ctx, userIDOf, []byte(u.Name)).Scan(&other); {
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		case other != "" && other != u.ID:
			return dbapi.ErrInvalid
		}
		res, err := tx.ExecContext(ctx, userUpdate, []byte(u.Name), []byte(roles), blob(u.TOTP),
			u.Locked, int64(u.Failures), u.LastLogin, []byte(u.ID))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		_, err = tx.ExecContext(ctx, userInsert, []byte(u.ID), []byte(u.Name), []byte(roles),
			blob(u.TOTP), u.Locked, int64(u.Failures), u.LastLogin)
		return err
	})
}

func (s *store) UserGet(ctx context.Context, id string) (dbapi.User, error) {
	return s.oneUser(ctx, userByID, []byte(id))
}

func (s *store) UserByName(ctx context.Context, name string) (dbapi.User, error) {
	return s.oneUser(ctx, userByName, []byte(name))
}

func (s *store) oneUser(ctx context.Context, stmt string, arg []byte) (dbapi.User, error) {
	var out dbapi.User
	err := s.query(ctx, func() error {
		u, err := scanUser(s.db.QueryRowContext(ctx, stmt, blob(arg)).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return dbapi.ErrNotFound
		}
		out = u
		return err
	})
	if err != nil {
		return dbapi.User{}, err
	}
	return out, nil
}

func (s *store) UserList(ctx context.Context) ([]dbapi.User, error) {
	var out []dbapi.User
	err := s.query(ctx, func() error {
		out = nil
		rows, err := s.db.QueryContext(ctx, userList)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u, err := scanUser(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}

func (s *store) UserDelete(ctx context.Context, id string) error {
	_, err := s.exec(ctx, userDelete, []byte(id))
	return err
}

// A value of this file reaches the driver as a conversion of a string where
// it comes from one, and through blob where it is a byte slice already - a
// token, a Noise key, a secret. That is the way OnceUse passes its value: the
// length checks below already keep a nil slice out, and the call is what the
// guard in sqlcheck_test.go reads - it follows the shape, not the reasoning.
//
// Where no length check stands in front of it, in SessionGet, SessionTouch,
// SessionDelete and PendingDelete, blob turns a nil token into an empty one,
// and that changes no answer: SessionPut and PendingPut refuse an empty token
// and an empty key, so no row carries one, and an empty token matches as
// little as NULL did. TestAnEmptyTokenIsNobodysSession measures both halves
// of that - the refusal as well as the answer, because without the refusal
// the empty token would be a key like any other.
func (s *store) SessionPut(ctx context.Context, v dbapi.Session) error {
	if len(v.Token) == 0 || v.User == "" || len(v.Token) > MaxKey || !okKey(v.User) ||
		!okDoc(v.Cert) || !okDoc(v.Proof) || !okDoc(v.APIKey) || !okDoc(v.APICert) {
		return dbapi.ErrInvalid
	}
	return s.put(ctx,
		sessionUpdate, []any{[]byte(v.User), blob(v.Cert), blob(v.Proof), blob(v.APIKey), blob(v.APICert),
			v.Created, v.Expires, v.Seen, blob(v.Token)},
		sessionInsert, []any{blob(v.Token), []byte(v.User), blob(v.Cert), blob(v.Proof), blob(v.APIKey),
			blob(v.APICert), v.Created, v.Expires, v.Seen})
}

func (s *store) SessionGet(ctx context.Context, token []byte) (dbapi.Session, error) {
	var out dbapi.Session
	err := s.query(ctx, func() error {
		err := s.db.QueryRowContext(ctx, sessionSelect, blob(token)).Scan(&out.Token, &out.User, &out.Cert,
			&out.Proof, &out.APIKey, &out.APICert, &out.Created, &out.Expires, &out.Seen)
		if errors.Is(err, sql.ErrNoRows) {
			return dbapi.ErrNotFound
		}
		return err
	})
	if err != nil {
		return dbapi.Session{}, err
	}
	return out, nil
}

func (s *store) SessionTouch(ctx context.Context, token []byte, now int64) error {
	n, err := s.exec(ctx, sessionTouch, now, blob(token))
	if err != nil {
		return err
	}
	if n == 0 {
		return dbapi.ErrNotFound
	}
	return nil
}

func (s *store) SessionDelete(ctx context.Context, token []byte) error {
	_, err := s.exec(ctx, sessionDelete, blob(token))
	return err
}

func (s *store) SessionPurge(ctx context.Context, now int64) error {
	_, err := s.exec(ctx, sessionPurge, now)
	return err
}

func (s *store) PendingPut(ctx context.Context, p dbapi.Pending) error {
	if p.Name == "" || len(p.Key) == 0 || !okKey(p.Name) || len(p.Key) > MaxKey {
		return dbapi.ErrInvalid
	}
	return s.put(ctx,
		pendingUpdate, []any{[]byte(p.Name), p.Since, blob(p.Key)},
		pendingInsert, []any{blob(p.Key), []byte(p.Name), p.Since})
}

func (s *store) PendingList(ctx context.Context) ([]dbapi.Pending, error) {
	var out []dbapi.Pending
	err := s.query(ctx, func() error {
		out = nil
		rows, err := s.db.QueryContext(ctx, pendingList)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p dbapi.Pending
			if err := rows.Scan(&p.Key, &p.Name, &p.Since); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

func (s *store) PendingDelete(ctx context.Context, key []byte) error {
	_, err := s.exec(ctx, pendingDelete, blob(key))
	return err
}
