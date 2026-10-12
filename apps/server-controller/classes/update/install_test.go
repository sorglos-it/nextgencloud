package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The versions these cases work with: what runs, and what lies ready.
const (
	oldVersion = "1.1.0"
	newVersion = "1.2.0"
)

// oldProgram and newProgram are the bytes of the two programs. They differ in
// length as well as in content, so a half-written file is neither of them and
// a case can say which one lies there.
var (
	oldProgram = []byte("the program that runs now")
	newProgram = []byte("the program of the new version, longer than the old one")
)

// --- the halves of state.Store that only Install uses ---------------------

func (f *fakeStore) Components(_ context.Context) (map[string]string, error) {
	if f.partsErr != nil {
		return nil, f.partsErr
	}
	if f.parts == nil {
		return nil, dbapi.ErrNotFound
	}
	out := map[string]string{}
	for k, v := range f.parts {
		out[k] = v
	}
	return out, nil
}

func (f *fakeStore) PutComponents(_ context.Context, m map[string]string) error {
	if f.putPartsErr != nil {
		return f.putPartsErr
	}
	f.parts = map[string]string{}
	for k, v := range m {
		f.parts[k] = v
	}
	return nil
}

// --- the stand-ins for everything an install touches ----------------------

// fakeServices is systemd. It counts what it was asked to do, so a case can
// say that the service was started again after a rollback and not only that
// the file came back.
type fakeServices struct {
	restarts   []string
	restartErr map[string]error
	running    bool
	runErr     error
}

func (s *fakeServices) Restart(_ context.Context, unit string) error {
	s.restarts = append(s.restarts, unit)
	if err := s.restartErr[unit]; err != nil {
		return err
	}
	return nil
}

func (s *fakeServices) Running(_ context.Context, _ string) (bool, error) {
	return s.running, s.runErr
}

// fakeApps is the installer of app packages (AppInstaller).
type fakeApps struct {
	needs    []string
	needsErr error
	updates  []install.AppUpdate
	updErr   error
}

func (a *fakeApps) Needs(_ context.Context, _ string) ([]string, error) {
	return a.needs, a.needsErr
}

func (a *fakeApps) Update(_ context.Context, in install.AppUpdate) error {
	a.updates = append(a.updates, in)
	return a.updErr
}

// --- the fixture ----------------------------------------------------------

// installFixture is one call to Install. Every case bends one field and calls
// run, so what the case measures is that one thing.
type installFixture struct {
	t       testing.TB
	target  string
	ver     string
	sha     []byte
	st      *fakeStore
	rep     *fakeReports
	svc     *fakeServices
	apps    *fakeApps
	bin     string
	stage   string
	web     string
	health  map[string]string // file -> the version that program names
	healthE error
	slept   []time.Duration
	deps    Deps
	user    string
	task    bool
}

// apiFixture is an install of an NGC part that has to go through: a staged
// file of the new version, a standing approval for it, the old program in
// place, and a new program that names the staged version when it is asked.
func apiFixture(t testing.TB) *installFixture {
	t.Helper()
	dir := t.TempDir()
	f := &installFixture{t: t, target: trust.AutoAPI, ver: newVersion,
		st: newStore(), rep: &fakeReports{}, svc: &fakeServices{running: true},
		apps: &fakeApps{}, bin: dir + "/bin", stage: dir + "/staged", web: dir + "/www",
		health: map[string]string{}}
	f.sha = sha(newProgram)
	mustMkdir(t, f.bin)
	mustMkdir(t, f.stage)
	// The program that runs now, and the file that lies ready.
	mustWrite(t, f.bin+"/"+trust.AutoAPI, oldProgram)
	mustWrite(t, f.stage+"/"+trust.AutoAPI, newProgram)
	// Both are asked for their version by their path, so the health of the
	// case follows the bytes and not a name: after a rollback the same path
	// answers the old version.
	f.health[f.bin+"/"+trust.AutoAPI] = newVersion
	f.st.parts = map[string]string{trust.AutoAPI: oldVersion}
	f.st.staged[trust.AutoAPI] = state.Staged{Target: trust.AutoAPI, Version: newVersion,
		SHA256: f.sha, Size: int64(len(newProgram)), Signer: enc.Bytes(releaseKey(t)), Seq: 3,
		File: f.stage + "/" + trust.AutoAPI, State: store.UpdateDownloaded, Time: 1}
	f.deps = Deps{State: f.st, List: &trust.List{Auto: []trust.Auto{
		{Target: trust.AutoAPI, Key: enc.Bytes(releaseKey(t))}}},
		ReleaseKey: releaseKey(t), StageDir: f.stage, Max: store.MaxPackage,
		Report: f.rep, Now: func() int64 { return 1790000600 },
		BinDir: f.bin, WebRoot: f.web, Services: f.svc, Apps: f.apps,
		// A server that carries the two roles the parts of these cases
		// belong to; trust.AutoController belongs to every server
		// (tunnel.Components). A case that wants another server sets its own
		// (TestInstallRefusesAPartThisServerDoesNotRun).
		Roles: []string{tunnel.RoleAPI, tunnel.RoleWeb},
		Alive: time.Second, Sleep: func(d time.Duration) { f.slept = append(f.slept, d) },
	}
	f.deps.Health = func(_ context.Context, file string) (string, error) {
		if f.healthE != nil {
			return "", f.healthE
		}
		// The version follows the BYTES on disk, so a rollback is measured by
		// what answers and not by what a map was told to say.
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		switch {
		case bytes.Equal(b, newProgram):
			return f.health[file], nil
		case bytes.Equal(b, oldProgram):
			return oldVersion, nil
		default:
			return "", errors.New("the file on disk is neither program")
		}
	}
	return f
}

// appFixtureInstall is an app update that has to go through.
func appUpdateFixture(t testing.TB) *installFixture {
	t.Helper()
	f := apiFixture(t)
	f.target = testApp
	f.ver = testAppV
	pkg := []byte("package of the new version")
	f.sha = sha(pkg)
	mustWrite(t, f.stage+"/"+testApp, pkg)
	pub, _ := signer(1)
	f.st.apps[testApp] = dbapi.App{ID: testApp, Version: oldVersion, Store: testStore,
		Active: true, Grants: []string{store.NeedNetwork}}
	f.st.staged[testApp] = state.Staged{Target: testApp, Version: testAppV, SHA256: f.sha,
		Size: int64(len(pkg)), Signer: enc.Bytes(pub), Seq: 9,
		File: f.stage + "/" + testApp, State: store.UpdateDownloaded, Time: 1}
	f.deps.List = &trust.List{
		Auto:   []trust.Auto{{Target: testApp, Key: enc.Bytes(pub)}},
		Stores: []trust.Store{{ID: testStore, Name: "Global", URL: "https://store.example.org", Key: enc.Bytes(pub)}},
	}
	f.apps.needs = []string{store.NeedNetwork}
	return f
}

