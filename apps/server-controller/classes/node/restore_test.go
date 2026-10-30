package node

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/serve"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The two apps every case below works with.
const (
	firstApp  = "org.sorglos.notes"
	secondApp = "org.sorglos.photos"
)

// restoreEngine is an engine that writes down what it was asked and answers
// what a case told it to. Everything it holds is behind mu: Restore walks the
// apps one after another, but a case may read the log while it runs.
type restoreEngine struct {
	mu      sync.Mutex
	calls   []string
	up      map[string]bool  // containers that are already running
	runErr  map[string]error // what Running answers with instead
	failOn  map[string]error // what Start answers with
	execErr error
	// stopErr is what Stop answers with: a container that cannot be stopped is
	// a container that keeps running, and nothing may be started beside it.
	stopErr error
	// limits is what every Start was told, in order. The ceiling of an app
	// has to come out of its own manifest at every start, so a case has to be
	// able to read what was passed and not only that Start happened.
	limits []runtime.Limits
}

func newRestoreEngine() *restoreEngine {
	return &restoreEngine{up: map[string]bool{}, runErr: map[string]error{},
		failOn: map[string]error{}}
}

func (e *restoreEngine) note(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, s)
}

func (e *restoreEngine) log() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

func (e *restoreEngine) Running(_ context.Context, name string) (bool, error) {
	e.note("running " + name)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.runErr[name]; err != nil {
		return false, err
	}
	return e.up[name], nil
}

func (e *restoreEngine) Start(_ context.Context, name string, l runtime.Limits) error {
	e.limits = append(e.limits, l)
	e.note("start " + name)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.failOn[name]
}

func (e *restoreEngine) Exec(_ context.Context, x runtime.Exec) (int, error) {
	e.note("exec " + x.Name + " " + strings.Join(x.Command, " "))
	return 0, e.execErr
}

func (e *restoreEngine) LoadImage(context.Context, string, string) (string, error) {
	e.note("loadimage")
	return "", errors.New("not used here")
}
func (e *restoreEngine) RemoveImage(context.Context, string, string) error {
	e.note("removeimage")
	return nil
}
func (e *restoreEngine) Create(context.Context, runtime.Container) error {
	e.note("create")
	return nil
}
func (e *restoreEngine) Stop(_ context.Context, name string) error {
	e.note("stop " + name)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stopErr
}
func (e *restoreEngine) Remove(context.Context, string) error { e.note("remove"); return nil }

// restoreStand is one machine coming back up: a local state, an engine, and
// the two seams Restore needs beside them.
type restoreStand struct {
	t         *testing.T
	st        *state.Store
	eng       *restoreEngine
	cfg       RestoreConfig
	unlocked  []string
	ran       []serve.CallInput
	madeRun   []string         // apps whose runtime directory was made
	unlock    map[string]error // what Unlock answers for an app
	paid      map[string]bool  // apps whose licence has run out
	licErr    map[string]error // apps whose licence could not be read
	runDirErr map[string]error // apps whose runtime directory could not be made
	// noCgroup names the containers that run in no cgroup of this program:
	// the state a controller from before the limits were real leaves behind.
	noCgroup map[string]bool
}

func newRestoreStand(t *testing.T) *restoreStand {
	t.Helper()
	s := &restoreStand{t: t, st: newState(t), eng: newRestoreEngine(),
		unlock: map[string]error{}, paid: map[string]bool{}, licErr: map[string]error{},
		runDirErr: map[string]error{}, noCgroup: map[string]bool{}}
	s.cfg = RestoreConfig{
		State:  s.st,
		Engine: s.eng,
		// Wired in every case, and it has to be: without it the default
		// (runtime.RunForApp) would look an app user up on the machine
		// running the cases and make a directory under /run. These cases
		// are about the ORDER and the refusals, not about a real /run -
		// that half is measured where it belongs, on Linux and as root
		// (runtime.TestEnsureRunTightensTheDirectoryAboveItToo).
		EnsureRun: func(_ context.Context, app string) error {
			s.madeRun = append(s.madeRun, app)
			return s.runDirErr[app]
		},
		// Wired for the same reason: the default reads /sys/fs/cgroup, and
		// these cases run where there is none. Whether a cgroup really holds
		// the limits of an app is measured in runtime, with a kernel played
		// (runtime/cgroup_test.go) and on a real one (docs). Here it is only
		// the state "this container runs braked by nothing", and the stand
		// says which containers are in it.
		HasCgroup: func(name string) bool { return !s.noCgroup[name] },
		Unlock: func(_ context.Context, app string) error {
			s.unlocked = append(s.unlocked, app)
			return s.unlock[app]
		},
		Licensed: func(_ context.Context, a dbapi.App) (bool, error) {
			if err := s.licErr[a.ID]; err != nil {
				return false, err
			}
			return !s.paid[a.ID], nil
		},
		Run: func(_ context.Context, in serve.CallInput) wire.Close {
			s.ran = append(s.ran, in)
			return wire.Close{Status: wire.StatusOK}
		},
		Log: slog.New(slog.DiscardHandler),
	}
	return s
}

