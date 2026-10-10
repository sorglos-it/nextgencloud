// Package coreapp serves the channels of the core apps (ngc.desktop,
// ngc.users, ngc.store) and hands every other channel to package relay. Core
// channels carry no end-to-end handshake of their own: the session to the API
// is already encrypted end to end (package wire, "Core apps").
//
// This package is the first caller of rights.Check, and that is where the
// promise of package rights is kept or broken. Check decides out of user,
// role, app, action and node, and it may read nothing unsigned out of the
// OPEN. What holds that is the resolving done here, and nowhere else:
//
//   - Request.App and Request.Manifest come from the app's row in the database
//     and from the manifest in that row, never from the OPEN. The OPEN only
//     says which row to look for; what the row answers is what decides. The
//     OPEN may therefore name an app it is not: the right that opens the
//     channel is the one of the resolved row
//     (TestTheResolvedAppDecidesAndNotTheOpen and its control).
//   - Request.Action is the action core.Lookup resolved, for a core channel,
//     and empty for an app channel, where Check takes the action out of the
//     manifest.
//   - Request.Node for a core controller action comes out of the task in the
//     OPEN (nodeOfTask), and that is a signed part: trust.MayRoot is checked
//     against exactly that node, and task.Check refuses a task whose own node
//     is a different one. For an app channel the node comes out of the row.
//
// What is logged, and why a complaint of the log never ends a channel: every
// channel that opens is written down (docs/superpowers/specs/
// 2026-09-19-ngc-kern-design.md, "Protokoll"). audit.ErrDropped says that the
// entry was written and that one field of it reads as dropped; whoever treated
// it as a refusal would turn a saved entry into a lost one, so it is ignored
// here on purpose. Only audit.ErrEntry writes nothing, and only an invented
// kind reaches it -- this package uses the entry points that are there
// (Log.Channel, Log.Task) and invents none.
package coreapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// trustList is the trust list as package state hands it out: the cached one,
// which every caller shares. Read it, never change it.
type trustList = trust.List

// verified is the chain back to the user's key, as the login made it.
type verifiedSession = session.Verified

// Tunnels is what the dispatcher needs of the tunnels that are open right
// now; *controllers.Registry implements it. It is an interface and not that
// type because a controller cannot be added to a registry from outside
// package controllers: without it a case could only ever measure an app whose
// controller is away, and "unreachable" would be the only state this package
// ever answered.
type Tunnels interface {
	Get(node string) (*controllers.Tunnel, bool)
}

// Relay forwards a channel to a controller; package relay implements it.
//
// raw is the payload of the browser's OPEN frame, byte for byte. It travels
// next to the parsed o and NOTHING can take its place: the end-to-end prologue
// of the forwarded channel hashes exactly these bytes
// (noisep.E2EPrologue), and an OPEN that was decoded and encoded again is a
// different byte string -- member order and spacing differ, and task 17
// measured it with a space the JSON encoder does not write. A relay handed
// only o would send the controller an OPEN that is not the shell's, and the
// far side would answer with a decryption error, which names the wrong layer.
//
// So: what is passed on here is the MEASURED thing and not the asserted one.
// Whoever "tidies" raw away because o looks like it carries the same
// information breaks every app channel, and the failure will not point here.
type Relay interface {
	Forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open, raw []byte,
		t *task.Task, node string)
	// CallController opens a channel to a controller without a user
	// (core.WithoutUser) and returns the result, or the code the channel
	// closed with. The API uses it to push a new trust list (users.go,
	// pushTrust).
	CallController(ctx context.Context, tun *controllers.Tunnel, app, service, action string,
		params json.RawMessage, in []byte) ([]byte, wire.Code)
}

// JoinCodes hands out the one-time code a new API server joins with; *join.Join
// implements it. Like Tunnels it is an interface and not that type because the
// package it lives in is a later task, and ngc.users/join-code has to be
// measurable before it exists.
//
// Nothing of a code ever reaches a log or a message from here: users.go takes
// the code, puts it into the result and forgets it.
type JoinCodes interface {
	NewJoinCode(ctx context.Context) (tunnel.JoinCode, int64, error)
}

// Deps are what the dispatcher needs.
type Deps struct {
	S     *state.State
	Log   *audit.Log
	Login *login.Handler
	// Tunnels are the controller tunnels open right now, Controllers the
	// server that pairs new ones, Sessions the open shells and Join the join
	// codes of a new API server; the actions of ngc.users use all four.
	Tunnels     Tunnels
	Controllers *controllers.Server
	Sessions    *browser.Registry
	Join        JoinCodes
	Relay       Relay
}

