// Package rights decides for every channel anew whether it may open, out of
// user, role, app and action. No unsigned part of the OPEN moves that decision;
// a signed one does, and Request says which and why. The rights of a role live
// in the database (dbapi.BlobRights), the roles of a user in the trust list,
// which only an admin can change. The role admin may use every core action,
// and holds the rights of every installed app without a row of its own
// (adminApps); every other right is granted per role and app.
//
// The licence of an app is the one source of the decision this package does
// not read yet: it arrives with the task that builds Check's caller (point 20
// of docs/vertraege-v1.2.md, store.LicenseOf and dbapi.BlobLicense). Until
// then a licensed app is open to whoever holds the right.
package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
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
	if admin {
		if err := adminApps(ctx, db, out); err != nil {
			return nil, false, err
		}
	}
	return out, admin, nil
}

// adminApps adds the rights of every installed app to an admin's rights. It
// closes a circle that left a freshly installed app unreachable for everybody:
// an app only reaches the menu with <app>/use (coreapp.Dispatcher.apps), and
// the one page that hands rights out read its list of apps FROM that menu, so
// the first right of a new app could never be given to anyone.
//
// It is granted here and not written into the row of the role admin, and that
// is the whole point: a right that lives in no row cannot be taken away by
// saving the page without it, and an installation never stands with an app
// nobody may see.
//
// A row that is switched off or carries no version is left out, exactly as
// the menu leaves it out; a manifest the database cannot parse is a broken row
// and grants nothing, which is what Dispatcher.apps does with it as well.
func adminApps(ctx context.Context, db dbapi.Store, out map[string]bool) error {
	list, err := db.AppList(ctx)
	if err != nil {
		return err
	}
	for _, a := range list {
		if !a.Active || a.Version == "" {
			continue
		}
		m, err := store.ParseManifest(a.Manifest)
		if err != nil {
			continue
		}
		out[a.ID+"/"+core.RightUse] = true
		for _, act := range m.Actions {
			out[a.ID+"/"+act.Name] = true
		}
	}
	return nil
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
//
// A row of a role the trust list no longer holds is left out here, and nothing
// in this package deletes one: a role that is created again under the same
// name silently gets its old rights back. This package has no way to delete,
// so the task that manages roles has to clear the row with the role.
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
