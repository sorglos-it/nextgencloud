package access

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

const (
	// node is the controller these tests run on: ctl-1 of trusttest.Genesis,
	// where the admins may run root tasks. otherNode is the second controller
	// the fixture adds, where nobody may.
	node      = "ctl-1"
	otherNode = "ctl-2"
	// testApp is the app the fixture has installed: the action run on the
	// service files, and the privileged wipe on the service admin.
	testApp     = "org.sorglos.terminal"
	testService = "files"
	testAction  = "run"
	// adminService carries wipe, which the manifest marks critical.
	adminService = "admin"
	adminAction  = "wipe"

	issued = 1790000000
	now    = issued + 10
)

// sessionSeed makes the Ed25519 key of the client certificate: the key that
// signs tasks. strangerSeed makes a key of the same kind that no certificate
// names.
var (
	sessionSeed  = bytes.Repeat([]byte{0x51}, ed25519.SeedSize)
	strangerSeed = bytes.Repeat([]byte{0x59}, ed25519.SeedSize)
)

// fixture is one logged-in admin of a trust list: the list, the session key
// of the certificate and the certificate with the user's proof over it.
type fixture struct {
	list      *trust.List
	admin     trusttest.Signer // the user of the certificate; also signs re-confirmations
	client    []byte           // the client certificate payload, exactly as the proof covers it
	certProof proof.Proof
	e2e       []byte // Noise message 1 of an app channel; core apps carry none
}

// newFixture builds the list of trusttest.Genesis with a second controller
// ctl-2 on which no role may run root tasks, and logs the admin in.
func newFixture(t testing.TB) fixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	otherDH, _ := trusttest.NodeKeys(0xC2)
	l.Nodes = append(l.Nodes, trust.Node{ID: otherNode, Kind: trust.NodeController, Name: "Ohne Root", Noise: otherDH.Public})
	// A fixture nobody could have signed would prove nothing, so the list has
	// to be one trust.List.Validate accepts.
	if err := l.Validate(); err != nil {
		t.Fatalf("test trust list: %v", err)
	}
	sessSig := ed25519.NewKeyFromSeed(sessionSeed)
	sessDH, _ := trusttest.NodeKeys(0x52)
	client := mustJSON(t, session.ClientCert{Installation: l.Installation, User: trusttest.ID(0x21),
		Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize), Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)),
		DH: sessDH.Public, Issued: issued, Expires: issued + 3600})
	return fixture{list: l, admin: admin, client: client, certProof: admin.Prove(sign.PurposeClientCert, client),
		e2e: bytes.Repeat([]byte{0x54}, wire.HandshakeSize)}
}

// request is the OPEN payload as a request of this controller, with the app
// the fixture has installed and a store that lets every task through.
func (f fixture) request(payload []byte) Request {
	return Request{Payload: payload, List: f.list, Node: node, Now: now, Apps: installed(), Tasks: freeOnce{}}
}

// withUser wraps a browser OPEN the way the API forwards it: with the client
// certificate of the fixture's admin and the user's proof over it.
func (f fixture) withUser(t testing.TB, o wire.Open) []byte {
	t.Helper()
	return mustJSON(t, tunnel.Open{Open: mustJSON(t, o), Window: wire.DefaultWindow, Client: f.client, Proof: &f.certProof})
}

// withoutUser wraps an OPEN the API opens on its own: no certificate, no proof.
func withoutUser(t testing.TB, o wire.Open) []byte {
	t.Helper()
	return mustJSON(t, tunnel.Open{Open: mustJSON(t, o), Window: wire.DefaultWindow})
}

// coreOpen is the browser OPEN of a core action of this controller. Core apps
// carry no end-to-end handshake (wire.Open.Validate).
func coreOpen(app, action string, env *sign.Envelope) wire.Open {
	return wire.Open{App: app, Service: core.ServiceController, Action: action, Window: wire.DefaultWindow, Task: env}
}

// appOpen is the browser OPEN of an app channel, which always carries message
// 1 of the end-to-end handshake.
func (f fixture) appOpen(app, service, action string) wire.Open {
	return wire.Open{App: app, Service: service, Action: action, Window: wire.DefaultWindow, E2E: f.e2e}
}

// task returns a task of this admin for an app, service and action on the
// controller on, signed by whoever holds seed. The session key of the
// certificate comes from sessionSeed; any other seed is a stranger's key.
func (f fixture) task(t testing.TB, seed []byte, on, app, service, action string, params any) *sign.Envelope {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		raw = mustJSON(t, params)
	}
	payload := mustJSON(t, task.Task{ID: bytes.Repeat([]byte{0x61}, task.IDSize), Installation: f.list.Installation,
		Cert: session.CertHash(f.client), Node: on, App: app, Service: service, Action: action,
		Params: raw, Issued: issued, Expires: issued + 60})
	env, err := sign.Sign(ed25519.NewKeyFromSeed(seed), sign.PurposeTask, payload)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// activateParams are valid parameters of ngc.store/controller/activate for
