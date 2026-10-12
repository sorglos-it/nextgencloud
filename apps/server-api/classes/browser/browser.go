// Package browser carries the session link to a shell: the WebSocket at
// session.PathSocket with Noise inside, the channels the browser opens and the
// control messages of channel 0. The static keys of this Noise session are the
// session's own keys: the browser's ClientCert.DH and the API's APICert.DH,
// whose private half lies sealed in the database, so any API can serve any
// session.
//
// This package is the one caller of link.Table on a session link, and it keeps
// the table's caller conditions. Both of them are held by the compiler, and
// that took two steps: the link is the unexported field Session.link, and since
// task 12 the table is the unexported field Session.table and link.Table holds
// a number only through FailChannel, which writes the CLOSE and notes it in one call.
// A dispatcher outside this package therefore reaches neither the frames nor the
// counters, and the sentences below are no longer a rule this package has to
// remember.
//
// Who keeps them, and by what:
//
//   - "Allow runs before every GRANT that goes out, and the GRANT goes out only
//     when Allow said true" is held by the compiler: the link itself is the
//     unexported field Session.link, so no package outside this one can write a
//     frame at all. The ways out are SendGrant, SendData, SendControl and Close,
//     and SendGrant is the only one that builds a GRANT. A dispatcher such as
//     coreapp therefore cannot go around Allow -- it has nothing to write with.
//     Inside this package the compiler cannot help, and lint_test.go walks the
//     package's syntax tree to say that no second function builds a GRANT frame.
//     That walk is a lint and not the holder of the sentence, and it has two
//     named limits of its own (lint_test.go): it reads the files on disk, so a
//     build that replaces one of them (go test -overlay) is invisible to it, and
//     it matches a name and not a value. The holder is the unexported field.
//   - "Every held number is followed by a CLOSE noted for the same number", so
//     a number held for a CLOSE E_PROTOCOL is given back once the other side's
//     CLOSE arrives. This one is held by link.Table.FailChannel: holding a number
//     alone is unexported there, and FailChannel writes the frame and notes the CLOSE
//     in one call. TestProtocolErrorGivesTheNumberBackAfterTheBrowsersClose
//     measures the effect on a real link.
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrGrant reports a credit this side may not grant: zero, or one that would
// lift what the other side may send above wire.MaxWindow. Both would be our own
// protocol error, which the other side answers with CLOSE E_PROTOCOL
// (shared/README.md), so the frame never goes out and the caller hears about it
// instead.
var ErrGrant = errors.New("browser: credit outside the window")

// ErrCredit reports DATA this side may not send yet: more bytes than the browser
// has granted on that channel. Sending it anyway would be our own protocol
// error, which the browser answers with CLOSE E_PROTOCOL, so nothing goes out
// and the caller hears about it.
var ErrCredit = errors.New("browser: not enough credit for this much data")

// Opener takes a channel the browser opened; package coreapp implements it.
//
// raw is the payload of the OPEN frame, byte for byte as it arrived, and it is
// handed over next to the parsed o because the two are not the same thing: an
// OPEN that was decoded and encoded again is a DIFFERENT byte string (member
// order, spacing), and the end-to-end prologue of a forwarded channel hashes
// exactly these bytes (noisep.E2EPrologue). A dispatcher that only had o could
// not forward a channel at all -- the controller would answer the rebuilt OPEN
// with a decryption error, which names the wrong layer. Whoever tidies raw
// away breaks every app channel.
//
// raw is a copy of its own, so it survives the next read of the link
// (wire.Frame.Payload points into the link read buffer).
type Opener interface {
	OpenChannel(ctx context.Context, s *Session, c *link.Channel, o wire.Open, raw []byte)
}

// Session is one open session link.
type Session struct {
	Live *login.Live

	// table is unexported for the same reason as link, and that is the second
	// condition of link.Table: whoever cannot reach the counters cannot raise
	// what may arrive, spend what may be sent or note a CLOSE that never went
	// out. What a dispatcher needs of it, it gets as a channel of its own from
	// OpenChannel.
	table *link.Table

	// link is unexported on purpose, and that is what holds the first condition
	// of link.Table: whoever cannot reach the link cannot send a GRANT without
	// Allow. Everything that writes to it lives in this package -- SendGrant,
	// SendData, SendControl, Close, End -- and a dispatcher gets those and
	// nothing else.
	link *link.Link
}

// Server serves session.PathSocket.
type Server struct {
	S        *state.State
	Login    *login.Handler
	Open     Opener
	Sessions *Registry
}

