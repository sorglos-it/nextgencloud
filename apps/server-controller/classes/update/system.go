// Updating the system packages of this server: the one call that hands the
// machine over to apt-get and nothing else.
//
// What makes this action different from every other update this file's
// package knows: there is no file, no signature and no version. Debian and
// Ubuntu sign their own packages, apt verifies them, and nothing here could
// add to that. So the whole of what this controller decides is WHETHER apt
// may run - and that decision is the one wall this file has:
//
//   - with a user, the signed task of that user carries the authority, and
//     access.Check verified it before this is reached;
//   - without a user, only a standing approval for trust.AutoSystem in the
//     installation's own trust list (core.Unattended, store.Approved). There
//     is no version comparison, because there is no version: for this target
//     core.InstallParams carries neither one nor a checksum, and what apt
//     installs is whatever the distribution calls current.
//
// After that wall there is nothing left to check and everything left to run,
// which is why the rest of the file is short. apt-get is started with an
// argument list and never through a shell; DEBIAN_FRONTEND=noninteractive is
// in its environment, because a package that asks a question on a server
// nobody is sitting at would hang for ever. "update" runs first and "upgrade"
// only if it succeeded: an upgrade against the package lists of last week
// installs last week's versions and calls it done.
//
// This call leaves NO record of its own. apt keeps one, and a second row
// beside it would be a second truth about the same machine; so System writes
// nothing into this controller's store - measured, over two runs and over a
// run that was broken off (TestSystemRunTwiceAndBrokenOffLeaveNoLedger). A
// second call therefore asks apt again instead of answering out of a row of
// ours, and what a broken-off apt left half-done is apt's own to finish; this
// controller does not try to.
package update

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrSystem says apt-get did not finish: a package manager that is locked, a
// mirror that does not answer, a package whose install script failed. Nothing
// about it is a peer's doing, so an API is told the update failed on this
// machine (store.UpdateFailed) and not that anything was refused.
var ErrSystem = errors.New("update: the system packages could not be updated")

// aptDefault is where apt-get lives on Debian and Ubuntu, the two systems NGC
// is built for. The absolute path is the point: a bare name would be looked up
// in PATH, and this program runs as root.
const aptDefault = "/usr/bin/apt-get"

// sysPaths is where this file finds the program it runs. It is a variable and
// not a constant for one reason, the same one runtime.paths has: a case has to
// put a program of its own in place of apt-get, because the real one changes
// this machine and a test may not. Nothing outside this package can reach it,
// and nothing inside it changes it at run time.
var sysPaths = struct{ apt string }{apt: aptDefault}

// aptArgs is the argument list of one step. One list for both steps and not
// two: everything in it is written here, the step is the only thing that
// changes, and a reader has one place to look.
//
// The two options that only matter to "upgrade" are passed to "update" as
// well, where they do nothing: --force-confold keeps a configuration file the
// admin has edited instead of asking about it, and --no-install-recommends
// keeps an upgrade from pulling in software nobody asked for. Keeping them in
// one list is worth more than saving two words apt ignores.
func aptArgs(step string) []string {
	return []string{"-o", "Dpkg::Options::=--force-confold", "-y", "--no-install-recommends", step}
}

// aptEnv is the whole environment apt-get gets, not an addition to this
// process's: a controller runs as root and carries whatever systemd gave it,
// and none of that is apt's business. DEBIAN_FRONTEND keeps every package
// script from asking a question nobody is there to answer; PATH is where the
// helpers apt starts itself live, and LANG keeps its output the same on every
// machine.
func aptEnv() []string {
	return []string{
		"DEBIAN_FRONTEND=noninteractive",
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=C.UTF-8",
	}
}

// aptCut bounds what apt wrote before any of it is logged or put into an
// error. An upgrade of a hundred packages writes more than anybody reads, and
// the end of it is what says why it failed - so the LAST service.MaxStderr
// bytes are kept, not the first.
func aptCut(out []byte) []byte {
	if len(out) <= service.MaxStderr {
		return out
	}
	return out[len(out)-service.MaxStderr:]
}

// System updates the system packages of this server.
//
// The order:
//
//	1 the dependencies, then the params of this channel through the
//	  contract's own parser
//	2 the wall: a user, or a standing approval for trust.AutoSystem
//	-- only now is anything on this machine changed ------------------------
//	3 apt-get update, and apt-get upgrade only if that succeeded
//	4 one store.UpdateReport - but only from step 2 on: a refusal in step 1
//	  has no params and so no target to name one with
func (m *Manager) System(ctx context.Context, d access.Decision) ([]byte, error) {
	if m == nil || m.d.List == nil || m.d.Report == nil {
		return nil, ErrInternal
	}
	p, err := m.systemParams(d)
	if err != nil {
		// No report: a report has to name a target store.ParseUpdateReport
		// takes, and without the params there is none to name (stage.go).
		// Here the target is always trust.AutoSystem - but the rule is the
		// one of the package, and a method that reports before it knows what
		// it was asked is how that rule gets lost at the next change.
		return nil, err
	}
	if err := m.system(ctx, d, p); err != nil {
		m.d.Report.Update(store.UpdateReport{Target: trust.AutoSystem,
			State: systemState(err), Message: systemMessage(err)})
		return nil, err
	}
	m.d.Report.Update(store.UpdateReport{Target: trust.AutoSystem, State: store.UpdateInstalled})
	return emptyResult, nil
}