// the app the fixture has installed, without grants: nothing that makes the
// action critical.
func activateParams() core.ActivateParams {
	return core.ActivateParams{App: testApp, Version: "1.0.0", SHA256: bytes.Repeat([]byte{0x01}, 32),
		UI: bytes.Repeat([]byte{0x02}, 32), Store: trust.StoreGlobal}
}

// manifest is the manifest of the installed app, the shape the escalation
// needs: the harmless run on the service files, and wipe on the service
// admin, which the manifest marks critical. No needs, so run's rule is
// neither root nor critical.
func manifest() []byte { return manifestFor(testApp) }

// manifestFor is that manifest under another app ID.
func manifestFor(id string) []byte {
	return []byte(`{"id":"` + id + `","name":"Terminal","version":"1.0.0","publisher":"Sorglos",` +
		`"services":[{"name":"` + testService + `","command":["/bin/terminal"]},` +
		`{"name":"` + adminService + `","command":["/bin/wipe"]}],` +
		`"actions":[{"name":"` + testAction + `","title":"Ausführen","service":"` + testService + `"},` +
		`{"name":"` + adminAction + `","title":"Löschen","service":"` + adminService + `","critical":true}]}`)
}

// withNeeds is that manifest with needs, which makes every one of its actions
// a root action (store.RuleFor).
func withNeeds(need string) []byte {
	return []byte(strings.TrimSuffix(string(manifest()), "}") + `,"needs":["` + need + `"]}`)
}

// rows is the one app of the fixture with another manifest.
func rows(m []byte) apps {
	r := installed()
	app := r[testApp]
	app.Manifest = m
	r[testApp] = app
	return r
}

// apps answers for the apps of this controller, as state.Store does.
type apps map[string]dbapi.App

func (a apps) App(_ context.Context, id string) (dbapi.App, error) {
	app, ok := a[id]
	if !ok {
		return dbapi.App{}, dbapi.ErrNotFound
	}
	return app, nil
}

// installed is the one app of the fixture.
func installed() apps {
	return apps{testApp: {ID: testApp, Version: "1.0.0", Store: trust.StoreGlobal, Node: node, Active: true,
		Manifest: manifest(), SHA256: bytes.Repeat([]byte{0x01}, 32)}}
}

// errApps answers every lookup with a failure that is not dbapi.ErrNotFound,
// the way a damaged row (state.ErrDocument) or a database that is not there
// would.
type errApps struct{}

func (errApps) App(context.Context, string) (dbapi.App, error) {
	return dbapi.App{}, errors.New("no database")
}

// errOnce cannot remember a task ID: not a replay, a store that failed.
type errOnce struct{}

func (errOnce) UseTask(context.Context, []byte, int64, int64) error { return errors.New("no database") }

// freeOnce takes every task ID: what a controller that has seen none answers.
type freeOnce struct{}

func (freeOnce) UseTask(context.Context, []byte, int64, int64) error { return nil }

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// wantClose fails unless the channel is refused with this code, and unless
// the refusal carries no decision at all.
func wantClose(t testing.TB, d Decision, cl *wire.Close, code wire.Code) {
	t.Helper()
	if cl == nil {
		t.Fatalf("channel accepted, want CLOSE %s", code)
	}
	if cl.Code != code || cl.Status != wire.StatusError || cl.Message != code.Message() {
		t.Errorf("CLOSE %+v, want status error and code %s with its message", *cl, code)
	}
	if !noDecision(d) {
		t.Errorf("refused channel carries a decision: %+v", d)
	}
}

// noDecision reports whether the decision says nothing: no refusal may leave
// a user, a task or an app behind.
func noDecision(d Decision) bool {
	return d.Opened.Session == nil && d.Opened.Open.App == "" && d.Opened.Window == 0 && !d.Core &&
		d.Action.Name == "" && d.Task == nil && d.User == "" && d.UserName == "" && d.App.ID == "" && d.Manifest == nil
}

func TestCheckAcceptsSignedRootTask(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env))))
	if cl != nil {
		t.Fatalf("CLOSE %+v, want the channel to open", *cl)
	}
	if d.Task == nil {
		t.Fatal("no task in the decision")
	}
	if d.User != trusttest.ID(0x21) || d.UserName != "admin" {
		t.Errorf("user %q (%q), want the admin of the list", d.User, d.UserName)
	}
	if !d.Core || d.Action.Name != core.ActActivate || !d.Action.Root {
		t.Errorf("action %+v, core %v", d.Action, d.Core)
	}
	if d.Opened.Session == nil || d.Opened.Session.Cert().User != d.User {
		t.Error("the decision does not carry the checked session")
	}
	if d.Manifest != nil || d.App.ID != "" {
		t.Errorf("core action carries app %q and manifest %v", d.App.ID, d.Manifest != nil)
	}
}

func TestCheckRefusesForgedTask(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, strangerSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env))))
	wantClose(t, d, cl, wire.CodeVerify)
}

