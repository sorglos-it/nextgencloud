package join

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
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// busyStore is a store whose write calls behave as a store call behaves while
// a second program holds the SQLite file: it waits, and what it gives back
// when it stops waiting is the CAUSE of the context it was handed, not the
// bare context.DeadlineExceeded -- the shape apps/server-db really hands up
// (sqlstore.cause, point 49).
//
// It also WRITES DOWN every call it was asked for, because that list is the
// assurance against an oracle: a path that must not say whether a code exists
// has to make the same calls for a code that does and one that does not, and
// that is held with slices.Equal and not with an argument.
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

// wait is what every blocked call of this store does.
func (b *busyStore) wait(ctx context.Context, name string) error {
	b.note(name)
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	// The cap belongs to the case and not to the thing under test: without a
	// deadline on the handler the wait above never ends, and a case that
	// HANGS is no assurance.
	case <-time.After(4 * dbwait.UserWait):
		return errTestNoDeadline
	}
}

func (b *busyStore) OnceUse(ctx context.Context, kind string, value []byte, now int64) (string, error) {
	return "", b.wait(ctx, "OnceUse")
}

func (b *busyStore) BlobPut(ctx context.Context, blob dbapi.Blob) error {
	return b.wait(ctx, "BlobPut")
}

// errTestNoDeadline is what a blocked call gives back when the case's own cap
// ran out before any deadline of the code did. It is never a pass.
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

// withBusyStore puts a store that never comes free under j and returns it.
func withBusyStore(j *Join) *busyStore {
	b := &busyStore{Store: j.S.DB}
	j.S.DB = b
	return b
}

// TestAcceptBoundsHowLongTheDatabaseMayTake is the assurance of the deadline:
// PathJoin does not run over httpapi.Routes.Mux, so the user deadline point 49
// gave the five session routes never reached it. Without one a store call of
// this handler waits busy_timeout times maxTries -- 80,6 s measured (point 48)
// -- and then answers the same word as a database that is away.
func TestAcceptBoundsHowLongTheDatabaseMayTake(t *testing.T) {
	j, fx := newJoin(t)
	secret, hash := newJoinSecret(t, j)
	withBusyStore(j)

	body, err := json.Marshal(fx.request(secret))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	w := httptest.NewRecorder()
	start := time.Now()
	j.Accept(w, post(hash, body))
	took := time.Since(start)

	if took > 2*dbwait.UserWait {
		t.Fatalf("the caller waited %v for an answer; the deadline is %v", took, dbwait.UserWait)
	}
	if took < dbwait.UserWait/2 {
		t.Fatalf("the answer came after %v, so no store call was waited on -- this case "+
			"measures an earlier wall, not the deadline", took)
	}
}

// TestAcceptAnswersBusyWithRetryAfter is the second half of the same spot: the
// handler answered with a bare httpapi.Fail, so a joining server heard 503
// with no header and the log named no reason. The code stays E_UNREACHABLE --
// E_BUSY is in frozen shared/wire/wire.go:450-462, P9 -- so the header and the
// log line are the whole of what can tell the two apart.
func TestAcceptAnswersBusyWithRetryAfter(t *testing.T) {
	j, fx := newJoin(t)
	secret, hash := newJoinSecret(t, j)
	buf := logging(t)
	withBusyStore(j)

	body, err := json.Marshal(fx.request(secret))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	w := httptest.NewRecorder()
	j.Accept(w, post(hash, body))

	if got := w.Code; got != session.HTTPStatus(wire.CodeUnreachable) {
		t.Fatalf("status = %d, want %d", got, session.HTTPStatus(wire.CodeUnreachable))
	}
	if got := codeOfBody(t, w); got != wire.CodeUnreachable {
		t.Fatalf("code = %q", got)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want \"5\" -- without it this is not tellable "+
			"from a database that is away", got)
	}
	if got := buf.String(); !strings.Contains(got, "db-busy") {
		t.Fatalf("the log names no reason: %q", got)
	}
	// And nothing of the request is in the log: a join request carries a node
	// name, a URL and three keys, and a server's log goes into reports.
	if got := buf.String(); strings.Contains(got, "api-2") || strings.Contains(got, "wss://") {
		t.Fatalf("the log carries something of the request: %q", got)
	}
}

