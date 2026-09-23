//go:build linux

package node

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// These tests need a real terminal, and only Linux has one that a test may
// make for itself. Linux is where the controller runs, so what is proven here
// is proven where it counts; on the machine the code is written on these tests
// are not built at all.
const (
	ptyLock   = 0x40045431 // TIOCSPTLCK: lock or unlock the other end
	ptyNumber = 0x80045430 // TIOCGPTN: which /dev/pts/<n> the other end is
)

// waitLimit bounds how long a test waits for the other process. It is long
// enough for a loaded server with -race and short enough to end as the failure
// it is instead of as the timeout of the whole run.
const waitLimit = 30 * time.Second

// Whether the two echo tests really ran is not something a green run shows by
// itself: where there is no pseudo terminal they skip. noPTY counts the skips
// of those two alone, echoTested the runs that really asked a terminal;
// TestMain fails a run that only skipped, so nobody takes "ok" for an answer
// that was never given.
var noPTY, echoTested atomic.Int64

// TestMain runs the tests of this package and then asks whether the echo was
// really proven. Only the two echo tests prove it, so only their skips count:
// a run narrowed to another test proves nothing about the echo either way and
// must not be made red over it.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 && noPTY.Load() > 0 && echoTested.Load() == 0 {
		fmt.Fprintln(os.Stderr, "no pseudo terminal on this machine: neither echo test ran, so nothing about the echo of the terminal was proven")
		code = 1
	}
	os.Exit(code)
}

// pty opens a pseudo terminal and gives back both of its ends: the side a
// program reads from and writes to, and the side that plays the admin.
func pty(t *testing.T) (screen, keyboard *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal here: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	var unlock int32
	if !ptyCall(m, ptyLock, unsafe.Pointer(&unlock)) {
		t.Fatal("the pseudo terminal stayed locked")
	}
	var n uint32
	if !ptyCall(m, ptyNumber, unsafe.Pointer(&n)) {
		t.Fatal("the pseudo terminal has no number")
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open the other end: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, m
}

// ptyCall carries one small value to or from the pseudo terminal.
func ptyCall(f *os.File, req uintptr, p unsafe.Pointer) bool {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(p))
	return errno == 0
}

// echoOn reports whether the terminal shows what is typed into it. It asks the
// terminal itself, so it sees the state a program left behind, whatever that
// program did or did not do on its way out.
func echoOn(t *testing.T, f *os.File) bool {
	t.Helper()
	var was termios
	if !ioctl(f, tcGets, &was) {
		t.Fatal("the settings of the pseudo terminal could not be read")
	}
	return was.Lflag&echo != 0
}

// A terminal is what answers the question every terminal answers, and nothing
// else is. The null device is a character device as a terminal is, and systemd
// hands exactly that to a unit as its standard input: a question asked into it
// is asked of nobody.
func TestTerminalIsOnlyARealTerminal(t *testing.T) {
	screen, _ := pty(t)
	if _, ok := terminal(screen); !ok {
		t.Error("a pseudo terminal does not count as a terminal")
	}
	dev, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dev.Close() })
	if _, ok := terminal(dev); ok {
		t.Error("the null device counts as a terminal, want it taken for what it is")
	}
}

// At a terminal the admin is asked, and the question is all that is written.
func TestReadCodeAsksAtARealTerminal(t *testing.T) {
	screen, keyboard := pty(t)
	code := pairCode()
	go func() { keyboard.WriteString(code + "\n") }()
	var prompt bytes.Buffer
	got, err := readCode(screen, &prompt)
	if err != nil {
		t.Fatalf("readCode: %v", err)
	}
	if got != code {
		t.Error("readCode did not return the line that was typed")
	}
	if prompt.String() != askCode+"\n" {
		t.Errorf("prompt = %q, want the question and the end of the line alone", prompt.String())
	}
}

// childAsk marks the process the two tests below start. The pairing code is
// not in its environment: it is typed into the pseudo terminal.
const childAsk = "NGC_TEST_ASK_CHILD"

