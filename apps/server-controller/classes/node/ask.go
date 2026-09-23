package node

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// The pairing code is read here instead of taken from the command line: an
// argument of a running program stands in /proc/<pid>/cmdline, and without
// hidepid every local user of the server may read that file. Whoever caught
// the code inside the thirty seconds of a pairing would pair a controller of
// their own with the API, because the request binds the key of whoever sends
// it (tunnel.PairMAC) and nothing else.
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
	return askAtTerminal(f, prompt, hideInput)
}

// askAtTerminal asks the admin for the code. hide switches the echo of the
// terminal off and hands back the call that switches it on again; only a real
// terminal has one, so the tests bring their own.
func askAtTerminal(f *os.File, prompt io.Writer, hide func(*os.File) (func() bool, bool)) (string, error) {
	restore, hidden := hide(f)
	if hidden {
		// The guard stands before the question: whoever is asked is covered
		// from the first moment he could press Ctrl+C.
		stop := guard(restore)
		defer stop()
		defer func() {
			if !restore() {
				fmt.Fprintln(prompt, msgEchoStuck)
			}
		}()
	} else {
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
	go func() {
		select {
		case s := <-sig:
			restore()
			signal.Stop(sig)
			signal.Reset(s) // from here the signal does what it always did
			if p, err := os.FindProcess(os.Getpid()); err == nil {
				p.Signal(s)
			}
		case <-done:
			signal.Stop(sig)
		}
	}()
	return sync.OnceFunc(func() { close(done) })
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
