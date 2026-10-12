// Installing an update that lies ready, and taking it back.
//
// Stage (stage.go) put a file on this disk and wrote down what the signed
// release or catalogue said about it (state.Staged). This file is the step
// that makes that file the running program - and the step that undoes it.
//
// Two things decide the shape of everything below.
//
// The first is that the channel may carry NO user (core.WithoutUser on
// install). Then nothing of the request is signed, so nothing of the request
// decides: version and checksum are held against the row Stage wrote, a
// standing approval of the trust list has to cover the target, and the new
// version has to be newer than the installed one. With a signed task the
// params come out of the task; the same walls still run, because a task
// signed by an admin says what to install, not what the disk holds.
//
// The second is that the program on the disk must never be half changed. No
// step writes the name of a running program: the new bytes are copied to a
// temporary name, the old program is LINKED to its .prev name, and the swap is
// one os.Rename over the existing name. So wherever a program was running, the
// name it runs under holds one COMPLETE program at every instant of an install
// and of a rollback - the old one or the new one - never a fragment. There is
// no state in which half of the install and half of the rollback hold, because
// there is only one line that changes which program runs and a rename is not
// divisible (TestInstallKeepsOneWholeProgramAtEveryStep).
//
// The one exception is the FIRST install of a part, where no program was
// running: until the rename there is nothing under that name, and if the new
// one does not come up there is nothing to go back to either. That case
// answers ErrNothing from the rollback and says so rather than claiming
// something was restored (TestInstallOfAFirstPartHasNothingToRollBackTo).
package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

var (
	// ErrNothing says no update lies ready for this target, or none that
	// could be taken back. It is not a refusal of the peer and not a broken
	// file: there is nothing to do. No caller exists yet (api task 24 wires
	// main.go), and the answer meant for one is wire.CodeVerify, as for
	// ErrVerify: an API asking to install something this server never laid
	// ready is asking about a state it does not share.
	ErrNothing = errors.New("update: nothing lies ready for this target")
	// ErrNeedsGrants says the new version needs rights the app was not
	// granted. It travels joined with ErrForbidden - the install does not
	// happen - and is a state of its own in the report, so the shell offers
	// the activation instead of an error (store.UpdateNeedsGrants).
	ErrNeedsGrants = errors.New("update: the new version needs rights this app was not granted")
	// ErrHealth says the program that was put in place did not come up: it
	// names another version than the one staged, or its service did not stay
	// running. It is the one error that triggers a rollback.
	ErrHealth = errors.New("update: the installed version did not come up")
)

// aliveDefault is how long the restarted service has to stay up before the
// install counts as done. The brief names 30 seconds; a case may set its own
// (Deps.Alive), so one case leaves it alone and holds its order of magnitude
// (TestInstallDefaultAliveIsThirtySeconds).
const aliveDefault = 30 * time.Second

// Services starts and watches the systemd units of the NGC parts. It is an
// interface because none of it can run where this is written: systemctl needs
// root and a running systemd. Systemd is the one of operation.
type Services interface {
	Restart(ctx context.Context, unit string) error
	Running(ctx context.Context, unit string) (bool, error)
}

// Systemd is the Services of operation: /usr/bin/systemctl with an argument
// list, never a shell (CLAUDE.md, "Programme ohne Shell starten"). NOT
// measured: this machine has no systemd, so the cases run against a stand-in
// and these two methods are written and compiled, not executed.
type Systemd struct {
	// Bin is systemctl; empty is /usr/bin/systemctl.
	Bin string
}

func (s Systemd) bin() string {
	if s.Bin != "" {
		return s.Bin
	}
	return "/usr/bin/systemctl"
}

// Restart restarts the unit.
func (s Systemd) Restart(ctx context.Context, unit string) error {
	return exec.CommandContext(ctx, s.bin(), "restart", unit).Run()
}

