package runtime

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// The three quantity limits of an app - how many tasks, how much memory, how
// much of a core - and the one place on this server where they are real.
//
// WHY THIS FILE EXISTS, measured, not reasoned out. Until 30.10.2026 the three
// limits were handed to podman as --pids-limit, --memory and --cpus and
// nowhere else. Podman took them, wrote them into its own answer (podman
// inspect says PidsLimit 512, Memory 268435456, NanoCpus 1000000000) and threw
// them away: on all three measured nodes of this plant no app container had a
// cgroup of its own. Measured on 24.10.2026 (single VM 208) and on 25.10.2026
// (svc-1, svc-2), repeated here on 30.10.2026 on svc-1 and svc-2:
//
//	/proc/<container pid>/cgroup  ->  0::/system.slice/ngc-controller.service
//	pids.max of that cgroup       ->  3466   (TasksMax of the unit, not 512)
//	memory.max of that cgroup     ->  max    (not 256M)
//	cpu.max of that cgroup        ->  the file does not exist
//
// And not only in the files: on 25.10.2026 a capped probe started 600 tasks in
// a container whose limit says 512, and all 600 started.
//
// THE CAUSE, in three layers, each one measured on svc-1 and svc-2 on
// 30.10.2026 and each one enough on its own:
//
//  1. ngc-controller.service has Delegate=no, so systemd hands the unit no
//     subtree of the cgroup tree. Its directory is root's, 0755, and the app's
//     user cannot write in it.
//  2. The unit's own cgroup.subtree_control is EMPTY and its cgroup.type is
//     domain with the controller's processes in it. So even root cannot turn it
//     into a parent of limited children: cgroup v2 refuses controllers in the
//     subtree of a cgroup that holds processes itself.
//  3. There is no systemd user session for the app's user (loginctl: "not
//     logged in or lingering"), so podman falls back to cgroupfs and says so:
//     477 bytes of warning on stderr of EVERY call, ending in "Falling back to
//     --cgroup-manager=cgroupfs". Nothing read them - Create and Start passed
//     nil for stderr (podman.go), so the one sentence that named the whole
//     problem fell on the floor for weeks.
//
// Those 477 bytes are the same 477 that point 78 of the book could not account
// for: measured here as one number, 477, on svc-1 on 30.10.2026.
//
// WHY NOT Delegate=yes. It is one line, it is in another repository (the unit
// is written by the ulh installer, not by this program), and it would buy
// less: system.slice has only memory and pids in its subtree_control, so a
// delegated subtree of this unit gets no cpu controller at all - cpu.max would
// still not exist. The root of the cgroup tree offers cpuset cpu io memory
// hugetlb pids rdma misc and hands cpu, memory and pids down, so a cgroup the
// controller makes THERE can hold all three. Measured on svc-1 on 30.10.2026.
//
// WHY THE CONTROLLER AND NOT PODMAN. Rootless podman cannot do it, and that is
// not a setting: a process may be moved into a cgroup only by someone who may
// write the cgroup.procs of the common ancestor of where it is and where it
// goes. The ancestor here is the root of the tree and belongs to root.
// Measured on svc-1 on 30.10.2026: the app's user, handed a cgroup of its own
// with its cgroup.procs chowned to it, still cannot enter it - "I/O error" -
// because the ancestor is not its. Root has no such limit. So the controller,
// which is root, makes the cgroup, writes the three numbers itself, and starts
// podman INSIDE it (Podman.Start, syscall CLONE_INTO_CGROUP). Everything podman
// then makes - conmon, the hold process, the service of every action - is a
// descendant and inherits the cgroup. The numbers are in files only root may
// write, so nothing in the container can raise them.
//
// WHAT IS MEASURED OF THE BRAKE ITSELF, on svc-1 on 30.10.2026, as the app's
// user, in a cgroup of pids.max 20 and memory.max 64 MiB, each with the
// counter-probe that has to succeed:
//
//	40 tasks in the cgroup       ->  "Cannot fork"      | without it: 40 of 40
//	128 MiB in the cgroup        ->  Killed, oom_kill 1 | without it: held
//	memory.peak of the cgroup    ->  exactly 67108864
//	systemctl daemon-reload      ->  the cgroup survives it
//	the node while all of it ran ->  load 0.13, controller active, both
//	                                 containers of the node still up
//
// WHAT THIS CANNOT DO, said here rather than promised anywhere:
//
//   - A kernel without cgroup v2 at /sys/fs/cgroup, or one that does not hand
//     down the pids and memory controllers, gets no brake from this. Then the
//     app does not start (ErrCgroupDropped) - a limit that is believed in and
//     absent is worse than one that is known to be absent.
//   - The cpu controller is the one of the three that may be missing without
//     stopping the app: a share of a core is being a good neighbour, not a
//     wall, and a node where cpu is not handed down would otherwise run no app
//     at all. Held reports what really holds, and the caller logs it.
//   - The ceiling counts the whole app: conmon, the hold process and every
//     service of every action of that app together. 512 tasks is the ceiling
//     for all of them, not for each.
//   - Management calls of the engine (create, stop, rm, ps, inspect) run
//     OUTSIDE the cgroup on purpose. Inside it, an app that has reached its
//     own ceiling could no longer be stopped, because stopping it would need
//     one more task of its own.
//   - A container started by an older controller has no cgroup. Exec says so
//     once, loudly, and runs; it cannot brake what was never started braked,
//     and refusing would only take the app away as well. node.Restore is what
//     ends that state: it starts such a container again, into a cgroup.

