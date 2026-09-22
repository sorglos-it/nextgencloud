// Package tunnel defines the link between API and controller (contract V2):
// pairing and joining, the hello exchange, status reports and the OPEN payload
// the API sends to a controller.
package tunnel

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Paths on the API listener, names and limits. Times in seconds.
const (
	PathTunnel     = "/tunnel"      // WebSocket; query key=<Fingerprint of the controller's Noise key>; TLS verified with PinnedTLS, Noise authenticates
	PathPair       = "/tunnel/pair" // POST PairRequest
	PathJoin       = "/tunnel/join" // POST JoinRequest
	Subprotocol    = "ngc.tunnel.v1"
	PairPrefix     = "ngc-pair:"
	JoinPrefix     = "ngc-join:"
	SecretSize     = 16
	PairLifetime   = 86400
	MaxPairCode    = 1024 // also for join codes
	MaxURL         = 256  // longest PairCode.URL and JoinCode.URL
	MaxPairRequest = 4096 // also for join requests
	MaxHello       = 4096
	MaxStatusApps  = 256 // at the longest app ID and version a status still fits one frame, with room to spare
	PingInterval   = 30
	ReconnectMin   = 1
	ReconnectMax   = 60
	Contract       = 1 // version of the contracts; every part also understands the previous one
)

// Roles a server carries.
const (
	RoleWeb      = "web"
	RoleAPI      = "api"
	RoleDB       = "db"
	RoleServices = "services"
)

// maxRoles is the number of roles, the most one server can carry.
const maxRoles = 4

// ValidRole reports whether r is one of the Role constants.
func ValidRole(r string) bool {
	return r == RoleWeb || r == RoleAPI || r == RoleDB || r == RoleServices
}

// Components returns the NGC software a server with these roles runs, sorted:
// always trust.AutoController ("ngc-controller"), plus trust.AutoAPI for
// RoleAPI and trust.AutoWeb for RoleWeb. RoleDB runs the migrations from the
// ngc-api release file without installing it; RoleServices runs app containers.
func Components(roles []string) []string {
	c := []string{trust.AutoController}
	if slices.Contains(roles, RoleAPI) {
		c = append(c, trust.AutoAPI)
	}
	if slices.Contains(roles, RoleWeb) {
		c = append(c, trust.AutoWeb)
	}
	slices.Sort(c)
	return c
}

// PairCode is handed from the API to the ulh script of a new controller:
// PairPrefix + base64url(JSON). The controller POSTs its PairRequest to
// https://<host:port of URL> + PathPair, verified with PinnedTLS(TLS).
type PairCode struct {
	URL    string       `json:"url"`    // wss://host:port/tunnel of the issuing API
	Key    enc.Bytes    `json:"key"`    // its Noise key
	TLS    enc.Bytes    `json:"tls"`    // the pin of the issuing API's TLS key (trust.TLSPin)
	Secret enc.Bytes    `json:"secret"` // one use, valid for PairLifetime
	Anchor trust.Anchor `json:"anchor"` // trust list version the controller pins
}

// PairRequest is what the controller sends to PathPair.
type PairRequest struct {
	Name  string    `json:"name"`  // the controller's name (config.Controller.Name, wire.ValidName), shown to the admin
	Noise enc.Bytes `json:"noise"` // the controller's X25519 key
	MAC   enc.Bytes `json:"mac"`   // PairMAC(secret, Noise, API key)
}

// JoinCode is handed from a running API to the ulh script of a new API
// server (answer.yaml join_code): JoinPrefix + base64url(JSON). The new
// server POSTs its JoinRequest to https://<host:port of URL> + PathJoin,
// verified with PinnedTLS(TLS). After an admin put the new server into the
// trust list, it fetches the shared keys from the issuing API; that exchange
// is internal to the API part.
type JoinCode struct {
	URL    string       `json:"url"`    // wss://host:port/tunnel of the issuing API
	Key    enc.Bytes    `json:"key"`    // its Noise key
	TLS    enc.Bytes    `json:"tls"`    // the pin of the issuing API's TLS key (trust.TLSPin)
	Secret enc.Bytes    `json:"secret"` // one use, valid for PairLifetime
	Anchor trust.Anchor `json:"anchor"`
}

