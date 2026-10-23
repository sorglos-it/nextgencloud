package install

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"syscall"
	"testing"
)

// journal collects what this Manager wrote, as a handler and not as text: a
// case that parsed a line would measure slog's formatting, and what is at
// stake here is the ATTRIBUTES - which words reach an administrator.
type journal struct {
	lines []map[string]string
}

func (j *journal) Enabled(context.Context, slog.Level) bool { return true }
func (j *journal) WithAttrs([]slog.Attr) slog.Handler       { return j }
func (j *journal) WithGroup(string) slog.Handler            { return j }
func (j *journal) Handle(_ context.Context, r slog.Record) error {
	line := map[string]string{"msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool {
		line[a.Key] = a.Value.String()
		return true
	})
	j.lines = append(j.lines, line)
	return nil
}

// withJournal puts a journal under this stand and answers it.
func withJournal(s *stand) *journal {
	j := &journal{}
	s.deps.Log = slog.New(j)
	return j
}

// only answers the one line of a failed setup. More than one is a failure of
// its own: this controller writes exactly one, at the step it stopped at.
func (j *journal) only(t *testing.T) map[string]string {
	t.Helper()
	if len(j.lines) != 1 {
		t.Fatalf("the journal holds %d lines, want the one of the failed setup: %v",
			len(j.lines), j.lines)
	}
	return j.lines[0]
}

// THE CASE OF THE 20.10.2026: an activation whose data directory could not be
// made says so in the journal, with the step and the kind of the cause.
//
// What it is held against is a measurement, not a guess. On the Debian machine
// the controller made the system user of the terminal app and removed it again
// in the same second, and nothing anywhere said why - the cause (podman
// answering EACCES on the directory above the app's) was found with strace on
// the running service. These three attributes are what that strace said.
func TestTheJournalSaysWhichStepFailedAndWhy(t *testing.T) {
	s := newStand(t)
	j := withJournal(s)
	s.sys.fail, s.sys.failWith = "EnsureData", fs.ErrPermission

	if _, err := s.activate(); err == nil {
		t.Fatal("the activation came off, so there is nothing to say why about")
	}
	line := j.only(t)
	for k, want := range map[string]string{
		"level":  "ERROR",
		"app":    testApp,
		"step":   "data",
		"reason": "no permission",
	} {
		if line[k] != want {
			t.Errorf("the journal says %s=%q, want %q", k, line[k], want)
		}
	}
	if line["msg"] == "" {
		t.Error("the line carries no sentence")
	}
}

// And nothing is written for an activation that comes off: a journal full of
// progress is a journal nobody reads, and this line is the end of something an
// administrator started.
func TestAnActivationThatWorksWritesNoLine(t *testing.T) {
	s := newStand(t)
	j := withJournal(s)
	if _, err := s.activate(); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if len(j.lines) != 0 {
		t.Fatalf("the journal holds %v for an activation that worked", j.lines)
	}
}

// NO FOREIGN TEXT IN A LINE AN ADMINISTRATOR READS. The container engine's
// stderr, the answer of a store, a name out of a package: all of it is
// somebody else's, and the same rule holds here as for the reason a store gets
// shown (appstore.reasons, "no text of a store's could hide in it").
//
// The error of this case is shaped like what it is written against: a line of
// its own, a word that reads like a verdict of this controller, and a sentinel
// underneath so the kind is still found. What must be true is that not one
// word of the text reaches the journal and the kind still does.
func TestNoTextOfAnErrorReachesTheJournal(t *testing.T) {
	s := newStand(t)
	j := withJournal(s)
	foreign := errors.New("\nngc-controller: every app of this server was removed by the admin")
	s.sys.fail, s.sys.failWith = "EnsureData", errors.Join(fs.ErrPermission, foreign)

	if _, err := s.activate(); err == nil {
		t.Fatal("the activation came off")
	}
	line := j.only(t)
	for k, v := range line {
		for _, bad := range []string{"removed by the admin", "ngc-controller:", "every app"} {
			if strings.Contains(v, bad) {
				t.Fatalf("the journal line carries the error's own text in %s=%q", k, v)
			}
		}
	}
	if line["reason"] != "no permission" {
		t.Errorf("reason=%q, want the kind of the cause to survive", line["reason"])
	}
}

// Every step and every reason that can ever be written stands in the two
// closed lists. The case drives a failure through each call of the machine and
// each call of the engine rather than reading the constants: a step added at a
// new return of setUp and left out of the list is exactly what this catches.
func TestEveryStepAndReasonComesOutOfTheClosedList(t *testing.T) {
	for _, call := range []string{"EnsureUser", "EnsureData", "EnsureRun"} {
		t.Run(call, func(t *testing.T) {
			s := newStand(t)
			j := withJournal(s)
			s.sys.fail = call
			if _, err := s.activate(); err == nil {
				t.Fatal("the activation came off")
			}
			line := j.only(t)
			if !hasStep(line["step"]) {
				t.Errorf("step=%q stands in no closed list", line["step"])
			}
			if !hasReason(line["reason"]) {
				t.Errorf("reason=%q stands in no closed list", line["reason"])
			}
		})
	}
	for _, call := range []string{"LoadImage", "Create", "Start"} {
		t.Run(call, func(t *testing.T) {
			s := newStand(t)
			j := withJournal(s)
			s.eng.fail = call
			if _, err := s.activate(); err == nil {
				t.Fatal("the activation came off")
			}
			line := j.only(t)
			if !hasStep(line["step"]) {
				t.Errorf("step=%q stands in no closed list", line["step"])
			}
			if !hasReason(line["reason"]) {
				t.Errorf("reason=%q stands in no closed list", line["reason"])
			}
		})
	}
}

func hasStep(s string) bool {
	for _, st := range steps {
		if string(st) == s {
			return true
		}
	}
	return false
}

func hasReason(s string) bool {
	for _, r := range reasons {
		if r == s {
			return true
		}
	}
	return false
}

// reasonFor reads the KIND of a cause through every wrapping, and the order of
// its questions is read here too: a wall of this installation is the answer,
// not the errno underneath it.
func TestTheReasonIsTheKindOfTheCause(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"nothing", nil, ""},
		{"the permission the strace showed", syscall.EACCES, reasonPermission},
		{"and wrapped three deep", errors.Join(errors.New("x"),
			errors.Join(errors.New("y"), fs.ErrPermission)), reasonPermission},
		{"a directory that is not there", fs.ErrNotExist, reasonMissing},
		{"one that is", fs.ErrExist, reasonExists},
		{"a full disk", syscall.ENOSPC, reasonFull},
		{"a cancelled activation", context.Canceled, reasonCancelled},
		{"one the installation refuses", ErrForbidden, reasonRefused},
		{"a paid app without a licence", ErrLicense, reasonLicence},
		{"this controller wired wrong", ErrInternal, reasonWiring},
		{"a package that did not verify", ErrVerify, reasonUnchecked},
		{"something nobody wrote a reason for", errors.New("the machine says no"), reasonOther},
		// The order: a refusal of this installation that carries an errno
		// underneath is still a refusal.
		{"a refusal over an errno", errors.Join(ErrForbidden, syscall.EACCES), reasonRefused},
	} {
		if got := reasonFor(c.err); got != c.want {
			t.Errorf("%s: reasonFor = %q, want %q", c.name, got, c.want)
		}
	}
	// And every answer it can give stands in the list a case holds the journal
	// against (hasReason above): a reason added here and not there would pass
	// every line of this file.
	for _, r := range reasons {
		if !hasReason(r) {
			t.Errorf("%q is in reasons and not found by hasReason", r)
		}
	}
}