// cgroupParent is the one directory this program makes in the cgroup tree, and
// every app gets a child of it.
//
// It lies at the root of the tree and not under system.slice for the reason
// given above - the root hands down cpu, system.slice does not - and it carries
// no ".slice" in its name on purpose: a name of that shape is what systemd
// reads as a unit of its own, and this is not one. That systemd leaves it alone
// is measured, not assumed: on svc-1 on 30.10.2026 the directory and its
// numbers survived a systemctl daemon-reload.
const cgroupParent = "ngc.apps"

// cpuPeriod is the window the cpu controller counts in, in microseconds, and
// podman's own default. The quota is written against it: Limits.CPU is
// thousandths of a core, so a tenth of a core is 10000 of these 100000.
const cpuPeriod = 100000

// Limits are the two quantity limits an app brings from its manifest. The
// third, the number of tasks, is PIDsLimit and the same for every app, for the
// reason given there.
//
// They stand in a type of their own because they have to reach Start: the
// limits are written when the container is STARTED, not when it is created -
// a cgroup lives in a tmpfs and is gone after every restart of the machine,
// while the container is not. A Start that was not told them could only guess
// them, and a guessed ceiling is the bug this file is about, one floor up.
type Limits struct {
	// CPU is thousandths of a core, Memory is MiB, both as Container has them.
	// 0 means the default of the contract (store.DefaultCPU,
	// store.DefaultMemory), never "no limit" - the same reading createArgs has.
	CPU, Memory int
}

// Held says which of the three limits the kernel really holds for this app,
// read back out of the cgroup files after they were written. It is what a
// caller logs instead of claiming all three.
type Held struct {
	PIDs, Memory, CPU bool
}

// String names what holds and what does not, for a log line of the caller.
func (h Held) String() string {
	return "pids=" + strconv.FormatBool(h.PIDs) +
		" memory=" + strconv.FormatBool(h.Memory) +
		" cpu=" + strconv.FormatBool(h.CPU)
}

