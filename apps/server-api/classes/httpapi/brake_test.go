package httpapi

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// reset puts the two counters back where a fresh process has them. The
// counters are of the package and every case here changes them, so a case
// that did not reset would read the one before it -- the trap messen.md names
// for counters and one-shot values.
func reset(t *testing.T) {
	t.Helper()
	for _, c := range []*counter{&busy, &away} {
		c.seen.Store(0)
		c.said.Store(0)
		c.last.Store(0)
	}
	reportedBusy.Store(0)
	reportedAway.Store(0)
}

// TestTheLineHasABrakeAndTheCountHasNone is the assurance. An unauthenticated
// caller must not be able to write a line of the log per request -- that is
// what login/start says about its own warning one file over -- and the brake
// must not swallow anything: every rejection the brake keeps quiet has to
// stand in the count, and the one line that is written has to say how many
// came behind it.
func TestTheLineHasABrakeAndTheCountHasNone(t *testing.T) {
	reset(t)
	buf := logging(t)
	const n = 500
	for i := 0; i < n; i++ {
		w := httptest.NewRecorder()
		if code := FailCoded(w, wire.CodeUnreachable, dbwait.ErrBusy); code != wire.CodeUnreachable {
			// The intermediate check: every call really walks to the busy arm.
			// A call that came back another code would be measuring an earlier
			// wall and would write no line at all.
			t.Fatalf("call %d came back %q, so the busy arm was never reached", i, code)
		}
		if w.Code != 503 {
			t.Fatalf("call %d answered %d, want 503", i, w.Code)
		}
		if w.Header().Get("Retry-After") == "" {
			t.Fatalf("call %d lost its Retry-After", i)
		}
	}
	if got := Busy(); got != n {
		t.Errorf("Busy() = %d, want %d: the brake must not swallow a rejection", got, n)
	}
	lines := strings.Count(buf.String(), "reason=db-busy")
	if lines != 1 {
		t.Errorf("%d lines for %d rejections, want 1: an unauthenticated caller writes the log", lines, n)
	}
	if !strings.Contains(buf.String(), "since=") || !strings.Contains(buf.String(), "count=") {
		t.Errorf("the one line names neither count nor since, so the %d behind it are lost: %q", n, buf.String())
	}
}

// TestTheTwoKindsCountApart holds the two counters apart. Busy and away are
// the same wire code and the same 503 (the frozen list, point 49), so the
// numbers are the only place an admin can tell how much of an overload was
// which.
func TestTheTwoKindsCountApart(t *testing.T) {
	reset(t)
	_ = logging(t)
	for i := 0; i < 3; i++ {
		FailCoded(httptest.NewRecorder(), wire.CodeUnreachable, dbwait.ErrBusy)
	}
	for i := 0; i < 7; i++ {
		FailCoded(httptest.NewRecorder(), wire.CodeUnreachable, errAway)
	}
	if Busy() != 3 || Unreachable() != 7 {
		t.Errorf("Busy()=%d Unreachable()=%d, want 3 and 7", Busy(), Unreachable())
	}
}

// TestReportIsSilentUntilSomethingGrows is the second way of showing: the
// total has to reach the log of its own accord out of the housekeeping round,
// and a line every minute saying "none" would teach people to stop reading.
func TestReportIsSilentUntilSomethingGrows(t *testing.T) {
	reset(t)
	buf := logging(t)
	Report()
	if buf.Len() != 0 {
		t.Errorf("Report wrote %q with nothing to report", buf.String())
	}
	FailCoded(httptest.NewRecorder(), wire.CodeUnreachable, dbwait.ErrBusy)
	buf.Reset()
	Report()
	if !strings.Contains(buf.String(), "busy=1") {
		t.Errorf("Report said %q, want the total busy=1", buf.String())
	}
	buf.Reset()
	Report()
	if buf.Len() != 0 {
		t.Errorf("Report repeated itself with nothing new: %q", buf.String())
	}
}

// TestNoRejectionIsLostUnderManyGoroutines is the lane the real load takes:
// the rejections arrive at once, not one after another, and the count is the
// thing that may not be short by one.
func TestNoRejectionIsLostUnderManyGoroutines(t *testing.T) {
	reset(t)
	_ = logging(t)
	const workers, each = 16, 50
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				FailCoded(httptest.NewRecorder(), wire.CodeUnreachable, dbwait.ErrBusy)
			}
		}()
	}
	wg.Wait()
	if got := Busy(); got != workers*each {
		t.Errorf("Busy() = %d, want %d", got, workers*each)
	}
}

// TestTheSecondLineCarriesWhatTheFirstOneSwallowed is what makes the brake
// something other than a swallowing. The first line of a kind is written at
// once and stands for itself; from the second on, the line has to carry how
// many came in the window that just ended, or the brake would throw away
// exactly the number an admin needs -- and 480 rejections in a second would
// read as one.
//
// The window is walked back by hand instead of waited out: warnEvery is 60
// seconds, and a case that slept them would be a minute of nothing. The field
// is of this package and every case here resets it anyway.
func TestTheSecondLineCarriesWhatTheFirstOneSwallowed(t *testing.T) {
	reset(t)
	buf := logging(t)
	for i := 0; i < 40; i++ {
		FailCoded(httptest.NewRecorder(), wire.CodeUnreachable, dbwait.ErrBusy)
	}
	// The intermediate check: the first window really did brake, so what
	// follows measures the second line and not a first one that never came.
	if n := strings.Count(buf.String(), "reason=db-busy"); n != 1 {
		t.Fatalf("the first window wrote %d lines, want 1", n)
	}
	busy.last.Store(busy.last.Load() - warnEvery - 1)
	buf.Reset()
	FailCoded(httptest.NewRecorder(), wire.CodeUnreachable, dbwait.ErrBusy)
	if n := strings.Count(buf.String(), "reason=db-busy"); n != 1 {
		t.Fatalf("the second window wrote %d lines, want 1", n)
	}
	// 40 of the first window plus this one: the one that was said out loud
	// when the first window opened is already paid for, so 40 are left.
	if !strings.Contains(buf.String(), "since=40") {
		t.Errorf("the second line says %q, want since=40: the 39 the brake kept quiet are lost", buf.String())
	}
	if !strings.Contains(buf.String(), "count=41") {
		t.Errorf("the second line says %q, want count=41", buf.String())
	}
}
