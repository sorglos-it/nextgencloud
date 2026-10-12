//go:build !linux

package terminal

import (
	"context"
	"io"
	"time"
)

// runShell answers errNoPTY everywhere but Linux, so the package builds on the
// machine this is developed on. Everything of this app that can be measured
// without a pseudo terminal - the frames, the two pumps, the idle clock, the
// three refusals of run and the gate in front of the app - is measured there;
// what needs the pty says so out loud and skips (see the head of
// terminal_test.go).
func runShell(_ context.Context, _ io.Reader, _ io.Writer, _ time.Duration) (int, error) {
	return 0, errNoPTY
}
