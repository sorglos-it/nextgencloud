// Package session defines the login, the session certificates and the HTTP
// exchange before the WebSocket (contract V1).
//
// # Before the WebSocket
//
// Until the WebSocket the shell speaks HTTPS with JSON: a body comes with
// Content-Type: application/json (any other type is E_PROTOCOL), and every
// answer carries Cache-Control: no-store. An error answer has the status
// HTTPStatus(code) and as its body the CLOSE payload wire.Error(code).
//
//	Path              Method         Body         Answer                           Errors
//	PathSetup         GET            -            200 SetupOffer                   404 E_NOTFOUND once an admin exists
//	PathSetup         POST           Setup        204                              403 E_VERIFY, 404 E_NOTFOUND,
//	                                                                               429 E_LOCKED
//	PathLoginStart    POST           LoginStart   200 LoginChallenge, for unknown  429 E_LOCKED
//	                                              names too (ParseLoginChallenge)
//	PathLoginFinish   POST           LoginFinish  200 LoginResult and the cookie   401 E_LOGIN, 429 E_LOCKED
//	PathLogout        POST           -            204, the cookie deleted          401 E_LOGIN
//	PathTrust?from=N  GET            -            200 TrustChain from N on; 204    401 E_LOGIN, 404 E_NOTFOUND
//	                                              when N is one past the newest    (N beyond that)
//	                                              version
//	PathSocket        GET (Upgrade)  -            101, subprotocol Subprotocol     401 E_LOGIN
//
// Every path may also answer 400 E_PROTOCOL (a malformed request, or a from
// that ParseFrom refuses), 413 E_TOOLARGE (a body over MaxRequest), 500
// E_INTERNAL and 503 E_UNREACHABLE (the database cannot be reached).
//
// E_LOGIN stands for every failed login alike (an unknown name, a wrong proof
// or TOTP code, a missing password proof, a used or expired nonce) and for a
// missing or expired session, so that no answer tells which part failed or
// whether a user exists. E_LOCKED comes with Retry-After in seconds. The
// session cookie is CookieName with CookieAttributes. Any API serves any
// session: the private DH key of the API certificate lies encrypted in the
// database, so after a failure the shell connects again with the same keys.
//
// Setup: the shell asks for the one-time code first (NormalizeSetupCode) and
// checks the offer with it (ParseSetupOffer: first OfferMAC over the exact
// bytes of the offer's Info, then Info). From it the shell builds version 1
// of the trust list: the admin, the API nodes of Info, the global store of
// Info unless the admin switches it off, and standing approvals only for
// ngc-web, ngc-api and ngc-controller with Info's ReleaseKey and for system.
// It sends version 1 with SetupMAC over the exact bytes of its payload
// (Setup), which the API checks with VerifySetup. The code itself never
// travels, so no one between browser and API learns it, and without it no
// one there can offer other keys or sign a version the API accepts.
package session

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// HTTP paths on the web server; Apache passes /api/ on to the APIs. The
// package comment says what each takes and answers.
//
// GET PathTrust?from=N pages through the versions of the trust list as
// TrustChain says. The API tells the shell with the control message "trust"
// (body TrustHint) that a newer version exists. Controllers get versions on
// the channel ngc.users/controller/trust instead.
const (
	PathSetup       = "/api/v1/setup" // GET answers a SetupOffer, POST takes a Setup
	PathLoginStart  = "/api/v1/login/start"
	PathLoginFinish = "/api/v1/login/finish"
	PathLogout      = "/api/v1/logout"
	PathTrust       = "/api/v1/trust"
	PathSocket      = "/api/v1/ws"
	Subprotocol     = "ngc.v1"
	CookieName      = "ngc_session"
)

// CookieAttributes are the attributes of the session cookie CookieName, whose
// value is base64url of TokenSize random bytes (TokenHash over those bytes is
// what the database keeps): never a Domain, no Max-Age, so it ends with the
// browser; the API ends the session after its IdleTimeout or SessionLifetime
// (config.API). Logout sends the same with an empty value and Max-Age=0.
const CookieAttributes = "Path=/api/; Secure; HttpOnly; SameSite=Strict"

