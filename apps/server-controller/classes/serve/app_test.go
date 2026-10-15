package serve

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// appEngine stands in for the container engine of the app of the fixture: it
// counts the starts of its container and the runs of its service, and runs the
// program the way a case tells it to.
//
// running is written and read in the goroutine of the channel alone (Start and
// Running), so nothing but the counters is shared with the case. The counters
// are atomic because a case reads them while a channel may still be running.
type appEngine struct {
	noEngine
	running bool
	starts  atomic.Int64
	runs    atomic.Int64
	// run is the program of the service. nil ends with 0 and writes nothing.
	run func(ctx context.Context, e runtime.Exec) (int, error)
}

func (e *appEngine) Running(context.Context, string) (bool, error) { return e.running, nil }

func (e *appEngine) Start(context.Context, string) error {
	e.starts.Add(1)
	e.running = true
	return nil
}

func (e *appEngine) Exec(ctx context.Context, ex runtime.Exec) (int, error) {
	e.runs.Add(1)
	if e.run == nil {
		return 0, nil
	}
	return e.run(ctx, ex)
}

// drain is a service program that reads its whole input and ends with 0. It is
// what lets a cut-off input reach this controller at all: a program that never
// reads leaves the input standing in the pipe, and the end of the channel
// behind it is then never read either.
func drain(_ context.Context, e runtime.Exec) (int, error) {
	if e.Stdin != nil {
		io.Copy(io.Discard, e.Stdin)
	}
	return 0, nil
}

// says is a service program that writes text to stdout and ends with 0 - or,
// where its context ended first, the way a real engine ends there.
//
// That last part is not decoration. podman kills the container with the
// context, and no status reaches the engine then (runtime.Engine.Exec), so it
// answers ErrKilled. A stand-in that said 0 instead would let a call whose
// output never got out look like a program that was happy, and the case about
// the clock of the action would measure against a fixture that lies.
func says(text string) func(context.Context, runtime.Exec) (int, error) {
	return func(ctx context.Context, e runtime.Exec) (int, error) {
		if e.Stdout != nil {
			if _, err := io.WriteString(e.Stdout, text); err != nil {
				return 0, err
			}
		}
		if err := ctx.Err(); err != nil {
			return 0, errors.Join(runtime.ErrKilled, err)
		}
		return 0, nil
	}
}

// shell plays the shell at the other end of one app channel: the end-to-end
// transport of this one channel and the inner messages that travel in its
// DATA.
type shell struct {
	t   *testing.T
	a   *apiSide
	num uint32
	tr  *noisep.Transport
	// credit is what the controller granted this side and this side has not
	// spent: the input of the shell is bounded by it, and what pays for more
	// is the GRANT the controller writes for every DATA it took.
	credit int64
	// halves says that this side gives the credit of a DATA back in two
	// GRANT instead of one, so that the controller meets a window which is
	// for a while smaller than one inner message.
	halves bool
	// pend carries the one read that may be in flight. A case that waits for
	// nothing (quiet) must not cancel a read: coder/websocket closes the
	// connection when a read context ends, because it cannot pick the stream
	// up again in the middle - measured, the whole tunnel went down and the
	// case blamed the controller for it. So the read runs in a goroutine, and
	// the one it brings is kept here until somebody takes it.
	pend    chan inbound
	reading bool
	// held are frames a wait took off the wire that were none of its business
	// - the heartbeat, a GRANT, and above all the CLOSE of the channel. They
	// are given to whoever reads next: a wait that threw them away would have
	// eaten the very end this case is about. Measured: quiet swallowed the
	// CLOSE and the case then waited 30 seconds for it.
	held []wire.Frame
	// data is what the controller sent so far, decrypted. A case that reads
	// single frames (one) and then the rest (out) gets the whole result.
	data []byte
	// biggest is the largest DATA frame the controller sent on this channel.
	// One inner message is one frame, so this is what says whether the
	// controller kept its messages inside the window of this side.
	biggest int
}

// start opens an app channel and completes the half of the handshake of the
// shell with the message 2 that came in the first GRANT.
func start(t *testing.T, a *apiSide, num uint32, payload []byte, hs *noise.HandshakeState) *shell {
	t.Helper()
	credit, msg2 := a.open(num, payload)
	if len(msg2) != wire.HandshakeSize {
		t.Fatalf("the first GRANT of channel %d carried %d bytes of handshake, want Noise message 2",
			num, len(msg2))
	}
	return &shell{t: t, a: a, num: num, tr: finish(t, hs, msg2), credit: credit}
}

