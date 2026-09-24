package link

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The keys of the two ends. They are the ones trusttest.Genesis lists for
// api-1 and ctl-1, so a test that later wants a trust list finds the same
// values there.
const (
	apiFill    = 0xA1
	ctlFill    = 0xC1
	apiTLSFill = 0xE1
	otherFill  = 0xE2
	apiHost    = "api-1.test"
)

// hello is the hello of this controller in the tests.
func hello() tunnel.Hello {
	return tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract}
}

// apiSide plays the API at the other end: the answering side of the Noise
// handshake and every message after it. Everything of it runs in the goroutine
// of the test server, where t.Fatal would leave the test standing, so it
// reports with t.Error and gives up.
type apiSide struct {
	t  *testing.T
	c  *websocket.Conn
	tr *noisep.Transport
}

// serve starts a test API with the handler h and returns the configuration a
// controller dials it with: the address of the server, the Noise key of the
// API and the pin of its TLS key.
func serve(t *testing.T, h http.HandlerFunc) Config {
	t.Helper()
	apiKey, _ := trusttest.NodeKeys(apiFill)
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	cert, pin := trusttest.APITLS(apiTLSFill, apiHost)
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return Config{
		API:   config.APIRef{URL: "wss://" + srv.Listener.Addr().String() + "/tunnel", Key: apiKey.Public, TLS: pin},
		Noise: ctlKey,
		Hello: hello,
	}
}

// accept takes the WebSocket of a tunnel and answers the Noise handshake the
// way the API does. It returns nil once it has said what went wrong; the
// handler then has nothing left to do.
func accept(t *testing.T, w http.ResponseWriter, r *http.Request) *apiSide {
	return acceptPinging(t, w, r, nil)
}

// acceptPinging is accept with an eye on the pings of the controller: onPing
// sees every one of them and says whether the pong goes back. An API whose
// pong stays away is a peer that went quiet without the socket saying so - the
// one thing the ping of the tunnel is there for.
func acceptPinging(t *testing.T, w http.ResponseWriter, r *http.Request, onPing func() bool) *apiSide {
	apiKey, _ := trusttest.NodeKeys(apiFill)
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	opts := &websocket.AcceptOptions{Subprotocols: []string{tunnel.Subprotocol}}
	if onPing != nil {
		opts.OnPingReceived = func(context.Context, []byte) bool { return onPing() }
	}
	c, err := websocket.Accept(w, r, opts)
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
	if err != nil || typ != websocket.MessageBinary || len(msg1) != wire.HandshakeSize {
		t.Errorf("message 1 = %d bytes of type %v (%v)", len(msg1), typ, err)
		c.CloseNow()
		return nil
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		t.Errorf("message 1: %v", err)
		c.CloseNow()
		return nil
	}
	msg2, cs1, cs2, err := hs.WriteMessage(nil, nil)
	if err != nil || cs1 == nil || cs2 == nil {
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

// control sends one control message on channel 0. A message without a body
// carries no member b: an explicit null is none of this contract, as
// wire.ParseControl reads it.
func (a *apiSide) control(typ string, body any) {
	var raw json.RawMessage
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			a.t.Error(err)
			return
		}
		raw = b
	}
	p, err := json.Marshal(wire.Control{T: typ, B: raw})
	if err != nil {
		a.t.Error(err)
		return
	}
	a.send(wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: p})
}

// read reads one frame of the controller.
func (a *apiSide) read() (wire.Frame, error) {
	typ, msg, err := a.c.Read(context.Background())
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

// greet reads the hello of the controller and answers with the API's.
func (a *apiSide) greet() {
	f, err := a.read()
	if err != nil {
		a.t.Errorf("hello of the controller: %v", err)
		return
	}
	if f.Channel != wire.ControlChannel || f.Type != wire.TypeData {
		a.t.Errorf("the first message was %v on channel %d, want the hello", f.Type, f.Channel)
		return
	}
	c, err := wire.ParseControl(f.Payload)
	if err != nil || c.T != wire.CtlHello {
		a.t.Errorf("the first message was %q (%v), want the hello", c.T, err)
		return
	}
	if _, err := tunnel.ParseHello(c.B); err != nil {
		a.t.Errorf("hello of the controller: %v", err)
		return
	}
	a.control(wire.CtlHello, tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract, Node: "ctl-1", TrustSeq: 1})
}

// quiet reads until the other end is gone. It keeps the connection open while
// a test does its work at the controller's end and lets the handler return as
// soon as the controller is finished.
func (a *apiSide) quiet() {
	for {
		if _, _, err := a.c.Read(context.Background()); err != nil {
			return
		}
	}
}

// ending reads until the tunnel is over and gives back the status of the close
// message that reached this side, or -1 when none did. This is the one thing
// only the other end can say: on the controller's side an orderly end and a
// link that fell apart both come back out of Run, and whether the API can tell
// them apart hangs on this message alone.
func (a *apiSide) ending() websocket.StatusCode {
	for {
		if _, _, err := a.c.Read(context.Background()); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

// The tunnel is dialled over TLS that is checked against the pin, it names the
// controller's key in the address, it speaks Noise_KK as the initiator and the
// hello of the API is read before anything else may happen.
func TestDialCompletesHandshakeAndHello(t *testing.T) {
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	var key atomic.Value
	key.Store("")
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		key.Store(r.URL.Query().Get("key"))
		a := accept(t, w, r)
		if a == nil {
			return
		}
		defer a.c.CloseNow()
		a.greet()
		a.quiet()
	})
	tn, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer tn.Close()
	if got := tn.Peer(); got.Contract != tunnel.Contract || got.Node != "ctl-1" {
		t.Fatalf("peer hello = %+v", got)
	}
	if got := key.Load().(string); got != tunnel.Fingerprint(ctlKey.Public) {
		t.Errorf("key = %q, want the fingerprint of the controller's key", got)
	}
}

// Peer hands out a hello of its own: whoever changes what he was given does
// not change what the tunnel holds.
func TestPeerHandsOutNoListOfItsOwn(t *testing.T) {
	tn := &Tunnel{peer: tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract, Roles: []string{tunnel.RoleServices}}}
	got := tn.Peer()
	got.Roles[0] = tunnel.RoleDB
	if again := tn.Peer(); again.Roles[0] != tunnel.RoleServices {
		t.Error("Peer hands out the list the tunnel holds itself")
	}
}

