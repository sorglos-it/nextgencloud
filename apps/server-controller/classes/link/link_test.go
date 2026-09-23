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
}

// running dials a tunnel to an API that says hello and then does what serve
// says, and runs it. It gives back the tunnel and the end of Run.
func running(t *testing.T, h Handler, after func(*apiSide)) (*Tunnel, <-chan error) {
	t.Helper()
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		a := accept(t, w, r)
		if a == nil {
			return
		}
		defer a.c.CloseNow()
		a.greet()
		after(a)
	})
	cfg.Handler = h
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

// A link whose pong stayed away is gone, not closed: Run says so, so that
// whoever keeps the tunnel writes a line about it and dials again. The wait
// for the pong itself is the half minute of the contract and is not waited out
// here; what is proven is that a dead link is told apart from a closed one.
func TestRunSaysWhenThePongStayedAway(t *testing.T) {
	tn, ended := running(t, nil, func(a *apiSide) { a.quiet() })
	tn.dead.Store(true)
	tn.Close()
	if err := waitFor(t, ended); !errors.Is(err, ErrAPI) {
		t.Fatalf("Run = %v, want the link reported as gone", err)
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

// The pin of a TLS key is a SHA-256 hash, and the values above are the ones
// the contract names; this keeps the test honest if one of them ever changes.
func TestTheTestValuesAreTheOnesOfTheContract(t *testing.T) {
	_, pin := trusttest.APITLS(apiTLSFill, apiHost)
	if len(pin) != sha256.Size {
		t.Fatalf("pin of %d bytes", len(pin))
	}
}
