package appstore

// Licences and content keys of paid apps.
//
// Two secrets live in this file, and neither may leave it other than on the
// one way it is meant to go: the installation's private X25519 key, which
// never leaves the API at all, and the content key of a paid app, which goes
// to one of our own controllers inside the tunnel and nowhere else.
//
// So nothing here writes a secret, a licence or any part of one into a log
// entry or into an error. The log entries this file writes are the two of
// licenseNotes, both fixed texts; every other function writes no entry at all,
// which is measured in TestOnlyTakeLicenseWritesToTheLog. The errors this file
// makes itself are ErrLicense alone, which names no value; the errors it hands
// on come from the database, from state and from store.UnwrapKey
// (store.ErrEncrypted), and none of those carries bytes of a key either
// (measured for the refusal path in TestNoKeyAndNoLicenseReachesTheLog).
// Nothing here builds an address at all.

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrLicense reports a licence NGC does not take, and an app it holds no
// licence for. It is one error for both on purpose: whoever hands in a licence
// learns that it was refused and not which of the checks refused it, and
// whoever asks for a content key learns nothing about the licences of apps
// they did not buy.
var ErrLicense = errors.New("appstore: licence refused")

// The details this file writes into the log. They are fixed texts and carry
// nothing of the licence beyond the app, which the entry names in its own
// field anyway: a licence holds the installation's key and a wrapped content
// key, and a log is read by people and kept for a long time.
const (
	NoteTaken = "Lizenz übernommen"
	NoteKept  = "Lizenz nicht übernommen, die vorhandene ist neuer"
)

// licenseNotes is the whole list, so a test can show that no other text ever
// reaches the log from here (TestNoKeyAndNoLicenseReachesTheLog).
var licenseNotes = []string{NoteTaken, NoteKept}

// licenseNote reports whether s is one of licenseNotes.
func licenseNote(s string) bool { return slices.Contains(licenseNotes, s) }

// TakeLicense checks a licence an admin uploaded (ngc.store/api/license) and
// keeps it if it is newer than the one held. The check is verify, which runs
// store.VerifyLicense -- the one a controller cannot run, because it needs the
// installation's key. How many users a per-user licence allows is the other
// check only the API can make, and that one is UsersAllowed: it is not part of
// taking a licence, because the users may change afterwards.
func (st *Stores) TakeLicense(ctx context.Context, app string, body []byte) (core.LicenseResult, error) {
	// The app comes out of a request, so it is checked before it reaches a
	// query -- the same wall Catalog puts in front of a store ID.
	if !wire.ValidAppID(app) {
		return core.LicenseResult{}, ErrLicense
	}
	env, err := sign.ParseEnvelope(body, store.MaxLicense)
	if err != nil {
		return core.LicenseResult{}, ErrLicense
	}
	l, err := st.verify(ctx, app, env)
	if err != nil {
		return core.LicenseResult{}, err
	}
	// Only a newer licence replaces the one held, so an old one cannot bring
	// back a shorter term or fewer users. A licence of the same issue time is
	// not newer either and changes nothing.
	if cur, _, ok, err := st.License(ctx, app); err != nil {
		return core.LicenseResult{}, err
	} else if ok && cur.Issued >= l.Issued {
		if err := st.noteLicense(ctx, app, NoteKept); err != nil {
			return core.LicenseResult{}, err
		}
		return core.LicenseResult{App: app, State: core.LicenseOf(cur, st.S.Now())}, nil
	}
	// What is kept is the envelope the store signed, not the parsed licence:
	// a controller checks the signature itself (store.VerifyLicenseFor), so it
	// never has to believe this API.
	data, err := json.Marshal(env)
	if err != nil {
		return core.LicenseResult{}, err
	}
	if err := st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense, ID: app, Seq: uint64(l.Issued),
		Data: data, Updated: st.S.Now()}); err != nil {
		return core.LicenseResult{}, err
	}
	if err := st.noteLicense(ctx, app, NoteTaken); err != nil {
		return core.LicenseResult{}, err
	}
	return core.LicenseResult{App: app, State: core.LicenseOf(l, st.S.Now())}, nil
}

