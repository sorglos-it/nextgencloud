//go:build linux

package node

import (
	"math"
	"syscall"
)

// diskFree is the space left for app data in bytes: the blocks a program
// without root privileges may still fill (Bavail), times the size of one. Not
// Bfree, which counts the reserve the file system keeps for root as well - what
// a status is about is the room the apps have.
//
// Everything that does not answer gives 0: a directory that is not there yet, a
// file system that says nothing, and a product that would not fit into the
// number the contract carries (tunnel.Status.DiskFree is signed and must not be
// negative). A status without a number is one an API takes; a wrong number is
// one an admin acts on.
func diskFree(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	// Bsize is signed and Bavail is not, so both are brought to the same width
	// before either is multiplied, and the multiplication is only done when its
	// result fits.
	size := int64(st.Bsize)
	if size <= 0 || st.Bavail > uint64(math.MaxInt64)/uint64(size) {
		return 0
	}
	return int64(st.Bavail) * size
}