// Socket upgrades a request with a session to a link and runs it.
func (srv *Server) Socket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	live, code, ok := srv.Login.Live(ctx, r)
	if !ok {
		httpapi.Fail(w, code)
		return
	}
	pub := apiPublic(live)
	if len(pub) == 0 {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	// live.Trust is the list this session was decided on, brought up to date by
	// Live itself; asking state.Trust again would add a failure path and could
	// hand back an older cached list than the one the session was just judged
	// against.
	l, err := link.Accept(w, r, link.Options{
		Subprotocol: session.Subprotocol,
		Prologue:    noisep.PrologueSession,
		Static:      noise.DHKey{Private: live.APIKey, Public: pub},
		Peer:        live.Verified.Cert().DH,
		Origins:     origins(live.Trust.List),
	})
	if err != nil {
		// Accept answered the request itself wherever it could; a list of
		// origins it refuses is the one way out that wrote nothing, and that is
		// a misconfigured server, not a bad request.
		return
	}
	s := &Session{Live: live, table: link.NewTable(wire.MaxSessionChannels, false), link: l}
	srv.Sessions.Add(s)
	defer srv.Sessions.Remove(s)
	srv.run(ctx, s)
}

// apiPublic returns the API's public session key from the stored certificate,
// so the Noise key pair matches what the shell got in session.LoginResult. The
// row was written by this API; a value it cannot read means the row is broken,
// and the caller answers E_INTERNAL rather than upgrading to a link whose
// handshake could only fail.
func apiPublic(live *login.Live) []byte {
	var env sign.Envelope
	if json.Unmarshal(live.Row.APICert, &env) != nil {
		return nil
	}
	var cert session.APICert
	if json.Unmarshal(env.Payload, &cert) != nil {
		return nil
	}
	return cert.DH
}

// origins are the Origin values the WebSocket accepts: those of the trust
// list, which are the ones the shell is served from. The cookie is
// SameSite=Strict besides, so a page of another site carries no session.
func origins(l *trust.List) []string { return l.RP.Origins }

// run reads the link until it ends.
func (srv *Server) run(ctx context.Context, s *Session) {
	// Whoever serves a channel waits in link.Channel.Next; once the link is
	// gone they hear that at once instead of waiting out their own clock.
	defer s.table.Hangup()
	for {
		f, err := s.link.Read(ctx)
		if err != nil {
			return
		}
		if f.Channel == wire.ControlChannel {
			if !srv.control(ctx, s, f) {
				s.End("bye")
				return
			}
			continue
		}
		if !srv.channel(ctx, s, f) {
			s.End("protocol")
			return
		}
	}
}

// channel handles one frame of a numbered channel; false ends the link.
func (srv *Server) channel(ctx context.Context, s *Session, f wire.Frame) bool {
	switch f.Type {
	case wire.TypeOpen:
		o, err := wire.ParseOpen(f.Payload)
		if err != nil {
			return s.protocolError(ctx, f.Channel)
		}
		c, err := s.table.Open(f.Channel, o.Window)
		if errors.Is(err, link.ErrLimit) {
			return false
		}
		if err != nil {
			return s.protocolError(ctx, f.Channel)
		}
		// A copy of the payload, because it points into the buffer the link
		// reads into and the dispatcher below runs beside the next read.
		raw := bytes.Clone(f.Payload)
		// Its own goroutine, and that is not decoration: a link has exactly one
		// reader (link.Read), so a dispatcher called in line here would hold that
		// one reader for as long as it runs -- no further frame of any channel
		// would be read, the window would stop moving, and a channel that waits
		// for the browser would wait for itself. It is the same reason feed hands
		// nothing on into a queue nobody reads yet.
		// TestTheDispatcherRunsBesideTheReader holds this line.
		go srv.Open.OpenChannel(ctx, s, c, o, raw)
		return true
	default:
		c, ok := s.table.Get(f.Channel)
		if !ok {
			return s.protocolError(ctx, f.Channel)
		}
		return s.feed(ctx, c, f)
	}
}

