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
	"unicode"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
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
	PasswordToo bool     `json:"passwordToo,omitempty"` // the API asks for the password in addition to a passkey
}

// Node is an API server or a controller.
type Node struct {
	ID        string    `json:"id"`
	Kind      NodeKind  `json:"kind"`
	Name      string    `json:"name"`
	Noise     enc.Bytes `json:"noise"`               // X25519 static key: tunnel, and for controllers end to end
	Sign      enc.Bytes `json:"sign,omitempty"`      // API only: Ed25519 key that signs API certificates
	URL       string    `json:"url,omitempty"`       // API only: tunnel address, wss://host:port/tunnel
	RootUsers []string  `json:"rootUsers,omitempty"` // controller only
	RootRoles []string  `json:"rootRoles,omitempty"` // controller only
}

// Store is an app source whose catalog key an admin confirmed.
type Store struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	URL  string    `json:"url"`
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

// Hash returns the SHA-256 of the payload of s.
func Hash(s Signed) []byte {
	h := sha256.Sum256(s.Payload)
	return h[:]
}

// Parse decodes and validates a payload. It does not check signatures.
func Parse(payload []byte) (*List, error) {
	var l List
	if err := enc.DecodeStrict(payload, sign.MaxPayload, &l); err != nil {
		return nil, ErrInvalid
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// Open verifies chain from a pinned anchor and returns the newest version.
// chain[0] must be the anchored version.
func Open(a Anchor, chain []Signed) (Version, error) {
	if len(chain) == 0 {
		return Version{}, ErrAnchor
	}
	l, err := Parse(chain[0].Payload)
	if err != nil {
		return Version{}, err
	}
	h := Hash(chain[0])
	if l.Seq != a.Seq || !bytes.Equal(h, a.Hash) {
		return Version{}, ErrAnchor
	}
	if l.Seq == 1 {
		if err := checkSigner(l, l, chain[0]); err != nil {
			return Version{}, err
		}
	}
	return Advance(Version{List: l, Hash: h}, chain[1:])
}

// Advance verifies the versions following cur, in order, and returns the newest.
func Advance(cur Version, chain []Signed) (Version, error) {
	for _, s := range chain {
		next, err := Parse(s.Payload)
		if err != nil {
			return Version{}, err
		}
		if next.Seq != cur.List.Seq+1 || !bytes.Equal(next.Prev, cur.Hash) || next.Installation != cur.List.Installation {
			return Version{}, ErrChain
		}
		if err := checkSigner(cur.List, next, s); err != nil {
			return Version{}, err
		}
		cur = Version{List: next, Hash: Hash(s)}
	}
	return cur, nil
}

// checkSigner verifies the proof of s with a key from prev and checks that
// its owner may make the change from prev to next.
func checkSigner(prev, next *List, s Signed) error {
	u, key, ok := prev.KeyOwner(s.Proof.KeyID)
	if !ok {
		return ErrSigner
	}
	if err := proof.Verify(s.Proof, key, prev.RP, sign.PurposeTrustList, s.Payload); err != nil {
		return err
	}
	if slices.Contains(u.Roles, RoleAdmin) {
		return nil
	}
	if prev == next || !onlyOwnKeys(prev, next, u.ID) {
		return ErrSigner
	}
	return nil
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
		len(l.Nodes) > MaxNodes || len(l.Stores) > MaxStores {
		return ErrInvalid
	}
	for i, r := range l.Roles {
		if !wire.ValidName(r.Name) || (i > 0 && l.Roles[i-1].Name >= r.Name) || !validLevel(r.Level) ||
			!validEntries(r.Black) || !validEntries(r.White) {
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
		if !wire.ValidName(n.ID) || (i > 0 && l.Nodes[i-1].ID >= n.ID) || !validLabel(n.Name) || len(n.Noise) != 32 {
			return ErrInvalid
		}
		switch n.Kind {
		case NodeAPI:
			if len(n.Sign) != ed25519.PublicKeySize || !strings.HasPrefix(n.URL, "wss://") || len(n.URL) > 256 ||
				len(n.RootUsers)+len(n.RootRoles) > 0 {
				return ErrInvalid
			}
		case NodeController:
			if len(n.Sign) != 0 || n.URL != "" {
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
		if !wire.ValidName(s.ID) || (i > 0 && l.Stores[i-1].ID >= s.ID) || !validLabel(s.Name) ||
			!strings.HasPrefix(s.URL, "https://") || len(s.URL) > 256 || len(s.Key) != ed25519.PublicKeySize {
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
		case a.Target == AutoWeb || a.Target == AutoAPI || a.Target == AutoController || wire.ValidAppID(a.Target):
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
		if !ValidID(u.ID) || (i > 0 && l.Users[i-1].ID >= u.ID) || !validLoginName(u.Name) || names[u.Name] ||
			len(u.Roles) == 0 || len(u.Keys) == 0 || len(u.Keys) > MaxKeysPerUser {
			return ErrInvalid
		}
		names[u.Name] = true
		passkeyOnly := false
		for j, name := range u.Roles {
			r, ok := l.Role(name)
			if !ok || (j > 0 && u.Roles[j-1] >= name) {
				return ErrInvalid
			}
			passkeyOnly = passkeyOnly || r.PasskeyOnly
		}
		for _, k := range u.Keys {
			if k.Validate() != nil || keys[k.ID] || (passkeyOnly && k.Kind == proof.KindPassword) {
				return ErrInvalid
			}
			keys[k.ID] = true
			passkeys = passkeys || k.Kind == proof.KindPasskey
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

// validLoginName allows lower-case letters, digits and . _ - (1 to 64 characters).
func validLoginName(s string) bool {
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

// validLabel allows any printable UTF-8 text of 1 to 64 bytes.
func validLabel(s string) bool {
	if len(s) == 0 || len(s) > 64 || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if !unicode.IsPrint(c) {
			return false
		}
	}
	return true
}
