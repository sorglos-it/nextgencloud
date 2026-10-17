package controllers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// controllerHello is the hello of the fixture's controller.
func controllerHello(fx fixture) tunnel.Hello {
	return tunnel.Hello{Version: "0.1.0", Contract: tunnel.Contract,
		Node: fx.ControllerNode, TrustSeq: 1, Arch: "amd64", OS: "debian-13",
		Roles: []string{tunnel.RoleServices}}
}

func TestTunnelExchangesHello(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	hello := c.readHello(t)
	if hello.Contract != tunnel.Contract || hello.Version == "" || hello.Node != srv.S.Cfg.Node {
		t.Fatalf("hello %+v", hello)
	}
	tun := waitForTunnel(t, srv, fx.ControllerNode)
	if tun.Hello().Arch != "amd64" {
		t.Fatalf("registry %+v", tun.Hello())
	}
}

func TestTunnelEndsOnAForeignNode(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	// The hello names a node other than the one whose key made the tunnel.
	h := controllerHello(fx)
	h.Node = "web-1"
	c.writeControl(t, wire.CtlHello, h)
	wantEnded(t, c)
	waitForNoTunnel(t, srv)
}

func TestTunnelTakesStatusAndReports(t *testing.T) {
	srv, fx := newServer(t)
	seen := make(chan store.UpdateReport, 1)
	srv.OnReport = func(_ context.Context, _ *Tunnel, r store.UpdateReport) { seen <- r }
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	c.writeControl(t, wire.CtlStatus, tunnel.Status{DiskFree: 1 << 30,
		Components: map[string]string{"ngc-controller": "0.1.0"}})
	c.writeControl(t, wire.CtlUpdate, store.UpdateReport{Target: "ngc-controller",
		Version: "0.2.0", State: store.UpdateDownloaded})
	var r store.UpdateReport
	select {
	case r = <-seen:
	case <-time.After(readWait):
		t.Fatal("the report never reached OnReport")
	}
	if r.Target != "ngc-controller" || r.State != store.UpdateDownloaded {
		t.Fatalf("report %+v", r)
	}
	tun := waitForTunnel(t, srv, fx.ControllerNode)
	// The status arrived before the report, and the report is in hand, so the
	// status is recorded by now.
	if tun.Status().DiskFree != 1<<30 {
		t.Fatalf("status %+v", tun.Status())
	}
}

// TestTunnelTakesANotice holds the fourth control message a controller may
// send.
func TestTunnelTakesANotice(t *testing.T) {
	srv, fx := newServer(t)
	seen := make(chan core.Notice, 1)
	srv.OnNotice = func(_ context.Context, _ *Tunnel, n core.Notice) { seen <- n }
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	c.writeControl(t, wire.CtlNotify, core.Notice{Level: core.NoticeWarn, Text: "Platte fast voll."})
	select {
	case n := <-seen:
		if n.Text != "Platte fast voll." {
			t.Fatalf("notice %+v", n)
		}
	case <-time.After(readWait):
		t.Fatal("the notice never reached OnNotice")
	}
}

// TestTunnelRunsOnHelloBesideTheReader holds that the hook does not hold the
// one reader of the tunnel: a hook that blocks must not stop the next frame.
func TestTunnelRunsOnHelloBesideTheReader(t *testing.T) {
	srv, fx := newServer(t)
	block := make(chan struct{})
	called := make(chan struct{}, 1)
	srv.OnHello = func(_ context.Context, _ *Tunnel) {
		called <- struct{}{}
		<-block
	}
	defer close(block)
	seen := make(chan core.Notice, 1)
	srv.OnNotice = func(_ context.Context, _ *Tunnel, n core.Notice) { seen <- n }
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	select {
	case <-called:
	case <-time.After(readWait):
		t.Fatal("OnHello never ran")
	}
	// The hook is still inside its own goroutine, and the next frame goes
	// through all the same.
	c.writeControl(t, wire.CtlNotify, core.Notice{Level: core.NoticeInfo, Text: "Weiter geht es."})
	select {
	case <-seen:
	case <-time.After(readWait):
		t.Fatal("the reader is held by OnHello")
	}
}