// feed passes a frame to a running channel; false ends the link. Nothing is
// handed on yet: the queue per channel and the dispatch of the core actions
// come with the sending side (task 15). What this version does is the half that
// cannot wait for it, because it is what holds the window: DATA is counted
// against what we granted, and a GRANT of the browser against what it may lift.
func (s *Session) feed(ctx context.Context, c *link.Channel, f wire.Frame) bool {
	switch f.Type {
	case wire.TypeData:
		// Counted HERE and not by whoever serves the channel, and that is the
		// half of the shared count the compiler holds: the reader is the only
		// code that ever sees an arriving frame, so a dispatcher cannot forget
		// to count what it was handed. The other half -- nothing goes out
		// that was not spent -- is held by SendData and SendGrant being the
		// only ways to the link.
		if !c.Take(len(f.Payload)) {
			return s.protocolError(ctx, f.Channel)
		}
	case wire.TypeGrant:
		credit, handshake, err := wire.ParseGrant(f.Payload)
		if err != nil || len(handshake) != 0 || !c.Grant(credit) {
			// Noise message 2 travels only in the first GRANT of the side that
			// accepted the channel, never from the opener.
			return s.protocolError(ctx, f.Channel)
		}
		// Counted and then dropped. A GRANT says nothing whoever serves the
		// channel has to see, and queueing it would hand a peer a way to fill
		// the queue of a channel with frames that cost no credit at all.
		return true
	case wire.TypeEOF, wire.TypeClose:
	default:
		return s.protocolError(ctx, f.Channel)
	}
	// DATA, EOF and CLOSE wait in the queue of the channel, where whoever
	// serves it takes them with link.Channel.Next. A full queue is the same
	// answer as more DATA than granted: E_PROTOCOL.
	if !s.table.Deliver(f.Channel, f) {
		return s.protocolError(ctx, f.Channel)
	}
	if f.Type == wire.TypeClose {
		c.RecvClose()
	}
	return true
}

// SendGrant lifts what the browser may send on a channel and sends the GRANT.
// It is the only place in this package that writes a GRANT frame, and the order
// here is the table's condition: Allow first, the frame only on true. A credit
// Allow refuses is ErrGrant and nothing goes out, because a GRANT the other
// side would answer with CLOSE E_PROTOCOL is our own protocol error.
//
// A failed write leaves the allowance raised without a GRANT on the line, and
// what that costs has to be read off link.Write and not guessed: of its three
// ways to fail, only the write to the socket ends the link; Encode and Seal
// answer an error and leave it standing (link.go, Write). For a GRANT neither
// can be reached -- its payload is 4 bytes, where Encode bounds at
// wire.MaxPayload and Seal at noisep.MaxPlaintext -- so here a failed write is
// always an ended link and the table dies with it. SendData says what the same
// sentence costs where the payload is not fixed.
func (s *Session) SendGrant(ctx context.Context, c *link.Channel, credit uint32, handshake []byte) error {
	if !c.Allow(credit) {
		return ErrGrant
	}
	// handshake is Noise message 2 of a forwarded channel. It travels in the
	// FIRST GRANT of the side that accepted the channel and nowhere else; a
	// core app has no end-to-end handshake and passes nil. Which GRANT is the
	// first one is the relay to know -- this function only carries what it is
	// given, because credit and handshake have to leave in ONE frame.
	return s.link.Write(ctx, wire.Frame{Type: wire.TypeGrant, Channel: c.Num, Payload: wire.Grant(credit, handshake)})
}

// SendData sends DATA on a channel and takes the bytes off what the browser
// granted us. It is the named way to the link for the dispatcher, which has no
// other: Spend first, the frame only on true, for the same reason SendGrant asks
// Allow first.
//
// Unlike SendGrant this one can fail with the link still standing: Encode
// refuses a payload over wire.MaxPayload, and then the credit is already spent
// while nothing went out. The channel loses that much allowance for good, which
// is the safe half of the mistake -- too little may be sent, never too much --
// and the caller must not retry the frame as if nothing had happened. Whoever
// cuts the payload in task 15 keeps it under wire.MaxPayload and does not lean
// on a failed write having ended anything.
func (s *Session) SendData(ctx context.Context, c *link.Channel, payload []byte) error {
	if !c.Spend(len(payload)) {
		return ErrCredit
	}
	return s.link.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: c.Num, Payload: payload})
}

// protocolError answers CLOSE E_PROTOCOL for a number; it keeps the number
// until the other side's CLOSE arrives, and ends the link when that would take
// one number too many. The holding, the frame and the note are one call of
// link.Table.FailChannel, so this function cannot hold a number and forget its CLOSE.
func (s *Session) protocolError(ctx context.Context, num uint32) bool {
	err := s.table.FailChannel(num, func() error { return s.writeClose(ctx, wire.Error(wire.CodeProtocol), num) })
	switch {
	case err == nil:
		return true
	case errors.Is(err, link.ErrClosing):
		// We already said E_PROTOCOL for this number; what still arrives for it
		// is dropped without an answer, and the link stands.
		return true
	default:
		// Channel 0, one number too many, and a CLOSE that did not go out: all
		// three end the link.
		return false
	}
}

// Close sends a CLOSE for a channel and notes it. It is the way a dispatcher
// ends a channel in order; a protocol error goes through protocolError, which
// notes the CLOSE inside link.Table.FailChannel instead.
//
// An empty code is the ok close of a channel that ran to its end, which is
// what SendResult sends through here. Both ends live in this one function
// because it is the only place of this package that notes a CLOSE of ours,
// and that is a sentence lint_test.go measures.
func (s *Session) Close(ctx context.Context, code wire.Code, num uint32) error {
	cl := wire.Error(code)
	if code == "" {
		cl = wire.Close{Status: wire.StatusOK}
	}
	return s.closeChannel(ctx, num, cl)
}

