package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

const testNow = int64(1790000000)

// readWait bounds every read of a case. It is generous on purpose: it is here
// so that a case which would otherwise wait for ever fails instead, not to
// measure anything.
const readWait = 10 * time.Second

// testClient dials with SO_LINGER 0, so a closed case leaves no socket in
// TIME_WAIT; a run of this package opens one tunnel per case.
var testClient = &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		return c, err
	},
}}

// fixture holds what a case needs to speak for a controller.
type fixture struct {
	// Version is the trust list version the API has verified, which a pairing
	// code has to fit.
	Version trust.Version
	// Admin signs a further version of the trust list, for the cases that take
	// a controller out of it again.
	Admin  trusttest.Signer
	Signed trust.Signed
	// ControllerNode and ControllerKeys are the controller of
	// trusttest.Genesis, the one node of kind controller in version 1.
	ControllerNode string
	ControllerKeys noise.DHKey
	// APIPub is this API's public Noise key, the peer of the handshake.
	APIPub []byte
	// HTTP serves tunnel.PathTunnel, so a case can really dial.
	HTTP *httptest.Server
}

// newServer builds an API whose trust list holds one controller, with the
// tunnel path served in front of it. It is the fixture of package browser once
// more -- test helpers are not visible across packages -- with two differences
// that matter here: the configuration carries the URL and the Noise key of the
// api-1 node of trusttest.Genesis and the pin of its TLS key, because a pairing
// code names all three and tunnel.PairCode.Check compares them against that
// node.
func newServer(t *testing.T) (*Server, fixture) {
	t.Helper()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	noiseKey, signKey := trusttest.NodeKeys(0xA1)
	_, apiPin := trusttest.APITLS(0xE1, "api-1.test")
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noiseKey.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey:    config.NewSecret(make([]byte, 32)),
		ChallengeKey: config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		ReleaseKey:   enc.Bytes(make([]byte, 32)), StoreKey: enc.Bytes(make([]byte, 32)),
		StoreURL: "https://store.ngc.test", SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, apiPin)
	s.Now = func() int64 { return testNow }
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	signed := admin.Sign(t, l)
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(l.Installation), Updated: testNow}); err != nil {
		t.Fatalf("installation: %v", err)
	}
	v, err := s.AddVersion(ctx, signed)
	if err != nil {
		t.Fatalf("version 1: %v", err)
	}
	ctlKeys, _ := trusttest.NodeKeys(0xC1)
	var node string
	for _, n := range l.Nodes {
		if n.Kind == trust.NodeController {
			node = n.ID
		}
	}
	if node == "" {
		t.Fatal("the fixture's trust list names no controller")
	}
	srv := &Server{S: s, Tunnels: NewRegistry()}
	pub, err := srv.noisePublic()
	if err != nil {
		t.Fatalf("noise public: %v", err)
	}
	fx := fixture{Version: v, Admin: admin, Signed: signed, ControllerNode: node,
		ControllerKeys: ctlKeys, APIPub: pub}
	fx.HTTP = httptest.NewServer(http.HandlerFunc(srv.Tunnel))
	t.Cleanup(fx.HTTP.Close)
	return srv, fx
}

