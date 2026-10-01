package link

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/trust"
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
	return dialOptions(t, Options{Origins: []string{"https://ngc.example"}})
}

// dialOptions is dialTest with room for the fields of Options a case wants to
// set; the subprotocol, the prologue and both keys belong to the helper.
func dialOptions(t *testing.T, o Options) (*Link, *noisep.Transport, *websocket.Conn, func()) {
	t.Helper()
	l, tr, conn, _, done := dialRaw(t, o)
	return l, tr, conn, done
}

// dialRaw is dialOptions with the peer's own TCP connection handed back. A close
// frame says nothing about whether the socket is gone — for a message over the
// read limit the library writes that frame itself (write.go:428) — so only the
// raw socket can hold the promise that a link really ended.
func dialRaw(t *testing.T, o Options) (*Link, *noisep.Transport, *websocket.Conn, net.Conn, func()) {
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
		o.Subprotocol, o.Prologue, o.Static, o.Peer = "ngc.test.v1", noisep.PrologueSession, server, client.Public
		l, err := Accept(w, r, o)
		if err != nil {
			return
		}
		links <- l
	}))
	var raw net.Conn
	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if tc, ok := c.(*net.TCPConn); ok {
				_ = tc.SetLinger(0)
			}
			raw = c
			return c, err
		},
	}}
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}, HTTPClient: hc})
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
	return l, noisep.NewTransport(true, cs1, cs2), conn, raw, func() { conn.CloseNow(); srv.Close() }
}

// drainToEOF reads the raw socket until it ends. It never touches the WebSocket
// client again, so what it sees is the state of the server's socket: EOF or a
// reset means gone, a read deadline that expires means still standing.
func drainToEOF(t *testing.T, raw net.Conn, wait time.Duration) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	if err := raw.SetReadDeadline(time.Now().Add(wait)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	buf := make([]byte, 4096)
	for {
		if _, err := raw.Read(buf); err != nil {
			return time.Since(start), err
		}
	}
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

// TestAcceptRefusesAHandshakeWithoutWaiting holds the same bound on the way in,
// where it matters more: a refusal there is reachable by anyone who reaches the
// port, before any login. This client sends a text message where Noise message 1
// belongs and then does not read, so it never answers the close frame.
func TestAcceptRefusesAHandshakeWithoutWaiting(t *testing.T) {
	server, err := noisep.Keypair(fixedReader{1})
	if err != nil {
		t.Fatalf("server keys: %v", err)
	}
	client, err := noisep.Keypair(fixedReader{2})
	if err != nil {
		t.Fatalf("client keys: %v", err)
	}
	back := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := Accept(w, r, Options{Subprotocol: "ngc.test.v1", Prologue: noisep.PrologueSession,
			Static: server, Peer: client.Public})
		back <- err
	}))
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}, HTTPClient: testClient})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	start := time.Now()
	select {
	case err := <-back:
		if err == nil {
			t.Fatal("Accept took a link whose first message was text")
		}
		// Two sides on purpose, and the upper one an absolute number: with
		// CloseWait raised to 1900 ms both cases stayed green when they only
		// asked for "under two seconds", so a raised constant went unseen. The
		// lower side holds that the close frame is sent at all, not that waiting
		// is a virtue: whoever later decides to send the frame without waiting
		// changes this line on purpose, and that is the point of having it.
		if d := time.Since(start); d < CloseWait || d > 1500*time.Millisecond {
			t.Fatalf("the refused handshake held the handler %v, CloseWait is %v", d, CloseWait)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Accept never came back from a refused handshake")
	}
}

// TestARefusalDoesNotWaitOnTheRulebreaker holds the bound on the close of a
// refused message: the peer breaks the rules and then does not read, so it never
// answers the close frame. Without the bound websocket.Close waits five seconds
// for that answer, and whoever opens many refused links holds a reader that long
// for each.
func TestARefusalDoesNotWaitOnTheRulebreaker(t *testing.T) {
	l, _, conn, done := dialTest(t)
	defer done()
	if err := conn.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	start := time.Now()
	if _, err := l.Read(ctx); err == nil {
		t.Fatal("a text message was taken")
	}
	// The same two sides as in TestAcceptRefusesAHandshakeWithoutWaiting.
	if d := time.Since(start); d < CloseWait || d > 1500*time.Millisecond {
		t.Fatalf("the refusal held the reader %v, CloseWait is %v", d, CloseWait)
	}
}

