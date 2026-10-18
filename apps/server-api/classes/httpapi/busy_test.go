package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// errAway is a database that is away: an error of no package Fault reads, the
// shape the last arm of Fault was written for.
var errAway = errors.New("dial tcp 10.0.0.9:3306: connect: connection refused")

// logging catches what the function under test writes, so the log line can be
// measured as what it is: the one place an admin can tell the two cases apart
// as long as the code is the same for both.
func logging(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// TestFailFaultTellsBusyFromAway is the assurance of the first half. Both
// cases are E_UNREACHABLE and both are 503 -- that much is the frozen code
// list and stays -- and everything else about them has to differ: the busy
// one says in a header when to come back, the one that is away says nothing
// of the kind, and the two log lines do not read alike.
func TestFailFaultTellsBusyFromAway(t *testing.T) {
	for _, c := range []struct {
		name  string
		err   error
		retry string
		word  string
		not   string
	}{
		{"busy", dbwait.ErrBusy, "5", "did not come free", "could not be reached"},
		{"away", errAway, "", "could not be reached", "did not come free"},
	} {
		t.Run(c.name, func(t *testing.T) {
			reset(t)
			buf := logging(t)
			w := httptest.NewRecorder()
			code := FailFault(w, c.err, FromRows)
			// The intermediate check: both really walk to the last arm of
			// Fault. A case that came back E_INTERNAL or E_NOTFOUND would be
			// measuring an earlier wall, and the header below would be
			// missing for a reason that has nothing to do with busy.
			if code != wire.CodeUnreachable {
				t.Fatalf("this error does not reach the arm under test, code = %q", code)
			}
			if got := w.Code; got != session.HTTPStatus(wire.CodeUnreachable) {
				t.Fatalf("status = %d", got)
			}
			if got := w.Header().Get("Retry-After"); got != c.retry {
				t.Fatalf("Retry-After = %q, want %q", got, c.retry)
			}
			if got := buf.String(); !strings.Contains(got, c.word) || strings.Contains(got, c.not) {
				t.Fatalf("the log line does not tell this case apart: %q", got)
			}
			// The reason is in the log; the request is not. A log line of a
			// server is passed on, and this path's errors can carry a path or
			// a configuration.
			if got := buf.String(); !strings.Contains(got, "reason=") {
				t.Fatalf("no reason in the log line: %q", got)
			}
		})
	}
}

// TestFailFaultLeavesEveryOtherCodeAlone holds the other side: FailFault adds
// a Retry-After to one case and to nothing else. Without this the function
// could answer every error of the state as "come back in five seconds".
func TestFailFaultLeavesEveryOtherCodeAlone(t *testing.T) {
	for _, err := range []error{state.ErrBadVersion, state.ErrNoTrust, dbapi.ErrNotFound} {
		w := httptest.NewRecorder()
		code := FailFault(w, err, FromRows)
		if code == wire.CodeUnreachable {
			t.Fatalf("%v came back E_UNREACHABLE, so it measures nothing here", err)
		}
		if got := w.Header().Get("Retry-After"); got != "" {
			t.Fatalf("%v got Retry-After %q", err, got)
		}
		// And the answer is written, with the status of the code it names.
		// The first draft of FailFault returned the code for these without
		// writing anything, and the handler's caller then saw the 200 of an
		// untouched recorder: three cases of package login caught it.
		if w.Code != session.HTTPStatus(code) {
			t.Fatalf("%v: status %d for code %q", err, w.Code, code)
		}
		if w.Body.Len() == 0 {
			t.Fatalf("%v: no body", err)
		}
	}
}

// TestAUserRouteBoundsHowLongTheDatabaseMayTake is the assurance of the
// second half: the five routes before the WebSocket hand their handler a
// context with a deadline, and the socket does not. Measured without one: a
// store call waits 10 s times 8 tries, 80,6 s (point 48).
func TestAUserRouteBoundsHowLongTheDatabaseMayTake(t *testing.T) {
	var left time.Duration
	var had bool
	seen := func(w http.ResponseWriter, r *http.Request) {
		d, ok := r.Context().Deadline()
		had = ok
		if ok {
			left = time.Until(d)
		}
		w.WriteHeader(http.StatusOK)
	}
	rt := Routes{Setup: seen, Login: seen, Finish: seen, Logout: seen, Trust: seen, Socket: seen}
	mux := rt.Mux()

	for _, c := range []struct {
		path, method string
		want         bool
	}{
		{session.PathSetup, http.MethodGet, true},
		{session.PathLoginStart, http.MethodPost, true},
		{session.PathLoginFinish, http.MethodPost, true},
		{session.PathLogout, http.MethodPost, true},
		{session.PathTrust, http.MethodGet, true},
		// The socket is held open for as long as the session lives.
		{session.PathSocket, http.MethodGet, false},
	} {
		t.Run(c.path, func(t *testing.T) {
			had, left = false, 0
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("the route did not reach the handler, status %d", w.Code)
			}
			if had != c.want {
				t.Fatalf("deadline = %v, want %v", had, c.want)
			}
			if !c.want {
				return
			}
			// The figure, not just the fact: a deadline of an hour would pass
			// a check that only asks whether there is one.
			if left > dbwait.UserWait || left < dbwait.UserWait/2 {
				t.Fatalf("%v left of %v", left, dbwait.UserWait)
			}
		})
	}
}

// TestAUserRouteEndsInBusyAndNotInSilence is the two halves together, at the
// shape the measurement found: a store that never answers. Before, the caller
// waited 80,6 s and then heard the same word as for a database that is gone.
func TestAUserRouteEndsInBusyAndNotInSilence(t *testing.T) {
	reset(t)
	buf := logging(t)
	// The handler is the store's caller: it waits on the context it was
	// given, exactly as a store call does while another program holds the
	// file, and then answers what came back.
	h := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		// The cap is the test's own and not the thing under test: without a
		// deadline on the route the wait above never ends, and a case that
		// HANGS is no assurance -- it has to fail and say what it waited for.
		case <-time.After(3 * dbwait.UserWait):
			t.Error("the request got no deadline at all, the store call would wait on")
		}
		// What apps/server-db hands back for a deadline of dbwait: the cause,
		// not the bare context.DeadlineExceeded (sqlstore.cause).
		FailFault(w, context.Cause(r.Context()), FromRows)
	}
	mux := Routes{Trust: h}.Mux()
	w := httptest.NewRecorder()
	start := time.Now()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, session.PathTrust, nil))
	took := time.Since(start)
	if took > 2*dbwait.UserWait {
		t.Fatalf("the caller waited %v", took)
	}
	if w.Code != session.HTTPStatus(wire.CodeUnreachable) {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Fatal("no Retry-After, so this is not tellable from a database that is away")
	}
	if got := buf.String(); !strings.Contains(got, "db-busy") {
		t.Fatalf("the log does not name the reason: %q", got)
	}
}
