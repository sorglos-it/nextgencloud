package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// A stand-in for the one thing these cases are about: a kernel that takes a
// number and does not hold it.
//
// Why a stand-in at all, and why it is not more generous than the contract.
// The fault this file measures is exactly that a write was ACCEPTED and
// dropped, and a regular file cannot act that way - it reads back whatever was
// written to it, so against os.WriteFile alone the readback in writeLimit
// would be a check of itself and every mutation of it would live. So the kernel
// is played here, with the two freedoms a real one has: which controllers it
// hands down to a child, and which numbers it keeps.
//
// What it does NOT do: it never holds a number that was not written, and it
// never hands down a controller that was not asked of it. Everything the cases
// below assert is therefore something the real kernel did on svc-1 on
// 30.10.2026 as well, where the numbers were read out of /sys/fs/cgroup.
type fakeKernel struct {
	t *testing.T
	// root is the tree, in place of /sys/fs/cgroup.
	root string
	// offer is what the root of the tree hands down (cgroup.controllers).
	offer []string
	// drop names cgroup files whose write is accepted and thrown away - the
	// whole fault, in one map.
	drop map[string]bool
	// joined are the cgroup directories a call of podman was put into, in
	// order. A management call adds nothing.
	joined []string
	// placed is whether the stand-in writes a task into cgroup.procs when a
	// call is put into a cgroup, as the kernel does. A case about a start that
	// went past its cgroup turns it off.
	placed bool
	// staysFull is the kernel that does NOT empty the cgroup on a
	// cgroup.kill. Only the case about a kill that did not take sets it, and
	// that case is the one in which no app may be started.
	staysFull bool
	// killed are the cgroups a cgroup.kill was written to, in order.
	killed []string
}