// TestOriginsAreChecked holds the rules on the list of allowed origins. The
// WebSocket library reads every value as a path.Match pattern, measured: "*"
// matches the origin host evil.example.com, "ngc.exampl?" matches ngc.example,
// "[" is a syntax error that makes the library refuse every origin, and
// "https://[::1]:8443" does not even match itself, quietly. A list that holds
// one of those is not a list of allowed origins.
func TestOriginsAreChecked(t *testing.T) {
	many := make([]string, trust.MaxOrigins+1)
	for i := range many {
		many[i] = "ngc.example"
	}
	good := [][]string{
		nil,
		{},
		{"https://ngc.example"},
		// http://localhost is why trust.ValidOrigin is not the check here.
		{"ngc.example", "https://ngc.example:8443", "http://localhost:8101"},
		many[:trust.MaxOrigins],
	}
	bad := [][]string{
		{""},                         // matches nothing, says nothing
		{"*"},                        // every origin host
		{"https://*.example"},        // every host under it
		{"ngc.exampl?"},              // one character free
		{"["},                        // unreadable: the library then refuses every origin
		{"a\\"},                      // unreadable for the same reason
		{"https://[::1]:8443"},       // a character class, so it never matches itself
		{"https://ngc.example", "*"}, // the wildcard behind a good value
		// A dead entry beside a good one: refused here, so Accept refuses every
		// request of that link rather than letting the list half work.
		{"https://ngc.example", "https://[::1]:8443"},
		many, // more than the contract allows
	}
	for _, o := range good {
		if err := CheckOrigins(o); err != nil {
			t.Errorf("CheckOrigins(%q) refused a list that is one: %v", o, err)
		}
	}
	for _, o := range bad {
		if err := CheckOrigins(o); !errors.Is(err, ErrOrigins) {
			t.Errorf("CheckOrigins(%q) answered %v", o, err)
		}
	}
}

// TestAcceptRefusesAWildcardOrigin holds that Accept uses those rules, and that
// it refuses before the upgrade: nothing is written to the peer, so a
// misconfigured server does not even look like a WebSocket.
func TestAcceptRefusesAWildcardOrigin(t *testing.T) {
	server, err := noisep.Keypair(fixedReader{1})
	if err != nil {
		t.Fatalf("server keys: %v", err)
	}
	client, err := noisep.Keypair(fixedReader{2})
	if err != nil {
		t.Fatalf("client keys: %v", err)
	}
	back := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := Accept(w, r, Options{Subprotocol: "ngc.test.v1", Prologue: noisep.PrologueSession,
			Static: server, Peer: client.Public, Origins: []string{"*"}})
		back <- err
	}))
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}, HTTPClient: testClient})
	if err == nil {
		conn.CloseNow()
		t.Fatal("the upgrade went through although a wildcard stood in the origins")
	}
	if err := <-back; !errors.Is(err, ErrOrigins) {
		t.Fatalf("Accept answered %v", err)
	}
}

// TestAnUnsealedFrameIsRefused holds the one rule the suite of the plan could
// not hold: only Noise can refuse a frame that is a perfectly good frame and was
// never sealed. The two payloads of TestLinkRefusesTextAndRubbish are each
// refused by both rules at once ("hi" does not decrypt, "not sealed" is no
// frame), so neither of them could fall; letting the error of tr.Open through
// kept the whole suite green, and with it a link that hands up bytes nobody
// authenticated.
func TestAnUnsealedFrameIsRefused(t *testing.T) {
	l, _, conn, done := dialTest(t)
	defer done()
	raw, err := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("hello")}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A peer that reads answers the close frame, so the refusal costs no second.
	go func() { _, _, _ = conn.Read(ctx) }()
	if got, err := l.Read(ctx); err == nil {
		t.Fatalf("Read took an unsealed frame: %+v", got)
	}
}

// TestASealedFrameAsTextIsRefused is the other half: only the "binary only" rule
// can refuse a message whose bytes are a sealed, perfectly good frame.
func TestASealedFrameAsTextIsRefused(t *testing.T) {
	l, tr, conn, done := dialTest(t)
	defer done()
	raw, err := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("hello")}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	sealed, err := tr.Seal(raw)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, sealed); err != nil {
		t.Fatalf("write: %v", err)
	}
	go func() { _, _, _ = conn.Read(ctx) }()
	if got, err := l.Read(ctx); err == nil {
		t.Fatalf("Read took a text message: %+v", got)
	}
}

