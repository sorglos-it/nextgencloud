package runtime

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Surplus process sets: what to do when podman knows a running container of an
// app and OTHER tasks hang in that app's cgroup beside it.
//
// This is the second half of point 98 of the book, and it needs a different
// tool than the first. Where podman knows NO container, cgroup.kill empties
// the whole cgroup and there is no healthy process in it that the kill could
// take. Here there IS one: the container podman knows. Emptying the cgroup
// would kill it too, so the surplus tasks have to be named one by one.
//
// Where they come from. Point 98: systemd deletes /run/ngc with the unit,
// podman refreshes its own state, says nothing runs, and the controller starts
// a set beside the one that lives on - once per start. And after an app is
// raised to a new version podman DELETES the old container record while its
// processes keep running, so from then on no podman ps, stop or rm reaches
// them. Measured on svc-1 on 30.10.2026: three sets per app, two of them
// running the previous version of the program on the data of the new one.
//
// THE TRAP THIS WAS BUILT AGAINST, and it is a trap in the fix and not in the
// fault. Measured on 3.11.2026 in WSL (podman 5.4.2 as on the nodes, rootless,
// cgroup v2): conmon IS the parent of the container's own process - the parent
// number of the number inspect names, and the conmon.pid that podman keeps in
// its runtime directory, are the same number. But conmon's OWN parent was a
// podman process that is gone a moment later, after which conmon falls to
// number 1. So whoever takes "the parent of the container" as conmon takes the
// number 1 for a container whose conmon has died - and would write the init of
// the machine into the list of what to keep, or worse, end it. Hence:
//
//	The parent is kept ONLY when it is not 1 AND itself stands in the cgroup
//	of this app.
//
// The cgroup membership is what carries that, not the text in the command
// line: a name is something a process says about itself.
//
// And failing safely here means NOT killing. In the first case an app that
// cannot be freed of its surplus stays off, because an app running twice on
// its data is worse. Here a healthy container runs, and the one thing that
// must not happen is killing it: so whenever the container's own number cannot
// be had, or a task cannot be placed, NOTHING is ended and the log says what
// would have to be cleaned and why it could not be. Both rules are the same
// rule - never be the one who destroys what is working - and they only look
// opposite because in the first case nothing is working.

// A task is a number together with the start time the kernel records for it.
// The pair is the identity: the kernel hands numbers out again, and a number
// that was read a moment ago may belong to another process by the time a
// signal is sent. A number alone is not a process.
type task struct {
	pid   int
	ppid  int
	start uint64
}

// ErrSurplus says that surplus tasks could not be dealt with. It never means
// "there were none".
var ErrSurplus = errors.New("runtime: the surplus tasks of this app could not be ended")

// readTask reads one task out of procRoot. A task that is gone by now is no
// error: between reading cgroup.procs and this line a process may simply have
// ended, and that is the ordinary case, not a fault.
func readTask(pid int) (task, bool) {
	b, err := os.ReadFile(paths.procRoot + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return task{}, false
	}
	// The second field is the name of the program, in brackets, and it may
	// contain spaces and brackets itself - so everything is counted from the
	// LAST closing bracket, which is what /proc guarantees. Splitting on
	// spaces from the left would read a wrong parent for a program whose name
	// has a space in it, and then a healthy task could count as surplus.
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 > len(s) {
		return task{}, false
	}
	f := strings.Fields(s[i+1:])
	// After the bracket the fields are state(3) ppid(4) ... starttime(22), so
	// here, counted from 0: f[0] is the state, f[1] the parent, f[19] the
	// start time.
	if len(f) < 20 {
		return task{}, false
	}
	ppid, err := strconv.Atoi(f[1])
	if err != nil {
		return task{}, false
	}
	start, err := strconv.ParseUint(f[19], 10, 64)
	if err != nil {
		return task{}, false
	}
	return task{pid: pid, ppid: ppid, start: start}, true
}

