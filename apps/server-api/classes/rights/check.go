package rights

import (
	"context"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Request is what the API knows about a channel that wants to open. Only the
// fields the API resolved itself decide: Action from core.Lookup, App and
// Manifest from the database, Node from the app's row or the action. Of Open
// nothing but Action names the channel, and Task and Confirm are evidence that
// task.Check verifies against the trust list — its params, window and
// handshake never reach a decision.
type Request struct {
	Action   core.Action       // the core action, empty for an app channel
	App      *dbapi.App        // the installed app, for an app channel
	Manifest *store.Manifest   // its manifest, parsed
	Node     string            // the controller the channel runs on, "" for the API itself
	Open     wire.Open         // the OPEN of the shell
	Session  *session.Verified // nil for a channel the API opens without a user
}

// Check decides whether the channel may open. It returns the verified task, if
// one was needed, and the wire.Code to close with; an empty code means the
// channel may open.
func Check(ctx context.Context, db dbapi.Store, l *trust.List, req Request, now int64) (*task.Task, wire.Code) {
	if req.Session == nil {
		// Channels without a user are the API's own; the shell never opens one.
		if req.Action.Name == "" || !core.WithoutUser(req.Action) {
			return nil, wire.CodeForbidden
		}
		return nil, ""
	}
	user := req.Session.Cert().User
	granted, admin, err := Of(ctx, db, l, user)
	if err != nil {
		// A row the database cannot hold is not "no rights": nothing opens
		// until an admin has set it again.
		return nil, wire.CodeUnreachable
	}
	var rule task.Rule
	app, service, action := req.Action.App, req.Action.Service, req.Action.Name
	switch {
	case req.Action.Name != "":
		// The shell may open the actions of service api and the root tasks of
		// service controller; stage, install and license are the API's own.
		if !core.ShellOpens(req.Action) {
			return nil, wire.CodeForbidden
		}
		if req.Action.Admin && !admin {
			return nil, wire.CodeForbidden
		}
		rule = core.RuleFor(req.Action)
	default:
		if req.App == nil || req.Manifest == nil {
			return nil, wire.CodeNotFound
		}
		if !req.App.Active {
			return nil, wire.CodeNotFound
		}
		if !usable(granted, req.App.ID) {
			// Without the right "use" the app does not exist for this user, so
			// the answer says nothing more than "not allowed".
			return nil, wire.CodeForbidden
		}
		a, ok := req.Manifest.Action(req.Open.Action)
		if !ok {
			return nil, wire.CodeNotFound
		}
		if !has(granted, req.App.ID, a.Name) {
			return nil, wire.CodeForbidden
		}
		r, err := store.RuleFor(req.Manifest, a.Name)
		if err != nil {
			return nil, wire.CodeNotFound
		}
		// App, action and service come from the row and the manifest, so a
		// task must name exactly the channel the API resolved.
		rule, app, service, action = r, req.App.ID, a.Service, a.Name
	}
	in := task.Input{
		Task: req.Open.Task, Confirm: req.Open.Confirm, Session: *req.Session,
		Node: req.Node, App: app, Service: service, Action: action,
		Rule: rule,
	}
	t, err := task.Check(in, l, now)
	if err != nil {
		return nil, codeOf(err)
	}
	if t != nil {
		// A task counts once: the ID is spent until it expires, so nobody can
		// replay a signed task.
		if err := db.OnceCreate(ctx, dbapi.OnceTask, t.ID, "", t.Expires); err != nil {
			return nil, wire.CodeVerify
		}
	}
	return t, ""
}

// codeOf maps the errors of package task to the codes of the contract.
func codeOf(err error) wire.Code {
	switch err {
	case task.ErrNoTask, task.ErrConfirm:
		return wire.CodeConfirm
	case task.ErrForbidden:
		return wire.CodeForbidden
	}
	return wire.CodeVerify
}
