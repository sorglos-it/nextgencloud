package dbwait_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
)

// TestEachDeadlineCarriesItsReason is the assurance: the deadline is bound to
// what it is FOR. Without the cause the two are one and the same
// context.DeadlineExceeded, and a user path that ran out is then no longer
// tellable from a database that is away one package further out.
func TestEachDeadlineCarriesItsReason(t *testing.T) {
	for _, c := range []struct {
		name string
		make func(context.Context) (context.Context, context.CancelFunc)
		want time.Duration
	}{
		{"user", dbwait.User, dbwait.UserWait},
		{"start", dbwait.Start, dbwait.StartWait},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := c.make(context.Background())
			defer cancel()
			d, ok := ctx.Deadline()
			if !ok {
				t.Fatal("no deadline")
			}
			// The figure and not only the fact: a deadline of an hour would
			// pass a check that asks whether there is one.
			if left := time.Until(d); left > c.want || left < c.want/2 {
				t.Fatalf("%v left of %v", left, c.want)
			}
			if got := context.Cause(ctx); got != nil {
				t.Fatalf("a cause before the deadline ran out: %v", got)
			}
			// Now let it run out, which is the only state the reason is for.
			short, cancel2 := context.WithTimeoutCause(context.Background(), time.Nanosecond, dbwait.ErrBusy)
			defer cancel2()
			<-short.Done()
			if !dbwait.Busy(context.Cause(short)) {
				t.Fatalf("the reason is gone: %v", context.Cause(short))
			}
			if !errors.Is(short.Err(), context.DeadlineExceeded) {
				t.Fatalf("Err() = %v", short.Err())
			}
		})
	}
}

// TestBusyIsNotEveryDeadline is the counter-measurement: Busy says yes to the
// reason of this package and to nothing else. A Busy that took every
// cancelled context would turn a caller who hung up into "the database is
// busy", and the answer would carry a Retry-After for nobody.
func TestBusyIsNotEveryDeadline(t *testing.T) {
	for _, err := range []error{nil, context.DeadlineExceeded, context.Canceled, errors.New("x")} {
		if dbwait.Busy(err) {
			t.Fatalf("Busy(%v) = true", err)
		}
	}
	if !dbwait.Busy(dbwait.ErrBusy) {
		t.Fatal("Busy of the reason itself = false")
	}
	// It is asked of the error, not of the context, so it survives a wrap on
	// the way out of two packages.
	if !dbwait.Busy(errors.Join(errors.New("state: read"), dbwait.ErrBusy)) {
		t.Fatal("Busy of a wrapped reason = false")
	}
}

// TestAUserPathWaitsLessThanTheStart holds the shape of the decision rather
// than the numbers: short where a person waits, long where the program comes
// up. The numbers themselves are measured (point 48: 80,6 s without any
// deadline), and whoever moves them has to move this line and say why.
func TestAUserPathWaitsLessThanTheStart(t *testing.T) {
	if dbwait.UserWait >= dbwait.StartWait {
		t.Fatalf("user %v, start %v", dbwait.UserWait, dbwait.StartWait)
	}
	// The start has to be able to sit out one whole retry chain of the
	// database -- busy_timeout 10 s times maxTries 8, measured at 80,6 s --
	// or a start that loses one race would not come up at all.
	if dbwait.StartWait < 81*time.Second {
		t.Fatalf("start %v is under one full retry chain", dbwait.StartWait)
	}
	// And the user path has to stay well under it, or it is no user path.
	if dbwait.UserWait > 10*time.Second {
		t.Fatalf("user %v is no longer short", dbwait.UserWait)
	}
}
