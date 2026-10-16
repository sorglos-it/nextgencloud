package guard

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// secret is the kind of thing a panic value carries by accident: a value
// somebody put into an error or a formatted string. No case here lets it reach
// a line, and every case looks for THIS string, so a line that grew a field
// would show up as a failure and not as a nicer log.
const secret = "NGC-TEST-SECRET-0123456789"

// catchLog puts a log of its own in place of the default one and gives back
// what was written. It restores the default itself, so one case cannot carry
// its log into the next.
func catchLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// TestNothingIsAPanic is the control of every case below: without it a Hit
// that answered true to everything would pass them all. recover() of a
// function that did not panic gives nil, and nil is not a hit.
func TestNothingIsAPanic(t *testing.T) {
	buf := catchLog(t)
	before := Panics()
	if Hit(nil, "test.site", 7) {
		t.Fatal("nil was taken for a panic")
	}
	if got := Panics(); got != before {
		t.Fatalf("the counter moved without a panic: %d -> %d", before, got)
	}
	if buf.Len() != 0 {
		t.Fatalf("a line was written without a panic: %q", buf.String())
	}
}

// TestTheLineCarriesNoValueOfThePanic is the assertion this whole package
// turns on, and it is the one a careless change breaks first: the panic value
// is written by whoever panicked and carries whatever that caller had in hand.
// So the line carries the TYPE and never the value -- and never the stack,
// which prints the argument words of every frame.
func TestTheLineCarriesNoValueOfThePanic(t *testing.T) {
	buf := catchLog(t)
	func() {
		defer func() {
			if !Hit(recover(), "test.site", 42) {
				t.Error("the panic was not caught")
			}
		}()
		panic(fmt.Errorf("the bearer token is %s", secret))
	}()
	line := buf.String()
	if strings.Contains(line, secret) {
		t.Fatalf("the value of the panic reached the line: %q", line)
	}
	if strings.Contains(line, "bearer token") {
		t.Fatalf("the text of the panic reached the line: %q", line)
	}
	// What has to be in it, because a line that says nothing is the silent
	// defect this package was built against. The kind is the Go type of the
	// value, which here is the type of a wrapped error and not its text.
	for _, want := range []string{"site=test.site", "channel=42", "panics=", "where=guard/guard_test.go:"} {
		if !strings.Contains(line, want) {
			t.Fatalf("%s is missing from the line: %q", want, line)
		}
	}
	// Measured, not assumed: fmt.Errorf without %w gives *errors.errorString,
	// and the type is all that is left of the value.
	if !strings.Contains(line, "kind=*errors.errorString") {
		t.Fatalf("the kind of the value is missing: %q", line)
	}
	if !strings.Contains(line, "level=ERROR") {
		t.Fatalf("a caught crash was not logged at ERROR: %q", line)
	}
}

// TestTheLineNamesTheLineThatPanicked holds the one thing that replaces the
// stack: without a place, a caught panic is a defect nobody can find again.
// The line number is read off the panicking frame, so a case that only looked
// for the file name would pass for a guard that printed its own file.
func TestTheLineNamesTheLineThatPanicked(t *testing.T) {
	buf := catchLog(t)
	var p *int
	func() {
		defer func() { _ = Hit(recover(), "test.deref", 1) }()
		_ = *p // the panicking line
	}()
	line := buf.String()
	// The frame of the panic is in this file, and it is NOT the frame of
	// guard.go: a guard that printed its own place would be useless.
	if !strings.Contains(line, "guard/guard_test.go:") {
		t.Fatalf("the place is not the panicking file: %q", line)
	}
	if strings.Contains(line, "guard/guard.go:") {
		t.Fatalf("the place is the guard's own file: %q", line)
	}
	if !strings.Contains(line, "kind=runtime.errorString") {
		t.Fatalf("the kind of a runtime error is missing: %q", line)
	}
	// And the runtime's own frames are not the place either: before this was
	// measured the line said where=runtime/panic.go:336 for every nil pointer.
	if strings.Contains(line, "runtime/panic.go") {
		t.Fatalf("the place is a frame of the Go runtime: %q", line)
	}
}

// TestEveryCatchCounts answers "a caught error that is only in the log is one
// nobody finds": the number is the second way it shows, and it counts every
// catch and not every kind.
func TestEveryCatchCounts(t *testing.T) {
	_ = catchLog(t)
	before := Panics()
	for i := 0; i < 3; i++ {
		func() {
			defer func() { _ = Hit(recover(), "test.count", uint32(i)) }()
			panic("x")
		}()
	}
	if got := Panics(); got != before+3 {
		t.Fatalf("three catches counted %d, not %d", got-before, 3)
	}
}

// TestTheShortPathKeepsTwoParts holds that the build directory of the machine
// stays out of the log -- a path of a developer's disk in a line somebody
// passes on is the same class of leak as a value.
func TestTheShortPathKeepsTwoParts(t *testing.T) {
	for in, want := range map[string]string{
		"/home/someone/secretproject/apps/server-api/classes/guard/guard.go": "guard/guard.go",
		`D:\claude\ngc\apps\server-api\classes\relay\relay.go`:               "relay/relay.go",
		"guard.go": "guard.go",
		"a/b.go":   "a/b.go",
	} {
		if got := shortPath(in); got != want {
			t.Fatalf("shortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReportSaysTheTotalOnlyWhenItGrew is the case for the second way a catch
// shows itself. Both halves matter and the silent one matters more: a line
// every minute saying "none" is noise, and noise is how a log stops being
// read.
//
// The cases of this package share one process and therefore one counter, so
// this one never asserts an absolute number -- it asserts the DIFFERENCE it
// made itself. The first call also swallows whatever earlier cases counted,
// which is why it is made before the panic and its line thrown away.
func TestReportSaysTheTotalOnlyWhenItGrew(t *testing.T) {
	buf := catchLog(t)
	Report() // clears whatever the cases before this one left standing
	buf.Reset()

	// Nothing has happened since: the round says nothing at all.
	Report()
	if buf.Len() != 0 {
		t.Fatalf("a round with no new panic wrote a line: %q", buf.String())
	}

	before := Panics()
	func() {
		defer func() { _ = Hit(recover(), "test.report", 3) }()
		panic(fmt.Errorf("the bearer token is %s", secret))
	}()
	total := Panics()
	if total != before+1 {
		t.Fatalf("the counter went %d -> %d, want one more", before, total)
	}
	buf.Reset() // the ERROR line of the catch is not what is measured here

	Report()
	line := buf.String()
	for _, want := range []string{
		"level=WARN", fmt.Sprintf("panics=%d", total), "new=1",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("%s is missing from the round's line: %q", want, line)
		}
	}
	// The same rule as for the line of a catch: a total is two numbers, and
	// nothing of the value that was panicked with comes along.
	if strings.Contains(line, secret) || strings.Contains(line, "bearer token") {
		t.Fatalf("the value of the panic reached the round's line: %q", line)
	}
	// And a total belongs to no site, so neither name goes in.
	if strings.Contains(line, "test.report") || strings.Contains(line, "channel=") {
		t.Fatalf("the round's line names a place or a channel: %q", line)
	}

	// Reported once is reported: the next round is silent again.
	buf.Reset()
	Report()
	if buf.Len() != 0 {
		t.Fatalf("the same panic was reported twice: %q", buf.String())
	}
}
