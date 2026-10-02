// App channels of this controller, from the end-to-end handshake to the one
// CLOSE: openApp answers the handshake the shell began in its OPEN, waits for
// the first inner message that decrypts, and only then hands the channel to
// the service of the app (RunService, call.go).
//
// Two bounds of the contract live here and nowhere else in this package.
//
// The input of the shell ends with its E2EEnd and with nothing else. An EOF or
// a CLOSE of the transport before it ends the transport only, so what the
// program got is half an input, and half an input is never a success
// (shellInput, RunService rule 1).
//
// The output of an action that does not stream is one result of at most
// wire.MaxCallResult bytes. Past that nothing more goes out and the channel
// ends with E_TOOLARGE, whatever the program then did (shellOutput).
//
// And one bound that is not of the contract but of this machine: every wait
// for credit hangs on the clock of the action, the way the input does
// (runApp). A peer that keeps back less than one whole message would otherwise
// hold the send lock of the channel for good, and with it the end this side
// still owes - the clock of the action would be worth nothing. What such a
// channel gets is its one CLOSE with the reason in it.
package serve

import (
	"context"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/e2e"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// errCut says that the input of the shell ended before its E2EEnd. It is no
// io.EOF on purpose: RunService tells a clean end of the input from a cut-off
// one by exactly that (CallInput.In), and only a clean one may be a success.
var errCut = errors.New("serve: the input of the shell was cut off")

// openApp runs the channel of an app: the end-to-end handshake the shell began
// in its OPEN, the wait for its first inner message, and the service that
// first message releases.
//
// That wait is what makes message 1 worth nothing on its own. Two Accepts of
// the same OPEN both come off - the handshake reads message 1 and answers, and
// nothing in it is bound to a time or to a number that was used before - so
// whoever repeats an OPEN he listened in on gets a session here.
// TestAnOpenRepeatedWithoutTheShellsKeysReachesNothing and
// TestReplayedOpenStartsNothing build that case instead of ruling it out. What
// he does not get is the transport keys: they come out of the ephemeral key of
// the real shell, which he does not hold. So he can send no inner message that
// decrypts, and nothing of the app is reached before one has - at the latest
// wire.E2EFirstTimeout seconds after the channel opened.
func (s *Server) openApp(ctx context.Context, ch *link.Channel, d access.Decision) error {
	sess, msg2, err := e2e.Accept(d.Opened, s.d.Node, s.d.Noise)
	if err != nil {
		// Counted, not written: see Server.handshakes.
		s.handshakes.Add(1)
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	// Noise message 2 travels in the first GRANT of the side that accepted
	// the channel, and that is this one. Its credit is the window of the
	// contract: the first inner message of the shell carries its input, and
	// less would make a shell wait for credit before anything of this channel
	// is even known to be real.
	if err := ch.Grant(wire.DefaultWindow, msg2); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	wait, cancel := context.WithTimeout(ctx, s.firstWait())
	defer cancel()
	first, cl := s.await(wait, ch, sess)
	if cl != nil {
		// Before the first inner message there is no shell to answer inside
		// the session: whoever is at the other end may hold none of its keys,
		// so the one CLOSE of the transport is all this side can say.
		return s.end(ch, *cl)
	}
	// From here the shell is the real one, and every outcome of this channel
	// is its E2EEnd (answer). The CLOSE of the transport only ends the
	// transport.
	return s.runApp(ctx, ch, sess, d, first)
}

// await waits for the first inner message of the shell and answers with it, or
// with the CLOSE the channel ends with.
func (s *Server) await(ctx context.Context, ch *link.Channel, sess *e2e.Session) (wire.E2EMessage, *wire.Close) {
	m, err := ch.Recv(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return wire.E2EMessage{}, refuse(wire.CodeTimeout)
		}
		// The channel ended before the first message. At the end of a channel
		// Recv answers with its own end or with the context that was
		// cancelled with it, whichever the runtime takes first; both say the
		// same thing here, so neither is told apart.
		return wire.E2EMessage{}, refuse(wire.CodeProtocol)
	}
	if m.Kind != wire.TypeData {
		// An EOF only ends the transport: the data is cut off, and that is an
		// error and not an end (wire, "End to end").
		return wire.E2EMessage{}, refuse(wire.CodeProtocol)
	}
	inner, err := sess.Open(m.Data)
	if err != nil {
		// Out of Open every failure is the fault of the shell: a DATA that is
		// no Noise message of this session, or one whose plaintext is no
		// inner message.
		return wire.E2EMessage{}, refuse(wire.CodeProtocol)
	}
	// The credit of this frame goes back here and not in shellInput, which
	// never sees the frame the first message came in: without this line the
	// shell loses that much of its window for the whole channel.
	if cl := grantBack(ch, len(m.Data)); cl != nil {
		return wire.E2EMessage{}, cl
	}
	// Data aliases the plaintext of this one message, and that plaintext is
	// this message alone: noisep decrypts into a fresh slice every time
	// (Transport.Open, Decrypt into nil), so no later message writes over
	// what the input still needs. A copy here would be one no case can reach.
	return inner, nil
}

// grantBack gives the credit of one DATA frame back, so the shell may go on
// sending without ever holding more than the first GRANT gave it.
func grantBack(ch *link.Channel, n int) *wire.Close {
	if n == 0 {
		// A GRANT of nothing is none of the contract, and an empty DATA took
		// no credit to give back.
		return nil
	}
	if err := ch.Grant(uint32(n), nil); err != nil {
		if errors.Is(err, link.ErrClosed) {
			// The channel ended while what the shell sent before its end was
			// still being handed out. That is the end of the transport before
			// the E2EEnd of the shell, which is a cut-off input.
			return refuse(wire.CodeProtocol)
		}
		return refuse(wire.CodeInternal)
	}
	return nil
}

// appPlan is what one app channel runs, taken from the manifest this
// controller wrote when the app was activated and never from the OPEN: the
// OPEN is what the API forwarded.
type appPlan struct {
	container string        // the container of the app, which is the name of its system user
	service   string        // the service the action runs on
	action    string        // the action, as the manifest has it
	command   []string      // the program of that service and its arguments
	stream    bool          // the action streams: no time limit and no bound on the output
	timeout   time.Duration // the time limit of the action, 0 for the one of the contract
}

// plan takes that out of the decision. Everything it refuses is this program
// wired wrong or a manifest this controller stored and cannot read again, and
// neither is a peer doing: access has already looked the action up in this
// same manifest (store.RuleFor), so a channel that gets here and finds none
// found two different answers in one version.
func (s *Server) plan(d access.Decision) (appPlan, *wire.Close) {
	if s.d.Engine == nil || d.Manifest == nil || d.App.ID == "" {
		return appPlan{}, refuse(wire.CodeInternal)
	}
	a, ok := d.Manifest.Action(d.Opened.Open.Action)
	if !ok {
		return appPlan{}, refuse(wire.CodeInternal)
	}
	i := slices.IndexFunc(d.Manifest.Services, func(sv store.Service) bool { return sv.Name == a.Service })
	if i < 0 {
		return appPlan{}, refuse(wire.CodeInternal)
	}
	return appPlan{container: runtime.UserName(d.App.ID), service: a.Service, action: a.Name,
		command: d.Manifest.Services[i].Command, stream: a.Stream,
		timeout: time.Duration(a.Timeout) * time.Second}, nil
}

// ensure makes sure the container of the app runs. A container that could not
// be started is the service unreachable, which is what service.Result names
// for a program that could not be started at all.
func (s *Server) ensure(ctx context.Context, name string) *wire.Close {
	up, err := s.d.Engine.Running(ctx, name)
	if err != nil {
		return refuse(wire.CodeUnreachable)
	}
	if up {
		return nil
	}
	if err := s.d.Engine.Start(ctx, name); err != nil {
		return refuse(wire.CodeUnreachable)
	}
	return nil
}

// runApp runs the service of the app behind a handshake that stands.
func (s *Server) runApp(ctx context.Context, ch *link.Channel, sess *e2e.Session,
	d access.Decision, first wire.E2EMessage) error {
	p, cl := s.plan(d)
	if cl != nil {
		return s.answer(ctx, ch, sess, *cl)
	}
	// Here the licence of the app belongs, between the handshake that stands
	// and the container that is started: an app whose licence has run out
	// ends with E_LICENSE and reaches no container at all. Task 17 brings the
	// licence and this is the one place it goes; until then no app channel is
	// refused for one, and no E_LICENSE leaves this package.
	if cl := s.ensure(ctx, p.container); cl != nil {
		return s.answer(ctx, ch, sess, *cl)
	}
	// The input hangs on a clock of its own, and it is the clock of the
	// action: a shell that neither ends its input nor sends more would
	// otherwise hold this call after the program is long over - RunService
	// waits for the goroutine that feeds the program (call.go, "<-fed"), and
	// that one waits here. What such a shell gets is E_PROTOCOL and not
	// E_TIMEOUT, because an input that never ended is a cut-off input and
	// rule 1 of RunService puts that ahead of the limit. An action that
	// streams has no limit at all, there as here: its end is for the shell to
	// decide.
	//
	// What that costs is reckoned here so that nobody has to work it out
	// again. A stream channel whose shell goes quiet holds, per channel: up
	// to wire.DefaultWindow (256 KiB) of DATA this side took credit for, the
	// service.MaxStderr ring (64 KiB), one sealed payload (up to 65514 bytes),
	// a pipe pair with three or four goroutines - and a process in the
	// container, which is the real price. At wire.MaxTunnelChannels (1024,
	// which this controller does enforce) that is roughly 0.4 GiB of buffers
	// and 1024 container processes. wire.MaxSessionChannels (64) would hold it
	// to about 16 browser sessions, but that bound is the API to enforce, not
	// this controller. The bound belongs in a task of its own because it
	// touches the contract: a stream has no time limit by contract, and the
	// contract is frozen. Not built here on purpose.
	// The clock of this call. It bounds the wait for input, the wait for
	// credit on the way out and the program itself - all three through this
	// one deadline.
	inCtx := ctx
	if d := limit(p.timeout, p.stream); d > 0 {
		var stop context.CancelFunc
		inCtx, stop = context.WithTimeout(ctx, d)
		defer stop()
	}
	// The window is read before anything of this side went out, so it is the
	// one the other end gave this channel.
	out := &shellOutput{ctx: inCtx, ch: ch, sess: sess, chunk: innerChunk(ch.Window())}
	if !p.stream {
		out.max = wire.MaxCallResult
	}
	// One clock for the whole call, and that is the point of handing inCtx on:
	// RunService puts the same limit on again (limit), so the deadline that
	// counts is this one, the earlier of two equal ones. With two clocks the
	// outcome was a race - measured, a call whose output the clock had just
	// cut ended E_INTERNAL instead of E_TIMEOUT, because RunService own
	// deadline was microseconds younger and had not fired yet.
	end := RunService(inCtx, CallInput{
		Engine: s.d.Engine, Container: p.container, Command: p.command,
		Call: service.Call{User: d.User, UserName: d.UserName, App: d.App.ID, Service: p.service,
			Action: p.action, Version: d.App.Version, Params: d.Params(), Start: false},
		Timeout: p.timeout, Stream: p.stream,
		In:  &shellInput{ctx: inCtx, ch: ch, sess: sess, buf: first.Data, done: first.Kind == wire.E2EEnd},
		Out: out, Log: s.log(), LogStderr: s.d.LogStderr,
	})
	// What the output side already knows beats what the program ended with: a
	// result past the limit is no result, however happily the program stopped
	// writing it. Nothing was sealed past that limit, so this end still
	// travels inside the session.
	if out.tooLarge {
		return s.answer(ctx, ch, sess, wire.Error(wire.CodeTooLarge))
	}
	if out.err != nil {
		// One inner message was sealed and did not get out - the credit ran
		// out under the clock of the action, or the channel ended. The
		// transport counts its messages, so anything sealed after that one
		// would decrypt at the wrong number: there is no end left to send,
		// and the CLOSE of the transport carries the reason instead. A side
		// that sees the CLOSE before the E2EEnd reports an error of its own
		// (wire, "End to end"), and the code it reads there is this one.
		//
		// The price, said plainly: the shell learns the outcome from the
		// CLOSE and not from inside the session. Keeping it inside would mean
		// taking the credit before sealing, which puts the credit arithmetic
		// of one message outside link - and a message sealed against the
		// wrong number breaks the channel for good, while a CLOSE always
		// gets out, because it costs no credit.
		if end.Status == wire.StatusOK {
			end = wire.Error(wire.CodeInternal)
		}
		return s.end(ch, end)
	}
	return s.answer(ctx, ch, sess, end)
}

// answer puts the outcome of the call into the E2EEnd of this side and ends
// the transport behind it. Nothing of the app follows that end (Session.End
// refuses it), so the CLOSE of the channel carries status ok: it ends the
// transport and says nothing about the call.
//
// Only where the end itself could not be sent does the CLOSE carry the error.
// Then the shell got no E2EEnd, and a side that sees the CLOSE of the
// transport before it reports an error of its own (wire, "End to end").
func (s *Server) answer(ctx context.Context, ch *link.Channel, sess *e2e.Session, cl wire.Close) error {
	ct, err := sess.End(cl)
	if err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	// The end of this side needs credit like everything else, and a peer that
	// gives none must not keep this channel open for it: after this wait the
	// CLOSE goes out instead, which needs no credit, and the shell reads the
	// missing end as an error of its own (wire, "End to end"). The shell gets
	// as long to take this end as it had for its own first message.
	wait, cancel := context.WithTimeout(ctx, s.lastWait())
	defer cancel()
	if err := ch.SendWhole(wait, ct); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	if err := ch.EOF(); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	return s.end(ch, wire.Close{Status: wire.StatusOK})
}

// shellInput is the input of the program: the first inner message of the shell
// and every E2EData behind it, up to its E2EEnd. Only that end is a clean one;
// everything else is errCut.
//
// It is read by the one goroutine RunService copies with, and that goroutine
// is the only one that touches these fields.
type shellInput struct {
	ctx  context.Context
	ch   *link.Channel
	sess *e2e.Session
	buf  []byte // what the last inner message still has to give
	done bool   // the E2EEnd of the shell is in
}

func (r *shellInput) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.done {
			return 0, io.EOF
		}
		if err := r.fill(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// fill takes the next inner message of the shell.
func (r *shellInput) fill() error {
	m, err := r.ch.Recv(r.ctx)
	if err != nil {
		// The channel is over, or the clock of the action ran out while the
		// shell still owed its end. Either way the input never ended.
		return errCut
	}
	if m.Kind != wire.TypeData {
		// The EOF of the transport before the E2EEnd of the shell. It ends
		// the transport and not the data (wire, "End to end").
		return errCut
	}
	if cl := grantBack(r.ch, len(m.Data)); cl != nil {
		return errCut
	}
	inner, err := r.sess.Open(m.Data)
	if err != nil {
		return errCut
	}
	switch inner.Kind {
	case wire.E2EEnd:
		r.done = true
	default:
		// Kept as it came: the plaintext of one message is that message alone
		// (see await), so nothing writes over the buffer under the program.
		r.buf = inner.Data
	}
	return nil
}

// shellOutput is the output of the program on its way to the shell: one inner
// E2EData per piece, and for an action that does not stream no more than max
// bytes in all.
//
// Past that limit nothing goes out and nothing is kept: the write is answered
// as if it had happened, so the program runs on to its own end and this side
// holds none of what it still writes. The outcome of the channel is then
// E_TOOLARGE whatever the program ended with (runApp).
//
// It is written by the goroutine the engine hands the output to, and that one
// alone - the sending half of the session belongs to it until RunService comes
// back (e2e.Session).
type shellOutput struct {
	// ctx is the clock of the action. It bounds the wait for credit, and that
	// bound is what keeps a peer which holds its credit back from holding the
	// channel past that clock: this side waits with the send lock of the
	// channel in hand, so the end of this call could not go out behind it
	// (link.Channel.SendWhole).
	ctx      context.Context
	ch       *link.Channel
	sess     *e2e.Session
	chunk    int   // the plaintext of one inner message, at most (innerChunk)
	max      int64 // 0: no bound, which is what an action that streams has
	tooLarge bool
	err      error
}

func (w *shellOutput) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if w.tooLarge || w.err != nil {
			return n, nil
		}
		c := p
		if len(c) > w.chunk {
			c = c[:w.chunk]
		}
		if w.max > 0 && w.sess.Sent()+int64(len(c)) > w.max {
			w.tooLarge = true
			return n, nil
		}
		ct, err := w.sess.Data(c)
		if err != nil {
			w.err = err
			return n, nil
		}
		if err := w.ch.SendWhole(w.ctx, ct); err != nil {
			w.err = err
			return n, nil
		}
		p = p[len(c):]
	}
	return n, nil
}

// innerChunk is how much of the output one inner message may carry: never
// more than wire.E2E allows, and never more than fits the window the other
// side gave. One inner message travels in exactly one DATA frame
// (link.Channel.SendWhole), so a message larger than that window would wait
// for credit which, window and all, never comes.
//
// What the frame costs is this much plus two things that are easy to forget,
// and forgetting them is a channel that stands still: the kind byte wire.E2E
// puts in front of the body, and the tag the Noise message carries behind it.
// Measured on 02.10.2026 without the kind byte: a shell with the smallest
// window of the contract got the first inner message of its result and then
// nothing at all, because 4080 bytes of body cost 4097 of credit against 4096.
func innerChunk(window uint32) int {
	n := wire.MaxE2EPlaintext - 1
	if w := int(window) - noisep.TagSize - 1; w < n {
		n = w
	}
	if n < 1 {
		n = 1
	}
	return n
}
