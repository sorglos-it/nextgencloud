// Package classes holds what ngc-uhr does: the service of the app
// org.sorglos.uhr and the one command that keeps its container up.
//
// WHY THIS APP EXISTS. It is the second app of NGC, and it is deliberately
// tiny: the work it proves is the WAY an app takes into the store
// (docs/app-weg.md), not the app. A clock needs no network, no root and no
// state, so every right it does not ask for is one the way does not need
// either.
//
// WHAT IT MEASURES THAT NOTHING ELSE DOES: the two directions a service can
// write in. Contract V3 hands the stdout of a service to the app's window
// (serve.CallInput.Out, appframe.doCall resolves ngc.call with it), while its
// stderr reaches the LOG of the controller and nothing else - and only where
// an operator switched the text on (serve.CallInput.LogStderr, off by
// default). So a service that reports on stderr has told the server about
// itself and the person in front of the window nothing. The action "zeit"
// writes one line in each direction, on purpose, so that whoever follows the
// way sees which of the two arrives where.
//
// WHAT 1.0.1 ADDED, AND WHY AN APP THAT NEEDS NO STATE GOT SOME. Up to 1.0.0
// this app wrote nothing anywhere, and that made one question unmeasurable:
// what happens to the DATA of an app when it is lifted to a new version. The
// data directory is encrypted and belongs to the app's own user
// (runtime.DataDirOf), and an app that keeps nothing in it cannot show
// whether its data survived. So "zeit" now counts how often it was read and
// keeps the count in $NGC_DATA/zugriffe. A count is the smallest thing that
// is worth nothing to anyone and is still visibly lost with the directory.
package classes

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/service"
)

// What ngc-uhr answers a call it cannot carry out. German, because it reaches
// an admin through the log of the controller.
const (
	usage       = "Aufruf: ngc-uhr (als Dienst, im Behälter) oder ngc-uhr hold"
	msgNoAction = "NGC_ACTION ist leer. Dieses Programm läuft als Dienst einer NGC-App, nicht von Hand."
	msgUnknown  = "Unbekannte Aktion %q. Dieser Dienst kennt nur %q.\n"
	actionZeit  = "zeit"
	holdCommand = "hold"
	// countFile is the one file this app keeps, directly under $NGC_DATA.
	countFile = "zugriffe"
	// timeFormat is RFC 3339 with seconds. The window shows what arrives and
	// formats nothing itself, so the one place the shape of this line is
	// decided is here.
	timeFormat = time.RFC3339
	// msgCount reaches an admin through the log of the controller when the
	// count could not be written. It is not a reason to refuse the action: a
	// clock that cannot count is still a clock.
	msgCount = "Der Zugriffszähler in %s war nicht schreibbar: %v\n"
)

// errNoData is what count answers when the environment named no data
// directory. Only a call from outside a container can get there: the
// controller always sets NGC_DATA (service.EnvData).
var errNoData = errors.New("NGC_DATA ist leer")

// Main carries out one call of ngc-uhr and returns the exit status: 0 for
// done, 1 for a call that failed, 2 for a call Main does not understand.
//
// The clock is read here and nowhere below, so everything behind this
// function stays reproducible.
func Main(version string, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == holdCommand:
		return hold(stderr)
	case len(args) != 0:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	return answer(os.Getenv(service.EnvAction), version, os.Getenv(service.EnvData),
		time.Now(), stdout, stderr)
}

// answer writes the reply to one action. now and dataDir are arguments and not
// a call to time.Now and to the environment, so a case can measure the lines
// this writes.
func answer(action, version, dataDir string, now time.Time, stdout, stderr io.Writer) int {
	if action == "" {
		fmt.Fprintln(stderr, msgNoAction)
		return 1
	}
	if action != actionZeit {
		fmt.Fprintf(stderr, msgUnknown, action, actionZeit)
		return 1
	}
	// ONE LINE TO EACH SIDE, and that is the measurement this app carries.
	// The line on stdout is the answer the window shows; the line on stderr
	// is the one only the controller's log can ever see. Whoever reads them
	// both in the same run has measured which direction arrives where, which
	// is what cost the terminal app a round (see the package comment).
	fmt.Fprintf(stderr, "ngc-uhr %s beantwortet %q\n", version, action)
	fmt.Fprintln(stdout, now.Format(timeFormat))
	// THE SECOND LINE, AND THE WINDOW READS IT BY ITS NUMBER. The shape of
	// what this service answers is decided here and nowhere else (app.js
	// splits on the newline and formats nothing): line one is the time, line
	// two the count. A window that got only line one stands in front of an
	// older service, which is a state it survives - it shows the time and
	// nothing about the count.
	n, err := count(dataDir)
	if err != nil {
		fmt.Fprintf(stderr, msgCount, dataDir, err)
	}
	fmt.Fprintln(stdout, n)
	return 0
}

// count raises the number in dataDir/zugriffe by one and gives back the new
// one.
//
// It answers a number in every case, and that is the decision in it: a
// directory that cannot be read gives 1 - the first reading of a clock that
// counts from now on - and the error goes to the admin's side. Refusing the
// action instead was dropped: the data of this app is a counter, and a
// counter is not worth an app that stops working.
//
// The file is written whole, under a second name and then renamed, so a
// reader that arrives in the middle of it sees the old number and never half
// a number. There is no lock: one container holds one service of one app, and
// the controller runs one call of it at a time (service.Call).
func count(dataDir string) (uint64, error) {
	if dataDir == "" {
		return 1, errNoData
	}
	path := filepath.Join(dataDir, countFile)
	var n uint64
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		// A file this app did not write, or wrote and lost half of, is not
		// worth an error of its own: it counts as no count.
		n, _ = strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	case !errors.Is(err, fs.ErrNotExist):
		return 1, err
	}
	n++
	tmp := path + ".neu"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(n, 10)+"\n"), 0o600); err != nil {
		return n, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return n, err
	}
	return n, nil
}

// hold waits until the container is stopped, so the image needs no program of
// its own to keep it up. The terminal app does the same with "ngc-controller
// hold" (node.hold); this app carries its own, because its image holds
// nothing of NGC besides this one program.
func hold(stderr io.Writer) int {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	// Nothing is written on the way out: a container that was stopped on
	// purpose is not an event an admin has to read about.
	_ = stderr
	return 0
}
