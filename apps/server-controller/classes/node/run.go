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
			keep(ctx, cfg)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.watchWeb(ctx, built)
	}()
	c.clock(ctx)
	// The context is over. The channels get their grace and then this program
	// returns, whether or not they came back: a controller that hangs on a
	// channel somebody holds open is one systemd kills, and that is worse
	// than a goroutine left behind in a process that is ending.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
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
	rep := NewReporter(ReportConfig{
		Version: version, Node: cfg.Node, Roles: cfg.Roles, OS: osName, State: st,
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
	srv := serve.New(serve.Deps{
		Node: cfg.Node, Noise: key, Engine: engine, Chain: keeper, State: st,
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
		Installer: installer{cfg: cfg, version: version, state: st, engine: engine},
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

// installer builds the install.Manager of one activate channel. It holds only
// what cannot change while this controller runs; the trust list comes with the
// call, because that is the one thing that does change under it (see
// serve.Installer).
type installer struct {
	cfg     *config.Controller
	version string
	state   *state.Store
	engine  ctrlrt.Engine
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

// Activate carries out one activation, on the trust list of its channel.
func (i installer) Activate(ctx context.Context, list *trust.List, d access.Decision,
	r io.Reader) ([]byte, error) {
	return install.New(install.Deps{
		State:  i.state,
		Engine: i.engine,
		Sys:    install.Host{ServerKey: i.cfg.DBKey.Bytes()},
		List:   list,
		Roles:  i.cfg.Roles,
		Node:   i.cfg.Node,
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
	}).Activate(ctx, d, r)
}
