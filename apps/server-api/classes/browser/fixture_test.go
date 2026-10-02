package browser

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

const testNow = int64(1790000000)

// readWait bounds every read of a case. It is generous on purpose: it is here so
// that a case which would otherwise wait for ever fails instead, not to measure
// anything.
const readWait = 10 * time.Second

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
// TIME_WAIT; a run of this package opens one link per case plus one per
// channel-limit case.
var testClient = &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		return c, err
	},
}}

// opened is what the Opener was handed for one channel.
type opened struct {
	s *Session
	c *link.Channel
	o wire.Open
}

// harness is one API with a login, a socket and a dispatcher that records what
// it gets.
type harness struct {
	Srv   *Server
	HTTP  *httptest.Server
	Login *login.Handler
	Fx    fixture
	// Opened collects every channel the dispatcher was handed.
	Opened chan opened
	// Grant is what the dispatcher does with a new channel. The default grants
	// the window of the OPEN, which is what a core app does; a case that wants
	// to grant by hand sets it to nothing.
	Grant func(ctx context.Context, s *Session, c *link.Channel, o wire.Open)
}

// OpenChannel is the dispatcher of the harness: package coreapp's part, cut
// down to a grant and a note.
func (h *harness) OpenChannel(ctx context.Context, s *Session, c *link.Channel, o wire.Open) {
	if h.Grant != nil {
		h.Grant(ctx, s, c, o)
	}
	h.Opened <- opened{s: s, c: c, o: o}
}

// fixture holds what a case needs to speak for the browser.
type fixture struct {
	Installation string
	Password     trusttest.Signer
	PasswordName string
	List         *trust.List
}

// newServer builds an API with one admin in its trust list and a socket in
// front of it. It is the login fixture of package login (task 7) once more,
// because test helpers are not visible across packages.
func newServer(t *testing.T) *harness {
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
	s := state.New(cfg, memstore.New(), box, make([]byte, 32))
	s.Now = func() int64 { return testNow }
	// trusttest.Genesis gives Users[0] a passkey, so the password admin has to
	// be the signer handed in.
	password := trusttest.NewSigner(1)
	l := trusttest.Genesis(password)
	signed := password.Sign(t, l)
	// Version 1 is rooted in the installation ID, which the setup writes before
	// version 1 is signed (state.AddVersion checks it).
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
	h := &harness{
		Login:  login.New(s),
		Fx:     fixture{Installation: l.Installation, Password: password, PasswordName: l.Users[1].Name, List: l},
		Opened: make(chan opened, 4*wire.MaxSessionChannels),
	}
	h.Grant = func(ctx context.Context, s *Session, c *link.Channel, o wire.Open) {
		_ = s.SendGrant(ctx, c, o.Window)
	}
	h.Srv = &Server{S: s, Login: h.Login, Open: h, Sessions: NewRegistry()}
	h.HTTP = httptest.NewServer(http.HandlerFunc(h.Srv.Socket))
	t.Cleanup(h.HTTP.Close)
	return h
}

// Socket answers one request to the socket, as the router would.
func (h *harness) Socket(w http.ResponseWriter, r *http.Request) { h.Srv.Socket(w, r) }

// socketRequest builds a GET of the socket path with the cookie value given; an
// empty value carries no cookie at all.
func socketRequest(cookie string) (*httptest.ResponseRecorder, *http.Request) {
	r := httptest.NewRequest(http.MethodGet, session.PathSocket, nil)
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	return httptest.NewRecorder(), r
}