func aKernel(t *testing.T, offer ...string) *fakeKernel {
	t.Helper()
	if len(offer) == 0 {
		offer = []string{"cpuset", "cpu", "io", "memory", "hugetlb", "pids", "rdma", "misc"}
	}
	k := &fakeKernel{t: t, root: t.TempDir(), offer: offer,
		drop: map[string]bool{}, placed: true}
	// A root cgroup that HAS these controllers and also hands them down. The
	// two are different files on a real machine and were the bug of
	// 30.10.2026: cgroup.controllers is what this cgroup has,
	// cgroup.subtree_control is what its children get. Here both carry the
	// same set, and the case about the difference sets them apart itself
	// (TestEnsureCgroupAsksTheParentAndNotTheRoot).
	k.write(filepath.Join(k.root, "cgroup.controllers"), strings.Join(offer, " "))
	k.write(filepath.Join(k.root, "cgroup.subtree_control"), strings.Join(offer, " "))

	was := paths
	t.Cleanup(func() { paths = was })
	paths.cgroupRoot = k.root
	// A new directory in this tree comes with the files a cgroup has, which is
	// what the kernel does and what os.Mkdir does not.
	paths.mkdir = func(path string, perm os.FileMode) error {
		if err := os.Mkdir(path, perm); err != nil {
			return err
		}
		if !strings.HasPrefix(path, k.root) {
			return nil
		}
		// A child gets what its parent enabled in its subtree_control, which
		// is the rule of cgroup v2 and the reason a missing controller above
		// is a missing file below.
		for _, c := range strings.Fields(k.read(filepath.Dir(path) + "/cgroup.subtree_control")) {
			switch c {
			case "pids":
				k.write(path+"/pids.max", "max")
				k.write(path+"/pids.events", "max 0")
			case "memory":
				k.write(path+"/memory.max", "max")
				k.write(path+"/memory.swap.max", "max")
			case "cpu":
				k.write(path+"/cpu.max", "max 100000")
			}
			k.append(path+"/cgroup.controllers", c)
		}
		k.write(path+"/cgroup.subtree_control", "")
		k.write(path+"/cgroup.procs", "")
		// cgroup.kill is there in every cgroup of a v2 tree, and the write
		// seam above refuses a file that is not there.
		k.write(path+"/cgroup.kill", "0")
		return nil
	}
	paths.cgroupWrite = func(path, value string) error {
		if _, err := os.Stat(path); err != nil {
			return err
		}
		if k.drop[filepath.Base(path)] {
			// Taken and thrown away. This is the fault, played.
			return nil
		}
		if filepath.Base(path) == "cgroup.subtree_control" {
			// The kernel adds to this file, it does not replace it - and a
			// write naming ONE controller it does not have is rejected
			// ENTIRELY, taking the others of the same write with it.
			//
			// That last half was missing here until 30.10.2026, and it is why
			// every case of this file passed while the real kernel refused on
			// svc-2: a stand-in that quietly drops what it cannot do is more
			// generous than the contract, and then it measures its own
			// generosity (messen.md). Measured shape of the real thing:
			// "+pids +io" into a parent offering "cpu memory pids" leaves the
			// file EMPTY and answers ENOENT.
			// What is available is what THIS cgroup has, not what the root
			// has - the same distinction the code under test got wrong.
			mine := map[string]bool{}
			for _, c := range strings.Fields(k.read(filepath.Dir(path) + "/cgroup.controllers")) {
				mine[c] = true
			}
			var ask []string
			for _, c := range strings.Fields(value) {
				c = strings.TrimPrefix(c, "+")
				if !mine[c] {
					return os.ErrNotExist
				}
				ask = append(ask, c)
			}
			for _, c := range ask {
				k.append(path, c)
			}
			return nil
		}
		if filepath.Base(path) == "cgroup.kill" {
			// The kernel turns a "1" here into a SIGKILL for EVERY task of
			// this cgroup, and afterwards cgroup.procs is empty. The
			// emptying is the WIRKUNG of this write, and a stand-in that
			// only takes the write and leaves cgroup.procs standing would let
			// every case about KillCgroup pass without a single task dying -
			// the same trap as the subtree_control above, one layer on
			// (messen.md: the check has to hit the effect, not its
			// preparation). k.staysFull plays the kernel that does not get
			// everything, which is the one case that must NOT start an app.
			if value != "1" {
				return os.ErrInvalid
			}
			// Noted and not stored: cgroup.kill is write-only on a real
			// kernel - a read of it answers 0, whatever was written. A
			// stand-in that kept the value would let a case assert something
			// the kernel does not offer, and then the case would measure the
			// stand-in (this is how the case below failed first).
			// The path as the CALLER spelled it, not filepath.Dir of it: a
			// cgroup path is built with "/" everywhere, and on Windows
			// filepath.Dir hands back backslashes - then the case compares
			// two spellings of one directory and fails for a reason that has
			// nothing to do with killing.
			k.killed = append(k.killed, strings.TrimSuffix(path, "/cgroup.kill"))
			if !k.staysFull {
				k.write(filepath.Dir(path)+"/cgroup.procs", "")
			}
			return nil
		}
		return os.WriteFile(path, []byte(value), 0o600)
	}
	// A rmdir of a cgroup takes the directory with its files, because they are
	// the kernel's and not the filesystem's. Here they are ordinary files, so
	// the stand-in has to say so - otherwise this case would measure the
	// difference between the two and not DropCgroup.
	paths.cgroupDrop = os.RemoveAll
	paths.cgroupJoin = func(attr *syscall.SysProcAttr, dir *os.File) (*syscall.SysProcAttr, error) {
		if dir == nil {
			return attr, nil
		}
		k.joined = append(k.joined, filepath.Base(dir.Name()))
		if k.placed {
			k.write(dir.Name()+"/cgroup.procs", "4711")
		}
		return attr, nil
	}
	return k
}

// handsDown sets what the root of the tree gives to its children
// (cgroup.subtree_control), apart from what it HAS (cgroup.controllers). On a
// real machine the two differ, and a case that cannot tell them apart cannot
// see the bug of 30.10.2026.
func (k *fakeKernel) handsDown(cs ...string) {
	k.write(filepath.Join(k.root, "cgroup.subtree_control"), strings.Join(cs, " "))
}

func (k *fakeKernel) has(c string) bool {
	for _, o := range k.offer {
		if o == c {
			return true
		}
	}
	return false
}

func (k *fakeKernel) write(path, value string) {
	k.t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		k.t.Fatal(err)
	}
}

