// Package session defines the login, the session certificates and the HTTP
// paths used before the WebSocket (contract V1).
package session

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
)

// HTTP paths on the web server; Apache passes /api/ on to the APIs.
//
// GET PathTrust?from=N pages through the versions of the trust list as
// TrustChain says. The API tells the shell with the control message "trust"
// (body TrustHint) that a newer version exists. Controllers get versions on
// the channel ngc.users/controller/trust instead.
const (
	PathSetup       = "/api/v1/setup"
	PathLoginStart  = "/api/v1/login/start"
	PathLoginFinish = "/api/v1/login/finish"
	PathLogout      = "/api/v1/logout"
	PathTrust       = "/api/v1/trust"
	PathSocket      = "/api/v1/ws"
	Subprotocol     = "ngc.v1"
	CookieName      = "ngc_session"
)

// Sizes and times in seconds.
const (
	TokenSize         = 32
	NonceSize         = 32
	MaxLifetime       = 86400 // longest session; the API may configure less
	MaxSkew           = 300   // tolerated clock difference between the servers
	ChallengeLifetime = 120
	MaxCert           = 4096
	MaxRequest        = 64 << 10 // largest JSON body before the WebSocket
	TOTPDigits        = 6
	TOTPPeriod        = 30 // RFC 6238 with HMAC-SHA1, as authenticator apps expect
)

// Limits of one answer of PathTrust.
const (
	MaxChainVersions = 64       // versions in one TrustChain
	MaxChain         = 32 << 20 // bytes of one TrustChain as JSON
)

// maxTrustHint bounds a TrustHint body; {"seq":N} needs at most 28 bytes.
const maxTrustHint = 64

// LoginStart asks for a login challenge.
type LoginStart struct {
	Name string `json:"name"`
}

// LoginChallenge tells the browser how the user logs in. For unknown names
// the API answers with made-up but stable values.
type LoginChallenge struct {
	Nonce        enc.Bytes   `json:"nonce"`
	Now          int64       `json:"now"`
	MaxLifetime  int64       `json:"maxLifetime"` // ClientCert.Expires <= Now + MaxLifetime
	Installation string      `json:"installation"`
	User         string      `json:"user"`
	KDF          *proof.KDF  `json:"kdf,omitempty"`      // set if the user may log in with a password
	Passkeys     []enc.Bytes `json:"passkeys,omitempty"` // credential IDs for allowCredentials
	TOTP         bool        `json:"totp"`
	PasswordToo  bool        `json:"passwordToo,omitempty"` // a role asks for the password in addition to a passkey
	RP           proof.RP    `json:"rp"`
}

// ClientCert binds the browser's session keys to a user. The browser builds
// it, the user signs it with a passkey or password key (purpose clientcert/v1).
type ClientCert struct {
	Installation string    `json:"inst"`
	User         string    `json:"user"`
	Nonce        enc.Bytes `json:"nonce"` // from LoginChallenge
	Sig          enc.Bytes `json:"sig"`   // Ed25519: signs tasks (package task)
	DH           enc.Bytes `json:"dh"`    // X25519: Noise key for the session and for end-to-end channels
	Issued       int64     `json:"iat"`
	Expires      int64     `json:"exp"`
}

// LoginFinish sends the signed client certificate.
type LoginFinish struct {
	Cert  enc.Bytes   `json:"cert"` // ClientCert as JSON, exactly the bytes the proof covers
	Proof proof.Proof `json:"proof"`
	// Password is a proof by a password key of the same user over the same
	// Cert bytes, purpose clientcert/v1 (VerifyPassword). The browser sends
	// it when LoginChallenge.PasswordToo is set.
	Password *proof.Proof `json:"password,omitempty"`
	TOTP     string       `json:"totp,omitempty"`
}

// APICert binds the API side of a session to the client certificate. An API
// node signs it with its key from the trust list (purpose apicert/v1).
type APICert struct {
	Installation string    `json:"inst"`
	Node         string    `json:"node"`
	Client       enc.Bytes `json:"client"` // CertHash of the client certificate
	DH           enc.Bytes `json:"dh"`     // X25519 key of the API for this session
	Issued       int64     `json:"iat"`
	Expires      int64     `json:"exp"`
}

// LoginResult answers LoginFinish; the session token comes as cookie.
type LoginResult struct {
	API          sign.Envelope `json:"api"`
	PasswordHint bool          `json:"passwordHint,omitempty"` // logged in with a password: show the hint to switch to passkeys
}

// TrustChain answers GET PathTrust?from=N: versions from N (inclusive) on,
// at most MaxChainVersions and MaxChain bytes. More tells that newer
// versions exist; the shell asks again from the one after the last.
type TrustChain struct {
	Versions []trust.Signed `json:"versions"`
	More     bool           `json:"more,omitempty"`
}

// TrustHint is the body of the control message "trust" from the API to the
// shell: version Seq exists. The shell fetches it with GET PathTrust.
type TrustHint struct {
	Seq uint64 `json:"seq"`
}

