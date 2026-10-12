// Package appstore serves ngc.store: the stores of the trust list and their
// catalogs, uploaded apps, the UIs the shell runs, the licences of paid apps
// and the activation of an app on a controller.
//
// Everything this package reads comes from a stranger. A store is a web server
// somebody else runs (Entwurf 4.4), so three things are never taken on trust
// here, and each of them is a wall of its own:
//
//   - the address, because a store URL is input too: trust.ValidStoreURL, before
//     any request is made;
//   - the size, because an answer must not be read into memory to be refused:
//     maxEnvelope, and one byte over it is enough to know;
//   - the document, through the Verify function of the contract alone
//     (store.VerifyCatalog, store.VerifyRelease), which checks the signature,
//     the key, the purpose, the form and the sequence in one place. This
//     package writes no check of its own over a signed document, and it never
//     reads a payload that did not pass one.
//
// The sequence is the one of those a signature cannot stand in for: a store
// that serves its own older, correctly signed catalog again would put apps,
// versions and prices it has withdrawn back in reach. store.ErrRollback is that
// case, and lastSeq is what this package has to hand the contract for it to be
// seen at all.
package appstore

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Names of the files a store serves. The contracts leave this to the API; a
// store is files on any web server (Entwurf 4.4).
const (
	CatalogFile = "catalog.json" // sign.Envelope over a store.Catalog
	ReleaseFile = "release.json" // sign.Envelope over a store.Release, global store only
)

// Rows this package keeps besides the catalogs themselves.
const (
	// SettingStore is the prefix of the row that holds what ngc.store/sources
	// shows about one store: the time of its last success and the reason its
	// last try failed. ID: SettingStore + the store's ID.
	SettingStore = "store."
	// SettingRelease holds the newest release envelope seen, with its Seq. It
	// is what store.VerifyRelease is handed as lastSeq, so a release rolled
	// back is seen the same way a catalog rolled back is.
	SettingRelease = "release"
)

// maxEnvelope is what an answer of a store may have: the base64url of the
// largest payload plus room for the envelope itself. sign.ParseEnvelope bounds
// the same thing again while it decodes; this bound is the one that holds
// before the bytes are in memory.
const maxEnvelope = int64(sign.MaxPayload/3*4 + 1<<10)

// maxDownloadURL is the longest address a catalog or a release may point at. It
// is store's own bound for a signed download link, which this package cannot
// call: store.Catalog ran it over every URL in the catalog already
// (store.VerifyCatalog), so the value is repeated here and nothing else of that
// grammar is.
const maxDownloadURL = 512

// ErrStore reports an answer of a store NGC does not take: an address outside
// the grammar, an answer too large, a status other than 200, a download that
// does not match its size or its checksum. What a signed document is refused
// for keeps the error of the contract instead (sign.ErrWrongKey,
// sign.ErrBadSignature, store.ErrCatalog, store.ErrRollback), because those
// tell an administrator apart what nobody can mend from what the store can.
var ErrStore = errors.New("appstore: store answered something unusable")

// Stores fetches and verifies catalogs and releases.
type Stores struct {
	S      *state.State
	Client *http.Client
	// Log is where a change to a store or a catalog is written down. It is the
	// only log this package writes to: audit.Log checks every field itself, so
	// nothing a store sends can forge a line in it. audit.ErrDropped is a
	// report and never costs the entry, so it is not acted on here.
	Log *audit.Log
	// MaxUpload bounds the package of one upload (Upload) and of the uploaded
	// file of one activation. New sets it to store.MaxPackage, and
	// uploadLimit does not let a higher value stand: it is here so a test need
	// not send 4 GiB, not so a caller can take more.
	MaxUpload int64
	// Send is how this package reaches a controller (Sender). It is nil until
	// the server wires it, and every step that would need it answers
	// E_INTERNAL while it is.
	Send Sender
	// Spool is the directory the package of an activation is written to while
	// it is read (fetchPackage). Empty means the directory the operating
	// system names, which is what a test uses; a server sets it to its data
	// directory, because a package may be gigabytes and must not land in a
	// RAM-backed /tmp.
	Spool string
}

// New returns Stores with a client that follows no redirect and waits at most
// a minute: a store is a plain web server, and NGC never follows it elsewhere.
// Proxy is nil on purpose and not http.ProxyFromEnvironment: an environment
// variable must not be able to put a machine between NGC and a store.
func New(s *state.State) *Stores {
	st := &Stores{S: s, Client: &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true},
	}, MaxUpload: store.MaxPackage}
	if s != nil {
		st.Log = &audit.Log{DB: s.DB, Now: s.Now}
	}
	return st
}

