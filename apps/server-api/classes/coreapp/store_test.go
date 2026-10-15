package coreapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// --- The seams of ngc.store, as a case can hold them -----------------------

// fakeStore answers like appstore without reaching a store, a controller or a
// disk, and writes down what it was handed. It is what lets a case measure the
// dispatcher's own share: the parameters it resolved, the signed task it
// passed on as it stands, and the node it took out of the task.
type fakeStore struct {
	calls []string
	// act, deact, unin are the requests the three controller actions got,
	// kept whole so a case can read the task and the node out of them.
	act   *appstore.ActivateRequest
	deact *appstore.DeactivateRequest
	unin  *appstore.UninstallRequest
	// uploaded is how many bytes the upload handler streamed through, licensed
	// the app name it read out of the licence envelope.
	uploaded int64
	licensed string
	// hold makes Upload collect the whole package instead of streaming it.
	// It is the null control of TestUploadHoldsNoPackageOnTheChannel: without
	// a run that really does hold the bytes, three flat numbers would only say
	// that the gauge sees nothing. held keeps them alive past the call, so the
	// collector cannot take them away before the peak is read.
	hold bool
	held []byte
	// catalogOf is the store ID the catalog handler asked for.
	catalogOf string
	// seats answers UsersAllowed per app: the number, whether the licence
	// allows it and the error. An app no case named has no licence, which is
	// what a free app looks like.
	seats map[string]seatAnswer
	// seatsAsked are the apps whose seat was asked, in order.
	seatsAsked []string
}

// seatAnswer is one answer of fakeStore.UsersAllowed.
type seatAnswer struct {
	n       int
	allowed bool
	err     error
}

// fakeStore fulfils the seam the dispatcher asks for.
var _ Store = (*fakeStore)(nil)

func (f *fakeStore) UsersAllowed(_ context.Context, app string) (int, bool, error) {
	f.note("users-allowed")
	f.seatsAsked = append(f.seatsAsked, app)
	if a, ok := f.seats[app]; ok {
		return a.n, a.allowed, a.err
	}
	return 0, false, appstore.ErrNoLicense
}

func (f *fakeStore) note(name string) { f.calls = append(f.calls, name) }

func (f *fakeStore) saw(name string) bool { return slices.Contains(f.calls, name) }

func (f *fakeStore) Sources(context.Context) ([]core.SourceInfo, error) {
	f.note(core.ActSources)
	return []core.SourceInfo{{ID: trust.StoreGlobal, Name: "Global", URL: "https://s.test"}}, nil
}

func (f *fakeStore) Catalog(_ context.Context, id string) (*store.Catalog, sign.Envelope, error) {
	f.note(core.ActCatalog)
	f.catalogOf = id
	return &store.Catalog{}, sign.Envelope{Payload: []byte(`{}`), KeyID: "k",
		Sig: make([]byte, 64)}, nil
}

func (f *fakeStore) Installed(context.Context) ([]core.InstalledApp, error) {
	f.note(core.ActInstalled)
	return []core.InstalledApp{}, nil
}

func (f *fakeStore) Upload(_ context.Context, r io.Reader) (core.UploadResult, error) {
	f.note(core.ActUpload)
	if f.hold {
		b, err := io.ReadAll(r)
		if err != nil {
			return core.UploadResult{}, err
		}
		f.held = b
		f.uploaded = int64(len(b))
		return core.UploadResult{ID: appX, Version: "1.0.0", SHA256: make([]byte, 32),
			UI: make([]byte, 32)}, nil
	}
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		return core.UploadResult{}, err
	}
	f.uploaded = n
	return core.UploadResult{ID: appX, Version: "1.0.0", SHA256: make([]byte, 32), UI: make([]byte, 32)}, nil
}

func (f *fakeStore) TakeLicense(_ context.Context, app string, _ []byte) (core.LicenseResult, error) {
	f.note(core.ActLicense)
	f.licensed = app
	return core.LicenseResult{App: app, State: "valid"}, nil
}

func (f *fakeStore) InstallKey(context.Context) (core.InstallKey, error) {
	f.note(core.ActInstallKey)
	return core.InstallKey{Installation: "i", Key: make([]byte, 32)}, nil
}

func (f *fakeStore) Activate(_ context.Context, rq appstore.ActivateRequest) (wire.Code, error) {
	f.note(core.ActActivate)
	f.act = &rq
	return "", nil
}

func (f *fakeStore) Deactivate(_ context.Context, rq appstore.DeactivateRequest) (wire.Code, error) {
	f.note(core.ActDeactivate)
	f.deact = &rq
	return "", nil
}

func (f *fakeStore) Uninstall(_ context.Context, rq appstore.UninstallRequest) (wire.Code, error) {
	f.note(core.ActUninstall)
	f.unin = &rq
	return "", nil
}

// fakeUpdates answers the two update actions and writes down the setting.
type fakeUpdates struct {
	calls []string
	set   *core.UpdateSetting
}

var _ Updates = (*fakeUpdates)(nil)

func (f *fakeUpdates) saw(name string) bool { return slices.Contains(f.calls, name) }

func (f *fakeUpdates) Rows(context.Context) ([]core.UpdateInfo, error) {
	f.calls = append(f.calls, core.ActUpdates)
	return []core.UpdateInfo{}, nil
}

