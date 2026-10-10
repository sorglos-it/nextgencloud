//go:build linux

package install

import (
	"os"
	"path/filepath"
	"syscall"
)

// freeSpace is how many bytes an unprivileged writer still has where dir is.
//
// Bavail and not Bfree: a filesystem keeps a share for root, and this process
// is root - but what is checked here is whether a package of several gigabytes
// fits, and eating into that share is how a disk gets full enough that nothing
// else can be written. Bavail is the honest number for that question.
//
// The directory may not be there yet - config.StageDir is made by Accept - so
// the nearest parent that is there is the one asked: it is the same
// filesystem, which is what the question is about.
func freeSpace(dir string) (int64, error) {
	for {
		var st syscall.Statfs_t
		err := syscall.Statfs(dir, &st)
		if err == nil {
			// Bavail is in blocks of Bsize. Both are unsigned on every
			// architecture NGC is built for, and the product of them is the
			// size of a disk, so it fits an int64 by a wide margin.
			return int64(st.Bavail) * int64(st.Bsize), nil
		}
		if !os.IsNotExist(err) {
			return 0, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0, err
		}
		dir = parent
	}
}