// fileURL joins the address of a store with a file name. trust.ValidStoreURL
// has no trailing slash and no query, so one slash is always right; the caller
// has checked base with it.
func fileURL(base, name string) string { return base + "/" + name }

// get fetches a document of at most max bytes.
func (st *Stores) get(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, ErrStore
	}
	res, err := st.Client.Do(req)
	if err != nil {
		return nil, ErrStore
	}
	defer res.Body.Close()
	// CheckRedirect hands the 3xx back instead of following it, so a redirect
	// arrives here as a status other than 200 and is refused like any other.
	if res.StatusCode != http.StatusOK {
		return nil, ErrStore
	}
	// One byte over the limit is enough to know it is too large; nothing
	// larger is ever read into memory.
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, ErrStore
	}
	return b, nil
}

// Refresh fetches the catalog of every store of the trust list and keeps what
// verifies. A store that fails leaves the last good catalog in place and does
// not stop the others: one store nobody can reach must not cost an
// installation the catalogs of the rest.
func (st *Stores) Refresh(ctx context.Context) error {
	v, err := st.S.Trust(ctx)
	if err != nil {
		return err
	}
	for _, s := range v.List.Stores {
		err := st.refreshOne(ctx, s)
		// The reason is shown to admins in ngc.store/sources, in German, and
		// writing it down must not swallow the next store either.
		_ = st.note(ctx, s.ID, err)
	}
	return nil
}

// refreshOne fetches and verifies one catalog and keeps it. Every return but
// the last leaves the stored catalog untouched.
func (st *Stores) refreshOne(ctx context.Context, s trust.Store) error {
	// A store is an address from outside, even inside a signed trust list: the
	// list is signed by an admin of this installation, who typed the address,
	// not by anything that checked it. trust.Parse refuses such a list too, so
	// this is the second wall and not the only one.
	if !trust.ValidStoreURL(s.URL) || !wire.ValidName(s.ID) {
		return ErrStore
	}
	body, err := st.get(ctx, fileURL(s.URL, CatalogFile), maxEnvelope)
	if err != nil {
		return err
	}
	env, err := sign.ParseEnvelope(body, sign.MaxPayload)
	if err != nil {
		return err
	}
	// The sequence already on record goes in, or store.VerifyCatalog cannot
	// see a rollback: without it every older catalog of this store verifies.
	last, err := st.lastSeq(ctx, dbapi.BlobCatalog, s.ID)
	if err != nil {
		return err
	}
	c, err := store.VerifyCatalog(env, s, last)
	if err != nil {
		return err
	}
	// What is kept is the envelope the store served, not the parsed catalog:
	// the shell checks the signature itself against the key in its own trust
	// list (core.CatalogResult), so it never has to believe this API.
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobCatalog, ID: s.ID, Seq: c.Seq,
		Data: data, Updated: st.S.Now()})
}

// lastSeq is the sequence of a stored document, 0 where none is stored. A read
// that fails for another reason is handed on: taking it for 0 would turn a
// database hiccup into an open door for an older catalog.
func (st *Stores) lastSeq(ctx context.Context, kind, id string) (uint64, error) {
	b, err := st.S.DB.BlobGet(ctx, kind, id)
	if errors.Is(err, dbapi.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return b.Seq, nil
}

// Release fetches the release file of the global store and checks it against
// the release key of the configuration -- never against a store key. The two
// keys are different on purpose: a store that is taken over may offer apps,
// but it must not be able to offer an NGC.
func (st *Stores) Release(ctx context.Context) (*store.Release, sign.Envelope, error) {
	base, err := st.globalURL(ctx)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	body, err := st.get(ctx, fileURL(base, ReleaseFile), maxEnvelope)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	env, err := sign.ParseEnvelope(body, sign.MaxPayload)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	last, err := st.lastSeq(ctx, dbapi.BlobSetting, SettingRelease)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	r, err := store.VerifyRelease(env, ed25519.PublicKey(st.S.Cfg.ReleaseKey), last)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	if err := st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: SettingRelease,
		Seq: r.Seq, Data: data, Updated: st.S.Now()}); err != nil {
		return nil, sign.Envelope{}, err
	}
	return r, env, nil
}