// TestAForeignOriginIsRefused holds what the list of allowed origins is for: it
// has to reach the library and be used there. Only a request that carries an
// Origin header shows it - websocket.Dial sends none, and the library lets a
// request without one through, so every other case runs past the matcher.
func TestAForeignOriginIsRefused(t *testing.T) {
	back, srv := acceptOnce(t, Options{Origins: []string{"https://ngc.example"},
		Handshake: 50 * time.Millisecond})
	defer srv.Close()
	conn, resp, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}, HTTPClient: testClient,
			HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}}})
	if err == nil {
		conn.CloseNow()
		t.Fatal("the upgrade went through for a foreign origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("the foreign origin got %v, not 403", resp)
	}
	if err := <-back; err == nil {
		t.Fatal("Accept took a link from a foreign origin")
	}
}

// TestAnAllowedOriginGetsThrough is the other direction: an origin the list
// names must get through, or the list would be a lock with no key.
func TestAnAllowedOriginGetsThrough(t *testing.T) {
	_, srv := acceptOnce(t, Options{Origins: []string{"https://ngc.example"},
		Handshake: 50 * time.Millisecond})
	defer srv.Close()
	conn, resp, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}, HTTPClient: testClient,
			HTTPHeader: http.Header{"Origin": []string{"https://ngc.example"}}})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("an origin the list names was refused: %v (status %d)", err, status)
	}
	conn.CloseNow()
}

// TestAWrongSubprotocolIsRefused: the library negotiates the empty subprotocol
// for a client that asks for another one, so without this rule such a client
// would get a link and speak something else. Noise does not cover it.
func TestAWrongSubprotocolIsRefused(t *testing.T) {
	// Two seconds for the handshake, so "refused at once" and "waited for a
	// message 1 that never came" are two different times: without the rule the
	// answer is the same ErrLink, only later.
	back, srv := acceptOnce(t, Options{Handshake: 2 * time.Second})
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.other.v1"}, HTTPClient: testClient})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	go func() { _, _, _ = conn.Read(ctx) }()
	start := time.Now()
	if err := <-back; !errors.Is(err, ErrLink) {
		t.Fatalf("Accept answered %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("the wrong subprotocol was not refused at once but after %v", d)
	}
}

// TestTheLargestFrameFits holds the read limit from the side that must not be
// refused: the largest frame the contract allows, sealed, is exactly the limit.
func TestTheLargestFrameFits(t *testing.T) {
	l, tr, conn, done := dialTest(t)
	defer done()
	f := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: make([]byte, wire.MaxPayload)}
	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(raw) != wire.MaxFrame {
		t.Fatalf("the largest frame is %d bytes, not wire.MaxFrame", len(raw))
	}
	sealed, err := tr.Seal(raw)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if len(sealed) != wire.MaxFrame+noisep.TagSize {
		t.Fatalf("the sealed largest frame is %d bytes, the read limit is %d",
			len(sealed), wire.MaxFrame+noisep.TagSize)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, sealed); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := l.Read(ctx)
	if err != nil || len(got.Payload) != wire.MaxPayload {
		t.Fatalf("the largest frame did not get through: %v (payload %d)", err, len(got.Payload))
	}
}

// TestOneByteOverTheLimitEndsTheLink holds the other side of the limit and the
// promise at Read that every failure ends the link. The library writes its close
// frame for a message that is too big but leaves the connection standing, so
// without the drop the caller held a link that could not write any more and paid
// five seconds in Close to be rid of the socket.
func TestOneByteOverTheLimitEndsTheLink(t *testing.T) {
	l, _, conn, done := dialTest(t)
	defer done()
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, wire.MaxFrame+noisep.TagSize+1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, err := l.Read(ctx); err == nil {
		t.Fatalf("a message over the read limit was taken: %+v", got)
	}
	if err := l.Write(ctx, wire.Frame{Type: wire.TypeEOF, Channel: 1}); err == nil {
		t.Fatal("the link still writes after a message over the limit")
	}
	start := time.Now()
	if err := l.Close(websocket.StatusProtocolError, "done"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Close took %v after a message over the limit", d)
	}
}