func mustMkdir(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t testing.TB, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// decision is the checked channel of an install. Without a user the params
// are the OPEN's; with one they are a signed task's, and the two go through
// the same parser (access.Decision.Params).
func (f *installFixture) decision() access.Decision {
	f.t.Helper()
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActInstall)
	if !ok {
		f.t.Fatal("the contract has no install action")
	}
	p := core.InstallParams{Target: f.target, Version: f.ver, SHA256: enc.Bytes(f.sha)}
	params, err := json.Marshal(p)
	if err != nil {
		f.t.Fatal(err)
	}
	d := access.Decision{
		Opened: tunnel.Opened{Open: wire.Open{App: core.AppStore, Service: core.ServiceController,
			Action: core.ActInstall, Params: params}},
		Action: a, Core: true, User: f.user,
	}
	if f.task {
		d.Task = &task.Task{Params: params}
	}
	return d
}

func (f *installFixture) run() ([]byte, error) {
	f.t.Helper()
	return New(f.deps).Install(context.Background(), f.decision())
}

// program is what lies under the name the service starts.
func (f *installFixture) program() []byte {
	f.t.Helper()
	b, err := os.ReadFile(f.bin + "/" + f.target)
	if err != nil {
		return nil
	}
	return b
}

// whichProgram names what lies in place, so a failing case says "half" rather
// than printing bytes.
func (f *installFixture) whichProgram() string {
	f.t.Helper()
	switch b := f.program(); {
	case b == nil:
		return "nothing"
	case bytes.Equal(b, oldProgram):
		return "old"
	case bytes.Equal(b, newProgram):
		return "new"
	default:
		return "half"
	}
}

// leftovers lists what lies in BinDir besides the program and its .prev, so a
// case can say that no temporary file was left behind.
func (f *installFixture) leftovers() []string {
	f.t.Helper()
	ents, err := os.ReadDir(f.bin)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if e.Name() == f.target || e.Name() == f.target+".prev" {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// reportStates lists the states that were reported, in order.
func (f *installFixture) reportStates() []string {
	f.t.Helper()
	var out []string
	for _, r := range f.rep.got {
		out = append(out, r.State)
	}
	return out
}

// --- the five cases of the brief ------------------------------------------

func TestInstallWithoutUserNeedsAStandingApproval(t *testing.T) {
	// The INTERMEDIATE check first: with the approval the same fixture goes
	// through, so the refusal below is this wall and not an earlier one.
	ok := apiFixture(t)
	if _, err := ok.run(); err != nil {
		t.Fatalf("the unbent fixture does not install: %v", err)
	}
	if ok.whichProgram() != "new" {
		t.Fatalf("after the install the program is %s", ok.whichProgram())
	}
	if ok.st.parts[trust.AutoAPI] != newVersion {
		t.Errorf("remembered version %q, want %q", ok.st.parts[trust.AutoAPI], newVersion)
	}
	if got := ok.reportStates(); !slices.Equal(got, []string{store.UpdateInstalling, store.UpdateInstalled}) {
		t.Errorf("reports %v", got)
	}

	f := apiFixture(t)
	f.deps.List = &trust.List{} // no standing approval at all
	if _, err := f.run(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("without an approval: %v, want ErrForbidden", err)
	}
	if f.whichProgram() != "old" {
		t.Errorf("the program is %s, want the old one untouched", f.whichProgram())
	}
	if n := len(f.svc.restarts); n != 0 {
		t.Errorf("%d restarts, want none", n)
	}
	if r := f.rep.last(); r.State != store.UpdateRejected {
		t.Errorf("report %+v, want rejected", r)
	}

	// And the approval has to name THIS signer, not just the target: a key
	// of another signer with the same target does not cover it.
	other, _ := signer(7)
	g := apiFixture(t)
	g.deps.List = &trust.List{Auto: []trust.Auto{{Target: trust.AutoAPI, Key: enc.Bytes(other)}}}
	if _, err := g.run(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("with an approval for another key: %v, want ErrForbidden", err)
	}
	if g.whichProgram() != "old" {
		t.Errorf("the program is %s, want the old one untouched", g.whichProgram())
	}
}

func TestInstallWithoutUserRefusesParamsThatDiffer(t *testing.T) {
	// Version and checksum come out of the staged row, which a signature
	// produced; the params of a channel without a user are not signed.
	for _, c := range []struct {
		name string
		bend func(f *installFixture)
	}{
		{"another version", func(f *installFixture) { f.ver = "9.9.9" }},
		{"another checksum", func(f *installFixture) { f.sha = sha([]byte("other bytes")) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := apiFixture(t)
			// The intermediate check is the first case of this file: the same
			// fixture unbent installs. Here the approval is left in place, so
			// the wall that answers is the comparison and not Unattended.
			c.bend(f)
			if _, err := f.run(); !errors.Is(err, ErrVerify) {
				t.Fatalf("%s: %v, want ErrVerify", c.name, err)
			}
			if f.whichProgram() != "old" {
				t.Errorf("the program is %s, want the old one untouched", f.whichProgram())
			}
			if r := f.rep.last(); r.State != store.UpdateRejected {
				t.Errorf("report %+v, want rejected", r)
			}
		})
	}
}

func TestInstallNeverGoesBackwards(t *testing.T) {
	// 1.3.0 runs, 1.2.0 lies ready. Refused - and refused with a signed task
	// as well, because an admin may sign anything and an older program is no
	// update.
	for _, c := range []struct{ name, user string }{
		{"without a user", ""},
		{"with a signed task", "admin-1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := apiFixture(t)
			f.user = c.user
			f.task = c.user != ""
			f.st.parts = map[string]string{trust.AutoAPI: "1.3.0"}
			_, err := f.run()
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("%s: %v, want ErrForbidden", c.name, err)
			}
			// WHICH wall answered, measured and not assumed: without a user
			// core.Unattended refuses first - it compares the versions
			// itself - and store.ErrRollback is not in its answer. With a
			// task Unattended never runs, and the line of this package is
			// the only one that refuses, which its ErrRollback says.
			if rolled := errors.Is(err, store.ErrRollback); rolled != f.task {
				t.Errorf("%s: store.ErrRollback in the answer: %v, want %v", c.name, rolled, f.task)
			}
			if f.whichProgram() != "old" {
				t.Errorf("the program is %s, want untouched", f.whichProgram())
			}

			// INTERMEDIATE: the same channel with an older version installed
			// goes through, so the refusal above is the version wall.
			g := apiFixture(t)
			g.user = c.user
			g.task = c.user != ""
			if _, err := g.run(); err != nil {
				t.Fatalf("%s, with 1.1.0 installed: %v", c.name, err)
			}
			if g.whichProgram() != "new" {
				t.Errorf("the program is %s, want new", g.whichProgram())
			}
		})
	}
}

