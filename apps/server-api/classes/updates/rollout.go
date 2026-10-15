// Driving one round of an NGC update: lay the file ready on a node, install
// it, wait until that node says it is up, then the next one.
//
// Everything in here is deliberately powerless. The API does not decide what
// may be installed: it hands the controller a file and the signed release it
// came out of, and the controller checks the signature, the sequence number,
// the part, the architecture, the version, the size and the checksum itself
// before a byte reaches its disk, and checks the standing approval and
// "newer than what runs" again before it installs
// (apps/server-controller/classes/update). So this file can delay an update,
// it can skip one, and it cannot make one happen that the installation did not
// sign.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// rolloutOrder is the order of an NGC update: the APIs first, then the
// controllers, the web interface last. Every part understands the previous
// contract, and every migration lets the previous API version run on, so NGC
// stays reachable throughout (Entwurf 4.8). The database comes before all of
// them and is not in this list, because it is not a part that is installed:
// one controller with the role db gets the ngc-api file LAID READY so that it
// can play the migrations in (MigrationNode).
var rolloutOrder = []string{trust.AutoAPI, trust.AutoController, trust.AutoWeb}

const (
	// lockLease is how long dbapi.LockUpdate is taken for at a time. It is
	// longer than ONE step of ONE node takes (InstallWait) and short enough
	// that an API which died mid-round does not keep the next one out for a
	// day. It is NOT long enough for a whole round, and no constant could be:
	// a round walks every part of every node one after the other, and each of
	// those steps may cost InstallWait. Measured: one single machine with the
	// roles api and web costs THREE waits, because tunnel.Components gives it
	// ngc-controller, ngc-api and ngc-web -- 45 minutes against this lease of
	// 40 (TestOneNodeCostsThreeWaits). That is why the round renews the lease
	// before every step (renewLease) instead of trusting this number.
	lockLease = int64(2400)
	// InstallWait is how long one node has to report store.UpdateInstalled
	// before the round goes on without it. A controller that installs itself
	// drops its tunnel and builds a new one, so this is minutes and not
	// seconds.
	InstallWait = 15 * time.Minute
)

// ErrNotDriving reports that another API holds dbapi.LockUpdate and that this
// round therefore did nothing at all. It is its own error and not nil: a
// caller that logs "rollout done" for it would say something untrue, and a
// caller that treats it as a failure would alarm an admin over the normal case
// of two APIs.
var ErrNotDriving = errors.New("updates: another API is driving the rollout")

// ErrAborted reports a round that did not run to its end: a node refused, it
// reported store.UpdateRolledBack, UpdateFailed, UpdateRejected or
// UpdateNeedsGrants, or this API is not wired up to reach a controller at all
// (Send nil until task 24 wires it). What was installed
// before it stays installed -- it is a whole program on its own node, not half
// of anything -- and the nodes behind it were not touched. Nothing anywhere
// says the round finished, because nothing of a round is written down at all.
var ErrAborted = errors.New("updates: the round stopped at a node")

// ErrLostLease reports that the lease on dbapi.LockUpdate ran out in the
// middle of a round and another API took it. It is a failure and is meant to
// be logged as one: unlike ErrNotDriving it does not say "another API drives
// and this one did nothing", it says this one drove, lost the wheel and left
// an installation half walked through. What was installed stays installed, as
// with ErrAborted; what is lost is the promise that exactly one API walks the
// nodes.
var ErrLostLease = errors.New("updates: the lease on the rollout lock ran out mid-round")

