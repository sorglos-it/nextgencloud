// The update actions of this controller: ngc.store/controller/install, and the
// fork inside it.
//
// There is one core action and two methods behind it. The contract knows a
// single action for installing (core.ActInstall, core.InstallParams), and its
// Target decides which of the two runs: trust.AutoSystem means the system
// packages of this server, which update.Manager.System does with apt, and
// every other target is a part of NGC or an app, which update.Manager.Install
// does. The fork lives here and nowhere else, because this is the one place
// that knows the action was opened at all.
//
// The target is read twice, and that is deliberate. This file reads it to pick
// the method; the method reads it again through the same parser and refuses
// what does not belong to it - Install refuses trust.AutoSystem with
// update.ErrForbidden (update.installParams), System refuses every other
// target with update.ErrInternal (update.systemParams). So a fork that ever
// picked wrong ends the channel with E_FORBIDDEN or E_INTERNAL instead of
// installing the wrong thing. The two codes differ because the two halves
// name the fault differently, and neither is this file's to rename;
// TestInstallAndSystemEachRefuseTheOthersTarget holds both.
package serve

import (
	"context"
	"errors"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/update"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// installKey is ngc.store/controller/install.
var installKey = name{core.AppStore, core.ServiceController, core.ActInstall}

// Updater is what carries out an install on this server
// (apps/server-controller/classes/update.Manager). An interface and not that
// type, so this package keeps the one direction it had: what a channel is
// handed to is wired in from outside and never reached for from here.
//
// Both methods answer with the JSON of their result and an error of package
// update; code() turns that error into the CLOSE of the channel.
type Updater interface {
	Install(ctx context.Context, d access.Decision) ([]byte, error)
	System(ctx context.Context, d access.Decision) ([]byte, error)
}

// emptyResult is what an install answers with. Both methods of package update
// answer with the bytes "{}" and with nothing else, and that is what this
// value encodes to; the raw bytes are not passed on, because core.EncodeResult
// would turn a []byte into a base64 string.
// TestAnInstallAnswersTheEmptyObject holds the two halves against each other.
type emptyResult struct{}

// install carries out ngc.store/controller/install. The input of this action
// is empty - it is FlowCall (core.All), so its MaxIn is 0 and read gave back
// nothing - and everything it does stands in its params.
func (s *Server) install(ctx context.Context, d access.Decision, _ []byte) (any, *wire.Close) {
	if s.d.Updater == nil {
		// A controller without an updater is this program wired wrong, not a
		// peer's doing: which halves a server was built with is nothing an
		// API decides.
		return nil, refuse(wire.CodeInternal)
	}
	target, cl := installTarget(d)
	if cl != nil {
		return nil, cl
	}
	var err error
	if target == trust.AutoSystem {
		_, err = s.d.Updater.System(ctx, d)
	} else {
		_, err = s.d.Updater.Install(ctx, d)
	}
	if err != nil {
		return nil, code(err)
	}
	return emptyResult{}, nil
}

// installTarget reads the target out of the params of this channel, through
// the parser of the contract and not through a second one: the params are the
// peer's bytes, so whatever will not parse as this action's params is answered
// as the peer's fault.
//
// It never looks at a decision for another action. access has already matched
// the OPEN against core.All, so a decision that names something else here is
// this program wired wrong.
func installTarget(d access.Decision) (string, *wire.Close) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActInstall)
	if !ok {
		return "", refuse(wire.CodeInternal)
	}
	if !d.Core || d.Action.App != a.App || d.Action.Service != a.Service || d.Action.Name != a.Name {
		return "", refuse(wire.CodeInternal)
	}
	params, err := core.ParseParams(a, d.Params())
	if err != nil {
		return "", refuse(wire.CodeVerify)
	}
	p, ok := params.(*core.InstallParams)
	if !ok {
		return "", refuse(wire.CodeInternal)
	}
	return p.Target, nil
}

// code is the CLOSE of a failed install. Every error of package update is one
// of the eight below or wraps one of them (ErrVerify, ErrForbidden,
// ErrNeedsGrants, ErrNothing, ErrInternal, ErrDisk, ErrHealth, ErrSystem -
// counted 12.10.2026; this said seven), and the three walls are told apart:
// what this installation did not sign is E_VERIFY, what it signed and still
// may not have is E_FORBIDDEN, and what went wrong on this machine is
// E_INTERNAL.
//
// ErrNothing is E_NOTFOUND and not E_FORBIDDEN: nothing was refused, there is
// simply no file lying ready for that target, and an admin who reads
// "Keine Berechtigung." for it goes looking for a right that is not missing.
func code(err error) *wire.Close {
	switch {
	case errors.Is(err, update.ErrVerify):
		return refuse(wire.CodeVerify)
	case errors.Is(err, update.ErrForbidden), errors.Is(err, update.ErrNeedsGrants):
		return refuse(wire.CodeForbidden)
	case errors.Is(err, update.ErrNothing):
		return refuse(wire.CodeNotFound)
	default:
		// ErrInternal, ErrDisk, ErrHealth, ErrSystem and anything the local
		// store answered with: all of them are this server's, and none of
		// them is written here. Package update reports every one of them on
		// the control channel of the API already (store.UpdateReport), which
		// is where an admin reads what went wrong; a line per channel on top
		// of it would be one a peer decides the number of.
		return refuse(wire.CodeInternal)
	}
}
