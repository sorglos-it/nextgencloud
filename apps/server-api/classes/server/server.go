// Package server puts the API together: configuration, database, TLS, the
// paths of the shell and of the controllers, and the background rounds. It
// starts only when database and keys are in order and stops with a clear
// German message otherwise (Entwurf 4.6), instead of running half way.
//
// This package is the one place that knows every other one. That is on
// purpose: every classes/ package below it names what it needs as a field or
// an interface and nothing reaches sideways, so the wiring is readable in one
// file and a seam that is NOT wired shows up here as a field nobody sets.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/apitls"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/coreapp"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/join"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/relay"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/setup"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/updates"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// SetupCodeFile is where the one-time code of the first setup lies: root's
// alone, never in the database and never in a log. The ulh script reads it.
var SetupCodeFile = filepath.Join(config.DataDir, "setup-code")

// SpoolDir is where appstore writes a package while it checks it and sends it
// on (appstore.Stores.Spool). DECISION, and the two things the contract notes
// asked to be weighed (Punkt 30) are weighed here:
//
//   - It is a directory of THIS server, under config.DataDir, and never
//     anything shared. Several API servers share the database and not the
//     disk, so a spool is only ever good for the one run that made it. That is
//     all it is used for: appstore.Activate writes the package, reads it twice
//     and removes it again within the one call (task 22, mutant spool-kept).
//   - Nothing waits here BETWEEN two calls, and that is a GAP and not a
//     solution. Task 24 wrote here that the shell sends the uploaded package
//     again on the activate channel; read against shared/core that is wrong.
//     ngc.store/controller/activate is a FlowTransfer action, and
//     core.Action.MaxIn says MaxIn bounds there "what the API sends the
//     controller -- the shell sends none, even when it opened the channel",
//     while core.ShellOpens says the shell sends EOF at once. So nobody holds
//     the package of an uploaded app between ngc.store/upload (which keeps
//     only the ui-tar) and its activation, and an activation out of
//     core.LocalStore answers E_UNREACHABLE. That is Punkt 30 of
//     docs/vertraege-v1.2.md, still open, and coreapp.storeActivate says it
//     again where a reader of the handler will see it.
//     An app out of a store of the trust list is not affected: its package is
//     downloaded here against the checksum of the verified catalog entry.
//
// It is not the directory of the operating system: a package may be gigabytes
// and /tmp is a RAM disk on many systems. Build sweeps it, see sweepSpool.
var SpoolDir = filepath.Join(config.DataDir, "spool")

// errNoTunnel reports that the controller a licence is meant for is away.
var errNoTunnel = errors.New("server: no tunnel to that controller")

// Server is one running API.
type Server struct {
	Cfg   *config.API
	State *state.State
	Cert  tls.Certificate

	handler http.Handler
	login   *login.Handler
	setup   *setup.Handler
	st      *appstore.Stores
	up      *updates.Updates
	core    *coreapp.Dispatcher
	relay   *relay.Relay
	ctl     *controllers.Server
	tunnels *controllers.Registry
	// sessions are the open shells. The notice of a controller goes to all of
	// them (Server.notice), so this server needs the same registry the socket
	// server and the dispatcher have.
	sessions *browser.Registry

	// spool is the directory of this run, SpoolDir unless a test moves it.
	spool string
	// round is held while a background round runs. DECISION on "how many at
	// once": exactly one. A round that outlives its tick must not start a
	// second one on top of itself -- the hourly round fetches catalogs over
	// the network and writes their rows, and two of them would write the same
	// rows against each other. A tick that finds the round busy is dropped and
	// not queued: the next tick does the same work anyway.
	round sync.Mutex
}