// TestNoFirstFrameEndsTheLink holds FirstFrameTimeout: a replayed handshake
// needs no key, and after it only this window keeps a goroutine and a file
// handle from being held for good.
func TestNoFirstFrameEndsTheLink(t *testing.T) {
	l, _, _, done := dialOptions(t, Options{FirstFrame: 50 * time.Millisecond})
	defer done()
	back := make(chan error, 1)
	start := time.Now()
	// In its own goroutine: without the window Read never comes back, and a
	// case that hangs for the ten minutes of go test is no red case.
	go func() { _, err := l.Read(ctx); back <- err }()
	select {
	case err := <-back:
		if err == nil {
			t.Fatal("Read took a frame that was never sent")
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("Read waited %v for the first frame", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read is still waiting for a first frame that never came")
	}
	if err := l.Close(websocket.StatusProtocolError, "done"); err != nil {
		t.Fatalf("Close after the window: %v", err)
	}
}

// TestAfterTheFirstFrameThereIsNoDeadline is the price of that window, held so
// nobody widens it into an idle cap: a session that is logged in may sit still.
func TestAfterTheFirstFrameThereIsNoDeadline(t *testing.T) {
	const window = 50 * time.Millisecond
	l, tr, conn, done := dialOptions(t, Options{FirstFrame: window})
	defer done()
	send := func(payload string) {
		raw, err := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte(payload)}.Encode()
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
	}
	send("one")
	if got, err := l.Read(ctx); err != nil || string(got.Payload) != "one" {
		t.Fatalf("first frame: %+v %v", got, err)
	}
	time.Sleep(3 * window)
	// The second read has to start before the second frame arrives, or it takes
	// bytes that are already in the buffer and an expired deadline never shows.
	type answer struct {
		f   wire.Frame
		err error
	}
	back := make(chan answer, 1)
	go func() {
		f, err := l.Read(ctx)
		back <- answer{f, err}
	}()
	time.Sleep(window)
	send("two")
	select {
	case a := <-back:
		if a.err != nil || string(a.f.Payload) != "two" {
			t.Fatalf("second frame after %v of quiet: %+v %v", 4*window, a.f, a.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second frame never arrived")
	}
}

// TestCloseIsFreeAfterARefusal holds the other half of CloseWait: Read is free
// after a second, and the Close a caller does next must not pay the four that
// are left. Measured before: Read 1,0005 s plus Close 3,9995 s = 5,0004 s, so
// the five seconds were moved, not gone.
func TestCloseIsFreeAfterARefusal(t *testing.T) {
	l, _, conn, done := dialTest(t)
	defer done()
	if err := conn.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := l.Read(ctx); err == nil {
		t.Fatal("a text message was taken")
	}
	start := time.Now()
	if err := l.Close(websocket.StatusProtocolError, "done"); err != nil {
		t.Fatalf("Close after a refusal: %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Close took %v after a refusal", d)
	}
}

// TestADroppedLinkEndsThePeersSocket holds what drop() is for, and nothing else
// holds it: the socket has to be gone. Everything the suite asked before — Read
// fails, Write fails, Close is quick and nil — is just as true while the file
// handle stands open, so a drop() that only set the flag kept all nineteen cases
// green. The library writes the close frame for a message over the limit itself,
// so the frame cannot tell the two apart; the raw socket can.
func TestADroppedLinkEndsThePeersSocket(t *testing.T) {
	l, _, conn, raw, done := dialRaw(t, Options{})
	defer done()
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, wire.MaxFrame+noisep.TagSize+1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, err := l.Read(ctx); err == nil {
		t.Fatalf("a message over the read limit was taken: %+v", got)
	}
	if err := l.Close(websocket.StatusProtocolError, "done"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	d, err := drainToEOF(t, raw, 3*time.Second)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the server's socket still stood %v after the failed read, and Close had said nil", d)
	}
}

// TestCloseEndsTheLinkInOrder holds the patient half of Close, which no case held
// either: a Close that does nothing at all and answers nil was green everywhere,
// because the two cases that touch Close only ask that it is quick. The peer has
// to see the close frame, and with the status it was given.
func TestCloseEndsTheLinkInOrder(t *testing.T) {
	l, _, conn, _, done := dialRaw(t, Options{})
	defer done()
	back := make(chan error, 1)
	// The peer reads, so the close handshake finishes at once and this is the
	// orderly end, not a refusal.
	go func() { _, _, err := conn.Read(ctx); back <- err }()
	if err := l.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-back:
		if got := websocket.CloseStatus(err); got != websocket.StatusNormalClosure {
			t.Fatalf("the peer saw %v (status %d), not a normal close", err, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the peer never saw a close frame")
	}
}

// TestTheDeadlinesBeforeLoginStayBounds holds the two deadlines that stand before
// any login against silent growth. Every case that needs one sets it through
// Options, so the default values themselves were held by nothing: a hundredfold
// HandshakeTimeout and a tenfold FirstFrameTimeout left all nineteen cases green.
// A band and not an equality, because an equality only repeats the number and has
// to be edited together with it — what matters is that these stay bounds a human
// can wait out and an attacker cannot live in.
func TestTheDeadlinesBeforeLoginStayBounds(t *testing.T) {
	if HandshakeTimeout <= 0 || HandshakeTimeout > 15*time.Second {
		t.Errorf("HandshakeTimeout is %v, which is no bound before a login", HandshakeTimeout)
	}
	if FirstFrameTimeout <= 0 || FirstFrameTimeout > 60*time.Second {
		t.Errorf("FirstFrameTimeout is %v, which is no bound before a login", FirstFrameTimeout)
	}
	// Zero or less means the default, anything else counts as it stands. The
	// negative value is the half the comment promises and the code almost missed.
	for _, c := range []struct {
		set, handshake, firstFrame time.Duration
	}{
		{0, HandshakeTimeout, FirstFrameTimeout},
		{-1, HandshakeTimeout, FirstFrameTimeout},
		{-time.Hour, HandshakeTimeout, FirstFrameTimeout},
		{time.Second, time.Second, time.Second},
	} {
		if got := (Options{Handshake: c.set}).handshake(); got != c.handshake {
			t.Errorf("Options{Handshake: %v}.handshake() = %v, want %v", c.set, got, c.handshake)
		}
		if got := (Options{FirstFrame: c.set}).firstFrame(); got != c.firstFrame {
			t.Errorf("Options{FirstFrame: %v}.firstFrame() = %v, want %v", c.set, got, c.firstFrame)
		}
	}
}

// TestASecondEndingIsFree holds the swap in fail and drop. Without it the second
// ending meets the close of the first in flight, and websocket.CloseNow then
// waits in waitGoroutines instead of dropping anything — measured, four seconds
// for a link that was already over. It takes the unexported drop because the
// contract gives a link one reader, so no sequence of public calls reaches here.
func TestASecondEndingIsFree(t *testing.T) {
	l, _, conn, _, done := dialRaw(t, Options{})
	defer done()
	// The peer does not read, so the first ending pays CloseWait in full.
	if err := conn.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := l.Read(ctx); err == nil {
		t.Fatal("a text message was taken")
	}
	start := time.Now()
	l.drop()
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("the second ending took %v", d)
	}
}

// TestWriteRefusesAFrameWireRefuses is the other end of the link, and it had no
// case at all: Write checks what wire says about a frame before it seals it. A
// Write that ignored that answer would seal the empty slice Encode returns, and
// the peer would take that for the next message in the nonce order — so the
// damage is not one bad message but every message after it. The case therefore
// asks the peer, not Write: the first thing it may see is the good frame.
func TestWriteRefusesAFrameWireRefuses(t *testing.T) {
	l, tr, conn, _, done := dialRaw(t, Options{})
	defer done()
	// EOF on the control channel is no frame (package wire), so nothing goes out.
	if err := l.Write(ctx, wire.Frame{Type: wire.TypeEOF, Channel: wire.ControlChannel}); err == nil {
		t.Fatal("Write took a frame wire refuses")
	}
	if err := l.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("hello")}); err != nil {
		t.Fatalf("write after the refused one: %v", err)
	}
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	plain, err := tr.Open(msg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, err := wire.Decode(plain)
	if err != nil || string(got.Payload) != "hello" {
		t.Fatalf("the first message the peer saw was %+v: %v", got, err)
	}
}

// acceptOnce serves one request with Accept and hands its answer back; the
// fields the helper owns are filled as in dialOptions.
func acceptOnce(t *testing.T, o Options) (<-chan error, *httptest.Server) {
	t.Helper()
	server, err := noisep.Keypair(fixedReader{1})
	if err != nil {
		t.Fatalf("server keys: %v", err)
	}
	client, err := noisep.Keypair(fixedReader{2})
	if err != nil {
		t.Fatalf("client keys: %v", err)
	}
	back := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.Subprotocol, o.Prologue, o.Static, o.Peer = "ngc.test.v1", noisep.PrologueSession, server, client.Public
		_, err := Accept(w, r, o)
		back <- err
	}))
	return back, srv
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
