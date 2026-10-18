package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// This file holds every case about the hardening of an app container, and it
// holds them so that they run on any machine, podman or not.
//
// Why it exists, measured and not reasoned out (12.10.2026, on the whole
// controller set, as an overlay, -count=1): the hardening is the only wall
// between an app and this server, and not one case held any of it. Three
// mutants lived - "--cap-drop ALL" plus no-new-privileges taken out,
// "--network none" plus --read-only taken out, and every 0o700 in run.go and
// data.go turned into 0o777 - each time exit code 0 and 0 --- FAIL out of 944
// cases. The reason was not a missing case but a skipped one: the stand-in for
// podman is a /bin/sh script, so it and the 23 cases that read podman's
// argument list skip themselves outside Linux, and a set that switches itself
// off looks green.
//
// So nothing here starts anything. createArgs and execArgs build the list,
// which is what the hardening is, and the two Ensure calls record the mode
// they asked for instead of reading one off a disk that has none.
//
// What stays unmeasured here, on purpose and said out loud: whether a
// directory on the disk really carries 0700 afterwards, and whether podman
// honours any of these switches. The first is measured on Linux by
// TestEnsureRunTightensTheModeBeforeItHandsTheDirectoryOver and
// TestEnsureDataTightensTheModeOfADirectoryAlreadyThere, the second by nothing
// in this repo and by nothing that could run on a machine without podman.

// hardApp is a container that every case here starts from: it passes every
// refusal of createArgs, so what a case then reads is the hardening and not an
// error. UID and GID are no user of this machine - createArgs never looks one
// up, that is Create's own line - they only have to be neither 0.
func hardApp() Container {
	return Container{
		Name: "ngc-terminal-1a2b3c4d", Image: "ngc/terminal:1.0.0",
		UID: 1234, GID: 1234,
		DataDir: "/var/lib/ngc/apps/org.sorglos.terminal",
		Hold:    "/usr/lib/ngc/ngc-controller",
	}
}

// grant is one of the four containers an app can be. A switch that is part of
// the hardening has to be in all four lists, or a grant of the store takes away
// more than it says it does.
type grant struct {
	what      string
	container Container
}

func grants() []grant {
	net, root, both := hardApp(), hardApp(), hardApp()
	net.Network = true
	root.Root = true
	both.Network, both.Root = true, true
	return []grant{
		{"nothing granted", hardApp()},
		{"the network granted", net},
		{"root granted", root},
		{"both granted", both},
	}
}

func argsOf(t *testing.T, c Container) []string {
	t.Helper()
	args, err := createArgs(c)
	if err != nil {
		t.Fatalf("createArgs: %v", err)
	}
	return args
}