// JoinRequest is what a new API server sends to PathJoin: everything it asks
// to be listed with as a trust.Node of kind api. The admin compares
// JoinFingerprint over these values with the output of the new server's ulh
// script before adding it.
type JoinRequest struct {
	Node  string    `json:"node"`  // node ID it gets in the trust list (wire.ValidName)
	Noise enc.Bytes `json:"noise"` // its X25519 key
	Sign  enc.Bytes `json:"sign"`  // its Ed25519 key for API certificates
	TLS   enc.Bytes `json:"tls"`   // the pin of its own TLS key (trust.TLSPin)
	URL   string    `json:"url"`   // its own tunnel URL, wss://host:port/tunnel
	MAC   enc.Bytes `json:"mac"`   // JoinMAC(secret, Noise, Sign, TLS, key of the issuing API, URL)
}

// Hello is the first message on channel 0, in both directions.
type Hello struct {
	Version  string `json:"version"` // NGC version of the sender
	Contract int    `json:"contract"`
	// Node is the node whose Noise key made this tunnel; the API ends the
	// tunnel when it names another; empty until the controller is in the
	// trust list.
	Node     string   `json:"node,omitempty"`
	TrustSeq uint64   `json:"trustSeq"`        // newest trust list version the sender verified
	Arch     string   `json:"arch,omitempty"`  // controller: amd64 or arm64
	OS       string   `json:"os,omitempty"`    // controller: e.g. debian-13, ubuntu-24.04
	Roles    []string `json:"roles,omitempty"` // controller: the roles of its server (Role constants)
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

// Status is the body of a status message from a controller. A controller
// without apps leaves Apps out, as a producer leaves out every empty list.
type Status struct {
	Apps       []AppStatus       `json:"apps,omitempty"`
	Components map[string]string `json:"components,omitempty"` // component (trust.AutoWeb, AutoAPI, AutoController) -> installed version
	DiskFree   int64             `json:"diskFree"`             // bytes free for app data
}

// components are the keys Status.Components may have.
var components = []string{trust.AutoWeb, trust.AutoAPI, trust.AutoController}

// Open is the payload of an OPEN from the API to a controller.
type Open struct {
	Open   enc.Bytes    `json:"open"`             // an OPEN payload: the browser's, which the API must forward unchanged, or the API's own for a channel without a user
	Window uint32       `json:"window"`           // credit the API grants the controller on this channel
	Client enc.Bytes    `json:"client,omitempty"` // client certificate payload exactly as the user's proof covers it
	Proof  *proof.Proof `json:"proof,omitempty"`  // the user's proof over Client
}

// Opened is a checked OPEN from the API.
type Opened struct {
	// Open is the OPEN as the API forwarded it, and the API can put any OPEN
	// here. Only two things bind it to what the shell sent: on an app channel
	// the end-to-end prologue, which hashes its params and task
	// (noisep.E2EPrologue), and the signature of its task (task.Check).
	// Params and TaskBytes keep the bytes as they arrived, for the prologue.
	Open   wire.Open
	Window uint32 // credit on the tunnel hop
	// Session is nil for a channel without a user. It proves who opened the
	// channel only together with the end-to-end handshake (its DH key) or a
	// task (its Sig key): the API keeps the certificate and proof of every
	// session and can attach any of them; on other channels (stage, trust)
	// the controller neither authorises nor logs a user by it.
	Session *session.Verified
}

// Every parser below answers any failure with its own error alone: the JSON
// decoder's errors quote member names and parts of the input, which may hold
// a secret.
var (
	ErrPairCode  = errors.New("tunnel: malformed pairing code")
	ErrPair      = errors.New("tunnel: pairing request rejected")
	ErrJoinCode  = errors.New("tunnel: malformed join code")
	ErrJoin      = errors.New("tunnel: join request rejected")
	ErrCodeTrust = errors.New("tunnel: code does not match the trust list")
	ErrHello     = errors.New("tunnel: malformed hello")
	ErrStatus    = errors.New("tunnel: malformed status")
)

// code holds what pairing and join codes share: the same fields, rules and
// limits, told apart only by their prefix.
type code struct {
	URL    string       `json:"url"`
	Key    enc.Bytes    `json:"key"`
	TLS    enc.Bytes    `json:"tls"`
	Secret enc.Bytes    `json:"secret"`
	Anchor trust.Anchor `json:"anchor"`
}

// codeBudget bounds a pairing or join code while it is decoded: it has no
// array.
var codeBudget = enc.Budget{Size: MaxPairCode}

func (c code) encode(prefix string) string {
	b, _ := json.Marshal(c)
	return prefix + enc.B64.EncodeToString(b)
}

func parseCode(prefix, s string) (code, bool) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok || len(s) > MaxPairCode {
		return code{}, false
	}
	b, err := enc.B64.DecodeString(rest)
	if err != nil {
		return code{}, false
	}
	var c code
	if codeBudget.Decode(b, &c) != nil {
		return code{}, false
	}
	if !strings.HasPrefix(c.URL, "wss://") || len(c.URL) > MaxURL || len(c.Key) != 32 || len(c.TLS) != sha256.Size ||
		len(c.Secret) != SecretSize || c.Anchor.Seq == 0 || len(c.Anchor.Hash) != sha256.Size {
		return code{}, false
	}
	return c, true
}

