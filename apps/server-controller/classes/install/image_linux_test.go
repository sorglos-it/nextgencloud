//go:build linux

package install

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
)

// The handover, read off the DISK and not off a recorder - with the real
// os.Chmod and the real os.Chown, which no other case of this package reaches.
//
// That difference is why this case exists. The two calls of image.go go
// through `files`, and every case that bends them measures the number the call
// was MADE with; the comment on `files` says so itself. Here nothing is bent:
// the staged layout is built as Accept and unpackApp build it - 0700 all the
// way down and a 0600 tar - handImage runs over it, and then the modes and the
// owner are stat'ed.
//
// What must be true is not "the numbers are 0711 and 0400" but what those
// numbers are for: every directory on the way can be ENTERED by someone who is
// not root and can be LISTED by nobody, and the tar belongs to the app's user
// and is readable by nobody else. All four are read.
func TestTheStagedImageIsHandedToTheAppOnDisk(t *testing.T) {
	// chown to a FOREIGN uid needs root; without root a process may only chown
	// to itself, and handImage refuses uid 0 on purpose - so root hands the
	// file to 1 and everybody else to themselves.
	//
	// WHAT THAT COSTS, and it is said here rather than left for a reader to
	// find: without root the owner half of this case is hollow. The file
	// already belongs to that uid, so a handImage with no chown at all leaves
	// exactly the same thing on the disk. Measured on 20.10.2026: the mutant
	// with the chown taken out LIVES against this case as a normal user and
	// dies against the recorded one (TestTheStagedImageIsHandedToTheApp, which
	// reads WHICH owner was asked for). So as a normal user this case carries
	// the modes and the other one carries the owner; as root it carries both.
	// The modes are not hollow either way - they are 0700 before and have to
	// be 0711 after, whoever runs this.
	uid, gid := os.Getuid(), os.Getgid()
	foreign := false
	if uid == 0 {
		uid, gid, foreign = 1, 1, true
	}
	if gid == 0 {
		gid = 1
	}
	if !foreign {
		t.Logf("not root: the owner is handed to this process's own %d:%d, so the "+
			"owner half of this case is hollow here - the recorded case carries it",
			uid, gid)
	}

	stage := filepath.Join(t.TempDir(), "staged")
	// The layout and the modes of Accept and unpackApp, built here rather than
	// taken from them: a case that called them would measure them, and what is
	// measured here is what handImage does to what they leave behind.
	tarDir := filepath.Join(stage, "accept-0123456789", "files", "services")
	if err := os.MkdirAll(tarDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tarPath := filepath.Join(tarDir, "amd64.tar")
	if err := os.WriteFile(tarPath, []byte("not really a tar"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The preparation is read back: MkdirAll and WriteFile both apply the
	// umask, and a directory that was already 0711 would make this case
	// measure nothing at all.
	for dir := tarDir; ; dir = filepath.Dir(dir) {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Fatalf("the preparation left %s at %v, not 0700 - nothing is measured here",
				dir, fi.Mode().Perm())
		}
		if dir == stage {
			break
		}
	}

	if err := handImage(stage, tarPath, runtime.User{
		Name: "ngc-a-00000000", UID: uint32(uid), GID: uint32(gid),
	}); err != nil {
		t.Fatalf("handImage: %v", err)
	}

	for dir := tarDir; ; dir = filepath.Dir(dir) {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		perm := fi.Mode().Perm()
		if perm&0o001 == 0 {
			t.Errorf("%s is %v - the app's user cannot walk to its own image", dir, perm)
		}
		if perm&0o044 != 0 {
			t.Errorf("%s is %v - somebody else can list what is being staged", dir, perm)
		}
		if dir == stage {
			break
		}
	}

	fi, err := os.Stat(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	if int(st.Uid) != uid || int(st.Gid) != gid {
		t.Errorf("the image belongs to %d:%d, want %d:%d", st.Uid, st.Gid, uid, gid)
	}
	if perm := fi.Mode().Perm(); perm&0o400 == 0 {
		t.Errorf("the image is %v - its own user cannot read it", perm)
	} else if perm&0o077 != 0 {
		t.Errorf("the image is %v - somebody other than its app can read it", perm)
	}
}
