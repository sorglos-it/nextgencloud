package sqlite

import (
	"strings"
	"testing"
	"time"
)

// shortBusy is the deadline the three cases below run on, and it is the whole
// point of Options: they wait out the SAME wall - a neighbour holds the write
// lock of the file - but on a clock a test can sit out. What they assert is
// the refusal and the sentence; how long the wait lasts is asserted once, in
// TestPragmas against DefaultBusyTimeout and in
// TestTheBusyDeadlineOfTheOptionsReachesTheFile against the option.
//
// 250 ms and not 1 ms: it has to be long enough that a lock taken a moment
// later is still waited for rather than missed, and short enough to be free.
const shortBusy = 250 * time.Millisecond

// TestTheBusyDeadlineOfTheOptionsReachesTheFile is what keeps the three
// shortened cases honest. They are only worth anything if the number in
// Options is really the one SQLite waits with - a knob that is read, checked
// and then dropped would make all three of them green in a fraction of a
// second while measuring nothing at all.
//
// So: the pool is opened with a deadline nothing else in this package uses,
// and the file is asked what it waits. The answer is in milliseconds.
func TestTheBusyDeadlineOfTheOptionsReachesTheFile(t *testing.T) {
	const want = 1234 * time.Millisecond
	if want == DefaultBusyTimeout {
		t.Fatal("the option carries the default, so this case cannot tell the two apart")
	}
	db, err := OpenDBWith(testConfig(t, "state.db"), Options{BusyTimeout: want})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Every connection of the pool, not just the first: the PRAGMA is run by
	// the connector on each one, and a pool that set it once would leave the
	// others at SQLite's own default of zero.
	for i := range maxOpenConns {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		defer conn.Close()
		var got int64
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&got); err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		if got != want.Milliseconds() {
			t.Errorf("connection %d waits %d ms, want %d", i, got, want.Milliseconds())
		}
	}
}

// TestTheZeroOptionsIsWhatNGCOpensWith: OpenDB takes no options, so the only
// way the production deadline can change by accident is through the zero
// value. Asked of the file, not of the constant.
func TestTheZeroOptionsIsWhatNGCOpensWith(t *testing.T) {
	db, err := OpenDBWith(testConfig(t, "state.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var got int64
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != DefaultBusyTimeout.Milliseconds() {
		t.Errorf("the zero Options waits %d ms, want the default of %d",
			got, DefaultBusyTimeout.Milliseconds())
	}
}

// TestABusyDeadlineBelowAMillisecondIsRefused. SQLite takes whole
// milliseconds, so anything under one would arrive as 0 - "do not wait at
// all", which is a different file than the one NGC opens. The pool is refused
// instead, and a refused pool is nil: a test that mistypes its deadline gets
// an error, not a case that passes in no time.
func TestABusyDeadlineBelowAMillisecondIsRefused(t *testing.T) {
	for _, bad := range []time.Duration{-time.Second, -1, 1, 999 * time.Microsecond} {
		db, err := OpenDBWith(testConfig(t, "state.db"), Options{BusyTimeout: bad})
		if err == nil {
			db.Close()
			t.Errorf("a busy deadline of %v was accepted", bad)
			continue
		}
		if db != nil {
			t.Errorf("%v: a pool came back beside the error", bad)
		}
		if !strings.HasPrefix(err.Error(), "sqlite: ") {
			t.Errorf("%v: the error does not say which part refused: %q", bad, err)
		}
	}
}

// TestBackupWithRefusesTheSameDeadline: the knob goes through Backup too, and
// a refusal there has to come back as a refusal and not as a backup made with
// the default. Without this the mistyped deadline of a measurement would
// quietly be a ten second one again.
func TestBackupWithRefusesTheSameDeadline(t *testing.T) {
	cfg := testConfig(t, "state.db")
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE t (a INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err = BackupWith(cfg, cfg.Path+".backup", Options{BusyTimeout: -time.Second})
	if err == nil {
		t.Fatal("a backup was made with a refused busy deadline")
	}
	if !strings.Contains(err.Error(), "busy timeout") {
		t.Errorf("the error does not name the refused deadline: %q", err)
	}
}
