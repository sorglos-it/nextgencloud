// Package relay forwards a channel of the shell to a controller. The OPEN of
// the browser goes into tunnel.Open byte for byte, DATA and EOF one to one,
// CLOSE with the same code. Credit is kept per hop, and the API's first GRANT
// to the browser waits for the controller's, so an end-to-end handshake can
// carry Noise message 2 along (package wire, "Relay").
//
// What may be cut into pieces and what may not, per hop. This is the one
// question of this package that costs a measured bug when it is answered
// wrongly, and the answer is written at each place as well:
//
//   - Shell -> controller DATA and controller -> shell DATA: never cut and
//     never joined. On an app channel each payload is one end-to-end Noise
//     transport message, and a message that arrives in two frames or two
//     messages that arrive in one do not decrypt. The failure then reads as
//     "cipher: message authentication failed", which names the wrong layer.
//     Measured in the controller on 03.10.2026 and reported to this strand,
//     not measured here; what is measured here is that a payload travels
//     whole or not at all. sendWhole therefore
//     waits for credit for the whole payload instead of sending what the
//     credit of the moment allows.
//   - The transfer header and the file the API writes itself (FlowTransfer):
//     cut freely at wire.MaxPayload. That hop is a byte stream which
//     core.ReadTransfer reads by its length prefix, so no frame boundary
//     carries meaning.
//   - The input of CallController and Transfer: cut freely, same reason.
//   - The result the API collects for itself: joined freely, same reason.
//
// This package never reads a socket. Both sides hand it frames out of the
// buffer their own reader fills (Shell.Next, Hop.Next), which is why the waits
// here may carry a deadline: a deadline on a read of coder/websocket closes the
// whole link, not the read. That the seams really are buffers and not the
// socket is a condition on whoever implements them; no case of this package can
// see it.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The clocks of this package. All of them are policy, not protocol: no byte on
// the line depends on them. They are deliberately of three different lengths,
// and none of them is 30 or 90 seconds (wire.E2EFirstTimeout,
// link.FirstFrameTimeout, controllers.IdleTimeout): two clocks of the same
// length turn two error codes into a race, which this project has measured.
const (
	// GrantWait bounds one wait for credit on a hop. Every wait needs a bound,
	// and this one needs it twice over: a payload that may not be cut waits for
	// the credit of a whole frame, and the side that owes the credit is the
	// other end of a channel the user is waiting on. Nothing of the channel's
	// lock is held while it waits (waitSpend), because an unbounded wait under
	// the channel's lock holds the channel past the clock of the action -- also
	// measured on 03.10.2026.
	GrantWait = 25 * time.Second

	// CloseDrain bounds how long the number of a hop channel waits for the
	// controller's CLOSE after the API's own. Without it the number would be
	// held for the life of the tunnel, and wire.MaxTunnelChannels of them end
	// the tunnel for everybody.
	CloseDrain = 10 * time.Second

	// CallTimeout bounds a whole call the API makes for itself
	// (CallController, Transfer). Such a call has no user waiting at a socket
	// that would end it, so it is the only thing that ends it.
	CallTimeout = 50 * time.Second
)

// Shell is the browser side of a forwarded channel: the few things package
// browser lets a dispatcher do with a session. Next hands the next frame of
// this channel out of the buffer the session's own reader fills, and answers
// false once the session is gone -- it never reads the socket itself.
type Shell interface {
	SendGrant(ctx context.Context, c *link.Channel, credit uint32, handshake []byte) error
	SendData(ctx context.Context, c *link.Channel, payload []byte) error
	SendEOF(ctx context.Context, c *link.Channel) error
	CloseWith(ctx context.Context, c *link.Channel, cl wire.Close) error
	Close(ctx context.Context, code wire.Code, num uint32) error
	Next(ctx context.Context, c *link.Channel) (wire.Frame, bool)
}

