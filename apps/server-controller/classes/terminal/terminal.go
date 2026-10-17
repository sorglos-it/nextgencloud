// Package terminal is the service program of the terminal app. It runs inside
// that app's container, not on the server: the controller starts it as
// "ngc-controller terminal" (the manifest's service terminal), hands it the
// input of the channel on stdin and takes the output of the shell from stdout.
//
// Nothing of what passes through here is written anywhere. A terminal sees
// everything a user types - passwords among it - and everything the shell
// answers, so no message of this package carries a byte of either: every line
// it can write stands as a constant below, and a case holds that none of them
// grows a value (TestNothingOfTheChannelReachesAMessage). That case measures
// with a SHORT marker on purpose: a logger that throws long details away would
// let a mutation "write the whole line" pass with a long one.
//
// THE PARAMETERS OF ngc.open ARE DECORATION, AND THAT IS A DECISION. The plan
// of the web side writes the call as
// ngc.open("terminal", "shell", {cols, rows}), and a reader takes that for the
// way the size travels. It is not. This program reads NO parameters: the size
// comes in a resize frame and nowhere else (ParseFrame, pumpIn), and shellEnv
// below builds the environment of the shell out of five fixed lines, so
// NGC_PARAMS reaches neither the shell nor the pseudo terminal.
//
// It stays that way on purpose, and the reason is measured on the other side:
// the web app sends the size as the FIRST thing it sends after the channel is
// open and again on every change (apps/web/assets/apps/terminal/app.js,
// tellSize; its case "the first thing the terminal sends is the window size,
// byte for byte" holds the three header bytes and the two numbers). The frame
// is therefore not a late correction of a size the parameters already carried
// - it is the first word of the channel.
//
// Reading the parameters as well would put ONE value on TWO ways in, and this
// project has paid for that shape before: the clamp of 1..MaxDim would then
// live in two places, and a size that got past one of them would be a size
// nobody bounded. There is a price, and it is named instead of hidden: between
// the start of the shell and the arrival of that first frame the pseudo
// terminal has whatever size the kernel gave it, so a prompt written in that
// window is laid out for the wrong width. Nothing is laid out before input
// arrives, and the first input is the frame.
//
// What this package does NOT decide is who gets here. The gate is on the
// server, in package access, and it is measured against the manifest of this
// app in manifest_test.go. The three checks in run are a second fence inside
// the container, for the case that something ever starts this program without
// a user behind it.
package terminal

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The frames of the channel: one byte kind, two bytes length of the payload
// (big endian), then the payload.
//
// The plan of this task wrote the length only into the resize frame and let
// the keys frame be "the bytes to the shell". That cannot be read off this
// input: serve.shellInput hands the plaintext of the inner messages on as one
// byte STREAM (apps/server-controller/classes/serve/app.go), so the boundary
// of a message is gone by the time it arrives here, and a keys frame without
// a length has no end. A parser that guesses one can be pushed out of step by
// whoever sends the bytes, and from then on every resize is read out of the
// middle of a paste. Hence the length on every frame, which is the deviation
// from the plan this file carries; the app protocol was explicitly still to be
// agreed with the web session, so nothing frozen moved.
const (
	KindKeys   byte = 1 // payload: 1..MaxKeys bytes for the shell
	KindResize byte = 2 // payload: cols and rows, each uint16 big endian

	// HeaderSize is kind and length.
	HeaderSize = 3
	// MaxKeys bounds one keys frame. A DECISION, not a measurement: it is far
	// above any keystroke and above a pasted screenful, and it keeps the read
	// buffer of this program small. Whoever pastes more sends several frames.
	MaxKeys = 16 << 10
	// MaxOutput is what one write of the output may carry. The host side
	// chunks for the channel on its own (serve.shellOutput); this is the
	// bound of the buffer here and the one place the contract's limit is
	// named.
	MaxOutput = wire.MaxE2EPlaintext - 1
	// MaxDim bounds a window size. A DECISION: a size of zero is no window,
	// and nothing useful is wider than this.
	MaxDim = 2000

	resizeSize = 4
)

// IdleLimit ends a terminal that neither takes input nor gives output for
// this long. A stream action has no time limit by contract, so without this a
// forgotten tab would hold a shell - and in this app that shell runs as root
// in the container. This is the safer of the two readings of the plan, which
// named no limit at all. It counts output too, so a long build is not cut off.
const IdleLimit = 30 * time.Minute

