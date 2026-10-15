// What is measured here and what is not.
//
// Everything of this app that does not need a pseudo terminal is measured on
// any machine: the frames, the two pumps, the idle clock, the watchdog and the
// three refusals of run. The pty itself is Linux only, so the two cases of the
// plan that drive a real shell (TestRunEchoesThroughAPTY,
// TestResizeIsAppliedNotEchoed) skip LOUDLY on anything else - with the reason
// and the date, so the next reader can see whether it is still true.
//
// The thing a terminal is dangerous for - who gets to open one - is not
// decided in this package at all. It is measured in manifest_test.go against
// the manifest that is shipped.
package terminal

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// marker is the stand-in for what a user types into a terminal. It is SHORT on
// purpose: a logger that throws a long detail away would let a mutation that
// writes the whole line through, and the case would still be green.
const marker = "pw1"

// keys and resize build the two frames of the protocol.
func keys(b []byte) []byte {
	f := []byte{KindKeys, 0, 0}
	binary.BigEndian.PutUint16(f[1:3], uint16(len(b)))
	return append(f, b...)
}

func resize(cols, rows uint16) []byte {
	f := []byte{KindResize, 0, resizeSize, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(f[3:5], cols)
	binary.BigEndian.PutUint16(f[5:7], rows)
	return f
}

// clock is hand-wound, so nothing here waits for a real tick and no limit here
// has to stay above what the clock of this machine can resolve
// (docs/arbeitsweise.md, rule 5: a test that measures 30 microseconds with a
// millisecond clock can never fail).
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func TestParseFrameTakesTheTwoKinds(t *testing.T) {
	t.Run("keys", func(t *testing.T) {
		f, err := ParseFrame(keys([]byte(marker)))
		if err != nil || f.Kind != KindKeys || string(f.Data) != marker {
			t.Fatalf("got %+v, %v", f, err)
		}
	})
	t.Run("the longest keys frame", func(t *testing.T) {
		f, err := ParseFrame(keys(bytes.Repeat([]byte{'a'}, MaxKeys)))
		if err != nil || len(f.Data) != MaxKeys {
			t.Fatalf("got %d bytes, %v", len(f.Data), err)
		}
	})
	t.Run("resize", func(t *testing.T) {
		f, err := ParseFrame(resize(80, 24))
		if err != nil || f.Kind != KindResize || f.Cols != 80 || f.Rows != 24 || f.Data != nil {
			t.Fatalf("got %+v, %v", f, err)
		}
	})
	// Both sides of the two borders, so neither bound could sit anywhere else:
	// one byte in is taken, one byte out is refused (the refusals are in
	// TestParseFrameRefusesGarbage).
	t.Run("the largest window", func(t *testing.T) {
		if _, err := ParseFrame(resize(MaxDim, MaxDim)); err != nil {
			t.Fatalf("%d x %d: %v", MaxDim, MaxDim, err)
		}
	})
	t.Run("the smallest window", func(t *testing.T) {
		if _, err := ParseFrame(resize(1, 1)); err != nil {
			t.Fatalf("1 x 1: %v", err)
		}
	})
}

func TestParseFrameRefusesGarbage(t *testing.T) {
	long := keys(bytes.Repeat([]byte{'a'}, MaxKeys+1))
	cases := []struct {
		name string
		b    []byte
	}{
		{"nothing at all", nil},
		{"half a header", []byte{KindKeys, 0}},
		{"unknown kind 0", []byte{0, 0, 0}},
		{"unknown kind 3", []byte{3, 0, 1, 'a'}},
		{"keys without a byte", []byte{KindKeys, 0, 0}},
		{"length above the buffer", []byte{KindKeys, 0, 4, 'a'}},
		{"buffer above the length", []byte{KindKeys, 0, 1, 'a', 'b'}},
		{"keys one byte too long", long},
		{"resize with three bytes", []byte{KindResize, 0, 3, 0, 80, 0}},
		{"resize with five bytes", []byte{KindResize, 0, 5, 0, 80, 0, 24, 0}},
		{"no columns", resize(0, 24)},
		{"no rows", resize(80, 0)},
		{"one column too many", resize(MaxDim+1, 24)},
		{"one row too many", resize(80, MaxDim+1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if f, err := ParseFrame(c.b); !errors.Is(err, ErrFrame) {
				t.Fatalf("got %+v, %v, want ErrFrame", f, err)
			}
		})
	}
}

// TestParseFrameCopiesItsData holds the one line that keeps a frame from
// aliasing the read buffer of the pump. The mutation that drops bytes.Clone
// makes this red: the second frame writes over the data of the first.
func TestParseFrameCopiesItsData(t *testing.T) {
	buf := keys([]byte(marker))
	f, err := ParseFrame(buf)
	if err != nil {
		t.Fatal(err)
	}
	copy(buf[HeaderSize:], "xxx")
	if string(f.Data) != marker {
		t.Fatalf("data moved with the buffer: %q", f.Data)
	}
}

func TestEncodeOutputPutsNoFrameAround(t *testing.T) {
	one := []byte{'x'}
	if got := EncodeOutput(one); len(got) != 1 || got[0] != 'x' {
		t.Errorf("one byte came back as %q", got)
	}
	if got := EncodeOutput(bytes.Repeat(one, MaxOutput)); len(got) != MaxOutput {
		t.Errorf("the largest write came back as %d bytes", len(got))
	}
	if got := EncodeOutput(nil); got != nil {
		t.Errorf("nothing came back as %q", got)
	}
	if got := EncodeOutput(bytes.Repeat(one, MaxOutput+1)); got != nil {
		t.Errorf("one byte too many came back as %d bytes", len(got))
	}
}

// pumpFixture runs pumpIn over b and reports what reached the shell and the
// sizes that were applied.
type pumpFixture struct {
	keys  bytes.Buffer
	sizes [][2]uint16
	err   error
}

func runIn(t testing.TB, b []byte) *pumpFixture {
	t.Helper()
	p := &pumpFixture{}
	act := newActivity(0, time.Now)
	p.err = pumpIn(bytes.NewReader(b), &p.keys, func(cols, rows uint16) error {
		p.sizes = append(p.sizes, [2]uint16{cols, rows})
		return nil
	}, act)
	return p
}

// TestPumpInAppliesAResizeAndDoesNotEchoIt is the plan's
// TestResizeIsAppliedNotEchoed without a pty: the four bytes of the size reach
// the ioctl and not the shell.
func TestPumpInAppliesAResizeAndDoesNotEchoIt(t *testing.T) {
	p := runIn(t, append(resize(80, 24), keys([]byte(marker))...))
	if p.err != nil {
		t.Fatalf("pumpIn: %v", p.err)
	}
	if len(p.sizes) != 1 || p.sizes[0] != [2]uint16{80, 24} {
		t.Fatalf("sizes %v, want one 80x24", p.sizes)
	}
	// The keys arrived, so the stream really was read past the resize - and
	// only the keys arrived.
	if got := p.keys.String(); got != marker {
		t.Fatalf("the shell got %q, want %q and nothing of the size", got, marker)
	}
}

func TestPumpInEndsCleanlyBetweenFrames(t *testing.T) {
	p := runIn(t, keys([]byte(marker)))
	if p.err != nil {
		t.Fatalf("a complete input ended with %v", p.err)
	}
}

// TestPumpInRefusesAFrameThatIsNone also carries the intermediate check this
// measurement needs: the frame before the garbage arrived, so the stream was
// open as far as the line under test.
func TestPumpInRefusesAFrameThatIsNone(t *testing.T) {
	p := runIn(t, append(keys([]byte(marker)), 0, 0, 0))
	if !errors.Is(p.err, ErrFrame) {
		t.Fatalf("got %v, want ErrFrame", p.err)
	}
	if p.keys.String() != marker {
		t.Fatalf("the way to the bad frame was not open: the shell got %q", p.keys.String())
	}
}

func TestPumpInReportsACutFrame(t *testing.T) {
	t.Run("cut in the header", func(t *testing.T) {
		if p := runIn(t, []byte{KindKeys, 0}); !errors.Is(p.err, errCut) {
			t.Fatalf("got %v, want errCut", p.err)
		}
	})
	t.Run("cut in the payload", func(t *testing.T) {
		if p := runIn(t, []byte{KindResize, 0, resizeSize, 0, 80}); !errors.Is(p.err, errCut) {
			t.Fatalf("got %v, want errCut", p.err)
		}
	})
}

// TestPumpInRefusesALengthAboveMaxKeys reaches the bound that stands before
// anything is read for the frame, so a length nobody can fill cannot make this
// side wait for bytes that will never come.
func TestPumpInRefusesALengthAboveMaxKeys(t *testing.T) {
	head := []byte{KindKeys, 0, 0}
	binary.BigEndian.PutUint16(head[1:3], uint16(MaxKeys+1))
	if p := runIn(t, head); !errors.Is(p.err, ErrFrame) {
		t.Fatalf("got %v, want ErrFrame", p.err)
	}
}

// byteReader hands out one byte per read, the way a pty does: the output pump
// then really writes more than once, which is what the case about a broken
// channel needs.
type byteReader struct{ b []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	p[0] = r.b[0]
	r.b = r.b[1:]
	return 1, nil
}

// errWriter breaks after the first write, the way a channel does that ends
// while the shell is still answering.
type errWriter struct {
	n   int
	err error
}

func (w *errWriter) Write(b []byte) (int, error) {
	w.n++
	if w.n > 1 {
		return 0, w.err
	}
	return len(b), nil
}

// TestPumpOutStopsWhenTheChannelBreaks: a write that fails ends the run. The
// first write went through, so the error is the second one and not a pump that
// never started.
func TestPumpOutStopsWhenTheChannelBreaks(t *testing.T) {
	broken := errors.New("the channel ended")
	w := &errWriter{err: broken}
	err := pumpOut(w, &byteReader{b: []byte("ab")}, newActivity(0, time.Now))
	if !errors.Is(err, broken) {
		t.Fatalf("got %v, want the error of the write", err)
	}
	if w.n != 2 {
		t.Fatalf("%d writes, want the first one through and the second one broken", w.n)
	}
}

// eioReader answers the way a pty does once its child is gone: not io.EOF but
// an error of the device.
type eioReader struct{ done bool }

func (r *eioReader) Read(b []byte) (int, error) {
	if r.done {
		return 0, errors.New("input/output error")
	}
	r.done = true
	return copy(b, "hallo"), nil
}

func TestPumpOutEndsOnTheEndOfThePTY(t *testing.T) {
	var out bytes.Buffer
	if err := pumpOut(&out, &eioReader{}, newActivity(0, time.Now)); err != nil {
		t.Fatalf("the end of a pty came back as %v", err)
	}
	if out.String() != "hallo" {
		t.Fatalf("output %q", out.String())
	}
}

// TestActivityGoesStaleOnlyAfterTheLimit nails both sides of the border: one
// tick under the limit it is fresh, at the limit it is stale. A test that only
// measured the overshoot would stay green with the limit at zero.
func TestActivityGoesStaleOnlyAfterTheLimit(t *testing.T) {
	c := &clock{t: time.Unix(1790000000, 0)}
	act := newActivity(time.Minute, c.now)
	c.add(time.Minute - time.Nanosecond)
	if act.stale() {
		t.Error("stale one nanosecond before the limit")
	}
	c.add(time.Nanosecond)
	if !act.stale() {
		t.Error("not stale at the limit")
	}
	act.touch()
	if act.stale() {
		t.Error("still stale after a byte went through")
	}
}

func TestActivityWithoutALimitNeverGoesStale(t *testing.T) {
	c := &clock{t: time.Unix(1790000000, 0)}
	act := newActivity(0, c.now)
	c.add(1000 * time.Hour)
	if act.stale() {
		t.Error("a limit of zero went stale")
	}
}

// TestWatchAnswersIdle drives the watchdog with a clock that is already past
// the limit, so it answers on its first tick and nothing here waits.
//
// The deadline is not decoration. Measured while mutating: without it the
// mutant that drops the answer does not make this case FALL, it makes it HANG
// until go test gives up ten minutes later - and a hang is no catch
// (docs/arbeitsweise.md, "Hängen ist kein Fallen"). With the deadline that
// mutant is red in two seconds, and the error it then answers is the context
// and not errIdle, so the case still says the right thing.
func TestWatchAnswersIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := &clock{t: time.Unix(1790000000, 0)}
	act := newActivity(4*time.Millisecond, c.now)
	c.add(time.Hour)
	if err := watch(ctx, act); !errors.Is(err, errIdle) {
		t.Fatalf("got %v, want errIdle", err)
	}
}

