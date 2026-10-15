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
	Log *slog.Logger // nil: slog.Default()
}

// Restore brings the apps of this server back up, in the order the state hands
// them out (state.Store.Apps: by ID). For every app that is active, carries a
// version and is no core app of NGC itself:
//
//  1. its licence is asked. No licence, or one that still runs, goes on; one
//     that ran out leaves the app off, which is what tunnel.StateUnlicensed
//     says of it in the next status.
//  2. its data directory is unlocked.
//  3. its container is started, if it is not running already.
//  4. the start action of its manifest (store.Manifest.Start) is run, with
//     service.Call{Start: true, User: ""} - a call of this server and of no
//     user. Its result goes into the log and nowhere else.
//
// Step 4 runs whether or not step 3 had to start anything. This is the boot
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
	up, err := c.Engine.Running(ctx, name)
	if err != nil {
		log.Warn("the engine does not say whether an app runs, it stays off", "app", a.ID, "err", err)
		return
	}
	if !up {
		if err := c.Engine.Start(ctx, name); err != nil {
			log.Warn("the container of an app did not start", "app", a.ID, "err", err)
			return
		}
	}
	c.start(ctx, log, a, name)
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
