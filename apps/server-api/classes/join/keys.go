package join

import (
	"encoding/json"
	"net/http"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// PathKeys is where a new API server fetches the keys every API of the
// installation shares, once an admin has put it into the trust list. The
// contracts leave this to the API itself; the exchange runs Noise_KK with the
// two nodes' keys from the trust list, so neither side has to trust the
// network.
const PathKeys = "/tunnel/keys"

// PrologueKeys tells this handshake apart from the session, tunnel and
// end-to-end ones (package noisep).
const PrologueKeys = "ngc/apikeys/v1"

// sharedKeys is what travels: the two keys every API of the installation has
// to have the same. SecretKey seals the database secrets -- the installation's
// private key lies in the database already, sealed with exactly this key, so
// nothing else has to travel. ChallengeKey draws the made-up login answer
// (session.Challenge); a server with its own would answer an unknown name
// differently from its neighbours, and asking two servers in turn would sort
// the names that exist from those that do not.
type sharedKeys struct {
	SecretKey    enc.Bytes `json:"secretKey"`
	ChallengeKey enc.Bytes `json:"challengeKey"`
}

// Keys serves GET PathKeys: the new server dials with its own Noise key, and
// only a key that stands in the trust list as an API node other than this one
// gets an answer. The fingerprint in the query only says which node is meant;
// the Noise_KK handshake against that node's key is what proves it, so a
// stranger who names a listed fingerprint gets no handshake.
func (j *Join) Keys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v, err := j.S.Trust(ctx)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var peer []byte
	fingerprint := r.URL.Query().Get("key")
	for _, n := range v.List.Nodes {
		if n.Kind == trust.NodeAPI && n.ID != j.S.Cfg.Node && tunnel.Fingerprint(n.Noise) == fingerprint {
			peer = n.Noise
		}
	}
	if peer == nil {
		// Before an admin signs the new server in, there is nothing to fetch,
		// and no hint why. An empty query reaches here too: Fingerprint of a
		// listed key is never the empty string.
		w.WriteHeader(http.StatusNotFound)
		return
	}
	static, err := j.staticKey()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	l, err := link.Accept(w, r, link.Options{Subprotocol: tunnel.Subprotocol,
		Prologue: PrologueKeys, Static: static, Peer: peer})
	if err != nil {
		return
	}
	defer l.Close(websocket.StatusNormalClosure, "")
	body, err := json.Marshal(sharedKeys{
		SecretKey:    enc.Bytes(j.S.Cfg.SecretKey.Bytes()),
		ChallengeKey: enc.Bytes(j.S.Cfg.ChallengeKey.Bytes()),
	})
	if err != nil {
		return
	}
	// One frame on channel 0 carries it; the Noise session is what protects it.
	_ = l.Write(ctx, wireData(body))
}

// wireData wraps a payload in the one frame this exchange sends.
func wireData(body []byte) wire.Frame {
	return wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: body}
}

// staticKey is this API's Noise key pair for the keys link.
func (j *Join) staticKey() (noise.DHKey, error) {
	pub, err := j.noisePublic()
	if err != nil {
		return noise.DHKey{}, err
	}
	return noise.DHKey{Private: j.S.Cfg.NoiseKey.Bytes(), Public: pub}, nil
}