// Rollout drives one round. Exactly one API drives it; the others answer
// ErrNotDriving and do nothing.
//
// The round is not a transaction and must not look like one. Every node is
// installed on its own, and a node that is done stays done if the round stops
// behind it: a mixed installation is what the contract is built for (every
// part understands the previous one), while a half-written program is what
// the controller's rename prevents. So there is no "finished" to roll back,
// and -- this is the point -- there is no row anywhere that says a round
// finished either. The rows of ngc.store/updates are computed from what the
// nodes report, so a round broken off in the middle shows itself as what it
// is: the nodes that are up, and the ones that are not.
func (u *Updates) Rollout(ctx context.Context) error {
	if u.Send == nil || u.Stores == nil || u.S == nil {
		return ErrAborted
	}
	now := u.S.Now()
	ok, err := u.S.DB.LockTake(ctx, dbapi.LockUpdate, u.S.Cfg.Node, now, now+lockLease)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotDriving
	}
	defer func() { _ = u.S.DB.LockFree(ctx, dbapi.LockUpdate, u.S.Cfg.Node) }()
	release, env, err := u.Stores.Release(ctx)
	if err != nil {
		return err
	}
	v, err := u.S.Trust(ctx)
	if err != nil {
		return err
	}
	// The migrations belong to the API version and run before the new API
	// starts. One controller with the role db gets the ngc-api file of its
	// architecture LAID READY for that, and nothing more: this contract has
	// no action that plays migrations in (core.All lists stage and install
	// and no third one), and "install ngc-api" on a node whose server is not
	// an API would copy the program into its bin directory and restart a
	// service that does not belong there. Playing the file in is the
	// controller's step and is not built on its branch either; the file lying
	// ready is what this side can do and all of it.
	//
	// A database server that is ALSO an API server needs nothing extra: its
	// own step below lays the same file ready before it installs it, and
	// sending it twice would only cost the bytes.
	if node, found := u.MigrationNode(); found && !slices.Contains(u.nodesFor(trust.AutoAPI), node) {
		if err := u.renewLease(ctx); err != nil {
			return err
		}
		if err := u.stage(ctx, node, trust.AutoAPI, release, env); err != nil {
			return err
		}
	}
	for _, part := range rolloutOrder {
		for _, n := range u.nodesFor(part) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := u.renewLease(ctx); err != nil {
				return err
			}
			if err := u.stageAndInstall(ctx, n, part, release, env, v.List); err != nil {
				return err
			}
			// One node at a time, so one of each kind keeps running.
		}
	}
	return nil
}

// renewLease puts the end of the lease on dbapi.LockUpdate one lockLease into
// the future, so that the step which follows has the whole lease to itself.
// It runs before EVERY step, not once per round: a round is as long as the
// nodes make it (measured: three waits for one machine that carries api and
// web), while the lease is one constant.
//
// dbapi.LockTake is the renewal too, because it answers true for the owner
// that already holds the lock -- the contract says so (shared/dbapi) and
// shared/dbapi/dbtest measures it. A false means the lease had run out and
// somebody else took the lock while this round was waiting on a node. Then
// this round stops at once: driving on would be the second round over the
// same controllers that ErrNotDriving exists to prevent.
func (u *Updates) renewLease(ctx context.Context) error {
	now := u.S.Now()
	ok, err := u.S.DB.LockTake(ctx, dbapi.LockUpdate, u.S.Cfg.Node, now, now+lockLease)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLostLease
	}
	return nil
}

// MigrationNode picks the controller that plays the migrations in: among the
// connected controllers with the role db the one with the smallest node ID; if
// it is away, the next. A node without a tunnel is no candidate, because the
// file has to reach it now.
func (u *Updates) MigrationNode() (string, bool) {
	ids := u.withRole(tunnel.RoleDB)
	if len(ids) == 0 {
		return "", false
	}
	return ids[0], true
}

