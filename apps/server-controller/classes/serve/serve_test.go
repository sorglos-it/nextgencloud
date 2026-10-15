package serve

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/chain"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

const (
	// The test API and the controller of trusttest.Genesis: api-1 with the
	// Noise key of NodeKeys(apiFill) and the TLS key of APITLS(apiTLSFill),
	// ctl-1 with NodeKeys(ctlFill). The prologue of every end-to-end channel
	// binds node, so these tests run as that controller and no other.
	apiFill    = 0xA1
	ctlFill    = 0xC1
	apiTLSFill = 0xE1
	apiHost    = "api-1.test"
	node       = "ctl-1"

	// The app the fixture has installed, with the one action of its manifest.
	testApp     = "org.sorglos.terminal"
	testService = "files"
	testAction  = "run"

	issued = 1790000000
	now    = issued + 10

	// answer bounds the wait for one frame of the controller, so a test that
	// waits for one which never comes says so instead of standing until the
	// timeout of go test.
	answer = 30 * time.Second
)

// sessionSeed makes the Ed25519 key of the client certificate.
var sessionSeed = bytes.Repeat([]byte{0x51}, ed25519.SeedSize)

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// world is one controller as these tests run it: the list it was anchored to,
// its local state, the server that answers the channels of its tunnel and the
// log both of them write to.
type world struct {
	admin trusttest.Signer
	list  *trust.List
	v1    trust.Signed
	st    *state.Store
	keep  *chain.Keeper
	srv   *Server
	eng   *appEngine
	notes *notes
	log   *slog.Logger
	deps  Deps
}

// notes is a log handler that counts what was written to it: what a peer may
// bring about must not reach the log of this server, and a count is what says
// whether it did.
type notes struct{ n atomic.Int64 }

func (l *notes) Enabled(context.Context, slog.Level) bool  { return true }
func (l *notes) Handle(context.Context, slog.Record) error { l.n.Add(1); return nil }
func (l *notes) WithAttrs([]slog.Attr) slog.Handler        { return l }
func (l *notes) WithGroup(string) slog.Handler             { return l }

// newWorld builds a controller that has verified version 1 of
// trusttest.Genesis and runs the one app of the fixture.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	// A fixture nobody could have signed would prove nothing.
	if err := l.Validate(); err != nil {
		t.Fatalf("test trust list: %v", err)
	}
	v1 := admin.Sign(t, l)
	st := state.New(memstore.New())
	t.Cleanup(func() { st.Close() })
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.PutApp(ctx, dbapi.App{ID: testApp, Version: "1.0.0", Store: trust.StoreGlobal, Node: node,
		Active: true, Manifest: manifest(), SHA256: bytes.Repeat([]byte{0x01}, 32)}); err != nil {
		t.Fatal(err)
	}
	k := chain.New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	w := &world{admin: admin, list: l, v1: v1, st: st, keep: k, eng: &appEngine{}, notes: &notes{}}
	w.log = slog.New(w.notes)
	// Kept, so a case may build a second server of the same world with one
	// field changed (world.rebuild) instead of writing the literal again.
	w.deps = Deps{Node: node, Noise: ctlKey, Chain: k, State: st, Engine: w.eng,
		Now: func() int64 { return now }, Log: w.log}
	w.srv = New(w.deps)
	return w
}

// rebuild puts a server of this world with changed dependencies in place of
// the one newWorld made. It is called before the tunnel of a case stands, so
// no goroutine of a channel reads w.srv while it is written.
func (w *world) rebuild(change func(d *Deps)) {
	d := w.deps
	change(&d)
	w.srv = New(d)
}

// manifest is the manifest of the installed app: one service with one action.
func manifest() []byte {
	return []byte(`{"id":"` + testApp + `","name":"Terminal","version":"1.0.0","publisher":"Sorglos",` +
		`"services":[{"name":"` + testService + `","command":["/bin/terminal"]}],` +
		`"actions":[{"name":"` + testAction + `","title":"Ausführen","service":"` + testService + `"}]}`)
}

// quickAction puts the app back with a manifest whose one action has this time
// limit in seconds, so a case about the clock of an action does not have to sit
// out the service.DefaultTimeout of the contract.
func (w *world) quickAction(t *testing.T, sec int) {
	t.Helper()
	m := bytes.Replace(manifest(), []byte(`"service":"`+testService+`"`),
		[]byte(`"service":"`+testService+`","timeout":`+strconv.Itoa(sec)), 1)
	if bytes.Equal(m, manifest()) {
		t.Fatal("the manifest of the fixture changed: no action to put a timeout on")
	}
	if err := w.st.PutApp(context.Background(), dbapi.App{ID: testApp, Version: "1.0.0",
		Store: trust.StoreGlobal, Node: node, Active: true, Manifest: m,
		SHA256: bytes.Repeat([]byte{0x01}, 32)}); err != nil {
		t.Fatal(err)
	}
}

// next is version 2 of the fixture's list, signed by the same admin.
func (w *world) next(t testing.TB) trust.Signed {
	t.Helper()
	return w.admin.Sign(t, trusttest.Next(t, w.v1))
}

// chainOf wraps versions the way GET session.PathTrust answers them; that is
// what the API sends on ngc.users/controller/trust.
func chainOf(t testing.TB, v ...trust.Signed) []byte {
	t.Helper()
	return mustJSON(t, session.TrustChain{Versions: v})
}

// trustOpen is the OPEN of ngc.users/controller/trust, which the API opens on
// its own: no client certificate and no proof (core.WithoutUser).
func trustOpen(t testing.TB) []byte {
	t.Helper()
	inner := mustJSON(t, wire.Open{App: core.AppUsers, Service: core.ServiceController, Action: core.ActTrust,
		Window: wire.DefaultWindow})
	return mustJSON(t, tunnel.Open{Open: inner, Window: wire.MaxWindow})
}

// appOpen is the OPEN of an app channel: message 1 of the end-to-end
// handshake, built over the prologue of exactly this OPEN, and the client
// certificate of the fixture's admin the way the API attaches it. It gives
// back the OPEN payload of the tunnel and the shell's handshake, which is
// waiting for message 2.
func (w *world) appOpen(t testing.TB, params json.RawMessage) ([]byte, *noise.HandshakeState) {
	t.Helper()
	return w.appOpenFor(t, node, params)
}

