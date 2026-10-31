package rights

import (
	"context"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
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
// Manifest from the database, Node from the app's row or the action.
//
// What the promise of this package excludes, and what it allows:
//
// No VALUE out of an unsigned part of the OPEN moves the decision, and that is
// a narrower sentence than "it is not read". Measured, every read of req.Open
// in this package (grep -n 'Open\.[A-Za-z]*' check.go rights.go, five hits):
//
//   - Window and E2E: no read at all.
//   - Params: the LENGTH is read and it does refuse -- an OPEN that carries a
//     task and params of its own is E_VERIFY below, because the task's params
//     are the params. Nothing of the content is looked at.
//   - Action: read for an app channel only, to find the action in the
//     manifest, and replaced at once by the manifest's own name, so it can hit
//     a right that is there but never invent one.
//   - Task and Confirm are signed and are the next paragraph.
//
// A SIGNED part moves the decision, and that is the point: Task and Confirm
// are evidence, and task.Check verifies both against the trust list before
// anything counts. A task for another channel can only refuse — it names a
// channel this one is not. A matching task with the user's re-confirmation
// opens a channel that E_CONFIRM had just turned away, which is exactly what
// E_CONFIRM asks for. The signature is what makes the difference, so a
// re-confirmation is no parameter: it is the user's answer.
type Request struct {
	Action   core.Action       // the core action, empty for an app channel
	App      *dbapi.App        // the installed app, for an app channel
	Manifest *store.Manifest   // its manifest, parsed
	Node     string            // the controller the channel runs on, "" for the API itself
	Open     wire.Open         // the OPEN of the shell
	Session  *session.Verified // nil for a channel the API opens without a user
}

// Check decides whether the channel may open. It returns the verified task, if
// one was needed, the wire.Code to close with and the sentence that says which
// check said no; an empty code means the channel may open, and the reason is
// then reason.None.
//
// The reason exists because a code alone is no reason. Measured on 19.10.2026
// on the Debian machine: a user with no root grant on that controller was
// turned away here, and the only thing anybody could read of it afterwards was
// "Das hat der Rechner nicht angenommen." -- the honest sentence
// (reason.NotRoot) was in task.ErrForbidden all along and was thrown away in
// codeOf. It is a sentence of the CLOSED list of package reason and never a
// text of the error: see that package for why, and for which of the two
// readers (log, screen) may see which.
func Check(ctx context.Context, db dbapi.Store, l *trust.List, req Request, now int64) (*task.Task, wire.Code, reason.Reason) {
	if req.Session == nil {
		// Channels without a user are the API's own; the shell never opens one.
		if req.Action.Name == "" || !core.WithoutUser(req.Action) {
			return nil, wire.CodeForbidden, reason.ShellClosed
		}
		return nil, "", reason.None
	}
	// wire.Open.Validate already says an OPEN with a task carries no params of
	// its own, since the task's params are the params and the rule judged
	// those. Checked again here, so the decision does not rest on how careful
	// the caller was.
	if req.Open.Task != nil && len(req.Open.Params) != 0 {
		return nil, wire.CodeVerify, reason.WithParams
	}
	user := req.Session.Cert().User
	granted, admin, err := Of(ctx, db, l, user)
	if err != nil {
		// A row the database cannot hold is not "no rights": nothing opens
		// until an admin has set it again.
		return nil, wire.CodeUnreachable, reason.DB
	}
	var rule task.Rule
	app, service, action := req.Action.App, req.Action.Service, req.Action.Name
	switch {
	case req.Action.Name != "":
		// The shell may open the actions of service api and the root tasks of
		// service controller; stage, install and license are the API's own.
		if !core.ShellOpens(req.Action) {
			return nil, wire.CodeForbidden, reason.ShellClosed
		}
		if req.Action.Admin && !admin {
			return nil, wire.CodeForbidden, reason.AdminOnly
		}
		rule = core.RuleFor(req.Action)
	default:
		// An app that is not installed, carries no manifest or is switched
		// off answers exactly like a missing right, so the answer tells a
		// user without the right nothing about the installation. The price:
		// such a user cannot tell a typo in the app's name from a right they
		// do not have, which makes their own search harder.
		// The one reason that goes into the log and not onto the screen
		// (reason.Screen): the log tells an admin of this installation which
		// of the four it was, and the shell is told no more than E_FORBIDDEN,
		// so the indistinguishability above survives the sentence.
		if req.App == nil || req.Manifest == nil || !req.App.Active {
			return nil, wire.CodeForbidden, reason.AppUnknown
		}
		if !usable(granted, req.App.ID) {
			return nil, wire.CodeForbidden, reason.NoRight
		}
		// core.RightUse is "use", and no contract keeps a manifest from
		// naming an action "use": the right <app>/use would then be the
		// action's right as well, and mere visibility would open the channel.
		// Refused here because the clean fix — core.ValidRight or store
		// separating the two namespaces — lies in frozen shared/. The price:
		// the same check then stands twice, and THIS one has to go once the
		// contract carries it. A licence check must not hang off <app>/use
		// either, for the same reason.
		if req.Open.Action == core.RightUse {
			return nil, wire.CodeNotFound, reason.ActionUnknown
		}
		a, ok := req.Manifest.Action(req.Open.Action)
		if !ok {
			return nil, wire.CodeNotFound, reason.ActionUnknown
		}
		if !has(granted, req.App.ID, a.Name) {
			return nil, wire.CodeForbidden, reason.NoRight
		}
		r, err := store.RuleFor(req.Manifest, a.Name)
		if err != nil {
			return nil, wire.CodeNotFound, reason.ActionUnknown
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
		// Code and reason come out of the SAME error, so the two can never
		// tell different stories about one decision.
		return nil, codeOf(err), reason.ForTask(err)
	}
	if t != nil {
		// A task counts once: the ID is spent until it expires, so nobody can
		// replay a signed task.
		if err := db.OnceCreate(ctx, dbapi.OnceTask, t.ID, "", t.Expires); err != nil {
			return nil, wire.CodeVerify, reason.TaskSpent
		}
	}
	return t, "", reason.None
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
