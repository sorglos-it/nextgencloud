// What this controller tells an API about itself: the hello at the start of a
// tunnel, the status of this server, the progress of an update and a notice for
// the admins. Every one of them goes out as the control message of its
// contract, and not one of them is an answer to a question: the API may send no
// status, no update and no notice on a tunnel (wire.ControlAllowed), so none of
// these is asked for. This is a controller telling.
//
// The parts of the controller that answer these questions are built by later
// tasks, so each of them comes in as a function of ReportConfig. A part that is
// not wired yet is not the same as one that was asked and did not answer: where
// there is nothing to ask, the answer is the quiet one - no app counts as
// running, every app counts as licensed, and a message without a tunnel goes
// into the log of this server.
//
// Where there is something and it fails, the two sides part: an app whose
// licence or engine failed is left out of the status altogether (appState),
// while a send that failed costs the message nothing but its way out - it goes
// into the log of this server and the Reporter carries on (Notify, Update).

package node

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

const (
	// OSReleaseFile is where a Linux system says what it is.
	OSReleaseFile = "/etc/os-release"
	// maxOSRelease bounds what OSRelease reads of that file and how long one of
	// its lines may be. The file holds a handful of short lines; what this is
	// there for is the other case - a program that runs as root must not read a
	// file into memory because the file said so.
	maxOSRelease = 64 << 10
)

