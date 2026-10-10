package install

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// The installation and the clock every case below starts from.
const (
	testInstall = "AAAAAAAAAAAAAAAAAAAAAA" // trust.ValidID: 16 bytes, base64url
	testNow     = int64(1790000000)
	testImage   = "ngc/notes:1.0.0"
)

// imageTar is an image tar as podman writes one: one manifest member that
// names exactly one tag. runtime.ImageTag reads the name out of this and out
// of nothing else, so a case that bends the tag measures that reading.
func imageTar(t testing.TB, ref string) []byte {
	t.Helper()
	body, err := json.Marshal([]map[string]any{{"RepoTags": []string{ref}}})
	if err != nil {
		t.Fatal(err)
	}
	return buildTar(t, []entry{{"manifest.json", body, tar.TypeReg}})
}

// appEntries is a package of the manifest m with a real image tar for every
// architecture NGC is built for, so the case runs on the machine it is written
// on and on an arm64 server alike.
func appEntries(t testing.TB, m *store.Manifest) []entry {
	t.Helper()
	img := imageTar(t, testImage)
	es := []entry{
		manifestEntry(t, m),
		{"ui", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Notizen</title>"), tar.TypeReg},
		{"ui/icon.svg", []byte("<svg/>"), tar.TypeReg},
		{"services/amd64.tar", img, tar.TypeReg},
		{"services/arm64.tar", img, tar.TypeReg},
	}
	return es
}

// engine records every call and answers as a case asks it to.
type engine struct {
	mu    sync.Mutex
	calls []string
	// fail names the call that fails, e.g. "Start"; its error is errEngine.
	fail string
	// running is what Running answers.
	running bool
	// loaded is the reference LoadImage answers with; empty: the one in the
	// tar, which is what the real engine answers.
	loaded     string
	created    runtime.Container
	removedImg []string
}

var errEngine = errors.New("the engine says no")

func (e *engine) did(call string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, call)
	if e.fail == call {
		return errEngine
	}
	return nil
}

func (e *engine) log() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.calls)
}

func (e *engine) LoadImage(_ context.Context, _, tarPath string) (string, error) {
	if err := e.did("LoadImage"); err != nil {
		return "", err
	}
	if e.loaded != "" {
		return e.loaded, nil
	}
	return runtime.ImageTag(tarPath)
}

func (e *engine) RemoveImage(_ context.Context, _, ref string) error {
	e.mu.Lock()
	e.removedImg = append(e.removedImg, ref)
	e.mu.Unlock()
	return e.did("RemoveImage")
}

func (e *engine) Create(_ context.Context, c runtime.Container) error {
	e.mu.Lock()
	e.created = c
	e.mu.Unlock()
	return e.did("Create")
}
func (e *engine) Start(_ context.Context, _ string) error  { return e.did("Start") }
func (e *engine) Stop(_ context.Context, _ string) error   { return e.did("Stop") }
func (e *engine) Remove(_ context.Context, _ string) error { return e.did("Remove") }
func (e *engine) Running(_ context.Context, _ string) (bool, error) {
	if err := e.did("Running"); err != nil {
		return false, err
	}
	return e.running, nil
}
func (e *engine) Exec(_ context.Context, _ runtime.Exec) (int, error) { return 0, e.did("Exec") }

// sys stands in for everything that changes this machine. The real three need
// root and a kernel that can encrypt a directory, so without this nothing
// after them could be measured here - and what a case wants to know is the
// order they were called in and what was taken back.
type sys struct {
	calls []string
	fail  string
	user  runtime.User
	data  string
}

var errSys = errors.New("the machine says no")

func (s *sys) did(call string) error {
	s.calls = append(s.calls, call)
	if s.fail == call {
		return errSys
	}
	return nil
}

func (s *sys) EnsureUser(_ context.Context, _ string) (runtime.User, error) {
	if err := s.did("EnsureUser"); err != nil {
		return runtime.User{}, err
	}
	return s.user, nil
}
func (s *sys) DropUser(_ context.Context, _ string) error { return s.did("DropUser") }
func (s *sys) EnsureData(_ context.Context, _ string, _ runtime.User) (string, error) {
	if err := s.did("EnsureData"); err != nil {
		return "", err
	}
	return s.data, nil
}
func (s *sys) DropData(_ context.Context, _ string) error { return s.did("DropData") }
func (s *sys) EnsureRun(_ context.Context, u runtime.User) (string, error) {
	if err := s.did("EnsureRun"); err != nil {
		return "", err
	}
	return "/run/ngc/" + u.Name, nil
}
func (s *sys) DropRun(_ string) error { return s.did("DropRun") }

// apps is this controller's local store, in memory.
type apps struct {
	rows     map[string]dbapi.App
	licences map[string]struct {
		env    sign.Envelope
		issued int64
	}
	putErr error
	puts   int
}

func newApps() *apps {
	return &apps{rows: map[string]dbapi.App{}, licences: map[string]struct {
		env    sign.Envelope
		issued int64
	}{}}
}

