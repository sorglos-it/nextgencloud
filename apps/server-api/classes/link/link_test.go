package link

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

// testClient dials with SO_LINGER 0, so closing sends a RST and leaves no
// socket in TIME_WAIT. FuzzLinkMessage opens one link per input; without this
// the machine ran out of ephemeral ports after about eight seconds of fuzzing,
// and the failing input was the dial, not the link.
var testClient = &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		return c, err
	},
}}

// dialTest starts a server that accepts one link and gives the client side of
// the same Noise handshake back.
func dialTest(t *testing.T) (*Link, *noisep.Transport, *websocket.Conn, func()) {
	t.Helper()
	server, err := noisep.Keypair(fixedReader{1})
	if err != nil {
		t.Fatalf("server keys: %v", err)
	}
	client, err := noisep.Keypair(fixedReader{2})
	if err != nil {
		t.Fatalf("client keys: %v", err)
	}
	links := make(chan *Link, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l, err := Accept(w, r, Options{Subprotocol: "ngc.test.v1", Prologue: noisep.PrologueSession,
			Static: server, Peer: client.Public})
		if err != nil {
			return
		}
		links <- l
	}))
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}, HTTPClient: testClient})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	hs, err := noisep.Handshake(true, []byte(noisep.PrologueSession), client, server.Public, fixedReader{3})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Fatalf("message 1: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, msg1); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	_, msg2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	_, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil {
		t.Fatalf("read message 2: %v", err)
	}
	l := <-links
	return l, noisep.NewTransport(true, cs1, cs2), conn, func() { conn.CloseNow(); srv.Close() }
}

func TestLinkCarriesFrames(t *testing.T) {
	l, tr, conn, done := dialTest(t)
	defer done()
	f := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("hello")}
	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	sealed, err := tr.Seal(raw)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, sealed); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := l.Read(ctx)
	if err != nil || got.Type != wire.TypeData || got.Channel != 1 || string(got.Payload) != "hello" {
		t.Fatalf("read %+v: %v", got, err)
	}
	if err := l.Write(ctx, wire.Frame{Type: wire.TypeEOF, Channel: 1}); err != nil {
		t.Fatalf("write back: %v", err)
	}
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	plain, err := tr.Open(msg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	back, err := wire.Decode(plain)
	if err != nil || back.Type != wire.TypeEOF {
		t.Fatalf("decode %+v: %v", back, err)
	}
}

func TestLinkRefusesTextAndRubbish(t *testing.T) {
	l, _, conn, done := dialTest(t)
	defer done()
	// A text message ends the link, whatever it holds.
	if err := conn.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := l.Read(ctx); err == nil {
		t.Fatal("a text message was taken")
	}

	l2, _, conn2, done2 := dialTest(t)
	defer done2()
	if err := conn2.Write(ctx, websocket.MessageBinary, []byte("not sealed")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := l2.Read(ctx); err == nil {
		t.Fatal("a message that does not decrypt was taken")
	}
}

// TestLinkWritesOneAtATime holds the promise of the write lock: several
// goroutines relay onto one link, and the nonce of the Noise cipher state makes
// the order they sealed in the order the peer has to open in. Without the lock
// two writers seal and send in different orders, and the peer's Open fails as
// soon as a message arrives out of turn.
func TestLinkWritesOneAtATime(t *testing.T) {
	l, tr, conn, done := dialTest(t)
	defer done()
	const writers = 32
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := wire.Frame{Type: wire.TypeData, Channel: uint32(i + 1), Payload: []byte("x")}
			if err := l.Write(ctx, f); err != nil {
				t.Errorf("write %d: %v", i+1, err)
			}
		}()
	}
	wg.Wait()
	seen := make(map[uint32]bool, writers)
	for range writers {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		plain, err := tr.Open(msg)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		f, err := wire.Decode(plain)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		seen[f.Channel] = true
	}
	for i := 1; i <= writers; i++ {
		if !seen[uint32(i)] {
			t.Fatalf("channel %d never arrived", i)
		}
	}
}

// TestAcceptGivesUpOnASilentClient holds the deadline on the handshake: Hijack
// takes the server's own deadlines off the connection and the request context
// only ends when the handler returns, so a client that upgrades and then sends
// nothing would otherwise hold a goroutine and a file handle for good — before
// any login. This client reads, so the close frame is answered at once and the
// time measured is the handshake's own.
func TestAcceptGivesUpOnASilentClient(t *testing.T) {
	server, err := noisep.Keypair(fixedReader{1})
	if err != nil {
		t.Fatalf("server keys: %v", err)
	}
	client, err := noisep.Keypair(fixedReader{2})
	if err != nil {
		t.Fatalf("client keys: %v", err)
	}
	const wait = 50 * time.Millisecond
	back := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := Accept(w, r, Options{Subprotocol: "ngc.test.v1", Prologue: noisep.PrologueSession,
			Static: server, Peer: client.Public, Handshake: wait})
		back <- err
	}))
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}, HTTPClient: testClient})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	go func() { _, _, _ = conn.Read(ctx) }()
	start := time.Now()
	select {
	case err := <-back:
		if err == nil {
			t.Fatal("Accept took a link from a client that sent nothing")
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("Accept waited %v on a handshake it gave %v", d, wait)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept is still waiting for a client that sent nothing")
	}
}

// fixedReader gives the same bytes every time, so the tests stay deterministic.
type fixedReader struct{ b byte }

func (f fixedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = f.b
	}
	return len(p), nil
}

var _ = noise.DHKey{}
