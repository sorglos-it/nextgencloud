package runtime

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// linuxOnly skips everything that needs a user database with numeric ids or a
// shell to run a stand-in program. NGC is written on Windows and run on Linux,
// so these cases say so instead of failing there.
func linuxOnly(t *testing.T) {
	t.Helper()
	if goruntime.GOOS != "linux" {
		t.Skip("system users and their sub id ranges are Linux")
	}
}

// fakeUseradd puts a program of its own in place of useradd and userdel and
// empty files in place of /etc/subuid and /etc/subgid, so that a case can read
// the argument list without changing this machine. The real paths are never
// touched by a test.
//
// The stand-in records every argument on its own line and ends with 0, so the
// case afterwards finds the user missing - which the real useradd would never
// leave behind, and that is exactly why the case reads the argument list and
// not the result.
func fakeUseradd(t *testing.T) string {
	t.Helper()
	linuxOnly(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$0\" >> '" + log + "'\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + log + "'; done\n" +
		"env > '" + dir + "/env'\n" +
		"exit 0\n"
	// A variable of this process that must not reach useradd. The whole
	// environment of a controller is read this way: it runs as root under
	// systemd and carries whatever systemd gave it.
	t.Setenv("NGC_LEAK", "this must not reach useradd")
	for _, name := range []string{"useradd", "userdel", "groupadd", "usermod"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	was := paths
	t.Cleanup(func() { paths = was })
	paths.useradd = filepath.Join(dir, "useradd")
	paths.userdel = filepath.Join(dir, "userdel")
	paths.groupadd = filepath.Join(dir, "groupadd")
	paths.usermod = filepath.Join(dir, "usermod")
	// AND THE GROUP ITSELF IS A STAND-IN, for every case but the one that
	// measures it (TestEnsureAppsGroupBuildsTheArgumentList, which puts the
	// real one back). The real one runs groupadd and then READS THE GROUP BACK
	// out of this machine - and a case may make no group here: measured on the
	// real VM on 19.10.2026, the test binary ran as an ordinary user and got
	// "groupadd: Permission denied" out of the real program, which turned five
	// cases red for a reason that was the apparatus and not the code.
	paths.appsGID = func(context.Context) (int, error) { return testAppsGID, nil }
	// AND THIS MACHINE HAS NO SUCH GROUP, whatever it really has: the group
	// database is the machine's, and a case that read it would pass or fail by
	// where it runs (measured, on a VM where the group really existed).
	paths.groupGID = func(string) (int, bool) { return 0, false }
	paths.subuid = filepath.Join(dir, "subuid")
	paths.subgid = filepath.Join(dir, "subgid")
	// A space in the path on purpose. The program is started with a list and no
	// shell sees it, so a space stays inside its argument - and a space is the
	// one character that shows the difference: through a shell the home
	// directory would arrive as two arguments and the name as a third.
	paths.appData = filepath.Join(dir, "apps of this test")
	for _, p := range []string{paths.subuid, paths.subgid} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return log
}

// loggedArgs is what the stand-in was called with, the program itself first.
func loggedArgs(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// testAppsGID is the gid the stand-in answers with. Nothing else in this
// package uses the number, so a chown that names it can only have got it from
// there.
const testAppsGID = 4242

// THE ARGUMENT LIST OF groupadd, and no shell anywhere: --system, the name,
// nothing else. The group is what makes the 0710 of the apps folder mean
// something (see AppsGroup), so what it is called and how it is made is held
// here as tightly as the user is.
//
// The real appsGID is put back for this one case, and it ENDS IN AN ERROR on
// purpose: the stand-in makes no group, so reading it back afterwards finds
// nothing - exactly as TestEnsureUserBuildsTheArgumentList works, and for the
// same reason. What is measured is the call, not the result.
func TestEnsureAppsGroupBuildsTheArgumentList(t *testing.T) {
	log := fakeUseradd(t)
	paths.appsGID = ensureAppsGroup
	if _, err := paths.appsGID(context.Background()); err == nil {
		t.Fatal("the stand-in made no group, so this had to fail")
	}
	// THE NAME IS WRITTEN DOWN ONCE, here and nowhere else, and it is held
	// against the LETTERS and not against the constant: it goes into
	// /etc/group, into the group of the apps folder on disk and into the
	// secondary group of every app user, so a build that renamed it would
	// leave every installation standing today with a folder whose group no
	// user is in - and a case that compared AppsGroup with AppsGroup would
	// have nothing at all to say about that (messen.md: a bound checked only
	// by itself is not checked).
	if AppsGroup != "ngc-apps" {
		t.Fatalf("the group is called %q - it stands in /etc/group, in the group of "+
			"the apps folder and in every app user, so renaming it needs a way to "+
			"rename all three", AppsGroup)
	}
	want := []string{paths.groupadd, "--system", AppsGroup}
	if got := loggedArgs(t, log); !slices.Equal(got, want) {
		t.Fatalf("args =\n%q\nwant\n%q", got, want)
	}
}

func TestUserNameIsStableAndShort(t *testing.T) {
	long := "org.example." + strings.Repeat("a", 80)
	n := UserName(long)
	if len(n) > 32 || !strings.HasPrefix(n, "ngc-") {
		t.Fatalf("name = %q (%d bytes)", n, len(n))
	}
	if n != UserName(long) {
		t.Fatal("UserName is not stable")
	}
	if UserName("org.example.a") == UserName("org.example.b") {
		t.Fatal("two apps share one user")
	}
	// Two apps whose last label is the same, which is the case the readable
	// part cannot tell apart on its own: what separates them is the hash of the
	// whole app id, and a name built out of the label alone would give these
	// two one user - and with it one data directory and one image store.
	if UserName("org.a.notes") == UserName("org.b.notes") {
		t.Fatal("two apps with the same last label share one user")
	}
	// The readable part is the last label and not the whole id, because that is
	// what a person looking at a process list recognises. Measured where the
	// difference shows: an id too long for both.
	if n := UserName("org.example.verylongprefix.notes"); !strings.Contains(n, "notes") {
		t.Fatalf("name = %q does not say which app it is", n)
	}
	for _, r := range n {
		if !(r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			t.Fatalf("name = %q has %q", n, r)
		}
	}
}

// Every name this package makes has to pass the grammar the engine holds every
// container name against, and that is the whole reason the name is made here: a
// name outside it fails not where it is built but at every single call for that
// app, and only on a server.
//
// The shapes with an edge: the longest app id there can be, a last label of
// nothing but dashes, one of a single character, one of digits only, and the
// shortest id of all.
//
// The last five are ids that are no ids at all. EnsureUser refuses those before
// it ever gets here, so this is a second fence behind the first - and it is
// worth having, because the one thing a caller can do with this function is
// hand it a string: it takes no error and returns none.
func TestEveryUserNameIsAContainerName(t *testing.T) {
	for _, app := range []string{
		"org.example.a",
		"org.example." + strings.Repeat("a", 87),
		strings.Repeat("a", 98) + ".b",
		"org.example.---",
		"org.example.-",
		"org.example.0",
		"a.b",
		"org.example.my-app-with-a-very-long-label-indeed",
		"ORG.Example.App",
		"org.example.a/../../etc",
		"org.example.a b",
		"org.example.a\nb",
		"",
	} {
		n := UserName(app)
		if !validName.MatchString(n) {
			t.Errorf("UserName(%q) = %q is no container name", app, n)
		}
	}
}

// The three flags of the plan, the home directory of task 11, the name, and
// nothing else - in that order, and no shell anywhere: the program is called
// with a list.
//
// --home-dir is not in the plan's own sentence and stands here on purpose: the
// engine reads the home of the app's user out of the user database and refuses
// a container whose data directory is anything else (ErrHome, ofName and Create
// in podman.go). A user made without it gets /home/<name>, and then not one
// container of that app would ever start.
func TestEnsureUserBuildsTheArgumentList(t *testing.T) {
	log := fakeUseradd(t)
	app := "org.example.notes"
	name := UserName(app)
	if _, err := EnsureUser(context.Background(), app); err == nil {
		t.Fatal("the stand-in made no user, so this had to fail")
	}
	want := []string{
		paths.useradd,
		"--system",
		"--no-create-home",
		// --groups AppsGroup since 19.10.2026, and it is not cosmetic: the
		// folder the data directories lie in is 0710 root:ngc-apps, and a
		// user outside that group cannot step through it. podman then fails
		// on its own home with "permission denied" and the whole activation
		// is rolled back - which is what happened on the real machine until
		// that day, on every installation.
		"--groups", AppsGroup,
		"--shell", "/usr/sbin/nologin",
		"--home-dir", paths.appData + "/" + app,
		name,
	}
	if got := loggedArgs(t, log); !slices.Equal(got, want) {
		t.Fatalf("args =\n%q\nwant\n%q", got, want)
	}
	// And it is given an environment of its own, not the one of this process.
	// Measured over the whole environment the program saw, not over the names
	// this case happens to expect: a fifth variable has to show up here
	// whatever it is called.
	b, err := os.ReadFile(filepath.Join(filepath.Dir(paths.useradd), "env"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		n, _, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		// What a shell adds to an environment of its own accord: dash sets PWD.
		if n == "PWD" {
			continue
		}
		names = append(names, n)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"LANG", "PATH"}) {
		t.Fatalf("useradd saw %q, want LANG and PATH and nothing else", names)
	}
}

func TestEnsureUserRefusesWhatIsNoApp(t *testing.T) {
	fakeUseradd(t)
	for _, app := range []string{"", "no-dot", "UPPER.CASE", "org.example.a/b",
		"org.example.a\nb", ".leading", "trailing.", "org.example.a:b",
		strings.Repeat("a", 60) + "." + strings.Repeat("b", 60)} {
		if _, err := EnsureUser(context.Background(), app); !errors.Is(err, ErrApp) {
			t.Errorf("EnsureUser(%q) = %v, want ErrApp", app, err)
		}
		if err := DropUser(context.Background(), app); !errors.Is(err, ErrApp) {
			t.Errorf("DropUser(%q) = %v, want ErrApp", app, err)
		}
	}
}

// ofUserDB is where a user that is there is held against what it has to be, and
// the two things it must never let through are root and a home somewhere else.
// Both are measurable without making a user: root is in every user database,
// and its home is /root.
func TestOfUserDBRefusesRootAndAHomeSomewhereElse(t *testing.T) {
	linuxOnly(t)
	was := paths
	t.Cleanup(func() { paths = was })
	// With no prefix, DataDirOf("root") is /root - the home root really has, so
	// the only thing left for this call to object to is the uid.
	paths.appData = ""
	if _, err := ofUserDB("root", "root"); !errors.Is(err, ErrIdentity) {
		t.Errorf("ofUserDB of root = %v, want ErrIdentity", err)
	}
	// And the other way round, over a user that is not root, so that the uid is
	// not what it could object to: the home of that user is not the data
	// directory this app would have, and that alone has to be enough. Without
	// it every call of the engine after Create would look for the container in
	// another store of podman than the one it was made in.
	app := anApp(t)
	paths.appData = "/somewhere-no-user-has-its-home"
	if _, err := ofUserDB(app.name, "org.example.a"); !errors.Is(err, ErrHome) {
		t.Errorf("ofUserDB of %s with a wrong home = %v, want ErrHome", app.name, err)
	}
	// With the right home the same user goes through, so the case above failed
	// for the home and for nothing else.
	paths.appData = ""
	if u, err := ofUserDB(app.name, strings.TrimPrefix(app.home, "/")); err != nil {
		t.Errorf("ofUserDB of %s with its own home = %v", app.name, err)
	} else if u.UID != app.uid || u.GID != app.gid {
		t.Errorf("ofUserDB = %d:%d, the database says %d:%d", u.UID, u.GID, app.uid, app.gid)
	}
}

// One range per app, 65536 ids wide, the first at 524288 - and a second app
// never gets a range that overlaps the first. The lines of other users stay
// where they are, and a line this package cannot read stays too: /etc/subuid is
// not ours alone, and dropping a line of it would take a range away from
// whoever owns it.
func TestSubIDsAreOnePerAppAndNeverOverlap(t *testing.T) {
	fakeUseradd(t)
	if err := os.WriteFile(paths.subuid,
		[]byte("someone:100000:65536\nsomething this package cannot read\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ngc-a", "ngc-b", "ngc-a", "ngc-c"} {
		if err := ensureSubIDs(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	b, err := os.ReadFile(paths.subuid)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("lines = %q, want the two foreign ones and three of ours", lines)
	}
	if lines[0] != "someone:100000:65536" {
		t.Fatalf("the foreign line changed: %q", lines[0])
	}
	if lines[1] != "something this package cannot read" {
		t.Fatalf("the unreadable line is gone: %q", lines[1])
	}
	// The two numbers written out and not taken from the constants they hold:
	// a case that says "SubIDCount wide" says nothing at all, because it moves
	// with the constant it is meant to hold still. 524288 is above everything a
	// distribution hands to a person (Debian starts at 100000 with 65536 each),
	// and 65536 is what a user namespace of a rootless container needs.
	const wantBase, wantCount = 524288, 65536
	seen := map[string]uint64{}
	for _, l := range lines[2:] {
		f := strings.Split(l, ":")
		if len(f) != 3 || f[2] != strconv.Itoa(wantCount) {
			t.Fatalf("line %q", l)
		}
		start, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if start < wantBase || (start-wantBase)%wantCount != 0 {
			t.Fatalf("line %q does not start on a range of ours", l)
		}
		for other, s := range seen {
			if start == s {
				t.Fatalf("%s and %s share the range at %d", other, f[0], start)
			}
		}
		if _, twice := seen[f[0]]; twice {
			t.Fatalf("%s has two ranges", f[0])
		}
		seen[f[0]] = start
	}
	// The group ranges are written the same way, into the other file.
	if b, err = os.ReadFile(paths.subgid); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 3 {
		t.Fatalf("subgid has %d lines, want three", n)
	}
}

// A half-written line would lock an app out of its own range for good, so the
// file is replaced whole or not at all: after a write the directory holds no
// leftover of a temporary file, and the file itself stays readable by everyone,
// which is what /etc/subuid has to be.
func TestSubIDsAreWrittenWholeOrNotAtAll(t *testing.T) {
	fakeUseradd(t)
	if err := ensureSubIDs("ngc-a"); err != nil {
		t.Fatal(err)
	}
	es, err := os.ReadDir(filepath.Dir(paths.subuid))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if strings.Contains(e.Name(), "subuid") && e.Name() != filepath.Base(paths.subuid) {
			t.Fatalf("left behind %q", e.Name())
		}
	}
	fi, err := os.Stat(paths.subuid)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", fi.Mode().Perm())
	}
}

// A range that something else already uses may never be handed to an app, and
// "already uses" means overlapping and not starting at the same number: the
// ranges of this machine are the machine's to choose and line up with nothing.
//
// Measured over a foreign range that begins 1000 ids inside the first range of
// ours and ends inside the second: an app may have neither, and the first it
// may have is the third.
func TestSubIDsNeverHandOutARangeThatIsTaken(t *testing.T) {
	fakeUseradd(t)
	const wantBase, wantCount = 524288, 65536
	foreign := "someone:" + strconv.Itoa(wantBase+1000) + ":" + strconv.Itoa(wantCount)
	if err := os.WriteFile(paths.subuid, []byte(foreign+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureSubIDs("ngc-a"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(paths.subuid)
	if err != nil {
		t.Fatal(err)
	}
	want := "ngc-a:" + strconv.Itoa(wantBase+2*wantCount) + ":" + strconv.Itoa(wantCount)
	if !strings.Contains(string(b), want) {
		t.Fatalf("want the line %q, the file is:\n%s", want, b)
	}
}

// The real thing, and it runs only where it can: useradd of this machine makes
// the user, EnsureUser answers with the uid and gid the user database now has,
// a second call changes nothing, and DropUser takes the user and both of its
// ranges away again.
func TestEnsureUserReallyMakesTheUser(t *testing.T) {
	linuxOnly(t)
	if os.Geteuid() != 0 {
		t.Skip("making a system user needs root")
	}
	if _, err := os.Stat(paths.useradd); err != nil {
		t.Skip("no useradd here: " + err.Error())
	}
	dir := t.TempDir()
	was := paths
	t.Cleanup(func() { paths = was })
	paths.subuid = filepath.Join(dir, "subuid")
	paths.subgid = filepath.Join(dir, "subgid")
	paths.appData = filepath.Join(dir, "apps")
	app := "org.ngc.test-ensure-user"
	name := UserName(app)
	t.Cleanup(func() { _ = DropUser(context.Background(), app) })

	u, err := EnsureUser(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != name || u.UID == 0 || u.GID == 0 {
		t.Fatalf("user = %+v", u)
	}
	db, err := user.Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	if db.Uid != strconv.FormatUint(uint64(u.UID), 10) ||
		db.Gid != strconv.FormatUint(uint64(u.GID), 10) {
		t.Fatalf("database says %s:%s, EnsureUser said %d:%d", db.Uid, db.Gid, u.UID, u.GID)
	}
	// The home is what makes every later call of the engine find the container:
	// it is the data directory of the app, and nothing else.
	if want := paths.appData + "/" + app; db.HomeDir != want {
		t.Fatalf("home = %q, want %q", db.HomeDir, want)
	}
	// And it really cannot log in.
	if shell := loginShell(t, name); shell != "/usr/sbin/nologin" {
		t.Fatalf("shell = %q", shell)
	}
	// Twice is once: the second call finds the user and adds no second range.
	again, err := EnsureUser(context.Background(), app)
	if err != nil || again != u {
		t.Fatalf("second call: %+v, %v", again, err)
	}
	for _, p := range []string{paths.subuid, paths.subgid} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(b), name+":"); n != 1 {
			t.Fatalf("%s names %s %d times", p, name, n)
		}
	}
	if err := DropUser(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if _, err := user.Lookup(name); err == nil {
		t.Fatal("the user is still there")
	}
	for _, p := range []string{paths.subuid, paths.subgid} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), name+":") {
			t.Fatalf("%s still names %s", p, name)
		}
	}
}

// /etc/subuid is read, and anything that is read gets a fuzz target. Two things
// are held over whatever the file says: it never panics, and the range it hands
// out overlaps none of the ranges it could read - because a range handed out
// twice maps the files of one app onto the ids of another.
//
// The file is root's own and not something from outside, so this is the second
// fence and not the first; it is here because the first one is a line of a
// distribution's tooling and not ours.
func FuzzSubIDs(f *testing.F) {
	f.Add("someone:100000:65536\n")
	f.Add("ngc-a:524288:65536\nngc-b:589824:65536\n")
	f.Add("a:0:4294967295\n")
	f.Add("x:18446744073709551615:18446744073709551615\n")
	f.Add("no colons at all\n:::\na::\n\n\n")
	f.Add("a:1:0\na:-1:5\na:1:1:1\n")
	f.Fuzz(func(t *testing.T, body string) {
		dir := t.TempDir()
		was := paths
		defer func() { paths = was }()
		paths.subuid = filepath.Join(dir, "subuid")
		if err := os.WriteFile(paths.subuid, []byte(body), 0o644); err != nil {
			t.Skip()
		}
		lines, rs, err := readSubIDs(paths.subuid)
		if err != nil {
			t.Fatalf("reading a file cannot fail here: %v", err)
		}
		// Every line that holds anything is kept, whether it could be read or
		// not: dropping one would take a range away from whoever owns it.
		var want int
		for _, l := range strings.Split(body, "\n") {
			if strings.TrimSpace(l) != "" {
				want++
			}
		}
		if len(lines) != want {
			t.Fatalf("kept %d of %d lines", len(lines), want)
		}
		start, err := freeSubIDStart(rs)
		if err != nil {
			// Nothing free is an answer, and the only one allowed here.
			if !errors.Is(err, ErrSubID) {
				t.Fatalf("freeSubIDStart = %v", err)
			}
			return
		}
		for _, r := range rs {
			if start < r.start+r.count && r.start < start+SubIDCount {
				t.Fatalf("the range at %d overlaps %s at %d+%d", start, r.name, r.start, r.count)
			}
		}
	})
}

// loginShell reads the shell of this user out of /etc/passwd. The user package
// does not hand it over, and that the flag took effect is the whole point.
func loginShell(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Split(l, ":")
		if len(f) == 7 && f[0] == name {
			return f[6]
		}
	}
	t.Fatalf("%s is in no line of /etc/passwd", name)
	return ""
}
