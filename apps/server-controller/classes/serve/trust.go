package serve

import (
	"context"
	"errors"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/chain"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// trust carries out ngc.users/controller/trust: the API sends the bytes of a
// session.TrustChain, this controller verifies every version in it from the
// anchor of its own configuration on, keeps what it verified in its local
// state and answers with the number of the newest version it now holds
// (core.TrustResult).
//
// The channel carries no user and needs none — every version brings the
// signature of the admin who issued it, and the keeper checks them all, so an
// API can hand a list on but never forge one (core.WithoutUser).
//
// Which failure ends the channel with which code follows chain.Keeper.Apply:
// what the local state refused, what this installation's own chain grew past
// and an anchor of this server that pins nothing are this server's fault
// (E_INTERNAL), and everything else — what the parser and the verification
// refused — is answered as the fault of what was sent (E_VERIFY). Telling those
// apart is what keeps an admin from being sent to sign the same change again
// over a store that cannot write: E_VERIFY reads "Unterschrift ungültig." to
// him. Where that answer can be wrong, and why nothing here can tell, stands
// at the E_VERIFY of this function.
// The list of the channel is the one thing this action does NOT read: it
// answers out of chain.Keeper, which is what that list is a snapshot of, and a
// snapshot cannot say what the keeper holds after this very call applied
// something to it.
func (s *Server) trust(ctx context.Context, _ *trust.List, _ access.Decision,
	in []byte) (any, *wire.Close) {
	seq, err := s.d.Chain.Apply(ctx, in)
	switch {
	case err == nil:
		// The list is kept, so the answer stands whatever comes of this: what
		// Deps.TrustChanged releases is Apache's business and not the API's,
		// and a web server that could not be told belongs in the log of THIS
		// server and not in a refused channel. It may do nothing slow, so it
		// is called here and not behind a goroutine of its own: one send on a
		// channel of one place is all node.wireUp puts behind it.
		if s.d.TrustChanged != nil {
			s.d.TrustChanged()
		}
		return core.TrustResult{Seq: seq}, nil
	case errors.Is(err, chain.ErrStore):
		// The one failure here that is worth a line: it takes a local store
		// that cannot be read or written, or a keeper that never read it
		// (chain.Keeper.Load), and neither is anything a peer decides. The
		// error carries nothing that came in — chain wraps the answer of this
		// server's own database.
		//
		// What a peer does decide is how many channels arrive, so only the
		// first of them writes and the rest are counted (StoreFailures): one
		// keeper in that state would otherwise put a line in the log for every
		// channel an API opens.
		if s.storeFails.Add(1) == 1 {
			s.log().Error("the trust list could not be kept", "err", err)
		}
		return nil, refuse(wire.CodeInternal)
	case errors.Is(err, chain.ErrTooMany), errors.Is(err, chain.ErrNoAnchor):
		// Neither is the fault of the versions that were sent: the first is
		// this installation's own chain grown past chain.MaxVersions, the
		// second this server's own anchor, which pins nothing
		// (config.Controller).
		return nil, refuse(wire.CodeInternal)
	}
	// Everything else comes out of the verification, and trust.ErrAnchor is
	// among it: a controller that has verified nothing opens this channel too
	// (access.firstChain), so a chain that begins anywhere but at the anchored
	// version arrives here — signed perfectly well and still not this
	// controller's chain. A controller that holds a list answers E_VERIFY when
	// a version does not follow the one it holds (trust.ErrChain), and a chain
	// that does not meet the anchor is the same shape of fault, so it gets the
	// same code. TestASignedVersionPastTheAnchorIsTheSendersFault holds it.
	//
	// What this code says is that the chain and the anchor do not meet, and
	// out of one chain nothing here can say which of the two is wrong. Only an
	// anchor that pins nothing at all is told apart, and only because it is
	// readable as such without any chain (chain.ErrNoAnchor, the case above,
	// "without a number or without a SHA-256"). An anchor that is well formed
	// and still not this installation's — a controller paired against another
	// installation, an anchor file copied from a second server — is a fault of
	// this server that looks exactly like a chain from somewhere else, and it
	// leaves here as E_VERIFY: such a controller turns away every sender there
	// is and names every one of them at fault. That is measured and fenced,
	// not supposed, in TestAnAnchorCopiedFromElsewhereBlamesEverySender.
	//
	// It is no hole — nothing gets in that way — but it sends the operator to
	// the API while the anchor of his own controller is the wrong one. Telling
	// the two apart would take a second, independent word on which version this
	// controller's anchor names, and there is none to be had: the anchor comes
	// out of the pairing and is the word in doubt, and the only versions that
	// ever reach a controller are the ones on this channel.
	return nil, refuse(wire.CodeVerify)
}