// osPattern is what may stand in the OS field of a hello. wire.ValidName is the
// rule this was to be checked with, and it is very nearly this one: at most 32
// bytes, a lower-case letter first, then lower-case letters, digits and the
// hyphen. The one thing wire.ValidName does not allow is the dot - and
// "ubuntu-24.04", the example the contract itself gives for this very field
// (tunnel.Hello), has one, so following that rule to the letter would leave
// every Ubuntu server without an OS. The dot is therefore allowed here, and
// nothing else is.
//
// It stays stricter than the contract's own rule, which also takes "+" and a
// leading digit (tunnel.ParseHello), so nothing that passes here is refused
// there. TestOSRelease and FuzzOSRelease hold that against the parser of the
// contract itself, not against a second copy of this rule.
var osPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,31}$`)

// errNoTunnel says that this controller has nowhere to send: a message for the
// API goes into the log of this server instead.
var errNoTunnel = errors.New("node: this controller has no tunnel to an API")

// ReportConfig is what a Reporter needs.
type ReportConfig struct {
	Version string   // NGC version of this build (tunnel.Hello.Version)
	Node    string   // its node ID, empty until an admin put this server in a trust list
	Roles   []string // config.Controller.Roles
	OS      string   // what OSRelease read, e.g. debian-13; empty if it read nothing
	State   *state.Store
	// TrustSeq is the number after which this controller needs versions of the
	// trust list, and chain.Keeper.TrustSeq is what answers it: the number of
	// the newest version it verified, or the one below its anchor while it has
	// verified none. nil: 0, which asks for the chain from version 1 on and is
	// right only for a controller anchored at 1.
	TrustSeq func() uint64
	// Running says whether an app is up. The engine of the container task
	// (runtime.Engine.Running) has this signature, and what it takes there is
	// the name of a container: whoever wires the two together maps the app ID
	// onto that name. nil: no app counts as running, because nobody was asked.
	Running func(ctx context.Context, app string) (bool, error)
	// Licensed says whether an app may run at all (license.Allowed of the
	// licence task). nil: every app may.
	Licensed func(ctx context.Context, a dbapi.App) (bool, error)
	// Send puts one control message on the tunnel ((*link.Tunnel).Control).
	// nil: this controller has no tunnel, and the message goes into its log.
	Send func(typ string, body any) error
	Log  *slog.Logger // nil: slog.Default()
}

// Reporter answers for one controller. Nothing of it changes after it was
// built, and what does change - the apps, the trust list, the containers - it
// asks for afresh every time, so a Reporter may be used from any goroutine as
// far as it goes itself. The four functions it was handed are called from
// whichever goroutine calls a Reporter, and whether they bear that is theirs
// to answer.
//
// Status needs State; the other three do not touch it.
type Reporter struct{ cfg ReportConfig }

// NewReporter takes the values of c. The roles are copied, so nobody changes
// the hello of this controller from outside afterwards.
func NewReporter(c ReportConfig) *Reporter {
	c.Roles = slices.Clone(c.Roles)
	return &Reporter{cfg: c}
}

func (r *Reporter) log() *slog.Logger {
	if r.cfg.Log != nil {
		return r.cfg.Log
	}
	return slog.Default()
}

// Hello is the hello of this controller, asked afresh for every dial: the
// version of this build and the contract it was built against, the node and the
// roles of the configuration, the architecture the binary was built for, the
// operating system under it and the trust list version after which it needs the
// chain (ReportConfig.TrustSeq). It fits link.Config.Hello.
//
// Node stays empty until an admin has put this server in a trust list. That is
// no fault of this controller's and no reason not to dial: what the API makes
// of a hello that names another node is the API's business, and what this side
// owes is its own node and nothing else.
func (r *Reporter) Hello() tunnel.Hello {
	var seq uint64
	if r.cfg.TrustSeq != nil {
		seq = r.cfg.TrustSeq()
	}
	return tunnel.Hello{
		Version:  r.cfg.Version,
		Contract: tunnel.Contract,
		Node:     r.cfg.Node,
		TrustSeq: seq,
		Arch:     arch(),
		OS:       r.cfg.OS,
		Roles:    slices.Clone(r.cfg.Roles),
	}
}

// arch names the architecture this binary was built for, and only the two the
// contract knows: on a third one the field stays empty, because a value the API
// refuses would cost the whole tunnel (tunnel.ParseHello).
func arch() string {
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		return runtime.GOARCH
	}
	return ""
}

// Status is what this server is doing: every app it knows with its version and
// its state, the NGC parts its roles carry, and the space left for app data.
//
// Four kinds of row are left out. Two of them the contract cannot carry, and a
// status the API refuses costs the tunnel and tells it nothing: a row of NGC's
// own (wire.CoreApp), which the API serves itself, and a row without a release
// version - an app of which only kept data remains has none (dbapi.App). The
// third is every row behind the one that filled the list to
// tunnel.MaxStatusApps. The fourth is every app this server could not ask
// about (appState).
//
// A row with an ID that is no app ID is not among them: such a row never gets
// this far, because state.Store.Apps refuses the whole read over it
// (state.ErrDocument), and that error comes back out of here. A damaged
// database is nothing to report around.
//
// The API shows this list and decides nothing by it, so a short list costs
// nobody anything.
func (r *Reporter) Status(ctx context.Context) (tunnel.Status, error) {
	apps, err := r.cfg.State.Apps(ctx)
	if err != nil {
		return tunnel.Status{}, err
	}
	s := tunnel.Status{DiskFree: diskFree(config.AppDataDir)}
	for _, a := range apps {
		if len(s.Apps) == tunnel.MaxStatusApps {
			break
		}
		// The version is either a release version or empty: state.Store.Apps
		// lets nothing else through, so the empty one is the only case here.
		if wire.CoreApp(a.ID) || a.Version == "" {
			continue
		}
		st, ok := r.appState(ctx, a)
		if !ok {
			continue
		}
		s.Apps = append(s.Apps, tunnel.AppStatus{App: a.ID, Version: a.Version, State: st})
	}
	comp, err := r.cfg.State.Components(ctx)
	if err != nil && !errors.Is(err, dbapi.ErrNotFound) {
		return tunnel.Status{}, err
	}
	// Only the parts the roles of this server carry are reported: what its
	// state holds of another role says nothing about this one. The map stays
	// away entirely while nothing matches, the way a producer leaves out every
	// empty list (tunnel.Status).
	mine := tunnel.Components(r.cfg.Roles)
	for c, v := range comp {
		if !slices.Contains(mine, c) {
			continue
		}
		if s.Components == nil {
			s.Components = make(map[string]string, len(mine))
		}
		s.Components[c] = v
	}
	return s, nil
}

// appState is what one app is doing, and whether that can be said at all. The
// licence is asked first: one that ran out past its grace period stops the
// app's services and starts none (store.LicenseExpired), so such an app is
// unlicensed and not merely stopped.
//
// Two things end with nothing to say, and they are the only two: the licence
// could not be read, or the engine does not say whether the app runs. Both give
// false, and Status then leaves the app out of the report. None of the four
// words of the contract is bent over them, failed least of all: failed is a
// state an app is really in, and the one an admin acts on - a second meaning
// "nobody could ask" would make it the one word of the four that says two
// things. The contract has no word for that and no field to carry it
// (tunnel.Status and tunnel.AppStatus have three each, none about time), so the
// row stays away and the reason goes into the log of this controller.
//
// What that costs, said plainly: in the message the app looks like one that is
// not installed on this server at all - the same picture a core app and a row
// without a version leave, and on a server with more than 256 apps to report,
// the same one the limit leaves. It costs no other app its place: the limit
// counts the rows that went in, and a row that is left out never gets that far.
// The log line is what tells them apart on this server.
//
// And the log line is all there is: no notice goes out from here, and that is a
// decision and not something forgotten. A status is nothing the API asks for -
// it may send none (wire.ControlAllowed) - this side sends one of its own
// accord, again and again, so a notice per status would repeat itself just as
// often. The only usable rule would be "only when it changes", and a change
// needs the answer of the round before. This Reporter has nowhere of its own to
// keep it: its own fields do not change after it was built. It could be written
// into the database behind ReportConfig.State - but that would be new state to
// keep, and a rule invented here to go with it, and a rule invented here is
// exactly what was taken out of this function when failed went. The answer of
// the round before belongs where such a history is kept anyway, which is the
// task that brings the engine (ReportConfig.Running), not here.
func (r *Reporter) appState(ctx context.Context, a dbapi.App) (tunnel.AppState, bool) {
	if r.cfg.Licensed != nil {
		ok, err := r.cfg.Licensed(ctx, a)
		if err != nil {
			r.log().Warn("the licence of an app could not be read, it stays out of the status",
				"app", a.ID, "err", err)
			return "", false
		}
		if !ok {
			return tunnel.StateUnlicensed, true
		}
	}
	if r.cfg.Running == nil {
		return tunnel.StateStopped, true
	}
	up, err := r.cfg.Running(ctx, a.ID)
	if err != nil {
		r.log().Warn("the engine does not say whether an app runs, it stays out of the status",
			"app", a.ID, "err", err)
		return "", false
	}
	if up {
		return tunnel.StateRunning, true
	}
	return tunnel.StateStopped, true
}

// SendStatus puts the status of this server on the tunnels of this controller.
// It is what the 60-second tick of Run calls, and it gives nothing back for
// the same reason Notify and Update do not: a status nobody asked for
// (wire.ControlAllowed - an API may send none) is nothing a caller can act on,
// and both ways it can fail belong in the log of this server.
//
// Those two ways are kept apart in that log on purpose: a status that could
// not be BUILT means this controller cannot read its own apps, which is a
// fault of this server, while one that could not be SENT means there was no
// tunnel or the sender refused it. The first is worth an operator's attention
// and the second is the normal state of a server whose API is away.
func (r *Reporter) SendStatus(ctx context.Context) {
	if r.cfg.State == nil {
		r.log().Warn("no status was built, this controller has no local state")
		return
	}
	s, err := r.Status(ctx)
	if err != nil {
		r.log().Warn("no status was built, this controller could not read its own apps", "err", err)
		return
	}
	if err := r.send(wire.CtlStatus, s); err != nil {
		r.log().Info("status stayed on this server", "apps", len(s.Apps), "err", err)
	}
}

// Notify tells the API something an admin should read: level is one of
// core.NoticeInfo, NoticeWarn and NoticeError, text is German, and app names the
// app it is about or stays empty.
//
// It gives nothing back. What did not get out - because this controller has no
// tunnel, or because the sender refused it, which (*link.Tunnel).Control does
// for every body its contract does not take - is written into the log of this
// server instead: a notice that falls silent is worse than one nobody reads
// twice.
//
// That log line holds the text as it came in. Whoever calls this puts his text
// into the journal of a server that runs as root, so a caller who has something
// of an API in his hands has it there too.
func (r *Reporter) Notify(level, text, app string) {
	if err := r.send(wire.CtlNotify, core.Notice{Level: level, Text: text, App: app}); err != nil {
		r.log().Warn("notice stayed on this server", "level", level, "text", text, "app", app, "err", err)
	}
}

// Update tells the API how an update is getting on (store.UpdateReport). Like
// Notify it gives nothing back and logs what did not get out.
func (r *Reporter) Update(u store.UpdateReport) {
	if err := r.send(wire.CtlUpdate, u); err != nil {
		r.log().Warn("update report stayed on this server", "target", u.Target, "state", u.State, "err", err)
	}
}

// send puts one control message on the tunnel, and says so when there is none.
func (r *Reporter) send(typ string, body any) error {
	if r.cfg.Send == nil {
		return errNoTunnel
	}
	return r.cfg.Send(typ, body)
}

// OSRelease names the operating system of this server for the hello, e.g.
// "debian-13": the ID and the VERSION_ID of an os-release file, joined with a
// hyphen. The path is an argument so a test can point it somewhere else; on a
// server it is OSReleaseFile.
//
// That file belongs to the system and not to a contract: it may be missing, and
// what is in it may be quoted, may have lines without an "=" and may be
// rubbish of any length. So at most maxOSRelease bytes are read, and anything
// that is not a value the hello may carry leaves the answer empty - an empty
// answer is a hello without an OS, which the contract allows.
//
// An error says the file could not be read or not be read to the end. It is
// never an answer: a controller that gets one says nothing about its system and
// goes on.
func OSRelease(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return scanOSRelease(f)
}

// parseOSRelease is OSRelease over bytes that are already in memory, for the
// fuzzing. It reads exactly as much of them as OSRelease reads of the file,
// because the bound sits in scanOSRelease: what is fuzzed here is the way the
// server really goes and not a longer one.
func parseOSRelease(b []byte) (string, error) { return scanOSRelease(bytes.NewReader(b)) }

// scanOSRelease reads the lines of an os-release file and builds the name of
// the system out of ID and VERSION_ID. A line without an "=" is none of this
// file and is stepped over; of the keys it does know, the last one in the file
// wins, the way a shell reading the same file would have it.
//
// This is the one place that bounds the reading, so every way in is bounded:
// at most maxOSRelease bytes are read, however much stands behind r, and no
// single line may be longer than that either.
func scanOSRelease(r io.Reader) (string, error) {
	var id, version string
	sc := bufio.NewScanner(io.LimitReader(r, maxOSRelease))
	// A line longer than everything this reads at all is no line of this file.
	// The scanner says so instead of growing for it; the buffer starts small,
	// because the lines of this file are short.
	sc.Buffer(make([]byte, 0, 4096), maxOSRelease)
	for sc.Scan() {
		// The bytes of the line and not sc.Text: a file of nothing but line ends
		// has as many lines as it has bytes, and of all of them only the two
		// values this is after ever need to become strings. Measured over
		// 64 KiB of line ends, the worst this reader can be handed: 1.14 ms
		// against 1.88 ms with a string per line.
		line := sc.Bytes()
		i := bytes.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		switch string(line[:i]) {
		case "ID":
			id = string(unquote(line[i+1:]))
		case "VERSION_ID":
			version = string(unquote(line[i+1:]))
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if id == "" || version == "" {
		return "", nil
	}
	name := id + "-" + version
	if !osPattern.MatchString(name) {
		return "", nil
	}
	return name, nil
}

// unquote takes exactly one pair of quotes off a value, double or single, the
// way an os-release file writes them. A quote on one side alone is no pair and
// stays where it is - and a value with a quote in it is no name, so the answer
// stays empty.
func unquote(s []byte) []byte {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
