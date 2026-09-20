package task

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
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
	list    *trust.List
	admin   trusttest.Signer
	sessSig ed25519.PrivateKey
	cert    session.ClientCert
	client  []byte
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
	return fixture{list: l, admin: admin, sessSig: sessSig, cert: cert, client: client}
}

func (f fixture) task() Task {
	return Task{ID: bytes.Repeat([]byte{0x61}, IDSize), Installation: f.list.Installation, Cert: session.CertHash(f.client),
		Node: "ctl-1", App: "ngc.store", Service: "controller", Action: "activate",
		Params: json.RawMessage(`{"app":"org.sorglos.terminal","version":"1.0.0"}`), Issued: issued, Expires: issued + 60}
}

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
	return Input{Task: env, Cert: f.cert, Client: f.client, Node: "ctl-1", App: "ngc.store", Service: "controller",
		Action: "activate", Root: true, Changes: true}
}

func TestCheck(t *testing.T) {
	f := newFixture(t)
	env := signTask(t, f.sessSig, f.task())
	got, err := Check(f.input(&env), f.list, now)
	if err != nil || got == nil || got.Action != "activate" {
		t.Fatalf("got %v, %v", got, err)
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
		{"other client certificate", func(in *Input, _ *int64) { in.Client = append([]byte(" "), f.client...) }, ErrTask},
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
	bad := trusttest.NewSigner(9).Prove(sign.PurposeConfirm, env.Payload)
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

func TestLevels(t *testing.T) {
	f := newFixture(t)
	shell := Input{Cert: f.cert, Client: f.client, Node: "ctl-1", App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Changes: true}
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
		Client: f.client, ClientProof: f.admin.Prove(sign.PurposeClientCert, f.client),
		Task: env, Confirm: f.admin.Prove(sign.PurposeConfirm, env.Payload),
	})
}

func FuzzVerify(f *testing.F) {
	fx := newFixture(f)
	f.Add([]byte(signTask(f, fx.sessSig, fx.task()).Payload))
	f.Fuzz(func(t *testing.T, payload []byte) {
		env, err := sign.Sign(fx.sessSig, sign.PurposeTask, payload)
		if err != nil {
			return
		}
		_, _ = Verify(env, fx.cert, fx.client, now)
	})
}