// Sizes and times in seconds.
const (
	TokenSize         = 32
	NonceSize         = 32
	MaxLifetime       = 86400 // longest session; the API may configure less
	MaxSkew           = 300   // tolerated clock difference between the servers
	ChallengeLifetime = 120
	MaxCert           = 4096
	MaxRequest        = 64 << 10  // largest JSON body before the WebSocket
	MaxSetupOffer     = 128 << 10 // a SetupOffer as JSON: the base64url of an Info of up to MaxRequest bytes, and the MAC
	MaxSetupNodes     = 16        // API nodes in one SetupInfo
	// MaxPasskeys bounds LoginChallenge.Passkeys. A user holds at most
	// trust.MaxKeysPerUser keys in the trust list, so no honest answer names
	// more passkeys than that, and the browser allocates no more either.
	MaxPasskeys = trust.MaxKeysPerUser
	TOTPDigits  = 6
	TOTPPeriod  = 30 // RFC 6238 with HMAC-SHA1, as authenticator apps expect
)

// Limits of one answer of PathTrust.
const (
	MaxChainVersions = 64       // versions in one TrustChain
	MaxChain         = 32 << 20 // bytes of one TrustChain as JSON
)

// maxTrustHint bounds a TrustHint body; {"seq":N} needs at most 24 bytes.
const maxTrustHint = 64

// Budgets of the documents before the WebSocket (enc.Budget). Only a
// TrustChain, a LoginChallenge and the Info of a SetupOffer have an array, so
// any other array is refused.
var (
	requestBudget   = enc.Budget{Size: MaxRequest}    // LoginStart, LoginFinish, Setup
	offerBudget     = enc.Budget{Size: MaxSetupOffer} // SetupOffer
	challengeBudget = enc.Budget{Size: MaxRequest, Items: map[string]int{"passkeys": MaxPasskeys, "rp/origins": trust.MaxOrigins}}
	infoBudget      = enc.Budget{Size: MaxRequest, Items: map[string]int{"nodes": MaxSetupNodes}}
	certBudget      = enc.Budget{Size: MaxCert}      // ClientCert, APICert
	hintBudget      = enc.Budget{Size: maxTrustHint} // TrustHint
	chainBudget     = enc.Budget{Size: MaxChain, Items: map[string]int{"versions": MaxChainVersions}}
)

// LoginStart asks for a login challenge.
type LoginStart struct {
	Name string `json:"name"`
}

// LoginChallenge tells the browser how the user logs in. For unknown names
// the API answers with made-up but stable values.
type LoginChallenge struct {
	Nonce        enc.Bytes    `json:"nonce"`
	Now          int64        `json:"now"`
	MaxLifetime  int64        `json:"maxLifetime"` // ClientCert.Expires <= Now + MaxLifetime
	Installation string       `json:"installation"`
	User         string       `json:"user"`
	KDF          *proof.KDF   `json:"kdf,omitempty"`      // set if the user may log in with a password
	Passkeys     []PasskeyRef `json:"passkeys,omitempty"` // the user's passkeys, empty for a user without one
	TOTP         bool         `json:"totp"`
	PasswordToo  bool         `json:"passwordToo,omitempty"` // a role asks for a passkey and the password: LoginFinish.Proof by a passkey, plus Password
	RP           proof.RP     `json:"rp"`
}

// PasskeyRef names one passkey the user may log in with: the credential ID for
// the browser's allowCredentials list, and the key ID that goes into
// proof.Proof.KeyID. Without the key ID a browser that has never seen the
// trust list cannot build the proof at all.
type PasskeyRef struct {
	CredID enc.Bytes `json:"credId"`
	KeyID  string    `json:"kid"`
}