func TestCheckRefusesSwappedTask(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActDeactivate, core.AppParams{App: testApp})
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppStore, core.ActUninstall, env))))
	wantClose(t, d, cl, wire.CodeVerify)
}

func TestCheckRefusesReplayedTask(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
	payload := f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env))
	r := f.request(payload)
	// The real store of this controller, so the test proves what it remembers,
	// not what a stub says.
	st := state.New(memstore.New())
	defer st.Close()
	r.Tasks = st
	if _, cl := Check(context.Background(), r); cl != nil {
		t.Fatalf("first CLOSE %+v, want the channel to open", *cl)
	}
	d, cl := Check(context.Background(), r)
	wantClose(t, d, cl, wire.CodeVerify)
}

// TestCheckRefusesReplayedTaskAtOnce is the replay in the same instant:
// several channels of one tunnel carry the same signed task, and only the
// store can tell them apart. Exactly one may run.
func TestCheckRefusesReplayedTaskAtOnce(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
	r := f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env)))
	st := state.New(memstore.New())
	defer st.Close()
	r.Tasks = st
	const channels = 16
	var wg sync.WaitGroup
	var open atomic.Int64
	for range channels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, cl := Check(context.Background(), r)
			switch {
			case cl == nil:
				open.Add(1)
			case cl.Code != wire.CodeVerify:
				t.Errorf("CLOSE %s, want E_VERIFY for a task that has run", cl.Code)
			}
		}()
	}
	wg.Wait()
	if got := open.Load(); got != 1 {
		t.Fatalf("%d of %d channels opened, want exactly one", got, channels)
	}
}

func TestCheckRefusesRootTaskWithoutRootPermission(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, otherNode, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
	r := f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env)))
	r.Node = otherNode
	d, cl := Check(context.Background(), r)
	wantClose(t, d, cl, wire.CodeForbidden)
}

func TestCheckAsksForConfirmation(t *testing.T) {
	f := newFixture(t)
	// The list has the one role admin. At level root every root task needs a
	// fresh confirmation, and this OPEN carries none.
	f.list.Roles[0].Level = trust.LevelRoot
	env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env))))
	wantClose(t, d, cl, wire.CodeConfirm)
}

func TestCheckWithoutUserOnlyForListedActions(t *testing.T) {
	f := newFixture(t)
	t.Run("trust", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(withoutUser(t, coreOpen(core.AppUsers, core.ActTrust, nil))))
		if cl != nil {
			t.Fatalf("CLOSE %+v, want the channel to open", *cl)
		}
		if d.User != "" || d.UserName != "" || d.Task != nil || d.Opened.Session != nil {
			t.Errorf("channel without a user carries user %q and task %v", d.User, d.Task != nil)
		}
		if !d.Core || d.Action.Name != core.ActTrust {
			t.Errorf("action %+v, core %v", d.Action, d.Core)
		}
	})
	t.Run("activate", func(t *testing.T) {
		env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
		d, cl := Check(context.Background(), f.request(withoutUser(t, coreOpen(core.AppStore, core.ActActivate, env))))
		wantClose(t, d, cl, wire.CodeForbidden)
	})
}

// TestCheckWithoutUserIgnoresTheTask: a channel without a user is decided
// without ever checking a task — the one it carries is not looked at, however
// wrong it is. What still holds is the shape: the OPEN has to be an OPEN, and
// wire.ParseOpen sees the envelope before this package decides anything.
func TestCheckWithoutUserIgnoresTheTask(t *testing.T) {
	f := newFixture(t)
	t.Run("a task nobody asks for is never read", func(t *testing.T) {
		// Everything about it is wrong: a stranger's key, another node,
		// another action.
		env := f.task(t, strangerSeed, otherNode, core.AppStore, core.ServiceController, core.ActUninstall,
			core.UninstallParams{App: testApp})
		d, cl := Check(context.Background(), f.request(withoutUser(t, coreOpen(core.AppUsers, core.ActTrust, env))))
		if cl != nil {
			t.Fatalf("CLOSE %+v, want the channel to open", *cl)
		}
		if d.Task != nil || d.User != "" {
			t.Errorf("task %v, user %q", d.Task != nil, d.User)
		}
	})
	t.Run("but the OPEN has to be an OPEN", func(t *testing.T) {
		broken := sign.Envelope{Payload: enc.Bytes("x"), KeyID: "k", Sig: enc.Bytes{1}}
		d, cl := Check(context.Background(), f.request(withoutUser(t, coreOpen(core.AppUsers, core.ActTrust, &broken))))
		wantClose(t, d, cl, wire.CodeVerify)
	})
}

