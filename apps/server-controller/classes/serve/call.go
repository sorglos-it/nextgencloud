// Service calls of contract V3: RunService runs the program of one service in
// the container of its app and answers with the one CLOSE that run ended
// with.
//
// Nothing of what a call was given is written by this controller itself.
// NGC_PARAMS carries whatever the user typed, so the environment travels in a
// file under config.RunDir and never in an argument (runtime.Exec.EnvFile),
// and the only thing of a call this file hands a logger is logKeys - three
// strings, built in one place, with no way to carry Params along.
//
// That is not the same as "it cannot reach the log", and the difference
// matters to whoever writes a service: contract V3 asks for the stderr of the
// program. What this package writes of it by default is that there was stderr
// and how many bytes - never the text. The text is a service telling this
// server about itself, and a service has NGC_PARAMS in its environment: one
// that writes its environment, or its parameters, or the text of an error
// which quotes them, would put what the user typed into the log of this
// server. Measured: shared/testdata/service/echo.sh writes NGC_APP,
// NGC_SERVICE, NGC_ACTION and NGC_USER to stderr, and a case here reads all
// four back out of the log - with CallInput.LogStderr on.
//
// So the text goes out only where someone switched it on, and off is the
// default (CallInput.LogStderr, Deps.LogStderr). The price in one sentence:
// whoever hunts a broken app has to turn the switch before the log tells him
// what the app said.

package serve

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// envDir is where the file with the environment of a call is written. It is
// config.RunDir, because that is the one directory the engine takes an
// EnvFile from (runtime.Exec.EnvFile) and a tmpfs, so no copy of NGC_PARAMS
// outlives a restart.
//
// Only a test moves it, to a directory it may write in. It is one variable for
// the whole package, so two cases that move it at the same time would measure
// against each other; nothing in this file can stop that, so the test side
// stops it instead - envHere refuses to move a directory that is already
// moved, which is what a second case in parallel would be doing.
var envDir = config.RunDir

// CallInput is one run of a service program.
type CallInput struct {
	// Engine runs the container of the app. nil is this program wired wrong
	// and ends the call with E_INTERNAL, as a missing container name or an
	// empty command does: none of the three can come from a peer.
	Engine runtime.Engine
	// Container is the container of the app, which is the name of its system
	// user (runtime.Container.Name). The engine checks the name itself.
	Container string
	// Command is the program of the service and its arguments, as the
	// manifest has them. No shell sees it.
	Command []string
	// Call is what the program is told, as contract V3 has it. Validate runs
	// before anything is started.
	Call service.Call
	// Timeout is the time limit of store.Action, 0 for the one of the
	// contract. Everything above service.MaxTimeout is cut down to it, and
	// Stream puts it aside altogether (see limit).
	Timeout time.Duration
	// Stream says the action streams, so nothing bounds how long it runs.
	Stream bool
	// In is the input of the program, nil for a program without one. Its end
	// decides whether this run can be a success at all: io.EOF is the input
	// of the shell complete, and every other error is input cut off, which
	// ends the call with E_PROTOCOL whatever the program did with what it got
	// (see RunService). Whoever hands the data of a channel in here therefore
	// answers a channel that ended early with an error and not with io.EOF.
	In io.Reader
	// Out takes the output of the program as it comes. nil throws it away.
	Out io.Writer
	// Log is where the one stderr line of this call goes. nil:
	// slog.Default().
	Log *slog.Logger
	// LogStderr lets the text of that line carry the stderr of the program.
	// False, the default, writes only that there was stderr and how much: the
	// text is the app's own and may hold what the user typed (see the head of
	// this file). An operator switches it on to hunt a broken app.
	LogStderr bool
}