func TestWatchEndsWithItsContext(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	stop()
	for _, limit := range []time.Duration{0, time.Hour} {
		if err := watch(ctx, newActivity(limit, time.Now)); !errors.Is(err, context.Canceled) {
			t.Fatalf("limit %v: got %v, want the cancelled context", limit, err)
		}
	}
}

// TestIdleLimitIsHalfAnHour holds the default nothing else measures: every
// case above brings its own limit, so this constant is the one number that
// would otherwise be free to be anything. It holds the order of magnitude and
// not the exact value, so a later adjustment does not break it.
func TestIdleLimitIsHalfAnHour(t *testing.T) {
	if IdleLimit < 5*time.Minute || IdleLimit > time.Hour {
		t.Fatalf("IdleLimit is %v, want minutes and not seconds or days", IdleLimit)
	}
	if maxTick <= 0 || maxTick > IdleLimit {
		t.Fatalf("maxTick is %v against a limit of %v", maxTick, IdleLimit)
	}
}

// goodEnv is the environment of a call that got through the gate.
func goodEnv(t *testing.T) {
	t.Helper()
	t.Setenv(service.EnvData, service.DataDir)
	t.Setenv(service.EnvStart, "0")
	t.Setenv(service.EnvUser, trusttest.ID(0x21))
}

