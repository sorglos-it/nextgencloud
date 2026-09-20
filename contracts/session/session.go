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
	Cert     enc.Bytes    `json:"cert"` // ClientCert as JSON, exactly the bytes the proof covers
	Proof    proof.Proof  `json:"proof"`
	Password *proof.Proof `json:"password,omitempty"` // second proof by the password key if a role asks for it
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

// TrustChain answers PathTrust?from=N with versions N to the newest.
type TrustChain struct {
	Versions []trust.Signed `json:"versions"`
}

// SetupInfo answers GET PathSetup while no admin exists.
type SetupInfo struct {
	Installation string       `json:"installation"`
	Nodes        []trust.Node `json:"nodes"` // the API servers for version 1 of the trust list
	Now          int64        `json:"now"`
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
)

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

// VerifyClient checks a client certificate and the user's proof over it
// against the trust list. The API checks the nonce in addition.
func VerifyClient(payload []byte, p proof.Proof, l *trust.List, now int64) (ClientCert, error) {
	var c ClientCert
	if err := enc.DecodeStrict(payload, MaxCert, &c); err != nil {
		return ClientCert{}, ErrCert
	}
	if err := c.Validate(); err != nil || c.Installation != l.Installation {
		return ClientCert{}, ErrCert
	}
	owner, key, ok := l.KeyOwner(p.KeyID)
	if !ok || owner.ID != c.User {
		return ClientCert{}, ErrUser
	}
	if err := proof.Verify(p, key, l.RP, sign.PurposeClientCert, payload); err != nil {
		return ClientCert{}, err
	}
	if now < c.Issued-MaxSkew || now >= c.Expires {
		return ClientCert{}, ErrExpired
	}
	return c, nil
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
// list and bound to the client certificate client.
func VerifyAPI(env sign.Envelope, l *trust.List, client []byte, now int64) (APICert, error) {
	var node trust.Node
	for _, n := range l.Nodes {
		if n.Kind == trust.NodeAPI && sign.KeyID(n.Sign) == env.KeyID {
			node = n
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
		len(c.DH) != 32 || c.Issued <= 0 || c.Expires <= c.Issued || c.Expires-c.Issued > MaxLifetime {
		return APICert{}, ErrCert
	}
	if now < c.Issued-MaxSkew || now >= c.Expires {
		return APICert{}, ErrExpired
	}
	return c, nil
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