// RunService runs the program of one service and answers with the CLOSE of
// that run - on an end-to-end channel the Status of the controller's
// wire.E2EEnd.
//
// The order of what it answers is the order of what it knows, and it is not
// the order the three may happen in:
//
//  1. Input cut off beats everything else, including a program that ended
//     with 0. A program that got half its input and was happy with it has
//     still not done what was asked, so a cut-off input is never a success.
//  2. A status the program really ended with beats the time limit. When the
//     limit runs out in the same moment the program ends, the engine answers
//     with that status (runtime.Engine.Exec), and then nothing was stopped.
//  3. Only then does the limit count, and after it a signal, and after that
//     an engine that could not be run at all.
func RunService(ctx context.Context, in CallInput) wire.Close {
	// Before anything is started, and that is the point of it: an invalid
	// call must not reach a container at all, so this stands ahead of the
	// environment file and of the engine.
	if err := in.Call.Validate(); err != nil {
		return wire.Error(wire.CodeVerify)
	}
	if in.Engine == nil || in.Container == "" || len(in.Command) == 0 {
		return wire.Error(wire.CodeInternal)
	}
	file, err := writeEnv(in.Call.Env())
	if err != nil {
		return wire.Error(wire.CodeInternal)
	}
	// The engine removes it once the call is over, and this line is for the
	// ways that never reach the engine - a refused path, an engine that could
	// not be started. Removing a file that is gone is no error.
	defer os.Remove(file)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if d := limit(in.Timeout, in.Stream); d > 0 {
		// The limit hangs under the cancel above, not the other way round:
		// what cut-off input cancels must stay tellable from what the limit
		// ends, and the two are told apart by nothing but ctx.Err() being
		// context.Canceled or context.DeadlineExceeded.
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, d)
		defer stop()
	}

	var (
		stdin io.Reader
		pr    *io.PipeReader
		src   *source
		fed   = make(chan struct{})
		// broke says a programming error ended the feeding. It is read after
		// <-fed, so the write and the read are ordered by that channel and
		// not by a lock.
		broke bool
	)
	if in.In != nil {
		var pw *io.PipeWriter
		pr, pw = io.Pipe()
		stdin, src = pr, &source{r: in.In}
		go func() {
			defer close(fed)
			// A goroutine of its own, feeding ONE call: a panic in it -- in
			// the reader this call was handed, say -- would take the whole
			// ngc-controller with every channel of every tunnel (V1.2 point
			// 38). It ends this call instead, and loudly: E_INTERNAL below.
			//
			// The catch does the same two things the ordinary end of the
			// copy does, and it has to: a program whose input is neither
			// closed nor cancelled waits for an end that is not coming. The
			// write end is closed, so the program sees its input end, and
			// the context goes, so the program goes with it. Without both,
			// the catch would trade a crash for a call that hangs until its
			// time limit. The error the write end is closed with is this
			// side's own and reaches the program as nothing but the end of
			// its input.
			defer func() {
				if guard.HitTo(in.logger(), recover(), "serve.feed", 0) {
					broke = true
					pw.CloseWithError(io.ErrUnexpectedEOF)
					cancel()
				}
			}()
			_, err := io.Copy(pw, src)
			// A clean end closes the input of the program, so a program that
			// reads to the end of its input gets there. A cut-off input does
			// not get that far: the context goes, and with it the program.
			pw.CloseWithError(err)
			if src.err != nil {
				cancel()
			}
		}()
	}

	errs := &ring{max: service.MaxStderr}
	code, execErr := in.Engine.Exec(ctx, runtime.Exec{
		Name: in.Container, Command: in.Command, EnvFile: file,
		Stdin: stdin, Stdout: in.Out, Stderr: errs,
	})
	if pr != nil {
		// The program is over, so nothing reads the pipe any more: closing
		// the reading end is what lets a goroutine still writing into it come
		// back, and without it this wait would be forever.
		pr.Close()
		<-fed
	}
	// One line, after the run, and never a second: how often a service writes
	// to stderr is the app's to choose and how often this controller writes
	// must not be.
	if b := errs.bytes(); len(b) > 0 {
		logStderr(in.logger(), keysOf(in.Call), b, in.LogStderr)
	}

	switch {
	case broke:
		// FIRST, ahead of the cut-off input: a programming error of ours is
		// not an input the peer cut off, and it must not be answered as one.
		// Whatever the program made of the half input it got, the call did
		// not do what was asked, and the one who asked is told so.
		return wire.Error(wire.CodeInternal)
	case src != nil && src.err != nil:
		return *cutOff()
	case execErr == nil:
		return service.Result(code, false, nil)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return service.Result(0, true, nil)
	case errors.Is(execErr, runtime.ErrKilled):
		// No status reached the engine and no limit ran out, so a signal
		// ended the program - or the context of the caller did. Neither is a
		// number a program can end with, and service.Result names E_EXIT for
		// exactly that.
		return service.Result(-1, false, nil)
	default:
		return service.Result(0, false, execErr)
	}
}