// appOpenFor is appOpen with the node the shell hashed into its prologue. On
// every channel but a test of the handshake that is this controller: a
// prologue over another node is what an OPEN meant for another controller
// looks like when it arrives here.
func (w *world) appOpenFor(t testing.TB, on string, params json.RawMessage) ([]byte, *noise.HandshakeState) {
	t.Helper()
	return w.appOpenOn(t, on, params, wire.MaxWindow)
}

// appOpenWin is appOpen with the credit the other end gives this channel. One
// inner message of the controller has to fit it, because it travels in one
// DATA frame (link.Channel.SendWhole).
func (w *world) appOpenWin(t testing.TB, params json.RawMessage, win uint32) ([]byte, *noise.HandshakeState) {
	t.Helper()
	return w.appOpenOn(t, node, params, win)
}

// appOpenOn builds that OPEN.
func (w *world) appOpenOn(t testing.TB, on string, params json.RawMessage,
	win uint32) ([]byte, *noise.HandshakeState) {
	t.Helper()
	shell, _ := trusttest.NodeKeys(0x52)
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	hs, err := noisep.Handshake(true, noisep.E2EPrologue(on, testApp, testService, testAction, params, nil),
		shell, ctlKey.Public, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := wire.Open{App: testApp, Service: testService, Action: testAction, Params: params,
		Window: wire.DefaultWindow, E2E: msg1}
	sessSig := ed25519.NewKeyFromSeed(sessionSeed)
	client := mustJSON(t, session.ClientCert{Installation: w.list.Installation, User: trusttest.ID(0x21),
		Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize), Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)),
		DH: shell.Public, Issued: issued, Expires: issued + 3600})
	p := w.admin.Prove(sign.PurposeClientCert, client)
	return mustJSON(t, tunnel.Open{Open: mustJSON(t, inner), Window: win, Client: client, Proof: &p}), hs
}

// apiSide plays the API at the other end of the tunnel: the answering half of
// the Noise handshake and every frame after it. All of it runs in the
// goroutine of the test server, where t.Fatal would leave the test standing,
// so it reports with t.Error and gives up.
type apiSide struct {
	t  *testing.T
	c  *websocket.Conn
	tr *noisep.Transport
}

// tunnel dials a tunnel from this controller to a test API that does what
// after says, and runs it. It gives back the end of Run. Both halves write to
// the one log of the world, so a line link writes for a handler that came
// back with an error is counted there like everything else.
func (w *world) tunnel(t *testing.T, after func(*apiSide)) <-chan error {
	t.Helper()
	var h link.Handler = w.srv
	apiKey, _ := trusttest.NodeKeys(apiFill)
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	cert, pin := trusttest.APITLS(apiTLSFill, apiHost)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		a := accept(t, rw, r)
		if a == nil {
			return
		}
		defer a.c.CloseNow()
		a.greet()
		after(a)
	}))
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	tn, err := link.Dial(context.Background(), link.Config{
		API:     config.APIRef{URL: "wss://" + ts.Listener.Addr().String() + "/tunnel", Key: apiKey.Public, TLS: pin},
		Noise:   ctlKey,
		Hello:   func() tunnel.Hello { return tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract} },
		Handler: h,
		Log:     w.log,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { tn.Close() })
	ended := make(chan error, 1)
	go func() { ended <- tn.Run(context.Background()) }()
	return ended
}

// accept takes the WebSocket of a tunnel and answers the Noise handshake the
// way the API does.
func accept(t *testing.T, w http.ResponseWriter, r *http.Request) *apiSide {
	apiKey, _ := trusttest.NodeKeys(apiFill)
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{tunnel.Subprotocol}})
	if err != nil {
		t.Error(err)
		return nil
	}
	c.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	hs, err := noisep.Handshake(false, []byte(noisep.PrologueTunnel), apiKey, ctlKey.Public, rand.Reader)
	if err != nil {
		t.Error(err)
		c.CloseNow()
		return nil
	}
	typ, msg1, err := c.Read(r.Context())
	if err != nil || typ != websocket.MessageBinary {
		t.Errorf("message 1 of type %v: %v", typ, err)
		c.CloseNow()
		return nil
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		t.Errorf("message 1: %v", err)
		c.CloseNow()
		return nil
	}
	msg2, cs1, cs2, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Errorf("message 2: %v", err)
		c.CloseNow()
		return nil
	}
	if err := c.Write(r.Context(), websocket.MessageBinary, msg2); err != nil {
		t.Error(err)
		c.CloseNow()
		return nil
	}
	return &apiSide{t: t, c: c, tr: noisep.NewTransport(false, cs1, cs2)}
}

// greet reads the hello of the controller and answers with the API's.
func (a *apiSide) greet() {
	f, err := a.next()
	if err != nil {
		a.t.Errorf("hello of the controller: %v", err)
		return
	}
	c, err := wire.ParseControl(f.Payload)
	if err != nil || f.Channel != wire.ControlChannel || c.T != wire.CtlHello {
		a.t.Errorf("the first message was %v on channel %d (%v), want the hello", f.Type, f.Channel, err)
		return
	}
	a.control(wire.CtlHello, tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract, Node: node, TrustSeq: 1})
}

// send writes one frame as the API does: one transport message with one frame.
func (a *apiSide) send(f wire.Frame) {
	b, err := f.Encode()
	if err != nil {
		a.t.Error(err)
		return
	}
	msg, err := a.tr.Seal(b)
	if err != nil {
		a.t.Error(err)
		return
	}
	if err := a.c.Write(context.Background(), websocket.MessageBinary, msg); err != nil {
		a.t.Error(err)
	}
}

// control sends one control message on channel 0.
func (a *apiSide) control(typ string, body any) {
	var raw json.RawMessage
	if body != nil {
		raw = mustJSON(a.t, body)
	}
	a.send(wire.Frame{Type: wire.TypeData, Payload: mustJSON(a.t, wire.Control{T: typ, B: raw})})
}

// data sends one DATA frame on num.
func (a *apiSide) data(num uint32, b []byte) {
	a.send(wire.Frame{Type: wire.TypeData, Channel: num, Payload: b})
}