// TestCheckOpensUnattendedInstall covers the only action of the contract that
// is both a root task and one the API may open without a user:
// ngc.store/controller/install. Without a certificate no task is checked at
// all, so nothing here asks for the signature a root task otherwise costs —
// what pays for this channel is the standing approval, which the controller
// will check afterwards (core.Unattended). A guard that turned every root
// action away on a channel without a user would shut the unattended install,
// and this is the one channel that would say so.
func TestCheckOpensUnattendedInstall(t *testing.T) {
	f := newFixture(t)
	o := coreOpen(core.AppStore, core.ActInstall, nil)
	o.Params = mustJSON(t, core.InstallParams{Target: testApp, Version: "1.0.0",
		SHA256: bytes.Repeat([]byte{0x01}, 32)})
	d, cl := Check(context.Background(), f.request(withoutUser(t, o)))
	if cl != nil {
		t.Fatalf("CLOSE %+v, want the channel to open", *cl)
	}
	if !d.Core || d.Action.Name != core.ActInstall || !d.Action.Root {
		t.Errorf("action %+v, core %v, want the root action install", d.Action, d.Core)
	}
	if d.User != "" || d.UserName != "" || d.Task != nil || d.Opened.Session != nil {
		t.Errorf("channel without a user carries user %q and task %v", d.User, d.Task != nil)
	}
}

func TestCheckRefusesShellOpenOfStage(t *testing.T) {
	f := newFixture(t)
	o := coreOpen(core.AppStore, core.ActStage, nil)
	o.Params = json.RawMessage(`{"target":"` + testApp + `","version":"1.0.0"}`)
	d, cl := Check(context.Background(), f.request(f.withUser(t, o)))
	wantClose(t, d, cl, wire.CodeForbidden)
}

// TestCheckAppChannel covers the channels of an installed app: the rule comes
// from the manifest of this controller's own database, and the end-to-end
// handshake is what binds the user.
func TestCheckAppChannel(t *testing.T) {
	f := newFixture(t)
	t.Run("the manifest of the installed app decides", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen(testApp, testService, testAction))))
		if cl != nil {
			t.Fatalf("CLOSE %+v, want the channel to open", *cl)
		}
		if d.Core || d.Task != nil {
			t.Errorf("core %v, task %v", d.Core, d.Task != nil)
		}
		if d.User != trusttest.ID(0x21) || d.UserName != "admin" {
			t.Errorf("user %q (%q), want the admin of the list", d.User, d.UserName)
		}
		if d.Manifest == nil || d.Manifest.ID != testApp || d.App.ID != testApp {
			t.Errorf("app %q, manifest %v", d.App.ID, d.Manifest)
		}
	})
	// Three channels whose OPEN asks for nothing special: what they cost is
	// decided by the manifest alone, and each of the three fields of
	// store.RuleFor decides one of them. Without a task every one ends in
	// E_CONFIRM, and the shell asks the user.
	t.Run("critical in the manifest", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen(testApp, adminService, adminAction))))
		wantClose(t, d, cl, wire.CodeConfirm)
	})
	t.Run("the manifest needs root", func(t *testing.T) {
		r := f.request(f.withUser(t, f.appOpen(testApp, testService, testAction)))
		r.Apps = rows(withNeeds(store.NeedRoot))
		d, cl := Check(context.Background(), r)
		wantClose(t, d, cl, wire.CodeConfirm)
	})
	t.Run("every channel changes something", func(t *testing.T) {
		// store.RuleFor says Changes for every app action, and a role at
		// level hardcore has every change confirmed. On a fixture of its
		// own: raising the level of the shared one would decide the other
		// channels here too, and only the order in this file would hide it.
		h := newFixture(t)
		h.list.Roles[0].Level = trust.LevelHardcore
		d, cl := Check(context.Background(), h.request(h.withUser(t, h.appOpen(testApp, testService, testAction))))
		wantClose(t, d, cl, wire.CodeConfirm)
	})
	t.Run("app this controller does not run", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen("org.sorglos.other", testService, testAction))))
		wantClose(t, d, cl, wire.CodeNotFound)
	})
	t.Run("action the manifest does not have", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen(testApp, testService, "delete"))))
		wantClose(t, d, cl, wire.CodeNotFound)
	})
	t.Run("damaged manifest", func(t *testing.T) {
		r := f.request(f.withUser(t, f.appOpen(testApp, testService, testAction)))
		broken := installed()
		app := broken[testApp]
		app.Manifest = []byte(`{"id":`)
		broken[testApp] = app
		r.Apps = broken
		d, cl := Check(context.Background(), r)
		wantClose(t, d, cl, wire.CodeInternal)
	})
	t.Run("without a user", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(withoutUser(t, f.appOpen(testApp, testService, testAction))))
		wantClose(t, d, cl, wire.CodeForbidden)
	})
}

