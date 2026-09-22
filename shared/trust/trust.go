// Package trust defines the trust list: users and their keys, API servers and
// controllers, root permissions, stores, re-confirmation policy and standing
// approvals for updates. An admin signs every version with a passkey or
// password key. Browsers and controllers verify the chain of versions from a
// pinned anchor themselves, so the API can distribute the list but not forge it.
package trust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Limits and the admin role.
const (
	RoleAdmin      = "admin"
	MaxUsers       = 10000
	MaxNodes       = 256
	MaxRoles       = 256
	MaxStores      = 64
	MaxKeysPerUser = 16
	MaxEntries     = 256 // per black or white list
	MaxAuto        = 64
	MaxOrigins     = 8
	MaxRoot        = 256 // per RootUsers or RootRoles list on one controller
)

// Level of re-confirmation for a role.
type Level string

const (
	LevelOff      Level = "off"
	LevelCritical Level = "critical"
	LevelRoot     Level = "root"
	LevelHardcore Level = "hardcore"
)

// NodeKind tells API servers and controllers apart.
type NodeKind string

const (
	NodeAPI        NodeKind = "api"
	NodeController NodeKind = "controller"
)

// Store IDs with a fixed meaning.
const (
	StoreGlobal = "global" // the global store; its key comes from the API configuration (config.API.StoreKey)
	StoreLocal  = "local"  // apps an admin uploaded; never a store in the list
)

// Targets of standing approvals besides app IDs.
const (
	AutoWeb        = "ngc-web"
	AutoAPI        = "ngc-api" // includes the database migrations of that API version
	AutoController = "ngc-controller"
	AutoSystem     = "system" // Debian and Ubuntu packages via apt
)

// User is a person with login keys.
type User struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"` // login name
	Roles []string        `json:"roles"`
	Keys  []proof.UserKey `json:"keys"`
}

// Role carries the re-confirmation policy. Rights on apps are kept by the API.
type Role struct {
	Name        string   `json:"name"`
	Level       Level    `json:"level"`
	Black       []string `json:"black,omitempty"`
	White       []string `json:"white,omitempty"`
	PasskeyOnly bool     `json:"passkeyOnly,omitempty"` // members have no password keys
	PasswordToo bool     `json:"passwordToo,omitempty"` // members log in with a passkey and their password in addition, so each holds both kinds of keys
}

// Node is an API server or a controller.
type Node struct {
	ID        string    `json:"id"`
	Kind      NodeKind  `json:"kind"`
	Name      string    `json:"name"`
	Noise     enc.Bytes `json:"noise"`               // X25519 static key: tunnel, and for controllers end to end
	Sign      enc.Bytes `json:"sign,omitempty"`      // API only: Ed25519 key that signs API certificates
	URL       string    `json:"url,omitempty"`       // API only: tunnel address (ValidTunnelURL)
	TLS       enc.Bytes `json:"tls,omitempty"`       // API only: TLSPin of its TLS key; controllers and Apache accept only a certificate with this key
	RootUsers []string  `json:"rootUsers,omitempty"` // controller only
	RootRoles []string  `json:"rootRoles,omitempty"` // controller only
}

// Store is an app source whose catalog key an admin confirmed.
type Store struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	URL  string    `json:"url"` // ValidStoreURL
	Key  enc.Bytes `json:"key"` // Ed25519 key that signs the catalog
}

// Auto is a standing approval: updates for Target signed with Key may be
// installed without a fresh signature. Key is empty for AutoSystem.
type Auto struct {
	Target string    `json:"target"`
	Key    enc.Bytes `json:"key,omitempty"`
}

// List is one version of the trust list. Users, roles, nodes, stores and
// standing approvals are sorted by ID, name or target, without duplicates.
type List struct {
	Seq          uint64    `json:"seq"`
	Prev         enc.Bytes `json:"prev,omitempty"` // SHA-256 of the previous payload; empty in version 1
	Issued       int64     `json:"issued"`
	Installation string    `json:"installation"`
	RP           proof.RP  `json:"rp"`
	Users        []User    `json:"users"`
	Roles        []Role    `json:"roles"`
	Nodes        []Node    `json:"nodes,omitempty"`
	Stores       []Store   `json:"stores,omitempty"`
	Auto         []Auto    `json:"auto,omitempty"`
}

// Signed is one version as distributed: the exact payload and the proof over it.
type Signed struct {
	Payload enc.Bytes   `json:"payload"`
	Proof   proof.Proof `json:"proof"`
}