// TestTunnelNeedsAKnownKey holds the query: a fingerprint of a key no list
// names gets no link at all, and says nothing about why.
func TestTunnelNeedsAKnownKey(t *testing.T) {
	_, fx := newServer(t)
	stranger, _ := trusttest.NodeKeys(0x5A)
	for _, c := range []struct{ name, key string }{
		{"no key", ""},
		{"a key nobody knows", tunnel.Fingerprint(stranger.Public)},
		{"not a fingerprint", "nonsense"},
	} {
		if _, err := dialTunnelErr(t, fx, stranger, c.key); err == nil {
			t.Errorf("%s got a tunnel", c.name)
		}
	}
}

// TestTunnelEndsWhenTheKeyLeavesTheTrustList is the second condition of the
// task: an open tunnel is asked again, so a controller an admin took out of the
// trust list loses its tunnel instead of keeping it for as long as the socket
// stands.
func TestTunnelEndsWhenTheKeyLeavesTheTrustList(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	waitForTunnel(t, srv, fx.ControllerNode)
	// The admin takes the controller out of the installation.
	withoutTheController(t, srv, fx)
	// Its very next frame is the last one.
	c.writeControl(t, wire.CtlStatus, tunnel.Status{DiskFree: 1})
	wantEnded(t, c)
	waitForNoTunnel(t, srv)
}

// TestTunnelEndsAfterTheIdleTime is the other half of that condition: a
// controller that says nothing at all cannot hold a tunnel for ever either, so
// a withdrawn key is gone at the latest after the idle time.
func TestTunnelEndsAfterTheIdleTime(t *testing.T) {
	srv, fx := newServer(t)
	srv.Idle = 150 * time.Millisecond
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	// Nothing more is sent. The tunnel ends on its own.
	wantEnded(t, c)
}

// TestTunnelKeepsRunningWhileFramesArrive is the control to the case above: the
// idle time must not end a tunnel that is talking, or the case above would
// hold nothing about silence.
func TestTunnelKeepsRunningWhileFramesArrive(t *testing.T) {
	srv, fx := newServer(t)
	srv.Idle = 400 * time.Millisecond
	seen := make(chan core.Notice, 8)
	srv.OnNotice = func(_ context.Context, _ *Tunnel, n core.Notice) { seen <- n }
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	// Four rounds of half the idle time each: more than the idle time in all,
	// and the tunnel stands because every frame starts the clock again.
	for i := 0; i < 4; i++ {
		time.Sleep(200 * time.Millisecond)
		c.writeControl(t, wire.CtlNotify, core.Notice{Level: core.NoticeInfo, Text: "Ich bin noch da."})
		select {
		case <-seen:
		case <-time.After(readWait):
			t.Fatalf("round %d: the tunnel ended although frames kept arriving", i)
		}
	}
}

// TestTunnelEndsOnAForbiddenControlMessage holds the direction of the control
// messages: what only the API may send ends a tunnel that sends it.
func TestTunnelEndsOnAForbiddenControlMessage(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	c.writeControl(t, wire.CtlTrust, map[string]uint64{"seq": 2})
	wantEnded(t, c)
	waitForNoTunnel(t, srv)
}

// TestTunnelEndsOnBye holds the one message that ends a tunnel in order.
func TestTunnelEndsOnBye(t *testing.T) {
	_, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	c.writeControl(t, wire.CtlBye, struct{}{})
	wantEnded(t, c)
}

