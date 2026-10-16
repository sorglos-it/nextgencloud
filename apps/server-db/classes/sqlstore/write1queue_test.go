package sqlstore

// The bound on the queue of the write channel. The channel itself is measured
// in write1_test.go; this case is about what it costs the writer who does NOT
// get the turn. The figure of the bound is held next door, in
// TestQueueWaitIsOneChainOfTheDatabase, which needs an open database and
// therefore cannot live in this package: both sqlite and migrate import
// sqlstore.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// TestAWriterGivesUpTheQueueInsteadOfWaitingForTheHolder is the assurance of
// the bound. Before it the second writer waited the whole chain of the first
// and then its own -- measured on D: on 16.10.2026, three writers of one
// program against a lock nobody releases: 80,491 s / 161,023 s / 241,667 s,
// against 80,642 / 81,551 / 82,209 s before the channel existed.
//
// It needs no database, and that is not thrift: a writer who gets no turn must
// come back WITHOUT having touched the pool, so a store whose db is nil is the
// sharpest form of the assurance -- if the writer reached the database at all,
// this case panics instead of passing.
//
// The queue is shortened for the case, because a case that really waited one
// chain would cost eighty seconds. What is measured here is the SHAPE (the
// writer comes back on the bound and not on the holder); the figure itself is
// held against the pragma of a real database next door.
func TestAWriterGivesUpTheQueueInsteadOfWaitingForTheHolder(t *testing.T) {
	st, ok := New(nil, SQLite).(*store)
	if !ok {
		t.Fatal("New does not give back this package's store")
	}
	if st.write1 == nil {
		t.Fatal("this store has no write channel, so there is no queue; nothing measured")
	}

	old := queueWait
	queueWait = 300 * time.Millisecond
	t.Cleanup(func() { queueWait = old })

	// One writer holds the turn far longer than the shortened bound, the way
	// a holder in a full retry chain does.
	done, err := st.takeWrite(context.Background())
	if err != nil {
		t.Fatalf("the first writer got no turn: %v", err)
	}
	defer done()
	// The case checks its own preparation: without a turn taken there is no
	// queue, and everything below would be green for nothing.
	if len(st.write1) != 1 {
		t.Fatal("no turn is held, so the writer below would get one at once; nothing measured")
	}

	ran := false
	start := time.Now()
	err = st.write(context.Background(), func(tx *sql.Tx) error { ran = true; return nil })
	el := time.Since(start)

	if ran {
		t.Error("the write ran although another one holds the turn")
	}
	if !errors.Is(err, errQueue) {
		t.Fatalf("a writer that got no turn came back with %v after %v, want errQueue", err, el)
	}
	if el > 4*queueWait {
		t.Fatalf("the writer waited %v, so it waited for the holder and not for the bound of %v",
			el, queueWait)
	}
	if el < queueWait/2 {
		t.Fatalf("the answer came after %v, so the queue was never entered -- this measures an "+
			"earlier wall and not the bound", el)
	}

	// And the reason of a caller who named one still wins: the bound must not
	// take his deadline away from him, because dbwait.Busy of the API reads
	// exactly that cause.
	ctx, cancel := context.WithTimeoutCause(context.Background(), 50*time.Millisecond, errTestOwn)
	defer cancel()
	if err := st.write(ctx, func(tx *sql.Tx) error { ran = true; return nil }); !errors.Is(err, errTestOwn) {
		t.Fatalf("a caller with his own deadline heard %v, want the reason he named", err)
	}
	if ran {
		t.Error("the write ran although another one holds the turn")
	}
}

// errTestOwn is the reason a caller names for his own deadline.
var errTestOwn = errors.New("the reason the caller named")
