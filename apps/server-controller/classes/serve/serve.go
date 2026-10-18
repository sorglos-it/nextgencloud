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
// watched all the same is counted (HandshakeFailures, StoreFailures).
//
// No line of this package is written per channel. A local fault an operator
// has to see — the trust list that cannot be kept — writes once and is counted
// from then on (trust.go), because how often a channel meets that fault is the
// API's to choose and how often it is written must not be.
//
// What this build carries out is ngc.users/controller/trust (trust.go) and
// the app channels of this controller from end to end (app.go): the handshake,
// the wait for the first inner message, and the service of the app. Since task
// 19 it carries out install as well (update.go), so the actions of this build
// are trust and install - Server.acts is the list. The core actions that
// remain (activate, deactivate, uninstall, delete-data, stage, license) come
// with the tasks that build them; until then a channel for one of them is
// refused instead of half served, and so is the licence of an app channel,
// which no build checks yet (runApp).
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
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Deps is what a Server needs. Everything in it belongs to the whole server
// and is used from the goroutine of every channel at once, so nothing here
// may be changed once New has seen it.
//
// What is not in it yet: the installer of app packages and the licence. Both
// are reached from a core action or from the licence check of an app channel,
// and neither is carried out here. They are added by the tasks that bring
// them, and a keyed Deps literal goes on compiling when they are.
type Deps struct {
	// Node is this controller's node ID in the trust list. It decides which
	// tasks are made out for this server (access) and it is hashed into the
	// prologue of every end-to-end channel (noisep.E2EPrologue), so a wrong
	// one lets no app channel through at all.
	Node string
	// Noise is this controller's own static Noise key pair, the one its node
	// carries in the trust list (trust.Node.Noise). e2e.Accept answers the
	// shell's message 1 with it.
	//
	// A zero key here lets no app channel come off at all: every handshake
	// fails, and it fails the way a shell that names another controller fails,
	// so nothing in the log tells the two apart. What says it is
	// HandshakeFailures: on a server without its key that counter takes every
	// app channel which reached the handshake, so it rises as fast as app
	// channels arrive instead of standing at the odd shell that opened the
	// wrong one, and that is the shape an operator reads it by.
	//
	// What it does not count is an app channel access turned away before the
	// handshake — one without a certificate, one for an app this controller
	// does not run, any of them while there is no trust list yet. Measured: an
	// app channel without a certificate ends E_FORBIDDEN and leaves the
	// counter at 0. The number is of the channels that got past the gate, not
	// of the app channels this server saw.
	Noise noise.DHKey
	// Engine runs the containers of the apps. nil is this program wired wrong
	// and ends every app channel with E_INTERNAL: which engine a server runs
	// is its configuration and not a peer's, so a channel that meets a
	// missing one says nothing else about itself.
	Engine runtime.Engine
	// Updater carries out ngc.store/controller/install: the parts of NGC, the
	// apps and the system packages of this server (update.go). nil is a
	// controller that was not built with an updater, and every install
	// channel ends E_INTERNAL.
	Updater Updater
	// Installer carries out ngc.store/controller/activate (activate.go). nil:
	// an activate channel ends E_INTERNAL.
	Installer Installer
	// MaxApps bounds how many app channels this controller runs at once,
	// over every tunnel it holds. 0 is DefaultMaxApps; a negative number is
	// no bound at all, which only a case sets.
	//
	// This is the bound the terminal task asked for and did not set
	// (docs/vertraege-v1.2.md, point 29: "Eine Obergrenze setzt dieses Paket
	// nicht; sie gehört in serve"). It sits on app channels and not on
	// terminals alone, because this package cannot tell the two apart before
	// the handshake, and refusing after the handshake would mean paying for
	// the channel first.
	MaxApps int
	// LogStderr lets the log of this server carry the text a service writes to
	// stderr. Off, the default, writes only that there was stderr and how
	// much: that text is the app's own and may hold what the user typed
	// (call.go, head). An operator switches it on to hunt a broken app and off
	// again afterwards.
	LogStderr bool
	// Chain holds the trust list. Every channel reads the newest verified
	// version once, at its start, and decides on that one to its end.
	Chain *chain.Keeper
	// State is this controller's local store: the apps it runs and the IDs of
	// the tasks that have run (access.Request), and the versions of the trust
	// list behind Chain.
	State *state.Store
	// TrustChanged is called once a version of the trust list was verified
	// and kept (trust.go). nil: nothing is told.
	//
	// It is the one place that knows the list GREW, and on a server with the
	// role web that is what Apache has to be told: apache.Writer.Sync is the
	// only half that puts a NEW API node into the balancer, the trust file and
	// web.json, while apache.Writer.Compare only renews the certificate of a
	// node that already stands there ("A node whose key is not in the file at
	// all is left to Sync, which owns the list").
	//
	// Measured on 17.10.2026 at a two-machine installation: a second API
	// joined and was taken into the trust list (anchor seq 1 -> 2), and
	// apache-apis.conf kept its ONE BalancerMember, api-trust.pem its one
	// certificate and web.json its one address, with the controller running.
	// Counted over the committed tree the same day: Sync had no caller outside
	// its own test file.
	//
	// It must do nothing slow, for the reason link.Config.TunnelUp must not:
	// it is called in the goroutine of the channel, and whoever is wired to it
	// only releases the work (node.wireUp sends on a channel of one place).
	TrustChanged func()
	// Now is the clock of the checks, in seconds. nil: time.Now().Unix().
	Now func() int64
	// Log is where this package writes. nil: slog.Default(). Nothing a peer
	// brings about is written here more than once, and almost all of it not at
	// all (see the doc of this package).
	Log *slog.Logger
}