// Anchor pins one version.
type Anchor struct {
	Seq  uint64    `json:"seq"`
	Hash enc.Bytes `json:"hash"`
}

// Version is a verified version.
type Version struct {
	List *List
	Hash []byte
}

var (
	ErrInvalid = errors.New("trust: invalid list")
	ErrAnchor  = errors.New("trust: list does not match the anchor")
	ErrChain   = errors.New("trust: broken chain of versions")
	ErrSigner  = errors.New("trust: signer may not make this change")
)

// ValidID reports whether s is an ID: 16 bytes, base64url, 22 characters.
func ValidID(s string) bool {
	b, err := enc.B64.DecodeString(s)
	return err == nil && len(b) == 16
}

// TLSPin returns the pin of a TLS key: SHA-256 of its SubjectPublicKeyInfo
// in DER (x509.Certificate.RawSubjectPublicKeyInfo), 32 bytes. API nodes list
// it (Node.TLS); shared/testdata/tls/pin.sh computes the same with openssl.
func TLSPin(spki []byte) []byte {
	h := sha256.Sum256(spki)
	return h[:]
}

// ValidAPINode reports whether n is a well-formed API node: of kind api, an
// ID (wire.ValidName), a name (wire.ValidText, 64), a 32-byte Noise key, a
// 32-byte signing key, a tunnel address (ValidTunnelURL), a 32-byte TLS pin
// and no root lists. Validate uses it; session.ParseSetupOffer too.
func ValidAPINode(n Node) bool {
	return n.Kind == NodeAPI && validNode(n) && len(n.Sign) == ed25519.PublicKeySize &&
		ValidTunnelURL(n.URL) && len(n.TLS) == sha256.Size &&
		len(n.RootUsers)+len(n.RootRoles) == 0
}

// ValidStore reports whether s is a well-formed store: an ID
// (wire.ValidName) other than StoreLocal, a name (wire.ValidText, 64), an
// address (ValidStoreURL) and a 32-byte key. Validate uses it;
// session.ParseSetupOffer too.
func ValidStore(s Store) bool {
	// StoreLocal marks uploaded apps, so no catalog key may claim it.
	return wire.ValidName(s.ID) && s.ID != StoreLocal && wire.ValidText(s.Name, 64) &&
		ValidStoreURL(s.URL) && len(s.Key) == ed25519.PublicKeySize
}

// validNode checks what every node has, whatever its kind: an ID, a name and
// a Noise key.
func validNode(n Node) bool {
	return wire.ValidName(n.ID) && wire.ValidText(n.Name, 64) && len(n.Noise) == 32
}

// Hash returns the SHA-256 of the payload of s.
func Hash(s Signed) []byte {
	h := sha256.Sum256(s.Payload)
	return h[:]
}

// listBudget bounds a trust list while it is decoded: every array holds at
// most what Validate allows. A user's roles are distinct roles of the list,
// so MaxRoles bounds them too.
var listBudget = enc.Budget{Size: sign.MaxPayload, Items: map[string]int{
	"rp/origins":        MaxOrigins,
	"users":             MaxUsers,
	"users/*/roles":     MaxRoles,
	"users/*/keys":      MaxKeysPerUser,
	"roles":             MaxRoles,
	"roles/*/black":     MaxEntries,
	"roles/*/white":     MaxEntries,
	"nodes":             MaxNodes,
	"nodes/*/rootUsers": MaxRoot,
	"nodes/*/rootRoles": MaxRoot,
	"stores":            MaxStores,
	"auto":              MaxAuto,
}}

// signedBudget bounds one version as distributed while it is decoded: the
// base64url of a payload of sign.MaxPayload bytes, plus 64 KiB for the proof
// (at most about 12 KiB), member names and white space. It has no array.
var signedBudget = enc.Budget{Size: enc.B64.EncodedLen(sign.MaxPayload) + 64<<10}

// ParseSigned decodes one version from outside, such as the body of
// ngc.users/api/trust: strict JSON within a budget (enc.Budget) of the
// base64url of sign.MaxPayload bytes plus 64 KiB, a payload of 1 to
// sign.MaxPayload bytes and a proof that passes proof.Proof.Validate. It
// checks no signature: Open and Advance do, and expect versions parsed here
// or by session.ParseTrustChain. Every failure is ErrInvalid alone, since the
// decoder's errors quote parts of the input.
func ParseSigned(b []byte) (Signed, error) {
	var s Signed
	if signedBudget.Decode(b, &s) != nil || len(s.Payload) == 0 || len(s.Payload) > sign.MaxPayload ||
		s.Proof.Validate() != nil {
		return Signed{}, ErrInvalid
	}
	return s, nil
}

