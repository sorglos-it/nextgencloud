// Who may open a terminal, and what he has to bring - measured, not claimed.
//
// A terminal is the most dangerous service of this system: whoever opens one
// runs commands as root inside the container of the app. This program cannot
// decide that question - it runs inside that container and gets what the
// controller hands it. The gate is package access on the server, and it judges
// by the manifest of the app. So the measurement here puts the manifest THAT
// IS SHIPPED (templates.TerminalManifest, embedded, so it is the file and not
// a copy) in front of that gate and asks, one attacker at a time, what he
// reaches.
//
// Every refusal below is worth nothing without the way through that the first
// case measures: a gate that lets nothing in would make all eleven refusals
// green. That case is the intermediate check this measurement needs - it
// proves the path is open as far as the line each refusal is about.
package terminal

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/templates"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The app this package is the service of, as the manifest names it.
const (
	appID     = "org.sorglos.terminal"
	svcName   = "terminal"
	actName   = "shell"
	node      = "ctl-1" // this controller; the admins of the fixture may run root tasks here
	otherNode = "ctl-2" // a second controller where nobody may
	issued    = 1790000000
	now       = issued + 10
	certLife  = 3600
)

// sessionSeed makes the key of the client certificate, which is the key that
// signs a task. strangerSeed is a key of the same kind that no certificate
// names.
var (
	sessionSeed  = bytes.Repeat([]byte{0x51}, ed25519.SeedSize)
	strangerSeed = bytes.Repeat([]byte{0x59}, ed25519.SeedSize)
)

// TestManifestMatchesTheContract keeps the shipped file from ever falling out
// of line with the contract it is read by.
func TestManifestMatchesTheContract(t *testing.T) {
	m, err := store.ParseManifest(templates.TerminalManifest)
	if err != nil {
		t.Fatalf("store.ParseManifest: %v", err)
	}
	if m.ID != appID || m.Runtime != store.RuntimePodman {
		t.Errorf("id %q, runtime %q", m.ID, m.Runtime)
	}
	a, ok := m.Action(actName)
	if !ok {
		t.Fatalf("the manifest has no action %q", actName)
	}
	// Three properties of this one action, and each is a decision of this
	// task: it streams (no time limit), it is critical (the user confirms),
	// and it runs on the service this package is.
	if !a.Stream || !a.Critical || a.Service != svcName {
		t.Errorf("action %+v", a)
	}
	if len(m.Needs) != 1 || m.Needs[0] != store.NeedRoot {
		t.Errorf("needs %v, want root alone - the network is not among them", m.Needs)
	}
	i := -1
	for k, s := range m.Services {
		if s.Name == svcName {
			i = k
		}
	}
	if i < 0 {
		t.Fatalf("the manifest has no service %q", svcName)
	}
	// The command is the program of this repository with this command, and
	// nothing else: a shell around it, or an argument of its own, would be a
	// way into the container that the manifest hands out.
	want := []string{"/usr/lib/ngc/ngc-controller", "terminal"}
	if len(m.Services[i].Command) != len(want) {
		t.Fatalf("command %v, want %v", m.Services[i].Command, want)
	}
	for k := range want {
		if m.Services[i].Command[k] != want[k] {
			t.Fatalf("command %v, want %v", m.Services[i].Command, want)
		}
	}
}

// TestRuleOfTheShellIsRootAndCritical reads the rule the gate judges by off
// that manifest. It is the reason every case below needs a signed task and a
// re-confirmation.
func TestRuleOfTheShellIsRootAndCritical(t *testing.T) {
	m, err := store.ParseManifest(templates.TerminalManifest)
	if err != nil {
		t.Fatal(err)
	}
	r, err := store.RuleFor(m, actName)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Root || !r.Changes {
		t.Errorf("root %v, changes %v", r.Root, r.Changes)
	}
	// Critical whatever the params, including none: an action that only some
	// params make critical would be opened by an unsigned OPEN without one.
	for _, p := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		critical, err := r.Critical(p)
		if err != nil || !critical {
			t.Errorf("params %s: critical %v, %v", p, critical, err)
		}
	}
}