// withoutTheController adds a version of the trust list that no longer names
// the controller, as an admin does who takes a server out of the installation.
func withoutTheController(t *testing.T, srv *Server, fx fixture) {
	t.Helper()
	l := trusttest.Next(t, fx.Signed)
	kept := l.Nodes[:0]
	for _, n := range l.Nodes {
		if n.Kind != trust.NodeController {
			kept = append(kept, n)
		}
	}
	l.Nodes = kept
	if _, err := srv.S.AddVersion(ctx, fx.Admin.Sign(t, l)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
}

// post builds a POST with the right content type.
func post(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// conn is a controller's end of one tunnel.
type conn struct {
	ws *websocket.Conn
	tr *noisep.Transport
}

// dialTunnel opens the tunnel path with the fingerprint of a controller's Noise
// key in the query and runs the Noise handshake as initiator. It carries no
// Origin header, which is how a controller reaches the tunnel.
func dialTunnel(t *testing.T, fx fixture, keys noise.DHKey) *conn {
	t.Helper()
	c, err := dialTunnelErr(t, fx, keys, tunnel.Fingerprint(keys.Public))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

// dialTunnelErr is dialTunnel with the query value picked by the case and the
// error handed back, so a case can hold that a key is refused.
func dialTunnelErr(t *testing.T, fx fixture, keys noise.DHKey, key string) (*conn, error) {
	t.Helper()
	ws, _, err := websocket.Dial(ctx, "ws"+fx.HTTP.URL[len("http"):]+tunnel.PathTunnel+"?key="+key,
		&websocket.DialOptions{
			Subprotocols: []string{tunnel.Subprotocol},
			HTTPClient:   testClient,
		})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	hs, err := noisep.Handshake(true, []byte(noisep.PrologueTunnel), keys, fx.APIPub, zeroReader{})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Fatalf("message 1: %v", err)
	}
	if err := ws.Write(ctx, websocket.MessageBinary, msg1); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	_, msg2, err := ws.Read(rctx)
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	_, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil {
		t.Fatalf("read message 2: %v", err)
	}
	c := &conn{ws: ws, tr: noisep.NewTransport(true, cs1, cs2)}
	t.Cleanup(c.close)
	return c, nil
}

// zeroReader gives the ephemeral key of the controller's handshake from a fixed
// byte, so a case stays deterministic.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 7
	}
	return len(p), nil
}

func (c *conn) close() { _ = c.ws.CloseNow() }

// writeControl sends one control message on channel 0.
func (c *conn) writeControl(t *testing.T, kind string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	payload, err := json.Marshal(wire.Control{T: kind, B: raw})
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	c.writeRaw(t, payload)
}

// writeRaw sends one DATA frame on channel 0 with the payload as it is, so a
// case and the fuzz target can put anything there.
func (c *conn) writeRaw(t *testing.T, payload []byte) {
	t.Helper()
	// A payload the tunnel refuses ends it, and then our own write fails too:
	// that is not the case failing.
	_ = c.writeFrame(wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: payload})
}

// writeFrame seals one frame and sends it.
func (c *conn) writeFrame(f wire.Frame) error {
	raw, err := f.Encode()
	if err != nil {
		return err
	}
	sealed, err := c.tr.Seal(raw)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageBinary, sealed)
}

// errTimeout says that the case's own deadline ran out, which is not the tunnel
// ending. Telling the two apart is what makes a case about "the tunnel ends"
// able to fall at all: a read that simply waits also answers an error.
var errTimeout = errText("the read ran into the case's own deadline")

// errText is a fixed error of a helper.
type errText string

func (e errText) Error() string { return string(e) }

// readErr returns the next frame, errTimeout when nothing came, or the error
// that ended the tunnel.
func (c *conn) readErr() (wire.Frame, error) {
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	kind, msg, err := c.ws.Read(rctx)
	if err != nil {
		if rctx.Err() != nil {
			return wire.Frame{}, errTimeout
		}
		return wire.Frame{}, err
	}
	if kind != websocket.MessageBinary {
		return wire.Frame{}, errText("not binary")
	}
	plain, err := c.tr.Open(msg)
	if err != nil {
		return wire.Frame{}, err
	}
	return wire.Decode(plain)
}

// readHello reads the API's hello.
func (c *conn) readHello(t *testing.T) tunnel.Hello {
	t.Helper()
	f, err := c.readErr()
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if f.Channel != wire.ControlChannel || f.Type != wire.TypeData {
		t.Fatalf("the first frame was %+v", f)
	}
	ctl, err := wire.ParseControl(f.Payload)
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if ctl.T != wire.CtlHello {
		t.Fatalf("the first message was %q", ctl.T)
	}
	h, err := tunnel.ParseHello(ctl.B)
	if err != nil {
		t.Fatalf("parse hello: %v", err)
	}
	return h
}

// drain reads until the tunnel ends or the case's own deadline runs out. Both
// are fine for the fuzz target; a panic in the handler is not.
func (c *conn) drain() {
	for {
		if _, err := c.readErr(); err != nil {
			return
		}
	}
}

// wantEnded reads until the tunnel ends and fails the case when it does not: a
// read that only ran out of time says nothing about the tunnel.
func wantEnded(t *testing.T, c *conn) {
	t.Helper()
	for {
		_, err := c.readErr()
		if err == nil {
			continue
		}
		if errors.Is(err, errTimeout) {
			t.Fatal("the tunnel is still standing; the case only ran out of time")
		}
		return
	}
}
