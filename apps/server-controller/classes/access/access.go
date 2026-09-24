// Package access decides whether a channel an API opens may run on this
// controller: it checks the OPEN against the trust list, takes the rule of
// the action from what this server holds itself, verifies the signed task
// behind it and refuses a task that has run before. Nothing here starts any
// work; the answer is a decision or the one CLOSE the channel ends with.
package access

import (
	"context"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// AppLookup returns the row of an app this controller runs; any error says
// there is no row to open a channel for. state.Store.App answers it.
type AppLookup interface {
	App(ctx context.Context, id string) (dbapi.App, error)
}

// TaskOnce remembers the ID of a signed task until it expires and answers
// dbapi.ErrUsed for one that was seen before; state.Store.UseTask answers it.
type TaskOnce interface {
	UseTask(ctx context.Context, id []byte, expires, now int64) error
}

// Request is one OPEN to decide on. Apps and Tasks are this controller's own
// store — the manifest an app channel is judged by, and the IDs of the tasks
// that have run — and both must be set.
type Request struct {
	// Payload is the OPEN payload as it arrived on the tunnel, unchanged.
	// This package parses it itself, because only the parse that checks the
	// client certificate says who opened the channel: a tunnel.Opened that
	// came from anywhere else carries no session, and every channel would
	// look like one without a user.
	Payload []byte
	// List is the newest trust list version this controller has verified.
	// Without it nothing can be checked at all.
	List  *trust.List
	Node  string // this controller's node ID
	Now   int64
	Apps  AppLookup
	Tasks TaskOnce
}

// Decision is a channel that may run.
type Decision struct {
	// Opened is the checked OPEN. Its Session is the one this package
	// verified, so it is nil only for a channel that truly carries no
	// certificate.
	Opened tunnel.Opened
	Action core.Action // the core action; the zero value on an app channel
	Core   bool        // a core action of this controller, not a channel to an app
	Task   *task.Task  // the verified task, nil where none was needed and none came
	// User is the user this channel is authorised and logged by, or empty:
	// then the channel proves none, whatever certificate it carries.
	User     string
	UserName string          // that user's login name in the trust list
	App      dbapi.App       // the row of the app on an app channel
	Manifest *store.Manifest // its manifest, nil on a core action
}

// Check decides whether the channel of this OPEN may run. It answers either a
// decision or the CLOSE the channel ends with, never both: a refusal says
// nothing but its code.
func Check(ctx context.Context, r Request) (Decision, *wire.Close) {
	// Nothing can be checked without the trust list: no certificate, no task,
	// no root permission. A controller that has verified no version yet
	// therefore opens no channel at all, not even one without a user.
	if r.List == nil {
		return refuse(wire.CodeUnreachable)
	}
	o, err := tunnel.VerifyOpen(r.Payload, r.List, r.Now)
	if err != nil {
		return refuse(wire.CodeVerify)
	}
	// The OPEN as it was checked, read once: everything below decides on this
	// copy, so no later read can say something else.
	open := o.Open
	withUser := o.Session != nil

	a, isCore := core.Lookup(open.App, open.Service, open.Action)
	var (
		rule task.Rule
		app  dbapi.App
		m    *store.Manifest
	)
	if isCore {
		// The actions of service api run in the API; this server has none of
		// them, so an OPEN for one finds nothing here.
		if a.Service != core.ServiceController {
			return refuse(wire.CodeNotFound)
		}
		if withUser {
			// The channel carries a certificate, so it is judged as a user's,
			// and a user opens only what core.ShellOpens allows. What that
			// leaves of this service — stage, license and
			// ngc.users/controller/trust — the API opens on its own.
			if !core.ShellOpens(a) {
				return refuse(wire.CodeForbidden)
			}
		} else if !core.WithoutUser(a) {
			return refuse(wire.CodeForbidden)
		}
		rule = core.RuleFor(a)
	} else {
		// A core app ID names no app of this controller: core.Lookup has just
		// said this is none of their actions, and a manifest may not carry
		// such an ID at all (store.Manifest.Validate). It would also be the
		// one channel that carries a certificate without an end-to-end
		// handshake, which wire.Open.Validate allows only for core app IDs.
		if wire.CoreApp(open.App) {
			return refuse(wire.CodeNotFound)
		}
		// Only the core actions of core.WithoutUser run without a user, so an
		// app channel without a certificate is refused — before the lookup,
		// since no answer of the database could let it through.
		if !withUser {
			return refuse(wire.CodeForbidden)
		}
		if app, err = r.Apps.App(ctx, open.App); err != nil {
			return refuse(wire.CodeNotFound)
		}
		// An app this controller does not run answers nothing. deactivate is
		// a root task a user signed for, so the row deciding here is what
		// that task bought.
		if !app.Active {
			return refuse(wire.CodeNotFound)
		}
		// The manifest comes from this controller's own database, written
		// when the app was activated; a row that cannot be read is this
		// server's fault, not the opener's.
		if m, err = store.ParseManifest(app.Manifest); err != nil {
			return refuse(wire.CodeInternal)
		}
		// The manifest says which service an action runs on. The OPEN names
		// one too, and the two have to be the same: an action matched by its
		// name alone would let any OPEN put a service of its choice under the
		// rule of the tamest action the app has.
		act, ok := m.Action(open.Action)
		if !ok || act.Service != open.Service {
			return refuse(wire.CodeNotFound)
		}
		// What the action may demand — root, changes, confirmation — comes
		// from that manifest alone, never from the OPEN or its params.
		if rule, err = store.RuleFor(m, open.Action); err != nil {
			return refuse(wire.CodeNotFound)
		}
	}

	var (
		tk   *task.Task
		user string
		name string
	)
	if withUser {
		tk, err = task.Check(task.Input{Task: open.Task, Confirm: open.Confirm, Session: *o.Session,
			Node: r.Node, App: open.App, Service: open.Service, Action: open.Action, Rule: rule}, r.List, r.Now)
		if err != nil {
			return refuse(taskCode(err))
		}
		if tk != nil {
			// A task runs once. Its ID is kept until it expires, so the same
			// signed task cannot open a second channel.
			if err := r.Tasks.UseTask(ctx, tk.ID, tk.Expires, r.Now); err != nil {
				if errors.Is(err, dbapi.ErrUsed) {
					return refuse(wire.CodeVerify) // this task has run already
				}
				return refuse(wire.CodeInternal) // the store could not answer
			}
		}
		// The API can attach any certificate it holds. What binds this
		// channel to the user is the signature of the task, or the
		// end-to-end handshake that follows, whose message 1 stands in the
		// OPEN. With neither, the channel names no user: nothing is
		// authorised or logged by one.
		if tk != nil || len(open.E2E) != 0 {
			user = o.Session.Cert().User
			// session.VerifyClient found the key of this user in this list,
			// so the list holds the user.
			u, _ := r.List.User(user)
			name = u.Name
		}
	}
	return Decision{Opened: o, Action: a, Core: isCore, Task: tk, User: user, UserName: name, App: app, Manifest: m}, nil
}

// taskCode is the code a failed task check closes the channel with: the shell
// may ask the user to confirm and try again, it may be told that the user has
// no root permission here, and everything else — from a bad signature to a
// task made out for something else or for another time — ends it as E_VERIFY.
func taskCode(err error) wire.Code {
	switch {
	case errors.Is(err, task.ErrNoTask), errors.Is(err, task.ErrConfirm):
		return wire.CodeConfirm
	case errors.Is(err, task.ErrForbidden):
		return wire.CodeForbidden
	}
	return wire.CodeVerify
}

// refuse ends the channel with this code and no decision.
func refuse(code wire.Code) (Decision, *wire.Close) {
	cl := wire.Error(code)
	return Decision{}, &cl
}