func TestInstallAppOnlyFromItsOwnStoreAndWithinItsGrants(t *testing.T) {
	// INTERMEDIATE: the unbent app fixture goes through.
	ok := appUpdateFixture(t)
	if _, err := ok.run(); err != nil {
		t.Fatalf("the unbent app fixture does not install: %v", err)
	}
	if len(ok.apps.updates) != 1 {
		t.Fatalf("%d app installs, want 1", len(ok.apps.updates))
	}
	// The grants the app gets are the needs of the new version.
	if u := ok.apps.updates[0]; u.App != testApp || u.Version != testAppV ||
		!slices.Equal(u.Needs, []string{store.NeedNetwork}) {
		t.Errorf("app install %+v", u)
	}
	if got := ok.reportStates(); !slices.Equal(got, []string{store.UpdateInstalling, store.UpdateInstalled}) {
		t.Errorf("reports %v", got)
	}

	// Another store: the key of the store the app was activated from is not
	// the one the staged file was accepted under.
	t.Run("another store", func(t *testing.T) {
		f := appUpdateFixture(t)
		other, _ := signer(9)
		f.deps.List.Stores = []trust.Store{{ID: testStore, Name: "Global",
			URL: "https://store.example.org", Key: enc.Bytes(other)}}
		if _, err := f.run(); !errors.Is(err, ErrVerify) {
			t.Fatalf("%v, want ErrVerify", err)
		}
		if len(f.apps.updates) != 0 {
			t.Error("the app was installed all the same")
		}
		if r := f.rep.last(); r.State != store.UpdateRejected {
			t.Errorf("report %+v, want rejected", r)
		}
	})

	// More needs than grants: refused, and reported as a state of its own.
	t.Run("more needs than grants", func(t *testing.T) {
		f := appUpdateFixture(t)
		f.apps.needs = []string{store.NeedNetwork, store.NeedRoot}
		_, err := f.run()
		if !errors.Is(err, ErrForbidden) || !errors.Is(err, ErrNeedsGrants) {
			t.Fatalf("%v, want ErrForbidden and ErrNeedsGrants", err)
		}
		if len(f.apps.updates) != 0 {
			t.Error("the app was installed all the same")
		}
		if r := f.rep.last(); r.State != store.UpdateNeedsGrants {
			t.Errorf("report state %q, want %q", r.State, store.UpdateNeedsGrants)
		}
	})

	// An app this controller does not run is not installed.
	t.Run("an app this controller does not run", func(t *testing.T) {
		f := appUpdateFixture(t)
		delete(f.st.apps, testApp)
		if _, err := f.run(); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%v, want ErrForbidden", err)
		}
	})
}

func TestRollbackAfterAFailedStart(t *testing.T) {
	// The new program is in place and names another version than the one that
	// was staged: the previous file comes back, the service is started again,
	// and the report says store.UpdateRolledBack.
	f := apiFixture(t)
	f.health[f.bin+"/"+trust.AutoAPI] = "7.7.7"
	_, err := f.run()
	if !errors.Is(err, ErrHealth) {
		t.Fatalf("%v, want ErrHealth", err)
	}
	if f.whichProgram() != "old" {
		t.Fatalf("the program is %s, want the old one back", f.whichProgram())
	}
	// Twice: once for the new program, once for the one that came back. The
	// service RUNS again, and that is what the second one is.
	if want := []string{config.ServiceAPI, config.ServiceAPI}; !slices.Equal(f.svc.restarts, want) {
		t.Errorf("restarts %v, want %v", f.svc.restarts, want)
	}
	if running, err := f.svc.Running(context.Background(), config.ServiceAPI); err != nil || !running {
		t.Errorf("the service does not run again: %v %v", running, err)
	}
	// The remembered version is the one that runs, not the one that failed.
	if f.st.parts[trust.AutoAPI] != oldVersion {
		t.Errorf("remembered %q, want %q", f.st.parts[trust.AutoAPI], oldVersion)
	}
	if got := f.reportStates(); !slices.Equal(got,
		[]string{store.UpdateInstalling, store.UpdateRolledBack, store.UpdateFailed}) {
		t.Errorf("reports %v", got)
	}
	// Nothing of the failed install is left lying about.
	if _, err := os.Lstat(f.bin + "/" + trust.AutoAPI + ".prev"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".prev is still there after the rollback: %v", err)
	}
	if n := f.leftovers(); len(n) != 0 {
		t.Errorf("left behind in BinDir: %v", n)
	}
}

// --- rollback out of the three states the brief asks for ------------------

func TestRollbackFromAnAbortedInstall(t *testing.T) {
	// The install is broken off at the restart, which is the first step after
	// the swap: the new program is in place, nothing has run it.
	f := apiFixture(t)
	f.svc.restartErr = map[string]error{config.ServiceAPI: errors.New("unit failed to start")}
	_, err := f.run()
	if !errors.Is(err, ErrHealth) {
		t.Fatalf("%v, want ErrHealth", err)
	}
	if f.whichProgram() != "old" {
		t.Fatalf("the program is %s, want the old one back", f.whichProgram())
	}
	// Two restarts were attempted; the second one is the one that brings the
	// old program up, and it failed too - so the error says BOTH went wrong
	// and nothing claims the service runs.
	if len(f.svc.restarts) != 2 {
		t.Errorf("restarts %v, want two attempts", f.svc.restarts)
	}
}

func TestRollbackWhileTheServiceDoesNotStayUp(t *testing.T) {
	// The new program names the right version and then dies: the service is
	// not running after Alive.
	f := apiFixture(t)
	f.svc.running = false
	if _, err := f.run(); !errors.Is(err, ErrHealth) {
		t.Fatalf("%v, want ErrHealth", err)
	}
	if f.whichProgram() != "old" {
		t.Errorf("the program is %s, want the old one back", f.whichProgram())
	}
	if !slices.Equal(f.slept, []time.Duration{time.Second}) {
		t.Errorf("waited %v, want one wait of Alive", f.slept)
	}
}

func TestResumeAfterARestartTakesAnUnconfirmedVersionBack(t *testing.T) {
	// The controller installed itself: the marker says installing, the
	// program in place is the new one, and nothing confirmed it.
	f := apiFixture(t)
	f.target = trust.AutoController
	mustWrite(t, f.bin+"/"+trust.AutoController, newProgram)
	mustWrite(t, f.bin+"/"+trust.AutoController+".prev", oldProgram)
	f.st.staged[trust.AutoController] = state.Staged{Target: trust.AutoController,
		Version: newVersion, SHA256: sha(newProgram), Size: int64(len(newProgram)),
		Signer: enc.Bytes(releaseKey(t)), Seq: 3, File: f.stage + "/" + trust.AutoAPI,
		State: store.UpdateInstalling, Time: 1}
	f.st.parts = map[string]string{trust.AutoController: oldVersion}

	// The program that came up names ANOTHER version: taken back.
	f.health[f.bin+"/"+trust.AutoController] = "7.7.7"
	if err := New(f.deps).Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if f.whichProgram() != "old" {
		t.Fatalf("the program is %s, want the old one back", f.whichProgram())
	}
	if !slices.Contains(f.svc.restarts, config.ServiceController) {
		t.Errorf("restarts %v, want the controller's unit", f.svc.restarts)
	}
	if f.st.parts[trust.AutoController] != oldVersion {
		t.Errorf("remembered %q, want %q", f.st.parts[trust.AutoController], oldVersion)
	}
	if st := f.st.staged[trust.AutoController]; st.State != store.UpdateRolledBack {
		t.Errorf("marker %q, want %q", st.State, store.UpdateRolledBack)
	}
	if r := f.rep.last(); r.State != store.UpdateRolledBack {
		t.Errorf("report %+v, want rolledback", r)
	}

	// INTERMEDIATE: the same marker with a program that DOES name the staged
	// version is confirmed, so the rollback above is the version check and
	// not the marker alone.
	g := apiFixture(t)
	g.target = trust.AutoController
	mustWrite(t, g.bin+"/"+trust.AutoController, newProgram)
	mustWrite(t, g.bin+"/"+trust.AutoController+".prev", oldProgram)
	g.st.staged[trust.AutoController] = state.Staged{Target: trust.AutoController,
		Version: newVersion, SHA256: sha(newProgram), Size: int64(len(newProgram)),
		Signer: enc.Bytes(releaseKey(t)), Seq: 3, File: g.stage + "/" + trust.AutoAPI,
		State: store.UpdateInstalling, Time: 1}
	g.health[g.bin+"/"+trust.AutoController] = newVersion
	if err := New(g.deps).Resume(context.Background()); err != nil {
		t.Fatalf("Resume, confirming: %v", err)
	}
	if g.whichProgram() != "new" {
		t.Errorf("the program is %s, want the new one kept", g.whichProgram())
	}
	if g.st.parts[trust.AutoController] != newVersion {
		t.Errorf("remembered %q, want %q", g.st.parts[trust.AutoController], newVersion)
	}
	if r := g.rep.last(); r.State != store.UpdateInstalled {
		t.Errorf("report %+v, want installed", r)
	}

	// And a marker in any other state is none of Resume's business.
	h := apiFixture(t)
	h.st.staged[trust.AutoController] = state.Staged{Target: trust.AutoController,
		Version: newVersion, SHA256: sha(newProgram), Size: int64(len(newProgram)),
		Signer: enc.Bytes(releaseKey(t)), File: h.stage + "/" + trust.AutoAPI,
		State: store.UpdateDownloaded, Time: 1}
	if err := New(h.deps).Resume(context.Background()); err != nil {
		t.Fatalf("Resume on a downloaded row: %v", err)
	}
	if len(h.rep.got) != 0 || len(h.svc.restarts) != 0 {
		t.Errorf("Resume acted on a row that is not installing: %v %v", h.rep.got, h.svc.restarts)
	}
}