// standCPU and standMemory are the ceiling of the fixture app: thousandths of
// a core and MiB, as store.Manifest has them, and on purpose not the defaults
// of the contract (store.DefaultCPU 1000, store.DefaultMemory 256).
const (
	standCPU    = 500
	standMemory = 128
)

// put writes the row of an app with a manifest that names one service and one
// action, and start says whether that action is the start action.
func (s *restoreStand) put(app string, active bool, start bool) {
	s.t.Helper()
	mf := store.Manifest{ID: app, Name: "Notizen", Version: "1.0.0", Publisher: "Sorglos IT",
		// Two numbers that are neither nought nor the default of the contract,
		// so a case can tell the manifest's ceiling from a forgotten one.
		CPU: standCPU, Memory: standMemory,
		Services: []store.Service{{Name: "main", Command: []string{"/bin/app", "serve"}}},
		Actions:  []store.Action{{Name: "boot", Title: "Starten", Service: "main"}}}
	if start {
		mf.Start = "boot"
	}
	b, err := json.Marshal(mf)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := store.ParseManifest(b); err != nil {
		s.t.Fatalf("the fixture manifest is no manifest: %v", err)
	}
	if err := s.st.PutApp(context.Background(), dbapi.App{ID: app, Version: "1.0.0",
		Store: "global", Node: "ctl-1", Active: active, Manifest: b}); err != nil {
		s.t.Fatal(err)
	}
}

// Two active apps, one of them without a usable licence: the first one gets
// its data unlocked, its container started and its start action run; the
// second one gets nothing at all.
func TestRestoreStartsContainersAndRunsStartActions(t *testing.T) {
	s := newRestoreStand(t)
	s.put(firstApp, true, true)
	s.put(secondApp, true, true)
	s.paid[secondApp] = true

	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}

	first := runtime.UserName(firstApp)
	second := runtime.UserName(secondApp)
	want := []string{"running " + first, "start " + first}
	if got := s.eng.log(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the engine saw %v, want %v", got, want)
	}
	if !reflect.DeepEqual(s.unlocked, []string{firstApp}) {
		t.Fatalf("unlocked %v, want only %s", s.unlocked, firstApp)
	}
	if len(s.ran) != 1 {
		t.Fatalf("%d start actions ran, want 1", len(s.ran))
	}
	in := s.ran[0]
	if in.Container != first {
		t.Fatalf("the action ran in %q, want %q", in.Container, first)
	}
	if !reflect.DeepEqual(in.Command, []string{"/bin/app", "serve"}) {
		t.Fatalf("the action ran %v", in.Command)
	}
	if !in.Call.Start || in.Call.User != "" {
		t.Fatalf("the call is bound to a user: %+v", in.Call)
	}
	if in.Call.App != firstApp || in.Call.Service != "main" || in.Call.Action != "boot" {
		t.Fatalf("the call names %+v", in.Call)
	}
	if err := in.Call.Validate(); err != nil {
		t.Fatalf("the call of a start action is no call: %v", err)
	}
	if strings.Contains(second, first) {
		t.Fatal("the two container names are not told apart")
	}
}

// Restore needs no API: nothing it is given can reach one, and it brings the
// apps back without a tunnel and without a trust list.
func TestRestoreNeedsNoAPI(t *testing.T) {
	// The seam first: no field of RestoreConfig may be of a type that speaks
	// to an API. A later field of such a type makes this fall, which is the
	// point - the sentence above would otherwise hold only by construction.
	ty := reflect.TypeOf(RestoreConfig{})
	for i := range ty.NumField() {
		f := ty.Field(i)
		for _, bad := range []string{"link.", "tunnel.", "noisep.", "net.", "trust."} {
			if strings.Contains(f.Type.String(), bad) {
				t.Fatalf("RestoreConfig.%s is a %s, which reaches an API", f.Name, f.Type)
			}
		}
	}

	s := newRestoreStand(t)
	s.put(firstApp, true, true)
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	if len(s.ran) != 1 || len(s.unlocked) != 1 {
		t.Fatalf("without an API %d actions ran and %d apps were unlocked", len(s.ran), len(s.unlocked))
	}
}

