// The actions of ngc.store: the stores of the trust list and their catalogs,
// what is installed, an upload, a licence, the install key, the update rows
// and the three actions that activate, deactivate and uninstall an app on a
// controller.
//
// Until this file existed, package appstore had every one of these steps built
// and measured and NO caller: nothing registered a handler for ngc.store, so
// each of them answered E_NOTFOUND to a shell (task 24, "weiter nur
// übersetzt"). This file is the registration and nothing else -- not one
// decision of the app store is repeated here.
//
// Three sentences about what this file is allowed to do, because a handler is
// the moment an action becomes reachable from outside:
//
//   - It decides nothing about WHO may open a channel. rights.Check has
//     already run when a handler is called (dispatch.go, open), and every
//     action of ngc.store carries core.Action.Admin, so only the role admin
//     ever reaches one; a root task of service controller has besides that a
//     signed task task.Check verified against the trust list. A handler added
//     here can refuse more, never less, and every refusal below is an extra
//     wall and not the wall.
//   - It believes no parameter of its own. What the shell sent went through
//     core.ParseParams before run called the handler; what it typed is
//     asserted once and a wrong type is E_PROTOCOL, which can only mean a
//     mistake of ours, since core.Lookup and the table pair action and type.
//   - It hands the signed task on AS IT STANDS. The controller checks the
//     signature, so nothing here re-encodes it (Call.TaskEnv).
package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// registerStore adds the actions of ngc.store. Each group is registered only
// when the thing that serves it is there, so an API built without it says
// E_NOTFOUND rather than E_INTERNAL; see Deps.Store.
func (d *Dispatcher) registerStore() {
	if d.Store != nil {
		d.Register(core.AppStore, core.ServiceAPI, core.ActSources, d.storeSources)
		d.Register(core.AppStore, core.ServiceAPI, core.ActCatalog, d.storeCatalog)
		d.Register(core.AppStore, core.ServiceAPI, core.ActInstalled, d.storeInstalled)
		d.Register(core.AppStore, core.ServiceAPI, core.ActUpload, d.storeUpload)
		d.Register(core.AppStore, core.ServiceAPI, core.ActLicense, d.storeLicense)
		d.Register(core.AppStore, core.ServiceAPI, core.ActInstallKey, d.storeInstallKey)
		d.Register(core.AppStore, core.ServiceController, core.ActActivate, d.storeActivate)
		d.Register(core.AppStore, core.ServiceController, core.ActDeactivate, d.storeDeactivate)
		d.Register(core.AppStore, core.ServiceController, core.ActUninstall, d.storeUninstall)
	}
	if d.Updates != nil {
		d.Register(core.AppStore, core.ServiceAPI, core.ActUpdates, d.storeUpdates)
		d.Register(core.AppStore, core.ServiceAPI, core.ActSetUpdate, d.storeSetUpdate)
	}
}

// storeSources answers ngc.store/sources: the stores of the trust list with
// what was last seen of each.
func (d *Dispatcher) storeSources(ctx context.Context, rq *Call) ([]byte, error) {
	list, err := d.Store.Sources(ctx)
	if err != nil {
		return nil, storeFail(err)
	}
	return result(list)
}

// storeCatalog answers ngc.store/catalog: the signed envelope of one store's
// catalog. Only the envelope travels, and that is the contract's own choice
// (core.CatalogResult): the shell verifies it against its own copy of the
// trust list, so it never has to believe this API. The parsed catalog is
// therefore dropped here -- it was parsed to refuse a row that no longer
// verifies, not to be answered.
func (d *Dispatcher) storeCatalog(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.StoreParams)
	if !ok {
		return nil, failWhy(wire.CodeProtocol, reason.Params)
	}
	_, env, err := d.Store.Catalog(ctx, p.Store)
	if err != nil {
		return nil, storeFail(err)
	}
	return result(core.CatalogResult{Catalog: env})
}

// storeInstalled answers ngc.store/installed.
func (d *Dispatcher) storeInstalled(ctx context.Context, rq *Call) ([]byte, error) {
	rows, err := d.Store.Installed(ctx)
	if err != nil {
		return nil, storeFail(err)
	}
	return result(rows)
}

// storeUpload answers ngc.store/upload: the admin sends a package, the API
// walks it safely and answers what it read.
//
// The stream is handed over and not read into memory: rq.In is the channel's
// DATA as it arrives (browser.Input, built in Dispatcher.run), store.MaxPackage
// is 4 GiB, and appstore.Upload reads it in one pass for exactly that reason.
// The dispatcher has bounded the channel at core.Action.MaxIn already, and
// appstore bounds it a second time with its own counter.
//
// This sentence was false until 12.10.2026 and is the reason the measurement
// below exists rather than the comment alone: the dispatcher collected the
// whole package into a []byte before it called anyone, so 64 MiB of input made
// the heap rise by 191 MiB and four gigabytes were allowed. What holds the
// sentence now is TestUploadHoldsNoPackageOnTheChannel, with a run that
// collects on purpose beside the flat ones.
func (d *Dispatcher) storeUpload(ctx context.Context, rq *Call) ([]byte, error) {
	res, err := d.Store.Upload(ctx, rq.In)
	if err != nil {
		return nil, storeFail(err)
	}
	return result(res)
}

