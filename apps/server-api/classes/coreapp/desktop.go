package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// proofKindPassword is the kind of key that makes the shell show the hint to
// switch to passkeys (core.Me.PasswordLogin).
const proofKindPassword = proof.KindPassword

// How long a TOTP secret that was handed out waits for its confirming code,
// and how many may wait at once. The secret is kept on the server between
// totp-begin and totp-confirm, and login.BeginTOTP says why it may not travel
// instead: a sealed secret is bound to nothing, so whoever held one could set
// it as anybody's second factor.
//
// Named limit of this, and it is measured in nothing but the code: the pending
// secret lies in the memory of ONE API process. An installation whose shell
// reaches another API between begin and confirm answers E_NOTFOUND there, and
// the user starts again. A row to keep it in would be a new key of dbapi,
// which is frozen (shared/), so this is where it stays until the contract
// carries one.
const (
	totpPendingSeconds = 600
	maxTOTPPending     = 64
)

// pendingTOTP is a secret waiting for its code.
type pendingTOTP struct {
	sealed  []byte
	expires int64
}

// totpStore holds the secrets of totp-begin until totp-confirm.
type totpStore struct {
	mu sync.Mutex
	by map[string]pendingTOTP
}

// put keeps a secret for a user, dropping what expired. Beyond
// maxTOTPPending nothing is kept: a caller that could fill this map would
// otherwise grow the memory of the API with one channel after another.
func (p *totpStore) put(user string, sealed []byte, now int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.by == nil {
		p.by = map[string]pendingTOTP{}
	}
	for id, v := range p.by {
		if v.expires <= now {
			delete(p.by, id)
		}
	}
	if _, ok := p.by[user]; !ok && len(p.by) >= maxTOTPPending {
		return false
	}
	p.by[user] = pendingTOTP{sealed: sealed, expires: now + totpPendingSeconds}
	return true
}

// take returns the secret of a user and forgets it, so a code confirms one
// secret once.
func (p *totpStore) take(user string, now int64) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.by[user]
	delete(p.by, user)
	if !ok || v.expires <= now {
		return nil, false
	}
	return v.sealed, true
}

// registerDesktop adds the actions of ngc.desktop.
func (d *Dispatcher) registerDesktop() {
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActMe, d.me)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActApps, d.apps)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActUI, d.ui)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActUIProof, d.uiProof)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActTOTPBegin, d.totpBegin)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActTOTPConfirm, d.totpConfirm)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActTOTPOff, d.totpOff)
}

// me answers ngc.desktop/me.
func (d *Dispatcher) me(ctx context.Context, rq *Call) ([]byte, error) {
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	id := rq.Live.Verified.Cert().User
	u, ok := v.List.User(id)
	if !ok {
		return nil, fail(wire.CodeNotFound)
	}
	row, err := rq.S.DB.UserGet(ctx, id)
	if err != nil && !errors.Is(err, dbapi.ErrNotFound) {
		return nil, fail(wire.CodeUnreachable)
	}
	// The strictest level of the user's roles counts; a role the list does not
	// hold cannot happen (trust.List.Validate).
	level := trust.LevelOff
	for _, name := range u.Roles {
		if r, ok := v.List.Role(name); ok && stricter(r.Level, level) {
			level = r.Level
		}
	}
	var hint bool
	for _, k := range u.Keys {
		hint = hint || k.Kind == proofKindPassword
	}
	return result(core.Me{ID: u.ID, Name: u.Name, Roles: u.Roles,
		Admin: slices.Contains(u.Roles, trust.RoleAdmin), PasswordLogin: hint,
		TOTP: len(row.TOTP) > 0, Level: level})
}

// stricter reports whether a is a stricter level than b.
func stricter(a, b trust.Level) bool { return rank(a) > rank(b) }

func rank(l trust.Level) int {
	switch l {
	case trust.LevelCritical:
		return 1
	case trust.LevelRoot:
		return 2
	case trust.LevelHardcore:
		return 3
	}
	return 0
}