// TestAcceptSaysNothingAboutWhetherACodeExists is the security assurance, and
// it is held with numbers rather than with the argument that busy is a state
// of the installation. A join code is a secret of 32 bytes; a path that
// answered a hash somebody once saw differently from one nobody ever made
// would sort the codes of the installation from the rest.
//
// Measured for a busy database, where the new answer is: the same status, the
// same headers with the same values, the same body, and the same list of store
// calls in the same order. The store call list is the one of the four that
// catches a difference the answer hides -- sqlstore.OnceUse does its lookup
// INSIDE one write transaction, so the lock is met before any row is read, and
// this is what holds that it stays that way.
func TestAcceptSaysNothingAboutWhetherACodeExists(t *testing.T) {
	type answer struct {
		status int
		header http.Header
		body   string
		calls  []string
	}
	ask := func(t *testing.T, known bool) answer {
		t.Helper()
		j, fx := newJoin(t)
		secret, hash := newJoinSecret(t, j)
		if !known {
			// A hash of the right shape that was never a secret of this
			// installation: the same 32 bytes, nothing behind them.
			other := sha256.Sum256([]byte("a secret nobody ever made"))
			hash = enc.B64.EncodeToString(other[:])
		}
		b := withBusyStore(j)
		body, err := json.Marshal(fx.request(secret))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		w := httptest.NewRecorder()
		j.Accept(w, post(hash, body))
		return answer{w.Code, w.Header().Clone(), w.Body.String(), b.list()}
	}
	known, unknown := ask(t, true), ask(t, false)

	// The intermediate check: both really reached a store call. Two answers
	// that never touched the database would be equal for a reason that has
	// nothing to do with this assurance.
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

// TestKeysBoundsTheTrustLookup holds the third line of this package that runs
// without a deadline (keys.go). Only the store call is bounded and not the
// handler: what follows it is a Noise handshake and a websocket write, and a
// deadline of five seconds over those would cut a link that is doing nothing
// wrong.
// The store is reached only while nothing is cached, which is the state right
// after a start -- State.Trust answers out of s.cur once a version stands
// (state/trust.go:63). So the case builds a state whose cache is empty, which
// is also the only state in which this line can wait on anything at all.
func TestKeysBoundsTheTrustLookup(t *testing.T) {
	j, _ := newJoin(t)
	b := &blockingTrust{Store: j.S.DB}
	cold := state.New(j.S.Cfg, b, j.S.Box, j.S.Pin)
	cold.Now = j.S.Now
	j = New(cold)

	w := httptest.NewRecorder()
	start := time.Now()
	j.Keys(w, httptest.NewRequest(http.MethodGet, PathKeys+"?key=x", nil))
	took := time.Since(start)

	if took > 2*dbwait.UserWait {
		t.Fatalf("the trust lookup waited %v, with no deadline over it", took)
	}
	if took < dbwait.UserWait/2 {
		t.Fatalf("the answer came after %v, so the blocked call was not reached", took)
	}
	// The answer of this path does not change: it says nothing about why, by
	// decision of keys.go, and a busy database is not a reason to start
	// saying something.
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// blockingTrust blocks the one read the keys path makes before the handshake.
type blockingTrust struct {
	dbapi.Store
	seen bool
}

func (b *blockingTrust) BlobFrom(ctx context.Context, kind string, from uint64) ([]dbapi.Blob, error) {
	b.seen = true
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-time.After(4 * dbwait.UserWait):
		return nil, errTestNoDeadline
	}
}

func (b *blockingTrust) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	b.seen = true
	select {
	case <-ctx.Done():
		return dbapi.Blob{}, context.Cause(ctx)
	case <-time.After(4 * dbwait.UserWait):
		return dbapi.Blob{}, errTestNoDeadline
	}
}

// newJoinSecret makes a join code through the real issuing side and gives back
// the secret and the query value that names it.
func newJoinSecret(t *testing.T, j *Join) (secret []byte, hash string) {
	t.Helper()
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("join code: %v", err)
	}
	h := sha256.Sum256(code.Secret)
	return code.Secret, enc.B64.EncodeToString(h[:])
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
