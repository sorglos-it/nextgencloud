package task

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"runtime"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

const (
	issued = 1790000000
	now    = issued + 10
)

var sessionSeed = bytes.Repeat([]byte{0x51}, ed25519.SeedSize)

type fixture struct {
	list     *trust.List
	admin    trusttest.Signer
	sessSig  ed25519.PrivateKey
	verified session.Verified
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	sessSig := ed25519.NewKeyFromSeed(sessionSeed)
	sessDH, _ := trusttest.NodeKeys(0x52)
	cert := session.ClientCert{Installation: l.Installation, User: l.Users[1].ID, Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize),
		Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)), DH: sessDH.Public, Issued: issued, Expires: issued + 3600}
	client, err := json.Marshal(cert)
	if err != nil {
		t.Fatal(err)
	}
	v, err := session.VerifyClient(client, admin.Prove(sign.PurposeClientCert, client), l, now)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{list: l, admin: admin, sessSig: sessSig, verified: v}
}

// activateParams returns valid params of ngc.store/controller/activate with
// extra members appended, such as grants.
func activateParams(extra string) json.RawMessage {
	sum := enc.B64.EncodeToString(bytes.Repeat([]byte{0x01}, 32))
	return json.RawMessage(`{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"` + trust.StoreGlobal + `"` + extra + `}`)
}

func (f fixture) task() Task {
	return Task{ID: bytes.Repeat([]byte{0x61}, IDSize), Installation: f.list.Installation, Cert: session.CertHash(f.verified.Payload()),
		Node: "ctl-1", App: "ngc.store", Service: "controller", Action: "activate",
		Params: activateParams(""), Issued: issued, Expires: issued + 60}
}