// globalURL is the address of the global store: the one the trust list names,
// and the configuration's only while the list names none. Both are checked
// again here, because being signed or being in a file says nothing about the
// address itself.
func (st *Stores) globalURL(ctx context.Context) (string, error) {
	v, err := st.S.Trust(ctx)
	if err != nil {
		return "", err
	}
	url := st.S.Cfg.StoreURL
	for _, s := range v.List.Stores {
		if s.ID == store.GlobalID {
			url = s.URL
			break
		}
	}
	if !trust.ValidStoreURL(url) {
		return "", ErrStore
	}
	return url, nil
}

// Download fetches a file of exactly size bytes whose SHA-256 is sum and
// writes it to w. A file that is larger, shorter or different is an error, and
// the caller throws away what w received: nothing here can take written bytes
// back, so a caller that keeps them on an error keeps a forged package.
//
// The address is one a verified catalog or release carried, so store's own
// grammar for a signed download link has already run over it
// (store.VerifyCatalog, store.VerifyRelease). That grammar is not exported, so
// what is checked again here is the scheme, the length and the host by the
// grammar of trust -- enough that a caller which composed an address itself
// cannot send NGC anywhere else.
func (st *Stores) Download(ctx context.Context, url string, size int64, sum []byte, w io.Writer) error {
	if size <= 0 || size > store.MaxPackage || len(sum) != sha256.Size || !downloadURL(url) {
		return ErrStore
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ErrStore
	}
	res, err := st.Client.Do(req)
	if err != nil {
		return ErrStore
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrStore
	}
	h := sha256.New()
	// size+1 and not size: a body that is longer must come out as an error and
	// not as the first size bytes of a file somebody swapped.
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(res.Body, size+1))
	if err != nil || n != size {
		return ErrStore
	}
	// hmac.Equal and not bytes.Equal: the comparison takes the same time
	// whatever the bytes are, so nothing is learnt from how long it took.
	if !hmac.Equal(h.Sum(nil), sum) {
		return ErrStore
	}
	return nil
}

// downloadURL reports whether u is an address NGC fetches a package from: see
// Download for what it checks and what checked it before.
func downloadURL(u string) bool {
	// A fragment says nothing to a server and would hide the rest of the
	// address from a reader; store's own grammar refuses it too.
	if len(u) > maxDownloadURL || strings.Contains(u, "#") {
		return false
	}
	rest, ok := strings.CutPrefix(u, "https://")
	if !ok {
		return false
	}
	// A bracketed IPv6 host holds no "/", so the first one starts the path;
	// user info, query and fragment fail as part of the host.
	authority, _, _ := strings.Cut(rest, "/")
	return trust.ValidHost(authority) || trust.ValidHostPort(authority)
}

// PackageURL resolves the URL of a package as a catalog carries it: an https
// address as it stands, or a path relative to the catalog (store.ValidPath).
// ok is false for anything else, so a caller never builds an address out of
// something the catalog did not say.
func PackageURL(base, u string) (string, bool) {
	if !trust.ValidStoreURL(base) {
		return "", false
	}
	if strings.HasPrefix(u, "https://") {
		if !downloadURL(u) {
			return "", false
		}
		return u, true
	}
	if !store.ValidPath(u) {
		return "", false
	}
	full := base + "/" + u
	if !downloadURL(full) {
		return "", false
	}
	return full, true
}

// note writes down how one store fared: the time of a success, or the reason a
// try failed, in German and out of a closed list (reasons). The row is what
// ngc.store/sources reads; a change of what NGC holds about a store goes into
// the log as well (core.LogStore).
func (st *Stores) note(ctx context.Context, id string, cause error) error {
	s := sourceState{}
	detail := id
	if cause == nil {
		s.Checked = st.S.Now()
		detail += " aktualisiert"
	} else {
		s.Error = reasonFor(cause)
		// The old success time stays: an administrator has to see when the
		// catalog on record was last confirmed, not only that the last try
		// failed.
		if old, err := st.readSource(ctx, id); err == nil {
			s.Checked = old.Checked
		}
		detail += " " + s.Error
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: SettingStore + id,
		Data: data, Updated: st.S.Now()}); err != nil {
		return err
	}
	if st.Log == nil {
		return nil
	}
	// audit.ErrDropped says a field was replaced and the entry was written: it
	// is a report, so it is not handed on as a failure of this step.
	if err := st.Log.Note(ctx, core.LogStore, audit.UserSystem, "", "", detail); err != nil &&
		!errors.Is(err, audit.ErrDropped) {
		return err
	}
	return nil
}

// sourceState is the row behind SettingStore: what NGC itself wrote about one
// store, never a word of the store's own.
type sourceState struct {
	Checked int64  `json:"checked,omitzero"`
	Error   string `json:"error,omitempty"`
}