// CloseWith sends a CLOSE that came from a controller, exactly as it stands.
// It is not the same as Close and may not be built out of it: a CLOSE of the
// far side can be status ok with no code at all, or carry a message and an
// exit code, and a relay that rebuilt it from the code alone would have to
// invent one where there is none (measured in task 17).
func (s *Session) CloseWith(ctx context.Context, c *link.Channel, cl wire.Close) error {
	return s.closeChannel(ctx, c.Num, cl)
}

// closeChannel writes one CLOSE and notes it. It is the ONE place of this
// package that notes a CLOSE of ours -- the sentence lint_test.go measures --
// and Close and CloseWith are the two ways in.
func (s *Session) closeChannel(ctx context.Context, num uint32, cl wire.Close) error {
	if err := s.writeClose(ctx, cl, num); err != nil {
		return err
	}
	if c, ok := s.table.Get(num); ok {
		c.SentClose()
	}
	return nil
}

// writeClose puts one CLOSE frame with a code on the link and notes nothing.
func (s *Session) writeClose(ctx context.Context, cl wire.Close, num uint32) error {
	payload, err := json.Marshal(cl)
	if err != nil {
		return err
	}
	return s.link.Write(ctx, wire.Frame{Type: wire.TypeClose, Channel: num, Payload: payload})
}

// End closes the whole link.
func (s *Session) End(reason string) { _ = s.link.Close(websocket.StatusNormalClosure, reason) }

// Next hands the next frame of a channel to whoever serves it, out of the
// queue that this package's one reader fills (link.Table.Deliver). False means
// nothing more will come: the session is gone, or ctx ended.
//
// Who may read from the queue and who from the socket, in one sentence: ONLY
// Server.run reads the socket, and everyone else reads the queue. The sentence
// is held by the compiler and not by a comment -- Session.link is unexported,
// so no package outside this one has anything to read a socket with, and
// link.Table.Deliver is a method of the table, which is unexported too. A
// dispatcher is handed a *link.Channel, and that carries Next and no Deliver.
// TestOnlyTheReaderFillsTheQueue measures the effect of it.
//
// Why it matters that nobody else reads the socket: a read of coder/websocket
// with a deadline ends the whole link instead of the read (link.go, Read). A
// second reader with a clock of its own would therefore kill the session of
// every other channel, and the relay needs clocks.
func (s *Session) Next(ctx context.Context, c *link.Channel) (wire.Frame, bool) {
	return c.Next(ctx)
}

// SendEOF ends what this side sends on a channel.
func (s *Session) SendEOF(ctx context.Context, c *link.Channel) error {
	return s.link.Write(ctx, wire.Frame{Type: wire.TypeEOF, Channel: c.Num})
}

// SendResult sends the result of a call-style channel and closes it with ok.
// It cuts the body into frames of at most wire.MaxPayload bytes, because
// SendData says what a payload over that costs: the credit is spent while
// nothing went out.
//
// The browser grants at least wire.MinWindow and more on request, so a result
// larger than that waits for its GRANT here. A browser that grants nothing
// more makes the channel end rather than hold a goroutine for the life of the
// link: every wait of this project has a bound.
func (s *Session) SendResult(ctx context.Context, c *link.Channel, body []byte) error {
	for len(body) > 0 {
		n := min(len(body), wire.MaxPayload)
		if !c.SendAllowed(n) {
			if err := s.waitCredit(ctx, c, n); err != nil {
				return err
			}
			continue
		}
		if err := s.SendData(ctx, c, body[:n]); err != nil {
			return err
		}
		body = body[n:]
	}
	if err := s.SendEOF(ctx, c); err != nil {
		return err
	}
	// Through Close and not with a CLOSE of its own: closeChannel is the one
	// place of this package that notes a CLOSE it sent, and lint_test.go holds
	// that. An empty code is the ok close.
	return s.Close(ctx, "", c.Num)
}

// CreditWait bounds one wait for credit on a session channel. It is policy and
// no byte on the line depends on it; it is here so that a browser which grants
// nothing more ends the channel instead of holding a goroutine for the life of
// the link.
const CreditWait = 20 * time.Second

// waitCredit waits until the channel may send n bytes, at most CreditWait.
// link.Channel has no signal of its own, so this looks again on a short
// interval; the price is one wake-up per interval per waiting channel.
func (s *Session) waitCredit(ctx context.Context, c *link.Channel, n int) error {
	deadline := time.After(CreditWait)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if c.SendAllowed(n) {
				return nil
			}
		case <-deadline:
			return ErrCredit
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
