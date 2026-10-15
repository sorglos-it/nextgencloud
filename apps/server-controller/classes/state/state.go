// Package state keeps what the server needs to carry on without an API: the
// versions of the trust list, the apps it runs, their licences, the updates it
// has laid ready, the NGC parts installed on it and the task IDs it has seen.
// Everything lives in dbapi.Local, so the same code runs on the controller's
// own SQLite and on memstore in the tests.
package state

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Document IDs under dbapi.BlobSetting. dbapi knows fixed kinds of documents;
// what only the controller has — the updates it laid ready and the NGC parts
// installed on it — is a setting, so the contract needs no change for it.
const (
	stagedPrefix  = "staged."    // + the target of the update
	componentsKey = "components" // the NGC parts installed on this server
)

// Limits.
const (
	// maxStaged bounds one staged update as a document. Every field of Staged
	// is bounded well below it, so the room is for the member names and the
	// numbers.
	maxStaged = 4096
	// maxFile is where the path of a staged file ends. Far below what Linux
	// allows, because the path has to fit into maxStaged along with the rest.
	maxFile = 1024
)

var (
	// maxVersion bounds one version of the trust list as a document: the same
	// room trust.ParseSigned gives it, so a version this store writes is one
	// it can read back.
	maxVersion = enc.B64.EncodedLen(sign.MaxPayload) + 64<<10
	// licenseBudget bounds one licence while it is decoded (enc.Budget): the
	// base64url of a payload of store.MaxLicense bytes plus room for the key
	// ID, the signature, the member names and the time it was issued. It has
	// no array, so the budget only bounds its size — and keeps out nulls and
	// numbers past enc.MaxSafeInteger, which enc.DecodeStrict alone would let
	// through.
	licenseBudget = enc.Budget{Size: enc.B64.EncodedLen(store.MaxLicense) + 2<<10}
)

// components are the NGC parts a server reports; the same three keys
// tunnel.ParseStatus takes, so what is kept here can always be reported.
var components = []string{trust.AutoWeb, trust.AutoAPI, trust.AutoController}

// componentsBudget bounds the installed parts while they are decoded: one
// object counted like a list, at most one version per component.
var componentsBudget = enc.Budget{Size: 4096, Items: map[string]int{"": len(components)}}

// updateStates are the states a staged update may be in; the same set
// store.ParseUpdateReport takes.
var updateStates = []string{store.UpdateAvailable, store.UpdateDownloaded, store.UpdateInstalling,
	store.UpdateInstalled, store.UpdateRolledBack, store.UpdateFailed, store.UpdateRejected,
	store.UpdateNeedsGrants}

// ErrDocument reports a stored document this package will not hand out: too
// large, not the JSON it wrote, or describing something it would refuse to
// write. It carries nothing of the row, the way the parsers of the contracts
// do, because a decoder's error quotes parts of its input.
var ErrDocument = errors.New("state: damaged document")

// Staged is an update lying ready on disk, waiting to be installed.
type Staged struct {
	Target  string    `json:"target"`
	Version string    `json:"version,omitempty"`
	SHA256  enc.Bytes `json:"sha256"`
	Size    int64     `json:"size"`
	Signer  enc.Bytes `json:"signer,omitempty"` // the key whose signature was verified; empty for system
	Seq     uint64    `json:"seq,omitempty"`
	File    string    `json:"file"`
	State   string    `json:"state"`
	Time    int64     `json:"time"`
}