// SetupInfo answers GET PathSetup while no admin exists.
type SetupInfo struct {
	Installation string       `json:"installation"`
	Nodes        []trust.Node `json:"nodes"` // the API servers for version 1 of the trust list
	Now          int64        `json:"now"`
	// Store is the global store as the API is configured (config.API.StoreURL,
	// StoreKey), ID trust.StoreGlobal, name "NGC Store". The shell lists it in
	// version 1 unless the admin switches it off.
	Store *trust.Store `json:"store,omitempty"`
}

// Setup creates the first admin with version 1 of the trust list.
type Setup struct {
	Code  string       `json:"code"` // one-time code shown by the ulh script
	Trust trust.Signed `json:"trust"`
}

var (
	ErrCert    = errors.New("session: malformed certificate")
	ErrUser    = errors.New("session: proof is not by a key of this user")
	ErrExpired = errors.New("session: certificate not valid at this time")
	ErrMessage = errors.New("session: malformed message")
	ErrSetup   = errors.New("session: version 1 does not match the setup")
)

// The parsers below answer every failure with ErrMessage alone: the decoder's
// own errors quote parts of the input, which may hold a code or a proof.

// ParseLoginStart decodes and checks the body of PathLoginStart.
func ParseLoginStart(b []byte) (LoginStart, error) {
	var s LoginStart
	if enc.DecodeStrict(b, MaxRequest, &s) != nil || !trust.ValidLoginName(s.Name) {
		return LoginStart{}, ErrMessage
	}
	return s, nil
}

// ParseLoginFinish decodes and checks the body of PathLoginFinish. It checks
// the structure only; VerifyClient and VerifyPassword check the proofs.
func ParseLoginFinish(b []byte) (LoginFinish, error) {
	var f LoginFinish
	if enc.DecodeStrict(b, MaxRequest, &f) != nil || len(f.Cert) == 0 || len(f.Cert) > MaxCert ||
		f.Proof.Validate() != nil ||
		(f.Password != nil && (f.Password.Kind != proof.KindPassword || f.Password.Validate() != nil)) ||
		(f.TOTP != "" && !ValidTOTP(f.TOTP)) {
		return LoginFinish{}, ErrMessage
	}
	return f, nil
}

// ParseSetup decodes and checks the body of POST PathSetup. VerifySetup
// checks the version it carries.
func ParseSetup(b []byte) (Setup, error) {
	var s Setup
	if enc.DecodeStrict(b, MaxRequest, &s) != nil || !ValidSetupCode(s.Code) || len(s.Trust.Payload) == 0 ||
		s.Trust.Proof.Validate() != nil {
		return Setup{}, ErrMessage
	}
	return s, nil
}

// ParseTrustChain decodes and checks an answer of PathTrust. trust.Open and
// trust.Advance check the versions themselves.
func ParseTrustChain(b []byte) (TrustChain, error) {
	var c TrustChain
	if enc.DecodeStrict(b, MaxChain, &c) != nil || len(c.Versions) == 0 || len(c.Versions) > MaxChainVersions {
		return TrustChain{}, ErrMessage
	}
	for _, v := range c.Versions {
		if len(v.Payload) == 0 || len(v.Payload) > sign.MaxPayload || v.Proof.Validate() != nil {
			return TrustChain{}, ErrMessage
		}
	}
	return c, nil
}

// ParseTrustHint decodes and checks the body of the control message "trust".
func ParseTrustHint(b []byte) (TrustHint, error) {
	var h TrustHint
	if enc.DecodeStrict(b, maxTrustHint, &h) != nil || h.Seq < 1 {
		return TrustHint{}, ErrMessage
	}
	return h, nil
}

// Validate checks the fields of c.
func (c ClientCert) Validate() error {
	if !trust.ValidID(c.Installation) || !trust.ValidID(c.User) || len(c.Nonce) != NonceSize ||
		len(c.Sig) != ed25519.PublicKeySize || len(c.DH) != 32 ||
		c.Issued <= 0 || c.Expires <= c.Issued || c.Expires-c.Issued > MaxLifetime {
		return ErrCert
	}
	return nil
}

// CertHash returns the SHA-256 of a client certificate payload.
func CertHash(client []byte) []byte {
	h := sha256.Sum256(client)
	return h[:]
}

// TokenHash returns the SHA-256 of a session token; only the hash is stored.
func TokenHash(token []byte) []byte {
	h := sha256.Sum256(token)
	return h[:]
}

// Verified is a client certificate whose proof has been checked against the
// trust list. Only VerifyClient makes one, so whoever holds a Verified knows
// the chain back to the user's key was checked.
type Verified struct {
	cert    ClientCert
	payload []byte
}

// Cert returns the certificate.
func (v Verified) Cert() ClientCert { return v.cert }

// Payload returns the exact bytes the user's proof covers.
func (v Verified) Payload() []byte { return v.payload }

