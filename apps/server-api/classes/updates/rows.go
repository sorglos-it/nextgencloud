// Package updates computes the rows of ngc.store/updates and drives a rollout.
// Nothing but the setting per target is stored (dbapi.SettingUpdate); the rows
// come from releases, catalogs and what the controllers reported, so they are
// always current and never stale.
//
// Two things this package will not do, and both are walls rather than habits:
//
//   - It names no version and no checksum of its own. What a row offers comes
//     out of a signed release (store.VerifyRelease, against the release key of
//     the configuration) or out of a signed catalog (store.VerifyCatalog,
//     against the key of the app's OWN store), and the checksum a row carries
//     is the one of that same signed entry -- never a value a controller sent.
//     A controller that reports a version nobody signed therefore moves
//     nothing: its report can colour a row's state, and it can do no more.
//   - It verifies nothing a second time. Releases and catalogs come through
//     appstore, which keeps the newest sequence number it saw and refuses an
//     older one (store.ErrRollback); the controller checks the signature again
//     before a byte reaches its disk. This package sits between the two and
//     adds no third judge.
package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Fleet is what this package reads about the controllers that are connected
// right now. It is an interface and not *controllers.Registry for the reason
// appstore.Sender is one: a controllers.Tunnel keeps its hello and its status
// behind unexported fields, so no case could build one, and a package that
// took the registry itself could not be measured at all. FromRegistry is the
// Fleet of operation, and the compiler holds that the registry fits it.
type Fleet interface {
	// Seen returns the last hello and status of node. ok is false while no
	// tunnel to that node is up, and that is the only thing this package reads
	// as "online".
	Seen(node string) (tunnel.Hello, tunnel.Status, bool)
	// Online returns the nodes with an open tunnel, in no order of its own.
	Online() []string
}

// FromRegistry is the Fleet of operation.
func FromRegistry(r *controllers.Registry) Fleet { return registryFleet{r} }

type registryFleet struct{ r *controllers.Registry }

func (f registryFleet) Seen(node string) (tunnel.Hello, tunnel.Status, bool) {
	t, ok := f.r.Get(node)
	if !ok {
		return tunnel.Hello{}, tunnel.Status{}, false
	}
	return t.Hello(), t.Status(), true
}

func (f registryFleet) Online() []string { return f.r.Nodes() }

// Updates answers ngc.store/updates and drives a rollout.
type Updates struct {
	S      *state.State
	Stores *appstore.Stores
	// Tunnels is what is connected right now.
	Tunnels Fleet
	// Send is how an update reaches a controller. It is appstore.Sender and
	// not a second seam of this package's own: stage and install are core
	// actions on a controller exactly as activate and uninstall are, so one
	// adapter in the wiring of the server serves both. Both calls of this
	// package go WITHOUT a user (core.WithoutUser): task, client and proof are
	// nil, and the controller checks the standing approval itself
	// (core.Unattended).
	Send appstore.Sender
	// Log is where an aborted rollout and a changed setting are written down
	// (core.LogUpdate).
	Log *audit.Log
	// Spool is the directory an update file is written to while it is read.
	// Empty means the directory the operating system names, which is what a
	// case uses; a server sets it to its data directory, because a release
	// file is not small and must not land in a RAM-backed /tmp.
	Spool string
	// Wait is how long one node has to report store.UpdateInstalled before the
	// next one follows; zero means InstallWait.
	Wait time.Duration
	// Zone is the time zone the maintenance window is read in; nil is
	// time.Local. It is a field so that a case can measure a window at all.
	Zone *time.Location

	mu sync.Mutex
	// seen is the newest report per node and target. It is deliberately
	// volatile: a controller reports again after every tunnel it builds, so a
	// stored report could only ever be older than the truth.
	seen map[reportKey]store.UpdateReport
	// wake is closed and replaced on every report, so a rollout waiting for
	// one is woken instead of polling.
	wake chan struct{}
}

type reportKey struct{ node, target string }

// Rows returns one row per target and node, sorted by target then node, so the
// shell always shows the same order.
func (u *Updates) Rows(ctx context.Context) ([]core.UpdateInfo, error) {
	v, err := u.S.Trust(ctx)
	if err != nil {
		return nil, err
	}
	// Without a release nothing of NGC can be offered; apps still can. A store
	// that is away must not empty the whole page.
	release, _, relErr := u.Stores.Release(ctx)
	if relErr != nil {
		release = nil
	}
	apps, err := u.S.DB.AppList(ctx)
	if err != nil {
		return nil, err
	}
	var out []core.UpdateInfo
	for _, n := range v.List.Nodes {
		if n.Kind != trust.NodeController {
			continue
		}
		hello, status, _ := u.seenOf(n.ID)
		for _, part := range tunnel.Components(hello.Roles) {
			row := core.UpdateInfo{Target: part, Node: n.ID, Installed: status.Components[part],
				Key: enc.Bytes(u.S.Cfg.ReleaseKey)}
			if _, ok := releaseFile(release, part, hello.Arch); ok &&
				store.Newer(release.Version, row.Installed) {
				row.Available = release.Version
			}
			u.fill(ctx, &row, release, nil)
			out = append(out, row)
		}
		// System packages have no version to compare: apt knows what is due,
		// and NGC neither names it nor signs it. The row carries the setting
		// and whatever the node last reported, and no Key -- a standing
		// approval for trust.AutoSystem names none (trust.Auto).
		sys := core.UpdateInfo{Target: trust.AutoSystem, Node: n.ID}
		u.fill(ctx, &sys, nil, nil)
		out = append(out, sys)
	}
	for _, a := range apps {
		if a.Version == "" {
			// Only kept data remains; there is nothing installed to update.
			continue
		}
		row := core.UpdateInfo{Target: a.ID, Node: a.Node, Installed: a.Version}
		pkg := u.available(ctx, &row, a)
		u.fill(ctx, &row, nil, pkg)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Node < out[j].Node
	})
	return out, nil
}