// TestRunRefusesWithoutNGCData is the plan's case: without the environment of
// a service this program is not in the container of its app.
func TestRunRefusesWithoutNGCData(t *testing.T) {
	for _, data := range []string{"", "/tmp", service.DataDir + "/x"} {
		var errOut bytes.Buffer
		goodEnv(t)
		t.Setenv(service.EnvData, data)
		if code := run(context.Background(), strings.NewReader(""), io.Discard, &errOut, 0); code == 0 {
			t.Fatalf("NGC_DATA=%q: exit 0, want a failure", data)
		}
		if !strings.Contains(errOut.String(), msgNoData) {
			t.Fatalf("NGC_DATA=%q said %q", data, errOut.String())
		}
	}
}

// TestRunRefusesWithoutAUser is the fence inside the container. The gate in
// front of it is measured in manifest_test.go; this is what answers if
// something ever starts this program past that gate.
func TestRunRefusesWithoutAUser(t *testing.T) {
	cases := map[string]string{
		"no user at all":  "",
		"not an ID":       "admin",
		"an ID too short": "AAAA",
		"an ID too long":  trusttest.ID(0x21) + "AAAA",
	}
	for name, user := range cases {
		t.Run(name, func(t *testing.T) {
			var errOut bytes.Buffer
			goodEnv(t)
			t.Setenv(service.EnvUser, user)
			if code := run(context.Background(), strings.NewReader(""), io.Discard, &errOut, 0); code == 0 {
				t.Fatal("exit 0, want a failure")
			}
			if !strings.Contains(errOut.String(), msgNoUser) {
				t.Fatalf("said %q", errOut.String())
			}
		})
	}
}

