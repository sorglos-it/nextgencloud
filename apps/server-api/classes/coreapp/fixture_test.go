package coreapp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

// The times of the fixture: trusttest.Genesis is issued at issued, and every
// channel opens at testNow, inside the lifetime of certificate and task.
const (
	issued  = int64(1790000000)
	testNow = issued + 10
)

// readWait bounds every read of a case. It is generous on purpose: it is here
// so that a case which would otherwise wait for ever fails instead, not to
// measure anything.
const readWait = 20 * time.Second

// The app rows the cases work with. appX is what an OPEN may claim, appY what
// the database answers for it (lyingStore), so a case can tell the two apart.
const (
	ctlNode = "ctl-1" // the controller of trusttest.Genesis, where admins may run root tasks
	appX    = "org.sorglos.x"
	appY    = "org.sorglos.y"
	appSvc  = "svc"
	appAct  = "doit"
)

// manifestOf is the manifest of a test app: one action on one service,
// nothing critical and no root need, so a channel to it needs no task.
func manifestOf(id string) string {
	return `{"id":"` + id + `","name":"App ` + id + `","version":"1.0.0","publisher":"Sorglos",` +
		`"icon":"logo.png","ui":{"entry":"index.html","width":800,"height":600},` +
		`"actions":[{"name":"` + appAct + `","title":"Tun","service":"` + appSvc + `"}],` +
		`"services":[{"name":"` + appSvc + `","command":["/usr/bin/app"]}]}`
}

// zeroReader gives the fixed X25519 key pair of the browser's session
// certificate, so the case knows the private half the real browser keeps.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 7
	}
	return len(p), nil
}

// testClient dials with SO_LINGER 0, so a closed case leaves no socket in
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

// lyingStore answers AppGet for one ID with the row of another. It is how a
// case measures that the dispatcher resolves the app itself: the OPEN names
// swapFrom, the database answers the row of swapTo, and every decision has to
// follow the row.
type lyingStore struct {
	dbapi.Store
	swapFrom, swapTo string
}

func (s *lyingStore) AppGet(ctx context.Context, id string) (dbapi.App, error) {
	if s.swapFrom != "" && id == s.swapFrom {
		return s.Store.AppGet(ctx, s.swapTo)
	}
	return s.Store.AppGet(ctx, id)
}

// fakeRelay records the channels the dispatcher forwarded instead of serving
// them, and the channels it opened to a controller itself. Package relay is
// task 17 and is not touched here.
type fakeRelay struct {
	ch    chan relayed
	calls chan pushed
}

// relayed is one forwarded channel.
type relayed struct {
	Open wire.Open
	Raw  []byte
	Task *task.Task
	Node string
}

// pushed is one channel the API opened to a controller itself.
type pushed struct {
	Node                 string
	App, Service, Action string
	Params               []byte
	In                   []byte
}

// CallController records the call and answers as a controller that took the
// chain. The node comes off the tunnel, which is all a case can tell the two
// apart by.
func (r *fakeRelay) CallController(ctx context.Context, tun *controllers.Tunnel,
	app, service, action string, params json.RawMessage, in []byte) ([]byte, wire.Code) {
	r.calls <- pushed{Node: tun.Node, App: app, Service: service, Action: action,
		Params: bytes.Clone(params), In: bytes.Clone(in)}
	return []byte(`{"seq":0}`), ""
}

// called waits for one call to a controller.
func (r *fakeRelay) called(t *testing.T) pushed {
	t.Helper()
	select {
	case v := <-r.calls:
		return v
	case <-time.After(readWait):
		t.Fatal("no controller was called")
		return pushed{}
	}
}

// maybeCalled reports whether a call to a controller is waiting. It is the
// counter-control of called and must not wait: a case that holds "nothing was
// pushed" would otherwise be slow by exactly readWait. The push runs in a
// goroutine of the handler's, so this is read only after the result of the
// channel that started it has arrived -- which is what the cases do.
func (r *fakeRelay) maybeCalled() (pushed, bool) {
	select {
	case v := <-r.calls:
		return v, true
	default:
		return pushed{}, false
	}
}

func (r *fakeRelay) Forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open,
	raw []byte, t *task.Task, node string) {
	// A copy, because the real relay has to make one too: the bytes came out
	// of the link's read buffer, and a case that kept the slice would measure
	// whatever the next frame wrote over it.
	r.ch <- relayed{Open: o, Raw: bytes.Clone(raw), Task: t, Node: node}
	// The channel is ended here so a case sees it close; the real relay ends
	// it when the controller does.
	_ = s.Close(ctx, wire.CodeUnreachable, c.Num)
}