func (f *fakeUpdates) SetSetting(_ context.Context, s core.UpdateSetting) error {
	f.calls = append(f.calls, core.ActSetUpdate)
	f.set = &s
	return nil
}

// storeHarness is the fixture with both seams of ngc.store set.
type storeHarness struct {
	*harness
	Store *fakeStore
	Up    *fakeUpdates
}

// newStoreFixture builds the API with the app store and the updates wired, so
// every action of ngc.store has a handler.
func newStoreFixture(t *testing.T) *storeHarness {
	t.Helper()
	sh := &storeHarness{Store: &fakeStore{}, Up: &fakeUpdates{}}
	sh.harness = newFixtureWith(t, func(_ *harness, d *Deps) {
		d.Store, d.Updates = sh.Store, sh.Up
	})
	return sh
}

// licenceBody is a licence envelope whose payload names app. It is signed with
// the admin's key, which is not the store's -- nothing of the signature is
// checked on the way to appstore.TakeLicense, and the handler's own comment
// says why that is safe: the name only picks the row, and store.VerifyLicense
// insists the signed licence names the same app.
func licenceBody(t *testing.T, h *harness, app string) []byte {
	t.Helper()
	env, err := sign.Sign(h.Admin.Priv, sign.PurposeLicense, []byte(`{"app":"`+app+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rootTask signs a root task of this session's admin for one action of
// ngc.store/controller on the controller the trust list lets admins root on.
func rootTask(t *testing.T, h *harness, fill byte, action string, params string) *sign.Envelope {
	t.Helper()
	return h.Task(t, task.Task{ID: taskID(fill), Node: ctlNode, App: core.AppStore,
		Service: core.ServiceController, Action: action, Params: json.RawMessage(params)})
}

// confirmOf is the user's re-confirmation of a signed task, which a critical
// action needs beside the task (task.Check, trust.LevelCritical).
func confirmOf(h *harness, env *sign.Envelope) *proof.Proof {
	pr := h.Admin.Prove(sign.PurposeConfirm, env.Payload)
	return &pr
}

// --- Reachability: one row per action of the contract ----------------------

// how an action is reached from a shell.
const (
	byHandler   = "handler"   // this API serves it
	byRelay     = "relay"     // the controller carries it out alone
	byForbidden = "forbidden" // the contract keeps the shell out (core.ShellOpens)
)

// storeCase is one action of ngc.store and how a shell opens it.
type storeCase struct {
	service string
	action  string
	want    string
	// drive opens the channel and reads its end. It returns the code the
	// channel closed with, "" for one that answered a result.
	drive func(t *testing.T, fx *storeHarness) wire.Code
}

// storeCases is the whole of ngc.store, one row per action. The two assertions
// of TestEveryStoreActionIsReachable hold it against core.All() in BOTH
// directions, so neither a row for an action that does not exist nor an action
// without a row can stay quiet.
func storeCases() []storeCase {
	call := func(service, action, params string) func(*testing.T, *storeHarness) wire.Code {
		return func(t *testing.T, fx *storeHarness) wire.Code {
			o := wire.Open{App: core.AppStore, Service: service, Action: action,
				Window: wire.MinWindow}
			if params != "" {
				o.Params = json.RawMessage(params)
			}
			return endOf(t, fx.Open(t, o))
		}
	}
	root := func(fill byte, action, params string, critical bool) func(*testing.T, *storeHarness) wire.Code {
		return func(t *testing.T, fx *storeHarness) wire.Code {
			fx.PutApp(t, appY, true)
			tk := rootTask(t, fx.harness, fill, action, params)
			o := wire.Open{App: core.AppStore, Service: core.ServiceController, Action: action,
				Window: wire.MinWindow, Task: tk}
			if critical {
				o.Confirm = confirmOf(fx.harness, tk)
			}
			return endOf(t, fx.Open(t, o))
		}
	}
	return []storeCase{
		{core.ServiceAPI, core.ActSources, byHandler, call(core.ServiceAPI, core.ActSources, "")},
		{core.ServiceAPI, core.ActCatalog, byHandler,
			call(core.ServiceAPI, core.ActCatalog, `{"store":"`+trust.StoreGlobal+`"}`)},
		{core.ServiceAPI, core.ActInstalled, byHandler, call(core.ServiceAPI, core.ActInstalled, "")},
		{core.ServiceAPI, core.ActUpload, byHandler, func(t *testing.T, fx *storeHarness) wire.Code {
			// Critical (core.Critical), and of service api: so it needs a
			// signed task with Node "" and the user's re-confirmation beside
			// it, and it is a FlowIn channel that sends bytes.
			tk := fx.Task(t, task.Task{ID: taskID(0xB1), App: core.AppStore,
				Service: core.ServiceAPI, Action: core.ActUpload})
			c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
				Action: core.ActUpload, Window: wire.MinWindow, Task: tk,
				Confirm: confirmOf(fx.harness, tk)})
			if c.awaitGrant(t) {
				c.sendBytes(t, 17)
			}
			return endOf(t, c)
		}},
		{core.ServiceAPI, core.ActLicense, byHandler, func(t *testing.T, fx *storeHarness) wire.Code {
			c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
				Action: core.ActLicense, Window: wire.MinWindow})
			if c.awaitGrant(t) {
				c.send(t, licenceBody(t, fx.harness, appY))
			}
			return endOf(t, c)
		}},
		{core.ServiceAPI, core.ActInstallKey, byHandler, call(core.ServiceAPI, core.ActInstallKey, "")},
		{core.ServiceAPI, core.ActUpdates, byHandler, call(core.ServiceAPI, core.ActUpdates, "")},
		{core.ServiceAPI, core.ActSetUpdate, byHandler,
			call(core.ServiceAPI, core.ActSetUpdate, `{"target":"`+trust.AutoAPI+`","mode":"`+store.ModeNotify+`"}`)},

		// Grants empty and KeepData true, so neither is critical
		// (core.Critical) and neither needs a re-confirmation; the two
		// critical variants are measured in their own cases.
		{core.ServiceController, core.ActActivate, byHandler,
			root(0xC1, core.ActActivate, `{"app":"`+appY+`","version":"1.0.0","sha256":"`+
				b64of(32, 1)+`","ui":"`+b64of(32, 2)+`","store":"`+trust.StoreGlobal+`"}`, false)},
		{core.ServiceController, core.ActDeactivate, byHandler,
			root(0xC2, core.ActDeactivate, `{"app":"`+appY+`"}`, false)},
		{core.ServiceController, core.ActUninstall, byHandler,
			root(0xC3, core.ActUninstall, `{"app":"`+appY+`","keepData":true}`, false)},

		// No handler of this API: the controller carries them out alone, and
		// the relay is still what takes them. delete-data is critical.
		{core.ServiceController, core.ActDeleteData, byRelay,
			root(0xC4, core.ActDeleteData, `{"app":"`+appY+`"}`, true)},
		{core.ServiceController, core.ActInstall, byRelay,
			root(0xC5, core.ActInstall, `{"target":"`+trust.AutoAPI+`","version":"1.0.0","sha256":"`+
				b64of(32, 3)+`"}`, false)},

		// The API alone opens these two (core.ShellOpens); a shell's OPEN for
		// them is E_FORBIDDEN, and that is reachability as the contract means
		// it -- not a seam that is missing.
		{core.ServiceController, core.ActStage, byForbidden,
			call(core.ServiceController, core.ActStage,
				`{"target":"`+trust.AutoAPI+`","version":"1.0.0"}`)},
		{core.ServiceController, core.ActLicense, byForbidden,
			call(core.ServiceController, core.ActLicense, `{"app":"`+appY+`"}`)},
	}
}