// Running asks systemctl whether the unit is active. Only the word "active"
// counts: systemctl says "activating" while a unit is coming up and "failed"
// after it died, and neither of those is a service that runs. is-active ends
// with code 3 for a unit that is not active, which is an answer and not a
// failure - so the text decides and the code only where there is no text.
func (s Systemd) Running(ctx context.Context, unit string) (bool, error) {
	out, err := exec.CommandContext(ctx, s.bin(), "is-active", unit).Output()
	if err != nil && len(out) == 0 {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "active", nil
}

// VersionOf is the Deps.Health of operation: it runs the program itself with
// "-version" and gives the single line it prints. No shell, an argument list,
// and the output bounded - a program that was just put in place is the one
// thing of an install that may be broken, so nothing of what it says is
// believed beyond its first line.
//
// NOT measured: running a freshly installed ngc program needs that program,
// which this machine does not have. The cases drive Deps.Health directly.
func VersionOf(ctx context.Context, file string) (string, error) {
	out, err := exec.CommandContext(ctx, file, "-version").Output()
	if err != nil {
		return "", err
	}
	// The first line, and at most as much of it as a version can be: a
	// program that prints a megabyte must not grow this string.
	line := out
	if i := bytes.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	if len(line) > maxVersionLine {
		line = line[:maxVersionLine]
	}
	return strings.TrimSpace(string(line)), nil
}

// maxVersionLine bounds what a program may say about its own version. A
// release version is at most 20 bytes (store.ValidVersion: three numbers of up
// to six digits), and this is well over it so that a program naming something
// longer is seen to name something else rather than being cut to fit.
const maxVersionLine = 64

// AppInstaller sets a staged app package up, the way an activation does, with
// the grants of the app cut down to the needs of the new version. It is an
// interface for the same reason install.System is one - containers, system
// users and encrypted directories need root - and for a second one: Needs has
// to answer BEFORE anything is changed, because an update whose needs the app
// was not granted is refused and not installed (store.UpdateNeedsGrants).
//
// Its operation half is *install.Manager (install/update.go): install.Accept
// cannot be used on a staged file - it reads a transfer header and the signed
// catalogue envelope, and staging keeps neither - so the unpacking half of an
// acceptance was named (install.unpackApp) and is walked by both. There is no
// second unpacker and no second copy of a checked value; why the other way,
// carrying the catalogue's ui hash and needs in state.Staged, was dropped is
// written where the decision is carried out (install/update.go).
type AppInstaller interface {
	// Needs reads the needs of the manifest in the staged package file. It
	// changes nothing on this machine.
	Needs(ctx context.Context, file string) ([]string, error)
	// Update sets the package up and writes the row of the app. It reads the
	// manifest again and refuses unless Needs names what it finds there, so
	// the grants held below answer for the container that is really built.
	Update(ctx context.Context, in install.AppUpdate) error
}

// knownParts is every NGC part this file has a branch for. A part added to the
// contract without a branch here is caught by a case
// (TestInstallHandlesEveryNGCPart), not by a server.
var knownParts = []string{trust.AutoWeb, trust.AutoAPI, trust.AutoController}

// unitOf is the systemd unit of an NGC part, and false for a part that has
// none: ngc-web is files under a document root, which Apache serves and which
// no unit owns.
func unitOf(target string) (string, bool) {
	switch target {
	case trust.AutoAPI:
		return config.ServiceAPI, true
	case trust.AutoController:
		return config.ServiceController, true
	default:
		return "", false
	}
}

// Install installs what lies ready for the target of this channel.
//
// The order, and every wall in it stands before anything on this machine is
// touched:
//
//	1 the dependencies and the params - of the signed task where one came, of
//	  the OPEN otherwise
//	2 the row Stage wrote; without one in state store.UpdateDownloaded nothing
//	  lies ready (ErrNothing)
//	3 version and checksum of the params against that row, EXACTLY, because
//	  the params of a channel without a user are not signed
//	4 without a user: a standing approval for target and signer, and a newer
//	  version (core.Unattended)
//	5 never backwards - also for a signed task (store.Newer)
//	6 an app: the key of its OWN store against the one that signed the staged
//	  file, and the needs of the new version among the grants of the
//	  activation
//	6b an NGC part, in place of 6 - the two are the two branches of one
//	  fork: the roles of THIS server have to run that part (runs), so that
//	  nobody can tell an API server to become a web server
//	-- only now is anything on this machine changed -------------------------
//	7 the part is put in place and its service restarted, or the app is set
//	  up; a part that does not come up healthy is taken back (Rollback)
//	8 every stage reports store.UpdateReport
func (m *Manager) Install(ctx context.Context, d access.Decision) ([]byte, error) {
	if err := m.wired(); err != nil {
		return nil, err
	}
	p, err := m.installParams(d)
	if err != nil {
		// No report: a report has to name a target store.ParseUpdateReport
		// takes, and without the params there is none to name (stage.go).
		return nil, err
	}
	out, err := m.install(ctx, d, p)
	if err != nil {
		m.report(p.Target, p.Version, stateOf(err), message(err))
		return nil, err
	}
	return out, nil
}

// wired reports whether this Manager can install at all. It is the same answer
// for every call, because none of it can be brought about by a peer.
func (m *Manager) wired() error {
	if m == nil || m.d.State == nil || m.d.List == nil || m.d.Report == nil || m.d.Now == nil ||
		m.d.BinDir == "" || m.d.Services == nil || m.d.Health == nil || len(m.d.Roles) == 0 {
		return ErrInternal
	}
	return nil
}

// stateOf is the state the report carries for a failed install. A refused
// update is store.UpdateRejected; one the app may not have is
// store.UpdateNeedsGrants, a state of its own because nothing is wrong with
// the update; everything else failed on this machine.
func stateOf(err error) string {
	switch {
	case errors.Is(err, ErrNeedsGrants):
		return store.UpdateNeedsGrants
	// Before the three below, which this one travels joined with when a
	// rollback had nothing to go back to: the nearest wall is the one that
	// stopped the update, and a version that did not come up is a failure of
	// this machine and not a refusal.
	case errors.Is(err, ErrHealth), errors.Is(err, ErrDisk):
		return store.UpdateFailed
	case errors.Is(err, ErrVerify), errors.Is(err, ErrForbidden), errors.Is(err, ErrNothing):
		return store.UpdateRejected
	default:
		return store.UpdateFailed
	}
}

// installParams reads the params of this channel through the contract's own
// parser. With a task they are the task's, without one the OPEN's
// (access.Decision.Params); which of the two it was decides nothing here,
// because the row Stage wrote is held against them either way.
func (m *Manager) installParams(d access.Decision) (*core.InstallParams, error) {
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
	// trust.AutoSystem is task 21 (apt), not this call. It is refused here
	// and not left to fall through: for it p.Version and p.SHA256 are empty
	// (core.InstallParams.Validate), so every comparison below would compare
	// nothing with nothing.
	if p.Target == trust.AutoSystem {
		return nil, ErrForbidden
	}
	return p, nil
}

// install is Install without the report, so that every way out of it is
// reported in one place.
func (m *Manager) install(ctx context.Context, d access.Decision, p *core.InstallParams) ([]byte, error) {
	st, err := m.d.State.Staged(ctx, p.Target)
	switch {
	case err == nil:
	case errors.Is(err, dbapi.ErrNotFound):
		return nil, ErrNothing
	default:
		return nil, err
	}
	// The file has to be one that was checked and not yet installed: a row in
	// any other state names a file that is not ready to be installed, and a
	// row without a file names nothing at all.
	if st.State != store.UpdateDownloaded || st.File == "" {
		return nil, ErrNothing
	}
	// EXACTLY the staged version and checksum. This is the wall that makes an
	// install without a user safe at all: the params are not signed, the row
	// is what a verified signature produced.
	if p.Version != st.Version || !bytes.Equal(p.SHA256, st.SHA256) {
		return nil, ErrVerify
	}

	installed, err := m.installedVersion(ctx, p.Target)
	if err != nil {
		return nil, err
	}
	// Without a user: a standing approval for this target AND this signer,
	// and a newer version. Unattended sees neither the app's row nor the
	// catalogue, so its yes is the approval and the version alone.
	if d.User == "" && !core.Unattended(p, m.d.List, st.Signer, installed) {
		return nil, ErrForbidden
	}
	// Never backwards, and this line runs for a signed task as well: an admin
	// may sign anything, and an older program is not an update. It is a
	// second floor under Unattended for the channel without a user, and the
	// ONLY wall of its kind for the channel with a task
	// (TestInstallNeverGoesBackwards names which of the two answers).
	//
	// An empty installed version means nothing of this part is installed at
	// all, and then every version is newer. store.Newer does NOT say that -
	// it answers false for a version that is no version, and "" is none - so
	// the case is guarded here. core.Unattended makes the same comparison
	// with store.Newer and therefore refuses the FIRST install of a part on
	// a channel without a user; that one is reported as BLOCKED, because the
	// line that would have to change is in shared/.
	if installed != "" && !store.Newer(st.Version, installed) {
		return nil, errors.Join(ErrForbidden, store.ErrRollback)
	}

	if slices.Contains(knownParts, p.Target) {
		if !m.runs(p.Target) {
			return nil, ErrForbidden
		}
		return emptyResult, m.installPart(ctx, p.Target, st)
	}
	return emptyResult, m.installApp(ctx, st)
}

// runs reports whether an NGC part belongs on THIS server, out of the roles
// this server carries (Deps.Roles, config.Controller.Roles).
//
// tunnel.Components is the mapping, and it is the one the API's rollout picks
// its nodes with as well (apps/server-api/classes/updates/rollout.go,
// nodesFor). So this is not a second table that could drift from the sending
// side: there is one function, and both sides ask it. What the roles of this
// server are is likewise held in one place - the configuration - and carried,
// not re-derived.
//
// It is asked only for the three parts of knownParts, because only those are
// installed here as programs. An app is not asked, and does not go unchecked
// either: an app that wants containers is refused against the same roles
// where its container is built (install/activate.go and install/update.go,
// tunnel.RoleServices) - and install/update.go is in the path this file takes
// (installApp -> Apps.Update).
//
// NOT covered, and named rather than claimed: the role of a server with
// RoleDB. The API lays the ngc-api FILE ready on such a node so that it can
// play the migrations in, and laying ready is Stage - this wall sits at the
// install and does not touch that.
func (m *Manager) runs(part string) bool {
	return slices.Contains(tunnel.Components(m.d.Roles), part)
}

// installedVersion is the version of the target running on this server: out of
// the parts this controller wrote down (state.Store.Components) for an NGC
// part, out of the row of the app otherwise. An empty answer means nothing of
// it is installed, and store.Newer takes every version as newer than "".
func (m *Manager) installedVersion(ctx context.Context, target string) (string, error) {
	if ngcPart(target) {
		parts, err := m.components(ctx)
		if err != nil {
			return "", err
		}
		return parts[target], nil
	}
	a, err := m.d.State.App(ctx, target)
	switch {
	case err == nil:
		return a.Version, nil
	case errors.Is(err, dbapi.ErrNotFound):
		// An update is an update of something; an app this controller does
		// not run is not installed by this call - the same answer Stage
		// gives for one.
		return "", ErrForbidden
	default:
		return "", err
	}
}

// components are the installed NGC parts, with nothing written yet answered as
// an empty list rather than as an error.
func (m *Manager) components(ctx context.Context) (map[string]string, error) {
	parts, err := m.d.State.Components(ctx)
	switch {
	case err == nil:
		if parts == nil {
			parts = map[string]string{}
		}
		return parts, nil
	case errors.Is(err, dbapi.ErrNotFound):
		return map[string]string{}, nil
	default:
		return nil, err
	}
}

// installApp checks what only this controller knows about the app - the store
// it was activated from and the rights it was granted - and then hands the
// staged file to the installer.
func (m *Manager) installApp(ctx context.Context, st state.Staged) error {
	if m.d.Apps == nil {
		return ErrInternal
	}
	a, err := m.d.State.App(ctx, st.Target)
	if err != nil {
		// Not found was answered above (installedVersion), so anything here
		// is this server's own trouble.
		return err
	}
	// The store comes out of this controller's OWN row of the app, and the
	// key of that store has to be the one whose signature the staged file was
	// accepted under. Stage held the row against the header of the transfer;
	// this holds it against the signature that was actually verified, so a
	// file signed by another store's key is refused here even though it was
	// staged - which is what happens when the trust list changes between the
	// two calls.
	if a.Store == "" {
		return ErrVerify
	}
	i := slices.IndexFunc(m.d.List.Stores, func(s trust.Store) bool { return s.ID == a.Store })
	if i < 0 || len(st.Signer) == 0 || !bytes.Equal(m.d.List.Stores[i].Key, st.Signer) {
		return ErrVerify
	}
	needs, err := m.d.Apps.Needs(ctx, st.File)
	if err != nil {
		return err
	}
	// The grants are the ones this controller wrote into its own database
	// when the app was activated, never a value an API sends it: an API that
	// has been taken over can then not widen what an app may do
	// (store.Granted).
	if !store.Granted(needs, a.Grants) {
		return errors.Join(ErrForbidden, ErrNeedsGrants)
	}
	m.report(st.Target, st.Version, store.UpdateInstalling, "")
	if err := m.d.Apps.Update(ctx, install.AppUpdate{App: st.Target, Version: st.Version,
		SHA256: st.SHA256, File: st.File, Needs: needs}); err != nil {
		return err
	}
	if err := m.mark(ctx, st, store.UpdateInstalled); err != nil {
		return err
	}
	m.report(st.Target, st.Version, store.UpdateInstalled, "")
	return nil
}

// report sends one stage of an update to the API.
func (m *Manager) report(target, version, st, msg string) {
	m.d.Report.Update(store.UpdateReport{Target: target, Version: version, State: st, Message: msg})
}

// mark writes the state of the staged row. It is the one thing about an
// install that survives a restart of this program, and that is what it is
// for: a controller that restarts into its own new version finds
// store.UpdateInstalling here and knows it has to confirm or take back
// (Resume).
func (m *Manager) mark(ctx context.Context, st state.Staged, s string) error {
	st.State = s
	st.Time = m.d.Now()
	return m.d.State.PutStaged(ctx, st)
}

// partFile is where an NGC part runs from.
func (m *Manager) partFile(target string) string { return m.d.BinDir + "/" + target }

// alive is how long the service has to stay up after the restart.
func (m *Manager) alive() time.Duration {
	if m.d.Alive > 0 {
		return m.d.Alive
	}
	return aliveDefault
}

// installPart puts an NGC part in place and makes sure it came up.
//
// The three steps that touch the disk are in this order on purpose:
//
//	a the new bytes go to a TEMPORARY name under BinDir - never the name of a
//	  program, so nothing can start a file that is still being written, and
//	  under BinDir and not StageDir because the swap below has to be a rename
//	  inside one directory and the two need not be one filesystem
//	b the old program is LINKED to <file>.prev - a link and not a move, so the
//	  name that runs never stops existing
//	c one os.Rename of the temporary name over <file> - the whole of the
//	  change, and not divisible
//
// Then the service is restarted, the new program is asked for its version, and
// it has to be running still after Alive. Anything that fails from (c) on is
// taken back by Rollback; anything that fails before it changed nothing.
func (m *Manager) installPart(ctx context.Context, target string, st state.Staged) error {
	if target == trust.AutoWeb {
		return m.installWeb(ctx, st)
	}
	unit, ok := unitOf(target)
	if !ok {
		// A part of knownParts without a branch here. It cannot be reached
		// from outside - the contract knows three parts and all three have
		// one - and a case holds that (TestInstallHandlesEveryNGCPart).
		return ErrInternal
	}
	file := m.partFile(target)
	if err := os.MkdirAll(m.d.BinDir, 0o755); err != nil {
		return asDisk(err)
	}
	tmp, err := copyInto(m.d.BinDir, st.File)
	if err != nil {
		return err
	}
	swapped := false
	defer func() {
		if !swapped {
			os.Remove(tmp)
		}
	}()
	// A .prev of an earlier install is not a second chance, it is the wrong
	// program: the one to go back to is the one that runs now.
	if err := os.Remove(file + ".prev"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return asDisk(err)
	}
	// Not found is the first install of this part on this server: there is
	// nothing to go back to, and that is no error. A failure after the swap
	// then answers ErrNothing from Rollback, which is reported as it is -
	// nothing is claimed to have been restored.
	if err := os.Link(file, file+".prev"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return asDisk(err)
	}

	// Before the swap, because a controller that installs ITSELF does not
	// come back into this call: the marker and the report have to be out
	// first, and the marker is what Resume reads on the next start.
	if err := m.mark(ctx, st, store.UpdateInstalling); err != nil {
		return err
	}
	m.report(target, st.Version, store.UpdateInstalling, "")

	if err := os.Rename(tmp, file); err != nil {
		return asDisk(err)
	}
	swapped = true

	if target == trust.AutoController {
		// This program is the one being replaced. The restart does not return
		// here and nothing after it would run; whether the new version is
		// good is decided by Resume on the next start, out of the marker
		// written above. A restart that FAILS does return, and then the old
		// program is still the one running - so it is taken back at once.
		if err := m.d.Services.Restart(ctx, unit); err != nil {
			return m.rolledBack(ctx, target, st, err)
		}
		return nil
	}
	if err := m.healthy(ctx, target, unit, st.Version); err != nil {
		return m.rolledBack(ctx, target, st, err)
	}
	return m.confirm(ctx, target, st)
}

// healthy restarts the unit and makes sure the new program is the one that
// answers: it has to name the staged version, and the service has to be up
// still after Alive. A version the program does not confirm is not this
// update, whatever the file is called.
func (m *Manager) healthy(ctx context.Context, target, unit, version string) error {
	if err := m.d.Services.Restart(ctx, unit); err != nil {
		return errors.Join(ErrHealth, err)
	}
	got, err := m.d.Health(ctx, m.partFile(target))
	if err != nil {
		return errors.Join(ErrHealth, err)
	}
	if got != version {
		return ErrHealth
	}
	m.sleep(m.alive())
	running, err := m.d.Services.Running(ctx, unit)
	if err != nil {
		return errors.Join(ErrHealth, err)
	}
	if !running {
		return ErrHealth
	}
	return nil
}

// sleep waits. Deps.Sleep lets a case run without the wait; nil is the real
// clock. The one case that leaves Deps.Alive alone holds the default
// (TestInstallDefaultAliveIsThirtySeconds), so the value nothing overrides is
// not the one value nothing checks.
func (m *Manager) sleep(d time.Duration) {
	if m.d.Sleep != nil {
		m.d.Sleep(d)
		return
	}
	time.Sleep(d)
}

// confirm writes down that this part now runs in this version and reports it.
func (m *Manager) confirm(ctx context.Context, target string, st state.Staged) error {
	if err := m.putComponent(ctx, target, st.Version); err != nil {
		return err
	}
	if err := m.mark(ctx, st, store.UpdateInstalled); err != nil {
		return err
	}
	m.report(target, st.Version, store.UpdateInstalled, "")
	return nil
}

// rolledBack takes the install back and keeps the reason: a rollback that
// works is still a failed update, so the caller sees the error of the install,
// and the report of the rollback says store.UpdateRolledBack.
func (m *Manager) rolledBack(ctx context.Context, target string, st state.Staged, cause error) error {
	if err := m.Rollback(ctx, target); err != nil {
		// Both went wrong, and that is the state an admin has to hear about:
		// the new program is in place and did not come up. ErrNothing among
		// them says there was no previous program to go back to - the first
		// install of this part - and that is reported as it is rather than
		// swallowed.
		return errors.Join(cause, err)
	}
	// A failure here loses only the marker, and the error of the install is
	// the one that matters: the program that runs is the old one again, which
	// is what the row would have said.
	_ = m.mark(ctx, st, store.UpdateRolledBack)
	return cause
}

// Rollback puts the program that ran before target's update back and starts
// its service again.
//
// It is the same single rename the install is: <file>.prev over <file>. So
// there is no instant at which half of the install and half of the rollback
// hold - before the rename the new program is in place, after it the old one
// is, and in between there is one indivisible step.
//
// Without a .prev there is nothing to go back to and ErrNothing says so. That
// is the honest answer for the first install of a part and for a second
// rollback: the one program to go back to is the one that ran, and after a
// rollback that is the one in place.
func (m *Manager) Rollback(ctx context.Context, target string) error {
	if err := m.wired(); err != nil {
		return err
	}
	if !slices.Contains(knownParts, target) {
		// An app is not a file this call swaps: its container, its user and
		// its data are set up by the installer, and taking that back is that
		// installer's own undo (install.setUp).
		return ErrForbidden
	}
	if target == trust.AutoWeb {
		return m.rollbackWeb(ctx)
	}
	unit, ok := unitOf(target)
	if !ok {
		return ErrInternal
	}
	file := m.partFile(target)
	prev := file + ".prev"
	if _, err := os.Lstat(prev); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNothing
		}
		return asDisk(err)
	}
	if err := os.Rename(prev, file); err != nil {
		return asDisk(err)
	}
	if err := m.d.Services.Restart(ctx, unit); err != nil {
		return err
	}
	// The version of the program that is back, asked of the program itself
	// and not read off any row: what runs is what answers.
	version, err := m.d.Health(ctx, file)
	if err != nil {
		return err
	}
	if err := m.putComponent(ctx, target, version); err != nil {
		return err
	}
	m.report(target, version, store.UpdateRolledBack,
		"Zurückgenommen: Die neue Fassung startete nicht, die vorige läuft wieder.")
	return nil
}

