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

// NewPodman takes the absolute path of the podman program
// (config.Controller.Podman, default config.DefaultPodman).
func NewPodman(bin string) *Podman { return &Podman{bin: bin} }

// validName is the name of a container, which is the name of the system user of
// its app: it begins with a letter or a digit, so it cannot become an option of
// podman, it is at most as long as a user name may be, and it holds nothing a
// user name may not hold.
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
// The name of the container is the name of that user, and after Create it is
// the only thing a call carries. Its home directory has to be the data
// directory of the app (config.AppDataDir + "/" + app), because that is the
// one Create hands podman: both have to name the same directory or podman
// looks for the container in another store than the one it made it in. Create
// holds that together where it can see both.
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
	return appUser{uid: uint32(uid), gid: uint32(gid), home: u.HomeDir, run: runDir(name)}, nil
}

// own is this process itself. Only the two calls about images use it: their
// signature carries no app, so the image lands in the store of whoever runs
// this controller.
func (p *Podman) own() appUser {
	return appUser{uid: uint32(os.Getuid()), gid: uint32(os.Getgid()),
		home: config.DataDir, run: config.RunDir}
}

// runDir is where podman keeps what is running for this container. It lies
// under config.RunDir, which is a tmpfs, so nothing of it survives a restart -
// which is what rootless podman expects of a runtime directory. Nothing here
// makes the directory: it has to be there and belong to the app's user before
// the first call, and after every restart of the machine again.
func runDir(name string) string { return config.RunDir + "/" + name }

// validPath holds every path that goes into an argument of podman: absolute,
// already clean, and without a colon. The colon is the reason: a volume is
// given as source:target:mode, so a colon in the source would cut the
// specification in another place and mount another directory.
func validPath(s string) error {
	if s == "" || !strings.HasPrefix(s, "/") || path.Clean(s) != s ||
		strings.ContainsAny(s, ":\x00\n") {
		return ErrPath
	}
	return nil
}

// LoadImage puts the image into the store and answers with its reference.
func (p *Podman) LoadImage(ctx context.Context, tarPath string) (string, error) {
	if err := validPath(tarPath); err != nil {
		return "", err
	}
	// The reference is read first: an image whose tar names no single tag is
	// not loaded at all, because the name under which podman would then keep it
	// is one no later call here could name.
	ref, err := ImageTag(tarPath)
	if err != nil {
		return "", err
	}
	if _, err := p.run(ctx, p.own(), []string{"load", "--input", tarPath}, nil, nil, nil); err != nil {
		return "", err
	}
	return ref, nil
}

// RemoveImage takes the image out of the store again.
func (p *Podman) RemoveImage(ctx context.Context, ref string) error {
	if !validRef.MatchString(ref) {
		return ErrImage
	}
	_, err := p.run(ctx, p.own(), []string{"rmi", ref}, nil, nil, nil)
	return err
}

