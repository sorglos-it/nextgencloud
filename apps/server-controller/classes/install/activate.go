// Activating an app: the one call that turns a checked package into a running
// container and a row in this controller's database.
//
// Everything it decides, it decides out of the signed task - never out of the
// OPEN of the channel and never out of the package. The package says what the
// app needs (store.Manifest.Needs); the task says what an admin allowed
// (core.ActivateParams.Grants), and the two have to be the same set, or nothing
// is set up at all. That is why the order below is what it is: every refusal
// stands before the first system user is made.
//
// What it sets up, it can take back. Each step of the setup remembers how to
// undo itself, and a failure after it runs the undos in reverse - with one
// exception that is named at the place it happens: the container of the version
// that was installed before is gone once it was removed, and no undo here can
// bring it back. The row of that version stays, so nothing says the new version
// runs when it does not.
package install

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

var (
	// ErrForbidden says the task asked for something this installation does
	// not allow: grants that are not the needs of the package, services on a
	// server without the role services, or a version older than the one
	// installed. A caller maps it to wire.CodeForbidden (E_FORBIDDEN).
	ErrForbidden = errors.New("install: this activation is not allowed")
	// ErrLicense says the paid app brought no licence this installation can
	// use, or one that has run out. A caller maps it to wire.CodeLicense
	// (E_LICENSE).
	ErrLicense = errors.New("install: no usable licence for this app")
	// ErrInternal says this program is wired wrong - a missing dependency, an
	// action this NGC does not know. None of it can be brought about by a
	// peer, and a caller maps it to wire.CodeInternal (E_INTERNAL).
	ErrInternal = errors.New("install: the installer is not wired up")
)

// Apps is the part of this controller's local store an activation touches
// (apps/server-controller/classes/state.Store).
type Apps interface {
	App(ctx context.Context, id string) (dbapi.App, error)
	PutApp(ctx context.Context, a dbapi.App) error
	License(ctx context.Context, app string) (sign.Envelope, int64, error)
	PutLicense(ctx context.Context, app string, env sign.Envelope, issued int64) error
}

// System is everything of an activation that changes this machine: the system
// user of the app, its encrypted data directory and the runtime directory of
// its container. It is an interface and not three calls of package runtime
// because every one of them needs root and two of them need a kernel that can
// encrypt a directory - so without a stand-in nothing after them could be
// measured where this is written. Host is the one of operation.
type System interface {
	EnsureUser(ctx context.Context, app string) (runtime.User, error)
	DropUser(ctx context.Context, app string) error
	EnsureData(ctx context.Context, app string, u runtime.User) (string, error)
	DropData(ctx context.Context, app string) error
	EnsureRun(ctx context.Context, u runtime.User) (string, error)
	DropRun(name string) error
}

// Host is the System of operation: it is package runtime, with the key of this
// server (config.Controller.DBKey) which the data directory is encrypted from.
// The key never leaves this struct, and nothing here writes it anywhere.
type Host struct{ ServerKey []byte }

func (h Host) EnsureUser(ctx context.Context, app string) (runtime.User, error) {
	return runtime.EnsureUser(ctx, app)
}
func (h Host) DropUser(ctx context.Context, app string) error { return runtime.DropUser(ctx, app) }
func (h Host) EnsureData(ctx context.Context, app string, u runtime.User) (string, error) {
	return runtime.EnsureData(ctx, app, u, h.ServerKey)
}
func (h Host) DropData(ctx context.Context, app string) error { return runtime.DropData(ctx, app) }
func (h Host) EnsureRun(ctx context.Context, u runtime.User) (string, error) {
	return runtime.EnsureRun(ctx, u)
}
func (h Host) DropRun(name string) error { return runtime.DropRun(name) }

