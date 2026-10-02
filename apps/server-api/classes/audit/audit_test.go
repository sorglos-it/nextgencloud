package audit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

// user is an ID trust.ValidID accepts: 16 bytes as base64url.
const user = "ISEhISEhISEhISEhISEhIQ"

// signature is the base64url of 64 bytes, the length of an ed25519 signature.
var signature = strings.Repeat("A", 86)

func newLog() *Log {
	return &Log{DB: memstore.New(), Now: func() int64 { return 1790000000 }}
}

func TestAddSetsIDAndTime(t *testing.T) {
	lg := newLog()
	if err := lg.Login(ctx, user, "ok", ""); err != nil {
		t.Fatalf("login: %v", err)
	}
	rows, err := lg.Query(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v %d", err, len(rows))
	}
	e := rows[0]
	if !core.ValidLogID(e.ID) || e.Time != 1790000000 || e.Kind != core.LogLogin || e.Result != "ok" {
		t.Fatalf("entry %+v", e)
	}
}

// TestAddIgnoresTheCallersIDAndTime pins that no caller can place an entry
// under an ID or a time of its own, which is how a forged entry would hide
// between two real ones.
func TestAddIgnoresTheCallersIDAndTime(t *testing.T) {
	lg := newLog()
	const forged = "ffffffffffffffffffffffffffffffff"
	if err := lg.Add(ctx, core.LogEntry{Kind: core.LogTrust, User: user, ID: forged, Time: 1}); err != nil {
		t.Fatalf("add: %v", err)
	}
	rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10})
	if len(rows) != 1 || rows[0].ID == forged || rows[0].Time != 1790000000 {
		t.Fatalf("entry %+v", rows[0])
	}
}

func TestChannelKeepsTheCode(t *testing.T) {
	lg := newLog()
	if err := lg.Channel(ctx, user, "org.sorglos.terminal", "shell", "services-1", wire.CodeForbidden); err != nil {
		t.Fatalf("channel: %v", err)
	}
	rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10})
	if rows[0].Result != string(wire.CodeForbidden) || rows[0].Node != "services-1" {
		t.Fatalf("entry %+v", rows[0])
	}
}

func TestQueryPagesByTimeAndID(t *testing.T) {
	lg := newLog()
	at := int64(1790000000)
	lg.Now = func() int64 { return at }
	for i := 0; i < 5; i++ {
		if err := lg.Login(ctx, user, "ok", ""); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	first, err := lg.Query(ctx, core.LogQuery{Limit: 2})
	if err != nil || len(first) != 2 {
		t.Fatalf("first page: %v %d", err, len(first))
	}
	last := first[len(first)-1]
	second, err := lg.Query(ctx, core.LogQuery{Limit: 2, BeforeTime: last.Time, BeforeID: last.ID})
	if err != nil || len(second) != 2 {
		t.Fatalf("second page: %v %d", err, len(second))
	}
	// No entry appears twice, although all five share one second.
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID {
				t.Fatalf("entry %s on both pages", a.ID)
			}
		}
	}
}

// refused runs one entry and demands ErrEntry. It first runs good, which must
// be kept: without that step a refusal could come from the memstore check in
// memstore.go:345 instead of this package, and the case would be green without
// ever reaching the line it means to pin.
func refused(t *testing.T, what string, good, bad core.LogEntry) {
	t.Helper()
	lg := newLog()
	if err := lg.Add(ctx, good); err != nil {
		t.Fatalf("%s: the way to the check is blocked, good entry gives %v", what, err)
	}
	err := lg.Add(ctx, bad)
	if !errors.Is(err, ErrEntry) {
		t.Fatalf("%s: got %v, want ErrEntry", what, err)
	}
	// Nothing of the refused entry reached the database.
	rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10})
	if len(rows) != 1 {
		t.Fatalf("%s: %d rows kept, want 1", what, len(rows))
	}
}

// TestAddRefusesWhatDoesNotBelongInAnEntry is the heart of this package: an
// entry an attacker can fill freely is a tool against whoever reads the log,
// and a secret in it is a secret lost. Every case below is short text a
// careless caller could really hand over.
func TestAddRefusesWhatDoesNotBelongInAnEntry(t *testing.T) {
	ok := core.LogEntry{Kind: core.LogTrust, User: user, App: "ngc.store", Action: "activate",
		Node: "ctl-1", Result: "ok", Detail: "role admin"}
	with := func(f func(e *core.LogEntry)) core.LogEntry {
		e := ok
		f(&e)
		return e
	}
	cases := []struct {
		what string
		bad  core.LogEntry
	}{
		{"unknown kind", with(func(e *core.LogEntry) { e.Kind = "login-ok" })},
		{"line break in the detail", with(func(e *core.LogEntry) {
			e.Detail = "ok\n1790000000 admin login ok"
		})},
		{"tab in the detail", with(func(e *core.LogEntry) { e.Detail = "a\tb" })},
		{"escape in the detail", with(func(e *core.LogEntry) { e.Detail = "a\x1b[2Kb" })},
		{"detail over the limit", with(func(e *core.LogEntry) { e.Detail = strings.Repeat("x", maxDetail+1) })},
		{"user that is no ID", with(func(e *core.LogEntry) { e.User = "admin" })},
		{"app that is no app id", with(func(e *core.LogEntry) { e.App = "../../etc/passwd" })},
		{"action that is no name", with(func(e *core.LogEntry) { e.Action = "Activate App" })},
		{"node that is no name", with(func(e *core.LogEntry) { e.Node = "ctl 1" })},
		{"result that is neither ok nor a code", with(func(e *core.LogEntry) { e.Result = "E_MADE_UP" })},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) { refused(t, c.what, ok, c.bad) })
	}
}

