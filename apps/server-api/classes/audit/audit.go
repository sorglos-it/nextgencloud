// Package audit keeps the log: logins and failed logins, changes to users,
// rights, apps, stores and licences, every root task with its signature and
// every channel that opened. Every entry gets an ID of 16 random bytes as
// lower-case hex, so entries of the same second still page in one order
// (core.LogQuery).
//
// Why this package checks every field, even those it gets from inside: an
// administrator reads these entries in the app "Benutzer & Rechte", and an
// entry a caller can fill freely is a tool against that reader - a line break
// in a detail forges a second entry, an escape sequence rewrites the line
// above. Worse, a log is the easiest place in the whole system to lose a
// secret: a password that slipped into a detail, a one-time code in a result,
// a key where a signature was meant. So no field here takes free text. Detail
// is the only field with room for a word, and each entry point says what may
// stand in it: a login takes a method name out of a fixed list, a task takes
// exactly the base64url of an ed25519 signature, and Note takes printable text
// of at most maxDetail bytes without control characters. Nothing from a user
// reaches it: whoever has only free text has to leave it out.
package audit

import (
	"context"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrEntry reports an entry this package refuses to keep. It names the field
// nowhere and carries nothing of the value, so a refusal cannot leak what a
// caller tried to write down.
var ErrEntry = errors.New("audit: entry does not belong in the log")

// maxDetail is how long a detail may be. It is roomy enough for the base64url
// of a signature (86 characters) and short enough that no secret of any length
// fits unnoticed.
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
// between two real ones, and that is how a forged line hides.
func (lg *Log) Add(ctx context.Context, e core.LogEntry) error {
	if e.Result == "" {
		e.Result = "ok"
	}
	if err := check(e); err != nil {
		return err
	}
	id, err := secrets.LogID()
	if err != nil {
		return err
	}
	e.ID, e.Time = id, lg.Now()
	return lg.DB.LogAdd(ctx, e)
}

// check reads every field of an entry against the grammar of the contract. The
// detail is checked by the entry point that knows what may stand in it, so
// here only its shape: printable, no control characters, at most maxDetail
// bytes. ID and Time are not checked, because Add overwrites them after this.
func check(e core.LogEntry) error {
	ok := slices.Contains(kinds, e.Kind) &&
		slices.Contains(results, e.Result) &&
		(e.User == "" || trust.ValidID(e.User)) &&
		(e.App == "" || wire.ValidAppID(e.App)) &&
		(e.Action == "" || wire.ValidName(e.Action)) &&
		(e.Node == "" || wire.ValidName(e.Node)) &&
		(e.Detail == "" || wire.ValidText(e.Detail, maxDetail))
	if !ok {
		return ErrEntry
	}
	return nil
}

// Login notes a login or a failed one. result is "ok" or a wire.Code; detail
// is the method the user proved themselves with and must be one of methods -
// never a password, a code or a key, and never anything the user typed.
func (lg *Log) Login(ctx context.Context, user, result, detail string) error {
	if !slices.Contains(methods, detail) {
		return ErrEntry
	}
	kind := core.LogLogin
	if result != "ok" {
		kind = core.LogLoginFailed
	}
	return lg.Add(ctx, core.LogEntry{User: user, Kind: kind, Result: result, Detail: detail})
}

// Channel notes that a channel opened or was refused.
func (lg *Log) Channel(ctx context.Context, user, app, action, node string, code wire.Code) error {
	result := "ok"
	if code != "" {
		result = string(code)
	}
	return lg.Add(ctx, core.LogEntry{User: user, Kind: core.LogChannel, App: app, Action: action,
		Node: node, Result: result})
}

// Task notes a signed task with its signature, so it can be shown later who
// ordered it. signature is the base64url of the signature of the task and
// nothing else: it must decode to exactly signatureSize bytes, which is why a
// key or a password cannot take its place.
func (lg *Log) Task(ctx context.Context, user, app, action, node, signature string) error {
	if b, err := enc.B64.DecodeString(signature); err != nil || len(b) != signatureSize {
		return ErrEntry
	}
	return lg.Add(ctx, core.LogEntry{User: user, Kind: core.LogTask, App: app, Action: action,
		Node: node, Detail: signature})
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
