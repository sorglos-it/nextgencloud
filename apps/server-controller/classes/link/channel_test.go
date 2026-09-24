package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// answer bounds the wait for one frame of the controller. A test that waits
// for a frame which never comes says so instead of standing until the timeout
// of go test.
const answer = 30 * time.Second

// opener is a handler made of one function.
type opener func(ctx context.Context, ch *Channel, o tunnel.Opened) error

func (f opener) Open(ctx context.Context, ch *Channel, o tunnel.Opened) error { return f(ctx, ch, o) }

// holding is the handler of every test that only wants numbers held: it grants
// what the contract asks of a first GRANT and then stays until the channel or
// the tunnel is over. seen sees the channel at that point.
func holding(seen func(*Channel)) opener {
	return func(ctx context.Context, ch *Channel, _ tunnel.Opened) error {
		if err := ch.Grant(wire.MinWindow, nil); err != nil {
			return err
		}
		<-ctx.Done()
		if seen != nil {
			seen(ch)
		}
		return nil
	}
}

// serving dials a tunnel whose channels go to h and runs it.
func serving(t *testing.T, h Handler, after func(*apiSide)) (*Tunnel, <-chan error) {
	t.Helper()
	return runningPinging(t, nil, nil, func(c *Config) { c.Handler = h }, after)
}

// openPayload is an OPEN of the API for a channel of a core app: window is the
// credit the API grants the controller on this hop. The window of the
// browser's OPEN inside it is the one between browser and API and none of this
// hop's business.
func openPayload(t testing.TB, window uint32) []byte {
	t.Helper()
	inner, err := json.Marshal(wire.Open{App: "ngc.desktop", Service: "core", Action: "json", Window: wire.MinWindow})
	if err != nil {
		t.Fatal(err)
	}
	p, err := json.Marshal(tunnel.Open{Open: inner, Window: window})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func openFrame(t testing.TB, num, window uint32) wire.Frame {
	t.Helper()
	return wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: openPayload(t, window)}
}

func closeFrame(t testing.TB, num uint32, cl wire.Close) wire.Frame {
	t.Helper()
	p, err := json.Marshal(cl)
	if err != nil {
		t.Fatal(err)
	}
	return wire.Frame{Type: wire.TypeClose, Channel: num, Payload: p}
}

// next reads one frame of the controller and gives up after answer.
func (a *apiSide) next() (wire.Frame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), answer)
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

// want reads the next frame of the controller and checks what it is. Every
// wait of a test for an answer goes through here, so a frame that comes for
// another number - or none at all - is named and not waited out.
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

// wantProtocol is want for the one answer to a protocol error.
func (a *apiSide) wantProtocol(num uint32) {
	f := a.want(wire.TypeClose, num)
	c, err := wire.ParseClose(f.Payload)
	if err != nil || c.Status != wire.StatusError || c.Code != wire.CodeProtocol {
		a.t.Errorf("the controller closed channel %d with %+v (%v), want the protocol error", num, c, err)
	}
}

// mark is where the numbers of the markers begin, far from the numbers a test
// opens.
const mark = 1000

// marker is how a test shows that nothing came for another number: a stray
// frame on a number of its own is answered at once, and that answer can only
// be read once everything the controller had to say before it has been read.
// Every marker takes a number of its own - a number this side has closed is
// not free, and a second stray frame on it would be dropped and waited for
// for ever.
func (a *apiSide) marker(n uint32) {
	a.send(wire.Frame{Type: wire.TypeEOF, Channel: mark + n})
	a.wantProtocol(mark + n)
}

