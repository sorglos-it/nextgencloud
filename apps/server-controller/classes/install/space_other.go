//go:build !linux

package install

// How much room is left on a filesystem is a call of the system, and NGC runs
// on Debian and Ubuntu. This says so instead of answering a number that would
// decide what may be written: a wrong answer here is either a refusal of every
// package or a disk that fills up. Where it comes back, the bound stays the
// one the caller set (bound), and the arithmetic has its own case behind
// diskFree.
func freeSpace(dir string) (int64, error) { return 0, errNoSpaceCheck }
