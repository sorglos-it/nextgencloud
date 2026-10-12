package join

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
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

// readWait bounds every read of a case, so a case that would otherwise wait
// for ever fails instead. It measures nothing.
const readWait = 10 * time.Second

// testClient dials with SO_LINGER 0, so a finished case leaves no socket in
// TIME_WAIT.
var testClient = &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		return c, err
	},
}}

// the two keys the fixture's configuration carries, so a case can hold that
// exactly these travel over PathKeys and nothing else.
var (
	testSecretKey    = bytes.Repeat([]byte{0x5A}, 32)
	testChallengeKey = bytes.Repeat([]byte{0xC7}, 32)
)

// fixture is what a case needs to speak for a joining API server.
type fixture struct {
	// Version is the trust list version this API has verified; a join code
	// has to fit exactly it.
	Version trust.Version
	// Admin signs a further version, for the case that lists the new server.
	Admin  trusttest.Signer
	Signed trust.Signed
	// APIPub is this API's public Noise key: the peer of a handshake and the
	// key a join MAC is bound to.
	APIPub []byte
	// NewKeys is the joining server's Noise key pair, NewSign its Ed25519 key
	// and NewTLS the pin of its TLS key.
	NewKeys noise.DHKey
	NewSign []byte
	NewTLS  []byte
	// HTTP serves PathKeys, so a case can really dial.
	HTTP *httptest.Server
}

// newJoin builds an API whose trust list is trusttest.Genesis, with the keys
// path served in front of it. The configuration carries the URL, the Noise key
// and the TLS pin of that list's api-1 node, because a join code names all
// three and tunnel.JoinCode.Check compares them against that node.
func newJoin(t testing.TB) (*Join, fixture) {
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
		SecretKey:    config.NewSecret(testSecretKey),
		ChallengeKey: config.NewSecret(testChallengeKey),
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
	j := New(s)
	pub, err := j.noisePublic()
	if err != nil {
		t.Fatalf("noise public: %v", err)
	}
	newKeys, newSign := trusttest.NodeKeys(0xA2)
	_, newPin := trusttest.APITLS(0xE2, "api-2.test")
	fx := fixture{Version: v, Admin: admin, Signed: signed, APIPub: pub,
		NewKeys: newKeys, NewSign: newSign.Public().(ed25519.PublicKey), NewTLS: newPin}
	fx.HTTP = httptest.NewServer(http.HandlerFunc(j.Keys))
	t.Cleanup(fx.HTTP.Close)
	return j, fx
}

// listTheNewServer adds a version of the trust list that names the joining
// server as a node of kind api, as an admin does after comparing the
// fingerprint.
func listTheNewServer(t testing.TB, j *Join, fx fixture) {
	t.Helper()
	l := trusttest.Next(t, fx.Signed)
	l.Nodes = append(l.Nodes, trust.Node{ID: "api-2", Kind: trust.NodeAPI, Name: "api-2",
		Noise: enc.Bytes(fx.NewKeys.Public), Sign: enc.Bytes(fx.NewSign),
		URL: "wss://api-2.test:7443/tunnel", TLS: enc.Bytes(fx.NewTLS)})
	// The list wants its nodes ordered by ID; without this the version is
	// refused as malformed and the case would measure that instead.
	slices.SortFunc(l.Nodes, func(a, b trust.Node) int { return strings.Compare(a.ID, b.ID) })
	if _, err := j.S.AddVersion(ctx, fx.Admin.Sign(t, l)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
}

// request builds the join request of the fixture's new server, signed with
// secret.
func (fx fixture) request(secret []byte) tunnel.JoinRequest {
	req := tunnel.JoinRequest{Node: "api-2", Noise: enc.Bytes(fx.NewKeys.Public),
		Sign: enc.Bytes(fx.NewSign), TLS: enc.Bytes(fx.NewTLS), URL: "wss://api-2.test:7443/tunnel"}
	req.MAC = enc.Bytes(tunnel.JoinMAC(secret, req.Node, req.Noise, req.Sign, req.TLS, fx.APIPub, req.URL))
	return req
}

// post builds a POST to PathJoin with the hash of the secret in the query. The
// value is escaped as a real client escapes it, and r.URL.Query() hands the
// handler the string back exactly: without that, a value with a space makes
// httptest.NewRequest panic over the request line, which is this helper
// failing and not the handler.
func post(hash string, body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, tunnel.PathJoin+"?secret="+url.QueryEscape(hash),
		bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// dialKeys opens PathKeys with a fingerprint in the query and runs the Noise
// handshake as initiator, the way the joining server does.
func dialKeys(t *testing.T, fx fixture, keys noise.DHKey, fingerprint string) (*noisep.Transport, *websocket.Conn, error) {
	t.Helper()
	ws, _, err := websocket.Dial(ctx, "ws"+fx.HTTP.URL[len("http"):]+PathKeys+"?key="+fingerprint,
		&websocket.DialOptions{Subprotocols: []string{tunnel.Subprotocol}, HTTPClient: testClient})
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	ws.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	hs, err := noisep.Handshake(true, []byte(PrologueKeys), keys, fx.APIPub, zeroReader{})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Fatalf("message 1: %v", err)
	}
	if err := ws.Write(ctx, websocket.MessageBinary, msg1); err != nil {
		return nil, nil, err
	}
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	_, msg2, err := ws.Read(rctx)
	if err != nil {
		return nil, nil, err
	}
	_, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil {
		return nil, nil, err
	}
	return noisep.NewTransport(true, cs1, cs2), ws, nil
}

// readFrame reads and opens one frame of the keys link.
func readFrame(t *testing.T, tr *noisep.Transport, ws *websocket.Conn) wire.Frame {
	t.Helper()
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	_, msg, err := ws.Read(rctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	raw, err := tr.Open(msg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f, err := wire.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return f
}

// zeroReader gives the ephemeral key of the joining side's handshake from a
// fixed byte, so a case stays deterministic.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 9
	}
	return len(p), nil
}

// listTheWaitingServer adds a version of the trust list that names exactly the
// keys of a waiting request, as an admin does who signs a row of
// ngc.users/pending-api. It is the way Run really takes; listTheNewServer is
// the same step with the fixture's own keys, for the cases that never post.
func listTheWaitingServer(t testing.TB, j *Join, fx fixture, p core.PendingAPI) {
	t.Helper()
	l := trusttest.Next(t, fx.Signed)
	l.Nodes = append(l.Nodes, trust.Node{ID: p.Node, Kind: trust.NodeAPI, Name: p.Node,
		Noise: p.Noise, Sign: p.Sign, URL: p.URL, TLS: p.TLS})
	slices.SortFunc(l.Nodes, func(a, b trust.Node) int { return strings.Compare(a.ID, b.ID) })
	if _, err := j.S.AddVersion(ctx, fx.Admin.Sign(t, l)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
}