// TestTunnelEndsOnAFrameForAChannelNobodyOpened holds the use of the table: the
// API picks the numbers on a tunnel, so a number it never picked is no channel.
//
// The open way stands in the case below it, and that is what keeps this one
// honest: a DATA frame on a number the API DID open does not end the tunnel
// and lands in that channel's queue. Without that pair, a tunnel that ended on
// every DATA frame would pass here -- measured on a mutant that dropped the
// table check and stayed alive while the hook was the only reason it died.
func TestTunnelEndsOnAFrameForAChannelNobodyOpened(t *testing.T) {
	_, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	if err := c.writeFrame(wire.Frame{Type: wire.TypeData, Channel: 7, Payload: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	wantEnded(t, c)
}

// TestAFrameOfAnOpenedChannelWaitsInItsQueue is the open way of the case
// above, and at the same time the seam package relay reads through: the
// tunnel's one reader counts the frame and puts it in the channel's queue,
// where Tunnel.Next hands it out. Nobody but that reader touches the socket.
func TestAFrameOfAnOpenedChannelWaitsInItsQueue(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	tn := waitForTunnel(t, srv, fx.ControllerNode)
	ch, err := tn.Open(wire.DefaultWindow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := tn.Grant(ctx, ch, wire.MinWindow); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := c.writeFrame(wire.Frame{Type: wire.TypeData, Channel: ch.Num, Payload: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	f, got := tn.Next(rctx, ch)
	if !got || f.Type != wire.TypeData || string(f.Payload) != "x" {
		t.Fatalf("%+v %v", f, got)
	}
}

// TestTunnelEndsOnABrokenHello holds the parser in front of the hello.
func TestTunnelEndsOnABrokenHello(t *testing.T) {
	_, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	raw, err := json.Marshal(wire.Control{T: wire.CtlHello, B: []byte(`{"contract":0}`)})
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	c.writeRaw(t, raw)
	wantEnded(t, c)
}

// TestAPairedControllerSaysHelloAndNothingMore holds what a controller that is
// not in the trust list yet may do: say hello, and not reach the registry. The
// notice after the hello is the sync point and not decoration: the API answers
// with its own hello BEFORE it reads the controller's, so a case that asks the
// registry right after reading that answer asks before the hello was handled --
// measured on a mutant that did file a pending controller and stayed alive.
func TestAPairedControllerSaysHelloAndNothingMore(t *testing.T) {
	srv, fx := newServer(t)
	seen := make(chan core.Notice, 1)
	srv.OnNotice = func(_ context.Context, _ *Tunnel, n core.Notice) { seen <- n }
	ctrl, _ := trusttest.NodeKeys(0x4C)
	if err := srv.S.DB.PendingPut(ctx, dbapi.Pending{Name: "services-2", Key: ctrl.Public, Since: testNow}); err != nil {
		t.Fatalf("pending: %v", err)
	}
	c := dialTunnel(t, fx, ctrl)
	// A pending controller has no node ID, so its hello names none.
	c.writeControl(t, wire.CtlHello, tunnel.Hello{Version: "0.1.0", Contract: tunnel.Contract,
		TrustSeq: 1, Arch: "amd64", Roles: []string{tunnel.RoleServices}})
	c.readHello(t)
	c.writeControl(t, wire.CtlNotify, core.Notice{Level: core.NoticeInfo, Text: "Ich warte."})
	select {
	case <-seen:
	case <-time.After(readWait):
		t.Fatal("the notice after the hello never arrived, so the hello was never handled")
	}
	// The hello is handled by now, and the tunnel is nowhere in the registry:
	// whoever asks for a controller asks for one the trust list names.
	if n := srv.Tunnels.Count(); n != 0 {
		t.Fatalf("a pending controller reached the registry: %d", n)
	}
	if _, ok := srv.Tunnels.Get(""); ok {
		t.Fatal("a tunnel was filed under the empty node")
	}
}

// TestASecondTunnelReplacesTheFirst holds what a controller that reconnects
// after a broken link must not leave behind.
func TestASecondTunnelReplacesTheFirst(t *testing.T) {
	srv, fx := newServer(t)
	first := dialTunnel(t, fx, fx.ControllerKeys)
	first.writeControl(t, wire.CtlHello, controllerHello(fx))
	first.readHello(t)
	one := waitForTunnel(t, srv, fx.ControllerNode)
	second := dialTunnel(t, fx, fx.ControllerKeys)
	second.writeControl(t, wire.CtlHello, controllerHello(fx))
	second.readHello(t)
	// The old one is closed, and the registry holds exactly one tunnel: the new
	// one.
	wantEnded(t, first)
	deadline := time.Now().Add(readWait)
	for {
		two, ok := srv.Tunnels.Get(fx.ControllerNode)
		if ok && two != one {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second tunnel never took the place of the first")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.Tunnels.Count(); n != 1 {
		t.Fatalf("the registry holds %d tunnels", n)
	}
	if nodes := srv.Tunnels.Nodes(); len(nodes) != 1 || nodes[0] != fx.ControllerNode {
		t.Fatalf("nodes %v", nodes)
	}
}

// TestRegistryEachReachesEveryTunnel holds Each, which is how a later task
// reaches every controller at once.
func TestRegistryEachReachesEveryTunnel(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	waitForTunnel(t, srv, fx.ControllerNode)
	var seen []string
	srv.Tunnels.Each(func(tun *Tunnel) { seen = append(seen, tun.Node) })
	if len(seen) != 1 || seen[0] != fx.ControllerNode {
		t.Fatalf("Each saw %v", seen)
	}
}

// waitForNoTunnel waits until the registry is empty again. The controller sees
// the close frame before the handler has come back from its read, so a case
// that asks at once can be ahead of the handler -- measured, one case of this
// package was.
func waitForNoTunnel(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(readWait)
	for {
		n := srv.Tunnels.Count()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the registry still holds %d tunnels", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForTunnel waits until the registry holds the tunnel of a node. The hello
// is answered by the API before the controller's own hello is read, so a case
// that reads the answer and asks at once can be ahead of the handler.
func waitForTunnel(t *testing.T, srv *Server, node string) *Tunnel {
	t.Helper()
	deadline := time.Now().Add(readWait)
	for {
		if tun, ok := srv.Tunnels.Get(node); ok {
			return tun
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tunnel of %s never reached the registry", node)
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAPanicInTheHelloHookKeepsTheTunnel is the catch of the hello hook. It is
// neither channel work nor a background round: the hook runs once per hello of
// ONE controller, in a goroutine of its own (so it does not hold the tunnel's
// single reader), and a goroutine is where a panic is final -- unqueued, it
// takes the whole ngc-api, with every session of every user and every other
// controller's tunnel (V1.2 point 38).
//
// What follows a catch here is therefore nothing but the line: the tunnel is
// left standing, and the controller gets its trust versions and its licences
// at its next hello, which is what the hook promises anyway (Server.hello,
// "Nothing in here may fail the hello"). That the tunnel really is left
// standing is the half measured below -- the next frame still reaches its hook.
func TestAPanicInTheHelloHookKeepsTheTunnel(t *testing.T) {
	srv, fx := newServer(t)
	before := guard.Panics()
	called := make(chan struct{}, 1)
	srv.OnHello = func(_ context.Context, _ *Tunnel) {
		called <- struct{}{}
		panic("a programming error in the hello hook")
	}
	seen := make(chan core.Notice, 1)
	srv.OnNotice = func(_ context.Context, _ *Tunnel, n core.Notice) { seen <- n }
	c := dialTunnel(t, fx, fx.ControllerKeys)
	c.writeControl(t, wire.CtlHello, controllerHello(fx))
	c.readHello(t)
	select {
	case <-called:
	case <-time.After(readWait):
		t.Fatal("OnHello never ran")
	}
	// The tunnel lives: the next frame goes through as before. Without the
	// catch this case does not fail here -- the test binary dies.
	c.writeControl(t, wire.CtlNotify, core.Notice{Level: core.NoticeInfo, Text: "Weiter geht es."})
	select {
	case <-seen:
	case <-time.After(readWait):
		t.Fatal("the tunnel died with the hook")
	}
	if got := guard.Panics(); got != before+1 {
		t.Fatalf("the counter went %d -> %d; one panic must count once", before, got)
	}
}

// TestTunnelTakesAHelloThatNamesNoNode holds the other half of the rule
// TestTunnelEndsOnAForeignNode keeps: naming ANOTHER node ends the tunnel,
// naming NO node does not. The contract says so of the field itself
// (tunnel.Hello.Node, "empty until the controller is in the trust list"), and
// a controller only ever learns its ID from a version of the list that lists
// it -- which it cannot be handed before its hello is taken. Refusing the
// empty one closed that circle: a controller an admin had just listed could
// never say hello again.
//
// The ID still comes from the KEY and never from the message: the tunnel has
// to reach the registry under the node of the fixture's controller, not under
// nothing.
func TestTunnelTakesAHelloThatNamesNoNode(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, fx, fx.ControllerKeys)
	h := controllerHello(fx)
	h.Node = ""
	c.writeControl(t, wire.CtlHello, h)
	hello := c.readHello(t)
	if hello.Contract != tunnel.Contract {
		t.Fatalf("hello %+v", hello)
	}
	tun := waitForTunnel(t, srv, fx.ControllerNode)
	if tun.Node != fx.ControllerNode {
		t.Fatalf("the tunnel reached the registry as %q, want %q", tun.Node, fx.ControllerNode)
	}
	if tun.Hello().Node != "" {
		t.Fatalf("the hello was changed on the way: %+v", tun.Hello())
	}
}
