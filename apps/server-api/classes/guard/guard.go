// Package guard turns a programming error in ONE goroutine into the end of
// that one piece of work, so the server keeps every other session of every
// other user. It is the built half of the decision of V1.2, point 38: before
// it, recover() came up nowhere in the production code of apps/server-api,
// apps/server-controller, apps/server-db, apps/web, shared, tools and cmd --
// so one nil dereference in one handler of one channel took the whole ngc-api
// down, with all 128 app channels of every link and the controller tunnels.
//
// What is caught is a BUG, never a refusal: a channel that answers a code
// answers it the ordinary way, and nothing here is a path anybody plans to
// take. The one thing worse than a crash is a crash nobody finds again (V1.2
// point 50, and messen.md, "Ein verschluckter Fehler wird zur
// Falschauskunft"), so every catch is loud -- but HOW loud depends on what the
// goroutine was doing, and the thirteen catch sites are not all alike:
//
//   - A catch in CHANNEL work ends the channel with E_INTERNAL, so the browser
//     is told something broke instead of waiting for an answer that never
//     comes: browser.channel, coreapp.open, relay.toController,
//     relay.sendStream.
//   - Of those four, the three that get their code to the one who writes the
//     log of the channel (coreapp.open through OpenChannel, and the two in
//     relay through Relay.Forward) also put E_INTERNAL in the AUDIT ENTRY, so
//     the defect is in the database and not only in a file somebody rotates
//     away. browser.channel cannot: the call that would have written the
//     entry is the very one that panicked before it began.
//   - coreapp.OpenChannel is a backstop and neither: it holds the little that
//     happens AFTER the work -- the audit entry and the CLOSE -- so a panic it
//     catches is one in the writing down itself, and there is nothing left to
//     write down or to close with.
//   - A catch in a ROUND or in a hook that belongs to no channel (server.round,
//     server.rollout, the three backstops in Server.serve, controllers.hello,
//     coreapp.pushTrust, link.closeBounded) has no channel to end and no audit
//     entry to write: there NOTHING follows but the line and the count.
//     Channel 0 in the line says exactly that.
//
// So the line and the count are the two ways that hold everywhere, and the
// count is the one that survives a rotated log file: Panics() is the number,
// and Report() is what puts it where somebody reads it -- called once a minute
// out of the housekeeping round (server.minute), and silent until the number
// grows. A defect that fires once an hour is then a number and not a feeling.
//
// WHAT NEVER GOES INTO THE LINE, and it is measured, not meant
// (guard_test.go, TestTheLineCarriesNoValueOfThePanic): the panic VALUE. Only
// its Go TYPE. A panic value is written by whoever panicked -- a wrapped
// error, a formatted string -- and it carries whatever that caller had in
// hand: a token, a password, a name, a row of the database. The same holds for
// the stack: a Go stack prints the argument words of every frame, so it is not
// taken either. What replaces it is the ONE place that matters, the line that
// panicked, read off the stack and printed as the last two parts of its path.
// Nothing of the request, the session, the user or the parameters is read here
// at all -- the call hands in a channel number and a fixed place name, and
// there is no third way in.
package guard

import (
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
)

// caught counts every panic this package turned into the end of one piece of
// work; reported is how much of that count Report has already said out loud.
var (
	caught   atomic.Uint64
	reported atomic.Uint64
)

// Panics is how many panics have been caught since the process started. It is
// the answer to "a caught error that is only in the log is one nobody finds":
// a defect shows itself as a number that does not stop growing.
func Panics() uint64 { return caught.Load() }

// Report writes the running total to the log when it has grown since the last
// call, and nothing at all when it has not. Without it Panics() was a number
// no running program ever read -- a diagnostic with no way out, which is the
// shape of V1.2 point 22 ("Ein Diagnoseschalter ohne Weg in die Konfiguration
// ist keiner"). The housekeeping round calls it once a minute (Server.minute),
// so the number reaches the log of its own accord.
//
// It is the catch's SECOND way of showing and not a repetition of the first:
// the line of a single catch is ERROR and names its place, and that line is
// gone once somebody rotates the file. This one carries the total of the whole
// life of the process, so a defect that fired an hour ago is still a number.
// It is therefore WARN and not ERROR -- the growing total is the news, and the
// ERROR about the defect itself has already been written.
//
// Silent while nothing grows, because a line every minute saying "none" is the
// kind of noise that teaches people to stop reading the log. It carries two
// numbers and nothing else: no place name and no channel, because a total
// belongs to no one site.
func Report() {
	n := caught.Load()
	last := reported.Swap(n)
	if n <= last {
		return
	}
	slog.Warn("programming errors were caught and single pieces of work ended, the server kept running",
		"panics", n, "new", n-last)
}

// Hit reports whether v is a panic, and if it is, counts it and writes the
// line. It is made to be called out of a deferred function with recover() in
// place, which is the one place recover answers in:
//
//	defer func() {
//		if guard.Hit(recover(), "coreapp.open", c.Num) {
//			code = wire.CodeInternal
//		}
//	}()
//
// site is a fixed name of the place in the code, never anything of a request.
// channel is the number of the browser's channel, or 0 where the work belongs
// to no channel of its own.
//
// It writes to the default logger. A caller that carries a logger of its own
// -- ngc-controller gives every tunnel one -- uses HitTo instead, so the line
// lands where the rest of that part's lines land.
func Hit(v any, site string, channel uint32) bool {
	return HitTo(slog.Default(), v, site, channel)
}

// HitTo is Hit into a logger the caller names. A nil logger is the default
// one, because a catch that dropped its line for want of a logger would be
// the silent defect this package exists against.
func HitTo(l *slog.Logger, v any, site string, channel uint32) bool {
	if v == nil {
		return false
	}
	if l == nil {
		l = slog.Default()
	}
	n := caught.Add(1)
	// The KIND and not the value: see the package comment. %T of a nil
	// dereference is runtime.Error's concrete type and says what happened
	// without saying what with.
	l.Error("a programming error ended one piece of work, the server keeps running",
		"site", site, "channel", channel, "kind", fmt.Sprintf("%T", v),
		"where", where(), "panics", n)
	return true
}

// where is the line that panicked: the first frame behind runtime.gopanic
// that is not the runtime's own. The second half of that sentence is measured
// and not tidy -- a nil dereference does not panic in our code at all, it
// panics in the runtime: the frames behind gopanic were runtime/panic.go:336
// (runtime.panicmem, then runtime.sigpanic) and the line of ours came only
// after them. A where that took the first frame would have named the Go
// runtime for every nil pointer in the program, which is the one kind of bug
// this line exists for. TestTheLineNamesTheLineThatPanicked holds it.
//
// Only the last two parts of the path are printed, so the build directory of
// the machine stays out of the log.
func where() string {
	var pcs [64]uintptr
	// 1 skips runtime.Callers itself; everything else is walked, because how
	// many frames lie between here and the panic depends on the caller.
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	past := false
	for {
		f, more := frames.Next()
		switch {
		case f.Function == "runtime.gopanic":
			past = true
		case past && f.File != "" && !strings.HasPrefix(f.Function, "runtime."):
			return shortPath(f.File) + ":" + strconv.Itoa(f.Line)
		}
		if !more {
			return "unknown"
		}
	}
}

// shortPath keeps the last two parts of a file path: enough to name the file
// of the package, not enough to print where the machine builds.
func shortPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}