// Validate checks the structure of r: a credential ID of 1..MaxCredID bytes,
// as proof.UserKey.Validate asks of a passkey, and a key ID by the rule of
// proof.Proof.Validate, so the browser can put it into the proof unchanged.
func (r PasskeyRef) Validate() error {
	if len(r.CredID) == 0 || len(r.CredID) > proof.MaxCredID || !proof.ValidKeyID(r.KeyID) {
		return ErrMessage
	}
	return nil
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
	// it when LoginChallenge.PasswordToo is set; Proof is then by a passkey.
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

// SetupInfo is what the API offers for version 1 of the trust list while no
// admin exists; GET PathSetup carries it in a SetupOffer.
type SetupInfo struct {
	Installation string       `json:"installation"`
	Nodes        []trust.Node `json:"nodes"` // the API servers for version 1 of the trust list
	Now          int64        `json:"now"`
	// ReleaseKey is the NGC release key as the API is configured
	// (config.API.ReleaseKey). The shell names it in version 1's standing
	// approvals for ngc-web, ngc-api and ngc-controller.
	ReleaseKey enc.Bytes `json:"releaseKey"`
	// Store is the global store as the API is configured (config.API.StoreURL,
	// StoreKey), ID trust.StoreGlobal, name "NGC Store". The shell lists it in
	// version 1 unless the admin switches it off.
	Store *trust.Store `json:"store,omitempty"`
}

// SetupOffer answers GET PathSetup while no admin exists: SetupInfo as JSON
// (Info) and OfferMAC over exactly those bytes. The shell checks the MAC with
// the code the admin typed before it reads Info, so no one between browser
// and API can offer other API keys, another store or another release key;
// the code itself never travels.
type SetupOffer struct {
	Info enc.Bytes `json:"info"`
	MAC  enc.Bytes `json:"mac"`
}

// Setup creates the first admin with version 1 of the trust list. MAC is
// SetupMAC over Trust.Payload: the code proves the request without travelling.
type Setup struct {
	MAC   enc.Bytes    `json:"mac"`
	Trust trust.Signed `json:"trust"`
}

var (
	ErrCert    = errors.New("session: malformed certificate")
	ErrUser    = errors.New("session: proof is not by a key of this user")
	ErrExpired = errors.New("session: certificate not valid at this time")
	ErrMessage = errors.New("session: malformed message")
	ErrSetup   = errors.New("session: setup does not match the code or the offer")
	// ErrChallenge reports that Challenge was called with values it cannot
	// build a login answer from. It never depends on the login name, so it
	// tells no one whether a user exists.
	ErrChallenge = errors.New("session: cannot build a login challenge")
)

// The parsers below answer every failure with ErrMessage alone: the decoder's
// own errors quote parts of the input, which may hold a code or a proof.

// ParseLoginStart decodes and checks the body of PathLoginStart.
func ParseLoginStart(b []byte) (LoginStart, error) {
	var s LoginStart
	if requestBudget.Decode(b, &s) != nil || !trust.ValidLoginName(s.Name) {
		return LoginStart{}, ErrMessage
	}
	return s, nil
}

// ParseLoginChallenge decodes and checks an answer of PathLoginStart: strict
// JSON within MaxRequest (at most MaxPasskeys passkeys and trust.MaxOrigins
// origins), a NonceSize nonce, Now above 0, MaxLifetime from 1 to
// MaxLifetime, Installation and User each an ID, a KDF that validates if one
// is there, an RP ID by proof.ValidRPID if one is there — the browser hands it
// to navigator.credentials.get, so it is a DNS name and never free text — and
// origins by trust.ValidOrigin. Passkeys may be empty (a user
// without one), but each entry passes PasskeyRef.Validate and names its
// credential ID and its key ID only once: two entries of one name would leave
// the browser guessing which key made a proof. A challenge that names
// passkeys also names the relying party, as trust.List.Validate asks of a
// list with passkeys, so the browser can ask the authenticator. The made-up
// answer for an unknown name keeps all of this: an answer the browser refuses
// would tell it that the user does not exist.
func ParseLoginChallenge(b []byte) (LoginChallenge, error) {
	var c LoginChallenge
	if challengeBudget.Decode(b, &c) != nil || len(c.Nonce) != NonceSize || c.Now <= 0 ||
		c.MaxLifetime < 1 || c.MaxLifetime > MaxLifetime ||
		!trust.ValidID(c.Installation) || !trust.ValidID(c.User) ||
		(c.KDF != nil && c.KDF.Validate() != nil) ||
		len(c.Passkeys) > MaxPasskeys || len(c.RP.Origins) > trust.MaxOrigins ||
		(c.RP.ID != "" && !proof.ValidRPID(c.RP.ID)) ||
		(len(c.Passkeys) > 0 && (c.RP.ID == "" || len(c.RP.Origins) == 0)) {
		return LoginChallenge{}, ErrMessage
	}
	for _, o := range c.RP.Origins {
		if !trust.ValidOrigin(o) {
			return LoginChallenge{}, ErrMessage
		}
	}
	keys, creds := make(map[string]bool, len(c.Passkeys)), make(map[string]bool, len(c.Passkeys))
	for _, p := range c.Passkeys {
		cred := string(p.CredID)
		if p.Validate() != nil || keys[p.KeyID] || creds[cred] {
			return LoginChallenge{}, ErrMessage
		}
		keys[p.KeyID], creds[cred] = true, true
	}
	return c, nil
}

// ChallengeKeyLabel keeps the made-up answer's HMAC apart from every other
// MAC, so the challenge key means one thing here and nothing elsewhere.
const ChallengeKeyLabel = "ngc/login-unknown/v1"

// ChallengeInput is what Challenge cannot take from the trust list: the values
// of this request and the two things only the API knows about users.
type ChallengeInput struct {
	Name string // the login name as typed (trust.ValidLoginName)
	// TOTP tells whether the user of Name holds a TOTP secret
	// (dbapi.User.TOTP is not empty). It is the one thing about a user the
	// trust list does not hold, so the API looks it up. For a name the list
	// does not know it is ignored, and TOTPShare decides instead.
	TOTP bool
	// TOTPShare is how many of the installation's users hold a TOTP secret,
	// in percent (0..100), which the API counts in the database and may keep.
	// A made-up answer draws its TOTP from it. A coin toss would say no for
	// half the names of an installation where TOTP is compulsory, and every
	// one of those answers would stand out as made up.
	TOTPShare int
	// Key is config.API.ChallengeKey, the same in every API node of the
	// installation, so all of them answer one name alike.
	Key         []byte
	Nonce       []byte // NonceSize random bytes
	Now         int64
	MaxLifetime int64 // longest session this API grants, 1..MaxLifetime
}

// Challenge builds the answer of PathLoginStart. Everything a known user needs
// stands in the trust list, so it reads no database and nothing else: his
// passkeys with credential ID and key ID, the KDF of his password key, and
// PasswordToo from his roles. The list must have passed trust.List.Validate;
// a password key without a KDF gives ErrChallenge.
//
// For a name the list does not know the answer is made up, because an answer
// of another shape would tell whoever asks which users exist. Its values are
// HMAC-SHA256 over ChallengeKeyLabel, a zero byte and the name, keyed with
// in.Key, so the same name gets the same answer as long as the trust list
// stands: the shape is drawn from the list, so a version that changes it may
// change the answer, while nonce and time differ with every call by design.
//
// What is made up is drawn from what the list really holds: how many users
// have passkeys alone, a password alone or both, how many passkeys those with
// passkeys have, the lengths of real credential IDs and the parameters of a
// real KDF. Fixed odds would be wrong wherever an installation is not average
// — where every user holds both kinds, an answer with one kind is made up at
// a glance, and a name with more passkeys than any made-up answer offers is
// one that exists.
//
// Both paths do the same work, and how much depends on the list alone, never
// on the name: the made-up answer is built every time and dropped for a user
// the list knows. Time and memory would otherwise tell the two apart although
// the answers do not, and an unknown name would cost more than a known one on
// a path nobody has logged in to yet.
func Challenge(l *trust.List, in ChallengeInput) (LoginChallenge, error) {
	if l == nil || len(in.Key) == 0 || len(in.Nonce) != NonceSize ||
		in.TOTPShare < 0 || in.TOTPShare > 100 || !trust.ValidLoginName(in.Name) {
		return LoginChallenge{}, ErrChallenge
	}
	c := LoginChallenge{Nonce: bytes.Clone(in.Nonce), Now: in.Now, MaxLifetime: in.MaxLifetime,
		Installation: l.Installation, RP: l.RP}
	// The whole list is walked either way and the made-up answer is built
	// either way, so neither the time nor the allocations follow the name.
	u, known := userByName(l, in.Name)
	madeUp := makeUpChallenge(l, in)
	if !known {
		c.User, c.KDF, c.Passkeys = madeUp.User, madeUp.KDF, madeUp.Passkeys
		c.TOTP, c.PasswordToo = madeUp.TOTP, madeUp.PasswordToo
		return finishChallenge(c)
	}
	c.User, c.TOTP, c.PasswordToo = u.ID, in.TOTP, l.PasswordToo(u.ID)
	for _, k := range u.Keys {
		switch k.Kind {
		case proof.KindPasskey:
			c.Passkeys = append(c.Passkeys, PasskeyRef{CredID: bytes.Clone(k.CredID), KeyID: k.ID})
		case proof.KindPassword:
			// A user may hold more than one password key; the browser
			// derives one key, so the first KDF is the one it uses.
			if c.KDF == nil {
				if k.KDF == nil {
					return LoginChallenge{}, ErrChallenge
				}
				kdf := *k.KDF
				kdf.Salt = bytes.Clone(k.KDF.Salt)
				c.KDF = &kdf
			}
		}
	}
	return finishChallenge(c)
}

// finishChallenge holds an answer against the rules the browser reads it with
// (ParseLoginChallenge). An answer a browser refuses would tell it that the
// name is unknown, so none leaves here.
func finishChallenge(c LoginChallenge) (LoginChallenge, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return LoginChallenge{}, ErrChallenge
	}
	if _, err := ParseLoginChallenge(b); err != nil {
		return LoginChallenge{}, ErrChallenge
	}
	return c, nil
}

