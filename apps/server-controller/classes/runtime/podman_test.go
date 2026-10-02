package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// fake stands in for podman. Its program writes down everything a case here
// needs to see and nothing it has to guess: every argument on its own line, its
// whole environment, the identity it runs as, and whatever came in on its
// standard input. It always says something on standard error. Then it does what
// the files beside it ask of it - print something, end with 7, hang, or die of a
// signal.
//
// The environment is written with env, not as a handful of names a case already
// expects: a fifth variable has to show up here whatever it is called, and that
// is what "and nothing else" means. For the same reason the paths it writes to
// cannot travel in the environment the way a script usually gets such a thing -
// they are written into the program itself.
type fake struct {
	bin string
	dir string
}

func fakePodman(t *testing.T) *fake {
	t.Helper()
	if goruntime.GOOS != "linux" {
		t.Skip("the engine starts podman under the app's user with a fixed environment; both are Linux")
	}
	dir := t.TempDir()
	f := &fake{bin: filepath.Join(dir, "podman"), dir: dir}
	q := func(name string) string { return "'" + filepath.Join(dir, name) + "'" }
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\" >> " + q("args") + "; done\n" +
		"env > " + q("env") + "\n" +
		"printf 'uid=%s gid=%s\\n' \"$(id -u)\" \"$(id -g)\" > " + q("id") + "\n" +
		"cat > " + q("stdin") + "\n" +
		"printf 'podman said something\\n' >&2\n" +
		"if [ -f " + q("out") + " ]; then cat " + q("out") + "; fi\n" +
		"if [ -f " + q("sleep") + " ]; then sleep 30; fi\n" +
		"if [ -f " + q("signal") + " ]; then kill -TERM $$; sleep 5; fi\n" +
		"if [ -f " + q("fail") + "\"$1\" ]; then exit 7; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(f.bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	// Reachable and writable for whoever the engine switches to: a run as root
	// really switches, and then the app's user has to be able to run this and
	// write its records. Everything here lives in a directory of this test.
	for _, path := range []string{filepath.Dir(dir), dir, f.bin} {
		if err := os.Chmod(path, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	// A variable of this process that must not reach podman.
	t.Setenv("NGC_LEAK", "this must not reach podman")
	return f
}

// args are the arguments of every call so far, in order.
func (f *fake) args(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "args"))
	if err != nil {
		t.Fatalf("podman was not started: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// ran reports whether podman was started at all.
func (f *fake) ran() bool {
	_, err := os.Stat(filepath.Join(f.dir, "args"))
	return err == nil
}

// env is the whole environment the started program found.
func (f *fake) env(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "env"))
	if err != nil {
		t.Fatalf("podman was not started: %v", err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if l == "" {
			continue
		}
		k, v, _ := strings.Cut(l, "=")
		m[k] = v
	}
	return m
}

// who is the identity podman ran as.
func (f *fake) who(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "id"))
	if err != nil {
		t.Fatalf("podman was not started: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// gotOnStdin is what podman read on its standard input.
func (f *fake) gotOnStdin(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "stdin"))
	if err != nil {
		t.Fatalf("podman was not started: %v", err)
	}
	return string(b)
}

// prints makes the next calls write s on their standard output. Readable by
// anyone, like the other files that steer the fake: a run as root really switches
// user, and the fake then reads these as the app's user, not as the one that
// wrote them.
func (f *fake) prints(t *testing.T, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "out"), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// failsOn makes the calls whose first argument is sub exit with 7.
func (f *fake) failsOn(t *testing.T, sub string) { f.touch(t, "fail"+sub) }

// sleeps makes the next call hang until something ends it.
func (f *fake) sleeps(t *testing.T) { f.touch(t, "sleep") }

// diesOfASignal makes the next call end by a signal instead of by a status.
func (f *fake) diesOfASignal(t *testing.T) { f.touch(t, "signal") }

func (f *fake) touch(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// testApp is a system user of this machine that a case may give an app.
type testApp struct {
	name     string
	uid, gid uint32
	home     string
}

// anApp picks that user. It is never root, because the engine refuses uid or gid
// 0 for an app - so a case cannot simply take whoever runs the test:
//
//   - without root this process cannot switch user at all, so it has to be this
//     process's own user, and then nothing is switched;
//   - as root it has to be somebody else, and then the switch really happens and
//     the fake records which user it ran as.
//
// Neither uid nor gid appears in any argument, so the cases about argument lists
// measure the same thing either way.
func anApp(t *testing.T) testApp {
	t.Helper()
	if goruntime.GOOS != "linux" {
		// A system user with a numeric uid is a thing of Linux, and so is every
		// case that needs one. Said here as well as in fakePodman, because a case
		// may ask for the user before it asks for the fake.
		t.Skip("the system users of an app are Linux")
	}
	var u *user.User
	var err error
	if os.Geteuid() == 0 {
		// An unprivileged user every Debian and Ubuntu has.
		u, err = user.Lookup("nobody")
	} else {
		u, err = user.Current()
	}
	if err != nil {
		t.Skip("no user database here: " + err.Error())
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	if uid == 0 || gid == 0 {
		t.Skip("the only user this run could give an app is root: " + u.Username)
	}
	if !validName.MatchString(u.Username) {
		t.Skip("the name of that user is no container name: " + u.Username)
	}
	if err := validPath(u.HomeDir); err != nil {
		t.Skip("the home of that user is no data directory: " + u.HomeDir)
	}
	return testApp{name: u.Username, uid: uint32(uid), gid: uint32(gid), home: u.HomeDir}
}

func TestCreateBuildsTheArgumentList(t *testing.T) {
	f := fakePodman(t)
	app := anApp(t)
	uid, gid := app.uid, app.gid
	p := NewPodman(f.bin)
	err := p.Create(context.Background(), Container{
		Name: "ngc-terminal-1a2b3c4d", Image: "ngc/terminal:1.0.0",
		UID: uid, GID: gid, DataDir: "/var/lib/ngc/apps/org.sorglos.terminal",
		Hold: "/usr/lib/ngc/ngc-controller", CPU: 1000, Memory: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := f.args(t)
	want := []string{"create", "--name", "ngc-terminal-1a2b3c4d", "--network", "none",
		"--cpus", "1.000", "--memory", "256m", "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--volume", "/var/lib/ngc/apps/org.sorglos.terminal:/data:rw",
		"--volume", "/usr/lib/ngc/ngc-controller:/ngc/ngc-hold:ro",
		"--user", "0:0", "ngc/terminal:1.0.0", "/ngc/ngc-hold", "hold"}
	if !slices.Equal(args, want) {
		t.Fatalf("args =\n%q\nwant\n%q", args, want)
	}
}

// The limits of the manifest become the limits of the container, and an empty
// manifest field becomes the default of the contract - not "no limit".
func TestCreateTakesTheLimitsApartInThousandths(t *testing.T) {
	cases := []struct {
		cpu, mem         int
		wantCPU, wantMem string
	}{
		{0, 0, "1.000", "256m"},
		{250, 16, "0.250", "16m"},
		{1500, 512, "1.500", "512m"},
		{64000, 65536, "64.000", "65536m"},
	}
	for _, c := range cases {
		f := fakePodman(t)
		app := anApp(t)
		uid, gid := app.uid, app.gid
		p := NewPodman(f.bin)
		if err := p.Create(context.Background(), Container{
			Name: "ngc-a-00000000", Image: "ngc/a:1.0.0", UID: uid, GID: gid,
			DataDir: "/var/lib/ngc/apps/a", Hold: "/usr/lib/ngc/ngc-controller",
			CPU: c.cpu, Memory: c.mem,
		}); err != nil {
			t.Fatal(err)
		}
		args := f.args(t)
		i := slices.Index(args, "--cpus")
		j := slices.Index(args, "--memory")
		if i < 0 || j < 0 || args[i+1] != c.wantCPU || args[j+1] != c.wantMem {
			t.Fatalf("cpu %d memory %d gave %q, want --cpus %s --memory %s",
				c.cpu, c.mem, args, c.wantCPU, c.wantMem)
		}
	}
}

func TestCreateWithNetworkAndRoot(t *testing.T) {
	// Network: where "--network none" stood, nothing stands. Naming a way out
	// here would pin this server to a program that may not be on it, and then no
	// app with network would start at all - so the engine leaves that choice to
	// podman, which is the one party that knows what it has.
	//
	// Root: "--cap-drop ALL" is gone. The capabilities come back because the
	// admin granted this app store.NeedRoot, and those capabilities are what
	// that grant is - an app that was allowed root in its container and then
	// kept at no capabilities would have been allowed nothing. Nothing else is
	// given up for it: the container still runs in the user namespace of the
	// app's own user, so root in there is not root on this server, and
	// --read-only and no-new-privileges stay.
	f := fakePodman(t)
	app := anApp(t)
	uid, gid := app.uid, app.gid
	p := NewPodman(f.bin)
	if err := p.Create(context.Background(), Container{
		Name: "ngc-terminal-1a2b3c4d", Image: "ngc/terminal:1.0.0",
		UID: uid, GID: gid, DataDir: "/var/lib/ngc/apps/org.sorglos.terminal",
		Hold: "/usr/lib/ngc/ngc-controller", Network: true, Root: true,
	}); err != nil {
		t.Fatal(err)
	}
	args := f.args(t)
	if slices.Contains(args, "--network") {
		t.Fatalf("args = %q, names a way to the network - that choice is podman's", args)
	}
	for _, a := range args {
		if strings.Contains(a, "slirp") || strings.Contains(a, "pasta") ||
			strings.Contains(a, "netns") || a == "none" || a == "bridge" || a == "host" {
			t.Fatalf("args = %q, pins the network to %q", args, a)
		}
	}
	if slices.Contains(args, "--cap-drop") {
		t.Fatalf("args = %q, still drops the capabilities an app with root needs", args)
	}
	for _, keep := range []string{"--read-only", "--security-opt", "no-new-privileges"} {
		if !slices.Contains(args, keep) {
			t.Fatalf("args = %q, gave up %s as well", args, keep)
		}
	}
	if i := slices.Index(args, "--user"); i < 0 || args[i+1] != "0:0" {
		t.Fatalf("args = %q, want --user 0:0", args)
	}
}

// Each of the two grants changes exactly one thing in the list and nothing else.
// Written as a difference because a list copied by hand is where a silently
// given-up option hides: root takes away "--cap-drop ALL", the network takes away
// "--network none" and puts nothing in its place.
func TestEachGrantChangesOnlyItsOwnOption(t *testing.T) {
	list := func(bend func(c *Container)) []string {
		f := fakePodman(t)
		app := anApp(t)
		uid, gid := app.uid, app.gid
		c := Container{
			Name: "ngc-a-00000000", Image: "ngc/a:1.0.0", UID: uid, GID: gid,
			DataDir: "/var/lib/ngc/apps/a", Hold: "/usr/lib/ngc/ngc-controller",
		}
		bend(&c)
		if err := NewPodman(f.bin).Create(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		return f.args(t)
	}
	plain := list(func(*Container) {})
	for _, c := range []struct {
		what  string
		bend  func(c *Container)
		drops string
	}{
		{"root", func(c *Container) { c.Root = true }, "--cap-drop"},
		{"the network", func(c *Container) { c.Network = true }, "--network"},
	} {
		i := slices.Index(plain, c.drops)
		if i < 0 {
			t.Fatalf("args = %q, no %s without the grant", plain, c.drops)
		}
		want := slices.Concat(plain[:i], plain[i+2:])
		if got := list(c.bend); !slices.Equal(got, want) {
			t.Fatalf("with %s =\n%q\nwant\n%q", c.what, got, want)
		}
	}
}

// Every one of these would turn into something else in the argument list: a
// leading dash becomes an option of podman, and a colon in a path cuts the
// volume specification in another place, which mounts another directory.
func TestCreateRefusesWhatWouldChangeTheArgumentList(t *testing.T) {
	good := Container{
		Name: "ngc-a-00000000", Image: "ngc/a:1.0.0",
		DataDir: "/var/lib/ngc/apps/a", Hold: "/usr/lib/ngc/ngc-controller",
	}
	good.UID, good.GID = anApp(t).uid, anApp(t).gid
	cases := map[string]func(c *Container){
		"name with a dash in front":  func(c *Container) { c.Name = "-privileged" },
		"name with a colon":          func(c *Container) { c.Name = "a:b" },
		"empty name":                 func(c *Container) { c.Name = "" },
		"image with a dash in front": func(c *Container) { c.Image = "-v" },
		"image without a tag":        func(c *Container) { c.Image = "ngc/a" },
		"data directory relative":    func(c *Container) { c.DataDir = "apps/a" },
		"data directory with colon":  func(c *Container) { c.DataDir = "/var/lib/ngc/apps/a:/etc:ro" },
		"hold relative":              func(c *Container) { c.Hold = "ngc-controller" },
		"hold with colon":            func(c *Container) { c.Hold = "/usr/lib/ngc:x" },
		"hold not clean":             func(c *Container) { c.Hold = "/usr/lib/ngc/../../ngc-controller" },
		"negative cpu":               func(c *Container) { c.CPU = -1 },
		"negative memory":            func(c *Container) { c.Memory = -1 },
		// What is mounted into a container is what the container can read, and
		// the data directory is mounted writable: neither may be anywhere.
		"hold outside the program directory": func(c *Container) { c.Hold = "/etc/shadow" },
		"hold is the program directory":      func(c *Container) { c.Hold = config.BinDir },
		"data directory is the root":         func(c *Container) { c.DataDir = "/" },
		"data directory elsewhere":           func(c *Container) { c.DataDir = "/var/lib/ngc/staged" },
	}
	for name, bend := range cases {
		t.Run(name, func(t *testing.T) {
			f := fakePodman(t)
			c := good
			bend(&c)
			if err := NewPodman(f.bin).Create(context.Background(), c); err == nil {
				t.Fatalf("Create took it: %q", f.args(t))
			}
			if f.ran() {
				t.Fatalf("podman was started anyway: %q", f.args(t))
			}
		})
	}
}

// The environment is the whole environment, not an addition to the one this
// process has: whatever stands here stands in no log and in no child. The case
// reads what podman really got, with env, so a fifth variable cannot hide behind
// a list of names this case already expects - a service's parameters are the
// thing that must never travel this way.
func TestCreatePassesFourVariablesAndNothingElse(t *testing.T) {
	f := fakePodman(t)
	app := anApp(t)
	uid, gid := app.uid, app.gid
	p := NewPodman(f.bin)
	if err := p.Create(context.Background(), Container{
		Name: "ngc-terminal-1a2b3c4d", Image: "ngc/terminal:1.0.0", UID: uid, GID: gid,
		DataDir: "/var/lib/ngc/apps/org.sorglos.terminal", Hold: "/usr/lib/ngc/ngc-controller",
	}); err != nil {
		t.Fatal(err)
	}
	env := f.env(t)
	want := map[string]string{
		"HOME":            "/var/lib/ngc/apps/org.sorglos.terminal",
		"XDG_RUNTIME_DIR": "/run/ngc/ngc-terminal-1a2b3c4d",
		"PATH":            "/usr/bin:/bin",
		"LANG":            "C.UTF-8",
	}
	for k, v := range want {
		if env[k] != v {
			t.Fatalf("%s = %q, want %q", k, env[k], v)
		}
	}
	// Everything else that may stand there is a name the shell keeps for itself,
	// and which names those are is measured in this very run rather than guessed:
	// a guessed list is a wider gate than the measurement asks for, and a
	// secret could walk through it under one of the guessed names.
	own := shellOwnNames(t)
	for k, v := range env {
		if _, ok := want[k]; ok {
			continue
		}
		if _, ok := own[k]; ok {
			continue
		}
		t.Fatalf("podman also got %s=%q", k, v)
	}
}

// shellOwnNames is what this machine's /bin/sh puts into an environment of its
// own accord, measured by starting one with exactly the four the engine sets.
// Whatever a call shows beyond these four and these names came out of cmd.Env.
func shellOwnNames(t *testing.T) map[string]struct{} {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "show")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nenv\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script)
	cmd.Env = []string{"HOME=/h", "XDG_RUNTIME_DIR=/r", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	own := map[string]struct{}{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		k, _, _ := strings.Cut(l, "=")
		switch k {
		case "HOME", "XDG_RUNTIME_DIR", "PATH", "LANG", "":
			continue
		}
		own[k] = struct{}{}
	}
	return own
}

// Switching user needs root. A controller without root must not start the
// container as itself instead - that would be another user than the one the
// app's data belong to - so it refuses and starts nothing.
func TestWithoutRootNoContainerStartsForAnotherUser(t *testing.T) {
	f := fakePodman(t)
	if os.Geteuid() == 0 {
		t.Skip("this case is about a controller without root")
	}
	err := NewPodman(f.bin).Create(context.Background(), Container{
		Name: "ngc-terminal-1a2b3c4d", Image: "ngc/terminal:1.0.0",
		UID: 900, GID: 900, DataDir: "/var/lib/ngc/apps/org.sorglos.terminal",
		Hold: "/usr/lib/ngc/ngc-controller",
	})
	if !errors.Is(err, ErrNotRoot) {
		t.Fatalf("err = %v, want ErrNotRoot", err)
	}
	if f.ran() {
		t.Fatalf("podman was started anyway: %q", f.args(t))
	}
}

func TestExecNeverUsesAShell(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	cmd := []string{"/usr/bin/greet", `say "hallo welt"`, "-c"}
	code, err := NewPodman(f.bin).Exec(context.Background(), Exec{
		Name: name, Command: cmd, Stdin: strings.NewReader(""),
	})
	if err != nil || code != 0 {
		t.Fatalf("code = %d, err = %v", code, err)
	}
	args := f.args(t)
	// The argument with the space and the quotes arrived as one argument, and
	// the one that looks like an option of a shell arrived as an argument too.
	if !slices.Contains(args, `say "hallo welt"`) {
		t.Fatalf("args = %q, the command was taken apart", args)
	}
	for _, a := range args {
		if a == "sh" || a == "/bin/sh" || a == "bash" {
			t.Fatalf("args = %q, a shell runs here", args)
		}
	}
	// -c may stand in a command of a service, but never before the -- that
	// ends the options of podman.
	end := slices.Index(args, "--")
	if end < 0 {
		t.Fatalf("args = %q, nothing ends the options of podman", args)
	}
	if slices.Contains(args[:end], "-c") {
		t.Fatalf("args = %q, -c counts as an option of podman", args)
	}
	if !slices.Equal(args[end+1:], append([]string{name}, cmd...)) {
		t.Fatalf("args after -- = %q, want %q", args[end+1:], append([]string{name}, cmd...))
	}
}

// NGC_PARAMS of a call must stand in no command line and in no log, so the
// environment of the service travels in a file. The file is gone when the call
// is over: it is the only copy that was on the machine.
func TestExecTakesTheEnvironmentFromAFileAndRemovesIt(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	envFile := runDirFile(t, name)
	if _, err := NewPodman(f.bin).Exec(context.Background(), Exec{
		Name: name, Command: []string{"/usr/bin/greet"}, EnvFile: envFile,
	}); err != nil {
		t.Fatal(err)
	}
	args := f.args(t)
	i := slices.Index(args, "--env-file")
	if i < 0 || args[i+1] != envFile {
		t.Fatalf("args = %q, want --env-file %s", args, envFile)
	}
	// And it has to stand among the options of podman. Behind the -- podman would
	// not read it at all: the call would run without its environment, and the path
	// of the file with the parameters would stand in the command line of the
	// service - the one place this file exists to keep it out of.
	if end := slices.Index(args, "--"); end < 0 || i > end {
		t.Fatalf("args = %q, --env-file does not stand among the options of podman", args)
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("the file with the parameters is still there: %v", err)
	}
}

// runDirFile makes a file under config.RunDir that an Exec may name. The engine
// removes such a file as this process, which is root on a server, so it takes
// nothing outside that directory - and a case about the file therefore needs a
// directory it may not be able to make.
func runDirFile(t *testing.T, name string) string {
	t.Helper()
	dir := config.RunDir + "/" + name
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Skip("cannot make " + dir + ", so no case about that file can run here: " + err.Error())
	}
	path := filepath.Join(dir, "env")
	if err := os.WriteFile(path, []byte("NGC_PARAMS={}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	return path
}

// A file the engine cannot hand to the app's user is ErrOwner, and that is the
// point of the case: ErrIdentity would blame the app for something the rights
// of this process decided, and whoever reads the log at three in the morning
// would look at the wrong half of the machine.
func TestExecSaysWhenTheFileCannotBeHandedOver(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	// A well-formed path in the one directory the engine takes a file from,
	// which is not there. The handing over then fails for a reason that has
	// nothing to do with who the app is - the same shape as a controller
	// without root, and the only one of those shapes a case can make here.
	missing := config.RunDir + "/" + name + "-env-that-is-not-there"
	_, err := NewPodman(f.bin).Exec(context.Background(), Exec{
		Name: name, Command: []string{"/usr/bin/greet"}, EnvFile: missing,
	})
	if !errors.Is(err, ErrOwner) {
		t.Fatalf("err = %v, want ErrOwner", err)
	}
	if errors.Is(err, ErrIdentity) {
		t.Fatalf("err = %v: it blames the uid of the app, and the uid is right", err)
	}
	// And podman was never started: a call whose environment the service cannot
	// read must not run without it.
	if f.ran() {
		t.Fatalf("podman was started anyway: %q", f.args(t))
	}
}

func TestExecRefusesAnEnvironmentFileThatIsNoPath(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	// The last two are paths that exist and are perfectly well formed. They are
	// refused because they are not under config.RunDir, and the engine removes
	// what it is handed: a path from anywhere would make this call a way to
	// delete any file of this server.
	for _, bad := range []string{"-v", "env", "/run/ngc/../etc/shadow",
		filepath.Join(t.TempDir(), "env"), "/etc/shadow", config.RunDir} {
		if _, err := NewPodman(f.bin).Exec(context.Background(), Exec{
			Name: name, Command: []string{"/usr/bin/greet"}, EnvFile: bad,
		}); err == nil {
			t.Fatalf("EnvFile %q was taken", bad)
		}
		if f.ran() {
			t.Fatalf("podman was started anyway: %q", f.args(t))
		}
	}
}

// The number a service ends with belongs to the service, not to this engine:
// it travels back as the result, and an error says only that podman could not
// be run at all.
func TestExecReportsTheExitCodeOfTheService(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	f.failsOn(t, "exec")
	code, err := NewPodman(f.bin).Exec(context.Background(), Exec{
		Name: name, Command: []string{"/usr/bin/greet"},
	})
	if err != nil {
		t.Fatalf("err = %v, want the code alone", err)
	}
	if code != 7 {
		t.Fatalf("code = %d, want 7", code)
	}
}

// A call that was cut short did not end with a status, and must not look as if
// it had. Go answers -1 for a process a signal ended, and -1 is no number a
// command can end with: handing it back as the result would tell the caller
// "the service decided to end with -1" about a service that was killed.
func TestExecDoesNotTurnACutShortCallIntoAStatus(t *testing.T) {
	name := anApp(t).name

	// The context runs out while podman is still running - the ordinary way a
	// service hits its time limit.
	f := fakePodman(t)
	f.sleeps(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	code, err := NewPodman(f.bin).Exec(ctx, Exec{Name: name, Command: []string{"/usr/bin/greet"}})
	if err == nil {
		t.Fatalf("code = %d and no error for a call that was cut short", code)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0 - there is no status to report", code)
	}
	if !errors.Is(err, ErrKilled) {
		t.Fatalf("err = %v, want ErrKilled", err)
	}
	// And the cause is named, not only the fact: the caller has to tell a time
	// limit from a service that someone else killed.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's own cause joined to it", err)
	}

	// The other form of a context that ended: cancelled by hand while podman runs.
	// It has to answer the same way and name its own cause, which is a different
	// one.
	f = fakePodman(t)
	f.sleeps(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel2()
	}()
	code, err = NewPodman(f.bin).Exec(ctx2, Exec{Name: name, Command: []string{"/usr/bin/greet"}})
	cancel2()
	if code != 0 || !errors.Is(err, ErrKilled) {
		t.Fatalf("code = %d, err = %v, want 0 and ErrKilled", code, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled joined to it", err)
	}

	// A signal that has nothing to do with the context: same answer, without a
	// cause from the context.
	f = fakePodman(t)
	f.diesOfASignal(t)
	code, err = NewPodman(f.bin).Exec(context.Background(), Exec{
		Name: name, Command: []string{"/usr/bin/greet"},
	})
	if code != 0 || !errors.Is(err, ErrKilled) {
		t.Fatalf("code = %d, err = %v, want 0 and ErrKilled", code, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, blames the context for a signal of someone else", err)
	}
}

// The other side of that line: an engine that could not be run at all says so
// without ErrKilled. If everything that goes wrong carried ErrKilled, the caller
// could no longer tell a time limit from a podman that is not installed, and it
// would answer a broken server with "the service was cut short".
func TestAnEngineThatCannotRunSaysSoWithoutErrKilled(t *testing.T) {
	name := anApp(t).name
	gone := filepath.Join(t.TempDir(), "no-podman-here")
	code, err := NewPodman(gone).Exec(context.Background(), Exec{
		Name: name, Command: []string{"/usr/bin/greet"},
	})
	if err == nil {
		t.Fatalf("code = %d and no error for a podman that is not there", code)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if errors.Is(err, ErrKilled) {
		t.Fatalf("err = %v, says the command was cut short - it never ran", err)
	}
}

// The same promise over a race instead of over one arranged moment. Go's Cmd.Wait
// prefers the error of the process and reports the one of the context only when
// the process had none, so a command that ends with 0 in the very moment the
// context runs out arrives as the plain context error and not as an ExitError -
// measured at 6 of 400 before this was held. Whatever mix of the three outcomes a
// run produces, each one has to be either a status or an error that says
// ErrKilled; a number must never come out of a call the context ended.
func TestNoCancelledCallComesBackWithoutErrKilled(t *testing.T) {
	name := anApp(t).name
	p := NewPodman(fakePodman(t).bin)
	kinds := map[string]int{}
	for i := range 200 {
		// A limit around the time the fake needs, so that all three outcomes
		// really occur instead of only the easy one.
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(1500+i*80)*time.Microsecond)
		code, err := p.Exec(ctx, Exec{Name: name, Command: []string{"/usr/bin/greet"}})
		cancel()
		switch {
		case err == nil:
			if code != 0 {
				t.Fatalf("code = %d without an error", code)
			}
			kinds["a status"]++
		case errors.Is(err, ErrKilled):
			if code != 0 {
				t.Fatalf("code = %d with ErrKilled - that is no status of a command", code)
			}
			kinds["ErrKilled"]++
		default:
			t.Fatalf("round %d: code = %d, err = %v - a call the context ended, "+
				"and the caller cannot tell: no ErrKilled", i, code, err)
		}
	}
	t.Logf("outcomes: %v", kinds)
}

func TestExecWithStdinAndWithout(t *testing.T) {
	for _, in := range []bool{true, false} {
		f := fakePodman(t)
		name := anApp(t).name
		e := Exec{Name: name, Command: []string{"/usr/bin/greet"}}
		if in {
			e.Stdin = strings.NewReader("")
		}
		if _, err := NewPodman(f.bin).Exec(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		// Without input the service has no open standard input in the
		// container; asking for one anyway holds a channel open that nobody
		// writes to.
		if got := slices.Contains(f.args(t), "--interactive"); got != in {
			t.Fatalf("stdin = %v gave --interactive = %v", in, got)
		}
	}
}

// All three ends of a call are wired, not only the one that is easy to see: what
// the shell sends arrives at the service, what the service answers arrives back,
// and what it says about itself arrives separately. A service whose standard
// error went nowhere would fail with nothing to show.
func TestExecCarriesAllThreeEndsThrough(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	f.prints(t, "hallo")
	var out, errOut bytes.Buffer
	if _, err := NewPodman(f.bin).Exec(context.Background(), Exec{
		Name: name, Command: []string{"/usr/bin/greet"},
		Stdin: strings.NewReader("was der Dienst lesen soll"), Stdout: &out, Stderr: &errOut,
	}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hallo" {
		t.Fatalf("out = %q, want %q", out.String(), "hallo")
	}
	if errOut.String() != "podman said something\n" {
		t.Fatalf("stderr = %q, want what podman said", errOut.String())
	}
	if got := f.gotOnStdin(t); got != "was der Dienst lesen soll" {
		t.Fatalf("podman read %q on its standard input", got)
	}
}

// Root names root in the container and changes nothing else. In particular it
// never hands out --privileged: whether the capabilities are there was decided
// once, at Create, and a call must not be able to widen that.
func TestExecWithRootNamesTheUserAndNothingElse(t *testing.T) {
	list := func(root bool) []string {
		f := fakePodman(t)
		if _, err := NewPodman(f.bin).Exec(context.Background(), Exec{
			Name: anApp(t).name, Command: []string{"/usr/bin/greet"}, Root: root,
		}); err != nil {
			t.Fatal(err)
		}
		return f.args(t)
	}
	plain, root := list(false), list(true)
	for _, a := range root {
		if strings.Contains(a, "privileged") || strings.Contains(a, "cap-add") ||
			strings.Contains(a, "security-opt") {
			t.Fatalf("args = %q, this call widens what the container may do", root)
		}
	}
	i := slices.Index(root, "--")
	if i < 0 {
		t.Fatalf("args = %q, nothing ends the options of podman", root)
	}
	// The difference between the two lists is exactly those two arguments, and
	// they stand among the options of podman, not in the command.
	want := slices.Concat(plain[:1], []string{"--user", "0:0"}, plain[1:])
	if !slices.Equal(root, want) {
		t.Fatalf("with root =\n%q\nwant\n%q", root, want)
	}
}

func TestStartAndRemoveBuildTheirArgumentList(t *testing.T) {
	name := anApp(t).name
	cases := []struct {
		call func(p *Podman) error
		want []string
	}{
		{func(p *Podman) error { return p.Start(context.Background(), name) }, []string{"start", name}},
		{func(p *Podman) error { return p.Remove(context.Background(), name) }, []string{"rm", name}},
	}
	for _, c := range cases {
		f := fakePodman(t)
		if err := c.call(NewPodman(f.bin)); err != nil {
			t.Fatal(err)
		}
		if args := f.args(t); !slices.Equal(args, c.want) {
			t.Fatalf("args = %q, want %q", args, c.want)
		}
	}
}

// Stop gives the service ten seconds, and only when that fails does it kill
// the container. A stop that worked must not be followed by a kill: that would
// take the ten seconds away again.
func TestStopKillsOnlyWhenStoppingFailed(t *testing.T) {
	name := anApp(t).name

	f := fakePodman(t)
	if err := NewPodman(f.bin).Stop(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	if args := f.args(t); !slices.Equal(args, []string{"stop", "--time", "10", name}) {
		t.Fatalf("args = %q, want stop --time 10 %s", args, name)
	}

	f = fakePodman(t)
	f.failsOn(t, "stop")
	if err := NewPodman(f.bin).Stop(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	want := []string{"stop", "--time", "10", name, "kill", name}
	if args := f.args(t); !slices.Equal(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}

	f = fakePodman(t)
	f.failsOn(t, "stop")
	f.failsOn(t, "kill")
	if err := NewPodman(f.bin).Stop(context.Background(), name); err == nil {
		t.Fatal("a container that neither stops nor dies counts as stopped")
	}
}

// Running answers from the emptiness of the output, so no format of podman is
// parsed. The filter is anchored: a container whose name begins with this one
// must not answer for it.
func TestRunningReadsOnlyWhetherAnythingCameBack(t *testing.T) {
	name := anApp(t).name

	f := fakePodman(t)
	f.prints(t, "")
	ok, err := NewPodman(f.bin).Running(context.Background(), name)
	if err != nil || ok {
		t.Fatalf("empty output gave %v, %v", ok, err)
	}
	want := []string{"ps", "--quiet", "--no-trunc", "--filter", "name=^" + name + "$",
		"--filter", "status=running"}
	if args := f.args(t); !slices.Equal(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}

	f = fakePodman(t)
	f.prints(t, "5f0e1c\n")
	ok, err = NewPodman(f.bin).Running(context.Background(), name)
	if err != nil || !ok {
		t.Fatalf("an answer gave %v, %v", ok, err)
	}

	f = fakePodman(t)
	f.prints(t, "   \n\n")
	ok, err = NewPodman(f.bin).Running(context.Background(), name)
	if err != nil || ok {
		t.Fatalf("blank output gave %v, %v", ok, err)
	}

	f = fakePodman(t)
	f.failsOn(t, "ps")
	if _, err := NewPodman(f.bin).Running(context.Background(), name); err == nil {
		t.Fatal("podman failed and the answer was still an answer")
	}
}

func TestLoadImageAnswersWithTheTagOfTheTar(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	tarPath := filepath.Join(t.TempDir(), "amd64.tar")
	writeTar(t, tarPath, archive(`[{"Config":"c.json","RepoTags":["ngc/terminal:1.0.0"],"Layers":["l/layer.tar"]}]`))
	ref, err := NewPodman(f.bin).LoadImage(context.Background(), name, tarPath)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "ngc/terminal:1.0.0" {
		t.Fatalf("ref = %q", ref)
	}
	want := []string{"load", "--input", tarPath}
	if args := f.args(t); !slices.Equal(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
}

// Whose image store an image lands in is the whole reason the app stands in this
// signature: every app has its own, and one store that all of them read would
// let every app see the image of every other. HOME is that store for rootless
// podman, so this is where it is measured - it has to be the home of the app's
// user, never a directory of this controller.
func TestLoadImageUsesTheStoreOfTheApp(t *testing.T) {
	f := fakePodman(t)
	app := anApp(t)
	name := app.name
	tarPath := filepath.Join(t.TempDir(), "amd64.tar")
	writeTar(t, tarPath, archive(`[{"RepoTags":["ngc/terminal:1.0.0"]}]`))
	if _, err := NewPodman(f.bin).LoadImage(context.Background(), name, tarPath); err != nil {
		t.Fatal(err)
	}
	env := f.env(t)
	if env["HOME"] != app.home {
		t.Fatalf("HOME = %q, want the home of %s, %q", env["HOME"], name, app.home)
	}
	if env["HOME"] == config.DataDir || env["XDG_RUNTIME_DIR"] == config.RunDir {
		t.Fatalf("the image went into the store of this controller: HOME=%q XDG_RUNTIME_DIR=%q",
			env["HOME"], env["XDG_RUNTIME_DIR"])
	}
	if env["XDG_RUNTIME_DIR"] != config.RunDir+"/"+name {
		t.Fatalf("XDG_RUNTIME_DIR = %q, want %q", env["XDG_RUNTIME_DIR"], config.RunDir+"/"+name)
	}

	// The same for taking it away again.
	f = fakePodman(t)
	if err := NewPodman(f.bin).RemoveImage(context.Background(), name, "ngc/terminal:1.0.0"); err != nil {
		t.Fatal(err)
	}
	if env = f.env(t); env["HOME"] != app.home || env["XDG_RUNTIME_DIR"] != config.RunDir+"/"+name {
		t.Fatalf("rmi ran with HOME=%q XDG_RUNTIME_DIR=%q", env["HOME"], env["XDG_RUNTIME_DIR"])
	}
}

// A tar whose tag this engine will not name is not loaded either: the name
// under which podman would keep the image would then be one nobody here knows.
func TestLoadImageRefusesATarWithoutOneTag(t *testing.T) {
	f := fakePodman(t)
	tarPath := filepath.Join(t.TempDir(), "amd64.tar")
	writeTar(t, tarPath, archive(`[{"RepoTags":["a:1","b:2"]}]`))
	if _, err := NewPodman(f.bin).LoadImage(context.Background(), anApp(t).name, tarPath); err == nil {
		t.Fatal("the tar was loaded")
	}
	if f.ran() {
		t.Fatalf("podman was started anyway: %q", f.args(t))
	}
}

func TestRemoveImageBuildsTheArgumentListAndRefusesNonsense(t *testing.T) {
	f := fakePodman(t)
	if err := NewPodman(f.bin).RemoveImage(context.Background(), anApp(t).name, "ngc/terminal:1.0.0"); err != nil {
		t.Fatal(err)
	}
	if args := f.args(t); !slices.Equal(args, []string{"rmi", "ngc/terminal:1.0.0"}) {
		t.Fatalf("args = %q", args)
	}

	for _, bad := range []string{"-a", "ngc/terminal", "", "ngc/terminal:1.0.0 --all"} {
		f = fakePodman(t)
		if err := NewPodman(f.bin).RemoveImage(context.Background(), anApp(t).name, bad); err == nil {
			t.Fatalf("ref %q was taken", bad)
		}
		if f.ran() {
			t.Fatalf("podman was started anyway: %q", f.args(t))
		}
	}
}

// Podman is the engine the rest of the controller talks to, so that LXC can sit
// beside it later without a caller changing. This is a question for the compiler,
// not for a run: a test that asks a pointer to a literal whether it is nil asks
// nothing.
var _ Engine = (*Podman)(nil)

// A call whose context is already over starts nothing at all.
func TestACancelledCallStartsNothing(t *testing.T) {
	f := fakePodman(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewPodman(f.bin).Start(ctx, anApp(t).name); err == nil {
		t.Fatal("the call ran anyway")
	}
	if f.ran() {
		t.Fatalf("podman was started anyway: %q", f.args(t))
	}
}

// Create is the one call that sees the data directory and the system user at
// the same time, so it is where the two are held together. Every call after it
// takes the home directory out of the user database; a user whose home lies
// somewhere else would send those calls to another store of podman than the one
// the container was made in.
func TestCreateRefusesAUserWhoseHomeIsNotTheDataDirectory(t *testing.T) {
	f := fakePodman(t)
	app := anApp(t)
	c := Container{Name: app.name, Image: "ngc/a:1.0.0", UID: app.uid, GID: app.gid,
		DataDir: app.home, Hold: config.BinDir + "/ngc-controller"}
	if err := NewPodman(f.bin).Create(context.Background(), c); err != nil {
		t.Fatalf("a user whose home is its data directory was refused: %v", err)
	}

	f = fakePodman(t)
	c.DataDir = "/var/lib/ngc/apps/org.sorglos.terminal"
	if err := NewPodman(f.bin).Create(context.Background(), c); !errors.Is(err, ErrHome) {
		t.Fatalf("err = %v, want ErrHome", err)
	}
	if f.ran() {
		t.Fatalf("podman was started anyway: %q", f.args(t))
	}
}

// Create is the only call that does not read the user out of the name, so it is
// the last way past that separation: a uid of another app would make podman
// create the container as that other user, while every later call looked for it
// as this one.
func TestCreateRefusesAUidOrGidThatIsNotTheUsers(t *testing.T) {
	app := anApp(t)
	for _, bend := range []func(c *Container){
		func(c *Container) { c.UID = app.uid + 1 },
		func(c *Container) { c.GID = app.gid + 1 },
	} {
		f := fakePodman(t)
		c := Container{Name: app.name, Image: "ngc/a:1.0.0", UID: app.uid, GID: app.gid,
			DataDir: app.home, Hold: config.BinDir + "/ngc-controller"}
		bend(&c)
		if err := NewPodman(f.bin).Create(context.Background(), c); !errors.Is(err, ErrIdentity) {
			t.Fatalf("err = %v, want ErrIdentity", err)
		}
		if f.ran() {
			t.Fatalf("podman was started anyway: %q", f.args(t))
		}
	}
}

// A container of an app is nobody's root. Without this line the one thing this
// package is for falls away in a single call: podman would run as root, and an
// app writing into /data would write into root's own directory. Measured in both
// directions - the name of the root user is refused as well, from either side,
// because the calls after Create read the user back out of the database.
func TestNoContainerOfAnAppIsRoot(t *testing.T) {
	app := anApp(t)
	good := Container{Name: app.name, Image: "ngc/a:1.0.0", UID: app.uid, GID: app.gid,
		DataDir: app.home, Hold: config.BinDir + "/ngc-controller"}
	cases := map[string]func(c *Container){
		"uid 0":  func(c *Container) { c.UID = 0 },
		"gid 0":  func(c *Container) { c.GID = 0 },
		"both 0": func(c *Container) { c.UID, c.GID = 0, 0 },
		"the root user": func(c *Container) {
			c.Name, c.UID, c.GID, c.DataDir = "root", 0, 0, "/root"
		},
	}
	for name, bend := range cases {
		t.Run(name, func(t *testing.T) {
			f := fakePodman(t)
			c := good
			bend(&c)
			if err := NewPodman(f.bin).Create(context.Background(), c); !errors.Is(err, ErrIdentity) {
				t.Fatalf("err = %v, want ErrIdentity", err)
			}
			if f.ran() {
				t.Fatalf("podman was started anyway: %q", f.args(t))
			}
		})
	}
	// And from the other side: a container whose name belongs to root reaches no
	// podman either, whichever call asks.
	if _, err := user.Lookup("root"); err != nil {
		t.Skip("there is no user root here: " + err.Error())
	}
	calls := map[string]func(p *Podman) error{
		"Start":  func(p *Podman) error { return p.Start(context.Background(), "root") },
		"Stop":   func(p *Podman) error { return p.Stop(context.Background(), "root") },
		"Remove": func(p *Podman) error { return p.Remove(context.Background(), "root") },
		"Running": func(p *Podman) error {
			_, err := p.Running(context.Background(), "root")
			return err
		},
		"Exec": func(p *Podman) error {
			_, err := p.Exec(context.Background(), Exec{Name: "root", Command: []string{"/usr/bin/greet"}})
			return err
		},
		"RemoveImage": func(p *Podman) error {
			return p.RemoveImage(context.Background(), "root", "ngc/a:1.0.0")
		},
	}
	for name, call := range calls {
		t.Run("the root user through "+name, func(t *testing.T) {
			f := fakePodman(t)
			if err := call(NewPodman(f.bin)); !errors.Is(err, ErrIdentity) {
				t.Fatalf("err = %v, want ErrIdentity", err)
			}
			if f.ran() {
				t.Fatalf("podman was started anyway: %q", f.args(t))
			}
		})
	}
}

// The user switch itself - the thing the whole separation rests on, and the one
// thing a run without root cannot show: without root the engine refuses before
// it starts anything, which is itself the measurement for that case. With root
// the container really is created as the app's user, and the fake says so.
func TestPodmanRunsUnderTheUserOfTheApp(t *testing.T) {
	f := fakePodman(t)
	// An unprivileged user of this machine that is not the one running the test.
	// Its home is where its data directory would be, which is what Create wants.
	const app = "nobody"
	db, err := user.Lookup(app)
	if err != nil {
		t.Skip("no user " + app + " here: " + err.Error())
	}
	uid, err := strconv.ParseUint(db.Uid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.ParseUint(db.Gid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	if uid == uint64(os.Getuid()) {
		t.Skip("this test runs as " + app + " itself, so there is no switch to see")
	}
	if err := validPath(db.HomeDir); err != nil {
		t.Skip("the home of " + app + " is no data directory: " + db.HomeDir)
	}
	c := Container{Name: app, Image: "ngc/a:1.0.0", UID: uint32(uid), GID: uint32(gid),
		DataDir: db.HomeDir, Hold: config.BinDir + "/ngc-controller"}

	if os.Geteuid() != 0 {
		// Everything Create can check agreed - otherwise this would be ErrHome
		// or ErrIdentity - and it got as far as the switch, which needs root.
		if err := NewPodman(f.bin).Create(context.Background(), c); !errors.Is(err, ErrNotRoot) {
			t.Fatalf("err = %v, want ErrNotRoot", err)
		}
		if f.ran() {
			t.Fatalf("podman was started anyway: %q", f.args(t))
		}
		return
	}
	// As root the switch happens, so the fake has to be reachable and writable
	// for a user that is not the one who made it.
	for _, path := range []string{filepath.Dir(f.dir), f.dir, f.bin} {
		if err := os.Chmod(path, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	if err := NewPodman(f.bin).Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if got, want := f.who(t), "uid="+db.Uid+" gid="+db.Gid; got != want {
		t.Fatalf("podman ran as %q, want %q", got, want)
	}
}

// A name that belongs to no system user reaches no podman: which user the call
// would have to run as is exactly what that name answers, so without it there
// is nothing to run.
func TestTheCallsAfterCreateNeedTheUserOfTheirName(t *testing.T) {
	// One fake per subtest: with a shared one, a call that did start podman would
	// colour every later subtest red as well, and which of them held the promise
	// would not be readable from the result.
	gone := "ngc-nobody-00000000"
	if _, err := user.Lookup(gone); err == nil {
		t.Skip("there is a user called " + gone + " on this machine")
	}
	calls := map[string]func(p *Podman) error{
		"Start":  func(p *Podman) error { return p.Start(context.Background(), gone) },
		"Stop":   func(p *Podman) error { return p.Stop(context.Background(), gone) },
		"Remove": func(p *Podman) error { return p.Remove(context.Background(), gone) },
		"Running": func(p *Podman) error {
			_, err := p.Running(context.Background(), gone)
			return err
		},
		"Exec": func(p *Podman) error {
			_, err := p.Exec(context.Background(), Exec{Name: gone, Command: []string{"/usr/bin/greet"}})
			return err
		},
		"LoadImage": func(p *Podman) error {
			_, err := p.LoadImage(context.Background(), gone, "/var/lib/ngc/staged/amd64.tar")
			return err
		},
		"RemoveImage": func(p *Podman) error {
			return p.RemoveImage(context.Background(), gone, "ngc/terminal:1.0.0")
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			f := fakePodman(t)
			if err := call(NewPodman(f.bin)); !errors.Is(err, ErrUser) {
				t.Fatalf("err = %v, want ErrUser", err)
			}
			if f.ran() {
				t.Fatalf("podman was started anyway: %q", f.args(t))
			}
		})
	}
}

// A call without a command reaches no podman. Podman would refuse it too, but
// one line later and with a message of its own, and a service that was started
// with nothing to run is a mistake of this controller, not of the app.
func TestExecRefusesACallWithoutACommand(t *testing.T) {
	f := fakePodman(t)
	name := anApp(t).name
	for _, cmd := range [][]string{nil, {}} {
		if _, err := NewPodman(f.bin).Exec(context.Background(), Exec{Name: name, Command: cmd}); !errors.Is(err, ErrCommand) {
			t.Fatalf("err = %v, want ErrCommand", err)
		}
		if f.ran() {
			t.Fatalf("podman was started anyway: %q", f.args(t))
		}
	}
}
