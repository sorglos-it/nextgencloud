package serve

// The attacks of the overall test, one case each, from the list in
// apps/server-controller/CLAUDE.md: a forged task, a swapped task, a missing
// re-confirmation, a foreign controller key, an escape from the container, a
// replayed OPEN, changed params, a cut-off input and a package whose UI hash
// does not match.
//
// Every case here is built the same way, and the shape is the point:
//
//  1. ONE intermediate check that the way to the wall under test was open at
//     all - the same channel without the attack has to come off. Without it a
//     case measures whatever wall stands first, and a green run would say the
//     bolt holds while it was never reached. Two of these caught a wrong wall
//     while this file was written: a task on a channel whose OPEN also carried
//     params is refused by wire.Open.Validate before any signature is looked
//     at, and a root task of a user without root permission ends E_FORBIDDEN
//     and not E_CONFIRM.
//  2. The attack itself, with the code the contract gives it.
//
// What is NOT measured here is said where it belongs, not left to the reader:
// TestAttackContainerEscape needs Linux and podman and skips loudly anywhere
// else, and the UI hash is walled in package install, not here (see
// TestAttackWrongUIHash).

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os/exec"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
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

// shellFill is the seed of the shell's static Noise key in these cases, the
// same one world.appOpenOn uses: the key the client certificate names as DH.
const shellFill = 0x52

// strangerSeed is a key no trust list of these cases holds. A task signed with
// it is a task the session key of the certificate did not sign.
var strangerSeed = bytes.Repeat([]byte{0x99}, ed25519.SeedSize)

// atkOpts describes the OPEN of one app channel, so that an attack is the one
// field that differs from the channel the intermediate check runs.
type atkOpts struct {
	// on is the controller the shell hashes into its prologue. Empty is this
	// controller, which is the only one that can answer.
	on string
	// peer is the controller static key the shell handshakes against. nil is
	// this controller's own, out of the fixture's trust list.
	peer []byte
	// app, service, action name the channel. Empty takes the app of the
	// fixture and its one action.
	app, service, action string
	// params of the OPEN. With a task there are none: wire.Open.Validate
	// refuses an OPEN that carries both, and the task's params are the params.
	params json.RawMessage
	// wireParams replaces params in the OPEN after the prologue was built
	// over params - that is the changed byte of TestAttackChangedParams and
	// nothing else.
	wireParams json.RawMessage
	// task and confirm travel in the OPEN as the shell attached them.
	task    *sign.Envelope
	confirm *proof.Proof
}

// atkChannel is one built OPEN: the payload of the tunnel, the shell's
// half-finished handshake, and the bytes of the client certificate the task of
// this channel is bound to.
type atkChannel struct {
	payload []byte
	hs      *noise.HandshakeState
	client  []byte
}

// atkCert is the client certificate of the fixture's admin, the way the API
// attaches it. It is built on its own because a task is bound to its bytes
// (session.CertHash), so the certificate has to exist before the task that
// names it.
func atkCert(t testing.TB, w *world) []byte {
	t.Helper()
	shell, _ := trusttest.NodeKeys(shellFill)
	sessSig := ed25519.NewKeyFromSeed(sessionSeed)
	return mustJSON(t, session.ClientCert{Installation: w.list.Installation, User: trusttest.ID(0x21),
		Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize),
		Sig:   enc.Bytes(sessSig.Public().(ed25519.PublicKey)),
		DH:    shell.Public, Issued: issued, Expires: issued + 3600})
}