// nodesFor are the connected controllers that run part, sorted by node ID so
// that one round and the next take the same order.
func (u *Updates) nodesFor(part string) []string {
	var out []string
	for _, n := range u.online() {
		h, _, ok := u.seenOf(n)
		if ok && slices.Contains(tunnel.Components(h.Roles), part) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// withRole are the connected controllers whose server carries role, sorted.
func (u *Updates) withRole(role string) []string {
	var out []string
	for _, n := range u.online() {
		if h, _, ok := u.seenOf(n); ok && slices.Contains(h.Roles, role) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (u *Updates) online() []string {
	if u.Tunnels == nil {
		return nil
	}
	return u.Tunnels.Online()
}

// stageAndInstall lays the file of part ready on node and, where the setting
// and the trust list allow it, installs it and waits for the node to say it is
// up.
//
// What stops at "laid ready", and why none of it is this file's own idea:
//
//   - store.ModeNotify: tell only. Nothing travels.
//   - store.ModeSemi: download and check in advance, install on click. The
//     file lies ready and the admin presses the button.
//   - store.ModeAuto outside the maintenance window: the window IS the
//     promise, so the file waits in it and the next round inside the window
//     installs.
//   - no standing approval for the target and this release key: the
//     controller would refuse an install without a user anyway
//     (core.Unattended), so asking would only produce a refusal in its log.
func (u *Updates) stageAndInstall(ctx context.Context, node, part string, rel *store.Release,
	env sign.Envelope, l *trust.List) error {
	if _, ok := u.fileFor(node, part, rel); !ok {
		return nil
	}
	// Never backwards, and never the same version again: the version this
	// node runs comes out of its own status, not out of the round. The line
	// is the one the controller has for the same question (install.go): an
	// empty installed version is no version, and store.Newer answers false
	// for it, so a part that is not installed at all is NOT read as "older".
	_, status, _ := u.seenOf(node)
	if installed := status.Components[part]; installed != "" && !store.Newer(rel.Version, installed) {
		return nil
	}
	s, err := u.Setting(ctx, part)
	if err != nil {
		return err
	}
	if s.Mode == store.ModeNotify {
		return nil
	}
	if err := u.stage(ctx, node, part, rel, env); err != nil {
		return err
	}
	if s.Mode != store.ModeAuto || !u.inWindow(s.Window) {
		return nil
	}
	if !store.Approved(l, part, u.S.Cfg.ReleaseKey) {
		return nil
	}
	f, ok := u.fileFor(node, part, rel)
	if !ok {
		return nil
	}
	p := &core.InstallParams{Target: part, Version: rel.Version, SHA256: f.SHA256}
	if err := p.Validate(); err != nil {
		return err
	}
	params, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, code := u.Send.Call(ctx, node, core.ActInstall, params, nil, nil, nil); code != "" {
		return u.abort(ctx, part, rel.Version, node, string(code))
	}
	return u.awaitInstalled(ctx, node, part, rel.Version)
}

// stage lays the file of part for the architecture of node ready on it, and
// does nothing when this release carries no file for that part and
// architecture.
//
// It asks nothing about what is installed. Its other caller is the migration
// node, where NOTHING of ngc-api is installed and nothing ever will be: the
// node holds the file so that it can play the migrations in. A guard on
// "newer than installed" belongs where the decision is -- in stageAndInstall,
// one per node and part the node really runs.
//
// The file goes to the disk and not through memory: a release file is not
// small, and several of them at once would be the API's memory and not the
// store's problem. It is deleted again in every exit of this function,
// including the ones that fail -- a spool nobody empties fills the disk of a
// machine that is then no longer able to update itself.
func (u *Updates) stage(ctx context.Context, node, part string, rel *store.Release,
	env sign.Envelope) error {
	f, ok := u.fileFor(node, part, rel)
	if !ok {
		return nil
	}
	// A file that already lies ready on this node for this very version is
	// not sent again: the node said so itself, and a release file is not
	// small. The report is volatile, so a controller that rebuilt its tunnel
	// is handed the file once more -- which costs bytes and never a wrong
	// state, because the controller checks the whole file against the signed
	// release again either way.
	if r, seen := u.report(node, part); seen && r.Version == rel.Version &&
		(r.State == store.UpdateDownloaded || r.State == store.UpdateInstalling ||
			r.State == store.UpdateInstalled) {
		return nil
	}
	p := &core.StageParams{Target: part, Version: rel.Version}
	if err := p.Validate(); err != nil {
		return err
	}
	params, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// The header repeats what the signed release says, and the controller
	// holds it against that same release before it reads a byte. Release and
	// nothing else: a header that also named a store would not be a release
	// at all (core.Transfer.Validate, and the controller's fromRelease).
	head := core.Transfer{Release: &env, Size: f.Size, SHA256: f.SHA256}
	if err := head.Validate(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(u.Spool, "ngc-update-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	// Download holds the bytes against the size and the checksum of the signed
	// entry and is an error for anything else, so what lies in tmp afterwards
	// is the file the release names or the function did not return nil.
	if err := u.Stores.FetchReleaseFile(ctx, f, tmp); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		return err
	}
	if _, code := u.Send.Transfer(ctx, node, core.ActStage, params, head, tmp,
		nil, nil, nil); code != "" {
		return u.abort(ctx, part, rel.Version, node, string(code))
	}
	return nil
}

// fileFor is the release entry of part for the architecture of node.
func (u *Updates) fileFor(node, part string, rel *store.Release) (store.File, bool) {
	h, _, online := u.seenOf(node)
	if !online {
		return store.File{}, false
	}
	return releaseFile(rel, part, h.Arch)
}

// awaitInstalled waits until node reports store.UpdateInstalled for target.
//
// Four of the eight states of store stop the round: UpdateRolledBack and
// UpdateFailed, because the node put its old version back or is in no state to
// carry an update; UpdateRejected, because it refused what it was handed; and
// UpdateNeedsGrants, because an admin has to decide before anything else
// moves. Sending the same file to the next node after any of them would spread
// a fault instead of an update. The other four -- available, downloaded,
// installing, installed -- are stages on the way and only the last one ends
// the wait.
//
// A node that says nothing at all does NOT stop the round: it installed
// itself, so its tunnel went down with it, and a controller that comes back
// quiet is a reported state this side never saw rather than a failure. The
// round goes on after InstallWait, and the row of that node keeps whatever it
// last reported -- never "installed", which only the node itself may say.
func (u *Updates) awaitInstalled(ctx context.Context, node, target, version string) error {
	deadline := time.NewTimer(u.wait())
	defer deadline.Stop()
	for {
		// The waiter is taken BEFORE the state is read, so a report that
		// arrives between the two wakes this loop instead of being lost.
		wake := u.waiter()
		if r, ok := u.report(node, target); ok && r.Version == version {
			switch r.State {
			case store.UpdateInstalled:
				return nil
			case store.UpdateRolledBack, store.UpdateFailed, store.UpdateRejected,
				store.UpdateNeedsGrants:
				return u.abort(ctx, target, version, node, r.State)
			}
		}
		select {
		case <-wake:
		case <-deadline.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (u *Updates) wait() time.Duration {
	if u.Wait <= 0 {
		return InstallWait
	}
	return u.Wait
}

// abort writes the one log entry of a stopped round and answers ErrAborted.
// The cause travels with it, so a caller sees which node said what; the text
// of the log is words of the program and never anything a controller sent as
// text (audit.Log checks the field again).
func (u *Updates) abort(ctx context.Context, target, version, node, cause string) error {
	detail := target + " " + version + " " + node + " " + cause
	if err := u.note(ctx, target, detail); err != nil {
		return errors.Join(ErrAborted, err)
	}
	return ErrAborted
}

// inWindow reports whether now is inside the maintenance window. An empty
// window is every hour of the day: a target whose admin named none wants the
// update whenever the round comes.
//
// A window that ends before it begins ("22:00-02:00") runs over midnight; that
// is the normal shape of a maintenance window and not a mistake. Both ends
// belong to the window, so a window of one minute is one minute and not zero.
func (u *Updates) inWindow(w string) bool {
	if w == "" {
		return true
	}
	from, to, ok := parseWindow(w)
	if !ok {
		// core.UpdateSetting.Validate refuses a window of another shape, so
		// nothing stored gets here. A row that was written past the contract
		// is not read as "always": it is read as "never", because the safer
		// of the two answers is the one that installs nothing unasked.
		return false
	}
	zone := u.Zone
	if zone == nil {
		zone = time.Local
	}
	t := time.Unix(u.S.Now(), 0).In(zone)
	cur := t.Hour()*60 + t.Minute()
	if from <= to {
		return cur >= from && cur <= to
	}
	return cur >= from || cur <= to
}

// parseWindow reads "HH:MM-HH:MM" into two minute counts.
func parseWindow(w string) (int, int, bool) {
	a, b, ok := strings.Cut(w, "-")
	if !ok {
		return 0, 0, false
	}
	from, ok1 := parseClock(a)
	to, ok2 := parseClock(b)
	return from, to, ok1 && ok2
}

func parseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}
