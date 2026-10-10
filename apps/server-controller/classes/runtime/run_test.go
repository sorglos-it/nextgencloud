package runtime

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

// anAppUser is the user of an app as EnsureUser would answer it: a name of the
// grammar the engine holds (validName) and two ids that are not root's.
func anAppUser() User { return User{Name: "ngc-notes-0123abcd", UID: 4711, GID: 4712} }

// runIn points the package at a directory of this test instead of
// config.RunDir, and records what it would have handed over to whom: the real
// chown is a call of Linux and needs root, so without a stand-in nothing after
// it could be measured where this is written.
//
// The recorded owner is the one EnsureRun asked for, not the one the
// filesystem then had - that second one is what
// TestEnsureRunHandsTheDirectoryToTheApp reads, on Linux and as root.
func runIn(t *testing.T) (dir string, owner func() (int, int, int)) {
	t.Helper()
	was := paths
	t.Cleanup(func() { paths = was })
	paths.runDir = filepath.Join(t.TempDir(), "run", "ngc")
	var mu sync.Mutex
	uid, gid, calls := -1, -1, 0
	paths.chown = func(_ string, u, g int) error {
		mu.Lock()
		defer mu.Unlock()
		uid, gid, calls = u, g, calls+1
		return nil
	}
	return paths.runDir, func() (int, int, int) {
		mu.Lock()
		defer mu.Unlock()
		return uid, gid, calls
	}
}

// The directory of the app under config.RunDir is made, it is 0700, and it is
// handed to the app's user - not to root, which is who this process is.
func TestEnsureRunMakesTheDirectoryOfTheApp(t *testing.T) {
	base, owner := runIn(t)
	u := anAppUser()
	dir, err := EnsureRun(t.Context(), u)
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}
	if want := base + "/" + u.Name; dir != want {
		t.Fatalf("EnsureRun answered %q, want %q", dir, want)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the directory is not there: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("%q is no directory", dir)
	}
	if goruntime.GOOS == "linux" && fi.Mode().Perm() != 0o700 {
		t.Fatalf("the mode is %v, want 0700", fi.Mode().Perm())
	}
	uid, gid, calls := owner()
	if calls != 1 {
		t.Fatalf("the directory was handed over %d times, want once", calls)
	}
	if uid != int(u.UID) || gid != int(u.GID) {
		t.Fatalf("the directory was handed to %d:%d, want %d:%d", uid, gid, u.UID, u.GID)
	}
}

// This is the restart. config.RunDir is a tmpfs, so a boot leaves nothing of it
// - not the directory of the app, and not config.RunDir itself. The case takes
// the whole tree away, which is what the tmpfs does, and calls again: the
// directory is back and belongs to the app again.
func TestEnsureRunMakesTheDirectoryAgainAfterARestart(t *testing.T) {
	base, owner := runIn(t)
	u := anAppUser()
	dir, err := EnsureRun(t.Context(), u)
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}
	// Everything a restart takes: the app's directory and the tmpfs above it.
	if err := os.RemoveAll(base); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the restart left something behind: %v", err)
	}
	again, err := EnsureRun(t.Context(), u)
	if err != nil {
		t.Fatalf("EnsureRun after the restart: %v", err)
	}
	if again != dir {
		t.Fatalf("after the restart the directory is %q, was %q", again, dir)
	}
	if _, err := os.Stat(again); err != nil {
		t.Fatalf("the directory is not back: %v", err)
	}
	if _, _, calls := owner(); calls != 2 {
		t.Fatalf("the directory was handed over %d times, want twice", calls)
	}
}

// A directory an earlier, half-finished activation left behind with another
// mode and another owner is tightened, not believed. Both halves matter: the
// mode, because os.Mkdir does not touch the mode of a directory that is already
// there, and the owner, because a directory of root is one every podman call of
// that app fails on.
func TestEnsureRunTightensADirectoryAlreadyThere(t *testing.T) {
	base, owner := runIn(t)
	u := anAppUser()
	dir := base + "/" + u.Name
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureRun(t.Context(), u); err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}
	if goruntime.GOOS == "linux" {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Fatalf("the mode of a directory already there is %v, want 0700", fi.Mode().Perm())
		}
	}
	uid, gid, calls := owner()
	if calls != 1 || uid != int(u.UID) || gid != int(u.GID) {
		t.Fatalf("a directory already there was handed to %d:%d in %d calls, want %d:%d once",
			uid, gid, calls, u.UID, u.GID)
	}
}

