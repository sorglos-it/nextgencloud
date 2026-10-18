package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/apache"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/chain"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// quiet is the log of every case here: what Run writes is measured by what it
// did, not by its lines.
func mute() *slog.Logger { return slog.New(slog.DiscardHandler) }

// runStand is one controller in continuous operation, with every seam of
// RunConfig written down instead of done.
type runStand struct {
	t    *testing.T
	st   *state.Store
	cfg  RunConfig
	mu   sync.Mutex
	seen []string // what happened, in order
	// what the seams answer
	resumeErr  error
	restoreErr error
	compareErr error
	// keep is what the stub Keep does; nil waits for the context.
	keep func(ctx context.Context, cfg link.Config)
	ctls []string // the control messages that went out
}

func newRunStand(t *testing.T) *runStand {
	t.Helper()
	st := newState(t)
	s := &runStand{t: t, st: st}
	rep := NewReporter(ReportConfig{State: st, Log: mute(), Send: func(typ string, _ any) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.ctls = append(s.ctls, typ)
		return nil
	}})
	s.cfg = RunConfig{
		State: st, Chain: chain.New(st, trust.Anchor{Seq: 1}), Report: rep, Log: mute(),
		// Short enough that a case does not wait, long enough that nothing
		// fires while a case is only looking at the order of the start.
		Status: 20 * time.Millisecond, Purge: 20 * time.Millisecond,
		Web: time.Hour, Grace: 50 * time.Millisecond,
		Resume: func(context.Context) error {
			s.note("resume")
			return s.resumeErr
		},
		Restore: func(context.Context) error {
			s.note("restore")
			return s.restoreErr
		},
		Keep: func(ctx context.Context, cfg link.Config) {
			s.note("keep " + cfg.API.URL)
			if s.keep != nil {
				s.keep(ctx, cfg)
				return
			}
			<-ctx.Done()
		},
	}
	return s
}

func (s *runStand) note(what string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, what)
}

func (s *runStand) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.seen)
}

func (s *runStand) controls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ctls)
}

// api adds one API to the configuration. Nothing dials it: Keep is a stub.
func (s *runStand) api(url string) {
	s.cfg.APIs = append(s.cfg.APIs, link.Config{API: config.APIRef{URL: url}})
}

// start runs Run in a goroutine and answers with the cancel and a channel that
// carries what Run came back with.
func (s *runStand) start() (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, s.cfg) }()
	return cancel, done
}

// wait takes the answer of Run, or fails: a Run that does not come back is the
// failure this whole task is about.
func answerOf(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not come back")
		return nil
	}
}

// Every half Run cannot do without is answered the same way and before
// anything of this machine is touched.
func TestRunNeedsItsThreeHalves(t *testing.T) {
	st := newState(t)
	whole := RunConfig{State: st, Chain: chain.New(st, trust.Anchor{Seq: 1}),
		Report: NewReporter(ReportConfig{State: st, Log: mute()})}
	for name, break_ := range map[string]func(c *RunConfig){
		"no state":  func(c *RunConfig) { c.State = nil },
		"no chain":  func(c *RunConfig) { c.Chain = nil },
		"no report": func(c *RunConfig) { c.Report = nil },
	} {
		c := whole
		ran := false
		c.Restore = func(context.Context) error { ran = true; return nil }
		break_(&c)
		if err := Run(context.Background(), c); !errors.Is(err, ErrNotWired) {
			t.Errorf("%s: Run = %v, want %v", name, err, ErrNotWired)
		}
		if ran {
			t.Errorf("%s: Run restored over a controller that is not wired up", name)
		}
	}
}

// The order of the start is the whole statement of this file: the interrupted
// update is decided FIRST, the apps come up SECOND, and no tunnel is dialled
// before either of them. A server whose apps waited for an API would not be a
// server.
func TestRunResumesThenRestoresThenDials(t *testing.T) {
	s := newRunStand(t)
	s.api("wss://one.example:8443/tunnel")
	s.keep = func(ctx context.Context, _ link.Config) {
		// Both must have happened before this goroutine was started.
		if got := s.log(); len(got) < 2 || got[0] != "resume" || got[1] != "restore" {
			t.Errorf("a tunnel was dialled after %v", got)
		}
		<-ctx.Done()
	}
	cancel, done := s.start()
	// The stub Keep notes itself, so waiting for three entries waits for the
	// dial without a sleep.
	until(t, func() bool { return len(s.log()) >= 3 })
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
	got := s.log()
	want := []string{"resume", "restore", "keep wss://one.example:8443/tunnel"}
	if !slices.Equal(got, want) {
		t.Fatalf("Run did %v, want %v", got, want)
	}
}

