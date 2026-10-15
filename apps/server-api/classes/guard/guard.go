// Package guard turns a programming error in the work of ONE channel into the
// end of that channel, so the server keeps every other session of every other
// user. It is the built half of the decision of V1.2, point 38: before it,
// recover() came up nowhere in the production code of apps/server-api,
// apps/server-controller, apps/server-db, apps/web, shared, tools and cmd --
// so one nil dereference in one handler of one channel took the whole ngc-api
// down, with all 128 app channels of every link and the controller tunnels.
//
// What is caught is a BUG, never a refusal: a channel that answers a code
// answers it the ordinary way, and nothing here is a path anybody plans to
// take. A catch is therefore loud in four places at once, because the one
// thing worse than a crash is a crash nobody finds again (V1.2 point 50, and
// messen.md, "Ein verschluckter Fehler wird zur Falschauskunft"):
//
//  1. The channel ends with E_INTERNAL, so the browser is told something broke
//     instead of waiting for an answer that never comes.
//  2. A log line of level ERROR names channel, place and kind.
//  3. The audit entry of the channel carries that code, so it is in the
//     database and not only in a file somebody rotates away.
//  4. Panics() counts, so a defect that fires once an hour is a number and not
//     a feeling.
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

// caught counts every panic this package turned into the end of a channel.
var caught atomic.Uint64

// Panics is how many panics have been caught since the process started. It is
// the answer to "a caught error that is only in the log is one nobody finds":
// a defect shows itself as a number that does not stop growing.
func Panics() uint64 { return caught.Load() }

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
func Hit(v any, site string, channel uint32) bool {
	if v == nil {
		return false
	}
	n := caught.Add(1)
	// The KIND and not the value: see the package comment. %T of a nil
	// dereference is runtime.Error's concrete type and says what happened
	// without saying what with.
	slog.Error("a programming error ended one channel, the server keeps running",
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