// TestCheckAnswersInternalForItsOwnFaults: in every case here what the opener
// sent is beyond reproach and the fault is this server's. E_NOTFOUND would
// tell the opener that something of theirs was wrong, and E_VERIFY would
// blame their signature.
func TestCheckAnswersInternalForItsOwnFaults(t *testing.T) {
	f := newFixture(t)
	t.Run("the app store cannot answer", func(t *testing.T) {
		r := f.request(f.withUser(t, f.appOpen(testApp, testService, testAction)))
		r.Apps = errApps{}
		d, cl := Check(context.Background(), r)
		wantClose(t, d, cl, wire.CodeInternal)
	})
	t.Run("the row names another app than its manifest", func(t *testing.T) {
		r := f.request(f.withUser(t, f.appOpen("org.sorglos.other", testService, testAction)))
		rows := installed()
		rows["org.sorglos.other"] = dbapi.App{ID: "org.sorglos.other", Version: "1.0.0", Store: trust.StoreGlobal,
			Node: node, Active: true, Manifest: manifestFor(testApp), SHA256: bytes.Repeat([]byte{0x01}, 32)}
		r.Apps = rows
		d, cl := Check(context.Background(), r)
		wantClose(t, d, cl, wire.CodeInternal)
	})
	t.Run("the task store cannot answer", func(t *testing.T) {
		env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
		r := f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env)))
		r.Tasks = errOnce{}
		d, cl := Check(context.Background(), r)
		wantClose(t, d, cl, wire.CodeInternal)
	})
	// A Request without the store is this program wired wrong, and a service
	// running as root may not fall over for it — half wired is the way it
	// would really happen, and each half is reached by a channel of its own:
	// the app store by an app channel, the task store by a signed task.
	for _, missing := range []string{"no store at all", "no app store", "no task store"} {
		t.Run(missing, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("Check panicked on %s: %v", missing, p)
				}
			}()
			r := f.request(f.withUser(t, f.appOpen(testApp, testService, testAction)))
			switch missing {
			case "no store at all":
				r.Apps, r.Tasks = nil, nil
			case "no app store":
				r.Apps = nil
			case "no task store":
				env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
				r = f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env)))
				r.Tasks = nil
			}
			d, cl := Check(context.Background(), r)
			wantClose(t, d, cl, wire.CodeInternal)
		})
	}
}

// TestCheckOpensCriticalActionWithConfirmation is the way through that every
// refusal above has to leave open: the critical wipe of the manifest, on the
// service the manifest gives it, with a task signed by the session key and
// the user's re-confirmation over that task. A gate that lets nothing through
// is no gate.
func TestCheckOpensCriticalActionWithConfirmation(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, testApp, adminService, adminAction, nil)
	o := f.appOpen(testApp, adminService, adminAction)
	o.Task = env
	confirm := f.admin.Prove(sign.PurposeConfirm, env.Payload)
	o.Confirm = &confirm
	d, cl := Check(context.Background(), f.request(f.withUser(t, o)))
	if cl != nil {
		t.Fatalf("CLOSE %+v, want the channel to open", *cl)
	}
	if d.Task == nil || d.User != trusttest.ID(0x21) || d.UserName != "admin" {
		t.Errorf("task %v, user %q (%q)", d.Task != nil, d.User, d.UserName)
	}
	if d.Core || d.Manifest == nil || d.Manifest.ID != testApp {
		t.Errorf("core %v, manifest %v", d.Core, d.Manifest)
	}
	// The channel that opened is the critical one, not some tamer namesake.
	if d.Opened.Open.Service != adminService || d.Opened.Open.Action != adminAction {
		t.Errorf("opened %s/%s", d.Opened.Open.Service, d.Opened.Open.Action)
	}
}

// TestCheckRefusesDeactivatedApp: deactivate is a root task, and a user paid
// a signed task for it. An app this controller does not run answers nothing
// (apps/server-controller/CLAUDE.md), whatever its row still holds.
func TestCheckRefusesDeactivatedApp(t *testing.T) {
	f := newFixture(t)
	r := f.request(f.withUser(t, f.appOpen(testApp, testService, testAction)))
	rows := installed()
	app := rows[testApp]
	app.Active = false
	rows[testApp] = app
	r.Apps = rows
	d, cl := Check(context.Background(), r)
	wantClose(t, d, cl, wire.CodeNotFound)
}

// TestCheckRefusesAppChannelForCoreApp is the channel that carries a
// certificate and proves nothing at all: a row of the database under a core
// app ID. core.Lookup does not know the service, so it falls to the app
// branch, and wire.Open.Validate forbids the end-to-end handshake for a core
// app ID — an accepted channel there would carry a user with nothing that
// binds them to it. What makes an ID a core app ID is the prefix
// (wire.CoreApp), not the three names, so every one of them is asked.
func TestCheckRefusesAppChannelForCoreApp(t *testing.T) {
	f := newFixture(t)
	// ngc.other is no app of this installation and no core app of the
	// contract either: the prefix alone has to be enough.
	for _, app := range []string{core.AppDesktop, core.AppUsers, core.AppStore, "ngc.other"} {
		t.Run(app, func(t *testing.T) {
			r := f.request(f.withUser(t, wire.Open{App: app, Service: testService, Action: testAction,
				Window: wire.DefaultWindow}))
			// The row is there, and its manifest is the one of a real app:
			// only the key it is filed under says ngc.
			rows := installed()
			rows[app] = dbapi.App{ID: app, Version: "1.0.0", Store: trust.StoreGlobal, Node: node,
				Active: true, Manifest: manifestFor(testApp), SHA256: bytes.Repeat([]byte{0x01}, 32)}
			r.Apps = rows
			d, cl := Check(context.Background(), r)
			if cl == nil {
				t.Fatalf("channel accepted for a core app ID: user %q, task %v, handshake %d bytes",
					d.User, d.Task != nil, len(d.Opened.Open.E2E))
			}
			wantClose(t, d, cl, wire.CodeNotFound)
		})
	}
}

