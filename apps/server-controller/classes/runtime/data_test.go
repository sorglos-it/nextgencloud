package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// aKey is a key of the shape the controller has: config.Controller.DBKey is 64
// bytes. Nothing of it reaches a file or a log, here or in the code under test.
var aKey = bytes.Repeat([]byte{0x5a}, 64)

// dataIn points the package at a directory of this test instead of
// config.AppDataDir, so that nothing of a case lands in the data of this
// machine.
func dataIn(t *testing.T) string {
	t.Helper()
	was := paths
	t.Cleanup(func() { paths = was })
	paths.appData = filepath.Join(t.TempDir(), "apps")
	// The group of the apps folder is a stand-in here, and it has to be: the
	// real one runs groupadd, and a case may not make a group on the machine
	// it runs on. Measured on the real VM on 19.10.2026 - without this the
	// test binary ran as an ordinary user and two cases died of "groupadd:
	// Permission denied", which is the apparatus and not the code. What the
	// group IS called and HOW it is made is measured where it belongs
	// (TestEnsureAppsGroupBuildsTheArgumentList).
	// The gid is the one THIS process already has, where it is not root: the
	// group of the apps folder is really set here (EnsureData sets the group
	// and nothing else, with -1 for the owner), and a group the caller does
	// not belong to is one only root may set.
	gid := testAppsGID
	if os.Geteuid() != 0 {
		gid = os.Getegid()
	}
	paths.appsGID = func(context.Context) (int, error) { return gid, nil }
	return paths.appData
}

// fscryptDir is a directory on a filesystem that can do fscrypt, or nothing. A
// test cannot make a filesystem - it needs mkfs and a mount, and both are the
// harness's job - so the harness names one in NGC_TEST_FSCRYPT_DIR.
//
// It then asks the kernel, and that question is not a formality: a kernel built
// without CONFIG_FS_ENCRYPTION answers every one of these ioctls with
// "operation not supported", whatever the filesystem under it was made with.
// The kernel of WSL on the machine this is written on is such a kernel, so this
// case does not run there, and what it would have measured is unmeasured and
// not claimed. The skip says which of the two is missing.
func fscryptDir(t *testing.T) string {
	t.Helper()
	if goruntime.GOOS != "linux" {
		t.Skip("fscrypt is a thing of the Linux kernel")
	}
	dir := os.Getenv("NGC_TEST_FSCRYPT_DIR")
	if dir == "" {
		t.Skip("no filesystem with fscrypt here: set NGC_TEST_FSCRYPT_DIR to a mount point of an ext4 or f2fs with encryption")
	}
	if os.Geteuid() != 0 {
		t.Skip("adding a key to a filesystem and owning a directory to another user needs root")
	}
	if _, err := addKey(dir, bytes.Repeat([]byte{0x11}, 64)); errors.Is(err, ErrNoFscrypt) {
		t.Skip("this kernel or this filesystem cannot encrypt a directory: " + err.Error())
	} else if err != nil {
		t.Fatalf("the filesystem of NGC_TEST_FSCRYPT_DIR answered %v", err)
	}
	was := paths
	t.Cleanup(func() { paths = was })
	paths.appData = filepath.Join(dir, "apps")
	t.Cleanup(func() { _ = os.RemoveAll(paths.appData) })
	return paths.appData
}

// The key of an app is tied to both halves and to nothing else: change the
// server's key or the app, and it is a different key. It is 64 bytes, which is
// what AES-256-XTS takes, and it never leaves this process - the test shows
// that by deriving it twice and comparing, not by reading it anywhere.
func TestTheKeyOfAnAppIsTiedToTheAppAndTheServer(t *testing.T) {
	a, err := appKey(aKey, "org.example.a")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 {
		t.Fatalf("len = %d, want 64", len(a))
	}
	again, err := appKey(aKey, "org.example.a")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, again) {
		t.Fatal("the same app and the same server key gave two keys")
	}
	b, err := appKey(aKey, "org.example.b")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two apps share one key")
	}
	other := bytes.Repeat([]byte{0x5b}, 64)
	c, err := appKey(other, "org.example.a")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, c) {
		t.Fatal("two servers give one app the same key")
	}
	// A key that is no key is refused instead of stretched: a short one would
	// make a 64 byte key all the same, and it would protect nothing.
	for _, k := range [][]byte{nil, {}, bytes.Repeat([]byte{1}, 31)} {
		if _, err := appKey(k, "org.example.a"); !errors.Is(err, ErrKey) {
			t.Errorf("appKey of %d bytes = %v, want ErrKey", len(k), err)
		}
	}
}