// forwarded waits for one forwarded channel.
func (r *fakeRelay) forwarded(t *testing.T) relayed {
	t.Helper()
	select {
	case v := <-r.ch:
		return v
	case <-time.After(readWait):
		t.Fatal("no channel was forwarded")
		return relayed{}
	}
}

// harness is one API with a login, a socket, the dispatcher under test and a
// stand-in for the relay.
type harness struct {
	D     *Dispatcher
	S     *state.State
	Lying *lyingStore
	Relay *fakeRelay
	Log   *audit.Log
	// Sessions is the registry the socket server and the dispatcher share, so
	// a case can see the hint of a trust push on its own session.
	Sessions *browser.Registry

	HTTP  *httptest.Server
	Login *login.Handler

	Installation string
	Admin        trusttest.Signer
	AdminName    string
	AdminID      string
	List         *trust.List

	cert []byte // the client certificate of this session, as the login signed it
	peer *peer
	num  uint32
}

// newFixture builds the API, logs one admin in and opens the session link.
func newFixture(t *testing.T) *harness {
	t.Helper()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	// The keys of "api-1" in trusttest.Genesis and not keys of their own: the
	// API certificate this node signs has to verify against the node of that
	// name (session.VerifyAPI).
	noiseKey, signKey := trusttest.NodeKeys(0xA1)
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.ngc.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noiseKey.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey:    config.NewSecret(make([]byte, 32)),
		ChallengeKey: config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		ReleaseKey:   enc.Bytes(make([]byte, 32)), StoreKey: enc.Bytes(make([]byte, 32)),
		StoreURL: "https://store.ngc.test", SessionLifetime: 43200, IdleTimeout: 1800,
	}
	lying := &lyingStore{Store: memstore.New()}
	s := state.New(cfg, lying, box, make([]byte, 32))
	s.Now = func() int64 { return testNow }
	// trusttest.Genesis gives Users[0] a passkey, so the admin that logs in
	// here is the password admin of Users[1].
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	signed := admin.Sign(t, l)
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(l.Installation), Updated: testNow}); err != nil {
		t.Fatalf("installation: %v", err)
	}
	if _, err := s.AddVersion(ctx, signed); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	for _, u := range l.Users {
		if err := s.DB.UserPut(ctx, dbapi.User{ID: u.ID, Name: u.Name, Roles: u.Roles}); err != nil {
			t.Fatalf("user %s: %v", u.ID, err)
		}
	}
	lg := &audit.Log{DB: s.DB, Now: s.Now}
	relay := &fakeRelay{ch: make(chan relayed, 4), calls: make(chan pushed, 4)}
	// One registry for the dispatcher and for the socket server: a hint of the
	// dispatcher only reaches a session of a case if both know the same one.
	sessions := browser.NewRegistry()
	h := &harness{
		S: s, Lying: lying, Relay: relay, Log: lg, Login: login.New(s), Sessions: sessions,
		Installation: l.Installation, Admin: admin, AdminName: l.Users[1].Name,
		AdminID: l.Users[1].ID, List: l,
	}
	h.D = New(Deps{S: s, Log: lg, Login: h.Login, Tunnels: controllers.NewRegistry(), Relay: relay,
		Sessions: sessions})
	srv := &browser.Server{S: s, Login: h.Login, Open: h.D, Sessions: sessions}
	h.HTTP = httptest.NewServer(http.HandlerFunc(srv.Socket))
	t.Cleanup(h.HTTP.Close)
	h.peer = dialSession(t, h)
	return h
}

// As opens a second session on the same API for another user of the trust
// list -- a user who is no admin, which is what the admin wall is measured
// with. The new harness keeps its own channel counter and its own client
// certificate, because both belong to a link and not to the API; everything
// else is the API this fixture built. The fields keep their names: the one
// signer of a harness is the user of its session, and for the fixture's own
// session that user is the admin.
func (h *harness) As(t *testing.T, signer trusttest.Signer, id, name string) *harness {
	t.Helper()
	other := *h
	other.Admin, other.AdminID, other.AdminName = signer, id, name
	other.num = 0
	other.peer = dialSession(t, &other)
	return &other
}

// Grant gives the admin's role a set of rights, so a case can open an app
// channel. It replaces whatever was granted before.
func (h *harness) Grant(t *testing.T, right ...string) {
	t.Helper()
	if err := rights.Set(ctx, h.S.DB, core.RoleRights{Role: trust.RoleAdmin, Rights: right}); err != nil {
		t.Fatalf("rights: %v", err)
	}
}

// PutApp writes an installed app row with the manifest of its own ID.
func (h *harness) PutApp(t *testing.T, id string, active bool) {
	t.Helper()
	if err := h.S.DB.AppPut(ctx, dbapi.App{ID: id, Version: "1.0.0", Store: trust.StoreGlobal,
		Node: ctlNode, Active: active, Manifest: []byte(manifestOf(id)), Updated: testNow}); err != nil {
		t.Fatalf("app %s: %v", id, err)
	}
}

