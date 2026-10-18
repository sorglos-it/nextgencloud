package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// echoScript is the example service of contract V3: it echoes stdin to stdout,
// names app, service, action and user on stderr and ends with the code in
// NGC_PARAMS.
const echoScript = "../../../../shared/testdata/service/echo.sh"

// callUser is a user ID of the shape trust.ValidID wants: 16 bytes, base64url
// without padding.
var callUser = enc.B64.EncodeToString(bytes.Repeat([]byte{7}, 16))

// envMoved says that a case holds envDir right now. It is what keeps the one
// package variable from being a trap for whoever adds the next case: a second
// case that moves it while the first still holds it is exactly what
// t.Parallel() would bring about, and it stops the run instead of quietly
// measuring against the wrong directory.
var envMoved atomic.Bool

// envHere moves the directory of the environment file into one this process
// may write in and gives it back.
func envHere(t *testing.T) string {
	t.Helper()
	if !envMoved.CompareAndSwap(false, true) {
		t.Fatal("another case holds envDir: no case in this package may run in parallel")
	}
	old := envDir
	dir := t.TempDir()
	envDir = dir
	t.Cleanup(func() {
		envDir = old
		envMoved.Store(false)
	})
	return dir
}

// needShell skips a case that starts the example service where no POSIX shell
// runs it.
func needShell(t *testing.T) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("the example service of contract V3 needs a POSIX shell, which this system has not")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh here: " + err.Error())
	}
}

// echoCall is a call of the example service with these params.
func echoCall(params string) service.Call {
	return service.Call{
		User: callUser, UserName: "Thomas", App: "ngc.test", Service: "demo",
		Action: "run", Version: "1.0.0", Params: json.RawMessage(params),
	}
}

// collector keeps every line a call wrote, message and attributes alike, so a
// case can ask what is nowhere in the whole log.
type collector struct{ text strings.Builder }

func newCollector() *collector { return &collector{} }

func (c *collector) Enabled(context.Context, slog.Level) bool { return true }

func (c *collector) Handle(_ context.Context, r slog.Record) error {
	c.text.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		c.text.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	c.text.WriteString("\n")
	return nil
}

func (c *collector) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *collector) WithGroup(string) slog.Handler      { return c }

func (c *collector) logger() *slog.Logger { return slog.New(c) }

// noEngine fulfils runtime.Engine with nothing: a service call uses Exec and
// nothing else, so a case that reached another method would be about something
// this file does not run.
type noEngine struct{}

func (noEngine) LoadImage(context.Context, string, string) (string, error) {
	panic("a service call does not load an image")
}
func (noEngine) RemoveImage(context.Context, string, string) error {
	panic("a service call does not remove an image")
}
func (noEngine) Create(context.Context, runtime.Container) error {
	panic("a service call does not create a container")
}
func (noEngine) Start(context.Context, string) error {
	panic("a service call does not start a container")
}
func (noEngine) Stop(context.Context, string) error {
	panic("a service call does not stop a container")
}
func (noEngine) Remove(context.Context, string) error {
	panic("a service call does not remove a container")
}
func (noEngine) Running(context.Context, string) (bool, error) {
	panic("a service call does not ask whether a container runs")
}

// stub answers an Exec as a case tells it to and keeps what it was handed.
type stub struct {
	noEngine
	run func(ctx context.Context, e runtime.Exec) (int, error)

	calls    int
	got      runtime.Exec
	env      []string
	mode     fs.FileMode
	fileRead bool
	deadline time.Duration
	bounded  bool
}

func (s *stub) Exec(ctx context.Context, e runtime.Exec) (int, error) {
	s.calls++
	s.got = e
	if d, ok := ctx.Deadline(); ok {
		s.bounded, s.deadline = true, time.Until(d)
	}
	if b, err := os.ReadFile(e.EnvFile); err == nil {
		s.fileRead = true
		s.env = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if fi, err := os.Stat(e.EnvFile); err == nil {
			s.mode = fi.Mode().Perm()
		}
	}
	if s.run != nil {
		return s.run(ctx, e)
	}
	return 0, nil
}

