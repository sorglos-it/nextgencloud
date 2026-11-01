// What this server does after a restart, before any API is in reach: it brings
// the apps that were running back up.
//
// Nothing here asks anyone. The rows of the apps, their manifests and their
// licences are all in this controller's own state, and the trust list it needs
// to judge a licence it verified itself and kept there too (chain.Keeper). So
// a server whose APIs are all down still comes back with its apps running -
// which is the point of the whole task: a file server that needs a web service
// to serve files is not a file server.
//
// What it does NOT do is report anything. A restore has nowhere to send: the
// tunnel comes later. Every single thing that went wrong goes into the log of
// this server, and the status an API gets later is built fresh by the Reporter
// out of what is really the case then (Reporter.Status) - never out of a note
// this call left behind.
//
// And nothing here is fatal except two things. One app that cannot come up is
// one app that cannot come up; the others are not held back by it, because a
// restore that stops at the first broken app would turn one bad package into a
// server with nothing running. The two that do end the call are the list of
// apps - if that cannot be read, this call knows nothing about what it did not
// see, and says so instead of reporting a quiet success over an empty list -
// and a context that was cancelled, which is this server shutting down again:
// there is no point in starting the next container into it.

package node

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/serve"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrNotWired says this program is wired wrong: a half of RestoreConfig is
// missing. None of it can be brought about by a peer - there is no peer at this
// point - so it is answered once and the same way, and nothing of this machine
// is touched before it is answered.
var ErrNotWired = errors.New("node: the restore is not wired up")

// RestoreConfig is what Restore needs. Every one of the four is required.
//
// Not one of them can reach an API, and that is the statement of this task, not
// an accident of the order they were written in: no tunnel, no trust list, no
// socket. The licence question arrives as a function (Licensed) for exactly
// that reason - the answer is license.Allowed over the local state and the
// locally verified trust list, and whoever wires the two together binds them
// before this call sees anything. TestRestoreNeedsNoAPI holds that, by looking
// at the types of these fields.
type RestoreConfig struct {
	// State is this controller's local store: the rows of the apps, with their
	// manifests.
	State *state.Store
	// Engine runs the containers.
	Engine runtime.Engine
	// Unlock puts the key of an app's data directory back into the keyring of
	// the filesystem (runtime.UnlockData with the key of this server). It has
	// to happen after every restart of the machine and before the container
	// starts, because the keys live in the kernel and not on the disk.
	Unlock func(ctx context.Context, app string) error
	// Licensed says whether an app may run at all (license.Allowed). Its error
	// is "nobody could say", which is not the same as "no": an app it cannot
	// answer for stays off and is logged, and the Reporter leaves it out of
	// the status later for the same reason (appState).
	Licensed func(ctx context.Context, a dbapi.App) (bool, error)
	// Run runs one service call in the container of its app. nil:
	// serve.RunService, which is what operation uses; a test puts its own in
	// so no container is needed.
	Run func(ctx context.Context, in serve.CallInput) wire.Close
	// EnsureRun makes the runtime directory the engine needs for this app's
	// container, and tightens the directory above it. nil: runtime.RunForApp,
	// which LOOKS THE APP'S USER UP and makes none - an app id without a user
	// is an app that is not installed here, and it stays off.
	//
	// It is a function and not a straight call for the same reason Unlock is
	// one: the cases of this package run with no /run, no app users and no
	// root. Its error means "the directory could not be made", which leaves
	// the app off and logged, like the other three.
	EnsureRun func(ctx context.Context, app string) error
	// HasCgroup says whether the container of this app runs inside a cgroup
	// of this program, which is the only way to see that an app is up and
	// braked by nothing. nil: runtime.HasCgroup.
	//
	// It is a function for the reason the two above are: it reads
	// /sys/fs/cgroup, and the cases of this package run where there is none -
	// so without a stand-in every case about a container that is already
	// running would measure the cgroup tree of the machine the case runs on.
	HasCgroup func(name string) bool
	// KillCgroup ends every task in the cgroup of an app and waits until none
	// is left (nil: runtime.KillCgroup). A function for the reason HasCgroup
	// is one, and for a second: a case about this must not be able to kill
	// anything on the machine it runs on.
	KillCgroup func(name string) error
	// Wait is the waiting between the two looks at the contradiction below
	// (nil: time.Sleep). It is here so the cases of this package take no real
	// twelve seconds - a case that really waits is a case somebody switches
	// off.
	Wait func(d time.Duration)
	Log  *slog.Logger // nil: slog.Default()
}

