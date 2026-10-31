package audit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/trust"
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

// only returns the one entry in the log and fails if there is not exactly one.
func only(t *testing.T, lg *Log) core.LogEntry {
	t.Helper()
	rows, err := lg.Query(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v, %d rows, want 1", err, len(rows))
	}
	return rows[0]
}

func TestAddSetsIDAndTime(t *testing.T) {
	lg := newLog()
	if err := lg.Login(ctx, user, "ok", ""); err != nil {
		t.Fatalf("login: %v", err)
	}
	e := only(t, lg)
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
	if e := only(t, lg); e.ID == forged || e.Time != 1790000000 {
		t.Fatalf("entry %+v", e)
	}
}

func TestChannelKeepsTheCode(t *testing.T) {
	lg := newLog()
	if err := lg.Channel(ctx, user, "org.sorglos.terminal", "shell", "services-1", wire.CodeForbidden,
		reason.NoRight); err != nil {
		t.Fatalf("channel: %v", err)
	}
	if e := only(t, lg); e.Result != string(wire.CodeForbidden) || e.Node != "services-1" {
		t.Fatalf("entry %+v", e)
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

// dropped runs one bad entry and demands three things at once: the event is in
// the log, the bad value is not, and the caller is told. It first writes good,
// which must be kept: without that step the outcome could come from the
// memstore check in memstore.go:345 instead of this package, and the case would
// be green without ever reaching the line it means to pin.
func dropped(t *testing.T, good, bad core.LogEntry, field func(core.LogEntry) string) {
	t.Helper()
	lg := newLog()
	if err := lg.Add(ctx, good); err != nil {
		t.Fatalf("the way to the check is blocked, good entry gives %v", err)
	}
	if got := field(only(t, lg)); got == Dropped {
		t.Fatalf("the good value already reads as dropped, the case proves nothing")
	}

	lg = newLog()
	err := lg.Add(ctx, bad)
	// The caller learns of it, and never as ErrEntry: nothing was refused.
	if !errors.Is(err, ErrDropped) || errors.Is(err, ErrEntry) {
		t.Fatalf("got %v, want ErrDropped and not ErrEntry", err)
	}
	e := only(t, lg) // the event is there - this is the assertion that matters
	if got := field(e); got != Dropped {
		t.Fatalf("field reads %q, want the placeholder %q", got, Dropped)
	}
	if e.Kind != bad.Kind || e.Time != 1790000000 {
		t.Fatalf("kind or time not kept: %+v", e)
	}
}

// TestABadFieldLosesTheFieldAndNotTheEvent is the heart of this package. A
// caller that can bend one field must not be able to keep the event out of the
// log - that would be a way to hide a failed attempt of its own. So the bad
// value is replaced by a placeholder that cannot come from any input, and the
// entry is written. The fields are taken one at a time, so each case shows
// which field it pins.
func TestABadFieldLosesTheFieldAndNotTheEvent(t *testing.T) {
	ok := core.LogEntry{Kind: core.LogTrust, User: user, App: "ngc.store", Action: "activate",
		Node: "ctl-1", Result: "ok", Detail: "role admin"}
	with := func(f func(e *core.LogEntry)) core.LogEntry {
		e := ok
		f(&e)
		return e
	}
	cases := []struct {
		what  string
		bad   core.LogEntry
		field func(core.LogEntry) string
	}{
		{"line break in the detail", with(func(e *core.LogEntry) {
			e.Detail = "ok\n1790000000 admin login ok"
		}), func(e core.LogEntry) string { return e.Detail }},
		{"tab in the detail", with(func(e *core.LogEntry) { e.Detail = "a\tb" }),
			func(e core.LogEntry) string { return e.Detail }},
		{"escape in the detail", with(func(e *core.LogEntry) { e.Detail = "a\x1b[2Kb" }),
			func(e core.LogEntry) string { return e.Detail }},
		{"detail of 101 bytes", with(func(e *core.LogEntry) { e.Detail = strings.Repeat("x", 101) }),
			func(e core.LogEntry) string { return e.Detail }},
		{"user that is no ID", with(func(e *core.LogEntry) { e.User = "admin" }),
			func(e core.LogEntry) string { return e.User }},
		{"app that is no app id", with(func(e *core.LogEntry) { e.App = "../../etc/passwd" }),
			func(e core.LogEntry) string { return e.App }},
		{"action that is no name", with(func(e *core.LogEntry) { e.Action = "Activate App" }),
			func(e core.LogEntry) string { return e.Action }},
		{"node that is no name", with(func(e *core.LogEntry) { e.Node = "ctl 1" }),
			func(e core.LogEntry) string { return e.Node }},
		{"result that is neither ok nor a code", with(func(e *core.LogEntry) { e.Result = "E_MADE_UP" }),
			func(e core.LogEntry) string { return e.Result }},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) { dropped(t, ok, c.bad, c.field) })
	}
}