// local runs the command of an Exec on this machine instead of in a container:
// the environment out of the file, the three ends of the call as they came, and
// a status only where the command really ended with one, as runtime.Engine.Exec
// has it.
type local struct{ noEngine }

func (local) Exec(ctx context.Context, e runtime.Exec) (int, error) {
	b, err := os.ReadFile(e.EnvFile)
	if err != nil {
		return 0, err
	}
	defer os.Remove(e.EnvFile)
	cmd := exec.CommandContext(ctx, e.Command[0], e.Command[1:]...)
	cmd.Env = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.Stdin, e.Stdout, e.Stderr
	// A killed shell leaves its children holding the pipes of this call, and
	// without this line Wait would sit there until they let go - measured: a
	// case about the time limit stood 20 seconds on a service that was long
	// dead. Podman has nothing of that: a container ends with everything in
	// it. This is what makes the stand-in behave like the thing it stands in
	// for.
	cmd.WaitDelay = 500 * time.Millisecond
	err = cmd.Run()
	var ended *exec.ExitError
	if errors.As(err, &ended) && ended.Exited() {
		return ended.ExitCode(), nil
	}
	if err != nil {
		return 0, errors.Join(runtime.ErrKilled, err, ctx.Err())
	}
	return 0, nil
}

func TestRunServicePassesTheEnvironmentAndEchoes(t *testing.T) {
	needShell(t)
	envHere(t)
	var out bytes.Buffer
	log := newCollector()
	cl := RunService(context.Background(), CallInput{
		Engine: local{}, Container: "ngc-app-test",
		Command: []string{"/bin/sh", echoScript},
		Call:    echoCall(`{}`),
		In:      strings.NewReader("hallo welt"), Out: &out, Log: log.logger(),
		// The environment is read back out of the stderr of the service, so
		// this case needs the switch that lets that text into the log.
		LogStderr: true,
	})
	if cl.Status != wire.StatusOK || cl.Code != "" {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	if out.String() != "hallo welt" {
		t.Fatalf("stdout = %q, want %q", out.String(), "hallo welt")
	}
	// The service names who asked on stderr, and that line is what says the
	// environment really arrived: app, service, action and the user ID, all
	// four out of NGC_* and none of them from a command line.
	want := "ngc.test demo run " + callUser
	if !strings.Contains(log.text.String(), want) {
		t.Fatalf("log = %q, want a line with %q", log.text.String(), want)
	}
}

func TestRunServiceReportsTheExitCode(t *testing.T) {
	needShell(t)
	envHere(t)
	cl := RunService(context.Background(), CallInput{
		Engine: local{}, Container: "ngc-app-test",
		Command: []string{"/bin/sh", echoScript},
		Call:    echoCall(`{"exit":2}`),
		In:      strings.NewReader(""), Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeExit {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeExit)
	}
	if cl.ExitCode == nil || *cl.ExitCode != 2 {
		t.Fatalf("exitCode = %v, want 2", cl.ExitCode)
	}
	if cl.Message != "Dienst mit Code 2 beendet." {
		t.Fatalf("message = %q, want %q", cl.Message, "Dienst mit Code 2 beendet.")
	}
}

func TestRunServiceTimesOut(t *testing.T) {
	needShell(t)
	envHere(t)
	// What the sleeping service would do if it ran on: the file is what says
	// the process is gone and not merely unwaited for.
	after := filepath.Join(t.TempDir(), "ran-on")
	done := make(chan wire.Close, 1)
	go func() {
		done <- RunService(context.Background(), CallInput{
			Engine: local{}, Container: "ngc-app-test",
			Command: []string{"/bin/sh", "-c", "sleep 2; : > " + after},
			Call:    echoCall(`{}`),
			Timeout: 150 * time.Millisecond, Log: newCollector().logger(),
		})
	}()
	var cl wire.Close
	select {
	case cl = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("RunService did not come back, so nothing stopped the service")
	}
	if cl.Status != wire.StatusError || cl.Code != wire.CodeTimeout {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeTimeout)
	}
	// Longer than the sleep of the service, so a service that was not stopped
	// has had its full time and really would have written the file.
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(after); !os.IsNotExist(err) {
		t.Fatalf("the service ran on past its limit: %v", err)
	}
}

func TestParamsNeverReachTheLog(t *testing.T) {
	needShell(t)
	envHere(t)
	const secret = "haus-der-mutter-1993"
	log := newCollector()
	cl := RunService(context.Background(), CallInput{
		Engine: local{}, Container: "ngc-app-test",
		Command: []string{"/bin/sh", echoScript},
		Call:    echoCall(`{"wort":"` + secret + `"}`),
		In:      strings.NewReader(""), Log: log.logger(),
		// With the switch on, which is the hard half of the rule: even the
		// whole stderr of the service must not carry the params.
		LogStderr: true,
	})
	if cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	got := log.text.String()
	// First that anything was written at all: a log nobody filled would pass
	// the two checks below without saying a word about them.
	if !strings.Contains(got, "ngc.test") {
		t.Fatalf("log = %q, want the one line of this call", got)
	}
	if strings.Contains(got, service.EnvParams) {
		t.Fatalf("log = %q, names %s", got, service.EnvParams)
	}
	if strings.Contains(got, secret) {
		t.Fatalf("log carries what the call was given: %q", got)
	}
}

func TestRunServiceStartsNothingWithoutAValidCall(t *testing.T) {
	dir := envHere(t)
	e := &stub{}
	c := echoCall(`{}`)
	// Capital letters are no app ID (wire.ValidAppID), and that is the whole
	// difference to the call every other case here uses.
	c.App = "NGC.Test"
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/bin/sh", echoScript},
		Call: c, Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeVerify {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeVerify)
	}
	if e.calls != 0 {
		t.Fatalf("the engine was called %d times for a call that is none", e.calls)
	}
	// And no environment file was written either: what the call was given must
	// not lie on the machine for a call that never ran.
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("%d files left in the run directory, want none", len(left))
	}
}

