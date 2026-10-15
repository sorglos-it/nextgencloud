package node

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// The pairing code is read here instead of taken from the command line: an
// argument of a running program stands in /proc/<pid>/cmdline, and without
// hidepid every local user of the server may read that file. Whoever caught
// the code while it is still good would pair a controller of their own with
// the API, because the request binds the key of whoever sends it
// (tunnel.PairMAC) and nothing else.
//
// How long "still good" is, measured 12.10.2026: tunnel.PairLifetime = 86400,
// so 24 HOURS, not the thirty seconds this comment claimed until today. The
// thirty seconds are pairing.Timeout and bound the HTTP request, nothing else.
// The window is 2880 times what the old text said, which does not change the
// decision here - it only makes it matter more. Whether 24 hours is the right
// life for a pairing code is a security question for the coordinator; it is
// written down in docs/controller-abnahme.md.
//
// The code is a secret: it goes from here into a config.SecretText and into
// nothing else. It is in no message, no error and no log, and only
// tunnel.ParsePairCode looks at it.
const (
	// askCode is what the admin reads at the terminal.
	askCode = "Kopplungscode: "
	// msgEchoOn is said before the code is entered, not afterwards: on a
	// terminal that does not let the echo be switched off, the code stands on
	// the screen. The pairing goes on all the same, so the line says what to
	// watch out for and not that something broke.
	msgEchoOn = "Achtung: Der Kopplungscode ist beim Eingeben zu sehen. Bitte darauf achten, dass niemand mitliest."
	// msgEchoStuck is the one case the admin cannot read his way out of: the
	// terminal did not take the echo back, so the screen stays empty whatever
	// he types. The line says what to type blind.
	msgEchoStuck = "Die Eingabe bleibt unsichtbar. Bitte stty sane eingeben und mit der Eingabetaste bestätigen."
	// maxCodeLine bounds the line before it is read, so an endless input
	// fills no memory of a program that runs as root. Anything longer is no
	// pairing code (tunnel.MaxPairCode); the two bytes are the end of a line
	// as Windows writes it.
	maxCodeLine = tunnel.MaxPairCode + 2
	// maxQuiet bounds how often a reader may answer with neither a byte nor an
	// error. Nothing behind os.Stdin does that, but the controller runs as
	// root: an input that says nothing must not turn it into a program that
	// spins one core for good.
	maxQuiet = 64
)

// errNoCode reports that no pairing code came in: nothing was entered, the
// line had no end, or the input could not be read. It names none of it.
var errNoCode = errors.New("node: no pairing code was read")

// hider switches the echo of a terminal off and hands back the call that
// switches it on again, which reports whether the terminal took it. Only a
// real terminal has one, so the tests bring their own.
type hider func(*os.File) (restore func() bool, ok bool)

// watcher puts the guard over the terminal and hands back the call that takes
// it down again; the tests bring their own for the same reason.
type watcher func(restore func() bool) (stop func())

// readCode reads the pairing code. At a terminal it asks for it and keeps it
// off the screen; from a pipe or a file it takes the first line and asks
// nothing, so the ulh script can hand the code over without putting it into
// the process list. The prompt is the only thing written, and the code is
// never part of it.
func readCode(stdin io.Reader, prompt io.Writer) (string, error) {
	f, isTTY := terminal(stdin)
	if !isTTY {
		return readLine(stdin)
	}
	return askAtTerminal(f, prompt, hideInput, guard)
}

// restorer carries the call that gives the terminal its echo back. The guard
// holds this and not the call itself, because it is installed before the echo
// is switched off: until then there is nothing to give back and restoring is
// a call that does nothing. The guard reads it from its own goroutine, so it
// is handed over atomically.
type restorer struct{ back atomic.Pointer[func() bool] }

// set names the call that gives the echo back, from the moment it exists.
func (r *restorer) set(back func() bool) { r.back.Store(&back) }

// restore gives the echo back and reports whether the terminal took it. With
// nothing to give back there is nothing that could fail.
func (r *restorer) restore() bool {
	if back := r.back.Load(); back != nil {
		return (*back)()
	}
	return true
}

