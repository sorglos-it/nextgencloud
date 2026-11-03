package serve

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// lines keeps what this package wrote, with the attributes. The notes handler
// of the other cases only counts, and a count cannot say whether the reason an
// operator reads is the right one, or whether it comes from the closed list at
// all.
type lines struct {
	mu   sync.Mutex
	recs []map[string]string
}

func (l *lines) Enabled(context.Context, slog.Level) bool { return true }

func (l *lines) Handle(_ context.Context, r slog.Record) error {
	m := map[string]string{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	l.recs = append(l.recs, m)
	l.mu.Unlock()
	return nil
}

func (l *lines) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *lines) WithGroup(string) slog.Handler      { return l }

// cuts are the lines about a channel that was cut, in the order they were
// written.
func (l *lines) cuts() []map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]string
	for _, r := range l.recs {
		if r["grund"] != "" {
			out = append(out, r)
		}
	}
	return out
}

// onlyCut is the one line with a reason, or a failure that says how many there
// were instead.
func onlyCut(t *testing.T, l *lines) map[string]string {
	t.Helper()
	c := l.cuts()
	if len(c) != 1 {
		t.Fatalf("%d lines with a reason were written, want exactly one: %v", len(c), c)
	}
	return c[0]
}

// deadEngine is an engine that cannot be reached: the question whether the
// container runs already fails. That is what a podman which cannot find its
// runtime directory looks like from here.
type deadEngine struct{ noEngine }

func (deadEngine) Running(context.Context, string) (bool, error) {
	return false, errors.New("engine: podman could not be run")
}

func (deadEngine) Exec(context.Context, runtime.Exec) (int, error) {
	panic("a container that cannot be reached runs nothing")
}

// orderEngine notes when it was asked anything, so a case can hold the ORDER
// of the preparation and the first question to the engine.
type orderEngine struct {
	*appEngine
	note func(string)
}

func (e *orderEngine) Running(ctx context.Context, name string) (bool, error) {
	e.note("running")
	return e.appEngine.Running(ctx, name)
}

func (e *orderEngine) Start(ctx context.Context, name string, l runtime.Limits) error {
	e.note("start")
	return e.appEngine.Start(ctx, name, l)
}

// A runtime directory that cannot be prepared ends the channel unreachable AND
// says so in the journal.
//
// This is the fault measured on the Debian machine on 22.10.2026: the machine
// had rebooted at 04:44, /run/ngc held nothing but itself, and every podman
// call of the installed app failed on a directory that was not there. What the
// person in the browser saw was "Die Verbindung wurde abgeschnitten."; what
// the journal of the controller held was nothing at all.
func TestARuntimeDirectoryThatCannotBePreparedSaysSo(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	l := &lines{}
	w.rebuild(func(d *Deps) {
		d.Log = slog.New(l)
		d.Prepare = func(context.Context, string) error {
			return errors.New("runtime: mkdir /run/ngc/ngc-terminal-439982dd: read-only file system")
		}
	})
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EEnd, nil)
		_, end := sh.out()
		wantCode(t, end, wire.CodeUnreachable)
		wantOK(t, sh.rest(), "the CLOSE of the channel")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	// The engine was never asked. A container cannot be reached through a
	// runtime directory that is not there, and the error of asking anyway was
	// the only trace this fault used to leave.
	if n := w.eng.starts.Load(); n != 0 {
		t.Errorf("the container was started %d times, want none without a runtime directory", n)
	}
	r := onlyCut(t, l)
	if r["grund"] != ReasonRunDir {
		t.Errorf("grund = %q, want %q", r["grund"], ReasonRunDir)
	}
	if r["app"] != testApp {
		t.Errorf("app = %q, want %q", r["app"], testApp)
	}
	if r["code"] != string(wire.CodeUnreachable) {
		t.Errorf("code = %q, want %q", r["code"], wire.CodeUnreachable)
	}
	// And nothing of the cause itself: the text of a system call names paths
	// of this machine, and a line an operator reads carries the closed list
	// and nothing else.
	for k, v := range r {
		if strings.Contains(v, "read-only file system") {
			t.Errorf("the attribute %q carried the text of the cause: %q", k, v)
		}
	}
}

