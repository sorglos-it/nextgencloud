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

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	// Named catch here and guard everywhere else: this package already has a
	// function guard (ask.go), which gives the terminal its echo back when a
	// signal ends the pairing question. Renaming that one for an import would
	// be the tail wagging the dog.
	catch "github.com/sorglos-it/nextgencloud/apps/server-api/classes/guard"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/apache"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/chain"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/license"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/serve"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
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
	// Sync brings the balancer, the trust file and web.json in line with the
	// API servers of the trust list (apache.Writer.Sync). It runs whenever a
	// version of the list was verified and kept (Listed) and once a day. nil:
	// this server has no role web and writes nothing.
	//
	// It is the half that owns the LIST, where Compare owns the certificate of
	// a node already written. Both are needed and neither does the other's
	// work: without this one a second API server that joined and was taken in
	// never reaches Apache at all (measured, serve.Deps.TrustChanged).
	Sync func(ctx context.Context) error
	// Listed releases Sync: one place in the buffer, because what it releases
	// does not depend on how many versions arrived, only that one did. nil:
	// only the daily beat runs Sync.
	Listed <-chan struct{}
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

// watchWeb is what a server with the role web tells Apache, and it is one
// goroutine for both halves: the list (Sync) whenever a version of the trust
// list was verified and kept (Listed), and the certificates (Compare) for
// every tunnel that came off (built). The daily beat runs both.
//
// Sync before Compare on the day, and that order is the point: Sync writes the
// trust file out of the list and Compare then finds nothing left to renew,
// while the other way round Compare would work on a file Sync is about to
// write anyway.
//
// The daily beat carries Sync and not only Compare because a Sync that failed
// is otherwise never retried: Listed fires once per version, and a version
// that arrived while an API was unreachable would leave the balancer behind
// the list until the next admin changed something. Sync writes nothing when
// the files already say what the list says (apache.Writer.apply compares the
// bytes), so the beat costs one read per API and no reload.
//
// It is a goroutine of its own and no case of clock, because both halves reach
// out over the network: a slow API would hold the status of this server behind
// them.
func (c RunConfig) watchWeb(ctx context.Context, built <-chan struct{}) {
	if c.Compare == nil && c.Sync == nil {
		return
	}
	day := time.NewTicker(c.every(c.Web, WebEvery))
	defer day.Stop()
	for {
		var sync, compare bool
		select {
		case <-ctx.Done():
			return
		case <-day.C:
			sync, compare = true, true
		case <-built:
			compare = true
		case <-c.Listed:
			// A version of the trust list arrived. Only the list half runs:
			// the certificate of a node Sync just wrote is the one it fetched
			// to check its pin.
			sync = true
		}
		if sync && c.Sync != nil {
			if err := c.Sync(ctx); err != nil && ctx.Err() == nil {
				// The list is kept either way - this is what Apache was not
				// told. Named as such, because the balancer then answers for
				// a set of API servers that is not the installation's.
				c.logger().Warn("Apache could not be brought in line with the trust list", "err", err)
			}
		}
		if compare && c.Compare != nil {
			if err := c.Compare(ctx); err != nil && ctx.Err() == nil {
				c.logger().Warn("the certificates of the APIs could not be checked", "err", err)
			}
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
	// The signal that a version of the trust list was verified and kept,
	// coalescing like built in Run: one place is enough, because what it
	// releases works on the list AS IT NOW STANDS and not on the version that
	// released it. It is never closed - the writer is the goroutine of a
	// channel, and closing it would be a send on a closed channel for the rest
	// of the program.
	listed := make(chan struct{}, 1)
	srv := serve.New(serve.Deps{
		// The one thing this does is release watchWeb, and it does it without
		// holding the channel that brought the list: serve.Deps.TrustChanged
		// may do nothing slow, and writing Apache's files plus a configtest
		// and a reload is as slow as it gets.
		TrustChanged: func() {
			select {
			case listed <- struct{}{}:
			default:
			}
		},
		// serve.Deps may not change once New has seen it, so this one is the
		// ID AS IT STANDS AT THE START, and that is a named price: a server an
		// admin lists while it runs says its node in the hello at once (nodeID
		// above) and takes the chain, but its app channels and its tasks go on
		// with what it knew at the start until it is restarted once. What makes
		// the price small is that the chain now arrives at all -- without it
		// the ID was never learned and no restart ever helped.
		Node: nodeID(), Noise: key, Engine: engine, Chain: keeper, State: st,
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
		// The installer is built per channel, not once here, and that is the
		// same seam the updater above names - only on this side it can be
		// closed. install.Deps takes the trust list as a VALUE as well, and
		// serve hands the list the channel was decided on to every call
		// (serve.Installer), so the Manager of a channel is built on that one
		// version instead of on whatever the keeper holds in the instant the
		// installer reaches for it.
		Installer: installer{cfg: cfg, version: version, state: st, engine: engine,
			node: nodeID},
	})
	c := RunConfig{State: st, Chain: keeper, Report: rep, Listed: listed}
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
		w := apache.New(apache.Options{
			WebConfig: cfg.WebConfig, TrustFile: config.APITrustFile, BalancerFile: config.BalancerFile,
		})
		c.Compare = webCompare(w, keeper)
		c.Sync = webSync(w, keeper)
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
func webCompare(w *apache.Writer, k *chain.Keeper) func(ctx context.Context) error {
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

// installer builds the install.Manager of one activate channel. It holds only
// what cannot change while this controller runs; the trust list comes with the
// call, because that is the one thing that does change under it (see
// serve.Installer).
type installer struct {
	cfg     *config.Controller
	version string
	state   *state.Store
	engine  ctrlrt.Engine
	// node is the ID of THIS server, asked afresh for every activation
	// (wireUp.nodeID): out of the verified list by this server's own Noise
	// key, and only then out of the configuration.
	//
	// Asked afresh and not frozen, and that is the whole difference to
	// serve.Deps.Node beside it: serve.New may not be handed a changing
	// value, but this Manager is built per channel, so it can have the ID as
	// it now stands. It matters because the node of the SIGNED task is held
	// against this one (task.Check over Input.Node), and pairing empties
	// config.Controller.Node - so a Manager that read the file would hold
	// every root task of a paired server against "" and answer E_VERIFY.
	// Measured 18.10.2026 on a running installation: with the ID out of the
	// file the activation ended "task: malformed task or other target"; with
	// the ID filled in, the task verified and the channel ran on into
	// install.Accept.
	node func() string
}

// lastSeq is the newest catalogue number this installation has already seen
// from a store, and it answers 0 for every store - which means "none seen".
//
// This is a GAP and not a decision of this file: nothing in this controller
// books the catalogue number of an installation (state.Store has no place for
// it, checked 18.10.2026), so store.VerifyCatalog cannot refuse an older
// catalogue than the last one (its line "c.Seq < lastSeq"). What stays in
// force is every other wall of install.Accept: the store's signature, the
// checksum of the task, and the version of the row, which refuses a package
// older than the one installed (install.Activate, step 2). What is missing is
// only the roll-back of the CATALOGUE, and that needs a counter whoever owns
// the local store has to add.
func (installer) lastSeq(string) uint64 { return 0 }

// nodeOf is this server's node ID for one activation. It is a method and not
// the bare field so that a case can hold the "afresh" of it: a reader that
// kept the ID would answer the same thing twice where this one answers what
// the list now says.
func (i installer) nodeOf() string {
	if i.node == nil {
		// A controller wired without it is this program wired wrong, and an
		// empty ID is what install.Activate refuses anyway: every signed task
		// names a node, and "" matches none.
		return ""
	}
	return i.node()
}

// deps is what the Manager of one activation is built with. It is named and
// not written inside Activate so that a case can READ it: what reaches
// install.Deps is the whole of this wiring, and a case that only called the
// pieces beside it would measure the pieces and not the wiring. Measured
// 18.10.2026: with the node taken from the configuration instead of from
// nodeOf, every case of the pieces stayed green.
func (i installer) deps(list *trust.List) install.Deps {
	return install.Deps{
		State:  i.state,
		Engine: i.engine,
		Sys:    install.Host{ServerKey: i.cfg.DBKey.Bytes()},
		List:   list,
		Roles:  i.cfg.Roles,
		Node:   i.nodeOf(),
		NGC:    i.version,
		// An activation never takes a release; install.Accept asks for the
		// key all the same (install.Deps.ReleaseKey).
		ReleaseKey: i.cfg.ReleaseKey,
		StageDir:   config.StageDir,
		// store.MaxPackage is the bound of the contract (4 GiB). Every
		// activation cuts it down to what the disk really has free
		// (install.bound), so this is the ceiling and not the promise.
		Max:     store.MaxPackage,
		LastSeq: i.lastSeq,
		Now:     func() int64 { return time.Now().Unix() },
	}
}

// Activate carries out one activation, on the trust list of its channel.
func (i installer) Activate(ctx context.Context, list *trust.List, d access.Decision,
	r io.Reader) ([]byte, error) {
	return install.New(i.deps(list)).Activate(ctx, d, r)
}

// webSync brings Apache in line with the API servers of the trust list: the
// balancer, the certificates it proxies to and web.json, all three out of the
// one list (apache.Writer.Sync).
//
// It is the half that puts a NEW server in, and nothing else does: Compare
// renews the certificate of a node that already stands in the trust file and
// leaves a node that does not to this one. A server that joined and was taken
// into the trust list therefore reaches Apache here or not at all - measured
// on 17.10.2026, where it was not at all (serve.Deps.TrustChanged).
//
// A list with no API node at all is NOT written: Sync would render a balancer
// with no member, and Apache answers every /api/ with 503 then. It cannot
// happen from a verified list - a list whose own API is gone is one no browser
// could have signed - and if it ever does, what Apache has keeps working and
// the log says why nothing was written.
func webSync(w *apache.Writer, k *chain.Keeper) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		l := k.List()
		if l == nil {
			return nil
		}
		apis := 0
		for _, n := range l.Nodes {
			if n.Kind == trust.NodeAPI {
				apis++
			}
		}
		if apis == 0 {
			return fmt.Errorf("node: version %d of the trust list names no API server", l.Seq)
		}
		// Both halves can be true at once: Sync leaves out a server it could
		// not reach or whose pin did not match, writes the rest and names the
		// ones it left. So the line that says Apache followed is written for
		// what WAS written, and the error goes on to the caller, which logs
		// it as a warning of its own. One of the two alone would hide half of
		// what happened.
		changed, err := w.Sync(ctx, l.Nodes)
		if changed {
			// The numbers and not the names: which servers an installation
			// has is its own, and this line is read to see THAT Apache
			// followed and over how many servers.
			slog.Info("Apache was brought in line with the trust list",
				"seq", l.Seq, "apis", apis)
		}
		return err
	}
}
