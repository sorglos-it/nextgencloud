//go:build !linux

package node

// Asking a file system how much room is left is a call of Linux, and NGC is
// built for Linux alone. Everywhere else the answer is 0, which is what a
// status of a controller that cannot measure it says; the file is here so that
// the tests of this package also run where they are written.
func diskFree(string) int64 { return 0 }
