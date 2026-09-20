// Package task defines signed tasks and re-confirmation. The shell signs a
// task with the session key; controllers and the API verify the chain from
// the task over the client certificate to the user's key in the trust list.
package task

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Limits.
const (
	IDSize      = 16
	MaxLifetime = 300 // seconds from Issued to Expires
	MaxTask     = 16 << 10
)

// Task describes exactly one channel. If a task comes with an OPEN, its
// Params replace the ones in the OPEN.
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

var (
	ErrTask      = errors.New("task: malformed task or other target")
	ErrNoTask    = errors.New("task: signed task required")
	ErrExpired   = errors.New("task: task not valid at this time")
	ErrForbidden = errors.New("task: no root permission on this server")
	ErrConfirm   = errors.New("task: re-confirmation missing or invalid")
)

// Validate checks the fields of t.
func (t Task) Validate() error {
	if len(t.ID) != IDSize || !trust.ValidID(t.Installation) || len(t.Cert) != 32 ||
		(t.Node != "" && !wire.ValidName(t.Node)) || !wire.ValidAppID(t.App) ||
		!wire.ValidName(t.Service) || !wire.ValidName(t.Action) || !wire.ValidParams(t.Params) ||
		t.Issued <= 0 || t.Expires <= t.Issued || t.Expires-t.Issued > MaxLifetime {
		return ErrTask
	}
	return nil
}

// Verify checks that env is a task signed with the session key of cert,
// bound to the client certificate payload client and valid at now.
func Verify(env sign.Envelope, cert session.ClientCert, client []byte, now int64) (Task, error) {
	if err := sign.Verify(env, ed25519.PublicKey(cert.Sig), sign.PurposeTask); err != nil {
		return Task{}, err
	}
	var t Task
	if err := enc.DecodeStrict(env.Payload, MaxTask, &t); err != nil {
		return Task{}, ErrTask
	}
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	if t.Installation != cert.Installation || !bytes.Equal(t.Cert, session.CertHash(client)) {
		return Task{}, ErrTask
	}
	if now < t.Issued-session.MaxSkew || now >= t.Expires {
		return Task{}, ErrExpired
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

// Input is what the verifier knows about a channel. Root, Critical and
// Changes come from the manifest or the core list, never from the sender.
type Input struct {
	Task     *sign.Envelope
	Confirm  *proof.Proof
	Cert     session.ClientCert // already verified with session.VerifyClient
	Client   []byte             // its payload
	Node     string             // the verifier's own node; empty for the API
	App      string
	Service  string
	Action   string
	Root     bool
	Critical bool
	Changes  bool // every channel to a service counts as a change
}

// Check decides whether a channel may open. It returns the verified task,
// or nil if none was needed and none came.
func Check(in Input, l *trust.List, now int64) (*Task, error) {
	op := trust.Op{App: in.App, Action: in.Action, Node: in.Node, Root: in.Root, Critical: in.Critical, Changes: in.Changes}
	confirm := l.NeedsConfirm(in.Cert.User, op)
	if in.Task == nil {
		if in.Root || confirm {
			return nil, ErrNoTask
		}
		return nil, nil
	}
	t, err := Verify(*in.Task, in.Cert, in.Client, now)
	if err != nil {
		return nil, err
	}
	if t.Node != in.Node || t.App != in.App || t.Service != in.Service || t.Action != in.Action {
		return nil, ErrTask
	}
	if in.Root && !l.MayRoot(in.Cert.User, in.Node) {
		return nil, ErrForbidden
	}
	if confirm && (in.Confirm == nil || VerifyConfirm(*in.Confirm, *in.Task, l, in.Cert.User) != nil) {
		return nil, ErrConfirm
	}
	return &t, nil
}