// TestEveryStoreActionIsReachable is the case this task exists for: before it,
// NO handler of ngc.store was registered and every action of it answered
// E_NOTFOUND to a shell. Each row drives its action through the real
// dispatcher over a real session link and holds that the answer is NOT
// E_NOTFOUND -- and, for the nine this API serves, that the app store or the
// updates were really called and not something that merely answered.
//
// The two assertions in front are what keeps the table honest. Without them a
// row could quietly disappear, or an action could be added to the contract and
// measured by nobody, and the test would still be green.
func TestEveryStoreActionIsReachable(t *testing.T) {
	cases := storeCases()
	// Every action of ngc.store has a row.
	for _, a := range core.All() {
		if a.App != core.AppStore {
			continue
		}
		if !slices.ContainsFunc(cases, func(c storeCase) bool {
			return c.service == a.Service && c.action == a.Name
		}) {
			t.Fatalf("ngc.store/%s/%s is in the contract and in no row of this table", a.Service, a.Name)
		}
	}
	// And every row names an action of ngc.store.
	for _, c := range cases {
		if _, ok := core.Lookup(core.AppStore, c.service, c.action); !ok {
			t.Fatalf("row ngc.store/%s/%s names no action of the contract", c.service, c.action)
		}
	}
	for _, c := range cases {
		t.Run(c.service+"/"+c.action, func(t *testing.T) {
			fx := newStoreFixture(t)
			code := c.drive(t, fx)
			if code == wire.CodeNotFound {
				t.Fatalf("E_NOTFOUND: nothing serves ngc.store/%s/%s", c.service, c.action)
			}
			switch c.want {
			case byHandler:
				if code != "" {
					t.Fatalf("close code %q, wanted a result", code)
				}
				if !fx.Store.saw(c.action) && !fx.Up.saw(c.action) {
					t.Fatalf("the channel answered but neither seam was called: %v %v",
						fx.Store.calls, fx.Up.calls)
				}
			case byRelay:
				if code != wire.CodeUnreachable {
					// The fake relay ends every forwarded channel with
					// E_UNREACHABLE, so this says it went to the relay.
					t.Fatalf("close code %q, wanted the relay's", code)
				}
				if fx.Store.saw(c.action) {
					t.Fatal("the app store was called for an action the controller carries out")
				}
			case byForbidden:
				if code != wire.CodeForbidden {
					t.Fatalf("close code %q, wanted E_FORBIDDEN", code)
				}
			}
		})
	}
}

// --- Who may open one: one case per refusal, with the control in between ---