func TestTheDataCallsRefuseWhatIsNoApp(t *testing.T) {
	dataIn(t)
	ctx := context.Background()
	for _, app := range []string{"", "no-dot", "UPPER.CASE", "org.example.a/b",
		"../etc", "org.example.a\nb", "org.example.a:b"} {
		if _, err := EnsureData(ctx, app, User{Name: "ngc-a", UID: 1, GID: 1}, aKey); !errors.Is(err, ErrApp) {
			t.Errorf("EnsureData(%q) = %v, want ErrApp", app, err)
		}
		if err := UnlockData(ctx, app, aKey); !errors.Is(err, ErrApp) {
			t.Errorf("UnlockData(%q) = %v, want ErrApp", app, err)
		}
		if err := DropData(ctx, app); !errors.Is(err, ErrApp) {
			t.Errorf("DropData(%q) = %v, want ErrApp", app, err)
		}
		if _, err := DataSize(app); !errors.Is(err, ErrApp) {
			t.Errorf("DataSize(%q) = %v, want ErrApp", app, err)
		}
	}
}

// A user of 0 is refused here for the same reason the engine refuses it: the
// data directory of an app would become root's own, and an app that writes into
// /data would write as root.
func TestEnsureDataRefusesRoot(t *testing.T) {
	dataIn(t)
	for _, u := range []User{{Name: "ngc-a", UID: 0, GID: 1}, {Name: "ngc-a", UID: 1, GID: 0}} {
		if _, err := EnsureData(context.Background(), "org.example.a", u, aKey); !errors.Is(err, ErrIdentity) {
			t.Errorf("EnsureData with %+v = %v, want ErrIdentity", u, err)
		}
	}
}

// DataSize counts the bytes under the directory of this app and of no other,
// and it does not follow a link out of it: a link is a way to make a quota of
// one app answer for the files of something else.
func TestDataSizeCountsOnlyWhatIsThere(t *testing.T) {
	linuxOnly(t)
	root := dataIn(t)
	dir := filepath.Join(root, "org.example.a")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "org.example.b"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b"), make([]byte, 24), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "org.example.b", "big"), make([]byte, 9000), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(big, make([]byte, 7000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(big, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	n, err := DataSize("org.example.a")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1024 {
		t.Fatalf("size = %d, want 1024 (the link and the other app are not ours)", n)
	}
	// An app without a directory has no data, and that is not an error: it is
	// the answer before the first install.
	if n, err = DataSize("org.example.nothing"); err != nil || n != 0 {
		t.Fatalf("size of an app without a directory = %d, %v", n, err)
	}
}

// DropData takes the directory and everything in it away, and a directory that
// is not there is no error - the call has to be able to clean up after a failed
// install.
func TestDropDataTakesTheDirectoryAway(t *testing.T) {
	linuxOnly(t)
	root := dataIn(t)
	dir := filepath.Join(root, "org.example.a")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DropData(context.Background(), "org.example.a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the directory is still there: %v", err)
	}
	if err := DropData(context.Background(), "org.example.a"); err != nil {
		t.Fatalf("a second DropData = %v, want nil", err)
	}
}

