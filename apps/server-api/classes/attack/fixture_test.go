package attack

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/server"
	"github.com/sorglos-it/nextgencloud/shared/config"
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

// The clock of every case: trusttest.Genesis is issued at issued, and a
// channel opens ten seconds later, inside the lifetime of certificate and
// task.
const (
	issued  = int64(1790000000)
	testNow = issued + 10
)

// ctlNode is the controller of trusttest.Genesis, the one node where an admin
// may run a root task. No tunnel to it is ever opened in this package, which
// is what makes E_UNREACHABLE the wall a channel that got through ends at.
const ctlNode = "ctl-1"

// readWait bounds every read of a case. It is here so that a case which would
// otherwise wait for ever fails instead; it measures nothing.
const readWait = 20 * time.Second

// zeroReader gives the fixed X25519 key pair of the browser's session
// certificate, so a case knows the private half a real browser keeps.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 7
	}
	return len(p), nil
}

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

// api is one running API with its router in front of it.
type api struct {
	Srv  *server.Server
	DB   dbapi.Store
	HTTP *httptest.Server
	// Code is the one-time code of the first setup, read off the disk the
	// moment Build wrote it. The handler removes the file once a version 1
	// got through, so a case that wanted the code afterwards could not read
	// it any more; this field is what the cases before that point use.
	Code string

	Installation string
	Admin        trusttest.Signer
	AdminName    string
	AdminID      string
	List         *trust.List

	cert []byte // the client certificate of this session, as the login signed it
	peer *peer
	num  uint32
}

// newConfig names the two files apitls.Ensure will write, in this case's own
// directory. The release key, the store key and the store URL are the ones
// trusttest.Genesis carries, which matters twice: the offer of the first setup
// is built out of them (session.ParseSetupOffer wants a 32-byte release key
// and a valid global store, so a configuration without them answers an offer
// no shell takes), and a case that signs version 1 out of that offer then
// signs the same store the rest of the package works with.
func newConfig(t *testing.T) *config.API {
	t.Helper()
	dir := t.TempDir()
	noiseKey, signKey := trusttest.NodeKeys(0xA1)
	_, releaseSign := trusttest.NodeKeys(trusttest.ReleaseFill)
	_, storeSign := trusttest.NodeKeys(0xB1)
	return &config.API{
		Node: "api-1", URL: "wss://api-1.test:7443/tunnel", Listen: "127.0.0.1:0",
		Cert: filepath.Join(dir, "api.crt"), CertKey: filepath.Join(dir, "api.key"),
		NoiseKey:     config.NewSecret(noiseKey.Private),
		SignKey:      config.NewSecret(signKey.Seed()),
		SecretKey:    config.NewSecret(bytes.Repeat([]byte{3}, 32)),
		ChallengeKey: config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		ReleaseKey:   enc.Bytes(releaseSign.Public().(ed25519.PublicKey)),
		StoreKey:     enc.Bytes(storeSign.Public().(ed25519.PublicKey)),
		StoreURL:     "https://store.ngc.example",
		// A lifetime long enough that the certificate of a case, issued at
		// testNow, is inside it.
		SessionLifetime: 43200, IdleTimeout: 1800,
	}
}

// build makes a server over an empty database and puts its router behind a
// listener. The setup is still open at this point: nothing is in the trust
// list yet.
//
// SetupCodeFile and SpoolDir are package variables of server, so the cases of
// this package must not run in parallel; none of them calls t.Parallel.
func build(t *testing.T) *api {
	t.Helper()
	db := memstore.New()
	dbopen.Register(func(context.Context, config.DB) (dbapi.Store, error) { return db, nil })
	t.Cleanup(func() { dbopen.Register(nil) })
	dir := t.TempDir()
	oldCode, oldSpool := server.SetupCodeFile, server.SpoolDir
	server.SetupCodeFile = filepath.Join(dir, "setup-code")
	server.SpoolDir = filepath.Join(dir, "spool")
	t.Cleanup(func() { server.SetupCodeFile, server.SpoolDir = oldCode, oldSpool })
	srv, err := server.Build(ctx, newConfig(t), "9.9.9")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The clock of the fixture, set the moment the server exists: everything a
	// case signs is dated by it.
	srv.State.Now = func() int64 { return testNow }
	raw, err := os.ReadFile(server.SetupCodeFile)
	if err != nil {
		t.Fatalf("the setup code was not written: %v", err)
	}
	code := strings.TrimSpace(string(raw))
	if !session.ValidSetupCode(code) {
		t.Fatalf("the setup code %q is none", code)
	}
	a := &api{Srv: srv, DB: db, Code: code}
	a.HTTP = httptest.NewServer(srv.Handler())
	t.Cleanup(a.HTTP.Close)
	return a
}

