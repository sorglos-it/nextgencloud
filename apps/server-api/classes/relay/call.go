package relay

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// CallAs opens a channel to a controller for a core action of flow call and
// returns the result, or the code the channel closed with.
//
// It is CallController with a user. The difference is the whole point and not
// a convenience: CallController sends no client certificate and no proof, so
// the controller may only let the actions of core.WithoutUser through on it
// (tunnel.VerifyOpen). ngc.store/controller/deactivate and .../uninstall are
// not in that list -- they carry a signed task and the user it was signed for
// -- so before this method there was no way at all to run them from the API,
// and appstore.Sender.Call had no implementation that could work.
//
// Nothing here verifies task, client or pr. The caller has done that (package
// rights, through the dispatcher), and the controller does it again; this
// method only puts them on the wire as they stand, exactly as Transfer does
// for the transfer flow.
func (rl *Relay) CallAs(ctx context.Context, hop Hop, app, service, action string,
	params json.RawMessage, task *sign.Envelope, client []byte, pr *proof.Proof) ([]byte, wire.Code) {
	payload, code := ownOpen(app, service, action, params, task, client, pr)
	if code != "" {
		return nil, code
	}
	return rl.call(ctx, hop, payload, bytes.NewReader(nil))
}