func TestTheTimeLimitOfACall(t *testing.T) {
	// The numbers are written out on purpose: a case that compares against
	// service.DefaultTimeout would go on passing after that constant moved,
	// and then it holds nothing.
	cases := []struct {
		name   string
		d      time.Duration
		stream bool
		want   time.Duration
	}{
		{"none named", 0, false, 30 * time.Second},
		{"negative", -time.Second, false, 30 * time.Second},
		{"as named", 5 * time.Second, false, 5 * time.Second},
		{"just under the ceiling", 3599 * time.Second, false, 3599 * time.Second},
		{"above the ceiling", 2 * time.Hour, false, 3600 * time.Second},
		{"streaming", 5 * time.Second, true, 0},
		{"streaming above the ceiling", 2 * time.Hour, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := limit(c.d, c.stream); got != c.want {
				t.Fatalf("limit(%v, %v) = %v, want %v", c.d, c.stream, got, c.want)
			}
		})
	}
}

func TestAStreamingCallGetsNoDeadlineAndABoundedOneDoes(t *testing.T) {
	envHere(t)
	stream := &stub{}
	if cl := RunService(context.Background(), CallInput{
		Engine: stream, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Timeout: 2 * time.Hour, Stream: true,
		Log: newCollector().logger(),
	}); cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	if stream.calls != 1 || stream.bounded {
		t.Fatalf("calls = %d, bounded = %v: a streaming call must reach the engine without a deadline",
			stream.calls, stream.bounded)
	}
	bounded := &stub{}
	if cl := RunService(context.Background(), CallInput{
		Engine: bounded, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Timeout: 2 * time.Hour,
		Log: newCollector().logger(),
	}); cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	// 3600 seconds written out, for the same reason as above. A deadline that
	// close cannot be read to the nanosecond, so the case asks for the hour it
	// must not exceed and for a rest of it that is left.
	if !bounded.bounded || bounded.deadline > 3600*time.Second ||
		bounded.deadline < 3599*time.Second {
		t.Fatalf("deadline = %v (bounded = %v), want just under 3600s",
			bounded.deadline, bounded.bounded)
	}
}

