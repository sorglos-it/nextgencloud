package appstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Deactivate stops an app on its controller without removing it
// (ngc.store/controller/deactivate). The row stays with everything in it and
// only Active and Updated change, so activating the app again needs neither
// the package nor the grants a second time.
func (st *Stores) Deactivate(ctx context.Context, rq DeactivateRequest) (wire.Code, error) {
	if st.Send == nil || rq.Params == nil {
		return wire.CodeInternal, nil
	}
	if rq.Params.Validate() != nil {
		return wire.CodeVerify, nil
	}
	row, code, err := st.rowFor(ctx, rq.Carried, rq.Params.App)
	if code != "" || err != nil {
		return code, err
	}
	params, err := json.Marshal(rq.Params)
	if err != nil {
		return wire.CodeInternal, err
	}
	if _, code := st.Send.Call(ctx, rq.Node, core.ActDeactivate, params,
		rq.Task, rq.Client, rq.Proof); code != "" {
		return code, nil
	}
	row.Active, row.Updated = false, st.S.Now()
	if err := st.S.DB.AppPut(ctx, row); err != nil {
		return wire.CodeInternal, err
	}
	return st.noteApp(ctx, rq.Carried, row.ID, "deaktiviert")
}

// Uninstall removes an app from its controller (ngc.store/controller/uninstall)
// and then takes it out of this API's books.
//
// What goes and what stays is a decision, not a side effect, and the UI rows
// go in both directions of KeepData:
//
//   - The ui-tar (dbapi.BlobUI) and the evidence of the installed version
//     (dbapi.BlobApproval) GO. They belong to a version that is on no node any
//     more, they are up to store.MaxUITotal of bytes, and nothing else in NGC
//     would ever remove them: a row that only this step deletes and only
//     Activate writes would otherwise lie there for good, with nothing left to
//     name it. Activating the version again writes both anew out of the
//     package, so nothing irreplaceable is lost. There are none to delete
//     where the row names no version (Version empty), which is a row that
//     already holds nothing but kept data.
//   - The licence (dbapi.BlobLicense) STAYS, with KeepData and without. It was
//     bought, a store signed it for this installation alone, and this API
//     cannot make another one: throwing it away would cost the admin a trip to
//     the shop for an app they uninstalled by mistake. It is small, and its row
//     is named by the app alone, so a reinstall picks it up again.
//   - The app row itself goes only without KeepData. With KeepData it stays as
//     the record that encrypted data of this app is still lying on its node: ID,
//     store and node remain, Version becomes empty, and everything that
//     described the version that is gone -- manifest, checksum, grants -- is
//     cleared, because a row that keeps them claims to describe something
//     installed (core.InstalledApp, "empty if only kept data remains").
//
// The order is controller, then the UI rows, then the app row, and it is that
// way round so that every failure leaves a state a repeat finishes from. A
// failure after the app row was gone would leave the UI rows with nothing that
// names them; a failure before it leaves a row that still says "installed",
// and uninstalling again removes what is left.
func (st *Stores) Uninstall(ctx context.Context, rq UninstallRequest) (wire.Code, error) {
	if st.Send == nil || rq.Params == nil {
		return wire.CodeInternal, nil
	}
	if rq.Params.Validate() != nil {
		return wire.CodeVerify, nil
	}
	row, code, err := st.rowFor(ctx, rq.Carried, rq.Params.App)
	if code != "" || err != nil {
		return code, err
	}
	params, err := json.Marshal(rq.Params)
	if err != nil {
		return wire.CodeInternal, err
	}
	if _, code := st.Send.Call(ctx, rq.Node, core.ActUninstall, params,
		rq.Task, rq.Client, rq.Proof); code != "" {
		return code, nil
	}
	if row.Version != "" {
		key := UIKey(row.ID, row.Version)
		if err := st.S.DB.BlobDelete(ctx, dbapi.BlobUI, key); err != nil {
			return wire.CodeInternal, err
		}
		if err := st.S.DB.BlobDelete(ctx, dbapi.BlobApproval, key); err != nil {
			return wire.CodeInternal, err
		}
	}
	note := "deinstalliert"
	if rq.Params.KeepData {
		// KeptData is carried over and not measured here: an uninstall of a
		// controller answers with nothing the contracts read a number out of
		// (see the report of task 22, "Ungemessenes"), so what was on record
		// stays on record.
		if err := st.S.DB.AppPut(ctx, dbapi.App{ID: row.ID, Store: row.Store, Node: row.Node,
			KeptData: row.KeptData, Updated: st.S.Now()}); err != nil {
			return wire.CodeInternal, err
		}
		note = "deinstalliert, Daten behalten"
	} else if err := st.S.DB.AppDelete(ctx, row.ID); err != nil {
		return wire.CodeInternal, err
	}
	return st.noteApp(ctx, rq.Carried, row.ID, note)
}