// action is what carries out one core action of this controller: it gets the
// decision access made and the input that arrived before the EOF of the API,
// and answers either a result to encode or the CLOSE the channel ends with.
type action func(ctx context.Context, d access.Decision, in []byte) (any, *wire.Close)

// stream is what carries out one core action whose input is a file: instead of
// the input it gets the reader that hands it out as it arrives, and the trust
// list the channel was decided on, because what it hands the input to is built
// on that list (activate.go).
type stream func(ctx context.Context, list *trust.List, d access.Decision,
	r *chanReader) (any, *wire.Close)

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
	// acts is what this build carries out with its input held whole: trust
	// (trust.go) and install (update.go). The core actions of service
	// controller that remain — deactivate, uninstall, delete-data, stage and
	// license — are carried out by the tasks that build them; activate stands
	// in streams instead, because its input is a file. Only a test puts
	// something else in, to see a result this server would refuse.
	acts map[name]action
	// streams is what this build carries out with its input read as it flows:
	// activate (activate.go). The two maps are one namespace — core looks in
	// streams first and then in acts — so no action may stand in both, and
	// TestNoActionIsBothBufferedAndStreamed holds that.
	streams map[name]stream
	// first bounds the wait for the shell's first inner message. Only a test
	// sets it, to run the wait out in a fraction of a second; without it the
	// wire.E2EFirstTimeout of the contract holds.
	first time.Duration
	// last bounds the wait for the credit the end of an app channel needs
	// (Server.answer). Only a test sets it, for the same reason as first;
	// without it the wire.E2EFirstTimeout of the contract holds here too.
	last time.Duration
	// handshakes counts the end-to-end handshakes that did not come off. It
	// is counted and not written: e2e.ErrHandshake says nothing about whose
	// fault it was, so every one of them would be answered with E_INTERNAL in
	// the log as well — and a shell whose message 1 names another controller
	// puts a line there for every channel it opens.
	// TestAHandshakeThatFailsIsCountedAndNotWritten holds both halves.
	handshakes atomic.Uint64
	// storeFails counts the channels that met a trust list this server could
	// not keep. The first of them writes a line, the rest only count: the
	// fault is local and does not change from channel to channel, while how
	// many channels arrive is the API's to choose.
	// TestATrustListThatCannotBeKeptWritesOneLineForManyChannels and
	// TestTheFirstChannelThatCannotKeepTheTrustListWritesTheLine hold it.
	storeFails atomic.Uint64
	// apps counts the app channels running right now, over every tunnel of
	// this server. It rises before the end-to-end handshake and falls when
	// the channel is over, so what it bounds is the cost of a channel and not
	// the number of channels that were opened.
	apps atomic.Int64
	// appRefused counts the app channels this server turned away because
	// MaxApps was reached. Counted and not written: how many channels arrive
	// is the API's to choose, so a line per refusal would let a peer fill the
	// log of a server running as root.
	appRefused atomic.Uint64
}

