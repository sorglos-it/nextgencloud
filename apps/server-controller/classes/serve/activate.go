// The one core action of this controller that carries a file:
// ngc.store/controller/activate. It is a FlowTransfer action of core.All, so
// what arrives before the EOF of the API is the transfer header and the
// package behind it, up to core.maxTransferIn bytes - 4303355908 of them,
// measured 12.10.2026.
//
// That number is why this file exists at all and why it does not use
// Server.acts. An action of Server.acts gets its input as one []byte, read
// whole before it runs (Server.read), and a server running as root cannot hold
// four gigabytes per channel for an API that may open wire.MaxTunnelChannels
// of them. So the input of this action is handed over as it flows: package
// install writes it to the staging directory and checks it there (install.
// Accept), and this side never holds more than one frame of it.
//
// The trust list travels with the call and is not reached for inside the
// installer. Server.Open reads the version of the channel once and decides
// everything on that one (its doc); an installer that asked chain.Keeper for
// the list of the moment could check the package against a version the task
// was never judged against. install.Deps takes the list as a value, so the one
// place that can keep the two together is the call - which is why Installer
// takes it as an argument and node.wireUp builds the Manager per channel.
package serve

import (
	"context"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// activateKey is ngc.store/controller/activate.
var activateKey = name{core.AppStore, core.ServiceController, core.ActActivate}

// Installer is what carries out an activation on this server
// (apps/server-controller/classes/install.Manager). An interface and not that
// type, so this package keeps the one direction it had: what a channel is
// handed to is wired in from outside and never reached for from here.
//
// list is the trust list this channel was decided on, and the installer is
// built on it rather than on the one of the moment (see the head of this
// file). r carries the transfer header and the package, as the API sends them;
// it ends with io.EOF at the EOF of the API and with an error of its own when
// the channel ended before the input did.
//
// The result bytes are not passed on: the contract gives an activation no
// result of its own, and emptyResult is what a caller encodes.
type Installer interface {
	Activate(ctx context.Context, list *trust.List, d access.Decision, r io.Reader) ([]byte, error)
}

// activate carries out ngc.store/controller/activate.
func (s *Server) activate(ctx context.Context, list *trust.List, d access.Decision,
	r *chanReader) (any, *wire.Close) {
	if s.d.Installer == nil {
		// A controller without an installer is this program wired wrong, not
		// a peer's doing: which halves a server was built with is nothing an
		// API decides. Nothing is read, so a channel that cannot be served
		// costs this server no byte of the package.
		return nil, refuse(wire.CodeInternal)
	}
	_, err := s.d.Installer.Activate(ctx, list, d, r)
	// The channel is asked first, and that order is the whole of it: an
	// installer that stopped halfway because the input stopped answers with
	// an error of its own (install.ErrDisk, ErrVerify), and naming the
	// package broken when the channel broke would send the API looking at the
	// package. What the channel said about itself outranks what the installer
	// made of the half it got.
	if cl := r.close(); cl != nil {
		return nil, cl
	}
	if err != nil {
		return nil, activateCode(err)
	}
	return emptyResult{}, nil
}

// activateCode is the CLOSE of a failed activation. Every error of package
// install is one of the six below or wraps one of them (ErrForbidden,
// ErrLicense, ErrInternal, ErrSource, ErrVerify, ErrDisk, ErrSpace - counted
// 18.10.2026), and the walls are told apart the way the contract names them:
// what this installation did not approve is E_VERIFY, what it approved and
// still may not run is E_FORBIDDEN, the paid app without a usable licence is
// E_LICENSE, and what went wrong on this machine is E_INTERNAL.
//
// ErrSource is E_VERIFY and not E_FORBIDDEN: a package whose store is in no
// approved source is one this installation cannot judge at all, which is a
// verification failure and not a right that is missing.
func activateCode(err error) *wire.Close {
	switch {
	case errors.Is(err, install.ErrVerify), errors.Is(err, install.ErrSource):
		return refuse(wire.CodeVerify)
	case errors.Is(err, install.ErrForbidden):
		return refuse(wire.CodeForbidden)
	case errors.Is(err, install.ErrLicense):
		return refuse(wire.CodeLicense)
	case errors.Is(err, ctrlrt.ErrNoFscrypt):
		// THE ONE FAILURE OF THIS SERVER THAT SAYS WHAT TO DO. The code stays
		// E_INTERNAL with every other one below -- the contract has none for
		// a disk, and inventing a second meaning for one of the others would
		// make every peer read it wrong -- but the SENTENCE is the one the
		// runtime wrote for a person.
		//
		// runtime.NoFscryptText has existed since the data directory did, and
		// its comment said "the caller shows NoFscryptText for it" while no
		// caller did: counted 18.10.2026, nothing read the constant but its
		// own test. On a kernel without CONFIG_FS_ENCRYPTION every activation
		// ends here (measured on 6.18.33.2-microsoft-standard-WSL2), and the
		// person read the general sentence of E_INTERNAL instead of the file
		// systems to use.
		cl := wire.Error(wire.CodeInternal)
		cl.Message = ctrlrt.NoFscryptText
		return &cl
	default:
		// ErrInternal, ErrDisk, ErrSpace and anything the local store or the
		// container engine answered with: all of them are this server's, and
		// none of them is written here. Package install reports what it did
		// on the control channel of the API already, which is where an admin
		// reads it; a line per channel on top of it would be one a peer
		// decides the number of.
		return refuse(wire.CodeInternal)
	}
}