// maxTick bounds how long the idle watchdog sleeps between two looks. It looks
// every quarter of the limit and never less often than this, so with IdleLimit
// it looks twice a minute - not four times in half an hour, which is what the
// quarter alone would give.
const maxTick = 30 * time.Second

// What this program answers. Each is one fixed German line and carries no
// value: see the head of this file.
const (
	msgNoData = "Das Terminal läuft nur im Container der Terminal-App."
	msgNoUser = "Das Terminal öffnet nur ein angemeldeter Benutzer."
	msgNoPTY  = "Das Terminal braucht Linux."
	msgNoRun  = "Die Shell ließ sich nicht starten."
	msgIdle   = "Das Terminal wurde nach langer Untätigkeit beendet."
	msgCut    = "Die Verbindung zum Terminal ist abgebrochen."
	msgBug    = "Das Terminal wurde durch einen internen Fehler beendet."
)

// exitFail is every refusal and every failure of this program itself; a shell
// that ran gives its own code back.
const exitFail = 1

// ErrFrame reports a frame that is none.
var ErrFrame = errors.New("terminal: invalid frame")

// errNoPTY says this build has no pseudo terminal; pty_other.go answers it.
var errNoPTY = errors.New("terminal: no pseudo terminal on this system")

// errIdle and errCut end the run: nothing came or went for the idle limit,
// and the input stopped in the middle of a frame.
var (
	errIdle = errors.New("terminal: idle for too long")
	errCut  = errors.New("terminal: input cut off")
)

// sentence is the one line the person at the terminal reads for an error that
// ended the run. It is a function of its own so that a case can hold the
// mapping on any machine: the run it belongs to needs a pseudo terminal, and
// a mapping measured only where a pty exists is a mapping not measured here.
func sentence(err error) string {
	switch {
	case errors.Is(err, errNoPTY):
		return msgNoPTY
	case errors.Is(err, errIdle):
		return msgIdle
	case errors.Is(err, errCut), errors.Is(err, ErrFrame):
		return msgCut
	case errors.Is(err, errBug):
		// Its own sentence, because msgNoRun would be a FALSE one: the shell
		// did start, and something of ours broke afterwards. A person who is
		// told the wrong cause looks in the wrong place, and a swallowed error
		// that comes out as another error is the false answer of V1.2 point 50.
		return msgBug
	default:
		return msgNoRun
	}
}

// errBug is what ended answers when it caught a programming error. It says
// nothing about what happened -- the kind and the panicking line are in the
// line guard wrote -- and run turns it into one sentence for the person at
// the terminal.
var errBug = errors.New("terminal: a programming error ended this terminal")

// ended runs fn and sends what fn answered into out; a panic in fn goes into
// out as errBug instead. It is how the four goroutines of runShell survive a
// programming error without the process doing so: each of them already says
// how it ended through a channel, and the first answer there ends the shell
// and comes back to the caller.
//
// Without it a panic in one of them was final -- a goroutine is where nothing
// above can catch one -- and it took the whole ngc-controller with it, every
// terminal and every channel of every user (V1.2 point 38). The send matters
// as much as the catch: a caught panic nobody is told about would leave
// runShell waiting on a goroutine that is already gone, which is the silent
// defect of V1.2 point 50 in its worst shape -- a terminal that simply stops.
//
// out must have room for every goroutine that writes to it, and runShell
// buffers for all of them, so the send after a catch cannot block. It is
// written as a select with a default all the same: a send that blocked here
// would hang the goroutine inside its own recover, and a second panic in a
// deferred function that is already handling one cannot be recovered at all.
//
// It lives in this file and not in pty_linux.go so that it is built and
// measured on every platform; the four callers are linux-only.
func ended(out chan<- error, site string, fn func() error) {
	defer func() {
		if guard.HitTo(nil, recover(), site, 0) {
			select {
			case out <- errBug:
			default:
			}
		}
	}()
	out <- fn()
}

// Frame is one message of the shell. Data belongs to the frame and to nothing
// else: ParseFrame copies it out of the read buffer, which the next frame
// overwrites.
type Frame struct {
	Kind       byte
	Data       []byte
	Cols, Rows uint16
}

