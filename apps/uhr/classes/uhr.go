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
package classes

import (
	"fmt"
	"io"
	"os"
	"os/signal"
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
	// timeFormat is RFC 3339 with seconds. The window shows what arrives and
	// formats nothing itself, so the one place the shape of this line is
	// decided is here.
	timeFormat = time.RFC3339
)

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
	return answer(os.Getenv(service.EnvAction), version, time.Now(), stdout, stderr)
}

// answer writes the reply to one action. now is an argument and not a call to
// time.Now, so a case can measure the line this writes.
func answer(action, version string, now time.Time, stdout, stderr io.Writer) int {
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
	return 0
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