func (a *apps) App(_ context.Context, id string) (dbapi.App, error) {
	row, ok := a.rows[id]
	if !ok {
		return dbapi.App{}, dbapi.ErrNotFound
	}
	return row, nil
}

func (a *apps) PutApp(_ context.Context, row dbapi.App) error {
	a.puts++
	if a.putErr != nil {
		return a.putErr
	}
	a.rows[row.ID] = row
	return nil
}

func (a *apps) License(_ context.Context, app string) (sign.Envelope, int64, error) {
	l, ok := a.licences[app]
	if !ok {
		return sign.Envelope{}, 0, dbapi.ErrNotFound
	}
	return l.env, l.issued, nil
}

func (a *apps) PutLicense(_ context.Context, app string, env sign.Envelope, issued int64) error {
	a.licences[app] = struct {
		env    sign.Envelope
		issued int64
	}{env, issued}
	return nil
}

// stand is one activation: the package the API sends, the task an admin
// signed, and the three stand-ins the setup goes through. Every case bends one
// thing of it and calls activate.
type stand struct {
	t      *testing.T
	f      *fixture
	mf     *store.Manifest
	eng    *engine
	sys    *sys
	apps   *apps
	deps   Deps
	grants []string
}

// newStand is an activation that has to come off: a store app of the trust
// list, a package whose checksum and UI hash are the ones the task names, a
// server with the role services, and a manifest with one service and no needs.
func newStand(t *testing.T) *stand {
	t.Helper()
	mf := appManifest(testApp, testVer, "")
	s := &stand{t: t, mf: mf, eng: &engine{}, apps: newApps(),
		sys: &sys{user: runtime.User{Name: "ngc-notes-0123abcd", UID: 4711, GID: 4712},
			data: "/var/lib/ngc/apps/" + testApp}}
	s.f = catalogFixture(t)
	s.setPackage()
	s.deps = Deps{State: s.apps, Engine: s.eng, Sys: s.sys, List: s.f.in.List,
		Roles: []string{tunnel.RoleServices}, Node: "ctl-1", NGC: testNGC,
		StageDir: filepath.Join(t.TempDir(), "staged"), Hold: "/usr/lib/ngc/ngc-controller",
		Max: store.MaxPackage, LastSeq: func(string) uint64 { return 0 },
		Now: func() int64 { return testNow }}
	return s
}

// setPackage builds the package of the current manifest and carries its
// checksum and UI hash into the header, the catalogue and the task - so a case
// that bends the manifest measures the manifest and not a stale checksum.
func (s *stand) setPackage() {
	s.t.Helper()
	pkg := buildTar(s.t, appEntries(s.t, s.mf))
	s.f.setFile(pkg)
	ui := uiHashOf(s.t, pkg)
	s.f.in.UI = ui
	if s.f.tr.Catalog != nil {
		s.f.withCatalog(func(c *store.Catalog) {
			c.Apps[0].Versions[0].UI = ui
			c.Apps[0].Versions[0].Version = s.mf.Version
		})
	}
}

// withManifest rewrites the manifest and builds the package again.
func (s *stand) withManifest(fn func(m *store.Manifest)) *stand {
	fn(s.mf)
	s.setPackage()
	return s
}

// params are the parameters of the signed task: they name the package by its
// checksum and its UI hash, and the grants an admin allowed.
func (s *stand) params() core.ActivateParams {
	return core.ActivateParams{App: s.mf.ID, Version: s.mf.Version, SHA256: enc.Bytes(s.f.in.SHA256),
		UI: enc.Bytes(s.f.in.UI), Store: testStore, Grants: s.grants}
}

// decision is the checked channel: the task carries the parameters, and the
// OPEN carries other ones, so every case measures which of the two Activate
// reads.
func (s *stand) decision(p core.ActivateParams) access.Decision {
	s.t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		s.t.Fatal(err)
	}
	other, err := json.Marshal(core.ActivateParams{App: "org.example.other", Version: "9.9.9",
		SHA256: enc.Bytes(bytes.Repeat([]byte{0xee}, 32)), UI: enc.Bytes(bytes.Repeat([]byte{0xee}, 32)),
		Store: testStore})
	if err != nil {
		s.t.Fatal(err)
	}
	a, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	d := access.Decision{Action: a, Core: true,
		Task: &task.Task{Installation: testInstall, Node: "ctl-1", App: core.AppStore,
			Service: core.ServiceController, Action: core.ActActivate, Params: raw}}
	d.Opened.Open.Params = other
	return d
}

// activate runs the activation of the current state of the stand.
func (s *stand) activate() ([]byte, error) {
	s.t.Helper()
	return New(s.deps).Activate(s.t.Context(), s.decision(s.params()), bytes.NewReader(s.f.stream()))
}

// row is the row the activation wrote, or the zero value.
func (s *stand) row() dbapi.App { return s.apps.rows[s.mf.ID] }

// staged is how many folders are left under StageDir. A package of up to
// store.MaxPackage bytes may be staged, so nothing of it may stay behind.
func (s *stand) staged() int {
	s.t.Helper()
	es, err := os.ReadDir(s.deps.StageDir)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return len(es)
}