// verify is the one place a licence envelope is judged, for a licence handed
// in and for one read back alike: the store of the app's own row out of the
// trust list, this installation, this app and this installation's key, all of
// it through store.VerifyLicense. Nothing in this package judges a licence
// anywhere else.
func (st *Stores) verify(ctx context.Context, app string, env sign.Envelope) (*store.License, error) {
	row, err := st.S.DB.AppGet(ctx, app)
	if err != nil {
		// An app that is not installed takes no licence, and the answer is
		// the same as for a licence that does not verify.
		if errors.Is(err, dbapi.ErrNotFound) {
			return nil, ErrLicense
		}
		return nil, err
	}
	v, err := st.S.Trust(ctx)
	if err != nil {
		return nil, err
	}
	var source *trust.Store
	for i := range v.List.Stores {
		if v.List.Stores[i].ID == row.Store {
			source = &v.List.Stores[i]
			break
		}
	}
	// A licence is only ever the one of the store the app came from: without
	// that, a store of one's own in the list could license an app of somebody
	// else's.
	if source == nil {
		return nil, ErrLicense
	}
	installation, err := st.S.Installation(ctx)
	if err != nil {
		return nil, err
	}
	key, err := st.S.InstallKey(ctx)
	if err != nil {
		return nil, err
	}
	l, err := store.VerifyLicense(env, *source, installation, app, key.PublicKey().Bytes())
	if err != nil {
		return nil, ErrLicense
	}
	return l, nil
}

// License reads the licence of app back and checks it again -- against the key
// the trust list names today, so a store key an admin has since withdrawn
// licenses nothing further. ok is false where no licence is on record; that is
// not an error, because a free app has none.
func (st *Stores) License(ctx context.Context, app string) (*store.License, sign.Envelope, bool, error) {
	if !wire.ValidAppID(app) {
		return nil, sign.Envelope{}, false, ErrLicense
	}
	b, err := st.S.DB.BlobGet(ctx, dbapi.BlobLicense, app)
	if errors.Is(err, dbapi.ErrNotFound) {
		return nil, sign.Envelope{}, false, nil
	}
	if err != nil {
		return nil, sign.Envelope{}, false, err
	}
	env, err := sign.ParseEnvelope(b.Data, store.MaxLicense)
	if err != nil {
		// A row that does not read is not "no licence": nothing that hangs off
		// a licence may run on a row this API cannot judge.
		return nil, sign.Envelope{}, false, ErrLicense
	}
	l, err := st.verify(ctx, app, env)
	if err != nil {
		return nil, sign.Envelope{}, false, err
	}
	return l, env, true, nil
}

// ContentKey unwraps the content key of a paid app. Only the API holds the
// installation's private key, and it gives the key to its own controllers
// alone, over the tunnel.
func (st *Stores) ContentKey(ctx context.Context, app string) ([]byte, error) {
	l, _, ok, err := st.License(ctx, app)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLicense
	}
	if l.State(st.S.Now()) == store.LicenseExpired {
		// Past the grace period the key is not handed out any more, so the
		// controllers cannot start the services again.
		return nil, ErrLicense
	}
	key, err := st.S.InstallKey(ctx)
	if err != nil {
		return nil, err
	}
	// store.UnwrapKey gives ErrEncrypted for a key that was wrapped for
	// another installation; it names no bytes, so it is handed on as it is.
	return store.UnwrapKey(l.Key, key, app)
}

