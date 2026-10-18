package appstore

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Sender is the one way out of this package to a controller. It is written
// out here instead of imported so that appstore does not depend on the relay
// that serves it, and so a case can measure the header and the file this
// package hands over byte for byte. relay.Relay does all three things; the
// adapter that maps a node to its tunnel hop belongs to the wiring of the
// server and is not built in this branch (see the report of task 22).
//
// Every method takes a node and not a hop, for the same reason: looking a
// node up among the open tunnels is the adapter's job, and this package then
// has no way at all to reach a controller it did not name.
type Sender interface {
	// Node reports what the tunnel to node says about it, the roles of its
	// server among it (tunnel.Hello.Roles). ok is false while no tunnel to
	// that node is up, and that is the only thing this package reads as
	// "online".
	Node(node string) (tunnel.Hello, bool)
	// Transfer opens a core action of flow transfer on node, writes head and
	// file on it and returns the result.
	Transfer(ctx context.Context, node, action string, params json.RawMessage, head core.Transfer,
		file io.Reader, task *sign.Envelope, client []byte, pr *proof.Proof) ([]byte, wire.Code)
	// Call opens a core action of flow call on node and returns the result.
	Call(ctx context.Context, node, action string, params json.RawMessage,
		task *sign.Envelope, client []byte, pr *proof.Proof) ([]byte, wire.Code)
}

// ErrTooLarge reports more bytes than a bound allows: a package over
// store.MaxPackage, or an upload over the bound of this API (MaxUpload). It
// is an error of its own and not ErrStore, because the two must not map to
// one code: a bound that was passed is E_TOOLARGE and tells the sender to
// send less, while ErrStore says a store answered unusably, and nothing the
// sender does changes that (CodeFor).
var ErrTooLarge = errors.New("appstore: more bytes than the bound allows")

// CodeFor maps what this package and the contracts it calls answer with to
// the code a channel ends with. It is the one place that turns an error of an
// app-store step into a wire code, so two callers cannot answer the same
// refusal differently.
//
// The grouping is by what the sender can do about it, not by where the error
// came from:
//
//   - E_VERIFY: something signed or hashed did not hold. A package store.Walk
//     refuses comes out as store.ErrPackage -- that is how task 20 reports an
//     unsafe archive, and this is where it becomes E_VERIFY. A catalog,
//     release or licence whose signature, key, form or order fails, a UI
//     proof that does not hold and a transfer header built wrong are the
//     same case: the sender may send something else, but not this again.
//   - E_TOOLARGE: a bound was passed (ErrTooLarge).
//   - E_LICENSE: a paid app without a licence this API acts on (ErrLicense).
//     Its own code, because "buy or renew" is a different instruction from
//     "this does not verify".
//   - E_UNREACHABLE: a store did not answer usably (ErrStore). A store is
//     somebody else's web server; neither this code nor the admin mends it.
//   - E_NOTFOUND: the row or the catalog entry asked for is not there
//     (dbapi.ErrNotFound).
//   - E_INTERNAL: everything else, which is this installation's own fault.
//
// A nil error maps to the empty code, which is what a channel that ended in
// order carries.
func CodeFor(err error) wire.Code {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTooLarge):
		return wire.CodeTooLarge
	case errors.Is(err, ErrLicense):
		return wire.CodeLicense
	case errors.Is(err, ErrStore):
		return wire.CodeUnreachable
	case errors.Is(err, dbapi.ErrNotFound):
		return wire.CodeNotFound
	case errors.Is(err, store.ErrPackage), errors.Is(err, store.ErrManifest),
		errors.Is(err, store.ErrEncrypted), errors.Is(err, store.ErrCatalog),
		errors.Is(err, store.ErrRelease), errors.Is(err, store.ErrRollback),
		errors.Is(err, store.ErrLicense), errors.Is(err, core.ErrUIProof),
		errors.Is(err, core.ErrTransfer), errors.Is(err, sign.ErrBadSignature),
		errors.Is(err, sign.ErrWrongKey), errors.Is(err, sign.ErrEnvelope):
		return wire.CodeVerify
	}
	return wire.CodeInternal
}