// rowFor returns the row of an installed app for one request.
//
// A task that names another node than the row is refused. It would otherwise
// take away the row of an app that goes on running on its own node -- and an
// app NGC holds no row for can be neither seen nor stopped, which is the one
// state this package must never create.
func (st *Stores) rowFor(ctx context.Context, c Carried, app string) (dbapi.App, wire.Code, error) {
	if !wire.ValidName(c.Node) {
		return dbapi.App{}, wire.CodeVerify, nil
	}
	row, err := st.S.DB.AppGet(ctx, app)
	if errors.Is(err, dbapi.ErrNotFound) {
		return dbapi.App{}, wire.CodeNotFound, nil
	}
	if err != nil {
		return dbapi.App{}, wire.CodeInternal, err
	}
	if row.Node != c.Node {
		return dbapi.App{}, wire.CodeForbidden, nil
	}
	return row, "", nil
}

// noteApp writes the log entry of one app step. audit.ErrDropped says a field
// was shortened and the entry was written, so it is not handed on as a failure
// of the step.
func (st *Stores) noteApp(ctx context.Context, c Carried, app, detail string) (wire.Code, error) {
	if st.Log == nil {
		return "", nil
	}
	if err := st.Log.Note(ctx, core.LogApp, c.user(), app, c.Node, detail); err != nil &&
		!errors.Is(err, audit.ErrDropped) {
		return wire.CodeInternal, err
	}
	return "", nil
}

// Installed answers ngc.store/installed: one row per app this API holds a
// record of, in the order of the database (by ID), so the shell always shows
// the same order.
//
// The state is the closed list of core: an app whose controller has no tunnel
// right now is unreachable, one whose licence is past its grace period is
// unlicensed, everything else is ok.
func (st *Stores) Installed(ctx context.Context) ([]core.InstalledApp, error) {
	list, err := st.S.DB.AppList(ctx)
	if err != nil {
		return nil, err
	}
	now := st.S.Now()
	out := make([]core.InstalledApp, 0, len(list))
	for _, a := range list {
		row := core.InstalledApp{ID: a.ID, Version: a.Version, Store: a.Store, Node: a.Node,
			Active: a.Active, State: core.AppOK, License: st.licenseState(ctx, a.ID, now),
			KeptData: a.KeptData}
		_, online := false, false
		if st.Send != nil {
			_, online = st.Send.Node(a.Node)
		}
		switch {
		case !online:
			row.State = core.AppUnreachable
		case row.License == core.LicenseExpired:
			row.State = core.AppUnlicensed
		}
		out = append(out, row)
	}
	return out, nil
}

// licenseState is the state of one app's licence for a row of Installed: none
// where no licence is on record, and expired for a row that cannot be read or
// no longer verifies. It never answers valid out of an error -- the row is
// this API's own, and one it cannot judge any more must not unlock anything.
func (st *Stores) licenseState(ctx context.Context, app string, now int64) string {
	l, _, ok, err := st.License(ctx, app)
	switch {
	case errors.Is(err, dbapi.ErrNotFound):
		return core.LicenseNone
	case err != nil:
		return core.LicenseExpired
	case !ok:
		return core.LicenseNone
	}
	return core.LicenseOf(l, now)
}