// TestOnlyAnAdminReachesAStoreAction is the other half of registering a
// handler. Every action of ngc.store carries core.Action.Admin, so a user who
// is no admin must not reach one -- and a handler that is registered is a door
// that is now open, so this is measured per action and not once.
//
// Each row is three measurements, in this order, so a green row cannot mean
// "nothing works": the admin's own channel goes through (the control), the
// non-admin's is E_FORBIDDEN, and the seam saw the admin's call and NOT the
// other's.
func TestOnlyAnAdminReachesAStoreAction(t *testing.T) {
	// The one other user of trusttest.Genesis who is NOT an admin. A control
	// in front holds that, so a trust list that gave him the role would be
	// seen here instead of making every row pass for the wrong reason.
	for _, c := range storeCases() {
		if c.want == byForbidden {
			// Already E_FORBIDDEN for an admin; it says nothing about roles.
			continue
		}
		t.Run(c.service+"/"+c.action, func(t *testing.T) {
			fx := newStoreFixture(t)
			// 1. the control: the admin gets through.
			if code := c.drive(t, fx); code == wire.CodeForbidden {
				t.Fatalf("the admin was refused: %q -- this row measures nothing", code)
			}
			before := len(fx.Store.calls) + len(fx.Up.calls)
			// 2. the same channel as a user who is no admin. His session
			// certificate carries his own key, so his task is signed by him
			// and his re-confirmation is his: everything a refusal could rest
			// on besides the role is in order.
			other := nonAdminSession(t, fx.harness)
			code := c.drive(t, &storeHarness{harness: other, Store: fx.Store, Up: fx.Up})
			if code != wire.CodeForbidden {
				t.Fatalf("a user who is no admin got %q, not E_FORBIDDEN", code)
			}
			// 3. and nothing of his reached a seam.
			if after := len(fx.Store.calls) + len(fx.Up.calls); after != before {
				t.Fatalf("%d calls reached the seam after the refusal, %d before", after, before)
			}
		})
	}
}

// --- What the dispatcher hands over ---------------------------------------

// TestTheSignedTaskReachesTheAppStoreAsItStands holds what appstore.Carried
// asks for: the task travels as the shell sent it, because the controller
// checks the signature itself and would read any change as a forgery. The
// pointer is compared, not the bytes: that is the only way to see that nothing
// re-encoded it on the way.
func TestTheSignedTaskReachesTheAppStoreAsItStands(t *testing.T) {
	fx := newStoreFixture(t)
	fx.PutApp(t, appY, true)
	tk := rootTask(t, fx.harness, 0xD1, core.ActDeactivate, `{"app":"`+appY+`"}`)
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Window: wire.MinWindow, Task: tk})
	c.result(t)
	rq := fx.Store.deact
	if rq == nil {
		t.Fatal("the app store was not called")
	}
	if rq.Task == nil || !bytes.Equal(rq.Task.Sig, tk.Sig) || !bytes.Equal(rq.Task.Payload, tk.Payload) {
		t.Fatalf("the task that arrived is not the one that was sent: %+v", rq.Task)
	}
	// The node is the VERIFIED task's own and not the guess out of the
	// unsigned payload.
	if rq.Node != ctlNode {
		t.Fatalf("node %q, not the one the signed task names", rq.Node)
	}
	if rq.Proof == nil || rq.Client == nil {
		t.Fatalf("certificate or proof is missing: client %d, proof %v", len(rq.Client), rq.Proof)
	}
	if rq.Live == nil || rq.Live.Verified.Cert().User != fx.AdminID {
		t.Fatal("the session of the channel did not travel")
	}
}

// TestTheCatalogHandlerAsksForTheStoreOfTheParameters is the small control
// over the one action that has a parameter worth confusing: a handler that
// passed a fixed ID, or the app of the OPEN, would be green without it.
func TestTheCatalogHandlerAsksForTheStoreOfTheParameters(t *testing.T) {
	fx := newStoreFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActCatalog, Window: wire.MinWindow,
		Params: json.RawMessage(`{"store":"other-store"}`)})
	c.result(t)
	if fx.Store.catalogOf != "other-store" {
		t.Fatalf("the catalog of %q was asked for", fx.Store.catalogOf)
	}
}

// TestTheLicenceHandlerReadsTheAppOutOfTheEnvelope holds the one place this
// file reads a field out of something nothing has verified yet. The action
// carries no parameters, so the app can only come out of the licence; the
// value picks the row and store.VerifyLicense insists the signed licence names
// the same app, which is why it is allowed.
func TestTheLicenceHandlerReadsTheAppOutOfTheEnvelope(t *testing.T) {
	fx := newStoreFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActLicense, Window: wire.MinWindow})
	if !c.awaitGrant(t) {
		t.Fatal("the channel was refused before it granted anything")
	}
	c.send(t, licenceBody(t, fx.harness, appY))
	c.result(t)
	if fx.Store.licensed != appY {
		t.Fatalf("the licence was taken for %q", fx.Store.licensed)
	}
}

// TestALicenceThatIsNoEnvelopeIsAProtocolError is its control: without a
// parser in front, the handler would hand appstore whatever bytes came.
func TestALicenceThatIsNoEnvelopeIsAProtocolError(t *testing.T) {
	fx := newStoreFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActLicense, Window: wire.MinWindow})
	if !c.awaitGrant(t) {
		t.Fatal("the channel was refused before it granted anything")
	}
	c.send(t, []byte(`{"app":"`+appY+`"}`))
	if cl := c.closed(t); cl.Code != wire.CodeProtocol {
		t.Fatalf("close %+v", cl)
	}
	if fx.Store.saw(core.ActLicense) {
		t.Fatal("the app store was handed bytes that are no envelope")
	}
}