// Hop is the controller side: a number on the tunnel, one typed way out per
// frame type, and the next frame of a channel out of the tunnel's buffer.
//
// Two things are deliberately absent, and their absence is what the compiler
// holds (controllers/hop.go):
//
//   - No channel table. *link.Table carries Deliver, which is what PUTS a
//     frame into a channel's queue, so a relay that held the table could hand
//     itself frames the controller never sent -- and every sentence of this
//     package rests on the frames being the controller's. Open and Get are
//     what is left of it.
//   - No writer for a frame of this package's own choosing. A GRANT exists
//     only through Grant, which raises what may arrive first, and DATA only
//     through SendData, which spends the credit first. This package therefore
//     cannot send what it did not count, and SendClose notes the CLOSE itself.
//
// What ARRIVES is counted by the tunnel's own reader, the only code that sees
// a frame arrive, so this package must not count it a second time.
type Hop interface {
	Open(window uint32) (*link.Channel, error)
	Get(num uint32) (*link.Channel, bool)
	SendOpen(ctx context.Context, c *link.Channel, payload []byte) error
	SendData(ctx context.Context, c *link.Channel, payload []byte) error
	SendEOF(ctx context.Context, c *link.Channel) error
	SendClose(ctx context.Context, c *link.Channel, cl wire.Close) error
	Grant(ctx context.Context, c *link.Channel, credit uint32) error
	Next(ctx context.Context, c *link.Channel) (wire.Frame, bool)
}

// Relay forwards channels.
type Relay struct {
	S       *state.State
	Tunnels *controllers.Registry
	Log     *audit.Log

	// Drain is how long one relay waits for a controller's CLOSE, Wait how long
	// for credit on a hop; zero or less means CloseDrain and GrantWait. A test
	// sets them to milliseconds instead of waiting.
	Drain time.Duration
	Wait  time.Duration
}

// Forwarded is what the dispatcher knows about one channel of the shell.
type Forwarded struct {
	// Raw is the payload of the browser's OPEN frame, byte for byte. It is what
	// goes into tunnel.Open.Open, and nothing else can take its place: the
	// end-to-end prologue hashes these bytes, and an OPEN that was decoded and
	// encoded again is a different byte string. wire.Frame.Payload aliases the
	// buffer the link reads into, so whoever fills this field copies it; this
	// package copies it again at the door, which no case of it can measure.
	Raw []byte
	// Open is the same OPEN, parsed by wire.ParseOpen. It decides nothing that
	// travels; it is read for the app (core app or not), for whether the
	// channel is end to end, and for the flow of a core action.
	Open   wire.Open
	Task   *task.Task
	Node   string
	Client []byte       // client certificate payload, as the user's proof covers it
	Proof  *proof.Proof // the user's proof over Client
	// Head and File are the transfer the API writes itself on a FlowTransfer
	// channel the shell opened. Nothing fills them yet: task 22 builds the
	// header in appstore.buildTransfer and opens a channel of its own
	// (appstore.Activate, through Transfer), so a dispatcher routes an
	// activation there instead of here. These two fields are the other way of
	// doing it and are still open. On such a
	// channel the shell sends no DATA at all.
	Head *core.Transfer
	File io.Reader
}

// coreRelay is the interface package coreapp hands its channels to. It is
// written out here, instead of imported, for two reasons: this package must not
// depend on the dispatcher it serves, and the compiler then holds the sentence
// "Relay fits coreapp" from this side -- a lint over names would not. It is a
// copy, so it holds only while this copy and the original say the same; coreapp
// owns the original, and a change there has to come here too. Its mirror image
// is coreapp/seam_test.go, which says the same about Shell and Hop from the
// other side.
type coreRelay interface {
	Forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open, raw []byte,
		t *task.Task, node string)
}

var _ coreRelay = (*Relay)(nil)

// Forward runs one channel of the shell against the controller on node. raw is
// the payload of the browser's OPEN frame and travels untouched -- see
// Forwarded.Raw, and do not "tidy" it into a re-encoded OPEN.
//
// One named gap is left, and it is not this package's to close: a forwarded
// channel of a core action with flow transfer needs the header and the file.
// Forward has neither, so such a channel ends with E_INTERNAL rather than
// sending the shell's bytes as if they were the file. Everything else runs.
// Task 22 did not close it this way either: appstore.Activate puts the header
// together and opens its own channel through Transfer, so an activation is to
// be routed there and not through Forward.
func (rl *Relay) Forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open,
	raw []byte, t *task.Task, node string) {
	code := rl.forward(ctx, s, c, o, raw, t, node)
	if rl.Log == nil {
		return
	}
	_ = rl.Log.Channel(ctx, s.Live.Verified.Cert().User, o.App, o.Action, node, code)
}