// The runtime directory is prepared BEFORE the engine is asked anything, on
// every app channel - not only on one whose container is down. A container
// that runs was started before the boot that took the directory away, so "it
// is running" is no proof that podman can be reached for it.
func TestEveryAppChannelPreparesTheRuntimeDirectoryFirst(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	w.eng.running = true
	w.eng.run = says("fertig")
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	w.rebuild(func(d *Deps) {
		d.Prepare = func(context.Context, string) error {
			note("prepare")
			return nil
		}
		d.Engine = &orderEngine{appEngine: w.eng, note: note}
	})
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EEnd, nil)
		_, end := sh.out()
		wantOK(t, end, "the end-to-end end")
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 {
		t.Fatalf("order = %v, want the preparation and at least one question to the engine", order)
	}
	if order[0] != "prepare" {
		t.Errorf("order = %v, want the preparation before the first question to the engine", order)
	}
}

// A container that cannot be reached says that, and says it as its own reason:
// an operator who reads "the container will not start" looks somewhere else
// than one who reads "the runtime directory could not be prepared".
func TestAContainerThatCannotBeReachedSaysSo(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	l := &lines{}
	w.rebuild(func(d *Deps) {
		d.Log = slog.New(l)
		d.Engine = deadEngine{}
	})
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EEnd, nil)
		_, end := sh.out()
		wantCode(t, end, wire.CodeUnreachable)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if r := onlyCut(t, l); r["grund"] != ReasonContainer {
		t.Errorf("grund = %q, want %q", r["grund"], ReasonContainer)
	}
}

// A service that ends with an error leaves a line too: the channel the person
// in the browser sees as cut is this one as well, and its reason is a
// different one again.
func TestAServiceThatFailsSaysSo(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	l := &lines{}
	w.rebuild(func(d *Deps) { d.Log = slog.New(l) })
	w.eng.run = func(context.Context, runtime.Exec) (int, error) { return 3, nil }
	payload, hs := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EEnd, nil)
		_, end := sh.out()
		if end.Status == wire.StatusOK {
			t.Errorf("the end was ok, want the error of a service that ended with 3")
		}
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if r := onlyCut(t, l); r["grund"] != ReasonService {
		t.Errorf("grund = %q, want %q", r["grund"], ReasonService)
	}
}