// inner sends one inner message of the shell and waits for the credit it
// costs, so a shell with more input than the first GRANT gave stands here
// instead of sending into a window that is spent.
func (sh *shell) inner(kind byte, body []byte) {
	ct := seal(sh.t, sh.tr, kind, body)
	for sh.credit < int64(len(ct)) {
		f, err := sh.recv()
		if err != nil {
			sh.t.Errorf("waiting for credit on channel %d: %v", sh.num, err)
			return
		}
		if f.Channel != sh.num || f.Type != wire.TypeGrant {
			sh.t.Errorf("the controller sent %v on channel %d while input was still to come",
				f.Type, f.Channel)
			return
		}
		c, _, err := wire.ParseGrant(f.Payload)
		if err != nil {
			sh.t.Errorf("GRANT of channel %d: %v", sh.num, err)
			return
		}
		sh.credit += int64(c)
	}
	sh.credit -= int64(len(ct))
	sh.a.data(sh.num, ct)
}

// out reads the inner messages of the controller up to its E2EEnd and gives
// its output and that outcome. Every DATA frame is granted back at once, so a
// result larger than the window of the OPEN gets out instead of standing in
// the credit of this side.
func (sh *shell) out() ([]byte, wire.Close) {
	for {
		f, err := sh.recv()
		if err != nil {
			sh.t.Errorf("waiting for the end-to-end end of channel %d: %v", sh.num, err)
			return sh.data, wire.Close{}
		}
		if f.Channel != sh.num {
			sh.t.Errorf("the controller sent %v on channel %d, want channel %d", f.Type, f.Channel, sh.num)
			return sh.data, wire.Close{}
		}
		switch f.Type {
		case wire.TypeGrant, wire.TypeEOF:
			continue
		case wire.TypeClose:
			sh.t.Errorf("channel %d ended before the end-to-end end of the controller", sh.num)
			return sh.data, wire.Close{}
		case wire.TypeData:
		default:
			sh.t.Errorf("the controller sent %v on channel %d", f.Type, sh.num)
			return sh.data, wire.Close{}
		}
		if len(f.Payload) > sh.biggest {
			sh.biggest = len(f.Payload)
		}
		sh.grant(len(f.Payload))
		m, ok := sh.inward(f)
		if !ok {
			return sh.data, wire.Close{}
		}
		if m.Kind == wire.E2EEnd {
			return sh.data, m.Status
		}
	}
}

// inward decrypts one DATA frame of the controller and keeps its data. The
// order matters and is not the case to choose: the transport counts its
// messages, so a frame that is skipped or read twice decrypts in neither.
func (sh *shell) inward(f wire.Frame) (wire.E2EMessage, bool) {
	plain, err := sh.tr.Open(f.Payload)
	if err != nil {
		sh.t.Errorf("DATA of channel %d: %v", sh.num, err)
		return wire.E2EMessage{}, false
	}
	m, err := wire.ParseE2E(plain, true)
	if err != nil {
		sh.t.Errorf("inner message of channel %d: %v", sh.num, err)
		return wire.E2EMessage{}, false
	}
	if m.Kind == wire.E2EData {
		sh.data = append(sh.data, m.Data...)
	}
	return m, true
}

// one reads the next DATA frame of the controller and gives its length on the
// wire. It grants nothing back: the credit is then the case to hand out, piece
// by piece, which is what makes the next frame a measurement and not a race.
func (sh *shell) one() int {
	for {
		f, err := sh.recv()
		if err != nil {
			sh.t.Errorf("waiting for a DATA of channel %d: %v", sh.num, err)
			return 0
		}
		if f.Channel != sh.num || f.Type != wire.TypeData {
			continue
		}
		if len(f.Payload) > sh.biggest {
			sh.biggest = len(f.Payload)
		}
		if _, ok := sh.inward(f); !ok {
			return 0
		}
		return len(f.Payload)
	}
}

// inbound is one frame a read brought, or why it brought none.
type inbound struct {
	f   wire.Frame
	err error
}

// pump starts a read unless one is already in flight.
func (sh *shell) pump() {
	if sh.pend == nil {
		sh.pend = make(chan inbound, 1)
	}
	if sh.reading {
		return
	}
	sh.reading = true
	go func() {
		f, err := sh.a.next()
		sh.pend <- inbound{f, err}
	}()
}