// Grants have to be the needs of the package as a set: a grant too few is a
// container with less than the app asked for, a grant too many is a right an
// admin signed for nothing.
func TestActivateGrantsMustEqualNeeds(t *testing.T) {
	cases := map[string]struct {
		needs, grants []string
		want          error
	}{
		"network for network":     {[]string{store.NeedNetwork}, []string{store.NeedNetwork}, nil},
		"nothing for nothing":     {nil, nil, nil},
		"both for both":           {[]string{store.NeedNetwork, store.NeedRoot}, []string{store.NeedRoot, store.NeedNetwork}, nil},
		"no grant for a need":     {[]string{store.NeedNetwork}, nil, ErrForbidden},
		"a grant too many":        {[]string{store.NeedNetwork}, []string{store.NeedNetwork, store.NeedRoot}, ErrForbidden},
		"the other grant":         {[]string{store.NeedNetwork}, []string{store.NeedRoot}, ErrForbidden},
		"a grant without a need":  {nil, []string{store.NeedNetwork}, ErrForbidden},
		"two grants for one need": {[]string{store.NeedRoot}, []string{store.NeedRoot, store.NeedNetwork}, ErrForbidden},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStand(t)
			s.withManifest(func(m *store.Manifest) { m.Needs = c.needs })
			s.grants = c.grants
			_, err := s.activate()
			if !errors.Is(err, c.want) {
				t.Fatalf("Activate answered %v, want %v", err, c.want)
			}
			if c.want != nil {
				if len(s.sys.calls) != 0 {
					t.Fatalf("a refused activation changed this machine: %v", s.sys.calls)
				}
				if len(s.eng.log()) != 0 {
					t.Fatalf("a refused activation called the engine: %v", s.eng.log())
				}
				if s.staged() != 0 {
					t.Fatal("a refused activation left the staged package behind")
				}
				return
			}
			// The needs of the package reach the container, and they are the
			// grants of the task by then.
			if s.eng.created.Network != slices.Contains(c.needs, store.NeedNetwork) ||
				s.eng.created.Root != slices.Contains(c.needs, store.NeedRoot) {
				t.Fatalf("the container got network=%v root=%v for needs %v",
					s.eng.created.Network, s.eng.created.Root, c.needs)
			}
		})
	}
}

// Services run only on a server with the role services. The case holds both
// halves: the same package is refused without the role and activated with it,
// so what is measured is the role and not the package.
func TestActivateRefusesServicesWithoutRoleServices(t *testing.T) {
	for _, roles := range [][]string{nil, {tunnel.RoleWeb}, {tunnel.RoleAPI, tunnel.RoleDB}} {
		s := newStand(t)
		s.deps.Roles = roles
		if _, err := s.activate(); !errors.Is(err, ErrForbidden) {
			t.Fatalf("roles %v answered %v, want ErrForbidden", roles, err)
		}
		if len(s.sys.calls) != 0 || len(s.eng.log()) != 0 {
			t.Fatalf("roles %v set something up: %v %v", roles, s.sys.calls, s.eng.log())
		}
	}
	// And with the role, the same package comes off: so what the cases above
	// measure is the role and not something of the package.
	s := newStand(t)
	if _, err := s.activate(); err != nil {
		t.Fatalf("an app with services on a services server: %v", err)
	}
}

// An app whose manifest names no service has no image and no container, and it
// needs no role either. This is not a hole in the package: store.Walk refuses
// a package that carries an image without a service and one that names a
// service without an image (shared/store/pack.go, "(len(m.Services) > 0) !=
// (images > 0)"), and store.Manifest.Validate allows a manifest with a UI and
// no service. So such an app is its row, its user and its data directory.
//
// The brief of this task names the container path only. This case is the one
// it does not mention, and it is what made the implementation branch.
func TestActivateTakesNoContainerForAnAppWithoutServices(t *testing.T) {
	s := newStand(t)
	s.deps.Roles = []string{tunnel.RoleWeb}
	s.mf.Services, s.mf.Actions = nil, nil
	// The package of such an app carries no image either, or Walk refuses it.
	pkg := buildTar(t, []entry{
		manifestEntry(t, s.mf),
		{"ui", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Notizen</title>"), tar.TypeReg},
		{"ui/icon.svg", []byte("<svg/>"), tar.TypeReg},
	})
	s.f.setFile(pkg)
	ui := uiHashOf(t, pkg)
	s.f.in.UI = ui
	s.f.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].UI = ui })
	if _, err := s.activate(); err != nil {
		t.Fatalf("an app without services on a web server: %v", err)
	}
	// No image and no container of its own: what the engine is asked at all
	// is whether an older version left one, and that one is taken away.
	if calls := s.eng.log(); slices.Contains(calls, "LoadImage") || slices.Contains(calls, "Create") ||
		slices.Contains(calls, "Start") {
		t.Fatalf("an app without services got a container: %v", calls)
	}
	if want := []string{"EnsureUser", "EnsureData", "DropRun"}; !slices.Equal(s.sys.calls, want) {
		t.Fatalf("this machine was changed as %v, want %v", s.sys.calls, want)
	}
	if row := s.row(); row.Version != testVer || !row.Active {
		t.Fatalf("the row of an app without services is %+v", row)
	}
	if s.staged() != 0 {
		t.Fatal("the staged package stayed behind")
	}

	// A version that HAD services before: its container runs, and it has to
	// go. Nothing else would ever stop it, because every later call reads the
	// manifest of the row, and that one names no service any more.
	s2 := newStand(t)
	s2.deps.Roles = []string{tunnel.RoleWeb}
	s2.apps.rows[testApp] = dbapi.App{ID: testApp, Version: "0.9.0", Store: testStore, Node: "ctl-1"}
	s2.eng.running = true
	s2.mf.Services, s2.mf.Actions = nil, nil
	s2.f.setFile(pkg)
	s2.f.in.UI = ui
	s2.f.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].UI = ui })
	if _, err := s2.activate(); err != nil {
		t.Fatalf("an app that loses its services: %v", err)
	}
	calls := s2.eng.log()
	stop, remove := slices.Index(calls, "Stop"), slices.Index(calls, "Remove")
	if stop < 0 || remove < 0 || stop > remove {
		t.Fatalf("the container of the version before was not stopped and removed: %v", calls)
	}
	if !slices.Contains(s2.sys.calls, "DropRun") {
		t.Fatalf("the runtime directory stayed behind: %v", s2.sys.calls)
	}
}

