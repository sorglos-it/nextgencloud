// Service calls of contract V3: RunService runs the program of one service in
// the container of its app and answers with the one CLOSE that run ended
// with.
//
// Nothing of what a call was given reaches the log of this controller.
// NGC_PARAMS carries whatever the user typed, so the environment travels in a
// file under config.RunDir and never in an argument (runtime.Exec.EnvFile),
// and the only thing of a call this file hands a logger is logKeys - three
// strings, built in one place, with no way to carry Params along.

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
// Only a test moves it, to a directory it may write in; it is one variable
// for the whole package, so no case that moves it may run in parallel with
// another.
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
	)
	if in.In != nil {
		var pw *io.PipeWriter
		pr, pw = io.Pipe()
		stdin, src = pr, &source{r: in.In}
		go func() {
			defer close(fed)
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
		logStderr(in.logger(), keysOf(in.Call), string(b))
	}

	switch {
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
// What this function cannot do is give the file to the app's user, and the
// engine runs podman as that user and has to read it (runtime.Exec.EnvFile).
// CallInput names no uid, so nothing here can chown it - see the report of
// task 13.
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
	// O_EXCL, so this call never writes into a file that was already there:
	// the name is random, and a name that is taken is a reason to stop and
	// not to overwrite.
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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

// logKeys is everything of a call that may be written. Params is not in it
// and cannot get in: the struct holds three strings, keysOf is the only thing
// that builds one, and logStderr takes nothing else. So "NGC_PARAMS never
// reaches the log" is not a rule a reader has to keep here - there is no
// argument to break it with.
type logKeys struct{ app, service, action string }

// keysOf picks those three out of a call.
func keysOf(c service.Call) logKeys { return logKeys{c.App, c.Service, c.Action} }

// logStderr writes the one line of this call.
func logStderr(log *slog.Logger, k logKeys, text string) {
	log.Info("a service wrote to stderr",
		"app", k.app, "service", k.service, "action", k.action, "stderr", text)
}

// logger is where this call writes.
func (in CallInput) logger() *slog.Logger {
	if in.Log != nil {
		return in.Log
	}
	return slog.Default()
}
