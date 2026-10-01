package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// fake stands in for podman. Its program appends every argument on its own
// line, writes down the four variables the environment is meant to hold plus
// one that must not have come through, prints what a test asked it to print
// and fails for the subcommand a test asked it to fail for.
//
// The path it records into cannot travel in the environment the way a script
// usually gets such a thing: the engine starts podman with a fixed environment
// of four variables, so the path is written into the program itself. That the
// environment really is only those four is what the leak line measures.
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
		"printf 'HOME=%s\\nXDG_RUNTIME_DIR=%s\\nPATH=%s\\nLANG=%s\\nLEAK=%s\\n'" +
		" \"$HOME\" \"$XDG_RUNTIME_DIR\" \"$PATH\" \"$LANG\" \"$NGC_LEAK\" > " + q("env") + "\n" +
		"if [ -f " + q("out") + " ]; then cat " + q("out") + "; fi\n" +
		"if [ -f " + q("fail") + "\"$1\" ]; then exit 7; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(f.bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
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

// env is what the started program found in its environment.
func (f *fake) env(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "env"))
	if err != nil {
		t.Fatalf("podman was not started: %v", err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		k, v, _ := strings.Cut(l, "=")
		m[k] = v
	}
	return m
}

// prints makes the next calls write s on their standard output.
func (f *fake) prints(t *testing.T, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "out"), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// failsOn makes the calls whose first argument is sub exit with 7.
func (f *fake) failsOn(t *testing.T, sub string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "fail"+sub), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// me is the user this test runs as. The engine switches user only when it has
// to, so a container of this user needs no root and the test measures the
// argument list alone - which is what these cases are about. Neither UID nor
// GID appears in any argument.
func me() (uint32, uint32) { return uint32(os.Getuid()), uint32(os.Getgid()) }

// meName is the name of this user, used where a case needs a container name
// that the engine can look up in the user database.
func meName(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skip("no user database here:", err)
	}
	return u.Username
}

