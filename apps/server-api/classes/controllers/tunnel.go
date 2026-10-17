package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// IdleTimeout bounds the quiet time on a tunnel, and it is the one deadline
// that stands after link.FirstFrameTimeout has been met. It is policy, not
// protocol: no byte on the line depends on it.
//
// Two things need it. A controller sends a ping every tunnel.PingInterval
// seconds, so a tunnel that says nothing for three of those intervals is gone
// whatever the socket still claims -- without this, a dead controller holds a
// goroutine and a file handle until the operating system notices, which can be
// hours. And it is the ceiling on how long a withdrawn key keeps a tunnel: the
// trust list is read again for every frame that arrives (trusted), so a
// controller the admin removed loses its tunnel at its next frame, and one that
// sends nothing at all loses it here.
const IdleTimeout = 3 * tunnel.PingInterval * time.Second

// Server serves the tunnel paths of this API.
type Server struct {
	S       *state.State
	Tunnels *Registry

	// Idle is the quiet time one tunnel of this server may have; zero or less
	// means IdleTimeout. A test sets it to milliseconds instead of waiting.
	Idle time.Duration

	// OnHello runs after a controller's hello is accepted: the API then sends
	// it the newest trust list and the licenses of its apps (tasks 16 and 21).
	OnHello func(ctx context.Context, t *Tunnel)
	// OnReport takes an update report, OnNotice a notice (tasks 23 and 15).
	OnReport func(ctx context.Context, t *Tunnel, r store.UpdateReport)
	OnNotice func(ctx context.Context, t *Tunnel, n core.Notice)
}

// idle is the quiet time of this server's tunnels.
func (srv *Server) idle() time.Duration {
	if srv.Idle <= 0 {
		return IdleTimeout
	}
	return srv.Idle
}

// Tunnel serves GET tunnel.PathTunnel?key=<fingerprint of the controller's
// Noise key>. The key says whose tunnel it is; Noise proves it.
func (srv *Server) Tunnel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	peer, node, ok := srv.peerKey(ctx, r.URL.Query().Get("key"))
	if !ok {
		// A key nobody knows gets no link and no hint why.
		w.WriteHeader(http.StatusNotFound)
		return
	}
	static, err := srv.staticKey()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	l, err := link.Accept(w, r, link.Options{
		Subprotocol: tunnel.Subprotocol, Prologue: noisep.PrologueTunnel,
		Static: static, Peer: peer,
	})
	if err != nil {
		// Accept answered the request itself wherever it could.
		return
	}
	t := &Tunnel{Node: node, key: peer, link: l,
		table: link.NewTable(wire.MaxTunnelChannels, true)}
	srv.run(ctx, t)
}

// peerKey finds the controller a fingerprint belongs to: a node of the trust
// list, or a controller that paired and waits to be listed (which then has no
// node ID yet and may open nothing).
func (srv *Server) peerKey(ctx context.Context, fingerprint string) ([]byte, string, bool) {
	if fingerprint == "" {
		return nil, "", false
	}
	if v, err := srv.S.Trust(ctx); err == nil && v.List != nil {
		for _, n := range v.List.Nodes {
			if n.Kind == trust.NodeController && tunnel.Fingerprint(n.Noise) == fingerprint {
				return n.Noise, n.ID, true
			}
		}
	}
	list, err := srv.S.DB.PendingList(ctx)
	if err != nil {
		return nil, "", false
	}
	for _, p := range list {
		if tunnel.Fingerprint(p.Key) == fingerprint {
			return p.Key, "", true
		}
	}
	return nil, "", false
}

// trusted asks again whether the peer of an open tunnel is still the node it
// was when the tunnel was made. It is asked for every frame that arrives, and
// that is the whole point: before this, a key an admin withdrew kept its tunnel
// for as long as the socket stood, because nothing ever looked at a running
// tunnel again. The list comes from state.Trust, which answers out of what it
// already holds, so the price per frame is a read lock and a walk over the
// nodes.
//
// The key decides, not the node ID: a node that keeps its ID but gets a new
// Noise key is another peer, and whoever holds the old key has to go.
func (srv *Server) trusted(ctx context.Context, t *Tunnel) bool {
	key, node, ok := srv.peerKey(ctx, tunnel.Fingerprint(t.key))
	return ok && node == t.Node && bytes.Equal(key, t.key)
}