// putComponent writes one part's installed version, or takes it out of the
// list when the program names none: a version that is no release version is
// not kept (state.validComponents), and a part whose version is unknown must
// not keep the one of the update that failed.
func (m *Manager) putComponent(ctx context.Context, target, version string) error {
	parts, err := m.components(ctx)
	if err != nil {
		return err
	}
	if store.ValidVersion(version) {
		parts[target] = version
	} else {
		delete(parts, target)
	}
	return m.d.State.PutComponents(ctx, parts)
}

// Resume decides an install that a restart of this program interrupted. It is
// MEANT to run at every start of the controller, before anything else uses the
// updater - and nothing calls it yet, because no main.go wires these packages
// up (docs/STAND.md: api task 24). So what is measured here is what Resume
// does when it is called, not that it is.
//
// The marker is the whole of it: a row of this part in store.UpdateInstalling
// means the program that wrote it restarted itself into the new version and
// never got to confirm. If the program now running names that version, it came
// up and the install is confirmed. If it names another, the update is taken
// back.
func (m *Manager) Resume(ctx context.Context) error {
	if err := m.wired(); err != nil {
		return err
	}
	st, err := m.d.State.Staged(ctx, trust.AutoController)
	switch {
	case err == nil:
	case errors.Is(err, dbapi.ErrNotFound):
		return nil
	default:
		return err
	}
	if st.State != store.UpdateInstalling {
		return nil
	}
	got, err := m.d.Health(ctx, m.partFile(trust.AutoController))
	if err != nil {
		return err
	}
	if got == st.Version {
		return m.confirm(ctx, trust.AutoController, st)
	}
	// ErrNothing here means the update was the first install of this part, so
	// there is no earlier program: the marker is still set to rolled back,
	// because this version is not confirmed and must not be tried again.
	if err := m.Rollback(ctx, trust.AutoController); err != nil && !errors.Is(err, ErrNothing) {
		return err
	}
	if err := m.mark(ctx, st, store.UpdateRolledBack); err != nil {
		return err
	}
	m.report(trust.AutoController, st.Version, store.UpdateRolledBack,
		"Zurückgenommen: Nach dem Neustart lief die neue Fassung nicht.")
	return nil
}

