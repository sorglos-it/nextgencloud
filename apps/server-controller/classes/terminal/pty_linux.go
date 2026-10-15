//go:build linux

package terminal

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"github.com/sorglos-it/nextgencloud/shared/service"
)

// shellPath is the shell of the image (debian:13-slim). It is a fixed path
// with an argument list and no shell of its own around it.
const shellPath = "/bin/sh"

// winsize is what TIOCSWINSZ takes: the order of the four fields is the
// kernel's, not ours.
type winsize struct {
	rows, cols     uint16
	xpixel, ypixel uint16
}

// openPTY makes a pseudo terminal and answers both its ends. The caller owns
// both: pts goes to the child and is closed in this process right after the
// start, or the output side would never see an end.
func openPTY() (ptmx, pts *os.File, err error) {
	ptmx, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	// Unlock first, then ask for the number: a locked pts cannot be opened,
	// and the number of a pts that was never unlocked is of no use.
	var zero int32
	if err := ioctl(ptmx.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&zero))); err != nil {
		ptmx.Close()
		return nil, nil, err
	}
	var n uint32
	if err := ioctl(ptmx.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		ptmx.Close()
		return nil, nil, err
	}
	// The name is built from a number the kernel just gave, so nothing of a
	// peer reaches this path.
	pts, err = os.OpenFile("/dev/pts/"+itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		ptmx.Close()
		return nil, nil, err
	}
	return ptmx, pts, nil
}

// itoa writes a pts number. strconv would do, but this keeps the path a thing
// built out of digits and nothing else.
func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// setWinsize applies a window size to the pseudo terminal. The size came
// through ParseFrame, so both numbers are between 1 and MaxDim.
func setWinsize(ptmx *os.File, cols, rows uint16) error {
	ws := winsize{rows: rows, cols: cols}
	return ioctl(ptmx.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

func ioctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

// runShell starts the shell on a pseudo terminal and runs the two pumps and
// the idle watchdog until one of them, or the shell itself, ends.
//
// Whichever ends first, the shell goes: its session is killed as a group, so
// nothing a user started in it keeps the container busy after the tab is gone.
func runShell(ctx context.Context, in io.Reader, out io.Writer, idle time.Duration) (int, error) {
	ptmx, pts, err := openPTY()
	if err != nil {
		return 0, err
	}
	defer ptmx.Close()
	cmd := exec.Command(shellPath, "-i")
	cmd.Dir = service.DataDir
	cmd.Env = shellEnv()
	// All three on the pts: the shell's own errors belong in the terminal the
	// user is looking at, not in the log of this server.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pts, pts, pts
	// Setsid makes the shell a session leader, Setctty gives it the pts as its
	// controlling terminal, and Ctty names the descriptor that pts became in
	// the child, which is Stdin.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		pts.Close()
		return 0, err
	}
	// The child holds it from here. Without this close the read side would
	// never end, because this process would still be a writer.
	pts.Close()

	act := newActivity(idle, time.Now)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	// Buffered for all three, so none of them is held alive by a send nobody
	// takes once the first answer has been read.
	fail := make(chan error, 3)
	go func() {
		fail <- pumpIn(in, ptmx, func(cols, rows uint16) error { return setWinsize(ptmx, cols, rows) }, act)
	}()
	go func() { fail <- pumpOut(out, ptmx, act) }()
	go func() { fail <- watch(runCtx, act) }()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var first error
	select {
	case first = <-fail:
	case <-done:
		return cmd.ProcessState.ExitCode(), nil
	case <-runCtx.Done():
		first = runCtx.Err()
	}
	// Negative PID: the whole session, not only the shell.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	<-done
	if first == nil {
		// The input ended between two frames, which is the shell's input
		// complete. Nothing went wrong, so this is a success.
		return 0, nil
	}
	return 0, first
}
