package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/update"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// fakeUpdater writes down which of the two methods the fork chose and answers
// what a case told it to.
type fakeUpdater struct {
	called string
	err    error
	// list is the trust list the last call was handed. A case reads it to
	// hold that the list of the CHANNEL reaches the updater, which is the
	// whole reason this interface takes one.
	list *trust.List
	// read is what a stage read out of its reader before it answered.
	read []byte
}

func (u *fakeUpdater) Stage(_ context.Context, list *trust.List, _ access.Decision,
	r io.Reader) ([]byte, error) {
	u.called = "stage"
	u.list = list
	u.read, _ = io.ReadAll(r)
	return []byte("{}"), u.err
}

func (u *fakeUpdater) Install(_ context.Context, list *trust.List,
	_ access.Decision) ([]byte, error) {
	u.called = "install"
	u.list = list
	return []byte("{}"), u.err
}

func (u *fakeUpdater) System(_ context.Context, list *trust.List,
	_ access.Decision) ([]byte, error) {
	u.called = "system"
	u.list = list
	return []byte("{}"), u.err
}

// installAction is the action of the contract this file serves.
func installAction(t *testing.T) core.Action {
	t.Helper()
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActInstall)
	if !ok {
		t.Fatal("the contract has no install action of the controller")
	}
	return a
}

// installDecision is a decision for that action with these params. It is built
// here and not taken from access.Check, because what is measured below is the
// fork behind the gate and not the gate.
func installDecision(t *testing.T, params any) access.Decision {
	t.Helper()
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	d := access.Decision{Core: true, Action: installAction(t)}
	d.Opened.Open.Params = json.RawMessage(b)
	return d
}

// The fork: the target of the params decides which of the two methods runs,
// and nothing else does.
func TestTheTargetDecidesWhichHalfOfTheUpdaterRuns(t *testing.T) {
	for name, c := range map[string]struct{ target, want string }{
		"the system packages": {trust.AutoSystem, "system"},
		"this controller":     {trust.AutoController, "install"},
		"the web interface":   {trust.AutoWeb, "install"},
		"the API":             {trust.AutoAPI, "install"},
		"an app":              {"org.sorglos.notes", "install"},
	} {
		u := &fakeUpdater{}
		s := New(Deps{Updater: u})
		p := core.InstallParams{Target: c.target, Version: "1.2.3",
			SHA256: bytes.Repeat([]byte{0xab}, 32)}
		if c.target == trust.AutoSystem {
			// For the system packages the contract carries neither
			// (core.InstallParams.Validate), and the fork must not need them.
			p = core.InstallParams{Target: c.target}
		}
		v, cl := s.install(context.Background(), nil, installDecision(t, p), nil)
		if cl != nil {
			t.Errorf("%s: the channel ended %+v", name, *cl)
			continue
		}
		if u.called != c.want {
			t.Errorf("%s: %q ran, want %q", name, u.called, c.want)
		}
		b, err := core.EncodeResult(v)
		if err != nil {
			t.Errorf("%s: the result does not encode: %v", name, err)
			continue
		}
		// The same bytes package update answers with, and the whole of what
		// the contract gives this action.
		if string(b) != "{}" {
			t.Errorf("%s: the result is %q, want %q", name, b, "{}")
		}
	}
}

// The seam, measured from both sides at once: each half of package update
// refuses the target of the other, so a fork that ever picked wrong refuses
// instead of installing the wrong thing. This is the case the comment in
// update/system.go points at.
func TestInstallAndSystemEachRefuseTheOthersTarget(t *testing.T) {
	m := update.New(update.Deps{
		State: state.New(memstore.New()), List: &trust.List{}, Report: noReports{},
		Now: func() int64 { return 1 }, BinDir: t.TempDir(), StageDir: t.TempDir(), Max: 1 << 20,
		ReleaseKey: make([]byte, 32), Services: noServices{},
		// A server that carries every role, so that the walls this case
		// measures are the two it names and not the role wall of
		// update.runs (point 33).
		Roles:  []string{tunnel.RoleWeb, tunnel.RoleAPI, tunnel.RoleDB, tunnel.RoleServices},
		Health: func(context.Context, string) (string, error) { return "", nil },
	})
	// Install, asked for the system packages.
	d := installDecision(t, core.InstallParams{Target: trust.AutoSystem})
	if _, err := m.Install(context.Background(), d); !errors.Is(err, update.ErrForbidden) {
		t.Fatalf("Install of the system target = %v, want %v", err, update.ErrForbidden)
	}
	// System, asked for anything else.
	d = installDecision(t, core.InstallParams{Target: trust.AutoController, Version: "1.2.3",
		SHA256: bytes.Repeat([]byte{0xab}, 32)})
	if _, err := m.System(context.Background(), d); !errors.Is(err, update.ErrInternal) {
		t.Fatalf("System of a part target = %v, want %v", err, update.ErrInternal)
	}
}