// Call is one core action the API carries out itself.
type Call struct {
	S      *state.State
	Live   *login.Live
	Action core.Action
	Params core.Params
	Task   *task.Task
	In     io.Reader // FlowIn: at most Action.MaxIn bytes
	Out    io.Writer // FlowOut: the API writes the data itself
	Admin  bool
	Rights map[string]bool
}

// Handler serves one core action; the bytes it returns are the result.
type Handler func(ctx context.Context, rq *Call) ([]byte, error)

// Dispatcher routes a channel to its handler.
type Dispatcher struct {
	Deps
	handlers map[string]Handler
	// totp holds the secrets of ngc.desktop/totp-begin until a code confirms
	// them; desktop.go says why they stay on the server.
	totp totpStore
}

// New returns a dispatcher with the handlers of ngc.desktop and ngc.users;
// tasks 19 to 23 register theirs beside them.
func New(d Deps) *Dispatcher {
	dp := &Dispatcher{Deps: d, handlers: map[string]Handler{}}
	dp.registerDesktop()
	dp.registerUsers()
	return dp
}

// Register adds a handler. A second one for the same action replaces the
// first, which is what lets a test put its own answer in place of ours.
func (d *Dispatcher) Register(app, service, name string, h Handler) {
	d.handlers[app+"/"+service+"/"+name] = h
}

// errClose carries the code a channel closes with.
type errClose struct{ code wire.Code }

func (e errClose) Error() string { return string(e.code) }

// fail returns an error that closes the channel with code.
func fail(code wire.Code) error { return errClose{code} }

// codeOf returns the code an error closes with; anything unexpected is
// E_INTERNAL, and nothing of the error ever reaches the shell.
func codeOf(err error) wire.Code {
	var e errClose
	if errors.As(err, &e) {
		return e.code
	}
	return wire.CodeInternal
}

// OpenChannel serves one channel the browser opened.
func (d *Dispatcher) OpenChannel(ctx context.Context, s *browser.Session, c *link.Channel,
	o wire.Open, raw []byte) {
	code, node := d.open(ctx, s, c, o, raw)
	user := s.Live.Verified.Cert().User
	// The answer is dropped on purpose: audit.ErrDropped means the entry was
	// written with one field replaced, and a channel that ran must not be
	// turned into a failed one by a complaint about the log. See the package
	// comment.
	_ = d.Log.Channel(ctx, user, o.App, o.Action, node, code)
	if code != "" {
		_ = s.Close(ctx, code, c.Num)
	}
}

// open does the work and returns the code to close with -- "" when the channel
// ran and closed itself -- and the node the channel went to, for the log.
func (d *Dispatcher) open(ctx context.Context, s *browser.Session, c *link.Channel,
	o wire.Open, raw []byte) (wire.Code, string) {
	v, err := d.S.Trust(ctx)
	if err != nil {
		return wire.CodeUnreachable, ""
	}
	now := d.S.Now()
	verified := s.Live.Verified
	if !wire.CoreApp(o.App) {
		return d.forward(ctx, s, c, o, raw, v.List, &verified, now)
	}
	action, ok := core.Lookup(o.App, o.Service, o.Action)
	if !ok {
		return wire.CodeNotFound, ""
	}
	node := nodeOfTask(o)
	t, code := rights.Check(ctx, d.S.DB, v.List, rights.Request{
		Action: action, Node: node, Open: o, Session: &verified}, now)
	if code != "" {
		return code, node
	}
	d.logTask(ctx, verified.Cert().User, o, t)
	if action.Service == core.ServiceController {
		// A root task of service controller runs on a controller; the API only
		// forwards it. The node is the verified task's own, not the guess
		// nodeOfTask read out of the unverified payload.
		if t == nil {
			return wire.CodeConfirm, node
		}
		d.Relay.Forward(ctx, s, c, o, raw, t, t.Node)
		return "", t.Node
	}
	granted, admin, err := rights.Of(ctx, d.S.DB, v.List, verified.Cert().User)
	if err != nil {
		return wire.CodeUnreachable, ""
	}
	h, ok := d.handlers[action.App+"/"+action.Service+"/"+action.Name]
	if !ok {
		return wire.CodeNotFound, ""
	}
	return d.run(ctx, s, c, o, action, t, h, granted, admin), ""
}