// ParseFrame reads exactly one whole frame. b has to be the frame and nothing
// more: a caller that hands in a longer buffer would be deciding on its own
// where the frame ends, and that is the one thing this format exists to settle.
func ParseFrame(b []byte) (Frame, error) {
	if len(b) < HeaderSize {
		return Frame{}, ErrFrame
	}
	n := int(binary.BigEndian.Uint16(b[1:HeaderSize]))
	if len(b) != HeaderSize+n {
		return Frame{}, ErrFrame
	}
	switch b[0] {
	case KindKeys:
		if n == 0 || n > MaxKeys {
			return Frame{}, ErrFrame
		}
		// Copied, because the caller reads the next frame into this buffer.
		return Frame{Kind: KindKeys, Data: bytes.Clone(b[HeaderSize:])}, nil
	case KindResize:
		if n != resizeSize {
			return Frame{}, ErrFrame
		}
		cols := binary.BigEndian.Uint16(b[HeaderSize : HeaderSize+2])
		rows := binary.BigEndian.Uint16(b[HeaderSize+2 : HeaderSize+resizeSize])
		if cols == 0 || rows == 0 || cols > MaxDim || rows > MaxDim {
			return Frame{}, ErrFrame
		}
		return Frame{Kind: KindResize, Cols: cols, Rows: rows}, nil
	}
	return Frame{}, ErrFrame
}

// EncodeOutput is the counterpart of ParseFrame for the way back, and it puts
// no frame around anything: the output of the shell travels raw, because the
// other side has nothing to tell apart. It exists so that the bound of one
// write stands in one place and so the web side has a named counterpart.
// Nothing and too much both answer nil.
func EncodeOutput(b []byte) []byte {
	if len(b) == 0 || len(b) > MaxOutput {
		return nil
	}
	return b
}

// shellEnv is the whole environment of the shell. It is built from nothing but
// these five lines and HOME, so NGC_PARAMS - which carries whatever the user
// typed into the app - cannot travel on: a shell that had it would put it in
// front of the next person who reads the process list. This is also the second
// half of "the parameters are decoration" (see the head of this file): there is
// no door here for a cols or rows out of NGC_PARAMS, and a case holds that for
// an environment poisoned with all of them (TestShellEnvCarriesNothing).
//
// It stands in this file and not beside the pseudo terminal, although only
// Linux starts a shell: what the shell is given is a decision, and a decision
// that can only be measured on a machine nobody here has is one nothing holds.
func shellEnv() []string {
	home := os.Getenv("HOME")
	if home == "" {
		home = service.DataDir
	}
	return []string{
		"TERM=xterm-256color",
		"HOME=" + home,
		"PATH=" + service.Path,
		"LANG=C.UTF-8",
		// The empty HISTFILE is the bolt of the one disk a typed secret can
		// reach: the root filesystem of the container is read only and /data
		// is the only writable mount, so HOME above is the encrypted app
		// data. A shell of the bash family would create HOME/.bash_history
		// there and write every typed line into it - a "mysql -p<secret>"
		// would outlive the tab. Empty, bash writes NO file; it is not
		// cleaned up afterwards, it never comes into being
		// (TestShellEnvWritesNoHistory). The image's shell is /bin/sh, which
		// is dash and keeps no history either way, so this line holds for the
		// day that symlink is something else.
		//
		// What it does NOT reach is named instead of hidden: a program the
		// user starts in the shell keeps its own history in HOME the same way
		// (.mysql_history, .psql_history, .lesshst, .viminfo). Keeping those
		// out needs a HOME that is not the persistent mount, which is a tmpfs
		// for the container and lives in package runtime, not here.
		"HISTFILE=",
	}
}

// activity is the clock of the idle limit. Both pumps touch it, the watchdog
// reads it; now is the clock, which a test replaces.
type activity struct {
	limit time.Duration
	now   func() time.Time
	last  atomic.Int64 // UnixNano of the last byte in either direction
}

func newActivity(limit time.Duration, now func() time.Time) *activity {
	a := &activity{limit: limit, now: now}
	a.touch()
	return a
}

func (a *activity) touch() { a.last.Store(a.now().UnixNano()) }

// stale reports whether nothing happened for limit. A limit of zero never
// goes stale, so a caller can switch the watchdog off.
func (a *activity) stale() bool {
	if a.limit <= 0 {
		return false
	}
	return a.now().Sub(time.Unix(0, a.last.Load())) >= a.limit
}