// Carried is who an action is carried out for, and where. All of it travels
// to the controller as it stands: nothing here signs, mends or re-encodes it,
// because the controller checks the task and the proof itself and would read
// any change as a forgery.
type Carried struct {
	Task   *sign.Envelope // the signed root task, as the shell sent it
	Client []byte         // client certificate payload, as the user's proof covers it
	Proof  *proof.Proof   // the user's proof over Client
	Node   string         // the controller the task names
	Live   *login.Live    // the session it runs for; only its user is read, for the log
}

// user is the name the log entry of this step carries, audit.UserSystem where
// no session is at hand.
func (c Carried) user() string {
	if c.Live == nil {
		return audit.UserSystem
	}
	return c.Live.Verified.Cert().User
}

// ActivateRequest is one activation of one app on one controller.
type ActivateRequest struct {
	Carried
	Params *core.ActivateParams
	// File is the package of an uploaded app (core.LocalStore), as the admin
	// sent it to ngc.store/upload; a store app has none, and its package is
	// downloaded. It is an io.ReaderAt and not an io.Reader because the
	// package is read more than once -- to see what is in it, and to send it.
	// This package keeps no upload of its own, so the caller holds the file.
	File io.ReaderAt
	Size int64 // length of File
}

// DeactivateRequest stops an app without removing it.
type DeactivateRequest struct {
	Carried
	Params *core.AppParams
}

// UninstallRequest removes an app, with or without its data.
type UninstallRequest struct {
	Carried
	Params *core.UninstallParams
}

// Activate installs and starts an app on a controller
// (ngc.store/controller/activate).
//
// The order is the whole of it. Everything that can be refused is refused
// before a byte reaches the controller, and no row is written to the database
// before the controller has said the app is up. The package itself is written
// to the spool on the way (fetchPackage) and is gone again when this returns:
//
//  1. the parameters and the tunnel: parameters that do not validate are
//     E_VERIFY, a nil seam or no parameters at all E_INTERNAL, and a node
//     without a tunnel E_UNREACHABLE. In each case nothing else happens;
//  2. the version: activating over a newer installed version is E_FORBIDDEN
//     (core.ActivateParams, "never an older one"). The same version again is
//     allowed, which is how an admin re-approves one;
//  3. the package, whole and verified, in the spool: downloaded with the size
//     and the checksum of the verified catalog entry (Download), or the
//     uploaded file. Its checksum and its UI hash must be the ones the signed
//     task names, or it is E_VERIFY. The controller checks both again; this is
//     the wall before the bytes travel at all;
//  4. the manifest, read out of that same file: an app with services needs a
//     node with the role services, and Grants must equal the manifest's Needs
//     as a set -- both E_FORBIDDEN;
//  5. the header and the file, over the tunnel. A code from there is handed
//     back as it stands and nothing is written;
//  6. the rows: dbapi.App first, then the ui-tar and the evidence. That order
//     is the recoverable one. An app that runs without a row can be neither
//     seen nor removed, while a row whose UI rows are missing is repaired by
//     activating the same version again.
//
// The error is only ever this installation's own failure, and then the code
// says so as well; a refusal carries the code alone.
func (st *Stores) Activate(ctx context.Context, rq ActivateRequest) (wire.Code, error) {
	p := rq.Params
	if st.Send == nil || p == nil {
		return wire.CodeInternal, nil
	}
	if p.Validate() != nil || !wire.ValidName(rq.Node) {
		return wire.CodeVerify, nil
	}
	hello, online := st.Send.Node(rq.Node)
	if !online {
		return wire.CodeUnreachable, nil
	}
	row, err := st.S.DB.AppGet(ctx, p.App)
	if err != nil && !errors.Is(err, dbapi.ErrNotFound) {
		return wire.CodeInternal, err
	}
	if err == nil && row.Version != "" && store.Newer(row.Version, p.Version) {
		return wire.CodeForbidden, nil
	}
	pkg, code, err := st.fetchPackage(ctx, rq)
	if code != "" || err != nil {
		return code, err
	}
	defer pkg.close()
	// A package with services may only go to a server that runs containers.
	// The controller refuses it too; this is the refusal that happens before
	// the file travels.
	if len(pkg.m.Services) > 0 && !slices.Contains(hello.Roles, tunnel.RoleServices) {
		return wire.CodeForbidden, nil
	}
	// Equal as a set, not "enough": an admin who allowed more than the app
	// asks for has approved something other than what was in front of them
	// (core.ActivateParams).
	if !store.Granted(pkg.m.Needs, p.Grants) || !store.Granted(p.Grants, pkg.m.Needs) {
		return wire.CodeForbidden, nil
	}
	head, err := st.buildTransfer(ctx, p, pkg.size)
	if err != nil {
		return CodeFor(err), nil
	}
	params, err := json.Marshal(p)
	if err != nil {
		return wire.CodeInternal, err
	}
	file, err := pkg.reader()
	if err != nil {
		return wire.CodeInternal, err
	}
	if _, code := st.Send.Transfer(ctx, rq.Node, core.ActActivate, params, head, file,
		rq.Task, rq.Client, rq.Proof); code != "" {
		return code, nil
	}
	return st.keepActivation(ctx, rq, pkg, head)
}