// seenOf is what the fleet says about one node.
func (u *Updates) seenOf(node string) (tunnel.Hello, tunnel.Status, bool) {
	if u.Tunnels == nil {
		return tunnel.Hello{}, tunnel.Status{}, false
	}
	return u.Tunnels.Seen(node)
}

// releaseFile is the entry of an NGC part for one architecture, picked by part
// AND architecture and by nothing else -- the same way the controller picks it
// when it stages (apps/server-controller/classes/update.fromRelease). The web
// interface has no machine code and is listed as arch "all" (store.File).
//
// A part this release does not carry for this architecture is NOT offered: a
// row that named a version no file belongs to would send the admin to a click
// that cannot work. An architecture nobody knows -- which is every node
// without a tunnel, because the architecture comes out of its hello -- is
// none, so such a node is offered nothing.
func releaseFile(r *store.Release, part, arch string) (store.File, bool) {
	if r == nil {
		return store.File{}, false
	}
	if part == trust.AutoWeb {
		arch = "all"
	}
	if arch == "" {
		return store.File{}, false
	}
	for _, f := range r.Files {
		if f.Part == part && f.Arch == arch {
			return f, true
		}
	}
	return store.File{}, false
}

// available sets Available for an app: the newest version of the catalog of
// the store the app was activated from, whose minNgc suits the node's NGC
// version. A version that needs more than the app was granted marks the row
// needsgrants instead of being offered. It returns the entry it chose, so the
// checksum of a staged file comes out of that same signed source.
func (u *Updates) available(ctx context.Context, row *core.UpdateInfo, a dbapi.App) *store.Package {
	c, _, err := u.Stores.Catalog(ctx, a.Store)
	if err != nil {
		return nil
	}
	ngc := ""
	if h, _, ok := u.seenOf(a.Node); ok {
		ngc = h.Version
	}
	var chosen *store.Package
	for _, e := range c.Apps {
		if e.ID != a.ID {
			continue
		}
		for i, p := range e.Versions {
			if !store.Newer(p.Version, a.Version) || !store.SupportsMinNGC(ngc, p.MinNGC) {
				continue
			}
			if !store.Granted(p.Needs, a.Grants) {
				// Nothing is wrong with the update; the app may simply not do
				// what it now needs, so the shell offers the activation. The
				// row stops at this version: a later one of the same list
				// would hide that the admin has something to decide.
				row.Available, row.State = p.Version, store.UpdateNeedsGrants
				return nil
			}
			row.Available, chosen = p.Version, &e.Versions[i]
		}
	}
	return chosen
}

// fill adds the setting of the target and the newest report of the node.
//
// The checksum is the one of the SIGNED entry the row offers, and it is set
// only when the node reports it has downloaded exactly that version. Neither
// half of that is cosmetic: a shell signs this value into core.InstallParams,
// so a controller allowed to name a checksum of its own could name the file an
// admin then approves.
func (u *Updates) fill(ctx context.Context, row *core.UpdateInfo, rel *store.Release, pkg *store.Package) {
	s, err := u.Setting(ctx, row.Target)
	if err == nil {
		row.Mode, row.Window = s.Mode, s.Window
	}
	if row.State != "" {
		return
	}
	r, ok := u.report(row.Node, row.Target)
	if !ok {
		return
	}
	row.State = r.State
	if r.State != store.UpdateDownloaded || row.Available == "" || r.Version != row.Available {
		return
	}
	switch {
	case pkg != nil:
		row.SHA256 = enc.Bytes(bytes.Clone(pkg.SHA256))
	case rel != nil:
		h, _, _ := u.seenOf(row.Node)
		if f, found := releaseFile(rel, row.Target, h.Arch); found {
			row.SHA256 = enc.Bytes(bytes.Clone(f.SHA256))
		}
	}
}

// report is the newest report of one node for one target.
func (u *Updates) report(node, target string) (store.UpdateReport, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok := u.seen[reportKey{node, target}]
	return r, ok
}