// The data of an app is unlocked BEFORE its container starts: the keys live in
// the kernel, so a container that starts first finds the directory locked.
func TestRestoreUnlocksTheDataBeforeTheContainerStarts(t *testing.T) {
	s := newRestoreStand(t)
	var order []string
	s.cfg.Unlock = func(_ context.Context, app string) error {
		order = append(order, "unlock "+app)
		return nil
	}
	// Into the same list, so this case reads the whole order of one app and
	// not three quarters of it.
	s.cfg.EnsureRun = func(_ context.Context, app string) error {
		order = append(order, "run "+app)
		return nil
	}
	s.eng = newRestoreEngine()
	s.cfg.Engine = &orderEngine{e: s.eng, order: &order}
	s.put(firstApp, true, true)
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"unlock " + firstApp, "run " + firstApp,
		"running " + runtime.UserName(firstApp), "start " + runtime.UserName(firstApp)}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("the order was %v, want %v", order, want)
	}
}

// The runtime directory is made BEFORE the engine is asked anything, and this
// case is about that one position in the list.
//
// It is not tidiness. config.RunDir is a tmpfs handed to systemd as the unit's
// RuntimeDirectory, so after every restart the directory above is new and the
// app's own is gone - and rootless podman reads its configuration out of
// XDG_RUNTIME_DIR, so Running cannot answer without it. Measured on the Debian
// VM on 23.10.2026, as the app's user and with the arguments Podman.Running
// passes: "lstat /run/ngc/<user>: permission denied", exit 1.
//
// That closed a circle: Running fell over the missing directory, one() gave up
// at its error one line later, and Start was never reached - while the only
// caller of EnsureRun was the activation. Every installed app stayed off after
// every restart until somebody activated it anew, and the status did not even
// show it as off: appState turns this error into "nothing to say" and the
// Reporter leaves the app out of the report.
//
// So the case reads the position and not merely the call. With the step
// anywhere behind Running it would be green and the circle would still be
// there.
func TestRestoreMakesTheRuntimeDirectoryBeforeItAsksTheEngine(t *testing.T) {
	s := newRestoreStand(t)
	var order []string
	s.cfg.EnsureRun = func(_ context.Context, app string) error {
		order = append(order, "run "+app)
		return nil
	}
	s.eng = newRestoreEngine()
	s.cfg.Engine = &orderEngine{e: s.eng, order: &order}
	s.put(firstApp, true, false)
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"run " + firstApp, "running " + runtime.UserName(firstApp),
		"start " + runtime.UserName(firstApp)}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("the order was %v, want %v", order, want)
	}
}

// An app whose runtime directory cannot be made stays off, and the engine is
// not asked about it at all. The second half is the one worth holding: asking
// an engine that has no directory to read is what produced the useless line in
// the log, and a caller that goes on anyway would produce it again.
func TestRestoreAsksNoEngineWithoutARuntimeDirectory(t *testing.T) {
	s := newRestoreStand(t)
	s.runDirErr[firstApp] = errors.New("no /run")
	s.put(firstApp, true, true)
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	if got := s.eng.log(); len(got) != 0 {
		t.Fatalf("the engine was asked %v, want nothing", got)
	}
	if len(s.ran) != 0 {
		t.Fatalf("%d start actions ran, want none", len(s.ran))
	}
	// The data was unlocked before it: that step is about the keys in the
	// kernel and has nothing to do with /run, so it keeps its place.
	if !reflect.DeepEqual(s.unlocked, []string{firstApp}) {
		t.Fatalf("unlocked %v, want just the one app", s.unlocked)
	}
}

// Without a wired EnsureRun the default is runtime.RunForApp. The case does not
// run it - it needs root and a real /run - it only holds that the nil case does
// not quietly SKIP the step, which is how this bug would come back: a default
// that does nothing looks exactly like a passing test.
func TestRestoreHasNoDefaultThatSkipsTheRuntimeDirectory(t *testing.T) {
	s := newRestoreStand(t)
	s.cfg.EnsureRun = nil
	s.put(firstApp, true, false)
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	// runtime.RunForApp looks the app's user up and makes none, so on a
	// machine where this app is not installed it refuses - and that refusal
	// is the proof the default was reached. A default that skipped the step
	// would have let the engine be asked.
	if got := s.eng.log(); len(got) != 0 {
		t.Fatalf("the engine was asked %v, so the default made no runtime directory and did not refuse either", got)
	}
}

// orderEngine writes the engine's calls into the same list as the unlock, so
// the order of the two can be read off one slice.
type orderEngine struct {
	e     *restoreEngine
	order *[]string
}