func (k *fakeKernel) append(path, value string) {
	k.t.Helper()
	have := strings.Fields(k.read(path))
	for _, h := range have {
		if h == value {
			return
		}
	}
	k.write(path, strings.TrimSpace(strings.Join(append(have, value), " ")))
}

func (k *fakeKernel) read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// of reads one cgroup file of one app.
func (k *fakeKernel) of(app, file string) string {
	return k.read(filepath.Join(k.root, cgroupParent, app, file))
}

// 1. The three numbers land in the kernel, and they are the numbers of the
// manifest - not podman's defaults and not the ceiling of the unit.
//
// This is the case that would have failed before 30.10.2026 for the plainest
// reason: nothing wrote a cgroup file at all.
func TestEnsureCgroupPutsTheThreeLimitsIntoTheKernel(t *testing.T) {
	k := aKernel(t)
	held, err := ensureCgroup("ngc-uhr-1", Limits{CPU: 500, Memory: 128})
	if err != nil {
		t.Fatal(err)
	}
	if !held.PIDs || !held.Memory || !held.CPU {
		t.Fatalf("held = %s, want all three", held)
	}
	want := map[string]string{
		"pids.max":        strconv.Itoa(PIDsLimit),
		"memory.max":      strconv.Itoa(128 << 20),
		"memory.swap.max": "0",
		"cpu.max":         "50000 100000",
	}
	for file, w := range want {
		if got := k.of("ngc-uhr-1", file); got != w {
			t.Errorf("%s = %q, want %q", file, got, w)
		}
	}
}

// 2. Nought in the manifest is the default of the contract, never "no limit".
// The same reading createArgs has, and the one place a mutant could quietly
// turn into max.
func TestEnsureCgroupReadsNoughtAsTheDefaultOfTheContract(t *testing.T) {
	k := aKernel(t)
	if _, err := ensureCgroup("ngc-uhr-1", Limits{}); err != nil {
		t.Fatal(err)
	}
	if got, want := k.of("ngc-uhr-1", "memory.max"),
		strconv.Itoa(store.DefaultMemory<<20); got != want {
		t.Errorf("memory.max = %q, want %q (store.DefaultMemory)", got, want)
	}
	if got, want := k.of("ngc-uhr-1", "cpu.max"),
		strconv.Itoa(store.DefaultCPU*cpuPeriod/1000)+" "+strconv.Itoa(cpuPeriod); got != want {
		t.Errorf("cpu.max = %q, want %q (store.DefaultCPU)", got, want)
	}
}

// 3. THE CASE THIS WHOLE FILE IS FOR. A kernel that takes the number and does
// not hold it must not get an app started. Until 30.10.2026 exactly this
// happened on three nodes, and podman inspect said all three limits were on.
func TestEnsureCgroupRefusesWhenTheKernelDoesNotHoldTheLimit(t *testing.T) {
	for _, file := range []string{"pids.max", "memory.max"} {
		t.Run(file, func(t *testing.T) {
			k := aKernel(t)
			k.drop[file] = true
			held, err := ensureCgroup("ngc-uhr-1", Limits{})
			if !errors.Is(err, ErrCgroupDropped) {
				t.Fatalf("err = %v, want ErrCgroupDropped", err)
			}
			// The error has to name the file and both numbers: an error that
			// only says "a limit" sends whoever reads it to all three.
			if !strings.Contains(err.Error(), file) {
				t.Errorf("err = %v, does not name %s", err, file)
			}
			if file == "pids.max" && held.PIDs {
				t.Error("held says pids holds, and it does not")
			}
			if file == "memory.max" && held.Memory {
				t.Error("held says memory holds, and it does not")
			}
		})
	}
}

// 4. A controller that was never handed down is the same refusal, and it is a
// different way in: there is no file to write at all. Measured shape of the
// real thing - on svc-1 the unit's cgroup has no cpu.max file, because
// system.slice hands cpu to nobody.
func TestEnsureCgroupRefusesWithoutThePidsOrTheMemoryController(t *testing.T) {
	for _, offer := range [][]string{{"memory", "cpu"}, {"pids", "cpu"}} {
		t.Run(strings.Join(offer, "+"), func(t *testing.T) {
			aKernel(t, offer...)
			if _, err := ensureCgroup("ngc-uhr-1", Limits{}); !errors.Is(err, ErrCgroupDropped) {
				t.Fatalf("err = %v, want ErrCgroupDropped", err)
			}
		})
	}
}