// A channel leaves ONE line, even when the end of its refusal cannot be sent
// any more.
//
// This is the case the CI found on 23.10.2026 under -race on Linux, where the
// timing made it happen and on Windows it did not: the container could not be
// reached, the reason went into the journal - and behind it a SECOND line,
// E_INTERNAL "could not be carried out server-side", because answer could not
// get that very refusal out to a shell that was already gone. Two lines for
// one channel, and the second one sends an operator to look in the wrong
// place.
//
// It is built here without a clock: the shell gives the controller a window of
// exactly one message, the service fills it, and the end of this side then has
// no credit left to travel on - the same way
// TestAnEndThatGetsNoCreditStillClosesTheChannel builds it. The service also
// ends with an error, so there IS a first line to be the only one.
func TestACutChannelLeavesOneLineEvenWhenItsEndCannotGoOut(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	l := &lines{}
	w.rebuild(func(d *Deps) { d.Log = slog.New(l) })
	w.quickAction(t, 1)
	w.srv.last = 100 * time.Millisecond
	// One whole message of output, so the credit of the shell is spent, and an
	// error at the end of it, so the service has a reason of its own.
	w.eng.run = func(_ context.Context, e runtime.Exec) (int, error) {
		if e.Stdout != nil {
			io.WriteString(e.Stdout, strings.Repeat("e", wire.MinWindow-noisep.TagSize-1))
		}
		return 3, nil
	}
	payload, hs := w.appOpenWin(t, nil, wire.MinWindow)
	ended := w.tunnel(t, func(a *apiSide) {
		sh := start(t, a, 1, payload, hs)
		sh.inner(wire.E2EData, nil)
		sh.inner(wire.E2EEnd, nil)
		if n := sh.one(); n != wire.MinWindow {
			t.Fatalf("the first frame was %d bytes, want the %d of a whole message", n, wire.MinWindow)
		}
		wantCode(t, sh.rest(), wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	// ONE line, and it is the reason of the service - not the internal one of
	// the end that could not be sent.
	r := onlyCut(t, l)
	if r["grund"] != ReasonService {
		t.Errorf("grund = %q, want the reason of the service %q", r["grund"], ReasonService)
	}
}

// Nothing is written for a channel that never got past its first inner
// message, however often such a channel arrives: which channels arrive is the
// API's to choose, so a line per OPEN would be a peer's to write (see the doc
// of this package and of reason.go).
func TestNothingIsWrittenBeforeTheFirstInnerMessage(t *testing.T) {
	envHere(t)
	w := newWorld(t)
	l := &lines{}
	w.rebuild(func(d *Deps) { d.Log = slog.New(l) })
	w.srv.first = 50 * time.Millisecond
	// One OPEN per channel: the handshake of a shell is good for one channel,
	// and a case that reused it would measure the handshake and not the log.
	var opens [][]byte
	var shakes []*noise.HandshakeState
	for i := 0; i < 3; i++ {
		payload, hs := w.appOpen(t, nil)
		opens, shakes = append(opens, payload), append(shakes, hs)
	}
	ended := w.tunnel(t, func(a *apiSide) {
		for num := uint32(1); num <= 3; num++ {
			sh := start(t, a, num, opens[num-1], shakes[num-1])
			// No inner message at all: after the wait the channel is over.
			wantCode(t, sh.rest(), wire.CodeTimeout)
			a.shut(num)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if c := l.cuts(); len(c) != 0 {
		t.Errorf("%d lines were written for channels that reached no shell, want none: %v", len(c), c)
	}
}

// Every reason is from the closed list, is German, is short enough for a
// journal and carries no place for anybody else's text. This is the line
// appstore/sources.go holds for the reasons it shows an administrator.
func TestEveryReasonIsFromTheClosedList(t *testing.T) {
	if len(reasons) == 0 {
		t.Fatal("the list of reasons is empty")
	}
	seen := map[string]bool{}
	for _, r := range reasons {
		if seen[r] {
			t.Errorf("the reason %q is in the list twice", r)
		}
		seen[r] = true
		if len(r) > 120 {
			t.Errorf("the reason %q is %d bytes long, want at most 120", r, len(r))
		}
		if !strings.HasSuffix(r, ".") {
			t.Errorf("the reason %q is no sentence", r)
		}
		if strings.ContainsAny(r, "%{}<>\n\t") {
			t.Errorf("the reason %q holds a place for somebody else's text", r)
		}
		for _, c := range r {
			if !unicode.IsPrint(c) {
				t.Errorf("the reason %q holds a character that is not printable", r)
				break
			}
		}
	}
	// And every reason the code hands over is one of them. A reason added to
	// the code and not to the list falls here; the cases above are what says
	// that the right one of them reaches the journal.
	for _, r := range []string{ReasonPlan, ReasonRunDir, ReasonContainer, ReasonTooLarge,
		ReasonCut, ReasonService, ReasonInternal} {
		if !slices.Contains(reasons, r) {
			t.Errorf("the reason %q is used but not in the list", r)
		}
	}
	if len(reasons) != 7 {
		t.Errorf("the list holds %d reasons, want the 7 this case names", len(reasons))
	}
}