// Deps are what a Manager needs. What is nil or empty of it AND read by the
// guard of Activate is this program wired wrong and ends every activation
// with ErrInternal: none of it can be brought about by a peer, so it is
// answered once and the same way. The guard reads nine of these
// fields (State, Engine, Sys, List, StageDir, Max, LastSeq, KeepSeq, Now -
// counted 18.10.2026); Roles, Node, NGC and ReleaseKey empty go through it, and an
// empty Hold is filled on purpose (Manager.hold).
type Deps struct {
	// State is this controller's local store: the row of the app and the
	// licence it keeps.
	State Apps
	// Engine runs the containers.
	Engine runtime.Engine
	// Sys changes this machine (System). Host in operation.
	Sys System
	// List is the installation's own trust list, the only place a store key
	// may come from.
	List *trust.List
	// Roles are this server's roles (config.Controller.Roles). An app with
	// services needs tunnel.RoleServices here.
	Roles []string
	// Node is this controller, as it stands in the trust list; it goes into
	// the row of the app.
	Node string
	// NGC is this NGC's version, held against the minNgc of the package.
	NGC string
	// ReleaseKey is this installation's own release key. An activation never
	// takes a release, so it is here only because Accept asks for it.
	ReleaseKey []byte
	// StageDir is where a package is staged while it is checked
	// (config.StageDir). The folder of this activation is removed again
	// afterwards, whether it worked or not.
	StageDir string
	// Hold is the program that keeps a container alive, under config.BinDir
	// (runtime.Container.Hold). Empty: config.BinDir + "/ngc-controller".
	Hold string
	// Max bounds the package file. It has no default on purpose: a package of
	// n bytes costs 2n on the disk while it is checked (StageFactor), so
	// store.MaxPackage here means 8 GiB of config.StageDir. Whatever stands
	// here, every activation cuts it down to what the disk really has free
	// (bound), and a Manager without it answers ErrInternal rather than let a
	// peer fill the disk.
	Max int64
	// LastSeq is the newest catalogue number already seen from a store. It is
	// what keeps a store from handing back a catalogue older than one already
	// acted on (store.VerifyCatalog, "c.Seq < lastSeq").
	LastSeq func(store string) uint64
	// KeepSeq writes that number down again after an activation out of that
	// store worked. It is the OTHER HALF of LastSeq and required for the same
	// reason: a bar that nothing ever raises is read as 0 for ever, and a
	// guard without memory is no guard. An activation of an uploaded app
	// (core.LocalStore) comes with no catalogue and never reaches it.
	KeepSeq func(ctx context.Context, store string, seq uint64) error
	// Log is where the one line of a failed setup goes (Manager.why): the
	// step it stopped at and the kind of the cause, both out of a closed
	// list. nil is slog.Default(), which under systemd is the journal of this
	// service - so an installation that wires nothing still says why.
	Log *slog.Logger
	// Now is the clock the licence is held against, in seconds. nil is this
	// program wired wrong as well: a licence checked against the zero time
	// never runs out.
	Now func() int64
}

// Manager carries out the actions that install and remove apps.
type Manager struct{ d Deps }

// New returns the manager of these dependencies.
func New(deps Deps) *Manager { return &Manager{d: deps} }

// emptyResult is the result of an activation: the contract gives it no fields
// (core.ActivateParams has no result type of its own), and an empty object is
// what a caller encodes.
var emptyResult = []byte("{}")