// gate is one logged-in admin in front of the gate of this controller.
type gate struct {
	list      *trust.List
	admin     trusttest.Signer
	client    []byte
	certProof proof.Proof
	e2e       []byte
	certEnds  int64
}

func newGate(t testing.TB) gate { return newGateUntil(t, issued+certLife) }

// newGateUntil builds the list of trusttest.Genesis with a second controller
// nobody may run root tasks on, and logs the admin in with a certificate that
// runs out at ends.
func newGateUntil(t testing.TB, ends int64) gate {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	otherDH, _ := trusttest.NodeKeys(0xC2)
	l.Nodes = append(l.Nodes, trust.Node{ID: otherNode, Kind: trust.NodeController, Name: "Ohne Root", Noise: otherDH.Public})
	if err := l.Validate(); err != nil {
		t.Fatalf("test trust list: %v", err)
	}
	sessSig := ed25519.NewKeyFromSeed(sessionSeed)
	sessDH, _ := trusttest.NodeKeys(0x52)
	client := mustJSON(t, session.ClientCert{Installation: l.Installation, User: trusttest.ID(0x21),
		Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize), Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)),
		DH: sessDH.Public, Issued: issued, Expires: ends})
	return gate{list: l, admin: admin, client: client, certEnds: ends,
		certProof: admin.Prove(sign.PurposeClientCert, client),
		e2e:       bytes.Repeat([]byte{0x54}, wire.HandshakeSize)}
}

// request is the OPEN as a request of this controller, with the terminal app
// installed and its shipped manifest in the row.
func (g gate) request(payload []byte, on string, at int64) access.Request {
	return access.Request{Payload: payload, List: g.list, Node: on, Now: at,
		Apps: apps{appID: {ID: appID, Version: "1.0.0", Store: trust.StoreGlobal, Node: on, Active: true,
			Manifest: templates.TerminalManifest, SHA256: bytes.Repeat([]byte{0x01}, 32)}},
		Tasks: &once{}}
}

// withUser wraps the OPEN the way the API forwards a browser channel: with the
// client certificate and the user's proof over it.
func (g gate) withUser(t testing.TB, o wire.Open) []byte {
	t.Helper()
	return mustJSON(t, tunnel.Open{Open: mustJSON(t, o), Window: wire.DefaultWindow, Client: g.client, Proof: &g.certProof})
}

// open is the OPEN of the shell. An app channel always carries message 1 of
// the end-to-end handshake.
func (g gate) open() wire.Open {
	return wire.Open{App: appID, Service: svcName, Action: actName, Window: wire.DefaultWindow, E2E: g.e2e}
}

