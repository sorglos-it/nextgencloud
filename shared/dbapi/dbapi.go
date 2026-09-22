// Package dbapi is the contract between API, controller and database (V4).
// The part apps/server-db/ implements it for MariaDB and SQLite; memstore
// implements it in memory and dbtest checks every implementation.
package dbapi

import (
	"context"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/core"
)

// Kinds of documents.
const (
	BlobSetting = "setting" // ID: setting key
	BlobTrust   = "trust"   // ID: version number as text, Seq: version number
	BlobCatalog = "catalog" // ID: store, Seq: catalog number
	BlobLicense = "license" // ID: app
	BlobRights  = "rights"  // ID: role, Data: core.RoleRights as JSON
	BlobUI      = "ui"      // ID: "<app>@<version>", Data: tar of the ui/ files
)

// Kinds of one-time values.
const (
	OnceNonce = "nonce" // login challenge; Data: user ID
	OncePair  = "pair"  // SHA-256 of a pairing secret
	OnceTask  = "task"  // ID of a signed task, against replay
)

// Setting keys and lock names.
const (
	SettingInstallation = "installation" // installation ID
	SettingInstallKey   = "installKey"   // X25519 private key of the installation, encrypted by the API
	SettingUpdate       = "update."      // prefix, e.g. "update.ngc-api": core.UpdateInfo as JSON
	LockUpdate          = "update"       // only one API drives a rollout
)

// Blob is a document.
type Blob struct {
	Kind    string
	ID      string
	Seq     uint64 // 0 where no order is needed
	Data    []byte
	Updated int64
}

// User is what the API keeps about a user besides the trust list.
type User struct {
	ID        string
	Name      string   // login name, unique
	Roles     []string // copy from the trust list, for queries
	TOTP      []byte   // secret, encrypted by the API; empty: no TOTP
	Locked    int64    // locked until this time
	Failures  int      // failed logins in a row
	LastLogin int64
}

// Session is a login. Token is session.TokenHash of the cookie value.
type Session struct {
	Token   []byte
	User    string
	Cert    []byte // client certificate payload
	Proof   []byte // the user's proof over it, as JSON
	APIKey  []byte // the API's private session key, encrypted by the API
	APICert []byte // the API certificate, as JSON
	Created int64
	Expires int64
	Seen    int64
}

// App is an app of this installation.
type App struct {
	ID       string
	Version  string // empty if only kept data remains
	Store    string
	Node     string // controller it runs on
	Active   bool
	Manifest []byte // manifest.json as delivered
	SHA256   []byte // checksum of the package
	Grants   []string
	KeptData int64
	Updated  int64
}

// Pending is a controller that paired but is not in the trust list yet.
type Pending struct {
	Name  string
	Key   []byte // its Noise key
	Since int64
}

var (
	ErrNotFound = errors.New("dbapi: not found")
	ErrUsed     = errors.New("dbapi: value already used")
	ErrInvalid  = errors.New("dbapi: invalid value")
)

// Local is what a controller needs; it works without an API.
type Local interface {
	// BlobPut inserts or replaces the document with this kind and ID.
	BlobPut(ctx context.Context, b Blob) error
	// BlobGet returns a document or ErrNotFound.
	BlobGet(ctx context.Context, kind, id string) (Blob, error)
	// BlobFrom returns the documents of a kind with Seq >= seq, ordered by
	// Seq then ID.
	BlobFrom(ctx context.Context, kind string, seq uint64) ([]Blob, error)
	// BlobDelete removes a document; removing what is not there is no error.
	BlobDelete(ctx context.Context, kind, id string) error

	AppPut(ctx context.Context, a App) error
	AppGet(ctx context.Context, id string) (App, error)
	// AppList returns every app, ordered by ID.
	AppList(ctx context.Context) ([]App, error)
	AppDelete(ctx context.Context, id string) error

	// OnceCreate remembers a one-time value until expires. A value that is
	// already there gives ErrUsed, even after it was used; OncePurge frees
	// expired values again. It is atomic, also across several APIs on one
	// database: of concurrent calls for the same kind and value, exactly one
	// succeeds.
	OnceCreate(ctx context.Context, kind string, value []byte, data string, expires int64) error
	// OnceUse marks a value as used and returns its data. It gives
	// ErrNotFound if it is unknown or expired and ErrUsed on a second use.
	OnceUse(ctx context.Context, kind string, value []byte, now int64) (string, error)
	// OncePurge removes values that expired before now.
	OncePurge(ctx context.Context, now int64) error

	Close() error
}

// Store is what the API needs.
type Store interface {
	Local

	// UserPut inserts or replaces a user. Names are unique: the same name
	// for another ID gives ErrInvalid.
	UserPut(ctx context.Context, u User) error
	UserGet(ctx context.Context, id string) (User, error)
	UserByName(ctx context.Context, name string) (User, error)
	// UserList returns every user, ordered by Name.
	UserList(ctx context.Context) ([]User, error)
	UserDelete(ctx context.Context, id string) error

	SessionPut(ctx context.Context, s Session) error
	SessionGet(ctx context.Context, token []byte) (Session, error)
	// SessionTouch sets Seen; it gives ErrNotFound for an unknown session.
	SessionTouch(ctx context.Context, token []byte, now int64) error
	SessionDelete(ctx context.Context, token []byte) error
	// SessionPurge removes sessions that expired before now.
	SessionPurge(ctx context.Context, now int64) error

	PendingPut(ctx context.Context, p Pending) error
	// PendingList returns every pending controller, ordered by Since then Name.
	PendingList(ctx context.Context) ([]Pending, error)
	PendingDelete(ctx context.Context, key []byte) error

	// LogAdd keeps an entry. The caller sets e.ID (core.ValidLogID). An ID
	// that is missing or not core.ValidLogID, an empty Kind or a Time below 1
	// gives ErrInvalid; a second entry with the same ID gives ErrUsed.
	LogAdd(ctx context.Context, e core.LogEntry) error
	// LogQuery returns entries newest first, ordered by (Time, ID)
	// descending, IDs compared byte by byte; only entries strictly after the
	// cursor, (Time, ID) < (q.BeforeTime, q.BeforeID), so with BeforeID empty
	// Time < BeforeTime, and no cursor while BeforeTime is 0; at most q.Limit;
	// only those of q.User if set. A Limit below 1, a BeforeTime below 0, or a
	// BeforeID that is not core.ValidLogID or comes without a BeforeTime gives
	// ErrInvalid.
	LogQuery(ctx context.Context, q core.LogQuery) ([]core.LogEntry, error)

	// LockTake takes the lock until until, unless someone else holds it and
	// their time has not passed at now. The same owner may take it again and
	// moves the time. It is atomic, also across several APIs on one database:
	// of concurrent calls of different owners for a free lock, exactly one
	// succeeds.
	LockTake(ctx context.Context, name, owner string, now, until int64) (bool, error)
	// LockFree gives the lock back; another owner is ignored.
	LockFree(ctx context.Context, name, owner string) error
}
