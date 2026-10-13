package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// RetryBusy is what a caller is told to wait when the database was busy,
// in seconds. It is short on purpose: the thing he waits for is a queue of
// writers that measured ~180 writes a second per process, not a service that
// has to be started. Longer would only be a guess with more digits.
const RetryBusy = 5

// FailFault answers an error of package state or of the store: the code
// Fault picks, and -- this is the whole of what it adds -- a Retry-After
// beside it when the error was the database being BUSY rather than away.
//
// The two were one answer until here, and that is measured, not supposed
// (point 48 of docs/vertraege-v1.2.md): a second program holding the file
// ends in the driver's "database is locked", which carries no name Fault
// reads, so it leaves the last arm of Fault as E_UNREACHABLE -- the same 503
// a database that is gone gets. A browser cannot tell "come back in a
// moment" from "the database is away" in it, and neither can an admin
// reading the log.
//
// The code stays E_UNREACHABLE, and that is a limit and not a choice: a word
// of its own for this would be a new wire.Code, and that list is in frozen
// shared/ (shared/wire/wire.go:450-469) -- BLOCKED, named in the report.
// Fault's own comment asked for exactly that word ("on the day a second API
// joins, ErrBusy belongs apart with its own answer"), and what can be built
// without the list is what is built here: the HTTP header that says come back
// (Retry-After, as E_LOCKED already uses it) and a line in the log that names
// the reason, so the two cases are apart for a client that reads headers and
// for a human who reads the log, if not yet for one that reads only the code.
//
// It logs the reason and nothing of the request. An error on this path can
// carry a path, a name or a configuration, and a log line of a server goes
// into reports and chats; the two messages differ in wording, which is what
// makes them tellable apart, and neither says what was asked for.
func FailFault(w http.ResponseWriter, err error, from Origin) wire.Code {
	return FailCoded(w, Fault(err, from), err)
}

// FailCoded is FailFault for a caller that already holds the code and carries
// the error beside it. It exists because a code alone cannot be told apart
// here: busy and away are the same E_UNREACHABLE, and only the error says
// which (dbwait.Busy).
//
// Two kinds of caller need it rather than FailFault. One picks the code by a
// reading of its own that Fault does not have -- login's sessionFault answers
// dbapi.ErrNotFound E_LOGIN, where Fault answers E_INTERNAL, because only the
// handler knows a missing row was the answer and not a damaged one. The other
// hands a code it wrote down itself (login/finish). Recomputing the code from
// the error would quietly undo the first and is why the code comes in.
//
// So the code decides what is answered and the error only decides the header
// and the log line. An error of a code that is not E_UNREACHABLE is never
// read, and a nil error is answered as a database that is away.
func FailCoded(w http.ResponseWriter, code wire.Code, err error) wire.Code {
	if code != wire.CodeUnreachable {
		// Every other code is answered exactly as before and said nothing
		// about in the log: this function adds one case and changes none.
		Fail(w, code)
		return code
	}
	if dbwait.Busy(err) {
		// Warn and not Error: nothing is broken, and an admin who sees this
		// often has one program too many on one file (point 47).
		slog.Warn("the database did not come free in time, the caller is told to come back",
			"reason", "db-busy", "retry-after", RetryBusy)
		FailRetry(w, code, RetryBusy)
		return code
	}
	slog.Warn("the database could not be reached", "reason", "db-unreachable")
	Fail(w, code)
	return code
}
