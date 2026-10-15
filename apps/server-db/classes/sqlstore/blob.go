package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// Every statement of this file, as a constant. Nothing is ever put together
// at run time: the names come from the fixed lists, the values from
// placeholders.
const (
	blobSelect = "SELECT seq, data, updated FROM ngc_blob WHERE kind = ? AND id = ?"
	blobUpdate = "UPDATE ngc_blob SET seq = ?, data = ?, updated = ? WHERE kind = ? AND id = ?"
	blobInsert = "INSERT INTO ngc_blob (kind, id, seq, data, updated) VALUES (?, ?, ?, ?, ?)"
	blobFrom   = "SELECT id, seq, data, updated FROM ngc_blob WHERE kind = ? AND seq >= ? ORDER BY seq, id"
	blobDelete = "DELETE FROM ngc_blob WHERE kind = ? AND id = ?"

	appSelect = "SELECT version, store, node, active, manifest, sha256, grants, kept_data, updated FROM ngc_app WHERE id = ?"
	appUpdate = "UPDATE ngc_app SET version = ?, store = ?, node = ?, active = ?, manifest = ?, " +
		"sha256 = ?, grants = ?, kept_data = ?, updated = ? WHERE id = ?"
	appInsert = "INSERT INTO ngc_app (id, version, store, node, active, manifest, sha256, grants, kept_data, updated) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	appList   = "SELECT id, version, store, node, active, manifest, sha256, grants, kept_data, updated FROM ngc_app ORDER BY id"
	appDelete = "DELETE FROM ngc_app WHERE id = ?"
)

func (s *store) BlobPut(ctx context.Context, b dbapi.Blob) error {
	if b.Kind == "" || b.ID == "" || !okKey(b.Kind) || !okKey(b.ID) || !okData(b.Data) {
		return dbapi.ErrInvalid
	}
	if err := checkSeq(b.Seq); err != nil {
		return err
	}
	seq := int64(b.Seq)
	return s.put(ctx,
		blobUpdate, []any{seq, blob(b.Data), b.Updated, []byte(b.Kind), []byte(b.ID)},
		blobInsert, []any{[]byte(b.Kind), []byte(b.ID), seq, blob(b.Data), b.Updated})
}

func (s *store) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	// A key the column cannot hold matches no row, and that answer is given
	// here instead of in the database: the bound belongs in front of the
	// work, and MariaDB would answer a value larger than its packet with an
	// error where SQLite simply compares and finds nothing.
	if !okKey(kind) || !okKey(id) {
		return dbapi.Blob{}, dbapi.ErrNotFound
	}
	out := dbapi.Blob{Kind: kind, ID: id}
	err := s.query(ctx, func() error {
		var seq int64
		err := s.db.QueryRowContext(ctx, blobSelect, []byte(kind), []byte(id)).Scan(&seq, &out.Data, &out.Updated)
		if errors.Is(err, sql.ErrNoRows) {
			return dbapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Seq, err = readSeq(seq)
		return err
	})
	if err != nil {
		return dbapi.Blob{}, err
	}
	return out, nil
}

func (s *store) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	if err := checkSeq(seq); err != nil {
		return nil, err
	}
	if !okKey(kind) {
		return nil, nil
	}
	var out []dbapi.Blob
	err := s.query(ctx, func() error {
		out = nil
		rows, err := s.db.QueryContext(ctx, blobFrom, []byte(kind), int64(seq))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			b := dbapi.Blob{Kind: kind}
			var n int64
			if err := rows.Scan(&b.ID, &n, &b.Data, &b.Updated); err != nil {
				return err
			}
			// The second lock on the same door: blobFrom asks for seq >= the
			// number given, which is never below zero, so the database leaves
			// such a row out already. The conversion is measured all the
			// same, so that it does not depend on a WHERE somewhere else.
			seq, err := readSeq(n)
			if err != nil {
				return err
			}
			b.Seq = seq
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

func (s *store) BlobDelete(ctx context.Context, kind, id string) error {
	// Removing what is not there is no error, and a key the column cannot
	// hold is exactly that.
	if !okKey(kind) || !okKey(id) {
		return nil
	}
	_, err := s.exec(ctx, blobDelete, []byte(kind), []byte(id))
	return err
}

// appColumns are the places one row of ngc_app is read into, in the order of
// appSelect and of appList behind its id. AppGet and AppList read the same
// row, so they read it through the same two functions and can never drift
// apart. Two columns need converting: active is a number, and grants is one
// column holding a short list.
func appColumns(a *dbapi.App, active *int64, grants *string) []any {
	return []any{&a.Version, &a.Store, &a.Node, active, &a.Manifest, &a.SHA256, grants, &a.KeptData, &a.Updated}
}

// appDone puts the two converted columns into the App. What the grants
// column carries is measured like anything that comes in from outside: a row
// of the store's own database is no different, see readList. What AppPut
// refuses to write is refused on the way out as well - the list goes on into
// the interface, and a grant nobody knows has no business there.
//
// grantsOK stands here and not in readList because readList also reads
// User.Roles from task 6 on, and a role is no grant.
func appDone(a *dbapi.App, active int64, grants string) error {
	list, err := readList(grants)
	if err != nil {
		return err
	}
	if !grantsOK(list) {
		return errBrokenGrants
	}
	a.Active, a.Grants = active == 1, list
	return nil
}

func (s *store) AppPut(ctx context.Context, a dbapi.App) error {
	grants, ok := joinList(a.Grants)
	if a.ID == "" || !okKey(a.ID) || !okKey(a.Version) || !okKey(a.Store) || !okKey(a.Node) ||
		!okDoc(a.Manifest) || !okKey(string(a.SHA256)) || !ok || !grantsOK(a.Grants) {
		return dbapi.ErrInvalid
	}
	active := int64(0)
	if a.Active {
		active = 1
	}
	return s.put(ctx,
		appUpdate, []any{[]byte(a.Version), []byte(a.Store), []byte(a.Node), active, blob(a.Manifest),
			blob(a.SHA256), []byte(grants), a.KeptData, a.Updated, []byte(a.ID)},
		appInsert, []any{[]byte(a.ID), []byte(a.Version), []byte(a.Store), []byte(a.Node), active,
			blob(a.Manifest), blob(a.SHA256), []byte(grants), a.KeptData, a.Updated})
}

func (s *store) AppGet(ctx context.Context, id string) (dbapi.App, error) {
	if !okKey(id) {
		return dbapi.App{}, dbapi.ErrNotFound
	}
	out := dbapi.App{ID: id}
	err := s.query(ctx, func() error {
		var active int64
		var grants string
		err := s.db.QueryRowContext(ctx, appSelect, []byte(id)).Scan(appColumns(&out, &active, &grants)...)
		if errors.Is(err, sql.ErrNoRows) {
			return dbapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		return appDone(&out, active, grants)
	})
	if err != nil {
		return dbapi.App{}, err
	}
	return out, nil
}

func (s *store) AppList(ctx context.Context) ([]dbapi.App, error) {
	var out []dbapi.App
	err := s.query(ctx, func() error {
		out = nil
		rows, err := s.db.QueryContext(ctx, appList)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a dbapi.App
			var active int64
			var grants string
			if err := rows.Scan(append([]any{&a.ID}, appColumns(&a, &active, &grants)...)...); err != nil {
				return err
			}
			if err := appDone(&a, active, grants); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (s *store) AppDelete(ctx context.Context, id string) error {
	if !okKey(id) {
		return nil
	}
	_, err := s.exec(ctx, appDelete, []byte(id))
	return err
}