// TestCheckRefusesCoreAppIDWithoutACertificate nails the order of the two
// guards the app branch opens with. The core app ID is refused first, which is
// what access.go claims of it: the OPEN ends the same way whether it carries a
// certificate or not, and the answer is E_NOTFOUND both times. Put the guard
// for the missing certificate in front and this channel ends as E_FORBIDDEN
// instead — with every other test of the repo still green.
func TestCheckRefusesCoreAppIDWithoutACertificate(t *testing.T) {
	f := newFixture(t)
	// ngc.users/controller/trust is the channel the API really does open
	// without a user; the same ID on any other service is no app of this
	// controller.
	r := f.request(withoutUser(t, wire.Open{App: core.AppUsers, Service: testService, Action: testAction,
		Window: wire.DefaultWindow}))
	// Nothing is filed under that ID, and nothing needs to be: this channel
	// carries no certificate, so the app branch never reaches the lookup. The
	// E_NOTFOUND below can only be the guard's — without it the channel ends
	// one guard later as E_FORBIDDEN.
	d, cl := Check(context.Background(), r)
	wantClose(t, d, cl, wire.CodeNotFound)
}

// TestCheckRefusesActionOnAnotherService is the escalation the OPEN would buy
// if only the name of the action were matched: run is the manifest's harmless
// action on the service files, admin carries the critical wipe. An OPEN that
// names the service admin and the action run must find nothing, not the
// privileged service under the harmless action's rule.
func TestCheckRefusesActionOnAnotherService(t *testing.T) {
	f := newFixture(t)
	d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen(testApp, adminService, testAction))))
	wantClose(t, d, cl, wire.CodeNotFound)
}

// TestCheckRefusesCoreAPIAction: the actions of service api run in the API,
// never here.
func TestCheckRefusesCoreAPIAction(t *testing.T) {
	f := newFixture(t)
	o := wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe, Window: wire.DefaultWindow}
	d, cl := Check(context.Background(), f.request(f.withUser(t, o)))
	wantClose(t, d, cl, wire.CodeNotFound)
}

// unlisted is a request of a controller that has verified no version of the
// trust list yet: the state a server is in between pairing and its first
// chain.
func (f fixture) unlisted(payload []byte) Request {
	r := f.request(payload)
	r.List = nil
	return r
}