// A number is not free with the CLOSE of this side alone: an OPEN for it opens
// nothing until the CLOSE of the API has arrived, and afterwards it opens a
// channel again.
func TestChannelNumberFreeOnlyAfterBothCloses(t *testing.T) {
	var opened atomic.Int64
	h := opener(func(_ context.Context, ch *Channel, _ tunnel.Opened) error {
		opened.Add(1)
		return ch.Close(wire.Close{Status: wire.StatusOK})
	})
	_, ended := serving(t, h, func(a *apiSide) {
		a.send(openFrame(t, 1, wire.MinWindow))
		a.want(wire.TypeClose, 1)
		// The controller has closed, the API has not: the number is still
		// taken, and this OPEN is nothing but a frame after one's own CLOSE.
		a.send(openFrame(t, 1, wire.MinWindow))
		a.marker(1)
		// With the CLOSE of the API the number is free, and the next OPEN is
		// a channel again.
		a.send(closeFrame(t, 1, wire.Close{Status: wire.StatusOK}))
		a.send(openFrame(t, 1, wire.MinWindow))
		a.want(wire.TypeClose, 1)
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	// Counted once the tunnel is over, so that no handler is still on its way.
	if n := opened.Load(); n != 2 {
		t.Errorf("the handler was called %d times, want the OPEN for a number that is not free to open nothing and the one after both closes to open a channel", n)
	}
}

// Every number that is not free counts, and the frame that would make one too
// many ends the link - the whole link, not the channel. One below the limit
// everything still runs.
func TestTooManyChannelsEndsTheLink(t *testing.T) {
	var back atomic.Int64
	_, ended := serving(t, holding(func(*Channel) { back.Add(1) }), func(a *apiSide) {
		for num := uint32(1); num <= wire.MaxTunnelChannels; num++ {
			a.send(openFrame(t, num, wire.MinWindow))
		}
		// Every one of them was taken: each channel grants, and the grants of
		// all of them come back. They are written by a goroutine per channel,
		// so what is checked is the set and not the order.
		got := make(map[uint32]bool, wire.MaxTunnelChannels)
		for range wire.MaxTunnelChannels {
			f, err := a.next()
			if err != nil {
				a.t.Errorf("after %d grants: %v", len(got), err)
				return
			}
			if f.Type != wire.TypeGrant || f.Channel == 0 || f.Channel > wire.MaxTunnelChannels || got[f.Channel] {
				a.t.Errorf("the controller sent %v on channel %d", f.Type, f.Channel)
				return
			}
			got[f.Channel] = true
		}
		// One number more than the contract allows.
		a.send(openFrame(t, wire.MaxTunnelChannels+1, wire.MinWindow))
		a.ending()
	})
	if err := waitFor(t, ended); !errors.Is(err, ErrLink) {
		t.Fatalf("Run = %v, want the link ended over the broken contract", err)
	}
	// The tunnel is gone, and with it every channel on it: the handlers were
	// all waiting on the end of their context and all of them come back.
	for range int(answer / time.Millisecond) {
		if back.Load() == wire.MaxTunnelChannels {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("%d of %d handlers came back, want the end of the tunnel to end every channel on it", back.Load(), wire.MaxTunnelChannels)
}

// The API may send exactly as much DATA as this side granted. The byte on the
// limit still goes through, the one above it closes the channel - and only the
// channel, the link stays.
func TestMoreDataThanGrantedClosesTheChannel(t *testing.T) {
	got := make(chan int, 4)
	h := opener(func(ctx context.Context, ch *Channel, o tunnel.Opened) error {
		if o.Window != wire.MinWindow {
			t.Errorf("the handler was given a window of %d, want the one of the OPEN on the tunnel", o.Window)
		}
		if err := ch.Grant(wire.MinWindow, nil); err != nil {
			return err
		}
		for {
			in, err := ch.Recv(ctx)
			if err != nil {
				return nil
			}
			got <- len(in.Data)
		}
	})
	_, ended := serving(t, h, func(a *apiSide) {
		a.send(openFrame(t, 1, wire.MinWindow))
		a.want(wire.TypeGrant, 1)
		a.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: make([]byte, wire.MinWindow)})
		select {
		case n := <-got:
			if n != wire.MinWindow {
				a.t.Errorf("the handler was given %d bytes, want the whole credit through", n)
			}
		case <-time.After(answer):
			a.t.Error("the data on the limit never reached the handler")
			return
		}
		// One byte more than was ever granted.
		a.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte{0}})
		a.wantProtocol(1)
		// The link stays: another channel is served as before.
		a.send(openFrame(t, 2, wire.MinWindow))
		a.want(wire.TypeGrant, 2)
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A GRANT may lift what this side may send up to the limit of the contract and
// not one byte above it.
func TestGrantAboveMaxWindowClosesTheChannel(t *testing.T) {
	window := make(chan uint32, 2)
	_, ended := serving(t, holding(func(ch *Channel) { window <- ch.Window() }), func(a *apiSide) {
		a.send(openFrame(t, 1, wire.MinWindow))
		a.want(wire.TypeGrant, 1)
		// Exactly the limit: the OPEN granted MinWindow, this grant is the rest.
		a.send(wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(wire.MaxWindow-wire.MinWindow, nil)})
		a.marker(1)
		// One byte above it.
		a.send(wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(1, nil)})
		a.wantProtocol(1)
		select {
		case w := <-window:
			if w != wire.MaxWindow {
				a.t.Errorf("the channel was left with a window of %d, want the grant on the limit taken and the one above it refused", w)
			}
		case <-time.After(answer):
			a.t.Error("the handler never came back")
			return
		}
		a.send(openFrame(t, 2, wire.MinWindow))
		a.want(wire.TypeGrant, 2)
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// After one's own CLOSE nothing is answered any more - but the CLOSE of the
// API among the dropped frames still counts as received and frees the number.
func TestFramesAfterOwnCloseAreDropped(t *testing.T) {
	var opened, back atomic.Int64
	h := opener(func(ctx context.Context, ch *Channel, _ tunnel.Opened) error {
		opened.Add(1)
		if err := ch.Close(wire.Close{Status: wire.StatusOK}); err != nil {
			return err
		}
		// Whatever comes after this side has closed reaches no handler.
		if _, err := ch.Recv(ctx); !errors.Is(err, ErrClosed) {
			t.Errorf("Recv after one's own CLOSE = %v, want the channel over", err)
		}
		back.Add(1)
		return nil
	})
	_, ended := serving(t, h, func(a *apiSide) {
		a.send(openFrame(t, 1, wire.MinWindow))
		a.want(wire.TypeClose, 1)
		a.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("x")})
		a.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
		a.marker(1)
		// The CLOSE under the dropped frames counts all the same.
		a.send(closeFrame(t, 1, wire.Close{Status: wire.StatusOK}))
		a.send(openFrame(t, 1, wire.MinWindow))
		a.want(wire.TypeClose, 1)
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n, b := opened.Load(), back.Load(); n != 2 || b != 2 {
		t.Errorf("the handler was called %d times and came back %d times, want two channels on the one number", n, b)
	}
}

// What this side sends waits for the credit of the API: as much as was granted
// goes out, the rest only once a GRANT for it has come.
func TestSendWaitsForTheCreditOfTheAPI(t *testing.T) {
	sent := make(chan error, 1)
	h := opener(func(_ context.Context, ch *Channel, _ tunnel.Opened) error {
		sent <- ch.Send(make([]byte, 2*wire.MinWindow))
		return nil
	})
	_, ended := serving(t, h, func(a *apiSide) {
		a.send(openFrame(t, 1, wire.MinWindow))
		if f := a.want(wire.TypeData, 1); len(f.Payload) != wire.MinWindow {
			a.t.Errorf("the controller sent %d bytes, want the credit of the OPEN and no more", len(f.Payload))
		}
		// Nothing else comes until the API grants: the marker is answered
		// before the rest of the data.
		a.marker(1)
		a.send(wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(wire.MinWindow, nil)})
		if f := a.want(wire.TypeData, 1); len(f.Payload) != wire.MinWindow {
			a.t.Errorf("the controller sent %d bytes after the grant", len(f.Payload))
		}
		select {
		case err := <-sent:
			if err != nil {
				a.t.Errorf("Send = %v, want everything out", err)
			}
		case <-time.After(answer):
			a.t.Error("Send never came back")
			return
		}
		a.want(wire.TypeClose, 1)
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// The end of a channel writes exactly one CLOSE: one for a handler that closed
// itself, one for a handler that did not, and the error of a handler ends its
// channel and not the link.
func TestAChannelEndsWithExactlyOneClose(t *testing.T) {
	h := opener(func(_ context.Context, ch *Channel, _ tunnel.Opened) error {
		switch ch.Num() {
		case 1:
			return ch.Close(wire.Close{Status: wire.StatusOK})
		case 2:
			return nil
		}
		return errors.New("this handler is done")
	})
	_, ended := serving(t, h, func(a *apiSide) {
		for _, c := range []struct {
			num  uint32
			want wire.Status
		}{{1, wire.StatusOK}, {2, wire.StatusOK}, {3, wire.StatusError}} {
			a.send(openFrame(t, c.num, wire.MinWindow))
			f := a.want(wire.TypeClose, c.num)
			cl, err := wire.ParseClose(f.Payload)
			if err != nil || cl.Status != c.want {
				a.t.Errorf("channel %d ended with %+v (%v), want status %q", c.num, cl, err, c.want)
			}
			// A second CLOSE for the same number would be read here instead
			// of the marker.
			a.marker(c.num)
		}
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want the error of a handler to end its channel and not the link", err)
	}
}

// An OPEN this hop cannot read is no channel: it is answered for its number
// and the link stays.
func TestAnOpenThatIsNoneClosesTheChannel(t *testing.T) {
	var opened atomic.Int64
	h := opener(func(_ context.Context, ch *Channel, _ tunnel.Opened) error {
		opened.Add(1)
		return ch.Close(wire.Close{Status: wire.StatusOK})
	})
	_, ended := serving(t, h, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: []byte("{}")})
		a.wantProtocol(1)
		a.send(openFrame(t, 2, wire.MinWindow))
		a.want(wire.TypeClose, 2)
		a.control(wire.CtlBye, nil)
		a.quiet()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := opened.Load(); n != 1 {
		t.Errorf("the handler was called %d times, want the OPEN that does not parse to reach no handler", n)
	}
}

// What this side refuses to send: the contract is kept before a frame goes
// out, not after the other end has closed the channel over it.
func TestAChannelRefusesWhatTheContractForbids(t *testing.T) {
	done := make(chan struct{})
	m := newMachine(opener(func(ctx context.Context, ch *Channel, o tunnel.Opened) error {
		defer close(done)
		if !bytes.Equal(ch.OpenPayload(), openPayload(t, wire.MinWindow)) {
			t.Error("the channel does not keep the OPEN payload as it arrived")
		}
		if ch.Num() != 1 || ch.Window() != wire.MinWindow {
			t.Errorf("channel %d with a window of %d, want the number and the window of the OPEN", ch.Num(), ch.Window())
		}
		// A first GRANT below the floor of the contract, and one with a
		// handshake that is not Noise message 2.
		if err := ch.Grant(wire.MinWindow-1, nil); !errors.Is(err, ErrConfig) {
			t.Errorf("Grant below MinWindow = %v, want it refused", err)
		}
		if err := ch.Grant(wire.MinWindow, make([]byte, wire.HandshakeSize-1)); !errors.Is(err, ErrConfig) {
			t.Errorf("Grant with a short handshake = %v, want it refused", err)
		}
		if err := ch.Grant(wire.MinWindow, make([]byte, wire.HandshakeSize)); err != nil {
			t.Errorf("the first GRANT with Noise message 2 = %v, want it out", err)
		}
		// Message 2 travels in the first GRANT and in no other, and what is
		// granted never goes above the window of the contract.
		if err := ch.Grant(wire.MinWindow, make([]byte, wire.HandshakeSize)); !errors.Is(err, ErrConfig) {
			t.Errorf("a handshake in a later GRANT = %v, want it refused", err)
		}
		if err := ch.Grant(wire.MaxWindow, nil); !errors.Is(err, ErrConfig) {
			t.Errorf("a GRANT above MaxWindow = %v, want it refused", err)
		}
		if err := ch.Grant(wire.MaxWindow-wire.MinWindow, nil); err != nil {
			t.Errorf("a GRANT up to MaxWindow = %v, want it out", err)
		}
		// The EOF of the API arrives as what it is.
		in, err := ch.Recv(ctx)
		if err != nil || in.Kind != wire.TypeEOF || len(in.Data) != 0 {
			t.Errorf("Recv = %+v (%v), want the EOF of the API", in, err)
		}
		// After its own EOF a side sends no more data, and it sends no second
		// EOF either.
		if err := ch.EOF(); err != nil {
			t.Errorf("EOF = %v, want it out", err)
		}
		if err := ch.EOF(); !errors.Is(err, ErrConfig) {
			t.Errorf("a second EOF = %v, want it refused", err)
		}
		if err := ch.Send([]byte("x")); !errors.Is(err, ErrConfig) {
			t.Errorf("Send after the own EOF = %v, want it refused", err)
		}
		// A CLOSE payload that is none of the contract is not sent, and the
		// channel stays open for the end of this handler.
		if err := ch.Close(wire.Close{Status: "weder ok noch error"}); !errors.Is(err, ErrConfig) {
			t.Errorf("Close with a status of its own = %v, want it refused", err)
		}
		return nil
	}))
	defer m.stop()
	if err := m.c.take(openFrame(t, 1, wire.MinWindow)); err != nil {
		t.Fatalf("OPEN: %v", err)
	}
	if err := m.c.take(wire.Frame{Type: wire.TypeEOF, Channel: 1}); err != nil {
		t.Fatalf("EOF: %v", err)
	}
	select {
	case <-done:
	case <-time.After(answer):
		t.Fatal("the handler never came back")
	}
	if !m.settle() {
		t.Fatal("the channel never wrote its CLOSE")
	}
	// One GRANT that was refused is one that never went out: what stands here
	// is the first GRANT with message 2, the one up to MaxWindow, the EOF and
	// the one CLOSE.
	var got []wire.Type
	for _, f := range m.frames() {
		got = append(got, f.Type)
	}
	want := []wire.Type{wire.TypeGrant, wire.TypeGrant, wire.TypeEOF, wire.TypeClose}
	if !slices.Equal(got, want) {
		t.Errorf("the channel wrote %v, want %v", got, want)
	}
}

// The CLOSE of this side is the last frame of this side on that number: a
// write that was already standing goes out in front of it, and nothing of the
// channel goes out behind it.
func TestTheCloseOfThisSideIsTheLastFrame(t *testing.T) {
	m := newMachine(opener(func(_ context.Context, ch *Channel, _ tunnel.Opened) error {
		if err := ch.Grant(wire.MinWindow, nil); err != nil {
			return err
		}
		return ch.Send([]byte("standing in the socket"))
	}))
	m.gate = make(chan struct{})
	defer m.stop()
	if err := m.c.take(openFrame(t, 1, wire.MinWindow)); err != nil {
		t.Fatalf("OPEN: %v", err)
	}
	// Wait until the write of the handler stands in the socket.
	for range 10000 {
		if m.waiting.Load() > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if m.waiting.Load() == 0 {
		t.Fatal("no write of the handler ever stood in the socket")
	}
	// An OPEN for a number that is open: the answer is a CLOSE, and it has to
	// wait for the write that is standing.
	closing := make(chan error, 1)
	go func() { closing <- m.c.take(openFrame(t, 1, wire.MinWindow)) }()
	time.Sleep(100 * time.Millisecond)
	for _, f := range m.frames() {
		if f.Type == wire.TypeClose {
			t.Fatal("the CLOSE went out in front of a write of this side that was standing")
		}
	}
	close(m.gate)
	if err := <-closing; err != nil {
		t.Fatalf("the answer to the OPEN ended the link: %v", err)
	}
	if !m.settle() {
		t.Fatal("the channel never wrote its CLOSE")
	}
	var got []wire.Type
	for _, f := range m.frames() {
		got = append(got, f.Type)
	}
	if want := []wire.Type{wire.TypeGrant, wire.TypeData, wire.TypeClose}; !slices.Equal(got, want) {
		t.Errorf("the channel wrote %v, want %v", got, want)
	}
}

// machine drives the state machine without a socket: every frame this side
// writes goes into the recorder, and the life of the channels ends with the
// test.
type machine struct {
	c    *channels
	stop context.CancelFunc
	mu   sync.Mutex
	out  []wire.Frame
	prot atomic.Int64 // answers of this side with CLOSE E_PROTOCOL
	// gate holds every DATA write of this side until it is closed; waiting
	// counts the writes that reached it. A test that wants a write standing
	// in the socket builds it here.
	gate    chan struct{}
	waiting atomic.Int64
}

func newMachine(h Handler) *machine {
	m := &machine{}
	ctx, cancel := context.WithCancel(context.Background())
	m.stop = cancel
	m.c = newChannels(ctx, h, m.write, slog.New(slog.DiscardHandler))
	return m
}

func (m *machine) write(f wire.Frame) error {
	if m.gate != nil && f.Type == wire.TypeData {
		m.waiting.Add(1)
		<-m.gate
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.out = append(m.out, wire.Frame{Type: f.Type, Channel: f.Channel, Payload: append([]byte(nil), f.Payload...)})
	if f.Type == wire.TypeClose {
		if c, err := wire.ParseClose(f.Payload); err == nil && c.Code == wire.CodeProtocol {
			m.prot.Add(1)
		}
	}
	return nil
}

func (m *machine) frames() []wire.Frame {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]wire.Frame(nil), m.out...)
}

// settle waits until every channel of the machine has written its CLOSE, so
// that no goroutine of a channel is still on its way when the numbers are
// looked at - and so that a test leaves none behind.
func (m *machine) settle() bool {
	for i := range 10000 {
		if m.quiet() {
			return true
		}
		// The goroutines of the channels only have to be let onto a
		// processor; waiting a millisecond for each of them would cost a
		// fuzzing run most of its inputs.
		if i < 1000 {
			runtime.Gosched()
			continue
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func (m *machine) quiet() bool {
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	for _, ch := range m.c.m {
		ch.mu.Lock()
		done := ch.closed
		ch.mu.Unlock()
		if !done {
			return false
		}
	}
	return true
}

// live says whether the machine holds num as a channel that is still running:
// not closed by this side, not closed by the API. It is what a frame that was
// taken leaves behind, and a frame that was refused - or dropped after one's
// own CLOSE - does not.
func (m *machine) live(num uint32) bool {
	m.c.mu.Lock()
	ch, ok := m.c.m[num]
	m.c.mu.Unlock()
	if !ok {
		return false
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return !ch.closing && !ch.peer
}

// A DATA frame with an empty payload costs no credit, and neither does an EOF,
// so the credit in bytes alone does not bound how many frames may wait for a
// handler that does not read. The count does: this side takes no more frames
// than the credit it granted could have paid for, one byte each, and the frame
// above that closes the channel.
func TestAChannelQueuesNoMoreFramesThanItsCreditCouldPay(t *testing.T) {
	m := newMachine(holding(nil))
	defer m.stop()
	if err := m.c.take(openFrame(t, 1, wire.MinWindow)); err != nil {
		t.Fatalf("OPEN: %v", err)
	}
	// The grant is written by the goroutine of the channel, and the credit is
	// what the count hangs on: wait for it to be out.
	for range 10000 {
		if len(m.frames()) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	empty := wire.Frame{Type: wire.TypeEOF, Channel: 1}
	for range wire.MinWindow + 1 {
		if err := m.c.take(empty); err != nil {
			t.Fatalf("the link ended over a frame that costs nothing: %v", err)
		}
	}
	if n := m.prot.Load(); n != 0 {
		t.Fatalf("%d frames were refused, want every frame the credit could have paid for taken", n)
	}
	if err := m.c.take(empty); err != nil {
		t.Fatalf("the frame above the count ended the link (%v), want it to end the channel", err)
	}
	if n := m.prot.Load(); n != 1 {
		t.Errorf("the frame above the count was answered %d times with the protocol error, want once", n)
	}
}

// canary is the number of the frames every fuzz input begins with, out of the
// reach of the numbers the fuzzer picks.
const canary = 100

// canonical is a payload of its type that the contract takes.
func canonical(t testing.TB, typ wire.Type) []byte {
	t.Helper()
	switch typ {
	case wire.TypeOpen:
		return openPayload(t, wire.MinWindow)
	case wire.TypeData:
		return []byte{7}
	case wire.TypeClose:
		return []byte(`{"status":"ok"}`)
	case wire.TypeGrant:
		return wire.Grant(wire.MinWindow, nil)
	}
	return nil
}

// FuzzChannelFrames throws frames at the state machine. Nothing it is given
// may make it panic, and no number may become free again for which no CLOSE
// has arrived. The bytes are read as a list of frames - type, number, length,
// payload - and the high bit of the first byte asks for a payload of the
// contract, so that the fuzzer reaches the states behind the parsers and not
// only their doors. How many frames it got through is counted: a target that
// is only ever told no proves nothing, and the count says so.
func FuzzChannelFrames(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 1})
	f.Add([]byte{0x81, 0, 0, 0x82, 0, 0, 0x84, 0, 0})
	f.Add([]byte{0x81, 0, 0, 0x85, 0, 0, 0x82, 0, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	var tried, taken atomic.Int64
	f.Cleanup(func() {
		// Over the seeds the share says something: their frames are fixed.
		// In a fuzzing run it says nothing - the fuzzer is paid in coverage,
		// and the ways of saying no are where the coverage lies, so it learns
		// to drive any such share down: measured over a fresh corpus 45 % of
		// the frames taken, and under 5 % after a minute of fuzzing. What
		// holds there is the canary below, on every single input.
		//
		// Over the seeds as they stand 7 of 13 frames are taken; a third is
		// the line, so a seed that is refused from now on says so and a seed
		// added later does not have to be counted in by hand.
		if n := tried.Load(); !fuzzing() && n > 0 && taken.Load()*3 < n {
			f.Errorf("only %d of %d frames were taken, want the states behind the parsers reached", taken.Load(), n)
		}
	})
	f.Fuzz(func(t *testing.T, b []byte) { channelFrames(t, b, &tried, &taken) })
}

// channelFrames is the body of the target, so that it can be driven from a
// test of its own as well.
func channelFrames(t *testing.T, b []byte, tried, taken *atomic.Int64) {
	m := newMachine(holding(nil))
	defer m.stop()
	// The canary runs before every input, on a number the frames below
	// cannot reach: an OPEN of the contract opens a channel, and a frame
	// this side has room for is taken. A machine that says no to
	// everything fails here on every input, whatever the count below
	// says - and the count alone could be talked down by a fuzzer that
	// learns to steer into the refusals.
	for _, f := range []wire.Frame{openFrame(t, canary, wire.MinWindow), {Type: wire.TypeEOF, Channel: canary}} {
		tried.Add(1)
		if err := m.c.take(f); err != nil || !m.live(canary) {
			t.Fatalf("the machine did not take %v on the number of the canary (%v)", f.Type, err)
		}
		taken.Add(1)
	}
	sent := make(map[uint32]bool)
	closed := make(map[uint32]bool)
	for len(b) >= 3 {
		typ := wire.Type(1 + b[0]%5)
		num := uint32(1 + b[1]%8)
		n, want := int(b[2]), b[0]&0x80 != 0
		b = b[3:]
		if n > len(b) {
			n = len(b)
		}
		p := b[:n:n]
		b = b[n:]
		switch {
		case typ == wire.TypeEOF:
			p = nil
		case want || len(p) == 0:
			p = canonical(t, typ)
		}
		raw, err := wire.Frame{Type: typ, Channel: num, Payload: p}.Encode()
		if err != nil {
			continue
		}
		fr, err := wire.Decode(raw)
		if err != nil {
			continue
		}
		tried.Add(1)
		if err := m.c.take(fr); err != nil {
			if !errors.Is(err, ErrLink) {
				t.Fatalf("the machine ended the link with %v, want the one error of a broken contract", err)
			}
			break
		}
		// Counted only once the frame is through: a frame that ended the
		// link never reached a number.
		sent[num] = true
		if typ == wire.TypeClose {
			closed[num] = true
		}
		// Taken means taken: the number is a channel that is still
		// running afterwards. A frame that was answered with E_PROTOCOL
		// and one that was dropped after this side's own CLOSE both leave
		// a number that is not, and neither counts here.
		if m.live(num) {
			taken.Add(1)
		}
	}
	m.stop()
	if !m.settle() {
		t.Fatal("a channel of this input never wrote its CLOSE")
	}
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	for num := range sent {
		if _, ok := m.c.m[num]; !ok && !closed[num] {
			t.Fatalf("number %d is free again although no CLOSE for it ever arrived", num)
		}
	}
}
