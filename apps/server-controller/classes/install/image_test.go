package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
)

// fileLog records what each of the two calls was asked for, in order, and
// makes the real chmod as well - a case that only recorded would measure its
// own recording, and the activation goes on from what the call left behind.
//
// The chown is NOT made: os.Chown answers "not supported by windows" for every
// path, and what is at stake is WHICH owner was asked for. The recorded uid
// and gid are what the case reads, and the real call keeps its own case on
// Linux (TestTheStagedImageIsHandedToTheAppOnDisk).
type fileLog struct {
	calls []string
	// chownErr is what the stand-in answers, for the cases about a handover
	// that fails.
	chownErr error
}

func (f *fileLog) note(s string) { f.calls = append(f.calls, s) }

// bendFiles puts the recorder in place of the two calls. newStand calls it for
// every stand, so no case of this package ever reaches the real os.Chown.
func bendFiles(t *testing.T) *fileLog {
	t.Helper()
	l := &fileLog{}
	old := files
	t.Cleanup(func() { files = old })
	files.chmod = func(p string, m os.FileMode) error {
		// The base name, because the path is a TempDir; and the accept-*
		// directory of an activation carries a random name (os.MkdirTemp), so
		// it is recorded under the one word it always is.
		name := filepath.Base(p)
		if strings.HasPrefix(name, "accept-") || strings.HasPrefix(name, "update-") {
			name = "accept"
		}
		l.note("chmod " + name + " " + m.Perm().String())
		return os.Chmod(p, m)
	}
	files.chown = func(p string, uid, gid int) error {
		l.note("chown " + filepath.Base(p) + " " + itoa(uid) + ":" + itoa(gid))
		return l.chownErr
	}
	return l
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// THE CASE OF THE 20.10.2026, second half: the engine reads the image as the
// app's user, so an activation opens the way to that one file and hands the
// file over - and does neither to anything else.
//
// What must be true, and it is read per path and not as "some chmod happened":
// the three directories on the way get 0711 (walk through, list nothing), and
// the tar goes to the app's uid and gid with 0400. A directory at 0700 is the
// wall this is about; a tar at 0644 would do the job too and is the thing this
// case refuses, because then every user of the machine could read every app's
// image.
func TestTheStagedImageIsHandedToTheApp(t *testing.T) {
	s := newStand(t)
	l := s.fileLog
	if _, err := s.activate(); err != nil {
		t.Fatalf("activate: %v", err)
	}
	// The order is the claim as much as the modes: the way is opened from the
	// outside in, and the file is handed over last.
	want := []string{
		"chmod staged -rwx--x--x",
		"chmod accept -rwx--x--x",
		"chmod files -rwx--x--x",
		"chmod services -rwx--x--x",
		"chown amd64.tar 4711:4712",
		"chmod amd64.tar -r--------",
	}
	if !slices.Equal(l.calls, want) {
		t.Fatalf("the activation asked for\n%q\nwant\n%q", l.calls, want)
	}
}

// A handover that fails ends the activation, and the journal names the step.
// It is not a line to swallow: an image the engine cannot read is an app that
// cannot start, and an activation that went on from here would leave a
// container that never comes up.
func TestAHandoverThatFailsEndsTheActivation(t *testing.T) {
	s := newStand(t)
	j := withJournal(s)
	s.fileLog.chownErr = fs.ErrPermission

	if _, err := s.activate(); err == nil {
		t.Fatal("the activation came off although the image was never handed over")
	}
	line := j.only(t)
	if line["step"] != "image" || line["reason"] != "no permission" {
		t.Errorf("the journal says step=%q reason=%q, want image / no permission",
			line["step"], line["reason"])
	}
}

// And it refuses to hand an image to root. The three Ensure calls of package
// runtime hold the same line, and this one runs after them - so it can only be
// reached with a user they already answered for, which is why a uid of 0 is an
// error here and not a question.
func TestNoImageIsHandedToRoot(t *testing.T) {
	for _, u := range []runtime.User{
		{Name: "ngc-a-00000000", UID: 0, GID: 4712},
		{Name: "ngc-a-00000000", UID: 4711, GID: 0},
	} {
		dir := t.TempDir()
		tar := filepath.Join(dir, "amd64.tar")
		if err := os.WriteFile(tar, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := handImage(dir, tar, u); !errors.Is(err, ErrInternal) {
			t.Errorf("uid=%d gid=%d: handImage = %v, want ErrInternal", u.UID, u.GID, err)
		}
	}
}