// forward resolves an app channel and hands it to the relay. App, manifest and
// node all come from the row; the OPEN only picked which row to read.
func (d *Dispatcher) forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open,
	raw []byte, l *trustList, verified *verifiedSession, now int64) (wire.Code, string) {
	row, err := d.S.DB.AppGet(ctx, o.App)
	switch {
	case errors.Is(err, dbapi.ErrNotFound):
		// An app that is not installed answers exactly like a missing right,
		// so the answer tells a user without the right nothing about the
		// installation (package rights, Check).
		return wire.CodeForbidden, ""
	case err != nil:
		return wire.CodeUnreachable, ""
	}
	m, err := store.ParseManifest(row.Manifest)
	if err != nil {
		// A manifest the database cannot hold is a broken row, and a broken
		// row grants nothing.
		return wire.CodeForbidden, ""
	}
	t, code := rights.Check(ctx, d.S.DB, l, rights.Request{
		App: &row, Manifest: m, Node: row.Node, Open: o, Session: verified}, now)
	if code != "" {
		return code, row.Node
	}
	d.logTask(ctx, verified.Cert().User, o, t)
	d.Relay.Forward(ctx, s, c, o, raw, t, row.Node)
	return "", row.Node
}

// logTask notes a signed task with its signature. The signature goes in as
// base64url, which is the one form audit.Task keeps: handed the raw bytes it
// would drop them, and the task would stand in the log without the proof of
// who ordered it.
func (d *Dispatcher) logTask(ctx context.Context, user string, o wire.Open, t *task.Task) {
	if t == nil {
		return
	}
	sig := ""
	if o.Task != nil {
		sig = enc.B64.EncodeToString(o.Task.Sig)
	}
	_ = d.Log.Task(ctx, user, t.App, t.Action, t.Node, sig)
}

// run answers the first GRANT, reads the input, calls the handler and sends
// the result.
func (d *Dispatcher) run(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open,
	action core.Action, t *task.Task, h Handler, granted map[string]bool, admin bool) wire.Code {
	// With a task the signed params are the params; the OPEN carries none of
	// its own then (wire.Open.Validate, and rights.Check again).
	raw := o.Params
	if t != nil {
		raw = t.Params
	}
	params, err := core.ParseParams(action, raw)
	if err != nil {
		return wire.CodeProtocol
	}
	// A core channel carries no end-to-end handshake of its own, so this GRANT
	// carries no Noise message 2: the session to the API is already encrypted
	// end to end (package wire, "Core apps").
	if err := s.SendGrant(ctx, c, wire.DefaultWindow, nil); err != nil {
		return wire.CodeInternal
	}
	var in io.Reader = bytes.NewReader(nil)
	if action.Flow == core.FlowIn {
		body, code := s.ReadInput(ctx, c, action.MaxIn)
		if code != "" {
			return code
		}
		in = bytes.NewReader(body)
	}
	out := &bytes.Buffer{}
	rq := &Call{S: d.S, Live: s.Live, Action: action, Params: params, Task: t, In: in, Out: out,
		Admin: admin, Rights: granted}
	body, err := h(ctx, rq)
	if err != nil {
		return codeOf(err)
	}
	if action.Flow == core.FlowOut {
		// A data stream and not a result, so wire.MaxCallResult is not its
		// bound; what the one FlowOut action carries is a ui-tar, whose bound
		// is store.MaxUITotal.
		body = out.Bytes()
		if len(body) > store.MaxUITotal {
			return wire.CodeTooLarge
		}
	} else if len(body) > wire.MaxCallResult {
		return wire.CodeTooLarge
	}
	if err := s.SendResult(ctx, c, body); err != nil {
		return wire.CodeInternal
	}
	return ""
}

// nodeOfTask returns the node a task names, "" when the OPEN carries none: the
// API checks a task with the node it will forward to.
//
// This reads a field out of a payload nothing has verified yet, and that is
// allowed for one reason only: the value picks the target, and task.Check
// compares it against the signed task and refuses a mismatch (t.Node !=
// in.Node is an error). So a wrong value here can refuse a channel, never open
// one on a node the signed task did not name.
func nodeOfTask(o wire.Open) string {
	if o.Task == nil {
		return ""
	}
	var t struct {
		Node string `json:"node"`
	}
	if json.Unmarshal(o.Task.Payload, &t) != nil {
		return ""
	}
	return t.Node
}
