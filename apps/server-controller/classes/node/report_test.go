package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// newState is an empty local state for one test.
func newState(t *testing.T) *state.Store {
	t.Helper()
	s := state.New(memstore.New())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// reporterFor builds a Reporter over st with the values a paired controller
// carries. Each test turns the one field it is about.
func reporterFor(t *testing.T, st *state.Store, turn func(*ReportConfig)) *Reporter {
	t.Helper()
	c := ReportConfig{
		Version:  "1.2.3",
		Node:     "ctl-7",
		Roles:    []string{tunnel.RoleWeb, tunnel.RoleAPI},
		OS:       "debian-13",
		State:    st,
		TrustSeq: func() uint64 { return 5 },
		Log:      slog.New(slog.DiscardHandler),
	}
	if turn != nil {
		turn(&c)
	}
	return NewReporter(c)
}

// The hello names this controller: its node out of the configuration, the
// contract of this build, its roles, its architecture, its operating system and
// the newest trust list version it has verified.
func TestHelloNamesNodeRolesAndContract(t *testing.T) {
	r := reporterFor(t, newState(t), nil)
	h := r.Hello()
	if h.Node != "ctl-7" || h.Contract != tunnel.Contract || h.Version != "1.2.3" ||
		h.TrustSeq != 5 || h.OS != "debian-13" {
		t.Errorf("hello = %+v", h)
	}
	if !slices.Equal(h.Roles, []string{tunnel.RoleWeb, tunnel.RoleAPI}) {
		t.Errorf("roles = %v", h.Roles)
	}
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		if h.Arch != runtime.GOARCH {
			t.Errorf("arch = %q, want %q", h.Arch, runtime.GOARCH)
		}
	} else if h.Arch != "" {
		// The contract knows two architectures. On a third one the field stays
		// empty, because a value the API refuses would cost the tunnel.
		t.Errorf("arch = %q on %s, want it left out", h.Arch, runtime.GOARCH)
	}
	// What is built here has to be a hello of the contract, with a node and
	// without one: the controller is in no trust list until an admin lists it.
	mustParseHello(t, h)
	empty := reporterFor(t, newState(t), func(c *ReportConfig) { c.Node = "" }).Hello()
	if empty.Node != "" {
		t.Errorf("node = %q, want it left out", empty.Node)
	}
	mustParseHello(t, empty)
	// The roles belong to the Reporter: whoever gets a hello holds no list of
	// another.
	h.Roles[0] = tunnel.RoleDB
	if again := r.Hello(); again.Roles[0] != tunnel.RoleWeb {
		t.Errorf("the second hello has the roles of the first: %v", again.Roles)
	}
}

// mustParseHello sends h through the parser of the contract, the one the API
// runs on it.
func mustParseHello(t *testing.T, h tunnel.Hello) {
	t.Helper()
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := tunnel.ParseHello(b); err != nil {
		t.Fatalf("the API refuses %s: %v", b, err)
	}
}