// A controller without an updater is this program wired wrong and says so with
// the code of its own faults - never with one that blames the API.
func TestAnInstallWithoutAnUpdaterIsThisServersFault(t *testing.T) {
	s := New(Deps{})
	_, cl := s.install(context.Background(), nil, installDecision(t, core.InstallParams{Target: trust.AutoSystem}), nil)
	if cl == nil || cl.Code != wire.CodeInternal {
		t.Fatalf("the channel ended %+v, want %s", cl, wire.CodeInternal)
	}
}

// Params that are no params of this action are the peer's bytes and are named
// as the peer's fault - and no method of the updater is reached over them.
func TestInstallParamsThatAreNoneEndTheChannelAsTheSendersFault(t *testing.T) {
	u := &fakeUpdater{}
	s := New(Deps{Updater: u})
	d := access.Decision{Core: true, Action: installAction(t)}
	d.Opened.Open.Params = json.RawMessage(`{"target":"nothing of the contract"}`)
	_, cl := s.install(context.Background(), nil, d, nil)
	if cl == nil || cl.Code != wire.CodeVerify {
		t.Fatalf("the channel ended %+v, want %s", cl, wire.CodeVerify)
	}
	if u.called != "" {
		t.Fatalf("%q ran over params that do not parse", u.called)
	}
}

// A decision for another action never reaches the updater: access matched the
// OPEN against core.All already, so one that names something else here is this
// program wired wrong and not a peer's doing.
func TestInstallRefusesADecisionForAnotherAction(t *testing.T) {
	u := &fakeUpdater{}
	s := New(Deps{Updater: u})
	other, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActDeactivate)
	if !ok {
		t.Fatal("the contract has no deactivate action")
	}
	d := installDecision(t, core.InstallParams{Target: trust.AutoSystem})
	d.Action = other
	_, cl := s.install(context.Background(), nil, d, nil)
	if cl == nil || cl.Code != wire.CodeInternal {
		t.Fatalf("the channel ended %+v, want %s", cl, wire.CodeInternal)
	}
	if u.called != "" {
		t.Fatalf("%q ran over a decision for another action", u.called)
	}
}

// Which failure of an install the API is told by which code. The three walls
// are kept apart: what this installation did not sign, what it signed and may
// not have, and what went wrong on this machine.
func TestEveryFailureOfAnInstallHasItsCode(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want wire.Code
	}{
		"not signed by this installation": {update.ErrVerify, wire.CodeVerify},
		"not allowed":                     {update.ErrForbidden, wire.CodeForbidden},
		"rights the app has not":          {update.ErrNeedsGrants, wire.CodeForbidden},
		"nothing lies ready":              {update.ErrNothing, wire.CodeNotFound},
		"the updater is not wired up":     {update.ErrInternal, wire.CodeInternal},
		"no room on this server":          {update.ErrDisk, wire.CodeInternal},
		"the new version did not come up": {update.ErrHealth, wire.CodeInternal},
		"apt failed":                      {update.ErrSystem, wire.CodeInternal},
		"something else entirely":         {errors.New("the store did not answer"), wire.CodeInternal},
	} {
		u := &fakeUpdater{err: c.err}
		s := New(Deps{Updater: u})
		_, cl := s.install(context.Background(), nil, installDecision(t, core.InstallParams{Target: trust.AutoSystem}), nil)
		if cl == nil || cl.Code != c.want {
			t.Errorf("%s: the channel ended %+v, want %s", name, cl, c.want)
		}
	}
	// And one joined error, which is how package update answers more than
	// once: the nearest wall is the one that counts.
	u := &fakeUpdater{err: errors.Join(update.ErrForbidden, errors.New("and a second word"))}
	s := New(Deps{Updater: u})
	_, cl := s.install(context.Background(), nil, installDecision(t, core.InstallParams{Target: trust.AutoSystem}), nil)
	if cl == nil || cl.Code != wire.CodeForbidden {
		t.Fatalf("a joined error ended %+v, want %s", cl, wire.CodeForbidden)
	}
}

// The install action is one this build carries out now. A server built without
// an updater still answers E_INTERNAL for it, which is what it answered before
// the updater was wired - so nothing of what the cases of this package hold
// changed for a controller that has none.
func TestTheInstallActionIsServedNow(t *testing.T) {
	s := New(Deps{})
	if _, ok := s.acts[installKey]; !ok {
		t.Fatal("this build does not carry out the install action")
	}
	// stage is a STREAM and never an act: its input is a file
	// (core.FlowTransfer), and an act would have to hold it in memory first.
	if _, ok := s.acts[stageKey]; ok {
		t.Fatal("the stage action stands among the acts, where a file would be read into memory")
	}
}