// Restore brings the apps of this server back up, in the order the state hands
// them out (state.Store.Apps: by ID). For every app that is active, carries a
// version and is no core app of NGC itself:
//
//  1. its licence is asked. No licence, or one that still runs, goes on; one
//     that ran out leaves the app off, which is what tunnel.StateUnlicensed
//     says of it in the next status.
//  2. its data directory is unlocked.
//  3. the runtime directory of its container is made (runtime.RunForApp). The
//     tmpfs took it with the last restart, and the engine cannot answer
//     without it - see the block at that call for what the missing step cost.
//  4. its container is started, if it is not running already.
//  5. the start action of its manifest (store.Manifest.Start) is run, with
//     service.Call{Start: true, User: ""} - a call of this server and of no
//     user. Its result goes into the log and nowhere else.
//
// Step 5 runs whether or not step 4 had to start anything. This is the boot
// path: a container that is already up was brought up by this machine starting,
// and its start action has not run yet. The manifest calls Start "the action
// the controller runs after every container start", and a restore of a machine
// is such a start.
//
// Rows that are left out entirely: an app that is not active (deactivated), a
// row of which only kept data remains (Version empty, dbapi.App), and the core
// apps of NGC, whose channels the API serves and which have no container here.
func Restore(ctx context.Context, c RestoreConfig) error {
	if c.State == nil || c.Engine == nil || c.Unlock == nil || c.Licensed == nil {
		return ErrNotWired
	}
	log := c.Log
	if log == nil {
		log = slog.Default()
	}
	apps, err := c.State.Apps(ctx)
	if err != nil {
		return err
	}
	for _, a := range apps {
		if wire.CoreApp(a.ID) || a.Version == "" || !a.Active {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		c.one(ctx, log, a)
	}
	return nil
}

// one brings a single app back up. It gives nothing back: every way it ends is
// a line in the log of this server, because there is nobody else to tell.
func (c RestoreConfig) one(ctx context.Context, log *slog.Logger, a dbapi.App) {
	ok, err := c.Licensed(ctx, a)
	if err != nil {
		log.Warn("the licence of an app could not be read, it stays off", "app", a.ID, "err", err)
		return
	}
	if !ok {
		log.Warn("the licence of an app has run out, it stays off", "app", a.ID)
		return
	}
	if err := c.Unlock(ctx, a.ID); err != nil {
		log.Warn("the data of an app stayed locked, it stays off", "app", a.ID, "err", err)
		return
	}
	name := runtime.UserName(a.ID)
	// BEFORE the engine is asked anything, and that order is the whole of this
	// step. config.RunDir is a tmpfs and the unit of this service hands it to
	// systemd as RuntimeDirectory, so after every restart of the machine or of
	// this service the directory above is new and the app's own is GONE. The
	// engine needs both to answer at all: rootless podman reads its
	// configuration out of XDG_RUNTIME_DIR, so the first call of every app
	// after a restart fell over the missing directory - and it fell over it in
	// Running, which is one line above where the directory would have been
	// made. Measured on the Debian VM on 23.10.2026, as the app's user, with
	// exactly the arguments Podman.Running passes:
	//
	//	lstat /run/ngc/ngc-terminal-439982dd: permission denied      exit 1
	//
	// and with the mode of the directory above put right by hand, the same call
	// one step further on:
	//
	//	lstat /run/ngc/ngc-terminal-439982dd: no such file or directory
	//
	// That was a closed circle, and it is the point of this step: Running could
	// not answer without the directory, and the directory was made nowhere on
	// this path - EnsureRun had exactly one caller, the activation
	// (install/activate.go). So the error went into the log, this function gave
	// up one line later, and Start was never reached. Every installed app
	// stayed off after every restart, for good, until somebody activated it
	// anew. Nothing of it was visible in the status either: appState turns this
	// same error into "nothing to say" and the Reporter then leaves the app out
	// of the report altogether, so an admin saw an app that looks like one that
	// was never installed here.
	//
	// It also puts the mode of the directory above right, on every restore and
	// not only on an activation (runtime.EnsureRun: 0711, so an app may walk
	// through to its own without reading the names of the others). That half
	// matters because the mode does not come from this program: the unit writes
	// it, in another repository, and it writes 0700. As long as this call runs
	// first, that number can no longer keep the apps down - and it is still
	// worth putting right where it is written.
	if err := c.ensureRun(ctx, a.ID); err != nil {
		log.Warn("the runtime directory of an app could not be made, it stays off",
			"app", a.ID, "err", err)
		return
	}
	up, err := c.Engine.Running(ctx, name)
	if err != nil {
		log.Warn("the engine does not say whether an app runs, it stays off", "app", a.ID, "err", err)
		return
	}
	// A container that is up but in no cgroup of this program was started by a
	// controller from before the limits were real: podman accepted
	// --pids-limit, --memory and --cpus, wrote them into its own answer and
	// dropped them, so the app runs with the ceiling of the controller and
	// none of its own (runtime/cgroup.go). Nothing of podman says so, and that
	// is why this stands here: a running container is the one state no start
	// would ever reach, so without this line such an app would keep running
	// unbraked until somebody activated it anew. It is stopped and started
	// again, once, and the line says so.
	if up && !c.hasCgroup(name) {
		log.Warn("an app runs outside its own limits and is started again into them",
			"app", a.ID)
		if err := c.Engine.Stop(ctx, name); err != nil {
			log.Warn("the container of an app could not be stopped, it keeps running unlimited",
				"app", a.ID, "err", err)
			return
		}
		up = false
	}
	// And the other way round: podman says nothing of this app runs, while its
	// cgroup still holds a task. Then a process set of this app is up that
	// podman has forgotten - after a hard death of the controller one set per
	// start of it, three per app on svc-1 after two starts (point 98). Podman
	// forgot them because it refreshes its own state when its runtime
	// directory is gone, which systemd deletes with the unit; the processes
	// did not notice, because since the limits are real they no longer sit in
	// the cgroup of the unit and the death of the service does not take them.
	// Starting now would put a SECOND writer on one data directory.
	//
	// It is looked at TWICE, and that is the whole care of this branch. Those
	// two numbers stand that way in ordinary operation as well - at every stop
	// and at every start, measured (runtime.OrphanWait names the window and
	// the measurement). Only a contradiction that survives a wait longer than
	// the deadline of a stop is no state of operation: an ordinary stop is
	// over by then and the cgroup empty, an orphan stands for hours.
	//
	// And it is only looked at HERE, while the node comes up, never again
	// while it runs: a recurring check would sooner or later look exactly into
	// the stop of a neighbour, and the wait that makes this safe would make a
	// watchdog useless. A watchdog is a new decision and needs its own
	// measurement.
	if !up && c.hasCgroup(name) {
		c.wait(runtime.OrphanWait)
		if c.hasCgroup(name) {
			log.Warn("tasks of an app are still running although the engine knows no container of it, they are ended before it starts again",
				"app", a.ID, "waited", runtime.OrphanWait)
			if err := c.killCgroup(name); err != nil {
				// Not started. An app whose surplus tasks cannot be ended is
				// an app that would run twice on one data directory, and that
				// is worse than an app that is off and logged.
				log.Warn("the surplus tasks of an app could not be ended, it stays off rather than running twice on its data",
					"app", a.ID, "err", err)
				return
			}
		}
	}
	if !up {
		if err := c.Engine.Start(ctx, name, limitsOf(a)); err != nil {
			log.Warn("the container of an app did not start", "app", a.ID, "err", err)
			return
		}
	}
	c.start(ctx, log, a, name)
}

// hasCgroup answers through the field if one was wired and through
// runtime.HasCgroup otherwise, for the reason ensureRun has its default here.
func (c RestoreConfig) hasCgroup(name string) bool {
	if c.HasCgroup != nil {
		return c.HasCgroup(name)
	}
	return runtime.HasCgroup(name)
}

// killCgroup and wait answer through their fields if ones were wired and
// through the real calls otherwise, for the reason hasCgroup does.
func (c RestoreConfig) killCgroup(name string) error {
	if c.KillCgroup != nil {
		return c.KillCgroup(name)
	}
	return runtime.KillCgroup(name)
}

func (c RestoreConfig) wait(d time.Duration) {
	if c.Wait != nil {
		c.Wait(d)
		return
	}
	time.Sleep(d)
}

// limitsOf is the ceiling of this app, out of its own manifest - the same two
// numbers the activation hands Create (install/activate.go).
//
// A manifest that cannot be read gives the two zeros, and a zero is the
// default of the contract and never "no limit" (runtime.Limits): a row whose
// manifest is broken gets store.DefaultCPU and store.DefaultMemory and runs
// braked, instead of getting no ceiling because its text was unreadable. That
// a broken manifest is reported is start's, which parses it again for the
// start action and says so there.
func limitsOf(a dbapi.App) runtime.Limits {
	mf, err := store.ParseManifest(a.Manifest)
	if err != nil {
		return runtime.Limits{}
	}
	return runtime.Limits{CPU: mf.CPU, Memory: mf.Memory}
}

// ensureRun makes the runtime directory of an app, through the field if one was
// wired and through runtime.RunForApp otherwise. The default sits here and not
// in Restore for the same reason the one of Run sits in start: a caller that
// wires nothing gets the behaviour of the operating program, and the cases of
// this package put their own in without having to know the default exists.
//
// runtime.RunForApp answers the path it made, and nothing here wants it: the
// engine builds the same path from the same name (runtime.RunDirOf), and a
// second place that carries it around would be a second place that could
// disagree about it.
func (c RestoreConfig) ensureRun(ctx context.Context, app string) error {
	if c.EnsureRun != nil {
		return c.EnsureRun(ctx, app)
	}
	_, err := runtime.RunForApp(ctx, app)
	return err
}

// start runs the start action of the manifest, if there is one.
//
// The manifest comes out of this controller's own row, which is where every
// other part of the controller takes it from as well (access.Check): nothing
// here reads a package again, and nothing of this comes from a peer.
func (c RestoreConfig) start(ctx context.Context, log *slog.Logger, a dbapi.App, name string) {
	mf, err := store.ParseManifest(a.Manifest)
	if err != nil {
		log.Warn("the manifest of an app could not be read, no start action ran",
			"app", a.ID, "err", err)
		return
	}
	if mf.Start == "" {
		return
	}
	// Both lookups are this controller reading what it stored itself. A
	// manifest that got this far has been through store.Manifest.Validate,
	// which refuses a Start that names no action - so a miss here is a row
	// that no longer agrees with the contract it was written under, and that
	// is worth a line of its own rather than a silent skip.
	act, found := mf.Action(mf.Start)
	if !found {
		log.Warn("the start action of an app is not in its manifest", "app", a.ID, "action", mf.Start)
		return
	}
	i := slices.IndexFunc(mf.Services, func(s store.Service) bool { return s.Name == act.Service })
	if i < 0 {
		log.Warn("the service of a start action is not in the manifest", "app", a.ID, "service", act.Service)
		return
	}
	call := service.Call{App: a.ID, Service: act.Service, Action: act.Name, Version: a.Version, Start: true}
	run := c.Run
	if run == nil {
		run = serve.RunService
	}
	cl := run(ctx, serve.CallInput{Engine: c.Engine, Container: name, Command: mf.Services[i].Command,
		Call: call, Timeout: time.Duration(act.Timeout) * time.Second, Stream: act.Stream, Log: log})
	// The result of a start action is bound to no user and goes nowhere but
	// here: nobody asked for it. An app whose start action failed is still
	// running - what it means is the app's own to say, and an admin reads it
	// in this log.
	if cl.Status != wire.StatusOK {
		log.Warn("the start action of an app did not end well", "app", a.ID, "action", act.Name,
			"status", string(cl.Status), "code", string(cl.Code))
		return
	}
	log.Info("the start action of an app ran", "app", a.ID, "action", act.Name)
}