// eof sends the EOF of the API on num.
func (a *apiSide) eof(num uint32) { a.send(wire.Frame{Type: wire.TypeEOF, Channel: num}) }

// shut sends the CLOSE of the API on num, which frees the number.
func (a *apiSide) shut(num uint32) {
	a.send(wire.Frame{Type: wire.TypeClose, Channel: num, Payload: mustJSON(a.t, wire.Close{Status: wire.StatusOK})})
}

// next reads one frame of the controller and gives up after answer.
func (a *apiSide) next() (wire.Frame, error) {
	return a.nextIn(answer)
}

// nextIn is next with a wait of its own, for a case that has to find out that
// nothing comes.
func (a *apiSide) nextIn(d time.Duration) (wire.Frame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	typ, msg, err := a.c.Read(ctx)
	if err != nil {
		return wire.Frame{}, err
	}
	if typ != websocket.MessageBinary {
		a.t.Errorf("the controller sent a message of type %v", typ)
		return wire.Frame{}, errors.New("no binary message")
	}
	plain, err := a.tr.Open(msg)
	if err != nil {
		return wire.Frame{}, err
	}
	return wire.Decode(plain)
}

// open sends the OPEN of channel num and reads the first GRANT of the
// controller, whose credit it gives back. On an app channel that GRANT also
// carries Noise message 2.
func (a *apiSide) open(num uint32, payload []byte) (int64, []byte) {
	a.send(wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: payload})
	f := a.want(wire.TypeGrant, num)
	credit, handshake, err := wire.ParseGrant(f.Payload)
	if err != nil {
		a.t.Errorf("first GRANT of channel %d: %v", num, err)
		return 0, nil
	}
	return int64(credit), handshake
}

// want reads the next frame of the controller and checks what it is.
func (a *apiSide) want(typ wire.Type, num uint32) wire.Frame {
	f, err := a.next()
	if err != nil {
		a.t.Errorf("waiting for %v on channel %d: %v", typ, num, err)
		return wire.Frame{}
	}
	if f.Type != typ || f.Channel != num {
		a.t.Errorf("the controller sent %v on channel %d, want %v on channel %d", f.Type, f.Channel, typ, num)
	}
	return f
}

// result reads the frames of num up to its CLOSE: the DATA are the result,
// the GRANT are credit for input this side already sent.
func (a *apiSide) result(num uint32) ([]byte, wire.Close) {
	var out []byte
	for {
		f, err := a.next()
		if err != nil {
			a.t.Errorf("waiting for the end of channel %d: %v", num, err)
			return nil, wire.Close{}
		}
		if f.Channel != num {
			a.t.Errorf("the controller sent %v on channel %d, want channel %d", f.Type, f.Channel, num)
			return nil, wire.Close{}
		}
		switch f.Type {
		case wire.TypeData:
			out = append(out, f.Payload...)
		case wire.TypeEOF, wire.TypeGrant:
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				a.t.Errorf("CLOSE of channel %d: %v", num, err)
			}
			return out, cl
		default:
			a.t.Errorf("the controller sent %v on channel %d", f.Type, num)
			return nil, wire.Close{}
		}
	}
}

// closed is result for a channel that answers nothing.
func (a *apiSide) closed(num uint32) wire.Close {
	out, cl := a.result(num)
	if len(out) != 0 {
		a.t.Errorf("the controller sent %d bytes on a channel it refuses", len(out))
	}
	return cl
}

// blob sends n bytes of DATA on num in frames of at most wire.MaxPayload and
// waits for the credit of the controller in between: credit is what its first
// GRANT gave. It stops when the controller closes the channel, so a controller
// that refuses early is not waited out.
func (a *apiSide) blob(num uint32, n, credit int64) {
	chunk := bytes.Repeat([]byte{'x'}, wire.MaxPayload)
	for n > 0 {
		m := int64(len(chunk))
		if m > n {
			m = n
		}
		for credit < m {
			f, err := a.next()
			if err != nil {
				a.t.Errorf("waiting for credit on channel %d: %v", num, err)
				return
			}
			if f.Channel != num || f.Type == wire.TypeClose {
				a.t.Errorf("the controller sent %v on channel %d while %d bytes were still to come",
					f.Type, f.Channel, n)
				return
			}
			if f.Type != wire.TypeGrant {
				continue
			}
			c, _, err := wire.ParseGrant(f.Payload)
			if err != nil {
				a.t.Errorf("GRANT of channel %d: %v", num, err)
				return
			}
			credit += int64(c)
		}
		a.data(num, chunk[:m])
		credit -= m
		n -= m
	}
}

// bye ends the tunnel in an orderly way and reads what is left of it.
func (a *apiSide) bye() {
	a.control(wire.CtlBye, nil)
	for {
		if _, _, err := a.c.Read(context.Background()); err != nil {
			return
		}
	}
}

// waitFor takes the end of Run, but not for ever.
func waitFor(t *testing.T, ended <-chan error) error {
	t.Helper()
	select {
	case err := <-ended:
		return err
	case <-time.After(answer):
		t.Fatal("Run did not end")
		return nil
	}
}

// wantCode fails unless the channel ended with exactly this error code and
// the German message that belongs to it.
func wantCode(t *testing.T, cl wire.Close, code wire.Code) {
	t.Helper()
	if cl.Status != wire.StatusError || cl.Code != code || cl.Message != code.Message() {
		t.Errorf("the channel ended with %+v, want status error and code %s with its message", cl, code)
	}
}

// The API sends a chain on ngc.users/controller/trust; the controller verifies
// it, keeps it and answers with the number of the newest version it now holds.
func TestTrustChannelAnswersWithNewestSeq(t *testing.T) {
	w := newWorld(t)
	v2 := w.next(t)
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, trustOpen(t))
		a.data(1, chainOf(t, v2))
		a.eof(1)
		out, cl := a.result(1)
		if cl.Status != wire.StatusOK || cl.Code != "" {
			t.Errorf("the channel ended with %+v, want status ok", cl)
		}
		var got core.TrustResult
		if err := json.Unmarshal(out, &got); err != nil {
			t.Errorf("the result %q is no TrustResult: %v", out, err)
		} else if got.Seq != 2 {
			t.Errorf("the controller answered seq %d, want 2", got.Seq)
		}
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := w.keep.TrustSeq(); seq != 2 {
		t.Errorf("the keeper holds version %d, want the one it just verified", seq)
	}
}

