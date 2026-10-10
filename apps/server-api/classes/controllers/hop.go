package controllers

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// This file is the seam package relay forwards a channel over. It is the
// smallest surface that carries a forwarded channel, and the shape of it is
// the point.
//
// What is NOT here, and why: no way to write a wire.Frame of the caller's own
// choosing, and no way to reach the channel table. Those two absences are what
// the compiler holds:
//
//   - A GRANT frame can only be built by Grant, which raises what may arrive
//     (link.Channel.Allow) before the frame goes out. There is no other way to
//     put a GRANT on a tunnel, so "a GRANT raises first" is not a rule a caller
//     has to remember.
//   - DATA can only be built by SendData, which takes the bytes off the credit
//     (link.Channel.Spend) before the frame goes out. So "nothing is sent that
//     was not counted" holds by construction as well.
//   - Arriving DATA and GRANT are counted by the tunnel's own reader
//     (Server.run), which is the only code that sees a frame arrive. A relay
//     therefore cannot forget to count what it was handed either.
//   - The table stays unexported, because *link.Table carries Deliver: a
//     caller that had the table could put frames into a channel's queue that
//     the controller never sent. Open and Get hand out what a relay really
//     needs of it.
//
// Reading goes through the queue and never through the socket: Next takes the
// next frame out of the buffer Server.run fills. A read of coder/websocket
// with a deadline ends the whole link instead of the read, so a second reader
// with a clock of its own would kill the tunnel of every other channel.

var (
	// ErrCredit reports DATA this side may not send yet: more bytes than the
	// controller has granted on that channel. Sending it anyway would be our
	// own protocol error, which the controller answers by ending the channel,
	// so nothing goes out and the caller hears about it instead.
	ErrCredit = errors.New("controllers: not enough credit for this much data")
	// ErrGrant reports a credit this side may not grant: zero, or one that
	// would lift what the controller may send above wire.MaxWindow. Both would
	// be our own protocol error, so the frame never goes out.
	ErrGrant = errors.New("controllers: credit outside the window")
)

// Open picks the next free channel number on the tunnel, with the window the
// API grants the controller. It is link.Table.Next, and it is handed out
// instead of the table itself.
func (t *Tunnel) Open(window uint32) (*link.Channel, error) { return t.table.Next(window) }

// Get returns a channel of the tunnel that is open or closing.
func (t *Tunnel) Get(num uint32) (*link.Channel, bool) { return t.table.Get(num) }

// Next hands the next frame of a channel to whoever serves it, out of the
// queue Server.run fills. False means nothing more will come: the tunnel is
// gone, or ctx ended.
func (t *Tunnel) Next(ctx context.Context, c *link.Channel) (wire.Frame, bool) {
	return c.Next(ctx)
}

// SendOpen writes the OPEN of a forwarded channel. The payload is a
// tunnel.Open the caller built and checked against the contract; this function
// adds nothing to it, because the bytes of the browser's OPEN inside it are
// hashed by the end-to-end prologue and may not be touched.
func (t *Tunnel) SendOpen(ctx context.Context, c *link.Channel, payload []byte) error {
	return t.link.Write(ctx, wire.Frame{Type: wire.TypeOpen, Channel: c.Num, Payload: payload})
}

// SendData sends DATA on a channel of the tunnel and takes the bytes off what
// the controller granted us. Spend first, the frame only on true.
//
// Like browser.Session.SendData this can fail with the tunnel still standing:
// Encode refuses a payload over wire.MaxPayload, and the credit is then spent
// while nothing went out. The channel loses that much allowance for good,
// which is the safe half of the mistake -- too little may be sent, never too
// much -- and the caller must not retry the frame as if nothing had happened.
func (t *Tunnel) SendData(ctx context.Context, c *link.Channel, payload []byte) error {
	if !c.Spend(len(payload)) {
		return ErrCredit
	}
	return t.link.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: c.Num, Payload: payload})
}

// SendEOF ends what the API sends on a channel of the tunnel.
func (t *Tunnel) SendEOF(ctx context.Context, c *link.Channel) error {
	return t.link.Write(ctx, wire.Frame{Type: wire.TypeEOF, Channel: c.Num})
}

// SendClose sends a CLOSE on a channel of the tunnel exactly as it stands and
// notes it. The whole wire.Close travels, not its code: a CLOSE of the shell
// may be status ok with no code at all, and one rebuilt from the code alone
// would have to invent one.
func (t *Tunnel) SendClose(ctx context.Context, c *link.Channel, cl wire.Close) error {
	payload, err := json.Marshal(cl)
	if err != nil {
		return err
	}
	if err := t.link.Write(ctx, wire.Frame{Type: wire.TypeClose, Channel: c.Num, Payload: payload}); err != nil {
		return err
	}
	c.SentClose()
	return nil
}

// Grant lifts what the controller may send on a channel and sends the GRANT.
// It is the only place in this package that builds a GRANT frame, and the
// order is link.Table's condition: Allow first, the frame only on true. A
// credit Allow refuses is ErrGrant and nothing goes out, because a GRANT the
// other side would answer with CLOSE E_PROTOCOL is our own protocol error.
//
// No Noise message 2 travels here: on a tunnel the API is the opener, and the
// handshake of a forwarded channel comes FROM the controller in its first
// GRANT.
func (t *Tunnel) Grant(ctx context.Context, c *link.Channel, credit uint32) error {
	if !c.Allow(credit) {
		return ErrGrant
	}
	return t.link.Write(ctx, wire.Frame{Type: wire.TypeGrant, Channel: c.Num, Payload: wire.Grant(credit, nil)})
}
