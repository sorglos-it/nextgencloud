package classes

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fixed is the one moment every case here measures against, so no case reads
// a clock.
var fixed = time.Date(2026, 10, 24, 11, 30, 0, 0, time.UTC)

// TestZeitAnswersOnStdout is the case the whole app exists for: the time goes
// to stdout, which is the only direction the app's window ever sees.
func TestZeitAnswersOnStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := answer("zeit", "1.0.0", fixed, &out, &errOut); code != 0 {
		t.Fatalf("answer = %d, want 0 (stderr: %q)", code, errOut.String())
	}
	if got := out.String(); got != "2026-10-24T11:30:00Z\n" {
		t.Errorf("stdout = %q, want the time in RFC 3339", got)
	}
}

// TestZeitAlsoWritesStderr holds the second half of that measurement: there IS
// a line on stderr, and it is NOT the answer. A reader who sees only this line
// in a log has not seen what the window got.
func TestZeitAlsoWritesStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	answer("zeit", "1.0.0", fixed, &out, &errOut)
	if !strings.Contains(errOut.String(), "zeit") {
		t.Errorf("stderr = %q, want the line that names the action", errOut.String())
	}
	if strings.Contains(errOut.String(), "2026-10-24T11:30:00Z") {
		t.Error("the time stands on stderr too; then the two directions cannot be told apart")
	}
}

// TestUnknownActionFails: an action the manifest does not carry must not be
// answered with a time. Nothing but the manifest decides what may be called,
// and this is the service's own half of that.
func TestUnknownActionFails(t *testing.T) {
	for _, action := range []string{"", "shell", "Zeit", "zeit "} {
		var out, errOut bytes.Buffer
		if code := answer(action, "1.0.0", fixed, &out, &errOut); code == 0 {
			t.Errorf("action %q = 0, want a failure", action)
		}
		if out.Len() != 0 {
			t.Errorf("action %q wrote %q to stdout; a refused action answers nothing", action, out.String())
		}
	}
}

// TestMainRefusesArguments: every call that is not "hold" and not the service
// call is a call Main does not understand, and it says so instead of
// answering.
func TestMainRefusesArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.0.0", []string{"zeit"}, &out, &errOut); code != 2 {
		t.Errorf("Main with an argument = %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", out.String())
	}
}
