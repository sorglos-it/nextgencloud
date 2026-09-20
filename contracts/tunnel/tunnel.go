// Package tunnel defines the link between API and controller (contract V2):
// pairing, the hello exchange, status reports and the OPEN payload the API
// sends to a controller.
package tunnel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Paths on the API listener, names and limits. Times in seconds.
const (
	PathTunnel     = "/tunnel"      // WebSocket; query key=<Fingerprint of the controller's Noise key>
	PathPair       = "/tunnel/pair" // POST PairRequest
	Subprotocol    = "ngc.tunnel.v1"
	PairPrefix     = "ngc-pair:"
	SecretSize     = 16
	PairLifetime   = 86400
	MaxPairCode    = 1024
	MaxURL         = 256 // longest PairCode.URL
	MaxPairRequest = 4096
	MaxHello       = 4096
	PingInterval   = 30
	ReconnectMin   = 1
	ReconnectMax   = 60
	Contract       = 1 // version of the contracts; every part also understands the previous one
)

// Parts of NGC a server can carry.
const (
	PartWeb      = "web"
	PartAPI      = "api"
	PartDB       = "db"
	PartServices = "services"
)

// PairCode is handed from the API to the ulh script of a new controller.
type PairCode struct {
	URL    string       `json:"url"`    // wss://host:port/tunnel of the issuing API
	Key    enc.Bytes    `json:"key"`    // its Noise key
	Secret enc.Bytes    `json:"secret"` // one use, valid for PairLifetime
	Anchor trust.Anchor `json:"anchor"` // trust list version the controller pins
}

// PairRequest is what the controller sends to PathPair.
type PairRequest struct {
	Name  string    `json:"name"`  // host name, shown to the admin
	Noise enc.Bytes `json:"noise"` // the controller's X25519 key
	MAC   enc.Bytes `json:"mac"`   // PairMAC(secret, Noise, API key)
}

// Hello is the first message on channel 0, in both directions.
type Hello struct {
	Version  string   `json:"version"` // NGC version of the sender
	Contract int      `json:"contract"`
	Node     string   `json:"node,omitempty"` // empty for a controller not yet in the trust list
	TrustSeq uint64   `json:"trustSeq"`       // newest trust list version the sender verified
	Arch     string   `json:"arch,omitempty"` // controller: amd64 or arm64
	OS       string   `json:"os,omitempty"`   // controller: e.g. debian-13, ubuntu-24.04
	Parts    []string `json:"parts,omitempty"`
}

// AppState is the state of an app on a controller.
type AppState string

const (
	StateRunning    AppState = "running"
	StateStopped    AppState = "stopped"
	StateFailed     AppState = "failed"
	StateUnlicensed AppState = "unlicensed" // subscription ran out after the grace period
)

// AppStatus reports one app.
type AppStatus struct {
	App     string   `json:"app"`
	Version string   `json:"version"`
	State   AppState `json:"state"`
}

// Status is the body of a status message from a controller.
type Status struct {
	Apps     []AppStatus       `json:"apps"`
	Parts    map[string]string `json:"parts"`    // installed NGC parts and their versions, e.g. "ngc-controller": "1.0.0"
	DiskFree int64             `json:"diskFree"` // bytes free for app data
}

// Open is the OPEN payload from the API to a controller: the browser's OPEN
// plus who asks. Without User only channels covered by a standing approval come.
type Open struct {
	wire.Open
	User   string       `json:"user,omitempty"`
	Client enc.Bytes    `json:"client,omitempty"` // client certificate payload
	Proof  *proof.Proof `json:"proof,omitempty"`  // the user's proof over it
}

var (
	ErrPairCode = errors.New("tunnel: malformed pairing code")
	ErrPair     = errors.New("tunnel: pairing request rejected")
	ErrHello    = errors.New("tunnel: malformed hello")
)

// String encodes c for copying: PairPrefix followed by base64url of the JSON.
func (c PairCode) String() string {
	b, _ := json.Marshal(c)
	return PairPrefix + enc.B64.EncodeToString(b)
}

// ParsePairCode decodes and checks a pairing code.
func ParsePairCode(s string) (PairCode, error) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, PairPrefix)
	if !ok || len(s) > MaxPairCode {
		return PairCode{}, ErrPairCode
	}
	b, err := enc.B64.DecodeString(rest)
	if err != nil {
		return PairCode{}, ErrPairCode
	}
	var c PairCode
	if err := enc.DecodeStrict(b, MaxPairCode, &c); err != nil {
		return PairCode{}, ErrPairCode
	}
	if !strings.HasPrefix(c.URL, "wss://") || len(c.URL) > MaxURL || len(c.Key) != 32 || len(c.Secret) != SecretSize ||
		c.Anchor.Seq == 0 || len(c.Anchor.Hash) != sha256.Size {
		return PairCode{}, ErrPairCode
	}
	return c, nil
}

// PairMAC binds a controller key and an API key to the pairing secret.
func PairMAC(secret, controllerKey, apiKey []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("ngc/pair/v1"))
	m.Write([]byte{0})
	m.Write(controllerKey)
	m.Write(apiKey)
	return m.Sum(nil)
}

var hostName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,62}$`)

// ParsePairRequest decodes and checks a pairing request; Verify checks the MAC.
func ParsePairRequest(b []byte) (PairRequest, error) {
	var r PairRequest
	if err := enc.DecodeStrict(b, MaxPairRequest, &r); err != nil {
		return PairRequest{}, ErrPair
	}
	if !hostName.MatchString(r.Name) || len(r.Noise) != 32 || len(r.MAC) != sha256.Size {
		return PairRequest{}, ErrPair
	}
	return r, nil
}

// Verify checks the MAC with the secret the API issued and its own Noise key.
func (r PairRequest) Verify(secret, apiKey []byte) error {
	if !hmac.Equal(r.MAC, PairMAC(secret, r.Noise, apiKey)) {
		return ErrPair
	}
	return nil
}

// Fingerprint names a Noise key for people and in the tunnel URL.
func Fingerprint(key []byte) string { return sign.KeyID(key) }

var version = regexp.MustCompile(`^[0-9a-z.+-]{1,32}$`)

// ParseHello decodes and checks a hello message.
func ParseHello(b []byte) (Hello, error) {
	var h Hello
	if err := enc.DecodeStrict(b, MaxHello, &h); err != nil {
		return Hello{}, ErrHello
	}
	if !version.MatchString(h.Version) || h.Contract < 1 || (h.Node != "" && !wire.ValidName(h.Node)) ||
		(h.Arch != "" && h.Arch != "amd64" && h.Arch != "arm64") || len(h.OS) > 32 || len(h.Parts) > 4 {
		return Hello{}, ErrHello
	}
	for _, p := range h.Parts {
		if !slices.Contains([]string{PartWeb, PartAPI, PartDB, PartServices}, p) {
			return Hello{}, ErrHello
		}
	}
	return h, nil
}

// ParseOpen decodes and checks the OPEN payload on the tunnel.
func ParseOpen(p []byte) (Open, error) {
	var o Open
	if err := enc.DecodeStrict(p, wire.MaxOpenPayload, &o); err != nil {
		return Open{}, err
	}
	if err := o.Open.Validate(); err != nil {
		return Open{}, err
	}
	noUser := o.User == ""
	if noUser != (len(o.Client) == 0) || noUser != (o.Proof == nil) || len(o.Client) > session.MaxCert ||
		(!noUser && !trust.ValidID(o.User)) {
		return Open{}, wire.ErrOpen
	}
	return o, nil
}