// newAPI is build plus the first admin: version 1 of the trust list, the user
// rows and the session of one admin. That is the state every attack but the
// one against the first setup works against.
func newAPI(t *testing.T) *api {
	t.Helper()
	a := build(t)
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	signed := admin.Sign(t, l)
	if err := a.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(l.Installation), Updated: testNow}); err != nil {
		t.Fatalf("installation: %v", err)
	}
	if _, err := a.Srv.State.AddVersion(ctx, signed); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	for _, u := range l.Users {
		if err := a.DB.UserPut(ctx, dbapi.User{ID: u.ID, Name: u.Name, Roles: u.Roles}); err != nil {
			t.Fatalf("user %s: %v", u.ID, err)
		}
	}
	// trusttest.Genesis gives Users[0] a passkey, so the admin who logs in
	// with a key of his own is Users[1].
	a.Installation, a.Admin = l.Installation, admin
	a.AdminName, a.AdminID, a.List = l.Users[1].Name, l.Users[1].ID, l
	a.peer = a.dial(t)
	return a
}

// Task signs a task of this session's admin. The session certificate carries
// the admin's own key, so the signing key is the session key.
func (a *api) Task(t *testing.T, tk task.Task) *sign.Envelope {
	t.Helper()
	return a.taskSignedBy(t, tk, a.Admin)
}

// taskSignedBy is Task with the signer picked by the case, which is what an
// attack with a foreign key needs.
func (a *api) taskSignedBy(t *testing.T, tk task.Task, signer trusttest.Signer) *sign.Envelope {
	t.Helper()
	tk.Installation = a.Installation
	tk.Cert = session.CertHash(a.cert)
	tk.Issued, tk.Expires = issued, issued+task.MaxLifetime
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	env, err := sign.Sign(signer.Priv, sign.PurposeTask, b)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return &env
}

// Open sends an OPEN for a fresh channel number and returns its handle.
func (a *api) Open(t *testing.T, o wire.Open) *chann {
	t.Helper()
	if o.Window == 0 {
		o.Window = wire.MinWindow
	}
	a.num++
	// Credit ZERO, not o.Window: o.Window is what this side grants the API,
	// and what this side may send is nothing until the API's first GRANT
	// (shared/wire, "Kanäle"). Starting at o.Window let a case send a window
	// nobody had granted it.
	c := &chann{a: a, num: a.num}
	a.peer.writeOpen(t, c.num, o)
	return c
}

// chann is the browser's end of one channel.
type chann struct {
	a      *api
	num    uint32
	credit uint64 // what the API granted and this side has not spent
	queue  []wire.Frame
}

// pull returns the next frame OF THIS CHANNEL, counting every GRANT into the
// credit this side may spend.
//
// The filter on the channel number is not tidiness, it is the measurement:
// several channels of a case run over one link, and a channel the API refused
// in the middle of an upload can leave a frame behind. Without the filter the
// next channel read it and answered the previous channel's code -- measured
// while this file was written, three upload cases drifted by one, and the
// numbers looked like findings about the package. A frame of another channel
// is dropped here, because every channel of this package is read to its end
// before the next one opens.
func (c *chann) pull(t *testing.T) wire.Frame {
	t.Helper()
	for {
		if len(c.queue) > 0 {
			f := c.queue[0]
			c.queue = c.queue[1:]
			return f
		}
		f := c.a.peer.read(t)
		if f.Channel != c.num {
			continue
		}
		if f.Type == wire.TypeGrant {
			credit, _, err := wire.ParseGrant(f.Payload)
			if err != nil {
				t.Fatalf("grant: %v", err)
			}
			c.credit += uint64(credit)
		}
		return f
	}
}

// collect reads the DATA of the channel up to its EOF or its CLOSE.
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

// code reads up to the CLOSE and answers the code it carries, "" for a channel
// that ended in order.
func (c *chann) code(t *testing.T) wire.Code {
	t.Helper()
	cl := c.closed(t)
	if cl.Status == wire.StatusOK {
		return ""
	}
	return cl.Code
}

// eof sends EOF alone, which is what the opener of a call-style channel does.
func (c *chann) eof(t *testing.T) {
	t.Helper()
	c.a.peer.write(t, wire.Frame{Type: wire.TypeEOF, Channel: c.num})
}

// result reads the result of a call-style channel; a channel that closed with
// an error fails the case.
func (c *chann) result(t *testing.T) []byte {
	t.Helper()
	body := c.collect(t)
	if cl := c.closed(t); cl.Status != wire.StatusOK {
		t.Fatalf("channel %d closed %+v", c.num, cl)
	}
	return body
}

// call opens a call-style channel, sends its EOF and answers the result.
func (a *api) call(t *testing.T, o wire.Open) []byte {
	t.Helper()
	c := a.Open(t, o)
	c.eof(t)
	return c.result(t)
}

// callCode is call for a channel that is expected to be refused: it answers
// the code the channel closed with.
func (a *api) callCode(t *testing.T, o wire.Open) wire.Code {
	t.Helper()
	c := a.Open(t, o)
	c.eof(t)
	return c.code(t)
}