// The stage action is carried out exactly when this server was built with an
// updater, and the map says so either way.
//
// This is the assurance of the wiring itself, and it is the one that was
// missing: with no entry at all a stage channel fell through Server.core to
// the acts lookup and ended E_INTERNAL without the updater ever being asked.
// An update round of an API ran into that at its FIRST node, every hour, after
// downloading the file - "the round stopped at a node" with E_INTERNAL beside
// it. Measured on the six-machine installation on 24.10.2026, three rounds in
// a row at db-1.
func TestTheStageActionIsServedOnlyWithAnUpdater(t *testing.T) {
	if _, ok := New(Deps{}).streams[stageKey]; ok {
		t.Error("a server without an updater carries out the stage action")
	}
	if _, ok := New(Deps{Updater: &fakeUpdater{}}).streams[stageKey]; !ok {
		t.Error("a server WITH an updater does not carry out the stage action")
	}
}

// A stage channel reaches the updater, hands it the file as it arrives, and
// answers the empty object.
//
// The list it is handed is the one the CHANNEL was decided on. That is the
// whole reason serve.Updater takes one: a Manager of package update is built
// on a list as a value, and a version that changed between the decision and
// the work would check the file against one list and the channel against
// another.
func TestAStageChannelRunsTheUpdaterOnTheListOfItsChannel(t *testing.T) {
	w := newWorld(t)
	u := &fakeUpdater{}
	w.rebuild(func(d *Deps) { d.Updater = u })
	file := bytes.Repeat([]byte{0x3c}, 2000)
	ended := w.tunnel(t, func(a *apiSide) {
		credit, _ := a.open(1, stageOpenWithParams(t))
		if credit < int64(len(file)) {
			t.Fatalf("the first GRANT is %d bytes, too little for the %d of this case",
				credit, len(file))
		}
		a.data(1, file)
		a.eof(1)
		out, cl := a.result(1)
		if string(out) != "{}" {
			t.Errorf("result = %q, want the empty object", out)
		}
		if cl.Status != wire.StatusOK {
			t.Errorf("CLOSE = %+v, want ok", cl)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if u.called != "stage" {
		t.Fatalf("the updater was asked for %q, want the stage", u.called)
	}
	if !bytes.Equal(u.read, file) {
		t.Errorf("the updater read %d bytes, want the %d of the file", len(u.read), len(file))
	}
	if u.list != w.keep.List() {
		t.Error("the updater got another trust list than the channel was decided on")
	}
}

// A controller without an updater refuses a stage channel as this program
// wired wrong, and reads nothing: which halves a server was built with is
// nothing an API decides, and a channel that cannot be served costs this
// server no byte of the file.
func TestAStageChannelWithoutAnUpdaterIsRefused(t *testing.T) {
	w := newWorld(t)
	ended := w.tunnel(t, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: stageOpenWithParams(t)})
		wantCode(t, a.closed(1), wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// An install is handed the list of its channel as well. The two halves are
// held together here rather than apart: an updater that got the list for a
// stage and not for an install would be built on two different lists inside
// one round.
func TestAnInstallGetsTheListOfItsChannel(t *testing.T) {
	u := &fakeUpdater{}
	s := New(Deps{Updater: u})
	list := &trust.List{Seq: 7}
	if _, cl := s.install(context.Background(), list,
		installDecision(t, core.InstallParams{Target: trust.AutoSystem}), nil); cl != nil {
		t.Fatalf("the channel ended %+v, want a result", cl)
	}
	if u.list != list {
		t.Error("the updater got another trust list than the channel was decided on")
	}
}

// stageOpenWithParams is the OPEN of ngc.store/controller/stage WITH the params
// a stage needs. stageOpen beside it (serve_test.go) carries none, because the
// case it serves never gets as far as reading them. It carries no task and
// no user: a stage is core.WithoutUser, so its params stand in the OPEN
// itself, which is also where update.Manager reads them from.
func stageOpenWithParams(t testing.TB) []byte {
	t.Helper()
	params := json.RawMessage(mustJSON(t, core.StageParams{
		Target: trust.AutoAPI, Version: "0.2.2"}))
	inner := mustJSON(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActStage, Params: params, Window: wire.DefaultWindow})
	return mustJSON(t, tunnel.Open{Open: inner, Window: wire.MaxWindow})
}

// noReports takes the update reports of package update and drops them: what a
// report says is measured in that package, not here.
type noReports struct{}

func (noReports) Update(store.UpdateReport) {}

// noServices is a systemd nothing of this file reaches: every case above is
// refused before a service is touched, and a case that ever got past it would
// fail here instead of restarting something.
type noServices struct{ t *testing.T }

func (noServices) Restart(context.Context, string) error {
	return errors.New("no service was to be restarted")
}
func (noServices) Running(context.Context, string) (bool, error) {
	return false, errors.New("no service was to be asked")
}