// 5. And the other half of that decision, which is a decision and not a
// measurement: a missing cpu controller does NOT stop the app. A share of a
// core is being a good neighbour, not a wall, and a node that hands cpu to
// nobody would otherwise run no app at all. What it must not do is claim it.
func TestACgroupWithoutTheCPUControllerStillStartsTheApp(t *testing.T) {
	k := aKernel(t, "pids", "memory")
	held, err := ensureCgroup("ngc-uhr-1", Limits{})
	if err != nil {
		t.Fatalf("err = %v, want the app to start", err)
	}
	if held.CPU {
		t.Error("held claims cpu holds, and there is no cpu controller")
	}
	if !held.PIDs || !held.Memory {
		t.Fatalf("held = %s, want pids and memory", held)
	}
	if k.of("ngc-uhr-1", "cpu.max") != "" {
		t.Error("a cpu.max was written where no cpu controller was handed down")
	}
}

// 5b. THE CASE THE FIRST VERSION OF THIS FILE NEEDED AND DID NOT HAVE: one
// controller that is not there must not cost the other two.
//
// The root of the tree HAS the cpu controller and does not HAND IT DOWN - two
// different files, and at boot they really differ, because systemd enables a
// controller only once a unit needs it. The first version asked the parent for
// all three in ONE write, and a write naming one controller the parent does
// not offer is rejected ENTIRELY: pids and memory went down with cpu, nothing
// was enabled, and the app did not start. Measured on svc-2 on 30.10.2026 by
// rolling it out.
//
// What has to hold is therefore about the WRITE, not about which file is read:
// each controller on its own, so pids and memory hold while cpu is honestly
// reported missing. Reading the parent rather than the root is kept because it
// is the right file and gives the right reason in the error - but it is not
// what this case holds, and a mutation of it stays alive on purpose
// (the mutation list says so).
func TestOneMissingControllerDoesNotCostTheOthers(t *testing.T) {
	k := aKernel(t, "cpuset", "cpu", "io", "memory", "hugetlb", "pids", "rdma", "misc")
	k.handsDown("memory", "pids") // what svc-2 looked like at boot
	held, err := ensureCgroup("ngc-uhr-1", Limits{Memory: 128})
	if err != nil {
		t.Fatalf("err = %v, want the app to start on pids and memory", err)
	}
	if !held.PIDs || !held.Memory {
		t.Fatalf("held = %s, want pids and memory - they must not go down with cpu", held)
	}
	if held.CPU {
		t.Error("held claims cpu, which the root does not hand down")
	}
	// The intermediate check, without which this case could pass on a parent
	// that offers nothing at all: the two that must hold really are in the
	// kernel, with the numbers asked for.
	if got, want := k.of("ngc-uhr-1", "pids.max"), strconv.Itoa(PIDsLimit); got != want {
		t.Errorf("pids.max = %q, want %q", got, want)
	}
	if got, want := k.of("ngc-uhr-1", "memory.max"), strconv.Itoa(128<<20); got != want {
		t.Errorf("memory.max = %q, want %q", got, want)
	}
}

// 6. No cgroup v2 tree, no brake, no start. A directory without
// cgroup.controllers is not a cgroup tree, and everything below would make
// ordinary directories and read back exactly what it wrote: a brake that
// measures itself.
func TestEnsureCgroupRefusesWithoutACgroupV2Tree(t *testing.T) {
	k := aKernel(t)
	if err := os.Remove(filepath.Join(k.root, "cgroup.controllers")); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureCgroup("ngc-uhr-1", Limits{}); !errors.Is(err, ErrCgroup) {
		t.Fatalf("err = %v, want ErrCgroup", err)
	}
}

