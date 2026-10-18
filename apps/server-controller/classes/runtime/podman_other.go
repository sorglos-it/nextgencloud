//go:build !linux

package runtime

import "syscall"

// This answers for the systems NGC is only written on, not run on: a container
// of an app has to run under the system user of that app, and the credential of
// a child process is a call of Linux. Nothing is started here, so every test
// that starts podman skips itself outside Linux. The file is here so that the
// package and its tests build where they are written.
func credential(uint32, uint32) (*syscall.SysProcAttr, error) { return nil, ErrNotRoot }