func TestInstallOfTheControllerLeavesTheMarkerAndDoesNotConfirmItself(t *testing.T) {
	// The controller replaces itself. It must report and mark BEFORE the
	// restart, because the restart does not come back - and it must not
	// write "installed", which only the program that came up may do.
	f := apiFixture(t)
	f.target = trust.AutoController
	f.ver = newVersion
	mustWrite(t, f.bin+"/"+trust.AutoController, oldProgram)
	mustWrite(t, f.stage+"/"+trust.AutoController, newProgram)
	f.health[f.bin+"/"+trust.AutoController] = newVersion
	f.st.parts = map[string]string{trust.AutoController: oldVersion}
	f.st.staged[trust.AutoController] = state.Staged{Target: trust.AutoController,
		Version: newVersion, SHA256: sha(newProgram), Size: int64(len(newProgram)),
		Signer: enc.Bytes(releaseKey(t)), Seq: 3, File: f.stage + "/" + trust.AutoController,
		State: store.UpdateDownloaded, Time: 1}
	f.deps.List = &trust.List{Auto: []trust.Auto{
		{Target: trust.AutoController, Key: enc.Bytes(releaseKey(t))}}}
	if _, err := f.run(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if f.whichProgram() != "new" {
		t.Fatalf("the program is %s, want new", f.whichProgram())
	}
	if st := f.st.staged[trust.AutoController]; st.State != store.UpdateInstalling {
		t.Errorf("marker %q, want %q - only the program that comes up confirms", st.State, store.UpdateInstalling)
	}
	if f.st.parts[trust.AutoController] != oldVersion {
		t.Errorf("remembered %q already, want the old one until Resume confirms",
			f.st.parts[trust.AutoController])
	}
	if got := f.reportStates(); !slices.Equal(got, []string{store.UpdateInstalling}) {
		t.Errorf("reports %v, want installing alone", got)
	}
	if !slices.Equal(f.svc.restarts, []string{config.ServiceController}) {
		t.Errorf("restarts %v", f.svc.restarts)
	}
}

// --- the one program at every instant -------------------------------------

func TestInstallKeepsOneWholeProgramAtEveryStep(t *testing.T) {
	// Every way an install can fail, and after each one the name the service
	// starts holds EXACTLY one of the two programs - never a fragment, never
	// nothing. There is no state in which half of the install and half of the
	// rollback hold.
	cases := []struct {
		name string
		bend func(f *installFixture)
		want string // which program lies in place afterwards
	}{
		{"the staged file is gone", func(f *installFixture) {
			if err := os.Remove(f.stage + "/" + trust.AutoAPI); err != nil {
				f.t.Fatal(err)
			}
		}, "old"},
		{"the .prev of an earlier install will not go", func(f *installFixture) {
			// A non-empty directory under the .prev name: os.Remove fails on
			// it, so the step before the swap fails.
			mustMkdir(f.t, f.bin+"/"+trust.AutoAPI+".prev")
			mustWrite(f.t, f.bin+"/"+trust.AutoAPI+".prev/x", []byte("x"))
		}, "old"},
		{"the marker cannot be written", func(f *installFixture) {
			f.st.putErr = errors.New("database full")
		}, "old"},
		{"the restart fails", func(f *installFixture) {
			f.svc.restartErr = map[string]error{config.ServiceAPI: errors.New("no")}
		}, "old"},
		{"the new program names another version", func(f *installFixture) {
			f.health[f.bin+"/"+trust.AutoAPI] = "7.7.7"
		}, "old"},
		{"the service does not stay up", func(f *installFixture) {
			f.svc.running = false
		}, "old"},
		{"the version cannot be written down", func(f *installFixture) {
			// This one fails AFTER the program came up healthy, so the new
			// program stays - and that is right: it runs.
			f.st.putPartsErr = errors.New("database full")
		}, "new"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := apiFixture(t)
			c.bend(f)
			if _, err := f.run(); err == nil {
				t.Fatal("the install went through, so this case measures nothing")
			}
			if got := f.whichProgram(); got != c.want {
				t.Fatalf("the program in place is %s, want %s", got, c.want)
			}
			if n := f.leftovers(); len(n) != 0 {
				t.Errorf("left behind in BinDir: %v", n)
			}
		})
	}
}

// --- the walls that stand before any of it --------------------------------

func TestInstallRefusesTheSystemTarget(t *testing.T) {
	// apt is task 21, not this call. The params of a system install carry no
	// version and no checksum, so every comparison below would compare
	// nothing with nothing.
	f := apiFixture(t)
	f.target = trust.AutoSystem
	f.ver = ""
	f.sha = nil
	if _, err := f.run(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("%v, want ErrForbidden", err)
	}
	if len(f.svc.restarts) != 0 {
		t.Errorf("restarts %v, want none", f.svc.restarts)
	}
}

func TestInstallNeedsACheckedRow(t *testing.T) {
	for _, c := range []struct {
		name string
		bend func(f *installFixture)
	}{
		{"nothing lies ready", func(f *installFixture) { delete(f.st.staged, trust.AutoAPI) }},
		{"the row is not downloaded", func(f *installFixture) {
			st := f.st.staged[trust.AutoAPI]
			st.State = store.UpdateInstalling
			f.st.staged[trust.AutoAPI] = st
		}},
		{"the row names no file", func(f *installFixture) {
			st := f.st.staged[trust.AutoAPI]
			st.File = ""
			f.st.staged[trust.AutoAPI] = st
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := apiFixture(t)
			c.bend(f)
			if _, err := f.run(); !errors.Is(err, ErrNothing) {
				t.Fatalf("%v, want ErrNothing", err)
			}
			if f.whichProgram() != "old" {
				t.Errorf("the program is %s, want untouched", f.whichProgram())
			}
			if r := f.rep.last(); r.State != store.UpdateRejected {
				t.Errorf("report %+v, want rejected", r)
			}
		})
	}
}