// An older version is refused, the same one and a newer one replace what is
// there - and both keep the data: nothing of the app's own is dropped, and the
// number the row carries for kept data stays its own.
func TestActivateRefusesOlderVersion(t *testing.T) {
	cases := map[string]struct {
		installed, now string
		want           error
	}{
		"older minor":        {"1.2.0", "1.1.0", ErrForbidden},
		"older patch":        {"1.2.1", "1.2.0", ErrForbidden},
		"older major":        {"2.0.0", "1.9.9", ErrForbidden},
		"the same again":     {"1.2.0", "1.2.0", nil},
		"newer patch":        {"1.2.0", "1.2.1", nil},
		"newer minor":        {"1.2.0", "1.3.0", nil},
		"nothing installed":  {"", "1.0.0", nil},
		"only kept data":     {"", "1.0.0", nil},
		"newer after a jump": {"1.2.0", "9.9.9", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStand(t)
			s.withManifest(func(m *store.Manifest) { m.Version = c.now })
			if c.installed != "" || name == "only kept data" {
				s.apps.rows[testApp] = dbapi.App{ID: testApp, Version: c.installed,
					Store: testStore, Node: "ctl-1", KeptData: 4096, Grants: []string{store.NeedRoot}}
			}
			_, err := s.activate()
			if !errors.Is(err, c.want) {
				t.Fatalf("installed %q, now %q answered %v, want %v", c.installed, c.now, err, c.want)
			}
			if c.want != nil {
				if len(s.sys.calls) != 0 || len(s.eng.log()) != 0 {
					t.Fatalf("a rollback set something up: %v %v", s.sys.calls, s.eng.log())
				}
				// The row of the version that is installed stays untouched.
				if row := s.row(); row.Version != c.installed || !slices.Equal(row.Grants, []string{store.NeedRoot}) {
					t.Fatalf("a rollback changed the row: %+v", row)
				}
				return
			}
			if slices.Contains(s.sys.calls, "DropData") {
				t.Fatalf("the data of the app was dropped: %v", s.sys.calls)
			}
			if row := s.row(); row.Version != c.now {
				t.Fatalf("the row says version %q, want %q", row.Version, c.now)
			} else if c.installed != "" && row.KeptData != 4096 {
				t.Fatalf("the row lost the kept data: %d", row.KeptData)
			}
		})
	}
}

// The UI hash of the signed task has to be the one of the package, and nothing
// is set up for a package where it is not. The mirror case is the one that
// makes this measure the UI hash at all: the same package with the right hash
// goes through, so the refusal is not some earlier wall.
func TestActivateRefusesWrongUIHash(t *testing.T) {
	s := newStand(t)
	// Both places the hash stands: the task and the catalogue entry. Bending
	// only one of them would measure the other comparison.
	other := bytes.Repeat([]byte{0x33}, 32)
	s.f.in.UI = other
	s.f.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].UI = other })
	if _, err := s.activate(); !errors.Is(err, ErrVerify) {
		t.Fatalf("a wrong UI hash answered %v, want ErrVerify", err)
	}
	if len(s.sys.calls) != 0 {
		t.Fatalf("a wrong UI hash changed this machine: %v", s.sys.calls)
	}
	if len(s.eng.log()) != 0 {
		t.Fatalf("a wrong UI hash called the engine: %v", s.eng.log())
	}
	if s.apps.puts != 0 {
		t.Fatal("a wrong UI hash wrote a row")
	}
	if s.staged() != 0 {
		t.Fatal("a wrong UI hash left the staged package behind")
	}
	if _, err := newStand(t).activate(); err != nil {
		t.Fatalf("the same package with the right UI hash: %v", err)
	}
}