// apps answers ngc.desktop/apps: the start menu of this user. An app the user
// may not use (core.RightUse) does not appear at all.
func (d *Dispatcher) apps(ctx context.Context, rq *Call) ([]byte, error) {
	list, err := rq.S.DB.AppList(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.DesktopApp, 0, len(list)+2)
	// The two core apps are always there and only for admins; their UI is part
	// of the shell, so they carry no entry and no node.
	if rq.Admin {
		out = append(out,
			core.DesktopApp{ID: core.AppStore, Name: "App-Store", State: core.AppOK, Core: true},
			core.DesktopApp{ID: core.AppUsers, Name: "Benutzer & Rechte", State: core.AppOK, Core: true})
	}
	for _, a := range list {
		if !a.Active || a.Version == "" || !rq.Rights[a.ID+"/"+core.RightUse] {
			continue
		}
		m, err := store.ParseManifest(a.Manifest)
		if err != nil {
			// A manifest the database cannot parse means the row is broken;
			// the app is left out instead of breaking the whole menu.
			continue
		}
		entry := core.DesktopApp{ID: a.ID, Name: m.Name, Version: a.Version, Icon: m.Icon,
			State: core.AppOK, Node: a.Node}
		if m.UI != nil {
			entry.Entry, entry.Width, entry.Height = m.UI.Entry, m.UI.Width, m.UI.Height
		}
		for _, act := range m.Actions {
			if rq.Rights[a.ID+"/"+act.Name] {
				entry.Actions = append(entry.Actions, act.Name)
			}
		}
		if _, online := d.Tunnels.Get(a.Node); !online {
			entry.State = core.AppUnreachable
		} else if st, ok := d.licenseState(ctx, a); ok && st == core.LicenseExpired {
			entry.State = core.AppUnlicensed
		}
		out = append(out, entry)
	}
	return result(out)
}

// licenseState returns the state of the app's licence, and false when there is
// none at all (a free app). A stored licence that no longer verifies counts as
// expired and never as valid: the row is the API's own, and one it cannot
// verify any more must not unlock anything.
func (d *Dispatcher) licenseState(ctx context.Context, a dbapi.App) (string, bool) {
	b, err := d.S.DB.BlobGet(ctx, dbapi.BlobLicense, a.ID)
	if err != nil {
		return "", false
	}
	v, err := d.S.Trust(ctx)
	if err != nil {
		return core.LicenseExpired, true
	}
	i := slices.IndexFunc(v.List.Stores, func(s trust.Store) bool { return s.ID == a.Store })
	if i < 0 {
		return core.LicenseExpired, true
	}
	installation, err := d.S.Installation(ctx)
	if err != nil {
		return core.LicenseExpired, true
	}
	key, err := d.S.InstallKey(ctx)
	if err != nil {
		return core.LicenseExpired, true
	}
	var env sign.Envelope
	if json.Unmarshal(b.Data, &env) != nil {
		return core.LicenseExpired, true
	}
	lic, err := store.VerifyLicense(env, v.List.Stores[i], installation, a.ID, key.PublicKey().Bytes())
	if err != nil {
		return core.LicenseExpired, true
	}
	return core.LicenseOf(lic, d.S.Now()), true
}

// ui answers ngc.desktop/ui: the ui-tar of the installed version, byte for
// byte as the API kept it (dbapi.BlobUI). The shell reads it like
// store.WalkUI and runs it only after the proof of ui-proof holds.
func (d *Dispatcher) ui(ctx context.Context, rq *Call) ([]byte, error) {
	a, code := d.appOf(ctx, rq)
	if code != "" {
		return nil, fail(code)
	}
	b, err := rq.S.DB.BlobGet(ctx, dbapi.BlobUI, a.ID+"@"+a.Version)
	if err != nil {
		return nil, fail(wire.CodeNotFound)
	}
	// Written out as they are: the shell compares their SHA-256 against the
	// proof, so the API cannot change a byte unseen.
	if _, err := rq.Out.Write(b.Data); err != nil {
		return nil, fail(wire.CodeInternal)
	}
	return nil, nil
}

// uiProof answers ngc.desktop/ui-proof with the evidence the API kept when the
// version was installed (dbapi.BlobApproval).
//
// The row is not just read back: appstore.Stores.Proof parses it, verifies it
// against the trust list AS IT READS TODAY, and -- where the ui-tar of that
// version is here too -- holds the hash the proof vouches for against the
// bytes ngc.desktop/ui would send. Both checks are the point of this seam: a
// key or a store an admin has withdrawn since the install would otherwise go
// on vouching for a UI out of a database, and evidence that does not fit the
// ui-tar next door is a mismatch the shell can only refuse. The shell checks
// the proof a third time against its own copy of the list, so this layer is
// the middle one of three and not the last.
//
// The Stores value is made here from the state rather than taken from
// Deps.Store, and the reason is read off Proof: it touches nothing but st.S
// (the two blob rows and state.Trust), so none of the fields server.Build
// fills in -- the HTTP client, the sender to a controller, the spool
// directory, the log -- can change its answer. Deps.Store may be nil (see
// Deps), while ui-proof is registered either way, so a dependency on it would
// turn this action off wherever the store actions are off.
func (d *Dispatcher) uiProof(ctx context.Context, rq *Call) ([]byte, error) {
	a, code := d.appOf(ctx, rq)
	if code != "" {
		return nil, fail(code)
	}
	p, err := (&appstore.Stores{S: rq.S}).Proof(ctx, a.ID, a.Version)
	// A version with no evidence kept answers like one that is not installed;
	// everything else is this installation's own problem, including a proof
	// that no longer holds. E_NOTFOUND for the latter would tell a shell that
	// the version is gone when it is the trust in it that is gone.
	if errors.Is(err, dbapi.ErrNotFound) {
		return nil, fail(wire.CodeNotFound)
	}
	if err != nil {
		return nil, fail(wire.CodeInternal)
	}
	return result(p)
}