// 7. Called again, it writes again. A cgroup lives in a tmpfs and is gone
// after every restart of the machine, and after a half-finished start the
// directory may carry the numbers of a run before. So the second call is not
// allowed to believe the directory it finds.
func TestEnsureCgroupWritesAgainIntoACgroupAlreadyThere(t *testing.T) {
	k := aKernel(t)
	if _, err := ensureCgroup("ngc-uhr-1", Limits{Memory: 128}); err != nil {
		t.Fatal(err)
	}
	// Somebody else widened it - this is what a run before, or a kernel
	// default, leaves behind.
	k.write(filepath.Join(k.root, cgroupParent, "ngc-uhr-1", "memory.max"), "max")
	if _, err := ensureCgroup("ngc-uhr-1", Limits{Memory: 128}); err != nil {
		t.Fatal(err)
	}
	if got, want := k.of("ngc-uhr-1", "memory.max"), strconv.Itoa(128<<20); got != want {
		t.Errorf("memory.max = %q, want %q", got, want)
	}
}

// 8. A name that is no container name reaches no directory. The name becomes a
// path here, so it is held against the same grammar the engine holds it
// against - not against "it is not empty".
func TestTheCgroupCallsRefuseANameThatIsNoName(t *testing.T) {
	aKernel(t)
	for _, name := range []string{"", "..", "a/b", "-x", strings.Repeat("a", 33)} {
		if _, err := ensureCgroup(name, Limits{}); !errors.Is(err, ErrName) {
			t.Errorf("ensureCgroup(%q) = %v, want ErrName", name, err)
		}
		if err := DropCgroup(name); !errors.Is(err, ErrName) {
			t.Errorf("DropCgroup(%q) = %v, want ErrName", name, err)
		}
		if HasCgroup(name) {
			t.Errorf("HasCgroup(%q) says yes", name)
		}
	}
}

// 9. HasCgroup answers the one state nothing else can see: a container that is
// up and braked by nothing, because a controller from before this file started
// it. An empty cgroup.procs is exactly that state, and it must not read as
// "there is a cgroup".
func TestHasCgroupIsAboutTasksAndNotAboutADirectory(t *testing.T) {
	k := aKernel(t)
	if HasCgroup("ngc-uhr-1") {
		t.Error("says yes without a cgroup at all")
	}
	if _, err := ensureCgroup("ngc-uhr-1", Limits{}); err != nil {
		t.Fatal(err)
	}
	if HasCgroup("ngc-uhr-1") {
		t.Error("says yes for a cgroup that holds no task")
	}
	k.write(filepath.Join(k.root, cgroupParent, "ngc-uhr-1", "cgroup.procs"), "4711")
	if !HasCgroup("ngc-uhr-1") {
		t.Error("says no for a cgroup that holds a task")
	}
}

// 10. DropCgroup takes the directory away, and a cgroup that is not there is
// no error - after a restart of the machine none of them is.
func TestDropCgroupTakesTheDirectoryAndForgivesAMissingOne(t *testing.T) {
	k := aKernel(t)
	if _, err := ensureCgroup("ngc-uhr-1", Limits{}); err != nil {
		t.Fatal(err)
	}
	if err := DropCgroup("ngc-uhr-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(k.root, cgroupParent, "ngc-uhr-1")); err == nil {
		t.Error("the cgroup is still there")
	}
	if err := DropCgroup("ngc-uhr-1"); err != nil {
		t.Errorf("a second call = %v, want nothing", err)
	}
}