// tasksOf reads every task that stands in the cgroup of this app.
func tasksOf(name string) ([]task, error) {
	b, err := os.ReadFile(cgroupDirOf(name) + "/cgroup.procs")
	if err != nil {
		return nil, err
	}
	var ts []task
	for _, w := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(w)
		if err != nil {
			continue
		}
		if t, ok := readTask(pid); ok {
			ts = append(ts, t)
		}
	}
	return ts, nil
}

// keepers answers which of these tasks belong to the container whose own
// process is pid: that process, its watchman, and its descendants.
//
// Belonging is established POSITIVELY. Whatever cannot be placed is surplus,
// and the caller is the one that decides what follows from that - this
// function kills nothing.
func keepers(ts []task, pid int) map[int]bool {
	in := map[int]task{}
	for _, t := range ts {
		in[t.pid] = t
	}
	keep := map[int]bool{pid: true}
	// The watchman: the parent of the container's process, but only when it is
	// not 1 and stands in this cgroup itself. See the head of this file - a
	// container whose conmon has died has 1 as its parent, and 1 is the init
	// of the machine.
	if own, ok := in[pid]; ok && own.ppid != 1 {
		if _, here := in[own.ppid]; here {
			keep[own.ppid] = true
		}
	}
	// The descendants: every task whose chain of parents reaches the
	// container's process. The chain is walked inside this cgroup only, and it
	// is bounded by the number of tasks, so a cycle - which the kernel does
	// not make, but a stand-in could - cannot turn this into a loop.
	for _, t := range ts {
		if keep[t.pid] {
			continue
		}
		seen := 0
		for cur := t; seen <= len(ts); seen++ {
			next, ok := in[cur.ppid]
			if !ok {
				break
			}
			if keep[next.pid] {
				keep[t.pid] = true
				break
			}
			cur = next
		}
	}
	return keep
}

// endSurplus ends every task in the cgroup of this app that does not belong to
// the container whose own process is pid, and answers with how many it ended.
//
// A pid that is not usable - 0 or negative - ends NOTHING and is an error: see
// the head of this file.
func endSurplus(name string, pid int) (int, error) {
	if !validName.MatchString(name) {
		return 0, ErrName
	}
	if pid <= 0 {
		return 0, errors.Join(ErrSurplus,
			fmt.Errorf("the container of %s has no usable process number (%d), so nothing is ended", name, pid))
	}
	ts, err := tasksOf(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No cgroup, so no surplus. An app whose cgroup is not there is
			// not this function's business.
			return 0, nil
		}
		return 0, errors.Join(ErrSurplus, err)
	}
	// The container's own process has to BE in the cgroup. If it is not, then
	// either the cgroup is not the one this container runs in, or the number
	// from podman is stale - and in neither case can this tell a surplus task
	// from a healthy one. Nothing is ended.
	var own bool
	for _, t := range ts {
		if t.pid == pid {
			own = true
		}
	}
	if !own {
		return 0, errors.Join(ErrSurplus,
			fmt.Errorf("the process %d of %s does not stand in %s, so no task here can be placed",
				pid, name, cgroupDirOf(name)))
	}
	keep := keepers(ts, pid)
	var bad []error
	ended := 0
	for _, t := range ts {
		if keep[t.pid] {
			continue
		}
		// Never 1, and never 0. The rule above keeps the number 1 out of the
		// KEEPERS; this keeps it out of the surplus, and the two are not the
		// same thing - the case of 3.11.2026 passed the first and then killed
		// the init of the machine. 0 would mean "every process in my group",
		// which would take the controller itself.
		//
		// Whether the kernel can even put 1 into the cgroup of an app does not
		// matter here: a wall that only holds while an assumption holds is not
		// a wall.
		if t.pid <= 1 {
			bad = append(bad, fmt.Errorf(
				"the number %d stands in %s and is not a task of this app - nothing was done to it",
				t.pid, cgroupDirOf(name)))
			continue
		}
		if err := paths.killTask(t.pid, t.start); err != nil {
			bad = append(bad, fmt.Errorf("%d: %w", t.pid, err))
			continue
		}
		ended++
	}
	if len(bad) > 0 {
		return ended, errors.Join(append([]error{ErrSurplus}, bad...)...)
	}
	return ended, nil
}