// task is a task of this admin for an app, service and action on the
// controller on, signed by whoever holds seed and valid until exp.
func (g gate) task(t testing.TB, seed []byte, on, app, svc, act string, exp int64) *sign.Envelope {
	t.Helper()
	payload := mustJSON(t, task.Task{ID: bytes.Repeat([]byte{0x61}, task.IDSize), Installation: g.list.Installation,
		Cert: session.CertHash(g.client), Node: on, App: app, Service: svc, Action: act,
		Issued: issued, Expires: exp})
	env, err := sign.Sign(ed25519.NewKeyFromSeed(seed), sign.PurposeTask, payload)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// signed is the whole OPEN of an admin who brings everything: the task for
// this node and his re-confirmation over it.
func (g gate) signed(t testing.TB, on string) []byte {
	return g.signedUntil(t, on, issued+60)
}

// signedUntil is that OPEN with a task of its own lifetime, so a case can tell
// the time of the task from the time of the login.
func (g gate) signedUntil(t testing.TB, on string, exp int64) []byte {
	t.Helper()
	env := g.task(t, sessionSeed, on, appID, svcName, actName, exp)
	o := g.open()
	o.Task = env
	c := g.admin.Prove(sign.PurposeConfirm, env.Payload)
	o.Confirm = &c
	return g.withUser(t, o)
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

// once remembers task IDs, so a task that has run cannot open a second
// channel.
type once struct{ seen map[string]bool }

func (o *once) UseTask(_ context.Context, id []byte, _, _ int64) error {
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	if o.seen[string(id)] {
		return dbapi.ErrUsed
	}
	o.seen[string(id)] = true
	return nil
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAnAdminWithRootOpensTheShell is the way through, and it is what makes
// every refusal below mean something: it proves the path to each of those
// lines is open.
func TestAnAdminWithRootOpensTheShell(t *testing.T) {
	g := newGate(t)
	d, cl := access.Check(context.Background(), g.request(g.signed(t, node), node, now))
	if cl != nil {
		t.Fatalf("CLOSE %+v, want the shell to open", *cl)
	}
	if d.Task == nil || d.User != trusttest.ID(0x21) {
		t.Fatalf("task %v, user %q", d.Task != nil, d.User)
	}
	// It is the shell of this app that opened, not a tamer namesake.
	if d.Opened.Open.App != appID || d.Opened.Open.Service != svcName || d.Opened.Open.Action != actName {
		t.Fatalf("opened %s %s/%s", d.Opened.Open.App, d.Opened.Open.Service, d.Opened.Open.Action)
	}
	// And the service it hands over to is the one this package is, under the
	// user it proved.
	call := service.Call{User: d.User, UserName: d.UserName, App: d.App.ID, Service: svcName,
		Action: actName, Version: d.App.Version, Params: d.Params()}
	if err := call.Validate(); err != nil {
		t.Fatalf("the call this would start is invalid: %v", err)
	}
}

// TestWhoCannotOpenTheShell: one attacker per case, each measured on its own.
func TestWhoCannotOpenTheShell(t *testing.T) {
	g := newGate(t)
	cases := []struct {
		name    string
		payload func(t *testing.T) []byte
		on      string
		at      int64
		want    wire.Code
	}{
		{
			// A channel the API opened on its own: no certificate, so nothing
			// says who is behind it. Nothing of this app may run on one.
			name: "ein Kanal ohne Benutzer",
			payload: func(t *testing.T) []byte {
				return mustJSON(t, tunnel.Open{Open: mustJSON(t, g.open()), Window: wire.DefaultWindow})
			},
			want: wire.CodeForbidden,
		},
		{
			// A signed-in user who brings no signed task. The manifest needs
			// root, so a task is required and the shell asks him for one.
			name:    "ein Benutzer ohne unterschriebene Aufgabe",
			payload: func(t *testing.T) []byte { return g.withUser(t, g.open()) },
			want:    wire.CodeConfirm,
		},
		{
			// Task but no re-confirmation: the action is critical.
			name: "eine Aufgabe ohne Bestätigung",
			payload: func(t *testing.T) []byte {
				o := g.open()
				o.Task = g.task(t, sessionSeed, node, appID, svcName, actName, issued+60)
				return g.withUser(t, o)
			},
			want: wire.CodeConfirm,
		},
		{
			// Everything right, on a controller where no role of this user
			// may run a root task. This is the one case that says "no right
			// HERE" instead of "nothing signed".
			name:    "ein Benutzer ohne Root-Recht auf diesem Server",
			payload: func(t *testing.T) []byte { return g.signed(t, otherNode) },
			on:      otherNode,
			want:    wire.CodeForbidden,
		},
		{
			// A task made out for another app: a user who was allowed a
			// terminal somewhere else, or something else here, cannot turn it
			// into this one.
			name: "eine Aufgabe für eine andere App",
			payload: func(t *testing.T) []byte {
				env := g.task(t, sessionSeed, node, "org.sorglos.other", svcName, actName, issued+60)
				o := g.open()
				o.Task = env
				c := g.admin.Prove(sign.PurposeConfirm, env.Payload)
				o.Confirm = &c
				return g.withUser(t, o)
			},
			want: wire.CodeVerify,
		},
		{
			name: "eine Aufgabe für eine andere Aktion derselben App",
			payload: func(t *testing.T) []byte {
				env := g.task(t, sessionSeed, node, appID, svcName, "stop", issued+60)
				o := g.open()
				o.Task = env
				c := g.admin.Prove(sign.PurposeConfirm, env.Payload)
				o.Confirm = &c
				return g.withUser(t, o)
			},
			want: wire.CodeVerify,
		},
		{
			name: "eine Aufgabe für einen anderen Server",
			payload: func(t *testing.T) []byte {
				env := g.task(t, sessionSeed, otherNode, appID, svcName, actName, issued+60)
				o := g.open()
				o.Task = env
				c := g.admin.Prove(sign.PurposeConfirm, env.Payload)
				o.Confirm = &c
				return g.withUser(t, o)
			},
			want: wire.CodeVerify,
		},
		{
			// A key no certificate names. It signs a task that is right in
			// every field.
			name: "eine Aufgabe mit fremder Unterschrift",
			payload: func(t *testing.T) []byte {
				env := g.task(t, strangerSeed, node, appID, svcName, actName, issued+60)
				o := g.open()
				o.Task = env
				c := g.admin.Prove(sign.PurposeConfirm, env.Payload)
				o.Confirm = &c
				return g.withUser(t, o)
			},
			want: wire.CodeVerify,
		},
		{
			// The task ran out while the certificate is still good, so this
			// measures the task's own time and not the certificate's.
			name:    "eine abgelaufene Aufgabe",
			payload: func(t *testing.T) []byte { return g.signed(t, node) },
			at:      issued + 61,
			want:    wire.CodeVerify,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			on, at := c.on, c.at
			if on == "" {
				on = node
			}
			if at == 0 {
				at = now
			}
			d, cl := access.Check(context.Background(), g.request(c.payload(t), on, at))
			if cl == nil {
				t.Fatalf("the shell opened for %q (user %q)", c.name, d.User)
			}
			if cl.Code != c.want {
				t.Fatalf("code %q, want %q", cl.Code, c.want)
			}
		})
	}
}

// TestAnExpiredLoginOpensNothing is its own case, because it needs a
// certificate that runs out before the task does: with one fixture the two
// times would run out together and the measurement would not say which of them
// shut the gate.
func TestAnExpiredLoginOpensNothing(t *testing.T) {
	g := newGateUntil(t, issued+120)
	// The intermediate check: with the same login, one second before it runs
	// out, the shell opens. Without this line a gate that refuses everything
	// would make the case below green.
	long := g.signedUntil(t, node, issued+task.MaxLifetime)
	if _, cl := access.Check(context.Background(), g.request(long, node, g.certEnds-1)); cl != nil {
		t.Fatalf("CLOSE %+v one second before the login ran out", *cl)
	}
	// One second later, with a task that is still inside its own lifetime at
	// that moment, so what shuts the gate here is the login and nothing else.
	d, cl := access.Check(context.Background(), g.request(long, node, g.certEnds))
	if cl == nil {
		t.Fatalf("the shell opened on an expired login (user %q)", d.User)
	}
	if cl.Code != wire.CodeVerify {
		t.Fatalf("code %q, want %q", cl.Code, wire.CodeVerify)
	}
}

// TestTheSameTaskOpensOnlyOneShell: a task runs once. The first channel is the
// intermediate check, the second is the measurement.
func TestTheSameTaskOpensOnlyOneShell(t *testing.T) {
	g := newGate(t)
	r := g.request(g.signed(t, node), node, now)
	if _, cl := access.Check(context.Background(), r); cl != nil {
		t.Fatalf("CLOSE %+v on the first channel", *cl)
	}
	// The same request again, which is the same task on the same store.
	d, cl := access.Check(context.Background(), r)
	if cl == nil {
		t.Fatalf("the task opened a second shell (user %q)", d.User)
	}
	if cl.Code != wire.CodeVerify {
		t.Fatalf("code %q, want %q", cl.Code, wire.CodeVerify)
	}
}
