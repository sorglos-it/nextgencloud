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

// termios is struct termios of Linux (termios.h, NCCS = 32), with the same
// layout on both architectures NGC is built for. The three bytes are the
// padding the C compiler puts before the two speeds.
type termios struct {
	Iflag, Oflag, Cflag, Lflag uint32
	Line                       uint8
	Cc                         [32]uint8
	_                          [3]uint8
	Ispeed, Ospeed             uint32
}

// hideInput switches the echo of the terminal f off and returns the call that
// switches it back on. ok is false when the terminal does not let it happen;
// the admin then sees what is typed, which is still far less than every local
// user seeing it in /proc. A pairing is never refused over this: the server
// would stay unpaired, and the code is spent either way.
func hideInput(f *os.File) (restore func(), ok bool) {
	var was termios
	if !ioctl(f, tcGets, &was) {
		return nil, false
	}
	quiet := was
	quiet.Lflag &^= echo
	if !ioctl(f, tcSets, &quiet) {
		return nil, false
	}
	return func() { ioctl(f, tcSets, &was) }, true
}

// ioctl carries one termios to or from the terminal and reports whether the
// kernel took it.
func ioctl(f *os.File, req uintptr, t *termios) bool {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(t)))
	return errno == 0
}
