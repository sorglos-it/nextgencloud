//go:build linux

package runtime

import (
	"errors"
	"fmt"
	"strconv"
	"syscall"
)

// killTask ends one task, and it ends THAT task and no other.
//
// Between "this number is surplus" and the signal lies time, and the kernel
// hands numbers out again. A plain kill(2) on a number that was freed in that
// moment reaches whoever got it next - and on a machine that starts containers
// that is not a theoretical worry. So the number is first turned into a
// HANDLE on the process (pidfd_open), and the signal goes to the handle
// (pidfd_send_signal). A handle cannot be inherited by another process: either
// it still names the process this decided about, or opening it fails because
// that process is gone.
//
// The start time is checked once more against the handle, through
// /proc/<pid>/stat, before the signal: the handle closes the window between
// the check and the signal, the start time is what makes the decision right in
// the first place. A number alone is not a process (surplus.go).
//
// Raw system calls and no foreign library: this repository carries no
// dependencies it does not need, and these two are two numbers and a struct.
func killTask(pid int, start uint64) error {
	if pid <= 1 {
		// 1 is the init of the machine and 0 means "every process in my
		// group" - kill(0, SIGKILL) would take the controller itself with it.
		// Neither can ever be a surplus task of an app.
		return fmt.Errorf("the number %d is never a surplus task of an app", pid)
	}
	fd, _, errno := syscall.Syscall(sysPidfdOpen, uintptr(pid), 0, 0)
	if errno != 0 {
		if errno == syscall.ESRCH {
			// Gone between reading and here. That is not a failure: the task
			// this was about no longer runs, which is what was wanted.
			return nil
		}
		return fmt.Errorf("pidfd_open %d: %w", pid, errno)
	}
	defer syscall.Close(int(fd))
	// The handle is open, so this number cannot change its meaning any more.
	// Now the one check that decides whether it was the right number at all.
	if t, ok := readTask(pid); !ok {
		return nil // gone in the meantime
	} else if t.start != start {
		return errors.New("the process " + strconv.Itoa(pid) +
			" is no longer the one this decided about: start time " +
			strconv.FormatUint(t.start, 10) + " instead of " +
			strconv.FormatUint(start, 10))
	}
	if _, _, errno := syscall.Syscall6(sysPidfdSendSignal, fd,
		uintptr(syscall.SIGKILL), 0, 0, 0, 0); errno != 0 && errno != syscall.ESRCH {
		return fmt.Errorf("pidfd_send_signal %d: %w", pid, errno)
	}
	return nil
}

// The two numbers are the same on every architecture Linux offers them on;
// they are written out here because the standard library does not name them.
const (
	sysPidfdOpen       = 434
	sysPidfdSendSignal = 424
)