// systemParams reads the params of this channel. With a task they are the
// task's, without one the OPEN's (access.Decision.Params); which of the two it
// was decides nothing here, because for this target they carry a target and
// nothing else.
func (m *Manager) systemParams(d access.Decision) (*core.InstallParams, error) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActInstall)
	if !ok {
		return nil, ErrInternal
	}
	// A decision for another action reaching this method is this program
	// wired wrong, not a peer's doing: the params below are read as this
	// action's, so they have to be this action's.
	if !d.Core || d.Action.App != a.App || d.Action.Service != a.Service || d.Action.Name != a.Name {
		return nil, ErrInternal
	}
	params, err := core.ParseParams(a, d.Params())
	if err != nil {
		return nil, errors.Join(ErrVerify, err)
	}
	p, ok := params.(*core.InstallParams)
	if !ok {
		return nil, ErrInternal
	}
	// Another target on THIS method is this program wired wrong and not a
	// peer's doing: this method answers for trust.AutoSystem alone, and
	// every other target belongs to Install, which refuses trust.AutoSystem
	// in turn (install.go, task 20). So neither of the two can be reached
	// through the other.
	//
	// STILL TO BE MEASURED: the piece that chooses between the two by the
	// target is not built in either task - it belongs to the wiring in
	// classes/serve - so the two halves of that seam have never run against
	// each other. Each half is measured on its own.
	if p.Target != trust.AutoSystem {
		return nil, ErrInternal
	}
	return p, nil
}

// system is System without the report, so that every way out of it is
// reported in one place.
func (m *Manager) system(ctx context.Context, d access.Decision, p *core.InstallParams) error {
	// Without a user nothing but a standing approval may let this run. The
	// signer is nil and the installed version empty on purpose: for
	// trust.AutoSystem store.Approved asks for no key and core.Unattended
	// compares no version.
	if d.User == "" && !core.Unattended(p, m.d.List, nil, "") {
		return ErrForbidden
	}
	// Asked before the first of the two, so a channel that is already gone
	// starts nothing at all. It is not a bolt against a cancel DURING the
	// run - exec.CommandContext kills apt for that - it is the cheaper half:
	// the half that leaves the machine untouched.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.apt(ctx, "update"); err != nil {
		return err
	}
	return m.apt(ctx, "upgrade")
}

// apt runs one step and gives back ErrSystem with apt's own last words behind
// it. The output goes into the log bounded (aptCut): it names packages,
// mirrors and dpkg's complaints, which is what helps an admin here, and apt
// is handed none of this installation's own configuration or keys (aptEnv is
// the whole of what it gets), so none of that can come back out in it.
func (m *Manager) apt(ctx context.Context, step string) error {
	cmd := exec.CommandContext(ctx, sysPaths.apt, aptArgs(step)...)
	cmd.Env = aptEnv()
	out, err := cmd.CombinedOutput()
	out = aptCut(out)
	if err != nil {
		slog.Error("apt-get did not finish", "step", step, "error", err, "output", string(out))
		// The context first: a call that was broken off is that, and not a
		// failure of apt. errors.Join keeps both readable for a caller that
		// wants to tell them apart.
		if cerr := ctx.Err(); cerr != nil {
			return errors.Join(cerr, err)
		}
		return errors.Join(ErrSystem, err)
	}
	slog.Info("apt-get finished", "step", step, "output", string(out))
	return nil
}

// systemState is the state the report carries. Only the one refusal of this
// file is store.UpdateRejected; everything else happened on this machine.
func systemState(err error) string {
	switch {
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrVerify):
		return store.UpdateRejected
	default:
		return store.UpdateFailed
	}
}

// systemMessage is the German line an admin reads about a system update that
// did not happen (store.UpdateReport.Message). It says which wall stopped it
// and nothing about this server's paths or about what apt wrote: the texts are
// bounded by wire.MaxMessageText, which store.ParseUpdateReport holds every
// report against (TestSystemMessagesFitTheContract).
func systemMessage(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "Abgebrochen: Die Paketaktualisierung wurde beendet, bevor sie fertig war."
	case errors.Is(err, ErrForbidden):
		return "Abgelehnt: Für Systempakete fehlt auf dieser Anlage die Dauerfreigabe."
	case errors.Is(err, ErrVerify):
		return "Abgelehnt: Die Anfrage für die Systempakete passt nicht."
	case errors.Is(err, ErrSystem):
		return "Nicht aktualisiert: Die Paketverwaltung des Servers meldete einen Fehler."
	default:
		return "Nicht aktualisiert: Dieser Server konnte die Systempakete nicht aktualisieren."
	}
}