// Activate installs an app and starts it. The reader carries the transfer
// header and the package, as the API sends them.
//
// The order is the whole of it:
//
//	 1 the parameters out of the SIGNED task, never out of the OPEN
//	 2 the row of the app: an older version is refused here, before a byte
//	   of the package is read
//	 3 Accept: checksum, signature, catalogue, rollback, minNgc, and the
//	   files of the package (package.go)
//	 4 grants == needs as a set
//	 5 services only with the role services
//	 6 the licence of a paid app is CHECKED, before anything is set up
//	-- only now is this machine changed, and every step can be undone -----
//	 7 system user -> data directory -> runtime directory
//	 8 image into the store of that user
//	 9 the old container away, the new one created and started
//	10 the row of the app, with its grants, and the checked licence kept
//	11 the start action of the manifest - task 18, not carried out here
func (m *Manager) Activate(ctx context.Context, d access.Decision, r io.Reader) ([]byte, error) {
	if m == nil || m.d.State == nil || m.d.Engine == nil || m.d.Sys == nil || m.d.List == nil ||
		m.d.StageDir == "" || m.d.Max <= 0 || m.d.LastSeq == nil || m.d.KeepSeq == nil ||
		m.d.Now == nil || r == nil {
		return nil, ErrInternal
	}
	// A root task is what this action is: without one nothing names an app, a
	// version and a checksum that an admin signed. access decides whether the
	// task is one; that it is there at all is held here, because everything
	// below reads it.
	if d.Task == nil {
		return nil, ErrForbidden
	}
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	if !ok {
		return nil, ErrInternal
	}
	params, err := core.ParseParams(a, d.Task.Params)
	if err != nil {
		// The parameters of a signed task that do not parse are no refusal of
		// an admin's wish: they are a task that says something else than this
		// NGC can read, and that is a verification failure.
		return nil, errors.Join(ErrVerify, err)
	}
	p, ok := params.(*core.ActivateParams)
	if !ok {
		return nil, ErrInternal
	}

	// The installed row before the package: a rollback is refused without the
	// API ever sending the file, and an update keeps what the row carries.
	old, err := m.d.State.App(ctx, p.App)
	switch {
	case err == nil:
	case errors.Is(err, dbapi.ErrNotFound):
		old = dbapi.App{}
	default:
		return nil, err
	}
	// Version empty means only kept data is left (dbapi.App), so there is
	// nothing to roll back from.
	installed := old.Version != ""
	if installed && old.Version != p.Version && !store.Newer(p.Version, old.Version) {
		return nil, ErrForbidden
	}

	// What may be written while the package is checked: the bound of the
	// configuration, cut down to what the disk of this server carries twice
	// over (space.go). It is asked here and not in Deps, because the disk
	// answers differently from one activation to the next.
	max, err := bound(m.d.StageDir, m.d.Max)
	if err != nil {
		return nil, err
	}
	pkg, err := Accept(ctx, AcceptInput{
		Reader: r, Max: max, List: m.d.List, Store: p.Store, App: p.App,
		Version: p.Version, SHA256: p.SHA256, UI: p.UI, NGC: m.d.NGC,
		Dir: m.d.StageDir, ReleaseKey: m.d.ReleaseKey, LastSeq: m.d.LastSeq,
	})
	if err != nil {
		return nil, err
	}
	// The staged folder is the package file and its files, up to
	// store.MaxPackage of them. It is gone when this call is - the image is in
	// the store of the app by then, and the ui-tar is the API's to keep
	// (dbapi.BlobUI).
	defer os.RemoveAll(pkg.Dir)
	if pkg.Manifest == nil {
		// A release carries no manifest and is no app (package.go, stage).
		// Nothing of it may be activated, and nil here is that case and not a
		// half-finished answer.
		return nil, ErrVerify
	}
	mf := pkg.Manifest

	// Grants have to be the needs of this package as a set - not a superset
	// and not a subset. A grant the package does not need is a right an admin
	// signed for nothing, and a need without its grant is a container that
	// would run with less than the app asked for.
	if !sameSet(p.Grants, mf.Needs) {
		return nil, ErrForbidden
	}
	if len(mf.Services) > 0 && !slices.Contains(m.d.Roles, tunnel.RoleServices) {
		return nil, ErrForbidden
	}

	// The licence is CHECKED before the setup, not before the start: a paid
	// app whose licence has run out sets nothing up at all. It is KEPT at the
	// end of the setup (keepLicence), because a write here would stand in
	// front of the line below and no rollback would take it back.
	lic, err := m.licence(d, p, pkg)
	if err != nil {
		return nil, err
	}

	set := appSetup{app: p.App, version: p.Version, store: p.Store, sha256: p.SHA256,
		grants: p.Grants, files: filepath.Join(pkg.Dir, FilesDir), seq: pkg.Seq}
	if err := m.setUp(ctx, set, mf, old, installed, pkg.Transfer.License, lic); err != nil {
		return nil, err
	}
	// The start action of the manifest (store.Manifest.Start) runs after every
	// start of the container. It belongs to task 18, which runs actions at
	// all; nothing here calls a service, and an app whose Start is named by a
	// manifest this build installed therefore does not get it yet.
	return emptyResult, nil
}

// appSetup is what the row of the app will say and where the files of this
// version lie. Activate fills it out of the SIGNED task; installing an update
// fills it out of the row of the app and the manifest of the new version
// (update.go). One setup with two callers, so nothing about what is set up can
// differ between an activation and an update - and the one thing that does
// differ, where the grants come from, is a field and not a second function.
type appSetup struct {
	app     string
	version string
	store   string
	sha256  []byte
	grants  []string
	// files is the directory unpackApp wrote the package's files into.
	files string
	// seq is the number of the catalogue this package came out of
	// (install.Package.Seq), 0 where no catalogue named it - an uploaded app,
	// and an update, which reads the row and no catalogue (update.go). Only a
	// number over 0 is ever written down (Manager.keepSeq).
	seq uint64
}