// A pin that belongs to another key is no API of this installation. Whoever
// answers there learns nothing: the handshake never starts.
func TestDialRefusesWrongTLSPin(t *testing.T) {
	var asked atomic.Int64
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if a := accept(t, w, r); a != nil {
			a.c.CloseNow()
		}
	})
	_, other := trusttest.APITLS(otherFill, apiHost)
	cfg.API.TLS = other
	if _, err := Dial(context.Background(), cfg); err == nil {
		t.Fatal("Dial took a server whose TLS key is not the pinned one")
	}
	if asked.Load() != 0 {
		t.Errorf("the server was asked %d times although its key is another one", asked.Load())
	}
}

// A text message is no message of this contract, in the handshake as
// afterwards: the link ends at once and without an answer.
func TestDialRefusesTextMessage(t *testing.T) {
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{tunnel.Subprotocol}})
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		if _, _, err := c.Read(r.Context()); err != nil {
			t.Error(err)
			return
		}
		c.Write(r.Context(), websocket.MessageText, []byte("hello"))
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	})
	_, err := Dial(context.Background(), cfg)
	if !errors.Is(err, ErrLink) {
		t.Fatalf("Dial = %v, want the link ended", err)
	}
}

// Messages 1 and 2 carry an empty payload and are therefore exactly
// wire.HandshakeSize bytes. Anything else is no handshake of this contract.
func TestDialRefusesAHandshakeOfTheWrongSize(t *testing.T) {
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{tunnel.Subprotocol}})
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		if _, _, err := c.Read(r.Context()); err != nil {
			t.Error(err)
			return
		}
		c.Write(r.Context(), websocket.MessageBinary, make([]byte, wire.HandshakeSize+1))
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	})
	if _, err := Dial(context.Background(), cfg); !errors.Is(err, ErrLink) {
		t.Fatalf("Dial = %v, want the link ended", err)
	}
}

// A server that does not speak the subprotocol of the tunnel is not the other
// end of a tunnel.
func TestDialRefusesTheWrongSubprotocol(t *testing.T) {
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	})
	if _, err := Dial(context.Background(), cfg); !errors.Is(err, ErrLink) {
		t.Fatalf("Dial = %v, want the link ended", err)
	}
}

// What the controller cannot dial with is refused before anything goes out:
// the one-time values of a pairing are long spent here, but an address that is
// none, a key of the wrong length or a missing hello are answers this side
// gives itself.
func TestDialRefusesValuesItCannotUse(t *testing.T) {
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	apiKey, _ := trusttest.NodeKeys(apiFill)
	_, pin := trusttest.APITLS(apiTLSFill, apiHost)
	good := Config{
		API:   config.APIRef{URL: "wss://" + apiHost + ":7443/tunnel", Key: apiKey.Public, TLS: pin},
		Noise: ctlKey,
		Hello: hello,
	}
	bad := map[string]func(*Config){
		"no address":    func(c *Config) { c.API.URL = "" },
		"a web address": func(c *Config) { c.API.URL = "https://" + apiHost + ":7443" },
		"a path of its own": func(c *Config) {
			c.API.URL = "wss://" + apiHost + ":7443/tunnel/../pair"
		},
		"a short key":   func(c *Config) { c.API.Key = c.API.Key[:31] },
		"a short pin":   func(c *Config) { c.API.TLS = c.API.TLS[:16] },
		"no own key":    func(c *Config) { c.Noise = noise.DHKey{} },
		"no public key": func(c *Config) { c.Noise.Public = nil },
		"no hello":      func(c *Config) { c.Hello = nil },
		// The ping is a value of this package and never comes from outside,
		// but a ticker of a nanosecond on a program that runs as root is a
		// storm and not an interval: it is refused where every other value is.
		"a ping of no time":          func(c *Config) { c.Ping = time.Nanosecond },
		"a ping below the floor":     func(c *Config) { c.Ping = minPing - 1 },
		"a ping that runs backwards": func(c *Config) { c.Ping = -time.Second },
	}
	for name, spoil := range bad {
		cfg := good
		cfg.API.Key = bytes.Clone(good.API.Key)
		cfg.API.TLS = bytes.Clone(good.API.TLS)
		spoil(&cfg)
		if _, err := Dial(context.Background(), cfg); !errors.Is(err, ErrConfig) {
			t.Errorf("Dial with %s = %v, want it refused before the request", name, err)
		}
	}
	if err := good.check(); err != nil {
		t.Errorf("the values of a working tunnel are refused: %v", err)
	}
	// The floor leaves the tests their short ping: what it keeps out is a
	// ticker storm, not a tunnel that pings faster than the contract.
	fast := good
	fast.Ping = pingEvery
	if err := fast.check(); err != nil {
		t.Errorf("the ping of these tests (%v) is refused: %v", pingEvery, err)
	}
}

// running dials a tunnel to an API that says hello and then does what serve
// says, and runs it. It gives back the tunnel and the end of Run.
func running(t *testing.T, h Handler, after func(*apiSide)) (*Tunnel, <-chan error) {
	t.Helper()
	return runningPinging(t, h, nil, nil, after)
}

