// Reading the input of a core channel as it flows, for the actions that carry
// a file. Server.read holds the whole input in memory and says in its own doc
// that the fork for a FlowTransfer channel belongs there; this file is that
// fork, and chanReader is what an action gets instead of a []byte.
package serve

import (
	"context"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// chanReader hands out the DATA of one core channel as an io.Reader and gives
// the credit of every frame back once that frame has been handed out whole.
// It is read from the goroutine of its channel and from nowhere else, so
// nothing in it is guarded.
//
// What it never does is hold more than one frame: that is the whole point of
// it (see the head of activate.go). The bound of the action still holds - it
// counts what went past and refuses the frame that would take the total over
// core.Action.MaxIn - so an API that keeps sending meets the same wall as on a
// buffered action, only without this server paying for it first.
type chanReader struct {
	ctx context.Context
	ch  inbox
	max int64
	// got is how many bytes of DATA came in so far, counted before any of
	// them is handed out.
	got int64
	// buf is what is left of the frame being handed out.
	buf []byte
	// credit is the credit of buf that is not given back yet: the frame is
	// paid for when the last byte of it has left this reader, not when it
	// arrived, because until then the bytes are still here.
	credit uint32
	// done is set once the EOF of the API came, so a second Read answers
	// io.EOF again instead of waiting on a channel that is over.
	done bool
	// cl is the CLOSE this channel owes, or nil. It is set once and read
	// after the action came back (chanReader.close): an action that stopped
	// reading for its own reasons leaves it nil.
	cl *wire.Close
}

// inbox is the half of link.Channel this reader uses. An interface and not
// that type, so a case can hold the two lines that cannot be reached over a
// real tunnel: the bound of an action whose MaxIn is 4303355908 bytes, and a
// Grant that fails.
type inbox interface {
	Recv(ctx context.Context) (link.Inbound, error)
	Grant(credit uint32, handshake []byte) error
}

// newChanReader reads the input of this action off ch.
func newChanReader(ctx context.Context, ch inbox, a core.Action) *chanReader {
	return &chanReader{ctx: ctx, ch: ch, max: a.MaxIn}
}

// Read fulfils io.Reader.
func (r *chanReader) Read(p []byte) (int, error) {
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
	if len(r.buf) > 0 {
		return n, nil
	}
	// The frame is out of this reader, so its credit goes back and the API
	// may send the next one.
	if err := r.give(); err != nil {
		return n, err
	}
	return n, nil
}

// fill takes the next frame of the channel into buf, or marks the end.
func (r *chanReader) fill() error {
	for {
		m, err := r.ch.Recv(r.ctx)
		if err != nil {
			// The channel ended before its input did. Whether the API closed
			// it or the tunnel went is not told apart, the same way
			// Server.read does not tell them apart: either way the action
			// does not get the rest of its input.
			r.cl = cutOff()
			return errCutOff
		}
		switch m.Kind {
		case wire.TypeEOF:
			r.done = true
			return io.EOF
		case wire.TypeData:
			// Counted before it is handed out: this server never lets more
			// than the action allows go past, whatever the API sends.
			r.got += int64(len(m.Data))
			if r.got > r.max {
				r.cl = refuse(wire.CodeTooLarge)
				return errTooLarge
			}
			if len(m.Data) == 0 {
				// A GRANT of nothing is none of the contract, and an empty
				// DATA took no credit to give back.
				continue
			}
			r.buf = m.Data
			r.credit = uint32(len(m.Data))
			return nil
		}
	}
}

// give returns the credit of the frame that was just handed out.
func (r *chanReader) give() error {
	if r.credit == 0 {
		return nil
	}
	credit := r.credit
	r.credit = 0
	if err := r.ch.Grant(credit, nil); err != nil {
		// The credit goes back unless the channel ended in the meantime, and
		// that end is the API's doing and not a failure of this server: the
		// CLOSE of the API comes in while what it sent before it is still
		// being handed out (link.Channel.Recv).
		if errors.Is(err, link.ErrClosed) {
			r.cl = cutOff()
			return errCutOff
		}
		r.cl = refuse(wire.CodeInternal)
		return errGrant
	}
	return nil
}

// close is the CLOSE this channel owes because of its input, or nil when the
// input was not what stopped. A caller reads it after the action came back and
// lets it outrank the error of the action (serve.Server.activate).
func (r *chanReader) close() *wire.Close { return r.cl }

// The errors this reader hands an action. Which of them it was does not reach
// the channel - chanReader.cl carries the CLOSE - so they exist to be told
// apart in a log of an action that writes one, and to be distinguishable from
// io.EOF by whoever reads the input.
var (
	errCutOff   = errors.New("serve: the channel ended before its input did")
	errTooLarge = errors.New("serve: more input than this action allows")
	errGrant    = errors.New("serve: the credit of this channel could not be given back")
)