var (
	// ErrCgroup reports that the cgroup of the app could not be made at all:
	// no cgroup v2 under paths.cgroupRoot, or a directory that cannot be
	// created. It carries the reason of the system call, which names the path.
	ErrCgroup = errors.New("runtime: the cgroup of the app could not be made")
	// ErrCgroupDropped reports the one thing this whole file is against: a
	// limit that was asked for and is not in the kernel. It says which file
	// disagreed and with what, and it is an error and not a warning - a
	// container that starts anyway would carry a promise nobody keeps.
	ErrCgroupDropped = errors.New("runtime: the kernel does not hold a limit of this app")
)

// cgroupDirOf is the cgroup of this app's container.
func cgroupDirOf(name string) string {
	return paths.cgroupRoot + "/" + cgroupParent + "/" + name
}

// controllersIn reads a cgroup.controllers or cgroup.subtree_control file and
// answers whether it names a controller. A file that is not there is no
// controller, which is the honest reading: on svc-1 the unit's cgroup has no
// cpu.max file at all because cpu was never handed down to it.
func controllersIn(path string) map[string]bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	have := map[string]bool{}
	for _, c := range strings.Fields(string(b)) {
		have[c] = true
	}
	return have
}

// ensureCgroup makes the cgroup of this app, writes the three limits into it
// and reads every one of them back. It answers with what the kernel really
// holds.
//
// It may be called again at any time and has to be, for the same reason
// EnsureRun has: after a restart of the machine the whole tree is gone, and
// after a half-finished start the directory may be there with the numbers of a
// run before. So a directory that is already there is written again rather
// than believed.
//
// The order is the point. The limits go in BEFORE any process does: a cgroup
// that holds a task for even a moment without its ceiling is a window in which
// a fork bomb has already won.
func ensureCgroup(name string, l Limits) (Held, error) {
	if !validName.MatchString(name) {
		return Held{}, ErrName
	}
	// Is this a cgroup v2 tree at all? Without this file there is none, and
	// everything below would make ordinary directories somewhere and read back
	// exactly what it wrote - a brake that measures itself.
	// Is this a cgroup v2 tree at all? Only that is asked of the root here -
	// WHICH controllers it hands down is asked of the parent below, for the
	// reason given there.
	if len(controllersIn(paths.cgroupRoot+"/cgroup.controllers")) == 0 {
		return Held{}, errors.Join(ErrCgroup,
			errors.New("no cgroup v2 tree under "+paths.cgroupRoot))
	}
	parent := paths.cgroupRoot + "/" + cgroupParent
	if err := paths.mkdir(parent, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return Held{}, errors.Join(ErrCgroup, err)
	}
	// WHAT THE PARENT ITSELF OFFERS, and one write per controller. Both halves
	// of that sentence are a measured bug of the first version of this file,
	// found on svc-2 on 30.10.2026 by rolling it out:
	//
	//  1. It asked what the ROOT of the tree HAS (cgroup.controllers, eight
	//     entries on that machine) instead of what it HANDS DOWN
	//     (cgroup.subtree_control, three). Those are different files, and the
	//     difference is not academic: at boot systemd enables a controller
	//     only once a unit needs it, so cpu was not yet below the root when
	//     the controller came up a tenth of a second after it.
	//  2. It asked for all three in ONE write. A write naming a controller the
	//     parent does not offer is rejected ENTIRELY - measured on svc-2:
	//     "+pids +io" into a parent offering "cpu memory pids" leaves
	//     subtree_control EMPTY, pids refused along with io. The same two,
	//     written one at a time, leave "pids".
	//
	// Together they did exactly what the comment above warned about: one
	// controller that was not there yet cost the other two, nothing was
	// enabled, and the app did not start. It did not start UNBRAKED, which is
	// the one thing that went right - the readback below refused, named the
	// directory and said pids=false memory=false cpu=false.
	//
	// So: read the parent after making it, ask it only for what it says it
	// has, and ask for each one on its own.
	offers := controllersIn(parent + "/cgroup.controllers")
	var bad []error
	for _, c := range []string{"pids", "memory", "cpu"} {
		if !offers[c] {
			bad = append(bad, fmt.Errorf("%s offers no %s controller", parent, c))
			continue
		}
		// The error is kept and not raised: writing +memory a second time
		// answers EINVAL on some kernels although the controller is on, and
		// that answer would stop a start that is fine. What decides is the
		// readback of the child, below - but an error that nobody keeps is how
		// this file's own bug stayed invisible for one rollout, so it travels
		// with the refusal when there is one.
		if err := paths.cgroupWrite(parent+"/cgroup.subtree_control", "+"+c); err != nil {
			bad = append(bad, fmt.Errorf("%s: +%s: %w", parent, c, err))
		}
	}
	dir := cgroupDirOf(name)
	if err := paths.mkdir(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return Held{}, errors.Join(ErrCgroup, err)
	}
	// What the child really got. This is the readback of the line above and
	// the only thing that decides what is promised from here on.
	have := controllersIn(dir + "/cgroup.controllers")

	pids := PIDsLimit
	cpu, mem := l.CPU, l.Memory
	if cpu == 0 {
		cpu = store.DefaultCPU
	}
	if mem == 0 {
		mem = store.DefaultMemory
	}
	var held Held
	if have["pids"] {
		held.PIDs = writeLimit(dir+"/pids.max", strconv.Itoa(pids), &bad)
	}
	if have["memory"] {
		held.Memory = writeLimit(dir+"/memory.max", strconv.Itoa(mem<<20), &bad)
		// Without this a container at its ceiling is not braked but swapped
		// out, which takes the disk of the host instead of its memory. A
		// kernel without swap accounting has no such file; then there is
		// nothing to take away either, so a missing file is not a failure.
		if _, err := os.Stat(dir + "/memory.swap.max"); err == nil {
			writeLimit(dir+"/memory.swap.max", "0", &bad)
		}
	}
	if have["cpu"] {
		q := cpu * cpuPeriod / 1000
		held.CPU = writeLimit(dir+"/cpu.max",
			strconv.Itoa(q)+" "+strconv.Itoa(cpuPeriod), &bad)
	}
	// The two that are a wall. A node that cannot hold them runs no app of
	// this server: this is the one place in the whole engine where a missing
	// limit stops a start, and it stops it because the alternative is a
	// container that carries a promise nobody keeps.
	if !held.PIDs || !held.Memory {
		bad = append(bad, fmt.Errorf("%s holds %s", dir, held))
		return held, errors.Join(append([]error{ErrCgroupDropped}, bad...)...)
	}
	if len(bad) > 0 {
		// cpu is the only one left that can have failed here, and it is not a
		// wall (see the file's head). It is reported, not raised.
		return held, nil
	}
	return held, nil
}

