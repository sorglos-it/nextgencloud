package core

import (
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// UIProof answers ngc.desktop/ui-proof: signed evidence of the UI hash of the
// installed version of an app, which the shell checks the ui-tar of
// ngc.desktop/ui against before it runs any of it. A store app brings the
// catalog of its store that lists the version (store-signed; payload at most
// sign.MaxPayload); an uploaded app brings the activate task that installed it
// with the client certificate and the proof that make it (admin-signed). The
// API keeps it per version (dbapi.BlobApproval); the whole answer is at most
// wire.MaxCallResult bytes.
//
// Which store vouches: any store in the current trust list whose verified
// catalog lists that app and version with that UI hash. Nothing signed tells
// the shell which store an app was installed from, so a compromised API
// together with a hostile store the admin added could give an app of another
// store a UI that store did not sign. An admin should therefore add only
// stores they trust as much as the software they install from them.
//
// Rollback: the shell keeps, per installation and app, the newest version
// whose UI it has verified, and refuses a UI proof for an older version; with
// a newer version it updates its record. The installed version comes from
// the API, so a browser that never saw the newer version can still be shown
// the older version's signed UI.
type UIProof struct {
	App     string         `json:"app"`
	Version string         `json:"version"`
	Store   string         `json:"store"`             // a trust.Store ID, or LocalStore
	Catalog *sign.Envelope `json:"catalog,omitempty"` // store apps
	Task    *sign.Envelope `json:"task,omitempty"`    // uploaded apps: the activate task
	Client  enc.Bytes      `json:"client,omitempty"`  // its client certificate payload
	Proof   *proof.Proof   `json:"proof,omitempty"`   // the user's proof over Client
}

// ErrUIProof reports a UI proof that is malformed or does not hold.
var ErrUIProof = errors.New("core: UI proof does not hold")

// uiProofBudget bounds a UI proof while it is decoded (enc.Budget): it has no
// array, and its envelopes and certificate hold bytes.
var uiProofBudget = enc.Budget{Size: wire.MaxCallResult}

// ParseUIProof decodes a UI proof from outside: strict JSON within
// wire.MaxCallResult bytes (no array); App a valid non-core app ID, Version
// store.ValidVersion, Store wire.ValidName; either Catalog alone
// (Validate(sign.MaxPayload)) with Store not LocalStore, or Task
// (Validate(task.MaxTask)), Client (1..session.MaxCert) and Proof
// (proof.Proof.Validate) with Store LocalStore. Every failure is ErrUIProof.
func ParseUIProof(b []byte) (UIProof, error) {
	var p UIProof
	if uiProofBudget.Decode(b, &p) != nil || !validApp(p.App) || !store.ValidVersion(p.Version) || !wire.ValidName(p.Store) {
		return UIProof{}, ErrUIProof
	}
	storeApp := p.Catalog != nil && p.Task == nil && len(p.Client) == 0 && p.Proof == nil &&
		p.Store != LocalStore && p.Catalog.Validate(sign.MaxPayload) == nil
	uploaded := p.Catalog == nil && p.Task != nil && len(p.Client) > 0 && len(p.Client) <= session.MaxCert && p.Proof != nil &&
		p.Store == LocalStore && p.Task.Validate(task.MaxTask) == nil && p.Proof.Validate() == nil
	if !storeApp && !uploaded {
		return UIProof{}, ErrUIProof
	}
	return p, nil
}

// VerifyUIProof checks p for app at version against the trust list l and
// returns the UI hash. Store app: the store p.Store of l signed p.Catalog
// (store.VerifyCatalog, lastSeq 0) and it lists app at version, whose ui is
// the hash; any store of l may vouch so, whichever store the app came from
// (UIProof, "Which store vouches"). Uploaded app:
// session.VerifyApproval(p.Client, *p.Proof, l) gives the certificate,
// task.VerifyApproval gives the task, which is ngc.store/controller/activate
// on a node where that user may run root tasks (l.MayRoot), with params
// (ParseParams) naming app, version and LocalStore; their ui is the hash. No
// time is checked: the approval holds while its key and root permission stay
// in l. Nor is the order of versions: refusing a proof for a version older
// than the newest one it verified is the shell's rule (UIProof, "Rollback").
// Every failure is ErrUIProof.
func VerifyUIProof(p UIProof, app, version string, l *trust.List) ([]byte, error) {
	ui, err := verifyUIProof(p, app, version, l)
	if err != nil {
		return nil, ErrUIProof
	}
	return ui, nil
}

// Reasons verifyUIProof refuses a proof, besides the errors of the packages
// it calls. VerifyUIProof answers each with ErrUIProof alone; the tests tell
// them apart.
var (
	errProofForm   = errors.New("core: UI proof of another form, app or version")
	errProofStore  = errors.New("core: store of the UI proof is not in the trust list")
	errProofListed = errors.New("core: catalog does not list the app at that version")
	errProofTask   = errors.New("core: task is no activate of the uploaded app at that version")
	errProofRoot   = errors.New("core: approver may not run root tasks on the task's node")
)

// verifyUIProof is VerifyUIProof with the reason of a refusal. It checks the
// form again, so a proof that did not come through ParseUIProof cannot mix
// the two kinds.
func verifyUIProof(p UIProof, app, version string, l *trust.List) ([]byte, error) {
	if l == nil || p.App != app || p.Version != version {
		return nil, errProofForm
	}
	if p.Catalog != nil {
		if p.Task != nil || len(p.Client) != 0 || p.Proof != nil {
			return nil, errProofForm
		}
		return catalogUI(*p.Catalog, p.Store, app, version, l)
	}
	if p.Task == nil || p.Proof == nil || p.Store != LocalStore {
		return nil, errProofForm
	}
	cert, err := session.VerifyApproval(p.Client, *p.Proof, l)
	if err != nil {
		return nil, err
	}
	t, err := task.VerifyApproval(*p.Task, cert, p.Client)
	if err != nil {
		return nil, err
	}
	if t.App != AppStore || t.Service != ServiceController || t.Action != ActActivate {
		return nil, errProofTask
	}
	if !l.MayRoot(cert.User, t.Node) {
		return nil, errProofRoot
	}
	activate, _ := Lookup(AppStore, ServiceController, ActActivate)
	params, err := ParseParams(activate, t.Params)
	if err != nil {
		return nil, err
	}
	a := params.(*ActivateParams)
	if a.App != app || a.Version != version || a.Store != LocalStore {
		return nil, errProofTask
	}
	return a.UI, nil
}

// catalogUI returns the ui of app at version from env, a catalog that the
// store id of l signed.
func catalogUI(env sign.Envelope, id, app, version string, l *trust.List) ([]byte, error) {
	var st *trust.Store
	for i := range l.Stores {
		if l.Stores[i].ID == id {
			st = &l.Stores[i]
		}
	}
	if st == nil {
		return nil, errProofStore
	}
	c, err := store.VerifyCatalog(env, *st, 0)
	if err != nil {
		return nil, err
	}
	for _, e := range c.Apps {
		if e.ID != app {
			continue
		}
		for _, v := range e.Versions {
			if v.Version == version {
				return v.UI, nil
			}
		}
	}
	return nil, errProofListed
}
