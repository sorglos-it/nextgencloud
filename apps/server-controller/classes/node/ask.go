package node

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
	// maxCodeLine bounds the line before it is read, so an endless input
	// fills no memory of a program that runs as root. Anything longer is no
	// pairing code (tunnel.MaxPairCode); the two bytes are the end of a line
	// as Windows writes it.
	maxCodeLine = tunnel.MaxPairCode + 2
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
	fmt.Fprint(prompt, askCode)
	if restore, ok := hideInput(f); ok {
		defer restore()
	}
	code, err := readLine(f)
	// The Enter of the admin was not echoed back, so the next line starts
	// here rather than behind the question.
	fmt.Fprintln(prompt)
	return code, err
}

// terminal reports whether r is the terminal of the admin: an *os.File the
// operating system calls a character device. A pipe, a file and every reader
// a test or a script hands in are none.
func terminal(r io.Reader) (*os.File, bool) {
	f, ok := r.(*os.File)
	if !ok {
		return nil, false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
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
	for line.Len() < maxCodeLine {
		n, err := r.Read(b[:])
		if n == 1 {
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
	}
	return "", errNoCode
}