// Task signs a task of this session's admin. The session certificate carries
// the admin's own key, so here the admin's key is the session key.
func (h *harness) Task(t *testing.T, tk task.Task) *sign.Envelope {
	t.Helper()
	tk.Installation = h.Installation
	tk.Cert = session.CertHash(h.cert)
	tk.Issued, tk.Expires = issued, issued+task.MaxLifetime
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(h.Admin.Priv, sign.PurposeTask, b)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// Open sends an OPEN for a fresh channel number and returns its handle.
func (h *harness) Open(t *testing.T, o wire.Open) *chann {
	t.Helper()
	if o.Window == 0 {
		o.Window = wire.MinWindow
	}
	h.num++
	c := &chann{h: h, num: h.num, credit: uint64(o.Window)}
	h.peer.writeOpen(t, c.num, o)
	return c
}

// OpenRaw sends an OPEN whose payload is exactly the bytes handed in, without
// going through json.Marshal. It is what lets a case put a byte string into
// the frame that the encoder would never produce.
func (h *harness) OpenRaw(t *testing.T, payload []byte, window uint32) *chann {
	t.Helper()
	h.num++
	c := &chann{h: h, num: h.num, credit: uint64(window)}
	h.peer.write(t, wire.Frame{Type: wire.TypeOpen, Channel: c.num, Payload: payload})
	return c
}

// OpenAdmin is Open: the one user of this fixture is an admin, so every
// channel of a case is already an admin's. It keeps its own name so a case
// says which it means.
func (h *harness) OpenAdmin(t *testing.T, o wire.Open) *chann { return h.Open(t, o) }

// chann is the browser's end of one channel.
type chann struct {
	h      *harness
	num    uint32
	credit uint64 // what the API granted us and we have not spent
	queue  []wire.Frame
}

// pull returns the next frame of the channel, counting every GRANT into the
// credit this side may spend.
func (c *chann) pull(t *testing.T) wire.Frame {
	t.Helper()
	if len(c.queue) > 0 {
		f := c.queue[0]
		c.queue = c.queue[1:]
		return f
	}
	f := c.h.peer.read(t)
	if f.Type == wire.TypeGrant {
		credit, _, err := wire.ParseGrant(f.Payload)
		if err != nil {
			t.Fatalf("grant: %v", err)
		}
		c.credit += uint64(credit)
	}
	return f
}

// read is pull: a case reads the frames in the order they came.
func (c *chann) read(t *testing.T) wire.Frame { t.Helper(); return c.pull(t) }

// collect reads the DATA of the channel up to its EOF.
func (c *chann) collect(t *testing.T) []byte {
	t.Helper()
	var out []byte
	for {
		f := c.pull(t)
		switch f.Type {
		case wire.TypeData:
			out = append(out, f.Payload...)
		case wire.TypeEOF:
			return out
		case wire.TypeClose:
			c.queue = append(c.queue, f)
			return out
		}
	}
}

// closed reads up to the CLOSE of the channel and returns it.
func (c *chann) closed(t *testing.T) wire.Close {
	t.Helper()
	for {
		f := c.pull(t)
		if f.Type != wire.TypeClose {
			continue
		}
		cl, err := wire.ParseClose(f.Payload)
		if err != nil {
			t.Fatalf("close payload: %v", err)
		}
		return cl
	}
}

// sendBytes sends n bytes of DATA and then EOF, waiting for the API's GRANT
// whenever the credit runs out. A CLOSE that arrives in between ends the
// sending: the API has refused the channel, and writing on would be a
// protocol error of ours.
func (c *chann) sendBytes(t *testing.T, n int) {
	t.Helper()
	left := n
	for left > 0 {
		chunk := min(left, wire.MaxPayload)
		for c.credit < uint64(chunk) {
			if f := c.pull(t); f.Type == wire.TypeClose {
				c.queue = append(c.queue, f)
				return
			}
		}
		c.credit -= uint64(chunk)
		c.h.peer.write(t, wire.Frame{Type: wire.TypeData, Channel: c.num, Payload: make([]byte, chunk)})
		left -= chunk
	}
	c.h.peer.write(t, wire.Frame{Type: wire.TypeEOF, Channel: c.num})
}

// eof sends EOF alone, which is what the opener of a call-style channel does.
func (c *chann) eof(t *testing.T) {
	t.Helper()
	c.h.peer.write(t, wire.Frame{Type: wire.TypeEOF, Channel: c.num})
}

// result reads the result of a call-style channel; a channel that closed with
// an error fails the case.
func (c *chann) result(t *testing.T) []byte {
	t.Helper()
	body := c.collect(t)
	if cl := c.closed(t); cl.Status != wire.StatusOK {
		t.Fatalf("close %+v", cl)
	}
	return body
}

// peer is the browser's end of one session link.
type peer struct {
	ws *websocket.Conn
	tr *noisep.Transport
}

// dialSession logs in, opens the socket and runs the Noise handshake as
// initiator with the session's own keys.
func dialSession(t *testing.T, h *harness) *peer {
	t.Helper()
	origins := h.List.RP.Origins
	if len(origins) == 0 {
		t.Fatal("the fixture's trust list carries no origin")
	}
	cookie, apiDH := loginOnce(t, h)
	client, err := noisep.Keypair(zeroReader{})
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	hdr := http.Header{}
	hdr.Set("Cookie", session.CookieName+"="+cookie)
	hdr.Set("Origin", origins[0])
	ws, _, err := websocket.Dial(ctx, "ws"+h.HTTP.URL[len("http"):]+session.PathSocket,
		&websocket.DialOptions{Subprotocols: []string{session.Subprotocol}, HTTPClient: testClient, HTTPHeader: hdr})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ws.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	hs, err := noisep.Handshake(true, []byte(noisep.PrologueSession), client, apiDH, zeroReader{})
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
	p := &peer{ws: ws, tr: noisep.NewTransport(true, cs1, cs2)}
	t.Cleanup(func() { _ = p.ws.CloseNow() })
	return p
}

// loginOnce runs the whole login and hands back the cookie value and the API's
// public session key out of session.LoginResult.
func loginOnce(t *testing.T, h *harness) (string, []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	h.Login.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+h.AdminName+`"}`))
	if w.Code != 200 {
		t.Fatalf("login/start: %d %s", w.Code, w.Body)
	}
	var c session.LoginChallenge
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	dh, err := noisep.Keypair(zeroReader{})
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	payload, err := json.Marshal(session.ClientCert{
		Installation: h.Installation, User: c.User, Nonce: c.Nonce,
		Sig: enc.Bytes(h.Admin.Priv.Public().(ed25519.PublicKey)), DH: enc.Bytes(dh.Public),
		Issued: c.Now, Expires: c.Now + c.MaxLifetime,
	})
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	h.cert = payload
	body, err := json.Marshal(session.LoginFinish{
		Cert:  enc.Bytes(payload),
		Proof: h.Admin.Prove(sign.PurposeClientCert, payload),
	})
	if err != nil {
		t.Fatalf("finish body: %v", err)
	}
	w = httptest.NewRecorder()
	h.Login.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 200 {
		t.Fatalf("login/finish: %d %s", w.Code, w.Body)
	}
	var res session.LoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("result: %v", err)
	}
	var cert session.APICert
	if err := json.Unmarshal(res.API.Payload, &cert); err != nil {
		t.Fatalf("api cert: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %+v", cookies)
	}
	return cookies[0].Value, cert.DH
}