// check compares c with v, the newest trust list version the shell verified.
// Key, URL and TLS must all belong to one API node: the code's receiver
// dials URL and trusts the TLS key and Noise key the code names.
func (c code) check(v trust.Version) error {
	if v.List == nil || c.Anchor.Seq != v.List.Seq || !bytes.Equal(c.Anchor.Hash, v.Hash) {
		return ErrCodeTrust
	}
	for _, n := range v.List.Nodes {
		if n.Kind == trust.NodeAPI && bytes.Equal(n.Noise, c.Key) && n.URL == c.URL && bytes.Equal(n.TLS, c.TLS) {
			return nil
		}
	}
	return ErrCodeTrust
}

// String encodes c for copying: PairPrefix followed by base64url of the JSON.
func (c PairCode) String() string { return code(c).encode(PairPrefix) }

// ParsePairCode decodes and checks a pairing code.
func ParsePairCode(s string) (PairCode, error) {
	c, ok := parseCode(PairPrefix, s)
	if !ok {
		return PairCode{}, ErrPairCode
	}
	return PairCode(c), nil
}

// Check compares a pairing code with the newest trust list version the shell
// has verified: the anchor must be exactly that version (Seq and Hash), and
// Key, URL and TLS must be the Noise key, tunnel URL and TLS pin of one API
// node in it. If the code names a newer version, the shell first fetches and
// verifies it (session.PathTrust) and checks again.
func (c PairCode) Check(v trust.Version) error { return code(c).check(v) }

// String encodes c for copying: JoinPrefix followed by base64url of the JSON.
func (c JoinCode) String() string { return code(c).encode(JoinPrefix) }

// ParseJoinCode decodes and checks a join code: the rules and limits of
// ParsePairCode, with JoinPrefix.
func ParseJoinCode(s string) (JoinCode, error) {
	c, ok := parseCode(JoinPrefix, s)
	if !ok {
		return JoinCode{}, ErrJoinCode
	}
	return JoinCode(c), nil
}

// Check compares a join code with the newest trust list version the shell has
// verified, as PairCode.Check does.
func (c JoinCode) Check(v trust.Version) error { return code(c).check(v) }

// PairMAC binds a controller key and an API key to the pairing secret.
func PairMAC(secret, controllerKey, apiKey []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("ngc/pair/v1"))
	m.Write([]byte{0})
	m.Write(controllerKey)
	m.Write(apiKey)
	return m.Sum(nil)
}

// requestBudget bounds a pairing or join request while it is decoded: it has
// no array.
var requestBudget = enc.Budget{Size: MaxPairRequest}