func TestInstallRefusesADecisionForAnotherAction(t *testing.T) {
	// A decision for another action reaching Install is this program wired
	// wrong, not a peer's doing: the params are read as this action's.
	f := apiFixture(t)
	d := f.decision()
	stage, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	if !ok {
		t.Fatal("no stage action")
	}
	d.Action = stage
	if _, err := New(f.deps).Install(context.Background(), d); !errors.Is(err, ErrInternal) {
		t.Fatalf("%v, want ErrInternal", err)
	}
}

func TestInstallWithoutItsDependenciesIsInternal(t *testing.T) {
	for _, c := range []struct {
		name string
		bend func(d *Deps)
	}{
		{"no store", func(d *Deps) { d.State = nil }},
		{"no trust list", func(d *Deps) { d.List = nil }},
		{"no reports", func(d *Deps) { d.Report = nil }},
		{"no clock", func(d *Deps) { d.Now = nil }},
		{"no bin directory", func(d *Deps) { d.BinDir = "" }},
		{"no services", func(d *Deps) { d.Services = nil }},
		{"no health check", func(d *Deps) { d.Health = nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := apiFixture(t)
			c.bend(&f.deps)
			if _, err := f.run(); !errors.Is(err, ErrInternal) {
				t.Fatalf("%v, want ErrInternal", err)
			}
			if err := New(f.deps).Rollback(context.Background(), trust.AutoAPI); !errors.Is(err, ErrInternal) {
				t.Fatalf("Rollback: %v, want ErrInternal", err)
			}
		})
	}
	// An app update without the installer is the same answer, and it is a
	// different field: Apps is only needed on that branch.
	f := appUpdateFixture(t)
	f.deps.Apps = nil
	if _, err := f.run(); !errors.Is(err, ErrInternal) {
		t.Fatalf("app without an installer: %v, want ErrInternal", err)
	}
}

func TestRollbackWritesDownTheVersionThatRunsAgain(t *testing.T) {
	// A mutant that leaves the write out survived the case above: there the
	// remembered version was ALREADY the old one, because a failed install
	// never confirms - so the assertion measured the fixture and not the
	// write. Here the row says the new version, the way it would after a
	// confirmed install, and only the write can bring it back.
	f := apiFixture(t)
	mustWrite(t, f.bin+"/"+trust.AutoAPI+".prev", oldProgram)
	mustWrite(t, f.bin+"/"+trust.AutoAPI, newProgram)
	f.st.parts = map[string]string{trust.AutoAPI: newVersion}
	if err := New(f.deps).Rollback(context.Background(), trust.AutoAPI); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if f.whichProgram() != "old" {
		t.Fatalf("the program is %s, want the old one back", f.whichProgram())
	}
	// The version is asked of the program that is back, not read off a row.
	if got := f.st.parts[trust.AutoAPI]; got != oldVersion {
		t.Errorf("remembered %q, want %q", got, oldVersion)
	}
	if r := f.rep.last(); r.State != store.UpdateRolledBack || r.Version != oldVersion {
		t.Errorf("report %+v", r)
	}
}

func TestRollbackWithoutAPreviousProgram(t *testing.T) {
	// The first install of a part leaves no .prev, and a second rollback has
	// none either: there is one program to go back to, and after a rollback
	// it is the one in place.
	f := apiFixture(t)
	if err := New(f.deps).Rollback(context.Background(), trust.AutoAPI); !errors.Is(err, ErrNothing) {
		t.Fatalf("%v, want ErrNothing", err)
	}
	if f.whichProgram() != "old" {
		t.Errorf("the program is %s, want untouched", f.whichProgram())
	}
	if len(f.svc.restarts) != 0 {
		t.Errorf("restarts %v, want none - nothing was put back", f.svc.restarts)
	}
	// And an app is not a file this call swaps.
	if err := New(f.deps).Rollback(context.Background(), testApp); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an app: %v, want ErrForbidden", err)
	}
}

func TestInstallOfAFirstPartHasNothingToRollBackTo(t *testing.T) {
	// No program in place at all: the install works, and if it fails the
	// rollback says ErrNothing rather than claiming something was restored.
	f := apiFixture(t)
	// With a signed task: a channel WITHOUT a user cannot install a first
	// part at all, because core.Unattended compares with store.Newer, which
	// answers false for an empty installed version. That is the BLOCKED of
	// this task and it is measured right below.
	f.user = "admin-1"
	f.task = true
	if err := os.Remove(f.bin + "/" + trust.AutoAPI); err != nil {
		t.Fatal(err)
	}
	f.st.parts = map[string]string{}
	f.health[f.bin+"/"+trust.AutoAPI] = "7.7.7" // it does not come up
	_, err := f.run()
	if !errors.Is(err, ErrHealth) || !errors.Is(err, ErrNothing) {
		t.Fatalf("%v, want ErrHealth and ErrNothing", err)
	}
	// Nothing was restored, and the file that did not come up is still there:
	// that is the honest state, and the report says failed.
	if f.whichProgram() != "new" {
		t.Errorf("the program is %s", f.whichProgram())
	}
	if r := f.rep.last(); r.State != store.UpdateFailed {
		t.Errorf("report %+v, want failed", r)
	}
}

func TestInstallOfAFirstPartWithoutAUserIsRefusedByTheContract(t *testing.T) {
	// BLOCKED, measured: with nothing of a part installed, an install without
	// a user is refused however right everything else is. core.Unattended
	// asks store.Newer(version, "") and store.Newer answers false for a
	// version that is no version - "" is none - while the comment of
	// core.Unattended says the opposite ("\"\" never is older"). So an
	// installation can never take its FIRST ngc-api or ngc-web unattended.
	// The line that would have to change is in shared/, which is frozen.
	if store.Newer("1.2.0", "") {
		t.Fatal("store.Newer takes \"\" as older now; the BLOCKED of task 20 is gone, " +
			"and the guard in install() can go with it")
	}
	p := &core.InstallParams{Target: trust.AutoAPI, Version: newVersion, SHA256: sha(newProgram)}
	l := &trust.List{Auto: []trust.Auto{{Target: trust.AutoAPI, Key: enc.Bytes(releaseKey(t))}}}
	if core.Unattended(p, l, releaseKey(t), "") {
		t.Fatal("core.Unattended allows a first install now; the BLOCKED of task 20 is gone")
	}
	// INTERMEDIATE: the same params with a version installed are allowed, so
	// what is measured above is the empty version and not the approval.
	if !core.Unattended(p, l, releaseKey(t), oldVersion) {
		t.Fatal("core.Unattended refuses the ordinary case; this case measures an earlier wall")
	}
	// And that is exactly what Install answers for it.
	f := apiFixture(t)
	if err := os.Remove(f.bin + "/" + trust.AutoAPI); err != nil {
		t.Fatal(err)
	}
	f.st.parts = map[string]string{}
	if _, err := f.run(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("%v, want ErrForbidden", err)
	}
	if f.whichProgram() != "nothing" {
		t.Errorf("something was installed: %s", f.whichProgram())
	}
}