// writeLimit writes one number into one cgroup file and reads it back. The
// readback is the whole of it: the bug this file exists for is a limit that was
// accepted and dropped, and an accepted write proves nothing about that.
//
// A kernel writes these files back in its own words, not in ours: memory.max
// answers "max" when it is off and cpu.max answers two numbers, so what is
// compared is the first field against what was asked for - not the whole line,
// which would fail on a trailing newline, and not just "no error".
func writeLimit(path, value string, bad *[]error) bool {
	if err := paths.cgroupWrite(path, value); err != nil {
		*bad = append(*bad, fmt.Errorf("%s: %w", path, err))
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		*bad = append(*bad, fmt.Errorf("%s: %w", path, err))
		return false
	}
	got := strings.Fields(string(b))
	want := strings.Fields(value)
	if len(got) == 0 || len(want) == 0 || got[0] != want[0] {
		*bad = append(*bad, fmt.Errorf("%s holds %q, asked for %q",
			path, strings.TrimSpace(string(b)), value))
		return false
	}
	return true
}

// HasCgroup reports whether this app's container runs inside a cgroup of this
// program.
//
// It answers the one state nothing else can see: a container that a controller
// before this one started is up and braked by nothing, and no call of podman
// says so - inspect reports the three limits either way, which is what started
// all this. node.Restore asks this to start such a container again.
func HasCgroup(name string) bool {
	if !validName.MatchString(name) {
		return false
	}
	b, err := os.ReadFile(cgroupDirOf(name) + "/cgroup.procs")
	return err == nil && len(strings.Fields(string(b))) > 0
}

