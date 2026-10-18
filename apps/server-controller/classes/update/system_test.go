package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// aptStubSource is the stand-in for apt-get: a real program, built for the
// cases below, which writes down its own argument list and the one environment
// variable this contract names. So what is measured is the call System really
// makes - argument by argument, through exec and not past it - and not a note
// written beside it.
//
// It writes more than service.MaxStderr to its output on purpose, so the case
// about the bound has something to cut, and it ends with 1 on the call whose
// number stands in failon.txt beside it.
const aptStubSource = `package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The stand-in gets NO environment of the test: System hands apt-get a whole
// environment of its own, which is the point of one of the cases. So the two
// files it needs lie next to the program itself.
func main() {
	dir := filepath.Dir(os.Args[0])
	log := filepath.Join(dir, "calls.txt")
	// string(rune(10)) and not an escape: this source sits inside a Go raw
	// string in the test, where a backslash is a backslash.
	nl := string(rune(10))
	fields := append([]string{}, os.Args[1:]...)
	fields = append(fields, "env:DEBIAN_FRONTEND="+os.Getenv("DEBIAN_FRONTEND"))
	fields = append(fields, "env:LANG="+os.Getenv("LANG"))
	// Count the calls, so a case can let the first one through and fail the
	// second.
	n := 1
	if b, err := os.ReadFile(log); err == nil {
		n = strings.Count(string(b), nl) + 1
	}
	fh, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		os.Exit(9)
	}
	fh.WriteString(strings.Join(fields, " ") + nl)
	fh.Close()
	os.Stdout.WriteString(strings.Repeat("x", 200000))
	b, _ := os.ReadFile(filepath.Join(dir, "failon.txt"))
	if f, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && f == n {
		os.Stderr.WriteString("apt-get: the stand-in was told to fail on call " + strconv.Itoa(n))
		os.Exit(1)
	}
}
`

// systemFixture is one call to System.
type systemFixture struct {
	t    testing.TB
	rep  *fakeReports
	dir  string
	log  string // where the stand-in writes what it was called with
	deps Deps
	user string // the user of the channel; empty is a channel without one
}

// systemApproved is a trust list with the standing approval for system.
func systemApproved() *trust.List {
	return &trust.List{Auto: []trust.Auto{{Target: trust.AutoSystem}}}
}

func systemFix(t testing.TB) *systemFixture {
	t.Helper()
	f := &systemFixture{t: t, rep: &fakeReports{}, dir: t.TempDir()}
	// The stand-in writes beside itself, so this is the same path it builds.
	f.log = f.dir + "/calls.txt"
	f.deps = Deps{State: newStore(), List: systemApproved(), ReleaseKey: make([]byte, 32),
		StageDir: f.dir, Max: store.MaxPackage, Report: f.rep,
		Now: func() int64 { return 1790000500 }}
	prog := buildAptStub(t, f.dir)
	old := sysPaths.apt
	sysPaths.apt = prog
	t.Cleanup(func() { sysPaths.apt = old })
	return f
}