// userByName is trust.List.UserByName without the early return: it looks at
// every user whether the first matches, the last or none, so the lookup costs
// the same for every name.
func userByName(l *trust.List, name string) (trust.User, bool) {
	var found trust.User
	known := false
	for _, u := range l.Users {
		if u.Name == name {
			found, known = u, true
		}
	}
	return found, known
}

// The four forms a user may have. formBothToo is both kinds of key with a role
// that asks for the password as well, formBothPlain both kinds without one.
const (
	formPasskey = iota
	formPassword
	formBothToo
	formBothPlain
	formCount
)

// shape is what a trust list says about the answers it really gives: how many
// users hold which form, how many passkeys those with passkeys hold, how many
// passkeys it has altogether and the KDF of a password key. A made-up answer
// draws from it, so it looks like one of this installation's own.
type shape struct {
	form     [formCount]int
	passkeys [trust.MaxKeysPerUser + 1]int // users with n passkeys, n >= 1
	holders  int                           // users the histogram counts
	total    int                           // passkeys in the list
	kdf      *proof.KDF
}

// listShape walks the list once. It fills counters of a fixed size and no
// slice that grows with the list, so it costs the walk and nothing more.
func listShape(l *trust.List) shape {
	var s shape
	for _, u := range l.Users {
		n, password := 0, false
		for _, k := range u.Keys {
			switch k.Kind {
			case proof.KindPasskey:
				n++
			case proof.KindPassword:
				password = true
				if s.kdf == nil && k.KDF != nil {
					s.kdf = k.KDF
				}
			}
		}
		s.total += n
		if n > 0 && n <= trust.MaxKeysPerUser {
			s.passkeys[n]++
			s.holders++
		}
		switch {
		case n > 0 && !password:
			s.form[formPasskey]++
		case n == 0 && password:
			s.form[formPassword]++
		case n > 0 && l.PasswordToo(u.ID):
			s.form[formBothToo]++
		case n > 0:
			s.form[formBothPlain]++
		}
	}
	return s
}