// TestRunRefusesAStartAction: a start action runs without a user
// (service.Call.Validate), and a shell with nobody behind it is the one thing
// this app must never be - even with a user ID in the environment beside it.
func TestRunRefusesAStartAction(t *testing.T) {
	var errOut bytes.Buffer
	goodEnv(t)
	t.Setenv(service.EnvStart, "1")
	if code := run(context.Background(), strings.NewReader(""), io.Discard, &errOut, 0); code == 0 {
		t.Fatal("exit 0, want a failure")
	}
	if !strings.Contains(errOut.String(), msgNoUser) {
		t.Fatalf("said %q", errOut.String())
	}
}

// TestNothingOfTheChannelReachesAMessage is the case about secrets. A terminal
// sees everything, so nothing of it may end up in a line this program writes.
// It measures with a SHORT marker: a cleaner that drops a long detail would
// let the mutation "the whole line into the message" pass with a long one.
func TestNothingOfTheChannelReachesAMessage(t *testing.T) {
	// The three refusals of run, each with the marker as the input of the
	// channel.
	t.Run("the refusals", func(t *testing.T) {
		for _, strip := range []string{service.EnvData, service.EnvUser} {
			var errOut bytes.Buffer
			goodEnv(t)
			t.Setenv(strip, "")
			run(context.Background(), bytes.NewReader(keys([]byte(marker))), io.Discard, &errOut, 0)
			if strings.Contains(errOut.String(), marker) {
				t.Fatalf("%s: the input reached the message %q", strip, errOut.String())
			}
		}
	})
	// The errors of the pumps, which are what a caller may log.
	t.Run("the errors of the pumps", func(t *testing.T) {
		bad := append([]byte{0, 0, byte(len(marker))}, marker...)
		p := runIn(t, bad)
		if p.err == nil || strings.Contains(p.err.Error(), marker) {
			t.Fatalf("pumpIn answered %v", p.err)
		}
		w := &errWriter{err: errors.New("the channel ended")}
		err := pumpOut(w, &byteReader{b: []byte(marker + marker)}, newActivity(0, time.Now))
		if err == nil || strings.Contains(err.Error(), marker) {
			t.Fatalf("pumpOut answered %v", err)
		}
	})
	// And the lines themselves: a constant with a verb in it is a line that
	// would carry a value the next time somebody touches it.
	t.Run("the lines carry no value", func(t *testing.T) {
		for _, m := range []string{msgNoData, msgNoUser, msgNoPTY, msgNoRun, msgIdle, msgCut} {
			if strings.Contains(m, "%") {
				t.Errorf("%q carries a place for a value", m)
			}
		}
	})
}

