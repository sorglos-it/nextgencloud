// Package serve hands every channel an API opens on the tunnel of this
// controller to what carries it out. It is the one place where the gate of
// package access and the end-to-end session of package e2e meet the channel
// machinery of package link: access decides whether a channel may run at all,
// serve then runs it to its one CLOSE.
//
// Two kinds of channel come in, and they are told apart by that decision and
// by nothing else (access.Decision.Core):
//
//   - A core action of this controller. Its DATA is plain inside the Noise
//     session of the tunnel, its input is bounded by core.Action.MaxIn and
//     its result is one JSON value of at most wire.MaxCallResult bytes
//     (core.EncodeResult).
//   - A channel to an app. Its DATA is encrypted a second time, between the
//     shell and this controller, and nothing of it is readable for the API
//     in between (package e2e).
//
// Open never comes back with an error: a handler that does costs its channel
// a line in the log of this controller (link.Handler), and which channels
// arrive is the API's to choose, so a peer would be writing that log. Every
// refusal is written as the CLOSE of the channel instead, and what must be
// watched all the same is counted (HandshakeFailures).
//
// What this build carries out is ngc.users/controller/trust (trust.go) and
// the end-to-end handshake of an app channel. The core actions that install,
// update and license an app, and the service an app channel is handed to
// once its handshake stands, come with the tasks that build them; until then
// a channel for one of them is refused instead of half served.
package serve

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/chain"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/e2e"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Deps is what a Server needs. Everything in it belongs to the whole server
// and is used from the goroutine of every channel at once, so nothing here
// may be changed once New has seen it.
//
// What is not in it yet: the installer, the updater, the licence and the
// container engine. Every one of them is reached from a core action or from
// the service of an app channel, and none of those is carried out here, so a
// field for them would name a type nothing in this repository has. They are
// added by the tasks that bring them, and a keyed Deps literal goes on
// compiling when they are.
type Deps struct {
	// Node is this controller's node ID in the trust list. It decides which
	// tasks are made out for this server (access) and it is hashed into the
	// prologue of every end-to-end channel (noisep.E2EPrologue), so a wrong
	// one lets no app channel through at all.
	Node string
	// Noise is this controller's own static Noise key pair, the one its node
	// carries in the trust list (trust.Node.Noise). e2e.Accept answers the
	// shell's message 1 with it.
	Noise noise.DHKey
	// Chain holds the trust list. Every channel reads the newest verified
	// version once, at its start, and decides on that one to its end.
	Chain *chain.Keeper
	// State is this controller's local store: the apps it runs and the IDs of
	// the tasks that have run (access.Request), and the versions of the trust
	// list behind Chain.
	State *state.Store
	// Now is the clock of the checks, in seconds. nil: time.Now().Unix().
	Now func() int64
	// Log is where this package writes. nil: slog.Default(). Nothing a peer
	// can bring about is written here (see the doc of this package).
	Log *slog.Logger
}

// action is what carries out one core action of this controller: it gets the
// decision access made and the input that arrived before the EOF of the API,
// and answers either a result to encode or the CLOSE the channel ends with.
type action func(ctx context.Context, d access.Decision, in []byte) (any, *wire.Close)

// name is an action of core.All, as a key: core.Action holds a function and
// cannot be compared.
type name [3]string

// trustKey is ngc.users/controller/trust, the one core action this package
// carries out itself (trust.go).
var trustKey = name{core.AppUsers, core.ServiceController, core.ActTrust}

