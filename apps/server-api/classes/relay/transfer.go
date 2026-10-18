package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// CallController opens a channel to a controller without a user and returns
// the result. More than wire.MaxCallResult ends the channel with E_TOOLARGE.
//
// "Without a user" is what it sends and not something it checks: no client
// certificate and no proof travel with the OPEN, and a channel like that is
// allowed only for the actions core.WithoutUser lists -- which the controller
// checks (tunnel.VerifyOpen), not the API.
//
// Only a core app can be called this way, and that is not a policy but what
// the API is able to do: every other app channel is encrypted between shell
// and controller, and the API holds no end-to-end key. Such a call is
// E_INTERNAL here rather than an OPEN the controller would have to refuse.
func (rl *Relay) CallController(ctx context.Context, hop Hop, app, service, action string,
	params json.RawMessage, in []byte) ([]byte, wire.Code) {
	payload, code := ownOpen(app, service, action, params, nil, nil, nil)
	if code != "" {
		return nil, code
	}
	return rl.call(ctx, hop, payload, bytes.NewReader(in))
}

// Transfer opens a channel to a controller for a core action with flow
// transfer and writes the header and the file on it. task is the signed task
// the action needs, client and proof the user it is carried out for; all three
// travel as they stand, and nothing here verifies them -- the caller has done
// that (package rights), and the controller does it again.
func (rl *Relay) Transfer(ctx context.Context, hop Hop, app, service, action string,
	params json.RawMessage, head core.Transfer, file io.Reader, task *sign.Envelope,
	client []byte, pr *proof.Proof) ([]byte, wire.Code) {
	// Our own header through the writer of the contract before anything is
	// opened: a header that does not validate is our mistake, and the channel
	// should not exist for it.
	var hb bytes.Buffer
	if err := core.WriteTransfer(&hb, head); err != nil {
		return nil, wire.CodeInternal
	}
	payload, code := ownOpen(app, service, action, params, task, client, pr)
	if code != "" {
		return nil, code
	}
	// The header and the file are one byte stream on this hop and may be cut
	// at any point; core.ReadTransfer reads the header by its length prefix.
	return rl.call(ctx, hop, payload, io.MultiReader(&hb, file))
}

// ownOpen builds the payload of an OPEN the API sends for itself and checks it
// through the parsers of the contract.
func ownOpen(app, service, action string, params json.RawMessage, task *sign.Envelope,
	client []byte, pr *proof.Proof) ([]byte, wire.Code) {
	if !wire.CoreApp(app) {
		return nil, wire.CodeInternal
	}
	o := wire.Open{App: app, Service: service, Action: action, Window: wire.DefaultWindow}
	if task != nil {
		// With a task only the signed params count, so the OPEN carries none.
		o.Task = task
	} else {
		o.Params = params
	}
	if err := o.Validate(); err != nil {
		return nil, wire.CodeInternal
	}
	inner, err := json.Marshal(o)
	if err != nil {
		return nil, wire.CodeInternal
	}
	payload, err := json.Marshal(tunnel.Open{Open: inner, Window: wire.DefaultWindow,
		Client: client, Proof: pr})
	if err != nil {
		return nil, wire.CodeInternal
	}
	if _, _, err := tunnel.ParseOpen(payload); err != nil {
		return nil, wire.CodeInternal
	}
	return payload, ""
}

// call opens the channel, sends in and collects the result.
func (rl *Relay) call(ctx context.Context, hop Hop, payload []byte, in io.Reader) ([]byte, wire.Code) {
	// A call the API makes for itself has no user at a socket whose end would
	// end it, so this clock is the only thing that does.
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	tc, err := hop.Open(wire.DefaultWindow)
	if err != nil {
		if errors.Is(err, link.ErrLimit) {
			return nil, wire.CodeLimit
		}
		return nil, wire.CodeInternal
	}
	if err := hop.SendOpen(cctx, tc, payload); err != nil {
		return nil, wire.CodeUnreachable
	}
	body, code := rl.collect(cctx, hop, tc, in)
	cl := wire.Close{Status: wire.StatusOK}
	if code != "" {
		cl = wire.Error(code)
	}
	// ctx and not cctx: the number has to be given back even when the call ran
	// out of time, and a cancelled context would write nothing.
	rl.endHop(ctx, hop, tc, cl)
	return body, code
}