// makeUpChallenge derives an answer for a name the list does not know. Only
// User, KDF, Passkeys, TOTP and PasswordToo of it are used; the rest of a
// challenge is the same for every name.
func makeUpChallenge(l *trust.List, in ChallengeInput) LoginChallenge {
	m := hmac.New(sha256.New, in.Key)
	m.Write([]byte(ChallengeKeyLabel))
	m.Write([]byte{0})
	m.Write([]byte(in.Name))
	seed := m.Sum(nil)
	// draw hands out the bytes of SHA-256 over the seed and a block number,
	// so every choice below has its own and none runs out.
	var stream []byte
	block := 0
	draw := func() byte {
		if len(stream) == 0 {
			h := sha256.New()
			h.Write(seed)
			h.Write([]byte{byte(block), byte(block >> 8)})
			stream, block = h.Sum(nil), block+1
		}
		b := stream[0]
		stream = stream[1:]
		return b
	}
	// below returns a derived number under n, from four bytes so that the
	// remainder stays even enough for counts as large as a list may reach.
	below := func(n int) int {
		v := uint32(draw())<<24 | uint32(draw())<<16 | uint32(draw())<<8 | uint32(draw())
		if n <= 0 {
			return 0
		}
		return int(v % uint32(n))
	}
	drawn := func(n int) enc.Bytes {
		b := make(enc.Bytes, n)
		for i := range b {
			b[i] = draw()
		}
		return b
	}
	// pick returns the index of counts whose lot the draw falls in, so every
	// user of the list is one lot and the made-up forms come in the same
	// proportion as the real ones.
	pick := func(counts []int, total int) int {
		lot := below(total)
		for i, n := range counts {
			if lot -= n; lot < 0 {
				return i
			}
		}
		return -1
	}

	s := listShape(l)
	var c LoginChallenge
	// A key ID of 16 derived bytes hits one of the list about as often as a
	// guessed AES key does, so nothing is compared against the list: the
	// comparison is what cost the time and the memory, not the collision.
	c.User = enc.B64.EncodeToString(drawn(sign.KeyIDSize))
	users := 0
	for _, n := range s.form {
		users += n
	}
	form := pick(s.form[:], users)
	withPasskeys := (form == formPasskey || form == formBothToo || form == formBothPlain) &&
		s.total > 0 && proof.ValidRPID(l.RP.ID) && len(l.RP.Origins) > 0
	withKDF := (form == formPassword || form == formBothToo || form == formBothPlain) && s.kdf != nil
	if withKDF {
		k := *s.kdf
		k.Salt = drawn(proof.SaltSize)
		c.KDF = &k
	}
	if withPasskeys {
		n := pick(s.passkeys[:], s.holders)
		for i := 0; i < n && i < MaxPasskeys; i++ {
			c.Passkeys = append(c.Passkeys, PasskeyRef{
				CredID: drawn(credIDLength(l, below(s.total))),
				KeyID:  enc.B64.EncodeToString(drawn(sign.KeyIDSize)),
			})
		}
		c.Passkeys = withoutDoubles(c.Passkeys)
	}
	// TOTPShare out of 100, drawn from one byte: an installation where every
	// user holds TOTP gets it in every made-up answer, one where none does in
	// none of them, and anything between in that proportion.
	c.TOTP = int(draw())*100 < in.TOTPShare*256
	c.PasswordToo = form == formBothToo && withKDF && withPasskeys
	return c
}