// runningPinging is running with an API that watches the pings of the
// controller (onPing says whether the pong goes back) and with set, which
// changes the configuration before the dial - the time between two pings above
// all, which is the half minute of the contract unless a test shortens it.
func runningPinging(t *testing.T, h Handler, onPing func() bool, set func(*Config), after func(*apiSide)) (*Tunnel, <-chan error) {
	t.Helper()
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		a := acceptPinging(t, w, r, onPing)
		if a == nil {
			return
		}
		defer a.c.CloseNow()
		a.greet()
		after(a)
	})
	cfg.Handler = h
	if set != nil {
		set(&cfg)
	}
	tn, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { tn.Close() })
	ended := make(chan error, 1)
	go func() { ended <- tn.Run(context.Background()) }()
	// Run has taken the tunnel for itself before the test does anything with
	// it: what a test sends afterwards is read, and a second Run meets the
	// first one and not the other way round.
	for range 1000 {
		if tn.ran.Load() {
			return tn, ended
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Run did not begin")
	return nil, nil
}

// waitFor takes the end of Run, but not for ever: a link that should have
// ended and does not would otherwise be the timeout of the whole run.
func waitFor(t *testing.T, ended <-chan error) error {
	t.Helper()
	select {
	case err := <-ended:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not end")
		return nil
	}
}

// A text message ends the running link as well.
func TestRunEndsOnATextMessage(t *testing.T) {
	_, ended := running(t, nil, func(a *apiSide) {
		a.c.Write(context.Background(), websocket.MessageText, []byte("{}"))
		a.quiet()
	})
	if err := waitFor(t, ended); !errors.Is(err, ErrLink) {
		t.Fatalf("Run = %v, want the link ended", err)
	}
}

// A message that does not decrypt is not from the other end of the handshake.
func TestRunEndsOnAMessageThatDoesNotDecrypt(t *testing.T) {
	_, ended := running(t, nil, func(a *apiSide) {
		a.c.Write(context.Background(), websocket.MessageBinary, bytes.Repeat([]byte{7}, 64))
		a.quiet()
	})
	if err := waitFor(t, ended); !errors.Is(err, ErrLink) {
		t.Fatalf("Run = %v, want the link ended", err)
	}
}

// A frame that does not decode ends it too, and a control message the API may
// not send on a tunnel is no better.
func TestRunEndsOnABrokenFrame(t *testing.T) {
	for name, send := range map[string]func(*apiSide){
		"a frame of an unknown type": func(a *apiSide) {
			a.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("x")})
			a.c.Write(context.Background(), websocket.MessageBinary, mustSeal(a, []byte{9, 0, 0, 0, 1}))
		},
		"a control message only the controller may send": func(a *apiSide) {
			a.control(wire.CtlStatus, tunnel.Status{DiskFree: 1})
		},
		"a second hello": func(a *apiSide) {
			a.control(wire.CtlHello, tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract})
		},
		// Frame.Encode refuses this one, so it goes out as the bytes it is:
		// channel 0 carries control messages and nothing else.
		"a frame that is no control message on channel 0": func(a *apiSide) {
			a.c.Write(context.Background(), websocket.MessageBinary,
				mustSeal(a, []byte{byte(wire.TypeGrant), 0, 0, 0, 0, 0, 0, 1, 0}))
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, ended := running(t, &keeper{}, func(a *apiSide) {
				send(a)
				a.quiet()
			})
			if err := waitFor(t, ended); !errors.Is(err, ErrLink) {
				t.Fatalf("Run = %v, want the link ended", err)
			}
		})
	}
}

// mustSeal encrypts bytes that are no frame, for the tests above.
func mustSeal(a *apiSide, plain []byte) []byte {
	msg, err := a.tr.Seal(plain)
	if err != nil {
		a.t.Error(err)
		return nil
	}
	return msg
}

// keeper keeps what it was handed. It copies the payload, as every handler
// must: it points into the buffer the next message is read into.
type keeper struct {
	mu  sync.Mutex
	got []wire.Frame
}

func (k *keeper) Frame(t *Tunnel, f wire.Frame) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.got = append(k.got, wire.Frame{Type: f.Type, Channel: f.Channel, Payload: bytes.Clone(f.Payload)})
	return nil
}

func (k *keeper) frames() []wire.Frame {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]wire.Frame(nil), k.got...)
}

// Everything on a channel goes to the handler, and channel 0 does not.
func TestRunHandsChannelsToTheHandler(t *testing.T) {
	k := &keeper{}
	open, err := json.Marshal(tunnel.Open{Open: []byte("{}"), Window: wire.MinWindow})
	if err != nil {
		t.Fatal(err)
	}
	_, ended := running(t, k, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 3, Payload: open})
		a.send(wire.Frame{Type: wire.TypeData, Channel: 3, Payload: []byte("hi")})
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	got := k.frames()
	if len(got) != 2 || got[0].Type != wire.TypeOpen || got[0].Channel != 3 ||
		got[1].Type != wire.TypeData || string(got[1].Payload) != "hi" {
		t.Fatalf("the handler was handed %+v", got)
	}
}