// installWeb unpacks the web interface into config.WebRoot and keeps the stand
// before it as <WebRoot>.prev.
//
// Unlike a part this is a directory, and two directories cannot be swapped by
// one rename. So there IS an instant in which WebRoot does not exist, between
// the two renames below, and a request in that instant is answered with 404.
// That gap is measured and named here rather than claimed away; it cannot be
// closed without a symbolic link, which this installation does not use. What
// cannot happen is a WebRoot holding half of each stand: the new one is
// unpacked beside it in full and then moved as a whole
// (TestInstallWebMovesAWholeTreeOrNone).
func (m *Manager) installWeb(ctx context.Context, st state.Staged) error {
	if m.d.WebRoot == "" {
		return ErrInternal
	}
	next := m.d.WebRoot + ".next"
	prev := m.d.WebRoot + ".prev"
	if err := os.RemoveAll(next); err != nil {
		return asDisk(err)
	}
	if err := m.unpack(st.File, next); err != nil {
		os.RemoveAll(next)
		return err
	}
	if err := m.mark(ctx, st, store.UpdateInstalling); err != nil {
		os.RemoveAll(next)
		return err
	}
	m.report(trust.AutoWeb, st.Version, store.UpdateInstalling, "")
	if err := os.RemoveAll(prev); err != nil {
		return asDisk(err)
	}
	if err := os.Rename(m.d.WebRoot, prev); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return asDisk(err)
	}
	if err := os.Rename(next, m.d.WebRoot); err != nil {
		// The document root is gone and the new one did not get there. The
		// stand before it is whole under .prev, so it goes back at once
		// rather than being left for an admin to find.
		_ = os.Rename(prev, m.d.WebRoot)
		return asDisk(err)
	}
	return m.confirm(ctx, trust.AutoWeb, st)
}