// Report takes one update report of a controller (wire.CtlUpdate, through
// controllers.Server.OnReport). store.ParseUpdateReport checked it before it
// got here, so target, version and state are values of the contract; nothing
// of it decides anything, it colours a row and wakes a rollout that waits.
//
// It writes no log entry: a controller reports several times per update, and a
// line per stage would bury the one entry that matters -- the aborted round,
// which Rollout writes (core.LogUpdate). The context is taken so that the hook
// of the tunnel server fits without a wrapper; nothing here blocks or waits.
func (u *Updates) Report(_ context.Context, node string, r store.UpdateReport) {
	if node == "" || !store.ValidTarget(r.Target) {
		return
	}
	u.mu.Lock()
	if u.seen == nil {
		u.seen = map[reportKey]store.UpdateReport{}
	}
	u.seen[reportKey{node, r.Target}] = r
	if u.wake != nil {
		close(u.wake)
		u.wake = nil
	}
	u.mu.Unlock()
}

// waiter returns the channel that is closed when the next report arrives. A
// caller takes it BEFORE it reads the state, so a report landing between the
// two wakes it instead of being missed.
func (u *Updates) waiter() <-chan struct{} {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.wake == nil {
		u.wake = make(chan struct{})
	}
	return u.wake
}

// settingID is the row ID of a target's update policy.
func settingID(target string) string { return dbapi.SettingUpdate + target }

// Setting returns the update policy of a target. store.ModeAuto is the default
// and is what a target without a row has: NGC keeps itself up to date unless
// an admin says otherwise. A row that does not read is an error and not the
// default, because the default in its place would quietly turn a "notify" back
// into "auto".
func (u *Updates) Setting(ctx context.Context, target string) (core.UpdateSetting, error) {
	if !store.ValidTarget(target) {
		return core.UpdateSetting{}, core.ErrParams
	}
	b, err := u.S.DB.BlobGet(ctx, dbapi.BlobSetting, settingID(target))
	if errors.Is(err, dbapi.ErrNotFound) {
		return core.UpdateSetting{Target: target, Mode: store.ModeAuto}, nil
	}
	if err != nil {
		return core.UpdateSetting{}, err
	}
	var s core.UpdateSetting
	if json.Unmarshal(b.Data, &s) != nil || s.Target != target || s.Validate() != nil {
		return core.UpdateSetting{}, state.ErrSetting
	}
	return s, nil
}

// SetSetting writes the update policy of a target (ngc.store/set-update).
//
// store.ModeAuto needs a standing approval for the target in the trust list
// (store.Approved): auto means "install without asking anybody", and the trust
// list is where this installation says that is allowed. Without one the
// setting is refused rather than written and then silently ignored -- an admin
// who reads "auto" and gets nothing would look for the fault elsewhere.
func (u *Updates) SetSetting(ctx context.Context, s core.UpdateSetting) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Mode == store.ModeAuto {
		v, err := u.S.Trust(ctx)
		if err != nil {
			return err
		}
		key, err := u.approvalKey(ctx, v.List, s.Target)
		if err != nil {
			return err
		}
		if !store.Approved(v.List, s.Target, key) {
			return core.ErrParams
		}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := u.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: settingID(s.Target),
		Data: b, Updated: u.S.Now()}); err != nil {
		return err
	}
	return u.note(ctx, s.Target, s.Target+" "+s.Mode)
}

// note writes one line of the update log. The detail is words of the program
// -- a target, a version, a state -- and never anything a store or a
// controller sent as text; audit.Log checks every field again itself.
func (u *Updates) note(ctx context.Context, target, detail string) error {
	if u.Log == nil {
		return nil
	}
	app := ""
	if !ngcPart(target) && target != trust.AutoSystem {
		app = target
	}
	err := u.Log.Note(ctx, core.LogUpdate, audit.UserSystem, app, "", detail)
	if err != nil && !errors.Is(err, audit.ErrDropped) {
		return err
	}
	return nil
}

// approvalKey is the key a standing approval for target has to name: the
// release key of this API for an NGC part, the key of the app's own store for
// an app, and none for the system packages (trust.Auto). An app whose row or
// whose store this installation does not know has no key, and store.Approved
// then says no.
func (u *Updates) approvalKey(ctx context.Context, l *trust.List, target string) ([]byte, error) {
	switch {
	case target == trust.AutoSystem:
		return nil, nil
	case ngcPart(target):
		return u.S.Cfg.ReleaseKey, nil
	}
	a, err := u.S.DB.AppGet(ctx, target)
	if err != nil {
		return nil, err
	}
	for _, s := range l.Stores {
		if s.ID == a.Store {
			return s.Key, nil
		}
	}
	return nil, nil
}

// ngcPart reports whether target is a part of NGC itself, which comes out of a
// signed release rather than out of a store's catalog.
func ngcPart(target string) bool {
	return target == trust.AutoWeb || target == trust.AutoAPI || target == trust.AutoController
}
