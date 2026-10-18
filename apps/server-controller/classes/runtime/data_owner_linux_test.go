//go:build linux

package runtime

import (
	"os"
	"syscall"
	"testing"
)

// ownerOf is who a directory belongs to. The os package hands the numbers over
// only through the system's own structure, and who owns the data directory of
// an app is the thing TestEnsureDataSetsAPolicy is about.
func ownerOf(t *testing.T, path string) (uid, gid uint32) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no owner for %q here", path)
	}
	return st.Uid, st.Gid
}