// Build makes everything the API needs and fails if anything is missing.
func Build(ctx context.Context, cfg *config.API, version string) (*Server, error) {
	box, err := secrets.NewBox(cfg.SecretKey)
	if err != nil {
		return nil, errors.New("der geheime Schlüssel in api.json ist unbrauchbar")
	}
	// config.LoadAPI has checked the length already; this says so where it
	// matters, because without this key session.Challenge cannot answer a name
	// that does not exist, and the API would have to give itself away.
	if len(cfg.ChallengeKey.Bytes()) != 32 {
		return nil, errors.New("der Schlüssel challengeKey in api.json ist unbrauchbar")
	}
	host, err := apitls.Host(cfg.URL)
	if err != nil {
		return nil, errors.New("die Adresse url in api.json ist keine Tunnel-Adresse")
	}
	cert, pin, err := apitls.Ensure(cfg.Cert, cfg.CertKey, host, time.Now())
	if err != nil {
		return nil, errors.New("das TLS-Zertifikat der API lässt sich nicht anlegen oder lesen")
	}
	// The owner of the migration lock. On SQLite this very call migrates, so
	// the name has to be in place before the open and not after it.
	// The deadline of the start, not none: on SQLite this call migrates, and
	// a second program on the same file holds it for the measured 80,6 s of a
	// full retry chain with no word to the admin meanwhile. Point 32 decided
	// the shape -- a service that hangs at the start is worse than one that
	// does not start, and the restart belongs in the service file.
	octx, ocancel := dbwait.Start(ctx)
	db, err := dbopen.Open(octx, cfg.DB, dbopen.OwnerOf(cfg.Node))
	ocancel()
	if err != nil {
		return nil, errors.New("die Datenbank ist nicht erreichbar")
	}
	s := state.New(cfg, db, box, pin)
	lg := &audit.Log{DB: db, Now: s.Now}
	lh := login.New(s)
	tunnels := controllers.NewRegistry()
	sessions := browser.NewRegistry()
	ctl := &controllers.Server{S: s, Tunnels: tunnels}
	rl := &relay.Relay{S: s, Tunnels: tunnels, Log: lg}
	st := appstore.New(s)
	jn := join.New(s)
	// The one implementation of appstore.Sender in operation. Task 22 left
	// Stores.Send nil and every step of it answered E_INTERNAL until here.
	st.Send = &sender{Tunnels: tunnels, Relay: rl}
	st.Spool = SpoolDir
	// ONE adapter for both packages, which is what task 23 asked for: Send is
	// the very same value appstore got, not a second one of its own. It can
	// be, because updates.Updates.Send is of type appstore.Sender -- stage and
	// install are core actions on a controller exactly as activate and
	// uninstall are. Updates uses only the half without a user (task, client
	// and proof nil, core.WithoutUser); appstore uses both halves.
	//
	// Spool is the same directory as well, and that is right rather than
	// convenient: both write a file of up to four gigabytes while they read
	// it, both remove it in the same call, and sweepSpool clears whatever a
	// killed run left behind for either of them. config.DataDir and never the
	// operating system's directory, which is a RAM disk on many systems.
	up := &updates.Updates{S: s, Stores: st, Tunnels: updates.FromRegistry(tunnels),
		Send: st.Send, Log: lg, Spool: SpoolDir}
	d := coreapp.New(coreapp.Deps{S: s, Log: lg, Login: lh, Tunnels: tunnels,
		Controllers: ctl, Sessions: sessions, Join: jn, Relay: coreRelay{rl},
		Store: st, Updates: up})
	bs := &browser.Server{S: s, Login: lh, Open: d, Sessions: sessions}
	sh := &setup.Handler{S: s, CodeFile: SetupCodeFile}
	if _, err := sh.EnsureCode(); err != nil {
		return nil, errors.New("der Einmal-Code für die Ersteinrichtung lässt sich nicht anlegen")
	}
	controllers.Version = version
	srv := &Server{Cfg: cfg, State: s, Cert: cert, login: lh, setup: sh, st: st, up: up,
		core: d, relay: rl, ctl: ctl, tunnels: tunnels, sessions: sessions, spool: SpoolDir}
	// A controller that connects gets the newest trust list and the newest
	// licence of every paid app it runs, right away.
	ctl.OnHello = srv.hello
	// What a controller reports about an update it was told to install.
	//
	// It needs a wrapper after all. Task 23 noted this seam as "fits without
	// one", and measured it does not: the hook is
	// func(ctx, *controllers.Tunnel, store.UpdateReport) while Report takes
	// the node as a string, and Go converts neither. The node comes off the
	// tunnel, which is the one place it can come from -- the report itself
	// carries no node, and one it carried would be the controller's word
	// about which controller it is.
	//
	// Report is called IN LINE in the tunnel's single reader
	// (controllers.Server.control), so it has to be short: it writes one map
	// entry and wakes whoever waits, and writes no log line on purpose (task
	// 23 -- a controller reports several times per update, and a line per step
	// would bury the one entry that counts, the round that was broken off).
	ctl.OnReport = func(ctx context.Context, t *controllers.Tunnel, r store.UpdateReport) {
		up.Report(ctx, t.Node, r)
	}
	// What a controller says about one of its services. Without this line the
	// API read every wire.CtlNotify and dropped it at the nil test in
	// controllers.Server.control -- "my service has died" reached no shell and
	// left nothing behind either.
	ctl.OnNotice = srv.notice
	srv.handler = srv.routes(httpapi.Routes{
		Setup: sh.Serve, Login: lh.Start, Finish: lh.Finish, Logout: lh.Logout,
		Trust: lh.Trust, Socket: bs.Socket,
	}.Mux(), ctl, jn)
	if err := srv.sweepSpool(); err != nil {
		return nil, errors.New("das Zwischenlager für Pakete lässt sich nicht anlegen oder räumen")
	}
	return srv, nil
}