// pumpOut copies the raw output of the shell to out. It ends with nil on every
// end of the pty (see below), and with the error of the write when the channel
// broke: half an answer is not an answer, and the run ends then instead of
// writing into a pipe nobody reads.
func pumpOut(out io.Writer, pty io.Reader, act *activity) error {
	buf := make([]byte, MaxOutput)
	for {
		n, rerr := pty.Read(buf)
		if n > 0 {
			act.touch()
			if _, werr := out.Write(EncodeOutput(buf[:n])); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			// Every end of a pty is an end: it answers EIO once the child is
			// gone, which is its EOF, and there is nothing left to read
			// either way.
			return nil
		}
	}
}

// pumpIn reads frames off the channel and applies them: keys go to the shell,
// a size goes to resize. io.EOF on a frame boundary is the input complete;
// anything in the middle of a frame is errCut. A frame that is none ends the
// run with ErrFrame - this side does not skip past garbage, because a parser
// that resynchronises is one whoever sends the bytes can steer.
func pumpIn(in io.Reader, keys io.Writer, resize func(cols, rows uint16) error, act *activity) error {
	buf := make([]byte, HeaderSize+MaxKeys)
	for {
		if _, err := io.ReadFull(in, buf[:HeaderSize]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil // the input ended between two frames
			}
			return errCut
		}
		n := int(binary.BigEndian.Uint16(buf[1:HeaderSize]))
		if n > MaxKeys {
			return ErrFrame // bounded before anything is read for it
		}
		if _, err := io.ReadFull(in, buf[HeaderSize:HeaderSize+n]); err != nil {
			return errCut
		}
		f, err := ParseFrame(buf[:HeaderSize+n])
		if err != nil {
			return err
		}
		act.touch()
		switch f.Kind {
		case KindKeys:
			if _, err := keys.Write(f.Data); err != nil {
				return err
			}
		case KindResize:
			if err := resize(f.Cols, f.Rows); err != nil {
				return err
			}
		}
	}
}

// watch answers errIdle once nothing has gone either way for the limit. A
// limit of zero switches it off, and then it only waits for the end of the run
// and answers with that.
func watch(ctx context.Context, act *activity) error {
	if act.limit <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	tick := act.limit / 4
	if tick > maxTick {
		tick = maxTick
	}
	if tick <= 0 {
		tick = time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if act.stale() {
				return errIdle
			}
		}
	}
}

// Run is the service program. It reads the frames of the channel from in,
// writes the raw output of the shell to out and gives the exit code of that
// shell back. Every refusal is exitFail with one German line on stderr.
func Run(ctx context.Context, in io.Reader, out io.Writer) int {
	return run(ctx, in, out, os.Stderr, IdleLimit)
}

// run is Run with the two things a test has to set: where the lines go and how
// long idle is. Run itself takes IdleLimit, and TestIdleLimitIsHalfAnHour
// holds that default, because every other case here brings its own.
func run(ctx context.Context, in io.Reader, out, errOut io.Writer, idle time.Duration) int {
	// This program runs in a container the controller built, and the three
	// checks are the fence inside it. The gate is package access on the
	// server (see the head of this file); these say no to a call that reached
	// this program some other way.
	//
	// Order is on purpose: the environment says where this is running before
	// anything says who is behind it, and only then is a shell even thought
	// about.
	if os.Getenv(service.EnvData) != service.DataDir {
		fmt.Fprintln(errOut, msgNoData)
		return exitFail
	}
	// A start action has no user (service.Call.Validate), and a shell with
	// nobody behind it is the one thing this app must never be. The manifest
	// declares no start action; this holds even if one is ever added.
	if os.Getenv(service.EnvStart) == "1" {
		fmt.Fprintln(errOut, msgNoUser)
		return exitFail
	}
	if !trust.ValidID(os.Getenv(service.EnvUser)) {
		fmt.Fprintln(errOut, msgNoUser)
		return exitFail
	}
	code, err := runShell(ctx, in, out, idle)
	if err == nil {
		return code
	}
	fmt.Fprintln(errOut, sentence(err))
	return exitFail
}

// Hold waits until this container is stopped, so an app image needs no program
// of its own. It ends on the signal the engine sends, which is what cancels
// ctx.
func Hold(ctx context.Context) int {
	<-ctx.Done()
	return 0
}