// staticKey returns this API's Noise key pair.
func (srv *Server) staticKey() (noise.DHKey, error) {
	priv := srv.S.Cfg.NoiseKey.Bytes()
	pub, err := srv.noisePublic()
	if err != nil {
		return noise.DHKey{}, err
	}
	return noise.DHKey{Private: priv, Public: pub}, nil
}

// run exchanges hello and then reads the tunnel until it ends.
func (srv *Server) run(ctx context.Context, t *Tunnel) {
	defer srv.Tunnels.remove(t)
	// Whoever serves a channel waits in link.Channel.Next; once the tunnel is
	// gone they hear that at once instead of waiting out their own clock.
	defer t.table.Hangup()
	if err := srv.sendHello(ctx, t); err != nil {
		_ = t.link.Close(websocket.StatusInternalError, "hello")
		return
	}
	for {
		// Every read is bounded: a ctx that ends a Read ends the link with it
		// (link.Read), which is what IdleTimeout is for.
		rctx, cancel := context.WithTimeout(ctx, srv.idle())
		f, err := t.link.Read(rctx)
		cancel()
		if err != nil {
			return
		}
		if !srv.trusted(ctx, t) {
			_ = t.link.Close(websocket.StatusPolicyViolation, "trust")
			return
		}
		if f.Channel == wire.ControlChannel {
			if !srv.control(ctx, t, f) {
				_ = t.link.Close(websocket.StatusProtocolError, "control")
				return
			}
			continue
		}
		// The API is the opener on a tunnel, so every number on it is one the
		// API picked: a frame for a number that is not in the table belongs to
		// a channel that was never opened, and that ends the tunnel.
		c, ok := t.table.Get(f.Channel)
		if !ok {
			_ = t.link.Close(websocket.StatusProtocolError, "channel")
			return
		}
		if !t.frame(c, f) {
			_ = t.link.Close(websocket.StatusProtocolError, "channel")
			return
		}
	}
}

// frame counts one frame of a numbered channel and puts it into that
// channel's queue, where whoever serves the channel takes it with
// Tunnel.Next. False ends the tunnel.
//
// The counting happens HERE, in the tunnel's one reader, and not in whoever
// serves the channel: this is the only code that sees a frame arrive, so a
// relay cannot forget to count what it was handed. What may be sent is counted
// on the other side, in Tunnel.SendData and Tunnel.Grant (hop.go).
//
// A GRANT is counted and still queued, unlike on a session link: its payload
// may carry Noise message 2 of the end-to-end handshake, and whoever serves
// the channel has to see that. A peer that sent nothing but GRANTs to fill the
// queue runs into link.MaxQueuedFrames, which answers false here, and into
// wire.MaxWindow, which answers false in Grant.
func (t *Tunnel) frame(c *link.Channel, f wire.Frame) bool {
	switch f.Type {
	case wire.TypeData:
		if !c.Take(len(f.Payload)) {
			return false
		}
	case wire.TypeGrant:
		credit, _, err := wire.ParseGrant(f.Payload)
		if err != nil || !c.Grant(credit) {
			return false
		}
	case wire.TypeEOF, wire.TypeClose:
	default:
		return false
	}
	if !t.table.Deliver(f.Channel, f) {
		return false
	}
	if f.Type == wire.TypeClose {
		c.RecvClose()
	}
	return true
}

// sendHello sends this API's hello, the first message on channel 0.
func (srv *Server) sendHello(ctx context.Context, t *Tunnel) error {
	seq := uint64(0)
	if v, err := srv.S.Trust(ctx); err == nil && v.List != nil {
		seq = v.List.Seq
	}
	return t.SendControl(ctx, wire.CtlHello, tunnel.Hello{
		Version: Version, Contract: tunnel.Contract, Node: srv.S.Cfg.Node, TrustSeq: seq,
	})
}