// asking starts the test binary in another process, reading the code from a
// pseudo terminal, and comes back once that process has asked. Another process
// is the only one a signal may kill without taking the test with it.
func asking(t *testing.T) (keyboard *os.File, cmd *exec.Cmd, out *bytes.Buffer) {
	t.Helper()
	// Only the two tests that go through here prove the echo, so only their
	// skips may make the guard of TestMain fire. Counted is every skip of
	// them and not only the first way out: whichever open of the terminal it
	// was that failed, a test that gave up before it asked one proved nothing
	// about the echo, and the guard has to see that.
	t.Cleanup(func() {
		if t.Skipped() {
			noPTY.Add(1)
		}
	})
	screen, keyboard := pty(t)
	out = &bytes.Buffer{}
	cmd = exec.Command(os.Args[0], "-test.run=^TestAskInAnotherProcess$", "-test.v")
	cmd.Env = append(os.Environ(), childAsk+"=1")
	cmd.Stdin, cmd.Stderr, cmd.Stdout = screen, screen, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitForQuestion(t, keyboard)
	if echoOn(t, keyboard) {
		t.Fatal("the terminal still shows what is typed while the code is read")
	}
	return keyboard, cmd, out
}

// waitForQuestion reads the terminal until the question stands on it. Only
// then is the other process really inside the reading.
func waitForQuestion(t *testing.T, keyboard *os.File) {
	t.Helper()
	seen := make(chan string, 1)
	go func() {
		var b [64]byte
		var got strings.Builder
		for {
			n, err := keyboard.Read(b[:])
			if n > 0 {
				got.Write(b[:n])
				if strings.Contains(got.String(), askCode) {
					break
				}
			}
			if err != nil {
				break
			}
		}
		seen <- got.String()
	}()
	select {
	case s := <-seen:
		if !strings.Contains(s, askCode) {
			t.Fatalf("the other process wrote %q, want the question", s)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the other process never asked")
	}
}

// waitFor waits for the other process, but not for ever. A controller that
// does not die of the signal any more, or does not end after the code, would
// otherwise stand here until go test gives up after ten minutes: what is a
// failure with a name would look like a run that hangs.
func waitFor(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	select {
	case err := <-ended:
		return err
	case <-time.After(waitLimit):
		cmd.Process.Kill()
		<-ended
		t.Fatalf("the other process was still running after %s, want it ended", waitLimit)
		return nil
	}
}

// quiet reports whether the other process kept the pairing code to itself.
func quiet(t *testing.T, out *bytes.Buffer) {
	t.Helper()
	if bytes.Contains(out.Bytes(), []byte(tunnel.PairPrefix)) {
		t.Fatal("the other process wrote a pairing code")
	}
}

// After the code was entered the terminal shows what is typed again.
func TestTheEchoIsBackAfterTheCode(t *testing.T) {
	keyboard, cmd, out := asking(t)
	echoTested.Add(1)
	if _, err := keyboard.WriteString(pairCode() + "\n"); err != nil {
		t.Fatal(err)
	}
	err := waitFor(t, cmd)
	quiet(t, out)
	if err != nil {
		t.Fatalf("the other process failed: %v\n%s", err, out)
	}
	if !echoOn(t, keyboard) {
		t.Error("the terminal shows nothing any more once the code was read")
	}
}

// And after Ctrl+C at the question as well. A signal kills the runtime without
// running a single defer, so whoever breaks off here would otherwise sit in
// front of a shell that shows nothing and would have to type "stty sane"
// blind. The program still ends of that signal, so the shell sees what
// happened.
func TestTheEchoIsBackAfterCtrlC(t *testing.T) {
	keyboard, cmd, out := asking(t)
	echoTested.Add(1)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err := waitFor(t, cmd)
	quiet(t, out)
	var ended *exec.ExitError
	if !errors.As(err, &ended) {
		t.Fatalf("the other process ended with %v, want it killed by the signal\n%s", err, out)
	}
	if st, ok := ended.Sys().(syscall.WaitStatus); !ok || !st.Signaled() || st.Signal() != syscall.SIGINT {
		t.Errorf("the other process ended with %v, want it killed by the signal", ended)
	}
	if !echoOn(t, keyboard) {
		t.Error("Ctrl+C at the question leaves the terminal without an echo")
	}
}

// TestAskInAnotherProcess is the process the two tests above start. It is no
// test of its own: without the mark in the environment it does nothing.
func TestAskInAnotherProcess(t *testing.T) {
	if os.Getenv(childAsk) == "" {
		t.Skip("started by the terminal tests alone")
	}
	got, err := readCode(os.Stdin, os.Stderr)
	if err != nil {
		t.Fatalf("readCode: %v", err)
	}
	if got != pairCode() {
		t.Fatal("readCode did not return the line that was typed")
	}
}