func TestCreateBuildsTheArgumentList(t *testing.T) {
	f := fakePodman(t)
	uid, gid := me()
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
		uid, gid := me()
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
	// Network: "--network slirp4netns" stands where "--network none" stood.
	//
	// Root: "--cap-drop ALL" is gone. The capabilities come back because the
	// admin granted this app store.NeedRoot, and those capabilities are what
	// that grant is - an app that was allowed root in its container and then
	// kept at no capabilities would have been allowed nothing. Nothing else is
	// given up for it: the container still runs in the user namespace of the
	// app's own user, so root in there is not root on this server, and
	// --read-only and no-new-privileges stay.
	f := fakePodman(t)
	uid, gid := me()
	p := NewPodman(f.bin)
	if err := p.Create(context.Background(), Container{
		Name: "ngc-terminal-1a2b3c4d", Image: "ngc/terminal:1.0.0",
		UID: uid, GID: gid, DataDir: "/var/lib/ngc/apps/org.sorglos.terminal",
		Hold: "/usr/lib/ngc/ngc-controller", Network: true, Root: true,
	}); err != nil {
		t.Fatal(err)
	}
	args := f.args(t)
	if i := slices.Index(args, "--network"); i < 0 || args[i+1] != "slirp4netns" {
		t.Fatalf("args = %q, want --network slirp4netns", args)
	}
	if slices.Contains(args, "none") {
		t.Fatalf("args = %q, still refuses the network", args)
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

// Without root the capabilities go, with root they come back, and nothing else
// differs between the two lists. Written as a difference because a list copied
// by hand is where a silently given-up option hides.
func TestRootChangesOnlyTheCapabilities(t *testing.T) {
	list := func(root bool) []string {
		f := fakePodman(t)
		uid, gid := me()
		p := NewPodman(f.bin)
		if err := p.Create(context.Background(), Container{
			Name: "ngc-a-00000000", Image: "ngc/a:1.0.0", UID: uid, GID: gid,
			DataDir: "/var/lib/ngc/apps/a", Hold: "/usr/lib/ngc/ngc-controller",
			Root: root,
		}); err != nil {
			t.Fatal(err)
		}
		return f.args(t)
	}
	plain, root := list(false), list(true)
	i := slices.Index(plain, "--cap-drop")
	if i < 0 {
		t.Fatalf("args = %q, no --cap-drop without root", plain)
	}
	want := slices.Concat(plain[:i], plain[i+2:])
	if !slices.Equal(root, want) {
		t.Fatalf("with root =\n%q\nwant\n%q", root, want)
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
	good.UID, good.GID = me()
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
// process has: whatever stands here stands in no log and in no child.
func TestCreatePassesFourVariablesAndNothingElse(t *testing.T) {
	f := fakePodman(t)
	uid, gid := me()
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
		"LEAK":            "",
	}
	for k, v := range want {
		if env[k] != v {
			t.Fatalf("%s = %q, want %q", k, env[k], v)
		}
	}
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
	name := meName(t)
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
	name := meName(t)
	envFile := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(envFile, []byte("NGC_PARAMS={}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("the file with the parameters is still there: %v", err)
	}
}

func TestExecRefusesAnEnvironmentFileThatIsNoPath(t *testing.T) {
	f := fakePodman(t)
	name := meName(t)
	for _, bad := range []string{"-v", "env", "/run/ngc/../etc/shadow"} {
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
	name := meName(t)
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

func TestExecWithStdinAndWithout(t *testing.T) {
	for _, in := range []bool{true, false} {
		f := fakePodman(t)
		name := meName(t)
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

func TestExecCarriesTheOutputThrough(t *testing.T) {
	f := fakePodman(t)
	name := meName(t)
	f.prints(t, "hallo")
	var out bytes.Buffer
	if _, err := NewPodman(f.bin).Exec(context.Background(), Exec{
		Name: name, Command: []string{"/usr/bin/greet"}, Stdout: &out,
	}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hallo" {
		t.Fatalf("out = %q, want %q", out.String(), "hallo")
	}
}

func TestStartAndRemoveBuildTheirArgumentList(t *testing.T) {
	name := meName(t)
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
	name := meName(t)

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
	name := meName(t)

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
	name := meName(t)
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
	name := meName(t)
	u, err := user.Current()
	if err != nil {
		t.Skip("no user database here: " + err.Error())
	}
	tarPath := filepath.Join(t.TempDir(), "amd64.tar")
	writeTar(t, tarPath, archive(`[{"RepoTags":["ngc/terminal:1.0.0"]}]`))
	if _, err := NewPodman(f.bin).LoadImage(context.Background(), name, tarPath); err != nil {
		t.Fatal(err)
	}
	env := f.env(t)
	if env["HOME"] != u.HomeDir {
		t.Fatalf("HOME = %q, want the home of %s, %q", env["HOME"], name, u.HomeDir)
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
	if env = f.env(t); env["HOME"] != u.HomeDir || env["XDG_RUNTIME_DIR"] != config.RunDir+"/"+name {
		t.Fatalf("rmi ran with HOME=%q XDG_RUNTIME_DIR=%q", env["HOME"], env["XDG_RUNTIME_DIR"])
	}
}

// A tar whose tag this engine will not name is not loaded either: the name
// under which podman would keep the image would then be one nobody here knows.
func TestLoadImageRefusesATarWithoutOneTag(t *testing.T) {
	f := fakePodman(t)
	tarPath := filepath.Join(t.TempDir(), "amd64.tar")
	writeTar(t, tarPath, archive(`[{"RepoTags":["a:1","b:2"]}]`))
	if _, err := NewPodman(f.bin).LoadImage(context.Background(), meName(t), tarPath); err == nil {
		t.Fatal("the tar was loaded")
	}
	if f.ran() {
		t.Fatalf("podman was started anyway: %q", f.args(t))
	}
}

func TestRemoveImageBuildsTheArgumentListAndRefusesNonsense(t *testing.T) {
	f := fakePodman(t)
	if err := NewPodman(f.bin).RemoveImage(context.Background(), meName(t), "ngc/terminal:1.0.0"); err != nil {
		t.Fatal(err)
	}
	if args := f.args(t); !slices.Equal(args, []string{"rmi", "ngc/terminal:1.0.0"}) {
		t.Fatalf("args = %q", args)
	}

	for _, bad := range []string{"-a", "ngc/terminal", "", "ngc/terminal:1.0.0 --all"} {
		f = fakePodman(t)
		if err := NewPodman(f.bin).RemoveImage(context.Background(), meName(t), bad); err == nil {
			t.Fatalf("ref %q was taken", bad)
		}
		if f.ran() {
			t.Fatalf("podman was started anyway: %q", f.args(t))
		}
	}
}

// The engine is the one interface the rest of the controller talks to, so that
// LXC can sit beside podman later without a caller changing.
func TestPodmanIsAnEngine(t *testing.T) {
	var e Engine = NewPodman("/usr/bin/podman")
	if e == nil {
		t.Fatal("no engine")
	}
}

// A call that was cancelled starts nothing and ends with the cause of the
// cancellation, not with a result of podman.
func TestACancelledCallStartsNothing(t *testing.T) {
	f := fakePodman(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewPodman(f.bin).Start(ctx, meName(t)); err == nil {
		t.Fatal("the call ran anyway")
	}
}

// Create is the one call that sees the data directory and the system user at
// the same time, so it is where the two are held together. Every call after it
// takes the home directory out of the user database; a user whose home lies
// somewhere else would send those calls to another store of podman than the one
// the container was made in.
func TestCreateRefusesAUserWhoseHomeIsNotTheDataDirectory(t *testing.T) {
	f := fakePodman(t)
	name := meName(t)
	if !validName.MatchString(name) {
		t.Skip("the name of this user is no container name: " + name)
	}
	u, err := user.Current()
	if err != nil {
		t.Skip("no user database here: " + err.Error())
	}
	uid, gid := me()
	c := Container{Name: name, Image: "ngc/a:1.0.0", UID: uid, GID: gid,
		DataDir: u.HomeDir, Hold: "/usr/lib/ngc/ngc-controller"}
	if err := validPath(c.DataDir); err != nil {
		t.Skip("the home of this user is no data directory: " + c.DataDir)
	}
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

// A name that belongs to no system user reaches no podman: which user the call
// would have to run as is exactly what that name answers, so without it there
// is nothing to run.
func TestTheCallsAfterCreateNeedTheUserOfTheirName(t *testing.T) {
	f := fakePodman(t)
	p := NewPodman(f.bin)
	gone := "ngc-nobody-00000000"
	if _, err := user.Lookup(gone); err == nil {
		t.Skip("there is a user called " + gone + " on this machine")
	}
	calls := map[string]func() error{
		"Start":  func() error { return p.Start(context.Background(), gone) },
		"Stop":   func() error { return p.Stop(context.Background(), gone) },
		"Remove": func() error { return p.Remove(context.Background(), gone) },
		"Running": func() error {
			_, err := p.Running(context.Background(), gone)
			return err
		},
		"Exec": func() error {
			_, err := p.Exec(context.Background(), Exec{Name: gone, Command: []string{"/usr/bin/greet"}})
			return err
		},
		"LoadImage": func() error {
			_, err := p.LoadImage(context.Background(), gone, "/var/lib/ngc/staged/amd64.tar")
			return err
		},
		"RemoveImage": func() error {
			return p.RemoveImage(context.Background(), gone, "ngc/terminal:1.0.0")
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrUser) {
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
	name := meName(t)
	for _, cmd := range [][]string{nil, {}} {
		if _, err := NewPodman(f.bin).Exec(context.Background(), Exec{Name: name, Command: cmd}); !errors.Is(err, ErrCommand) {
			t.Fatalf("err = %v, want ErrCommand", err)
		}
		if f.ran() {
			t.Fatalf("podman was started anyway: %q", f.args(t))
		}
	}
}