// forward finds the tunnel and runs the channel; it answers the code the
// channel ended with, "" for one that ended in order.
func (rl *Relay) forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open,
	raw []byte, t *task.Task, node string) wire.Code {
	tun, ok := rl.Tunnels.Get(node)
	if !ok {
		// No tunnel to that controller right now; the session stays.
		_ = s.Close(ctx, wire.CodeUnreachable, c.Num)
		return wire.CodeUnreachable
	}
	// The certificate and the proof of this session, exactly as the proof
	// covers them. The row was written by this API and the proof was checked at
	// login; it goes through its own Validate again because nothing travels to
	// a controller from here unchecked, and because a row that cannot answer it
	// would otherwise become an OPEN the controller refuses by ending the whole
	// tunnel -- every other channel on it with it.
	var pr proof.Proof
	if json.Unmarshal(s.Live.Row.Proof, &pr) != nil || pr.Validate() != nil {
		_ = s.Close(ctx, wire.CodeInternal, c.Num)
		return wire.CodeInternal
	}
	return rl.ForwardRaw(ctx, s, c, tun, Forwarded{Raw: raw, Open: o, Task: t, Node: node,
		Client: s.Live.Row.Cert, Proof: &pr})
}

// chanState carries what the two directions of one channel have to tell each
// other. Both goroutines touch it, so everything in it sits behind the lock.
type chanState struct {
	mu     sync.Mutex
	code   wire.Code // the code the channel must end with, "" while it runs
	cancel context.CancelFunc

	// pass is a CLOSE of the shell that travels to the controller as it stands.
	// It is a whole wire.Close and not a code, and that is the point: a CLOSE
	// with status ok has no code at all, and one with an error carries a
	// message and perhaps an exit code that are the controller's to see.
	pass *wire.Close

	// sent counts what has gone to the shell on a core-app channel, for
	// wire.MaxCallResult.
	sent int

	// credit wakes the sending direction once a GRANT of the controller has
	// raised the credit of the hop. One place buffered and never waited on:
	// the reader must not be held up by a sender that is busy, and a wake-up
	// already pending says the same as a second one.
	credit chan struct{}
}

// newChanState returns the shared half of one channel.
func newChanState(cancel context.CancelFunc) *chanState {
	return &chanState{cancel: cancel, credit: make(chan struct{}, 1)}
}

// wakeCredit says that the credit of the hop has grown.
func (st *chanState) wakeCredit() {
	select {
	case st.credit <- struct{}{}:
	default:
	}
}

// fail notes the code the channel ends with and wakes both directions. Only the
// first code counts: whoever got there first said what happened, and a second
// code would be a race between two clocks.
func (st *chanState) fail(code wire.Code) {
	st.mu.Lock()
	if st.code == "" {
		st.code = code
	}
	st.mu.Unlock()
	st.cancel()
}

// ended returns the code noted so far.
func (st *chanState) ended() wire.Code {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.code
}

// setPass notes the CLOSE of the shell and ends the other direction. Nothing
// is noted as an error with it: the shell has said what happened, and a code of
// our own on top would be a second answer.
func (st *chanState) setPass(cl wire.Close) {
	st.mu.Lock()
	if st.pass == nil {
		st.pass = &cl
	}
	st.mu.Unlock()
	st.cancel()
}

// passOn returns the CLOSE of the shell, nil while there is none.
func (st *chanState) passOn() *wire.Close {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.pass
}

// count adds n bytes to what went to the shell and reports whether that is
// still within wire.MaxCallResult.
func (st *chanState) count(n int) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sent+n > wire.MaxCallResult {
		return false
	}
	st.sent += n
	return true
}

