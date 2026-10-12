package rights

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"

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
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The times of the fixture: Genesis is issued at issued, and every channel
// opens at now, inside the lifetime of certificate and task.
const (
	issued = 1790000000
	now    = issued + 10
)

const (
	roleOffice = "office"
	ctlNode    = "services-1"
	termApp    = "org.sorglos.terminal"
	termSvc    = "terminal"
	termAct    = "shell"
)

// ctx is the context of every call: these tests never cancel one.
var ctx = context.Background()

// manifestJSON is the manifest of the installed test app: one action shell on
// service terminal, nothing critical and no root need, so a channel to it
// needs no task.
const manifestJSON = `{"id":"` + termApp + `","name":"Terminal","version":"1.0.0","publisher":"Sorglos",` +
	`"actions":[{"name":"` + termAct + `","title":"Shell","service":"` + termSvc + `"}],` +
	`"services":[{"name":"` + termSvc + `","command":["/usr/bin/terminal"]}]}`

type fixture struct {
	DB   dbapi.Store
	List *trust.List

	AdminUser  string
	OfficeUser string

	App      *dbapi.App
	Manifest *store.Manifest

	// sess holds one verified session per user, sessKey the session key that
	// signs its tasks and userKey the user's own key, which re-confirms one.
	sess    map[string]session.Verified
	sessKey map[string]ed25519.PrivateKey
	userKey map[string]trusttest.Signer
}

// newFixture builds a trust list with the roles admin and office, a
// controller services-1 on which admins may run root tasks, a session for one
// user of each role and the installed app with its parsed manifest.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	office := trusttest.NewSigner(2)
	officeID := trusttest.ID(0x22)
	// Roles, users and nodes are each ordered by their key (trust.Validate),
	// and admin < office, ID(0x21) < ID(0x22), "ctl-1" < "services-1".
	l.Roles = append(l.Roles, trust.Role{Name: roleOffice, Level: trust.LevelOff})
	l.Users = append(l.Users, trust.User{ID: officeID, Name: roleOffice,
		Roles: []string{roleOffice}, Keys: []proof.UserKey{office.Key}})
	ctlDH, _ := trusttest.NodeKeys(0xC2)
	l.Nodes = append(l.Nodes, trust.Node{ID: ctlNode, Kind: trust.NodeController, Name: "Dienste 1",
		Noise: ctlDH.Public, RootRoles: []string{trust.RoleAdmin}})
	if err := l.Validate(); err != nil {
		t.Fatalf("trust list: %v", err)
	}
	m, err := store.ParseManifest([]byte(manifestJSON))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	fx := &fixture{
		DB: memstore.New(), List: l, AdminUser: l.Users[1].ID, OfficeUser: officeID,
		App: &dbapi.App{ID: termApp, Version: "1.0.0", Store: trust.StoreGlobal, Node: ctlNode,
			Active: true, Manifest: []byte(manifestJSON)},
		Manifest: m,
		sess:     map[string]session.Verified{}, sessKey: map[string]ed25519.PrivateKey{},
		userKey: map[string]trusttest.Signer{},
	}
	fx.login(t, l.Users[1].ID, admin, 0x51)
	fx.login(t, officeID, office, 0x52)
	return fx
}

// login makes the verified session of user, signed by that user's key signer.
func (fx *fixture) login(t *testing.T, user string, signer trusttest.Signer, seed byte) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	dh, _ := trusttest.NodeKeys(seed ^ 0xFF)
	client, err := json.Marshal(session.ClientCert{Installation: fx.List.Installation, User: user,
		Nonce: bytes.Repeat([]byte{seed}, session.NonceSize), Sig: enc.Bytes(priv.Public().(ed25519.PublicKey)),
		DH: dh.Public, Issued: issued, Expires: issued + 3600})
	if err != nil {
		t.Fatal(err)
	}
	v, err := session.VerifyClient(client, signer.Prove(sign.PurposeClientCert, client), fx.List, now)
	if err != nil {
		t.Fatalf("login %s: %v", user, err)
	}
	fx.sess[user], fx.sessKey[user], fx.userKey[user] = v, priv, signer
}