// TestAddFillsAnEmptyResult: an entry without a result means success, so Add
// writes "ok" rather than refusing it. That is why no row ever holds an empty
// result although check demands one.
func TestAddFillsAnEmptyResult(t *testing.T) {
	lg := newLog()
	if err := lg.Add(ctx, core.LogEntry{Kind: core.LogTrust, User: user}); err != nil {
		t.Fatalf("add: %v", err)
	}
	rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10})
	if rows[0].Result != "ok" {
		t.Fatalf("entry %+v", rows[0])
	}
}

// TestTaskTakesOnlyASignature pins that the signature field cannot be used to
// smuggle something else in. A key is 32 bytes, a signature 64; a caller that
// hands over the wrong one is refused instead of writing it down.
func TestTaskTakesOnlyASignature(t *testing.T) {
	lg := newLog()
	if err := lg.Task(ctx, user, "ngc.store", "activate", "ctl-1", signature); err != nil {
		t.Fatalf("task: %v", err)
	}
	rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10})
	if len(rows) != 1 || rows[0].Detail != signature || rows[0].Kind != core.LogTask {
		t.Fatalf("entry %+v", rows[0])
	}
	for _, bad := range []string{"", strings.Repeat("A", 43), strings.Repeat("A", 85), "hunter2",
		strings.Repeat("A", 85) + "="} {
		if err := lg.Task(ctx, user, "ngc.store", "activate", "ctl-1", bad); !errors.Is(err, ErrEntry) {
			t.Fatalf("signature %q: got %v, want ErrEntry", bad, err)
		}
	}
}

// TestLoginTakesOnlyAKnownMethod: the detail of a login is the one field a
// login path could fill with input of the user. Only the method words are
// allowed, so neither a password, a one-time code nor a name can land there.
func TestLoginTakesOnlyAKnownMethod(t *testing.T) {
	lg := newLog()
	for _, good := range []string{"", "password", "passkey", "totp", "setup", "recovery"} {
		if err := lg.Login(ctx, user, "ok", good); err != nil {
			t.Fatalf("method %q: %v", good, err)
		}
	}
	for _, bad := range []string{"hunter2", "123456", "totp ", "TOTP", "password+totp", "user admin"} {
		if err := lg.Login(ctx, user, "ok", bad); !errors.Is(err, ErrEntry) {
			t.Fatalf("method %q: got %v, want ErrEntry", bad, err)
		}
	}
}

func TestLoginMarksTheFailedOne(t *testing.T) {
	lg := newLog()
	if err := lg.Login(ctx, user, string(wire.CodeLogin), "password"); err != nil {
		t.Fatalf("login: %v", err)
	}
	rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10})
	if rows[0].Kind != core.LogLoginFailed || rows[0].Result != string(wire.CodeLogin) {
		t.Fatalf("entry %+v", rows[0])
	}
}

// TestQueryChecksTheQueryItself uses queries memstore accepts (memstore.go:368
// refuses only Limit < 1 and a broken BeforeID), so a green case here can come
// from nowhere but core.LogQuery.Validate in Query.
func TestQueryChecksTheQueryItself(t *testing.T) {
	lg := newLog()
	if _, err := lg.Query(ctx, core.LogQuery{Limit: 500, User: user}); err != nil {
		t.Fatalf("the way to the check is blocked: %v", err)
	}
	for _, q := range []core.LogQuery{{Limit: 501}, {Limit: 10, User: "admin"}} {
		if _, err := lg.Query(ctx, q); err == nil || errors.Is(err, dbapi.ErrInvalid) {
			t.Fatalf("query %+v: got %v, want the refusal of the contract", q, err)
		}
	}
}

// TestNoteKeepsTheOtherKinds covers the kinds of the list in the design that
// have no entry point of their own: changes to users, rights, apps, stores,
// licences and updates.
func TestNoteKeepsTheOtherKinds(t *testing.T) {
	lg := newLog()
	for _, kind := range []string{core.LogTrust, core.LogRights, core.LogApp, core.LogStore,
		core.LogLicense, core.LogUpdate} {
		if err := lg.Note(ctx, kind, user, "ngc.store", "ctl-1", "role admin"); err != nil {
			t.Fatalf("note %s: %v", kind, err)
		}
	}
	rows, err := lg.Query(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(rows) != 6 {
		t.Fatalf("query: %v %d", err, len(rows))
	}
}
