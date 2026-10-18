//go:build linux

package runtime

import (
	"os"
	"syscall"
	"testing"
)

// "Written whole or not at all" means the file is replaced and not overwritten,
// and the one thing that tells the two apart is the inode: a write into the
// existing file keeps it, a new file put in its place changes it. A reader that
// has the old file open goes on reading a whole old file either way, which is
// the point.
func TestSubIDsReplaceTheFileInsteadOfWritingIntoIt(t *testing.T) {
	fakeUseradd(t)
	before := inodeOf(t, paths.subuid)
	if err := ensureSubIDs("ngc-a"); err != nil {
		t.Fatal(err)
	}
	after := inodeOf(t, paths.subuid)
	if before == after {
		t.Fatalf("inode %d both times: the file was written into, not replaced", before)
	}
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no inode for %q here", path)
	}
	return st.Ino
}