// atkTask signs a task with seed for this node, app, service and action. The
// genuine one is signed with sessionSeed, the key the certificate names; any
// other seed is a stranger's. id keeps the tasks of one case apart, because a
// task runs once and its ID is remembered (access.Check over dbapi.ErrUsed).
func atkTask(t testing.TB, w *world, seed []byte, id byte, on, app, service, action string,
	params json.RawMessage) *sign.Envelope {
	t.Helper()
	payload := mustJSON(t, task.Task{ID: bytes.Repeat([]byte{id}, task.IDSize),
		Installation: w.list.Installation, Cert: session.CertHash(atkCert(t, w)), Node: on,
		App: app, Service: service, Action: action, Params: params,
		Issued: issued, Expires: issued + 60})
	env, err := sign.Sign(ed25519.NewKeyFromSeed(seed), sign.PurposeTask, payload)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// atkOpen builds the OPEN of an app channel out of these options.
func atkOpen(t testing.TB, w *world, o atkOpts) atkChannel {
	t.Helper()
	on := o.on
	if on == "" {
		on = node
	}
	peer := o.peer
	if peer == nil {
		ctlKey, _ := trusttest.NodeKeys(ctlFill)
		peer = ctlKey.Public
	}
	app, service, action := o.app, o.service, o.action
	if app == "" {
		app, service, action = testApp, testService, testAction
	}
	// The task goes into the prologue as the OPEN carries it, byte for byte
	// (wire.Open.TaskBytes), so the same encoding is used for both.
	var taskBytes []byte
	if o.task != nil {
		taskBytes = mustJSON(t, o.task)
	}
	shell, _ := trusttest.NodeKeys(shellFill)
	hs, err := noisep.Handshake(true, noisep.E2EPrologue(on, app, service, action, o.params, taskBytes),
		shell, peer, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	params := o.params
	if o.wireParams != nil {
		params = o.wireParams
	}
	inner := wire.Open{App: app, Service: service, Action: action, Params: params,
		Window: wire.DefaultWindow, E2E: msg1, Task: o.task, Confirm: o.confirm}
	client := atkCert(t, w)
	p := w.admin.Prove(sign.PurposeClientCert, client)
	return atkChannel{
		payload: mustJSON(t, tunnel.Open{Open: mustJSON(t, inner), Window: wire.MaxWindow,
			Client: client, Proof: &p}),
		hs:     hs,
		client: client,
	}
}

// atkPutApp puts the app of the fixture back with this manifest, so a case can
// run against an action that needs root or is critical.
func atkPutApp(t testing.TB, w *world, m []byte) {
	t.Helper()
	if err := w.st.PutApp(context.Background(), dbapi.App{ID: testApp, Version: "1.0.0",
		Store: trust.StoreGlobal, Node: node, Active: true, Manifest: m,
		SHA256: bytes.Repeat([]byte{0x01}, 32)}); err != nil {
		t.Fatal(err)
	}
}

// atkRootManifest is the manifest of the fixture with needs root, which makes
// every one of its actions a root action (store.RuleFor): a channel to it runs
// only on a signed task.
func atkRootManifest(t testing.TB) []byte {
	t.Helper()
	m := bytes.Replace(manifest(), []byte(`"actions":`),
		[]byte(`"needs":["`+store.NeedRoot+`"],"actions":`), 1)
	if bytes.Equal(m, manifest()) {
		t.Fatal("the manifest of the fixture changed: nowhere to put needs")
	}
	return m
}

// atkCriticalManifest is that manifest with its one action marked critical,
// which is what makes the role of the fixture's admin ask for a
// re-confirmation (trust.Role.needs at trust.LevelCritical).
func atkCriticalManifest(t testing.TB) []byte {
	t.Helper()
	m := bytes.Replace(manifest(), []byte(`"title":"Ausführen"`),
		[]byte(`"title":"Ausführen","critical":true`), 1)
	if bytes.Equal(m, manifest()) {
		t.Fatal("the manifest of the fixture changed: no action to mark critical")
	}
	return m
}

// atkRuns drives one app channel to its end and reports the outcome inside the
// session. It is the intermediate check of most cases here: a channel that
// comes off this far has passed access, the handshake and the start of the
// container, so a refusal of the attack beside it is the wall under test.
func atkRuns(t *testing.T, a *apiSide, num uint32, c atkChannel) ([]byte, wire.Close) {
	t.Helper()
	sh := start(t, a, num, c.payload, c.hs)
	sh.inner(wire.E2EData, nil)
	sh.inner(wire.E2EEnd, nil)
	data, end := sh.out()
	wantOK(t, sh.rest(), "the CLOSE of a channel that was meant to come off")
	return data, end
}

// atkRefused sends an OPEN that is refused before any GRANT and gives the
// CLOSE. apiSide.open is not used: it waits for the first GRANT, which a
// refused channel never gets.
func atkRefused(a *apiSide, num uint32, payload []byte) wire.Close {
	a.send(wire.Frame{Type: wire.TypeOpen, Channel: num, Payload: payload})
	return a.closed(num)
}

// A task signed by a key the certificate does not name is no task of this
// user: task.Verify fails at the signature, and the channel ends E_VERIFY.
func TestAttackForgedTask(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = says("fertig")
	atkPutApp(t, w, atkRootManifest(t))
	good := atkOpen(t, w, atkOpts{task: atkTask(t, w, sessionSeed, 0x61, node, testApp, testService, testAction, nil)})
	forged := atkOpen(t, w, atkOpts{task: atkTask(t, w, strangerSeed, 0x62, node, testApp, testService, testAction, nil)})
	ended := w.tunnel(t, func(a *apiSide) {
		// Intermediate check: a root action on a genuine task comes off, so
		// the way to the signature is open and E_VERIFY below is the
		// signature and not the root permission or the manifest.
		_, end := atkRuns(t, a, 1, good)
		wantOK(t, end, "the end-to-end end of the channel on a genuine task")
		a.shut(1)
		wantCode(t, atkRefused(a, 2, forged.payload), wire.CodeVerify)
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.eng.runs.Load(); n != 1 {
		t.Errorf("the service ran %d times, want once - the forged task may not reach it", n)
	}
}

// A task a user really signed, but for another action of the same app: the
// signature is good and everything else about it is not. task.Check compares
// node, app, service and action, so it ends E_VERIFY.
func TestAttackSwappedTask(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = says("fertig")
	// Two actions on two services, so there is a second action to make a task
	// out for. Both need root, so neither runs without a task.
	m := bytes.Replace(atkRootManifest(t), []byte(`"services":[`),
		[]byte(`"services":[{"name":"admin","command":["/bin/wipe"]},`), 1)
	m2 := bytes.Replace(m, []byte(`"actions":[`),
		[]byte(`"actions":[{"name":"wipe","title":"Weg","service":"admin"},`), 1)
	if bytes.Equal(m, m2) {
		t.Fatal("the manifest of the fixture changed: no second action could be added")
	}
	atkPutApp(t, w, m2)
	own := atkTask(t, w, sessionSeed, 0x63, node, testApp, "admin", "wipe", nil)
	swapped := atkTask(t, w, sessionSeed, 0x64, node, testApp, "admin", "wipe", nil)
	good := atkOpen(t, w, atkOpts{app: testApp, service: "admin", action: "wipe", task: own})
	// The same kind of task, attached to a channel of the other action.
	bad := atkOpen(t, w, atkOpts{app: testApp, service: testService, action: testAction, task: swapped})
	ended := w.tunnel(t, func(a *apiSide) {
		// Intermediate check: on its own channel the task opens, so what
		// E_VERIFY answers below is the swap.
		_, end := atkRuns(t, a, 1, good)
		wantOK(t, end, "the end-to-end end of the channel the task was made out for")
		a.shut(1)
		wantCode(t, atkRefused(a, 2, bad.payload), wire.CodeVerify)
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.eng.runs.Load(); n != 1 {
		t.Errorf("the service ran %d times, want once - the swapped task may not reach it", n)
	}
}

// A root action without the signed task it needs, and a critical action with
// one but without the user's re-confirmation: both end E_CONFIRM, the code
// that tells the shell to ask the user and come again.
func TestAttackMissingConfirmation(t *testing.T) {
	t.Run("root without a task", func(t *testing.T) {
		envHere(t)
		w := newWorld(t)
		w.eng.run = says("fertig")
		atkPutApp(t, w, atkRootManifest(t))
		good := atkOpen(t, w, atkOpts{task: atkTask(t, w, sessionSeed, 0x65, node, testApp, testService, testAction, nil)})
		bare := atkOpen(t, w, atkOpts{})
		ended := w.tunnel(t, func(a *apiSide) {
			// Intermediate check: with the task the same root action runs, so
			// the user does hold root permission here (trust.List.MayRoot) and
			// E_CONFIRM below is the missing task and not E_FORBIDDEN.
			_, end := atkRuns(t, a, 1, good)
			wantOK(t, end, "the end-to-end end of the root channel on a task")
			a.shut(1)
			wantCode(t, atkRefused(a, 2, bare.payload), wire.CodeConfirm)
			a.shut(2)
			a.bye()
		})
		if err := waitFor(t, ended); err != nil {
			t.Fatalf("Run = %v, want an orderly end after bye", err)
		}
		if n := w.eng.runs.Load(); n != 1 {
			t.Errorf("the service ran %d times, want once", n)
		}
	})

	t.Run("critical with a task but no confirmation", func(t *testing.T) {
		envHere(t)
		w := newWorld(t)
		w.eng.run = says("fertig")
		atkPutApp(t, w, atkCriticalManifest(t))
		withConfirm := atkTask(t, w, sessionSeed, 0x66, node, testApp, testService, testAction, nil)
		c := w.admin.Prove(sign.PurposeConfirm, withConfirm.Payload)
		good := atkOpen(t, w, atkOpts{task: withConfirm, confirm: &c})
		noConfirm := atkTask(t, w, sessionSeed, 0x67, node, testApp, testService, testAction, nil)
		bad := atkOpen(t, w, atkOpts{task: noConfirm})
		ended := w.tunnel(t, func(a *apiSide) {
			// Intermediate check: the very same kind of task with the user's
			// re-confirmation runs, so E_CONFIRM below is the confirmation
			// alone.
			_, end := atkRuns(t, a, 1, good)
			wantOK(t, end, "the end-to-end end of the critical channel with a confirmation")
			a.shut(1)
			wantCode(t, atkRefused(a, 2, bad.payload), wire.CodeConfirm)
			a.shut(2)
			a.bye()
		})
		if err := waitFor(t, ended); err != nil {
			t.Fatalf("Run = %v, want an orderly end after bye", err)
		}
		if n := w.eng.runs.Load(); n != 1 {
			t.Errorf("the service ran %d times, want once", n)
		}
	})
}

// A shell that handshakes against another controller's static key: nothing it
// sends decrypts here, so no handshake comes off. The channel ends E_INTERNAL
// - e2e.ErrHandshake says nothing about whose fault it was - and nothing of it
// reaches the log; HandshakeFailures is what counts it (Server.handshakes).
func TestAttackForeignControllerKey(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = says("fertig")
	stranger, _ := trusttest.NodeKeys(0x7F)
	good := atkOpen(t, w, atkOpts{})
	foreign := atkOpen(t, w, atkOpts{peer: stranger.Public})
	before := w.srv.HandshakeFailures()
	ended := w.tunnel(t, func(a *apiSide) {
		// Intermediate check: against this controller's own key the handshake
		// comes off and the channel runs, so the OPEN itself is good and what
		// fails below is the key.
		_, end := atkRuns(t, a, 1, good)
		wantOK(t, end, "the end-to-end end of the channel on the right key")
		a.shut(1)
		wantCode(t, atkRefused(a, 2, foreign.payload), wire.CodeInternal)
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if got := w.srv.HandshakeFailures() - before; got != 1 {
		t.Errorf("HandshakeFailures rose by %d, want 1", got)
	}
	if n := w.eng.runs.Load(); n != 1 {
		t.Errorf("the service ran %d times, want once - a foreign key may reach no container", n)
	}
}

// Out of the container neither /etc/shadow of the host is readable nor the
// network reachable. This needs Linux and podman and is measured nowhere else:
// where they are missing it says so and counts as NOT measured.
func TestAttackContainerEscape(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skipf("NOT MEASURED: the escape from the container needs Linux and podman; this is %s",
			goruntime.GOOS)
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Skipf("NOT MEASURED: the escape from the container needs podman, which is not installed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Nothing is fetched: the image has to be on the machine already, or this
	// case says it did not measure instead of reaching for a registry.
	const image = "docker.io/library/busybox:latest"
	if out, err := exec.CommandContext(ctx, podman, "image", "exists", image).CombinedOutput(); err != nil {
		t.Skipf("NOT MEASURED: podman holds no %s to run the case in: %v (%s)", image, err, out)
	}
	// The container of an app: no network at all and nothing of the host
	// mounted, which is what runtime builds for an app without NeedNetwork.
	run := func(args ...string) (string, error) {
		all := append([]string{"run", "--rm", "--network=none", "--read-only", image}, args...)
		out, err := exec.CommandContext(ctx, podman, all...).CombinedOutput()
		return string(out), err
	}
	// Intermediate check: the container runs at all. Without it both refusals
	// below would look right while podman simply could not start anything.
	if out, err := run("true"); err != nil {
		t.Fatalf("the container does not start, so nothing below measures anything: %v (%s)", err, out)
	}
	if out, err := run("cat", "/etc/shadow"); err == nil && strings.Contains(out, ":") {
		t.Errorf("/etc/shadow was readable in the container: %q", out)
	}
	if out, err := run("wget", "-q", "-T", "3", "-O", "-", "http://192.0.2.1/"); err == nil {
		t.Errorf("the network was reachable out of the container: %q", out)
	}
}

// The same signed task twice. The first channel runs, the second is refused
// E_VERIFY: a task runs once, and its ID is remembered until it expires
// (access.Check over dbapi.ErrUsed).
func TestAttackReplayedOpen(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = says("fertig")
	atkPutApp(t, w, atkRootManifest(t))
	once := atkTask(t, w, sessionSeed, 0x68, node, testApp, testService, testAction, nil)
	// Two channels of the one task: the handshake of each is its own, so the
	// second is not refused for a repeated Noise message.
	first := atkOpen(t, w, atkOpts{task: once})
	again := atkOpen(t, w, atkOpts{task: once})
	ended := w.tunnel(t, func(a *apiSide) {
		// Intermediate check and first half of the attack in one: this task
		// does open a channel, so the refusal below is the second use.
		_, end := atkRuns(t, a, 1, first)
		wantOK(t, end, "the end-to-end end of the first channel of the task")
		a.shut(1)
		wantCode(t, atkRefused(a, 2, again.payload), wire.CodeVerify)
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if n := w.eng.runs.Load(); n != 1 {
		t.Errorf("the service ran %d times, want once - nothing may run twice on one task", n)
	}
}

// One byte of params changed after the shell built its prologue over them:
// noisep.E2EPrologue hashes the params, so the handshake no longer comes off.
// The channel ends E_INTERNAL and nothing of the app is reached.
func TestAttackChangedParams(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.run = says("fertig")
	params := json.RawMessage(`{"a":"x"}`)
	bent := json.RawMessage(`{"a":"y"}`)
	if len(params) != len(bent) {
		t.Fatal("the two params differ in more than the one byte this case is about")
	}
	good := atkOpen(t, w, atkOpts{params: params})
	changed := atkOpen(t, w, atkOpts{params: params, wireParams: bent})
	before := w.srv.HandshakeFailures()
	ended := w.tunnel(t, func(a *apiSide) {
		// Intermediate check: with the params the prologue was built over the
		// channel runs, so these params are ones the action takes and the
		// handshake below fails at the one byte.
		_, end := atkRuns(t, a, 1, good)
		wantOK(t, end, "the end-to-end end of the channel with untouched params")
		a.shut(1)
		wantCode(t, atkRefused(a, 2, changed.payload), wire.CodeInternal)
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if got := w.srv.HandshakeFailures() - before; got != 1 {
		t.Errorf("HandshakeFailures rose by %d, want 1", got)
	}
	if n := w.eng.runs.Load(); n != 1 {
		t.Errorf("the service ran %d times, want once - changed params may reach no container", n)
	}
}

// The input of the shell ends before its E2EEnd: the EOF of the transport ends
// the transport and not the data (shellInput.fill, errCut). What the shell
// gets is E_PROTOCOL, never the success of a program that read half an input
// and was happy with it.
func TestAttackCutInput(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	// drain reads the whole input, so the cut really reaches this controller:
	// a program that never reads leaves the input standing in the pipe.
	w.eng.run = drain
	good := atkOpen(t, w, atkOpts{})
	cut := atkOpen(t, w, atkOpts{})
	ended := w.tunnel(t, func(a *apiSide) {
		// Intermediate check: the same two inner messages with the E2EEnd
		// behind them end in success, so E_PROTOCOL below is the missing end.
		sh := start(t, a, 1, good.payload, good.hs)
		sh.inner(wire.E2EData, []byte("halb"))
		sh.inner(wire.E2EEnd, nil)
		_, end := sh.out()
		wantOK(t, end, "the end-to-end end of the channel whose input ended")
		wantOK(t, sh.rest(), "the CLOSE of the channel whose input ended")
		a.shut(1)

		sh2 := start(t, a, 2, cut.payload, cut.hs)
		sh2.inner(wire.E2EData, []byte("halb"))
		// The EOF of the transport instead of the E2EEnd of the shell.
		a.eof(2)
		_, end2 := sh2.out()
		wantCode(t, end2, wire.CodeProtocol)
		wantOK(t, sh2.rest(), "the CLOSE of the channel behind a cut-off input")
		a.shut(2)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A package whose UI hash does not match is refused with E_VERIFY - and NOT
// here. This build of serve carries out two core actions, trust and install
// (Server.acts), and activate is not one of them, so an activate channel never
// reaches a package at all: the wall it meets is "this build does not carry
// that out" (E_INTERNAL), which would say nothing about any hash.
//
// Where the hash is measured, with an intermediate check of its own:
// TestAcceptChecksChecksumAndUIHash and TestActivateRefusesWrongUIHash in
// apps/server-controller/classes/install. This case holds the sentence above
// instead of repeating them: it turns red the day serve grows an activate or
// stage action, and then the attack belongs in here as well.
func TestAttackWrongUIHash(t *testing.T) {
	w := newWorld(t)
	for _, n := range []name{
		{core.AppStore, core.ServiceController, core.ActActivate},
		{core.AppStore, core.ServiceController, core.ActStage},
	} {
		if _, ok := w.srv.acts[n]; ok {
			t.Errorf("serve now carries out %v: the attack with a wrong UI hash has to be measured here, "+
				"not only in package install", n)
		}
	}
}
