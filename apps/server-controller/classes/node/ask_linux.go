//go:build linux

package node

import (
	"os"
	"syscall"
	"unsafe"
)

// The pairing code must not stand on the screen either, so it is read with the
// echo of the terminal switched off. golang.org/x/term does this, but NGC
// carries no package for it and takes none in; the two calls below are what
// that package does on Linux, and Linux is where the controller runs.
const (
	tcGets = 0x5401 // TCGETS: read the settings of the terminal
	tcSets = 0x5402 // TCSETS: write them back
	echo   = 0x8    // ECHO in Lflag: the terminal shows what is typed
)

// termios is exactly what the kernel carries over TCGETS and TCSETS: four flag
// words, the line discipline and the control characters, and nothing behind
// them. The form is the same on both architectures NGC is built for. The
// struct of the C library is not this one - it carries two speeds at the end -
// and other architectures order it differently (mips counts 23 control
// characters, powerpc puts the line discipline behind them), so whoever ports
// NGC reads the form off that kernel instead of copying this one.
type termios struct {
	Iflag, Oflag, Cflag, Lflag uint32
	Line                       uint8
	Cc                         [19]uint8
}

// isTerminal reports whether f is a terminal. Reading the settings of a
// terminal is the question that only a terminal answers; "is it a character
// device" is a different one, and the null device answers that with yes -
// which is what systemd hands a unit as its standard input.
func isTerminal(f *os.File) bool {
	var was termios
	return ioctl(f, tcGets, &was)
}

// hideInput switches the echo of the terminal f off and returns the call that
// switches it back on; that call reports whether the terminal took it. ok is
// false when the echo could not be switched off at all. The pairing then goes
// on with the code visible rather than being refused: what this guards against
// is /proc, which every local user reads, not the screen of the admin who is
// typing the code himself - and he is told before he types it (msgEchoOn).
func hideInput(f *os.File) (restore func() bool, ok bool) {
	var was termios
	if !ioctl(f, tcGets, &was) {
		return nil, false
	}
	quiet := was
	quiet.Lflag &^= echo
	if !ioctl(f, tcSets, &quiet) {
		return nil, false
	}
	return func() bool { return ioctl(f, tcSets, &was) }, true
}

// ioctl carries one termios to or from the terminal and reports whether the
// kernel took it.
func ioctl(f *os.File, req uintptr, t *termios) bool {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(t)))
	return errno == 0
}