// grantRule mimics core's rule for ngc.store/controller/activate: a root task
// that is critical once it grants the app something. Like core, it refuses
// params it cannot read, and no params at all.
var grantRule = Rule{Root: true, Changes: true, Critical: func(params json.RawMessage) (bool, error) {
	var p struct {
		Grants []string `json:"grants"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return false, err
	}
	return len(p.Grants) > 0, nil
}}

// appRule is the rule of an app channel whose manifest asks for nothing
// special.
var appRule = Rule{Changes: true, Critical: func(json.RawMessage) (bool, error) { return false, nil }}

func signTask(t testing.TB, priv ed25519.PrivateKey, tk Task) sign.Envelope {
	t.Helper()
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, sign.PurposeTask, b)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func (f fixture) input(env *sign.Envelope) Input {
	return Input{Task: env, Session: f.verified, Node: "ctl-1", App: "ngc.store", Service: "controller",
		Action: "activate", Rule: grantRule}
}

func TestCheck(t *testing.T) {
	f := newFixture(t)
	tk := f.task()
	env := signTask(t, f.sessSig, tk)
	got, err := Check(f.input(&env), f.list, now)
	if err != nil || got == nil || got.Action != "activate" || !bytes.Equal(got.Params, tk.Params) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestValidate(t *testing.T) {
	f := newFixture(t)
	base := f.task()
	if err := base.Validate(); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(tk *Task)
	}{
		{"short id", func(tk *Task) { tk.ID = tk.ID[:IDSize-1] }},
		{"bad installation", func(tk *Task) { tk.Installation = "" }},
		{"bad certificate hash", func(tk *Task) { tk.Cert = tk.Cert[:len(tk.Cert)-1] }},
		{"non-empty invalid node", func(tk *Task) { tk.Node = "Ctl 1" }},
		{"bad app id", func(tk *Task) { tk.App = "Terminal" }},
		{"bad service name", func(tk *Task) { tk.Service = "Not-Valid" }},
		{"bad action name", func(tk *Task) { tk.Action = "Not-Valid" }},
		{"params not an object", func(tk *Task) { tk.Params = json.RawMessage(`[1,2,3]`) }},
		{"issued zero", func(tk *Task) { tk.Issued = 0 }},
		{"expires equals issued", func(tk *Task) { tk.Expires = tk.Issued }},
	}
	for _, c := range cases {
		tk := base
		c.mutate(&tk)
		if err := tk.Validate(); err != ErrTask {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrTask)
		}
	}
}

func TestCheckRejects(t *testing.T) {
	f := newFixture(t)
	env := signTask(t, f.sessSig, f.task())
	_, stranger := trusttest.NodeKeys(0x77)
	foreign := signTask(t, stranger, f.task())
	long := f.task()
	long.Expires = long.Issued + MaxLifetime + 1
	longEnv := signTask(t, f.sessSig, long)
	// A second, legitimately verified session bound to different certificate
	// bytes (only the nonce differs) but the same user and session key, so
	// only the task's Cert-hash binding is wrong, nothing else about it.
	otherClient, err := json.Marshal(session.ClientCert{Installation: f.list.Installation, User: f.list.Users[1].ID,
		Nonce: bytes.Repeat([]byte{0x54}, session.NonceSize), Sig: enc.Bytes(f.sessSig.Public().(ed25519.PublicKey)),
		DH: f.verified.Cert().DH, Issued: issued, Expires: issued + 3600})
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := session.VerifyClient(otherClient, f.admin.Prove(sign.PurposeClientCert, otherClient), f.list, now)
	if err != nil {
		t.Fatal(err)
	}
	// A string where grantRule expects a list: valid params for Task.Validate
	// that only the rule refuses.
	unreadable := f.task()
	unreadable.Params = activateParams(`,"grants":"network"`)
	unreadableEnv := signTask(t, f.sessSig, unreadable)
	cases := []struct {
		name   string
		mutate func(in *Input, now *int64)
		want   error
	}{
		{"root task without task", func(in *Input, _ *int64) { in.Task = nil }, ErrNoTask},
		{"other node", func(in *Input, _ *int64) { in.Node = "ctl-2" }, ErrTask},
		{"other action", func(in *Input, _ *int64) { in.Action = "deactivate" }, ErrTask},
		{"expired", func(_ *Input, now *int64) { *now = issued + 60 }, ErrExpired},
		{"too long", func(in *Input, _ *int64) { in.Task = &longEnv }, ErrTask},
		{"other session key", func(in *Input, _ *int64) { in.Task = &foreign }, sign.ErrWrongKey},
		{"other client certificate", func(in *Input, _ *int64) { in.Session = otherSession }, ErrTask},
		{"params the rule rejects", func(in *Input, _ *int64) { in.Task = &unreadableEnv }, ErrTask},
		{"zero rule", func(in *Input, _ *int64) { in.Rule = Rule{} }, ErrTask},
		// An action that cannot judge an OPEN without signed params refuses it.
		{"no task, the rule refuses nil params", func(in *Input, _ *int64) {
			in.Task = nil
			in.Rule = Rule{Changes: true, Critical: func(params json.RawMessage) (bool, error) {
				if params == nil {
					return false, errors.New("params must be signed")
				}
				return false, nil
			}}
		}, ErrTask},
	}
	for _, c := range cases {
		in, n := f.input(&env), int64(now)
		c.mutate(&in, &n)
		if _, err := Check(in, f.list, n); err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	noRoot := trusttest.Genesis(f.admin)
	noRoot.Nodes[1].RootRoles = nil
	if _, err := Check(f.input(&env), noRoot, now); err != ErrForbidden {
		t.Errorf("no root permission: %v", err)
	}
}

func TestConfirm(t *testing.T) {
	f := newFixture(t)
	f.list.Roles[0].Level = trust.LevelRoot
	env := signTask(t, f.sessSig, f.task())
	in := f.input(&env)
	if _, err := Check(in, f.list, now); err != ErrConfirm {
		t.Errorf("missing confirmation: %v", err)
	}
	good := f.admin.Prove(sign.PurposeConfirm, env.Payload)
	in.Confirm = &good
	if _, err := Check(in, f.list, now); err != nil {
		t.Errorf("confirmed: %v", err)
	}
	// admin2's key is genuinely in the trust list, but she is not the user
	// who owns this task's session (f.admin, Users[1]): still ErrConfirm.
	bad := trusttest.NewPasskey(trusttest.GenesisPasskeyFill).Prove(sign.PurposeConfirm, env.Payload)
	in.Confirm = &bad
	if _, err := Check(in, f.list, now); err != ErrConfirm {
		t.Errorf("foreign confirmation: %v", err)
	}
	wrong := f.admin.Prove(sign.PurposeTask, env.Payload)
	in.Confirm = &wrong
	if _, err := Check(in, f.list, now); err != ErrConfirm {
		t.Errorf("confirmation for another purpose: %v", err)
	}
}

// TestCriticalFromTask shows that the signed params decide whether an action
// is critical: at level critical the same action needs a confirmation only
// when its task grants something.
func TestCriticalFromTask(t *testing.T) {
	f := newFixture(t)
	f.list.Roles[0].Level = trust.LevelCritical
	granting := f.task()
	granting.Params = activateParams(`,"grants":["network"]`)
	env := signTask(t, f.sessSig, granting)
	in := f.input(&env)
	var seen json.RawMessage
	in.Rule.Critical = func(params json.RawMessage) (bool, error) {
		seen = params
		return grantRule.Critical(params)
	}
	if _, err := Check(in, f.list, now); err != ErrConfirm {
		t.Errorf("grant in the task, no confirmation: got %v, want %v", err, ErrConfirm)
	}
	if !bytes.Equal(seen, granting.Params) {
		t.Errorf("rule saw %s, want the task's params %s", seen, granting.Params)
	}
	good := f.admin.Prove(sign.PurposeConfirm, env.Payload)
	in.Confirm = &good
	if _, err := Check(in, f.list, now); err != nil {
		t.Errorf("grant in the task, confirmed: %v", err)
	}
	plain := signTask(t, f.sessSig, f.task())
	if got, err := Check(f.input(&plain), f.list, now); err != nil || got == nil {
		t.Errorf("no grant in the task: %v", err)
	}
}

// TestAPIPreCheck: before the API forwards a task to a controller it checks
// it itself, with the task's node as Node.
func TestAPIPreCheck(t *testing.T) {
	f := newFixture(t)
	env := signTask(t, f.sessSig, f.task())
	in := f.input(&env)
	in.Node = f.task().Node
	if got, err := Check(in, f.list, now); err != nil || got == nil {
		t.Errorf("API pre-check with the task's node: %v", err)
	}
	in.Node = ""
	if _, err := Check(in, f.list, now); err != ErrTask {
		t.Errorf("API pre-check with empty node: got %v, want %v", err, ErrTask)
	}
}

// TestRootApp: a channel to an app whose manifest asks for root is a root
// task like a core root action.
func TestRootApp(t *testing.T) {
	f := newFixture(t)
	rootRule := Rule{Root: true, Changes: true, Critical: func(json.RawMessage) (bool, error) { return false, nil }}
	tk := f.task()
	tk.App, tk.Service, tk.Action, tk.Params = "org.sorglos.terminal", "terminal", "shell", nil
	env := signTask(t, f.sessSig, tk)
	in := Input{Task: &env, Session: f.verified, Node: "ctl-1", App: tk.App, Service: tk.Service, Action: tk.Action, Rule: rootRule}
	if got, err := Check(in, f.list, now); err != nil || got == nil {
		t.Errorf("root app channel with task: %v", err)
	}
	noTask := in
	noTask.Task = nil
	if _, err := Check(noTask, f.list, now); err != ErrNoTask {
		t.Errorf("root app channel without task: got %v, want %v", err, ErrNoTask)
	}
	noRoot := trusttest.Genesis(f.admin)
	noRoot.Nodes[1].RootRoles = nil
	if _, err := Check(in, noRoot, now); err != ErrForbidden {
		t.Errorf("root app channel, user without root: got %v, want %v", err, ErrForbidden)
	}
}

func TestLevels(t *testing.T) {
	f := newFixture(t)
	shell := Input{Session: f.verified, Node: "ctl-1", App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Rule: appRule}
	if tk, err := Check(shell, f.list, now); err != nil || tk != nil {
		t.Errorf("plain channel at level critical: %v %v", tk, err)
	}
	f.list.Roles[0].Level = trust.LevelHardcore
	if _, err := Check(shell, f.list, now); err != ErrNoTask {
		t.Errorf("plain channel at level hardcore: %v", err)
	}
}

type vector struct {
	Trust       trust.Signed  `json:"trust"`
	Now         int64         `json:"now"`
	SessionSeed enc.Bytes     `json:"sessionSeed"` // private test key, so JavaScript can reproduce the signature
	Client      enc.Bytes     `json:"client"`
	ClientProof proof.Proof   `json:"clientProof"`
	Task        sign.Envelope `json:"task"`
	Confirm     proof.Proof   `json:"confirm"`
}

func TestVectors(t *testing.T) {
	f := newFixture(t)
	env := signTask(t, f.sessSig, f.task())
	golden.Check(t, *update, "task.json", vector{
		Trust: f.admin.Sign(t, f.list), Now: now, SessionSeed: sessionSeed,
		Client: f.verified.Payload(), ClientProof: f.admin.Prove(sign.PurposeClientCert, f.verified.Payload()),
		Task: env, Confirm: f.admin.Prove(sign.PurposeConfirm, env.Payload),
	})
}

// TestVerifyBoundsAllocation feeds Verify a task of the full size that is an
// array of empty objects where a task has none, signed with the session key
// so that it reaches the decoder: refusing it must allocate next to nothing
// besides the signature check.
func TestVerifyBoundsAllocation(t *testing.T) {
	f := newFixture(t)
	var b bytes.Buffer
	b.WriteString(`{"id":[{}`)
	for b.Len()+len(`,{}]}`) <= MaxTask {
		b.WriteString(`,{}`)
	}
	b.WriteString(`]}`)
	env, err := sign.Sign(f.sessSig, sign.PurposeTask, b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = Verify(env, f.verified, now)
	runtime.ReadMemStats(&after)
	if n := after.TotalAlloc - before.TotalAlloc; n > 64<<10 {
		t.Errorf("%d bytes allocated", n)
	}
	if err != ErrTask {
		t.Errorf("got %v, want %v", err, ErrTask)
	}
}

func FuzzVerify(f *testing.F) {
	fx := newFixture(f)
	f.Add([]byte(signTask(f, fx.sessSig, fx.task()).Payload))
	f.Fuzz(func(t *testing.T, payload []byte) {
		env, err := sign.Sign(fx.sessSig, sign.PurposeTask, payload)
		if err != nil {
			return
		}
		_, _ = Verify(env, fx.verified, now)
	})
}