// credIDLength returns the length of the i-th passkey credential ID of the
// list, so a made-up one is as long as a real one. It walks the list, which is
// the same walk for every name.
func credIDLength(l *trust.List, i int) int {
	seen := 0
	for _, u := range l.Users {
		for _, k := range u.Keys {
			if k.Kind != proof.KindPasskey {
				continue
			}
			if seen == i {
				return len(k.CredID)
			}
			seen++
		}
	}
	return proof.SaltSize
}

// withoutDoubles drops an entry whose credential ID an earlier one already
// has. ParseLoginChallenge refuses two entries of one name, and credential
// IDs as short as a list may hold let two derived ones meet; dropping one
// keeps the answer valid without a retry whose count would follow the name.
func withoutDoubles(refs []PasskeyRef) []PasskeyRef {
	out := refs[:0]
	for i, r := range refs {
		double := false
		for _, before := range refs[:i] {
			double = double || bytes.Equal(before.CredID, r.CredID)
		}
		if !double {
			out = append(out, r)
		}
	}
	return out
}

// ParseLoginFinish decodes and checks the body of PathLoginFinish. It checks
// the structure only; VerifyClient and VerifyPassword check the proofs.
func ParseLoginFinish(b []byte) (LoginFinish, error) {
	var f LoginFinish
	if requestBudget.Decode(b, &f) != nil || len(f.Cert) == 0 || len(f.Cert) > MaxCert ||
		f.Proof.Validate() != nil ||
		(f.Password != nil && (f.Password.Kind != proof.KindPassword || f.Password.Validate() != nil)) ||
		(f.TOTP != "" && !ValidTOTP(f.TOTP)) {
		return LoginFinish{}, ErrMessage
	}
	return f, nil
}

// ParseSetup decodes and checks the body of POST PathSetup: a 32-byte MAC, a
// payload and a proof of the right form. VerifySetup checks the MAC and the
// version it carries.
func ParseSetup(b []byte) (Setup, error) {
	var s Setup
	if requestBudget.Decode(b, &s) != nil || len(s.MAC) != sha256.Size || len(s.Trust.Payload) == 0 ||
		s.Trust.Proof.Validate() != nil {
		return Setup{}, ErrMessage
	}
	return s, nil
}

// ParseSetupOffer decodes and checks an answer of GET PathSetup with the
// canonical code (NormalizeSetupCode; a code that is not canonical holds for
// no MAC): strict JSON within MaxSetupOffer (no array); Info 1 to
// MaxRequest bytes and a 32-byte MAC, else ErrMessage; OfferMAC with code
// (hmac.Equal) before Info is decoded, else ErrSetup; Info strictly within
// MaxRequest (nodes at most MaxSetupNodes): a valid installation ID, 1 to
// MaxSetupNodes nodes each trust.ValidAPINode, sorted by ID without two of
// one ID, Now > 0, a 32-byte ReleaseKey, Store absent or with ID
// trust.StoreGlobal and passing trust.ValidStore, else ErrMessage. The
// addresses of the nodes and of the store follow the grammar of package
// trust (trust.ValidTunnelURL and trust.ValidStoreURL, through
// trust.ValidAPINode and trust.ValidStore), so what the shell writes into
// version 1 goes on into configurations unchanged. These are the rules of
// trust.List.Validate for what version 1 takes from the offer, so a version 1
// built from an accepted offer as the package comment says validates.
func ParseSetupOffer(b []byte, code string) (SetupInfo, error) {
	var o SetupOffer
	if offerBudget.Decode(b, &o) != nil || len(o.Info) == 0 || len(o.Info) > MaxRequest || len(o.MAC) != sha256.Size {
		return SetupInfo{}, ErrMessage
	}
	// Whoever does not hold the code cannot make the MAC, so nothing he
	// wrote into Info is decoded. A code that is not canonical is none.
	if !ValidSetupCode(code) || !hmac.Equal(o.MAC, OfferMAC(code, o.Info)) {
		return SetupInfo{}, ErrSetup
	}
	var info SetupInfo
	if infoBudget.Decode(o.Info, &info) != nil || !trust.ValidID(info.Installation) ||
		len(info.Nodes) == 0 || len(info.Nodes) > MaxSetupNodes || info.Now <= 0 ||
		len(info.ReleaseKey) != ed25519.PublicKeySize ||
		(info.Store != nil && (info.Store.ID != trust.StoreGlobal || !trust.ValidStore(*info.Store))) {
		return SetupInfo{}, ErrMessage
	}
	// Version 1 lists the nodes as offered (VerifySetup compares them in
	// order), and a trust list keeps its nodes sorted by ID.
	for i, n := range info.Nodes {
		if !trust.ValidAPINode(n) || (i > 0 && info.Nodes[i-1].ID >= n.ID) {
			return SetupInfo{}, ErrMessage
		}
	}
	return info, nil
}

