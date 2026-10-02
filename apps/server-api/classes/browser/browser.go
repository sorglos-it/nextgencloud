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
	"context"
	"encoding/json"
	"errors"
	"net/http"

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
type Opener interface {
	OpenChannel(ctx context.Context, s *Session, c *link.Channel, o wire.Open)
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
		// Its own goroutine, and that is not decoration: a link has exactly one
		// reader (link.Read), so a dispatcher called in line here would hold that
		// one reader for as long as it runs -- no further frame of any channel
		// would be read, the window would stop moving, and a channel that waits
		// for the browser would wait for itself. It is the same reason feed hands
		// nothing on into a queue nobody reads yet.
		// TestTheDispatcherRunsBesideTheReader holds this line.
		go srv.Open.OpenChannel(ctx, s, c, o)
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
	case wire.TypeEOF, wire.TypeClose:
	default:
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
func (s *Session) SendGrant(ctx context.Context, c *link.Channel, credit uint32) error {
	if !c.Allow(credit) {
		return ErrGrant
	}
	// No Noise message 2 here: core apps have no end-to-end handshake, and the
	// GRANT that carries one for a forwarded channel comes with the tunnel.
	return s.link.Write(ctx, wire.Frame{Type: wire.TypeGrant, Channel: c.Num, Payload: wire.Grant(credit, nil)})
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
	err := s.table.FailChannel(num, func() error { return s.writeClose(ctx, wire.CodeProtocol, num) })
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

// Close sends a CLOSE with a code for a channel and notes it. It is the way a
// dispatcher ends a channel in order; a protocol error goes through
// protocolError, which notes the CLOSE inside link.Table.FailChannel instead.
func (s *Session) Close(ctx context.Context, code wire.Code, num uint32) error {
	if err := s.writeClose(ctx, code, num); err != nil {
		return err
	}
	if c, ok := s.table.Get(num); ok {
		c.SentClose()
	}
	return nil
}

// writeClose puts one CLOSE frame with a code on the link and notes nothing.
func (s *Session) writeClose(ctx context.Context, code wire.Code, num uint32) error {
	payload, err := json.Marshal(wire.Error(code))
	if err != nil {
		return err
	}
	return s.link.Write(ctx, wire.Frame{Type: wire.TypeClose, Channel: num, Payload: payload})
}

// End closes the whole link.
func (s *Session) End(reason string) { _ = s.link.Close(websocket.StatusNormalClosure, reason) }