// ForwardRaw forwards one channel of the shell to one controller and returns
// the code it ended with, "" when the channel ended in order -- which is
// either side's own CLOSE, because neither needs an answer from us then. It is
// what Forward will be once the two seams named there exist.
func (rl *Relay) ForwardRaw(ctx context.Context, sh Shell, c *link.Channel, hop Hop,
	fw Forwarded) wire.Code {
	// The window we grant the controller is what we can hold until the browser
	// takes it, never more.
	tc, err := hop.Open(wire.DefaultWindow)
	if err != nil {
		// Only this tunnel is full; the session stays. Every other error of
		// Open is our own mistake about the window, which is a constant here.
		code := wire.CodeLimit
		if !errors.Is(err, link.ErrLimit) {
			code = wire.CodeInternal
		}
		_ = sh.Close(ctx, code, c.Num)
		return code
	}
	code, hopClose := rl.openAndPump(ctx, sh, c, hop, tc, fw)
	// The shell first, then the tunnel: the user's answer must not wait for a
	// controller that may take CloseDrain to say goodbye.
	if code != "" {
		_ = sh.Close(ctx, code, c.Num)
	}
	rl.endHop(ctx, hop, tc, hopClose)
	return code
}

// openAndPump writes the OPEN and runs both directions. It answers the code
// the channel ends with -- "" when one side's own CLOSE did that, so that side
// needs no answer -- and the CLOSE the tunnel hop is to be ended with.
func (rl *Relay) openAndPump(ctx context.Context, sh Shell, c *link.Channel, hop Hop,
	tc *link.Channel, fw Forwarded) (wire.Code, wire.Close) {
	// A copy, because wire.Frame.Payload aliases the buffer the link reads
	// into: whoever handed Raw over may have read the next frame by now.
	raw := bytes.Clone(fw.Raw)
	payload, err := json.Marshal(tunnel.Open{Open: raw, Window: wire.DefaultWindow,
		Client: fw.Client, Proof: fw.Proof})
	if err != nil {
		return wire.CodeInternal, wire.Error(wire.CodeInternal)
	}
	// Our own payload through the parser of the contract, which is the one
	// place that bounds it: an OPEN of the shell that still fits
	// wire.MaxBrowserOpen can grow past wire.MaxOpenPayload once the
	// certificate and the proof are in it, and then the controller would end
	// the tunnel over a frame of ours.
	if _, _, err := tunnel.ParseOpen(payload); err != nil {
		return wire.CodeInternal, wire.Error(wire.CodeInternal)
	}
	if err := hop.SendOpen(ctx, tc, payload); err != nil {
		return wire.CodeUnreachable, wire.Error(wire.CodeUnreachable)
	}
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st := newChanState(cancel)
	code, theirClose := rl.pump(pctx, sh, c, hop, tc, fw, st)
	switch {
	case theirClose:
		// The controller closed and the shell has that CLOSE already; ours
		// only answers it, which the contract asks for.
		return "", wire.Close{Status: wire.StatusOK}
	case st.passOn() != nil:
		// The shell closed. Its CLOSE goes on as it stands, and the shell
		// needs no answer from us.
		return "", *st.passOn()
	case code == "":
		// Neither side closed and no direction noted a code, so the tunnel
		// itself is gone.
		code = wire.CodeUnreachable
	}
	return code, wire.Error(code)
}