// askAtTerminal asks the admin for the code. hide switches the echo of the
// terminal off and hands back the call that switches it on again; watch puts
// the guard over it that gives the echo back when a signal ends the program.
func askAtTerminal(f *os.File, prompt io.Writer, hide hider, watch watcher) (string, error) {
	// The guard stands before the echo goes off, not behind it: between the
	// two calls lies a moment in which a signal would leave the admin in
	// front of a terminal that shows nothing any more. While there is nothing
	// to give back the guard costs nothing.
	var back restorer
	stop := watch(back.restore)
	defer stop()
	restore, hidden := hide(f)
	if hidden {
		// From here the guard reaches the terminal, and whoever is asked is
		// covered from the first moment he could press Ctrl+C.
		back.set(restore)
		defer func() {
			if !restore() {
				fmt.Fprintln(prompt, msgEchoStuck)
			}
		}()
	} else {
		// Nothing was switched off, so there is nothing to give back: the
		// guard steps down again right away.
		stop()
		fmt.Fprintln(prompt, msgEchoOn)
	}
	fmt.Fprint(prompt, askCode)
	code, err := readLine(f)
	if hidden {
		// The Enter of the admin was not echoed back, so the next line starts
		// here rather than behind the question. A terminal that echoed it has
		// broken the line itself already.
		fmt.Fprintln(prompt)
	}
	return code, err
}

// guard gives the terminal its echo back when a signal ends the program, and
// stop takes it down again. A signal kills the runtime without running a
// single defer: whoever presses Ctrl+C at the question would otherwise sit in
// front of a shell that shows nothing any more and would have to type
// "stty sane" blind. Afterwards the program still dies of that signal, so the
// shell sees what happened and the exit code stays what it was.
func guard(restore func() bool) (stop func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		watchSignal(sig, done, restore, raise)
	}()
	// Waiting keeps the order: whoever ends the question goes on only once a
	// signal that came in at the same moment has been let through, and not
	// while the program is already pairing.
	return sync.OnceFunc(func() { close(done); <-ended })
}

// watchSignal waits for a signal or for the end of the question, gives the
// terminal its echo back and lets the signal through. It is the guard without
// its channels, so a test can hand it both.
func watchSignal(sig chan os.Signal, done <-chan struct{}, restore func() bool, raise func(os.Signal)) {
	select {
	case s := <-sig:
		restore()
		signal.Stop(sig)
		raise(s)
	case <-done:
		signal.Stop(sig)
		// A signal that came in a moment before the question ended lies in
		// the channel, and which of the two branches was taken is then a
		// matter of chance. signal.Stop only ends the delivery, so after it
		// the channel holds everything that ever arrived: whoever pressed
		// Ctrl+C ended the program, and dropping his signal here would leave
		// a controller that pairs on as if nothing had happened.
		select {
		case s := <-sig:
			restore()
			raise(s)
		default:
		}
	}
}

// raise lets the signal do what it always did: the program dies of it, so the
// shell sees what happened and the exit code stays what it was.
func raise(s os.Signal) {
	signal.Reset(s)
	if p, err := os.FindProcess(os.Getpid()); err == nil {
		p.Signal(s)
	}
}

// terminal reports whether r is the terminal of the admin. A pipe, a file and
// every reader a test or a script hands in are none, and the null device is
// none either: systemd gives exactly that to a unit as its standard input, and
// a question asked into it is asked of nobody.
func terminal(r io.Reader) (*os.File, bool) {
	f, ok := r.(*os.File)
	if !ok || !isTerminal(f) {
		return nil, false
	}
	return f, true
}

// readLine reads one line, at most maxCodeLine bytes of it. It takes one byte
// at a time: a terminal hands the line over when Enter is pressed, and nothing
// behind the line is taken out of the reader. A line that does not end inside
// the bound is no code, and nothing of it is returned.
func readLine(r io.Reader) (string, error) {
	var line strings.Builder
	var b [1]byte
	quiet := 0
	for line.Len() < maxCodeLine {
		n, err := r.Read(b[:])
		if n == 1 {
			quiet = 0
			if b[0] == '\n' {
				return line.String(), nil
			}
			line.WriteByte(b[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) { // a last line without an end
				return line.String(), nil
			}
			return "", errNoCode
		}
		if n == 0 {
			if quiet++; quiet > maxQuiet {
				return "", errNoCode
			}
		}
	}
	return "", errNoCode
}
