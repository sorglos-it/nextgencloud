// Package task defines signed tasks and re-confirmation. The shell signs a
// task with the session key; controllers and the API verify the chain from
// the task over the client certificate to the user's key in the trust list.
package task

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Limits.
const (
	IDSize      = 16
	MaxLifetime = 300 // seconds from Issued to Expires
	MaxTask     = 16 << 10
)

// taskBudget bounds a task while it is decoded (enc.Budget): it has no array
// of its own, and its params stay raw bytes, which the rule and core read.
var taskBudget = enc.Budget{Size: MaxTask, Items: map[string]int{"params": enc.Raw}}

// Task describes exactly one channel. An OPEN that carries a task carries no
// params of its own (wire.Open.Validate); the task's params are the params.
type Task struct {
	ID           enc.Bytes       `json:"id"` // random; verifiers remember it until Expires
	Installation string          `json:"inst"`
	Cert         enc.Bytes       `json:"cert"` // session.CertHash of the client certificate whose key signs
	Node         string          `json:"node"` // target controller; empty for core actions the API carries out
	App          string          `json:"app"`
	Service      string          `json:"service"`
	Action       string          `json:"action"`
	Params       json.RawMessage `json:"params,omitempty"`
	Issued       int64           `json:"iat"`
	Expires      int64           `json:"exp"`
}

// Errors from Verify, VerifyConfirm and Check. The caller maps them to
// wire.Code: ErrNoTask and ErrConfirm to E_CONFIRM (the shell asks the user
// to confirm and tries again), ErrForbidden to E_FORBIDDEN, and everything
// else — including errors returned as-is from packages sign and proof — to
// E_VERIFY.
var (
	ErrTask      = errors.New("task: malformed task or other target")
	ErrNoTask    = errors.New("task: signed task required")
	ErrExpired   = errors.New("task: task not valid at this time")
	ErrForbidden = errors.New("task: no root permission on this server")
	ErrConfirm   = errors.New("task: re-confirmation missing or invalid")
)

// Validate checks the fields of t.
func (t Task) Validate() error {
	if len(t.ID) != IDSize || !trust.ValidID(t.Installation) || len(t.Cert) != sha256.Size ||
		(t.Node != "" && !wire.ValidName(t.Node)) || !wire.ValidAppID(t.App) ||
		!wire.ValidName(t.Service) || !wire.ValidName(t.Action) || !wire.ValidParams(t.Params) ||
		t.Issued <= 0 || t.Expires <= t.Issued || t.Expires-t.Issued > MaxLifetime {
		return ErrTask
	}
	return nil
}

// Verify checks that env is a task signed with the session key of v, bound
// to v's client certificate payload and valid at now.
func Verify(env sign.Envelope, v session.Verified, now int64) (Task, error) {
	if len(env.Payload) > MaxTask {
		return Task{}, ErrTask
	}
	cert := v.Cert()
	if now >= cert.Expires {
		return Task{}, ErrExpired
	}
	t, err := verifyBound(env, cert, v.Payload())
	if err != nil {
		return Task{}, err
	}
	if now < t.Issued-session.MaxSkew || now >= t.Expires {
		return Task{}, ErrExpired
	}
	return t, nil
}

// VerifyApproval checks env as Verify does — signed with the session key of
// the client certificate cert, whose payload is client, bound to it, valid in
// form and of this installation — but not the time, and without a Verified:
// for evidence of what a user approved in the past (core.UIProof). Nothing
// may run on it; it remembers no task ID. cert is what
// session.VerifyApproval returned for client.
func VerifyApproval(env sign.Envelope, cert session.ClientCert, client []byte) (Task, error) {
	if len(env.Payload) > MaxTask {
		return Task{}, ErrTask
	}
	return verifyBound(env, cert, client)
}