// undo is one step of the setup taken back.
type undo struct {
	what string
	run  func()
}

// setUp changes this machine, and takes every step back that it took if a
// later one fails.
func (m *Manager) setUp(ctx context.Context, p appSetup, mf *store.Manifest,
	old dbapi.App, installed bool, env *sign.Envelope, lic *store.License) error {
	var undos []undo
	ok := false
	defer func() {
		if ok {
			return
		}
		// In reverse: the runtime directory before the data directory before
		// the user, because the later ones were made on top of the earlier.
		for i := len(undos) - 1; i >= 0; i-- {
			undos[i].run()
		}
	}()

	u, err := m.d.Sys.EnsureUser(ctx, p.app)
	if err != nil {
		return m.why(p.app, stepUser, err)
	}
	if !installed {
		// Only for an app that was not installed. An update must never take
		// the user of the app away on a failure: the data directory belongs to
		// it, and a directory whose owner is gone belongs to a number that the
		// next user to get it would inherit (runtime.DropUser).
		undos = append(undos, undo{"user", func() { _ = m.d.Sys.DropUser(ctx, p.app) }})
	}
	dataDir, err := m.d.Sys.EnsureData(ctx, p.app, u)
	if err != nil {
		return m.why(p.app, stepData, err)
	}
	if !installed {
		// And for the same reason the data of an app that was installed is
		// never dropped here: an update keeps the data (core.ActivateParams).
		undos = append(undos, undo{"data", func() { _ = m.d.Sys.DropData(ctx, p.app) }})
	}
	// An app without services has no container, and it cannot have one: a
	// package carries an image exactly when its manifest names a service
	// (store.Walk refuses the other two combinations), and a manifest is
	// allowed to name a UI and no service at all (store.Manifest.Validate).
	// Such an app is its row, its user and its data directory, and nothing
	// below this line applies to it. The brief of this task names the
	// container path only; this is the case it does not mention, and it is
	// measured (TestActivateTakesNoContainerForAnAppWithoutServices).
	if len(mf.Services) == 0 {
		// The version before this one may well have had services, and then
		// its container is still there and running. An app that no longer has
		// one must not keep it: nothing would ever stop it again, because
		// every later call of this controller reads the manifest of the row.
		if running, err := m.d.Engine.Running(ctx, u.Name); err == nil && running {
			_ = m.d.Engine.Stop(ctx, u.Name)
		}
		_ = m.d.Engine.Remove(ctx, u.Name)
		_ = m.d.Sys.DropRun(u.Name)
		// The image of the version before stays where it is, and that is
		// named rather than fixed: the reference an image has in the store of
		// the app is read out of the tar of the package it came with
		// (runtime.ImageTag), and this activation has no such tar - the new
		// package carries none. Nothing of this installation writes that
		// reference down either (dbapi.App has no field for it), so there is
		// no name to hand Engine.RemoveImage. Removing "whatever is in there"
		// is not on offer: the engine takes a reference, and reading a list
		// out of its output is exactly what this package does not do
		// (runtime.Engine.LoadImage). So the image of such an app is left
		// behind, it costs the disk of its own user, and whoever uninstalls
		// the app takes it with the user. A reference in the row would fix it
		// and is a change to shared/dbapi - reported, not built.
		if err := m.putRow(ctx, p, mf, old); err != nil {
			return m.why(p.app, stepRow, err)
		}
		if err := m.keepLicence(ctx, p.app, env, lic); err != nil {
			return m.why(p.app, stepLicence, err)
		}
		if err := m.keepSeq(ctx, p); err != nil {
			return m.why(p.app, stepSeq, err)
		}
		ok = true
		return nil
	}
	if _, err := m.d.Sys.EnsureRun(ctx, u); err != nil {
		return m.why(p.app, stepRun, err)
	}
	// The runtime directory is a tmpfs and holds nothing of the app
	// (runtime.EnsureRun), so taking it back costs nothing and leaves nothing
	// of a failed activation behind - also for an app that was installed,
	// whose container is gone by then anyway.
	undos = append(undos, undo{"run", func() { _ = m.d.Sys.DropRun(u.Name) }})

	// The image of this architecture, out of the files store.Walk let through.
	// The name is built here and not taken from an output of the engine, and
	// it is read out of the tar itself (runtime.ImageTag).
	tarPath := filepath.Join(p.files, "services", goruntime.GOARCH+".tar")
	ref, err := runtime.ImageTag(tarPath)
	if err != nil {
		return m.why(p.app, stepImage, err)
	}
	loaded, err := m.d.Engine.LoadImage(ctx, u.Name, tarPath)
	if err != nil {
		return m.why(p.app, stepImage, err)
	}
	if loaded != ref {
		// The engine answers with the reference it read out of the tar itself,
		// so the two can only differ if the engine read another tar than this
		// call named. Nothing is believed about it: the image goes out again.
		_ = m.d.Engine.RemoveImage(ctx, u.Name, loaded)
		return m.why(p.app, stepImage, ErrVerify)
	}
	undos = append(undos, undo{"image", func() { _ = m.d.Engine.RemoveImage(ctx, u.Name, ref) }})

	// The old container has to go before the new one can have its name, and
	// its name is the name of the user, which does not change. Whether it is
	// there cannot be read off the row: a container may be gone while the row
	// stands (a restart of the machine, an activation that failed halfway), so
	// Running is asked, and the error of Remove is borne and not made fatal -
	// the engine says a caller does exactly one of the two (runtime.Engine).
	// Nothing is made quiet by it either: a container that really is still
	// there takes the name, and Create below fails on it.
	if running, err := m.d.Engine.Running(ctx, u.Name); err == nil && running {
		if err := m.d.Engine.Stop(ctx, u.Name); err != nil {
			return m.why(p.app, stepContainer, err)
		}
	}
	_ = m.d.Engine.Remove(ctx, u.Name)

	c := runtime.Container{
		Name: u.Name, Image: ref, UID: u.UID, GID: u.GID, DataDir: dataDir,
		Hold: m.hold(), CPU: mf.CPU, Memory: mf.Memory,
		// The needs of the package, which are the grants of the task by now
		// (sameSet above): so this reads the manifest and says the same thing
		// as the signed task, and no third place can drift from either.
		Network: slices.Contains(mf.Needs, store.NeedNetwork),
		Root:    slices.Contains(mf.Needs, store.NeedRoot),
	}
	if err := m.d.Engine.Create(ctx, c); err != nil {
		return m.why(p.app, stepContainer, err)
	}
	undos = append(undos, undo{"container", func() {
		_ = m.d.Engine.Stop(ctx, u.Name)
		_ = m.d.Engine.Remove(ctx, u.Name)
	}})
	if err := m.d.Engine.Start(ctx, u.Name); err != nil {
		return m.why(p.app, stepContainer, err)
	}

	if err := m.putRow(ctx, p, mf, old); err != nil {
		return m.why(p.app, stepRow, err)
	}
	if err := m.keepLicence(ctx, p.app, env, lic); err != nil {
		return m.why(p.app, stepLicence, err)
	}
	if err := m.keepSeq(ctx, p); err != nil {
		return m.why(p.app, stepSeq, err)
	}
	ok = true
	return nil
}

