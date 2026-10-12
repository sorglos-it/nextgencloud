//go:build linux

package runtime

import (
	"os"
	"syscall"
)

// credential is how podman comes to run under the system user of the app: each
// app has its own, so whoever breaks out of the container lands on a user with
// no rights of its own - not on root, and not on the user of another app.
//
// Switching needs root, which the controller has as a unit of systemd. Without
// it the container is not started under this process's own user instead: the
// data of the app belong to another user, and a container of this one would be
// a second owner of them. So it refuses.
func credential(uid, gid uint32) (*syscall.SysProcAttr, error) {
	if uid == uint32(os.Getuid()) && gid == uint32(os.Getgid()) {
		// Nothing to switch, and more than unnecessary: setting a credential
		// makes the child call setgroups as well, and that needs root even for
		// the ids it already has.
		return nil, nil
	}
	if os.Geteuid() != 0 {
		return nil, ErrNotRoot
	}
	return &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid}}, nil
}