func (o *orderEngine) Running(ctx context.Context, name string) (bool, error) {
	*o.order = append(*o.order, "running "+name)
	return o.e.Running(ctx, name)
}
func (o *orderEngine) Start(ctx context.Context, name string, l runtime.Limits) error {
	*o.order = append(*o.order, "start "+name)
	return o.e.Start(ctx, name, l)
}
func (o *orderEngine) Exec(ctx context.Context, x runtime.Exec) (int, error) {
	return o.e.Exec(ctx, x)
}
func (o *orderEngine) LoadImage(ctx context.Context, n, p string) (string, error) {
	return o.e.LoadImage(ctx, n, p)
}
func (o *orderEngine) RemoveImage(ctx context.Context, n, r string) error {
	return o.e.RemoveImage(ctx, n, r)
}
func (o *orderEngine) Create(ctx context.Context, c runtime.Container) error {
	return o.e.Create(ctx, c)
}
func (o *orderEngine) Stop(ctx context.Context, n string) error   { return o.e.Stop(ctx, n) }
func (o *orderEngine) Remove(ctx context.Context, n string) error { return o.e.Remove(ctx, n) }

// One app that cannot come up does not hold the others back, whichever of the
// four steps it falls at.
func TestRestoreCarriesOnAfterOneAppFailed(t *testing.T) {
	cases := map[string]func(*restoreStand){
		"the licence could not be read": func(s *restoreStand) {
			s.licErr[firstApp] = errors.New("database")
		},
		"the data stays locked": func(s *restoreStand) {
			s.unlock[firstApp] = errors.New("no fscrypt")
		},
		"the runtime directory cannot be made": func(s *restoreStand) {
			s.runDirErr[firstApp] = errors.New("no /run")
		},
		"the engine says nothing": func(s *restoreStand) {
			s.eng.runErr[runtime.UserName(firstApp)] = errors.New("no podman")
		},
		"the container does not start": func(s *restoreStand) {
			s.eng.failOn[runtime.UserName(firstApp)] = errors.New("no image")
		},
	}
	for name, bend := range cases {
		t.Run(name, func(t *testing.T) {
			s := newRestoreStand(t)
			s.put(firstApp, true, true)
			s.put(secondApp, true, true)
			bend(s)
			if err := Restore(context.Background(), s.cfg); err != nil {
				t.Fatalf("one broken app ended the whole restore: %v", err)
			}
			if len(s.ran) != 1 || s.ran[0].Call.App != secondApp {
				t.Fatalf("%d actions ran, want only the one of %s", len(s.ran), secondApp)
			}
		})
	}
}

