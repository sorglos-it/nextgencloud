package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// Podman runs the containers of the apps with podman.
//
// It keeps nothing of its own per container. A container outlives the process
// that made it, so a call may well be the first this process makes about it, and
// a remembered table would be empty exactly then: every call finds its way with
// nothing but the name it was handed.
type Podman struct {
	bin string
}

// waitDelay is how long a cancelled call waits for the ends of the command
// after the podman client was killed, before they are cut (see Podman.run). It
// is short on purpose: at this point nothing is expected from the call any
// more, and the caller is already waiting past the limit it set.
const waitDelay = 500 * time.Millisecond

// PIDsLimit is the number of tasks a container of an app may have at once, and
// it is the whole answer to a fork bomb in an app: without it the only bound is
// podman's own default of 2048 (containers.conf, pids_limit), a decision that
// does not stand in this repo and changes with a podman version.
//
// Why 512 and not a guess, measured on this machine (13.10.2026, go1.27.2,
// NumCPU 64): a Go program of the shape of the hold process - 1000 goroutines,
// GOMAXPROCS 64 - holds 24 OS threads, and the cgroup pids controller counts
// threads, not processes. 512 is a factor of 21 above that and a quarter of
// podman's default, so the number is tighter than the unstated one and still
// far above anything a single app plausibly needs (a worker pool of 50
// processes lands near 50, not near 500).
//
// What it cannot be: a per-app number. That would be a field of the manifest,
// and the manifest lies in frozen shared/ - so every app gets the same ceiling
// until the contract says otherwise. An app that really needs more fails
// visibly, at its own fork, and not quietly.
const PIDsLimit = 512

// NewPodman takes the absolute path of the podman program
// (config.Controller.Podman, default config.DefaultPodman).
func NewPodman(bin string) *Podman { return &Podman{bin: bin} }

// validName is the name of a container, which is the name of the system user of
// its app: it begins with a letter or a digit, so it cannot become an option of
// podman, it is at most as long as a user name may be, and it holds nothing a
// user name may not hold.
//
// Whoever builds those names has to stay inside this grammar. A name outside it
// does not fail where it is built but here, at every single call for that app,
// and only on a server - so this line and the one that makes the user name are
// one decision in two places.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// appUser is who podman runs as and where it keeps its own two directories.
// Rootless podman holds the images under $HOME and the running containers under
// $XDG_RUNTIME_DIR, so a container is found again only by a call that names the
// same two.
type appUser struct {
	uid, gid  uint32
	home, run string
}

