package reason

import (
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestEverySentenceIsGermanAndBounded holds what the list is FOR: a sentence
// that goes into a log and onto a screen has to survive both grammars
// unchanged. It is the counterpart of appstore's
// TestTheReasonIsGermanAndBounded, and it checks the bound of the log
// (MaxReason) and the bound of a CLOSE (wire.MaxMessageText) over the same
// strings, so a sentence that fits one and not the other cannot slip through.
func TestEverySentenceIsGermanAndBounded(t *testing.T) {
	for _, r := range All() {
		s := string(r)
		if !wire.ValidText(s, MaxReason) {
			t.Errorf("%q does not survive the grammar of a log detail (%d bytes)", s, len(s))
		}
		if !wire.ValidText(s, wire.MaxMessageText) {
			t.Errorf("%q does not survive the grammar of a CLOSE message", s)
		}
		// A line break would forge a second entry in the log, and a tab or a
		// control character would move the line of a reader. wire.ValidText
		// refuses them already; this says the sentence out loud, because it is
		// the whole argument for the list being closed.
		if strings.ContainsAny(s, "\n\r\t") {
			t.Errorf("%q carries a break", s)
		}
		if !strings.HasSuffix(s, ".") {
			t.Errorf("%q is not a sentence", s)
		}
	}
}

// TestTheListIsClosed holds that the list is a list: no duplicate, nothing
// empty, and nothing outside it is Known. A duplicate would make two
// decisions read as one in the log.
func TestTheListIsClosed(t *testing.T) {
	seen := map[Reason]bool{}
	for _, r := range All() {
		if r == None {
			t.Error("None is in the list")
		}
		if seen[r] {
			t.Errorf("%q is in the list twice", r)
		}
		seen[r] = true
	}
	if len(All()) != len(all) {
		t.Fatal("All does not answer the whole list")
	}
	for _, s := range []Reason{"", "Bitte es noch einmal versuchen.", "E_INTERNAL",
		Reason(string(NotRoot) + " "), Reason(strings.ToLower(string(DB)))} {
		if Known(s) {
			t.Errorf("%q counts as one of the list", s)
		}
	}
}

// TestAllHandsOutACopy holds that no caller can bend the list a case measures.
func TestAllHandsOutACopy(t *testing.T) {
	got := All()
	got[0] = "Alles ist gut."
	if All()[0] == "Alles ist gut." {
		t.Fatal("All hands out the list itself")
	}
}

// TestOnlyTheInstallationQuestionStaysOffTheScreen is the boundary between the
// two readers, written out: every reason may be read by an administrator in
// the log, and exactly one may not go to the operator of a session, because it
// would answer a question package rights refuses to answer.
func TestOnlyTheInstallationQuestionStaysOffTheScreen(t *testing.T) {
	if Screen(AppUnknown) {
		t.Error("the screen is told whether an app is installed")
	}
	for _, r := range All() {
		if r == AppUnknown {
			continue
		}
		if !Screen(r) {
			t.Errorf("%q reaches no operator, and nothing says why", r)
		}
	}
	// None is no sentence, so it is nothing to show either.
	if Screen(None) {
		t.Error("the absence of a reason went onto a screen")
	}
	// And a text nobody wrote here never goes out, which is the point of the
	// list: whoever wrote it is unknown.
	if Screen("Bitte den Admin fragen: http://example.invalid") {
		t.Error("a stranger text went onto a screen")
	}
}

// TestForTaskNamesTheCheckThatSaidNo is the case of 19.10.2026. A user with no
// root grant on that controller was turned away, and the whole of what anybody
// could read about it afterwards was a code. The honest sentence was in
// task.ErrForbidden the whole time.
func TestForTaskNamesTheCheckThatSaidNo(t *testing.T) {
	for _, c := range []struct {
		err  error
		want Reason
	}{
		{task.ErrForbidden, NotRoot},
		{task.ErrNoTask, NoTask},
		{task.ErrConfirm, NoConfirm},
		{task.ErrTask, BadTask},
		{task.ErrExpired, BadTask},
		{nil, None},
	} {
		if got := ForTask(c.err); got != c.want {
			t.Errorf("ForTask(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// TestTheBackstopNamesTheLayerAndNotTheCause holds that the fallback does not
// pretend to be a reason: it says that the reason is missing, so a gap in the
// log reads as a gap.
func TestTheBackstopNamesTheLayerAndNotTheCause(t *testing.T) {
	if ForCode("") != None {
		t.Error("a channel that ran got a reason")
	}
	if ForCode(wire.CodeUnreachable) != NodeAway || ForCode(wire.CodeTimeout) != NodeAway {
		t.Error("a far side that is away is not named as such")
	}
	for _, c := range []wire.Code{wire.CodeInternal, wire.CodeForbidden, wire.CodeVerify,
		wire.CodeProtocol, wire.CodeConfirm, wire.CodeNotFound, wire.CodeLimit} {
		if got := ForCode(c); got != InsideAPI {
			t.Errorf("ForCode(%s) = %q", c, got)
		}
	}
	// Every answer of ForCode is one of the list, or nothing travels.
	for _, c := range []wire.Code{"", wire.CodeInternal, wire.CodeUnreachable, wire.Code("E_NEW")} {
		if r := ForCode(c); r != None && !Known(r) {
			t.Errorf("ForCode(%s) answered %q, which is no sentence of the list", c, r)
		}
	}
}