// The row the activation writes: the store, this node, the manifest, the
// checksum of the package and the grants of the task.
func TestActivateWritesTheRowWithItsGrants(t *testing.T) {
	s := newStand(t)
	s.withManifest(func(m *store.Manifest) { m.Needs = []string{store.NeedNetwork} })
	s.grants = []string{store.NeedNetwork}
	out, err := s.activate()
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if string(out) != "{}" {
		t.Fatalf("Activate answered %q, want {}", out)
	}
	row := s.row()
	if row.ID != testApp || row.Version != testVer || row.Store != testStore || row.Node != "ctl-1" {
		t.Fatalf("the row names %q %q %q %q", row.ID, row.Version, row.Store, row.Node)
	}
	if !row.Active {
		t.Fatal("the row is not active")
	}
	if !bytes.Equal(row.SHA256, s.f.in.SHA256) {
		t.Fatalf("the row carries the checksum %x, want %x", row.SHA256, s.f.in.SHA256)
	}
	if !slices.Equal(row.Grants, []string{store.NeedNetwork}) {
		t.Fatalf("the row carries the grants %v", row.Grants)
	}
	if row.Updated != testNow {
		t.Fatalf("the row says %d, want %d", row.Updated, testNow)
	}
	// The manifest in the row is the one that was checked, read back the way
	// every later caller reads it.
	back, err := store.ParseManifest(row.Manifest)
	if err != nil {
		t.Fatalf("the manifest of the row does not parse: %v", err)
	}
	if back.ID != testApp || back.Version != testVer ||
		!slices.Equal(back.Needs, []string{store.NeedNetwork}) ||
		len(back.Services) != len(s.mf.Services) {
		t.Fatalf("the manifest of the row is another one: %+v", back)
	}
	// The row is written after the container was started, and not before: a
	// row that says the app runs while it does not is the one thing the order
	// is for.
	calls := s.eng.log()
	if i := slices.Index(calls, "Start"); i < 0 {
		t.Fatalf("the container was never started: %v", calls)
	}
	if s.apps.puts != 1 {
		t.Fatalf("the row was written %d times", s.apps.puts)
	}
	// And the staged package is gone.
	if s.staged() != 0 {
		t.Fatal("the staged package stayed behind after a success")
	}
}

// The setup happens in the order the contract needs it in, and the runtime
// directory of the container is there before the container is: podman writes
// into it while it creates one (runtime.EnsureRun).
func TestActivateMakesUserDataAndRunDirBeforeTheContainer(t *testing.T) {
	s := newStand(t)
	if _, err := s.activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if want := []string{"EnsureUser", "EnsureData", "EnsureRun"}; !slices.Equal(s.sys.calls, want) {
		t.Fatalf("this machine was changed as %v, want %v", s.sys.calls, want)
	}
	calls := s.eng.log()
	if i := slices.Index(calls, "LoadImage"); i != 0 {
		t.Fatalf("the image was not loaded first: %v", calls)
	}
	if slices.Index(calls, "Create") < slices.Index(calls, "LoadImage") {
		t.Fatalf("the container was created before its image: %v", calls)
	}
	if slices.Index(calls, "Start") < slices.Index(calls, "Create") {
		t.Fatalf("the container was started before it was created: %v", calls)
	}
	// The container carries the user of the app, its data directory as /data
	// and the program that keeps it alive.
	c := s.eng.created
	if c.Name != s.sys.user.Name || c.UID != s.sys.user.UID || c.GID != s.sys.user.GID {
		t.Fatalf("the container runs as %q %d:%d", c.Name, c.UID, c.GID)
	}
	if c.DataDir != s.sys.data || c.Hold != "/usr/lib/ngc/ngc-controller" || c.Image != testImage {
		t.Fatalf("the container got %+v", c)
	}
}

// Whatever fails, nothing of the setup stays behind - and for an app that was
// installed before, its user and its data are never among what is taken back:
// an update keeps the data.
func TestActivateTakesEveryStepBackWhenOneFails(t *testing.T) {
	cases := []struct {
		fail   string
		engine bool
		back   []string
	}{
		{"EnsureData", false, []string{"DropUser"}},
		{"EnsureRun", false, []string{"DropData", "DropUser"}},
		{"LoadImage", true, []string{"DropRun", "DropData", "DropUser"}},
		{"Create", true, []string{"DropRun", "DropData", "DropUser"}},
		{"Start", true, []string{"DropRun", "DropData", "DropUser"}},
	}
	for _, c := range cases {
		t.Run(c.fail+" fails for a new app", func(t *testing.T) {
			s := newStand(t)
			if c.engine {
				s.eng.fail = c.fail
			} else {
				s.sys.fail = c.fail
			}
			_, err := s.activate()
			if err == nil {
				t.Fatal("Activate came off although a step failed")
			}
			var undone []string
			for _, call := range s.sys.calls {
				if strings.HasPrefix(call, "Drop") {
					undone = append(undone, call)
				}
			}
			if !slices.Equal(undone, c.back) {
				t.Fatalf("%s failed and %v was taken back, want %v", c.fail, undone, c.back)
			}
			if s.apps.puts != 0 {
				t.Fatal("a failed activation wrote a row")
			}
			if s.staged() != 0 {
				t.Fatal("a failed activation left the staged package behind")
			}
			// The image goes out again wherever it was already in.
			if c.fail == "Create" || c.fail == "Start" {
				if len(s.eng.removedImg) != 1 || s.eng.removedImg[0] != testImage {
					t.Fatalf("the image was not taken out again: %v", s.eng.removedImg)
				}
			}
		})
		t.Run(c.fail+" fails for an update", func(t *testing.T) {
			s := newStand(t)
			s.apps.rows[testApp] = dbapi.App{ID: testApp, Version: "0.9.0", Store: testStore,
				Node: "ctl-1", KeptData: 4096}
			if c.engine {
				s.eng.fail = c.fail
			} else {
				s.sys.fail = c.fail
			}
			if _, err := s.activate(); err == nil {
				t.Fatal("Activate came off although a step failed")
			}
			for _, call := range s.sys.calls {
				if call == "DropUser" || call == "DropData" {
					t.Fatalf("an update that failed took the app's own away: %v", s.sys.calls)
				}
			}
			// The row of the version that was installed stays.
			if row := s.row(); row.Version != "0.9.0" {
				t.Fatalf("the row says %q, want the version that was installed", row.Version)
			}
		})
	}
}