// The one that does the thing it is about: on a filesystem that can fscrypt the
// directory really gets a policy, a second call changes nothing, the directory
// belongs to the app's user with 0700, and DataSize counts what is written into
// it.
func TestEnsureDataSetsAPolicy(t *testing.T) {
	root := fscryptDir(t)
	app := "org.example.a"
	// nobody, the one unprivileged user every Debian and Ubuntu has.
	u := User{Name: UserName(app), UID: 65534, GID: 65534}
	dir, err := EnsureData(context.Background(), app, u, aKey)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, app) {
		t.Fatalf("dir = %q", dir)
	}
	id, err := policyOf(dir)
	if err != nil {
		t.Fatalf("no policy on %q: %v", dir, err)
	}
	if id == ([16]byte{}) {
		t.Fatal("the policy names no key")
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want 0700", fi.Mode().Perm())
	}
	if owner, group := ownerOf(t, dir); owner != u.UID || group != u.GID {
		t.Fatalf("owner = %d:%d, want %d:%d", owner, group, u.UID, u.GID)
	}
	// A second call changes nothing and keeps the same policy.
	if _, err = EnsureData(context.Background(), app, u, aKey); err != nil {
		t.Fatal(err)
	}
	again, err := policyOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Fatalf("the policy changed: %x -> %x", id, again)
	}
	// With the key in place the directory can be written and read, and the
	// bytes are counted.
	if err = os.WriteFile(filepath.Join(dir, "a-file-with-a-name"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := DataSize(app)
	if err != nil || n != 100 {
		t.Fatalf("size = %d, %v", n, err)
	}
	// UnlockData is what happens after a restart: it adds the key again and
	// leaves the policy alone. Twice in a row has to be as harmless as once,
	// because nothing tells it whether the key is already there.
	for range 2 {
		if err = UnlockData(context.Background(), app, aKey); err != nil {
			t.Fatal(err)
		}
	}
	if again, err = policyOf(dir); err != nil || again != id {
		t.Fatalf("after UnlockData: %x, %v", again, err)
	}
	// A key that is not this app's is refused by the kernel, not accepted
	// quietly: it would make a second policy under the same name impossible to
	// tell from the right one.
	if err = UnlockData(context.Background(), app, bytes.Repeat([]byte{0x5b}, 64)); err == nil {
		// Adding another key is allowed - what must never happen is that it
		// becomes the key of this directory.
		if again, err = policyOf(dir); err != nil || again != id {
			t.Fatalf("a foreign key changed the policy: %x, %v", again, err)
		}
	}
	if err = DropData(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the directory is still there: %v", err)
	}
}

// A filesystem that cannot do fscrypt gets no app with data, and it says so in
// a sentence a user can read. Measured where it is always true: a tmpfs.
func TestEnsureDataSaysSoWhenTheFilesystemCannot(t *testing.T) {
	linuxOnly(t)
	if os.Getenv("NGC_TEST_PLAIN_DIR") == "" {
		t.Skip("set NGC_TEST_PLAIN_DIR to a directory on a filesystem without encryption")
	}
	was := paths
	t.Cleanup(func() { paths = was })
	paths.appData = filepath.Join(os.Getenv("NGC_TEST_PLAIN_DIR"), "apps")
	t.Cleanup(func() { _ = os.RemoveAll(paths.appData) })
	app := "org.example.a"
	u := User{Name: UserName(app), UID: 65534, GID: 65534}
	_, err := EnsureData(context.Background(), app, u, aKey)
	if !errors.Is(err, ErrNoFscrypt) {
		t.Fatalf("err = %v, want ErrNoFscrypt", err)
	}
	// And nothing is left behind that a later call would take for a ready
	// directory: an unencrypted data directory is the one thing this must not
	// make.
	if _, err := os.Stat(filepath.Join(paths.appData, app)); !os.IsNotExist(err) {
		t.Fatalf("an unencrypted directory was left behind: %v", err)
	}
}

// NoFscryptText is the sentence a user reads, and it is German, because every
// text of NGC that a person sees is.
func TestTheSentenceForAUserIsGerman(t *testing.T) {
	if NoFscryptText == "" {
		t.Fatal("there is no sentence")
	}
	for _, word := range []string{"Der Datenträger", "fscrypt", "ext4", "f2fs"} {
		if !bytes.Contains([]byte(NoFscryptText), []byte(word)) {
			t.Errorf("the sentence does not say %q: %q", word, NoFscryptText)
		}
	}
}

// fakeFscrypt puts two calls of this test in place of the two the kernel does.
// They answer the way a kernel that can encrypt answers - an identifier that is
// not zero, and a policy that is set - and they touch nothing. Everything after
// them in EnsureData is the real thing.
//
// This is not a stand-in for fscrypt itself: what the two really do has its own
// cases (TestEnsureDataSetsAPolicy against a real filesystem, and the ioctl
// numbers against the kernel's header). What this makes measurable is the part
// after them, which no kernel here can be made to reach.
func fakeFscrypt(t *testing.T) {
	t.Helper()
	was := paths
	t.Cleanup(func() { paths = was })
	paths.addKey = func(mount string, key []byte) ([keyIdentifierSize]byte, error) {
		// The one thing the real one refuses, refused here as well, so a case
		// cannot pass through this on a key the kernel would have thrown out.
		if len(key) != keyBytes {
			return [keyIdentifierSize]byte{}, ErrKey
		}
		var id [keyIdentifierSize]byte
		id[0] = 1
		return id, nil
	}
	paths.setPolicy = func(dir string, id [keyIdentifierSize]byte) error { return nil }
}

// A context that is already cancelled stops both calls, and it stops them before
// anything exists. That one line is what the commit before this one moved out of
// a branch of the engine up to here; without it EnsureData walks on and answers
// with whatever the filesystem says - which on this machine is ErrNoFscrypt, so
// a cancelled call would read as a disk that cannot encrypt.
func TestTheDataCallsStopForACancelledContext(t *testing.T) {
	root := dataIn(t)
	app := "org.example.a"
	u := User{Name: UserName(app), UID: 65534, GID: 65534}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EnsureData(ctx, app, u, aKey); !errors.Is(err, context.Canceled) {
		t.Errorf("EnsureData = %v, want context.Canceled", err)
	}
	// And nothing is left behind that a later call would find and take for a
	// ready data directory.
	if _, err := os.Stat(filepath.Join(root, app)); !os.IsNotExist(err) {
		t.Errorf("a cancelled EnsureData left a directory behind: %v", err)
	}
	if err := UnlockData(ctx, app, aKey); !errors.Is(err, context.Canceled) {
		t.Errorf("UnlockData = %v, want context.Canceled", err)
	}
	if err := DropData(ctx, app); !errors.Is(err, context.Canceled) {
		t.Errorf("DropData = %v, want context.Canceled", err)
	}
	// A key that is no key is refused before the context is asked, because a
	// cancelled call with a broken key is still a broken key. Said here so the
	// order of the two is a measured one and not a reading of the source.
	if _, err := EnsureData(ctx, app, u, []byte("short")); !errors.Is(err, ErrKey) {
		t.Errorf("EnsureData with a short key = %v, want ErrKey", err)
	}
}

// The data directory belongs to the app's user and not to root. That is the
// core promise of this file, and until this case it had no run on this machine:
// the only other case that reaches the chown needs a kernel with
// CONFIG_FS_ENCRYPTION and skips itself everywhere here.
//
// What this case cannot do without root: move the uid. Giving a file to another
// user is root's alone. So as a person it moves the GROUP to a supplementary
// group of this user, which a user may do for a file of their own - a real
// change, and one that is gone the moment the chown is. As root it does the
// whole thing, to nobody.
func TestEnsureDataGivesTheDirectoryToTheAppsUser(t *testing.T) {
	linuxOnly(t)
	dataIn(t)
	fakeFscrypt(t)
	app := "org.example.a"
	u := User{Name: UserName(app), UID: 65534, GID: 65534}
	if os.Geteuid() != 0 {
		groups, err := os.Getgroups()
		if err != nil {
			t.Fatal(err)
		}
		mine := uint32(os.Getegid())
		other := mine
		for _, g := range groups {
			if uint32(g) != mine && g != 0 {
				other = uint32(g)
				break
			}
		}
		if other == mine {
			t.Skip("this user is in no second group, so no change of owner is allowed here; the uid half needs a root harness")
		}
		u.UID, u.GID = uint32(os.Geteuid()), other
	}
	if u.UID == 0 || u.GID == 0 {
		t.Fatalf("the case built a user of root: %+v", u)
	}
	dir, err := EnsureData(context.Background(), app, u, aKey)
	if err != nil {
		t.Fatal(err)
	}
	if owner, group := ownerOf(t, dir); owner != u.UID || group != u.GID {
		t.Fatalf("owner = %d:%d, want %d:%d", owner, group, u.UID, u.GID)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want 0700", fi.Mode().Perm())
	}
}

// What the chmod after the chown really does: a data directory an earlier,
// half-finished call left behind with another mode is tightened to 0700.
// os.Mkdir does not touch the mode of a directory that is already there, so
// without that line the app would keep the wider one.
func TestEnsureDataTightensTheModeOfADirectoryAlreadyThere(t *testing.T) {
	linuxOnly(t)
	root := dataIn(t)
	fakeFscrypt(t)
	app := "org.example.a"
	dir := filepath.Join(root, app)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// By hand and not through MkdirAll: the umask takes bits off a mode given
	// to mkdir, and a chmod is what sets one.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	u := User{Name: UserName(app), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	if u.UID == 0 || u.GID == 0 {
		u = User{Name: UserName(app), UID: 65534, GID: 65534}
	}
	if _, err := EnsureData(context.Background(), app, u, aKey); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want 0700 (the directory was 0777 before the call)", fi.Mode().Perm())
	}
}
