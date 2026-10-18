// The controller in continuous operation: what it does from the moment it
// starts until SIGTERM, and what it keeps doing while no API is in reach.
//
// This is the file in which the parts the other tasks built get a caller.
// Before it, update.Resume had none, node.Restore had none, serve.RunService
// was a default nothing ever took, and nothing sent the status of this server
// on a tunnel at all. What runs here is therefore the first time those seams
// bear weight, and the three states of continuous operation are what they are
// measured in: a restart, a crash in the middle of a write, and a context that
// was cancelled.
//
// The order at the start is not a taste. Resume comes FIRST, before anything
// else touches the updater or a container: a controller that restarted itself
// into a new version has a row in store.UpdateInstalling, and whether that
// version counts as installed or is taken back decides which program the apps
// are brought up by. Restore comes second, before any tunnel: a file server
// whose files need an API to be served is not a file server (restore.go). The
// tunnels come last, because nothing of this server waits for them.
//
// What this file does NOT do, named rather than hidden:
//
//   - It does not run the migrations of an UPDATE. A controller with the role
//     db starts those as a program ("ngc-api migrate",
//     docs/vertraege-v1.2.md point 28), and that subcommand is task 24 of the
//     API plan and does not exist yet (apps/server-api/main.go knows -version
//     and -config). There is no migrate.go here, and that is the decision: a
//     scaffold would look like a promise.
//
//     What openState DOES do is bring the schema of this controller's own
//     SQLite up to date, because sqlite.Open does that at every open. The two
//     are not the same thing and the difference is the whole of point 28: one
//     file opening its own schema has no ledger to write and no second server
//     to wait for, while the migrations of an update do.
package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/flynn/noise"
	"golang.org/x/crypto/curve25519"

	// Named catch here and guard everywhere else: this package already has a
	// function guard (ask.go), which gives the terminal its echo back when a
	// signal ends the pairing question. Renaming that one for an import would
	// be the tail wagging the dog.
	catch "github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/apache"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/chain"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/license"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/serve"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// The intervals of continuous operation. Every one of them is a DECISION of
// this file, written here once so that no second number can drift from it.
const (
	// StatusEvery is how often this controller tells its APIs what it is
	// doing (the plan: "Statusmeldungen alle 60 s"). A status is cheap - one
	// control message built out of rows this server has anyway - and it is
	// the only thing that makes a server visible in NextGenCloud between two
	// actions.
	StatusEvery = 60 * time.Second
	// PurgeEvery is how often the one-time task IDs that have expired are
	// dropped (the plan: "state.Purge stuendlich"). Nothing depends on it
	// being prompt: a used ID is refused by UseTask whether it was purged or
	// not, so this only keeps the table from growing.
	PurgeEvery = time.Hour
	// WebEvery is how often a server with the role web checks the
	// certificates of the APIs against what Apache was told
	// (apache.Writer.Compare, "once a day" in its own doc). The other half of
	// that rule - after every tunnel was built - is no clock and hangs on
	// link.Config.TunnelUp.
	WebEvery = 24 * time.Hour
	// Grace is how long the running channels have once the context ended (the
	// plan: "laufende Kanaele bekommen 10 Sekunden, dann wird geschlossen").
	// It bounds the WAIT and nothing else: what the channels do inside it is
	// their own, and after it this program returns whether or not they came
	// back. The containers keep running either way - the server goes on
	// working without an API and without this controller.
	Grace = 10 * time.Second
)

// Owner is what this controller writes into the migration lock of its own
// SQLite, and it is a DECISION of this file.
//
// It has to name THIS controller to whoever reads the lock, and it has to be
// there: migrate.Apply refuses an empty owner. config.Controller.Node is
// empty until an admin has put this server in a trust list, so the node alone
// would be no name at all on a freshly paired server - the name is therefore
// the one of the configuration, which config.LoadController never lets
// through empty (wire.ValidName), and the node is added when there is one.
//
// It is not "ngc": a lock that says "ngc" says nothing, and the one thing an
// admin wants from that row is WHICH program is migrating.
const Owner = "ngc-controller"

