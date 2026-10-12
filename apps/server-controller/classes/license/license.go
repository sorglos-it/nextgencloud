// Package license is what a controller knows about the licences of its paid
// apps: which one it keeps, and whether an app may run at all.
//
// Nothing here checks a licence of its own making. Both calls go through
// store.VerifyLicenseFor, the one entrance a controller has, and the one the
// activation uses as well (install.Manager.licence) - so the same licence can
// never be judged two ways on the same server. What that entrance does NOT do
// is check that the content key is wrapped for this installation's key: a
// controller does not hold that key, and whoever held it could unwrap the app
// itself. That half is the API's (docs/vertraege-v1.2.md, "Die Lizenz war nie
// eine Vertragsfrage - sie ist eine nicht aufgerufene Funktion").
//
// The heading and not the number of that point: a number moves when a document
// grows, and a pointer that moved is worse than none.
//
// Why both calls take the trust list, although the plan of this task gives
// Allowed none: a licence is kept as the signed envelope it arrived in
// (state.Store.License), and shared/ exports no way to read a License out of an
// envelope without the key of its store - parseLicense is private, on purpose.
// Measured, not assumed: a probe that calls store.parseLicense does not
// compile. The alternatives were to decode the payload here, which is a second
// licence check in the controller and exactly what the sentence above forbids,
// or to ask the store key for it. The list is where a store key may come from,
// so it is a parameter.
//
// The trust list is the local one (chain.Keeper.List), which this controller
// verified itself and keeps in its own state - so neither call needs an API.
package license

import (
	"context"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrVerify says the licence that came in is not one this installation can
// use: another store, another installation, another app, or a signature that
// does not check out. A caller maps it to wire.CodeVerify (E_VERIFY). It is
// joined with the error of the contract, which names which of them it was; that
// error goes into the log of this server and never to a peer.
var ErrVerify = errors.New("license: this licence is not for this installation")

// Keep checks the licence env of app and keeps it if it is newer than the one
// this controller already holds, and answers with the state of the licence that
// is KEPT afterwards - not of the one that came in. That is the whole point of
// the answer: an admin who sends an old licence must not be told "valid"
// because the old one says so, and one who sends a newer, already expired one
// must be told "expired".
//
// Only a bigger Issued replaces what is there. Equal does not: two licences of
// the same second are indistinguishable by age, and replacing on equal would
// make the answer depend on the order they arrive in.
//
// installation is the one the task was signed for, never one out of the
// licence: otherwise a licence of another NGC would serve every installation
// that gets hold of it. storeID is the store of the app as this controller's
// own row has it (dbapi.App.Store), and it has to be one of the stores of the
// trust list l.
func Keep(ctx context.Context, st *state.Store, l *trust.List, installation, app, storeID string,
	env sign.Envelope, now int64) (core.LicenseResult, error) {
	if st == nil || l == nil {
		return core.LicenseResult{}, ErrVerify
	}
	i := slices.IndexFunc(l.Stores, func(s trust.Store) bool { return s.ID == storeID })
	if i < 0 {
		return core.LicenseResult{}, ErrVerify
	}
	fresh, err := store.VerifyLicenseFor(env, l.Stores[i], installation, app)
	if err != nil {
		return core.LicenseResult{}, errors.Join(ErrVerify, err)
	}
	// What is kept now, before anything is written: a licence that is there
	// and at least as new as the fresh one stays, and then the answer is its
	// state and not the fresh one's.
	old, issued, err := st.License(ctx, app)
	switch {
	case err == nil && issued >= fresh.Issued:
		kept, err := store.VerifyLicenseFor(old, l.Stores[i], installation, app)
		if err != nil {
			// The licence in this controller's own state does not check out
			// any more: the store key of the list changed, or the state is
			// damaged. Neither is the fresh licence's fault, and it is not a
			// peer doing - so the fresh one is not quietly put in its place,
			// because that would let a changed key of the list overwrite a
			// licence with an older one.
			return core.LicenseResult{}, errors.Join(ErrVerify, err)
		}
		return core.LicenseResult{App: app, State: core.LicenseOf(kept, now)}, nil
	case err != nil && !errors.Is(err, dbapi.ErrNotFound):
		return core.LicenseResult{}, err
	}
	if err := st.PutLicense(ctx, app, env, fresh.Issued); err != nil {
		return core.LicenseResult{}, err
	}
	return core.LicenseResult{App: app, State: core.LicenseOf(fresh, now)}, nil
}

// Allowed says whether the app a may run at now. An app with no licence in the
// state is a free app and may: whether an app is paid is the catalogue's to
// say, and a sealed package without a key never gets through install.Accept at
// all. So this is about the licence that is there, never about the one that is
// missing.
//
// A licence that is there and says LicenseExpired - run out past its grace
// period - is a no. The caller then stops the services of the app, starts none,
// refuses its channels with E_LICENSE and reports tunnel.StateUnlicensed.
// LicenseGrace is a yes: it runs and NGC warns.
//
// Two cases answer no without an error, and both are the safer side of a
// question that cannot be answered: there is no trust list (l nil), or the
// store of the app is not in it. A paid app whose licence nobody can judge must
// not run; a free app is not touched by either, because its answer is given
// before the list is ever looked at.
//
// An error means the licence could not be read at all. The caller then knows
// nothing about this app - node.Restore starts it not, and
// node.Reporter.appState leaves it out of the status instead of bending one of
// the four words of the contract over it.
func Allowed(ctx context.Context, st *state.Store, l *trust.List, installation string,
	a dbapi.App, now int64) (bool, error) {
	if st == nil {
		return false, ErrVerify
	}
	env, _, err := st.License(ctx, a.ID)
	if errors.Is(err, dbapi.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if l == nil {
		return false, nil
	}
	i := slices.IndexFunc(l.Stores, func(s trust.Store) bool { return s.ID == a.Store })
	if i < 0 {
		return false, nil
	}
	lic, err := store.VerifyLicenseFor(env, l.Stores[i], installation, a.ID)
	if err != nil {
		return false, errors.Join(ErrVerify, err)
	}
	return lic.State(now) != store.LicenseExpired, nil
}