// verifyBound is what Verify and VerifyApproval share: the signature by the
// session key of cert, before the payload is decoded, then the task's form
// and its binding to the client certificate payload client.
func verifyBound(env sign.Envelope, cert session.ClientCert, client []byte) (Task, error) {
	if err := sign.Verify(env, ed25519.PublicKey(cert.Sig), sign.PurposeTask); err != nil {
		return Task{}, err
	}
	var t Task
	if err := taskBudget.Decode(env.Payload, &t); err != nil {
		return Task{}, ErrTask
	}
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	if t.Installation != cert.Installation || !bytes.Equal(t.Cert, session.CertHash(client)) {
		return Task{}, ErrTask
	}
	return t, nil
}

// VerifyConfirm checks a re-confirmation of the task in env by a key of user.
func VerifyConfirm(p proof.Proof, env sign.Envelope, l *trust.List, user string) error {
	owner, key, ok := l.KeyOwner(p.KeyID)
	if !ok || owner.ID != user {
		return ErrConfirm
	}
	return proof.Verify(p, key, l.RP, sign.PurposeConfirm, env.Payload)
}

// Rule says how a verifier judges a channel. The verifier builds it from
// what it holds itself — core.RuleFor for core actions, store.RuleFor from
// the installed app's manifest — never from the OPEN or its params.
type Rule struct {
	Root    bool // root task on a controller: a signed task is required and the user needs root there
	Changes bool // every channel to a service counts as a change
	// Critical reports whether the action is critical with these params.
	// Check calls it with the verified task's params, or with nil when no
	// task came. The answer for nil must hold for any params an OPEN without
	// a task may carry, since those are not signed: an action that some
	// params make critical answers true (or an error) for nil. Params the
	// action does not accept give an error, which Check returns as ErrTask.
	Critical func(params json.RawMessage) (bool, error)
}

// Input is what the verifier knows about a channel.
type Input struct {
	Task    *sign.Envelope
	Confirm *proof.Proof
	Session session.Verified // the chain back to the user's key; only session.VerifyClient makes one
	// Node is the controller the channel runs on. A controller passes its
	// own node ID. The API passes "" for core actions it carries out itself
	// (service "api"); when it checks a task before forwarding it, it passes
	// the node it forwards to: for ngc.store/controller actions the task's
	// node (task.Verify first), for an app the node the app runs on.
	Node                 string
	App, Service, Action string
	Rule                 Rule
}

// Check decides whether a channel may open. It returns the verified task,
// or nil if none was needed and none came. Whether the action is critical
// comes from the rule applied to the signed params, so a sender cannot make
// a critical action look harmless.
func Check(in Input, l *trust.List, now int64) (*Task, error) {
	if in.Rule.Critical == nil {
		return nil, ErrTask
	}
	cert := in.Session.Cert()
	op := trust.Op{App: in.App, Action: in.Action, Node: in.Node, Root: in.Rule.Root, Changes: in.Rule.Changes}
	if in.Task == nil {
		if in.Rule.Root {
			return nil, ErrNoTask
		}
		critical, err := in.Rule.Critical(nil)
		if err != nil {
			return nil, ErrTask
		}
		op.Critical = critical
		if l.NeedsConfirm(cert.User, op) {
			return nil, ErrNoTask
		}
		return nil, nil
	}
	t, err := Verify(*in.Task, in.Session, now)
	if err != nil {
		return nil, err
	}
	if t.Node != in.Node || t.App != in.App || t.Service != in.Service || t.Action != in.Action {
		return nil, ErrTask
	}
	critical, err := in.Rule.Critical(t.Params)
	if err != nil {
		return nil, ErrTask
	}
	op.Critical = critical
	if in.Rule.Root && !l.MayRoot(cert.User, in.Node) {
		return nil, ErrForbidden
	}
	if l.NeedsConfirm(cert.User, op) && (in.Confirm == nil || VerifyConfirm(*in.Confirm, *in.Task, l, cert.User) != nil) {
		return nil, ErrConfirm
	}
	return &t, nil
}