// control handles a frame on channel 0 of a tunnel; false ends the tunnel.
// Channel 0 carries control messages as DATA and nothing else; the type check
// is kept for the reason browser.control names, that this package's own
// statement about channel 0 must not rest on another package's list of frame
// types staying what it is.
func (srv *Server) control(ctx context.Context, t *Tunnel, f wire.Frame) bool {
	if f.Type != wire.TypeData {
		return false
	}
	c, err := wire.ParseControl(f.Payload)
	if err != nil || !wire.ControlAllowed(wire.LinkTunnel, false, c.T) {
		return false
	}
	switch c.T {
	case wire.CtlHello:
		h, err := tunnel.ParseHello(c.B)
		if err != nil {
			return false
		}
		// A hello that names another node than the one whose key made this
		// tunnel ends it: the key decides, never the message.
		//
		// NAMING NO NODE IS NOT NAMING ANOTHER ONE, and the field says so
		// itself (tunnel.Hello.Node: "empty until the controller is in the
		// trust list"). Two controllers send an empty one: the one that only
		// paired, and -- measured on a running installation on 17.10.2026 --
		// the one an admin has just listed. The second learns its ID only from
		// a version of the list that lists it, and a controller gets versions
		// of the list on this tunnel, AFTER its hello was taken. Comparing an
		// empty node against t.Node closed that circle: the tunnel of a
		// freshly listed controller was ended on every dial, and no app could
		// ever run.
		//
		// What the rule protects is untouched: t.Node comes out of peerKey, so
		// out of the KEY, and nothing below reads h.Node. An empty hello buys
		// its sender no other node, only its own.
		if h.Node != "" && h.Node != t.Node {
			return false
		}
		t.setHello(h)
		if t.Node == "" {
			// It may say hello and nothing else until an admin lists it, so it
			// never reaches the registry: whoever asks the registry for a
			// controller asks for one the trust list names.
			return true
		}
		if old := srv.Tunnels.add(t); old != nil && old != t {
			_ = old.link.Close(websocket.StatusNormalClosure, "replaced")
		}
		if srv.OnHello != nil {
			// Its own goroutine for the reason browser.channel names: a tunnel
			// has exactly one reader, and a hook called in line here would hold
			// that one reader for as long as it runs.
			go func() {
				// Not channel work and not a round: this is the hook of ONE
				// tunnel, run once per hello. A panic in it may not take the
				// server, and there is nothing to answer -- so the tunnel is
				// left standing and the controller gets its trust versions
				// and licences at its next hello, which is what the hook's own
				// comment in server.hello already promises. Channel 0: a
				// tunnel carries no channel of a browser here.
				defer func() { _ = guard.Hit(recover(), "controllers.hello", 0) }()
				srv.OnHello(ctx, t)
			}()
		}
		return true
	case wire.CtlStatus:
		s, err := tunnel.ParseStatus(c.B)
		if err != nil {
			return false
		}
		t.setStatus(s)
		return true
	case wire.CtlUpdate:
		r, err := store.ParseUpdateReport(c.B)
		if err != nil {
			return false
		}
		if srv.OnReport != nil {
			srv.OnReport(ctx, t, r)
		}
		return true
	case wire.CtlNotify:
		n, err := core.ParseNotice(c.B)
		if err != nil {
			return false
		}
		if srv.OnNotice != nil {
			srv.OnNotice(ctx, t, n)
		}
		return true
	}
	// What is left is CtlBye: the controller closes the tunnel itself, and so
	// do we. ControlAllowed let nothing else through.
	return false
}

// SendControl writes one control message onto the tunnel. It is one of the
// named ways out of this package; the link itself stays unexported.
func (t *Tunnel) SendControl(ctx context.Context, kind string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(wire.Control{T: kind, B: raw})
	if err != nil {
		return err
	}
	return t.link.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: payload})
}

// End closes the whole tunnel in order.
func (t *Tunnel) End(reason string) { _ = t.link.Close(websocket.StatusNormalClosure, reason) }

// Version is the NGC version this API reports in its hello; main.go sets it.
var Version = "0.0.0-dev"
