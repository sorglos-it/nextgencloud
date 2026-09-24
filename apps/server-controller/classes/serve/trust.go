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
// what the verification refused is the fault of what was sent (E_VERIFY), and
// what the local state or this server's own anchor refused is the fault of
// this server (E_INTERNAL). Telling those apart is what keeps an admin from
// being sent to sign the same change again over a store that cannot write:
// E_VERIFY reads "Unterschrift ungültig." to him.
func (s *Server) trust(ctx context.Context, _ access.Decision, in []byte) (any, *wire.Close) {
	seq, err := s.d.Chain.Apply(ctx, in)
	switch {
	case err == nil:
		return core.TrustResult{Seq: seq}, nil
	case errors.Is(err, chain.ErrStore):
		// The one failure here that is worth a line: it takes a local store
		// that cannot be read or written, which no peer brings about on a
		// healthy server. The error carries nothing that came in — chain
		// wraps the answer of this server's own database.
		s.log().Error("the trust list could not be kept", "err", err)
		return nil, refuse(wire.CodeInternal)
	case errors.Is(err, chain.ErrTooMany), errors.Is(err, trust.ErrAnchor):
		// Neither is the fault of the versions that were sent: the first is
		// this installation's own chain grown past chain.MaxVersions, the
		// second an anchor that pins nothing (config.Controller).
		return nil, refuse(wire.CodeInternal)
	}
	return nil, refuse(wire.CodeVerify)
}