// TestCheckWithoutTrustListTakesOnlyTheFirstChain fences the one exception of
// this gate in all four of its marks. A controller that has verified no
// version can check nothing — and would therefore never get one either, since
// the chain that would give it one arrives on a channel. The channel that
// brings it is let through, and it alone.
//
// Each subtest below is one mark of that exception. Take a mark out of Check
// and the subtest named after it turns red.
func TestCheckWithoutTrustListTakesOnlyTheFirstChain(t *testing.T) {
	f := newFixture(t)

	// Mark 3 and 4: what the decision carries. Nothing of this channel names
	// a user or a task, so nothing downstream can authorise or log one.
	t.Run("the first chain runs and names nobody", func(t *testing.T) {
		d, cl := Check(context.Background(), f.unlisted(withoutUser(t, coreOpen(core.AppUsers, core.ActTrust, nil))))
		if cl != nil {
			t.Fatalf("CLOSE %+v, want the channel that brings the first version to open", *cl)
		}
		if !d.Core || d.Action.App != core.AppUsers || d.Action.Service != core.ServiceController ||
			d.Action.Name != core.ActTrust {
			t.Errorf("action %+v, core %v, want ngc.users/controller/trust", d.Action, d.Core)
		}
		if d.Opened.Session != nil || d.User != "" || d.UserName != "" {
			t.Errorf("the channel names user %q (%q) and carries a session %v, want none of the three",
				d.User, d.UserName, d.Opened.Session != nil)
		}
		if d.Task != nil {
			t.Errorf("the channel carries task %+v, want none: nothing can verify one without a list", *d.Task)
		}
		if d.App.ID != "" || d.Manifest != nil {
			t.Errorf("the channel carries app %q and a manifest %v, want neither", d.App.ID, d.Manifest != nil)
		}
		if d.Opened.Open.App != core.AppUsers || d.Opened.Window != wire.DefaultWindow {
			t.Errorf("the decision carries OPEN %+v with window %d", d.Opened.Open, d.Opened.Window)
		}
	})

	// Mark 2: a certificate. Without a list nothing can check one, and
	// session.VerifyClient would read the users of a list that is not there.
	t.Run("with a certificate", func(t *testing.T) {
		d, cl := Check(context.Background(), f.unlisted(f.withUser(t, coreOpen(core.AppUsers, core.ActTrust, nil))))
		wantClose(t, d, cl, wire.CodeUnreachable)
	})

	// Mark 1: the action. The three other core actions the API may open
	// without a user are none of this exception.
	for _, name := range []string{core.ActStage, core.ActInstall, core.ActLicense} {
		t.Run("another action without a user: "+name, func(t *testing.T) {
			a, ok := core.Lookup(core.AppStore, core.ServiceController, name)
			if !ok || !core.WithoutUser(a) {
				t.Fatalf("%s is not a core action the API opens without a user", name)
			}
			d, cl := Check(context.Background(), f.unlisted(withoutUser(t, coreOpen(core.AppStore, name, nil))))
			wantClose(t, d, cl, wire.CodeUnreachable)
		})
	}

	// Mark 1 again, in its other half: what is no core action at all. The
	// certificate is left off on purpose — with one this channel is already
	// turned away by the mark above and would never reach the lookup, so it
	// would fence nothing here.
	t.Run("an app channel without a certificate", func(t *testing.T) {
		d, cl := Check(context.Background(), f.unlisted(withoutUser(t, f.appOpen(testApp, testService, testAction))))
		wantClose(t, d, cl, wire.CodeUnreachable)
	})
	// And with one it is refused twice over: by the certificate and by the
	// lookup. It fences neither mark on its own and is here for the case
	// itself, which is the one a shell really sends.
	t.Run("an app channel with a certificate", func(t *testing.T) {
		d, cl := Check(context.Background(), f.unlisted(f.withUser(t, f.appOpen(testApp, testService, testAction))))
		wantClose(t, d, cl, wire.CodeUnreachable)
	})
	t.Run("an action of service api", func(t *testing.T) {
		o := wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe, Window: wire.DefaultWindow}
		d, cl := Check(context.Background(), f.unlisted(withoutUser(t, o)))
		wantClose(t, d, cl, wire.CodeUnreachable)
	})

	// An OPEN that cannot be read is not the first chain either. This one
	// fences no mark of its own: firstChain refuses it twice over, once at the
	// parse and once at the lookup, because tunnel.ParseOpen gives back zero
	// values with its error and three empty names are in no core action. A
	// mutation that drops the parse check survives the package for exactly
	// that reason — measured, and written at the check itself. What the
	// subtest holds is the answer, and the fact the equality rests on: the day
	// core.Lookup finds something under three empty names, the check is
	// load-bearing and this line goes red first.
	t.Run("an open that is none", func(t *testing.T) {
		if _, ok := core.Lookup("", "", ""); ok {
			t.Fatal("core.Lookup finds an action under three empty names")
		}
		d, cl := Check(context.Background(), f.unlisted([]byte(`{}`)))
		wantClose(t, d, cl, wire.CodeUnreachable)
	})
}

// TestCheckWithATrustListDecidesAsBefore: the exception is the missing list
// and nothing else. With a list the same two OPENs go the way they always
// went — the one without a certificate through, the one with a certificate
// refused as E_FORBIDDEN, because a shell may not open this action
// (core.ShellOpens). The codes differ from the ones above on purpose: without
// a list nothing can be said about a certificate at all, with one it can.
func TestCheckWithATrustListDecidesAsBefore(t *testing.T) {
	f := newFixture(t)
	if _, cl := Check(context.Background(),
		f.request(withoutUser(t, coreOpen(core.AppUsers, core.ActTrust, nil)))); cl != nil {
		t.Errorf("CLOSE %+v, want the channel to open with a list as it does without one", *cl)
	}
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppUsers, core.ActTrust, nil))))
	wantClose(t, d, cl, wire.CodeForbidden)
}

// TestCheckRefusesMalformedOpen: whatever cannot be read is a failed check.
func TestCheckRefusesMalformedOpen(t *testing.T) {
	f := newFixture(t)
	d, cl := Check(context.Background(), f.request([]byte(`{}`)))
	wantClose(t, d, cl, wire.CodeVerify)
}

// TestOnlyATaskOrAHandshakeCanNameAUser fences the last rule of Check, the
// one no test tells from "if true": with a certificate but neither a task nor
// the end-to-end handshake, a channel names no user. Put "if true" there and
// every test of this package stays green, because one guard per branch keeps
// that case from ever arriving. Give way either one and the rule turns live
// and unwatched, so both are nailed below: the core branch in its two halves,
// the app branch in one.
func TestOnlyATaskOrAHandshakeCanNameAUser(t *testing.T) {
	// A core channel of a user reaches the rule only through core.ShellOpens,
	// and on service controller that leaves the root actions alone.
	for _, a := range core.All() {
		if a.Service == core.ServiceController && core.ShellOpens(a) && !a.Root {
			t.Errorf("%s/%s/%s: a shell may open it without a signed task, and nothing would bind the "+
				"certificate of that channel to its user", a.App, a.Service, a.Name)
		}
	}
	// A root action without a task never gets that far: task.Check asks for
	// one, so tk is never nil on a core channel that is accepted.
	f := newFixture(t)
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, nil))))
	wantClose(t, d, cl, wire.CodeConfirm)
	// An app channel always carries message 1: an OPEN of an app that is no
	// core app and brings none is not even read.
	o := f.appOpen(testApp, testService, testAction)
	o.E2E = nil
	if _, err := wire.ParseOpen(mustJSON(t, o)); err == nil {
		t.Error("an OPEN of an app channel without message 1 parses")
	}
}