// Without a handler nothing here serves channels, so every channel the API
// opens is closed as not reachable - and the link stays.
func TestRunClosesChannelsWithoutAHandler(t *testing.T) {
	answered := make(chan wire.Close, 1)
	_, ended := running(t, nil, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeData, Channel: 5, Payload: []byte("x")})
		f, err := a.read()
		if err != nil {
			a.t.Errorf("read: %v", err)
			return
		}
		c, err := wire.ParseClose(f.Payload)
		if err != nil || f.Type != wire.TypeClose || f.Channel != 5 {
			a.t.Errorf("the controller answered %v on channel %d (%v)", f.Type, f.Channel, err)
			return
		}
		answered <- c
		a.quiet()
	})
	select {
	case c := <-answered:
		if c.Code != wire.CodeUnreachable {
			t.Errorf("close = %+v, want the channel refused as not reachable", c)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the controller never answered the channel")
	}
	select {
	case err := <-ended:
		t.Fatalf("Run ended with %v, want the link to stay", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// pingEvery is the time between two pings in the tests below, and the time the
// tunnel then waits for the pong. It is short enough for a test to sit it out
// twice and long enough for a pong to come back over the loopback of a loaded
// machine; the value of the contract (tunnel.PingInterval) holds everywhere
// else, see TestTheTestValuesAreTheOnesOfTheContract.
const pingEvery = 100 * time.Millisecond

// A tunnel that was closed ends as it was closed: Run comes back with nil.
// Closing writes a close message and waits for the answer of the other end, so
// the reading runs into an error on the way out either way - the end of the
// tunnel has to be settled before that, or every orderly end would be reported
// as a broken link.
func TestRunEndsWithoutAnErrorWhenTheTunnelIsClosed(t *testing.T) {
	t.Run("Close", func(t *testing.T) {
		tn, ended := running(t, nil, func(a *apiSide) { a.quiet() })
		tn.Close()
		if err := waitFor(t, ended); err != nil {
			t.Fatalf("Run = %v, want an orderly end", err)
		}
	})
	t.Run("the context of the caller", func(t *testing.T) {
		cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
			a := accept(t, w, r)
			if a == nil {
				return
			}
			defer a.c.CloseNow()
			a.greet()
			a.quiet()
		})
		tn, err := Dial(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		t.Cleanup(func() { tn.Close() })
		ctx, cancel := context.WithCancel(context.Background())
		ended := make(chan error, 1)
		go func() { ended <- tn.Run(ctx) }()
		for range 1000 {
			if tn.ran.Load() {
				break
			}
			time.Sleep(time.Millisecond)
		}
		cancel()
		if err := waitFor(t, ended); err != nil {
			t.Fatalf("Run = %v, want an orderly end", err)
		}
	})
}

// echo answers every frame on the channel it came on. A test that wants to
// close a tunnel while Run is reading waits for that answer at the API first:
// once it is there the handler is long done and the reading is back at the
// socket, which is the only place where closing in the wrong order loses the
// close message. Without that wait the case that matters runs about three
// times in ten.
type echo struct{}

func (echo) Frame(t *Tunnel, f wire.Frame) error {
	return t.Send(wire.Frame{Type: wire.TypeData, Channel: f.Channel, Payload: []byte("ok")})
}

// What the API sees at the end of a tunnel. There are four ways out of Run and
// no more - closed from outside, the goodbye of the API, a broken contract and
// a pong that stayed away - and each of them has to reach the other end as the
// close message it is. Only that message lets the API tell a controller that
// shut down from one that fell over, and this side cannot see it at all: what
// Run gives back here is measured together with it, so the two can no longer
// drift apart.
func TestTheAPISeesHowTheTunnelEnded(t *testing.T) {
	for _, c := range []struct {
		name    string
		onPing  func() bool
		api     func(*apiSide)
		close   bool
		wantRun error
		wantSaw websocket.StatusCode
	}{{
		name:    "closed from outside while Run reads",
		close:   true,
		wantRun: nil,
		wantSaw: websocket.StatusNormalClosure,
	}, {
		name:    "the goodbye of the API",
		api:     func(a *apiSide) { a.control(wire.CtlBye, nil) },
		wantRun: nil,
		wantSaw: websocket.StatusNormalClosure,
	}, {
		name: "the API broke the contract",
		api: func(a *apiSide) {
			a.c.Write(context.Background(), websocket.MessageText, []byte("{}"))
		},
		wantRun: ErrLink,
		wantSaw: websocket.StatusProtocolError,
	}, {
		name:    "the pong stayed away",
		onPing:  func() bool { return false },
		wantRun: ErrAPI,
		wantSaw: websocket.StatusNormalClosure,
	}} {
		t.Run(c.name, func(t *testing.T) {
			saw := make(chan websocket.StatusCode, 1)
			ready := make(chan struct{}, 1)
			cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
				a := acceptPinging(t, w, r, c.onPing)
				if a == nil {
					return
				}
				defer a.c.CloseNow()
				a.greet()
				// One frame on a channel of its own and the answer of the
				// handler back: the tunnel has carried something both ways,
				// so the reading of it is at the socket and nowhere else.
				a.send(wire.Frame{Type: wire.TypeData, Channel: 3, Payload: []byte("x")})
				if _, err := a.read(); err != nil {
					a.t.Errorf("the controller never answered the channel: %v", err)
					return
				}
				ready <- struct{}{}
				if c.api != nil {
					c.api(a)
				}
				saw <- a.ending()
			})
			cfg.Handler = echo{}
			cfg.Ping = pingEvery
			tn, err := Dial(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			t.Cleanup(func() { tn.Close() })
			ended := make(chan error, 1)
			go func() { ended <- tn.Run(context.Background()) }()
			if c.close {
				select {
				case <-ready:
				case <-time.After(30 * time.Second):
					t.Fatal("the tunnel never carried a frame both ways")
				}
				tn.Close()
			}
			if err := waitFor(t, ended); !errors.Is(err, c.wantRun) {
				t.Fatalf("Run = %v, want %v", err, c.wantRun)
			}
			select {
			case got := <-saw:
				if got != c.wantSaw {
					t.Fatalf("the API saw the tunnel end with %v, want %v", got, c.wantSaw)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("the API never came to the end of the tunnel")
			}
		})
	}
}

// The ping goes out again and again, one every interval: a tunnel nobody
// notices is gone would take every channel on it with it.
func TestThePingGoesOutAgainAndAgain(t *testing.T) {
	pinged := make(chan time.Time, 8)
	tn, ended := runningPinging(t, nil,
		func() bool {
			select {
			case pinged <- time.Now():
			default:
			}
			return true // the API answers as it should
		},
		func(c *Config) { c.Ping = pingEvery },
		func(a *apiSide) { a.quiet() })
	var first, last time.Time
	for i := range 3 {
		select {
		case at := <-pinged:
			if i == 0 {
				first = at
			}
			last = at
		case <-time.After(30 * time.Second):
			t.Fatalf("the API saw %d pings, want one every %v", i, pingEvery)
		}
	}
	// Three pings lie far more than one interval apart: what is proven is a
	// ping every interval and not a burst of them at the start. The bound is
	// below the two intervals they take, because a ticker of the runtime is
	// allowed to be a hair early and a test may not hang on that hair.
	if d := last.Sub(first); d < 3*pingEvery/2 {
		t.Errorf("three pings within %v, want one every %v", d, pingEvery)
	}
	if tn.dead.Load() {
		t.Error("the link counts as dead although every pong came back")
	}
	select {
	case err := <-ended:
		t.Fatalf("Run ended with %v, want the link to stay while the API answers", err)
	default:
	}
	tn.Close()
	waitFor(t, ended)
}

// A link whose pong stayed away is gone, not closed: Run says so, so that
// whoever keeps the tunnel writes a line about it and dials again. The API
// here reads everything and keeps only the pong - the socket says nothing, and
// the ping is the only thing that notices.
func TestRunSaysWhenThePongStayedAway(t *testing.T) {
	tn, ended := runningPinging(t, nil,
		func() bool { return false }, // the pong stays away
		func(c *Config) { c.Ping = pingEvery },
		func(a *apiSide) { a.quiet() })
	if err := waitFor(t, ended); !errors.Is(err, ErrAPI) {
		t.Fatalf("Run = %v, want the link reported as gone", err)
	}
	if !tn.dead.Load() {
		t.Error("the link ended without the missing pong being noticed")
	}
}

// A tunnel that is closed while the ping is still waiting for its pong ends as
// it was closed. The pong that never comes is this side shutting the link
// down, not a peer that went quiet, and Run must not report the one as the
// other. The API here keeps every pong, so the ping is sitting in its wait
// when the close arrives.
func TestClosingWhileThePingWaitsIsStillAnOrderlyEnd(t *testing.T) {
	// Half a second, so that the close falls inside the wait for the pong and
	// not after it: the wait is one interval long.
	const slowPing = 500 * time.Millisecond
	pinged := make(chan struct{}, 1)
	tn, ended := runningPinging(t, nil,
		func() bool {
			select {
			case pinged <- struct{}{}:
			default:
			}
			return false // the pong stays away, so the ping waits
		},
		func(c *Config) { c.Ping = slowPing },
		func(a *apiSide) { a.quiet() })
	select {
	case <-pinged:
	case <-time.After(30 * time.Second):
		t.Fatal("the API never saw a ping")
	}
	tn.Close()
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end", err)
	}
	if tn.dead.Load() {
		t.Error("the link counts as gone although it was closed here")
	}
}

// waiter holds the reading of the tunnel for a fixed time on every frame it is
// given, and counts the frames.
type waiter struct {
	hold time.Duration
	seen atomic.Int64
}

func (w *waiter) Frame(*Tunnel, wire.Frame) error {
	w.seen.Add(1)
	time.Sleep(w.hold)
	return nil
}

// What the doc of Handler promises, and what it does not. The pong is picked
// up by the very goroutine the handler holds, and it waits behind every frame
// that came in before it: one call longer than two ping intervals costs the
// tunnel for certain, and so does a queue of calls that are each far shorter
// than one. The length of a single call is not what decides - the time the
// reading is held between a ping and its deadline is.
func TestAHandlerThatHoldsTheReadingCostsTheTunnel(t *testing.T) {
	t.Run("one call longer than two intervals", func(t *testing.T) {
		var pings atomic.Int64
		h := &waiter{hold: 5 * pingEvery / 2}
		tn, ended := runningPinging(t, h,
			func() bool { pings.Add(1); return true }, // the API answers as it should
			func(c *Config) { c.Ping = pingEvery },
			func(a *apiSide) {
				a.send(wire.Frame{Type: wire.TypeData, Channel: 3, Payload: []byte("x")})
				a.quiet()
			})
		if err := waitFor(t, ended); !errors.Is(err, ErrAPI) {
			t.Fatalf("Run = %v, want the link reported as gone", err)
		}
		if !tn.dead.Load() {
			t.Error("the tunnel ended without the missing pong being noticed")
		}
		if pings.Load() == 0 {
			t.Error("the API never saw a ping, so the handler is not what cost the tunnel")
		}
		if h.seen.Load() != 1 {
			t.Errorf("the handler was called %d times, want the one frame", h.seen.Load())
		}
	})
	t.Run("sixteen calls of a quarter interval", func(t *testing.T) {
		// No single call comes near one interval, but the sixteen of them
		// hold the reading four intervals on end - and the pong waits behind
		// the frames that came in before it, so it is just as late.
		const frames = 16
		var pings atomic.Int64
		h := &waiter{hold: pingEvery / 4}
		if frames*h.hold <= 2*pingEvery {
			t.Fatalf("%d calls of %v do not add up past two intervals of %v", frames, h.hold, pingEvery)
		}
		tn, ended := runningPinging(t, h,
			func() bool { pings.Add(1); return true }, // the API answers as it should
			func(c *Config) { c.Ping = pingEvery },
			func(a *apiSide) {
				for range frames {
					a.send(wire.Frame{Type: wire.TypeData, Channel: 3, Payload: []byte("x")})
				}
				a.quiet()
			})
		if err := waitFor(t, ended); !errors.Is(err, ErrAPI) {
			t.Fatalf("Run = %v, want the link reported as gone", err)
		}
		if !tn.dead.Load() {
			t.Error("the tunnel ended without the missing pong being noticed")
		}
		if pings.Load() == 0 {
			t.Error("the API never saw a ping, so the handler is not what cost the tunnel")
		}
		// Four calls are a whole ping interval of held reading, so this is the
		// claim itself and not merely "the handler ran": a tunnel that came
		// down after one or two short calls came down for some other reason,
		// and would prove nothing about calls that add up. Measured are eight.
		if got := h.seen.Load(); got < frames/4 {
			t.Errorf("the handler was called %d times, want a whole interval of it held by short calls", got)
		}
	})
}

// stuffFrames bounds what the writer below puts into the socket: the tunnel
// carries at most one wire.MaxFrame per message, so this is about sixteen
// megabytes. A loopback that swallows all of it without ever making a write
// wait is a machine this test cannot set up its case on, and it says so
// instead of writing for ever.
const stuffFrames = 256

// stuffer is the handler the promise of Handler asks for, word for word: it
// gives the reading back at once and answers from a goroutine of its own with
// Tunnel.Send. What it writes is more than the socket takes while the API
// reads nothing, so one of those writes stands in the socket - and a write of
// this side standing in the socket is what takes the ping the lock it needs
// for its own frame.
type stuffer struct {
	start sync.Once
	stop  chan struct{}
	done  chan struct{}
	sent  atomic.Int64
}

func newStuffer() *stuffer {
	return &stuffer{stop: make(chan struct{}), done: make(chan struct{})}
}

func (s *stuffer) Frame(t *Tunnel, f wire.Frame) error {
	s.start.Do(func() {
		go func() {
			defer close(s.done)
			p := make([]byte, wire.MaxPayload)
			for range stuffFrames {
				select {
				case <-s.stop:
					return
				default:
				}
				if err := t.Send(wire.Frame{Type: wire.TypeData, Channel: f.Channel, Payload: p}); err != nil {
					return
				}
				s.sent.Add(1)
			}
		}()
	})
	return nil // the reading is given back at once, as the promise says
}

// congested dials a tunnel whose API says hello, sends one frame and then
// reads nothing at all until drain is closed, and whose handler fills the
// socket from a goroutine of its own. pings counts what the API sees.
func congested(t *testing.T, every time.Duration, drain <-chan struct{}, pings *atomic.Int64) (*Tunnel, <-chan error, *stuffer) {
	t.Helper()
	s := newStuffer()
	t.Cleanup(func() { close(s.stop) })
	tn, ended := runningPinging(t, s,
		func() bool { pings.Add(1); return true }, // the API answers every ping it sees
		func(c *Config) { c.Ping = every },
		func(a *apiSide) {
			// The one frame starts the writer. From here the API reads
			// nothing, so the socket fills up and a write of this side comes
			// to a stand in it.
			a.send(wire.Frame{Type: wire.TypeData, Channel: 3, Payload: []byte("x")})
			<-drain
			a.quiet()
		})
	return tn, ended, s
}

// waitWriting waits until a write of this side stands in the socket. That is
// the case both tests below are about, and a machine whose loopback takes
// sixteen megabytes without ever making a writer wait would prove nothing.
func waitWriting(t *testing.T, tn *Tunnel, s *stuffer) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		if tn.writing.Load() > 0 {
			return
		}
		select {
		case <-s.done:
			t.Fatalf("all %d messages went into the socket without one of them waiting: this machine buffers more than the test can fill", s.sent.Load())
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no write of this side ever came to a stand in the socket although the API reads nothing")
}

// A ping that lost to a write of this side says nothing about the API. The
// ping frame goes through the same lock of the library as every message
// (writeFrameMu, vendor .../write.go:289), and the library gives that lock up
// after five seconds for a control frame (write.go:276-277) while a write of
// this side may stand in it up to WriteTimeout. Without that difference a
// handler that does exactly what the promise of Handler asks - give the
// reading back at once, answer from a goroutine of its own with Tunnel.Send -
// loses the tunnel to a pong that was never even asked for, and the API never
// sees a single ping of it.
func TestAPingLostToOurOwnWriteDoesNotKillTheTunnel(t *testing.T) {
	t.Run("the twelve seconds of the report", func(t *testing.T) {
		// The measured case at its own clock: the tick falls at twelve
		// seconds, the library gives up on the frame five seconds later, and
		// at seventeen seconds the tunnel used to count as gone. The interval
		// has to be longer than those five seconds, or the ping runs into its
		// own deadline first and the case is a different one - that one is
		// the second test below.
		const every = 12 * time.Second
		drain := make(chan struct{})
		var pings atomic.Int64
		tn, ended, s := congested(t, every, drain, &pings)
		waitWriting(t, tn, s)
		began := time.Now()
		// A second and a half past the moment the old code gave up.
		for deadline := began.Add(every + 5*time.Second + 3*time.Second/2); time.Now().Before(deadline); {
			if tn.dead.Load() {
				t.Fatalf("the tunnel counted as gone after %v, and the API saw %d pings", time.Since(began).Round(time.Millisecond), pings.Load())
			}
			select {
			case err := <-ended:
				t.Fatalf("Run = %v after %v, want the tunnel still standing", err, time.Since(began).Round(time.Millisecond))
			default:
			}
			if tn.writing.Load() == 0 {
				t.Fatalf("no write of this side stands in the socket any more after %v: the case of this test is gone", time.Since(began).Round(time.Millisecond))
			}
			time.Sleep(50 * time.Millisecond)
		}
		// From here the API reads again, so the write that stood in the
		// socket gets out and the tunnel can be closed the orderly way.
		close(drain)
		if err := waitFor(t, endedAfter(t, tn, ended)); err != nil {
			t.Fatalf("Run = %v, want an orderly end", err)
		}
	})
	t.Run("one interval, in a fraction of a second", func(t *testing.T) {
		// The same rule at an interval a test can sit out. Here the ping runs
		// into its own deadline and not into the five seconds of the library,
		// and the answer has to be the same one. Afterwards the API reads
		// again, and the next ping has to reach it: a ping skipped over a
		// write of ours may not cost the tunnel its heartbeat for good.
		const every = 300 * time.Millisecond
		drain := make(chan struct{})
		var pings atomic.Int64
		tn, ended, s := congested(t, every, drain, &pings)
		waitWriting(t, tn, s)
		began := time.Now()
		// Three intervals: two pings ran into the lock and gave up there.
		for deadline := began.Add(3 * every); time.Now().Before(deadline); {
			if tn.dead.Load() {
				t.Fatalf("the tunnel counted as gone after %v, and the API saw %d pings", time.Since(began).Round(time.Millisecond), pings.Load())
			}
			select {
			case err := <-ended:
				t.Fatalf("Run = %v after %v, want the tunnel still standing", err, time.Since(began).Round(time.Millisecond))
			default:
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(drain)
		for deadline := time.Now().Add(30 * time.Second); pings.Load() == 0; {
			if !time.Now().Before(deadline) {
				t.Fatal("the API saw no ping although it reads again: a skipped ping cost the tunnel its heartbeat")
			}
			if tn.dead.Load() {
				t.Fatal("the tunnel counted as gone although the API answers every ping it sees")
			}
			select {
			case err := <-ended:
				t.Fatalf("Run = %v, want the tunnel still standing", err)
			default:
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := waitFor(t, endedAfter(t, tn, ended)); err != nil {
			t.Fatalf("Run = %v, want an orderly end", err)
		}
	})
}

// endedAfter closes the tunnel and hands the end of Run on, so a test that
// wants the orderly end reads it in one line.
func endedAfter(t *testing.T, tn *Tunnel, ended <-chan error) <-chan error {
	t.Helper()
	tn.Close()
	return ended
}

// Send carries channels and not control messages: channel 0 goes through
// Control, which sends only what a controller may send on a tunnel. A handler
// that wrote its own control message here would have the API end the link.
func TestSendRefusesChannelZero(t *testing.T) {
	tn, ended := running(t, nil, func(a *apiSide) { a.quiet() })
	p, err := json.Marshal(wire.Control{T: wire.CtlTrust})
	if err != nil {
		t.Fatal(err)
	}
	if err := tn.Send(wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: p}); !errors.Is(err, ErrConfig) {
		t.Errorf("Send on channel 0 = %v, want it refused", err)
	}
	tn.Close()
	waitFor(t, ended)
}

// A body that is nothing stays away: an explicit "b":null is no message of
// this contract, and the API would end the tunnel over it. A typed nil pointer
// is not nil once it is in an interface, so the value and not the interface
// decides.
func TestControlSendsNoNullBody(t *testing.T) {
	if _, err := wire.ParseControl([]byte(`{"t":"status","b":null}`)); err == nil {
		t.Fatal("a written-out null is taken by the contract, so this test guards nothing")
	}
	sent := make(chan []byte, 1)
	tn, ended := running(t, nil, func(a *apiSide) {
		f, err := a.read()
		if err != nil {
			a.t.Errorf("read: %v", err)
			return
		}
		sent <- bytes.Clone(f.Payload)
		a.quiet()
	})
	var nothing *tunnel.Status // a typed nil: not nil in an interface
	if err := tn.Control(wire.CtlStatus, nothing); err != nil {
		t.Fatalf("Control: %v", err)
	}
	select {
	case p := <-sent:
		if bytes.Contains(p, []byte("null")) {
			t.Errorf("the controller sent %s", p)
		}
		if c, err := wire.ParseControl(p); err != nil || c.T != wire.CtlStatus {
			t.Errorf("the API cannot take %s (%v)", p, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the controller never sent the message")
	}
	tn.Close()
	waitFor(t, ended)
}

// The answer to a channel nothing here serves is built once, at start. Whoever
// lets that fail sends an empty CLOSE payload, which Frame.Encode refuses - so
// the tunnel would end where a channel should have.
func TestTheAnswerToAChannelNobodyServesIsOne(t *testing.T) {
	c, err := wire.ParseClose(closeUnreachable)
	if err != nil {
		t.Fatalf("the answer to an unserved channel is no CLOSE payload: %v", err)
	}
	if c.Code != wire.CodeUnreachable {
		t.Errorf("close = %+v, want the code of a channel nothing here serves", c)
	}
	if _, err := (wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: closeUnreachable}).Encode(); err != nil {
		t.Errorf("the answer does not go into a frame: %v", err)
	}
}

// One reader at a time: two would take the messages of one Noise session apart
// between them, and the second must not end the tunnel of the first.
func TestRunRunsOnce(t *testing.T) {
	tn, ended := running(t, nil, func(a *apiSide) { a.quiet() })
	if err := tn.Run(context.Background()); !errors.Is(err, ErrConfig) {
		t.Fatalf("the second Run = %v, want it refused", err)
	}
	if err := tn.Control(wire.CtlStatus, tunnel.Status{DiskFree: 1}); err != nil {
		t.Errorf("the second Run ended the tunnel of the first: %v", err)
	}
	tn.Close()
	waitFor(t, ended)
}

// Control writes on channel 0, and many writers at once stay one message
// after the other: the counter of the Noise session takes no concurrency, so
// a message out of order would not decrypt at the other end.
func TestControlWritesOnChannelZeroOneAfterTheOther(t *testing.T) {
	const many = 16
	seen := make(chan int, 1)
	tn, ended := running(t, nil, func(a *apiSide) {
		n := 0
		for {
			f, err := a.read()
			if err != nil {
				break
			}
			c, err := wire.ParseControl(f.Payload)
			if err != nil || f.Channel != wire.ControlChannel || c.T != wire.CtlStatus {
				a.t.Errorf("message %d was %v on channel %d (%v)", n, c.T, f.Channel, err)
				break
			}
			if _, err := tunnel.ParseStatus(c.B); err != nil {
				a.t.Errorf("status %d: %v", n, err)
				break
			}
			if n++; n == many {
				break
			}
		}
		seen <- n
		a.quiet()
	})
	var wg sync.WaitGroup
	for i := range many {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tn.Control(wire.CtlStatus, tunnel.Status{DiskFree: int64(i)}); err != nil {
				t.Errorf("Control: %v", err)
			}
		}()
	}
	wg.Wait()
	select {
	case n := <-seen:
		if n != many {
			t.Fatalf("the API read %d of %d messages", n, many)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the API never read the messages")
	}
	tn.Close()
	waitFor(t, ended)
}

// Control sends nothing the API would have to end the link over: only what a
// controller may send on a tunnel goes out.
func TestControlRefusesWhatTheControllerMayNotSend(t *testing.T) {
	tn, ended := running(t, nil, func(a *apiSide) { a.quiet() })
	if err := tn.Control(wire.CtlTrust, nil); !errors.Is(err, ErrConfig) {
		t.Errorf("Control(trust) = %v, want it refused", err)
	}
	if err := tn.Control("weiter-so", nil); !errors.Is(err, ErrConfig) {
		t.Errorf("Control of an unknown type = %v, want it refused", err)
	}
	tn.Close()
	waitFor(t, ended)
}

// Keep dials again and again, and waits longer each time: one second, then
// two, each with up to a fifth on top so that seven controllers do not all
// come back in the same second.
func TestKeepWaitsLongerEachTime(t *testing.T) {
	var tries atomic.Int64
	served := make(chan struct{})
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if tries.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		a := accept(t, w, r)
		if a == nil {
			return
		}
		defer a.c.CloseNow()
		a.greet()
		close(served)
		a.quiet()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var paused []time.Duration
	cfg.Sleep = func(d time.Duration) {
		mu.Lock()
		paused = append(paused, d)
		mu.Unlock()
	}
	done := make(chan struct{})
	go func() { defer close(done); Keep(ctx, cfg) }()
	select {
	case <-served:
	case <-time.After(30 * time.Second):
		t.Fatal("Keep never got through")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Keep did not end with its context")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paused) != 2 {
		t.Fatalf("Keep waited %v, want one pause after each of the two refusals", paused)
	}
	for i, want := range []time.Duration{time.Second, 2 * time.Second} {
		if paused[i] < want || paused[i] >= want+want/5 {
			t.Errorf("pause %d = %v, want %v with up to a fifth on top", i+1, paused[i], want)
		}
	}
	if tries.Load() != 3 {
		t.Errorf("Keep dialled %d times, want three", tries.Load())
	}
}

// Only a tunnel that really stood begins again at the shortest pause. An API
// that plays TLS, Noise and hello and then says goodbye at once would
// otherwise hold this controller at about a second per attempt for as long as
// it likes - the growing pause is the only bound there is on that way.
func TestOnlyATunnelThatStoodResetsThePause(t *testing.T) {
	var clock, rounds atomic.Int64
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		a := accept(t, w, r)
		if a == nil {
			return
		}
		defer a.c.CloseNow()
		a.greet()
		// The hello of the controller is read by now, so Dial has taken its
		// own reading of the clock: the fourth tunnel stood a ping interval,
		// the three before it no time at all.
		if rounds.Add(1) == 4 {
			clock.Add(tunnel.PingInterval)
		}
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	cfg.Now = clock.Load
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var paused []time.Duration
	cfg.Sleep = func(d time.Duration) {
		mu.Lock()
		paused = append(paused, d)
		n := len(paused)
		mu.Unlock()
		if n == 4 {
			cancel()
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); Keep(ctx, cfg) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Keep did not end with its context")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paused) != 4 {
		t.Fatalf("Keep waited %v, want a pause after each of the four tunnels", paused)
	}
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Second} {
		if paused[i] < want || paused[i] >= want+want/5 {
			t.Errorf("pause %d = %v, want %v with up to a fifth on top", i+1, paused[i], want)
		}
	}
}

// The pause doubles up to tunnel.ReconnectMax and stays there: an API that is
// away for an hour is asked once a minute, not once a second.
func TestThePauseDoublesUpToTheLongestOne(t *testing.T) {
	d := tunnel.ReconnectMin * time.Second
	for _, want := range []time.Duration{2, 4, 8, 16, 32, 60, 60, 60} {
		if d = grow(d); d != want*time.Second {
			t.Fatalf("pause = %v, want %v", d, want*time.Second)
		}
	}
	if tunnel.ReconnectMax*time.Second != 60*time.Second {
		t.Fatalf("the longest pause of the contract is %d seconds", tunnel.ReconnectMax)
	}
}

// Every pause is its own: up to a fifth on top, so that seven controllers do
// not all come back in the same second after an API was restarted.
func TestJitterSpreadsThePause(t *testing.T) {
	d := tunnel.ReconnectMax * time.Second
	same := 0
	for range 50 {
		got := jitter(d)
		if got < d || got >= d+d/5 {
			t.Fatalf("pause = %v, want %v with up to a fifth on top", got, d)
		}
		if got == d {
			same++
		}
	}
	if same == 50 {
		t.Error("every pause came back the same, want them spread")
	}
}

// frames are the messages an API may send on a tunnel, for the two tests
// below: the seeds of the fuzz target are these bytes.
func frames(t testing.TB) [][]byte {
	t.Helper()
	open, err := json.Marshal(tunnel.Open{Open: []byte("{}"), Window: wire.MinWindow})
	if err != nil {
		t.Fatal(err)
	}
	hi, err := json.Marshal(wire.Control{T: wire.CtlHello})
	if err != nil {
		t.Fatal(err)
	}
	bye, err := json.Marshal(wire.Control{T: wire.CtlBye})
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for _, f := range []wire.Frame{
		{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: hi},
		{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: bye},
		{Type: wire.TypeOpen, Channel: 1, Payload: open},
		{Type: wire.TypeData, Channel: 2, Payload: []byte("x")},
		{Type: wire.TypeEOF, Channel: 3},
		{Type: wire.TypeGrant, Channel: 4, Payload: wire.Grant(wire.MinWindow, nil)},
		{Type: wire.TypeClose, Channel: 5, Payload: []byte(`{"status":"ok"}`)},
	} {
		b, err := f.Encode()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

// Every message the contract allows on a tunnel is taken, and taken as what it
// is.
func TestTakeMessageTakesEveryKindOfMessage(t *testing.T) {
	for i, b := range frames(t) {
		f, c, err := takeMessage(b)
		if err != nil {
			t.Fatalf("message %d was refused: %v", i, err)
		}
		if f.Channel == wire.ControlChannel && c.T == "" {
			t.Errorf("message %d on channel 0 came back without a control message", i)
		}
		if f.Channel != wire.ControlChannel && c.T != "" {
			t.Errorf("message %d on channel %d came back as control message %q", i, f.Channel, c.T)
		}
	}
	for name, b := range map[string][]byte{
		"nothing":                      {},
		"a header without a type":      {0, 0, 0, 0, 1},
		"an OPEN on channel 0":         {byte(wire.TypeOpen), 0, 0, 0, 0, '{', '}'},
		"a GRANT on channel 0":         {byte(wire.TypeGrant), 0, 0, 0, 0, 0, 0, 1, 0},
		"a control message of its own": append([]byte{byte(wire.TypeData), 0, 0, 0, 0}, []byte(`{"t":"weiter-so"}`)...),
		"a status from the API":        append([]byte{byte(wire.TypeData), 0, 0, 0, 0}, []byte(`{"t":"status","b":{"diskFree":0}}`)...),
	} {
		if _, _, err := takeMessage(b); !errors.Is(err, ErrLink) {
			t.Errorf("%s was taken, want the link ended", name)
		}
	}
}

// fuzzing reports whether this is a fuzzing run (go test -fuzz). Then the
// coordinator hands every input to a worker process of its own and runs none
// itself, so what this process counted says nothing about the run.
func fuzzing() bool {
	f := flag.Lookup("test.fuzz")
	return f != nil && f.Value.String() != ""
}

// FuzzTakeMessage throws bytes at the only step that reads what the API sent.
// What it takes has to be exactly one frame again, byte for byte, and on
// channel 0 exactly one control message the API may send on a tunnel.
func FuzzTakeMessage(f *testing.F) {
	for _, b := range frames(f) {
		f.Add(b)
	}
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0}, wire.HeaderSize))
	var taken atomic.Int64
	f.Cleanup(func() {
		// A target that takes nothing proves nothing: the checks below would
		// never run, and a green run would say the opposite. In a fuzzing run
		// the inputs go to worker processes of their own, so the count of this
		// process says nothing there.
		if !fuzzing() && taken.Load() < int64(len(frames(f))) {
			f.Errorf("only %d messages were taken, want every kind of message of the contract through", taken.Load())
		}
	})
	f.Fuzz(func(t *testing.T, b []byte) {
		fr, c, err := takeMessage(b)
		if err != nil {
			if !errors.Is(err, ErrLink) {
				t.Fatalf("takeMessage refused with %v, want the one error of a broken link", err)
			}
			return
		}
		taken.Add(1)
		again, err := fr.Encode()
		if err != nil || !bytes.Equal(again, b) {
			t.Fatalf("what was taken is not the message that came in (%v)", err)
		}
		if fr.Channel == wire.ControlChannel {
			if fr.Type != wire.TypeData {
				t.Fatalf("frame %v on channel 0 was taken", fr.Type)
			}
			if !wire.ControlAllowed(wire.LinkTunnel, true, c.T) {
				t.Fatalf("control message %q was taken, which the API may not send on a tunnel", c.T)
			}
			return
		}
		if c.T != "" {
			t.Fatalf("frame on channel %d came back as control message %q", fr.Channel, c.T)
		}
	})
}

// Everything these tests work with is what the contract says, not a value of
// their own: whoever changes one of them in shared/ has this test in front of
// him and not a run that goes green beside the contract.
func TestTheTestValuesAreTheOnesOfTheContract(t *testing.T) {
	_, pin := trusttest.APITLS(apiTLSFill, apiHost)
	if len(pin) != sha256.Size {
		t.Errorf("pin of %d bytes, want the SHA-256 the contract pins with", len(pin))
	}
	apiKey, _ := trusttest.NodeKeys(apiFill)
	ctlKey, _ := trusttest.NodeKeys(ctlFill)
	if len(apiKey.Public) != 32 || len(ctlKey.Public) != 32 || len(ctlKey.Private) != 32 {
		t.Error("the keys of these tests are not the 32 bytes of the contract")
	}
	// The hello of these tests is one the other end takes, and it names the
	// contract this package speaks.
	h, err := json.Marshal(hello())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := tunnel.ParseHello(h); err != nil || got.Contract != tunnel.Contract {
		t.Errorf("the hello of these tests is %s (%v)", h, err)
	}
	// The API of these tests reads exactly as much as the link lets in: one
	// frame and the tag of its Noise message, which is the longest message
	// Noise carries - no byte of slack on either side (accept, Dial).
	if wire.MaxFrame+noisep.TagSize != noisep.MaxMessage {
		t.Errorf("a frame and its tag are %d bytes, the longest Noise message is %d",
			wire.MaxFrame+noisep.TagSize, noisep.MaxMessage)
	}
	// The tests ping faster than the contract; everywhere else the half minute
	// of the contract holds, and the pauses of Keep are its seconds.
	if pingEvery >= tunnel.PingInterval*time.Second {
		t.Errorf("these tests ping every %v, the contract says every %d seconds", pingEvery, tunnel.PingInterval)
	}
	if tunnel.ReconnectMin != 1 || tunnel.ReconnectMax != 60 {
		t.Errorf("the pauses of the contract are %d and %d seconds, the tests count on 1 and 60",
			tunnel.ReconnectMin, tunnel.ReconnectMax)
	}
}