// pump carries the controller's frames to the shell until one side closes. It
// starts the other direction after the controller's first GRANT and answers
// with the code to end on, plus whether the controller's own CLOSE arrived.
func (rl *Relay) pump(ctx context.Context, sh Shell, c *link.Channel, hop Hop, tc *link.Channel,
	fw Forwarded, st *chanState) (wire.Code, bool) {
	e2e := len(fw.Open.E2E) != 0
	// Only a core-app channel has a result the API can measure. On an
	// end-to-end channel the payload is a Noise message, so wire.MaxCallResult
	// is the shell's to enforce (package wire, "Results").
	bounded := wire.CoreApp(fw.Open.App)
	first := true
	for {
		f, ok := hop.Next(ctx, tc)
		if !ok {
			// Either the tunnel is gone or the other direction ended the
			// channel and cancelled this one.
			return st.ended(), false
		}
		switch f.Type {
		case wire.TypeGrant:
			// Read for the handshake only. The credit was already added by the
			// tunnel's own reader, the one place that sees a frame arrive
			// (controllers.Tunnel.frame); adding it here as well would count it
			// twice, and the hop would believe it may send more than the
			// controller allowed. A payload ParseGrant refuses never reaches
			// this queue either, because that reader ends the tunnel over it --
			// the branch stays because this function is what reads the
			// handshake out, and it must not rest on another package's checks
			// staying what they are.
			credit, handshake, err := wire.ParseGrant(f.Payload)
			if err != nil {
				return wire.CodeProtocol, false
			}
			// More credit on the hop is what the other direction may be
			// waiting for.
			st.wakeCredit()
			if first {
				// An end-to-end channel carries Noise message 2 in exactly
				// this GRANT and in no other; a core-app channel carries none
				// at all.
				if e2e != (len(handshake) != 0) {
					return wire.CodeProtocol, false
				}
				// Our first GRANT to the browser: the window we can hold, and
				// the controller's handshake with it.
				if err := sh.SendGrant(ctx, c, wire.DefaultWindow, handshake); err != nil {
					return wire.CodeInternal, false
				}
				first = false
				go rl.toController(ctx, sh, c, hop, tc, fw, st)
				continue
			}
			if len(handshake) != 0 {
				// A handshake in any GRANT but the first is a protocol error.
				return wire.CodeProtocol, false
			}
			if err := sh.SendGrant(ctx, c, credit, nil); err != nil {
				return wire.CodeInternal, false
			}
		case wire.TypeData:
			// Not counted here either: the tunnel's reader took these bytes off
			// what the controller may send before it queued the frame.
			//
			// The limit is read before anything is passed on, so not one byte
			// over it reaches the shell.
			if bounded && !st.count(len(f.Payload)) {
				return wire.CodeTooLarge, false
			}
			// One payload, one frame: see the package comment.
			if err := sh.SendData(ctx, c, f.Payload); err != nil {
				return wire.CodeInternal, false
			}
			// Give the controller its credit back now the browser has it.
			if err := hop.Grant(ctx, tc, uint32(len(f.Payload))); err != nil {
				return wire.CodeInternal, false
			}
		case wire.TypeEOF:
			if err := sh.SendEOF(ctx, c); err != nil {
				return wire.CodeInternal, false
			}
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				return wire.CodeProtocol, false
			}
			// Not noted here: the tunnel's reader noted the controller's CLOSE
			// before it queued the frame, exactly as it counted the credit.
			//
			// The CLOSE travels unchanged, so the shell sees what the
			// controller said -- code, message and exit code.
			_ = sh.CloseWith(ctx, c, cl)
			return "", true
		default:
			// Only the API opens channels on a tunnel, so an OPEN from the
			// controller is a protocol error like any unknown type.
			return wire.CodeProtocol, false
		}
	}
}

// toController carries the shell's frames to the controller. It runs beside
// pump, which is why everything the two share sits in chanState.
func (rl *Relay) toController(ctx context.Context, sh Shell, c *link.Channel, hop Hop,
	tc *link.Channel, fw Forwarded, st *chanState) {
	transfer := isTransfer(fw.Open)
	if transfer {
		if fw.Head == nil || fw.File == nil {
			// Nobody put the transfer together; forwarding the shell's bytes
			// instead is not an option, because they are not the file.
			st.fail(wire.CodeInternal)
			return
		}
		// writeTransfer ends with the EOF that closes the API's input, so
		// nothing more goes out on this side.
		if code := rl.writeTransfer(ctx, hop, tc, st, *fw.Head, fw.File); code != "" {
			st.fail(code)
			return
		}
	}
	for {
		f, ok := sh.Next(ctx, c)
		if !ok {
			// The session is gone or the other direction ended the channel.
			return
		}
		switch f.Type {
		case wire.TypeData:
			if transfer {
				// On a forwarded transfer channel the API writes the header and
				// the file; DATA of the shell is a protocol error (package
				// wire, "Relay").
				st.fail(wire.CodeProtocol)
				return
			}
			if code := rl.sendWhole(ctx, hop, tc, st, f.Payload); code != "" {
				st.fail(code)
				return
			}
			// The shell gets its credit back once the controller has the bytes,
			// not before: what we grant is what we can hold.
			if err := sh.SendGrant(ctx, c, uint32(len(f.Payload)), nil); err != nil {
				st.fail(wire.CodeInternal)
				return
			}
		case wire.TypeEOF:
			if transfer {
				// The shell's EOF right after the first GRANT is how a transfer
				// channel is meant to run; ours went out above.
				continue
			}
			if err := hop.SendEOF(ctx, tc); err != nil {
				st.fail(wire.CodeUnreachable)
				return
			}
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				st.fail(wire.CodeProtocol)
				return
			}
			// The shell has said it; it needs no answer, and the CLOSE travels
			// to the controller as it stands.
			st.setPass(cl)
			return
		default:
			st.fail(wire.CodeProtocol)
			return
		}
	}
}