// --- the web interface ----------------------------------------------------

// webFixture is an install of the web interface: a tar of two files, staged.
func webFixture(t testing.TB) *installFixture {
	t.Helper()
	f := apiFixture(t)
	f.target = trust.AutoWeb
	tar := uiTar(t, map[string]string{"index.html": "<p>neu</p>", "css/app.css": "body{}"})
	f.sha = sha(tar)
	mustWrite(t, f.stage+"/"+trust.AutoWeb, tar)
	f.st.parts = map[string]string{trust.AutoWeb: oldVersion}
	f.st.staged[trust.AutoWeb] = state.Staged{Target: trust.AutoWeb, Version: newVersion,
		SHA256: f.sha, Size: int64(len(tar)), Signer: enc.Bytes(releaseKey(t)), Seq: 3,
		File: f.stage + "/" + trust.AutoWeb, State: store.UpdateDownloaded, Time: 1}
	f.deps.List = &trust.List{Auto: []trust.Auto{
		{Target: trust.AutoWeb, Key: enc.Bytes(releaseKey(t))}}}
	mustMkdir(t, f.web)
	mustWrite(t, f.web+"/index.html", []byte("<p>alt</p>"))
	return f
}

func TestInstallWebMovesAWholeTreeOrNone(t *testing.T) {
	f := webFixture(t)
	if _, err := f.run(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if b, err := os.ReadFile(f.web + "/index.html"); err != nil || string(b) != "<p>neu</p>" {
		t.Fatalf("index.html is %q, %v", b, err)
	}
	if b, err := os.ReadFile(f.web + "/css/app.css"); err != nil || string(b) != "body{}" {
		t.Fatalf("css/app.css is %q, %v", b, err)
	}
	// The stand before it is whole under .prev, and the folder the new one
	// was unpacked into is gone.
	if b, err := os.ReadFile(f.web + ".prev/index.html"); err != nil || string(b) != "<p>alt</p>" {
		t.Fatalf(".prev/index.html is %q, %v", b, err)
	}
	if _, err := os.Lstat(f.web + ".next"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".next is still there: %v", err)
	}
	if f.st.parts[trust.AutoWeb] != newVersion {
		t.Errorf("remembered %q, want %q", f.st.parts[trust.AutoWeb], newVersion)
	}
	// No unit was restarted: Apache serves these files and no unit owns them.
	if len(f.svc.restarts) != 0 {
		t.Errorf("restarts %v, want none for the web interface", f.svc.restarts)
	}

	// An archive that is not one changes nothing at all.
	g := webFixture(t)
	mustWrite(t, g.stage+"/"+trust.AutoWeb, []byte("this is no tar"))
	st := g.st.staged[trust.AutoWeb]
	st.SHA256 = sha([]byte("this is no tar"))
	g.st.staged[trust.AutoWeb] = st
	g.sha = st.SHA256
	if _, err := g.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("%v, want ErrVerify", err)
	}
	if b, err := os.ReadFile(g.web + "/index.html"); err != nil || string(b) != "<p>alt</p>" {
		t.Errorf("the document root changed: %q %v", b, err)
	}
	if _, err := os.Lstat(g.web + ".next"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".next was left behind: %v", err)
	}
}