// What EnsureRun refuses, and that it writes nothing for any of it. A name
// outside the grammar of the engine would become a path, and a uid of 0 would
// make a directory an app writes into as root.
func TestEnsureRunRefusesWhatIsNoAppUser(t *testing.T) {
	cases := map[string]struct {
		u    User
		want error
	}{
		"no name":            {User{UID: 1, GID: 1}, ErrName},
		"name with a slash":  {User{Name: "ngc/../etc", UID: 1, GID: 1}, ErrName},
		"name with a dot":    {User{Name: "..", UID: 1, GID: 1}, ErrName},
		"name as an option":  {User{Name: "-rm", UID: 1, GID: 1}, ErrName},
		"name too long":      {User{Name: "ngc-" + string(make([]byte, 0)) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UID: 1, GID: 1}, ErrName},
		"uid of root":        {User{Name: "ngc-a", UID: 0, GID: 1}, ErrIdentity},
		"gid of root":        {User{Name: "ngc-a", UID: 1, GID: 0}, ErrIdentity},
		"both of them root":  {User{Name: "ngc-a", UID: 0, GID: 0}, ErrIdentity},
		"name with a NUL in": {User{Name: "ngc-a\x00b", UID: 1, GID: 1}, ErrName},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			base, owner := runIn(t)
			dir, err := EnsureRun(t.Context(), c.u)
			if !errors.Is(err, c.want) {
				t.Fatalf("EnsureRun answered %v, want %v", err, c.want)
			}
			if dir != "" {
				t.Fatalf("EnsureRun answered the path %q with an error", dir)
			}
			if _, _, calls := owner(); calls != 0 {
				t.Fatalf("something was handed over %d times for a refusal", calls)
			}
			// Nothing was written at all: not even config.RunDir itself, which
			// a refusal before the first MkdirAll is what this reads.
			if _, err := os.Stat(base); !os.IsNotExist(err) {
				t.Fatalf("a refusal left %q behind: %v", base, err)
			}
		})
	}
}

// DropRun takes the directory away and says nothing about one that is not
// there: after a restart there is none, and after a failed activation there may
// be one.
func TestDropRunTakesTheDirectoryAndMissesNone(t *testing.T) {
	_, _ = runIn(t)
	u := anAppUser()
	dir, err := EnsureRun(t.Context(), u)
	if err != nil {
		t.Fatal(err)
	}
	if err := DropRun(u.Name); err != nil {
		t.Fatalf("DropRun: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the directory is still there: %v", err)
	}
	if err := DropRun(u.Name); err != nil {
		t.Fatalf("DropRun on a directory that is not there: %v", err)
	}
	if err := DropRun("../etc"); !errors.Is(err, ErrName) {
		t.Fatalf("DropRun of a name that is none answered %v, want ErrName", err)
	}
}

// The real chown, on a real filesystem, as root. This is the one case that
// measures what the stand-in above only records, and it says out loud which of
// the two things it needs is missing when it skips.
func TestEnsureRunHandsTheDirectoryToTheApp(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("who owns a directory is a number of Linux")
	}
	if os.Geteuid() != 0 {
		t.Skip("handing a directory to another user needs root")
	}
	was := paths
	t.Cleanup(func() { paths = was })
	paths.runDir = filepath.Join(t.TempDir(), "run", "ngc")
	u := anAppUser()
	dir, err := EnsureRun(t.Context(), u)
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}
	uid, gid := ownerOf(t, dir)
	if uid != u.UID || gid != u.GID {
		t.Fatalf("the directory belongs to %d:%d, want %d:%d", uid, gid, u.UID, u.GID)
	}
}