// TestActivatingAnUploadedAppHandsOverNoFile nails the open seam of Punkt 30
// where it shows: ngc.store/upload keeps only the ui-tar, and the shell sends
// no DATA on a FlowTransfer channel it opened (core.Action.MaxIn), so nobody
// holds the package of an uploaded app and ActivateRequest.File stays nil.
//
// It is here so that the day somebody decides where that file lies, a case
// says what changed instead of nothing.
func TestActivatingAnUploadedAppHandsOverNoFile(t *testing.T) {
	fx := newStoreFixture(t)
	fx.PutApp(t, appY, true)
	tk := rootTask(t, fx.harness, 0xD2, core.ActActivate,
		`{"app":"`+appY+`","version":"2.0.0","sha256":"`+b64of(32, 1)+`","ui":"`+
			b64of(32, 2)+`","store":"`+core.LocalStore+`"}`)
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActActivate, Window: wire.MinWindow, Task: tk})
	c.result(t)
	rq := fx.Store.act
	if rq == nil {
		t.Fatal("the app store was not called")
	}
	if rq.Params == nil || rq.Params.Store != core.LocalStore {
		t.Fatalf("params %+v", rq.Params)
	}
	if rq.File != nil || rq.Size != 0 {
		t.Fatalf("a file arrived after all: size %d -- somebody decided Punkt 30, "+
			"and this case has to say what it decided", rq.Size)
	}
}

// --- The two halves of the order in open ----------------------------------

// TestAControllerActionWithoutAHandlerStillGoesToTheRelay is the half that
// could have broken while the other was mended. Turning the order round in
// open -- handler first, relay second -- must not keep an action from the
// relay that only the controller can carry out: delete-data and install have
// no handler here and no step in appstore at all.
func TestAControllerActionWithoutAHandlerStillGoesToTheRelay(t *testing.T) {
	for _, c := range storeCases() {
		if c.want != byRelay {
			continue
		}
		t.Run(c.action, func(t *testing.T) {
			fx := newStoreFixture(t)
			if code := c.drive(t, fx); code != wire.CodeUnreachable {
				t.Fatalf("close code %q, not the fake relay's E_UNREACHABLE", code)
			}
			r := fx.Relay.forwarded(t)
			if r.Open.Action != c.action || r.Node != ctlNode {
				t.Fatalf("forwarded %q to %q", r.Open.Action, r.Node)
			}
			if r.Task == nil || r.Task.Node != ctlNode {
				t.Fatalf("task %+v", r.Task)
			}
		})
	}
}

// TestAnAppChannelStillGoesToTheRelay is the same question one layer out: an
// app that is not a core app never looks for a handler at all, and the
// reordering must not have moved that.
func TestAnAppChannelStillGoesToTheRelay(t *testing.T) {
	fx := newStoreFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse, appY+"/"+appAct)
	c := fx.Open(t, wire.Open{App: appY, Service: appSvc, Action: appAct,
		Window: wire.MinWindow, E2E: make([]byte, wire.HandshakeSize)})
	r := fx.Relay.forwarded(t)
	if r.Open.App != appY {
		t.Fatalf("forwarded %q", r.Open.App)
	}
	if cl := c.closed(t); cl.Code != wire.CodeUnreachable {
		t.Fatalf("close %+v", cl)
	}
}

// TestTheNodeOfAStoreActionIsLogged holds that the log of a channel this API
// serves on a controller names the node. Before the reorder the handler path
// logged "" for the node, because only the relay path returned one.
func TestTheNodeOfAStoreActionIsLogged(t *testing.T) {
	fx := newStoreFixture(t)
	fx.PutApp(t, appY, true)
	tk := rootTask(t, fx.harness, 0xD3, core.ActDeactivate, `{"app":"`+appY+`"}`)
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Window: wire.MinWindow, Task: tk})
	c.result(t)
	e := fx.waitEntry(t, core.AppStore, core.ActDeactivate)
	if e.Node != ctlNode {
		t.Fatalf("the log says node %q", e.Node)
	}
}

// awaitGrant reads up to the API's first GRANT.
//
// It is NOT a convenience and it is not a wait for a race to settle: on a
// FlowIn channel the window in the OPEN is what the SHELL grants the API, and
// the credit the shell may spend comes from the API's GRANT alone. A shell
// that writes DATA before it has that GRANT is over its credit, and the
// channel ends with E_PROTOCOL instead of reaching the handler -- measured on
// ngc.store/upload, where verifying the signed task and the re-confirmation
// takes the API long enough that the DATA overtook its GRANT every time.
// Without this the case would have measured flow control and called it the
// handler.
// It answers false for a channel that was refused before it granted anything,
// which is the normal end of every case that measures a refusal; the caller
// then reads the CLOSE instead of writing on.
func (c *chann) awaitGrant(t *testing.T) bool {
	t.Helper()
	for {
		f := c.pull(t)
		switch f.Type {
		case wire.TypeGrant:
			return true
		case wire.TypeClose:
			c.queue = append(c.queue, f)
			return false
		}
	}
}