// peer is the browser's end of one session link.
type peer struct {
	ws *websocket.Conn
	tr *noisep.Transport
}

// dial logs in over the router, opens the socket and runs the Noise handshake
// as the shell does -- with the Origin of the trust list, so every case goes
// through the allow-list of the socket and not past it.
func (a *api) dial(t *testing.T) *peer {
	t.Helper()
	cookie, apiDH := a.loginOnce(t)
	client, err := noisep.Keypair(zeroReader{})
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	origins := a.List.RP.Origins
	if len(origins) == 0 {
		t.Fatal("the fixture's trust list carries no origin")
	}
	hdr := http.Header{}
	hdr.Set("Cookie", session.CookieName+"="+cookie)
	hdr.Set("Origin", origins[0])
	ws, _, err := websocket.Dial(ctx, "ws"+a.HTTP.URL[len("http"):]+session.PathSocket,
		&websocket.DialOptions{Subprotocols: []string{session.Subprotocol},
			HTTPClient: testClient, HTTPHeader: hdr})
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

// loginOnce runs the whole login over the router and hands back the cookie and
// the API's public session key out of session.LoginResult.
func (a *api) loginOnce(t *testing.T) (string, []byte) {
	t.Helper()
	res := a.post(t, session.PathLoginStart, `{"name":"`+a.AdminName+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("login/start: %d %s", res.Code, res.Body)
	}
	var c session.LoginChallenge
	if err := json.Unmarshal(res.Body.Bytes(), &c); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	a.cert = a.certBytes(t, c)
	body, err := json.Marshal(session.LoginFinish{
		Cert:  enc.Bytes(a.cert),
		Proof: a.Admin.Prove(sign.PurposeClientCert, a.cert),
	})
	if err != nil {
		t.Fatalf("finish body: %v", err)
	}
	res = a.post(t, session.PathLoginFinish, string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("login/finish: %d %s", res.Code, res.Body)
	}
	var out session.LoginResult
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("result: %v", err)
	}
	var cert session.APICert
	if err := json.Unmarshal(out.API.Payload, &cert); err != nil {
		t.Fatalf("api cert: %v", err)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %+v", cookies)
	}
	return cookies[0].Value, cert.DH
}

// certBytes builds the browser's session certificate with the fixed X25519
// key pair of zeroReader.
func (a *api) certBytes(t *testing.T, c session.LoginChallenge) []byte {
	t.Helper()
	dh, err := noisep.Keypair(zeroReader{})
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	b, err := json.Marshal(session.ClientCert{
		Installation: a.Installation, User: c.User, Nonce: c.Nonce,
		Sig: enc.Bytes(a.Admin.Priv.Public().(ed25519.PublicKey)), DH: enc.Bytes(dh.Public),
		Issued: c.Now, Expires: c.Now + c.MaxLifetime,
	})
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	return b
}

// post sends one POST through the router of the server under attack, which is
// the point: a case that called a handler directly would walk past the path
// guard the router puts in front of it.
func (a *api) post(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.Srv.Handler().ServeHTTP(rec, r)
	return rec
}

// get sends one GET through the router.
func (a *api) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// codeOf reads the wire code out of an answer of httpapi.Fail.
func codeOf(t *testing.T, rec *httptest.ResponseRecorder) wire.Code {
	t.Helper()
	var e struct {
		Code wire.Code `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("answer %q is not an error of the contract: %v", rec.Body.String(), err)
	}
	return e.Code
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
	f, err := p.readErr()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return f
}

// errTimeout says that the case's own deadline ran out, which is not the link
// ending. Telling the two apart is what makes a case about "the link ends"
// able to fall: a read that simply waits also answers an error.
var errTimeout = errText("the read ran into the case's own deadline")

type errText string

func (e errText) Error() string { return string(e) }

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

// wantLinkEnded reads until the link ends and fails the case when it does not:
// a read that only ran out of time says nothing about the link.
func wantLinkEnded(t *testing.T, p *peer) {
	t.Helper()
	for {
		_, err := p.readErr()
		if err == nil {
			continue
		}
		if err == errTimeout {
			t.Fatal("the link was still standing when the case's deadline ran out")
		}
		return
	}
}

// send writes body as DATA frames and then EOF, waiting for a GRANT whenever
// the credit runs out. A CLOSE that arrives in between ends the sending: the
// API has refused the channel, and writing on would be a protocol error of
// ours.
func (c *chann) send(t *testing.T, body []byte) {
	t.Helper()
	for len(body) > 0 {
		n := min(len(body), wire.MaxPayload)
		for c.credit < uint64(n) {
			if f := c.pull(t); f.Type == wire.TypeClose {
				c.queue = append(c.queue, f)
				return
			}
		}
		c.credit -= uint64(n)
		c.a.peer.write(t, wire.Frame{Type: wire.TypeData, Channel: c.num, Payload: body[:n]})
		body = body[n:]
	}
	c.a.peer.write(t, wire.Frame{Type: wire.TypeEOF, Channel: c.num})
}