// An interrupted update this program could not decide is NOT fatal: the row
// stays marked and the next start decides it, while the apps of this server
// come up either way.
func TestAResumeThatCouldNotDecideDoesNotHoldTheAppsBack(t *testing.T) {
	s := newRunStand(t)
	s.resumeErr = errors.New("the store did not answer")
	cancel, done := s.start()
	until(t, func() bool { return slices.Contains(s.log(), "restore") })
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

// A restore that could not read the apps of this server IS fatal: such a
// controller knows nothing about what it did not start, and it says so instead
// of standing there looking well.
func TestARestoreThatCouldNotReadTheAppsStopsTheController(t *testing.T) {
	s := newRunStand(t)
	s.restoreErr = errors.New("the apps could not be read")
	s.api("wss://one.example:8443/tunnel")
	err := Run(context.Background(), s.cfg)
	if !errors.Is(err, s.restoreErr) {
		t.Fatalf("Run = %v, want %v", err, s.restoreErr)
	}
	if got := s.log(); slices.Contains(got, "keep wss://one.example:8443/tunnel") {
		t.Fatalf("a tunnel was dialled although the restore failed: %v", got)
	}
}

// The clock sends the status of this server on its own, without anyone asking
// (wire.ControlAllowed: an API may send none).
func TestRunSendsTheStatusOnItsOwnClock(t *testing.T) {
	s := newRunStand(t)
	cancel, done := s.start()
	until(t, func() bool { return len(s.controls()) >= 2 })
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
	for _, typ := range s.controls() {
		if typ != wire.CtlStatus {
			t.Fatalf("the clock sent %q, want only %q", typ, wire.CtlStatus)
		}
	}
}

// The hourly purge really frees the one-time task IDs that have expired, and
// it does it over the store Run was handed: the ID may be used again
// afterwards, which is what a freed row means.
func TestRunPurgesTheExpiredTaskIDs(t *testing.T) {
	ctx := context.Background()
	s := newRunStand(t)
	// The clock of the purge is of this case, so the row is expired when it
	// fires and the result cannot come from the time of the machine.
	const expires = int64(1000)
	s.cfg.Now = func() int64 { return expires + 1 }
	id := bytes.Repeat([]byte{3}, task.IDSize)
	if err := s.st.UseTask(ctx, id, expires, expires-1); err != nil {
		t.Fatal(err)
	}
	if err := s.st.UseTask(ctx, id, expires, expires-1); err == nil {
		t.Fatal("the same task ID was used twice before anything was purged")
	}
	cancel, done := s.start()
	// Measured by the thing itself and not by a sleep: the row is free again.
	until(t, func() bool { return s.st.UseTask(ctx, id, expires*3, expires+2) == nil })
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// Every tunnel that came off releases the certificate check of a web server,
// and it does so without holding the goroutine that reads that tunnel: the
// check runs in a goroutine of its own, so a Keep that announces a tunnel and
// goes on reading is not waiting for it.
func TestEveryTunnelThatCameOffReleasesTheCertificateCheck(t *testing.T) {
	s := newRunStand(t)
	var checks atomic.Int64
	s.cfg.Compare = func(context.Context) error {
		checks.Add(1)
		return nil
	}
	s.api("wss://one.example:8443/tunnel")
	s.keep = func(ctx context.Context, cfg link.Config) {
		cfg.TunnelUp(nil) // a tunnel came off; nothing of it is read here
		<-ctx.Done()
	}
	cancel, done := s.start()
	until(t, func() bool { return checks.Load() >= 1 })
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// A server without the role web checks nothing, and the goroutine that would
// check still ends with the context rather than staying behind.
func TestWithoutTheRoleWebNothingIsChecked(t *testing.T) {
	s := newRunStand(t)
	s.cfg.Compare = nil
	s.cfg.Sync = nil
	cancel, done := s.start()
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// A version of the trust list that was kept brings Apache in line with it, and
// that is the half nothing ran before: apache.Writer.Sync is the only one that
// puts a NEW API server into the balancer, the trust file and web.json, and it
// had no caller outside its own test file. Measured at a two-machine
// installation on 17.10.2026: a second API was taken into the list (anchor
// seq 1 -> 2) and apache-apis.conf kept its one BalancerMember.
//
// The certificate half does NOT run for it: the certificate of a node Sync
// just wrote is the one it fetched to check that node's pin.
func TestAKeptTrustListBringsApacheInLineWithIt(t *testing.T) {
	s := newRunStand(t)
	listed := make(chan struct{}, 1)
	s.cfg.Listed = listed
	var syncs, compares atomic.Int64
	s.cfg.Sync = func(context.Context) error {
		syncs.Add(1)
		return nil
	}
	s.cfg.Compare = func(context.Context) error {
		compares.Add(1)
		return nil
	}
	cancel, done := s.start()
	listed <- struct{}{}
	until(t, func() bool { return syncs.Load() >= 1 })
	if n := compares.Load(); n != 0 {
		t.Fatalf("a kept trust list also ran the certificate check %d times", n)
	}
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// And the other way round: a tunnel that came off runs the certificate check
// and NOT the list. The two signals are not one.
func TestATunnelThatCameOffDoesNotRewriteTheBalancer(t *testing.T) {
	s := newRunStand(t)
	var syncs, compares atomic.Int64
	s.cfg.Sync = func(context.Context) error {
		syncs.Add(1)
		return nil
	}
	s.cfg.Compare = func(context.Context) error {
		compares.Add(1)
		return nil
	}
	s.api("wss://one.example:8443/tunnel")
	s.keep = func(ctx context.Context, cfg link.Config) {
		cfg.TunnelUp(nil)
		<-ctx.Done()
	}
	cancel, done := s.start()
	until(t, func() bool { return compares.Load() >= 1 })
	if n := syncs.Load(); n != 0 {
		t.Fatalf("a tunnel that came off rewrote the balancer %d times", n)
	}
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// A Sync that failed is retried, and the daily beat is what retries it: Listed
// fires once per version, so a version that arrived while an API was away
// would otherwise leave the balancer behind the list until the next admin
// changed something. The beat runs both halves.
func TestTheDailyBeatRunsTheListAndTheCertificates(t *testing.T) {
	s := newRunStand(t)
	s.cfg.Web = 10 * time.Millisecond
	var syncs, compares atomic.Int64
	s.cfg.Sync = func(context.Context) error {
		syncs.Add(1)
		return errors.New("an API was not reachable")
	}
	s.cfg.Compare = func(context.Context) error {
		compares.Add(1)
		return nil
	}
	cancel, done := s.start()
	// Twice, so it is the BEAT and not one run: a Sync that answered with an
	// error is asked again.
	until(t, func() bool { return syncs.Load() >= 2 && compares.Load() >= 1 })
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// The grace period bounds the WAIT and nothing else: a link that does not come
// back does not keep this program alive, because a controller systemd has to
// kill is worse than a goroutine left behind in a process that is ending.
func TestTheLinksGetTheirGraceAndRunEndsAnyway(t *testing.T) {
	s := newRunStand(t)
	s.cfg.Grace = 30 * time.Millisecond
	// A link that ignores its context. It is released at the end of the case
	// so nothing of it outlives the test.
	stuck := make(chan struct{})
	defer close(stuck)
	s.api("wss://one.example:8443/tunnel")
	s.keep = func(context.Context, link.Config) { <-stuck }
	cancel, done := s.start()
	until(t, func() bool { return len(s.log()) >= 3 })
	start := time.Now()
	cancel()
	if err := answerOf(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
	// The bound is what is measured, not the length: a Run that came back
	// before the grace period would mean it never waited at all.
	if took := time.Since(start); took < s.cfg.Grace {
		t.Fatalf("Run came back after %v, before its grace period of %v", took, s.cfg.Grace)
	}
}

// The registry is what the status goes out over. It holds the tunnels that
// stand and nothing else, and it says so when none does - which is the normal
// state of a server whose API is away, and which the Reporter turns into a
// line in the log of this server.
func TestTheRegistryHoldsOnlyTheTunnelsThatStand(t *testing.T) {
	var r Tunnels
	if r.Len() != 0 {
		t.Fatalf("a fresh registry holds %d tunnels", r.Len())
	}
	if err := r.Control(wire.CtlStatus, nil); !errors.Is(err, errNoTunnel) {
		t.Fatalf("Control without a tunnel = %v, want %v", err, errNoTunnel)
	}
	// The tunnels are only ever compared by pointer here, so two distinct
	// zero values are two distinct tunnels.
	a, b := &link.Tunnel{}, &link.Tunnel{}
	r.Up(a)
	r.Up(b)
	if r.Len() != 2 {
		t.Fatalf("the registry holds %d tunnels, want 2", r.Len())
	}
	r.Down(a)
	if r.Len() != 1 {
		t.Fatalf("the registry holds %d tunnels after one ended, want 1", r.Len())
	}
	// A tunnel taken out twice takes nothing else with it.
	r.Down(a)
	if r.Len() != 1 {
		t.Fatalf("the registry holds %d tunnels, want 1", r.Len())
	}
	r.Down(b)
	if err := r.Control(wire.CtlStatus, nil); !errors.Is(err, errNoTunnel) {
		t.Fatalf("Control after the last tunnel ended = %v, want %v", err, errNoTunnel)
	}
}

// The local store of this controller is really opened now, and openState is
// the one call that does it. What is measured is what that call puts together:
// the file and the key of controller.json become a config.DB of the SQLite
// driver, and the store that comes back is usable at once.
func TestOpenStateOpensTheFileOfTheConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	cfg := &config.Controller{Name: "srv-1", Node: "ctl-1",
		DB: path, DBKey: config.NewSecret(bytes.Repeat([]byte{9}, 64))}
	st, err := openState(cfg)
	if err != nil {
		t.Fatalf("openState: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("openState opened no file at the path of the configuration: %v", err)
	}
	if _, err := st.Apps(context.Background()); err != nil {
		t.Fatalf("the store openState gave back is not usable: %v", err)
	}
}

// A key that is no key is answered by openState and leaves no file behind:
// the length is checked in state.Open before anything is opened.
func TestOpenStateRefusesAKeyThatIsNoKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	cfg := &config.Controller{Name: "srv-1",
		DB: path, DBKey: config.NewSecret(bytes.Repeat([]byte{9}, 32))}
	if st, err := openState(cfg); err == nil {
		st.Close()
		t.Fatal("openState took a 32-byte key")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("openState left a file behind for a key that is no key")
	}
}

// What this controller writes into the migration lock. It has to name this
// program and this server, it must never be empty - migrate.Apply refuses an
// empty owner - and the node is only in it once there is one: a freshly
// paired server has no node ID at all.
func TestTheOwnerOfTheMigrationLockNamesThisController(t *testing.T) {
	withNode := owner(&config.Controller{Name: "srv-1", Node: "ctl-1"})
	without := owner(&config.Controller{Name: "srv-1"})
	for name, got := range map[string]string{"with a node": withNode, "without a node": without} {
		if got == "" {
			t.Errorf("%s: the owner is empty, which migrate.Apply refuses", name)
		}
		if !strings.Contains(got, "ngc-controller") {
			t.Errorf("%s: the owner %q does not name this program", name, got)
		}
		if !strings.Contains(got, "srv-1") {
			t.Errorf("%s: the owner %q does not name this server", name, got)
		}
		if got == "ngc" {
			t.Errorf("%s: the owner says nothing", name)
		}
	}
	if !strings.Contains(withNode, "ctl-1") {
		t.Errorf("the owner %q leaves the node ID out", withNode)
	}
	if strings.Contains(without, "//") {
		t.Errorf("the owner %q carries an empty part", without)
	}
}

// The command without a command is built now: it reads the configuration
// first, so a server that was never set up reads the line that tells it so and
// not a stack trace.
func TestMainWithoutACommandRunsTheController(t *testing.T) {
	var out, errOut strings.Builder
	if code := Main("1.2.3", nil, noInput(), &out, &errOut); code != 1 {
		t.Fatalf("Main(nil) = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), msgNoConfig) {
		t.Fatalf("Main(nil) stderr = %q, want %q", errOut.String(), msgNoConfig)
	}
	if strings.Contains(errOut.String(), "noch nicht gebaut") {
		t.Fatal("Main(nil) still answers like a part that is not built")
	}
}

// runWith stops at a store that does not open and says one German line, and
// the line names no file: the path of the database has no business on the
// screen of whoever typed the command. The configuration it reads is the one
// ulh leaves behind, so what stops the start is the store and nothing before
// it.
func TestRunWithStopsAtAStoreThatDoesNotOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	cfg := writeController(t, path)
	// The real opener, and the file it stumbles over may not open on ANY
	// platform. The path writeController leaves behind
	// (/var/lib/ngc/controller.db) does not do that: on windows the store
	// refuses it by its own grammar (filepath.IsAbs is false for it there),
	// but on linux as root the very same path OPENS, runWith goes on to start
	// the whole controller, and this case never ends - measured on
	// 18.10.2026, where it held the run of this package for 628 s until it
	// was killed. /dev/null is a character device and no directory, so every
	// open below it fails with ENOTDIR, while the path still passes both
	// grammars (config.DB.Validate wants the leading "/", sqlite.validPath
	// wants an absolute, clean name).
	cfg.DB = "/dev/null/controller.db"
	if err := config.SaveController(path, cfg); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	var out, errOut strings.Builder
	if code := runWith("1.2.3", path, openState, &out, &errOut); code != 1 {
		t.Fatalf("runWith = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), msgNoState) {
		t.Fatalf("stderr = %q, want %q", errOut.String(), msgNoState)
	}
	if strings.Contains(errOut.String(), cfg.DB) {
		t.Fatalf("stderr names the database file: %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

// A configuration that is not there is answered before the store is reached at
// all: nothing of this server is opened for a server that was never set up.
func TestRunWithStopsAtAConfigurationThatIsNotThere(t *testing.T) {
	opened := false
	open := func(*config.Controller) (*state.Store, error) {
		opened = true
		return nil, nil
	}
	var out, errOut strings.Builder
	code := runWith("1.2.3", filepath.Join(t.TempDir(), "nothing.json"), open, &out, &errOut)
	if code != 1 {
		t.Fatalf("runWith = %d, want 1", code)
	}
	if opened {
		t.Fatal("the store was opened although there is no configuration")
	}
	if !strings.Contains(errOut.String(), msgNoConfig) {
		t.Fatalf("stderr = %q, want %q", errOut.String(), msgNoConfig)
	}
}

// What wireUp hands which part. It opens no socket and starts nothing, so this
// is the whole of the wiring measured in one call.
func TestWireUpBuildsEveryHalfOutOfTheConfiguration(t *testing.T) {
	st := newState(t)
	cfg := &config.Controller{
		Node: "ctl-1", Name: "srv-1", Roles: []string{tunnel.RoleServices},
		NoiseKey: config.NewSecret(make([]byte, 32)), DBKey: config.NewSecret(make([]byte, 64)),
		APIs: []config.APIRef{{URL: "wss://one.example:8443/tunnel"}, {URL: "wss://two.example:8443/tunnel"}},
	}
	key, err := staticKey(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c := wireUp("1.2.3", cfg, key, st, chain.New(st, trust.Anchor{Seq: 1}))
	if len(c.APIs) != len(cfg.APIs) {
		t.Fatalf("%d tunnels wired, want %d", len(c.APIs), len(cfg.APIs))
	}
	for i, l := range c.APIs {
		if l.API.URL != cfg.APIs[i].URL {
			t.Fatalf("tunnel %d dials %q", i, l.API.URL)
		}
		if l.Handler == nil || l.Hello == nil || l.TunnelUp == nil || l.TunnelDown == nil {
			t.Fatalf("tunnel %d is half wired: %+v", i, l)
		}
	}
	if c.Restore == nil {
		t.Fatal("nothing brings the apps of this server back up")
	}
	// No role web: nothing checks the certificates of the APIs and nothing
	// writes Apache's files.
	if c.Compare != nil {
		t.Fatal("a server without the role web checks certificates")
	}
	if c.Sync != nil {
		t.Fatal("a server without the role web writes Apache's files")
	}
	// But the signal is wired whatever the roles are: it is the serve side
	// that sends on it, and that side knows nothing of roles.
	if c.Listed == nil {
		t.Fatal("nothing can tell this controller that the trust list grew")
	}
	// The hello of this controller is the one the Reporter builds, with the
	// node and the roles of the configuration in it.
	h := c.APIs[0].Hello()
	if h.Node != cfg.Node || !slices.Equal(h.Roles, cfg.Roles) || h.Version != "1.2.3" {
		t.Fatalf("the hello is %+v", h)
	}
	// And the updater is NOT wired: an install channel of this build ends
	// E_INTERNAL, which is what the comment at Deps.Updater says and what the
	// cases of serve hold.
	web := &config.Controller{Node: cfg.Node, NoiseKey: cfg.NoiseKey, DBKey: cfg.DBKey,
		Roles: []string{tunnel.RoleWeb}}
	w := wireUp("1.2.3", web, key, st, chain.New(st, trust.Anchor{Seq: 1}))
	if w.Compare == nil {
		t.Fatal("a server with the role web checks no certificates")
	}
	// Both halves, because neither does the other's work: Compare renews the
	// certificate of a node already in the trust file, Sync is the only one
	// that puts a server the list has newly got into the balancer, the trust
	// file and web.json.
	if w.Sync == nil {
		t.Fatal("a server with the role web never writes the balancer out of the trust list")
	}
}

// The public half of this controller's Noise key is derived from the private
// one and never read from the configuration, so the two cannot disagree.
func TestTheNoiseKeyIsDerivedFromTheOneHalfThatIsStored(t *testing.T) {
	priv := make([]byte, 32)
	priv[0] = 7
	key, err := staticKey(&config.Controller{NoiseKey: config.NewSecret(priv)})
	if err != nil {
		t.Fatal(err)
	}
	if len(key.Public) != 32 {
		t.Fatalf("the public key is %d bytes", len(key.Public))
	}
	again, err := staticKey(&config.Controller{NoiseKey: config.NewSecret(priv)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(key.Public, again.Public) {
		t.Fatal("the same private key gave two public keys")
	}
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := staticKey(&config.Controller{NoiseKey: config.NewSecret(make([]byte, n))}); err == nil {
			t.Errorf("a key of %d bytes was taken", n)
		}
	}
}

// until waits until want is true, and fails instead of hanging. Nothing here
// sleeps for a fixed time: every case waits for the thing it measures.
func until(t *testing.T, want func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !want() {
		if time.Now().After(deadline) {
			t.Fatal("what this case waits for did not happen")
		}
		time.Sleep(time.Millisecond)
	}
}

// The default of RestoreConfig.Run is serve.RunService, and until this task
// nothing ever took it: every case of task 18 puts its own function in, so the
// line was compiled and never run ("gebaut ist nicht gelaufen"). This is the
// case that runs it.
//
// What it measures, exactly, and the bound on it is named rather than hidden:
// Restore really calls serve.RunService, that call really comes back with a
// wire.Close, and Restore really reads it - the line in the log is written by
// the branch that reads that Close and by nothing else. What it does NOT reach
// on this machine is the engine: serve.RunService writes the environment of
// the call into config.RunDir ("/run/ngc") before it starts anything, and that
// directory is of a Linux server. So on a Linux server the same case sees the
// engine as well, and everywhere else it sees the default run as far as that
// file. The one thing it rules out either way is the one this task is about:
// that nothing called the default at all.
func TestTheDefaultOfRestoreReallyRunsTheService(t *testing.T) {
	lines := &countLog{}
	s := newRestoreStand(t)
	s.cfg.Run = nil // the default, which is what is measured here
	s.cfg.Log = slog.New(lines)
	s.put(firstApp, true, true)

	if err := Restore(context.Background(), s.cfg); err != nil {
		t.Fatal(err)
	}

	name := runtime.UserName(firstApp)
	// The container was brought up either way.
	if got := s.eng.log(); !slices.Contains(got, "start "+name) {
		t.Fatalf("the engine saw %v", got)
	}
	// Nothing was written down by the stand: the call did not go through the
	// seam a case puts in, it went through the default.
	if len(s.ran) != 0 {
		t.Fatalf("%d calls went through the seam of the case", len(s.ran))
	}
	// And the start action really ran and really answered: exactly one line
	// about it, written by the branch that reads what RunService gave back.
	if got := lines.about("start action"); got != 1 {
		t.Fatalf("%d lines about the start action, want 1 - the default never answered", got)
	}
}

// countLog counts the lines written to it and keeps their messages.
type countLog struct {
	mu   sync.Mutex
	msgs []string
}

func (c *countLog) Enabled(context.Context, slog.Level) bool { return true }

func (c *countLog) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, r.Message)
	return nil
}

func (c *countLog) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *countLog) WithGroup(string) slog.Handler      { return c }

// about counts the lines whose message holds this text.
func (c *countLog) about(what string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		if strings.Contains(m, what) {
			n++
		}
	}
	return n
}

// The installer this controller hands an activate channel is wired up in
// full: every field install.Activate reads in its own guard is there, so a
// channel that arrives gets a refusal of its task and not "the installer is
// not wired up".
//
// That is what the two errors tell apart, and it is the whole of this case.
// A Manager missing one of State, Engine, Sys, List, StageDir, Max, LastSeq
// or Now answers install.ErrInternal for every activation whatever the task
// says; this one answers install.ErrForbidden, which is the guard behind it -
// the one that asks for a signed root task. A decision without a task is the
// cheapest way past the first guard and into the second.
func TestTheInstallerOfThisControllerIsWiredUpInFull(t *testing.T) {
	st := newState(t)
	cfg := &config.Controller{
		Node: "ctl-1", Name: "srv-1", Roles: []string{tunnel.RoleServices},
		NoiseKey: config.NewSecret(make([]byte, 32)), DBKey: config.NewSecret(make([]byte, 64)),
	}
	i := installer{cfg: cfg, version: "1.2.3", state: st, engine: ctrlrt.NewPodman("podman"),
		node: func() string { return cfg.Node }}
	_, err := i.Activate(context.Background(), &trust.List{}, access.Decision{},
		bytes.NewReader(nil))
	if errors.Is(err, install.ErrInternal) {
		t.Fatalf("the installer is wired wrong: %v", err)
	}
	if !errors.Is(err, install.ErrForbidden) {
		t.Fatalf("an activation without a task gave %v, want install.ErrForbidden", err)
	}
	// And the catalogue counter is wired to this controller's own state: 0
	// for a store nothing was ever activated from, which is "none seen".
	if n := i.lastSeq(context.Background())("ngc"); n != 0 {
		t.Errorf("lastSeq of an unseen store = %d, want 0", n)
	}
}

// TestWireUpLetsTheListOutrankTheFile holds the precedence of wireUp's nodeID,
// and it is a decision, not a detail: the VERIFIED LIST decides what this
// server's node is, and controller.json only remembers it. A file that
// outranked the list would go on naming a node an admin has renamed or taken
// out, and the API ends the tunnel of a controller that names another node
// than its key carries (controllers.control).
//
// A mutant that turns the two around passed every other case of this package
// on 17.10.2026 — nothing held the order until this one.
func TestWireUpLetsTheListOutrankTheFile(t *testing.T) {
	ctx := context.Background()
	st := newState(t)
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	// The list carries this server as "ctl-1" under the Noise key of
	// trusttest.NodeKeys(0xC1); the file still remembers the old name.
	ctlDH, _ := trusttest.NodeKeys(0xC1)
	cfg := &config.Controller{
		Node: "ctl-alt", Name: "srv-1", Roles: []string{tunnel.RoleServices},
		NoiseKey: config.NewSecret(ctlDH.Private), DBKey: config.NewSecret(make([]byte, 64)),
		APIs: []config.APIRef{{URL: "wss://one.example:8443/tunnel"}},
	}
	key, err := staticKey(cfg)
	if err != nil {
		t.Fatal(err)
	}
	keeper := chain.New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := keeper.Load(ctx); err != nil {
		t.Fatal(err)
	}
	c := wireUp("1.2.3", cfg, key, st, keeper)
	if got := c.APIs[0].Hello().Node; got != "ctl-1" {
		t.Fatalf("the hello names %q, want %q out of the list", got, "ctl-1")
	}
	// The other way round is the fallback, and it has to stay: a server that
	// no verified version lists keeps what the file remembers, so a controller
	// paired but not listed yet still says what it was told.
	bare := &config.Controller{
		Node: "ctl-alt", Name: "srv-1", Roles: []string{tunnel.RoleServices},
		NoiseKey: config.NewSecret(make([]byte, 32)), DBKey: config.NewSecret(make([]byte, 64)),
		APIs: []config.APIRef{{URL: "wss://one.example:8443/tunnel"}},
	}
	bareKey, err := staticKey(bare)
	if err != nil {
		t.Fatal(err)
	}
	cb := wireUp("1.2.3", bare, bareKey, st, chain.New(st, trust.Anchor{Seq: 1}))
	if got := cb.APIs[0].Hello().Node; got != "ctl-alt" {
		t.Fatalf("without a list the hello names %q, want %q out of the file", got, "ctl-alt")
	}
}

// webSync is the half that owns the LIST, and this measures it at the files:
// a trust list with two API nodes gives a balancer with two members, a trust
// file with two certificates and a web.json with two addresses.
//
// It is the fix of what was measured at a two-machine installation on
// 17.10.2026: a second API joined, an admin took it into the trust list (the
// anchor went from seq 1 to 2) and apache-apis.conf kept its ONE
// BalancerMember, api-trust.pem its one certificate and web.json its one
// address - because nothing in this program ever called apache.Writer.Sync.
func TestTheBalancerGetsEveryAPIOfTheTrustList(t *testing.T) {
	dir := t.TempDir()
	balancer := filepath.Join(dir, "apache-apis.conf")
	trustFile := filepath.Join(dir, "api-trust.pem")
	webConfig := filepath.Join(dir, "web.json")
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	// The second API, as one stands in the list after a join: own keys, own
	// address, own TLS pin. Sorted by ID, which Validate wants.
	dh, sign := trusttest.NodeKeys(0xB2)
	cert2, pin2 := trusttest.APITLS(0xE2, "api-2.test")
	second := trust.Node{ID: "api-2", Kind: trust.NodeAPI, Name: "api-2",
		Noise: enc.Bytes(dh.Public), Sign: enc.Bytes(sign.Public().(ed25519.PublicKey)),
		URL: "wss://api-2.test:7443/tunnel", TLS: enc.Bytes(pin2)}
	if !trust.ValidAPINode(second) {
		t.Fatal("the second API node of this case is no valid API node")
	}
	l.Nodes = append(l.Nodes, second)
	slices.SortFunc(l.Nodes, func(a, b trust.Node) int { return strings.Compare(a.ID, b.ID) })
	if err := l.Validate(); err != nil {
		t.Fatalf("a list nobody could have signed proves nothing: %v", err)
	}
	cert1, _ := trusttest.APITLS(0xE1, "api-1.test")
	certs := map[string][]byte{
		"wss://api-1.test:7443/tunnel": cert1.Certificate[0],
		"wss://api-2.test:7443/tunnel": cert2.Certificate[0],
	}
	var tests, reloads int
	w := apache.New(apache.Options{
		WebConfig: webConfig, TrustFile: trustFile, BalancerFile: balancer,
		Fetch: func(_ context.Context, n trust.Node) ([]byte, error) {
			der, ok := certs[n.URL]
			if !ok {
				return nil, errors.New("no server at this address")
			}
			return der, nil
		},
		ConfigTest: func(context.Context) error { tests++; return nil },
		Reload:     func(context.Context) error { reloads++; return nil },
	})
	st := newState(t)
	v1 := admin.Sign(t, l)
	if err := st.PutVersion(context.Background(), v1, 1); err != nil {
		t.Fatal(err)
	}
	k := chain.New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	sync := webSync(w, k)
	if err := sync(context.Background()); err != nil {
		t.Fatalf("webSync = %v", err)
	}
	conf, err := os.ReadFile(balancer)
	if err != nil {
		t.Fatalf("no balancer was written: %v", err)
	}
	if n := strings.Count(string(conf), "BalancerMember"); n != 2 {
		t.Errorf("the balancer names %d servers, want 2:\n%s", n, conf)
	}
	for _, host := range []string{"https://api-1.test:7443", "https://api-2.test:7443"} {
		if !strings.Contains(string(conf), host) {
			t.Errorf("the balancer does not name %s:\n%s", host, conf)
		}
	}
	pem, err := os.ReadFile(trustFile)
	if err != nil {
		t.Fatalf("no trust file was written: %v", err)
	}
	if n := strings.Count(string(pem), "BEGIN CERTIFICATE"); n != 2 {
		t.Errorf("the trust file holds %d certificates, want 2", n)
	}
	web, err := os.ReadFile(webConfig)
	if err != nil {
		t.Fatalf("web.json was not written: %v", err)
	}
	if n := strings.Count(string(web), ":7443"); n != 2 {
		t.Errorf("web.json names %d addresses, want 2:\n%s", n, web)
	}
	if tests != 1 || reloads != 1 {
		t.Errorf("%d config tests and %d reloads, want 1 and 1", tests, reloads)
	}
	// A second run writes nothing: the files already say what the list says,
	// so the daily beat costs no reload.
	if err := sync(context.Background()); err != nil {
		t.Fatalf("the second webSync = %v", err)
	}
	if reloads != 1 {
		t.Errorf("a second run reloaded Apache again (%d reloads)", reloads)
	}
}

// A version that names no API server at all is not written: a balancer with no
// member makes Apache answer every /api/ with 503, and what it has keeps
// working instead.
func TestAListWithoutAnAPIDoesNotEmptyTheBalancer(t *testing.T) {
	dir := t.TempDir()
	balancer := filepath.Join(dir, "apache-apis.conf")
	if err := os.WriteFile(balancer, []byte("BalancerMember \"https://api-1.test:7443\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	l.Nodes = slices.DeleteFunc(l.Nodes, func(n trust.Node) bool { return n.Kind == trust.NodeAPI })
	st := newState(t)
	v1 := admin.Sign(t, l)
	if err := st.PutVersion(context.Background(), v1, 1); err != nil {
		t.Fatal(err)
	}
	k := chain.New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := apache.New(apache.Options{
		WebConfig: filepath.Join(dir, "web.json"), TrustFile: filepath.Join(dir, "api-trust.pem"),
		BalancerFile: balancer,
		Fetch: func(context.Context, trust.Node) ([]byte, error) {
			return nil, errors.New("nothing should be fetched")
		},
		ConfigTest: func(context.Context) error { return errors.New("nothing should be tested") },
		Reload:     func(context.Context) error { return errors.New("nothing should be reloaded") },
	})
	if err := webSync(w, k)(context.Background()); err == nil {
		t.Fatal("a list without an API server was written without a word")
	}
	b, err := os.ReadFile(balancer)
	if err != nil || !strings.Contains(string(b), "api-1.test") {
		t.Fatalf("the balancer that was there is gone: %q (%v)", b, err)
	}
}

// The installer takes this server's node ID from the VERIFIED LIST and asks
// for it afresh on every activation - not out of controller.json, and not
// frozen at the start.
//
// This is the bug the running installation showed on 18.10.2026 and the
// reason this case exists. Pairing writes controller.json without a node
// (main.go), so an installer that read the file held every signed root task
// against "" - and task.Check answers "malformed task or other target",
// which the channel ends E_VERIFY and the user reads as a package that does
// not check out. The task was fine; the server did not know its own name.
//
// Measured against both halves: the file alone is NOT enough, and the list
// alone IS - and the second half is what a frozen value would fail, because
// the ID arrives with a version of the list that is verified later.
func TestTheInstallerAsksForItsNodeIDAfreshAndLetsTheListDecide(t *testing.T) {
	st := newState(t)
	cfg := &config.Controller{
		Name: "srv-1", Roles: []string{tunnel.RoleServices},
		NoiseKey: config.NewSecret(make([]byte, 32)), DBKey: config.NewSecret(make([]byte, 64)),
	}
	// What pairing leaves behind: a configuration with no node at all.
	if cfg.Node != "" {
		t.Fatal("this case needs a configuration without a node")
	}
	var id string
	i := installer{cfg: cfg, version: "1.2.3", state: st, engine: ctrlrt.NewPodman("podman"),
		node: func() string { return id }}
	// Read off install.Deps and not off nodeOf, and that is the point of the
	// case: a first version of it called nodeOf and stayed GREEN with the
	// node taken from the configuration again, because nothing it looked at
	// was the line that hands the ID over (measured 18.10.2026).
	if got := i.deps(context.Background(), &trust.List{}).Node; got != "" {
		t.Fatalf("the installer hands over node %q before any list did", got)
	}
	// Read once per activation and not once per installer: the second call
	// has to see the ID the first one did not.
	id = "ngc-2"
	if got := i.deps(context.Background(), &trust.List{}).Node; got != "ngc-2" {
		t.Fatalf("the installer hands over node %q after the list did, want ngc-2", got)
	}
	// And a node in the file does NOT outrank the list: this is the same
	// precedence wireUp.nodeID holds, carried into the activation.
	cfg.Node = "aus-der-datei"
	if got := i.deps(context.Background(), &trust.List{}).Node; got != "ngc-2" {
		t.Fatalf("the file outranked the list: node %q, want ngc-2", got)
	}
	// The list of the channel is the list the Manager is built on, not one
	// this installer reached for.
	l := &trust.List{Seq: 7}
	if got := i.deps(context.Background(), l); got.List != l {
		t.Error("the Manager was built on another list than the channel's")
	}
}

// THE TWO HALVES ARE WIRED TO THE SAME MEMORY, and that is the whole of this
// case: Deps.LastSeq reading a number nothing writes is 0 for ever, and
// store.VerifyCatalog then has no floor to refuse an older catalogue against.
// Measured through the state of this controller and not through a stub, because
// the stub is what could agree with itself.
func TestTheFloorOfAStoreGoesThroughTheStateOfThisController(t *testing.T) {
	st := newState(t)
	cfg := &config.Controller{
		Node: "ctl-1", Name: "srv-1", Roles: []string{tunnel.RoleServices},
		NoiseKey: config.NewSecret(make([]byte, 32)), DBKey: config.NewSecret(make([]byte, 64)),
	}
	i := installer{cfg: cfg, version: "1.2.3", state: st, engine: ctrlrt.NewPodman("podman"),
		node: func() string { return cfg.Node }}
	ctx := t.Context()
	read := i.lastSeq(ctx)
	if n := read("global"); n != 0 {
		t.Fatalf("lastSeq of an unseen store = %d, want 0", n)
	}
	if err := i.keepSeq(ctx, "global", 7); err != nil {
		t.Fatalf("keepSeq = %v", err)
	}
	// The SAME reader, built before the write: it reads the state at every
	// call and holds no number of its own, so an activation after another one
	// sees what the earlier one wrote.
	if n := read("global"); n != 7 {
		t.Errorf("lastSeq after keepSeq(7) = %d, want 7", n)
	}
	// And the floor does not go down again.
	if err := i.keepSeq(ctx, "global", 3); err != nil {
		t.Fatalf("keepSeq(3) = %v", err)
	}
	if n := read("global"); n != 7 {
		t.Errorf("lastSeq after keepSeq(3) = %d, want the floor left at 7", n)
	}
	// What deps hands the Manager is this pair and not something beside it.
	d := i.deps(ctx, &trust.List{})
	if d.LastSeq == nil || d.KeepSeq == nil {
		t.Fatal("deps hands the Manager no memory at all")
	}
	if n := d.LastSeq("global"); n != 7 {
		t.Errorf("the LastSeq of deps = %d, want the 7 in the state", n)
	}
}

// A READ THAT FAILS REFUSES EVERY CATALOGUE. install.Deps.LastSeq has no error
// in it, so the only answers are numbers, and 0 is the one that opens the
// door: a controller that cannot read its own memory would let any catalogue
// through. MaxUint64 is above every number a catalogue can carry, so it
// refuses them all.
func TestAFloorThatCannotBeReadRefusesEveryCatalogue(t *testing.T) {
	i := installer{cfg: &config.Controller{}, version: "1.2.3"}
	read := i.lastSeq(t.Context())
	if n := read("global"); n != math.MaxUint64 {
		t.Errorf("lastSeq without a state = %d, want MaxUint64 and no catalogue through", n)
	}
	// A store without a name is no store, and 0 for it would be "none seen"
	// for every store at once.
	if n := read(""); n != math.MaxUint64 {
		t.Errorf("lastSeq of a nameless store = %d, want MaxUint64", n)
	}
	if err := i.keepSeq(t.Context(), "global", 7); err == nil {
		t.Error("keepSeq without a state answered no error")
	}
	// And the case that matters more, because a state is always there in
	// operation: the state is wired, and the READ of it fails. 0 here is the
	// open door, and a mutant that took the error for 0 passed every other
	// case of this change.
	broken := state.New(brokenRead{Local: memstore.New(), err: errors.New("no read")})
	t.Cleanup(func() { _ = broken.Close() })
	b := installer{cfg: &config.Controller{}, version: "1.2.3", state: broken}
	if n := b.lastSeq(t.Context())("global"); n != math.MaxUint64 {
		t.Errorf("lastSeq over a database that cannot be read = %d, want MaxUint64", n)
	}
	if err := b.keepSeq(t.Context(), "global", 7); err == nil {
		t.Error("keepSeq over a database that cannot be read answered no error")
	}
}

// brokenRead is a database whose BlobGet fails and whose every other call is
// the real one: what is measured is the one read.
type brokenRead struct {
	dbapi.Local
	err error
}

func (b brokenRead) BlobGet(context.Context, string, string) (dbapi.Blob, error) {
	return dbapi.Blob{}, b.err
}
