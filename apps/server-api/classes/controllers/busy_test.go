package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// busyStore is a store whose calls behave as a store call behaves while a
// second program holds the SQLite file: it waits, and what it gives back is
// the CAUSE of the context it was handed (sqlstore.cause, point 49).
//
// It writes down every call, because that list is the assurance against an
// oracle: a pairing path must make the same calls for a code that exists and
// one that does not, and that is held with slices.Equal.
type busyStore struct {
	dbapi.Store
	mu    sync.Mutex
	calls []string
}

func (b *busyStore) note(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, name)
}

func (b *busyStore) list() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

func (b *busyStore) wait(ctx context.Context, name string) error {
	b.note(name)
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	// The cap belongs to the case: without a deadline on the handler the wait
	// never ends, and a case that HANGS is no assurance.
	case <-time.After(4 * dbwait.UserWait):
		return errTestNoDeadline
	}
}

func (b *busyStore) OnceUse(ctx context.Context, kind string, value []byte, now int64) (string, error) {
	return "", b.wait(ctx, "OnceUse")
}

func (b *busyStore) PendingPut(ctx context.Context, p dbapi.Pending) error {
	return b.wait(ctx, "PendingPut")
}

var errTestNoDeadline = errNoDeadline{}

type errNoDeadline struct{}

func (errNoDeadline) Error() string { return "the handler set no deadline at all" }

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

// TestPairBoundsHowLongTheDatabaseMayTake is the assurance of the deadline:
// tunnel.PathPair does not run over httpapi.Routes.Mux, so the user deadline
// point 49 gave the five session routes never reached it. Without one a store
// call of this handler waits busy_timeout times maxTries -- 80,6 s measured
// (point 48).
func TestPairBoundsHowLongTheDatabaseMayTake(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(20)
	body := pairBody(t, code, ctrl.Public, code.Secret)
	srv.S.DB = &busyStore{Store: srv.S.DB}

	w := httptest.NewRecorder()
	start := time.Now()
	srv.Pair(w, post(pairPath(code), body))
	took := time.Since(start)

	if took > 2*dbwait.UserWait {
		t.Fatalf("the caller waited %v for an answer; the deadline is %v", took, dbwait.UserWait)
	}
	if took < dbwait.UserWait/2 {
		t.Fatalf("the answer came after %v, so no store call was waited on -- this case "+
			"measures an earlier wall, not the deadline", took)
	}
}

// TestPairAnswersBusyWithRetryAfter is the second half of the same spot. The
// code stays E_UNREACHABLE -- E_BUSY is in frozen shared/wire/wire.go:450-462,
// P9 -- so the header and the log line are the whole of what tells the two
// apart.
//
// What this changes is bigger than the one line the report named: the FIRST
// store call of this handler is the one-time value, and its error arm answered
// E_VERIFY for everything. Measured before the change: a busy database told a
// controller that is pairing correctly that its code was wrong (403), and an
// admin would have made a new code for nothing.
func TestPairAnswersBusyWithRetryAfter(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(20)
	body := pairBody(t, code, ctrl.Public, code.Secret)
	buf := logging(t)
	srv.S.DB = &busyStore{Store: srv.S.DB}

	w := httptest.NewRecorder()
	srv.Pair(w, post(pairPath(code), body))

	if got := w.Code; got != session.HTTPStatus(wire.CodeUnreachable) {
		t.Fatalf("status = %d, want %d", got, session.HTTPStatus(wire.CodeUnreachable))
	}
	if got := codeOfBody(t, w); got != wire.CodeUnreachable {
		t.Fatalf("code = %q", got)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want \"5\"", got)
	}
	if got := buf.String(); !strings.Contains(got, "db-busy") {
		t.Fatalf("the log names no reason: %q", got)
	}
	// Nothing of the request is in the log: a pairing request carries a name
	// and a key, and a server's log goes into reports.
	if got := buf.String(); strings.Contains(got, "services-1") {
		t.Fatalf("the log carries something of the request: %q", got)
	}
}

// TestPairSaysNothingAboutWhetherACodeExists is the security assurance, held
// with numbers and not with the argument that busy is a state of the
// installation: the same status, the same headers with the same values, the
// same body and the same list of store calls in the same order.
//
// The call list is the one of the four that catches a difference the answer
// hides -- sqlstore.OnceUse does its lookup INSIDE one write transaction, so
// the lock is met before any row is read, and this holds that it stays so.
func TestPairSaysNothingAboutWhetherACodeExists(t *testing.T) {
	type answer struct {
		status int
		header http.Header
		body   string
		calls  []string
	}
	ask := func(t *testing.T, known bool) answer {
		t.Helper()
		srv, _ := newServer(t)
		code, _, err := srv.NewPairCode(ctx)
		if err != nil {
			t.Fatalf("code: %v", err)
		}
		ctrl, _ := trusttest.NodeKeys(20)
		body := pairBody(t, code, ctrl.Public, code.Secret)
		path := pairPath(code)
		if !known {
			// A query of the right shape naming a hash that was never a
			// secret of this installation.
			other := sha256.Sum256([]byte("a secret nobody ever made"))
			path = tunnel.PathPair + "?secret=" + enc.B64.EncodeToString(other[:])
		}
		b := &busyStore{Store: srv.S.DB}
		srv.S.DB = b
		w := httptest.NewRecorder()
		srv.Pair(w, post(path, body))
		return answer{w.Code, w.Header().Clone(), w.Body.String(), b.list()}
	}
	known, unknown := ask(t, true), ask(t, false)

	// The intermediate check: both really reached a store call, so two equal
	// answers cannot be equal for a reason that has nothing to do with this.
	if len(known.calls) == 0 || len(unknown.calls) == 0 {
		t.Fatalf("no store call was made: known %v, unknown %v", known.calls, unknown.calls)
	}
	if known.status != unknown.status {
		t.Fatalf("status %d for a code that exists, %d for one that does not",
			known.status, unknown.status)
	}
	if !maps.EqualFunc(known.header, unknown.header, slices.Equal) {
		t.Fatalf("the headers differ: %v against %v", known.header, unknown.header)
	}
	if known.body != unknown.body {
		t.Fatalf("the bodies differ: %q against %q", known.body, unknown.body)
	}
	if !slices.Equal(known.calls, unknown.calls) {
		t.Fatalf("the store calls differ: %v against %v", known.calls, unknown.calls)
	}
}

// codeOfBody reads the wire code out of an error answer.
func codeOfBody(t *testing.T, rec *httptest.ResponseRecorder) wire.Code {
	t.Helper()
	var e struct {
		Code wire.Code `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("answer %q is not an error of the contract: %v", rec.Body.String(), err)
	}
	return e.Code
}