// TestDetailOf100BytesIsKept nails the limit down to its number. Without it a
// change from 100 to 101 would live: the case above would simply measure the
// new limit, and nobody who raises the bound would see a red case.
func TestDetailOf100BytesIsKept(t *testing.T) {
	lg := newLog()
	long := strings.Repeat("x", 100)
	if err := lg.Note(ctx, core.LogTrust, user, "ngc.store", "ctl-1", long); err != nil {
		t.Fatalf("note: %v", err)
	}
	if e := only(t, lg); e.Detail != long {
		t.Fatalf("detail of %d bytes, want %d", len(e.Detail), len(long))
	}
}

// TestThePlaceholderCannotBeARealValue: the placeholders say "this was thrown
// away". If one of them could also be a value a caller really sends, a reader
// could not tell the two apart, and the whole scheme would be worthless.
func TestThePlaceholderCannotBeARealValue(t *testing.T) {
	for _, p := range []string{Dropped, UserSystem} {
		if trust.ValidID(p) {
			t.Fatalf("%q is a valid user ID", p)
		}
	}
	if wire.ValidAppID(Dropped) || wire.ValidName(Dropped) {
		t.Fatalf("%q is a valid app id or name", Dropped)
	}
	if slicesContains(results, Dropped) {
		t.Fatalf("%q is a valid result", Dropped)
	}
	// Both are plain words, so neither can be confused with a signature or a
	// detail an attacker chose; and they are not each other.
	if Dropped == UserSystem {
		t.Fatalf("the two placeholders are the same")
	}
}

func slicesContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestAnEmptyUserBecomesTheMachine: the design asks for time AND user in every
// entry. A change without a person is real (an automatic update), so it names
// the machine instead of leaving the field empty.
func TestAnEmptyUserBecomesTheMachine(t *testing.T) {
	lg := newLog()
	if err := lg.Note(ctx, core.LogUpdate, "", "ngc.store", "ctl-1", "mode auto"); err != nil {
		t.Fatalf("note: %v", err)
	}
	if e := only(t, lg); e.User != UserSystem {
		t.Fatalf("user %q, want %q", e.User, UserSystem)
	}
}

// TestNoFieldCanKeepAnEventOutOfTheLog walks every field of an entry with a
// value that is as wrong as it can be, each one alone and then all together,
// and demands a row every single time. This is the case that answers the
// question "can an attacker who bends one field hide the event?".
func TestNoFieldCanKeepAnEventOutOfTheLog(t *testing.T) {
	const evil = "\x00\x1b[2K' OR 1=1 --\n\n" // control characters, SQL, line breaks
	huge := strings.Repeat("\xff", 10000)     // invalid UTF-8, far over every limit
	base := core.LogEntry{Kind: core.LogChannel, User: user, App: "ngc.store", Action: "open",
		Node: "ctl-1", Result: "ok", Detail: "ok"}
	fields := map[string]func(e *core.LogEntry, v string){
		"user":   func(e *core.LogEntry, v string) { e.User = v },
		"app":    func(e *core.LogEntry, v string) { e.App = v },
		"action": func(e *core.LogEntry, v string) { e.Action = v },
		"node":   func(e *core.LogEntry, v string) { e.Node = v },
		"result": func(e *core.LogEntry, v string) { e.Result = v },
		"detail": func(e *core.LogEntry, v string) { e.Detail = v },
	}
	for name, set := range fields {
		for _, v := range []string{evil, huge, "", " "} {
			lg := newLog()
			e := base
			set(&e, v)
			if err := lg.Add(ctx, e); err != nil && !errors.Is(err, ErrDropped) {
				t.Fatalf("%s = %q: %v", name, v, err)
			}
			if got := only(t, lg); got.Kind != core.LogChannel {
				t.Fatalf("%s = %q: kind %q", name, v, got.Kind)
			}
		}
	}
	// Every field wrong at once, and the event still stands.
	all := base
	for _, set := range fields {
		set(&all, evil)
	}
	lg := newLog()
	if err := lg.Add(ctx, all); !errors.Is(err, ErrDropped) {
		t.Fatalf("all fields wrong: %v, want ErrDropped", err)
	}
	e := only(t, lg)
	if e.Kind != core.LogChannel || e.Time != 1790000000 || e.User != Dropped || e.Detail != Dropped {
		t.Fatalf("entry %+v", e)
	}
}