// keepSeq raises the floor of the store this package came out of, so the next
// activation out of it cannot be handed an older catalogue (Deps.LastSeq).
//
// IT RUNS LAST, after the row and the licence, and inside setUp rather than
// after it: an activation whose floor could not be written is undone whole by
// the undos above, rather than leaving a container running that this
// installation has no memory of. The other order - the floor first - would
// raise the bar for an activation that then failed and shut a store out of
// catalogues it never acted on.
//
// A package no catalogue named writes nothing: an uploaded app
// (core.LocalStore) is covered by the checksum of its signed task alone, and
// there is no number to remember.
func (m *Manager) keepSeq(ctx context.Context, p appSetup) error {
	if p.seq == 0 || p.store == "" || p.store == core.LocalStore {
		return nil
	}
	return m.d.KeepSeq(ctx, p.store, p.seq)
}

// putRow writes the row of the app. The manifest goes in as JSON and not as it
// was delivered: Accept hands over the parsed manifest (store.Manifest), and
// store.Walk does not write manifest.json among the files - so the bytes the
// store signed are not here any more. This is a re-encoding of exactly what
// was checked, which store.ParseManifest reads back; the checksum of the
// package file (p.SHA256) is what binds the row to the delivered bytes.
func (m *Manager) putRow(ctx context.Context, p appSetup, mf *store.Manifest, old dbapi.App) error {
	raw, err := json.Marshal(mf)
	if err != nil {
		return err
	}
	return m.d.State.PutApp(ctx, dbapi.App{
		ID: p.app, Version: p.version, Store: p.store, Node: m.d.Node, Active: true,
		Manifest: raw, SHA256: p.sha256, Grants: slices.Clone(p.grants),
		// What the app kept from before stays its own number; this activation
		// did not change it.
		KeptData: old.KeptData, Updated: m.d.Now(),
	})
}