// The parameters come from the signed task and from nothing else. The OPEN of
// every stand carries another app, another version and another checksum, so
// this case fails the moment Activate reads it.
func TestActivateReadsTheSignedTaskAndNotTheOpen(t *testing.T) {
	s := newStand(t)
	if _, err := s.activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if row := s.row(); row.ID != testApp {
		t.Fatalf("the row names %q, the app of the OPEN", row.ID)
	}
	if _, there := s.apps.rows["org.example.other"]; there {
		t.Fatal("the app of the OPEN was activated")
	}
	// And without a task nothing is activated at all: this action is a root
	// task, and without one nothing names an app an admin signed for.
	d := s.decision(s.params())
	d.Task = nil
	if _, err := New(s.deps).Activate(t.Context(), d, bytes.NewReader(s.f.stream())); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an activation without a task answered %v, want ErrForbidden", err)
	}
}

// Parameters of a signed task that this NGC cannot read are a verification
// failure, and nothing of the package is read for them.
func TestActivateRefusesParamsThatAreNone(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":                  "",
		"no app":                 `{"version":"1.0.0","sha256":"","ui":"","store":"global"}`,
		"a field too many":       `{"app":"org.sorglos.notes","version":"1.0.0","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","ui":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","store":"global","what":1}`,
		"a grant that is none":   `{"app":"org.sorglos.notes","version":"1.0.0","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","ui":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","store":"global","grants":["everything"]}`,
		"a version that is none": `{"app":"org.sorglos.notes","version":"1.0","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","ui":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","store":"global"}`,
		"a core app":             `{"app":"ngc.store","version":"1.0.0","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","ui":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","store":"global"}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newStand(t)
			d := s.decision(s.params())
			d.Task.Params = []byte(raw)
			_, err := New(s.deps).Activate(t.Context(), d, bytes.NewReader(s.f.stream()))
			if !errors.Is(err, ErrVerify) {
				t.Fatalf("params %q answered %v, want ErrVerify", raw, err)
			}
			if len(s.sys.calls) != 0 || len(s.eng.log()) != 0 {
				t.Fatal("params that are none set something up")
			}
		})
	}
}

// A licence is checked against this installation and against the clock, and
// nothing is set up for one that has run out.
func TestActivateChecksTheLicenceBeforeAnythingIsSetUp(t *testing.T) {
	cases := map[string]struct {
		expires int64
		inst    string
		app     string
		signer  byte
		want    error
	}{
		"valid":                     {0, testInstall, testApp, 1, nil},
		"a subscription that runs":  {testNow + 86400, testInstall, testApp, 1, nil},
		"expired but in the grace":  {testNow - 86400, testInstall, testApp, 1, nil},
		"expired past the grace":    {testNow - 30*86400, testInstall, testApp, 1, ErrLicense},
		"of another installation":   {0, "BBBBBBBBBBBBBBBBBBBBBB", testApp, 1, ErrLicense},
		"of another app":            {0, testInstall, "org.example.other", 1, ErrLicense},
		"signed by somebody else":   {0, testInstall, testApp, 9, ErrLicense},
		"expired exactly at expiry": {testNow, testInstall, testApp, 1, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStand(t)
			s.seal(c.inst, c.app, c.expires, c.signer)
			_, err := s.activate()
			if !errors.Is(err, c.want) {
				t.Fatalf("Activate answered %v, want %v", err, c.want)
			}
			if c.want != nil {
				if len(s.sys.calls) != 0 || len(s.eng.log()) != 0 {
					t.Fatalf("a refused licence set something up: %v %v", s.sys.calls, s.eng.log())
				}
				if len(s.apps.licences) != 0 {
					t.Fatal("a refused licence was kept")
				}
				return
			}
			if _, kept := s.apps.licences[testApp]; !kept {
				t.Fatal("the licence was not kept")
			}
		})
	}
}

// Only the newest licence is kept: a licence older than the one this
// controller has changes nothing, a newer one replaces it.
func TestActivateKeepsOnlyTheNewestLicence(t *testing.T) {
	s := newStand(t)
	s.apps.licences[testApp] = struct {
		env    sign.Envelope
		issued int64
	}{sign.Envelope{}, testNow}
	// The licence of the package is issued a year ago (seal), so it is older
	// than the one this controller has - and the older one changes nothing.
	s.seal(testInstall, testApp, 0, 1)
	if _, err := s.activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := s.apps.licences[testApp].issued; got != testNow {
		t.Fatalf("the kept licence says %d, want the newer %d", got, testNow)
	}
}

// seal makes the package a paid one: it is encrypted with a content key, and
// the header carries that key and a licence of the store.
func (s *stand) seal(installation, app string, expires int64, signerFill byte) {
	s.t.Helper()
	key := bytes.Repeat([]byte{0x7c}, store.KeySize)
	plain := buildTar(s.t, appEntries(s.t, s.mf))
	sealed := encrypt(s.t, plain, key)
	// The UI hash is the one of the plain package; the checksum is the one of
	// the sealed file the store delivers.
	ui := uiHashOf(s.t, plain)
	s.f.file = sealed
	s.f.tr.Size, s.f.tr.SHA256 = int64(len(sealed)), sha(sealed)
	s.f.in.SHA256, s.f.in.UI = sha(sealed), ui
	s.f.withCatalog(func(c *store.Catalog) {
		c.Apps[0].Versions[0].Size, c.Apps[0].Versions[0].SHA256 = int64(len(sealed)), sha(sealed)
		c.Apps[0].Versions[0].UI = ui
	})
	s.f.tr.Key = enc.Bytes(key)
	l := store.License{ID: "lic-1", Store: testStore, App: app, Installation: installation,
		InstallKey: enc.Bytes(bytes.Repeat([]byte{0x11}, 32)), Per: store.PerSite,
		Issued: testNow - 365*86400, Expires: expires,
		Key: &store.WrappedKey{Ephemeral: enc.Bytes(bytes.Repeat([]byte{0x22}, 32)),
			Nonce: enc.Bytes(bytes.Repeat([]byte{0x33}, 12)), Sealed: enc.Bytes(bytes.Repeat([]byte{0x44}, 48))}}
	body, err := json.Marshal(l)
	if err != nil {
		s.t.Fatal(err)
	}
	_, priv := signer(signerFill)
	env, err := sign.Sign(priv, sign.PurposeLicense, body)
	if err != nil {
		s.t.Fatal(err)
	}
	s.f.tr.License = &env
}

// A manager that is not wired up answers one way for every missing half, and
// never touches this machine. None of it can be brought about by a peer.
func TestActivateSaysWhenItIsNotWiredUp(t *testing.T) {
	cases := map[string]func(d *Deps){
		"no state":    func(d *Deps) { d.State = nil },
		"no engine":   func(d *Deps) { d.Engine = nil },
		"no system":   func(d *Deps) { d.Sys = nil },
		"no list":     func(d *Deps) { d.List = nil },
		"no stagedir": func(d *Deps) { d.StageDir = "" },
		"no bound":    func(d *Deps) { d.Max = 0 },
		"no lastseq":  func(d *Deps) { d.LastSeq = nil },
		"no clock":    func(d *Deps) { d.Now = nil },
	}
	for name, bend := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStand(t)
			bend(&s.deps)
			if _, err := s.activate(); !errors.Is(err, ErrInternal) {
				t.Fatalf("%s answered %v, want ErrInternal", name, err)
			}
			if len(s.sys.calls) != 0 || len(s.eng.log()) != 0 {
				t.Fatal("a manager that is not wired up changed something")
			}
		})
	}
	// And a reader that is none, which is the same kind of mistake.
	s := newStand(t)
	if _, err := New(s.deps).Activate(t.Context(), s.decision(s.params()), nil); !errors.Is(err, ErrInternal) {
		t.Fatalf("an activation without a reader answered %v, want ErrInternal", err)
	}
	// A manager that is nil answers the same and does not take the controller
	// down with it: the controller runs as root.
	var nothing *Manager
	if _, err := nothing.Activate(t.Context(), s.decision(s.params()), bytes.NewReader(nil)); !errors.Is(err, ErrInternal) {
		t.Fatalf("a manager that is nil answered %v, want ErrInternal", err)
	}
}

// The image of this architecture is the one that is loaded, and its name is
// read out of the tar - not out of what the engine says. An engine that
// answers with another reference gets the image taken out again.
func TestActivateTakesTheImageOfThisArchitecture(t *testing.T) {
	s := newStand(t)
	if _, err := s.activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if s.eng.created.Image != testImage {
		t.Fatalf("the container runs the image %q, want %q", s.eng.created.Image, testImage)
	}
	// The package of the stand carries an image for amd64 and for arm64, and
	// the one of this build is the one that was read. The case says which that
	// was, so a run on another architecture is still a measurement.
	t.Logf("the image of %s was loaded", goruntime.GOARCH)

	// An engine that names another image than the tar does.
	s = newStand(t)
	s.eng.loaded = "somebody/else:latest"
	if _, err := s.activate(); !errors.Is(err, ErrVerify) {
		t.Fatalf("an engine that renames the image answered %v, want ErrVerify", err)
	}
	if len(s.eng.removedImg) != 1 || s.eng.removedImg[0] != "somebody/else:latest" {
		t.Fatalf("the image the engine named was not taken out: %v", s.eng.removedImg)
	}
	if s.apps.puts != 0 {
		t.Fatal("a row was written for an image nobody could name")
	}
}

// A package without the image of this architecture is refused, and it is
// refused before the old container is touched.
func TestActivateRefusesAPackageWithoutAnImage(t *testing.T) {
	s := newStand(t)
	s.f.setFile(buildTar(t, []entry{
		manifestEntry(t, s.mf),
		{"ui", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Notizen</title>"), tar.TypeReg},
		{"ui/icon.svg", []byte("<svg/>"), tar.TypeReg},
		{"services/amd64.tar", []byte("this is no image tar"), tar.TypeReg},
		{"services/arm64.tar", []byte("this is no image tar"), tar.TypeReg},
	}))
	ui := uiHashOf(t, s.f.file)
	s.f.in.UI = ui
	s.f.withCatalog(func(c *store.Catalog) { c.Apps[0].Versions[0].UI = ui })
	if _, err := s.activate(); err == nil {
		t.Fatal("a package without an image came off")
	}
	if calls := s.eng.log(); len(calls) != 0 {
		t.Fatalf("the engine was called for a package without an image: %v", calls)
	}
	// And everything that was set up is gone again.
	if want := []string{"EnsureUser", "EnsureData", "EnsureRun", "DropRun", "DropData", "DropUser"}; !slices.Equal(s.sys.calls, want) {
		t.Fatalf("this machine kept %v, want %v", s.sys.calls, want)
	}
}

// The old container is stopped before it is removed, and only when it runs.
func TestActivateStopsTheOldContainerOnlyWhenItRuns(t *testing.T) {
	s := newStand(t)
	s.eng.running = true
	if _, err := s.activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	calls := s.eng.log()
	stop, remove := slices.Index(calls, "Stop"), slices.Index(calls, "Remove")
	if stop < 0 || remove < 0 || stop > remove {
		t.Fatalf("a running container was not stopped before it was removed: %v", calls)
	}
	s = newStand(t)
	s.eng.running = false
	if _, err := s.activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if calls := s.eng.log(); slices.Contains(calls, "Stop") {
		t.Fatalf("a container that does not run was stopped: %v", calls)
	}
}

// A row that cannot be written is a failed activation, and the container does
// not stay behind: a container nothing knows about would run for good.
func TestActivateTakesTheContainerBackWhenTheRowFails(t *testing.T) {
	s := newStand(t)
	s.apps.putErr = errors.New("the database says no")
	if _, err := s.activate(); err == nil {
		t.Fatal("Activate came off although the row failed")
	}
	calls := s.eng.log()
	// Stop and Remove after Start: the container that was just started.
	start := slices.Index(calls, "Start")
	if start < 0 || !slices.Contains(calls[start:], "Remove") {
		t.Fatalf("the container stayed behind: %v", calls)
	}
	if len(s.eng.removedImg) != 1 {
		t.Fatalf("the image stayed behind: %v", s.eng.removedImg)
	}
	if _, there := s.apps.rows[testApp]; there {
		t.Fatal("a row was kept although writing it failed")
	}
}

// What Accept refuses, Activate refuses with it: the one case here is the
// store that is not in this installation's trust list, so the chain from the
// task to Accept is measured and not assumed. Everything Accept refuses is
// measured in package_test.go.
func TestActivateCarriesTheRefusalsOfAccept(t *testing.T) {
	s := newStand(t)
	s.deps.List.Stores[0].ID = "another-store"
	if _, err := s.activate(); !errors.Is(err, ErrSource) {
		t.Fatalf("a store outside the trust list answered %v, want ErrSource", err)
	}
	if len(s.sys.calls) != 0 {
		t.Fatalf("a package of an unknown store changed this machine: %v", s.sys.calls)
	}
}

// Host is the System of operation, and what it does cannot be measured here:
// every one of its six calls needs root, and two of them a kernel that can
// encrypt a directory. What this case holds is the one thing that can be held
// without them - that Host is a System at all, so a Manager of operation can
// be wired with it. The calls themselves are measured in package runtime,
// where their stand-ins live.
func TestHostIsTheSystemOfOperation(t *testing.T) {
	var s System = Host{ServerKey: bytes.Repeat([]byte{0x5a}, 64)}
	if _, err := s.EnsureUser(t.Context(), "no.app.at.all"); err == nil {
		t.Fatal("Host made a user for an app id that is none")
	}
	if err := s.DropRun("../etc"); err == nil {
		t.Fatal("Host dropped a runtime directory for a name that is none")
	}
}