// subIDFiles puts files of this test in place of /etc/subuid and /etc/subgid.
// It needs no Linux and no root: ensureSubIDs reads and writes two files and
// nothing else.
func subIDFiles(t *testing.T) {
	t.Helper()
	was := paths
	t.Cleanup(func() { paths = was })
	dir := t.TempDir()
	paths.subuid = filepath.Join(dir, "subuid")
	paths.subgid = filepath.Join(dir, "subgid")
	for _, p := range []string{paths.subuid, paths.subgid} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// rangesOf is every line of the file as name:start:count, in the order they
// stand there.
func rangesOf(t *testing.T, file string) []subIDRange {
	t.Helper()
	_, rs, err := readSubIDs(file)
	if err != nil {
		t.Fatalf("readSubIDs: %v", err)
	}
	return rs
}

// Two activations at once must not hand out the same range, and must not lose
// a line. This is the case for the lock around the read-modify-write of
// /etc/subuid, and it is not a case `go test -race` could ever answer: nothing
// here is one piece of memory touched from two goroutines - the file is the
// shared state.
//
// paths.subIDWindow is what makes the measurement repeatable instead of a coin
// toss. It holds every caller in the window between "I read the file and picked
// the lowest free range" and "I write it back", until either all of them are in
// that window or the wait runs out. With the lock exactly one caller is ever in
// it, so each one waits its own wait out and the answer is right; without the
// lock all of them stand in it together, pick the same range and overwrite each
// other - and then this case is red.
func TestSubIDsHandOutEveryRangeOnlyOnceUnderALock(t *testing.T) {
	subIDFiles(t)
	const apps = 6
	// Long enough that the window is really open for whoever may enter it, and
	// short enough that six serialized waits are a fraction of a second. It is
	// a wait, not a synchronisation: nothing of the result depends on it being
	// long enough, only the sharpness of the measurement does.
	const window = 60 * time.Millisecond
	var mu sync.Mutex
	inside := 0
	most := 0
	paths.subIDWindow = func() {
		mu.Lock()
		inside++
		if inside > most {
			most = inside
		}
		mu.Unlock()
		time.Sleep(window)
		mu.Lock()
		inside--
		mu.Unlock()
	}
	var wg sync.WaitGroup
	errs := make([]error, apps)
	for i := range apps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = ensureSubIDs("ngc-app" + strconv.Itoa(i))
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("ensureSubIDs for app %d: %v", i, err)
		}
	}
	// The sharpness of the measurement, said as a number: with the lock only
	// one caller is ever in the window. If this were above 1 the lock would not
	// hold, and the two checks below would be the ones to fail.
	if most != 1 {
		t.Fatalf("%d callers were in the window between read and write at once, want 1", most)
	}
	for _, file := range []string{paths.subuid, paths.subgid} {
		rs := rangesOf(t, file)
		if len(rs) != apps {
			t.Fatalf("%s holds %d ranges, want %d: a line was lost", file, len(rs), apps)
		}
		seenStart := map[uint64]string{}
		seenName := map[string]bool{}
		for _, r := range rs {
			if other, twice := seenStart[r.start]; twice {
				t.Fatalf("%s gives the range at %d to %s and to %s", file, r.start, other, r.name)
			}
			seenStart[r.start] = r.name
			if seenName[r.name] {
				t.Fatalf("%s names %s twice", file, r.name)
			}
			seenName[r.name] = true
			if r.count != SubIDCount {
				t.Fatalf("%s gives %s %d ids, want %d", file, r.name, r.count, SubIDCount)
			}
		}
	}
	// And the ranges of both files are the same ranges: a caller that slipped
	// in between the two files would give one user two different ones.
	gids := map[string]uint64{}
	for _, r := range rangesOf(t, paths.subgid) {
		gids[r.name] = r.start
	}
	for _, r := range rangesOf(t, paths.subuid) {
		if gids[r.name] != r.start {
			t.Fatalf("%s has its uid range at %d and its gid range at %d", r.name, r.start, gids[r.name])
		}
	}
}

// The same lock holds while a user's lines are taken out again: a hand-out that
// slipped in between the read and the write would be dropped with them.
func TestSubIDsStayWhileAnotherUserIsDropped(t *testing.T) {
	subIDFiles(t)
	for _, name := range []string{"ngc-a", "ngc-b"} {
		if err := ensureSubIDs(name); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	inside, most := 0, 0
	paths.subIDWindow = func() {
		mu.Lock()
		inside++
		if inside > most {
			most = inside
		}
		mu.Unlock()
		time.Sleep(60 * time.Millisecond)
		mu.Lock()
		inside--
		mu.Unlock()
	}
	var wg sync.WaitGroup
	wg.Add(2)
	var dropErr, addErr error
	go func() { defer wg.Done(); dropErr = dropSubIDs("ngc-a") }()
	go func() { defer wg.Done(); addErr = ensureSubIDs("ngc-c") }()
	wg.Wait()
	if dropErr != nil || addErr != nil {
		t.Fatalf("drop: %v, add: %v", dropErr, addErr)
	}
	if most > 1 {
		t.Fatalf("%d callers were in the window at once, want at most 1", most)
	}
	for _, file := range []string{paths.subuid, paths.subgid} {
		names := map[string]bool{}
		for _, r := range rangesOf(t, file) {
			names[r.name] = true
		}
		if names["ngc-a"] {
			t.Fatalf("%s still names the dropped user", file)
		}
		if !names["ngc-b"] || !names["ngc-c"] {
			t.Fatalf("%s lost a line: %v", file, names)
		}
	}
}