// send sends body as DATA and then EOF, waiting for the API's GRANT whenever
// the credit runs out. It is sendBytes with content: a licence has to be the
// bytes of a licence, not a run of zeros.
func (c *chann) send(t *testing.T, body []byte) {
	t.Helper()
	for len(body) > 0 {
		chunk := min(len(body), wire.MaxPayload)
		for c.credit < uint64(chunk) {
			if f := c.pull(t); f.Type == wire.TypeClose {
				c.queue = append(c.queue, f)
				return
			}
		}
		c.credit -= uint64(chunk)
		c.h.peer.write(t, wire.Frame{Type: wire.TypeData, Channel: c.num, Payload: body[:chunk]})
		body = body[chunk:]
	}
	c.h.peer.write(t, wire.Frame{Type: wire.TypeEOF, Channel: c.num})
}

// --- Helpers of this file --------------------------------------------------

// b64of is n bytes of fill, base64url as enc.Bytes reads it.
func b64of(n int, fill byte) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	return enc.B64.EncodeToString(b)
}

// nonAdminSession puts the next version of the trust list into the
// installation -- the one that carries the role "user" and bob in it -- and
// returns a session of bob's own. Both users of trusttest.Genesis are admins,
// so a user who is no admin has to be added before the admin wall can be
// measured at all.
func nonAdminSession(t *testing.T, h *harness) *harness {
	t.Helper()
	l := nextVersion(t, h)
	if _, err := h.S.AddVersion(ctx, h.Admin.Sign(t, l)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	if err := h.S.DB.UserPut(ctx, dbapi.User{ID: bobID, Name: bobName,
		Roles: []string{userRole}}); err != nil {
		t.Fatalf("user bob: %v", err)
	}
	return h.As(t, trusttest.NewSigner(bobFill), bobID, bobName)
}

// endOf reads a channel to its end and answers the code it closed with, "" for
// one that answered a result in order.
func endOf(t *testing.T, c *chann) wire.Code {
	t.Helper()
	c.collect(t)
	cl := c.closed(t)
	if cl.Status == wire.StatusOK {
		return ""
	}
	return cl.Code
}

// onlineSender is appstore.Sender with every node online and no way out. It is
// here so a case can drive the REAL app store up to the point where the
// package would be read, which is where Punkt 30 bites.
type onlineSender struct{}

func (onlineSender) Node(string) (tunnel.Hello, bool) {
	return tunnel.Hello{Roles: []string{tunnel.RoleServices}}, true
}

func (onlineSender) Transfer(context.Context, string, string, json.RawMessage, core.Transfer,
	io.Reader, *sign.Envelope, []byte, *proof.Proof) ([]byte, wire.Code) {
	return nil, wire.CodeInternal
}

func (onlineSender) Call(context.Context, string, string, json.RawMessage, *sign.Envelope,
	[]byte, *proof.Proof) ([]byte, wire.Code) {
	return nil, wire.CodeInternal
}

// TestAnUploadedAppCannotBeActivatedThroughTheRealAppStore is Punkt 30 from end
// to end, with the real appstore behind the handler instead of a fake: a shell
// opens ngc.store/controller/activate for an app of core.LocalStore, the
// controller is online, everything is signed -- and the channel ends with
// E_UNREACHABLE, because nobody holds the uploaded package.
//
// The control is in front of it: with the SAME request out of a store of the
// trust list the answer is a different one, so the E_UNREACHABLE is about the
// missing file and not about a wall that stops every activation.
func TestAnUploadedAppCannotBeActivatedThroughTheRealAppStore(t *testing.T) {
	var real *appstore.Stores
	fx := newFixtureWith(t, func(h *harness, d *Deps) {
		real = appstore.New(h.S)
		real.Send = onlineSender{}
		real.Spool = t.TempDir()
		d.Store = real
	})
	fx.PutApp(t, appY, true)
	drive := func(fill byte, src string) wire.Code {
		tk := rootTask(t, fx, fill, core.ActActivate,
			`{"app":"`+appY+`","version":"2.0.0","sha256":"`+b64of(32, 1)+`","ui":"`+
				b64of(32, 2)+`","store":"`+src+`"}`)
		return endOf(t, fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
			Action: core.ActActivate, Window: wire.MinWindow, Task: tk}))
	}
	// First that there is a handler at all, so a green case cannot rest on an
	// E_NOTFOUND of the dispatcher that looks like one of the app store.
	if !fx.D.Serves(core.AppStore, core.ServiceController, core.ActActivate) {
		t.Fatal("no handler for activate -- this case measures nothing")
	}
	// The control, and it is a DIFFERENT code and not merely "not
	// E_NOTFOUND": an app out of a store of the trust list gets past the
	// missing file and fails on the catalog this installation never fetched,
	// which appstore.CodeFor answers with E_NOTFOUND. Two different codes out
	// of the same channel are what says both ends were really reached.
	if code := drive(0xE1, trust.StoreGlobal); code != wire.CodeNotFound {
		t.Fatalf("a store app answered %q, not the E_NOTFOUND of a catalog that is not there", code)
	}
	if code := drive(0xE2, core.LocalStore); code != wire.CodeUnreachable {
		t.Fatalf("an uploaded app answered %q; if that is no longer E_UNREACHABLE, "+
			"somebody decided where the file lies (Punkt 30)", code)
	}
	// And nothing stayed behind in the spool.
	if es, err := os.ReadDir(real.Spool); err != nil || len(es) != 0 {
		t.Fatalf("the spool holds %d entries (%v)", len(es), err)
	}
}