// DefaultMaxApps is how many app channels one controller runs at once when
// Deps.MaxApps says nothing. It is a DECISION of this package, not a number of
// the contract, and this is the whole of its reckoning:
//
// One app channel holds, measured and counted in app.go and in point 29 of
// docs/vertraege-v1.2.md: up to wire.DefaultWindow (256 KiB) of DATA this side
// took credit for, the service.MaxStderr ring (64 KiB), one sealed payload (up
// to 65514 bytes), a pipe pair with three or four goroutines — and a process
// in the container, which is the real price. A terminal channel holds a PTY
// pair on top of that. 128 of them are therefore about 50 MiB of buffers and
// 128 container processes on a server that runs as root.
//
// Why not the two numbers that already exist: wire.MaxTunnelChannels (1024) is
// per tunnel and link enforces it, so seven tunnels of one installation are
// seven times that; and wire.MaxSessionChannels (64) is the API's to enforce
// per browser session, so a controller that took it for its own bound would
// let one session fill the whole server. This number is of the server.
//
// What it is NOT: a promise about memory. It bounds the channels, and what one
// channel costs is bounded elsewhere (app.go). The two together are the bound.
const DefaultMaxApps = 128

// New returns the server of these dependencies.
func New(d Deps) *Server {
	s := &Server{d: d}
	s.acts = map[name]action{trustKey: s.trust, installKey: s.install}
	// ONLY WHAT THIS BUILD REALLY CARRIES OUT goes in. An activate channel on
	// a server wired without an installer ends E_INTERNAL either way - with
	// the key it ends there inside the action (activate.go), without it at the
	// same code in core - so leaving the key out changes nothing an API sees.
	// What it changes is that Serves answers the question it is asked: a map
	// that holds the key regardless says this server carries out activations
	// that it refuses, and whoever wires it then reads back the literal it
	// passed to New instead of what the server got.
	s.streams = map[name]stream{}
	if d.Installer != nil {
		s.streams[activateKey] = s.activate
	}
	return s
}

// Serves says whether this build carries out that core action, in either map.
// It is for whoever WIRES this server: a program that built it can ask what it
// got instead of reading the literal it passed, and coreapp.Dispatcher.Serves
// on the API side answers the same question for the same reason.
func (s *Server) Serves(app, service, action string) bool {
	k := name{app, service, action}
	_, inActs := s.acts[k]
	_, inStreams := s.streams[k]
	return inActs || inStreams
}

// HandshakeFailures is how many end-to-end handshakes failed on this server
// since it started. Nothing of them reaches the log (see Server.handshakes),
// so this number is what says that they happen at all.
func (s *Server) HandshakeFailures() uint64 { return s.handshakes.Load() }

// StoreFailures is how many channels this server had to refuse because it
// could not keep its trust list. Only the first of them reached the log (see
// Server.storeFails), so this number is what says how long it has been going
// on.
func (s *Server) StoreFailures() uint64 { return s.storeFails.Load() }

// AppChannels is how many app channels this controller runs right now.
func (s *Server) AppChannels() int64 { return s.apps.Load() }

// AppRefusals is how many app channels this server turned away because its
// bound was reached. Nothing of them reaches the log (see Server.appRefused),
// so this number is what says that the bound is being met at all.
func (s *Server) AppRefusals() uint64 { return s.appRefused.Load() }

// maxApps is the bound of this server.
func (s *Server) maxApps() int {
	if s.d.MaxApps == 0 {
		return DefaultMaxApps
	}
	return s.d.MaxApps
}

// takeApp books one app channel against the bound and says whether it may
// run. A channel that got through gives its place back with dropApp.
//
// The counter rises first and is put back when it was one too many, rather
// than being read and then raised: between a read and a raise two channels
// both see room for one. What a refused channel costs is that the number
// stands one too high for the instant in between, and nothing reads it in
// that instant but another channel of the same kind, which is then refused
// one channel too early. TestTheBoundOfAppChannelsHoldsUnderManyAtOnce holds
// the bound itself over 64 channels arriving at once.
func (s *Server) takeApp() bool {
	max := s.maxApps()
	if max < 0 {
		return true
	}
	if s.apps.Add(1) > int64(max) {
		s.apps.Add(-1)
		s.appRefused.Add(1)
		return false
	}
	return true
}