// rollbackWeb puts the stand before the last web update back. The same gap as
// in installWeb applies and for the same reason.
func (m *Manager) rollbackWeb(ctx context.Context) error {
	if m.d.WebRoot == "" {
		return ErrInternal
	}
	prev := m.d.WebRoot + ".prev"
	if _, err := os.Lstat(prev); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNothing
		}
		return asDisk(err)
	}
	gone := m.d.WebRoot + ".gone"
	if err := os.RemoveAll(gone); err != nil {
		return asDisk(err)
	}
	if err := os.Rename(m.d.WebRoot, gone); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return asDisk(err)
	}
	if err := os.Rename(prev, m.d.WebRoot); err != nil {
		_ = os.Rename(gone, m.d.WebRoot)
		return asDisk(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		return asDisk(err)
	}
	// Which version lies there now is not known: the stand before the update
	// is a directory of files and names no version anywhere this call reads.
	// So the part is taken out of the list rather than given a version that
	// was guessed.
	if err := m.putComponent(ctx, trust.AutoWeb, ""); err != nil {
		return err
	}
	m.report(trust.AutoWeb, "", store.UpdateRolledBack,
		"Zurückgenommen: Der vorige Stand des Webinterface liegt wieder.")
	return nil
}

// unpack writes the files of a web release into dir. The rules of package
// store hold: no absolute path, no "..", no link, no sparse file and bounded
// sizes - store.WalkUI is that check, and nothing here repeats it.
//
// BLOCKED, and the report of task 20 says it too: NOTHING in shared/ says what
// a release file of ngc-web is. config.WebRoot is named in no other file of
// this repo, and the only safe unpack the contract offers for a surface is
// store.WalkUI, whose names have to be rooted at "ui/" (store.uiName). So this
// build takes an ngc-web release file to be such a tar and strips that one
// prefix. If the release of ngc-web is to be a plain tar instead, shared/
// needs the function that unpacks one safely - this package must not grow a
// second unpacker of its own (CLAUDE.md).
func (m *Manager) unpack(file, dir string) error {
	f, err := os.Open(file)
	if err != nil {
		return asDisk(err)
	}
	defer f.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return asDisk(err)
	}
	// A failure of the disk and a refused archive must not get the same
	// answer, and store.WalkUI gives one error for both: so the disk's own
	// error is kept here and decides.
	var diskErr error
	if err := store.WalkUI(f, func(e store.PackFile, body io.Reader) error {
		// The path came through store.WalkUI, so it is rooted at "ui/", is
		// relative and has no step upwards; the prefix is what the document
		// root replaces. It is joined with slashes only, so nothing of a
		// platform's own spelling gets in.
		rel, ok := strings.CutPrefix(e.Path, "ui/")
		if !ok || rel == "" {
			// store.WalkUI hands out nothing else, so this is unreachable
			// from outside; it is here because the line above depends on it.
			return store.ErrPackage
		}
		path := dir + "/" + rel
		if i := strings.LastIndexByte(rel, '/'); i > 0 {
			if err := os.MkdirAll(dir+"/"+rel[:i], 0o755); err != nil {
				diskErr = err
				return err
			}
		}
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			diskErr = err
			return err
		}
		if _, err := io.Copy(out, body); err != nil {
			out.Close()
			diskErr = err
			return err
		}
		if err := out.Close(); err != nil {
			diskErr = err
			return err
		}
		return nil
	}); err != nil {
		if diskErr != nil {
			return asDisk(diskErr)
		}
		// The archive is not the one that was signed, or is no archive.
		return errors.Join(ErrVerify, err)
	}
	return nil
}

// copyInto copies src into dir under a temporary name and gives that name.
// Temporary and never the name of a program, for the same reason a staged file
// gets its name by a rename (stage.go): nothing may be able to start a file
// that is still being written.
func copyInto(dir, src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", asDisk(err)
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, "install-")
	if err != nil {
		return "", asDisk(err)
	}
	name := out.Name()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(name)
		return "", asDisk(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(name)
		return "", asDisk(err)
	}
	// 0755 and not the mode of the staged file: this one is started, while
	// the staged file is deliberately 0600 (stage.go). NOT measured at all:
	// no case of this package reads the mode, because Windows does not carry
	// it, so a mutant that writes another mode here would live. stage.go has
	// the same gap and a case that skips loudly; this file has neither.
	if err := os.Chmod(name, 0o755); err != nil {
		os.Remove(name)
		return "", asDisk(err)
	}
	return name, nil
}