// ParseTrustChain decodes and checks an answer of PathTrust. trust.Open and
// trust.Advance check the versions themselves.
func ParseTrustChain(b []byte) (TrustChain, error) {
	var c TrustChain
	if chainBudget.Decode(b, &c) != nil || len(c.Versions) == 0 || len(c.Versions) > MaxChainVersions {
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
	if hintBudget.Decode(b, &h) != nil || h.Seq < 1 {
		return TrustHint{}, ErrMessage
	}
	return h, nil
}

// ParseFrom reads the query parameter from of PathTrust: a decimal number
// 1..enc.MaxSafeInteger without sign or leading zero, else ErrMessage.
func ParseFrom(s string) (uint64, error) {
	// enc.MaxSafeInteger has 16 digits, so 16 digits cannot overflow.
	if len(s) == 0 || len(s) > 16 || s[0] == '0' {
		return 0, ErrMessage
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, ErrMessage
		}
		n = n*10 + uint64(s[i]-'0')
	}
	if n > enc.MaxSafeInteger {
		return 0, ErrMessage
	}
	return n, nil
}

// HTTPStatus is the HTTP status of an error answer before the WebSocket; its
// body is the CLOSE payload wire.Error(c): E_PROTOCOL 400, E_LOGIN 401,
// E_FORBIDDEN and E_VERIFY 403, E_NOTFOUND 404, E_TOOLARGE 413, E_LOCKED 429,
// E_INTERNAL 500, E_UNREACHABLE 503; any other code 500.
func HTTPStatus(c wire.Code) int {
	switch c {
	case wire.CodeProtocol:
		return 400
	case wire.CodeLogin:
		return 401
	case wire.CodeForbidden, wire.CodeVerify:
		return 403
	case wire.CodeNotFound:
		return 404
	case wire.CodeTooLarge:
		return 413
	case wire.CodeLocked:
		return 429
	case wire.CodeUnreachable:
		return 503
	}
	return 500
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
// against the trust list. The API checks the nonce in addition. If a role of
// the user asks for the password too (trust.List.PasswordToo), the proof must
// be by a passkey: the password key alone never makes a session, not at the
// API (which checks the password with VerifyPassword besides) and not at a
// controller, which sees only this proof. A password-key proof gives ErrUser.
func VerifyClient(payload []byte, p proof.Proof, l *trust.List, now int64) (Verified, error) {
	c, err := verifyCert(payload, p, l)
	if err != nil {
		return Verified{}, err
	}
	if now < c.Issued-MaxSkew || now >= c.Expires {
		return Verified{}, ErrExpired
	}
	return Verified{cert: c, payload: payload}, nil
}

// VerifyApproval checks a client certificate and its proof as VerifyClient
// does (structure, installation, key of that user, passkey for password-too
// roles, proof), but not the time: for evidence of what a user approved in
// the past, such as the activate task in core.UIProof. It makes no Verified,
// so no channel can open on it.
func VerifyApproval(client []byte, p proof.Proof, l *trust.List) (ClientCert, error) {
	return verifyCert(client, p, l)
}

// verifyCert is what VerifyClient and VerifyApproval share: everything but
// the time.
func verifyCert(payload []byte, p proof.Proof, l *trust.List) (ClientCert, error) {
	var c ClientCert
	if err := certBudget.Decode(payload, &c); err != nil {
		return ClientCert{}, ErrCert
	}
	if err := c.Validate(); err != nil || c.Installation != l.Installation {
		return ClientCert{}, ErrCert
	}
	owner, key, ok := l.KeyOwner(p.KeyID)
	if !ok || owner.ID != c.User || (key.Kind != proof.KindPasskey && l.PasswordToo(owner.ID)) {
		return ClientCert{}, ErrUser
	}
	if err := proof.Verify(p, key, l.RP, sign.PurposeClientCert, payload); err != nil {
		return ClientCert{}, err
	}
	return c, nil
}

// VerifyPassword checks the second proof of a role that asks for the password
// too: by a password key of the user of v, over v.Payload(), purpose
// clientcert/v1. ErrUser if the key is not a password key of that user. v's
// own proof was then by a passkey (VerifyClient), so the two proofs come from
// two different keys of the same user.
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
	if err := certBudget.Decode(client, &cc); err != nil {
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
	if err := certBudget.Decode(env.Payload, &c); err != nil {
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

// VerifySetup checks the version 1 the new admin sent, with the canonical
// one-time code and against what the API offered in info. First the code
// (ValidSetupCode) and the MAC (SetupMAC over the exact payload, hmac.Equal),
// before anything is decoded; then trust.Open with the anchor {1,
// trust.Hash(s.Trust)} (so an admin in the list signed it), the same
// Installation, Nodes equal to info.Nodes (compared as encoding/json
// output), Stores either empty or exactly the global store info.Store; and
// standing approvals only for system (without a key) and for ngc-web,
// ngc-api and ngc-controller with info.ReleaseKey. Returns trust errors or
// ErrSetup.
func VerifySetup(s Setup, info SetupInfo, code string) (trust.Version, error) {
	if !ValidSetupCode(code) || !hmac.Equal(s.MAC, SetupMAC(code, s.Trust.Payload)) {
		return trust.Version{}, ErrSetup
	}
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
	// A standing approval lets updates signed with its key install without
	// anyone confirming them, so version 1 names no key but the release key
	// the API offered under the code; approvals for apps come later, with the
	// key of their store.
	for _, a := range l.Auto {
		switch a.Target {
		case trust.AutoSystem:
			if len(a.Key) != 0 {
				return trust.Version{}, ErrSetup
			}
		case trust.AutoWeb, trust.AutoAPI, trust.AutoController:
			if len(info.ReleaseKey) != ed25519.PublicKeySize || !bytes.Equal(a.Key, info.ReleaseKey) {
				return trust.Version{}, ErrSetup
			}
		default:
			return trust.Version{}, ErrSetup
		}
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

// ValidSetupCode reports whether s is a one-time setup code in its canonical
// form: 20 characters of A-H, J-N, P-Z and 2-9 in groups of five joined by
// '-'.
func ValidSetupCode(s string) bool { return setupCode.MatchString(s) }

// setupChar reports whether c belongs to the alphabet of setup codes: the
// capital letters and digits without I, O, 0 and 1, which people confuse.
func setupChar(c byte) bool {
	return ('A' <= c && c <= 'Z' && c != 'I' && c != 'O') || ('2' <= c && c <= '9')
}

// NormalizeSetupCode turns what someone typed into the canonical code: spaces,
// tabs and '-' removed, a-z made upper case, then exactly 20 characters of
// the code alphabet (A-H, J-N, P-Z, 2-9) in groups of five joined by '-'
// (ValidSetupCode). false if that is not possible.
func NormalizeSetupCode(s string) (string, bool) {
	var c [20]byte
	n := 0
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b == ' ' || b == '\t' || b == '-':
			continue
		case 'a' <= b && b <= 'z':
			b -= 'a' - 'A'
		}
		if n == len(c) || !setupChar(b) {
			return "", false
		}
		c[n] = b
		n++
	}
	if n != len(c) {
		return "", false
	}
	return string(c[0:5]) + "-" + string(c[5:10]) + "-" + string(c[10:15]) + "-" + string(c[15:20]), true
}

// OfferMAC is HMAC-SHA256 keyed with the canonical setup code over
// "ngc/setup-offer/v1", a zero byte and info.
func OfferMAC(code string, info []byte) []byte { return setupHMAC(code, "ngc/setup-offer/v1", info) }

// SetupMAC is HMAC-SHA256 keyed with the canonical setup code over
// "ngc/setup/v1", a zero byte and payload.
func SetupMAC(code string, payload []byte) []byte { return setupHMAC(code, "ngc/setup/v1", payload) }

// setupHMAC keeps the two MACs apart by their labels, so a MAC made for one
// direction never passes for the other, whatever bytes it covers.
func setupHMAC(code, label string, b []byte) []byte {
	m := hmac.New(sha256.New, []byte(code))
	m.Write([]byte(label))
	m.Write([]byte{0})
	m.Write(b)
	return m.Sum(nil)
}

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