// hold is the program that keeps a container alive.
func (m *Manager) hold() string {
	if m.d.Hold != "" {
		return m.d.Hold
	}
	return config.BinDir + "/ngc-controller"
}

// licence checks the licence a paid app brought. It writes nothing: keeping it
// is keepLicence's, and the two are split for one reason - this runs before
// anything of this machine is changed, and a write there would be the one
// change of an activation that no rollback takes back.
//
// A package without a licence passes: whether an app is paid is the
// catalogue's to say, and a sealed package without a key never gets through
// Accept at all (store.Walk answers store.ErrPackage for it). So this is about
// the licence that is there, not about the one that is missing.
func (m *Manager) licence(d access.Decision, p *core.ActivateParams, pkg Package) (*store.License, error) {
	env := pkg.Transfer.License
	if env == nil {
		return nil, nil
	}
	// A licence belongs to a store, and only to one of this installation's own
	// trust list: the header says which of the listed stores it is, never what
	// its key is.
	i := slices.IndexFunc(m.d.List.Stores, func(s trust.Store) bool { return s.ID == p.Store })
	if i < 0 {
		return nil, ErrLicense
	}
	// The installation the task was signed for, not one out of the package:
	// a licence of another installation would otherwise let one licence serve
	// every NGC that can get hold of it.
	//
	// VerifyLicenseFor and not VerifyLicense, and that difference is the
	// security statement, not a shortcut: this checks the signature of the
	// store, the installation and the app, and it does NOT check that the
	// content key is wrapped for this installation's key - a controller
	// cannot, because it does not hold that key, and whoever could would hold
	// it. The other entrance, with the key, is the API's
	// (docs/vertraege-v1.2.md, "Die Lizenz war nie eine Vertragsfrage - sie ist
	// eine nicht aufgerufene Funktion"). Nobody builds a licence check of
	// their own, and nothing of this hangs on the right "<app>/use".
	//
	// The heading and not the number of that point: a number moves when a
	// document grows, and a pointer that moved is worse than none.
	l, err := store.VerifyLicenseFor(*env, m.d.List.Stores[i], d.Task.Installation, p.App)
	if err != nil {
		return nil, errors.Join(ErrLicense, err)
	}
	if l.State(m.d.Now()) == store.LicenseExpired {
		return nil, ErrLicense
	}
	return l, nil
}

// keepLicence writes the checked licence, as the last step of an activation
// that worked. Last, because it is the one write of this call that no rollback
// undoes: there is no way to take a licence back out of the local store
// (state.Store has no delete for one), so it happens where nothing can fail
// after it.
//
// Only the newest licence is kept (CLAUDE.md), and which one that is, the
// caller decides - which is here.
func (m *Manager) keepLicence(ctx context.Context, app string, env *sign.Envelope, l *store.License) error {
	if l == nil {
		return nil
	}
	if _, issued, err := m.d.State.License(ctx, app); err == nil && issued >= l.Issued {
		return nil
	} else if err != nil && !errors.Is(err, dbapi.ErrNotFound) {
		return err
	}
	return m.d.State.PutLicense(ctx, app, *env, l.Issued)
}

// sameSet reports whether a and b hold the same values, each at most once.
// core.ActivateParams.Validate and store.Manifest.Validate both refuse a
// repeated value already, so the lengths alone would do - the check is written
// without relying on that, because it decides what a container may do.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		if !slices.Contains(b, v) {
			return false
		}
	}
	for _, v := range b {
		if !slices.Contains(a, v) {
			return false
		}
	}
	return true
}
