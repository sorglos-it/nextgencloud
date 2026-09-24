package access

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"slices"
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
	// testApp is the app the fixture has installed, with the one action run.
	testApp    = "org.sorglos.terminal"
	testAction = "run"

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
	client    []byte // the client certificate payload, exactly as the proof covers it
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
	return fixture{list: l, client: client, certProof: admin.Prove(sign.PurposeClientCert, client),
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
func (f fixture) appOpen(app, action string) wire.Open {
	return wire.Open{App: app, Service: "files", Action: action, Window: wire.DefaultWindow, E2E: f.e2e}
}

// task returns a task of this admin for an action on the controller on,
// signed by whoever holds seed. The session key of the certificate comes from
// sessionSeed; any other seed is a stranger's key.
func (f fixture) task(t testing.TB, seed []byte, on, app, action string, params any) *sign.Envelope {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		raw = mustJSON(t, params)
	}
	payload := mustJSON(t, task.Task{ID: bytes.Repeat([]byte{0x61}, task.IDSize), Installation: f.list.Installation,
		Cert: session.CertHash(f.client), Node: on, App: app, Service: core.ServiceController, Action: action,
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

// manifest is the manifest of the installed app: one service, the one action
// run, and no needs, so store.RuleFor gives a rule that is neither root nor
// critical.
func manifest() []byte {
	return []byte(`{"id":"` + testApp + `","name":"Terminal","version":"1.0.0","publisher":"Sorglos",` +
		`"services":[{"name":"files","command":["/bin/terminal"]}],` +
		`"actions":[{"name":"` + testAction + `","title":"Ausführen","service":"files"}]}`)
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
	env := f.task(t, sessionSeed, node, core.AppStore, core.ActActivate, activateParams())
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
	env := f.task(t, strangerSeed, node, core.AppStore, core.ActActivate, activateParams())
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env))))
	wantClose(t, d, cl, wire.CodeVerify)
}

func TestCheckRefusesSwappedTask(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, core.AppStore, core.ActDeactivate, core.AppParams{App: testApp})
	d, cl := Check(context.Background(), f.request(f.withUser(t, coreOpen(core.AppStore, core.ActUninstall, env))))
	wantClose(t, d, cl, wire.CodeVerify)
}

func TestCheckRefusesReplayedTask(t *testing.T) {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, core.AppStore, core.ActActivate, activateParams())
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
	env := f.task(t, sessionSeed, node, core.AppStore, core.ActActivate, activateParams())
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
	env := f.task(t, sessionSeed, otherNode, core.AppStore, core.ActActivate, activateParams())
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
	env := f.task(t, sessionSeed, node, core.AppStore, core.ActActivate, activateParams())
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
		env := f.task(t, sessionSeed, node, core.AppStore, core.ActActivate, activateParams())
		d, cl := Check(context.Background(), f.request(withoutUser(t, coreOpen(core.AppStore, core.ActActivate, env))))
		wantClose(t, d, cl, wire.CodeForbidden)
	})
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
		d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen(testApp, testAction))))
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
	t.Run("app this controller does not run", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen("org.sorglos.other", testAction))))
		wantClose(t, d, cl, wire.CodeNotFound)
	})
	t.Run("action the manifest does not have", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(f.withUser(t, f.appOpen(testApp, "delete"))))
		wantClose(t, d, cl, wire.CodeNotFound)
	})
	t.Run("damaged manifest", func(t *testing.T) {
		r := f.request(f.withUser(t, f.appOpen(testApp, testAction)))
		broken := installed()
		app := broken[testApp]
		app.Manifest = []byte(`{"id":`)
		broken[testApp] = app
		r.Apps = broken
		d, cl := Check(context.Background(), r)
		wantClose(t, d, cl, wire.CodeInternal)
	})
	t.Run("without a user", func(t *testing.T) {
		d, cl := Check(context.Background(), f.request(withoutUser(t, f.appOpen(testApp, testAction))))
		wantClose(t, d, cl, wire.CodeForbidden)
	})
}

// TestCheckRefusesCoreAPIAction: the actions of service api run in the API,
// never here.
func TestCheckRefusesCoreAPIAction(t *testing.T) {
	f := newFixture(t)
	o := wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe, Window: wire.DefaultWindow}
	d, cl := Check(context.Background(), f.request(f.withUser(t, o)))
	wantClose(t, d, cl, wire.CodeNotFound)
}

// TestCheckWithoutTrustList: a controller that has verified no list can check
// nothing, and must not fall back to letting a channel without a user through.
func TestCheckWithoutTrustList(t *testing.T) {
	f := newFixture(t)
	r := f.request(withoutUser(t, coreOpen(core.AppUsers, core.ActTrust, nil)))
	r.List = nil
	d, cl := Check(context.Background(), r)
	wantClose(t, d, cl, wire.CodeUnreachable)
}

// TestCheckRefusesMalformedOpen: whatever cannot be read is a failed check.
func TestCheckRefusesMalformedOpen(t *testing.T) {
	f := newFixture(t)
	d, cl := Check(context.Background(), f.request([]byte(`{}`)))
	wantClose(t, d, cl, wire.CodeVerify)
}

// seeds are the payloads FuzzCheck starts from: the accepted channels of the
// tests above, the refused ones, and bare rubbish.
func seeds(t testing.TB) [][]byte {
	f := newFixture(t)
	env := f.task(t, sessionSeed, node, core.AppStore, core.ActActivate, activateParams())
	stage := coreOpen(core.AppStore, core.ActStage, nil)
	stage.Params = json.RawMessage(`{"target":"` + testApp + `","version":"1.0.0"}`)
	return [][]byte{
		f.withUser(t, coreOpen(core.AppStore, core.ActActivate, env)),
		f.withUser(t, f.appOpen(testApp, testAction)),
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
	for i, b := range seeds(t) {
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
	for _, b := range seeds(f) {
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
