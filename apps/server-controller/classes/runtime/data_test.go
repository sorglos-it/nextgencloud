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