// ParsePairRequest decodes and checks a pairing request; Verify checks the MAC.
func ParsePairRequest(b []byte) (PairRequest, error) {
	var r PairRequest
	if requestBudget.Decode(b, &r) != nil || !wire.ValidName(r.Name) || len(r.Noise) != 32 || len(r.MAC) != sha256.Size {
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

// JoinMAC = HMAC-SHA256(secret, "ngc/join/v1" 0x00 noise sign tls apiKey url):
// noise, sign, tls and apiKey have 32 bytes each, url comes last.
func JoinMAC(secret, noise, sign, tls, apiKey []byte, url string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("ngc/join/v1"))
	m.Write([]byte{0})
	m.Write(noise)
	m.Write(sign)
	m.Write(tls)
	m.Write(apiKey)
	m.Write([]byte(url))
	return m.Sum(nil)
}

// JoinFingerprint names what a joining API asks to be listed with, for the
// admin to compare: base64url of the first 16 bytes of SHA-256 over
// "ngc/join-fingerprint/v1", a zero byte, noise, sign, tls and url. The new
// server's ulh script shows it; the shell computes it from core.PendingAPI.
func JoinFingerprint(noise, sign, tls []byte, url string) string {
	h := sha256.New()
	h.Write([]byte("ngc/join-fingerprint/v1"))
	h.Write([]byte{0})
	h.Write(noise)
	h.Write(sign)
	h.Write(tls)
	h.Write([]byte(url))
	return enc.B64.EncodeToString(h.Sum(nil)[:16])
}

// ParseJoinRequest decodes and checks a join request: a node ID
// (wire.ValidName), 32 bytes each of Noise, Sign and TLS, a wss:// URL of at
// most MaxURL bytes and a 32-byte MAC. Verify checks the MAC.
func ParseJoinRequest(b []byte) (JoinRequest, error) {
	var r JoinRequest
	if requestBudget.Decode(b, &r) != nil || !wire.ValidName(r.Node) || len(r.Noise) != 32 || len(r.Sign) != 32 ||
		len(r.TLS) != sha256.Size || !strings.HasPrefix(r.URL, "wss://") || len(r.URL) > MaxURL || len(r.MAC) != sha256.Size {
		return JoinRequest{}, ErrJoin
	}
	return r, nil
}

// Verify checks the MAC with the secret the API issued and its own Noise key.
func (r JoinRequest) Verify(secret, apiKey []byte) error {
	if !hmac.Equal(r.MAC, JoinMAC(secret, r.Noise, r.Sign, r.TLS, apiKey, r.URL)) {
		return ErrJoin
	}
	return nil
}

// Fingerprint names a Noise key for people and in the tunnel URL.
func Fingerprint(key []byte) string { return sign.KeyID(key) }

// errPin refuses a TLS server whose key is none of the pins.
var errPin = errors.New("tunnel: TLS key does not match the pin")

// PinnedTLS returns the client configuration for NGC's TLS links to an API
// (tunnel, pairing and join requests, and the web server's controller
// fetching certificates for Apache): the server's leaf certificate must hold
// a key whose trust.TLSPin is one of pins; who signed it and its names do
// not matter, as with SSH host keys. Go skips its own chain check only with
// InsecureSkipVerify, so this sets it and checks the pin in
// VerifyConnection, which runs on every handshake and resumption: no
// connection is accepted without the pin. TLS 1.3 only; with no pins every
// handshake fails. Set ServerName yourself where the server needs SNI.
func PinnedTLS(pins ...[]byte) *tls.Config {
	own := make([][]byte, len(pins))
	for i, p := range pins {
		own[i] = bytes.Clone(p)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		// TLS 1.3 has the server prove it holds the leaf's key
		// (CertificateVerify) whatever this says; only the chain and the
		// names go unchecked, and the pin stands in for them.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errPin
			}
			pin := trust.TLSPin(cs.PeerCertificates[0].RawSubjectPublicKeyInfo)
			for _, p := range own {
				if bytes.Equal(p, pin) {
					return nil
				}
			}
			return errPin
		},
	}
}

var (
	versionPattern = regexp.MustCompile(`^[0-9a-z.+-]{1,32}$`)
	osPattern      = regexp.MustCompile(`^[a-z0-9.+-]{1,32}$`)
)

// helloBudget bounds a hello while it is decoded: its only array is the
// server's roles.
var helloBudget = enc.Budget{Size: MaxHello, Items: map[string]int{"roles": maxRoles}}