func jsonPost(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// write seals one frame and sends it.
func (p *peer) write(t *testing.T, f wire.Frame) {
	t.Helper()
	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("encode %v: %v", f.Type, err)
	}
	sealed, err := p.tr.Seal(raw)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	wctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	if err := p.ws.Write(wctx, websocket.MessageBinary, sealed); err != nil {
		t.Fatalf("write %v: %v", f.Type, err)
	}
}

// writeOpen sends an OPEN for a channel number.
func (p *peer) writeOpen(t *testing.T, num uint32, o wire.Open) {
	t.Helper()
	body, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	p.write(t, wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: body})
}

// read returns the next frame; a failure fails the case.
func (p *peer) read(t *testing.T) wire.Frame {
	t.Helper()
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	kind, msg, err := p.ws.Read(rctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if kind != websocket.MessageBinary {
		t.Fatal("not binary")
	}
	plain, err := p.tr.Open(msg)
	if err != nil {
		t.Fatalf("open frame: %v", err)
	}
	f, err := wire.Decode(plain)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return f
}

// waitEntry waits for the channel entry of an app and action to appear. The
// entry is written after the channel closed, so a case that read the log the
// moment its CLOSE arrived would be measuring a race and not the log.
func (h *harness) waitEntry(t *testing.T, app, action string) core.LogEntry {
	t.Helper()
	deadline := time.Now().Add(readWait)
	for time.Now().Before(deadline) {
		if e, ok := channelEntry(h.logEntries(t), app, action); ok {
			return e
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no channel entry for %s/%s: %+v", app, action, h.logEntries(t))
	return core.LogEntry{}
}

// logEntries returns the log of the fixture, newest first.
func (h *harness) logEntries(t *testing.T) []core.LogEntry {
	t.Helper()
	out, err := h.Log.Query(ctx, core.LogQuery{Limit: 500})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	return out
}
