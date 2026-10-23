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
	userWithID = "SELECT id FROM ngc_user WHERE id = ?"
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
//
// That the check sits there is held twice over, because neither way holds it
// alone.
//
// TestTheNameCheckSitsInTheTransaction reads the source. It has to, because
// no answer of this method says where the check is: the unique index
// ngc_user_name gives the same word on both ways, so taking the check out,
// asking it of the pool instead of this transaction, or leaving it written
// where it stands and running it from a defer all leave every test that asks
// this store a question green, and no group of the contract goes red with any
// of them. Measured, all of those shapes - the contract half of that by hand,
// because dbtest.Run then had one call site in the tree and it was against
// memstore, so no group of it ran against this store in go test ./... . Task 7
// gave it a second one, so that half is measurable by a standing run now; it
// has not been retaken. The note at TestOneWriteAndOneDelete in user_test.go
// says it in full.
//
// But a guard over the syntax tree can say where a statement is written and
// never that it runs and that its answer gates this write - that is a
// property of the run, not of the shape - and three ways past it were found
// in three rounds, each one a shape the round before had not thought of. So
// TestTheCheckAloneRefusesATakenName and TestOnlyOneWriterGetsAName ask this
// method instead, on a database whose ngc_user_name has been dropped, where
// the check is the only thing left that can answer. The two shapes the guard
// could not see - the check under a condition that can never hold, and the
// question asked with its answer thrown away - are red there at once, and the
// guard stays green on both. It is not the answer of the check that makes it
// worth its statement, it is its place; the guard measures that place in the
// file, and those two measure it in the run.
//
// The question in front of the write has a third answer besides a row and no
// row, and that is an error. Read as "no row" it lets the write through, so a
// second writer takes the name and is told he got it.
// TestAFailedQuestionIsNotAFreeName holds that one, on a database where a view
// puts a NULL where the id is read: the question fails there, and the INSERT
// and the UPDATE behind it do not.
//
// dbapi promises "the same name for another ID gives ErrInvalid", so a caller
// that asks errors.Is for it may not be handed a driver error instead. Both
// statements of the write can meet an index, and each of them answers where
// it stands, not from one translation around them both:
//
//   - The UPDATE renames onto a name the SELECT did not see - a writer one
//     statement behind, or a row nobody of ours wrote. It leaves the ID alone,
//     so the primary key is not in its way, and ngc_user carries no unique
//     index besides that one and ngc_user_name: only the name can stop this
//     statement, so its duplicate is ErrInvalid on the spot. A second unique
//     index on this table would come out here as "the name belongs to another
//     ID" and say nothing about itself.
//   - The INSERT can meet either index, and which one it was decides the
//     answer. So the database is asked whether a row carries this ID. If one
//     does, somebody was faster with it, and writing on top of that row is the
//     answer and not an error. If none does, the INSERT can only have met the
//     name - ErrInvalid.
//
// Asked, and not counted. How many rows an UPDATE affected is not one number
// everywhere: it is the rows it matched, or only the rows it really changed,
// depending on the driver - on MariaDB that is the ClientFoundRows flag of the
// DSN, and nothing in this repository opens a MariaDB connection yet, so
// nobody sets it today. put rests on that flag and says so, LockTake beside it
// names who will have to set it, and put has no key of its own to ask the
// database with; here the key is at hand. Read off the count, the answer of
// this method would hang on that flag: the same user written again unchanged
// leaves a driver that counts changed rows nothing to count, the INSERT behind
// it then breaks on the primary key, and a valid repeated write would come
// back as ErrInvalid. TestUserPutAnswersTheSameHoweverRowsAreCounted makes
// SQLite count the other way and puts both sets of answers side by side.
//
// That second round is the correction of a sentence that stood here and was
// wrong: "the primary key cannot reach it". It does, and one translation for
// both statements could not tell it apart. Two writers adding the same ID
// that is not there yet both miss with the UPDATE, and the second breaks on
// the primary key - which Duplicate reports as well, sqliteDialect names
// CONSTRAINT_PRIMARYKEY itself. That valid input came back as ErrInvalid, the
// contract's word for "the name belongs to another ID".
//
// Splitting the translation is what keeps the two measurable.
// TestADuplicateNameIsErrInvalid plants a row whose name is taken and whose
// ID is empty, so the SELECT lets both ways through - once into the INSERT,
// once into the UPDATE - and each of the two places answers its half alone.
// The other end of the round, where the row is there after all, is reached
// from the counting side: with rows counted as changed the second UPDATE finds
// the row it had nothing to change in. What no test reaches is the race, where
// another writer puts that row there between the two statements, because on
// SQLite a writer holds the database while it writes.
func (s *store) UserPut(ctx context.Context, u dbapi.User) error {
	roles, ok := joinList(u.Roles)
	if u.ID == "" || u.Name == "" || !okKey(u.ID) || !okKey(u.Name) || !ok || !okDoc(u.TOTP) {
		return dbapi.ErrInvalid
	}
	err := s.write(ctx, func(tx *sql.Tx) error {
		var other string
		switch err := tx.QueryRowContext(ctx, userIDOf, []byte(u.Name)).Scan(&other); {
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		case other != "" && other != u.ID:
			return dbapi.ErrInvalid
		}
		for round := range 2 {
			res, err := tx.ExecContext(ctx, userUpdate, []byte(u.Name), []byte(roles), blob(u.TOTP),
				u.Locked, int64(u.Failures), u.LastLogin, []byte(u.ID))
			if err != nil {
				if s.d.Duplicate(err) {
					return dbapi.ErrInvalid // the rename met the name index
				}
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n > 0 {
				return nil
			}
			if round == 1 {
				// The INSERT met an index, and this UPDATE has written
				// whatever there was to write. Which index it was is asked of
				// the database and not read off n, because n is 0 here both
				// where no row carries this ID and where a driver that counts
				// changed rows found nothing to change in the row that does.
				var have string
				switch err := tx.QueryRowContext(ctx, userWithID, []byte(u.ID)).Scan(&have); {
				case err == nil:
					return nil
				case !errors.Is(err, sql.ErrNoRows):
					return err
				}
				// No row carries this ID, so it was the name. put answers an
				// error of its own here, because a row it writes has nothing
				// but its primary key to meet.
				return dbapi.ErrInvalid
			}
			if _, err := tx.ExecContext(ctx, userInsert, []byte(u.ID), []byte(u.Name), []byte(roles),
				blob(u.TOTP), u.Locked, int64(u.Failures), u.LastLogin); err != nil {
				if !s.d.Duplicate(err) {
					return err
				}
				continue // the primary key or the name: the UPDATE says which
			}
			return nil
		}
		return nil
	})
	return err
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
// token, a Noise key, a secret. That is the way OnceUse passes its value, and
// the call is what the guard in sqlcheck_test.go reads: it follows the shape,
// not the reasoning.
//
// blob is the defence, not the length check in front of it. okDoc(nil) is
// true, so nothing but blob keeps a nil out of u.TOTP, v.Cert, v.Proof,
// v.APIKey and v.APICert; only the len(v.Token) == 0 and len(p.Key) == 0 of
// SessionPut and PendingPut keep one out, and only for those two values. A
// nil byte slice binds as NULL - the vendored driver says so itself, "Handle
// []byte(nil) as a NULL value" in packets.go - and every column of the
// migration is NOT NULL, so on MariaDB those five write a row the database
// refuses the moment blob goes. The SQLite driver of this
// project binds an empty blob instead, so there the same row is written and
// no test of behaviour can see the difference: measured both ways round, with
// the ten calls that pass those five values taken out - each value goes to the
// database twice, in the UPDATE and in the INSERT - this whole package stays
// green and no group of the contract goes red either, and
// TestEveryByteSliceComesFromBlob goes red on its own. Retaken on task 7
// against the standing run of the contract that TestConformance in
// classes/sqlite now gives: all eleven groups green, that one guard red. That
// guard reads the source, and it is the only thing holding these ten calls.
//
// Where no check stands in front of it at all - the token of SessionGet,
// SessionTouch and SessionDelete, the key of PendingDelete - blob turns a nil
// into an empty one, and that changes no answer: SessionPut and PendingPut
// refuse an empty token and an empty key, so no row this store writes carries
// one, and an empty token matches as little as NULL did. A row nobody of ours
// wrote could carry one, the way TestAUserRowNobodyOfOursWrote plants a roles
// column, and such a row would answer a nil token where a NULL would have
// matched nothing - NULL is equal to nothing in SQL, not even to NULL. The
// store writes no such row. In oneUser the argument is a conversion of a
// string and never nil, so blob is the shape there and nothing more.
// TestAnEmptyTokenIsNobodysSession measures both halves - the refusal as well
// as the answer, because without the refusal the empty token would be a key
// like any other.
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
