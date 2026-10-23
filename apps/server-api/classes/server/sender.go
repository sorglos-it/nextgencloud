package server

import (
	"context"
	"encoding/json"
	"io"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/coreapp"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/relay"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// sender is the one thing that maps a node name to the tunnel it is reachable
// on, and it exists here and nowhere else for the reason task 22 gave: package
// appstore names a node and must have no way at all to reach a controller it
// did not name, so the lookup is the wiring's job.
//
// A node without an open tunnel is E_UNREACHABLE and not an error of its own.
// That is the same answer appstore gives when it finds no tunnel itself
// (Stores.Node reports false), and it says what is true: the controller is
// away, nothing was sent, and sending it again later is the whole repair.
type sender struct {
	Tunnels *controllers.Registry
	Relay   *relay.Relay
}

// sender fulfils the one seam appstore left open.
var _ appstore.Sender = (*sender)(nil)

// Node reports what the tunnel to node says about itself; ok is false while no
// tunnel is up, which is the only thing appstore reads as "online".
func (sn *sender) Node(node string) (tunnel.Hello, bool) {
	t, ok := sn.Tunnels.Get(node)
	if !ok || t == nil {
		return tunnel.Hello{}, false
	}
	return t.Hello(), true
}

// Transfer opens a core action of flow transfer on node and writes head and
// file on it.
func (sn *sender) Transfer(ctx context.Context, node, action string, params json.RawMessage,
	head core.Transfer, file io.Reader, task *sign.Envelope, client []byte,
	pr, confirm *proof.Proof) ([]byte, wire.Code) {
	t, ok := sn.Tunnels.Get(node)
	if !ok || t == nil {
		return nil, wire.CodeUnreachable
	}
	return sn.Relay.Transfer(ctx, t, core.AppStore, core.ServiceController, action, params,
		head, file, task, client, pr, confirm)
}

// Call opens a core action of flow call on node.
//
// It goes through relay.CallAs and not relay.CallController, and that is the
// difference task 22 named as missing: deactivate and uninstall carry a signed
// task and the user it was signed for, and a channel without a user may only
// carry the actions of core.WithoutUser, which these two are not. Before
// CallAs there was no way to send them at all.
func (sn *sender) Call(ctx context.Context, node, action string, params json.RawMessage,
	task *sign.Envelope, client []byte, pr, confirm *proof.Proof) ([]byte, wire.Code) {
	t, ok := sn.Tunnels.Get(node)
	if !ok || t == nil {
		return nil, wire.CodeUnreachable
	}
	return sn.Relay.CallAs(ctx, t, core.AppStore, core.ServiceController, action, params,
		task, client, pr, confirm)
}

// coreRelay adapts relay.Relay to coreapp.Relay. The two differ in one place:
// package relay takes its destination as the narrow relay.Hop (a number on a
// tunnel and one typed way out per frame type, nothing more), package coreapp
// names *controllers.Tunnel. Go has no implicit conversion between two
// interfaces that differ in a parameter type, so without this wrapper no
// implementation of coreapp.Relay exists at all and the dispatcher cannot be
// built -- which is why it stands here, in the wiring, and not in either of
// the two packages that must not depend on each other.
type coreRelay struct{ rl *relay.Relay }

// coreRelay fulfils what the dispatcher asks for.
var _ coreapp.Relay = coreRelay{}

// Forward hands one channel of the shell to the relay.
func (c coreRelay) Forward(ctx context.Context, s *browser.Session, ch *link.Channel, o wire.Open,
	raw []byte, t *task.Task, node string) {
	c.rl.Forward(ctx, s, ch, o, raw, t, node)
}

// CallController opens a channel to a controller without a user.
func (c coreRelay) CallController(ctx context.Context, t *controllers.Tunnel, app, service,
	action string, params json.RawMessage, in []byte) ([]byte, wire.Code) {
	return c.rl.CallController(ctx, t, app, service, action, params, in)
}
