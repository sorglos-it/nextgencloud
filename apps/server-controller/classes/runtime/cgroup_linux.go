//go:build linux

package runtime

import (
	"errors"
	"os"
	"syscall"
)

// inCgroup is how a call of podman comes to run inside the cgroup of its app,
// and with it everything that call makes: conmon, the hold process, the
// service of an action.
//
// It is one flag of clone3, CLONE_INTO_CGROUP, and not a write of cgroup.procs
// after the fact, and the difference is the whole reason it is here. A process
// that is moved afterwards has already run - and a fork bomb only needs the
// moment before the move. Here the first instruction of the child already runs
// under the ceiling.
//
// Who may do this: whoever may write the cgroup.procs of the common ancestor
// of where the process is and where it goes. For the app's own user that
// ancestor is the root of the tree, so it may not - measured on svc-1 on
// 30.10.2026, "I/O error", with its own cgroup.procs handed to it. The
// controller is root and has no such limit, so this stands here and not in
// podman's arguments. The credential of the child is set in the same call, so
// the child enters the cgroup as root and drops to the app's user at once -
// the order is the kernel's and cannot be taken apart.
//
// A kernel older than 5.7 has no CLONE_INTO_CGROUP. It then answers EINVAL at
// the clone, so the start fails and says so, which is what this file's head
// says it must do: no app runs unbraked because a kernel could not brake it.
func inCgroup(attr *syscall.SysProcAttr, dir *os.File) (*syscall.SysProcAttr, error) {
	if dir == nil {
		return attr, nil
	}
	if attr == nil {
		// Only where podman already runs as the app's user, which is nothing
		// this program does in operation and everything a case does.
		attr = &syscall.SysProcAttr{}
	}
	attr.UseCgroupFD = true
	attr.CgroupFD = int(dir.Fd())
	return attr, nil
}

// openCgroup opens the cgroup directory of this app, for inCgroup. A handle and
// not a path, because that is what the kernel takes.
func openCgroup(name string) (*os.File, error) {
	if !validName.MatchString(name) {
		return nil, ErrName
	}
	f, err := os.Open(cgroupDirOf(name))
	if err != nil {
		return nil, errors.Join(ErrCgroup, err)
	}
	return f, nil
}