// refusingStore answers every controller step with one code and no error,
// which is how appstore reports a refusal (Stores.Activate).
type refusingStore struct {
	*fakeStore
	code wire.Code
}

func (r *refusingStore) Deactivate(context.Context, appstore.DeactivateRequest) (wire.Code, error) {
	r.note(core.ActDeactivate)
	return r.code, nil
}

// TestARefusalOfTheAppStoreEndsTheChannelWithItsCode holds what storeAnswer
// does: the three controller steps answer a wire.Code for every refusal and an
// error only for this installation's own failure, so the CODE is what ends the
// channel. A handler that answered {} and dropped the code would tell a shell
// the app was deactivated when it was not.
func TestARefusalOfTheAppStoreEndsTheChannelWithItsCode(t *testing.T) {
	rs := &refusingStore{fakeStore: &fakeStore{}, code: wire.CodeLicense}
	fx := newFixtureWith(t, func(_ *harness, d *Deps) { d.Store = rs })
	fx.PutApp(t, appY, true)
	tk := rootTask(t, fx, 0xE5, core.ActDeactivate, `{"app":"`+appY+`"}`)
	code := endOf(t, fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Window: wire.MinWindow, Task: tk}))
	if !rs.saw(core.ActDeactivate) {
		t.Fatal("the app store was not called")
	}
	if code != wire.CodeLicense {
		t.Fatalf("the channel ended with %q, not with the code the app store gave", code)
	}
}