// loginOnce runs the whole login and hands back the cookie value and the API's
// public session key out of session.LoginResult.
func loginOnce(t *testing.T, h *harness) (string, []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	h.Login.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+h.Fx.PasswordName+`"}`))
	if w.Code != 200 {
		t.Fatalf("login/start: %d %s", w.Code, w.Body)
	}
	var c session.LoginChallenge
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	payload := certBytes(t, h.Fx, c)
	body, err := json.Marshal(session.LoginFinish{
		Cert:  enc.Bytes(payload),
		Proof: h.Fx.Password.Prove(sign.PurposeClientCert, payload),
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

// certBytes builds the browser's session certificate with the fixed X25519 key
// pair of zeroReader.
func certBytes(t *testing.T, fx fixture, c session.LoginChallenge) []byte {
	t.Helper()
	dh, err := noisep.Keypair(zeroReader{})
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	cert := session.ClientCert{
		Installation: fx.Installation, User: c.User, Nonce: c.Nonce,
		Sig: enc.Bytes(fx.Password.Priv.Public().(ed25519.PublicKey)), DH: enc.Bytes(dh.Public),
		Issued: c.Now, Expires: c.Now + c.MaxLifetime,
	}
	b, err := json.Marshal(cert)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func jsonPost(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// peer is the browser's end of one session link.
type peer struct {
	ws *websocket.Conn
	tr *noisep.Transport
}

// dialSession logs in, opens the socket and runs the Noise handshake as
// initiator with the session's own keys. It carries the Origin of the trust
// list, which is what a shell carries, so every case of this package goes
// through the allow-list of the socket and not past it.
func dialSession(t *testing.T, h *harness) *peer {
	t.Helper()
	origins := h.Fx.List.RP.Origins
	if len(origins) == 0 {
		t.Fatal("the fixture's trust list carries no origin")
	}
	p, err := dialOrigin(t, h, origins[0])
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return p
}

// dialOrigin is dialSession with the Origin header picked by the case and the
// error handed back, so a case can hold that an origin is refused.
func dialOrigin(t *testing.T, h *harness, origin string) (*peer, error) {
	t.Helper()
	cookie, apiDH := loginOnce(t, h)
	client, err := noisep.Keypair(zeroReader{})
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	hdr := http.Header{}
	hdr.Set("Cookie", session.CookieName+"="+cookie)
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	ws, _, err := websocket.Dial(ctx, "ws"+h.HTTP.URL[len("http"):]+session.PathSocket,
		&websocket.DialOptions{
			Subprotocols: []string{session.Subprotocol},
			HTTPClient:   testClient,
			HTTPHeader:   hdr,
		})
	if err != nil {
		return nil, err
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
	t.Cleanup(p.close)
	return p, nil
}

func (p *peer) close() { _ = p.ws.CloseNow() }

// write seals one frame and sends it.
func (p *peer) write(t *testing.T, f wire.Frame) {
	t.Helper()
	if err := p.writeErr(f); err != nil {
		t.Fatalf("write %+v: %v", f.Type, err)
	}
}

// writeErr is write without the case failing, for the frames that are meant to
// end the link: once it has ended, a write of ours fails too.
func (p *peer) writeErr(f wire.Frame) error {
	raw, err := f.Encode()
	if err != nil {
		return err
	}
	sealed, err := p.tr.Seal(raw)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	return p.ws.Write(wctx, websocket.MessageBinary, sealed)
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

// writeControl sends a control message on channel 0.
func (p *peer) writeControl(t *testing.T, kind string, body []byte) {
	t.Helper()
	payload, err := json.Marshal(wire.Control{T: kind, B: body})
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	p.write(t, wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: payload})
}

// read returns the next frame; a failure fails the case.
func (p *peer) read(t *testing.T) wire.Frame {
	t.Helper()
	f, err := p.readErr()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return f
}

// errTimeout says that the case's own deadline ran out, which is not the link
// ending. Telling the two apart is what makes a case about "the link ends"
// able to fall: a read that simply waits also answers an error, and three
// cases of this package passed on that error while the link was still standing
// -- measured on three mutants that all stayed alive.
var errTimeout = errText("the read ran into the case's own deadline")

// readErr returns the next frame, errTimeout when nothing came, or the error
// that ended the link.
func (p *peer) readErr() (wire.Frame, error) {
	rctx, cancel := context.WithTimeout(ctx, readWait)
	defer cancel()
	kind, msg, err := p.ws.Read(rctx)
	if err != nil {
		if rctx.Err() != nil {
			return wire.Frame{}, errTimeout
		}
		return wire.Frame{}, err
	}
	if kind != websocket.MessageBinary {
		return wire.Frame{}, errText("not binary")
	}
	plain, err := p.tr.Open(msg)
	if err != nil {
		return wire.Frame{}, err
	}
	return wire.Decode(plain)
}

// readAll reads until the link ends and returns the last frame with the error
// that ended it. A link that never ends runs into readWait, which is a failed
// case and not a hung run.
func (p *peer) readAll() (wire.Frame, error) {
	var last wire.Frame
	for {
		f, err := p.readErr()
		if err != nil {
			return last, err
		}
		last = f
	}
}

// wantEnded reads until the link ends and fails the case when it does not: a
// read that only ran out of time says nothing about the link.
func wantEnded(t *testing.T, p *peer) {
	t.Helper()
	_, err := p.readAll()
	if err == nil {
		t.Fatal("the link did not end")
	}
	if errors.Is(err, errTimeout) {
		t.Fatal("the link is still standing; the case only ran out of time")
	}
}

// errText is a fixed error of a helper.
type errText string

func (e errText) Error() string { return string(e) }

// openOf is the OPEN most cases use: a core app, the smallest window.
func openOf() wire.Open { return openWindow(wire.MinWindow) }

// openWindow is openOf with a window of the case's own choosing, so that the
// window of an OPEN is not the same number in every case of this package: a
// bound written as wire.MinWindow instead of as the OPEN's own window would
// otherwise have nowhere to fall.
func openWindow(window uint32) wire.Open {
	return wire.Open{App: "ngc.desktop", Service: "api", Action: "me", Window: window}
}

// within runs fn in a goroutine of its own and fails the case when it does not
// come back in time. Every call into the Registry goes through it, because they
// all take the one lock: a lock that is not given back would otherwise block the
// case itself, and a deadline one has to reach THROUGH the blocking call is never
// reached. Measured: without this the package run ended in
// "panic: test timed out", which deletes the result of every other case; with it
// the case falls by name and the run goes on.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(readWait):
		t.Fatalf("%s did not come back in %v -- a lock was not given back", what, readWait)
	}
}

// registryCount is Registry.Count behind within.
func registryCount(t *testing.T, r *Registry) int {
	t.Helper()
	var n int
	within(t, "Registry.Count", func() { n = r.Count() })
	return n
}

// firstOpened waits for the dispatcher to be handed a channel.
func firstOpened(t *testing.T, h *harness) opened {
	t.Helper()
	select {
	case o := <-h.Opened:
		return o
	case <-time.After(readWait):
		t.Fatal("the dispatcher was never handed a channel")
		return opened{}
	}
}

// grantNothing is a dispatcher that answers an OPEN with nothing at all, so a
// case can send the GRANT itself.
func grantNothing(t *testing.T, h *harness) {
	t.Helper()
	h.Grant = nil
}