// Parse decodes and validates a payload. It does not check signatures.
func Parse(payload []byte) (*List, error) {
	var l List
	if err := listBudget.Decode(payload, &l); err != nil {
		return nil, ErrInvalid
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// Open verifies chain from a pinned anchor and returns the newest version.
// chain[0] must be the anchored version. Its hash is compared before its
// payload is decoded, and Advance checks each later proof before decoding
// that payload, so the only payload decoded unsigned is the pinned one.
// Versions from outside come through ParseSigned or session.ParseTrustChain.
func Open(a Anchor, chain []Signed) (Version, error) {
	if len(chain) == 0 {
		return Version{}, ErrAnchor
	}
	h := Hash(chain[0])
	if !bytes.Equal(h, a.Hash) {
		return Version{}, ErrAnchor
	}
	l, err := Parse(chain[0].Payload)
	if err != nil {
		return Version{}, err
	}
	if l.Seq != a.Seq {
		return Version{}, ErrAnchor
	}
	if l.Seq == 1 {
		// Version 1 signs itself, so only an admin of it may have signed.
		u, err := verifySigner(l, chain[0])
		if err != nil {
			return Version{}, err
		}
		if !slices.Contains(u.Roles, RoleAdmin) {
			return Version{}, ErrSigner
		}
	}
	return Advance(Version{List: l, Hash: h}, chain[1:])
}

// Advance verifies the versions following cur, in order, and returns the
// newest. Each proof is checked with a key of the version before it first,
// so a payload that no listed key signed is refused before it is decoded.
func Advance(cur Version, chain []Signed) (Version, error) {
	if cur.List == nil {
		return Version{}, ErrChain
	}
	for _, s := range chain {
		u, err := verifySigner(cur.List, s)
		if err != nil {
			return Version{}, err
		}
		next, err := Parse(s.Payload)
		if err != nil {
			return Version{}, err
		}
		if next.Seq != cur.List.Seq+1 || !bytes.Equal(next.Prev, cur.Hash) || next.Installation != cur.List.Installation {
			return Version{}, ErrChain
		}
		if !slices.Contains(u.Roles, RoleAdmin) && !onlyOwnKeys(cur.List, next, u.ID) {
			return Version{}, ErrSigner
		}
		cur = Version{List: next, Hash: Hash(s)}
	}
	return cur, nil
}

// verifySigner checks the proof of s with a key of prev and returns the key's
// owner, who still has to be allowed the change.
func verifySigner(prev *List, s Signed) (User, error) {
	u, key, ok := prev.KeyOwner(s.Proof.KeyID)
	if !ok {
		return User{}, ErrSigner
	}
	if err := proof.Verify(s.Proof, key, prev.RP, sign.PurposeTrustList, s.Payload); err != nil {
		return User{}, err
	}
	return u, nil
}

// onlyOwnKeys reports whether next differs from prev in nothing but the keys
// of user id (and the version fields).
func onlyOwnKeys(prev, next *List, id string) bool {
	a, b := *prev, *next
	i, j := a.userIndex(id), b.userIndex(id)
	if i < 0 || j < 0 {
		return false
	}
	a.Users, b.Users = slices.Clone(a.Users), slices.Clone(b.Users)
	a.Users[i].Keys, b.Users[j].Keys = nil, nil
	a.Seq, a.Prev, a.Issued = 0, nil, 0
	b.Seq, b.Prev, b.Issued = 0, nil, 0
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

func (l *List) userIndex(id string) int {
	i, ok := slices.BinarySearchFunc(l.Users, id, func(u User, id string) int { return strings.Compare(u.ID, id) })
	if !ok {
		return -1
	}
	return i
}

// User returns the user with id.
func (l *List) User(id string) (User, bool) {
	if i := l.userIndex(id); i >= 0 {
		return l.Users[i], true
	}
	return User{}, false
}

// Role returns the role with name.
func (l *List) Role(name string) (Role, bool) {
	i, ok := slices.BinarySearchFunc(l.Roles, name, func(r Role, n string) int { return strings.Compare(r.Name, n) })
	if !ok {
		return Role{}, false
	}
	return l.Roles[i], true
}

// Node returns the node with id.
func (l *List) Node(id string) (Node, bool) {
	i, ok := slices.BinarySearchFunc(l.Nodes, id, func(n Node, id string) int { return strings.Compare(n.ID, id) })
	if !ok {
		return Node{}, false
	}
	return l.Nodes[i], true
}

// KeyOwner finds the user a key belongs to.
func (l *List) KeyOwner(keyID string) (User, proof.UserKey, bool) {
	for _, u := range l.Users {
		for _, k := range u.Keys {
			if k.ID == keyID {
				return u, k, true
			}
		}
	}
	return User{}, proof.UserKey{}, false
}

// PasswordToo reports whether a role of the user asks for the password in
// addition to a passkey (session.LoginChallenge.PasswordToo). The user's
// client certificate then needs a passkey proof: session.VerifyClient refuses
// a password key alone, and VerifyPassword checks the password as well.
func (l *List) PasswordToo(userID string) bool {
	u, ok := l.User(userID)
	if !ok {
		return false
	}
	for _, name := range u.Roles {
		if r, ok := l.Role(name); ok && r.PasswordToo {
			return true
		}
	}
	return false
}

// MayRoot reports whether a user may run root tasks on a controller.
func (l *List) MayRoot(userID, nodeID string) bool {
	n, ok := l.Node(nodeID)
	u, ok2 := l.User(userID)
	if !ok || !ok2 || n.Kind != NodeController {
		return false
	}
	if slices.Contains(n.RootUsers, userID) {
		return true
	}
	for _, r := range u.Roles {
		if slices.Contains(n.RootRoles, r) {
			return true
		}
	}
	return false
}

// Op describes an operation for the re-confirmation check.
type Op struct {
	App, Action, Node string
	Root              bool // runs as root on a controller
	Critical          bool // a critical operation (package task, or marked in the app manifest)
	Changes           bool // changes something; every channel to a service counts as a change
}

// NeedsConfirm reports whether user must confirm op again. Unknown users always must.
func (l *List) NeedsConfirm(userID string, op Op) bool {
	u, ok := l.User(userID)
	if !ok {
		return true
	}
	for _, name := range u.Roles {
		if r, ok := l.Role(name); !ok || r.needs(op) {
			return true
		}
	}
	return false
}

func (r Role) needs(op Op) bool {
	switch {
	case matches(r.Black, op):
		return true
	case matches(r.White, op):
		return false
	case r.Level == LevelCritical:
		return op.Critical
	case r.Level == LevelRoot:
		return op.Critical || op.Root
	case r.Level == LevelHardcore:
		return op.Critical || op.Root || op.Changes
	}
	return false
}

func matches(entries []string, op Op) bool {
	for _, e := range entries {
		if e == "app/"+op.App || e == "app/"+op.App+"/"+op.Action || (op.Node != "" && e == "node/"+op.Node) {
			return true
		}
	}
	return false
}

// Validate checks structure, order and references of l.
func (l *List) Validate() error {
	if l.Seq == 0 || (l.Seq == 1) != (len(l.Prev) == 0) || (l.Seq > 1 && len(l.Prev) != sha256.Size) ||
		l.Issued <= 0 || !ValidID(l.Installation) {
		return ErrInvalid
	}
	if len(l.Users) == 0 || len(l.Users) > MaxUsers || len(l.Roles) > MaxRoles ||
		len(l.Nodes) > MaxNodes || len(l.Stores) > MaxStores || len(l.Auto) > MaxAuto ||
		len(l.RP.Origins) > MaxOrigins {
		return ErrInvalid
	}
	for i, r := range l.Roles {
		// A role cannot both forbid password keys and ask for the password.
		if !wire.ValidName(r.Name) || (i > 0 && l.Roles[i-1].Name >= r.Name) || !validLevel(r.Level) ||
			!validEntries(r.Black) || !validEntries(r.White) || (r.PasskeyOnly && r.PasswordToo) {
			return ErrInvalid
		}
	}
	if _, ok := l.Role(RoleAdmin); !ok {
		return ErrInvalid
	}
	if err := l.validateUsers(); err != nil {
		return err
	}
	for i, n := range l.Nodes {
		if i > 0 && l.Nodes[i-1].ID >= n.ID {
			return ErrInvalid
		}
		switch n.Kind {
		case NodeAPI:
			if !ValidAPINode(n) {
				return ErrInvalid
			}
		case NodeController:
			// Signing keys, addresses and TLS pins belong to API nodes: nobody
			// dials a controller.
			if !validNode(n) || len(n.Sign) != 0 || n.URL != "" || len(n.TLS) != 0 ||
				len(n.RootUsers) > MaxRoot || len(n.RootRoles) > MaxRoot {
				return ErrInvalid
			}
			for _, id := range n.RootUsers {
				if _, ok := l.User(id); !ok {
					return ErrInvalid
				}
			}
			for _, r := range n.RootRoles {
				if _, ok := l.Role(r); !ok {
					return ErrInvalid
				}
			}
		default:
			return ErrInvalid
		}
	}
	for i, s := range l.Stores {
		if !ValidStore(s) || (i > 0 && l.Stores[i-1].ID >= s.ID) {
			return ErrInvalid
		}
	}
	for i, a := range l.Auto {
		if i > 0 && l.Auto[i-1].Target >= a.Target {
			return ErrInvalid
		}
		switch {
		case a.Target == AutoSystem:
			if len(a.Key) != 0 {
				return ErrInvalid
			}
		// Core apps ship inside the NGC parts, not from a store, so they are
		// no update target: the same set as store.ValidTarget, which trust
		// cannot import.
		case a.Target == AutoWeb || a.Target == AutoAPI || a.Target == AutoController ||
			(wire.ValidAppID(a.Target) && !wire.CoreApp(a.Target)):
			if len(a.Key) != ed25519.PublicKeySize {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

func (l *List) validateUsers() error {
	keys := map[string]bool{}
	names := map[string]bool{}
	admins, passkeys := 0, false
	for i, u := range l.Users {
		if !ValidID(u.ID) || (i > 0 && l.Users[i-1].ID >= u.ID) || !ValidLoginName(u.Name) || names[u.Name] ||
			len(u.Roles) == 0 || len(u.Keys) == 0 || len(u.Keys) > MaxKeysPerUser {
			return ErrInvalid
		}
		names[u.Name] = true
		passkeyOnly, passwordToo := false, false
		for j, name := range u.Roles {
			r, ok := l.Role(name)
			if !ok || (j > 0 && u.Roles[j-1] >= name) {
				return ErrInvalid
			}
			passkeyOnly = passkeyOnly || r.PasskeyOnly
			passwordToo = passwordToo || r.PasswordToo
		}
		// Such a user could never log in: one role forbids the password key
		// the other one asks for.
		if passkeyOnly && passwordToo {
			return ErrInvalid
		}
		hasPasskey, hasPassword := false, false
		for _, k := range u.Keys {
			if k.Validate() != nil || keys[k.ID] || (passkeyOnly && k.Kind == proof.KindPassword) {
				return ErrInvalid
			}
			keys[k.ID] = true
			hasPasskey = hasPasskey || k.Kind == proof.KindPasskey
			hasPassword = hasPassword || k.Kind == proof.KindPassword
		}
		passkeys = passkeys || hasPasskey
		// Such a user could never log in either: a role that asks for the
		// password too needs a passkey and a password key (session.VerifyClient,
		// VerifyPassword). Setting it on the admin role would lock every admin
		// out, and the setup cannot run again once an admin exists.
		if passwordToo && !(hasPasskey && hasPassword) {
			return ErrInvalid
		}
		if slices.Contains(u.Roles, RoleAdmin) {
			admins++
		}
	}
	if admins == 0 || (passkeys && (l.RP.ID == "" || len(l.RP.Origins) == 0)) {
		return ErrInvalid
	}
	for _, o := range l.RP.Origins {
		if !strings.HasPrefix(o, "https://") || len(o) > 256 {
			return ErrInvalid
		}
	}
	return nil
}

func validLevel(l Level) bool {
	return l == LevelOff || l == LevelCritical || l == LevelRoot || l == LevelHardcore
}

func validEntries(entries []string) bool {
	if len(entries) > MaxEntries {
		return false
	}
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e, "node/"):
			if !wire.ValidName(e[len("node/"):]) {
				return false
			}
		case strings.HasPrefix(e, "app/"):
			app, action, found := strings.Cut(e[len("app/"):], "/")
			if !wire.ValidAppID(app) || (found && !wire.ValidName(action)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ValidLoginName allows lower-case letters, digits and . _ - (1 to 64
// characters, the first a letter or digit). The API checks a typed name with
// it before looking it up (session.ParseLoginStart).
func ValidLoginName(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i, c := range s {
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '.' || c == '_' || c == '-'))
		if !ok {
			return false
		}
	}
	return true
}
