// What this controller tells an API about itself: the hello at the start of a
// tunnel, the status the API asks for, the progress of an update and a notice
// for the admins. Every one of them goes out as the control message of its
// contract, and none of them asks the API for anything - this is a controller
// telling, not a controller asking.
//
// The parts of the controller that answer these questions are built by later
// tasks, so each of them comes in as a function of ReportConfig. What is not
// wired yet is answered the way a controller that cannot ask answers: no app
// counts as running, every app counts as licensed, and a message without a
// tunnel goes into the log of this server.

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
	// TrustSeq is the newest trust list version this controller has verified
	// (chain.Keeper.Seq). nil: 0, which is what a controller that has verified
	// none says.
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
// operating system under it and the newest trust list version it has verified.
// It fits link.Config.Hello.
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
// Left out is everything the contract would refuse, because a status the API
// refuses costs the tunnel and tells it nothing: a row whose ID is no app ID or
// is one of NGC's own (wire.CoreApp), and a row without a release version - an
// app of which only kept data remains has none (dbapi.App), and a status has no
// room for one. Left out too is everything past tunnel.MaxStatusApps, on a
// server that has more. The API shows this list and decides nothing by it, so a
// short list costs nobody anything.
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
		if !wire.ValidAppID(a.ID) || wire.CoreApp(a.ID) || !store.ValidVersion(a.Version) {
			continue
		}
		s.Apps = append(s.Apps, tunnel.AppStatus{App: a.ID, Version: a.Version, State: r.appState(ctx, a)})
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

// appState is what one app is doing. The licence is asked first: one that ran
// out past its grace period stops the app's services and starts none
// (store.LicenseExpired), so such an app is unlicensed and not merely stopped.
//
// An engine that cannot answer leaves the app failed. The contract has no word
// for "nobody knows" (tunnel.AppState), and of the four it has, failed is the
// one an admin should see - an app nobody can ask about is a problem on this
// server. What went wrong goes into the log of this controller beside it.
func (r *Reporter) appState(ctx context.Context, a dbapi.App) tunnel.AppState {
	if r.cfg.Licensed != nil {
		ok, err := r.cfg.Licensed(ctx, a)
		if err != nil {
			r.log().Warn("the licence of an app could not be read", "app", a.ID, "err", err)
			return tunnel.StateFailed
		}
		if !ok {
			return tunnel.StateUnlicensed
		}
	}
	if r.cfg.Running == nil {
		return tunnel.StateStopped
	}
	up, err := r.cfg.Running(ctx, a.ID)
	if err != nil {
		r.log().Warn("the engine does not say whether an app runs", "app", a.ID, "err", err)
		return tunnel.StateFailed
	}
	if up {
		return tunnel.StateRunning
	}
	return tunnel.StateStopped
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