// appOf resolves the app of an action that names one. The app ID is a
// parameter and therefore unsigned, so nothing but the row decides: an app the
// user may not use answers like one that is not installed, which tells a user
// without the right nothing about the installation.
func (d *Dispatcher) appOf(ctx context.Context, rq *Call) (dbapi.App, wire.Code) {
	p, ok := rq.Params.(*core.AppParams)
	if !ok {
		return dbapi.App{}, wire.CodeProtocol
	}
	a, err := rq.S.DB.AppGet(ctx, p.App)
	if err != nil || a.Version == "" || !rq.Rights[a.ID+"/"+core.RightUse] {
		return dbapi.App{}, wire.CodeNotFound
	}
	return a, ""
}

// totpBegin answers ngc.desktop/totp-begin: a new secret the user reads or
// scans, kept on the server until a code confirms it. Nothing of the database
// changes here, so a user who walks away keeps the second factor they had.
func (d *Dispatcher) totpBegin(ctx context.Context, rq *Call) ([]byte, error) {
	id := rq.Live.Verified.Cert().User
	row, err := rq.S.DB.UserGet(ctx, id)
	if err != nil {
		return nil, fail(wire.CodeNotFound)
	}
	begin, sealed, err := d.Login.BeginTOTP(row.Name)
	if err != nil {
		return nil, fail(wire.CodeInternal)
	}
	if !d.totp.put(id, sealed, rq.S.Now()) {
		return nil, fail(wire.CodeLimit)
	}
	// The secret travels once, here, because the user has to type or scan it.
	// It is never logged, and the log of this channel holds app and action
	// only (audit.Log.Channel).
	return result(begin)
}

// totpConfirm answers ngc.desktop/totp-confirm: the code of the secret that
// totp-begin handed out switches TOTP on. A wrong code changes nothing, and
// the secret is spent either way -- a code that may be tried again and again
// against one secret is a second factor of six digits.
func (d *Dispatcher) totpConfirm(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.CodeParams)
	if !ok {
		return nil, fail(wire.CodeProtocol)
	}
	id := rq.Live.Verified.Cert().User
	row, err := rq.S.DB.UserGet(ctx, id)
	if err != nil {
		return nil, fail(wire.CodeNotFound)
	}
	sealed, ok := d.totp.take(id, rq.S.Now())
	if !ok {
		return nil, fail(wire.CodeNotFound)
	}
	if !d.Login.CheckTOTP(dbapi.User{TOTP: sealed}, p.Code, rq.S.Now()) {
		return nil, fail(wire.CodeForbidden)
	}
	row.TOTP = sealed
	if err := rq.S.DB.UserPut(ctx, row); err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	return result(struct{}{})
}

// totpOff answers ngc.desktop/totp-off: a valid code of the second factor
// switches it off. Without the code nothing changes, so a session someone else
// got hold of cannot take the factor away.
func (d *Dispatcher) totpOff(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.CodeParams)
	if !ok {
		return nil, fail(wire.CodeProtocol)
	}
	id := rq.Live.Verified.Cert().User
	row, err := rq.S.DB.UserGet(ctx, id)
	if err != nil {
		return nil, fail(wire.CodeNotFound)
	}
	if !d.Login.CheckTOTP(row, p.Code, rq.S.Now()) {
		return nil, fail(wire.CodeForbidden)
	}
	row.TOTP = nil
	if err := rq.S.DB.UserPut(ctx, row); err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	return result(struct{}{})
}

// result encodes what a handler answers. core.EncodeResult is the one way out:
// it refuses a null and anything over wire.MaxCallResult, so a result the
// shell would not take never leaves the API.
func result(v any) ([]byte, error) {
	b, err := core.EncodeResult(v)
	if err != nil {
		return nil, fail(wire.CodeInternal)
	}
	return b, nil
}