// valid reports whether st is an update this server could have laid ready. It
// guards both ways: Target becomes part of a document ID, and File names a
// file a later task hands to a program running as root, so neither a caller's
// slip nor a damaged row gets that far.
func (st Staged) valid() bool {
	switch {
	case !slices.Contains(updateStates, st.State):
		return false
	case st.Size < 0 || st.Size > enc.MaxSafeInteger || st.Seq > enc.MaxSafeInteger ||
		st.Time < 1 || st.Time > enc.MaxSafeInteger:
		return false
	case st.Target == trust.AutoSystem:
		// apt brings the system packages: nothing of ours is downloaded, so
		// there is usually no file at all, no version we picked and no
		// checksum to check, and their standing approval carries no key
		// either (store.Approved).
		return (st.File == "" || validFile(st.File)) && st.Version == "" &&
			len(st.SHA256) == 0 && len(st.Signer) == 0
	default:
		// An update without a signature is possible (core.Unattended on a
		// standing approval), so Signer may be empty; a key is a key, though.
		return store.ValidTarget(st.Target) && store.ValidVersion(st.Version) &&
			len(st.SHA256) == sha256.Size && validFile(st.File) &&
			(len(st.Signer) == 0 || len(st.Signer) == ed25519.PublicKeySize)
	}
}

// validFile checks the path of a staged file: inside config.StageDir, bounded,
// without a step upwards and without a zero byte. A later task hands this path
// to a program running as root, so it may name nothing but a file this server
// laid ready itself.
func validFile(p string) bool {
	if len(p) > maxFile || !strings.HasPrefix(p, config.StageDir+"/") || strings.IndexByte(p, 0) >= 0 {
		return false
	}
	return !slices.Contains(strings.Split(p, "/"), "..")
}

// stagedID is the document ID of one staged update. The prefix keeps every
// target inside its own namespace, whatever the target says.
func stagedID(target string) string { return stagedPrefix + target }

// Store is the controller's local state.
type Store struct{ db dbapi.Local }

// New wraps a local database.
func New(db dbapi.Local) *Store { return &Store{db: db} }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// PutVersion keeps one version of the trust list under its number. It refuses
// what trust.ParseSigned would refuse on the way out, so nothing is written
// that cannot be read again.
func (s *Store) PutVersion(ctx context.Context, v trust.Signed, seq uint64) error {
	if seq < 1 || seq > enc.MaxSafeInteger || len(v.Payload) == 0 || len(v.Payload) > sign.MaxPayload ||
		v.Proof.Validate() != nil {
		return dbapi.ErrInvalid
	}
	return s.putJSON(ctx, dbapi.BlobTrust, strconv.FormatUint(seq, 10), seq, maxVersion, v)
}

// Versions returns the versions from number from on, oldest first. Every one
// goes through trust.ParseSigned; Open and Advance check the signatures.
func (s *Store) Versions(ctx context.Context, from uint64) ([]trust.Signed, error) {
	rows, err := s.db.BlobFrom(ctx, dbapi.BlobTrust, from)
	if err != nil {
		return nil, err
	}
	out := make([]trust.Signed, 0, len(rows))
	for _, b := range rows {
		// The ID is the number; a row where the two disagree is damaged.
		if b.ID != strconv.FormatUint(b.Seq, 10) {
			return nil, ErrDocument
		}
		v, err := trust.ParseSigned(b.Data)
		if err != nil {
			return nil, ErrDocument
		}
		out = append(out, v)
	}
	return out, nil
}

// NewestSeq returns the number of the newest version kept, or 0 while there is
// none. dbapi.Local has no query for the largest number, so this reads the
// versions; a second row holding the number would be a second truth that a
// crash between the two writes could break.
func (s *Store) NewestSeq(ctx context.Context) (uint64, error) {
	rows, err := s.db.BlobFrom(ctx, dbapi.BlobTrust, 0)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	newest := rows[len(rows)-1]
	if newest.ID != strconv.FormatUint(newest.Seq, 10) {
		return 0, ErrDocument
	}
	return newest.Seq, nil
}

// validApp checks the row of an app. A row is input from outside on the way
// out too: its ID names a container and a directory, and its version decides
// whether an update counts as newer (store.Newer). An app whose version is
// empty is one of which only kept data remains (dbapi.App).
func validApp(a dbapi.App) bool {
	return wire.ValidAppID(a.ID) && (a.Version == "" || store.ValidVersion(a.Version))
}

// PutApp inserts or replaces the row of an app.
func (s *Store) PutApp(ctx context.Context, a dbapi.App) error {
	if !validApp(a) {
		return dbapi.ErrInvalid
	}
	return s.db.AppPut(ctx, a)
}

