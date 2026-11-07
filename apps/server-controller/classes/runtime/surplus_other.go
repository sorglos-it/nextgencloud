//go:build !linux

package runtime

import "errors"

// Ending a single task by (number, start time) needs a handle on a process,
// and that is a thing of Linux. Nothing of NGC runs anywhere else - the stub
// is here so the package builds where the cases are written, and it refuses
// rather than ending something by number alone.
func killTask(pid int, start uint64) error {
	return errors.New("runtime: a task can only be ended on Linux")
}