// ParseHello decodes and checks a hello message.
func ParseHello(b []byte) (Hello, error) {
	var h Hello
	if helloBudget.Decode(b, &h) != nil {
		return Hello{}, ErrHello
	}
	if !versionPattern.MatchString(h.Version) || h.Contract < 1 || (h.Node != "" && !wire.ValidName(h.Node)) ||
		(h.Arch != "" && h.Arch != "amd64" && h.Arch != "arm64") || (h.OS != "" && !osPattern.MatchString(h.OS)) ||
		len(h.Roles) > maxRoles {
		return Hello{}, ErrHello
	}
	for i, r := range h.Roles {
		if !ValidRole(r) || slices.Contains(h.Roles[:i], r) {
			return Hello{}, ErrHello
		}
	}
	return h, nil
}

// statusBudget bounds a status while it is decoded: at most MaxStatusApps
// apps, and a map with one version per component.
var statusBudget = enc.Budget{Size: wire.MaxPayload, Items: map[string]int{
	"apps":       MaxStatusApps,
	"components": len(components),
}}

// ParseStatus decodes and checks the body of a status message: at most
// MaxStatusApps apps, each with a valid non-core app ID (no duplicates),
// store.ValidVersion and a known AppState; Components keys only ngc-web,
// ngc-api, ngc-controller with store.ValidVersion values; DiskFree >= 0.
func ParseStatus(b []byte) (Status, error) {
	var s Status
	if statusBudget.Decode(b, &s) != nil || len(s.Apps) > MaxStatusApps || s.DiskFree < 0 {
		return Status{}, ErrStatus
	}
	seen := make(map[string]bool, len(s.Apps))
	for _, a := range s.Apps {
		if !wire.ValidAppID(a.App) || wire.CoreApp(a.App) || seen[a.App] || !store.ValidVersion(a.Version) {
			return Status{}, ErrStatus
		}
		switch a.State {
		case StateRunning, StateStopped, StateFailed, StateUnlicensed:
		default:
			return Status{}, ErrStatus
		}
		seen[a.App] = true
	}
	for c, v := range s.Components {
		if !slices.Contains(components, c) || !store.ValidVersion(v) {
			return Status{}, ErrStatus
		}
	}
	return s, nil
}

// openBudget bounds an OPEN on the tunnel while it is decoded: it has no
// array, and the browser's OPEN inside it is bytes, which wire.ParseOpen
// decodes within its own budget.
var openBudget = enc.Budget{Size: wire.MaxOpenPayload}

// ParseOpen decodes and checks the OPEN payload on the tunnel and the
// browser's OPEN inside it: wire.MaxOpenPayload bytes; wire.ParseOpen(o.Open);
// MinWindow <= Window <= MaxWindow; Client and Proof both or neither;
// len(Client) <= session.MaxCert; Proof.Validate(). Every failure is
// wire.ErrOpen.
func ParseOpen(p []byte) (Open, wire.Open, error) {
	var o Open
	if openBudget.Decode(p, &o) != nil || o.Window < wire.MinWindow || o.Window > wire.MaxWindow ||
		(len(o.Client) == 0) != (o.Proof == nil) || len(o.Client) > session.MaxCert ||
		(o.Proof != nil && o.Proof.Validate() != nil) {
		return Open{}, wire.Open{}, wire.ErrOpen
	}
	inner, err := wire.ParseOpen(o.Open)
	if err != nil {
		return Open{}, wire.Open{}, wire.ErrOpen
	}
	return o, inner, nil
}

// VerifyOpen parses p and, if it carries a client certificate, verifies it
// against l at now (session.VerifyClient). The user of the channel is
// Session.Cert().User and nothing else. A channel without a client
// certificate is allowed only for the actions core.WithoutUser lists; the
// controller checks that, and for install also core.Unattended.
//
// A role that asks for the password too (trust.List.PasswordToo) is enforced
// by the API at login (session.VerifyPassword); no password proof travels to
// a controller, which relies on the passkey proof that session.VerifyClient
// requires of such users.
func VerifyOpen(p []byte, l *trust.List, now int64) (Opened, error) {
	o, inner, err := ParseOpen(p)
	if err != nil {
		return Opened{}, err
	}
	opened := Opened{Open: inner, Window: o.Window}
	if o.Proof == nil {
		return opened, nil
	}
	v, err := session.VerifyClient(o.Client, *o.Proof, l, now)
	if err != nil {
		return Opened{}, err
	}
	opened.Session = &v
	return opened, nil
}