func TestRollbackWebBringsThePreviousStandBack(t *testing.T) {
	f := webFixture(t)
	if _, err := f.run(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := New(f.deps).Rollback(context.Background(), trust.AutoWeb); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if b, err := os.ReadFile(f.web + "/index.html"); err != nil || string(b) != "<p>alt</p>" {
		t.Fatalf("index.html is %q, %v", b, err)
	}
	// The file the new stand brought is gone with it.
	if _, err := os.Lstat(f.web + "/css/app.css"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file of the new stand is still there: %v", err)
	}
	// Which version lies there is not known, so the part is out of the list
	// rather than carrying a version that was guessed.
	if v, ok := f.st.parts[trust.AutoWeb]; ok {
		t.Errorf("remembered %q, want the part taken out of the list", v)
	}
	if r := f.rep.last(); r.State != store.UpdateRolledBack || r.Version != "" {
		t.Errorf("report %+v", r)
	}
	// A second rollback has nothing to go back to.
	if err := New(f.deps).Rollback(context.Background(), trust.AutoWeb); !errors.Is(err, ErrNothing) {
		t.Errorf("second rollback: %v, want ErrNothing", err)
	}
}

// --- the numbers nothing else holds ---------------------------------------

func TestInstallDefaultAliveIsThirtySeconds(t *testing.T) {
	// Every other case sets Deps.Alive to keep itself quick, so this is the
	// one that does not: without it the value that counts in operation would
	// be the one value nothing holds. Its ORDER OF MAGNITUDE is what is held,
	// not the exact number, so a later adjustment does not break it.
	f := apiFixture(t)
	f.deps.Alive = 0
	f.deps.Sleep = func(d time.Duration) { f.slept = append(f.slept, d) }
	if _, err := f.run(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(f.slept) != 1 {
		t.Fatalf("waited %v, want exactly one wait", f.slept)
	}
	if f.slept[0] < 10*time.Second || f.slept[0] > 120*time.Second {
		t.Errorf("the default wait is %v, want seconds and not milliseconds or minutes", f.slept[0])
	}
	if aliveDefault != 30*time.Second {
		t.Errorf("aliveDefault is %v; the brief names 30 seconds", aliveDefault)
	}
}

func TestInstallHandlesEveryNGCPart(t *testing.T) {
	// A part added to the contract without a branch in installPart would be
	// an ErrInternal on a server. This is the static assurance that every
	// part of knownParts has one, and that knownParts is the contract's own
	// list of parts.
	want := []string{trust.AutoWeb, trust.AutoAPI, trust.AutoController}
	if !slices.Equal(slices.Sorted(slices.Values(knownParts)), slices.Sorted(slices.Values(want))) {
		t.Fatalf("knownParts is %v, want %v", knownParts, want)
	}
	for _, p := range knownParts {
		if p == trust.AutoWeb {
			if _, ok := unitOf(p); ok {
				t.Errorf("%s has a unit; it is files Apache serves", p)
			}
			continue
		}
		unit, ok := unitOf(p)
		if !ok || unit == "" {
			t.Errorf("%s has no systemd unit", p)
		}
	}
	// And every part of knownParts is one ngcPart answers yes to, so the two
	// lists cannot drift apart.
	for _, p := range knownParts {
		if !ngcPart(p) {
			t.Errorf("%s is in knownParts but ngcPart says no", p)
		}
	}
}

func TestInstallMessagesFitTheContract(t *testing.T) {
	// Every message an install sends has to pass store.ParseUpdateReport, and
	// the three new ones have to be told apart from the ones Stage sends.
	msgs := map[string]string{}
	for _, err := range []error{
		errors.Join(ErrForbidden, ErrNeedsGrants),
		errors.Join(ErrHealth, errors.New("unit failed")),
		ErrNothing,
		errors.Join(ErrForbidden, store.ErrRollback),
		ErrForbidden, ErrVerify, ErrDisk, errors.New("something else"),
	} {
		msg := message(err)
		r := store.UpdateReport{Target: trust.AutoAPI, Version: newVersion,
			State: stateOf(err), Message: msg}
		b, jerr := json.Marshal(r)
		if jerr != nil {
			t.Fatal(jerr)
		}
		got, perr := store.ParseUpdateReport(b)
		if perr != nil {
			t.Errorf("%v: the report does not parse: %v", err, perr)
			continue
		}
		if got.Message != msg || got.State != r.State {
			t.Errorf("%v: came back as %+v", err, got)
		}
		if first, ok := msgs[msg]; ok {
			t.Errorf("%v says the same as %q", err, first)
		}
		msgs[msg] = msg
	}
	if len(msgs) != 8 {
		t.Errorf("%d distinct messages, want 8", len(msgs))
	}
}

func TestInstallRefusesParamsThatDoNotParse(t *testing.T) {
	f := apiFixture(t)
	d := f.decision()
	d.Opened.Open.Params = json.RawMessage(`{"target":"ngc-api","unknown":1}`)
	if _, err := New(f.deps).Install(context.Background(), d); !errors.Is(err, ErrVerify) {
		t.Fatalf("%v, want ErrVerify", err)
	}
	if f.whichProgram() != "old" {
		t.Errorf("the program is %s, want untouched", f.whichProgram())
	}
}

func TestVersionOfAsksTheProgramItself(t *testing.T) {
	// The health check of operation, actually RUN - against a program built
	// for this case, because no ngc program exists on this machine. It needs
	// the go toolchain; without it the case says so out loud rather than
	// passing quietly.
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("LOUD SKIP: no go toolchain to build a program with: %v", err)
	}
	dir := t.TempDir()
	src := dir + "/main.go"
	mustWrite(t, src, []byte(`package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-version" {
		fmt.Println("2.3.4")
		fmt.Println("a second line nobody may read")
		return
	}
	os.Exit(3)
}
`))
	prog := dir + "/prog.exe"
	build := exec.Command(gobin, "build", "-o", prog, src)
	build.Dir = dir
	// A folder of its own, so the module of this repo is not in the way.
	build.Env = append(os.Environ(), "GO111MODULE=off", "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("LOUD SKIP: the program for this case does not build: %v\n%s", err, out)
	}
	got, err := VersionOf(context.Background(), prog)
	if err != nil {
		t.Fatalf("VersionOf: %v", err)
	}
	// The FIRST line only, trimmed: a program that says more does not grow
	// the string that is compared with a version.
	if got != "2.3.4" {
		t.Errorf("VersionOf gave %q, want %q", got, "2.3.4")
	}
	if !store.ValidVersion(got) {
		t.Errorf("%q is no version", got)
	}
	// A program that fails is an error and not an empty version: an empty
	// one would equal nothing and be taken for a part that is not installed.
	if _, err := VersionOf(context.Background(), prog+".missing"); err == nil {
		t.Error("a program that is not there gave no error")
	}
}

func TestSystemdRunsSystemctlWithAnArgumentList(t *testing.T) {
	// Systemd itself cannot run here: there is no systemd on this machine.
	// What IS held is that it names a program and not a shell, and that the
	// default is the absolute path - a relative name would be looked up in
	// PATH, which a program running as root must not do.
	s := Systemd{}
	if s.bin() != "/usr/bin/systemctl" {
		t.Errorf("default systemctl is %q", s.bin())
	}
	if !strings.HasPrefix(s.bin(), "/") {
		t.Errorf("%q is not an absolute path", s.bin())
	}
	if got := (Systemd{Bin: "/opt/sbin/systemctl"}).bin(); got != "/opt/sbin/systemctl" {
		t.Errorf("Bin is not used: %q", got)
	}
}

func TestInstallUsesTheConfiguredPaths(t *testing.T) {
	// The paths of operation come from the contract and not from a string in
	// this package: a server whose BinDir moved must not keep installing into
	// the old one.
	if config.BinDir != "/usr/lib/ngc" || config.WebRoot != "/var/www/ngc" {
		t.Errorf("config.BinDir %q, config.WebRoot %q - the comments of this file name these",
			config.BinDir, config.WebRoot)
	}
	if config.ServiceAPI == "" || config.ServiceController == "" {
		t.Error("the units of the contract are empty")
	}
	f := apiFixture(t)
	if got := New(f.deps).partFile(trust.AutoAPI); got != f.bin+"/"+trust.AutoAPI {
		t.Errorf("partFile is %q", got)
	}
}

// uiTar builds the tar of a web release: a "ui/"-rooted tar, the one shape
// the contract can unpack safely (store.WalkUI). What an ngc-web release file
// really is, is not written down anywhere - see the BLOCKED note on unpack.
//
// The blocks are written by hand and NOT with archive/tar. Measured: Go's tar
// writer puts octal zeros in devmajor and devminor, where the form of a UI tar
// has NUL bytes, so store.WalkUI refuses everything it writes - a file alone,
// a directory alone, both. The layout below is the one shared/store documents
// at the head of ui.go, field by field.
func uiTar(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	buf.Write(uiBlock(t, '5', "ui/", 0))
	seen := map[string]bool{}
	for _, n := range names {
		if i := strings.LastIndexByte(n, '/'); i > 0 && !seen[n[:i]] {
			seen[n[:i]] = true
			buf.Write(uiBlock(t, '5', "ui/"+n[:i]+"/", 0))
		}
		body := files[n]
		buf.Write(uiBlock(t, '0', "ui/"+n, int64(len(body))))
		buf.WriteString(body)
		if pad := (512 - len(body)%512) % 512; pad > 0 {
			buf.Write(make([]byte, pad))
		}
	}
	buf.Write(make([]byte, 1024)) // the end marker: two blocks of NUL
	return buf.Bytes()
}

// uiBlock is one header block of a UI tar.
func uiBlock(t testing.TB, flag byte, name string, size int64) []byte {
	t.Helper()
	if len(name) > 100 {
		t.Fatalf("%q needs a PAX header, which these cases do not build", name)
	}
	var b [512]byte
	copy(b[0:100], name)
	mode := "0000644\x00"
	if flag == '5' {
		mode = "0000755\x00"
	}
	copy(b[100:108], mode)
	copy(b[108:124], "0000000\x000000000\x00")
	copy(b[124:136], octalField(size, 12))
	copy(b[136:148], "00000000000\x00")
	b[156] = flag
	copy(b[257:265], "ustar\x0000")
	sum := 8 * int(' ')
	for i, c := range b {
		if i < 148 || i >= 156 {
			sum += int(c)
		}
	}
	copy(b[148:156], octalField(int64(sum), 7)+" ")
	return b[:]
}

// octalField formats v as octal digits and a NUL, width bytes in all.
func octalField(v int64, width int) string {
	s := strconv.FormatInt(v, 8)
	return strings.Repeat("0", width-1-len(s)) + s + "\x00"
}

// ensure the test file's own fixtures keep the shapes this package needs.
var (
	_ Services     = (*fakeServices)(nil)
	_ AppInstaller = (*fakeApps)(nil)
	_ Local        = (*fakeStore)(nil)
)

// --- point 33: the part has to belong on this server ----------------------

// installFixtureFor is an install of one NGC part that has to go through, for each
// of the three parts there are. It is one function so that the cases below
// walk the SAME path for every part instead of one path three times.
func installFixtureFor(t testing.TB, target string) *installFixture {
	t.Helper()
	switch target {
	case trust.AutoAPI:
		return apiFixture(t)
	case trust.AutoWeb:
		return webFixture(t)
	case trust.AutoController:
		f := apiFixture(t)
		f.target = trust.AutoController
		mustWrite(t, f.bin+"/"+trust.AutoController, oldProgram)
		mustWrite(t, f.stage+"/"+trust.AutoController, newProgram)
		f.health[f.bin+"/"+trust.AutoController] = newVersion
		f.st.parts = map[string]string{trust.AutoController: oldVersion}
		f.st.staged[trust.AutoController] = state.Staged{Target: trust.AutoController,
			Version: newVersion, SHA256: f.sha, Size: int64(len(newProgram)),
			Signer: enc.Bytes(releaseKey(t)), Seq: 3,
			File:  f.stage + "/" + trust.AutoController,
			State: store.UpdateDownloaded, Time: 1}
		f.deps.List = &trust.List{Auto: []trust.Auto{
			{Target: trust.AutoController, Key: enc.Bytes(releaseKey(t))}}}
		return f
	default:
		t.Fatalf("no fixture for %q", target)
		return nil
	}
}

// stand is what lies in place for the target of this fixture - "old", "new",
// "nothing" or "half". For a part that is a program it is whichProgram; the
// web interface is a directory and is read where it is unpacked, so that a
// case over all three parts can ask one question.
func (f *installFixture) stand() string {
	f.t.Helper()
	if f.target != trust.AutoWeb {
		return f.whichProgram()
	}
	b, err := os.ReadFile(f.web + "/index.html")
	switch {
	case err != nil:
		return "nothing"
	case string(b) == "<p>alt</p>":
		return "old"
	case string(b) == "<p>neu</p>":
		return "new"
	default:
		return "half"
	}
}

// TestInstallRefusesAPartThisServerDoesNotRun nails the wall of point 33 down:
// which NGC part belongs on this machine is decided at the INSTALL and not
// only on the API that sends. A database server asked to install ngc-api
// refuses; it does not copy a program into its bin directory and restart a
// service it does not own.
//
// Everything else about the request is in order - the staged row, the version,
// the checksum, the standing approval - so the role is the only thing left
// that can refuse. The intermediate run is what proves that: the SAME request
// on a server that does carry the role goes all the way through and the new
// stand lies in place. Without it this case might be measuring an earlier
// wall.
func TestInstallRefusesAPartThisServerDoesNotRun(t *testing.T) {
	for _, c := range []struct {
		name   string
		target string
		roles  []string
	}{
		// A pure database node IS sent the ngc-api file, so that it can play
		// the migrations in (the API's rollout, MigrationNode) - but that is
		// Stage. Installing it is another thing and is refused.
		{"ngc-api on a database server", trust.AutoAPI, []string{tunnel.RoleDB}},
		{"ngc-api on a services server", trust.AutoAPI, []string{tunnel.RoleServices}},
		// An API server is not a web server, and nothing but its own roles
		// says so.
		{"ngc-web on an API server", trust.AutoWeb, []string{tunnel.RoleAPI}},
		{"ngc-web on a database server", trust.AutoWeb, []string{tunnel.RoleDB}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := installFixtureFor(t, c.target)
			f.deps.Roles = c.roles
			_, err := f.run()
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("%v, want ErrForbidden", err)
			}
			// The refusal is NOT a disagreement about the file: a role is
			// this server's own property, nothing of the staged state is in
			// question, and ErrVerify would tell an admin to look at the
			// update instead of at the node.
			if errors.Is(err, ErrVerify) {
				t.Errorf("the answer carries ErrVerify as well: %v", err)
			}
			// Refused means nothing happened at all.
			if f.stand() != "old" {
				t.Errorf("what lies in place is %s, want untouched", f.stand())
			}
			if len(f.svc.restarts) != 0 {
				t.Errorf("restarts %v, want none", f.svc.restarts)
			}
			// What the admin sees: a refused update (store.UpdateRejected),
			// not a failure of this machine - that is what stateOf makes of
			// ErrForbidden, and it is the whole point of choosing it.
			if got := f.reportStates(); len(got) != 1 || got[0] != store.UpdateRejected {
				t.Errorf("reported %v, want one %s", got, store.UpdateRejected)
			}

			// INTERMEDIATE: the same target on a server that DOES run it
			// goes through to the end. So the refusal above is the role wall
			// and not one before it.
			g := installFixtureFor(t, c.target)
			g.deps.Roles = runsIt(c.target)
			if _, err := g.run(); err != nil {
				t.Fatalf("on a server with the role: %v", err)
			}
			if g.stand() != "new" {
				t.Errorf("on a server with the role what lies in place is %s, want new", g.stand())
			}
		})
	}
}

