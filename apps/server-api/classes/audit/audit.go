// Package audit keeps the log: logins and failed logins, changes to users,
// rights, apps, stores and licences, every root task with its signature and
// every channel that opened. Every entry gets an ID of 16 random bytes as
// lower-case hex, so entries of the same second still page in one order
// (core.LogQuery).
//
// Why this package checks every field, even those it gets from inside: an
// administrator reads these entries in the app "Benutzer & Rechte", and a field
// a caller can fill freely is a tool against that reader - a line break in a
// detail forges a second entry, an escape sequence rewrites the line above.
// Worse, a log is the easiest place in the whole system to lose a secret: a
// password that slipped into a detail, a one-time code in a result, a key where
// a signature was meant. So no field here takes free text.
//
// And why a bad field never costs the entry: whoever looks for a hole does not
// need the field to be clean, only the event to be there at all. A caller that
// could bend one field into a refusal would hold a way to keep its own failed
// attempt out of the log, and that is worse than an ugly field. So a value
// outside the grammar is replaced by Dropped and the entry is written; the
// caller is told with ErrDropped, but the writing does not depend on it acting
// on that. The one exception is the kind, which is what happened and cannot be
// stood in for - see ErrEntry.
package audit

import (
	"context"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var (
	// ErrDropped says that the entry was written and that at least one field
	// of it reads Dropped. It is a report, not a refusal: a caller may note it,
	// and nothing is lost if it does not.
	ErrDropped = errors.New("audit: a field of the entry was dropped")

	// ErrEntry says that nothing was written. Only an unknown kind gets here:
	// the kind is the event itself, so a placeholder in its place would write
	// down something no reader and no filter can make sense of. The four entry
	// points below set their kind as a constant, so only a hand-made Add or
	// Note can reach this.
	ErrEntry = errors.New("audit: entry of an unknown kind")
)

// Neither error carries anything of the value it is about: an error text of a
// log is read and kept in places an entry is not, so a value in it would lose
// exactly the secret the entry was not allowed to get.

const (
	// Dropped stands where a value did not belong. It is no valid user ID, app
	// id, service name or result, and a reader can therefore never mistake it
	// for something a caller really sent (TestThePlaceholderCannotBeARealValue).
	Dropped = "-"

	// UserSystem is the user of an entry nobody ordered - an automatic update,
	// a licence that ran out. The design asks for time and user in every entry,
	// so the machine gets a name instead of leaving the field empty. It is no
	// valid ID either, so no person can ever be called this.
	UserSystem = "system"
)

// maxDetail is how long a detail may be. It is roomy enough for the base64url
// of a signature (86 characters) and short enough that no secret of any length
// fits unnoticed. TestDetailOf100BytesIsKept pins the number itself.
const maxDetail = 100

// signatureSize is the length of an ed25519 signature in bytes. A key is 32
// bytes, so this length alone keeps a key out of Task.
const signatureSize = 64

// kinds are the kinds of entry the log holds; core.LogEntry.Kind takes no
// other. The list is closed on purpose: an unknown kind would hide an entry
// from every filter that counts the known ones.
var kinds = []string{core.LogLogin, core.LogLoginFailed, core.LogChannel, core.LogTask,
	core.LogTrust, core.LogRights, core.LogApp, core.LogStore, core.LogLicense, core.LogUpdate}

// methods are the words a login may note as its detail. They name how the user
// proved themselves, nothing else; no value of the proof itself is ever one of
// them.
var methods = []string{"", "password", "passkey", "totp", "setup", "recovery"}

// results are the values core.LogEntry.Result takes: "ok" or one of the codes
// of the protocol. A result is never a message, so no text of an error reaches
// the log through it.
var results = func() []string {
	out := []string{"ok"}
	for _, c := range []wire.Code{wire.CodeForbidden, wire.CodeUnreachable, wire.CodeExit,
		wire.CodeTimeout, wire.CodeLimit, wire.CodeProtocol, wire.CodeConfirm, wire.CodeLicense,
		wire.CodeNotFound, wire.CodeVerify, wire.CodeInternal, wire.CodeTooLarge, wire.CodeLogin,
		wire.CodeLocked} {
		out = append(out, string(c))
	}
	return out
}()

// Log writes and reads the log.
type Log struct {
	DB  dbapi.Store
	Now func() int64
}

// Add keeps one entry; it sets the ID and the time itself, so neither can come
// from a caller: an entry under a chosen ID or a chosen time would sort itself
// between two real ones, and that is how a forged line hides. A field outside
// the grammar is replaced by Dropped and the entry is written anyway; the
// answer is then ErrDropped. Only an unknown kind gives ErrEntry and writes
// nothing.
func (lg *Log) Add(ctx context.Context, e core.LogEntry) error { return lg.add(ctx, e, false) }

// add is Add with what the entry point already knows: lost says that it threw a
// value away itself, so that its loss is reported too. Without that flag a
// dropped method or signature would look clean here, because the placeholder
// passes every check.
func (lg *Log) add(ctx context.Context, e core.LogEntry, lost bool) error {
	if !slices.Contains(kinds, e.Kind) {
		return ErrEntry
	}
	e, dirty := clean(e)
	lost = lost || dirty
	id, err := secrets.LogID()
	if err != nil {
		return err
	}
	e.ID, e.Time = id, lg.Now()
	if err := lg.DB.LogAdd(ctx, e); err != nil {
		return err
	}
	if lost {
		return ErrDropped
	}
	return nil
}

// clean replaces every field outside the grammar of the contract by its
// placeholder and reports whether it had to. The detail is checked only for its
// shape here - printable, no control characters, at most maxDetail bytes -
// because what may stand in it is known to the entry point, not here. ID and
// Time are untouched, because Add overwrites them after this.
func clean(e core.LogEntry) (core.LogEntry, bool) {
	lost := false
	keep := func(field *string, ok bool, placeholder string) {
		if !ok {
			*field, lost = placeholder, true
		}
	}
	// An entry without a user is the machine, which is a real answer and no
	// loss; a user that is no ID is a mistake and reads as dropped.
	if e.User == "" {
		e.User = UserSystem
	}
	keep(&e.User, e.User == UserSystem || trust.ValidID(e.User), Dropped)
	// An empty result means success; the contract has no third value.
	if e.Result == "" {
		e.Result = "ok"
	}
	keep(&e.Result, slices.Contains(results, e.Result), Dropped)
	keep(&e.App, e.App == "" || wire.ValidAppID(e.App), Dropped)
	keep(&e.Action, e.Action == "" || wire.ValidName(e.Action), Dropped)
	keep(&e.Node, e.Node == "" || wire.ValidName(e.Node), Dropped)
	keep(&e.Detail, e.Detail == "" || wire.ValidText(e.Detail, maxDetail), Dropped)
	return e, lost
}

// Login notes a login or a failed one. result is "ok" or a wire.Code; detail
// is the method the user proved themselves with and must be one of methods -
// never a password, a code or a key, and never anything the user typed. A
// method outside the list is dropped; the attempt is written down either way,
// because an attempt nobody can see is the one an attacker wants.
func (lg *Log) Login(ctx context.Context, user, result, detail string) error {
	lost := !slices.Contains(methods, detail)
	if lost {
		detail = Dropped
	}
	kind := core.LogLogin
	if result != "ok" {
		kind = core.LogLoginFailed
	}
	return lg.add(ctx, core.LogEntry{User: user, Kind: kind, Result: result, Detail: detail}, lost)
}

// Channel notes that a channel opened or was refused. why says which check
// said no and becomes the detail of the entry; a channel that opened carries
// reason.None and no detail.
//
// Why the detail is bounded to the closed list of package reason and not to
// the shape of a text: a code alone is no reason -- "E_INTERNAL" tells an
// administrator nothing -- but the field that fixes that is the one field of
// this log a caller could otherwise fill freely, and this package takes no
// free text (see the package comment). So a sentence outside the list is
// dropped and the entry is written all the same, as everything else here is:
// whoever looks for a hole does not need the field, only the event to be
// missing.
func (lg *Log) Channel(ctx context.Context, user, app, action, node string, code wire.Code,
	why reason.Reason) error {
	result := "ok"
	if code != "" {
		result = string(code)
	}
	detail := string(why)
	lost := why != reason.None && !reason.Known(why)
	if lost {
		detail = Dropped
	}
	return lg.add(ctx, core.LogEntry{User: user, Kind: core.LogChannel, App: app, Action: action,
		Node: node, Result: result, Detail: detail}, lost)
}

// Task notes a signed task with its signature, so it can be shown later who
// ordered it. signature is the base64url of the signature of the task and
// nothing else: it must decode to exactly signatureSize bytes, which is why a
// key or a password cannot take its place. One that does not is dropped - the
// task stays in the log without its signature.
func (lg *Log) Task(ctx context.Context, user, app, action, node, signature string) error {
	b, err := enc.B64.DecodeString(signature)
	lost := err != nil || len(b) != signatureSize
	if lost {
		signature = Dropped
	}
	return lg.add(ctx, core.LogEntry{User: user, Kind: core.LogTask, App: app, Action: action,
		Node: node, Detail: signature}, lost)
}

// Note keeps an entry of any other kind (core.LogTrust, LogRights, LogApp,
// LogStore, LogLicense, LogUpdate). detail says what changed in words of the
// program - a role name, a version, a mode - never a value a user gave and
// never a secret.
func (lg *Log) Note(ctx context.Context, kind, user, app, node, detail string) error {
	return lg.Add(ctx, core.LogEntry{User: user, Kind: kind, App: app, Node: node, Detail: detail})
}

// Query answers ngc.users/log. It checks the query against the contract first,
// so a query no node would accept never reaches a database.
func (lg *Log) Query(ctx context.Context, q core.LogQuery) ([]core.LogEntry, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return lg.DB.LogQuery(ctx, q)
}