// What stays out: an app that is not active, a row of which only kept data
// remains, and a core app.
func TestRestoreLeavesTheseAlone(t *testing.T) {
	ctx := context.Background()
	s := newRestoreStand(t)
	s.put(firstApp, false, true) // deactivated
	if err := s.st.PutApp(ctx, dbapi.App{ID: secondApp, Version: "", Store: "global",
		Node: "ctl-1", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.st.PutApp(ctx, dbapi.App{ID: coreAppID(t), Version: "1.0.0", Store: "global",
		Node: "ctl-1", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, s.cfg); err != nil {
		t.Fatal(err)
	}
	if len(s.eng.log()) != 0 || len(s.unlocked) != 0 || len(s.ran) != 0 {
		t.Fatalf("engine %v, unlocked %v, actions %d", s.eng.log(), s.unlocked, len(s.ran))
	}
}

// coreAppID is a core app id, which is what wire.CoreApp answers true for.
// It is asked and not assumed, so a change to that rule shows up here.
func coreAppID(t *testing.T) string {
	t.Helper()
	const id = "ngc.store"
	if !wire.CoreApp(id) {
		t.Fatalf("%q is no core app any more", id)
	}
	return id
}

// An app whose manifest names no start action comes up all the same; only the
// action stays away.
func TestRestoreRunsNoActionWithoutOne(t *testing.T) {
	s := newRestoreStand(t)
	s.put(firstApp, true, false)
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	if len(s.ran) != 0 {
		t.Fatalf("%d actions ran for an app without a start action", len(s.ran))
	}
	if len(s.unlocked) != 1 || len(s.eng.log()) != 2 {
		t.Fatalf("the app did not come up: unlocked %v, engine %v", s.unlocked, s.eng.log())
	}
}

// A container that is already running is not started a second time, and its
// start action runs all the same: this is the boot path, and whatever brought
// the container up did not run the action.
func TestRestoreDoesNotStartWhatRuns(t *testing.T) {
	s := newRestoreStand(t)
	s.put(firstApp, true, true)
	s.eng.up[runtime.UserName(firstApp)] = true
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"running " + runtime.UserName(firstApp)}
	if got := s.eng.log(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the engine saw %v, want %v", got, want)
	}
	if len(s.ran) != 1 {
		t.Fatalf("%d start actions ran, want 1", len(s.ran))
	}
}

// A container that is up and in no cgroup of this program is stopped and
// started again, into one.
//
// This is the one state no start would ever reach, and it is the state the
// whole plant was in until 30.10.2026: podman accepted --pids-limit, --memory
// and --cpus, wrote them into its own answer and dropped them, so an app ran
// with the ceiling of the controller and none of its own
// (runtime/cgroup.go). A running container is not started again by anything,
// so without this step such an app would keep running unbraked until somebody
// activated it anew - which is exactly how the fault survived three measured
// nodes for a week.
func TestRestoreStartsAgainWhatRunsOutsideItsOwnLimits(t *testing.T) {
	s := newRestoreStand(t)
	s.put(firstApp, true, true)
	name := runtime.UserName(firstApp)
	s.eng.up[name] = true
	s.noCgroup[name] = true
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"running " + name, "stop " + name, "start " + name}
	if got := s.eng.log(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the engine saw %v, want %v", got, want)
	}
	// And it is started with the ceiling of its own manifest, not with
	// whatever a start without limits would mean.
	if len(s.eng.limits) != 1 {
		t.Fatalf("%d starts, want 1", len(s.eng.limits))
	}
	if s.eng.limits[0] != (runtime.Limits{CPU: standCPU, Memory: standMemory}) {
		t.Fatalf("started with %+v, want the manifest's %d/%d",
			s.eng.limits[0], standCPU, standMemory)
	}
}

// A container that cannot be stopped keeps running, and the app is NOT started
// a second time beside it. Two containers of one app would be two writers of
// its data directory.
func TestRestoreLeavesAnUnstoppableContainerAloneRatherThanDoublingIt(t *testing.T) {
	s := newRestoreStand(t)
	s.put(firstApp, true, true)
	name := runtime.UserName(firstApp)
	s.eng.up[name] = true
	s.noCgroup[name] = true
	s.eng.stopErr = errors.New("no")
	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"running " + name, "stop " + name}
	if got := s.eng.log(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the engine saw %v, want %v", got, want)
	}
	if len(s.ran) != 0 {
		t.Fatalf("%d start actions ran, want none", len(s.ran))
	}
}

// A Restore that is not wired up touches nothing and says so, once and the
// same way for every missing half.
func TestRestoreSaysWhenItIsNotWiredUp(t *testing.T) {
	cases := map[string]func(*RestoreConfig){
		"no state":   func(c *RestoreConfig) { c.State = nil },
		"no engine":  func(c *RestoreConfig) { c.Engine = nil },
		"no unlock":  func(c *RestoreConfig) { c.Unlock = nil },
		"no licence": func(c *RestoreConfig) { c.Licensed = nil },
	}
	for name, bend := range cases {
		t.Run(name, func(t *testing.T) {
			s := newRestoreStand(t)
			s.put(firstApp, true, true)
			bend(&s.cfg)
			if err := Restore(context.Background(), s.cfg); !errors.Is(err, ErrNotWired) {
				t.Fatalf("answered %v, want ErrNotWired", err)
			}
			if len(s.eng.log()) != 0 || len(s.unlocked) != 0 || len(s.ran) != 0 {
				t.Fatal("a Restore that is not wired up changed something")
			}
		})
	}
}

// errNoRows is what a database that cannot be read answers with.
var errNoRows = errors.New("test: the app list cannot be read")

// muteStore is a local state whose list of apps fails. Everything else is a
// real memstore.
type muteStore struct{ dbapi.Local }

func (muteStore) AppList(context.Context) ([]dbapi.App, error) { return nil, errNoRows }

// A database that cannot be read is not an app that failed: Restore gives that
// error back instead of walking on over it, because it cannot know what it did
// not see.
func TestRestoreGivesBackABrokenDatabase(t *testing.T) {
	s := newRestoreStand(t)
	s.cfg.State = state.New(muteStore{Local: memstore.New()})
	if err := Restore(context.Background(), s.cfg); !errors.Is(err, errNoRows) {
		t.Fatalf("answered %v, want the error of the database", err)
	}
	if len(s.eng.log()) != 0 || len(s.unlocked) != 0 || len(s.ran) != 0 {
		t.Fatal("something was started although no row could be read")
	}
}