// recv is the next frame of the controller, read through that one goroutine.
func (sh *shell) recv() (wire.Frame, error) {
	if len(sh.held) > 0 {
		f := sh.held[0]
		sh.held = sh.held[1:]
		return f, nil
	}
	sh.pump()
	in := <-sh.pend
	sh.reading = false
	return in.f, in.err
}

// quiet fails unless the controller sends nothing on this channel for d. What
// else travels on the tunnel - the heartbeat on channel 0, a GRANT of the
// other direction - is none of this. A read that is still in flight when d is
// up stays in flight: the next recv takes it.
func (sh *shell) quiet(d time.Duration) {
	until := time.Now().Add(d)
	for {
		left := time.Until(until)
		if left <= 0 {
			return
		}
		sh.pump()
		select {
		case in := <-sh.pend:
			sh.reading = false
			if in.err != nil {
				return
			}
			if in.f.Channel == sh.num && in.f.Type == wire.TypeData {
				sh.t.Errorf("the controller sent %d bytes on channel %d while the credit was below one whole message",
					len(in.f.Payload), sh.num)
				return
			}
			sh.held = append(sh.held, in.f)
		case <-time.After(left):
			return
		}
	}
}

// give hands n bytes of credit to the controller.
func (sh *shell) give(n int) {
	sh.a.send(wire.Frame{Type: wire.TypeGrant, Channel: sh.num, Payload: wire.Grant(uint32(n), nil)})
}

// grant gives the credit of one DATA frame back, in one GRANT or in two.
func (sh *shell) grant(n int) {
	if sh.halves && n > 1 {
		sh.a.send(wire.Frame{Type: wire.TypeGrant, Channel: sh.num, Payload: wire.Grant(uint32(n/2), nil)})
		sh.a.send(wire.Frame{Type: wire.TypeGrant, Channel: sh.num, Payload: wire.Grant(uint32(n-n/2), nil)})
		return
	}
	sh.a.send(wire.Frame{Type: wire.TypeGrant, Channel: sh.num, Payload: wire.Grant(uint32(n), nil)})
}

// rest reads what follows the end-to-end end of the controller: nothing of the
// app may come after it, so anything but the EOF and the CLOSE of the
// transport is a fault.
func (sh *shell) rest() wire.Close {
	for {
		f, err := sh.recv()
		if err != nil {
			sh.t.Errorf("waiting for the CLOSE of channel %d: %v", sh.num, err)
			return wire.Close{}
		}
		switch f.Type {
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				sh.t.Errorf("CLOSE of channel %d: %v", sh.num, err)
			}
			return cl
		case wire.TypeData:
			sh.t.Errorf("the controller sent %d bytes on channel %d after its own end",
				len(f.Payload), sh.num)
		}
	}
}

// wantOK fails unless the channel ended in success.
func wantOK(t *testing.T, cl wire.Close, what string) {
	t.Helper()
	if cl.Status != wire.StatusOK || cl.Code != "" {
		t.Errorf("%s = %+v, want status ok", what, cl)
	}
}

// The service is started only once the first inner message of the shell has
// decrypted: before it nothing of the app is reached, and a dummy engine
// counts both halves of that.
func TestAppChannelRunsOnlyAfterTheFirstInnerMessage(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = says("fertig")
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		// The controller is waiting for the first inner message, so nothing
		// can have run: the only way to the service leads through it.
		if n := w.eng.runs.Load(); n != 0 {
			t.Errorf("the service ran %d times before the first inner message, want none", n)
		}
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		data, end := sh.out()
		if string(data) != "fertig" {
			t.Errorf("the shell got %q, want the output of the service", data)
		}
		wantOK(t, end, "the end-to-end end")
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.eng.runs.Load(); n != 1 {
		t.Errorf("the service ran %d times, want once", n)
	}
	if n := w.eng.starts.Load(); n != 1 {
		t.Errorf("the container was started %d times, want once for one that was not running", n)
	}
}

// The same OPEN a second time is taken - message 1 can be repeated - but
// whoever repeats it holds none of the keys of the shell, so he sends no inner
// message that decrypts. After the wait for the first one the channel is over,
// and the service never ran for it.
func TestReplayedOpenStartsNothing(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = says("eins")
	w.srv.first = 50 * time.Millisecond
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		data, end := sh.out()
		if string(data) != "eins" {
			t.Errorf("the real shell got %q, want the output of the service", data)
		}
		wantOK(t, end, "the end-to-end end of the real shell")
		wantOK(t, sh.rest(), "the CLOSE of the channel of the real shell")
		a.shut(1)
		// The same OPEN once more, and not one inner message behind it.
		a.open(2, payload)
		wantCode(t, a.closed(2), wire.CodeTimeout)
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.eng.runs.Load(); n != 1 {
		t.Errorf("the service ran %d times, want the one run of the real shell", n)
	}
}