// 11. Which calls of podman run inside the ceiling and which do not. The two
// that make the processes of the app go in; the ones that manage the container
// stay out, because inside it an app at its own ceiling could no longer be
// stopped.
func TestOnlyTheCallsThatMakeTheAppsProcessesRunInsideItsCgroup(t *testing.T) {
	f := fakePodman(t)
	k := aKernel(t)
	// fakePodman replaces nothing of the cgroup seam and aKernel replaces
	// nothing of the engine, so the order of the two above does not matter -
	// but paths is one variable, and aKernel saves and restores the whole of
	// it, so it has to come second. If it ever came first, the fake podman
	// would be gone. That is what this line is: the check that the
	// preparation of this case held (messen.md).
	if paths.cgroupRoot != k.root {
		t.Fatal("the preparation of this case did not hold")
	}
	name := anApp(t).name
	p := NewPodman(f.bin)
	ctx := context.Background()
	if err := p.Start(ctx, name, Limits{}); err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := p.Remove(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Running(ctx, name); err != nil {
		t.Fatal(err)
	}
	if len(k.joined) != 1 || k.joined[0] != name {
		t.Fatalf("calls put into a cgroup: %q, want exactly one, %q", k.joined, name)
	}
}

// 12. The measurement that catches the fault of 24.10.2026 at the moment it
// happens: the container came up, and it came up outside its ceiling. It is
// stopped again, and the error says so - an app that cannot be braked does not
// run on this server.
func TestAContainerThatCameUpOutsideItsCgroupIsStoppedAgain(t *testing.T) {
	f := fakePodman(t)
	k := aKernel(t)
	k.placed = false // the kernel took the handle and put nothing in it
	name := anApp(t).name
	err := NewPodman(f.bin).Start(context.Background(), name, Limits{})
	if !errors.Is(err, ErrCgroupDropped) {
		t.Fatalf("err = %v, want ErrCgroupDropped", err)
	}
	// And what podman said about it is in the error, instead of on the floor.
	// The fake writes one line to stderr at every call; the real one writes
	// 477 bytes ending in "Falling back to --cgroup-manager=cgroupfs".
	if !strings.Contains(err.Error(), "podman said") {
		t.Errorf("err = %v, carries nothing podman wrote", err)
	}
	args := f.args(t)
	if !strings.Contains(strings.Join(args, " "), "stop") {
		t.Errorf("podman was called with %q, and nothing stopped the container", args)
	}
}

// 13. A service of an action runs inside the ceiling of its app as well.
// Measured on 25.10.2026 (point 95 of the book): the tasks of a podman exec
// land in the cgroup of whoever CALLED podman. Without this, every action of
// every app would run outside the ceiling its app was given, and a fork bomb
// would simply be an action instead of a service.
func TestExecRunsInsideTheCgroupOfItsApp(t *testing.T) {
	f := fakePodman(t)
	k := aKernel(t)
	name := anApp(t).name
	if _, err := ensureCgroup(name, Limits{}); err != nil {
		t.Fatal(err)
	}
	k.write(filepath.Join(k.root, cgroupParent, name, "cgroup.procs"), "4711")
	p := NewPodman(f.bin)
	if _, err := p.Exec(context.Background(),
		Exec{Name: name, Command: []string{"/bin/true"}}); err != nil {
		t.Fatal(err)
	}
	if len(k.joined) != 1 || k.joined[0] != name {
		t.Fatalf("calls put into a cgroup: %q, want exactly one, %q", k.joined, name)
	}
}

// 14. And the other side of that, which is a decision: a container an older
// controller started has no cgroup of this program, and Exec runs anyway
// instead of refusing. Such a container is braked by nothing either way, so
// refusing would take the app away and brake nothing. node.Restore is what
// ends that state.
func TestExecWithoutACgroupRunsRatherThanRefusing(t *testing.T) {
	f := fakePodman(t)
	k := aKernel(t)
	name := anApp(t).name
	if _, err := NewPodman(f.bin).Exec(context.Background(),
		Exec{Name: name, Command: []string{"/bin/true"}}); err != nil {
		t.Fatal(err)
	}
	if len(k.joined) != 0 {
		t.Fatalf("a call was put into a cgroup that does not exist: %q", k.joined)
	}
	if !f.ran() {
		t.Fatal("podman was not started at all")
	}
}

// 16. KillCgroup ends every task of a cgroup and does not come back before the
// cgroup is empty. The emptiness is what it promises: whoever calls it starts a
// container next, and a task left behind would be the second writer on one
// data directory (point 98).
func TestKillCgroupEndsTheTasksAndWaitsForTheCgroupToBeEmpty(t *testing.T) {
	k := aKernel(t)
	var waited int
	paths.sleep = func(time.Duration) { waited++ }
	// A plain name and not anApp(t): that helper LOOKS A SYSTEM USER UP and
	// skips where there is none, so on a machine without the app users these
	// two cases did not run at all - and "ok" from a run of skipped cases
	// looked exactly like a measurement (messen.md: a skipped case is not a
	// case that ran). Two mutants of 30.10.2026 lived on that, and they are
	// what found it. KillCgroup asks no user; it only needs a name the regexp
	// takes.
	const name = "ngc-uhr-ad8c00e2"
	// No podman here: what is measured is KillCgroup, and for that the cgroup
	// only has to HOLD a task. A start would drag the whole engine into a case
	// about one write, and on a machine without the engine the case would skip
	// - a skipped case is not a case that ran (messen.md).
	if _, err := ensureCgroup(name, Limits{}); err != nil {
		t.Fatal(err)
	}
	k.write(cgroupDirOf(name)+"/cgroup.procs", "4711")
	// The preparation has to have WORKED: without a task in there this case
	// would measure a kill of nothing and pass for the wrong reason.
	if !HasCgroup(name) {
		t.Fatal("the preparation of this case did not hold: no task in the cgroup")
	}
	if err := KillCgroup(name); err != nil {
		t.Fatalf("KillCgroup = %v, want the tasks ended", err)
	}
	if HasCgroup(name) {
		t.Fatal("KillCgroup came back while the cgroup still holds a task")
	}
	if want := []string{cgroupDirOf(name)}; !slices.Equal(k.killed, want) {
		t.Fatalf("killed %v, want %v - the kernel was not asked to kill", k.killed, want)
	}
	if waited != 0 {
		t.Fatalf("waited %d times although the cgroup was empty at the first look", waited)
	}
}

// 17. And a kernel that does not get everything: the cgroup still holds a task
// after the kill. Then this says so and does not pretend - the caller must not
// start a second set of processes on the same data.
func TestKillCgroupSaysSoWhenATaskSurvives(t *testing.T) {
	k := aKernel(t)
	k.staysFull = true
	var waited int
	paths.sleep = func(time.Duration) { waited++ }
	// A plain name and not anApp(t): that helper LOOKS A SYSTEM USER UP and
	// skips where there is none, so on a machine without the app users these
	// two cases did not run at all - and "ok" from a run of skipped cases
	// looked exactly like a measurement (messen.md: a skipped case is not a
	// case that ran). Two mutants of 30.10.2026 lived on that, and they are
	// what found it. KillCgroup asks no user; it only needs a name the regexp
	// takes.
	const name = "ngc-uhr-ad8c00e2"
	if _, err := ensureCgroup(name, Limits{}); err != nil {
		t.Fatal(err)
	}
	k.write(cgroupDirOf(name)+"/cgroup.procs", "4711")
	if !HasCgroup(name) {
		t.Fatal("the preparation of this case did not hold: no task in the cgroup")
	}
	err := KillCgroup(name)
	if err == nil {
		t.Fatal("a cgroup that still holds a task counted as emptied")
	}
	if !errors.Is(err, ErrCgroup) {
		t.Fatalf("err = %v, want it to carry ErrCgroup", err)
	}
	// It names the CGROUP, which is what a reader needs to look further - not
	// a fixed phrase. A case that pinned the wording would kill a mutant that
	// only rewords the message, and then the wording would be the promise
	// (messen.md: an expectation that only the mutant meets measures nothing;
	// mutant 8 of 30.10.2026 died on exactly that and was right to).
	if !strings.Contains(err.Error(), cgroupDirOf(name)) {
		t.Fatalf("err = %v, want it to name the cgroup that is still occupied", err)
	}
	if waited == 0 {
		t.Fatal("it did not look again even once")
	}
}

// 18. A name that is no name reaches no kernel at all, like every other call of
// this file.
func TestKillCgroupRefusesANameThatIsNoName(t *testing.T) {
	aKernel(t)
	paths.cgroupWrite = func(string, string) error {
		t.Fatal("a name that is no name reached the kernel")
		return nil
	}
	if err := KillCgroup("../../etc"); !errors.Is(err, ErrName) {
		t.Fatalf("err = %v, want ErrName", err)
	}
}

// 19. The wait before a contradiction counts as an orphan is longer than the
// deadline a stop hands podman - that is the whole reason it is safe, and it is
// a sum of one named number, not a number of its own. Measured on 30.10.2026:
// at every stop podman says "not running" while the tasks still live, so a
// wait that was SHORTER than the deadline would kill a healthy container.
func TestTheOrphanWaitOutlastsTheDeadlineOfAStop(t *testing.T) {
	if OrphanWait <= StopGrace {
		t.Fatalf("OrphanWait = %v, StopGrace = %v - a wait that does not outlast a stop kills healthy containers",
			OrphanWait, StopGrace)
	}
}
