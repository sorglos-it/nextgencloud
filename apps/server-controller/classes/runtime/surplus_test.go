package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A stand-in for the two things these cases are about: the tasks the kernel
// lists in a cgroup, and what it says about each of them in /proc.
//
// Why a stand-in at all. On the machine these cases run on there is no process
// of an app, no cgroup of an app, and against the real /proc every case about
// WHICH tasks belong to a container would measure the process tree of
// whatever runs there. And the one thing that must never happen while
// measuring is the thing being measured: a case that ends a task would end a
// task of this machine.
//
// What it does NOT do: it never makes a task vanish by itself and it never
// invents a parent. Every number a case asks for is written out, and a number
// a case leaves out is a number /proc does not know - which is the ordinary
// case of a process that ended between two reads, and one of the cases below.
type fakeProc struct {
	t *testing.T
	// killed are the (number, start time) pairs killTask was called with, in
	// order. The pair and not the number: the pair is the identity, and a case
	// about the wrong one would pass if only numbers were noted.
	killed []task
	// killErr is what killTask answers for this number instead of ending it.
	killErr map[int]error
}

// aProc lays out a cgroup with these tasks and a /proc to match. The first
// task of the list is written first, which is also the order cgroup.procs
// gets - a real cgroup.procs is sorted, and a case that depended on the order
// would measure the stand-in.
func aProc(t *testing.T, name string, ts ...task) *fakeProc {
	t.Helper()
	f := &fakeProc{t: t, killErr: map[int]error{}}
	root := t.TempDir()
	was := paths
	t.Cleanup(func() { paths = was })
	paths.cgroupRoot = filepath.Join(root, "cgroup")
	paths.procRoot = filepath.Join(root, "proc")
	paths.killTask = func(pid int, start uint64) error {
		f.killed = append(f.killed, task{pid: pid, start: start})
		return f.killErr[pid]
	}
	dir := cgroupDirOf(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var procs []string
	for _, x := range ts {
		procs = append(procs, strconv.Itoa(x.pid))
		d := filepath.Join(paths.procRoot, strconv.Itoa(x.pid))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		// The shape of /proc/<pid>/stat, with the program's name in brackets
		// at the second field - and the name carries a space and a bracket on
		// purpose: the code counts from the LAST closing bracket, and a case
		// with a harmless name could not tell that apart from counting from
		// the left.
		line := strconv.Itoa(x.pid) + " (ngc hold (1)) S " + strconv.Itoa(x.ppid)
		for i := 5; i <= 21; i++ {
			line += " 0"
		}
		line += " " + strconv.FormatUint(x.start, 10) + " 0 0\n"
		if err := os.WriteFile(filepath.Join(d, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(dir+"/cgroup.procs",
		[]byte(strings.Join(procs, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The preparation has to have WORKED, and this is the line that says so:
	// without tasks in the cgroup every case below would measure a cleanup of
	// nothing and pass for the wrong reason (messen.md).
	if got, err := tasksOf(name); err != nil || len(got) != len(ts) {
		t.Fatalf("the preparation of this case did not hold: %d of %d tasks, %v",
			len(got), len(ts), err)
	}
	return f
}

func (f *fakeProc) numbers() []int {
	var n []int
	for _, k := range f.killed {
		n = append(n, k.pid)
	}
	return n
}

const anAppName = "ngc-uhr-ad8c00e2"

// 1. A healthy container alone: nothing is surplus, nothing is ended. This is
// the case that runs on every start of every node, so it is the one that must
// not cost anything and must not kill anything.
func TestEndSurplusLeavesAHealthyContainerAlone(t *testing.T) {
	// conmon 100, the container's own process 101 below it.
	f := aProc(t, anAppName,
		task{pid: 100, ppid: 7, start: 500},
		task{pid: 101, ppid: 100, start: 501})
	n, err := endSurplus(anAppName, 101)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("ended %d tasks, want none", n)
	}
	if len(f.killed) != 0 {
		t.Fatalf("killed %v of a healthy container", f.numbers())
	}
}

// 2. One set of an earlier life beside it: exactly its two numbers go, and the
// two of the container podman knows stay. That is the state of svc-1 after a
// hard death of the controller (point 102).
func TestEndSurplusEndsTheSetOfAnEarlierLifeAndKeepsTheKnownOne(t *testing.T) {
	f := aProc(t, anAppName,
		task{pid: 100, ppid: 7, start: 500},   // old conmon
		task{pid: 101, ppid: 100, start: 501}, // old ngc-hold
		task{pid: 200, ppid: 9, start: 900},   // conmon of the known container
		task{pid: 201, ppid: 200, start: 901}) // the known container
	n, err := endSurplus(anAppName, 201)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("ended %d tasks, want 2", n)
	}
	if got := f.numbers(); !slices.Equal(got, []int{100, 101}) {
		t.Fatalf("killed %v, want the old set 100 and 101", got)
	}
	// And the pair was handed over, not the number alone: the start time is
	// half the identity.
	if f.killed[0].start != 500 || f.killed[1].start != 501 {
		t.Fatalf("killed %+v, want each with its start time", f.killed)
	}
}

// 3. Three sets, as svc-1 carried them after two restarts of the controller:
// four numbers go, two stay. A fix that handled "the one orphan" would have
// left the third standing - that is why this case counts.
func TestEndSurplusEndsEverySurplusSetAndNotOnlyTheFirst(t *testing.T) {
	f := aProc(t, anAppName,
		task{pid: 100, ppid: 7, start: 500},
		task{pid: 101, ppid: 100, start: 501},
		task{pid: 300, ppid: 8, start: 700},
		task{pid: 301, ppid: 300, start: 701},
		task{pid: 200, ppid: 9, start: 900},
		task{pid: 201, ppid: 200, start: 901})
	n, err := endSurplus(anAppName, 201)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("ended %d tasks, want 4", n)
	}
	got := f.numbers()
	slices.Sort(got)
	if !slices.Equal(got, []int{100, 101, 300, 301}) {
		t.Fatalf("killed %v, want both older sets", got)
	}
}

// 4. An app that forks workers of its own keeps them, however many and however
// deep. This is the case a counting rule ("more than two tasks") would kill,
// and it is the reason there is no counting rule.
func TestEndSurplusKeepsTheChildrenAnAppMakesItself(t *testing.T) {
	f := aProc(t, anAppName,
		task{pid: 200, ppid: 9, start: 900},   // conmon
		task{pid: 201, ppid: 200, start: 901}, // the container
		task{pid: 202, ppid: 201, start: 902}, // a worker
		task{pid: 203, ppid: 202, start: 903}, // a worker of the worker
		task{pid: 204, ppid: 203, start: 904})
	n, err := endSurplus(anAppName, 201)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(f.killed) != 0 {
		t.Fatalf("ended %d (%v) of an app that only has children", n, f.numbers())
	}
}

// 5. THE TRAP OF THIS WHOLE FIX: a container whose watchman has died has 1 as
// its parent, and 1 is the init of the machine. It is neither kept as a
// watchman nor ended as surplus - and both halves are the case. Measured on
// 3.11.2026: conmon's own parent is a podman process that is gone a moment
// later, after which conmon falls to 1 (surplus.go).
func TestEndSurplusNeverTakesNumberOneForTheWatchman(t *testing.T) {
	f := aProc(t, anAppName,
		task{pid: 1, ppid: 0, start: 1},       // the init of the machine
		task{pid: 201, ppid: 1, start: 901},   // a container whose conmon died
		task{pid: 100, ppid: 7, start: 500},   // an old conmon
		task{pid: 101, ppid: 100, start: 501}) // its ngc-hold
	// It REPORTS the oddity - a 1 in the cgroup of an app is nothing that
	// should be there - and it still does its job on the rest. Quietly
	// skipping it would hide a state nobody has explained.
	n, err := endSurplus(anAppName, 201)
	if !errors.Is(err, ErrSurplus) {
		t.Fatalf("err = %v, want it to report the 1 it found", err)
	}
	// It has to name the NUMBER and the cgroup - that is what a reader needs
	// to look further. Not the wording: a case that pinned the phrase would
	// kill a mutant that only rewrites the message, and then the wording
	// would be the promise (messen.md, and a mutant of 30.10.2026 that died
	// on exactly that).
	if !strings.Contains(err.Error(), "1") || !strings.Contains(err.Error(), cgroupDirOf(anAppName)) {
		t.Fatalf("err = %v, want it to name the number and the cgroup it refused to touch", err)
	}
	if slices.Contains(f.numbers(), 1) {
		t.Fatalf("killed %v - the init of the machine was taken for a surplus task", f.numbers())
	}
	// The old set still goes; the trap must not cost the fix.
	got := f.numbers()
	slices.Sort(got)
	if !slices.Equal(got, []int{100, 101}) {
		t.Fatalf("killed %v, want exactly the old set", got)
	}
	if n != 2 {
		t.Fatalf("ended %d, want 2", n)
	}
}

// 6. A task that is gone between the list and the look is no error: it ended
// by itself, which is what was wanted. The stand-in simply has no /proc entry
// for it.
func TestEndSurplusForgivesATaskThatEndedByItself(t *testing.T) {
	f := aProc(t, anAppName,
		task{pid: 200, ppid: 9, start: 900},
		task{pid: 201, ppid: 200, start: 901})
	// 999 stands in cgroup.procs and nowhere in /proc.
	dir := cgroupDirOf(anAppName)
	if err := os.WriteFile(dir+"/cgroup.procs", []byte("200\n201\n999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := endSurplus(anAppName, 201)
	if err != nil {
		t.Fatalf("a task that ended by itself counted as a failure: %v", err)
	}
	if n != 0 || len(f.killed) != 0 {
		t.Fatalf("ended %d (%v), want none", n, f.numbers())
	}
}

// 7. No usable number from the engine: NOTHING is ended, and it says why.
// Failing safely here is the opposite of failing safely in KillCgroup, and on
// purpose - there no healthy container exists, here one does.
func TestEndSurplusEndsNothingWithoutAUsableProcessNumber(t *testing.T) {
	for _, pid := range []int{0, -1} {
		f := aProc(t, anAppName,
			task{pid: 100, ppid: 7, start: 500},
			task{pid: 101, ppid: 100, start: 501})
		n, err := endSurplus(anAppName, pid)
		if !errors.Is(err, ErrSurplus) {
			t.Fatalf("pid %d: err = %v, want ErrSurplus", pid, err)
		}
		if n != 0 || len(f.killed) != 0 {
			t.Fatalf("pid %d: ended %d (%v) although nothing could be placed", pid, n, f.numbers())
		}
	}
}

// 8. And the other half of failing safely: the number the engine named does
// not stand in this cgroup at all. Then nothing here can be placed - it could
// be the wrong cgroup or a stale number - and nothing is ended.
func TestEndSurplusEndsNothingWhenTheKnownContainerIsNotInThisCgroup(t *testing.T) {
	f := aProc(t, anAppName,
		task{pid: 100, ppid: 7, start: 500},
		task{pid: 101, ppid: 100, start: 501})
	n, err := endSurplus(anAppName, 4711)
	if !errors.Is(err, ErrSurplus) {
		t.Fatalf("err = %v, want ErrSurplus", err)
	}
	if !strings.Contains(err.Error(), cgroupDirOf(anAppName)) {
		t.Fatalf("err = %v, want it to name the cgroup it looked in", err)
	}
	if n != 0 || len(f.killed) != 0 {
		t.Fatalf("ended %d (%v) although the known container was not in there", n, f.numbers())
	}
}

// 9. A kill that does not take is reported, and it does not stop the others:
// one task that cannot be ended must not leave three standing.
func TestEndSurplusReportsAKillThatDidNotTakeAndEndsTheRest(t *testing.T) {
	f := aProc(t, anAppName,
		task{pid: 100, ppid: 7, start: 500},
		task{pid: 101, ppid: 100, start: 501},
		task{pid: 200, ppid: 9, start: 900},
		task{pid: 201, ppid: 200, start: 901})
	f.killErr[100] = errors.New("no")
	n, err := endSurplus(anAppName, 201)
	if !errors.Is(err, ErrSurplus) {
		t.Fatalf("err = %v, want ErrSurplus", err)
	}
	if n != 1 {
		t.Fatalf("ended %d, want the one that could be ended", n)
	}
	if got := f.numbers(); !slices.Equal(got, []int{100, 101}) {
		t.Fatalf("tried %v, want both of the old set", got)
	}
}

// 10. No cgroup of this app at all: no surplus, and no error. An app that was
// never started on this machine is not this call's business.
func TestEndSurplusIsQuietWithoutACgroup(t *testing.T) {
	aProc(t, anAppName, task{pid: 200, ppid: 9, start: 900})
	if err := os.RemoveAll(cgroupDirOf(anAppName)); err != nil {
		t.Fatal(err)
	}
	n, err := endSurplus(anAppName, 200)
	if err != nil || n != 0 {
		t.Fatalf("n = %d, err = %v, want 0 and no error", n, err)
	}
}

// 11. A name that is no name reaches neither /proc nor a kill.
func TestEndSurplusRefusesANameThatIsNoName(t *testing.T) {
	f := aProc(t, anAppName, task{pid: 200, ppid: 9, start: 900})
	if _, err := endSurplus("../../etc", 200); !errors.Is(err, ErrName) {
		t.Fatalf("err = %v, want ErrName", err)
	}
	if len(f.killed) != 0 {
		t.Fatalf("killed %v for a name that is no name", f.numbers())
	}
}

// 12. What podman answered becomes a process number only if it IS one. A value
// that is not a number must never become a pid, because something gets killed
// over that pid.
//
// The four warning lines of rootless podman (the 477 bytes of point 78) go to
// standard error, so they cannot land in this string - but they have corrupted
// a measured value three times in one day, and this is the wall that holds if
// the two buffers of one call are ever confused: the LAST field, and it has to
// be a number.
func TestOnlyANumberBecomesAProcessNumber(t *testing.T) {
	warnungen := `time="..." level=warning msg="The cgroupv2 manager is set to systemd but there is no systemd user session available"
time="..." level=warning msg="For using systemd, you may need to log in using a user session"
time="..." level=warning msg="Alternatively, you can enable lingering with: ` + "`loginctl enable-linger 1002`" + `"
time="..." level=warning msg="Falling back to --cgroup-manager=cgroupfs"
`
	gut := []struct {
		was  string
		want int
	}{
		{"4711\n", 4711},
		{"  4711  \n", 4711},
		// The case this is really about: the warnings in front of the value.
		{warnungen + "4711\n", 4711},
	}
	for _, c := range gut {
		pid, err := pidFromInspect(c.was)
		if err != nil || pid != c.want {
			t.Fatalf("pidFromInspect(%q) = %d, %v, want %d", c.was, pid, err, c.want)
		}
	}
	schlecht := []string{
		"",
		"\n",
		// Only warnings and no number: nothing may come out of this.
		warnungen,
		"level=warning\n",
		"abc\n",
		// A container that is not running answers 0 - and that is NOT refused
		// here but in endSurplus, which says so with the name of the app. This
		// line only holds that 0 arrives as a number and not as a refusal.
	}
	for _, w := range schlecht {
		if pid, err := pidFromInspect(w); err == nil {
			t.Fatalf("pidFromInspect(%q) = %d and no error - a value that is no number became a pid", w, pid)
		}
	}
	if pid, err := pidFromInspect("0\n"); err != nil || pid != 0 {
		t.Fatalf("pidFromInspect(\"0\") = %d, %v - the 0 belongs to endSurplus, not here", pid, err)
	}
}

// 13. And the first wall of endSurplus earns its own place beside the second.
// Without a usable number NOTHING is read: this case has no cgroup at all, so
// the later wall ("the known container does not stand in this cgroup") cannot
// speak - it would answer "no cgroup, no surplus, no error". The error has to
// come from the first wall.
//
// This is what the mutation run of 3.11.2026 found: with the first wall taken
// out, case 7 stayed green because the SECOND one refused. A case that is only
// green because another wall already refused measures that other wall
// (messen.md).
func TestTheRefusalWithoutAUsableNumberIsItsOwnWall(t *testing.T) {
	f := aProc(t, anAppName, task{pid: 200, ppid: 9, start: 900})
	if err := os.RemoveAll(cgroupDirOf(anAppName)); err != nil {
		t.Fatal(err)
	}
	// Control first: WITH a usable number this very state is quiet (case 10).
	if n, err := endSurplus(anAppName, 200); err != nil || n != 0 {
		t.Fatalf("the preparation of this case did not hold: n = %d, err = %v", n, err)
	}
	// And without one it is an error, although there is no cgroup to look in.
	n, err := endSurplus(anAppName, 0)
	if !errors.Is(err, ErrSurplus) {
		t.Fatalf("err = %v, want ErrSurplus from the first wall", err)
	}
	if n != 0 || len(f.killed) != 0 {
		t.Fatalf("ended %d (%v)", n, f.numbers())
	}
}