// An input that ends with the EOF of the transport instead of with the E2EEnd
// of the shell is cut off, and a cut-off input is never a success - whatever
// the program made of what it got.
func TestCutInputIsAnError(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = drain
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, []byte("halb"))
		// No E2EEnd of the shell: only the transport ends.
		a.eof(1)
		data, end := sh.out()
		if len(data) != 0 {
			t.Errorf("the shell got %d bytes on a call whose input was cut off, want none", len(data))
		}
		wantCode(t, end, wire.CodeProtocol)
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// An action that does not stream has one result, of at most
// wire.MaxCallResult bytes. A service with more ends with E_TOOLARGE, and
// nothing past the limit goes out.
func TestResultAboveMaxCallResultEndsWithTooLarge(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = func(_ context.Context, e runtime.Exec) (int, error) {
		chunk := bytes.Repeat([]byte{'y'}, 64<<10)
		for sent := 0; sent <= wire.MaxCallResult; sent += len(chunk) {
			if _, err := e.Stdout.Write(chunk); err != nil {
				return 0, err
			}
		}
		return 0, nil
	}
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		data, end := sh.out()
		if len(data) > wire.MaxCallResult {
			t.Errorf("the shell got %d bytes, want at most the %d of one result",
				len(data), wire.MaxCallResult)
		}
		if len(data) == 0 {
			t.Error("the shell got nothing at all, want the result up to the limit")
		}
		wantCode(t, end, wire.CodeTooLarge)
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// One inner message travels in one DATA frame, so it may never be larger than
// the window the other side gave this channel: a shell with the smallest
// window of the contract gets its whole result all the same, in pieces that
// fit that window. A message broken over two frames would decrypt in neither
// of them, and a message that waits for credit the window never holds would
// never come at all.
func TestResultFitsTheWindowOfTheShell(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	const size = 200 << 10
	w.eng.run = says(strings.Repeat("z", size))
	payload, hs := w.appOpenWin(t, nil, wire.MinWindow)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		data, end := sh.out()
		if len(data) != size {
			t.Errorf("the shell got %d bytes, want the %d of the result", len(data), size)
		}
		if sh.biggest > wire.MinWindow {
			t.Errorf("the largest DATA frame was %d bytes, want at most the %d of the window",
				sh.biggest, wire.MinWindow)
		}
		wantOK(t, end, "the end-to-end end")
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// counts is a service program that reads its whole input and writes how many
// bytes it was.
func counts(_ context.Context, e runtime.Exec) (int, error) {
	n, err := io.Copy(io.Discard, e.Stdin)
	if err != nil {
		return 0, err
	}
	_, err = io.WriteString(e.Stdout, strconv.FormatInt(n, 10))
	return 0, err
}

// An input larger than the first GRANT gave reaches the program whole: every
// DATA this side takes off the channel is granted again at once, so the shell
// never holds more credit than that one window and never runs out of it
// either. Without that the input of this case would stop after the first
// 262144 bytes of it.
func TestInputBeyondTheFirstGrantIsGrantedBack(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = counts
	const pieces, piece = 12, 32 << 10
	w.srv.first = answer
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		body := bytes.Repeat([]byte{'i'}, piece)
		for i := 0; i < pieces; i++ {
			sh.inner(wire.E2EData, body)
		}
		sh.inner(wire.E2EEnd, nil)
		data, end := sh.out()
		if want := strconv.Itoa(pieces * piece); string(data) != want {
			t.Errorf("the program read %q bytes of input, want %s", data, want)
		}
		wantOK(t, end, "the end-to-end end")
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// settle is how long a case waits to find out that the controller sends
// nothing. It only has to outlast one wake of a goroutine that is already
// parked on the credit of this channel, so it is generous by a lot.
const settle = time.Second

// One inner message travels in exactly one DATA frame, and the controller
// waits for the credit of a whole one before it writes anything
// (link.Channel.SendWhole). Half a Noise message decrypts in neither of its
// frames, so a controller that fills the credit of the moment instead breaks
// the channel - and it breaks it depending on the timing, which is why this
// case does not leave the timing a say.
//
// The credit is handed out in two halves with a look in between: while only
// half a message is paid for, NOTHING may come. With a fragmenting write in
// place of SendWhole a short frame comes in that window, or at the latest the
// frame after it is short - two ways to die, neither of them a race. The
// compiler refuses the plain swap of the two methods (link.Whole), so what is
// left for this case to hold is somebody who takes the bytes out of the sealed
// message on purpose (e2e.Sealed.WholePayload) and streams them.
//
// That is the lesson of this round, and it was paid for: the first version of
// this case sent both halves right after one another and let the controller
// decide when it looked. Measured over 30 runs it let the mutant live 9 times.
// A case that holds a rule in 70 % of its runs holds nothing.
func TestCreditBelowOneMessageHoldsTheWholeFrameBack(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	const size = 20 << 10
	w.eng.run = says(strings.Repeat("c", size))
	payload, hs := w.appOpenWin(t, nil, wire.MinWindow)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		// The first message of the result is paid for by the window of the
		// OPEN and comes whole. After it the credit of this side is spent.
		if n := sh.one(); n != wire.MinWindow {
			t.Fatalf("the first frame was %d bytes, want the %d of a whole message",
				n, wire.MinWindow)
		}
		// Half of it back, and no more.
		sh.give(wire.MinWindow / 2)
		sh.quiet(settle)
		// Now the rest, and only now may the next message come - whole.
		sh.give(wire.MinWindow / 2)
		if n := sh.one(); n != wire.MinWindow {
			t.Errorf("the second frame was %d bytes, want the %d of a whole message",
				n, wire.MinWindow)
		}
		sh.give(wire.MinWindow)
		data, end := sh.out()
		if len(data) != size {
			t.Errorf("the shell got %d bytes, want the %d of the result", len(data), size)
		}
		if sh.biggest > wire.MinWindow {
			t.Errorf("the largest DATA frame was %d bytes, want at most the %d of the window",
				sh.biggest, wire.MinWindow)
		}
		wantOK(t, end, "the end-to-end end")
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A shell that keeps back less than one whole message of credit and then
// stands still must not hold this channel. The wait for credit runs with the
// clock of the action (link.Channel.SendWhole takes a context), and that is
// not comfort: the whole wait holds the send lock of the channel, so without
// the clock the end of this side could not go out behind it - the shell would
// get no E2EEnd at all and the channel would stay open until the other side or
// the tunnel ended it.
//
// What the shell gets instead is the CLOSE of the channel with the E_TIMEOUT
// of the action in it, and that is as far as it can go: the message that was
// waiting had been sealed, and the transport counts its messages.
func TestAPeerThatHoldsItsCreditDoesNotHoldTheChannel(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.quickAction(t, 1)
	// Two messages of output, so the second one has to wait for credit this
	// side never gives.
	w.eng.run = says(strings.Repeat("h", 2*(wire.MinWindow-noisep.TagSize-1)))
	payload, hs := w.appOpenWin(t, nil, wire.MinWindow)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		if n := sh.one(); n != wire.MinWindow {
			t.Fatalf("the first frame was %d bytes, want the %d of a whole message", n, wire.MinWindow)
		}
		// Not one byte of credit back for the message that follows. The
		// clock of the action cuts the wait, and the CLOSE of the channel
		// carries its code: the message that waited was already sealed, so
		// nothing may follow it inside the session (runApp).
		sh.quiet(settle)
		wantCode(t, sh.rest(), wire.CodeTimeout)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// And where even the end of this side gets no credit, the channel still ends:
// after that wait the CLOSE goes out, which costs none, and it carries the
// error because the shell never got the E2EEnd (wire, "End to end").
func TestAnEndThatGetsNoCreditStillClosesTheChannel(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.quickAction(t, 1)
	w.srv.last = 100 * time.Millisecond
	// Exactly one whole message of output: after it the credit of the shell is
	// spent, and the end of this side has nothing to travel on.
	w.eng.run = says(strings.Repeat("e", wire.MinWindow-noisep.TagSize-1))
	payload, hs := w.appOpenWin(t, nil, wire.MinWindow)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		if n := sh.one(); n != wire.MinWindow {
			t.Fatalf("the first frame was %d bytes, want the %d of a whole message", n, wire.MinWindow)
		}
		wantCode(t, sh.rest(), wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}