// owner is that name for this configuration.
func owner(cfg *config.Controller) string {
	if cfg.Node != "" {
		return Owner + "/" + cfg.Name + "/" + cfg.Node
	}
	return Owner + "/" + cfg.Name
}

// msgNoState is what an admin reads for it. German, like every other line of
// this program he reads on the server, and it names the next step.
const msgNoState = "Die lokale Datenbank dieses Servers lässt sich nicht öffnen. Bitte den Server mit ulh neu einrichten."

// msgStopped is the one line a clean end writes. An admin who stopped the
// service reads it and knows the containers were left alone.
const msgStopped = "Der Controller wurde beendet. Die Dienste dieses Servers laufen weiter."

// Tunnels is the registry of the tunnels that stand right now, one per API.
// link.Keep holds its tunnel and gives nothing back, so this is where the rest
// of this program reaches one (link.Config.TunnelUp): the status of this
// server goes out over every tunnel in here, and nothing goes out over one
// that is over.
//
// It is used from the goroutine of every Keep and from the ticker of Run at
// once, so every read and every write of the slice is under the mutex.
type Tunnels struct {
	mu sync.Mutex
	// t is a slice and not a map by API address: identity is the pointer, and
	// a Keep that announced a new tunnel before it took the old one back
	// would lose one in a map keyed by address.
	t []*link.Tunnel
}

// Up takes a tunnel into the registry.
func (r *Tunnels) Up(t *link.Tunnel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = append(r.t, t)
}

// Down takes it out again.
func (r *Tunnels) Down(t *link.Tunnel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.Index(r.t, t); i >= 0 {
		r.t = slices.Delete(r.t, i, i+1)
	}
}

// Len is how many tunnels stand.
func (r *Tunnels) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.t)
}

// Control puts one control message on every tunnel that stands, and fits
// ReportConfig.Send.
//
// It goes to every one of them and not to the first that takes it: each API
// shows this server to whoever is signed in there, and a status that reached
// one of three APIs would leave the other two showing a server that stopped
// reporting. An error says that NO tunnel took it - either none stands
// (errNoTunnel, which the Reporter writes into the log of this server) or
// every one of them refused it, and then the answer of the last is the one
// that comes back.
//
// The sends happen while the mutex is held, so a tunnel cannot be taken out
// from under one. (*link.Tunnel).Control writes one frame and waits for no
// answer, so the hold is a write on a socket and no more.
func (r *Tunnels) Control(typ string, body any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.t) == 0 {
		return errNoTunnel
	}
	var (
		sent bool
		last error
	)
	for _, t := range r.t {
		if err := t.Control(typ, body); err != nil {
			last = err
			continue
		}
		sent = true
	}
	if sent {
		return nil
	}
	return last
}

// RunConfig is what Run needs. State, Chain and Report are required; every
// other field left empty is one thing this server does not do, and that is
// said at the field.
type RunConfig struct {
	// State is this controller's own store. Purge works on it hourly.
	State *state.Store
	// Chain holds the trust list.
	Chain *chain.Keeper
	// Report is what tells the APIs about this server.
	Report *Reporter
	// APIs is one tunnel configuration per API of controller.json. Run starts
	// a Keep for each and waits for all of them at the end.
	APIs []link.Config
	// Keep dials one API again and again until the context ends. nil:
	// link.Keep. A case puts its own in, so that no socket is needed.
	Keep func(ctx context.Context, cfg link.Config)
	// Resume decides an install that this program's own restart interrupted
	// ((*update.Manager).Resume). It runs FIRST, before anything else touches
	// the updater or a container. nil: this server has no updater.
	//
	// Its failure is NOT fatal, and that is a decision: what it could not
	// decide stays marked store.UpdateInstalling and is decided at the next
	// start, while a controller that refused to come up over it would take
	// the apps of this server down for a row in its own database.
	Resume func(ctx context.Context) error
	// Restore brings the apps that were running back up (Restore with a
	// RestoreConfig). nil: this server brings nothing up.
	//
	// Its failure IS fatal, and that is the other half of the same decision:
	// Restore answers with an error only for a list of apps it could not read
	// or a context that ended (restore.go), and a controller that cannot read
	// its own apps knows nothing about what it did not start.
	Restore func(ctx context.Context) error
	// Compare checks the certificates of the APIs against what Apache was
	// told (apache.Writer.Compare). It runs once a day and after every tunnel
	// that came off. nil: this server has no role web and checks nothing.
	Compare func(ctx context.Context) error
	// The intervals. 0 is the constant of this file.
	Status, Purge, Web, Grace time.Duration
	// Now is the clock of Purge, in seconds. nil: time.Now().Unix().
	Now func() int64
	Log *slog.Logger // nil: slog.Default()
}