// routes puts the tunnel paths in front of the shell's router. It is a switch
// and not a second http.ServeMux on purpose: a ServeMux answers a path it has
// to clean with a redirect of its own making, past everything this program
// promises, and httpapi.Routes.Mux guards exactly against that -- a mux
// wrapped around it would put that hole back in front of the guard.
func (s *Server) routes(shell http.Handler, ctl *controllers.Server, jn *join.Join) http.Handler {
	type route struct {
		method string
		h      http.HandlerFunc
	}
	paths := map[string]route{
		tunnel.PathTunnel: {http.MethodGet, ctl.Tunnel},
		tunnel.PathPair:   {http.MethodPost, ctl.Pair},
		tunnel.PathJoin:   {http.MethodPost, jn.Accept},
		join.PathKeys:     {http.MethodGet, jn.Keys},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The escaped path, matched EXACTLY and never cleaned. A path that
		// would have to be cleaned matches none of these four and goes to the
		// shell's router, whose own guard answers it with E_NOTFOUND -- so
		// //tunnel/pair is not the pair path, and that is measured.
		//
		// A second clean guard stood here and is gone again: a mutant that
		// took it away killed no case, because every path either matches one
		// of these four exactly or reaches the guard behind. A bolt that
		// holds nothing is worse than none, it only reads as if it did.
		rt, ok := paths[r.URL.EscapedPath()]
		if !ok {
			shell.ServeHTTP(w, r)
			return
		}
		if r.Method != rt.method {
			httpapi.Fail(w, wire.CodeProtocol)
			return
		}
		rt.h(w, r)
	})
}

// Handler returns the router; tests use it without a listener.
func (s *Server) Handler() http.Handler { return s.handler }

