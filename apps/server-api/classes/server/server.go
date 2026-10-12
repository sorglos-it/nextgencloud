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
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/join"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/relay"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/setup"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/sign"
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
//   - Nothing waits here BETWEEN two calls. The package an admin uploaded does
//     not lie here until he activates it: ngc.store/controller/activate is a
//     core action of flow transfer, so the shell sends the file again on that
//     channel. Nothing on any disk has to survive in between, which is why
//     neither the second API server nor a crash is a question for it.
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
	core    *coreapp.Dispatcher
	relay   *relay.Relay
	tunnels *controllers.Registry

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
	db, err := dbopen.Open(ctx, cfg.DB)
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
	d := coreapp.New(coreapp.Deps{S: s, Log: lg, Login: lh, Tunnels: tunnels,
		Controllers: ctl, Sessions: sessions, Join: jn, Relay: coreRelay{rl}})
	bs := &browser.Server{S: s, Login: lh, Open: d, Sessions: sessions}
	sh := &setup.Handler{S: s, CodeFile: SetupCodeFile}
	if _, err := sh.EnsureCode(); err != nil {
		return nil, errors.New("der Einmal-Code für die Ersteinrichtung lässt sich nicht anlegen")
	}
	controllers.Version = version
	srv := &Server{Cfg: cfg, State: s, Cert: cert, login: lh, setup: sh, st: st,
		core: d, relay: rl, tunnels: tunnels, spool: SpoolDir}
	// A controller that connects gets the newest trust list and the newest
	// licence of every paid app it runs, right away.
	ctl.OnHello = srv.hello
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

// TLSConfig is the one hop between Apache and this API. Apache offers nothing
// else, and the design asks for AES-256-GCM alone on it (Entwurf 4.5).
func (s *Server) TLSConfig() *tls.Config {
	return &tls.Config{
		Certificates:     []tls.Certificate{s.Cert},
		MinVersion:       tls.VersionTLS13,
		MaxVersion:       tls.VersionTLS13,
		CipherSuites:     []uint16{tls.TLS_AES_256_GCM_SHA384},
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
	rounds.Add(1)
	go func() { defer rounds.Done(); s.rounds(ctx) }()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
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
	fn()
}

// minute is the housekeeping round: expired sessions out, trust list afresh.
func (s *Server) minute(ctx context.Context) {
	if err := s.login.Purge(ctx); err != nil {
		slog.Warn("housekeeping failed", "error", err)
	}
	if _, err := s.State.Refresh(ctx); err != nil {
		slog.Warn("trust list could not be read", "error", err)
	}
}

// hour is the catalog round.
func (s *Server) hour(ctx context.Context) {
	if err := s.st.Refresh(ctx); err != nil {
		slog.Warn("catalogs could not be fetched", "error", err)
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