// Run is the controller in continuous operation. It comes back when ctx ends:
// Resume, Restore, one Keep per API, and then the clock until SIGTERM.
//
// What is left running when it comes back is the point of the whole task: the
// containers. Nothing here stops one. A controller that is being replaced by
// an update, or restarted by an admin, must not take the services of the
// server with it - and an update of the controller is exactly that restart
// (update.installPart).
func Run(ctx context.Context, c RunConfig) error {
	if c.State == nil || c.Chain == nil || c.Report == nil {
		return ErrNotWired
	}
	log := c.logger()
	// First, before the updater or a container is touched by anything else.
	if c.Resume != nil {
		if err := c.Resume(ctx); err != nil {
			log.Warn("an interrupted update of this controller could not be decided, it stays marked", "err", err)
		}
	}
	if c.Restore != nil {
		if err := c.Restore(ctx); err != nil {
			return err
		}
	}
	// The signal that a tunnel came off, coalescing: one place in the buffer
	// is enough, because what it releases does not depend on how many tunnels
	// came off, only that one did. It is never closed - the writers are the
	// goroutines of Keep, and closing it would be a send on a closed channel
	// at the end of the program.
	built := make(chan struct{}, 1)
	var wg sync.WaitGroup
	keep := c.Keep
	if keep == nil {
		keep = link.Keep
	}
	for _, cfg := range c.APIs {
		// TunnelUp and TunnelDown are called in the goroutine of Keep, so
		// neither may do anything slow: until TunnelUp comes back the tunnel
		// is not being read, and a tunnel that is not read loses its pings
		// (link, frameHook). Both do one slice operation; the certificate
		// check the tunnel releases runs in the goroutine below.
		up := cfg.TunnelUp
		cfg.TunnelUp = func(t *link.Tunnel) {
			if up != nil {
				up(t)
			}
			select {
			case built <- struct{}{}:
			default:
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// One goroutine per API, and a panic in one of them took the
			// whole controller with every tunnel and every channel of every
			// user (V1.2 point 38). Caught, this one API has no tunnel for
			// the life of the process -- the catch is around the keeper and
			// not around a round, because Keep IS the loop and there is no
			// outer beat to come back to. That price is named rather than
			// hidden: the line is ERROR and counted, so an installation whose
			// controller no longer reaches one of its APIs can be found.
			// wg.Done above still runs, so a stop does not wait out the
			// grace period for it.
			defer func() { _ = catch.HitTo(log, recover(), "node.keep", 0) }()
			keep(ctx, cfg)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		// The same, and the price is the certificates of the APIs no longer
		// being compared while this process lives. The daily beat is inside
		// watchWeb, so a catch here ends it; what it must not do is end the
		// process.
		defer func() { _ = catch.HitTo(log, recover(), "node.watchWeb", 0) }()
		c.watchWeb(ctx, built)
	}()
	c.clock(ctx)
	// The context is over. The channels get their grace and then this program
	// returns, whether or not they came back: a controller that hangs on a
	// channel somebody holds open is one systemd kills, and that is worse
	// than a goroutine left behind in a process that is ending.
	done := make(chan struct{})
	go func() {
		// close is deferred, not the last statement, so a panic in the wait
		// -- a WaitGroup counter that went negative is one -- neither ends
		// the process nor makes this stop sit out the whole grace period for
		// a goroutine that will never report.
		defer close(done)
		defer func() { _ = catch.HitTo(log, recover(), "node.grace", 0) }()
		wg.Wait()
	}()
	timer := time.NewTimer(c.grace())
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Warn("the links did not come back within the grace period, ending anyway",
			"seconds", int(c.grace()/time.Second))
	}
	return nil
}

// clock is the two tickers of continuous operation. It comes back with the
// context.
//
// Nothing here is done twice when a tick is missed: a status is built fresh
// every time and says what is the case now, and Purge takes whatever has
// expired by now. So a controller that was held up sends one status and purges
// once, not one per tick it slept through.
func (c RunConfig) clock(ctx context.Context) {
	status := time.NewTicker(c.every(c.Status, StatusEvery))
	defer status.Stop()
	purge := time.NewTicker(c.every(c.Purge, PurgeEvery))
	defer purge.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-status.C:
			c.Report.SendStatus(ctx)
			// The count of caught programming errors, said out loud when it
			// has grown and silent when it has not: without this call
			// guard.Panics() would be a number no running controller ever
			// reads, which is a diagnostic with no way out (V1.2 point 22).
			// On the status tick because that is this program's own beat --
			// the only round of the controller that keeps turning whatever
			// the tunnels do.
			catch.Report()
		case <-purge.C:
			if err := c.State.Purge(ctx, c.now()); err != nil && !errors.Is(err, dbapi.ErrNotFound) {
				c.logger().Warn("the expired one-time task IDs could not be dropped", "err", err)
			}
		}
	}
}

