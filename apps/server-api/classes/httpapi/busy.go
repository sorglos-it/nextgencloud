package httpapi

import (
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

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
		busy.hit("the database did not come free in time, the caller is told to come back",
			"db-busy", slog.Int("retry-after", RetryBusy))
		FailRetry(w, code, RetryBusy)
		return code
	}
	away.hit("the database could not be reached", "db-unreachable")
	Fail(w, code)
	return code
}

// The two counters of this path, and why they exist at all.
//
// Measured on 17.10.2026 against the running installation on Debian 13 (WSL2),
// 4096 callers at once on POST /api/v1/login/start: 12288 answers of this
// function wrote 12288 lines, up to 480 of them in ONE second, 1.3 MB of
// journal in 3.9 seconds. Every one of those requests is unauthenticated --
// the name in the body need not exist (login/start answers every name) -- so
// the lines are written at the asking of whoever asks, and the only brake on
// them is how fast the machine answers.
//
// login/start says this very thing one file over, about its OWN warning:
// "login/start has no brake in front of it, so anything oftener would be a way
// to fill a disk by asking" (login/start.go, nonceWarnTTL). That warning has a
// brake and is said at most once every five minutes; this one sat on the same
// unauthenticated path with none. The two halves of the fix are the two halves
// guard uses for the same problem one layer down (point 55):
//
//   - The COUNT is what survives, and it is what an overload looks like from
//     outside: Busy() and Unreachable() grow and never shrink, Report() puts
//     them in the log out of the housekeeping round once a minute and is
//     silent while neither has grown. Before this there was NO number for "too
//     full" anywhere in ngc-api -- only lines, and a line is gone once
//     somebody rotates the file.
//   - The LINE stays, because the first of a kind is the news, and it carries
//     "since" -- how many answers of that kind came in the window that just
//     ended. So nothing is lost by the brake: 480 rejections in a second are
//     one line saying 480, where they were 480 lines saying nothing each.
//
// NOT changed, and it is a finding of the same measurement rather than a thing
// to build here: state.ErrNoTrust -- an installation that is MIGRATED but not
// yet set up -- reaches the second line, so every login/start of such an
// installation says "the database could not be reached" about a database that
// answers perfectly well. Measured: 12288 of these lines in 3.9 s against a
// reachable file. The word for it would be a third reason, and which of the
// two callers may give it is a decision this function cannot make alone.
const warnEvery = 60 // seconds between two lines of one kind

// counter is one kind of rejection: how many there were, how many have been
// said out loud, and when a line of this kind was last written.
type counter struct {
	seen atomic.Uint64
	said atomic.Uint64
	last atomic.Int64
}

var (
	busy counter // the database was there and would not come free
	away counter // the database could not be reached
)

// Busy is how many callers have been told to come back because the database
// was busy, since the process started. Unreachable is the same for a database
// that could not be reached. They are the answer to "an overload that is only
// in the log is one nobody finds": the pair of numbers is what an admin can
// read after the fact, and what a report can carry.
func Busy() uint64        { return busy.seen.Load() }
func Unreachable() uint64 { return away.seen.Load() }

// hit counts one rejection and writes the line when the window has run out.
//
// The count is raised FIRST and always: a rejection the brake keeps quiet
// still has to show up in Busy(), or the brake would be the swallowing of
// point 50 with a nicer name.
func (c *counter) hit(msg, reason string, extra ...slog.Attr) {
	n := c.seen.Add(1)
	now := time.Now().Unix()
	last := c.last.Load()
	if n > 1 && now-last < warnEvery {
		return
	}
	// CompareAndSwap and not a plain store: under this load the line is
	// reached by many goroutines in the same moment, and exactly one of them
	// may have the turn. Whoever loses says nothing -- his rejection is
	// already counted, and the winner's "since" carries it.
	if !c.last.CompareAndSwap(last, now) {
		return
	}
	since := n - c.said.Swap(n)
	args := []any{"reason", reason, "count", n, "since", since}
	for _, a := range extra {
		args = append(args, a)
	}
	slog.Warn(msg, args...)
}

// Report writes the two totals to the log when either has grown since the last
// call, and nothing at all when neither has. It is called once a minute out of
// the housekeeping round, the same place guard.Report is called from, and for
// the same reason: a number no running program ever reads is a diagnostic with
// no way out (point 22).
//
// It is the SECOND way of showing and not a repetition of the first. The line
// of a single rejection is gone once somebody rotates the file; this one
// carries the total of the whole life of the process, so an overload an hour
// ago is still a number.
func Report() {
	b, u := busy.seen.Load(), away.seen.Load()
	lb, lu := reportedBusy.Swap(b), reportedAway.Swap(u)
	if b <= lb && u <= lu {
		return
	}
	slog.Warn("callers were turned away because the database was busy or could not be reached",
		"busy", b, "unreachable", u, "new-busy", b-lb, "new-unreachable", u-lu)
}

// ResetCountsForTest puts both counters and both windows back where a fresh
// process has them. It is here for tests and for nothing else, and it is
// exported for one measured reason: the counters are state of the PROCESS, so
// a case in ANOTHER package that wants to measure the LINE is only the first
// of its kind if nothing before it in the same test binary already was. Two
// cases in package login measured exactly that and went quiet when the brake
// arrived -- and a case that reads the silence of an earlier case is the trap
// messen.md names for counters and one-shot values.
//
// Nothing in the running program calls it: an operator who could put the
// numbers back could make an overload invisible, which is the whole thing the
// counters are against.
func ResetCountsForTest() {
	for _, c := range []*counter{&busy, &away} {
		c.seen.Store(0)
		c.said.Store(0)
		c.last.Store(0)
	}
	reportedBusy.Store(0)
	reportedAway.Store(0)
}

// What Report has already said out loud. Kept apart from counter.said, which
// belongs to the line of a single rejection: the two ways of showing count
// their own windows, or one would eat the other's news.
var (
	reportedBusy atomic.Uint64
	reportedAway atomic.Uint64
)