// TestRunSaysSoWhereThereIsNoPTY: on this machine the shell cannot be reached
// at all, and that is said out loud instead of passing for a success.
func TestRunSaysSoWhereThereIsNoPTY(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("linux has a pty: TestRunEchoesThroughAPTY measures the shell itself")
	}
	var errOut bytes.Buffer
	goodEnv(t)
	if code := run(context.Background(), strings.NewReader(""), io.Discard, &errOut, 0); code == 0 {
		t.Fatal("exit 0 without a pty")
	}
	if !strings.Contains(errOut.String(), msgNoPTY) {
		t.Fatalf("said %q", errOut.String())
	}
}

// skipWithoutPTY says out loud what is not measured here and why, with the
// date, so whoever reads it can see whether the reason still holds.
func skipWithoutPTY(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("NICHT GEMESSEN auf %s: /dev/ptmx gibt es nur unter Linux (12.10.2026). "+
			"Dieser Fall ist auf einem Linux-Server nachzuholen.", runtime.GOOS)
	}
}

// TestRunEchoesThroughAPTY drives the real shell: "echo hallo" goes in as a
// keys frame, "hallo" comes out, and "exit" ends it.
func TestRunEchoesThroughAPTY(t *testing.T) {
	skipWithoutPTY(t)
	goodEnv(t)
	in := append(keys([]byte("echo hallo\n")), keys([]byte("exit\n"))...)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run(context.Background(), bytes.NewReader(in), &out, &errOut, time.Minute)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "hallo") {
		t.Fatalf("output %q, want hallo in it", out.String())
	}
}

// TestResizeIsAppliedNotEchoed drives the real shell too: stty reads the size
// off the pty, so the four bytes reached the ioctl - and they are not in the
// output.
func TestResizeIsAppliedNotEchoed(t *testing.T) {
	skipWithoutPTY(t)
	goodEnv(t)
	in := append(resize(80, 24), keys([]byte("stty size\nexit\n"))...)
	var out bytes.Buffer
	if code := run(context.Background(), bytes.NewReader(in), &out, io.Discard, time.Minute); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "24 80") {
		t.Fatalf("output %q, want the size 24 80 in it", out.String())
	}
	if bytes.Contains(out.Bytes(), []byte{0, 80, 0, 24}) {
		t.Fatal("the four bytes of the size were echoed")
	}
}

// TestHoldEndsWithItsSignal: hold is what keeps the container of the app alive
// between two channels. It ends when the engine stops the container, which is
// what cancels its context, and that is no error.
func TestHoldEndsWithItsSignal(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if code := Hold(ctx); code != 0 {
		t.Fatalf("Hold = %d, want 0", code)
	}
}