// storeLicense answers ngc.store/api/license: the admin sends a licence the
// store signed, the API verifies it with the installation's key and keeps it
// if it is newer than the one held.
//
// The action carries NO parameters (core.All), so which app the licence is for
// can only come out of the licence itself. The name is read out of the
// envelope's payload before anything verified it, and that is allowed for the
// one reason nodeOfTask is allowed: the value only PICKS the row to look at,
// and store.VerifyLicense insists that the signed licence names the very same
// app (verifyLicense, l.App != app is an error). So a wrong name here can
// refuse a licence and can never keep one for an app it was not issued for.
//
// The envelope is parsed twice -- here for the name, in appstore for the
// licence. That is on purpose rather than a second entry point into appstore:
// core.MaxLicenseIn is 16 KiB, and one parser for a licence envelope is worth
// more than one parse saved.
func (d *Dispatcher) storeLicense(ctx context.Context, rq *Call) ([]byte, error) {
	// The same bound once more, so the handler does not depend on its caller
	// for the limit that keeps a licence out of memory.
	body, err := io.ReadAll(io.LimitReader(rq.In, core.MaxLicenseIn+1))
	if err != nil || int64(len(body)) > core.MaxLicenseIn {
		return nil, failWhy(wire.CodeTooLarge, reason.TooLarge)
	}
	app, ok := appOfLicense(body)
	if !ok {
		return nil, failWhy(wire.CodeProtocol, reason.License)
	}
	res, err := d.Store.TakeLicense(ctx, app, body)
	if err != nil {
		return nil, storeFail(err)
	}
	return result(res)
}

// appOfLicense reads the app out of a licence envelope. It goes through
// sign.ParseEnvelope, the parser of the contract, so the bytes are bounded and
// formed before a field of them is looked at; the payload itself is NOT
// verified here, and the caller's comment says why that is safe.
func appOfLicense(body []byte) (string, bool) {
	env, err := sign.ParseEnvelope(body, store.MaxLicense)
	if err != nil {
		return "", false
	}
	var l struct {
		App string `json:"app"`
	}
	if json.Unmarshal(env.Payload, &l) != nil || !wire.ValidAppID(l.App) {
		return "", false
	}
	return l.App, true
}

// storeInstallKey answers ngc.store/install-key. Only the public half of the
// installation's key leaves the API, and appstore.InstallKey is what holds
// that.
func (d *Dispatcher) storeInstallKey(ctx context.Context, rq *Call) ([]byte, error) {
	k, err := d.Store.InstallKey(ctx)
	if err != nil {
		return nil, storeFail(err)
	}
	return result(k)
}

// storeUpdates answers ngc.store/updates: one row per target and node.
func (d *Dispatcher) storeUpdates(ctx context.Context, rq *Call) ([]byte, error) {
	rows, err := d.Updates.Rows(ctx)
	if err != nil {
		return nil, failWhy(wire.CodeUnreachable, reason.DB)
	}
	return result(rows)
}

// storeSetUpdate answers ngc.store/set-update: the update mode of one target.
// Package updates writes the log entry and refuses store.ModeAuto for a target
// the trust list carries no standing approval for, so nothing of that is
// repeated here.
//
// That refusal is core.ErrParams, and it is mapped here by name and not left
// to appstore.CodeFor: CodeFor knows the errors of the app store, and an error
// it does not know is E_INTERNAL -- which would tell the admin this
// installation is broken, when what happened is that it has not allowed
// unattended updates for that target. E_FORBIDDEN says the true thing.
func (d *Dispatcher) storeSetUpdate(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.UpdateSetting)
	if !ok {
		return nil, failWhy(wire.CodeProtocol, reason.Params)
	}
	switch err := d.Updates.SetSetting(ctx, *p); {
	case err == nil:
	case errors.Is(err, core.ErrParams):
		return nil, failWhy(wire.CodeForbidden, reason.Params)
	default:
		return nil, failWhy(wire.CodeUnreachable, reason.DB)
	}
	return result(struct{}{})
}

// storeActivate answers ngc.store/controller/activate: the app is installed
// and started on the controller the signed task names.
//
// THE HANDLER STILL HANDS OVER NO FILE, and it still must not: the shell
// sends no DATA on this channel (core.Action.MaxIn bounds for a FlowTransfer
// action "what the API sends the controller ... the shell sends none, even
// when it opened the channel", and core.ShellOpens says it sends EOF at
// once), so ActivateRequest.File stays nil here as it always did.
//
// WHAT CHANGED on 17.10.2026 is who finds the package: appstore keeps it at
// the upload and looks it up by the checksum of the SIGNED task
// (appstore/hold.go, server.HoldDir). So an activation of an uploaded app
// goes through now, and where the file lies was decided where that decision
// belongs -- in the package that owns the disk and in the server that names
// the directory, not in this handler. Punkt 30 of docs/vertraege-v1.2.md is
// closed with it; what remains of it is one named limit, that several API
// servers share the database and not the disk.
func (d *Dispatcher) storeActivate(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.ActivateParams)
	if !ok {
		return nil, failWhy(wire.CodeProtocol, reason.Params)
	}
	c, err := carried(rq)
	if err != nil {
		return nil, err
	}
	code, err := d.Store.Activate(ctx, appstore.ActivateRequest{Carried: c, Params: p})
	return storeAnswer(code, err)
}