// UsersAllowed answers how many users hold the right "<app>/use" and whether
// the licence of app allows that many: a per-user licence allows License.Users
// of them, a per-site licence any number. Two cases allow nobody: a licence
// past its grace period, which answers false without an error, and an app with
// no licence on record at all, which answers false and ErrLicense. A caller
// that reads neither the error nor the flag therefore still errs on the closed
// side -- the count alone never reads as permission.
func (st *Stores) UsersAllowed(ctx context.Context, app string) (int, bool, error) {
	l, _, ok, err := st.License(ctx, app)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, ErrLicense
	}
	v, err := st.S.Trust(ctx)
	if err != nil {
		return 0, false, err
	}
	all, err := rights.All(ctx, st.S.DB, v.List)
	if err != nil {
		return 0, false, err
	}
	// The right of an app belongs to a role, and a user holds it through a
	// role; being an admin grants no app right (rights.Check), so the roles
	// that hold it are the whole answer.
	use := app + "/" + core.RightUse
	holders := map[string]bool{}
	for _, r := range all {
		if slices.Contains(r.Rights, use) {
			holders[r.Role] = true
		}
	}
	n := 0
	for _, u := range v.List.Users {
		for _, role := range u.Roles {
			if holders[role] {
				n++
				break
			}
		}
	}
	if l.State(st.S.Now()) == store.LicenseExpired {
		return n, false, nil
	}
	if l.Per != store.PerUser {
		return n, true, nil
	}
	return n, n <= l.Users, nil
}

// InstallKey answers ngc.store/install-key: what an admin gives the shop when
// buying. Only the public half ever leaves the API.
func (st *Stores) InstallKey(ctx context.Context) (core.InstallKey, error) {
	id, err := st.S.Installation(ctx)
	if err != nil {
		return core.InstallKey{}, err
	}
	key, err := st.S.InstallKey(ctx)
	if err != nil {
		return core.InstallKey{}, err
	}
	return core.InstallKey{Installation: id, Key: enc.Bytes(key.PublicKey().Bytes())}, nil
}

// PushLicenses hands send the newest licence of every paid app, for the
// controller the app runs on (ngc.store/controller/license).
//
// Who calls it is NOT built yet: the plan has it run after every new licence
// and after every tunnel came up (controllers.Server.OnHello), so that a
// controller which was away learns of a licence that ran out and stops the
// app's services. Until that caller exists, nothing in this repository calls
// PushLicenses outside its own test.
//
// Only the envelope travels, never the content key: a controller gets that one
// with the app itself (core.Transfer), and a licence envelope carries no
// secret of this installation -- the wrapped key in it opens with the
// installation's private key alone.
//
// A node nobody can reach does not stop the others: every failure is collected
// and reported at the end, because one controller that is away must not cost
// the rest their licence.
func (st *Stores) PushLicenses(ctx context.Context, send func(node, app string, env sign.Envelope) error) error {
	if send == nil {
		return ErrLicense
	}
	apps, err := st.S.DB.AppList(ctx)
	if err != nil {
		return err
	}
	var failed []error
	for _, a := range apps {
		_, env, ok, err := st.License(ctx, a.ID)
		if errors.Is(err, ErrLicense) {
			// A licence this API cannot judge is not sent on; the app stays
			// listed and the next refresh may mend it.
			failed = append(failed, err)
			continue
		}
		if err != nil {
			return err
		}
		if !ok {
			// A free app has no licence, and that is the normal case.
			continue
		}
		if err := send(a.Node, a.ID, env); err != nil {
			failed = append(failed, err)
		}
	}
	return errors.Join(failed...)
}

// noteLicense writes down what happened to the licence of one app. The detail
// is one of licenseNotes and never anything out of the licence; audit.Log
// checks every field again itself.
//
// A log that cannot be written fails the step, although the licence is already
// stored at that point: a licence nobody can see arriving is worse than one
// the admin has to upload again.
func (st *Stores) noteLicense(ctx context.Context, app, note string) error {
	if st.Log == nil {
		return nil
	}
	// audit.ErrDropped says a field was replaced and the entry was written: it
	// is a report, so it is not handed on as a failure of this step.
	if err := st.Log.Note(ctx, core.LogLicense, audit.UserSystem, app, "", note); err != nil &&
		!errors.Is(err, audit.ErrDropped) {
		return err
	}
	return nil
}