// OrphanWait is how long a contradiction has to stand before it counts as an
// orphan: the cgroup of an app holds a task while podman says nothing of this
// app runs.
//
// Why there has to be a wait at all, measured on 30.10.2026 in WSL (podman
// 5.4.2 as on the nodes, cgroup v2, own store and own user): those two numbers
// stand that way in ORDINARY operation, and not rarely.
//
//   - At every stop. "podman stop" sets the state to not-running at once, and
//     the tasks live on until the container is really out - 134 answers in a
//     row in the measurement.
//   - At every start. The first task is in the cgroup before podman says
//     running - one answer, about 56 ms, which was the resolution of the
//     measurement and not necessarily the length of the window.
//   - In an update, which contains the stop.
//
// So a detector that looks ONCE would fire at every stop of every app, and
// kill a healthy container. What the measurement gives is the EXISTENCE of the
// window, not its length: the probe image had a sleep as process 1 with no
// SIGTERM handler, and the kernel drops signals to process 1 without one, so
// it died at the SIGKILL after the deadline. ngc-hold handles SIGTERM and goes
// at once. The length is therefore unknown - but its CEILING is not: no stop
// takes longer than the deadline Stop hands podman, because after it the
// container is killed.
//
// Hence the sum below, and not a round number somebody cannot explain later:
// the deadline itself, plus a slack for the kernel to reap the last task and
// for conmon to leave. An ordinary stop is over when this is up, and the
// cgroup is empty; an orphan stands (on svc-1 for hours, point 98). A
// contradiction that survives this wait is no state of operation.
const OrphanWait = StopGrace + 2*time.Second

// KillCgroup ends every task in the cgroup of an app at once and waits until
// none is left.
//
// It writes cgroup.kill, which the kernel turns into a SIGKILL for every task
// in that cgroup, however many there are. That number matters: after a hard
// death of the controller there is not ONE surplus process set but one per
// start of the controller - three per app on svc-1 after two starts, point 98.
// Something that handled "the one orphan" would leave the third standing.
//
// It kills and does not ask podman to stop: podman knows only the set its own
// record points at, and the others hang on ppid 1 and on nothing else - that
// is what the measurement on svc-1 showed, and it is why Stop cannot do this
// job.
func KillCgroup(name string) error {
	if !validName.MatchString(name) {
		return ErrName
	}
	dir := cgroupDirOf(name)
	if err := paths.cgroupWrite(dir+"/cgroup.kill", "1"); err != nil {
		return errors.Join(ErrCgroup, fmt.Errorf("%s: cgroup.kill: %w", dir, err))
	}
	// And then it is read back, like every other write in this file: a kill
	// that was accepted and left a task behind is not a kill, and a caller
	// that started a container next would be the second writer on one data
	// directory (point 98).
	for i := 0; i < 50; i++ {
		if !HasCgroup(name) {
			return nil
		}
		paths.sleep(10 * time.Millisecond)
	}
	return errors.Join(ErrCgroup,
		errors.New(dir+" still holds a task after cgroup.kill"))
}

// DropCgroup takes the cgroup of an app away, after its container is gone.
//
// A cgroup that still holds a task cannot be removed, and that is the kernel's
// rule, not ours: whoever calls this stops the container first. A cgroup that
// is not there is no error - after a restart of the machine none of them is.
func DropCgroup(name string) error {
	if !validName.MatchString(name) {
		return ErrName
	}
	if err := paths.cgroupDrop(cgroupDirOf(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(ErrCgroup, err)
	}
	return nil
}