// VerifyClient checks a client certificate and the user's proof over it
// against the trust list. The API checks the nonce in addition.
func VerifyClient(payload []byte, p proof.Proof, l *trust.List, now int64) (Verified, error) {
	var c ClientCert
	if err := enc.DecodeStrict(payload, MaxCert, &c); err != nil {
		return Verified{}, ErrCert
	}
	if err := c.Validate(); err != nil || c.Installation != l.Installation {
		return Verified{}, ErrCert
	}
	owner, key, ok := l.KeyOwner(p.KeyID)
	if !ok || owner.ID != c.User {
		return Verified{}, ErrUser
	}
	if err := proof.Verify(p, key, l.RP, sign.PurposeClientCert, payload); err != nil {
		return Verified{}, err
	}
	if now < c.Issued-MaxSkew || now >= c.Expires {
		return Verified{}, ErrExpired
	}
	return Verified{cert: c, payload: payload}, nil
}

// VerifyPassword checks the second proof of a role that asks for the password
// too: by a password key of the user of v, over v.Payload(), purpose
// clientcert/v1. ErrUser if the key is not a password key of that user.
func VerifyPassword(v Verified, p proof.Proof, l *trust.List) error {
	owner, key, ok := l.KeyOwner(p.KeyID)
	if !ok || owner.ID != v.Cert().User || key.Kind != proof.KindPassword {
		return ErrUser
	}
	return proof.Verify(p, key, l.RP, sign.PurposeClientCert, v.Payload())
}

// SignAPI signs an API certificate with the key of an API node.
func SignAPI(priv ed25519.PrivateKey, c APICert) (sign.Envelope, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return sign.Envelope{}, err
	}
	return sign.Sign(priv, sign.PurposeAPICert, b)
}

// VerifyAPI checks an API certificate: signed by an API node of the trust
// list, bound to the client certificate client and never valid longer than
// it, so a session cannot outlive the user's approval.
func VerifyAPI(env sign.Envelope, l *trust.List, client []byte, now int64) (APICert, error) {
	var cc ClientCert
	if err := enc.DecodeStrict(client, MaxCert, &cc); err != nil {
		return APICert{}, ErrCert
	}
	var node trust.Node
	for _, n := range l.Nodes {
		if n.Kind == trust.NodeAPI && sign.KeyID(n.Sign) == env.KeyID {
			node = n
			break
		}
	}
	if node.ID == "" {
		return APICert{}, ErrCert
	}
	if err := sign.Verify(env, ed25519.PublicKey(node.Sign), sign.PurposeAPICert); err != nil {
		return APICert{}, err
	}
	var c APICert
	if err := enc.DecodeStrict(env.Payload, MaxCert, &c); err != nil {
		return APICert{}, ErrCert
	}
	if c.Installation != l.Installation || c.Node != node.ID || !bytes.Equal(c.Client, CertHash(client)) ||
		len(c.DH) != 32 || c.Issued <= 0 || c.Expires <= c.Issued || c.Expires-c.Issued > MaxLifetime ||
		c.Expires > cc.Expires {
		return APICert{}, ErrCert
	}
	if now < c.Issued-MaxSkew || now >= c.Expires {
		return APICert{}, ErrExpired
	}
	return c, nil
}

// VerifySetup checks version 1 the new admin sent against what the API
// offered in SetupInfo: trust.Open with the anchor {1, trust.Hash(s.Trust)}
// (so an admin in the list signed it), the same Installation, Nodes equal to
// info.Nodes (compared as encoding/json output), and Stores either empty or
// exactly the global store info.Store. Returns trust errors or ErrSetup.
func VerifySetup(s Setup, info SetupInfo) (trust.Version, error) {
	v, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(s.Trust)}, []trust.Signed{s.Trust})
	if err != nil {
		return trust.Version{}, err
	}
	l := v.List
	if l.Installation != info.Installation || !sameNodes(l.Nodes, info.Nodes) {
		return trust.Version{}, ErrSetup
	}
	if len(l.Stores) != 0 && (info.Store == nil || len(l.Stores) != 1 || !sameJSON(l.Stores[0], *info.Store)) {
		return trust.Version{}, ErrSetup
	}
	return v, nil
}

// sameNodes compares node by node, so that no nodes at all match whether
// they are nil or empty.
func sameNodes(a, b []trust.Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameJSON(a[i], b[i]) {
			return false
		}
	}
	return true
}

// sameJSON reports whether a and b encode to the same JSON: the form the
// admin signed and the API offered.
func sameJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

var setupCode = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{5}(-[A-HJ-NP-Z2-9]{5}){3}$`)

// ValidSetupCode reports whether s looks like a one-time setup code.
func ValidSetupCode(s string) bool { return setupCode.MatchString(s) }

// ValidTOTP reports whether s is a TOTP code: exactly six ASCII digits.
func ValidTOTP(s string) bool {
	if len(s) != TOTPDigits {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