func TestTheEnvironmentTravelsInAFileAndNotInAnArgument(t *testing.T) {
	dir := envHere(t)
	const secret = "muehlenweg-sieben"
	e := &stub{}
	if cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/usr/local/bin/demo", "--go"},
		Call: echoCall(`{"wort":"` + secret + `"}`), Log: newCollector().logger(),
	}); cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	if !e.fileRead {
		t.Fatalf("the engine got no readable environment file (EnvFile = %q)", e.got.EnvFile)
	}
	if filepath.Dir(filepath.FromSlash(e.got.EnvFile)) != filepath.Clean(dir) {
		t.Fatalf("EnvFile = %q, want a file in %q", e.got.EnvFile, dir)
	}
	// Windows keeps no mode of its own, so this half of the case is measured
	// where the controller runs and nowhere else. Measured on Windows: the
	// file comes out 0666, which says nothing about the 0600 of the code.
	if goruntime.GOOS != "windows" && e.mode != 0o600 {
		t.Fatalf("mode = %v, want 600: this file is the only copy of what the call was given", e.mode)
	}
	if !slices.Contains(e.env, service.EnvParams+`={"wort":"`+secret+`"}`) {
		t.Fatalf("environment = %q, want a compact %s line", e.env, service.EnvParams)
	}
	for _, a := range append(slices.Clone(e.got.Command), e.got.Name) {
		if strings.Contains(a, secret) {
			t.Fatalf("what the call was given stands in an argument: %q", a)
		}
	}
	// And it is gone once the call is over. The engine removes it itself; this
	// side removes it too, because the ways that never reach the engine would
	// leave it lying there.
	if _, err := os.Stat(e.got.EnvFile); !os.IsNotExist(err) {
		t.Fatalf("the environment file is still there: %v", err)
	}
}

// halfReader gives out its bytes and then breaks off, the way the input of a
// channel that ended before its end-to-end end does.
type halfReader struct {
	s    string
	read bool
}

func (h *halfReader) Read(p []byte) (int, error) {
	if !h.read {
		h.read = true
		n := copy(p, h.s)
		return n, nil
	}
	return 0, errors.New("the input of the channel broke off")
}

func TestInputCutOffIsNeverASuccess(t *testing.T) {
	envHere(t)
	// The engine reads everything it is given and is happy: so nothing but
	// the end of the input can make this call an error.
	e := &stub{run: func(_ context.Context, ex runtime.Exec) (int, error) {
		io.Copy(io.Discard, ex.Stdin)
		return 0, nil
	}}
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), In: &halfReader{s: "half"}, Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeProtocol {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeProtocol)
	}
}

func TestAWholeInputIsASuccess(t *testing.T) {
	envHere(t)
	e := &stub{run: func(_ context.Context, ex runtime.Exec) (int, error) {
		io.Copy(io.Discard, ex.Stdin)
		return 0, nil
	}}
	// The same call as above with the one difference that decides it: this
	// reader ends with io.EOF instead of an error. Without this case the one
	// above would also pass on a RunService that refuses every input.
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), In: strings.NewReader("half"), Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusOK || cl.Code != "" {
		t.Fatalf("close = %+v, want status ok", cl)
	}
}

func TestTheStderrOfACallIsBoundedAndKeepsItsEnd(t *testing.T) {
	envHere(t)
	log := newCollector()
	e := &stub{run: func(_ context.Context, ex runtime.Exec) (int, error) {
		ex.Stderr.Write([]byte("ANFANG"))
		ex.Stderr.Write(bytes.Repeat([]byte{'a'}, 70000))
		ex.Stderr.Write([]byte("ENDE"))
		return 0, nil
	}}
	if cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Log: log.logger(), LogStderr: true,
	}); cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	got := log.text.String()
	i := strings.Index(got, "stderr=")
	if i < 0 {
		t.Fatalf("log = %q (%d bytes), want a stderr field", got[:min(len(got), 80)], len(got))
	}
	kept := got[i+len("stderr="):]
	kept = strings.TrimSuffix(kept, "\n")
	// 65536 written out: a case that compared against service.MaxStderr would
	// go on passing after that constant moved.
	if len(kept) != 65536 {
		t.Fatalf("stderr field = %d bytes, want 65536", len(kept))
	}
	if !strings.HasSuffix(kept, "ENDE") {
		t.Fatalf("the end of stderr was thrown away instead of its beginning: %q",
			kept[max(0, len(kept)-20):])
	}
	if strings.Contains(kept, "ANFANG") {
		t.Fatal("more than 65536 bytes of stderr were kept")
	}
}