// fresh puts a controller between pairing and its first chain in place of the
// one newWorld built: a keeper over an empty state, anchored at a, and a
// server over that keeper. load says whether the keeper read the state — one
// that never did refuses every chain (chain.Keeper.Load), which is the case
// TestATrustListThatCannotBeKeptWritesOneLineForManyChannels needs.
func (w *world) fresh(t *testing.T, a trust.Anchor, load bool) *chain.Keeper {
	t.Helper()
	st := state.New(memstore.New())
	t.Cleanup(func() { st.Close() })
	k := chain.New(st, a)
	if load {
		if err := k.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
		if k.List() != nil {
			t.Fatal("a controller that stored nothing holds a list")
		}
	}
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	w.keep, w.st = k, st
	w.srv = New(Deps{Node: node, Noise: ctlKey, Chain: k, State: st,
		Now: func() int64 { return now }, Log: w.log})
	return k
}

// A controller between pairing and its first chain: it holds the anchor and
// has stored no version. That is the state the design means (chain.Keeper
// takes its first chain from the anchor), and it is the one a controller
// cannot get out of on its own — the chain arrives on a channel. So the
// channel runs, the first version lands, and the one after it lands on the
// ordinary way.
func TestAFreshControllerTakesItsFirstChain(t *testing.T) {
	w := newWorld(t)
	k := w.fresh(t, trust.Anchor{Seq: 1, Hash: trust.Hash(w.v1)}, true)
	// Anchored at 1, so the number it reports is 0 and the chain an API sends
	// after it starts at version 1 — the anchored one. That the two numbers
	// agree here is what hid the case below for a whole round:
	// TestAFreshControllerAnchoredInTheMiddleTakesItsFirstChain is a
	// controller anchored anywhere else.
	if seq := k.TrustSeq(); seq != 0 {
		t.Fatalf("a controller anchored at 1 asks from %d on, want 1", seq+1)
	}
	v1, v2 := w.v1, w.next(t)
	ended := w.tunnel(t, func(a *apiSide) {
		// The anchor's own version is the first link of the chain.
		a.open(1, trustOpen(t))
		a.data(1, chainOf(t, v1))
		a.eof(1)
		out, cl := a.result(1)
		if cl.Status != wire.StatusOK || cl.Code != "" {
			t.Fatalf("the first chain ended with %+v, want status ok", cl)
		}
		var got core.TrustResult
		if err := json.Unmarshal(out, &got); err != nil || got.Seq != 1 {
			t.Errorf("the controller answered %q (%v), want seq 1", out, err)
		}
		a.shut(1)
		// From here the ordinary way carries it: the controller now has a
		// list, and the next version goes through the check like any other.
		a.open(2, trustOpen(t))
		a.data(2, chainOf(t, v2))
		a.eof(2)
		out, cl = a.result(2)
		if cl.Status != wire.StatusOK || cl.Code != "" {
			t.Errorf("the second chain ended with %+v, want status ok", cl)
		}
		if err := json.Unmarshal(out, &got); err != nil || got.Seq != 2 {
			t.Errorf("the controller answered %q (%v), want seq 2", out, err)
		}
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := k.TrustSeq(); seq != 2 {
		t.Errorf("the keeper holds version %d, want the second one it verified", seq)
	}
}

// The controller a pairing produces once the list has moved past its first
// version: anchored somewhere in the middle of the chain, holding nothing. What it puts into its
// hello is the number after which it needs versions, and an API answers that
// with the chain from the anchored version on — the only chain trust.Open
// takes. A controller that said 0 here would be sent the chain from version 1,
// and would have to refuse it.
func TestAFreshControllerAnchoredInTheMiddleTakesItsFirstChain(t *testing.T) {
	w := newWorld(t)
	v1 := w.v1
	v2 := w.admin.Sign(t, trusttest.Next(t, v1))
	v3 := w.admin.Sign(t, trusttest.Next(t, v2))
	k := w.fresh(t, trust.Anchor{Seq: 2, Hash: trust.Hash(v2)}, true)
	if seq := k.TrustSeq(); seq != 1 {
		t.Fatalf("a controller anchored at 2 asks from %d on, want 2", seq+1)
	}
	ended := w.tunnel(t, func(a *apiSide) {
		// What an API sends after a reported 0: versions 1 and 2. Signed as
		// well as any other chain, and still not this controller's, so it is
		// refused as the sender's fault.
		a.open(1, trustOpen(t))
		a.data(1, chainOf(t, v1, v2))
		a.eof(1)
		wantCode(t, a.closed(1), wire.CodeVerify)
		a.shut(1)
		// What it sends after the reported 1: versions 2 and 3.
		a.open(2, trustOpen(t))
		a.data(2, chainOf(t, v2, v3))
		a.eof(2)
		out, cl := a.result(2)
		if cl.Status != wire.StatusOK || cl.Code != "" {
			t.Fatalf("the first chain ended with %+v, want status ok", cl)
		}
		var got core.TrustResult
		if err := json.Unmarshal(out, &got); err != nil || got.Seq != 3 {
			t.Errorf("the controller answered %q (%v), want seq 3", out, err)
		}
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := k.TrustSeq(); seq != 3 {
		t.Errorf("the keeper holds version %d, want the newest of the chain it took", seq)
	}
}

// A chain that misses the anchor is the fault of what was sent, and it has to
// be named as one. The admin reads E_VERIFY as "Unterschrift ungültig." and
// E_INTERNAL as "Interner Fehler."; the first sends him to look at what the
// API handed over, the second at his own server, and here the API is the one
// that sent the wrong window.
func TestASignedVersionPastTheAnchorIsTheSendersFault(t *testing.T) {
	w := newWorld(t)
	v2 := w.next(t)
	k := w.fresh(t, trust.Anchor{Seq: 1, Hash: trust.Hash(w.v1)}, true)
	ended := w.tunnel(t, func(a *apiSide) {
		// Version 2, signed by the admin of the list and correct in every way
		// — only it is not the version this controller is anchored at.
		a.open(1, trustOpen(t))
		a.data(1, chainOf(t, v2))
		a.eof(1)
		wantCode(t, a.closed(1), wire.CodeVerify)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := k.TrustSeq(); seq != 0 {
		t.Errorf("the keeper holds version %d after a chain it refused", seq)
	}
	// Nothing of it reached the log: the fault came in from outside.
	if n := w.notes.n.Load(); n != 0 {
		t.Errorf("a chain that missed the anchor wrote %d lines", n)
	}
}

// An anchor of this controller that pins nothing is this server's own fault,
// and this is that fault at the wire: the channel ends E_INTERNAL, not a line
// is written, and StoreFailures does not move. The counter is what separates
// the two local faults — a store that could not be used is a state that may
// come back, an anchor that pins nothing is a configuration that will not, and
// only the first of them is counted and written.
//
// Three anchors here, but the wire cannot tell them apart: chain.Keeper.Apply
// turns all three into the same chain.ErrNoAnchor, so this test reaches one
// answer three times over, not three. That each is refused on its own —
// without a number, without a hash, with a hash that is no SHA-256
// (chain.validAnchor) — is what TestAnAnchorThatPinsNothingIsThisServersFault
// in chain holds. This test stays: it costs nothing and still walks the path
// over the wire, and all three are read before Apply asks whether the state
// was ever loaded, so none of them needs a keeper that read it.
func TestAnAnchorThatPinsNothingEndsEveryChannelWithoutALine(t *testing.T) {
	w := newWorld(t)
	v2 := w.next(t)
	for _, c := range []struct {
		name   string
		anchor trust.Anchor
	}{
		{"without a number", trust.Anchor{Hash: trust.Hash(w.v1)}},
		{"without a hash", trust.Anchor{Seq: 1}},
		{"with a hash that is no SHA-256", trust.Anchor{Seq: 1, Hash: trust.Hash(w.v1)[:16]}},
	} {
		w.fresh(t, c.anchor, false)
		ended := w.tunnel(t, func(a *apiSide) {
			a.open(1, trustOpen(t))
			a.data(1, chainOf(t, v2))
			a.eof(1)
			wantCode(t, a.closed(1), wire.CodeInternal)
			a.shut(1)
			a.bye()
		})
		if err := waitFor(t, ended); err != nil {
			t.Fatalf("an anchor %s: Run = %v, want an orderly end after bye", c.name, err)
		}
		if got := w.srv.StoreFailures(); got != 0 {
			t.Errorf("an anchor %s counted %d store failures, want 0: an anchor that pins nothing is no store that failed",
				c.name, got)
		}
	}
	if n := w.notes.n.Load(); n != 0 {
		t.Errorf("three anchors that pin nothing wrote %d lines, want none", n)
	}
}

// The same fault over one tunnel, many channels: not one of them writes. This
// is the half three servers cannot hold, and it is the half that matters, because
// how many channels arrive is the API's to choose and not this server's. A line
// in that branch would be a line a peer can ask for as often as it likes, on a
// controller whose anchor it cannot repair by asking.
//
// Eight is enough to see a slip: a rule that writes once per channel gives
// eight, one that writes every channel but the first gives seven, and both are
// as far from none as any number is.
func TestManyChannelsAtOneAnchorThatPinsNothingWriteNoLine(t *testing.T) {
	w := newWorld(t)
	v2 := w.next(t)
	// One controller, one anchor that pins nothing — here an anchor file that
	// carries a number and no hash — and every channel of one tunnel meeting
	// it.
	w.fresh(t, trust.Anchor{Seq: 1}, false)
	const channels = 8
	ended := w.tunnel(t, func(a *apiSide) {
		for n := uint32(1); n <= channels; n++ {
			a.open(n, trustOpen(t))
			a.data(n, chainOf(t, v2))
			a.eof(n)
			wantCode(t, a.closed(n), wire.CodeInternal)
			a.shut(n)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.notes.n.Load(); n != 0 {
		t.Errorf("%d channels at an anchor that pins nothing wrote %d lines, want none", channels, n)
	}
	if got := w.srv.StoreFailures(); got != 0 {
		t.Errorf("StoreFailures = %d after %d such channels, want 0: an anchor that pins nothing is no store that failed",
			got, channels)
	}
}

// An anchor of this controller that is well formed and still not this
// installation's — paired against another installation, or an anchor file
// copied off a second server — answers a faultless chain with E_VERIFY, which
// reads "Unterschrift ungültig." to the admin. Such a controller turns away
// every API there is and names each of them at fault.
//
// This test is no approval of that. It is the receipt on the gap serve/trust.go
// names and cannot close: out of one chain nothing says whether the chain or
// the anchor is the wrong one, so what goes out is the code for a wrong chain.
// Whoever finds a second word on which version this controller's anchor names
// can tell the two apart, and changes this test first.
//
// This gap cannot be fenced any tighter on its own: the same line — trust's
// final return nil, refuse(wire.CodeVerify) — is what
// TestAFreshControllerAnchoredInTheMiddleTakesItsFirstChain and
// TestASignedVersionPastTheAnchorIsTheSendersFault fall through to as well,
// each for a chain of its own. Red here on its own is that gap closing, not a
// fence breaking: fix the comment above first, then retire this test.
func TestAnAnchorCopiedFromElsewhereBlamesEverySender(t *testing.T) {
	w := newWorld(t)
	// Version 1 of a second installation: the same admin, correct in every
	// way, and no version of this controller's list. Its hash is a SHA-256
	// like any other, so an anchor file holding it is readable and looks right
	// (chain.validAnchor answers yes).
	other := trusttest.Genesis(w.admin)
	other.Installation = trusttest.ID(0x12)
	if err := other.Validate(); err != nil {
		t.Fatalf("version 1 of the second installation: %v", err)
	}
	foreign := w.admin.Sign(t, other)
	k := w.fresh(t, trust.Anchor{Seq: 1, Hash: trust.Hash(foreign)}, true)
	ended := w.tunnel(t, func(a *apiSide) {
		// What this controller's own API sends: version 1 of its own
		// installation, signed by an admin of it and faultless.
		a.open(1, trustOpen(t))
		a.data(1, chainOf(t, w.v1))
		a.eof(1)
		wantCode(t, a.closed(1), wire.CodeVerify)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := k.TrustSeq(); seq != 0 {
		t.Errorf("the keeper holds version %d after a chain its anchor refused", seq)
	}
}

// A trust list this server cannot keep is a local fault and worth a line — but
// only one. How many channels meet it is the API's to choose, and a line for
// each would let a peer fill this server's log. A keeper that never read its
// state is that fault today: nothing outside tests calls Load.
func TestATrustListThatCannotBeKeptWritesOneLineForManyChannels(t *testing.T) {
	w := newWorld(t)
	v2 := w.next(t)
	w.fresh(t, trust.Anchor{Seq: 1, Hash: trust.Hash(w.v1)}, false)
	const channels = 5
	ended := w.tunnel(t, func(a *apiSide) {
		for n := uint32(1); n <= channels; n++ {
			a.open(n, trustOpen(t))
			a.data(n, chainOf(t, v2))
			a.eof(n)
			wantCode(t, a.closed(n), wire.CodeInternal)
			a.shut(n)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.notes.n.Load(); n != 1 {
		t.Errorf("%d channels wrote %d lines, want 1", channels, n)
	}
	if got := w.srv.StoreFailures(); got != channels {
		t.Errorf("StoreFailures = %d, want %d", got, channels)
	}
}

// The one line belongs to the first channel that meets the fault, and a single
// channel is that first one. This is the half the count of many channels
// cannot hold: a server with one API and one such channel is the case an
// operator most needs to see, and a rule that wrote from the second channel on
// would leave exactly that one silent.
func TestTheFirstChannelThatCannotKeepTheTrustListWritesTheLine(t *testing.T) {
	w := newWorld(t)
	v2 := w.next(t)
	w.fresh(t, trust.Anchor{Seq: 1, Hash: trust.Hash(w.v1)}, false)
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, trustOpen(t))
		a.data(1, chainOf(t, v2))
		a.eof(1)
		wantCode(t, a.closed(1), wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.notes.n.Load(); n != 1 {
		t.Errorf("one channel that could not keep the trust list wrote %d lines, want 1", n)
	}
	if got := w.srv.StoreFailures(); got != 1 {
		t.Errorf("StoreFailures = %d, want 1", got)
	}
}

// A chain that does not verify changes nothing: the channel ends with
// E_VERIFY and the controller still holds the version it held before.
func TestTrustChannelRefusesBrokenChain(t *testing.T) {
	w := newWorld(t)
	// Version 2 signed by somebody the list does not know.
	broken := trusttest.NewSigner(9).Sign(t, trusttest.Next(t, w.v1))
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, trustOpen(t))
		a.data(1, chainOf(t, broken))
		a.eof(1)
		wantCode(t, a.closed(1), wire.CodeVerify)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := w.keep.TrustSeq(); seq != 1 {
		t.Errorf("the keeper holds version %d, want the one it held before the broken chain", seq)
	}
}

// More input than core.Action.MaxIn allows ends the channel with E_TOOLARGE,
// and nothing of it reaches the action.
func TestInputAboveMaxInClosesWithTooLarge(t *testing.T) {
	w := newWorld(t)
	act, ok := core.Lookup(core.AppUsers, core.ServiceController, core.ActTrust)
	if !ok || act.MaxIn <= 0 {
		t.Fatalf("the trust action of this controller has no input limit: %+v", act)
	}
	ended := w.tunnel(t, func(a *apiSide) {
		credit, _ := a.open(1, trustOpen(t))
		a.blob(1, act.MaxIn+1, credit)
		wantCode(t, a.closed(1), wire.CodeTooLarge)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := w.keep.TrustSeq(); seq != 1 {
		t.Errorf("the keeper holds version %d, want nothing of an input it refused", seq)
	}
}

// A result above wire.MaxCallResult is not sent: the channel ends with
// E_TOOLARGE instead.
func TestCallResultAboveLimitClosesWithTooLarge(t *testing.T) {
	w := newWorld(t)
	// The one action this build carries out answers a number; a result that
	// is too large has to be put there.
	w.srv.acts[trustKey] = func(context.Context, access.Decision, []byte) (any, *wire.Close) {
		return strings.Repeat("x", wire.MaxCallResult), nil
	}
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, trustOpen(t))
		a.eof(1)
		wantCode(t, a.closed(1), wire.CodeTooLarge)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A result that is none of the contract for another reason than its size ends
// the channel with E_TOOLARGE all the same: core.EncodeResult refuses three
// things with one error, and this is the counter-case to the one the code
// names. Whoever tells them apart fails here.
func TestAResultWithANullIsNamedTooLargeAsWell(t *testing.T) {
	w := newWorld(t)
	w.srv.acts[trustKey] = func(context.Context, access.Decision, []byte) (any, *wire.Close) {
		// A typed nil is a null, and no result of this contract holds one.
		return (*core.TrustResult)(nil), nil
	}
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, trustOpen(t))
		a.eof(1)
		wantCode(t, a.closed(1), wire.CodeTooLarge)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A server that was handed none of its halves refuses every channel instead
// of taking this controller down with it: it runs as root, and a nil keeper
// or store would be a panic on the first OPEN.
func TestAServerWiredWrongRefusesEveryChannel(t *testing.T) {
	w := newWorld(t)
	w.srv = New(Deps{Node: node, Log: w.log})
	ended := w.tunnel(t, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: trustOpen(t)})
		f := a.want(wire.TypeClose, 1)
		cl, err := wire.ParseClose(f.Payload)
		if err != nil {
			t.Errorf("CLOSE of channel 1: %v", err)
		}
		wantCode(t, cl, wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// stageOpen is the OPEN of ngc.store/controller/stage: a core action of this
// controller that the API opens without a user and that this build does not
// carry out.
func stageOpen(t testing.TB) []byte {
	t.Helper()
	inner := mustJSON(t, wire.Open{App: core.AppStore, Service: core.ServiceController, Action: core.ActStage,
		Window: wire.DefaultWindow})
	return mustJSON(t, tunnel.Open{Open: inner, Window: wire.MaxWindow})
}

// A core action this build does not carry out is refused before anything is
// granted: the CLOSE is the first frame of the channel, so no API makes this
// server hold a byte for work that never runs.
func TestAnActionThisBuildDoesNotCarryOutIsRefusedWithoutAGrant(t *testing.T) {
	w := newWorld(t)
	ended := w.tunnel(t, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: stageOpen(t)})
		f := a.want(wire.TypeClose, 1)
		cl, err := wire.ParseClose(f.Payload)
		if err != nil {
			t.Errorf("CLOSE of channel 1: %v", err)
		}
		wantCode(t, cl, wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// An input that ends before its EOF runs no action: the channel says so and
// the controller keeps what it held.
func TestAnInputCutOffBeforeItsEOFChangesNothing(t *testing.T) {
	w := newWorld(t)
	full := chainOf(t, w.next(t))
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, trustOpen(t))
		a.data(1, full[:len(full)/2])
		// The API closes instead of sending its EOF.
		a.shut(1)
		wantCode(t, a.closed(1), wire.CodeProtocol)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := w.keep.TrustSeq(); seq != 1 {
		t.Errorf("the keeper holds version %d, want nothing of an input that was cut off", seq)
	}
}

// finish completes the shell's half of the end-to-end handshake with the
// message 2 the first GRANT carried.
func finish(t testing.TB, hs *noise.HandshakeState, msg2 []byte) *noisep.Transport {
	t.Helper()
	payload, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil || len(payload) != 0 {
		t.Fatalf("message 2: %v, payload %d bytes", err, len(payload))
	}
	return noisep.NewTransport(true, cs1, cs2)
}

// seal builds one inner message of the shell and encrypts it for the
// controller.
func seal(t testing.TB, tr *noisep.Transport, kind byte, body []byte) []byte {
	t.Helper()
	plain, err := wire.E2E(kind, body)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := tr.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

// The handshake is answered in the first GRANT, and the channel goes on only
// once an inner message of the shell has decrypted. What the wait releases is
// the service of the app (app.go); this case holds the shape of that first
// GRANT and that the channel runs to its end behind it.
func TestAppChannelStartsNothingBeforeTheFirstMessageDecrypts(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		credit, msg2 := a.open(1, payload)
		if credit < wire.MinWindow || len(msg2) != wire.HandshakeSize {
			t.Errorf("the first GRANT gave %d bytes of credit and %d of handshake, want a window and Noise message 2",
				credit, len(msg2))
		}
		sh := &shell{t: t, a: a, num: 1, tr: finish(t, hs, msg2), credit: credit}
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		_, end := sh.out()
		wantOK(t, end, "the end-to-end end")
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.srv.HandshakeFailures(); n != 0 {
		t.Errorf("%d handshakes counted as failed, want none", n)
	}
}

// Message 1 can be repeated: whoever listened in on an OPEN gets a session of
// his own out of it, and this test builds that case instead of claiming it
// cannot happen. What he does not get is the transport keys, which come out
// of the ephemeral key of the real shell — so the message that decrypted on
// the first channel is nothing on the second, and nothing is started for it.
func TestAnOpenRepeatedWithoutTheShellsKeysReachesNothing(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		credit, first := a.open(1, payload)
		tr := finish(t, hs, first)
		msg := seal(t, tr, wire.E2EData, nil)
		sh := &shell{t: t, a: a, num: 1, tr: tr, credit: credit - int64(len(msg))}
		a.data(1, msg)
		sh.inner(wire.E2EEnd, nil)
		_, end := sh.out()
		wantOK(t, end, "the end-to-end end of the real shell")
		wantOK(t, sh.rest(), "the CLOSE of the channel of the real shell")
		a.shut(1)
		// The same OPEN once more. It is taken, and it is answered with a
		// message 2 of its own.
		_, second := a.open(2, payload)
		if len(second) != wire.HandshakeSize {
			t.Fatalf("the repeated OPEN got %d bytes of handshake, want Noise message 2", len(second))
		}
		if bytes.Equal(first, second) {
			t.Error("the repeated OPEN was answered with the message 2 of the first channel")
		}
		a.data(2, msg)
		wantCode(t, a.closed(2), wire.CodeProtocol)
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A shell that sends no first message holds the channel for as long as the
// contract allows and no longer.
func TestAppChannelWaitsNoLongerThanTheFirstTimeout(t *testing.T) {
	w := newWorld(t)
	w.srv.first = 50 * time.Millisecond
	payload, _ := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, payload)
		wantCode(t, a.closed(1), wire.CodeTimeout)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A controller wired without its own Noise key answers every app channel the
// way it answers an attack: E_INTERNAL, counted, and not a line anywhere. That
// is the price of not letting a peer write the log, and this is the shape an
// operator reads it by — HandshakeFailures climbing with every app channel
// while not one of them ever comes off.
func TestAServerWithoutItsNoiseKeyRefusesEveryAppChannelTheSameWay(t *testing.T) {
	w := newWorld(t)
	w.srv = New(Deps{Node: node, Chain: w.keep, State: w.st, Engine: w.eng,
		Now: func() int64 { return now }, Log: w.log})
	payload, _ := w.appOpen(t, nil)
	const channels = 3
	ended := w.tunnel(t, func(a *apiSide) {
		for num := uint32(1); num <= channels; num++ {
			a.send(wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: payload})
			f := a.want(wire.TypeClose, num)
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				t.Errorf("CLOSE of channel %d: %v", num, err)
			}
			wantCode(t, cl, wire.CodeInternal)
			a.shut(num)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if got := w.srv.HandshakeFailures(); got != channels {
		t.Errorf("HandshakeFailures = %d, want %d", got, channels)
	}
	if n := w.notes.n.Load(); n != 0 {
		t.Errorf("a missing Noise key wrote %d lines, want none", n)
	}
}

// A handshake that does not come off is counted and nothing of it is written:
// an OPEN whose prologue names another controller is one a shell may send as
// often as it likes, and a line per channel would be its to write.
func TestAHandshakeThatFailsIsCountedAndNotWritten(t *testing.T) {
	w := newWorld(t)
	payload, _ := w.appOpenFor(t, "ctl-2", nil)
	ended := w.tunnel(t, func(a *apiSide) {
		for num := uint32(1); num <= 3; num++ {
			a.send(wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: payload})
			f := a.want(wire.TypeClose, num)
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				t.Errorf("CLOSE of channel %d: %v", num, err)
			}
			wantCode(t, cl, wire.CodeInternal)
			a.shut(num)
		}
		if n := w.notes.n.Load(); n != 0 {
			t.Errorf("%d lines were written for three handshakes a shell brought about, want none", n)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.srv.HandshakeFailures(); n != 3 {
		t.Errorf("HandshakeFailures = %d, want the three that failed", n)
	}
}

// The list a channel decided on does not change under it: the keeper puts a
// new version in place of the old one instead of writing into it, so the
// pointer one channel holds is the version it started with. That is what lets
// access read the certificate, the root permission and the user's name out of
// one version and not out of two.
func TestTheListOfAChannelDoesNotChangeUnderIt(t *testing.T) {
	w := newWorld(t)
	held := w.keep.List()
	if held == nil {
		t.Fatal("the keeper holds no list")
	}
	seq, users, nodes := held.Seq, len(held.Users), len(held.Nodes)
	if _, err := w.keep.Apply(context.Background(), chainOf(t, w.next(t))); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if w.keep.TrustSeq() != 2 {
		t.Fatalf("the keeper holds version %d after a second one was applied", w.keep.TrustSeq())
	}
	if held.Seq != seq || len(held.Users) != users || len(held.Nodes) != nodes {
		t.Errorf("the list handed out before is now seq %d with %d users and %d nodes, want %d/%d/%d",
			held.Seq, len(held.Users), len(held.Nodes), seq, users, nodes)
	}
	if w.keep.List() == held {
		t.Error("the keeper handed out the same list again, so a channel holding it would see the new version")
	}
}

// Readers of the list and the one writer run at the same moment: the keeper
// hands out versions and writes into none of them, so nothing a reader holds
// is written while it reads. The barrier before the loops is what gives the
// race detector the chance to see it; without one the writer would be through
// before the readers began.
func TestReadingTheListWhileANewVersionIsApplied(t *testing.T) {
	const (
		readers = 16
		rounds  = 200
	)
	w := newWorld(t)
	next := chainOf(t, w.next(t))
	seen := make([]uint64, readers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range rounds {
				l := w.keep.List()
				if l == nil {
					t.Error("the keeper handed out no list")
					return
				}
				// Read the way access reads it: the number first, then what
				// hangs off it.
				seen[i] = l.Seq
				if len(l.Users) == 0 || len(l.Nodes) == 0 {
					t.Errorf("the list of version %d has %d users and %d nodes", l.Seq, len(l.Users), len(l.Nodes))
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if _, err := w.keep.Apply(context.Background(), next); err != nil {
			t.Errorf("Apply: %v", err)
		}
	}()
	close(start)
	wg.Wait()
	for i, s := range seen {
		if s != 1 && s != 2 {
			t.Errorf("reader %d saw version %d, want the one before the write or the one after it", i, s)
		}
	}
}

// Many channels of one tunnel run side by side, and the trust list moves on
// while they do: every one of them gets its own answer, and the version a
// channel started on is none of the other's business. The waiting ones hold
// their place until the first timeout runs out, so all of them really are in
// flight at the same time and not one after the other — and the failing ones
// are what put the one piece of state this server shares between channels,
// the counter, under several goroutines at once.
func TestChannelsRunSideBySideWhileTheListMovesOn(t *testing.T) {
	const (
		waiting  = 16
		failing  = 16
		trustNum = waiting + failing + 1
	)
	w := newWorld(t)
	w.srv.first = 300 * time.Millisecond
	payload, _ := w.appOpen(t, nil)
	wrong, _ := w.appOpenFor(t, "ctl-2", nil)
	next := chainOf(t, w.next(t))
	ended := w.tunnel(t, func(a *apiSide) {
		for num := uint32(1); num <= waiting; num++ {
			a.send(wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: payload})
		}
		for num := uint32(waiting + 1); num <= waiting+failing; num++ {
			a.send(wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: wrong})
		}
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: trustNum, Payload: trustOpen(t)})
		var (
			out  []byte
			sent bool
		)
		closes := make(map[uint32]wire.Close, trustNum)
		for len(closes) < trustNum {
			f, err := a.next()
			if err != nil {
				t.Errorf("after %d of %d channels ended: %v", len(closes), trustNum, err)
				return
			}
			switch f.Type {
			case wire.TypeGrant:
				// The chain goes out once the trust channel has granted the
				// credit for it, and not before: nothing may be sent on
				// credit that was never given.
				if f.Channel == trustNum && !sent {
					a.data(trustNum, next)
					a.eof(trustNum)
					sent = true
				}
			case wire.TypeClose:
				cl, err := wire.ParseClose(f.Payload)
				if err != nil {
					t.Errorf("CLOSE of channel %d: %v", f.Channel, err)
					return
				}
				closes[f.Channel] = cl
				a.shut(f.Channel)
			case wire.TypeData:
				if f.Channel == trustNum {
					out = append(out, f.Payload...)
				}
			}
		}
		for num := uint32(1); num <= waiting; num++ {
			wantCode(t, closes[num], wire.CodeTimeout)
		}
		for num := uint32(waiting + 1); num <= waiting+failing; num++ {
			wantCode(t, closes[num], wire.CodeInternal)
		}
		if cl := closes[trustNum]; cl.Status != wire.StatusOK || cl.Code != "" {
			t.Errorf("the trust channel ended with %+v, want status ok", cl)
		}
		var got core.TrustResult
		if err := json.Unmarshal(out, &got); err != nil || got.Seq != 2 {
			t.Errorf("the trust channel answered %q (%v), want seq 2", out, err)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if seq := w.keep.TrustSeq(); seq != 2 {
		t.Errorf("the keeper holds version %d, want the one the trust channel verified", seq)
	}
	if n := w.srv.HandshakeFailures(); n != failing {
		t.Errorf("HandshakeFailures = %d, want the %d that failed side by side", n, failing)
	}
	if n := w.notes.n.Load(); n != 0 {
		t.Errorf("%d lines were written for channels a peer opened, want none", n)
	}
}