// keepActivation writes down what now runs: the app row, then the ui-tar and
// the evidence of this version. It runs after the controller reported the app
// up, so a failure in it is a failure of bookkeeping and not of the
// activation -- which is why the app row goes first.
func (st *Stores) keepActivation(ctx context.Context, rq ActivateRequest, pkg *onDisk,
	head core.Transfer) (wire.Code, error) {
	p := rq.Params
	if err := st.S.DB.AppPut(ctx, dbapi.App{ID: p.App, Version: p.Version, Store: p.Store,
		Node: rq.Node, Active: true, Manifest: pkg.raw, SHA256: p.SHA256,
		Grants: slices.Clone(p.Grants), Updated: st.S.Now()}); err != nil {
		return wire.CodeInternal, err
	}
	if err := st.KeepUI(ctx, p.App, p.Version, pkg.ui); err != nil {
		return wire.CodeInternal, err
	}
	pr := core.UIProof{App: p.App, Version: p.Version, Store: p.Store}
	if p.Store == core.LocalStore {
		pr.Task, pr.Client, pr.Proof = rq.Task, enc.Bytes(rq.Client), rq.Proof
	} else {
		pr.Catalog = head.Catalog
	}
	if err := st.KeepProof(ctx, pr); err != nil {
		// The app runs, and the evidence for its UI does not hold. The app
		// row stays as it is, so the app can be seen and removed, and the
		// shell is told that this activation does not stand up all the way.
		return CodeFor(err), nil
	}
	if st.Log == nil {
		return "", nil
	}
	if err := st.Log.Note(ctx, core.LogApp, rq.user(), p.App, rq.Node,
		"aktiviert "+p.Version); err != nil && !errors.Is(err, audit.ErrDropped) {
		return wire.CodeInternal, err
	}
	return "", nil
}

// buildTransfer puts together the header of an activation: the catalog of the
// app's store for a store app, so the controller can check version, checksum
// and UI hash against a signed document itself; the licence and the unwrapped
// content key for a paid app, which travel only inside the tunnel, because
// the API alone can unwrap the key; and nothing but size and checksum for an
// uploaded app, whose signed task names them.
func (st *Stores) buildTransfer(ctx context.Context, p *core.ActivateParams, size int64) (core.Transfer, error) {
	head := core.Transfer{Size: size, SHA256: p.SHA256}
	if p.Store == core.LocalStore {
		return head, head.Validate()
	}
	_, env, err := st.Catalog(ctx, p.Store)
	if err != nil {
		return core.Transfer{}, err
	}
	head.Catalog, head.Store = &env, p.Store
	// licenseFrom and not License: the app may have no row yet -- the row is
	// what this activation writes -- so the store it came from is the one the
	// signed task names.
	l, lenv, ok, err := st.licenseFrom(ctx, p.App, p.Store)
	if err != nil {
		return core.Transfer{}, err
	}
	if ok && l.Key != nil {
		key, err := st.contentKeyFrom(ctx, p.App, p.Store)
		if err != nil {
			return core.Transfer{}, err
		}
		head.License, head.Key = &lenv, key
	}
	return head, head.Validate()
}