// dropApp gives the place of one app channel back.
func (s *Server) dropApp() { s.apps.Add(-1) }

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
	list := s.d.Chain.List()
	d, cl := access.Check(ctx, access.Request{Payload: ch.OpenPayload(), List: list,
		Node: s.d.Node, Now: s.now(), Apps: s.d.State, Tasks: s.d.State})
	if cl != nil {
		return s.end(ch, *cl)
	}
	if d.Core {
		return s.core(ctx, ch, list, d)
	}
	return s.openApp(ctx, ch, d)
}

// core runs the channel of a core action of this controller: the first GRANT,
// the input up to the EOF of the API, the action, and its result as DATA, EOF
// and CLOSE ok.
func (s *Server) core(ctx context.Context, ch *link.Channel, list *trust.List,
	d access.Decision) error {
	key := name{d.Action.App, d.Action.Service, d.Action.Name}
	if flow, ok := s.streams[key]; ok {
		return s.coreStream(ctx, ch, list, d, flow)
	}
	run, ok := s.acts[key]
	if !ok {
		// access let the channel through, so the action is one of this
		// controller's — this build just does not carry it out yet. Nothing
		// is read and nothing is granted: a channel that cannot be served is
		// over before it costs this server a byte.
		//
		// The same state has two codes, and that is worth knowing while it
		// lasts: with a trust list a stage or license channel gets here and
		// ends E_INTERNAL, without one it never gets past access.firstChain
		// and ends E_UNREACHABLE. An install channel does NOT reach this
		// line any more: it stands in Server.acts since task 19, and an
		// activate channel stands in Server.streams since this one. Both mean
		// "this build does not carry that action out".
		// Once the tasks that bring them are in,
		// the line above is gone and with it the difference; until then the
		// second code is the honest one of the two, because without a list
		// this server really cannot be reached for anything but its first
		// chain.
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
	// SendStream and not SendWhole, deliberately: this is the result of a core
	// channel, and there the contract reads it as all DATA up to the EOF of
	// this side ("Core apps", wire). It may be broken into as many frames as
	// the credit of the moment allows, and nothing on the other side has to
	// put a single frame back together.
	if err := ch.SendStream(b); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	if err := ch.EOF(); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	return s.end(ch, wire.Close{Status: wire.StatusOK})
}

// coreStream runs the channel of a core action whose input is a file: the
// first GRANT, the action with the input as a reader, and its result as DATA,
// EOF and CLOSE ok.
//
// What it does NOT do is read the input itself, and that is the difference to
// core: the action gets the reader and decides how much of the input it ever
// holds (stream.go). What is left unread when the action comes back is read
// out by the ACTION, not here: only the action knows whether it is done, and
// only the rest of the input says whether the channel was cut off or overran
// (chanReader.drain, and serve.Server.activate is the one action that has to
// do it).
func (s *Server) coreStream(ctx context.Context, ch *link.Channel, list *trust.List,
	d access.Decision, flow stream) error {
	// The first GRANT is the credit the API sends its input with; what this
	// side may send back is the window of the OPEN. A core channel carries no
	// handshake in that GRANT (wire, "End to end").
	if err := ch.Grant(firstGrant(d.Action), nil); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	v, cl := flow(ctx, list, d, newChanReader(ctx, ch, d.Action))
	if cl != nil {
		return s.end(ch, *cl)
	}
	b, err := core.EncodeResult(v)
	if err != nil {
		// The same line as in core, and for the same reason: only the size
		// of the result can bring this about, and the contract names
		// E_TOOLARGE for it.
		return s.end(ch, wire.Error(wire.CodeTooLarge))
	}
	if err := ch.SendStream(b); err != nil {
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
//
// It keeps the whole input in memory, and it does so for every action alike.
// What this build serves is bounded by the MaxIn of ngc.users/controller/trust,
// 33554432 bytes — but a tunnel may hold wire.MaxTunnelChannels numbers at
// once, so even that is 32 GiB in a process running as root. The controller
// actions of core.All that carry a file are already written and only not
// served yet: ngc.store/controller/stage and activate stand at 4303355908
// bytes each (core.maxTransferIn, measured). Whoever serves them reads the
// input of a FlowTransfer channel as it flows instead of holding it, and this
// function is where that fork belongs.
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

// lastWait is how long the shell has to take the end of this side: as long as
// it had for its own first message. A number of its own would be one more
// thing to keep in step with the contract, and nothing says it should differ.
func (s *Server) lastWait() time.Duration {
	if s.last > 0 {
		return s.last
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