// TestOnlyAnUnknownKindIsRefused names the one way left on which an event does
// not reach the log, so nobody has to find it by surprise: a kind outside
// core.Log*. A kind cannot be replaced by a placeholder, because it IS what
// happened - a placeholder there would write down an event that nobody can
// read. It is no lever for an attacker either: the four entry points set their
// own kind as a constant, so only a hand-made Add or Note can hit this.
func TestOnlyAnUnknownKindIsRefused(t *testing.T) {
	lg := newLog()
	if err := lg.Add(ctx, core.LogEntry{Kind: "login-ok", User: user}); !errors.Is(err, ErrEntry) {
		t.Fatalf("got %v, want ErrEntry", err)
	}
	if rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10}); len(rows) != 0 {
		t.Fatalf("%d rows, want 0", len(rows))
	}
	// And the entry points cannot reach it, whatever they are handed.
	for _, err := range []error{
		lg.Login(ctx, "\x00", "\x00", "\x00"),
		lg.Channel(ctx, "\x00", "\x00", "\x00", "\x00", wire.Code("\x00"), reason.Reason("\x00")),
		lg.Task(ctx, "\x00", "\x00", "\x00", "\x00", "\x00"),
	} {
		if errors.Is(err, ErrEntry) {
			t.Fatalf("an entry point refused the event: %v", err)
		}
	}
	if rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10}); len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
}

// TestTaskKeepsOnlyASignature pins that the signature field cannot be used to
// smuggle something else in. A key is 32 bytes, a signature 64; a caller that
// hands over the wrong one loses the value, not the task.
func TestTaskKeepsOnlyASignature(t *testing.T) {
	lg := newLog()
	if err := lg.Task(ctx, user, "ngc.store", "activate", "ctl-1", signature); err != nil {
		t.Fatalf("task: %v", err)
	}
	if e := only(t, lg); e.Detail != signature || e.Kind != core.LogTask {
		t.Fatalf("entry %+v", e)
	}
	for _, bad := range []string{"", strings.Repeat("A", 43), strings.Repeat("A", 85), "hunter2",
		strings.Repeat("A", 85) + "="} {
		lg := newLog()
		if err := lg.Task(ctx, user, "ngc.store", "activate", "ctl-1", bad); !errors.Is(err, ErrDropped) {
			t.Fatalf("signature %q: got %v, want ErrDropped", bad, err)
		}
		if e := only(t, lg); e.Detail != Dropped || e.Kind != core.LogTask {
			t.Fatalf("signature %q: entry %+v", bad, e)
		}
	}
}

// TestLoginKeepsOnlyAKnownMethod: the detail of a login is the one field a
// login path could fill with input of the user. Only the method words survive,
// so neither a password, a one-time code nor a name can land there - but the
// attempt itself is always written down.
func TestLoginKeepsOnlyAKnownMethod(t *testing.T) {
	for _, good := range []string{"", "password", "passkey", "totp", "setup", "recovery"} {
		lg := newLog()
		if err := lg.Login(ctx, user, "ok", good); err != nil {
			t.Fatalf("method %q: %v", good, err)
		}
		if e := only(t, lg); e.Detail != good {
			t.Fatalf("method %q: detail %q", good, e.Detail)
		}
	}
	for _, bad := range []string{"hunter2", "123456", "totp ", "TOTP", "password+totp", "user admin"} {
		lg := newLog()
		if err := lg.Login(ctx, user, string(wire.CodeLogin), bad); !errors.Is(err, ErrDropped) {
			t.Fatalf("method %q: got %v, want ErrDropped", bad, err)
		}
		// The failed attempt stands, with the method thrown away.
		e := only(t, lg)
		if e.Kind != core.LogLoginFailed || e.Detail != Dropped {
			t.Fatalf("method %q: entry %+v", bad, e)
		}
	}
}