// failOn makes the stand-in end with 1 on the n-th call.
func (f *systemFixture) failOn(n int) {
	f.t.Helper()
	if err := os.WriteFile(f.dir+"/failon.txt", []byte(strconv.Itoa(n)), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// calls is what the stand-in was called with, one entry per call.
func (f *systemFixture) calls() []string {
	f.t.Helper()
	b, err := os.ReadFile(f.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// decision is the checked channel of a system install: the install action with
// target system and nothing beside it, because core.InstallParams.Validate
// refuses a version or a checksum for that target.
func (f *systemFixture) decision() access.Decision {
	f.t.Helper()
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActInstall)
	if !ok {
		f.t.Fatal("the contract has no install action")
	}
	params, err := json.Marshal(core.InstallParams{Target: trust.AutoSystem})
	if err != nil {
		f.t.Fatal(err)
	}
	return access.Decision{
		Opened: tunnel.Opened{Open: wire.Open{App: core.AppStore, Service: core.ServiceController,
			Action: core.ActInstall, Params: params}},
		Action: a, Core: true, User: f.user,
	}
}

func (f *systemFixture) run() ([]byte, error) {
	f.t.Helper()
	return New(f.deps).System(context.Background(), f.decision())
}

// buildAptStub builds the stand-in. It needs the go toolchain; without it the
// case says so out loud rather than passing quietly.
// aptStubOnce builds the stand-in ONCE for the whole package, and that is a
// measured saving and not a tidy-up: eleven cases called buildAptStub, every
// one of them ran "go build" over the same twenty lines, and every one of
// them paid about a second for it. Measured 18.10.2026 on D:, same 103 cases
// and the same assertions - the numbers are in the report of that round.
//
// What each case still gets is a stand-in of ITS OWN: buildAptStub copies the
// built program into the case's own directory, because the stand-in writes
// calls.txt and reads failon.txt NEXT TO ITSELF (see aptStubSource). One
// shared program would mean one shared call log, and the cases would read
// each other's calls - exactly the kind of case that passes alone and fails
// in company.
var aptStubOnce struct {
	sync.Once
	prog string // the built program, outside any case's directory
	err  error  // what went wrong, so every case gets the same answer
	skip string // the one reason that is a skip and not an error
}

// aptStubProgram builds the stand-in at the first call and answers the same
// program afterwards. Its directory is deliberately NOT a t.TempDir: the
// first case to ask would take it away again when it ends.
func aptStubProgram() (string, string, error) {
	aptStubOnce.Do(func() {
		gobin, err := exec.LookPath("go")
		if err != nil {
			aptStubOnce.skip = fmt.Sprintf("LOUD SKIP: no go toolchain to build the apt stand-in with: %v", err)
			return
		}
		dir, err := os.MkdirTemp("", "ngc-aptstub-")
		if err != nil {
			aptStubOnce.err = err
			return
		}
		src := filepath.Join(dir, "aptstub.go")
		if err := os.WriteFile(src, []byte(aptStubSource), 0o600); err != nil {
			aptStubOnce.err = err
			return
		}
		prog := filepath.Join(dir, "aptstub.exe")
		build := exec.Command(gobin, "build", "-o", prog, src)
		build.Dir = dir
		// A folder of its own, so the module of this repo is not in the way.
		build.Env = append(os.Environ(), "GO111MODULE=off", "GOFLAGS=")
		if out, err := build.CombinedOutput(); err != nil {
			aptStubOnce.err = fmt.Errorf("the apt stand-in does not build: %v: %s", err, out)
			return
		}
		if _, err := os.Stat(prog); err != nil {
			aptStubOnce.err = fmt.Errorf("the apt stand-in was not built: %w", err)
			return
		}
		aptStubOnce.prog = prog
	})
	return aptStubOnce.prog, aptStubOnce.skip, aptStubOnce.err
}

// buildAptStub puts a stand-in into dir and answers its path.
func buildAptStub(t testing.TB, dir string) string {
	t.Helper()
	built, skip, err := aptStubProgram()
	if skip != "" {
		t.Skip(skip)
	}
	// A stand-in that does not build is a fault of THIS file and not a
	// missing tool, so it fails instead of skipping. Measured why that
	// matters: with a skip here, ten cases skipped and "go test" printed a
	// bare "ok" - the skip lines only show under -v.
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(built)
	if err != nil {
		t.Fatalf("read the built apt stand-in: %v", err)
	}
	prog := filepath.Join(dir, "aptstub.exe")
	if err := os.WriteFile(prog, b, 0o700); err != nil {
		t.Fatalf("copy the apt stand-in: %v", err)
	}
	// The measurement checks its own preparation: a stand-in that is not
	// there, or not the whole program, would make every case below green for
	// the wrong reason.
	st, err := os.Stat(prog)
	if err != nil {
		t.Fatalf("the apt stand-in was not put down: %v", err)
	}
	if st.Size() != int64(len(b)) {
		t.Fatalf("the copy of the apt stand-in is %d bytes, the original is %d", st.Size(), len(b))
	}
	return prog
}

// wantUpdate and wantUpgrade are the two lines the stand-in writes when it is
// called the way the brief names it: the argument list, then DEBIAN_FRONTEND
// as the program itself sees it.
const (
	wantUpdate  = "-o Dpkg::Options::=--force-confold -y --no-install-recommends update env:DEBIAN_FRONTEND=noninteractive env:LANG=C.UTF-8"
	wantUpgrade = "-o Dpkg::Options::=--force-confold -y --no-install-recommends upgrade env:DEBIAN_FRONTEND=noninteractive env:LANG=C.UTF-8"
)

// --- the cases of the brief ------------------------------------------------

func TestSystemRunsAptWithAnArgumentList(t *testing.T) {
	f := systemFix(t)
	out, err := f.run()
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	if string(out) != "{}" {
		t.Errorf("result %q, want {}", out)
	}
	calls := f.calls()
	if len(calls) != 2 {
		t.Fatalf("apt-get was called %d times: %q, want update then upgrade", len(calls), calls)
	}
	// First update, then upgrade: an upgrade against the package lists of
	// last week installs last week's versions.
	if calls[0] != wantUpdate {
		t.Errorf("first call\n got %q\nwant %q", calls[0], wantUpdate)
	}
	// The list of the brief, word for word.
	if calls[1] != wantUpgrade {
		t.Errorf("second call\n got %q\nwant %q", calls[1], wantUpgrade)
	}
	if r := f.rep.last(); r.Target != trust.AutoSystem || r.State != store.UpdateInstalled || r.Version != "" {
		t.Errorf("report %+v, want target system, state installed, no version", r)
	}
	// A report has to be one an API takes.
	for _, r := range f.rep.got {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ParseUpdateReport(b); err != nil {
			t.Errorf("the report %+v is not one the contract takes: %v", r, err)
		}
	}
}

func TestSystemStartsNoShell(t *testing.T) {
	// The default path is absolute: a relative name would be looked up in
	// PATH, which a program running as root must not do.
	if !strings.HasPrefix(aptDefault, "/") {
		t.Errorf("apt-get is %q, want an absolute path", aptDefault)
	}
	// The arguments arrive AS arguments: the stand-in below sees seven
	// fields - the five of the list and the two environment variables it is
	// told to report - and a shell in between would have made another number
	// of them. Nothing in the list comes from outside this program: the step
	// is the only thing that changes, and it is one of two words written
	// here.
	for _, step := range []string{"update", "upgrade"} {
		args := aptArgs(step)
		if len(args) != 5 || args[4] != step {
			t.Errorf("aptArgs(%q) = %q, want five arguments with the step last", step, args)
		}
	}
	f := systemFix(t)
	if _, err := f.run(); err != nil {
		t.Fatalf("System: %v", err)
	}
	for _, c := range f.calls() {
		if n := len(strings.Fields(c)); n != 7 {
			t.Errorf("the call %q arrived as %d fields, want the five arguments and the environment", c, n)
		}
	}
}

func TestSystemNeedsAStandingApprovalWithoutUser(t *testing.T) {
	f := systemFix(t)
	f.deps.List = &trust.List{} // no standing approval
	if _, err := f.run(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("%v, want ErrForbidden", err)
	}
	if calls := f.calls(); calls != nil {
		t.Errorf("apt-get ran anyway: %q", calls)
	}
	if r := f.rep.last(); r.State != store.UpdateRejected || r.Target != trust.AutoSystem {
		t.Errorf("report %+v, want target system rejected", r)
	}

	// The intermediate check this case needs: with the approval put back and
	// NOTHING else changed, the very same decision reaches apt. Without it
	// the refusal above could be an earlier wall and this case would hold a
	// bolt that is not there.
	g := systemFix(t)
	if _, err := g.run(); err != nil {
		t.Fatalf("with the approval: %v", err)
	}
	if len(g.calls()) != 2 {
		t.Fatalf("with the approval apt-get ran %d times, want 2", len(g.calls()))
	}
}

func TestSystemWithAUserNeedsNoStandingApproval(t *testing.T) {
	// A signed task of a user carries its own authority; the standing
	// approval is what a channel WITHOUT one needs. And there is no version
	// comparison for system either way - core.InstallParams carries no
	// version for that target at all.
	f := systemFix(t)
	f.user = "u-1"
	f.deps.List = &trust.List{}
	if _, err := f.run(); err != nil {
		t.Fatalf("System with a user: %v", err)
	}
	if len(f.calls()) != 2 {
		t.Errorf("apt-get ran %d times, want 2", len(f.calls()))
	}
}

func TestSystemRefusesAnotherTargetAndAnotherAction(t *testing.T) {
	f := systemFix(t)
	d := f.decision()
	// A target that is not system reaching this method is this program wired
	// wrong: Install keeps every other target (install.go, task 20).
	p, err := json.Marshal(core.InstallParams{Target: trust.AutoAPI, Version: "1.2.0",
		SHA256: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	d.Opened.Open.Params = p
	if _, err := New(f.deps).System(context.Background(), d); !errors.Is(err, ErrInternal) {
		t.Errorf("another target: %v, want ErrInternal", err)
	}
	d2 := f.decision()
	d2.Action.Name = core.ActStage
	if _, err := New(f.deps).System(context.Background(), d2); !errors.Is(err, ErrInternal) {
		t.Errorf("another action: %v, want ErrInternal", err)
	}
	d3 := f.decision()
	d3.Opened.Open.Params = json.RawMessage(`{"target":"system","unknown":1}`)
	if _, err := New(f.deps).System(context.Background(), d3); !errors.Is(err, ErrVerify) {
		t.Errorf("params that do not parse: %v, want ErrVerify", err)
	}
	if calls := f.calls(); calls != nil {
		t.Errorf("apt-get ran anyway: %q", calls)
	}
}

func TestSystemWithoutItsDependenciesIsInternal(t *testing.T) {
	f := systemFix(t)
	for name, bend := range map[string]func(*Deps){
		"no list":   func(d *Deps) { d.List = nil },
		"no report": func(d *Deps) { d.Report = nil },
	} {
		d := f.deps
		bend(&d)
		if _, err := New(d).System(context.Background(), f.decision()); !errors.Is(err, ErrInternal) {
			t.Errorf("%s: %v, want ErrInternal", name, err)
		}
	}
	var nilManager *Manager
	if _, err := nilManager.System(context.Background(), f.decision()); !errors.Is(err, ErrInternal) {
		t.Errorf("nil manager: %v, want ErrInternal", err)
	}
}

func TestSystemStopsAfterAFailedUpdate(t *testing.T) {
	// A failed "update" leaves the package lists as they were, so the upgrade
	// below it must not run: it would install what was current last week and
	// report success.
	f := systemFix(t)
	f.failOn(1)
	if _, err := f.run(); !errors.Is(err, ErrSystem) {
		t.Fatalf("%v, want ErrSystem", err)
	}
	calls := f.calls()
	if len(calls) != 1 || calls[0] != wantUpdate {
		t.Errorf("apt-get was called %q, want update alone", calls)
	}
	if r := f.rep.last(); r.State != store.UpdateFailed {
		t.Errorf("report %+v, want failed", r)
	}
}

func TestSystemReportsAFailedUpgrade(t *testing.T) {
	f := systemFix(t)
	f.failOn(2)
	if _, err := f.run(); !errors.Is(err, ErrSystem) {
		t.Fatalf("%v, want ErrSystem", err)
	}
	if len(f.calls()) != 2 {
		t.Errorf("apt-get was called %q, want update and upgrade", f.calls())
	}
	if r := f.rep.last(); r.State != store.UpdateFailed {
		t.Errorf("report %+v, want failed", r)
	}
}

func TestSystemCutsTheOutputToTheBound(t *testing.T) {
	f := systemFix(t)
	f.failOn(2)
	_, err := f.run()
	if err == nil {
		t.Fatal("the failed call gave no error")
	}
	// The stand-in writes 200000 bytes. Neither the error that travels to an
	// API nor the line in the log may grow with it.
	if n := len(err.Error()); n > service.MaxStderr {
		t.Errorf("the error is %d bytes long, want at most %d", n, service.MaxStderr)
	}
	if got := len(aptCut(bytes.Repeat([]byte{'x'}, 200000))); got != service.MaxStderr {
		t.Errorf("aptCut gave %d bytes, want %d", got, service.MaxStderr)
	}
	// A control that must LIVE: output inside the bound comes back whole.
	short := []byte("apt-get: E: Could not get lock")
	if got := aptCut(short); !bytes.Equal(got, short) {
		t.Errorf("aptCut bent a short output: %q", got)
	}
}

func TestSystemRunTwiceAndBrokenOffLeaveNoLedger(t *testing.T) {
	// Twice in a row: this controller keeps NO record for system - apt keeps
	// its own - so the second run asks apt again and is not skipped. Measured
	// here, because a second run that silently did nothing would look exactly
	// like a successful one.
	f := systemFix(t)
	if _, err := f.run(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := f.run(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := len(f.calls()); n != 4 {
		t.Errorf("apt-get ran %d times over two calls, want 4", n)
	}
	st, ok := f.deps.State.(*fakeStore)
	if !ok {
		t.Fatal("the fixture's store is not the stand-in")
	}
	if len(st.staged) != 0 || st.puts != 0 {
		t.Errorf("System wrote %d rows into the store, want none", st.puts)
	}
	if len(f.rep.got) != 2 || f.rep.got[0].State != store.UpdateInstalled ||
		f.rep.got[1].State != store.UpdateInstalled {
		t.Errorf("reports %+v, want two installed", f.rep.got)
	}

	// Broken off: the context ends before apt is asked, so nothing ran, and
	// the report says anything but installed.
	g := systemFix(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(g.deps).System(ctx, g.decision()); err == nil {
		t.Fatal("a cancelled call gave no error")
	}
	if calls := g.calls(); calls != nil {
		t.Errorf("apt-get ran after the context ended: %q", calls)
	}
	if r := g.rep.last(); r.State == store.UpdateInstalled {
		t.Errorf("report %+v, want anything but installed", r)
	}
}

func TestSystemMessagesFitTheContract(t *testing.T) {
	for _, err := range []error{ErrSystem, ErrForbidden, ErrVerify, ErrInternal, context.Canceled} {
		m := systemMessage(err)
		if m == "" {
			t.Errorf("%v has no message", err)
			continue
		}
		if !wire.ValidText(m, wire.MaxMessageText) {
			t.Errorf("the message of %v is not one the contract takes: %q", err, m)
		}
	}
}