func TestAnEngineThatCannotBeRunMakesTheServiceUnreachable(t *testing.T) {
	envHere(t)
	e := &stub{run: func(context.Context, runtime.Exec) (int, error) {
		return 0, errors.New("podman is not installed")
	}}
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeUnreachable {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeUnreachable)
	}
}

func TestAProgramEndedBySignalHasNoStatus(t *testing.T) {
	envHere(t)
	e := &stub{run: func(context.Context, runtime.Exec) (int, error) {
		return 0, runtime.ErrKilled
	}}
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeExit {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeExit)
	}
	// No number: a signal is no status a program ended with, and a 0 here
	// would read like a service that was happy.
	if cl.ExitCode != nil {
		t.Fatalf("exitCode = %d, want none", *cl.ExitCode)
	}
}

func TestParamsReachNoLineOfAnyCall(t *testing.T) {
	envHere(t)
	// The same rule as TestParamsNeverReachTheLog, measured where no POSIX
	// shell is: that case needs the example service and skips on Windows, and
	// a rule this heavy may not be unmeasured on half the machines it is
	// written on. The secret is its own, so a value hard-wired in one case
	// cannot carry the other.
	const secret = "stoerstelle-im-kristall"
	log := newCollector()
	e := &stub{run: func(_ context.Context, ex runtime.Exec) (int, error) {
		ex.Stderr.Write([]byte("der Dienst meldet sich\n"))
		return 0, nil
	}}
	if cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{"wort":"` + secret + `"}`), Log: log.logger(), LogStderr: true,
	}); cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	got := log.text.String()
	if !strings.Contains(got, "der Dienst meldet sich") {
		t.Fatalf("log = %q, want the stderr of this call", got)
	}
	if strings.Contains(got, service.EnvParams) || strings.Contains(got, secret) {
		t.Fatalf("log = %q, carries what the call was given", got)
	}
}

func TestACallWhoseProgramIgnoresItsInputStillEnds(t *testing.T) {
	envHere(t)
	// A program that ends without reading its input leaves this side writing
	// into a pipe nobody empties, and io.Pipe buffers nothing: whoever copies
	// there waits for a reader that will never come. So RunService has to
	// close the reading end itself, and this is the case that says it does.
	e := &stub{}
	done := make(chan wire.Close, 1)
	go func() {
		done <- RunService(context.Background(), CallInput{
			Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
			Call: echoCall(`{}`),
			In:   strings.NewReader(strings.Repeat("x", 1<<20)),
			Log:  newCollector().logger(),
		})
	}()
	select {
	case cl := <-done:
		if cl.Status != wire.StatusOK || cl.Code != "" {
			t.Fatalf("close = %+v, want status ok", cl)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("RunService did not come back: nothing freed the input of a program that never read it")
	}
}

func TestACancelledCallerIsNoTimeLimit(t *testing.T) {
	envHere(t)
	ctx, cancel := context.WithCancel(context.Background())
	// The engine answers as it does for a program no status came back from,
	// which is what a cancelled context leaves behind (runtime.ErrKilled).
	e := &stub{run: func(context.Context, runtime.Exec) (int, error) {
		return 0, runtime.ErrKilled
	}}
	cancel()
	cl := RunService(ctx, CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Log: newCollector().logger(),
	})
	// E_TIMEOUT would tell the shell this server stopped the service because
	// it took too long, and nothing of the sort happened: whoever owns the
	// channel let it go.
	if cl.Status != wire.StatusError || cl.Code != wire.CodeExit {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeExit)
	}
}

func TestACallThatWroteNoStderrWritesNoLine(t *testing.T) {
	envHere(t)
	log := newCollector()
	if cl := RunService(context.Background(), CallInput{
		Engine: &stub{}, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Log: log.logger(),
	}); cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	// A line per call would be a line per channel, and how many channels
	// arrive is the API's to choose - the doc of this package says what that
	// makes of a log.
	if got := log.text.String(); got != "" {
		t.Fatalf("log = %q, want nothing", got)
	}
}

func TestAServiceCallWiredWrongReachesNoEngine(t *testing.T) {
	envHere(t)
	cases := []struct {
		name string
		in   CallInput
	}{
		{"no engine", CallInput{Container: "ngc-app-test", Command: []string{"/x"}}},
		{"no container", CallInput{Engine: &stub{}, Command: []string{"/x"}}},
		{"no command", CallInput{Engine: &stub{}, Container: "ngc-app-test"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			in.Call, in.Log = echoCall(`{}`), newCollector().logger()
			cl := RunService(context.Background(), in)
			if cl.Status != wire.StatusError || cl.Code != wire.CodeInternal {
				t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeInternal)
			}
			if e, ok := in.Engine.(*stub); ok && e.calls != 0 {
				t.Fatalf("the engine was called %d times", e.calls)
			}
		})
	}
}

func TestInputCutOffEndsTheService(t *testing.T) {
	needShell(t)
	envHere(t)
	// What the service would do if it ran on to its own end. The call is cut
	// off a moment after it started, so this file must never appear: a channel
	// somebody dropped may not leave a program of an app running for the hour
	// a time limit allows.
	after := filepath.Join(t.TempDir(), "ran-on")
	done := make(chan wire.Close, 1)
	go func() {
		done <- RunService(context.Background(), CallInput{
			Engine: local{}, Container: "ngc-app-test",
			// It reads its input first, because a program that never reads
			// cannot notice an input at all - and then nothing would be
			// measured here.
			Command: []string{"/bin/sh", "-c", "head -c 4 >/dev/null; sleep 2; : > " + after},
			Call:    echoCall(`{}`),
			// An hour is the ceiling of the contract: if the cut-off input did
			// not end this call, the case below would be waiting for it.
			Timeout: time.Hour,
			In:      &halfReader{s: "half"}, Log: newCollector().logger(),
		})
	}()
	var cl wire.Close
	select {
	case cl = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("RunService did not come back, so the cut-off input ended nothing")
	}
	if cl.Status != wire.StatusError || cl.Code != wire.CodeProtocol {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeProtocol)
	}
	// Longer than the sleep of the service, so a service that was not stopped
	// has had its full time and really would have written the file.
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(after); !os.IsNotExist(err) {
		t.Fatalf("the service ran on after its input was cut off: %v", err)
	}
}

func TestInputCutOffCancelsTheCallAtOnce(t *testing.T) {
	envHere(t)
	// The same rule as TestInputCutOffEndsTheService, measured where no POSIX
	// shell is: that case watches a real program stop, this one watches the
	// one thing that stops it. Without it the rule would hang on a case that
	// is skipped on Windows, and that is how a rule goes unmeasured.
	seen := make(chan error, 1)
	e := &stub{run: func(ctx context.Context, ex runtime.Exec) (int, error) {
		// A program that reads its input and then takes its time, like a
		// service with an hour of limit left.
		io.Copy(io.Discard, ex.Stdin)
		select {
		case <-ctx.Done():
			seen <- ctx.Err()
		case <-time.After(20 * time.Second):
			seen <- nil
		}
		return 0, runtime.ErrKilled
	}}
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Timeout: time.Hour,
		In:  &halfReader{s: "half"},
		Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeProtocol {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeProtocol)
	}
	if err := <-seen; !errors.Is(err, context.Canceled) {
		t.Fatalf("the context of the call ended with %v, want it cancelled: a cut-off input has to stop the program, not wait out its hour", err)
	}
}

func TestTheOutputOfTheProgramReachesTheCaller(t *testing.T) {
	envHere(t)
	// The same rule as TestRunServicePassesTheEnvironmentAndEchoes, with a
	// stand-in instead of the example service: that case needs a POSIX shell
	// and is skipped on Windows, and "the output of a service reaches the
	// shell" may not be unmeasured on the machine this is built on.
	var out bytes.Buffer
	e := &stub{run: func(_ context.Context, ex runtime.Exec) (int, error) {
		if ex.Stdout == nil {
			return 0, errors.New("the call got no output end")
		}
		ex.Stdout.Write([]byte("zweiundvierzig"))
		return 0, nil
	}}
	if cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Out: &out, Log: newCollector().logger(),
	}); cl.Status != wire.StatusOK {
		t.Fatalf("close = %+v, want status ok", cl)
	}
	if out.String() != "zweiundvierzig" {
		t.Fatalf("output = %q, want %q", out.String(), "zweiundvierzig")
	}
}

func TestATimeLimitThatRanOutIsNoExitCode(t *testing.T) {
	envHere(t)
	// The same rule as TestRunServiceTimesOut, with a stand-in: that case needs
	// a shell. Here the engine answers the way it does for a call no status
	// came back from, after the limit has run out.
	e := &stub{run: func(ctx context.Context, _ runtime.Exec) (int, error) {
		<-ctx.Done()
		return 0, runtime.ErrKilled
	}}
	done := make(chan wire.Close, 1)
	go func() {
		done <- RunService(context.Background(), CallInput{
			Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
			Call: echoCall(`{}`), Timeout: 50 * time.Millisecond,
			Log: newCollector().logger(),
		})
	}()
	select {
	case cl := <-done:
		if cl.Status != wire.StatusError || cl.Code != wire.CodeTimeout {
			t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeTimeout)
		}
		// E_EXIT would tell the shell the service decided something. It did
		// not: this server stopped it.
		if cl.ExitCode != nil {
			t.Fatalf("exitCode = %d, want none after a time limit", *cl.ExitCode)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("RunService did not come back after its time limit")
	}
}

func TestTheExitCodeOfTheProgramIsTheOneReported(t *testing.T) {
	envHere(t)
	// The same rule as TestRunServiceReportsTheExitCode, with a stand-in, and
	// with a different number on purpose: a case that shared the 2 of that one
	// would go on passing on a RunService that answers with a hard-wired 2.
	e := &stub{run: func(context.Context, runtime.Exec) (int, error) { return 7, nil }}
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
		Call: echoCall(`{}`), Log: newCollector().logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeExit {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeExit)
	}
	if cl.ExitCode == nil || *cl.ExitCode != 7 {
		t.Fatalf("exitCode = %v, want 7", cl.ExitCode)
	}
	if cl.Message != "Dienst mit Code 7 beendet." {
		t.Fatalf("message = %q, want %q", cl.Message, "Dienst mit Code 7 beendet.")
	}
}

func TestANameThatIsTakenIsNoPlaceForTheParams(t *testing.T) {
	dir := envHere(t)
	name := filepath.Join(dir, "env-taken")
	const first = "NGC_PARAMS={\"erster\":1}" + "\n"
	f, err := createEnv(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(first)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	// Whoever holds this name holds a file another call reads its own
	// parameters out of. Writing into it would hand that call the parameters of
	// this one.
	if second, err := createEnv(name); err == nil {
		second.Close()
		t.Fatal("a name that is taken was opened a second time")
	}
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != first {
		t.Fatalf("the file of the first call now holds %q", b)
	}
}

func TestTwoCallsGetTwoFilesOfTheirOwn(t *testing.T) {
	envHere(t)
	first, err := writeEnv([]string{"NGC_PARAMS={\"a\":1}"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(first)
	second, err := writeEnv([]string{"NGC_PARAMS={\"b\":2}"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(second)
	if first == second {
		t.Fatalf("both calls got the same file %q", first)
	}
	// And each one still holds its own: a name that is not new would have the
	// second call reading what the first was given, or the other way round.
	for path, want := range map[string]string{
		first:  "NGC_PARAMS={\"a\":1}" + "\n",
		second: "NGC_PARAMS={\"b\":2}" + "\n",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Fatalf("%s holds %q, want %q", path, b, want)
		}
	}
}

// The stderr of a service is its own text, and a service has NGC_PARAMS in its
// environment: a program that writes its environment would put what the user
// typed into the log of this server. So the text goes out only where someone
// switched it on, and off is the default - what stands in the log then is that
// there was stderr and how much of it.
func TestStderrTextStaysOutOfTheLogUnlessItIsSwitchedOn(t *testing.T) {
	envHere(t)
	const said = "ich bin der fehler 4711"
	for _, c := range []struct {
		name string
		on   bool
	}{{"off", false}, {"on", true}} {
		t.Run(c.name, func(t *testing.T) {
			log := newCollector()
			e := &stub{run: func(_ context.Context, ex runtime.Exec) (int, error) {
				ex.Stderr.Write([]byte(said))
				return 0, nil
			}}
			if cl := RunService(context.Background(), CallInput{
				Engine: e, Container: "ngc-app-test", Command: []string{"/x"},
				Call: echoCall(`{}`), Log: log.logger(), LogStderr: c.on,
			}); cl.Status != wire.StatusOK {
				t.Fatalf("close = %+v, want status ok", cl)
			}
			got := log.text.String()
			// Both ways: that there was stderr, and how much. Without this the
			// check below would pass on a log nobody wrote at all.
			if !strings.Contains(got, "bytes="+strconv.Itoa(len(said))) {
				t.Fatalf("log = %q, want the length of the stderr", got)
			}
			if strings.Contains(got, said) != c.on {
				t.Fatalf("log = %q, want the text of the stderr only with the switch on", got)
			}
		})
	}
}

// panicReader is the input of a call whose reader has a programming error in
// it. It gives one piece of input back first, so the feeding really starts and
// the case does not measure a panic before the goroutine did anything.
type panicReader struct{ first bool }

func (p *panicReader) Read(b []byte) (int, error) {
	if !p.first {
		p.first = true
		b[0] = 'x'
		return 1, nil
	}
	panic(fmt.Errorf("the bearer token is %s", callSecret))
}

// callSecret is what the panic value carries by accident.
const callSecret = "NGC-TEST-SECRET-0123456789"

// TestAPanicInTheFeederEndsTheCallAndNotTheServer is V1.2 point 38 for the one
// goroutine of a service call: the input of a call is fed to the program from a
// goroutine of its own, and a panic in it -- in the reader this call was handed
// -- was the end of the whole ngc-controller, with every channel of every
// tunnel.
//
// Three things are measured, and the second is the one that was not there
// before the catch: the call ANSWERS, and it answers E_INTERNAL and not the
// cut-off input that stands right beside it in the same switch. A programming
// error of ours is not a peer that stopped sending, and a person who is told
// the wrong one of the two looks in the wrong place (V1.2 point 50).
func TestAPanicInTheFeederEndsTheCallAndNotTheServer(t *testing.T) {
	envHere(t)
	before := guard.Panics()
	log := newCollector()
	e := &stub{run: func(ctx context.Context, ex runtime.Exec) (int, error) {
		// Read until the input ends, which is what the catch has to bring
		// about: without the pipe being closed this read would wait for an
		// end that is never coming, and the case would hang instead of fail.
		_, _ = io.Copy(io.Discard, ex.Stdin)
		return 0, nil
	}}
	cl := RunService(context.Background(), CallInput{
		Engine: e, Container: "ngc-app-test", Command: []string{"/bin/sh", echoScript},
		Call: echoCall(`{}`), In: &panicReader{}, Log: log.logger(),
	})
	if cl.Status != wire.StatusError || cl.Code != wire.CodeInternal {
		t.Fatalf("close = %+v, want an error with %s", cl, wire.CodeInternal)
	}
	if got := guard.Panics(); got != before+1 {
		t.Fatalf("the counter went %d -> %d; one panic counts once", before, got)
	}
	line := log.text.String()
	if !strings.Contains(line, "site=serve.feed") {
		t.Fatalf("no crash line of the feeder: %q", line)
	}
	// The same rule as everywhere: the line names the kind and the place, and
	// never the value -- a service call carries what the user typed.
	if strings.Contains(line, callSecret) || strings.Contains(line, "bearer token") {
		t.Fatalf("the value of the panic reached the log: %q", line)
	}
}