// onDisk is the package of one activation: the whole file, verified, in the
// spool, with what was read out of it.
type onDisk struct {
	f    *os.File
	size int64
	m    *store.Manifest
	raw  []byte // manifest.json as the package carries it
	ui   []byte // the canonical ui-tar (store.PackageUI)
}

// reader hands the file out from its first byte.
func (d *onDisk) reader() (io.Reader, error) {
	if _, err := d.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return d.f, nil
}

// close drops the spooled file, name and all: a package left lying in the
// spool is a copy of software nobody asked to keep.
func (d *onDisk) close() {
	if d == nil || d.f == nil {
		return
	}
	name := d.f.Name()
	_ = d.f.Close()
	_ = os.Remove(name)
}

// fetchPackage puts the package of rq into the spool and reads it.
//
// It goes to disk and not into memory on purpose: store.MaxPackage is 4 GiB,
// and none of it may be held here -- task 19 saw a process die of out of
// memory at that size, and task 20 measured 268 MiB held for a 128 MiB
// package read with io.ReadAll against 0 MiB read in one pass. Neither
// measurement was of 4 GiB in memory; nobody ran that on purpose. And it goes
// to disk rather than straight onto the tunnel for a second reason: a stream is read once, so nothing could be refused before
// the controller already had the bytes -- neither the role of the node, nor
// the grants, nor the UI hash.
func (st *Stores) fetchPackage(ctx context.Context, rq ActivateRequest) (*onDisk, wire.Code, error) {
	p := rq.Params
	f, err := os.CreateTemp(st.Spool, "ngc-pkg-")
	if err != nil {
		return nil, wire.CodeInternal, err
	}
	d := &onDisk{f: f}
	size, err := st.spoolPackage(ctx, rq, f)
	if err != nil {
		d.close()
		return nil, CodeFor(err), nil
	}
	d.size = size
	// The checksum of the signed task is the last word on these bytes. For a
	// store app Download compared the catalog's already; both must say the
	// same thing, or admin and store do not mean the same file.
	sum := sha256.New()
	if _, err := io.Copy(sum, io.NewSectionReader(f, 0, size)); err != nil {
		d.close()
		return nil, wire.CodeInternal, err
	}
	// hmac.Equal and not bytes.Equal, as everywhere else a hash is compared
	// in this package.
	if !hmac.Equal(sum.Sum(nil), p.SHA256) {
		d.close()
		return nil, wire.CodeVerify, nil
	}
	if d.raw, err = manifestOf(io.NewSectionReader(f, 0, size)); err != nil {
		d.close()
		return nil, CodeFor(err), nil
	}
	var ui bytes.Buffer
	m, uiHash, err := store.PackageUI(io.NewSectionReader(f, 0, size), &ui)
	if err != nil {
		d.close()
		return nil, CodeFor(err), nil
	}
	// The bytes kept as the manifest need no judge of their own: they are the
	// first entry of the archive, and that is the entry store.Walk insists on
	// being manifest.json and parses itself (store.ParseManifest) before any
	// other byte is read. Parsing them a second time here would ask the same
	// question twice, and a mutation that took it out killed no case.
	if m.ID != p.App || m.Version != p.Version || !hmac.Equal(uiHash, p.UI) {
		d.close()
		return nil, wire.CodeVerify, nil
	}
	d.m, d.ui = m, ui.Bytes()
	return d, "", nil
}