// limit is how long one call may run: the time limit of the action, the
// service.DefaultTimeout of the contract where the action names none, never
// more than service.MaxTimeout - and nothing at all for an action that
// streams, whose output is the point and whose end is the shell's to decide.
func limit(d time.Duration, stream bool) time.Duration {
	if stream {
		return 0
	}
	if d <= 0 {
		return service.DefaultTimeout * time.Second
	}
	if d > service.MaxTimeout*time.Second {
		return service.MaxTimeout * time.Second
	}
	return d
}

// writeEnv puts env into a fresh file under envDir, one NAME=value per line,
// and answers with its path.
//
// Mode 0600 is not tidiness: this file is the only copy on the machine of what
// the call was given, NGC_PARAMS and all, and it lies in a directory the apps
// of this server have theirs in too.
//
// The owner is not this function's to set and does not have to be: the engine
// gives the file to the app's user itself, out of the user database it reads
// by container name (runtime.Podman.Exec). Only the mode is the caller's,
// because only the caller knows that this is the one copy.
func writeEnv(env []string) (string, error) {
	var b bytes.Buffer
	for _, e := range env {
		b.WriteString(e)
		b.WriteByte('\n')
	}
	name, err := envName()
	if err != nil {
		return "", err
	}
	f, err := createEnv(name)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b.Bytes()); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// createEnv makes the one file of a call and nothing else. O_EXCL, so this
// never writes into a file that was already there: the name is random, and a
// name that is taken means something else is using it - another call of the
// same app, or someone who guessed. Overwriting it would put the parameters of
// one call where another call reads its own.
func createEnv(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// envName is the path of one environment file. The random half is what keeps
// two calls of the same app apart; nothing of the call is in the name,
// because a name is readable for everyone who may list the directory.
func envName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return envDir + "/env-" + hex.EncodeToString(b[:]), nil
}

// source is the input of a program and the one error of reading it. The error
// is written by the goroutine that copies and read by RunService after that
// goroutine has ended, and only there.
//
// It records what the reader said and not what the writing said: once the
// program is over the pipe is closed under the copy, and that write error is
// this side's doing, not a cut-off input.
type source struct {
	r   io.Reader
	err error
}

func (s *source) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.err = err
	}
	return n, err
}

// ring keeps the last max bytes that were written to it and throws the rest
// away. The stderr of a service is an app's to fill and may never be the
// memory of this controller: service.MaxStderr is what one call may cost.
type ring struct {
	b   []byte
	max int
}

func (r *ring) Write(p []byte) (int, error) {
	n := len(p)
	if r.max <= 0 {
		return n, nil
	}
	if len(p) > r.max {
		p = p[len(p)-r.max:]
	}
	r.b = append(r.b, p...)
	if len(r.b) > r.max {
		r.b = r.b[len(r.b)-r.max:]
	}
	return n, nil
}

// bytes is what the ring held when the call ended.
func (r *ring) bytes() []byte { return r.b }

// logKeys is everything of a call that this package writes by itself. Params
// is not in it and cannot get in: the struct holds three strings, keysOf is
// the only thing that builds one, and logStderr takes no second part of a
// call. So no line of this package has to remember not to write Params -
// there is no argument to write it with.
//
// What logStderr may take besides them is the stderr of the program itself,
// and that one is behind a switch (see the head of this file). So this barrier
// holds this package to its own words, and the switch holds what the app says
// about itself.
type logKeys struct{ app, service, action string }

// keysOf picks those three out of a call.
func keysOf(c service.Call) logKeys { return logKeys{c.App, c.Service, c.Action} }

// logStderr writes the one line of this call: always that there was stderr and
// how many bytes of it, and the text itself only where full says so.
//
// The length is in both lines and on purpose. It is the app's own doing and
// says nothing about what the user typed, and without it a log with the switch
// off would not even say that a service is talking.
func logStderr(log *slog.Logger, k logKeys, b []byte, full bool) {
	if full {
		log.Info("a service wrote to stderr", "app", k.app, "service", k.service,
			"action", k.action, "bytes", len(b), "stderr", string(b))
		return
	}
	log.Info("a service wrote to stderr", "app", k.app, "service", k.service,
		"action", k.action, "bytes", len(b))
}

// logger is where this call writes.
func (in CallInput) logger() *slog.Logger {
	if in.Log != nil {
		return in.Log
	}
	return slog.Default()
}
