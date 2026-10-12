package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/update"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// fakeUpdater writes down which of the two methods the fork chose and answers
// what a case told it to.
type fakeUpdater struct {
	called string
	err    error
}

func (u *fakeUpdater) Install(context.Context, access.Decision) ([]byte, error) {
	u.called = "install"
	return []byte("{}"), u.err
}

func (u *fakeUpdater) System(context.Context, access.Decision) ([]byte, error) {
	u.called = "system"
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
		v, cl := s.install(context.Background(), installDecision(t, p), nil)
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
	_, cl := s.install(context.Background(), installDecision(t, core.InstallParams{Target: trust.AutoSystem}), nil)
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
	_, cl := s.install(context.Background(), d, nil)
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
	_, cl := s.install(context.Background(), d, nil)
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
		_, cl := s.install(context.Background(), installDecision(t, core.InstallParams{Target: trust.AutoSystem}), nil)
		if cl == nil || cl.Code != c.want {
			t.Errorf("%s: the channel ended %+v, want %s", name, cl, c.want)
		}
	}
	// And one joined error, which is how package update answers more than
	// once: the nearest wall is the one that counts.
	u := &fakeUpdater{err: errors.Join(update.ErrForbidden, errors.New("and a second word"))}
	s := New(Deps{Updater: u})
	_, cl := s.install(context.Background(), installDecision(t, core.InstallParams{Target: trust.AutoSystem}), nil)
	if cl == nil || cl.Code != wire.CodeForbidden {
		t.Fatalf("a joined error ended %+v, want %s", cl, wire.CodeForbidden)
	}
}

// The install action is one this build carries out now. A server built without
// an updater still answers E_INTERNAL for it, which is what it answered before
// this task - so nothing of what the cases of this package hold changed for a
// controller that has none.
func TestTheInstallActionIsServedNow(t *testing.T) {
	s := New(Deps{})
	if _, ok := s.acts[installKey]; !ok {
		t.Fatal("this build does not carry out the install action")
	}
	if _, ok := s.acts[name{core.AppStore, core.ServiceController, core.ActStage}]; ok {
		t.Fatal("this build carries out the stage action, which it does not")
	}
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