// App returns the row of one app or dbapi.ErrNotFound.
func (s *Store) App(ctx context.Context, id string) (dbapi.App, error) {
	if !wire.ValidAppID(id) {
		return dbapi.App{}, dbapi.ErrInvalid
	}
	a, err := s.db.AppGet(ctx, id)
	if err != nil {
		return dbapi.App{}, err
	}
	if !validApp(a) || a.ID != id {
		return dbapi.App{}, ErrDocument
	}
	return a, nil
}

// Apps returns every app, ordered by ID.
func (s *Store) Apps(ctx context.Context) ([]dbapi.App, error) {
	apps, err := s.db.AppList(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range apps {
		if !validApp(a) {
			return nil, ErrDocument
		}
	}
	return apps, nil
}

// DeleteApp removes the row of an app; removing what is not there is no error.
func (s *Store) DeleteApp(ctx context.Context, id string) error {
	if !wire.ValidAppID(id) {
		return dbapi.ErrInvalid
	}
	return s.db.AppDelete(ctx, id)
}

// licenseDoc is how one licence is kept: the envelope as it arrived and the
// time from the payload the controller verified, so the newest can be told
// from an older one without verifying again.
type licenseDoc struct {
	Envelope sign.Envelope `json:"envelope"`
	Issued   int64         `json:"issued"`
}

// PutLicense keeps the licence of an app. It replaces whatever is there;
// keeping only the newest one (CLAUDE.md) is the caller's job, which reads the
// licence it has, compares issued and answers with the one it keeps.
func (s *Store) PutLicense(ctx context.Context, app string, env sign.Envelope, issued int64) error {
	if !wire.ValidAppID(app) || issued < 1 || issued > enc.MaxSafeInteger ||
		env.Validate(store.MaxLicense) != nil {
		return dbapi.ErrInvalid
	}
	return s.putJSON(ctx, dbapi.BlobLicense, app, 0, licenseBudget.Size, licenseDoc{Envelope: env, Issued: issued})
}

// License returns the licence kept for an app and the time it was issued, or
// dbapi.ErrNotFound. It says nothing about whether the app may run: that
// answers store.License.State, which alone looks at the expiry.
func (s *Store) License(ctx context.Context, app string) (sign.Envelope, int64, error) {
	if !wire.ValidAppID(app) {
		return sign.Envelope{}, 0, dbapi.ErrInvalid
	}
	data, err := s.blob(ctx, dbapi.BlobLicense, app)
	if err != nil {
		return sign.Envelope{}, 0, err
	}
	return decodeLicense(data)
}

// decodeLicense reads one licence document. The envelope goes through the
// check of its own contract, the one sign.ParseEnvelope runs.
func decodeLicense(data []byte) (sign.Envelope, int64, error) {
	var doc licenseDoc
	if licenseBudget.Decode(data, &doc) != nil || doc.Issued < 1 ||
		doc.Envelope.Validate(store.MaxLicense) != nil {
		return sign.Envelope{}, 0, ErrDocument
	}
	return doc.Envelope, doc.Issued, nil
}

// PutStaged keeps one update lying ready, one per target.
func (s *Store) PutStaged(ctx context.Context, st Staged) error {
	if !st.valid() {
		return dbapi.ErrInvalid
	}
	return s.putJSON(ctx, dbapi.BlobSetting, stagedID(st.Target), 0, maxStaged, st)
}

// Staged returns the update lying ready for a target or dbapi.ErrNotFound.
func (s *Store) Staged(ctx context.Context, target string) (Staged, error) {
	if !store.ValidTarget(target) {
		return Staged{}, dbapi.ErrInvalid
	}
	data, err := s.blob(ctx, dbapi.BlobSetting, stagedID(target))
	if err != nil {
		return Staged{}, err
	}
	st, err := decodeStaged(data)
	if err != nil {
		return Staged{}, err
	}
	// The row has to be the one that was asked for. A row moved or edited
	// under another key must not answer here: the caller decides by Target
	// whether apt runs or a downloaded file is installed.
	if st.Target != target {
		return Staged{}, ErrDocument
	}
	return st, nil
}

// DeleteStaged forgets the update lying ready for a target; the file itself is
// the caller's to remove. Forgetting what is not there is no error.
func (s *Store) DeleteStaged(ctx context.Context, target string) error {
	if !store.ValidTarget(target) {
		return dbapi.ErrInvalid
	}
	return s.db.BlobDelete(ctx, dbapi.BlobSetting, stagedID(target))
}

// decodeStaged reads one staged update. A row that says something PutStaged
// would not have written is refused like any input from outside.
func decodeStaged(data []byte) (Staged, error) {
	var st Staged
	if err := enc.DecodeStrict(data, maxStaged, &st); err != nil || !st.valid() {
		return Staged{}, ErrDocument
	}
	return st, nil
}

// PutComponents keeps the versions of the NGC parts installed on this server.
// An empty map clears them.
func (s *Store) PutComponents(ctx context.Context, m map[string]string) error {
	if err := validComponents(m); err != nil {
		return err
	}
	if m == nil {
		// encoding/json writes a nil map as null, which no document may hold.
		m = map[string]string{}
	}
	return s.putJSON(ctx, dbapi.BlobSetting, componentsKey, 0, componentsBudget.Size, m)
}

// Components returns the versions of the NGC parts installed on this server,
// or dbapi.ErrNotFound before the first write.
func (s *Store) Components(ctx context.Context) (map[string]string, error) {
	data, err := s.blob(ctx, dbapi.BlobSetting, componentsKey)
	if err != nil {
		return nil, err
	}
	return decodeComponents(data)
}

// decodeComponents reads the installed parts.
func decodeComponents(data []byte) (map[string]string, error) {
	var m map[string]string
	if componentsBudget.Decode(data, &m) != nil || validComponents(m) != nil {
		return nil, ErrDocument
	}
	return m, nil
}

// validComponents checks the installed parts by the rule that lets them be
// reported at all (tunnel.ParseStatus): only the three NGC parts, each with a
// release version. A development build is no release version and is therefore
// not kept here.
func validComponents(m map[string]string) error {
	if len(m) > len(components) {
		return dbapi.ErrInvalid
	}
	for c, v := range m {
		if !slices.Contains(components, c) || !store.ValidVersion(v) {
			return dbapi.ErrInvalid
		}
	}
	return nil
}

// UseTask remembers the ID of a signed task until it expires and gives
// dbapi.ErrUsed if it was seen before, so no task runs twice. An ID whose task
// has already expired is refused: Purge would free it again at once, and the
// guard would be none.
func (s *Store) UseTask(ctx context.Context, id []byte, expires, now int64) error {
	if len(id) != task.IDSize || expires <= now {
		return dbapi.ErrInvalid
	}
	return s.db.OnceCreate(ctx, dbapi.OnceTask, id, "", expires)
}

// Purge frees the IDs of tasks that have expired. The controller has its own
// database and one clock, so it subtracts no session.MaxSkew: nobody else
// accepts a task by a clock of their own.
func (s *Store) Purge(ctx context.Context, now int64) error { return s.db.OncePurge(ctx, now) }

// blob returns the bytes of one document or dbapi.ErrNotFound.
func (s *Store) blob(ctx context.Context, kind, id string) ([]byte, error) {
	b, err := s.db.BlobGet(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	return b.Data, nil
}

// putJSON writes one document, refusing anything over max: what this store
// writes, it must be able to read back. A value that does not fit is the
// caller's mistake, so it gives dbapi.ErrInvalid, not ErrDocument — that one
// means a row already in the database is damaged.
func (s *Store) putJSON(ctx context.Context, kind, id string, seq uint64, max int, v any) error {
	data, err := json.Marshal(v)
	if err != nil || len(data) > max {
		return dbapi.ErrInvalid
	}
	return s.db.BlobPut(ctx, dbapi.Blob{Kind: kind, ID: id, Seq: seq, Data: data})
}
