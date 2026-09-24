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

// AppLookup returns the row of an app this controller runs. Only
// dbapi.ErrNotFound says there is no such app; every other error is this
// server's own fault. state.Store.App answers it.
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
	// List is the newest trust list version this controller has verified, and
	// it must not change while the check runs. The certificate, the root
	// permission, the re-confirmation and the user's name are read from it
	// one after the other: a caller that writes a new version into the same
	// list has the certificate checked against one and the root permission
	// against the next. Without a list nothing can be checked at all.
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
	// Task is the task this check verified, or nil. Nil does not say that the
	// OPEN carried none: a channel without a user is decided without ever
	// checking one, however wrong the task it brought. Its shape was read all
	// the same — wire.ParseOpen refuses an envelope that is none before this
	// package decides anything.
	Task *task.Task
	// User is empty, or the user of the certificate — and set only where the
	// signature of a task or the end-to-end handshake that follows binds the
	// channel to them. Empty means the channel proves no user at all,
	// whatever certificate it carries: nothing may be authorised or logged
	// by one then.
	User     string
	UserName string          // that user's login name in the trust list
	App      dbapi.App       // the row of the app on an app channel
	Manifest *store.Manifest // its manifest, nil on a core action
}

// Check decides whether the channel of this OPEN may run. It answers either a
// decision or the CLOSE the channel ends with, never both: a refusal says
// nothing but its code.
func Check(ctx context.Context, r Request) (Decision, *wire.Close) {
	// A Request without the store is this program wired wrong, and this line
	// is what keeps the mistake from being fatal: the controller runs as
	// root, and a channel that reaches the missing half would take it down.
	// Which channel that is depends on the half — an app channel asks Apps,
	// a signed task asks Tasks — so both are answered here, before either is
	// reached.
	if r.Apps == nil || r.Tasks == nil {
		return refuse(wire.CodeInternal)
	}
	// Nothing can be checked without the trust list: no certificate, no task,
	// no root permission. One channel runs all the same, and it is the only
	// one — the one that brings the first version (firstChain).
	if r.List == nil {
		return firstChain(r)
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
		// such an ID at all (store.Manifest.Validate). This refuses the OPEN
		// whether it carries a certificate or not; the one that carries one
		// would be a channel with a user and nothing binding them to it,
		// since wire.Open.Validate keeps the end-to-end handshake off every
		// core app ID.
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
			// Only "there is no such app" is the opener's answer. A damaged
			// row or a store that cannot answer is this server's fault.
			if errors.Is(err, dbapi.ErrNotFound) {
				return refuse(wire.CodeNotFound)
			}
			return refuse(wire.CodeInternal)
		}
		// An app this controller does not run answers nothing. deactivate is
		// a root task a user signed for, so the row deciding here is what
		// that task bought.
		if !app.Active {
			return refuse(wire.CodeNotFound)
		}
		// The manifest comes from this controller's own database, written
		// when the app was activated; a manifest that cannot be read leaves
		// no rule to judge by, and that is this server's fault.
		if m, err = store.ParseManifest(app.Manifest); err != nil {
			return refuse(wire.CodeInternal)
		}
		// Row, manifest and OPEN have to name one app. A row that answers
		// under another ID, or carries the manifest of another app, would put
		// this channel under a rule that belongs to something else.
		if app.ID != open.App || m.ID != open.App {
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

// firstChain decides the one channel a controller may run while it has
// verified no version of the trust list: ngc.users/controller/trust, the
// channel the API sends the first chain on. Everything else is refused, the
// way everything was refused here before.
//
// Why this one is not a hole in the gate, and what it rests on:
//
//   - A controller between pairing and its first chain holds an anchor and
//     nothing else. Pairing gives it no version — tunnel.PairCode carries a
//     trust.Anchor, a number and a hash, and PathPair answers 204 with a body
//     that carries nothing — and no contract path lets a controller fetch
//     one: it speaks PathTunnel, PathPair and PathJoin, while
//     session.PathTrust belongs to the browser. The contract says it outright
//     (package wire, CtlTrust): controllers get versions on this channel.
//   - So the state is one the design means: chain.Keeper.Load "leaves the
//     keeper empty and gives no error: the controller then runs on until an
//     API sends the first chain", and Apply verifies that chain from the
//     anchor (trust.Open) and not from a list. The anchor is the root here,
//     and refusing this channel would leave a paired server no way ever to
//     get a list.
//   - The exception gives nothing away, because a channel without a
//     certificate never reads the list anyway: tunnel.VerifyOpen answers
//     before session.VerifyClient when there is no proof. What this decision
//     leaves out is what could not be checked — and so it names no user, no
//     task, no app.
//   - The other way round is why a certificate has to be turned away here
//     and not further down: session.VerifyClient would take the list into
//     session.verifyCert, which compares c.Installation against l.Installation
//     before it asks the list anything else — on a nil list that is a crash, in
//     a program that runs as root. Measured with the panic caught and its
//     stack read: session.go:828, by way of tunnel.VerifyOpen and
//     session.VerifyClient. trust.List.User, which an earlier reading of this
//     named, is never reached.
//
// Four marks, and every one of them is fenced by a subtest of
// TestCheckWithoutTrustListTakesOnlyTheFirstChain. Whoever widens one of them
// turns that subtest red. Widening this is not a convenience: any second
// action here would be one this server carries out for an API it has not yet
// been able to check.
func firstChain(r Request) (Decision, *wire.Close) {
	// ParseOpen is the parse that needs no list; VerifyOpen would be the same
	// call without a proof, but only this one says so at the door.
	//
	// This refusal changes no answer, and that is measured, not assumed:
	// ParseOpen gives back two zero values with its error, and three empty
	// names find nothing in core.Lookup, so an OPEN that is none is turned away
	// by mark 2 whether this line stands or not. A mutation that drops it
	// survives the whole package. It stays because reading on from values that
	// mean nothing is how the next mark comes to be read from a payload nobody
	// checked; the subtest "an open that is none" holds the answer and the
	// lookup this rests on.
	o, inner, err := tunnel.ParseOpen(r.Payload)
	if err != nil {
		return refuse(wire.CodeUnreachable)
	}
	// Mark 1: no certificate and no proof.
	if len(o.Client) != 0 || o.Proof != nil {
		return refuse(wire.CodeUnreachable)
	}
	// Mark 2: this one action and no other. The name is compared in all three
	// of its parts, so no app of its own can carry an action called trust in
	// here.
	a, ok := core.Lookup(inner.App, inner.Service, inner.Action)
	if !ok || a.App != core.AppUsers || a.Service != core.ServiceController || a.Name != core.ActTrust {
		return refuse(wire.CodeUnreachable)
	}
	// Marks 3 and 4: the decision carries no session and no user (nothing
	// verified one), and no task (nothing could check one). Everything else
	// of a Decision stays at its zero value for the same reason.
	return Decision{Opened: tunnel.Opened{Open: inner, Window: o.Window}, Action: a, Core: true}, nil
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