// TestALicenceForAnImpossibleAppIsAProtocolError is the second half of
// appOfLicense: the name out of the envelope has to be an app ID before it is
// used to pick a row. Without the check it would reach the database as it
// came.
func TestALicenceForAnImpossibleAppIsAProtocolError(t *testing.T) {
	fx := newStoreFixture(t)
	env, err := sign.Sign(fx.Admin.Priv, sign.PurposeLicense, []byte(`{"app":"NOT AN APP"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActLicense, Window: wire.MinWindow})
	if !c.awaitGrant(t) {
		t.Fatal("the channel was refused before it granted anything")
	}
	c.send(t, body)
	if code := endOf(t, c); code != wire.CodeProtocol {
		t.Fatalf("close code %q", code)
	}
	if fx.Store.saw(core.ActLicense) {
		t.Fatalf("the app store was asked for %q", fx.Store.licensed)
	}
}

// refusingUpdates answers SetSetting with one error.
type refusingUpdates struct {
	*fakeUpdates
	err error
}

func (r *refusingUpdates) SetSetting(_ context.Context, s core.UpdateSetting) error {
	r.calls = append(r.calls, core.ActSetUpdate)
	r.set = &s
	return r.err
}

// TestAModeTheInstallationDidNotAllowIsForbidden holds the one error of
// package updates that is the caller's and not the installation's: a target
// without a standing approval in the trust list may not be switched to
// store.ModeAuto, and updates says so with core.ErrParams. Mapped by
// appstore.CodeFor it would be E_INTERNAL -- a working installation telling an
// admin it is broken.
//
// The control is the second half: any OTHER error of SetSetting stays
// E_UNREACHABLE, so this is not a handler that calls every failure a refusal.
func TestAModeTheInstallationDidNotAllowIsForbidden(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want wire.Code
	}{
		{"no standing approval", core.ErrParams, wire.CodeForbidden},
		{"the database did not answer", errors.New("x"), wire.CodeUnreachable},
	} {
		t.Run(c.name, func(t *testing.T) {
			ru := &refusingUpdates{fakeUpdates: &fakeUpdates{}, err: c.err}
			fx := newFixtureWith(t, func(_ *harness, d *Deps) { d.Updates = ru })
			code := endOf(t, fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
				Action: core.ActSetUpdate, Window: wire.MinWindow,
				Params: json.RawMessage(`{"target":"` + trust.AutoAPI + `","mode":"` + store.ModeAuto + `"}`)}))
			if !ru.saw(core.ActSetUpdate) {
				t.Fatal("the updates were not asked")
			}
			if code != c.want {
				t.Fatalf("close code %q, wanted %q", code, c.want)
			}
		})
	}
}

// --- The memory of ngc.store/api/upload ------------------------------------

// peakHeap runs fn and answers how far the LIVE heap rose above where it stood
// when fn started. The peak and not the increase: a run that holds a package
// and lets it go again shows an increase of nothing.
//
// It collects before every reading, and that is the whole point of this
// function rather than a tidiness: HeapAlloc counts garbage that has not been
// swept yet, so without the collection the number answers the collector's
// clock and not the question. MEASURED, and it is why the first version of
// this case was worthless: on unchanged code the figure swung between 3 and 22
// MiB and swung WITHOUT the input -- 16 MiB of input showed 17 MiB, 64 MiB
// showed 13. Garbage is what the collector may take away; a package that is
// really HELD it may not. So a reading taken right after a collection answers
// "held", and only that.
//
// The price is the collection, so the sampler runs every 20 ms and not every
// millisecond. That is resolution enough by a wide margin: a package that is
// collected is held for the whole upload, which is seconds, and never for one
// window of 20 ms.
func peakHeap(fn func()) int64 {
	read := func() int64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return int64(m.HeapAlloc)
	}
	base := read()
	var peak atomic.Int64
	peak.Store(base)
	note := func() {
		if h := read(); h > peak.Load() {
			peak.Store(h)
		}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			note()
			time.Sleep(20 * time.Millisecond)
		}
	}()
	fn()
	// Once more after fn returned: the last frames may have come between two
	// samples.
	note()
	close(stop)
	<-done
	return peak.Load() - base
}

// uploadPeak sends size bytes through a real channel to ngc.store/api/upload
// and answers the peak of the heap while they go. With hold the seam collects
// the package instead of streaming it (fakeStore.hold), which is the null
// control of the case below.
func uploadPeak(t *testing.T, size int, hold bool) int64 {
	t.Helper()
	fx := newStoreFixture(t)
	fx.Store.hold = hold
	tk := fx.Task(t, task.Task{ID: taskID(0xB2), App: core.AppStore,
		Service: core.ServiceAPI, Action: core.ActUpload})
	c := fx.Open(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActUpload, Window: wire.MinWindow, Task: tk,
		Confirm: confirmOf(fx.harness, tk)})
	// The way to the handler has to be open before a number means anything: a
	// channel refused at the task or the confirmation sends nothing and holds
	// nothing, and the measurement would be flat for the wrong reason.
	if !c.awaitGrant(t) {
		t.Fatal("no GRANT came: the channel was refused before the handler ran")
	}
	var code wire.Code
	peak := peakHeap(func() {
		c.sendBytes(t, size)
		code = endOf(t, c)
	})
	if code != "" {
		t.Fatalf("the channel closed with %q", code)
	}
	// Second intermediate check: every byte really went through. A run that
	// was cut short would also be flat.
	if fx.Store.uploaded != int64(size) {
		t.Fatalf("the seam saw %d bytes, the shell sent %d", fx.Store.uploaded, size)
	}
	fx.Store.held = nil
	return peak
}

// TestUploadHoldsNoPackageOnTheChannel is the memory of ngc.store/api/upload,
// measured over a REAL channel and not against the handler: the package goes
// in as DATA frames through the session, the dispatcher and on to the seam.
//
// Why it has to be the channel and not appstore: appstore.Upload streams, and
// a case one layer below (TestUploadHoldsNoPackage there) measures that it
// does. What used to collect the whole package was the dispatcher, which read
// its input into a []byte before it called anyone -- at core.Action.MaxIn,
// which is store.MaxPackage = 4 GiB for this action. A comment in store.go
// said the stream was handed over, and no case held it.
//
// WHAT IS ASSERTED, and the first version of this case got it wrong: not an
// absolute number of bytes. That version said "under 8 MiB" and was red in 7
// of 10 runs on unchanged code, because the floor of this gauge is garbage not
// yet collected and swings between 3 and 22 MiB with the collector's clock --
// INDEPENDENT of the input. An absolute bound therefore measures the
// collector, and loosening it until it holds would have measured nothing at
// all.
//
// What a collected package does instead is grow WITH the input, and that is
// what is asserted here:
//
//   - the slope. From smallSend to largeSend the input grows by 240 MiB; what
//     is held may grow by at most a quarter of that. A run that streams grows
//     by nothing; a run that collects grows by the whole 240 MiB and more. The
//     slope and not a number of bytes, so that whatever floor this machine has
//     cancels out: it is in both readings.
//   - the null control, at the SAME size, through the SAME apparatus, only
//     the seam collects (fakeStore.hold): it has to hold at least eight times
//     as much, and at least half the package. Both halves are needed -- the
//     factor says the gauge can still tell the two apart, the half says it
//     really sees a package and not a difference between two floors.
//
// Measured, 12.10.2026, on the LOCAL disk (C:), go1.27.2 windows/amd64. Not
// measured on the network drive the main checkout lives on (Z:), and this
// case touches no file, so there is nothing it would be expected to depend on
// there -- but that is read, not run.
func TestUploadHoldsNoPackageOnTheChannel(t *testing.T) {
	const (
		smallSend = 16 << 20
		largeSend = 256 << 20
	)
	small := uploadPeak(t, smallSend, false)
	large := uploadPeak(t, largeSend, false)
	null := uploadPeak(t, largeSend, true)
	t.Logf("held: %d B at %d B in, %d B at %d B in, null control %d B at %d B in",
		small, smallSend, large, largeSend, null, largeSend)

	// The slope: sixteen times the input, and what is held may not follow it.
	grewBy := large - small
	slopeBound := int64(largeSend-smallSend) / 4
	if grewBy > slopeBound {
		t.Fatalf("the input grew by %d bytes and what is held grew by %d, bound %d "+
			"-- the package is being collected somewhere on the way",
			largeSend-smallSend, grewBy, slopeBound)
	}

	// The null control, and its assertions are the other way round: a run that
	// really holds the package MUST be seen, or the slope above is only a
	// gauge that sees nothing.
	if null < 8*large {
		t.Fatalf("the null control held %d bytes against %d of the streaming run at the "+
			"same size -- less than eightfold, so the gauge cannot tell collecting "+
			"from streaming and the slope proves nothing", null, large)
	}
	if null < largeSend/2 {
		t.Fatalf("the null control collected %d bytes and the heap rose by only %d "+
			"-- the gauge does not see a held package at all", largeSend, null)
	}
}