// setLevel puts role on a re-confirmation level, so a test can ask for a
// channel that needs a signed task and the user's re-confirmation.
func (fx *fixture) setLevel(t *testing.T, role string, level trust.Level) {
	t.Helper()
	for i := range fx.List.Roles {
		if fx.List.Roles[i].Name == role {
			fx.List.Roles[i].Level = level
			return
		}
	}
	t.Fatalf("no role %q", role)
}

// confirm is the user's re-confirmation of the task in env, by the user's own
// key, as task.VerifyConfirm checks it.
func (fx *fixture) confirm(t *testing.T, user string, env *sign.Envelope) *proof.Proof {
	t.Helper()
	p := fx.userKey[user].Prove(sign.PurposeConfirm, env.Payload)
	return &p
}

// appTask returns the signed task of the app channel termApp/termSvc/action on
// services-1, by user. An app channel's task carries no params: store.RuleFor
// reads the manifest, not the task.
func (fx *fixture) appTask(t *testing.T, user, action string) *sign.Envelope {
	t.Helper()
	return fx.signTask(t, user, task.Task{ID: bytes.Repeat([]byte{0x63}, task.IDSize),
		Node: ctlNode, App: termApp, Service: termSvc, Action: action})
}

// session returns the verified session of user.
func (fx *fixture) session(t *testing.T, user string) *session.Verified {
	t.Helper()
	v, ok := fx.sess[user]
	if !ok {
		t.Fatalf("no session for %s", user)
	}
	return &v
}

// DeactivateTask returns the signed root task
// ngc.store/controller/deactivate of the test app on services-1, signed with
// the admin's session key. Its ID is fixed, so two calls are the same task
// and the second is a replay.
func (fx *fixture) DeactivateTask(t *testing.T) *sign.Envelope {
	t.Helper()
	return fx.signTask(t, fx.AdminUser, task.Task{ID: bytes.Repeat([]byte{0x61}, task.IDSize),
		Node: ctlNode, App: core.AppStore, Service: core.ServiceController, Action: core.ActDeactivate,
		Params: json.RawMessage(`{"app":"` + termApp + `"}`)})
}

// signTask fills in what every task of this fixture shares — installation,
// certificate and lifetime — and signs it with the session key of user.
func (fx *fixture) signTask(t *testing.T, user string, tk task.Task) *sign.Envelope {
	t.Helper()
	tk.Installation = fx.List.Installation
	tk.Cert = session.CertHash(fx.session(t, user).Payload())
	tk.Issued, tk.Expires = issued, issued+60
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(fx.sessKey[user], sign.PurposeTask, b)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// nodeOf is the node the API passes for a core action: a controller action
// runs on services-1, an api action on the API itself.
func nodeOf(a core.Action) string {
	if a.Service == core.ServiceController {
		return ctlNode
	}
	return ""
}

// checkAs asks Check for the core action a as user, with the signed task tk
// or none.
func checkAs(t *testing.T, fx *fixture, user string, a core.Action, tk *sign.Envelope) wire.Code {
	t.Helper()
	_, code := Check(ctx, fx.DB, fx.List, Request{Action: a, Node: nodeOf(a), Session: fx.session(t, user),
		Open: wire.Open{App: a.App, Service: a.Service, Action: a.Name, Window: wire.MinWindow, Task: tk}}, now)
	return code
}

// CheckApp asks Check for the action of the installed app as user.
func (fx *fixture) CheckApp(t *testing.T, user, action string) wire.Code {
	t.Helper()
	_, code := fx.checkAppOpen(t, user, wire.Open{App: termApp, Service: termSvc, Action: action, Window: wire.MinWindow})
	return code
}

// checkAppOpen asks Check for an app channel with exactly this OPEN, so a
// test can vary the OPEN and nothing else.
func (fx *fixture) checkAppOpen(t *testing.T, user string, open wire.Open) (*task.Task, wire.Code) {
	t.Helper()
	return Check(ctx, fx.DB, fx.List, Request{App: fx.App, Manifest: fx.Manifest, Node: ctlNode,
		Session: fx.session(t, user), Open: open}, now)
}
