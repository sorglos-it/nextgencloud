// Package browser carries the session link to a shell: the WebSocket at
// session.PathSocket with Noise inside, the channels the browser opens and the
// control messages of channel 0. The static keys of this Noise session are the
// session's own keys: the browser's ClientCert.DH and the API's APICert.DH,
// whose private half lies sealed in the database, so any API can serve any
// session.
//
// This package is the one caller of link.Table on a session link, and the
// table's three caller conditions are kept here and nowhere else:
//
//   - Allow runs before every GRANT that goes out, and the GRANT goes out only
//     when Allow said true. SendGrant is the only place in this package that
//     writes a GRANT frame, which is what makes "every" a fact and not a wish:
//     TestEveryGrantGoesThroughSendGrant reads this package's own source and
//     counts.
//   - Every Reserve is followed by a SentClose for the same number, so a number
//     held for a CLOSE E_PROTOCOL is given back once the other side's CLOSE
//     arrives. protocolError is the only place that reserves, and
//     TestEveryReserveIsFollowedBySentClose counts that one too.
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

// Opener takes a channel the browser opened; package coreapp implements it.
type Opener interface {
	OpenChannel(ctx context.Context, s *Session, c *link.Channel, o wire.Open)
}

// Session is one open session link.
type Session struct {
	Live  *login.Live
	Link  *link.Link
	Table *link.Table
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
	s := &Session{Live: live, Link: l, Table: link.NewTable(wire.MaxSessionChannels, false)}
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
		f, err := s.Link.Read(ctx)
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
		c, err := s.Table.Open(f.Channel, o.Window)
		if errors.Is(err, link.ErrLimit) {
			return false
		}
		if err != nil {
			return s.protocolError(ctx, f.Channel)
		}
		// The dispatcher answers with a GRANT or a CLOSE and runs the channel.
		go srv.Open.OpenChannel(ctx, s, c, o)
		return true
	default:
		c, ok := s.Table.Get(f.Channel)
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
// A failed write leaves the allowance raised without a GRANT on the line. That
// is no leak: a write that fails has ended the link (package link), and the
// table dies with it.
func (s *Session) SendGrant(ctx context.Context, c *link.Channel, credit uint32) error {
	if !c.Allow(credit) {
		return ErrGrant
	}
	// No Noise message 2 here: core apps have no end-to-end handshake, and the
	// GRANT that carries one for a forwarded channel comes with the tunnel.
	return s.Link.Write(ctx, wire.Frame{Type: wire.TypeGrant, Channel: c.Num, Payload: wire.Grant(credit, nil)})
}

// protocolError answers CLOSE E_PROTOCOL for a number; it keeps the number
// until the other side's CLOSE arrives, and ends the link when that would take
// one number too many.
func (s *Session) protocolError(ctx context.Context, num uint32) bool {
	if num == wire.ControlChannel {
		return false
	}
	if err := s.Table.Reserve(num); err != nil {
		return false
	}
	c, _ := s.Table.Get(num)
	if c != nil && c.Closing() {
		// We already said E_PROTOCOL for this number; what still arrives for it
		// is dropped without an answer.
		return true
	}
	if err := s.Close(ctx, wire.CodeProtocol, num); err != nil {
		return false
	}
	return true
}

// Close sends a CLOSE with a code for a channel and notes it, which is the
// SentClose every Reserve of this package waits for.
func (s *Session) Close(ctx context.Context, code wire.Code, num uint32) error {
	payload, err := json.Marshal(wire.Error(code))
	if err != nil {
		return err
	}
	if err := s.Link.Write(ctx, wire.Frame{Type: wire.TypeClose, Channel: num, Payload: payload}); err != nil {
		return err
	}
	if c, ok := s.Table.Get(num); ok {
		c.SentClose()
	}
	return nil
}

// End closes the whole link.
func (s *Session) End(reason string) { _ = s.Link.Close(websocket.StatusNormalClosure, reason) }