// A controller with more apps than a status may carry reports as many as the
// contract takes and no more, and the message goes through the parser of the
// API without a complaint.
func TestStatusFitsTheLimits(t *testing.T) {
	ctx := context.Background()
	st := newState(t)
	const many = 300
	for i := range many {
		if err := st.PutApp(ctx, dbapi.App{ID: fmt.Sprintf("org.sorglos.app%03d", i), Version: "1.0.0"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutComponents(ctx, map[string]string{
		trust.AutoController: "1.2.3", trust.AutoAPI: "1.2.3", trust.AutoWeb: "1.2.3",
	}); err != nil {
		t.Fatal(err)
	}
	r := reporterFor(t, st, func(c *ReportConfig) {
		c.Running = func(context.Context, string) (bool, error) { return true, nil }
	})
	s, err := r.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(s.Apps) != tunnel.MaxStatusApps {
		t.Errorf("apps = %d, want %d of %d", len(s.Apps), tunnel.MaxStatusApps, many)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := tunnel.ParseStatus(b); err != nil {
		t.Fatalf("the API refuses the status: %v", err)
	}
	// The roles decide which parts are reported: this server has web and api,
	// so all three parts belong to it.
	if got := slices.Sorted(maps.Keys(s.Components)); !slices.Equal(got, tunnel.Components(r.Hello().Roles)) {
		t.Errorf("components = %v", got)
	}
	if s.DiskFree < 0 {
		t.Errorf("diskFree = %d", s.DiskFree)
	}
}

// A server with the role services alone reports only the part every server
// carries, whatever its state holds.
func TestStatusReportsOnlyTheComponentsOfTheseRoles(t *testing.T) {
	ctx := context.Background()
	st := newState(t)
	if err := st.PutComponents(ctx, map[string]string{
		trust.AutoController: "1.2.3", trust.AutoAPI: "1.2.3", trust.AutoWeb: "1.2.3",
	}); err != nil {
		t.Fatal(err)
	}
	r := reporterFor(t, st, func(c *ReportConfig) { c.Roles = []string{tunnel.RoleServices} })
	s, err := r.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(s.Components) != 1 || s.Components[trust.AutoController] != "1.2.3" {
		t.Errorf("components = %v, want only %s", s.Components, trust.AutoController)
	}
}

// What an app is doing: the engine says running or stopped, a licence that ran
// out past its grace period says unlicensed, and an engine that cannot answer
// at all leaves the app as failed - the contract has no word for "nobody
// knows", and an app nobody can ask about is something an admin should see.
func TestStatusSaysWhatTheAppsAreDoing(t *testing.T) {
	ctx := context.Background()
	st := newState(t)
	for _, id := range []string{"org.sorglos.a", "org.sorglos.b", "org.sorglos.c", "org.sorglos.d"} {
		if err := st.PutApp(ctx, dbapi.App{ID: id, Version: "1.0.0"}); err != nil {
			t.Fatal(err)
		}
	}
	// An app of which only kept data remains carries no version, and a status
	// has no room for one: it is left out instead of costing the message.
	if err := st.PutApp(ctx, dbapi.App{ID: "org.sorglos.gone"}); err != nil {
		t.Fatal(err)
	}
	broken := errors.New("the engine does not answer")
	r := reporterFor(t, st, func(c *ReportConfig) {
		c.Running = func(_ context.Context, app string) (bool, error) {
			switch app {
			case "org.sorglos.a":
				return true, nil
			case "org.sorglos.d":
				return false, broken
			}
			return false, nil
		}
		c.Licensed = func(_ context.Context, a dbapi.App) (bool, error) {
			return a.ID != "org.sorglos.c", nil
		}
	})
	s, err := r.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	want := map[string]tunnel.AppState{
		"org.sorglos.a": tunnel.StateRunning,
		"org.sorglos.b": tunnel.StateStopped,
		"org.sorglos.c": tunnel.StateUnlicensed,
		"org.sorglos.d": tunnel.StateFailed,
	}
	if len(s.Apps) != len(want) {
		t.Fatalf("apps = %+v, want %d of them", s.Apps, len(want))
	}
	for _, a := range s.Apps {
		if a.State != want[a.App] {
			t.Errorf("%s = %q, want %q", a.App, a.State, want[a.App])
		}
	}
}

// A controller without an engine says nothing is running: no app may be called
// running because nobody asked.
func TestStatusWithoutAnEngineSaysStopped(t *testing.T) {
	ctx := context.Background()
	st := newState(t)
	if err := st.PutApp(ctx, dbapi.App{ID: "org.sorglos.a", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	s, err := reporterFor(t, st, nil).Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(s.Apps) != 1 || s.Apps[0].State != tunnel.StateStopped {
		t.Errorf("apps = %+v, want one stopped app", s.Apps)
	}
}

// The free space is read with a call of Linux; everywhere else it stays 0,
// which is what a status of a controller that cannot measure it says.
func TestDiskFreeReadsTheFreeSpace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("free disk space is read with a call of Linux")
	}
	if free := diskFree(t.TempDir()); free <= 0 {
		t.Errorf("diskFree = %d, want the free space of the temporary directory", free)
	}
	// A directory that is not there is no reason to fail: the status goes out
	// without a number.
	if free := diskFree(filepath.Join(t.TempDir(), "nowhere")); free != 0 {
		t.Errorf("diskFree of a missing directory = %d, want 0", free)
	}
}

// A notice and an update report go out as the control messages of their
// contract; what no tunnel can take is written into the log of this server
// instead, because a notice is meant for an admin and must not fall silent.
func TestNotifyAndUpdateGoOutAsControlMessages(t *testing.T) {
	type message struct {
		typ  string
		body any
	}
	var sent []message
	failing := false
	r := reporterFor(t, newState(t), func(c *ReportConfig) {
		c.Send = func(typ string, body any) error {
			if failing {
				return errors.New("no tunnel")
			}
			sent = append(sent, message{typ, body})
			return nil
		}
	})
	r.Notify(core.NoticeWarn, "Die Platte wird knapp.", "org.sorglos.terminal")
	r.Update(store.UpdateReport{Target: trust.AutoAPI, Version: "1.1.0", State: store.UpdateInstalled})
	if len(sent) != 2 || sent[0].typ != wire.CtlNotify || sent[1].typ != wire.CtlUpdate {
		t.Fatalf("sent = %+v", sent)
	}
	n, ok := sent[0].body.(core.Notice)
	if !ok || n.Level != core.NoticeWarn || n.App != "org.sorglos.terminal" {
		t.Errorf("notice = %+v", sent[0].body)
	}
	// Both bodies have to be ones the API takes.
	for _, m := range sent {
		b, err := json.Marshal(m.body)
		if err != nil {
			t.Fatal(err)
		}
		switch m.typ {
		case wire.CtlNotify:
			if _, err := core.ParseNotice(b); err != nil {
				t.Errorf("the API refuses the notice: %v", err)
			}
		case wire.CtlUpdate:
			if _, err := store.ParseUpdateReport(b); err != nil {
				t.Errorf("the API refuses the update report: %v", err)
			}
		}
	}
	// A Reporter whose tunnel is gone, and one that never had a tunnel, must
	// not panic and must not lose the message without a word.
	failing = true
	r.Notify(core.NoticeError, "Ein Dienst ist ausgefallen.", "")
	NewReporter(ReportConfig{Version: "1.2.3", Roles: []string{tunnel.RoleServices}, Log: slog.New(slog.DiscardHandler)}).
		Notify(core.NoticeInfo, "Niemand hört zu.", "")
}

// /etc/os-release is a file of a Linux system and not a contract: it may be
// missing, it may have quotes, lines without an "=" and ten megabytes of
// rubbish, and none of it may upset a program that runs as root.
func TestOSRelease(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{name: "debian", content: "PRETTY_NAME=\"Debian GNU/Linux 13\"\nID=debian\nVERSION_ID=\"13\"\n", want: "debian-13"},
		{name: "ubuntu", content: "ID=ubuntu\nVERSION_ID=\"24.04\"\n", want: "ubuntu-24.04"},
		{name: "no version", content: "ID=debian\n"},
		{name: "no id", content: "VERSION_ID=\"13\"\n"},
		{name: "empty"},
		{name: "lines without an equals sign", content: "ID=debian\nnonsense\n\n=\nVERSION_ID=13", want: "debian-13"},
		{name: "a quote on one side only", content: "ID=\"debian\nVERSION_ID=13\n"},
		{name: "an ID that is no name", content: "ID=Debian 13\nVERSION_ID=13\n"},
		{name: "a version that is no name", content: "ID=debian\nVERSION_ID=\"13 (trixie)\"\n"},
		{name: "the last line without an end", content: "ID=debian\nVERSION_ID=13", want: "debian-13"},
		{name: "windows line ends", content: "ID=debian\r\nVERSION_ID=\"13\"\r\n", want: "debian-13"},
		// Ten megabytes in one line is no line of this file, and a megabyte of
		// lines is more than the reader takes: both end without an answer,
		// although the second one has the answer standing in it - behind the
		// 64 KiB the reader is given, where nothing of this file reaches.
		{name: "ten megabytes in one line", content: strings.Repeat("x", 10<<20), wantErr: true},
		{name: "a megabyte of lines and the answer behind it", content: strings.Repeat("x=y\n", 1<<18) + "ID=debian\nVERSION_ID=13\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(dir, "os-release")
			if err := os.WriteFile(p, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := OSRelease(p)
			if (err != nil) != c.wantErr {
				t.Fatalf("OSRelease = %q, %v; want an error: %v", got, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("OSRelease = %q, want %q", got, c.want)
			}
			// Whatever comes out has to be something the hello may carry.
			mustParseHello(t, tunnel.Hello{Version: "1.2.3", Contract: tunnel.Contract, OS: got})
		})
	}
	// A file that is not there is no answer and no crash either: a controller
	// without /etc/os-release says nothing about its system and runs on.
	if got, err := OSRelease(filepath.Join(dir, "nowhere")); err == nil || got != "" {
		t.Errorf("a file that is not there gave %q, %v", got, err)
	}
}

// osReleaseSeeds are the fixed inputs FuzzOSRelease starts from. They stand
// here and not in the target itself because the test below measures over them
// what a run of the fuzzer cannot: how many of them reach the accepting way at
// all. The fuzzer steers its own inputs, and the rejecting ways carry the
// coverage, so a share it counted would sink whatever the parser does.
var osReleaseSeeds = [][]byte{
	[]byte("ID=debian\nVERSION_ID=\"13\"\n"),
	[]byte("ID=ubuntu\nVERSION_ID=\"24.04\"\n"),
	[]byte("ID=\"\"\nVERSION_ID=\n"),
	[]byte("ID=debian\nnonsense\n=\nVERSION_ID=13"),
	[]byte(strings.Repeat("x", 70<<10)),
}

// More than half the seeds have to come out with a name. The check of the
// contract inside FuzzOSRelease only runs on an answer, so seeds that all ended
// empty would leave it a clean run that checked nothing.
func TestOSReleaseSeedsReachTheAnswer(t *testing.T) {
	named := 0
	for _, s := range osReleaseSeeds {
		got, err := parseOSRelease(s)
		if err != nil {
			continue
		}
		if got != "" {
			named++
		}
	}
	if named*2 <= len(osReleaseSeeds) {
		t.Errorf("%d of %d seeds come out with a name, want more than half", named, len(osReleaseSeeds))
	}
}

// Whatever the file says, what comes out is either nothing or a value the hello
// of the contract carries - the parser of the API is what decides that, not a
// rule written twice. The canary runs the same way at every input and cannot be
// steered away: a parser that answered nothing at all for everything would look
// like a clean run otherwise.
//
// One thing to know before reading a run of this: the counter stands still at
// "0/sec" for as long as a worker is minimizing a newly interesting input, and
// this parser is handed inputs of 64 KiB, which cost about a millisecond each -
// so a minimization of one of them takes the default minute and the run looks
// stalled. It is not. Measured at the same commit: with -fuzzminimizetime 100x
// the same target holds about 225 000 inputs a second and 4.5 million of them
// in twenty seconds.
func FuzzOSRelease(f *testing.F) {
	for _, s := range osReleaseSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if canary, err := parseOSRelease([]byte("ID=debian\nVERSION_ID=\"13\"\n")); err != nil || canary != "debian-13" {
			t.Fatalf("the canary reads %q (%v), want debian-13", canary, err)
		}
		got, err := parseOSRelease(b)
		if err != nil && got != "" {
			t.Fatalf("parseOSRelease = %q with the error %v, want nothing", got, err)
		}
		if got == "" {
			return
		}
		h, err := json.Marshal(tunnel.Hello{Version: "1.2.3", Contract: tunnel.Contract, OS: got})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tunnel.ParseHello(h); err != nil {
			t.Fatalf("the API refuses OS %q: %v", got, err)
		}
	})
}
