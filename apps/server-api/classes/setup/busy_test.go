package setup

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// logging catches the log of the function under test: with E_BUSY still in
// frozen shared/ (P9), the log line and the header are the only two places
// busy and away are apart.
func logging(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// busyRead is a store whose first read gives back what a store call gives back
// when a deadline of dbwait ran out: the cause, not the bare
// context.DeadlineExceeded (sqlstore.cause, point 49).
type busyRead struct {
	dbapi.Store
}

func (busyRead) BlobFrom(context.Context, string, uint64) ([]dbapi.Blob, error) {
	return nil, dbwait.ErrBusy
}

func (busyRead) BlobGet(context.Context, string, string) (dbapi.Blob, error) {
	return dbapi.Blob{}, dbwait.ErrBusy
}

// TestABusyDatabaseIsNotABrokenInstallation is the decision of this spot,
// measured instead of argued.
//
// fault turns EVERY E_UNREACHABLE of this path into E_INTERNAL, by decision
// and with a reason: the two cases it knew by name (a configured key that will
// not parse, a damaged row of the counting) reach it as E_UNREACHABLE because
// httpapi.Fault does not read them, and E_INTERNAL is the right word for both.
// Its own comment names the price and the way out -- "whoever one day wants a
// legitimate 'come back later' here has to take the word back off this rule
// first, and with the two cases gone there is no finer place left to hang it
// on."
//
// Since point 49 there IS a finer place: dbwait.Busy asks the error and not
// the path. A busy database is the one case on this path that passes by itself
// in a moment, and 500 tells an admin his installation is broken while his
// second program writes -- the same shape of wrong answer point 50 measured
// for the login.
//
// The deadline is NOT changed with it, and that is the other half of the
// decision: this handler is a path an admin waits on in a browser, so the user
// deadline of five seconds is its own. The ninety seconds of dbwait.Start
// belong to the path that brings the program up, and nothing of the start
// comes through here -- Build calls EnsureCode (server.go:181), which reads a
// file and touches no store; every store call of this package is inside Serve,
// behind an HTTP request.
func TestABusyDatabaseIsNotABrokenInstallation(t *testing.T) {
	if got := fault(dbwait.ErrBusy, httpapi.FromRows); got != wire.CodeUnreachable {
		t.Fatalf("a busy database is answered %q, so an admin hears his installation is "+
			"broken while a second program writes", got)
	}
	// The intermediate check, so this does not pass because the rule is gone:
	// everything else of this path is still E_INTERNAL, including a database
	// that is genuinely away.
	away := errTestAway{}
	if got := fault(away, httpapi.FromRows); got != wire.CodeInternal {
		t.Fatalf("a database that is away is answered %q, want %q", got, wire.CodeInternal)
	}
	if got := fault(errKey, httpapi.FromRows); got != wire.CodeInternal {
		t.Fatalf("a key of the wrong size is answered %q, want %q", got, wire.CodeInternal)
	}
}

type errTestAway struct{}

func (errTestAway) Error() string { return "dial tcp 10.0.0.9:3306: connect: connection refused" }

// TestTheSetupAnswersBusyWithRetryAfter is the same decision at the answer the
// admin's browser really gets: 503 with the header that says when to come
// back, and a log line that names the reason. Before: 500, no header, and a
// log that said nothing about why.
func TestTheSetupAnswersBusyWithRetryAfter(t *testing.T) {
	h := newHandler(t)
	buf := logging(t)
	h.S.DB = busyRead{Store: h.S.DB}

	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))

	if got := w.Code; got != session.HTTPStatus(wire.CodeUnreachable) {
		t.Fatalf("status = %d, want %d", got, session.HTTPStatus(wire.CodeUnreachable))
	}
	c, err := wire.ParseClose(w.Body.Bytes())
	if err != nil || c.Code != wire.CodeUnreachable {
		t.Fatalf("body %s: %v", w.Body, err)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want \"5\"", got)
	}
	if got := buf.String(); !strings.Contains(got, "db-busy") {
		t.Fatalf("the log names no reason: %q", got)
	}
}

// TestTheSetupPathKeepsTheUserDeadline holds the half of the decision that
// does NOT change, so a later reading of "the setup is the start" cannot move
// it quietly: the route of this handler carries dbwait.UserWait and not
// dbwait.StartWait.
//
// The figure is held and not just the fact: a deadline of ninety seconds here
// would leave an admin at a white page for a minute and a half, and the
// measurement says the waiting buys nothing -- four blocked writers already
// cost the first read 9,513 s (point 48).
func TestTheSetupPathKeepsTheUserDeadline(t *testing.T) {
	var left time.Duration
	var had bool
	h := newHandler(t)
	seen := func(w http.ResponseWriter, r *http.Request) {
		d, ok := r.Context().Deadline()
		had = ok
		if ok {
			left = time.Until(d)
		}
		h.Serve(w, r)
	}
	mux := httpapi.Routes{Setup: seen}.Mux()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))

	// The intermediate check: the route really reached the handler, so a
	// missing deadline below is the route's and not a path that never ran.
	if w.Code != http.StatusOK {
		t.Fatalf("the route did not reach the handler, status %d, body %s", w.Code, w.Body)
	}
	if !had {
		t.Fatal("the setup route hands its handler no deadline at all")
	}
	if left > dbwait.UserWait || left < dbwait.UserWait/2 {
		t.Fatalf("%v left of %v -- this is not the user deadline", left, dbwait.UserWait)
	}
	if dbwait.UserWait >= dbwait.StartWait {
		t.Fatal("the two deadlines are no longer apart, so this case measures nothing")
	}
}
