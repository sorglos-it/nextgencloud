package browser

import (
	"context"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrInput is what Read answers for an input that will not arrive whole: the
// bound was passed, a CLOSE came instead of an EOF, a frame of the wrong type
// came, or the context ended. Which of them it was decides the code of the
// channel and not the error, so the error carries no case of its own -- see
// Input.Code.
var ErrInput = errors.New("browser: the input of the channel did not arrive whole")

// Input is the DATA of a FlowIn channel as a stream. Read hands out the bytes
// of the frames as they come and grants the credit back as it consumes, so
// what this process holds of an upload is one frame and not the package.
//
// Why a stream and not a []byte, and this is the whole reason the type exists:
// core.Action.MaxIn for ngc.store/api/upload is store.MaxPackage, four
// gigabytes. A reader that answers a []byte has to hold every one of them, and
// an admin who really uploads a large package then kills the process on a
// small server -- measured over this channel at 16 and 64 MiB before this type
// existed (TestUploadHoldsNoPackageOnTheChannel in coreapp). appstore.Upload
// reads its package in ONE pass for exactly that reason, and this is what lets
// it.
//
// The bound still holds the memory of the channel, by the same means as
// before: past max nothing more is handed out and no further credit is
// granted, so a sender that keeps going runs out of credit instead of filling
// this process.
//
// The context is kept in the struct because io.Reader has nowhere to put one.
// It is the context of the channel the dispatcher serves, so it ends with the
// channel and with nothing else.
type Input struct {
	s   *Session
	c   *link.Channel
	ctx context.Context

	max  int64  // Action.MaxIn: the most this channel may send
	read int64  // what came as DATA so far
	rest []byte // the tail of the frame last read, not yet handed out

	code wire.Code // the code the channel must close with, "" while healthy
	done bool      // the EOF came, or the stream failed: nothing more
}

// Input starts reading the DATA of a FlowIn channel, at most max bytes, up to
// the browser's EOF.
//
// Nothing is read before the first Read: whoever serves the action decides
// when and how much, and an action that wants its input in one piece says so
// with io.ReadAll and a bound of its own (coreapp, trust and license do).
func (s *Session) Input(ctx context.Context, c *link.Channel, max int64) *Input {
	return &Input{s: s, c: c, ctx: ctx, max: max}
}

// Code is the code the channel has to close with, or "" if the input arrived
// whole up to its EOF -- and "" as well while nobody has read to the end yet.
//
// E_TOOLARGE means more than max came, E_PROTOCOL a CLOSE or a frame of the
// wrong type instead of an EOF, E_TIMEOUT a context that ended first.
//
// It is asked AFTER the handler on purpose: with a stream the break shows up
// inside the handler, which answers an error of its own for it, and a code
// about the input is nearer the truth than a code about what the handler made
// of it. A handler that swallows the error of its reader therefore cannot turn
// a half upload into a result.
func (in *Input) Code() wire.Code { return in.code }

// Drain reads what is left of the input away and answers Code. A handler that
// answered before the EOF leaves the rest of the upload on the channel; it is
// thrown away here, so the channel ends on a whole input or on a code, and
// never quietly on half of one.
//
// It reads nothing once the stream is done, so for a handler that read to the
// EOF itself -- which every FlowIn handler of this API does -- it costs one
// comparison.
func (in *Input) Drain() wire.Code {
	if !in.done {
		_, _ = io.Copy(io.Discard, in)
	}
	return in.code
}

// Read fills p from the frames of the channel. It answers io.EOF at the
// browser's EOF and ErrInput for everything Code names; after a failure every
// further Read answers ErrInput again, so a handler that reads on cannot
// mistake a broken stream for its end.
func (in *Input) Read(p []byte) (int, error) {
	if len(in.rest) > 0 {
		n := copy(p, in.rest)
		in.rest = in.rest[n:]
		return n, nil
	}
	if in.done {
		if in.code != "" {
			return 0, ErrInput
		}
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	for {
		f, ok := in.c.Next(in.ctx)
		if !ok {
			return 0, in.fail(wire.CodeTimeout)
		}
		switch f.Type {
		case wire.TypeData:
			if in.read+int64(len(f.Payload)) > in.max {
				return 0, in.fail(wire.CodeTooLarge)
			}
			in.read += int64(len(f.Payload))
			// Granted again right here, so an upload larger than one window
			// does not stall. An empty payload grants nothing: a credit of
			// zero is no GRANT (wire.ParseGrant), and ErrGrant would end a
			// channel over a frame that cost nothing.
			if len(f.Payload) == 0 {
				continue
			}
			if err := in.s.SendGrant(in.ctx, in.c, uint32(len(f.Payload)), nil); err != nil {
				return 0, in.fail(wire.CodeInternal)
			}
			// The payload is this frame's own (link.Table.Deliver clones it),
			// so the tail may be kept until the next Read. It is one frame,
			// at most wire.MaxPayload, and it is let go before the next frame
			// is asked for.
			n := copy(p, f.Payload)
			in.rest = f.Payload[n:]
			return n, nil
		case wire.TypeEOF:
			in.done = true
			return 0, io.EOF
		case wire.TypeClose:
			// A CLOSE instead of an EOF is the browser giving the channel up
			// in the middle of its input; whoever asked for the input stops
			// rather than acting on half an upload.
			return 0, in.fail(wire.CodeProtocol)
		default:
			return 0, in.fail(wire.CodeProtocol)
		}
	}
}

// fail notes the code of a stream that ended early and answers ErrInput.
func (in *Input) fail(code wire.Code) error {
	in.done, in.code = true, code
	return ErrInput
}