// sweepSpool makes the spool directory and removes what an earlier run left in
// it. A run that is killed between writing a package and removing it leaves up
// to four gigabytes behind: appstore removes it with defer, and defer does not
// run on SIGKILL or a power cut. Nobody else writes here, so everything found
// is a leftover of a run that is over.
func (s *Server) sweepSpool() error {
	if err := os.MkdirAll(s.spool, 0o700); err != nil {
		return err
	}
	es, err := os.ReadDir(s.spool)
	if err != nil {
		return err
	}
	for _, e := range es {
		if err := os.RemoveAll(filepath.Join(s.spool, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Run listens and keeps the background rounds going until ctx ends.
func (s *Server) Run(ctx context.Context) error {
	ln, err := tls.Listen("tcp", s.Cfg.Listen, s.TLSConfig())
	if err != nil {
		return err
	}
	return s.serve(ctx, ln)
}

// TLSConfig is the one hop between Apache and this API.
//
// What this listener holds, measured on 12.10.2026 against a listener with
// these fields AND the CipherSuites line that used to stand below -- the point
// of the measurement is that the line changed nothing (openssl s_client 3.5.7
// and a Go client):
//
//   - TLS 1.3 and nothing else. A client that offers TLS 1.2 gets
//     "tlsv1 alert protocol version" and no connection.
//   - X25519 and P-256 as the key share groups. A client that offers only
//     secp384r1 gets "handshake failure". Both halves are a case of their own
//     now (TestTLSConfigTakesTLS13Only), measured in Go, because
//     CurvePreferences IS read under TLS 1.3 while CipherSuites is not.
//
// What it does NOT hold, and what the line that used to stand here claimed:
// the design asks for AES-256-GCM alone on this hop (Entwurf 4.5), and a
// CipherSuites field cannot ask for it. Go ignores CipherSuites under TLS 1.3
// ("TLS 1.3 cipher suites are not configurable", crypto/tls), and this
// listener therefore takes all three of them: measured, a client offering only
// TLS_CHACHA20_POLY1305_SHA256 connects, one offering only
// TLS_AES_128_GCM_SHA256 connects, and a Go client with the default list ends
// up on TLS_AES_128_GCM_SHA256 although the field named AES-256 alone. So the
// field is gone rather than left standing: a setting that reads like a bound
// and holds nothing is worse than no setting, because the next reader stops
// looking.
//
// Where the bound really is: on the CLIENT, which is Apache and only Apache in
// front of this listener -- "Apache bietet der API nur diese Verschlüsselung
// an" (Entwurf 4.5), set up by ngc-web as SSLProxyProtocol TLSv1.3 and
// SSLProxyCipherSuite TLSv1.3 TLS_AES_256_GCM_SHA384 (shared/ulh.md). A
// TLS 1.3 server cannot pick a suite its client did not offer, so the hop runs
// on AES-256-GCM as long as that configuration is the one in front. What is
// open, and what no code here can close, is anything ELSE that reaches this
// port: it is not held to AES-256-GCM (decision protocol, 12.10.2026). All
// three suites are strong, so this is a promise that was not kept, not a hole.
//
// TestTLSConfigNamesNoCipherSuite and TestTLSConfigTakesTLS13Only keep both
// halves of this paragraph from drifting back.
func (s *Server) TLSConfig() *tls.Config {
	return &tls.Config{
		Certificates:     []tls.Certificate{s.Cert},
		MinVersion:       tls.VersionTLS13,
		MaxVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}
}

// ShutdownWait is how long a stopping server waits for open requests.
const ShutdownWait = 5 * time.Second

// serve runs the listener and the rounds and returns when both are done.
//
// No limit is set on how many connections may be open at once, and that is
// said rather than left to be found: Apache stands in front of this listener
// and is the only thing that reaches it, so a bound here would cap Apache and
// not a caller. What is bounded is one background round at a time
// (Server.round) and the ShutdownWait a stop waits for open requests.
func (s *Server) serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	var rounds sync.WaitGroup
	rounds.Add(2)
	// The catch around the LOOP, next to the one around a single round in
	// Server.once: once keeps the beat turning over a panic in the work of a
	// round, this one keeps a panic in the beat itself -- the tickers, the
	// select -- from taking the server down with every session of every user.
	// After it the housekeeping is over for the life of this process, which is
	// exactly why guard's line is ERROR and counted: an installation whose
	// sessions stop expiring must be findable (V1.2 point 50).
	go func() {
		defer rounds.Done()
		defer func() { _ = guard.Hit(recover(), "server.rounds", 0) }()
		s.rounds(ctx)
	}()
	// In the same WaitGroup, so a stop waits for a round that is running
	// instead of leaving it behind: Rollout checks the context between nodes
	// and awaitInstalled selects on it, so it comes back of its own accord.
	go func() {
		defer rounds.Done()
		defer func() { _ = guard.Hit(recover(), "server.rollouts", 0) }()
		s.rollouts(ctx, s.up.Rollout)
	}()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		// A panic in the shutdown is the one that would be hardest to see: the
		// process is going down anyway, so a crash here looks like the stop it
		// was asked for. close(stopped) above still runs, so serve comes back
		// and the exit code stays the one the caller decides.
		defer func() { _ = guard.Hit(recover(), "server.shutdown", 0) }()
		<-ctx.Done()
		// WithoutCancel: ctx is already done, and a shutdown that inherited it
		// would cut every open connection at once instead of letting it end.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownWait)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("ngc-api listening", "listen", s.Cfg.Listen, "node", s.Cfg.Node)
	err := srv.Serve(ln)
	<-stopped
	rounds.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Minute and Hour are the two beats of the background rounds; a test makes
// them milliseconds instead of waiting.
var (
	Minute = time.Minute
	Hour   = time.Hour
)

// rounds keeps housekeeping and catalogs going until ctx ends.
func (s *Server) rounds(ctx context.Context) {
	minute := time.NewTicker(Minute)
	hour := time.NewTicker(Hour)
	defer minute.Stop()
	defer hour.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-minute.C:
			s.once(func() { s.minute(ctx) })
		case <-hour.C:
			s.once(func() { s.hour(ctx) })
		}
	}
}

// once runs fn unless a round is already running; see Server.round.
func (s *Server) once(fn func()) {
	if !s.round.TryLock() {
		slog.Warn("a background round is still running, this tick is dropped")
		return
	}
	defer s.round.Unlock()
	// The catch sits around ONE round, and that is the decision this line
	// carries: a background round may not quietly stop. Caught around the
	// loop alone, one panic in one minute round would end the housekeeping
	// for the life of the process -- expired sessions would stay logged in
	// and the trust list would stay as it was, and the API would keep
	// answering as if both had been done, which is the false answer of V1.2
	// point 50. Here the broken round ends loudly and the NEXT tick runs the
	// round again. Channel 0: this is nobody's channel.
	defer func() { _ = guard.Hit(recover(), "server.round", 0) }()
	fn()
}

// minute is the housekeeping round: expired sessions out, trust list afresh,
// and the count of caught programming errors said out loud if it has grown.
//
// guard.Report is LAST on purpose. A panic in either step above is caught by
// Server.once around this round, so the count it would report is one this
// round can no longer reach -- it reaches the log at the next tick instead,
// one minute later, which is the price of the catch sitting around the round
// and not around each step. Put first, the call would report the state of the
// minute before and read just as late.
func (s *Server) minute(ctx context.Context) {
	if err := s.login.Purge(ctx); err != nil {
		slog.Warn("housekeeping failed", "error", err)
	}
	if _, err := s.State.Refresh(ctx); err != nil {
		slog.Warn("trust list could not be read", "error", err)
	}
	guard.Report()
}

// hour is the catalog round.
func (s *Server) hour(ctx context.Context) {
	if err := s.st.Refresh(ctx); err != nil {
		slog.Warn("catalogs could not be fetched", "error", err)
	}
}

// rollouts is the beat of the update rounds, on a goroutine of its own beside
// the two housekeeping rounds.
//
// DECISION on "who calls Rollout how often" (the question task 23 left open):
// once an hour, from here and from nowhere else, and never from a channel of
// the shell. Hourly because a round can only act on what the catalog round
// fetched, and because the controllers decide themselves whether they install
// -- a faster beat would ask the same nodes the same question sooner without
// any answer changing. The first beat is an hour after the start and not at
// the start: an installation that comes up must not begin replacing its own
// parts before anybody has looked at it.
//
// DECISION on its OWN goroutine, and not inside Server.rounds: a round waits
// updates.InstallWait (15 minutes) per node for that node to report itself up,
// so a rollout over four nodes may be busy for an hour. Inside rounds that
// hour would drop every minute tick with it -- expired sessions would stay
// logged in and the trust list would not be read afresh, which are the two
// things housekeeping is for. The reason task 24 gave for one round at a time
// was that two of them write the same catalog rows; a rollout writes none, so
// it only has to be alone with itself.
//
// And it is alone with itself WITHOUT a lock, which is the one thing worth
// saying here. This loop is the only caller, and it calls synchronously, so a
// tick cannot arrive in a round: time.Ticker keeps one place and drops what
// does not fit, so a round that outlives ten ticks is followed by ONE and not
// by ten. A TryLock stood here and is gone again -- a mutant that replaced it
// with a plain Lock killed no case, and it could not: nothing could ever find
// the lock taken. A bolt that holds nothing is worse than none.
// TestTheBeatOfTheRolloutTurns measures the property instead, against the
// mutant that does start a goroutine per tick.
//
// drive is handed in rather than read off the server, so a case can measure
// the beat and the handling of ErrNotDriving without a release, a store and a
// controller behind them. serve passes s.up.Rollout and nothing else ever
// does.
func (s *Server) rollouts(ctx context.Context, drive func(context.Context) error) {
	tick := time.NewTicker(Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.rollout(ctx, drive)
		}
	}
}

// rollout drives one update round.
//
// updates.ErrNotDriving is NOT a failure and is not logged as one: it says
// another API holds the rollout lock, which is the normal case of an
// installation with two API servers, and its own comment asks for exactly
// this. A context that ended is not one either -- that is a server stopping.
func (s *Server) rollout(ctx context.Context, drive func(context.Context) error) {
	// Per ROUND and not around the loop, for the reason Server.once gives: a
	// rollout that broke off must not take the beat with it, or an
	// installation would stop updating itself and say nothing. The next hour
	// asks the same nodes again, which is what a rollout is built to survive.
	defer func() { _ = guard.Hit(recover(), "server.rollout", 0) }()
	err := drive(ctx)
	switch {
	case err == nil, errors.Is(err, updates.ErrNotDriving), errors.Is(err, ctx.Err()):
	default:
		slog.Warn("the update round stopped", "error", err)
	}
}

// hello is what a controller gets the moment its tunnel is up: the versions of
// the trust list it is missing and the licence of every paid app it runs.
// Nothing in here may fail the hello -- a controller that cannot be reached
// gets both again at its next one.
func (s *Server) hello(ctx context.Context, t *controllers.Tunnel) {
	s.pushTrust(ctx, t)
	if err := s.st.PushLicenses(ctx, s.pushLicense); err != nil {
		slog.Warn("licences could not be pushed", "error", err)
	}
}

// notice carries a controller's notice to every open shell, and writes a line
// where none could take it.
//
// The line is the half this seam was missing: a notice that reaches no shell
// is exactly the case an administrator has to hear about ("my service has
// died" while nobody is logged in), and until now nothing anywhere said that
// one had come. It goes to the operator log and not to audit.Log on purpose:
// a controller decides how often it sends, and a row per notice would let it
// write the log of an installation full -- the same reason OnReport keeps no
// row (above).
//
// The text of the notice is NOT in the line. It is text from another machine,
// and what this line has to say is that one came and found nobody; the text
// itself is the shell's business, and a controller that put a secret in it must
// not get it written down here.
//
// NOT decided here, and named rather than left to be found: the notice goes to
// EVERY open shell, including the shells of users who have no right to the app
// it names. The shell is what sorts notices today (web: appframe.js), and a
// filter by right would need the rights of every session inside the tunnel's
// reader. Whether a notice of one app may be seen by a user who may not use it
// is a question of the design, not of this seam -- it belongs on the list of
// open points.
//
// It runs IN LINE in the tunnel's single reader (controllers.Server.control),
// like OnReport, so it has to be short: one pass over the open sessions, each
// a write on a link that is already there, and nothing that waits for a
// network of its own.
func (s *Server) notice(ctx context.Context, t *controllers.Tunnel, n core.Notice) {
	if s.sessions.NotifyAll(ctx, n) == 0 {
		slog.Warn("notice of a controller reached no shell",
			"node", t.Node, "level", n.Level, "app", n.App)
	}
}

// pushTrust sends one controller the versions of the trust list it is missing.
func (s *Server) pushTrust(ctx context.Context, t *controllers.Tunnel) {
	v, err := s.State.Trust(ctx)
	if err != nil {
		return
	}
	chain, err := s.State.Chain(ctx, v, t.Hello().TrustSeq+1)
	if err != nil || len(chain.Versions) == 0 {
		return
	}
	body, err := json.Marshal(chain)
	if err != nil {
		return
	}
	_, _ = s.relay.CallController(ctx, t, core.AppUsers, core.ServiceController,
		core.ActTrust, nil, body)
}

// pushLicense is the sender appstore.PushLicenses takes: one envelope to the
// controller that runs the app. It is the first caller that function has ever
// had -- task 21 built it, task 22 left it without one on purpose.
func (s *Server) pushLicense(node, app string, env sign.Envelope) error {
	t, ok := s.tunnels.Get(node)
	if !ok || t == nil {
		return errNoTunnel
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	params, err := json.Marshal(core.AppParams{App: app})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), relay.CallTimeout)
	defer cancel()
	// ngc.store/controller/license is one of core.WithoutUser: it carries no
	// user, so CallController and not CallAs.
	if _, code := s.relay.CallController(ctx, t, core.AppStore, core.ServiceController,
		core.ActLicense, params, body); code != "" {
		return fmt.Errorf("server: the controller refused the licence: %s", code)
	}
	return nil
}
