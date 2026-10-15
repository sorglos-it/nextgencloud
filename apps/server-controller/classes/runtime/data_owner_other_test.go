//go:build !linux

package runtime

import "testing"

// Who a file belongs to is a number only where NGC runs. Every case that asks
// skips itself outside Linux; this is here so the test package builds where it
// is written.
func ownerOf(t *testing.T, path string) (uid, gid uint32) {
	t.Helper()
	t.Skip("the owner of a file is a number of Linux")
	return 0, 0
}