// storeDeactivate answers ngc.store/controller/deactivate.
func (d *Dispatcher) storeDeactivate(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.AppParams)
	if !ok {
		return nil, failWhy(wire.CodeProtocol, reason.Params)
	}
	c, err := carried(rq)
	if err != nil {
		return nil, err
	}
	code, err := d.Store.Deactivate(ctx, appstore.DeactivateRequest{Carried: c, Params: p})
	return storeAnswer(code, err)
}

// storeUninstall answers ngc.store/controller/uninstall.
func (d *Dispatcher) storeUninstall(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.UninstallParams)
	if !ok {
		return nil, failWhy(wire.CodeProtocol, reason.Params)
	}
	c, err := carried(rq)
	if err != nil {
		return nil, err
	}
	code, err := d.Store.Uninstall(ctx, appstore.UninstallRequest{Carried: c, Params: p})
	return storeAnswer(code, err)
}

// storeFail turns an error of package appstore into the code of the contract
// and the sentence that says which check said no. The code is appstore.CodeFor
// and nothing else, so the answer to the shell is unchanged; what is added is
// the reason, and it is read off the SAME error, so the two cannot tell
// different stories about one refusal.
//
// The groups are the groups of CodeFor, in its order: the first that matches
// decides there too. Nothing of the error text travels -- a store is a
// stranger, and appstore/sources.go says in full why a stranger writes no line
// an administrator reads.
func storeFail(err error) error {
	code := appstore.CodeFor(err)
	switch {
	case errors.Is(err, appstore.ErrTooLarge):
		return failWhy(code, reason.TooLarge)
	case errors.Is(err, appstore.ErrLicense):
		return failWhy(code, reason.License)
	case errors.Is(err, appstore.ErrStore):
		return failWhy(code, reason.StoreAnswer)
	case errors.Is(err, dbapi.ErrNotFound):
		return failWhy(code, reason.RowGone)
	case code == wire.CodeVerify:
		// The verify group of CodeFor: package, manifest, catalog, release,
		// rollback, licence, ui-proof, transfer, signature, key, envelope.
		// One sentence covers them because what the reader has to do is the
		// same for all of them, and the detail belongs to the store that sent
		// the bytes, not to this log.
		return failWhy(code, reason.Signature)
	}
	// CodeFor answers E_INTERNAL for everything it does not know, and so does
	// this: a fault of the installation, with no check that said no.
	return failWhy(code, reason.Defect)
}

// storeAnswer turns what the three controller steps answer into the end of the
// channel. They answer a wire.Code for every refusal and an error only for
// this installation's own failure, and then the code says so as well
// (appstore.Stores.Activate) -- so the code alone decides, and the error is
// not mapped a second time. None of the three has a result of its own, and an
// action without one answers {} (core.Flow).
func storeAnswer(code wire.Code, err error) ([]byte, error) {
	if code != "" {
		// The code came from the far side, so the sentence says that and no
		// more: the words of a controller are not this API to write into its
		// own log, and the controller writes its own reason into its own
		// journal.
		return nil, failWhy(code, reason.NodeRefused)
	}
	if err != nil {
		// Unreachable as the three are written today: a code comes with every
		// error. It is here so a future step that forgets the code does not
		// turn a failure into a success.
		return nil, storeFail(err)
	}
	return result(struct{}{})
}

// carried is who an action of service controller runs for, and where. All of
// it travels to the controller as it stands, because the controller checks
// task, proof and re-confirmation itself and would read any change as a
// forgery.
//
// The node is the VERIFIED task's own (task.Task.Node), never the guess the
// dispatcher read out of the unsigned payload to pick the target.
func carried(rq *Call) (appstore.Carried, error) {
	if rq.Task == nil || rq.TaskEnv == nil || rq.Live == nil {
		// open answers E_CONFIRM for a root task without a task, so this is
		// our own mistake and not a refusal of the shell's.
		return appstore.Carried{}, failWhy(wire.CodeInternal, reason.OwnTask)
	}
	// The proof of this session, as the proof covers it. It goes through its
	// own Validate again for the reason relay.forward gives: a row that cannot
	// answer it would otherwise become an OPEN the controller refuses by
	// ending the whole tunnel, every other channel on it with it.
	var pr proof.Proof
	if json.Unmarshal(rq.Live.Row.Proof, &pr) != nil || pr.Validate() != nil {
		return appstore.Carried{}, failWhy(wire.CodeInternal, reason.OwnProof)
	}
	return appstore.Carried{Task: rq.TaskEnv, Client: rq.Live.Row.Cert, Proof: &pr,
		Confirm: rq.Confirm, Node: rq.Task.Node, Live: rq.Live}, nil
}