// TestShellEnvCarriesNothing is the bolt under the decision that the
// parameters of ngc.open are decoration (see the head of terminal.go): the
// environment of the shell is four fixed lines plus HOME, so neither a cols
// nor a rows nor anything else of NGC_PARAMS has a way into this program.
//
// It poisons EVERY variable a service gets - the whole list of
// shared/service - with a value that would be visible if it travelled, and
// then holds two things: the four lines are exactly the four lines, and no
// poisoned value is anywhere in the result. A mutation that hands the process
// environment on (append(..., os.Environ()...)), or that lets NGC_PARAMS
// through, or that takes TERM out of the environment instead of fixing it,
// dies on this.
//
// HOME is the one value read out of the environment, and that is not a hole:
// it is where the shell starts and it is set by the engine, not by the user.
// The second part of the case holds the fallback for an empty one.
func TestShellEnvCarriesNothing(t *testing.T) {
	const poison = "poison-9e3f"
	for _, k := range []string{
		service.EnvUser, service.EnvUserName, service.EnvApp, service.EnvService,
		service.EnvAction, service.EnvParams, service.EnvVersion, service.EnvData,
		"HISTFILE", "HISTFILESIZE", "HISTSIZE",
		service.EnvStart, "TERM", "PATH", "LANG", "SHELL", "COLUMNS", "LINES",
	} {
		t.Setenv(k, poison)
	}
	// Set last, so it is not one of the poisoned ones above.
	t.Setenv("HOME", "/data/app")
	want := []string{
		"TERM=xterm-256color",
		"HOME=/data/app",
		"PATH=" + service.Path,
		"LANG=C.UTF-8",
		"HISTFILE=",
	}
	got := shellEnv()
	if len(got) != len(want) {
		// The NAMES, never the whole slice: a mutation that hands the process
		// environment on would print every secret of the machine that runs
		// this into the log of the run, which is a second leak on top of the
		// one the case is about. Measured on 12.10.2026 with mutant m1 of
		// this bolt - a token stood in the output.
		names := make([]string, 0, len(got))
		for _, e := range got {
			names = append(names, strings.SplitN(e, "=", 2)[0])
		}
		t.Fatalf("shellEnv() names %d variables (%q), want the %d fixed lines",
			len(got), names, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shellEnv()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Said twice on purpose: the comparison above would also hold if a sixth
	// line had been dropped by a mutation of len. This holds the VALUE, which
	// is what a user would see in the process list.
	for _, e := range got {
		if strings.Contains(e, poison) {
			t.Fatalf("shellEnv() carries %q in %q", poison, e)
		}
	}
	// And the one value that does come out of the environment falls back to
	// the data directory rather than to nothing: a shell with HOME= starts in
	// a directory that is not there.
	t.Setenv("HOME", "")
	if got := shellEnv(); got[1] != "HOME="+service.DataDir {
		t.Fatalf("shellEnv() with an empty HOME = %q, want HOME=%s", got[1], service.DataDir)
	}
}

func FuzzParseFrame(f *testing.F) {
	f.Add(keys([]byte("a")))
	f.Add(resize(80, 24))
	f.Add([]byte{1, 'a'})
	f.Add([]byte{2, 0, 80, 0, 24})
	f.Add([]byte(nil))
	f.Fuzz(func(t *testing.T, b []byte) {
		fr, err := ParseFrame(b)
		if err != nil {
			return
		}
		// What got through has to be one of the two kinds and inside its
		// bounds: a fuzzer that only called ParseFrame and dropped the answer
		// would measure that it does not panic and nothing else.
		switch fr.Kind {
		case KindKeys:
			if len(fr.Data) == 0 || len(fr.Data) > MaxKeys {
				t.Fatalf("keys of %d bytes got through", len(fr.Data))
			}
		case KindResize:
			if fr.Cols == 0 || fr.Rows == 0 || fr.Cols > MaxDim || fr.Rows > MaxDim {
				t.Fatalf("size %dx%d got through", fr.Cols, fr.Rows)
			}
		default:
			t.Fatalf("kind %d got through", fr.Kind)
		}
	})
}

// TestShellEnvWritesNoHistory is the bolt of the history file. It is the one
// place a typed secret can reach a DISK: the root filesystem of the app
// container is read only (runtime/podman.go, "--read-only"), /data is the only
// writable mount, and shellEnv points HOME there. A shell of the bash family
// creates HOME/.bash_history and writes every typed line into it, so a
// "mysql -p<secret>" would lie in the encrypted app data and outlive the tab.
//
// The remedy is that the file is never created, not that it is cleaned up
// afterwards: an empty HISTFILE makes bash write none at all. Measured on
// 15.10.2026 in WSL with a marker, bash 5.x: without the line the marker is in
// HOME/.bash_history, with it HOME is empty. The image's own shell is
// /bin/sh (dash in debian:13-slim), which keeps no history either way - the
// line is what holds once that symlink is something else.
//
// The case holds the NAME and the empty VALUE, because "HISTFILE=/data/x"
// would read as set and be the leak itself.
func TestShellEnvWritesNoHistory(t *testing.T) {
	t.Setenv("HOME", "/data")
	t.Setenv("HISTFILE", "/data/.bash_history")
	var seen bool
	for _, e := range shellEnv() {
		if strings.HasPrefix(e, "HISTFILE=") {
			seen = true
			if e != "HISTFILE=" {
				t.Fatalf("shellEnv() names %q, want an empty HISTFILE", e)
			}
		}
	}
	if !seen {
		t.Fatalf("shellEnv() names no HISTFILE, so a bash would write HOME/.bash_history into the app data")
	}
}
