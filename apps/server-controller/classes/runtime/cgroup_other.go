//go:build !linux

package runtime

import (
	"errors"
	"os"
	"syscall"
)

// This answers for the systems NGC is only written on, not run on. Putting a
// child process into a cgroup is a call of Linux, and there is no cgroup tree
// to open either: every case that would start podman skips itself outside
// Linux, and the ones that measure the numbers written into a cgroup use a
// directory of their own (paths.cgroupRoot) and never reach these two.
func inCgroup(attr *syscall.SysProcAttr, dir *os.File) (*syscall.SysProcAttr, error) {
	if dir == nil {
		return attr, nil
	}
	return nil, errors.Join(ErrCgroup, errors.New("putting a process into a cgroup needs Linux"))
}

func openCgroup(string) (*os.File, error) {
	return nil, errors.Join(ErrCgroup, errors.New("a cgroup tree needs Linux"))
}
