// Package rights decides for every channel anew whether it may open, out of
// user, role, app and action — never out of what the OPEN carries. The rights
// of a role live in the database (dbapi.BlobRights), the roles of a user in
// the trust list, which only an admin can change. The role admin may use every
// core action; every other right is granted per role and app.
//
// The licence of an app is the one source of the decision this package does
// not yet read: no contract names where a channel reads it, and nothing in the
// API checks one, so a licensed app is open to whoever holds the right. The
// place for it is Check, next to the app's row (dbapi.BlobLicense).
package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrRights reports a rights row the database cannot hold.
var ErrRights = errors.New("rights: malformed rights row")

// Of returns the rights of a user, joined over the user's roles, and whether
// the user is an admin.
func Of(ctx context.Context, db dbapi.Store, l *trust.List, user string) (map[string]bool, bool, error) {
	u, ok := l.User(user)
	if !ok {
		return nil, false, nil
	}
	admin := slices.Contains(u.Roles, trust.RoleAdmin)
	out := map[string]bool{}
	for _, role := range u.Roles {
		r, err := read(ctx, db, role)
		if err != nil {
			return nil, false, err
		}
		for _, right := range r.Rights {
			out[right] = true
		}
	}
	return out, admin, nil
}

// read returns the rights of one role; a role without a row has none.
func read(ctx context.Context, db dbapi.Store, role string) (core.RoleRights, error) {
	b, err := db.BlobGet(ctx, dbapi.BlobRights, role)
	if errors.Is(err, dbapi.ErrNotFound) {
		return core.RoleRights{Role: role}, nil
	}
	if err != nil {
		return core.RoleRights{}, err
	}
	var r core.RoleRights
	if json.Unmarshal(b.Data, &r) != nil || r.Validate() != nil || r.Role != role {
		return core.RoleRights{}, ErrRights
	}
	return r, nil
}

// Set replaces the rights of a role. The caller has checked r with
// core.ParseParams, so every right is core.ValidRight; Set checks again, since
// it is the only way a row is written and read must be able to trust one.
func Set(ctx context.Context, db dbapi.Store, r core.RoleRights) error {
	if r.Validate() != nil {
		return ErrRights
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobRights, ID: r.Role, Data: data})
}

// All returns the rights of every role of the trust list, for
// ngc.users/rights. A role without a row comes with an empty list, so the
// shell shows every role.
func All(ctx context.Context, db dbapi.Store, l *trust.List) ([]core.RoleRights, error) {
	out := make([]core.RoleRights, 0, len(l.Roles))
	for _, role := range l.Roles {
		r, err := read(ctx, db, role.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// has reports whether the rights hold "<app>/<action>".
func has(r map[string]bool, app, action string) bool { return r[app+"/"+action] }

// usable reports whether the user may see an app at all (core.RightUse).
func usable(r map[string]bool, app string) bool { return r[app+"/"+core.RightUse] }
