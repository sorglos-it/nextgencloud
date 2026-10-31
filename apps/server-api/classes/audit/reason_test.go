package audit

import (
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestEveryReasonSurvivesAsADetail is the cross-check of the two bounds. The
// list of package reason says its sentences fit a detail of this log
// (reason.MaxReason), and that number is not allowed to prove itself: here
// every sentence goes through the real entry point and the real grammar, and
// what is read back has to be the sentence, not the placeholder.
//
// Without this case the two bounds could drift apart with a single edit, and
// the loss would be invisible: an entry whose detail reads "-" still looks
// like a line of a log.
func TestEveryReasonSurvivesAsADetail(t *testing.T) {
	for _, r := range reason.All() {
		lg := newLog()
		if err := lg.Channel(ctx, user, "org.sorglos.terminal", "shell", "services-1",
			wire.CodeForbidden, r); err != nil {
			t.Fatalf("%q: %v", r, err)
		}
		e := only(t, lg)
		if e.Detail != string(r) {
			t.Fatalf("detail of %q reads %q", r, e.Detail)
		}
		if e.Kind != core.LogChannel || e.Result != string(wire.CodeForbidden) {
			t.Fatalf("the entry of %q is not a refused channel: %+v", r, e)
		}
	}
}

// TestAChannelThatRanSaysNothing holds the other end: a reason is said about a
// refusal, and a channel that opened carries no detail at all. A sentence
// there would be a reason for something that did not happen.
func TestAChannelThatRanSaysNothing(t *testing.T) {
	lg := newLog()
	if err := lg.Channel(ctx, user, "org.sorglos.terminal", "shell", "services-1", "",
		reason.None); err != nil {
		t.Fatalf("channel: %v", err)
	}
	e := only(t, lg)
	if e.Detail != "" || e.Result != "ok" {
		t.Fatalf("a channel that ran carries %+v", e)
	}
}

// TestASentenceOutsideTheListIsDroppedAndTheEntryStays is the rule of this
// package applied to the new field: the one field a caller could fill freely
// is held to the closed list, and a value outside it costs the field and never
// the entry. Whoever looks for a hole does not need the field to be clean,
// only the event to be missing.
func TestASentenceOutsideTheListIsDroppedAndTheEntryStays(t *testing.T) {
	for _, bad := range []reason.Reason{
		"Fehler: /var/lib/ngc/apps/org.sorglos.terminal: permission denied",
		"ok\nKanal ngc.store activate ngc-2 ok",
		reason.Reason("Passwort: hunter2"),
	} {
		lg := newLog()
		err := lg.Channel(ctx, user, "org.sorglos.terminal", "shell", "services-1",
			wire.CodeInternal, bad)
		if !errors.Is(err, ErrDropped) {
			t.Fatalf("%q was not reported as dropped: %v", bad, err)
		}
		e := only(t, lg)
		if e.Detail != Dropped {
			t.Fatalf("%q reached the log as %q", bad, e.Detail)
		}
		if e.Result != string(wire.CodeInternal) {
			t.Fatalf("the entry itself was lost: %+v", e)
		}
	}
}