// Create makes the container without starting it.
func (p *Podman) Create(ctx context.Context, c Container) error {
	if !validName.MatchString(c.Name) {
		return ErrName
	}
	if !validRef.MatchString(c.Image) {
		return ErrImage
	}
	if err := validPath(c.DataDir); err != nil {
		return err
	}
	if err := validPath(c.Hold); err != nil {
		return err
	}
	if c.CPU < 0 || c.Memory < 0 {
		return ErrLimit
	}
	cpu, mem := c.CPU, c.Memory
	if cpu == 0 {
		cpu = store.DefaultCPU
	}
	if mem == 0 {
		mem = store.DefaultMemory
	}
	// Without the network the container has none at all; with it, podman builds
	// one in the user namespace of the app's own user, which is what
	// slirp4netns is: no interface of this server lands in the container.
	net := "none"
	if c.Network {
		net = "slirp4netns"
	}
	args := []string{"create", "--name", c.Name, "--network", net,
		// Thousandths of a core, written as podman reads it.
		"--cpus", fmt.Sprintf("%d.%03d", cpu/1000, cpu%1000),
		"--memory", strconv.Itoa(mem) + "m",
		// The image itself stays unwritable; what the app keeps belongs under
		// /data, which is the only place it may write.
		"--read-only"}
	// The capabilities go, unless the app was granted store.NeedRoot: then they
	// come back, and that is what root in the container means and all it means.
	// Everything else above and below stays either way, and the container runs
	// in the user namespace of the app's own user either way, so this is never
	// root on this server.
	if !c.Root {
		args = append(args, "--cap-drop", "ALL")
	}
	args = append(args, "--security-opt", "no-new-privileges",
		"--volume", c.DataDir+":/data:rw",
		// ngc-controller itself keeps the container alive, started with the
		// subcommand hold. An app image brings nothing of its own for this.
		"--volume", c.Hold+":/ngc/ngc-hold:ro",
		// 0 in the container is the app's own user outside it: podman runs
		// rootless, so the user namespace maps it to exactly that user.
		"--user", "0:0", c.Image, "/ngc/ngc-hold", "hold")

	u := appUser{uid: c.UID, gid: c.GID, home: c.DataDir, run: runDir(c.Name)}
	// This is the one call that sees both the data directory and the user, so
	// it is the one that can tell whether the two agree. They have to: every
	// call after this one takes the home directory out of the user database, so
	// a user whose home is somewhere else would send those calls to another
	// podman store than the container was made in. A name that is in no user
	// database yet is let through - podman itself needs the entry, so where it
	// matters the entry is there.
	if db, err := user.Lookup(c.Name); err == nil && db.HomeDir != c.DataDir {
		return errors.Join(ErrHome, errors.New("the home of "+c.Name+" is "+db.HomeDir))
	}
	_, err := p.run(ctx, u, args, nil, nil, nil)
	return err
}

// Start starts a container that was created.
func (p *Podman) Start(ctx context.Context, name string) error {
	u, err := ofName(name)
	if err != nil {
		return err
	}
	_, err = p.run(ctx, u, []string{"start", name}, nil, nil, nil)
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

// Remove takes the container away.
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

// Exec runs a command in a running container and answers with the number the
// command ended with.
func (p *Podman) Exec(ctx context.Context, e Exec) (int, error) {
	u, err := ofName(e.Name)
	if err != nil {
		return 0, err
	}
	if len(e.Command) == 0 {
		return 0, ErrCommand
	}
	args := []string{"exec"}
	if e.Stdin != nil {
		// Without input the command gets no open standard input: a channel
		// nobody writes to would hold the call open.
		args = append(args, "--interactive")
	}
	if e.EnvFile != "" {
		if err := validPath(e.EnvFile); err != nil {
			return 0, err
		}
		// The file is the only copy of what the call was given, and it goes as
		// soon as the call is over. Not earlier: podman reads it itself while
		// it starts, so taking it away right after the start is a race with
		// that read.
		defer os.Remove(e.EnvFile)
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

	code, err := p.run(ctx, u, args, e.Stdin, e.Stdout, e.Stderr)
	var ended *exec.ExitError
	if errors.As(err, &ended) {
		// The number belongs to the command, not to this engine. Podman uses
		// 125 to 127 for its own troubles and a service may end with those
		// same numbers, so the two cannot be told apart without reading what
		// podman wrote - which is the caller's to show, not ours to parse.
		return code, nil
	}
	return 0, err
}

// run starts podman with this argument list and nothing else: no shell, no
// string built out of anything. It answers with the number podman ended with,
// and an error whenever that number is not 0 - whether the number means the
// command failed or podman did is a question only Exec asks.
func (p *Podman) run(ctx context.Context, u appUser, args []string,
	in io.Reader, out, errOut io.Writer) (int, error) {
	attr, err := credential(u.uid, u.gid)
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
	err = cmd.Run()
	var ended *exec.ExitError
	if errors.As(err, &ended) {
		return ended.ExitCode(), err
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}