// spoolPackage writes the package of rq to w and answers its length: the
// uploaded file as it stands, or the file the verified catalog entry points
// at, fetched with that entry's size and checksum.
func (st *Stores) spoolPackage(ctx context.Context, rq ActivateRequest, w io.Writer) (int64, error) {
	p := rq.Params
	if p.Store == core.LocalStore {
		if rq.File == nil || rq.Size <= 0 {
			// NO FILE FROM THE CALLER, so the one this server kept at the
			// upload is used -- found by the checksum of the SIGNED task and
			// by nothing else (hold.go). This is Punkt 30: until the hold
			// existed the one line here was the whole branch, and an uploaded
			// app could not be activated at all.
			//
			// The caller still wins where it brings a file: nothing here
			// decides for a caller that knows better, and every case that
			// hands one over measures the same path as before.
			n, err := st.copyHeld(p.SHA256, w, st.uploadLimit())
			if err != nil {
				// ErrStore and not the error of the disk: for whoever asked,
				// "this server has no such file" is one case, and it is the
				// one that was answered before -- a server without a hold, a
				// package that aged out and an upload that went to another API
				// server all read alike. ErrTooLarge keeps its own code,
				// because a bound that was passed must not read as a missing
				// file.
				if errors.Is(err, ErrTooLarge) {
					return 0, err
				}
				return 0, ErrStore
			}
			return n, nil
		}
		if rq.Size > st.uploadLimit() {
			return 0, ErrTooLarge
		}
		n, err := io.Copy(w, io.NewSectionReader(rq.File, 0, rq.Size))
		if err != nil {
			return 0, err
		}
		if n != rq.Size {
			return 0, ErrStore
		}
		return n, nil
	}
	c, _, err := st.Catalog(ctx, p.Store)
	if err != nil {
		return 0, err
	}
	pk, err := packageOf(c, p.App, p.Version)
	if err != nil {
		return 0, err
	}
	// The catalog and the task must name the same file. Comparing them here
	// means a store that signed another checksum or another UI for this
	// version is refused before anything is fetched.
	if !hmac.Equal(pk.SHA256, p.SHA256) || !hmac.Equal(pk.UI, p.UI) {
		return 0, store.ErrCatalog
	}
	base, err := st.storeURL(ctx, p.Store)
	if err != nil {
		return 0, err
	}
	url, ok := PackageURL(base, pk.URL)
	if !ok {
		return 0, ErrStore
	}
	if err := st.Download(ctx, url, pk.Size, pk.SHA256, w); err != nil {
		return 0, err
	}
	return pk.Size, nil
}

// packageOf finds the entry of app at version in a verified catalog.
func packageOf(c *store.Catalog, app, version string) (store.Package, error) {
	for _, e := range c.Apps {
		if e.ID != app {
			continue
		}
		for _, pk := range e.Versions {
			if pk.Version == version {
				return pk, nil
			}
		}
	}
	return store.Package{}, dbapi.ErrNotFound
}

// storeURL is the address of one store of the trust list, checked again with
// the grammar of trust: being in a signed list says nothing about the address
// itself.
func (st *Stores) storeURL(ctx context.Context, id string) (string, error) {
	v, err := st.S.Trust(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range v.List.Stores {
		if s.ID == id {
			if !trust.ValidStoreURL(s.URL) {
				return "", ErrStore
			}
			return s.URL, nil
		}
	}
	return "", ErrStore
}

// manifestOf reads manifest.json out of a package, byte for byte as the
// package carries it, so dbapi.App.Manifest holds the document and not a
// re-encoding of it -- no exported function of the contract hands those bytes
// out. Nothing is judged here: the archive and this document are store.Walk's
// to judge (store.PackageUI runs it over the same file in fetchPackage), and
// the bounds below only keep this read from being the one that costs
// something.
func manifestOf(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	h, err := tr.Next()
	if err != nil || h.Typeflag != tar.TypeReg || h.Name != store.ManifestFile {
		return nil, store.ErrPackage
	}
	if h.Size <= 0 || h.Size > store.MaxManifest {
		return nil, store.ErrPackage
	}
	buf := make([]byte, h.Size)
	if _, err := io.ReadFull(tr, buf); err != nil {
		return nil, store.ErrPackage
	}
	return buf, nil
}