// Server answers the channels of one controller. It may be used by every
// tunnel of this server at once: after New nothing in it is written but the
// counter, which is atomic. The two fields a test changes it sets before the
// first channel exists, so no goroutine of a channel reads them while they
// are written.
type Server struct {
	d Deps
	// acts is what this build carries out. Only trust is in it; the other
	// core actions of service controller — activate, deactivate, uninstall,
	// delete-data, stage, install and license — are carried out by the tasks
	// that bring the installer, the updater and the licence. Only a test puts
	// something else in, to see a result this server would refuse.
	acts map[name]action
	// first bounds the wait for the shell's first inner message. Only a test
	// sets it, to run the wait out in a fraction of a second; without it the
	// wire.E2EFirstTimeout of the contract holds.
	first time.Duration
	// handshakes counts the end-to-end handshakes that did not come off. It
	// is counted and not written: e2e.ErrHandshake says nothing about whose
	// fault it was, so every one of them would be answered with E_INTERNAL in
	// the log as well — and a shell whose message 1 names another controller
	// puts a line there for every channel it opens.
	// TestAHandshakeThatFailsIsCountedAndNotWritten holds both halves.
	handshakes atomic.Uint64
}

// New returns the server of these dependencies.
func New(d Deps) *Server {
	s := &Server{d: d}
	s.acts = map[name]action{trustKey: s.trust}
	return s
}

// HandshakeFailures is how many end-to-end handshakes failed on this server
// since it started. Nothing of them reaches the log (see Server.handshakes),
// so this number is what says that they happen at all.
func (s *Server) HandshakeFailures() uint64 { return s.handshakes.Load() }

// Open answers one channel of a tunnel and fulfils link.Handler. It comes
// back once the channel has its CLOSE, and always with nil: what went wrong
// stands in that CLOSE.
//
// The tunnel.Opened of link carries no session (its doc), so nothing of it is
// read here: access parses the OPEN payload itself, and only that parse says
// who opened the channel.
func (s *Server) Open(ctx context.Context, ch *link.Channel, _ tunnel.Opened) error {
	// The channel of this handler gets a context of its own. link ends the
	// one it hands over with the CLOSE of either side, so this one ends with
	// it; what it adds is the end of this call, which frees whatever the
	// waits below hang on the clock.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A Server without its halves is this program wired wrong, and this line
	// is what keeps the mistake from being fatal: the controller runs as
	// root, and the first channel to reach a missing half would take it down.
	// access guards its own two the same way.
	if s.d.Chain == nil || s.d.State == nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	// The trust list is read once, here, and the whole channel is decided on
	// that one version: access reads the certificate, the root permission,
	// the re-confirmation and the user's name out of it one after the other,
	// and a version that changed in between would have them from two. A
	// keeper never writes into a list it has handed out — it puts the next
	// version in place of it (chain.Keeper.Current, measured in
	// TestTheListOfAChannelDoesNotChangeUnderIt) — so holding the pointer is
	// holding the version.
	d, cl := access.Check(ctx, access.Request{Payload: ch.OpenPayload(), List: s.d.Chain.List(),
		Node: s.d.Node, Now: s.now(), Apps: s.d.State, Tasks: s.d.State})
	if cl != nil {
		return s.end(ch, *cl)
	}
	if d.Core {
		return s.core(ctx, ch, d)
	}
	return s.app(ctx, ch, d)
}