// pairAt reports whether opt is in args and is followed by value. Written as a
// pair and not as two Contains calls: "--security-opt" somewhere and
// "no-new-privileges" somewhere else is not the same switch.
func pairAt(args []string, opt, value string) bool {
	for i, a := range args {
		if a == opt && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// volumesOf is every spec that follows a --volume, and optsOf is the option
// list of one spec - everything after the mount point. Both exist because a
// spec is "source:target:opt,opt" and a case that reads it as a whole string
// cannot say whether an option is missing or merely somewhere else.
func volumesOf(args []string) []string {
	var out []string
	for i, a := range args {
		if a == "--volume" && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

func optsOf(spec string) []string {
	parts := strings.Split(spec, ":")
	if len(parts) < 3 {
		return nil
	}
	return strings.Split(parts[len(parts)-1], ",")
}

// 1. The whole list at once. This one dies at every mutant of the hardening and
// is meant to: it is the one place that also catches a switch nobody thought to
// ask about. The cases after it say which switch is which, so a red run here is
// read together with them.
func TestHardeningIsTheWholeArgumentList(t *testing.T) {
	c := hardApp()
	c.CPU, c.Memory = 1000, 256
	want := []string{
		"create", "--name", "ngc-terminal-1a2b3c4d",
		"--network", "none",
		"--cpus", "1.000",
		"--memory", "256m",
		"--pids-limit", "512",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--volume", "/var/lib/ngc/apps/org.sorglos.terminal:/data:rw,nosuid,nodev",
		"--volume", "/usr/lib/ngc/ngc-controller:/ngc/ngc-hold:ro,nosuid,nodev",
		"--user", "0:0",
		"ngc/terminal:1.0.0", "/ngc/ngc-hold", "hold",
	}
	if got := argsOf(t, c); !slices.Equal(got, want) {
		t.Fatalf("createArgs =\n%q\nwant\n%q", got, want)
	}
}

// 2. --cap-drop ALL. Without the grant it is there, and it is the capabilities
// of the container: an app that keeps them needs one kernel hole to be root in
// a namespace that maps to a real user of this server.
func TestHardeningDropsEveryCapabilityWithoutTheRootGrant(t *testing.T) {
	for _, g := range grants() {
		args := argsOf(t, g.container)
		got := pairAt(args, "--cap-drop", "ALL")
		want := !g.container.Root
		if got != want {
			t.Fatalf("%s: --cap-drop ALL present = %v, want %v, args = %q",
				g.what, got, want, args)
		}
		// And nothing ever hands one back.
		for _, bad := range []string{"--cap-add", "--privileged"} {
			if slices.Contains(args, bad) {
				t.Fatalf("%s: args = %q carry %s", g.what, args, bad)
			}
		}
	}
}

// 3. no-new-privileges, in all four. This is what holds whatever a set-user-id
// program inside the image would otherwise gain, and no grant of the store may
// take it away: root in the container is already given by the capabilities, so
// giving up this one as well would buy nothing.
func TestHardeningForbidsGainingRightsInEveryContainer(t *testing.T) {
	for _, g := range grants() {
		if args := argsOf(t, g.container); !pairAt(args, "--security-opt", "no-new-privileges") {
			t.Fatalf("%s: args = %q without --security-opt no-new-privileges", g.what, args)
		}
	}
}

// 4. --read-only, in all four. The image stays unwritable; an app that could
// write into it would keep what it wrote across a reinstall of that image.
func TestHardeningKeepsTheImageUnwritableInEveryContainer(t *testing.T) {
	for _, g := range grants() {
		if args := argsOf(t, g.container); !slices.Contains(args, "--read-only") {
			t.Fatalf("%s: args = %q without --read-only", g.what, args)
		}
	}
}

// 5. --network none without the grant, and no way out named with it. Both
// halves are the switch: an app without the grant has no network at all, and an
// app with it gets whatever podman has, because naming one here would pin this
// server to a program that may not be on it.
func TestHardeningGivesNoNetworkWithoutTheGrant(t *testing.T) {
	for _, g := range grants() {
		args := argsOf(t, g.container)
		if g.container.Network {
			if slices.Contains(args, "--network") {
				t.Fatalf("%s: args = %q name a way to the network", g.what, args)
			}
			for _, a := range args {
				if a == "none" || a == "host" || a == "bridge" ||
					strings.Contains(a, "slirp") || strings.Contains(a, "pasta") {
					t.Fatalf("%s: args = %q pin the network to %q", g.what, args, a)
				}
			}
			continue
		}
		if !pairAt(args, "--network", "none") {
			t.Fatalf("%s: args = %q without --network none", g.what, args)
		}
	}
}

// 6. The program that holds the container open is mounted read-only. It is
// ngc-controller itself: a container that could write into it would be writing
// a program of this server, and the next start of any app would run what it
// wrote.
func TestHardeningMountsTheHoldProgramReadOnly(t *testing.T) {
	for _, g := range grants() {
		args := argsOf(t, g.container)
		// Read apart and not as one string: the options grew when nosuid and
		// nodev came, and what this case is about is "ro" being among them.
		found := false
		for _, v := range volumesOf(args) {
			if strings.HasPrefix(v, g.container.Hold+":/ngc/ngc-hold:") &&
				slices.Contains(optsOf(v), "ro") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: args = %q do not mount the hold program read-only", g.what, args)
		}
	}
}

// 7. /data is the only writable thing in the container, and it is the one
// directory of the app. Counted and not merely looked for: a second writable
// volume is exactly the shape a way out of the container takes.
func TestHardeningLeavesOnlyTheDataDirectoryWritable(t *testing.T) {
	for _, g := range grants() {
		args := argsOf(t, g.container)
		var writable []string
		for _, v := range volumesOf(args) {
			// The options come after the third colon, so a spec is read apart
			// and not matched at its end: ":rw" stopped being the last thing in
			// a spec when nosuid and nodev were added, and a HasSuffix here
			// would then have found no writable volume at all and passed.
			if slices.Contains(optsOf(v), "rw") {
				writable = append(writable, v)
			}
		}
		want := []string{g.container.DataDir + ":/data:rw,nosuid,nodev"}
		if !slices.Equal(writable, want) {
			t.Fatalf("%s: writable volumes = %q, want %q (args %q)",
				g.what, writable, want, args)
		}
	}
}

// 8. --user 0:0, and what that is: podman runs rootless as the app's own user,
// so 0 inside the container is that user outside it. Without this the container
// would run as whatever user the image names, which is a number of the image
// maker's choosing and maps to some other user of this server.
func TestHardeningRunsAsTheMappedUserAndNotAsTheImagesChoice(t *testing.T) {
	for _, g := range grants() {
		if args := argsOf(t, g.container); !pairAt(args, "--user", "0:0") {
			t.Fatalf("%s: args = %q without --user 0:0", g.what, args)
		}
	}
}

// 9. Both limits are always given, and an empty manifest field becomes the
// default of the contract and not "no limit". A container without them is one
// app that takes the machine away from all the others.
func TestHardeningAlwaysGivesBothLimits(t *testing.T) {
	for _, g := range grants() {
		args := argsOf(t, g.container)
		if !pairAt(args, "--cpus", "1.000") {
			t.Fatalf("%s: args = %q without the default --cpus 1.000", g.what, args)
		}
		if !pairAt(args, "--memory", "256m") {
			t.Fatalf("%s: args = %q without the default --memory 256m", g.what, args)
		}
	}
	c := hardApp()
	c.CPU, c.Memory = 1500, 512
	args := argsOf(t, c)
	if !pairAt(args, "--cpus", "1.500") || !pairAt(args, "--memory", "512m") {
		t.Fatalf("args = %q, want --cpus 1.500 and --memory 512m", args)
	}
}

// 10. A command in a running container gains nothing over the container. Root
// for a call says the user out loud and nothing else: what the container may do
// was decided once, at Create.
func TestHardeningExecGrantsNothingBeyondTheContainer(t *testing.T) {
	for _, root := range []bool{false, true} {
		e := Exec{Name: "ngc-terminal-1a2b3c4d", Command: []string{"/bin/busy", "-n"}, Root: root}
		args, err := execArgs(e)
		if err != nil {
			t.Fatalf("execArgs: %v", err)
		}
		for _, bad := range []string{"--privileged", "--cap-add", "--security-opt", "--volume"} {
			if slices.Contains(args, bad) {
				t.Fatalf("root=%v: args = %q carry %s", root, args, bad)
			}
		}
		if root && !pairAt(args, "--user", "0:0") {
			t.Fatalf("args = %q, want --user 0:0 for a call with root", args)
		}
		if !root && slices.Contains(args, "--user") {
			t.Fatalf("args = %q name a user for a call without root", args)
		}
		// Everything behind -- is the container and its command, so a command
		// of a manifest that begins with a dash stays its argument.
		i := slices.Index(args, "--")
		if i < 0 || !slices.Equal(args[i+1:], []string{"ngc-terminal-1a2b3c4d", "/bin/busy", "-n"}) {
			t.Fatalf("args = %q, want the container and its command behind --", args)
		}
	}
}

// modeLog records what mode each directory call was asked for, in order. It
// makes the real call as well: a case that only recorded would measure its own
// recording, and the two Ensure calls go on from what the call left behind.
type modeLog struct {
	calls []string
}

func (m *modeLog) note(what, path string, mode os.FileMode) {
	m.calls = append(m.calls, what+" "+filepath.ToSlash(path)+" "+mode.Perm().String())
}

// bendModes puts the recorder in place of the three directory calls. It checks
// nothing itself; every case that uses it compares the recorded list against
// the numbers the comments in run.go and data.go name, and a recorder that was
// never called records an empty list, which no case here accepts.
func bendModes(t *testing.T) *modeLog {
	t.Helper()
	m := &modeLog{}
	oldAll, oldDir, oldMode := paths.mkdirAll, paths.mkdir, paths.chmod
	t.Cleanup(func() { paths.mkdirAll, paths.mkdir, paths.chmod = oldAll, oldDir, oldMode })
	paths.mkdirAll = func(p string, perm os.FileMode) error {
		m.note("mkdirAll", p, perm)
		return os.MkdirAll(p, perm)
	}
	paths.mkdir = func(p string, perm os.FileMode) error {
		m.note("mkdir", p, perm)
		return os.Mkdir(p, perm)
	}
	paths.chmod = func(p string, mode os.FileMode) error {
		m.note("chmod", p, mode)
		return os.Chmod(p, mode)
	}
	return m
}

// 11. The two modes of EnsureRun: 0711 for the directory above, which every app
// enters and nobody lists, and 0700 for the app's own, made and then tightened
// again - because a remnant of an earlier run keeps its mode, os.Mkdir does not
// touch it, and /run is a tmpfs full of remnants after a restart.
//
// The order matters and is read here too: the mode is tightened BEFORE the
// directory is handed to the app's user. After the chown a 0777 directory of
// that user is a door standing open, and no tightening afterwards closes it for
// the moment in between.
func TestHardeningEnsureRunAsksForTheTightModes(t *testing.T) {
	root := filepath.ToSlash(filepath.Join(t.TempDir(), "run", "ngc"))
	oldRun, oldChown := paths.runDir, paths.chown
	t.Cleanup(func() { paths.runDir, paths.chown = oldRun, oldChown })
	paths.runDir = root
	m := bendModes(t)
	chownAt := -1
	paths.chown = func(string, int, int) error { chownAt = len(m.calls); return nil }

	dir, err := EnsureRun(context.Background(), User{Name: "ngc-a-00000000", UID: 1234, GID: 1234})
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}
	want := []string{
		"mkdirAll " + root + " -rwx--x--x",
		"chmod " + root + " -rwx--x--x",
		"mkdir " + filepath.ToSlash(dir) + " -rwx------",
		"chmod " + filepath.ToSlash(dir) + " -rwx------",
	}
	if !slices.Equal(m.calls, want) {
		t.Fatalf("EnsureRun asked for\n%q\nwant\n%q", m.calls, want)
	}
	if chownAt != len(want) {
		t.Fatalf("the directory was handed over after %d of %d mode calls, want after all of them",
			chownAt, len(want))
	}

	// And again on the same directory, after someone widened it: the second run
	// is the case the chmod is for, and it has to tighten it again.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	m.calls = nil
	if _, err := EnsureRun(context.Background(),
		User{Name: "ngc-a-00000000", UID: 1234, GID: 1234}); err != nil {
		t.Fatalf("EnsureRun again: %v", err)
	}
	if !slices.Contains(m.calls, "chmod "+filepath.ToSlash(dir)+" -rwx------") {
		t.Fatalf("the second run asked for %q, none of it tightens the directory again", m.calls)
	}
}

// 12. The mode of a data directory: 0711 root:ngc-apps for the folder of all
// apps and 0700 for the app's own, asked for at the making and again
// afterwards. Nobody but the app's user may ENTER the app's own - it is where
// everything the app keeps lies, and the encryption below it protects a stolen
// disk, not a neighbour on this machine.
//
// THE FOLDER ABOVE WAS 0700 UNTIL 18.10.2026, and this case said so. It was
// wrong, and no case could see it: podman runs as the app's own user and keeps
// the image store of the app under that folder, so a user that may not
// traverse it reaches nothing. Measured on the real machine, from podman's own
// stderr - "cannot resolve /var/lib/ngc/apps/org.sorglos.terminal: lstat ...:
// permission denied" - and every activation ended there, with the reason
// thrown away. So no app had ever run, on any installation.
//
// 0711 AND the group (Thomas, 19.10.2026), and the mode is the half that was
// measured: 0710 was tried first and refused, because a rootless container
// runs in a user namespace where a supplementary group is not mapped - crun
// died on "merged: Permission denied" with the group correctly set and the
// user correctly in it. A/B on the real machine, the same build with this one
// mode changed: 0710 refused, 0711 ran. The read bit is set for nobody either
// way, so the list of installed apps stays unreadable.
//
// What is held here is the whole of what these lines do - which call, which
// path, which mode - and the GROUP of the folder with it, because the mode
// alone would be true of a 0710 folder that belongs to root and reaches
// nobody.
func TestHardeningEnsureDataAsksForTheTightMode(t *testing.T) {
	root := filepath.ToSlash(filepath.Join(t.TempDir(), "apps"))
	oldData, oldAdd, oldPolicy := paths.appData, paths.addKey, paths.setPolicy
	t.Cleanup(func() {
		paths.appData, paths.addKey, paths.setPolicy = oldData, oldAdd, oldPolicy
	})
	paths.appData = root
	var id [keyIdentifierSize]byte
	paths.addKey = func(string, []byte) ([keyIdentifierSize]byte, error) { return id, nil }
	paths.setPolicy = func(string, [keyIdentifierSize]byte) error { return nil }
	m := bendModes(t)
	// The group is a stand-in: the real one runs groupadd, which this machine
	// has not got and which a case may not run anywhere. What it answers is a
	// gid nothing else here uses, so the chown below can only have got it from
	// this call.
	oldGID, oldChown := paths.appsGID, paths.chown
	t.Cleanup(func() { paths.appsGID, paths.chown = oldGID, oldChown })
	const groupGID = 4242
	asked := 0
	paths.appsGID = func(context.Context) (int, error) {
		asked++
		return groupGID, nil
	}
	var chowns []string
	paths.chown = func(p string, uid, gid int) error {
		chowns = append(chowns, fmt.Sprintf("%s %d:%d", filepath.ToSlash(p), uid, gid))
		// The call is RECORDED and not made: os.Chown does not exist on the
		// machine this is written on, and an error here would stop EnsureData
		// before the calls this case is about. That the real chown works is
		// measured on Linux, by the cases that run there.
		return nil
	}

	dir, err := EnsureData(context.Background(), "org.sorglos.terminal",
		User{Name: "ngc-a-00000000", UID: 1234, GID: 1234}, make([]byte, 32))
	// The chown of EnsureData is os.Chown, which Windows does not have. What
	// this case is about happens before it, so an error from there is noted and
	// not a failure - but the calls before it have to have happened, and the
	// tightening after it is read wherever it was reached.
	if err != nil && !errors.Is(err, errors.ErrUnsupported) {
		t.Logf("EnsureData ended with %v - the mode calls below are still the case", err)
	}
	want := []string{
		"mkdirAll " + root + " -rwx--x--x",
		"chmod " + root + " -rwx--x--x",
		"mkdir " + root + "/org.sorglos.terminal -rwx------",
	}
	if len(m.calls) < len(want) || !slices.Equal(m.calls[:len(want)], want) {
		t.Fatalf("EnsureData asked for\n%q\nwant it to begin with\n%q", m.calls, want)
	}
	// THE GROUP IS WHAT MAKES 0710 MEAN ANYTHING, so it is asked for and it is
	// the one the system answered - never a number written down here twice.
	if asked == 0 {
		t.Fatal("EnsureData never asked for the group of the apps folder")
	}
	if !slices.Contains(chowns, fmt.Sprintf("%s -1:%d", root, groupGID)) {
		t.Fatalf("the apps folder was chowned %q, want root and the gid of %s",
			chowns, AppsGroup)
	}
	// Every OTHER directory stays 0700: the one of the app, and the tightening
	// of one that was already there.
	for _, c := range m.calls {
		if !strings.HasPrefix(c, "chmod ") || strings.HasPrefix(c, "chmod "+root+" ") {
			continue
		}
		if !strings.HasSuffix(c, " -rwx------") {
			t.Fatalf("EnsureData asked for %q", c)
		}
	}
	if err == nil {
		last := "chmod " + filepath.ToSlash(dir) + " -rwx------"
		if m.calls[len(m.calls)-1] != last {
			t.Fatalf("EnsureData ended with %q, want %q", m.calls[len(m.calls)-1], last)
		}
	}
}

// 13. --pids-limit, in all four. It is the only bound on the number of tasks a
// container may have, and without it the bound is podman's own default - a
// number that does not stand in this repo. A fork bomb in an app is otherwise
// one app taking every process slot of the kernel, and the controller itself
// cannot fork any more either.
//
// The number is read against PIDsLimit and against what it has to be, not
// against a literal: a case that only compared with the constant would measure
// the constant by itself (see messen.md, "eine Grenze, die nur durch sich
// selbst geprueft wird"). So the value has to be a number, it has to be the
// constant, it has to be above what the hold process measurably needs, and it
// must not be one of the two values podman reads as "no limit".
func TestHardeningLimitsTheNumberOfProcessesInEveryContainer(t *testing.T) {
	for _, g := range grants() {
		args := argsOf(t, g.container)
		i := slices.Index(args, "--pids-limit")
		if i < 0 || i+1 >= len(args) {
			t.Fatalf("%s: args = %q without --pids-limit", g.what, args)
		}
		got, err := strconv.Atoi(args[i+1])
		if err != nil {
			t.Fatalf("%s: --pids-limit %q is no number: %v", g.what, args[i+1], err)
		}
		if got != PIDsLimit {
			t.Fatalf("%s: --pids-limit %d, want PIDsLimit = %d", g.what, got, PIDsLimit)
		}
		// 0 and -1 are both "unlimited" to podman, and either would be a
		// switch that is sent and does nothing.
		if got <= 0 {
			t.Fatalf("%s: --pids-limit %d is no limit at all", g.what, got)
		}
		// 24 OS threads were measured for a Go program of the shape of the
		// hold process on this machine (64 cores, 1000 goroutines), and the
		// pids controller counts threads. A ceiling anywhere near that would
		// kill ordinary apps instead of fork bombs.
		if got < 128 {
			t.Fatalf("%s: --pids-limit %d is below what an app plausibly needs", g.what, got)
		}
		// And nothing may hand the limit back further along the list.
		if slices.Index(args[i+1:], "--pids-limit") > 0 {
			t.Fatalf("%s: args = %q name --pids-limit twice", g.what, args)
		}
	}
}

// 14. nosuid and nodev on every mount, in all four. no-new-privileges holds
// what a set-user-id program would gain through an exec of the container's own
// init; these two say the kernel must not honour the bit, and must not honour a
// device node, for anything that comes in through a mount at all. /data is the
// one the app writes itself, which is why it is the one that matters - and the
// case reads every mount, because a second mount added later without the
// options is the same hole in a new place.
func TestHardeningMountsForbidSetUserIDAndDeviceNodes(t *testing.T) {
	for _, g := range grants() {
		args := argsOf(t, g.container)
		vols := volumesOf(args)
		if len(vols) != 2 {
			t.Fatalf("%s: %d mounts in %q, want the two of the contract",
				g.what, len(vols), args)
		}
		for _, v := range vols {
			opts := optsOf(v)
			for _, want := range []string{"nosuid", "nodev"} {
				if !slices.Contains(opts, want) {
					t.Fatalf("%s: mount %q carries %q, without %s", g.what, v, opts, want)
				}
			}
			// The positive forms are what an option list looks like when
			// somebody turned one of them off again.
			for _, bad := range []string{"suid", "dev"} {
				if slices.Contains(opts, bad) {
					t.Fatalf("%s: mount %q carries %q", g.what, v, bad)
				}
			}
		}
	}
}
