// Why an activation was taken back - in the journal of this controller, and
// nowhere else.
//
// It exists because of what it costs when it does not. Measured on 20.10.2026
// on the Debian machine: the terminal app was activated, the controller made
// the system user and removed it again in the same second, and NOTHING said
// why - not the journal, not the answer to the surface, which showed the
// general sentence of E_INTERNAL. The cause was found with strace on the
// running service: "podman load" answered EACCES on the directory above the
// app's, because that directory was root's and 0700 (runtime.EnsureData). A
// line of six words in the journal would have said it.
//
// What is written is a CLOSED LIST, twice over - the step and the reason - and
// that is the whole design of this file. The journal of a server is read by an
// administrator, and the same rule holds for it as for the reason a store gets
// shown (appstore.reasons): a line a stranger can write words into is not a
// line. So nothing of an error's own text goes in. The container engine's
// stderr, a file name out of a package, the answer of a store: all of it is
// somebody else's text, and an administrator reading a journal must not have
// to wonder who wrote the line.
//
// What is left out, and on purpose: the error itself. It would name the step
// and the cause far better, and it cannot be let in - not with "%v" and not
// with slog's own quoting, which escapes a newline but writes every other
// character a foreign program put in there. The price is paid where it is
// cheap: the step says WHERE, the reason says WHAT KIND, and the two together
// are what the strace said.
package install

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"syscall"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// A step of a setup, as it stands in the journal. The names are the ones
// Manager.setUp already uses for the undo of each step (undo.what), so a line
// in the journal and the code that wrote it carry one word.
type step string

const (
	stepUser      step = "user"
	stepData      step = "data"
	stepRun       step = "run"
	stepImage     step = "image"
	stepContainer step = "container"
	stepRow       step = "row"
	stepLicence   step = "licence"
	stepSeq       step = "seq"
)

// steps is the whole list, so a case can show that no other word ever reaches
// an administrator (TestEveryStepAndReasonComesOutOfTheClosedList).
var steps = []step{stepUser, stepData, stepRun, stepImage, stepContainer,
	stepRow, stepLicence, stepSeq}

// The reasons. English, like every other line this controller writes
// (node.run), and short enough to read at the end of a line that already
// carries the app and the step.
const (
	reasonPermission = "no permission"
	reasonMissing    = "not there"
	reasonExists     = "already there"
	reasonFull       = "no space left"
	reasonCancelled  = "cancelled"
	reasonRefused    = "not allowed by this installation"
	reasonLicence    = "no usable licence"
	reasonWiring     = "this controller is wired wrong"
	reasonUnchecked  = "did not verify"
	reasonOther      = "something else"
)

// reasons is the whole list, for the same case as steps.
var reasons = []string{reasonPermission, reasonMissing, reasonExists, reasonFull,
	reasonCancelled, reasonRefused, reasonLicence, reasonWiring, reasonUnchecked, reasonOther}

// reasonFor turns a cause into one of reasons.
//
// The order is read from the outside in: the walls of this installation first
// (they are the answer, not a symptom), then what the kernel said. EACCES is
// what this file was written for and stands high up, because every other
// question about it ends in reasonOther - and reasonOther is exactly the line
// that sent a person to strace.
//
// Nothing here looks at the text of an error, only at its kind. A sentinel is
// compared with errors.Is, an errno with errors.As: both go through every
// wrapping, which is how an error of the engine three calls down still arrives
// as "no permission".
func reasonFor(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrForbidden), errors.Is(err, store.ErrRollback):
		return reasonRefused
	case errors.Is(err, ErrLicense):
		return reasonLicence
	case errors.Is(err, ErrInternal):
		return reasonWiring
	case errors.Is(err, ErrVerify), errors.Is(err, ErrSource):
		return reasonUnchecked
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return reasonCancelled
	case errors.Is(err, fs.ErrPermission):
		// fs.ErrPermission covers EACCES and EPERM both, and the strace of
		// 20.10.2026 is the first of the two.
		return reasonPermission
	case errors.Is(err, fs.ErrNotExist):
		return reasonMissing
	case errors.Is(err, fs.ErrExist):
		return reasonExists
	default:
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.ENOSPC {
			return reasonFull
		}
		return reasonOther
	}
}

// why writes the one line of this file and hands the error back unchanged, so
// it stands at the place that failed and reads as the return it is:
//
//	return m.why(p.app, stepData, err)
//
// It is a method and not a function because the logger is the Manager's
// (Deps.Log) and a Manager without one writes to slog.Default(), which under
// systemd is the journal of this service.
//
// Level Error, and that is deliberate: this line is the END of an activation
// an administrator started and every step of it being taken back. There is no
// line for the steps that worked - an activation that works says so by the app
// being there, and a journal full of progress is a journal nobody reads.
func (m *Manager) why(app string, at step, err error) error {
	m.log().Error("an app was not set up and every step was taken back",
		"app", app, "step", string(at), "reason", reasonFor(err))
	return err
}

// log is the logger of this Manager, or the default one.
func (m *Manager) log() *slog.Logger {
	if m.d.Log != nil {
		return m.d.Log
	}
	return slog.Default()
}