func TestLoginMarksTheFailedOne(t *testing.T) {
	lg := newLog()
	if err := lg.Login(ctx, user, string(wire.CodeLogin), "password"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if e := only(t, lg); e.Kind != core.LogLoginFailed || e.Result != string(wire.CodeLogin) {
		t.Fatalf("entry %+v", e)
	}
}

// TestAddFillsAnEmptyResult: an entry without a result means success, so Add
// writes "ok" rather than throwing anything away.
func TestAddFillsAnEmptyResult(t *testing.T) {
	lg := newLog()
	if err := lg.Add(ctx, core.LogEntry{Kind: core.LogTrust, User: user}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if e := only(t, lg); e.Result != "ok" {
		t.Fatalf("entry %+v", e)
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

// ---- Pairing ----------------------------------------------------------------

// TestAPairingOfAnUnknownActionIsDropped holds the grammar of the "Aktion"
// column: Pairing writes one of pairActions or the placeholder, never a third
// word -- and it writes the entry all the same, because the attempt is the
// event.
//
// The word is "login" and not a broken one on purpose. A newline or an empty
// string is caught by clean for EVERY entry of this log, so a case built on
// one of those measures clean and not the closed list here -- measured on
// 24.10.2026: with the list switched off (lost := false) such a case still
// went green. What only the list catches is a word that passes the grammar
// and is still not a pairing, and that is the one an admin would misread:
// "login" in the Aktion column of a trust entry reads as an attempt to sign
// in.
func TestAPairingOfAnUnknownActionIsDropped(t *testing.T) {
	lg := newLog()
	err := lg.Pairing(ctx, "login", wire.CodeVerify, reason.PairProof)
	if !errors.Is(err, ErrDropped) {
		t.Fatalf("pairing: %v, want ErrDropped", err)
	}
	e := only(t, lg)
	if e.Action != Dropped {
		t.Fatalf("action %q, want %q", e.Action, Dropped)
	}
	// The entry is there and still says why it failed.
	if e.Kind != core.LogTrust || e.Result != string(wire.CodeVerify) ||
		e.Detail != string(reason.PairProof) {
		t.Fatalf("entry %+v", e)
	}
}

// TestAPairingReasonOutsideTheListIsDropped is the same for the detail: the
// one field of this log that carries a sentence takes only sentences of
// package reason.
func TestAPairingReasonOutsideTheListIsDropped(t *testing.T) {
	lg := newLog()
	err := lg.Pairing(ctx, ActionJoin, wire.CodeVerify, reason.Reason("Passwort: hunter2"))
	if !errors.Is(err, ErrDropped) {
		t.Fatalf("pairing: %v, want ErrDropped", err)
	}
	e := only(t, lg)
	if e.Detail != Dropped || e.Action != ActionJoin {
		t.Fatalf("entry %+v", e)
	}
}

// TestAPairingThatGotThroughCarriesNoReason pins the other end: no code means
// "ok", and an entry without a reason has an empty detail rather than a
// placeholder, so an admin never reads a dropped field where nothing was
// dropped.
func TestAPairingThatGotThroughCarriesNoReason(t *testing.T) {
	lg := newLog()
	if err := lg.Pairing(ctx, ActionPair, "", reason.None); err != nil {
		t.Fatalf("pairing: %v", err)
	}
	e := only(t, lg)
	if e.Result != "ok" || e.Detail != "" || e.User != UserSystem {
		t.Fatalf("entry %+v", e)
	}
}

// TestEveryPairingReasonSurvivesAsADetail is the sibling of
// TestEveryReasonSurvivesAsADetail for the two sentences Pairing uses: both
// have to pass the grammar of clean unchanged, or the line an admin reads
// would be a placeholder exactly when it matters.
func TestEveryPairingReasonSurvivesAsADetail(t *testing.T) {
	for _, why := range []reason.Reason{reason.PairProof, reason.JoinProof, reason.Defect, reason.DB} {
		lg := newLog()
		if err := lg.Pairing(ctx, ActionPair, wire.CodeVerify, why); err != nil {
			t.Fatalf("%q: %v", why, err)
		}
		if e := only(t, lg); e.Detail != string(why) {
			t.Fatalf("%q came out as %q", why, e.Detail)
		}
	}
}