// seeds are the payloads FuzzCheck starts from, of this fixture: a second
// one would be the same bytes, but nothing would say so: the accepted channels of the
// tests above, the refused ones, and bare rubbish.
func (f fixture) seeds(t testing.TB) [][]byte {
	env := f.task(t, sessionSeed, node, core.AppStore, core.ServiceController, core.ActActivate, activateParams())
	stage := coreOpen(core.AppStore, core.ActStage, nil)
	stage.Params = json.RawMessage(`{"target":"` + testApp + `","version":"1.0.0"}`)
	return [][]byte{
		f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env)),
		f.withUser(t, f.appOpen(testApp, testService, testAction)),
		withoutUser(t, coreOpen(core.AppUsers, core.ActTrust, nil)),
		withoutUser(t, stage),
		f.withUser(t, coreOpen(core.AppStore, core.ActActivate, nil)),
		f.withUser(t, stage),
		[]byte(`{"open":"","window":4096}`),
		[]byte(`{}`),
		nil,
	}
}

// codes are the refusals this package answers with; no channel may end with
// anything else.
var codes = []wire.Code{wire.CodeUnreachable, wire.CodeVerify, wire.CodeNotFound, wire.CodeForbidden,
	wire.CodeConfirm, wire.CodeInternal}

// canary is what must hold for every answer, whatever came in: a refusal says
// nothing but its code, and an accepted channel never carries a user the OPEN
// did not prove.
func canary(t *testing.T, d Decision, cl *wire.Close) {
	t.Helper()
	if cl != nil {
		if cl.Status != wire.StatusError || !slices.Contains(codes, cl.Code) || cl.Message != cl.Code.Message() {
			t.Fatalf("CLOSE %+v", *cl)
		}
		if !noDecision(d) {
			t.Fatalf("refused channel carries a decision: %+v", d)
		}
		return
	}
	if d.Opened.Session == nil {
		// A channel without a user: only the actions core.WithoutUser lists,
		// and it carries neither a user nor a task.
		if !core.WithoutUser(d.Action) || d.User != "" || d.UserName != "" || d.Task != nil {
			t.Fatalf("accepted without a user: %+v", d)
		}
		return
	}
	// A root task of this controller runs on a signed task or not at all. The
	// one root action the API may open without a user is install, and that
	// channel came back above.
	if d.Core && d.Action.Root && d.Task == nil {
		t.Fatalf("root action without a task: %+v", d)
	}
	if d.User != "" && d.User != d.Opened.Session.Cert().User {
		t.Fatalf("user %q is not the one of the certificate %q", d.User, d.Opened.Session.Cert().User)
	}
	// A user is authorised by the task or by the end-to-end handshake; with
	// neither, the channel carries none.
	if d.User == "" && (d.Task != nil || len(d.Opened.Open.E2E) != 0) {
		t.Fatalf("task or handshake without a user: %+v", d)
	}
	if d.User != "" && d.Task == nil && len(d.Opened.Open.E2E) == 0 {
		t.Fatalf("user without task or handshake: %+v", d)
	}
	if d.Core != (d.Manifest == nil) {
		t.Fatalf("core %v with manifest %v", d.Core, d.Manifest != nil)
	}
}

// TestSeedsReachCheck measures the fixed seeds: the fuzz target is worth
// running only while some of them get through and some do not.
func TestSeedsReachCheck(t *testing.T) {
	f := newFixture(t)
	var open, refused int
	for i, b := range f.seeds(t) {
		d, cl := Check(context.Background(), f.request(b))
		canary(t, d, cl)
		if cl == nil {
			open++
		} else {
			refused++
			t.Logf("seed %d: CLOSE %s", i, cl.Code)
		}
	}
	t.Logf("seeds: %d open, %d refused", open, refused)
	if open == 0 || refused == 0 {
		t.Fatalf("%d of %d seeds open the channel; the target needs both", open, open+refused)
	}
}

func FuzzCheck(f *testing.F) {
	fx := newFixture(f)
	for _, b := range fx.seeds(f) {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d, cl := Check(context.Background(), fx.request(b))
		canary(t, d, cl)
	})
}

// The controller's own store is what fills both interfaces in the running
// program; the compiler says here whether it still does.
var (
	_ AppLookup = (*state.Store)(nil)
	_ TaskOnce  = (*state.Store)(nil)
)