// collect waits for the first GRANT, sends the input beside the reading, and
// gathers the answer up to the controller's EOF. The result counts only if the
// channel ends with CLOSE ok (package wire, "Results").
func (rl *Relay) collect(ctx context.Context, hop Hop, tc *link.Channel, in io.Reader) ([]byte, wire.Code) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st := newChanState(cancel)
	var buf bytes.Buffer
	eof := false
	first := true
	for {
		f, ok := hop.Next(sctx, tc)
		if !ok {
			if code := st.ended(); code != "" {
				return nil, code
			}
			return nil, wire.CodeTimeout
		}
		switch f.Type {
		case wire.TypeGrant:
			// The credit is already counted by the tunnel's own reader; what is
			// read here is whether a handshake came with it, and a channel the
			// API opened for itself is never end to end, so none may.
			_, handshake, err := wire.ParseGrant(f.Payload)
			if err != nil || len(handshake) != 0 {
				return nil, wire.CodeProtocol
			}
			st.wakeCredit()
			if first {
				first = false
				// Beside the reading, because a large input needs the GRANTs
				// that arrive while it is being sent.
				go func() {
					// A goroutine of its own, so a panic in the sending
					// would take the whole process (V1.2 point 38). It ends
					// like any other failure of the sending: the channel
					// fails, the server lives.
					defer func() {
						if guard.Hit(recover(), "relay.sendStream", tc.Num) {
							st.fail(wire.CodeInternal)
						}
					}()
					if code := rl.sendStream(sctx, hop, tc, st, in); code != "" {
						st.fail(code)
					}
				}()
			}
		case wire.TypeData:
			// Counted by the tunnel's reader before it was queued, never here.
			if !st.count(len(f.Payload)) {
				return nil, wire.CodeTooLarge
			}
			buf.Write(f.Payload)
			if err := hop.Grant(ctx, tc, uint32(len(f.Payload))); err != nil {
				return nil, wire.CodeInternal
			}
		case wire.TypeEOF:
			eof = true
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				return nil, wire.CodeProtocol
			}
			tc.RecvClose()
			if cl.Status != wire.StatusOK {
				// After an error whatever came is thrown away.
				return nil, cl.Code
			}
			if !eof {
				// A result is what came up to the other side's EOF; without one
				// there is no telling whether it is whole.
				return nil, wire.CodeProtocol
			}
			return buf.Bytes(), ""
		default:
			return nil, wire.CodeProtocol
		}
	}
}

// sendStream writes a byte stream of the API's own and ends it with EOF. This
// hop may be cut at any point (see the package comment), so it is cut at
// wire.MaxPayload.
func (rl *Relay) sendStream(ctx context.Context, hop Hop, tc *link.Channel, st *chanState,
	in io.Reader) wire.Code {
	buf := make([]byte, wire.MaxPayload)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			if code := rl.sendWhole(ctx, hop, tc, st, buf[:n]); code != "" {
				return code
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return wire.CodeInternal
		}
	}
	if err := hop.SendEOF(ctx, tc); err != nil {
		return wire.CodeUnreachable
	}
	return ""
}

// writeTransfer writes the header and the file of a forwarded transfer channel.
func (rl *Relay) writeTransfer(ctx context.Context, hop Hop, tc *link.Channel, st *chanState,
	head core.Transfer, file io.Reader) wire.Code {
	var hb bytes.Buffer
	if err := core.WriteTransfer(&hb, head); err != nil {
		return wire.CodeInternal
	}
	// sendStream ends with EOF, which is what a transfer channel needs after
	// the file, so the caller sends none of its own.
	return rl.sendStream(ctx, hop, tc, st, io.MultiReader(&hb, file))
}

// wait is how long this relay waits for credit on a hop; zero or less means
// GrantWait.
func (rl *Relay) wait() time.Duration {
	if rl.Wait <= 0 {
		return GrantWait
	}
	return rl.Wait
}

// drain is how long this relay waits for the controller's CLOSE; zero or less
// means CloseDrain. A test sets it to milliseconds instead of waiting.
func (rl *Relay) drain() time.Duration {
	if rl.Drain <= 0 {
		return CloseDrain
	}
	return rl.Drain
}