// TestInstallTakesTheControllerOnEveryServer is the other half of the same
// wall, and it keeps it from being too tight: ngc-controller runs on every NGC
// server whatever its roles are (tunnel.Components always names it), so no
// combination of roles may refuse it. A wall that refused this one would lock
// every server out of its own updates.
func TestInstallTakesTheControllerOnEveryServer(t *testing.T) {
	for _, roles := range [][]string{
		{tunnel.RoleDB},
		{tunnel.RoleServices},
		{tunnel.RoleAPI},
		{tunnel.RoleWeb},
	} {
		f := installFixtureFor(t, trust.AutoController)
		f.deps.Roles = roles
		if _, err := f.run(); err != nil {
			t.Fatalf("roles %v: %v", roles, err)
		}
		if f.stand() != "new" {
			t.Errorf("roles %v: what lies in place is %s, want new", roles, f.stand())
		}
	}
}

// TestInstallWithoutRolesIsThisProgramsOwnFault: a Manager that was handed no
// roles cannot say which parts belong here, and guessing would be the
// dangerous direction. Every configuration has one to four roles
// (config.Controller.Validate), so an empty list is this program wired wrong
// and never something a peer can bring about - ErrInternal, not a refusal of
// the peer.
func TestInstallWithoutRolesIsThisProgramsOwnFault(t *testing.T) {
	f := apiFixture(t)
	f.deps.Roles = nil
	if _, err := f.run(); !errors.Is(err, ErrInternal) {
		t.Fatalf("%v, want ErrInternal", err)
	}
	if f.stand() != "old" {
		t.Errorf("what lies in place is %s, want untouched", f.stand())
	}
}

// runsIt are the roles of a server that runs part, read out of the one mapping
// there is (tunnel.Components) instead of being written down a second time
// here: a case with its own table would still pass if the mapping changed
// under it.
func runsIt(part string) []string {
	for _, r := range []string{tunnel.RoleAPI, tunnel.RoleWeb, tunnel.RoleDB, tunnel.RoleServices} {
		if slices.Contains(tunnel.Components([]string{r}), part) {
			return []string{r}
		}
	}
	return nil
}