// ofName is the user a container belongs to, read out of the user database.
//
// Two things have to hold for this lookup to be the right one: the name of a
// container is the name of the system user of its app - that is how the user is
// made - and the home directory of that user is the data directory of the app
// (config.AppDataDir + "/" + app), which is the directory Create hands podman as
// HOME. If the home were anywhere else, every call after Create would look for
// the container in another store of podman than the one it was made in.
//
// Create is where that is held, with one limit worth knowing: it compares
// against the user database, so it catches the mismatch only once the user is in
// it. Before that it can only insist that the data directory lies under
// config.AppDataDir.
//
// This is also why the lookup stands here at all instead of a remembered table:
// a container outlives the process that made it, so the name is all there is.
func ofName(name string) (appUser, error) {
	if !validName.MatchString(name) {
		return appUser{}, ErrName
	}
	u, err := user.Lookup(name)
	if err != nil {
		return appUser{}, errors.Join(ErrUser, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return appUser{}, errors.Join(ErrUser, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return appUser{}, errors.Join(ErrUser, err)
	}
	// The same line Create holds, read from the other side: a container whose
	// user turns out to be root belongs to no app of this server, and running
	// podman as root for it would undo what the separation is for. Create cannot
	// have made such a container, so finding one here means the user database
	// says something else today than it did then.
	if uid == 0 || gid == 0 {
		return appUser{}, errors.Join(ErrIdentity, errors.New("uid or gid 0 for "+name))
	}
	return appUser{uid: uint32(uid), gid: uint32(gid), home: u.HomeDir, run: runDir(name)}, nil
}

// runDir is where podman keeps what is running for this container. It lies
// under config.RunDir, which is a tmpfs, so nothing of it survives a restart -
// which is what rootless podman expects of a runtime directory.
//
// Nothing here makes the directory: it has to be there and belong to the app's
// user before the first call, and after every restart of the machine again.
// EnsureRun is what makes it (run.go), and a caller that starts a container
// after a restart has to call it first - nothing in this file would notice
// that it did not.
//
// config.RunDir and not paths.runDir, and that difference is real: the two are
// the same in operation (paths.runDir is config.RunDir), and they differ only
// where a case moves paths.runDir to a directory it may write in. Such a case
// does not reach this line, because every call of podman needs Linux and root
// and skips itself otherwise.
func runDir(name string) string { return config.RunDir + "/" + name }

// validPath holds every path that goes into an argument of podman. Five things
// it must not be: empty, relative (a relative path would also be read against a
// working directory nobody set here), not already clean, carrying a colon, or
// carrying a NUL or a line break. The colon is the one with the sharpest edge: a
// volume is given as source:target:mode, so a colon in the source would cut the
// specification in another place and mount another directory. A leading dash is
// covered by the first two: a dash is not a slash.
func validPath(s string) error {
	if s == "" || !strings.HasPrefix(s, "/") || path.Clean(s) != s ||
		strings.ContainsAny(s, ":\x00\n") {
		return ErrPath
	}
	return nil
}

// LoadImage puts the image into the store of the app and answers with its
// reference.
func (p *Podman) LoadImage(ctx context.Context, name, tarPath string) (string, error) {
	// Whose store comes before what goes into it: an image that cannot be
	// placed with an app has no business being unpacked anywhere.
	u, err := ofName(name)
	if err != nil {
		return "", err
	}
	if err := validPath(tarPath); err != nil {
		return "", err
	}
	// The reference is read next: an image whose tar names no single tag is not
	// loaded at all, because the name under which podman would then keep it is
	// one no later call here could name.
	ref, err := ImageTag(tarPath)
	if err != nil {
		return "", err
	}
	// THE TAR GOES IN ON STDIN AND NOT AS A PATH, and that is the whole point
	// of the change of 19.10.2026: podman runs as the app's own user, and the
	// staged package lies under a directory of root (install.Accept writes it,
	// 0700). With --input the app user has to OPEN that file itself, and it
	// may not - measured on the real machine, from podman's own mouth:
	//
	//   Error: faccessat /var/lib/ngc/staged/accept-.../files/services/
	//   amd64.tar: permission denied
	//
	// The file is opened HERE instead, by the controller, which is root and
	// whose file it is; the child gets the open end and no path at all. So
	// nothing of the staging directory has to be opened up for an app, which
	// is the other way this could have been mended and the worse one: that
	// directory holds the package of every app being set up.
	f, err := os.Open(tarPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var loadErr bytes.Buffer
	if _, err := p.run(ctx, u, []string{"load"}, f, nil, &loadErr); err != nil {
		return "", withSaid(err, &loadErr)
	}
	return ref, nil
}

// RemoveImage takes the image out of the store of that app again.
//
// As with Remove, an image that is not there is an error and is meant to be
// one: nothing of podman's output is read, so a missing image and a refused
// removal arrive alike, and the caller either asks beforehand or bears it.
func (p *Podman) RemoveImage(ctx context.Context, name, ref string) error {
	u, err := ofName(name)
	if err != nil {
		return err
	}
	if !validRef.MatchString(ref) {
		return ErrImage
	}
	_, err = p.run(ctx, u, []string{"rmi", ref}, nil, nil, nil)
	return err
}

// createArgs is the argument list of Create and every refusal that comes
// before it. It stands apart from Create for one reason, and it is a measuring
// reason: the hardening of an app container - no capabilities, no rights to
// gain, nothing writable but /data, no network without a grant - lives entirely
// in this list, and as long as the list only existed inside a call of podman,
// no case on a machine without podman could hold any of it. Measured on
// 12.10.2026: with the list built inside Create, --cap-drop ALL,
// no-new-privileges, --read-only and --network none could all be taken out and
// the whole controller set stayed green. Nothing of the order or of the
// refusals changed with this move; it is the same list, reachable without
// starting anything (hardening_test.go).
func createArgs(c Container) ([]string, error) {
	if !validName.MatchString(c.Name) {
		return nil, ErrName
	}
	if !validRef.MatchString(c.Image) {
		return nil, ErrImage
	}
	if err := validPath(c.DataDir); err != nil {
		return nil, err
	}
	// The program that holds the container open is mounted into it, so where it
	// comes from decides what the container can read. Only what NGC installed
	// may be mounted: without this, any readable file of this server could be
	// handed to an app.
	if err := validPath(c.Hold); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(c.Hold, config.BinDir+"/") {
		return nil, ErrPath
	}
	if c.CPU < 0 || c.Memory < 0 {
		return nil, ErrLimit
	}
	// A container of an app is nobody's root. Without this, the one thing this
	// whole package is for falls away in a single call: podman would run as root,
	// the app's data directory would be root's own, and an app that writes into
	// /data would write there. Create is the only call that is told a user, so
	// it is the only place this can be said.
	if c.UID == 0 || c.GID == 0 {
		return nil, errors.Join(ErrIdentity, errors.New("uid or gid 0 for "+c.Name))
	}
	cpu, mem := c.CPU, c.Memory
	if cpu == 0 {
		cpu = store.DefaultCPU
	}
	if mem == 0 {
		mem = store.DefaultMemory
	}
	args := []string{"create", "--name", c.Name}
	// Without the network the container has none at all, and that is said out
	// loud. With the network, nothing is said at all: how a rootless container
	// reaches the network is podman's own choice, and naming one here would pin
	// this server to a program that may not be on it. Podman has changed that
	// choice before, and the one it carried earlier is a separate package today.
	//
	// A way out that is pinned and missing is worse than one nobody named: every
	// app with network would fail at once, on a fresh server, with a message
	// nobody can read. What we give up for that: podman may one day choose
	// differently, and then the network of an app behaves differently - visibly,
	// and for one app at a time. Whether rootless network works for an app's user
	// at all is something the installation has to find out, not this line.
	if !c.Network {
		args = append(args, "--network", "none")
	}
	args = append(args,
		// Thousandths of a core, written as podman reads it.
		"--cpus", fmt.Sprintf("%d.%03d", cpu/1000, cpu%1000),
		"--memory", strconv.Itoa(mem)+"m",
		// The third limit, and the one that is not about being a good
		// neighbour: see PIDsLimit.
		"--pids-limit", strconv.Itoa(PIDsLimit),
		// The image itself stays unwritable; what the app keeps belongs under
		// /data, which is the only place it may write.
		"--read-only")
	// The capabilities go, unless the app was granted store.NeedRoot: then they
	// come back, and that is what root in the container means and all it means.
	// Everything else above and below stays either way, and the container runs
	// in the user namespace of the app's own user either way, so this is never
	// root on this server.
	if !c.Root {
		args = append(args, "--cap-drop", "ALL")
	}
	args = append(args, "--security-opt", "no-new-privileges",
		// nosuid and nodev are the second layer behind no-new-privileges, and
		// /data is the one mount whose content the app itself writes: without
		// them an app that gets a file with the set-user-id bit onto its own
		// disk - by upload, by its own writing, by a restored backup - has the
		// bit honoured inside the container. nodev takes the other half: a
		// device node written there would be a door to the disks of the host.
		// Both cost nothing, because no app of a store needs either.
		"--volume", c.DataDir+":/data:rw,nosuid,nodev",
		// ngc-controller itself keeps the container alive, started with the
		// subcommand hold. An app image brings nothing of its own for this.
		// The same two here, although this one is ours and read-only: a mount
		// option is not a judgement about who wrote the file, and if a program
		// of this server ever carried the set-user-id bit, this is where the
		// container would be the one to find out.
		"--volume", c.Hold+":/ngc/ngc-hold:ro,nosuid,nodev",
		// 0 in the container is the app's own user outside it: podman runs
		// rootless, so the user namespace maps it to exactly that user.
		"--user", "0:0", c.Image, "/ngc/ngc-hold", "hold")
	return args, nil
}

// Create makes the container without starting it.
func (p *Podman) Create(ctx context.Context, c Container) error {
	args, err := createArgs(c)
	if err != nil {
		return err
	}

	// Create is the only call that does not read the user out of the name: user,
	// group and home come from the argument. That makes it the last way past the
	// separation the name is, so everything it was handed is held against the
	// user database here - every call after this one takes all three from there.
	//
	// Where the name is in no user database yet, there is nothing to hold it
	// against, and then only the place is left: a data directory lies under
	// config.AppDataDir and nowhere else. Why a missing entry is let through at
	// all: podman is believed to need the entry of the user it runs as (rootless
	// podman reads its subuid range by name), so where it matters the entry is
	// there - believed, not measured here, because no podman runs on this
	// machine.
	if db, err := user.Lookup(c.Name); err == nil {
		if db.HomeDir != c.DataDir {
			return errors.Join(ErrHome, errors.New("the home of "+c.Name+" is "+db.HomeDir))
		}
		if db.Uid != strconv.FormatUint(uint64(c.UID), 10) ||
			db.Gid != strconv.FormatUint(uint64(c.GID), 10) {
			return errors.Join(ErrIdentity,
				errors.New(c.Name+" is "+db.Uid+":"+db.Gid))
		}
	} else if !strings.HasPrefix(c.DataDir, config.AppDataDir+"/") {
		return ErrHome
	}
	u := appUser{uid: c.UID, gid: c.GID, home: c.DataDir, run: runDir(c.Name)}
	var createErr bytes.Buffer
	_, err = p.run(ctx, u, args, nil, nil, &createErr)
	err = withSaid(err, &createErr)
	return err
}

// Start starts a container that was created.
func (p *Podman) Start(ctx context.Context, name string) error {
	u, err := ofName(name)
	if err != nil {
		return err
	}
	var startErr bytes.Buffer
	_, err = p.run(ctx, u, []string{"start", name}, nil, nil, &startErr)
	err = withSaid(err, &startErr)
	return err
}

// Stop ends the container.
func (p *Podman) Stop(ctx context.Context, name string) error {
	u, err := ofName(name)
	if err != nil {
		return err
	}
	if _, err := p.run(ctx, u, []string{"stop", "--time", "10", name}, nil, nil, nil); err == nil {
		return nil
	}
	// The ten seconds were given and this still failed, so the container is
	// killed now. Only now: a kill behind a stop that worked would take those
	// ten seconds away from every service that has something to write out.
	_, err = p.run(ctx, u, []string{"kill", name}, nil, nil, nil)
	return err
}

// Remove takes the container away. A container that is not there is an error on
// purpose; the doc of Engine.Remove says why and what the caller does instead.
func (p *Podman) Remove(ctx context.Context, name string) error {
	u, err := ofName(name)
	if err != nil {
		return err
	}
	_, err = p.run(ctx, u, []string{"rm", name}, nil, nil, nil)
	return err
}

// Running reports whether the container is running.
func (p *Podman) Running(ctx context.Context, name string) (bool, error) {
	u, err := ofName(name)
	if err != nil {
		return false, err
	}
	// Nothing of podman's output is parsed here, only whether anything came
	// back at all: the filter names one container by both ends of its name and
	// asks for the running ones, so an answer is the answer. A container whose
	// name merely begins with this one cannot answer for it.
	var out bytes.Buffer
	args := []string{"ps", "--quiet", "--no-trunc",
		"--filter", "name=^" + name + "$", "--filter", "status=running"}
	if _, err := p.run(ctx, u, args, nil, &out, nil); err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out.Bytes())) > 0, nil
}

// execArgs is the argument list of Exec and the refusals that belong to it. It
// stands apart from Exec for the reason given at createArgs: what this list
// does and does not carry - no --privileged, no --cap-add, nothing that would
// hand out more than Create decided - is only measurable without podman if the
// list can be built without podman (hardening_test.go).
//
// The handing over of the environment file is not here: that one changes the
// machine (chown, and the removal afterwards), and Exec keeps it, in the order
// it had.
func execArgs(e Exec) ([]string, error) {
	if len(e.Command) == 0 {
		return nil, ErrCommand
	}
	args := []string{"exec"}
	if e.Stdin != nil {
		// Without input the command gets no open standard input: a channel
		// nobody writes to would hold the call open.
		args = append(args, "--interactive")
	}
	if e.EnvFile != "" {
		if err := validPath(e.EnvFile); err != nil {
			return nil, err
		}
		// It has to lie on the tmpfs under config.RunDir, and that is not a
		// matter of tidiness: the line below removes the file, and this process
		// is root. A path from anywhere would make this call a way to delete any
		// file of this server.
		if !strings.HasPrefix(e.EnvFile, config.RunDir+"/") {
			return nil, ErrPath
		}
		args = append(args, "--env-file", e.EnvFile)
	}
	if e.Root {
		// The container already runs as 0:0, so this says out loud what it
		// would do anyway. Whether the capabilities are there was decided once,
		// at Create, and nothing here hands out more than that.
		args = append(args, "--user", "0:0")
	}
	// Everything behind -- is the container and its command. A command of a
	// manifest may well begin with a dash, and this is what keeps podman from
	// reading it as one of its own options.
	args = append(args, "--", e.Name)
	args = append(args, e.Command...)
	return args, nil
}

// Exec runs a command in a running container and answers with the number the
// command ended with.
func (p *Podman) Exec(ctx context.Context, e Exec) (int, error) {
	u, err := ofName(e.Name)
	if err != nil {
		return 0, err
	}
	args, err := execArgs(e)
	if err != nil {
		return 0, err
	}
	if e.EnvFile != "" {
		// The file is the only copy of what the call was given, and it goes as
		// soon as the call is over. Not earlier: podman reads it itself while
		// it starts, so taking it away right after the start is a race with
		// that read.
		defer os.Remove(e.EnvFile)
		// Podman runs below as the app's user and reads the file itself, so the
		// file has to belong to that user: the caller writes it as this
		// process, which is root, and a 0600 file of root is nothing the app's
		// user may open. The uid is right here - ofName read it out of the user
		// database at the top of this function - so no caller has to carry it,
		// and the mode stays the caller's, because only the caller knows that
		// the file is the one copy.
		//
		// ErrOwner and not ErrIdentity: the uid above came out of the user
		// database and is the right one. What failed is the handing over, and
		// its ordinary reason is a controller that does not run as root.
		if err := os.Chown(e.EnvFile, int(u.uid), int(u.gid)); err != nil {
			return 0, errors.Join(ErrOwner, err)
		}
	}

	code, err := p.run(ctx, u, args, e.Stdin, e.Stdout, e.Stderr)
	var ended *exec.ExitError
	// Only a status the command really ended with becomes the answer. A process
	// that a signal ended has no status at all - Go reports -1 for it, and -1 is
	// no number a command can end with - so that stays an error, and a cancelled
	// call is exactly that case: the context kills podman. What the caller must
	// not get is a -1 that looks like something the service decided.
	if errors.As(err, &ended) && ended.Exited() {
		// The number belongs to the command, not to this engine. Podman uses
		// 125 to 127 for its own troubles and a service may end with those
		// same numbers, so the two cannot be told apart without reading what
		// podman wrote - which is the caller's to show, not ours to parse.
		return code, nil
	}
	return 0, err
}

// appGroups is the supplementary group list every podman of an app runs with:
// the group of the apps folder, and nothing else. One group and not the ones
// the user database happens to hold, because what this list is FOR is one
// thing - stepping through that folder - and a process that carries more than
// it needs carries it into every container it starts.
func appGroups(ctx context.Context) ([]uint32, error) {
	gid, err := paths.appsGID(ctx)
	if err != nil {
		return nil, err
	}
	if gid <= 0 {
		return nil, errors.Join(ErrUser, errors.New("the gid of the apps group is not above 0"))
	}
	return []uint32{uint32(gid)}, nil
}

// said is what podman wrote on its error channel, cut to MaxSaid bytes and with
// the line breaks turned into one line, so it can go into an error.
//
// WHY THIS EXISTS AT ALL, and it is the most expensive line of this round:
// every call below handed nil for stderr, so what came back from a failure was
// "exit status 1" and nothing else. On 18.10.2026 that cost a whole evening on
// the real machine - the activation failed, the controller rolled everything
// back, the API answered E_INTERNAL, no journal of either program held a word,
// and the reason was one sentence podman had written and nobody had read:
//
//	cannot resolve /var/lib/ngc/apps/org.sorglos.terminal: lstat ...:
//	permission denied
//
// It is CUT, because the output of a foreign program is not ours to let grow,
// and it goes into the error of this package - which the controller logs for
// an admin and never shows a browser (serve/activate.go answers the contract's
// own code and its own sentence).
const MaxSaid = 2000

func said(b *bytes.Buffer) string {
	t := strings.TrimSpace(b.String())
	if len(t) > MaxSaid {
		t = t[:MaxSaid] + " ..."
	}
	return strings.Join(strings.Fields(t), " ")
}

// withSaid joins what podman said to err, and answers err unchanged where it
// said nothing. A failure whose cause the program named is a failure that says
// so.
func withSaid(err error, b *bytes.Buffer) error {
	if err == nil {
		return nil
	}
	if t := said(b); t != "" {
		return errors.Join(err, errors.New("podman: "+t))
	}
	return err
}

// run starts podman with this argument list and nothing else: no shell, no
// string built out of anything. It answers with the number podman ended with,
// and an error whenever that number is not 0 - whether the number means the
// command failed or podman did is a question only Exec asks.
func (p *Podman) run(ctx context.Context, u appUser, args []string,
	in io.Reader, out, errOut io.Writer) (int, error) {
	// THE SUPPLEMENTARY GROUPS, and without them nothing of this works: a
	// child with a Credential calls setgroups, and a Credential without a
	// group list sets NONE - so what /etc/group says about this user is
	// nothing the process carries. The folder the data directories lie in is
	// 0710 root:ngc-apps (data.go), so a podman without that group cannot
	// reach the home of its own app. Measured on the real machine on
	// 19.10.2026, with the group set on the folder and the user in it:
	// "cannot resolve /var/lib/ngc/apps/org.sorglos.terminal: lstat ...:
	// permission denied" - the membership was in the user database and not in
	// the process.
	groups, err := appGroups(ctx)
	if err != nil {
		return 0, err
	}
	attr, err := credential(u.uid, u.gid, groups)
	if err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, p.bin, args...)
	cmd.SysProcAttr = attr
	// The whole environment, not an addition to the one of this process: a
	// controller runs as root and carries whatever systemd gave it, and none of
	// that belongs to an app. These four are what rootless podman needs - where
	// its store is, where what runs is, where its own helpers are, and a
	// locale that is the same everywhere.
	cmd.Env = []string{
		"HOME=" + u.home,
		"XDG_RUNTIME_DIR=" + u.run,
		"PATH=/usr/bin:/bin",
		"LANG=C.UTF-8",
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errOut
	// Without this, a cancelled call does not come back while the service
	// lives. The context kills podman - the host client - and nothing else:
	// the process inside the container is no child of it, the container goes
	// on running and is meant to, and it keeps the ends of the call open. Go's
	// Wait then waits for those ends, which is for as long as the service
	// wants. Measured on the stand-in of this engine: 3.15 s for a service of
	// 2 s, 12.51 s for one of 10 s - unbounded, not slow. So the ends are cut
	// a moment after the kill, and a caller gets its answer at the limit it
	// set. What stays running in the container is the container's business;
	// whoever has to end that stops the container (Stop).
	cmd.WaitDelay = waitDelay
	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	// A status the command really ended with is the answer, even when the
	// context ran out at the same moment: the command did end that way, and that
	// is what the caller wants to know.
	var ended *exec.ExitError
	if errors.As(err, &ended) && ended.Exited() {
		return ended.ExitCode(), err
	}
	// From here there is no status, and the caller must never be told a number.
	// The context is asked first, and not only in the branch where Go hands over
	// an ExitError: Cmd.Wait prefers the error of the process and reports the one
	// of the context only when the process itself had none, so a command that
	// ended with 0 in the moment the context ran out arrives here as the plain
	// context error. Measured, not reasoned out: 6 of 400 cancelled calls take
	// that way. Both ways mean the same thing for the caller, so both say it.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return 0, errors.Join(ctxErr, ErrKilled, err)
	}
	// An ExitError without a status and without a context: a signal of someone
	// else ended it. Anything else never ran at all - a missing program, a
	// refused switch of user - and that is this engine's failure, not a killing.
	if errors.As(err, &ended) {
		return 0, errors.Join(ErrKilled, err)
	}
	return 0, err
}
