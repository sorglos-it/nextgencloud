// Package dbwait holds the two things a deadline on a database call of this
// API is for: how long it waits, and what the waiting MEANT when it ran out.
//
// Measured on 13.10.2026 (docs/vertraege-v1.2.md, point 48): without a
// deadline of its own a store call of this API waits busy_timeout (10 s,
// apps/server-db/classes/sqlite/sqlite.go) times maxTries (8,
// apps/server-db/classes/sqlstore/store.go) -- 80,6 s against a lock a second
// program held, on C: and on the NAS alike. Then it fails with the driver's
// "database is locked", and that error carries no name of ours: httpapi.Fault
// does not read it, so it falls out of the last arm as E_UNREACHABLE, the one
// word the contract also uses for a database that is AWAY. 80 seconds of
// waiting and then the wrong word.
//
// Both halves are mended with one mechanism. A caller says what the waiting
// is for -- User on a path a person is waiting on, Start on the path that
// brings the program up -- and the deadline carries ErrBusy as its CAUSE
// (context.WithTimeoutCause). apps/server-db hands that cause back in place
// of the bare context.DeadlineExceeded, so the one place that picks a code can
// tell the two apart: a database that is busy passes by itself in a moment and
// gets Retry-After, one that is away does not.
//
// NOT MEASURED, and it is the honest half of ErrBusy: that contention is the
// only way to run past these deadlines. Measured is what the queue costs (four
// blocked writers make the first READ of the same process take 9,513 s, where
// two make it 0,008 s) and that an unloaded call takes milliseconds. A store
// call of this API that runs seconds without a queue in front of it has not
// been seen -- but nothing here proves there is no such call, so ErrBusy says
// "the database did not come free in time", not "another program is writing".
package dbwait

import (
	"context"
	"errors"
	"time"
)

// ErrBusy is the cause of a deadline of this package that ran out: the
// database did not come free in time. It is not a database that is away and
// not one that is broken -- waiting a moment and asking again is the whole of
// what it takes, which is why it is the one error of this package that earns
// a Retry-After.
var ErrBusy = errors.New("dbwait: the database did not come free in time")

const (
	// UserWait is the deadline of a path a person is waiting on. It is the
	// figure of the measurement and not a round number: four blocked writers
	// take 9,513 s off the first read of a process, two take 0,008 s, so a
	// user path that waits seconds is already in the queue and waiting longer
	// buys nothing but a longer wait. Five seconds leaves room for one slow
	// read on a Pi and cuts the measured 80,6 s to a sixteenth.
	//
	// It is SHORTER than httpapi.readLimit (15 s), the time a body may take
	// to trickle in, and that is a difference worth naming: the deadline runs
	// from the start of the request, so a client that needs more than
	// UserWait to send its body meets it on the first store call afterwards
	// and hears "busy" where it heard E_PROTOCOL at 15 s before. Only a
	// client that drips gets there -- Body's own note puts MaxRequest in
	// fifteen seconds at about 35 kbit/s -- and the socket's read deadline,
	// which is what answers a dripper, is untouched by any of this.
	UserWait = 5 * time.Second
	// StartWait is the deadline of the start, where nobody is waiting at a
	// screen and the one thing that must not happen is a program that neither
	// comes up nor stops. Point 32 decided that shape for the controller: a
	// service that hangs at the start is worse than one that does not start,
	// and the restart belongs to the service file. Ninety seconds is the
	// measured 80,6 s of one full retry chain with room over it, so a start
	// that loses one race still comes up, and one that is locked out for good
	// ends with a word instead of hanging.
	StartWait = 90 * time.Second
)

// User returns ctx with the deadline of a user path and ErrBusy as its cause.
// The caller cancels it, as with every context of the standard library.
func User(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, UserWait, ErrBusy)
}

// Start returns ctx with the deadline of the start and ErrBusy as its cause.
func Start(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, StartWait, ErrBusy)
}

// Busy reports whether err is the database being busy rather than away: one
// of this package's deadlines ran out. It is the question httpapi asks before
// it writes the answer, and it is asked of the ERROR and not of the context,
// so a handler that hands the error on through two packages can still ask it.
func Busy(err error) bool { return errors.Is(err, ErrBusy) }