// core runs the channel of a core action of this controller: the first GRANT,
// the input up to the EOF of the API, the action, and its result as DATA, EOF
// and CLOSE ok.
func (s *Server) core(ctx context.Context, ch *link.Channel, d access.Decision) error {
	run, ok := s.acts[name{d.Action.App, d.Action.Service, d.Action.Name}]
	if !ok {
		// access let the channel through, so the action is one of this
		// controller's — this build just does not carry it out yet. Nothing
		// is read and nothing is granted: a channel that cannot be served is
		// over before it costs this server a byte.
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	// The first GRANT is the credit the API sends its input with; what this
	// side may send back is the window of the OPEN. A core channel carries no
	// handshake in that GRANT (wire, "End to end").
	if err := ch.Grant(firstGrant(d.Action), nil); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	in, cl := s.read(ctx, ch, d.Action)
	if cl != nil {
		return s.end(ch, *cl)
	}
	v, cl := run(ctx, d, in)
	if cl != nil {
		return s.end(ch, *cl)
	}
	// No second check of len(b) against wire.MaxCallResult follows this call,
	// and none belongs here: core.EncodeResult carries that limit itself
	// (shared/core/results.go, "len(b) > wire.MaxCallResult"), so a check
	// behind it could never answer. Whoever writes one writes a line no test
	// can reach.
	b, err := core.EncodeResult(v)
	if err != nil {
		// core.EncodeResult refuses three things with one error: a value that
		// does not encode, a null anywhere in it, and more than
		// wire.MaxCallResult bytes. Only the last of them can happen to a
		// result this server built out of its own types, and the contract
		// names E_TOOLARGE for exactly it ("A sender with more ends with
		// CodeTooLarge"), so that is what goes out. The other two are named
		// wrong here, knowingly and measurably:
		// TestAResultWithANullIsNamedTooLargeAsWell holds one of them on this
		// code, so whoever tells them apart fails there.
		return s.end(ch, wire.Error(wire.CodeTooLarge))
	}
	if err := ch.Send(b); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	if err := ch.EOF(); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	return s.end(ch, wire.Close{Status: wire.StatusOK})
}

// read takes the input of a core action: every DATA up to the EOF of the API,
// at most core.Action.MaxIn bytes. What this side took off the channel it
// grants again at once, so the API may go on sending without ever holding
// more than the first GRANT gave it.
func (s *Server) read(ctx context.Context, ch *link.Channel, a core.Action) ([]byte, *wire.Close) {
	var (
		in  []byte
		got int64
	)
	for {
		m, err := ch.Recv(ctx)
		if err != nil {
			return nil, cutOff()
		}
		switch m.Kind {
		case wire.TypeEOF:
			return in, nil
		case wire.TypeData:
			// Counted before it is kept: this server never holds more than
			// the action allows, whatever the API sends.
			got += int64(len(m.Data))
			if got > a.MaxIn {
				return nil, refuse(wire.CodeTooLarge)
			}
			in = append(in, m.Data...)
			if len(m.Data) == 0 {
				// A GRANT of nothing is none of the contract, and an empty
				// DATA took no credit to give back.
				continue
			}
			if err := ch.Grant(uint32(len(m.Data)), nil); err != nil {
				// The credit that was taken goes back unless the channel
				// ended in the meantime, and that end is the API's doing and
				// not a failure of this server: the CLOSE of the API comes in
				// while what it sent before it is still being handed out
				// (link.Channel.Recv), so the grant for the last piece of it
				// meets a channel that is over.
				if errors.Is(err, link.ErrClosed) {
					return nil, cutOff()
				}
				return nil, refuse(wire.CodeInternal)
			}
		}
	}
}

// cutOff is the CLOSE of a channel that ended before its input did. Whether
// the API closed it or the tunnel went is not told apart: either way no
// action runs on half an input, and the one CLOSE this side owes says so. An
// API that closed the channel itself has already said what the shell is told;
// this answer only frees the number.
func cutOff() *wire.Close { return refuse(wire.CodeProtocol) }

// app runs the channel of an app: the end-to-end handshake the shell began in
// its OPEN, and the wait for its first inner message.
//
// That wait is what makes message 1 worth nothing on its own. Two Accepts of
// the same OPEN both come off — the handshake reads message 1 and answers,
// and nothing in it is bound to a time or to a number that was used before —
// so whoever repeats an OPEN he listened in on gets a session here.
// TestAnOpenRepeatedWithoutTheShellsKeysReachesNothing builds that case
// instead of ruling it out. What he does not get is the transport keys: they
// come out of the ephemeral key of the real shell, which he does not hold. So
// he can send no inner message that decrypts, and nothing of the app is
// reached before one has — at the latest wire.E2EFirstTimeout seconds after
// the channel opened.
func (s *Server) app(ctx context.Context, ch *link.Channel, d access.Decision) error {
	sess, msg2, err := e2e.Accept(d.Opened, s.d.Node, s.d.Noise)
	if err != nil {
		// Counted, not written: see Server.handshakes.
		s.handshakes.Add(1)
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	// Noise message 2 travels in the first GRANT of the side that accepted
	// the channel, and that is this one. Its credit is the window of the
	// contract: the shell's first inner message carries its input, and less
	// would make a shell wait for credit before anything of this channel is
	// even known to be real.
	if err := ch.Grant(wire.DefaultWindow, msg2); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	wait, cancel := context.WithTimeout(ctx, s.firstWait())
	defer cancel()
	if cl := s.await(wait, ch, sess); cl != nil {
		return s.end(ch, *cl)
	}
	// The shell is at the other end and its first message is in. Here the
	// service of the app is started, and the name of the service it runs on
	// is taken from the manifest of the decision
	// (d.Manifest.Action(…).Service), never from the OPEN: the OPEN is what
	// the API forwarded, and the manifest is what this controller wrote when
	// the app was activated. Until that is built there is no service to
	// reach, and the channel says so.
	return s.end(ch, wire.Error(wire.CodeUnreachable))
}

// await waits for the first inner message of the shell and answers with the
// CLOSE the channel ends with, or with nil once one has decrypted.
func (s *Server) await(ctx context.Context, ch *link.Channel, sess *e2e.Session) *wire.Close {
	m, err := ch.Recv(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return refuse(wire.CodeTimeout)
		}
		// The channel ended before the first message. At the end of a channel
		// Recv answers with its own end or with the context that was
		// cancelled with it, whichever the runtime takes first; both say the
		// same thing here, so neither is told apart.
		return refuse(wire.CodeProtocol)
	}
	if m.Kind != wire.TypeData {
		// An EOF only ends the transport: the data is cut off, and that is an
		// error and not an end (wire, "End to end").
		return refuse(wire.CodeProtocol)
	}
	if _, err := sess.Open(m.Data); err != nil {
		// Out of Open every failure is the shell's: a DATA that is no Noise
		// message of this session, or one whose plaintext is no inner
		// message.
		return refuse(wire.CodeProtocol)
	}
	return nil
}

// end closes the channel with cl and comes back with nil, so that no error of
// this package reaches the log of the tunnel (see the doc of this package).
// What Close answers is not passed on either: the channel is over, and the
// one CLOSE it gets is written by whoever got there first.
func (s *Server) end(ch *link.Channel, cl wire.Close) error {
	ch.Close(cl)
	return nil
}

// refuse is the CLOSE of this code, with the German message that belongs to it.
func refuse(code wire.Code) *wire.Close {
	cl := wire.Error(code)
	return &cl
}

// firstGrant is the credit this side gives the API on a core channel: enough
// for the input of the action, never more than wire.DefaultWindow and never
// less than the wire.MinWindow a first GRANT has to carry. The upper bound is
// what keeps the memory in hand: a tunnel may hold wire.MaxTunnelChannels
// numbers at once, so the window of one channel is paid for that many times
// over, and what a byte of credit costs this side in waiting frames is bounded
// by link (Channel.room).
func firstGrant(a core.Action) uint32 {
	w := a.MaxIn
	if w > wire.DefaultWindow {
		w = wire.DefaultWindow
	}
	if w < wire.MinWindow {
		w = wire.MinWindow
	}
	return uint32(w)
}

// firstWait is how long the shell has for its first inner message.
func (s *Server) firstWait() time.Duration {
	if s.first > 0 {
		return s.first
	}
	return wire.E2EFirstTimeout * time.Second
}

// now is the clock of the checks.
func (s *Server) now() int64 {
	if s.d.Now != nil {
		return s.d.Now()
	}
	return time.Now().Unix()
}

// log is where this package writes.
func (s *Server) log() *slog.Logger {
	if s.d.Log != nil {
		return s.d.Log
	}
	return slog.Default()
}