// isTransfer reports whether o is a core action with flow transfer that the
// shell may open. On such a channel the API writes header and file itself.
func isTransfer(o wire.Open) bool {
	a, ok := core.Lookup(o.App, o.Service, o.Action)
	return ok && a.Flow == core.FlowTransfer && core.ShellOpens(a)
}

// sendWhole sends one payload as exactly one DATA frame, waiting for credit for
// all of it. See the package comment for why it is never cut.
func (rl *Relay) sendWhole(ctx context.Context, hop Hop, tc *link.Channel, st *chanState,
	payload []byte) wire.Code {
	if len(payload) > wire.MaxPayload {
		// Not reachable through link.Read, which decodes nothing larger; kept
		// because this function is the one that promises a whole frame.
		return wire.CodeProtocol
	}
	if !rl.waitCredit(ctx, tc, st, len(payload)) {
		return wire.CodeTimeout
	}
	// Asked first and spent inside SendData, in two steps, and that is safe
	// here for one reason only: a hop channel has exactly one sender -- the
	// goroutine of toController, or the one collect starts -- so nothing takes
	// the credit in between. What changes it otherwise is a GRANT, and more
	// credit never makes this fail.
	if err := hop.SendData(ctx, tc, payload); err != nil {
		return wire.CodeUnreachable
	}
	return ""
}

// waitCredit waits until n bytes fit the credit of a hop channel, at most
// GrantWait. It does not poll: a GRANT of the controller wakes it through
// chanState, and the reader that counts that GRANT is the only thing that can
// raise the credit at all. One source and no second way beside it -- a ticker
// next to this would be that second way.
//
// No lock is held while it waits: link.Channel's own lock is taken and given
// back inside SendAllowed, once per wake-up.
func (rl *Relay) waitCredit(ctx context.Context, tc *link.Channel, st *chanState, n int) bool {
	if tc.SendAllowed(n) {
		return true
	}
	wctx, cancel := context.WithTimeout(ctx, rl.wait())
	defer cancel()
	for {
		select {
		case <-wctx.Done():
			return false
		case <-st.credit:
			if tc.SendAllowed(n) {
				return true
			}
		}
	}
}

// endHop closes the hop channel and waits for the controller's CLOSE, so the
// number is free again. A number that is never given back counts toward
// wire.MaxTunnelChannels for the life of the tunnel.
func (rl *Relay) endHop(ctx context.Context, hop Hop, tc *link.Channel, cl wire.Close) {
	if tc.Closing() {
		// Nothing in this package sends a CLOSE for a hop channel anywhere
		// else, so this is only the guard that keeps the sentence true.
		return
	}
	// SendClose notes the CLOSE itself, so there is no way to send one here and
	// forget to note it.
	if err := hop.SendClose(ctx, tc, cl); err != nil {
		// The tunnel is gone, and its table with it.
		return
	}
	// When the controller closed first, its CLOSE is already counted and the
	// number is free the moment ours is noted: link.Table drops it and Get no
	// longer answers with this channel. Waiting then would hold this goroutine
	// for the whole drain on every channel that ends in order -- measured by
	// TestNormalCloseGivesTheNumberBackAtOnce, which runs with the real clock.
	if cur, ok := hop.Get(tc.Num); !ok || cur != tc {
		return
	}
	dctx, cancel := context.WithTimeout(ctx, rl.drain())
	defer cancel()
	for {
		f, ok := hop.Next(dctx, tc)
		if !ok {
			return
		}
		// Frames after our own CLOSE are dropped without an answer, and they
		// have to be taken out of the queue all the same: a queue nobody
		// empties reaches link.MaxQueuedFrames, and then the tunnel's reader
		// ends the TUNNEL -- every other channel on it with it. The
		// controller's CLOSE is noted by that same reader, not here.
		if f.Type == wire.TypeClose {
			return
		}
	}
}
