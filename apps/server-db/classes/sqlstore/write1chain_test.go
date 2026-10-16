package sqlstore_test

// The figure of the queue bound, held against the database and not against a
// copy of the same constant. busy_timeout is set in
// apps/server-db/classes/sqlite and is not readable from sqlstore, so the case
// asks the OPEN DATABASE for the pragma; a bound checked against a second
// spelling of the same number would not be checked at all.

import (
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
)

func TestQueueWaitIsOneChainOfTheDatabase(t *testing.T) {
	_, db, _ := write1Fixture(t, sqlstore.SQLite)

	var ms int64
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&ms); err != nil {
		t.Fatalf("the pragma could not be read, so nothing is held: %v", err)
	}
	if ms <= 0 {
		t.Fatalf("busy_timeout = %d; with no wait of the driver there is no chain to match", ms)
	}
	chain := time.Duration(sqlstore.MaxTries) * time.Duration(ms) * time.Millisecond
	if got := sqlstore.QueueWait(); got != chain {
		t.Errorf("the queue bound is %v; one chain of this database is %d x %d ms = %v",
			got, sqlstore.MaxTries, ms, chain)
	}
}
