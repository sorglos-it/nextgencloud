// The activation of an app on this controller: ngc.store/controller/activate.
//
// IT IS THE FIRST ACTION OF THIS SERVER THAT CARRIES A FILE, and that is the
// whole reason this file exists beside update.go. Everything served until now
// was FlowCall: its input fits in memory, and serve.read holds it whole. An
// activation is FlowTransfer and carries up to core.maxTransferIn bytes --
// 4303355908, measured -- which may not be held at all: a tunnel holds up to
// wire.MaxTunnelChannels numbers at once, and this process runs as root.
// serve.read says so itself and names the fork this file takes ("Whoever
// serves them reads the input of a FlowTransfer channel as it flows instead of
// holding it").
//
// WHY IT WAS NOT SERVED UNTIL 18.10.2026, measured on a running installation
// rather than read: a shell uploaded a package, the API found it and sent it,
// and the channel ended E_INTERNAL -- serve.core looked the action up in
// Server.acts, found nothing, and answered with the code of this program being
// wired wrong. install.Manager.Activate was written and tested the whole time;
// nobody called it. The nine refusals inside it were unreachable, and a probe
// that logged every one of them saw not a single line.
package serve

import (
	"context"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// activateKey is ngc.store/controller/activate.
var activateKey = name{core.AppStore, core.ServiceController, core.ActActivate}

// Installer carries out an activation on this server
// (apps/server-controller/classes/install.Manager). An interface and not that
// type, for the reason Updater names: what a channel is handed to is wired in
// from outside and never reached for from here.
//
// IT IS ASKED AFRESH FOR EVERY CHANNEL, and whoever wires it has to build it
// that way. install.Deps takes the trust list as a VALUE and this server's
// node as a string, while both move under it: the list is replaced version by
// version (chain.Keeper.Current) and the node is only learned once a version
// lists this server. A Manager built once at the start would install against
// the list of the moment it was built in -- which is the reason the updater
// beside it is still unwired, written out in node/run.go. An interface whose
// implementation builds its Manager per call has neither problem, and nothing
// of that belongs in this package.
//
// THE LIST IS HANDED OVER AND NOT REACHED FOR. It is the version this
// channel was decided on (Server.Open), so the package is checked against
// the same version that verified the task. An installer that asked the
// keeper itself would take whatever version happened to be newest by then,
// and an activation runs long enough for that to be another one.
type Installer interface {
	Activate(ctx context.Context, list *trust.List, d access.Decision, r io.Reader) ([]byte, error)
}

// activate carries out ngc.store/controller/activate. The package arrives as
// it flows: nothing of it is held here, and what the installer does not read
// is never taken off the channel either.
func (s *Server) activate(ctx context.Context, list *trust.List, d access.Decision, r io.Reader) (any, *wire.Close) {
	if s.d.Installer == nil {
		// A controller without an installer is this program wired wrong, not
		// a peer's doing: which halves a server was built with is nothing an
		// API decides. It is the same answer serve.core gave while this
		// action had no handler at all, so a build without an installer is
		// not a new state for an API.
		return nil, refuse(wire.CodeInternal)
	}
	if !d.Core || d.Action.App != activateKey.app || d.Action.Service != activateKey.service ||
		d.Action.Name != activateKey.action {
		// access matched the OPEN against core.All already, so a decision
		// that names something else here is this program wired wrong.
		return nil, refuse(wire.CodeInternal)
	}
	if _, err := s.d.Installer.Activate(ctx, list, d, r); err != nil {
		return nil, installCode(err)
	}
	return emptyResult{}, nil
}

// chanInput is the input of a FlowTransfer channel as an io.Reader: every DATA
// up to the EOF of the API, at most core.Action.MaxIn bytes, and the credit of
// what was handed out given back at once.
//
// WHAT IT KEEPS is one frame at most, and only the part of it a Read had no
// room for. That is the difference to serve.read, which appends every frame to
// one slice: here the bytes travel through to whoever reads, and a package of
// four gigabytes costs this server the size of a frame.
//
// THE CREDIT IS GIVEN BACK WHEN THE FRAME IS TAKEN OFF THE CHANNEL, not when
// the reader has consumed it. That is the same moment serve.read chose, and it
// is the one that keeps the API sending; holding the grant until a reader
// asked again would make the window of this side the speed of the installer.
// The price is that one frame of credit is out while this holds the frame, and
// that is bounded by the frame, not by the file.
//
// EVERY FAULT BECOMES THE CLOSE THIS SIDE OWES, kept in cl and handed out by
// Close: a reader sees a plain error and must not have to know the contract.
// The caller reads cl after the installer is done, so a package that was cut
// off ends the channel as E_PROTOCOL even where the installer answered its own
// error first -- what the API did outweighs what the installer made of half a
// file.
// frames is the half of link.Channel this reader uses: what comes in and the
// credit that goes back. An interface and not the type, for the one reason
// this package names everywhere -- and here it buys a measurement as well:
// a case can hand this reader a cut-off stream, an empty DATA or a grant
// that fails, none of which a real tunnel produces on demand.
type frames interface {
	Recv(ctx context.Context) (link.Inbound, error)
	Grant(credit uint32, handshake []byte) error
}

type chanInput struct {
	ctx  context.Context
	ch   frames
	max  int64
	got  int64
	rest []byte
	done bool
	cl   *wire.Close
}

func (in *chanInput) Read(p []byte) (int, error) {
	if len(in.rest) > 0 {
		n := copy(p, in.rest)
		in.rest = in.rest[n:]
		return n, nil
	}
	if in.done {
		return 0, io.EOF
	}
	for {
		m, err := in.ch.Recv(in.ctx)
		if err != nil {
			in.done, in.cl = true, cutOff()
			return 0, io.ErrUnexpectedEOF
		}
		switch m.Kind {
		case wire.TypeEOF:
			in.done = true
			return 0, io.EOF
		case wire.TypeData:
			// Counted before anything is handed on: this server never lets
			// more than the action allows through, whatever the API sends.
			in.got += int64(len(m.Data))
			if in.got > in.max {
				in.done, in.cl = true, refuse(wire.CodeTooLarge)
				return 0, io.ErrUnexpectedEOF
			}
			if len(m.Data) == 0 {
				// A GRANT of nothing is none of the contract, and an empty
				// DATA took no credit to give back.
				continue
			}
			if err := in.ch.Grant(uint32(len(m.Data)), nil); err != nil {
				// The credit goes back unless the channel ended in the
				// meantime, and that end is the API's doing: its CLOSE comes
				// in while what it sent before is still being handed out
				// (link.Channel.Recv).
				if errors.Is(err, link.ErrClosed) {
					in.done, in.cl = true, cutOff()
					return 0, io.ErrUnexpectedEOF
				}
				in.done, in.cl = true, refuse(wire.CodeInternal)
				return 0, io.ErrUnexpectedEOF
			}
			n := copy(p, m.Data)
			if n < len(m.Data) {
				in.rest = m.Data[n:]
			}
			return n, nil
		}
	}
}

// drain reads what the installer left and answers the CLOSE this side owes.
//
// IT HAS TO RUN EVEN WHERE THE INSTALLER SUCCEEDED. An installer that refused
// a package after its first bytes -- a checksum in the header, a manifest that
// names another app -- leaves the rest of a four gigabyte file on the channel,
// and a channel that is closed over unread DATA is a channel the API is still
// sending into. Reading it out is what lets this side answer at all.
//
// The bound still holds while it drains: what comes past MaxIn is refused here
// exactly as it would be in Read.
func (in *chanInput) drain() *wire.Close {
	if in.cl == nil && !in.done {
		var buf [32 << 10]byte
		for in.cl == nil && !in.done {
			if _, err := in.Read(buf[:]); err != nil && err != io.EOF {
				break
			}
		}
	}
	return in.cl
}

// flow runs the channel of a core action that carries a file: the first GRANT,
// the reader over the DATA, the action, what it did not read drained, and its
// result as DATA, EOF and CLOSE ok.
//
// It is Server.core with one difference, and the difference is the whole point:
// core reads the input into a slice and then calls the action, this one hands
// the action a reader. Everything after the action is the same, and it is
// written twice rather than factored out so that neither half has to carry an
// if for the other.
func (s *Server) flow(ctx context.Context, ch *link.Channel, list *trust.List,
	d access.Decision, run transfer) error {
	if err := ch.Grant(firstGrant(d.Action), nil); err != nil {
		return s.end(ch, wire.Error(wire.CodeInternal))
	}
	in := &chanInput{ctx: ctx, ch: ch, max: d.Action.MaxIn}
	v, cl := run(ctx, list, d, in)
	if sent := closeOf(in, cl); sent != nil {
		return s.end(ch, *sent)
	}
	b, err := core.EncodeResult(v)
	if err != nil {
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

// installCode is the CLOSE of a failed activation. It is a function of its
// own beside code() and NOT the same one, and a case holds the difference
// (TestEveryFailureOfAnActivationHasItsCode): the two map errors of two
// different packages, and code() answered E_INTERNAL for every error of
// package install -- measured 18.10.2026, where install.ErrVerify came out
// as "Interner Fehler." and told an admin his own installation was broken
// where the package was not what he signed for.
//
// The three walls are kept apart, as they are on the API side (appstore.CodeFor
// answers the same three for the same errors): what this installation did not
// sign for, what it signed for and may not have, and what went wrong on this
// machine. ErrSource belongs to the first -- a package that names a source
// this installation never approved is a verification failure and not a
// permission.
//
// ErrSpace and ErrDisk fall to E_INTERNAL because the contract has no code for
// a disk, and that is the honest end of it: it is this server's own trouble
// and nothing the shell can act on.
func installCode(err error) *wire.Close {
	switch {
	case errors.Is(err, install.ErrVerify), errors.Is(err, install.ErrSource),
		errors.Is(err, store.ErrPackage), errors.Is(err, store.ErrManifest),
		errors.Is(err, store.ErrEncrypted), errors.Is(err, store.ErrCatalog),
		errors.Is(err, store.ErrRelease), errors.Is(err, store.ErrRollback),
		errors.Is(err, sign.ErrBadSignature), errors.Is(err, sign.ErrWrongKey),
		errors.Is(err, sign.ErrEnvelope):
		return refuse(wire.CodeVerify)
	case errors.Is(err, install.ErrForbidden):
		return refuse(wire.CodeForbidden)
	case errors.Is(err, install.ErrLicense), errors.Is(err, store.ErrLicense):
		return refuse(wire.CodeLicense)
	case errors.Is(err, ctrlrt.ErrNoFscrypt):
		// THE ONE FAILURE OF THIS ACTION THAT SAYS WHAT TO DO. The disk of
		// this server cannot encrypt a directory, and an app's data directory
		// is never made without encryption (runtime.ErrNoFscrypt). The code
		// stays E_INTERNAL, because it IS this server's own doing and no
		// decision of the API: there is no code for a disk in the contract,
		// and inventing a meaning for one of the others would make every
		// peer read it wrong.
		//
		// What changes is the sentence. runtime.NoFscryptText has existed
		// since the data directory did, and its comment said "the caller
		// shows NoFscryptText for it" while NO caller did (counted
		// 18.10.2026: the constant was read by nothing but its own test).
		// Everyone who ran into this wall read the general sentence of
		// E_INTERNAL instead and had nothing to go by -- measured by a peer
		// session on a WSL kernel without CONFIG_FS_ENCRYPTION, where every
		// activation ends here.
		cl := wire.Error(wire.CodeInternal)
		cl.Message = ctrlrt.NoFscryptText
		return &cl
	}
	return refuse(wire.CodeInternal)
}

// closeOf is the CLOSE of a flow once the action is done, and it is a function
// of its own so that one decision can be held by a case: WHAT THE API DID
// OUTWEIGHS WHAT THE ACTION MADE OF IT.
//
// An action that refused a package after its first bytes saw half a file and
// cannot know that the rest was cut off or went past the bound. So the rest is
// read out first (drain, which also frees the channel the API is still sending
// into), and its code wins where there is one; only then does the action's own
// refusal count, and nil means the result may go out.
//
// It is drain() and never chanInput.cl: cl is what has been noticed SO FAR,
// and what the action did not read has not been noticed yet. A mutant that
// reads cl instead passed every case of this file on 18.10.2026, which is why
// the decision sits here with a case of its own.
func closeOf(in *chanInput, cl *wire.Close) *wire.Close {
	if sent := in.drain(); sent != nil {
		return sent
	}
	return cl
}