// watchWeb runs the certificate check of a server with the role web: once a
// day, and once for every tunnel that came off (built).
//
// It is a goroutine of its own and no case of clock, because the check reaches
// out over the network: a slow API would hold the status of this server behind
// it.
func (c RunConfig) watchWeb(ctx context.Context, built <-chan struct{}) {
	if c.Compare == nil {
		return
	}
	day := time.NewTicker(c.every(c.Web, WebEvery))
	defer day.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-day.C:
		case <-built:
		}
		if err := c.Compare(ctx); err != nil && ctx.Err() == nil {
			c.logger().Warn("the certificates of the APIs could not be checked", "err", err)
		}
	}
}

func (c RunConfig) every(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func (c RunConfig) grace() time.Duration {
	if c.Grace > 0 {
		return c.Grace
	}
	return Grace
}

func (c RunConfig) now() int64 {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().Unix()
}

func (c RunConfig) logger() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

// opener opens this controller's own store. It is a type and an argument so
// that a case can drive everything below the store without one on disk; on a
// server it is openState and nothing else.
type opener func(cfg *config.Controller) (*state.Store, error)

// openState opens the encrypted SQLite of this controller. It is the one call
// of the whole program that reaches an implementation of the store, and
// state.Open is the one place that names one.
//
// config.Controller keeps the file and its key as two plain fields; the store
// takes a config.DB, so this is where the two are put together. The driver is
// named here and not read from anywhere: a controller's own database is this
// file format and no other, and a field for it would be a field that could
// say "mariadb" on a server that has none.
//
// now is the clock of the migration lock. It is read here, at the start, and
// not handed in: the lock belongs to this one start of this one program.
func openState(cfg *config.Controller) (*state.Store, error) {
	return state.Open(config.DB{
		Driver: config.DriverSQLite,
		Path:   cfg.DB,
		Key:    cfg.DBKey,
	}, owner(cfg), time.Now().Unix())
}

// run is the controller itself, the command without a command: it reads the
// configuration, opens the store, builds every half out of it and hands them
// to Run.
func run(version, cfgPath string, stdout, stderr io.Writer) int {
	return runWith(version, cfgPath, openState, stdout, stderr)
}

// runWith is run with the store opener as an argument, so that everything
// below the store is measured over one that exists.
func runWith(version, cfgPath string, open opener, stdout, stderr io.Writer) int {
	ctx, stop := stopOnSignal()
	defer stop()
	cfg, err := config.LoadController(cfgPath)
	if err != nil {
		fmt.Fprintln(stderr, msgNoConfig)
		return 1
	}
	key, err := staticKey(cfg)
	if err != nil {
		fmt.Fprintln(stderr, msgNoKey)
		return 1
	}
	st, err := open(cfg)
	if err != nil {
		// The cause is not named to the admin: on this way it would name the
		// file of the database, and the line he needs is the next step and
		// not the reason. It goes into the log of this server instead.
		slog.Error("the local store of this controller did not open", "err", err)
		fmt.Fprintln(stderr, msgNoState)
		return 1
	}
	defer st.Close()
	keeper := chain.New(st, cfg.Anchor)
	// Load before anything reads the list: a keeper that never read its state
	// answers every channel with chain.ErrStore (chain.Keeper.Apply), and
	// Restore judges the licences of the apps against the list it holds.
	if err := keeper.Load(ctx); err != nil {
		slog.Error("the trust list of this controller could not be read", "err", err)
		fmt.Fprintln(stderr, msgNoState)
		return 1
	}
	if err := Run(ctx, wireUp(version, cfg, key, st, keeper)); err != nil {
		slog.Error("this controller stopped", "err", err)
		return 1
	}
	fmt.Fprintln(stdout, msgStopped)
	return 0
}

// staticKey rebuilds this controller's Noise key pair out of the private key
// of controller.json. Only the private half is stored (config.Controller
// NoiseKey), and the public half is derived from it rather than kept beside
// it: two values that must agree and are written separately are two values
// that can disagree.
func staticKey(cfg *config.Controller) (noise.DHKey, error) {
	priv := cfg.NoiseKey.Bytes()
	if len(priv) != curve25519.ScalarSize {
		return noise.DHKey{}, fmt.Errorf("node: the Noise key of this server is %d bytes, not %d",
			len(priv), curve25519.ScalarSize)
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return noise.DHKey{}, err
	}
	return noise.DHKey{Private: priv, Public: pub}, nil
}

// wireUp builds every half of the controller out of the configuration and the
// store, and is the one place that says which part is handed what. It opens no
// socket and starts nothing, so it is measurable on its own.
func wireUp(version string, cfg *config.Controller, key noise.DHKey, st *state.Store, keeper *chain.Keeper) RunConfig {
	reg := &Tunnels{}
	// The operating system of this server is read once, at the start: it
	// cannot change while this program runs, and a hello that names none is
	// one the contract takes (report.go).
	osName, err := OSRelease(OSReleaseFile)
	if err != nil {
		slog.Info("this server does not say what operating system it runs", "err", err)
	}
	engine := ctrlrt.NewPodman(podmanOf(cfg))
	licensed := func(ctx context.Context, a dbapi.App) (bool, error) {
		return license.Allowed(ctx, st, keeper.List(), installationOf(keeper), a, time.Now().Unix())
	}
	// The node ID of THIS server, asked afresh for every hello: it comes out
	// of the verified list by this server's own Noise key, and it falls back
	// to what the configuration holds while no verified version lists the key.
	// Which way round the two stand is a decision: the LIST decides, the file
	// only remembers. A file that outranked the list would keep naming a node
	// an admin has since renamed or removed.
	nodeID := func() string {
		if id := keeper.NodeID(key.Public); id != "" {
			return id
		}
		return cfg.Node
	}
	rep := NewReporter(ReportConfig{
		Version: version, Node: nodeID, Roles: cfg.Roles, OS: osName, State: st,
		TrustSeq: keeper.TrustSeq,
		// The engine takes the name of a container, and the container of an
		// app is named after its system user: this is the mapping report.go
		// asks whoever wires the two together for.
		Running: func(ctx context.Context, app string) (bool, error) {
			return engine.Running(ctx, ctrlrt.UserName(app))
		},
		Licensed: licensed,
		Send:     reg.Control,
	})
	// THE INSTALLER OF APPS, and it is built PER CALL on purpose.
	// install.Deps takes the trust list as a VALUE and this server’s node as
	// a string, and both move: the list is replaced version by version
	// (chain.Keeper.Current) and the node is only learned once a version
	// lists this server (nodeID above). A Manager built once here would
	// install against the list of this moment -- which is the reason the
	// updater beside it is still unwired, written out below.
	//
	// WHAT IT CARRIES OUT AND WHAT IT REFUSES. Only an app an admin
	// UPLOADED (core.LocalStore). An app out of a store of the trust list is
	// refused with install.ErrInternal, and that is the safer of the two
	// answers rather than a gap nobody sees: install.AcceptInput.LastSeq is
	// the newest catalogue number already seen from that store, and it is
	// what keeps a store from handing back an older catalogue. This
	// controller’s state has no memory of that number (state.Store has no
	// method for it, counted 18.10.2026), and a guard without memory is no
	// guard. So a store activation does not run until somebody gives it one;
	// nothing is lost by it, because no store activation ran before either.
	installer := appInstaller(version, cfg, st, engine, nodeID)
	srv := serve.New(serve.Deps{
		// serve.Deps may not change once New has seen it, so this one is the
		// ID AS IT STANDS AT THE START, and that is a named price: a server an
		// admin lists while it runs says its node in the hello at once (nodeID
		// above) and takes the chain, but its app channels and its tasks go on
		// with what it knew at the start until it is restarted once. What makes
		// the price small is that the chain now arrives at all -- without it
		// the ID was never learned and no restart ever helped.
		Node: nodeID(), Noise: key, Engine: engine, Chain: keeper, State: st,
		Installer: installer,
		// No updater yet, and this is a seam named rather than papered over:
		// update.Deps takes the trust list as a VALUE (update.Deps.List),
		// while this controller's list is replaced version by version under
		// it (chain.Keeper.Current). A Manager built here would install
		// against the list of the moment it was built in, which is not the
		// list the channel was decided on. Which of the two gives way is a
		// change in package update and belongs to whoever owns it; until
		// then an install channel ends E_INTERNAL, exactly as it did before
		// this task.
		Updater: nil,
	})
	c := RunConfig{State: st, Chain: keeper, Report: rep}
	for _, a := range cfg.APIs {
		c.APIs = append(c.APIs, link.Config{
			API: a, Noise: key, Hello: rep.Hello, Handler: srv,
			TunnelUp: reg.Up, TunnelDown: reg.Down,
		})
	}
	// Restore gets serve.RunService as its default (RestoreConfig.Run stays
	// nil), and that is what makes the default run for the first time: every
	// case of task 18 puts its own in.
	c.Restore = func(ctx context.Context) error {
		return Restore(ctx, RestoreConfig{
			State: st, Engine: engine, Licensed: licensed,
			Unlock: func(ctx context.Context, app string) error {
				return ctrlrt.UnlockData(ctx, app, cfg.DBKey.Bytes())
			},
		})
	}
	if slices.Contains(cfg.Roles, tunnel.RoleWeb) {
		c.Compare = webCompare(cfg, keeper)
	}
	return c
}

// podmanOf is the engine binary of this server.
func podmanOf(cfg *config.Controller) string {
	if cfg.Podman != "" {
		return cfg.Podman
	}
	return config.DefaultPodman
}

// installationOf is the installation this controller belongs to, as its own
// verified trust list names it. A controller that has verified no version yet
// has none to name, and a licence is then for another installation and
// refused - which is right: nothing of a licence may be believed before the
// list it is judged against was verified.
func installationOf(k *chain.Keeper) string {
	if l := k.List(); l != nil {
		return l.Installation
	}
	return ""
}

// webCompare is the certificate check of a server with the role web: every API
// node of the trust list, one after the other.
//
// A node it cannot reach does not stop the others: the next API may be up, and
// what Apache has for the one that is away keeps working until Sync takes it
// out. The errors are joined so the log names every one of them.
func webCompare(cfg *config.Controller, k *chain.Keeper) func(ctx context.Context) error {
	w := apache.New(apache.Options{
		WebConfig: cfg.WebConfig, TrustFile: config.APITrustFile, BalancerFile: config.BalancerFile,
	})
	return func(ctx context.Context) error {
		l := k.List()
		if l == nil {
			return nil
		}
		var errs []error
		for _, n := range l.Nodes {
			if n.Kind != trust.NodeAPI {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			changed, err := w.Compare(ctx, n)
			if err != nil {
				errs = append(errs, fmt.Errorf("node %q: %w", n.ID, err))
				continue
			}
			if changed {
				slog.Info("the certificate of an API was renewed and Apache was told", "node", n.ID)
			}
		}
		return errors.Join(errs...)
	}
}

// installerFunc is one function as a serve.Installer. The seam takes an
// interface so that nothing of serve reaches for package install; what this
// program hands over is a closure, because the Manager behind it is built
// anew for every call.
type installerFunc func(ctx context.Context, d access.Decision, list *trust.List,
	r io.Reader) ([]byte, error)

func (f installerFunc) Activate(ctx context.Context, d access.Decision, list *trust.List,
	r io.Reader) ([]byte, error) {
	return f(ctx, d, list, r)
}

// appDeps is what install.Manager is built out of for ONE activation. It is a
// type of its own so a case can read what the wiring hands over -- the node
// above all, which comes out of the verified list and not out of the file, and
// which no case saw while this was a literal inside the closure (18.10.2026).
type appDeps struct {
	version string
	cfg     *config.Controller
	st      *state.Store
	engine  ctrlrt.Engine
	nodeID  func() string
}

// deps are the dependencies of the Manager for an activation that runs
// against THIS version of the trust list.
func (a appDeps) deps(list *trust.List) install.Deps {
	return install.Deps{
		State: a.st, Engine: a.engine, Sys: install.Host{ServerKey: a.cfg.DBKey.Bytes()},
		// The list of the CHANNEL, handed over by serve, and the node as the
		// newest verified version names it. A list reached for here instead
		// would be whichever version is newest by now, and the package would
		// be checked against another one than the task was verified with.
		List: list, Node: a.nodeID(), Roles: a.cfg.Roles,
		NGC: a.version, ReleaseKey: a.cfg.ReleaseKey, StageDir: config.StageDir,
		// store.MaxPackage is the contract's own bound, and it is safe to
		// name here although a package costs twice its size while it is
		// checked: install.bound cuts this down to what the disk really has
		// free at every activation, and answers ErrSpace rather than filling
		// it.
		Max: store.MaxPackage,
		// No memory of a catalogue number, and appInstaller refuses every
		// store that would need one before this is ever read.
		LastSeq: func(string) uint64 { return 0 },
		Now:     func() int64 { return time.Now().Unix() },
	}
}

// appInstaller is the installer of apps this program hands the server, and
// it is a named function so that a case can reach it: a test-only accessor
// on the server would be a door in the product for the sake of a case.
//
// It builds its Manager ANEW for every call -- see the comment at its one
// caller for why -- and refuses every store but the uploaded one.
func appInstaller(version string, cfg *config.Controller, st *state.Store,
	engine ctrlrt.Engine, nodeID func() string) installerFunc {
	a := appDeps{version: version, cfg: cfg, st: st, engine: engine, nodeID: nodeID}
	return func(ctx context.Context, d access.Decision, list *trust.List,
		r io.Reader) ([]byte, error) {
		// THE REFUSAL ABOVE, BUILT AND NOT ONLY WRITTEN. The comment said a
		// store app is refused and nothing did it, until this line was added
		// on 18.10.2026 -- a comment that says the opposite of the code is
		// worse than no comment, and this one would have promised a guard
		// that was not there.
		//
		// The params are read through the contract’s own parser, so bytes that
		// are no params of this action are the peer’s fault and read as one;
		// the Manager reads them again the same way, which is one question
		// asked twice and cheaper than a value carried between the two.
		act, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
		if !ok {
			return nil, install.ErrInternal
		}
		params, err := core.ParseParams(act, d.Params())
		if err != nil {
			return nil, install.ErrVerify
		}
		p, ok := params.(*core.ActivateParams)
		if !ok || p.Store != core.LocalStore {
			return nil, install.ErrInternal
		}
		return install.New(a.deps(list)).Activate(ctx, d, r)
	}
}
