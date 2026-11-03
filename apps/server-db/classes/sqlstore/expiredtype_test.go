package sqlstore_test

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// bendExpires gives ngc_once.expires the byte type instead of the number type
// the migration gave it, keeping the rows. It is how a database looks whose
// schema does not match what the code reads out of it: a backup restored from
// another release, a hand-edited file, a migration whose replay met an object
// of its own name (W-1 of the review of 10.10.2026).
//
// It checks its own EFFECT and not its preparation, which is the rule of
// messen.md: not only that the column now says BLOB, but that the comparison
// the bolt in SQL rests on really has turned around. A test whose setup half
// worked would otherwise measure the untouched code and come out green.
func bendExpires(t *testing.T, db *sql.DB, kind string, value []byte) {
	t.Helper()
	for _, s := range []string{
		"ALTER TABLE ngc_once RENAME TO ngc_once_old",
		"CREATE TABLE ngc_once ( kind BLOB NOT NULL , token BLOB NOT NULL , data BLOB NOT NULL , " +
			"expires BLOB NOT NULL , used INTEGER NOT NULL , PRIMARY KEY ( kind , token ) )",
		"INSERT INTO ngc_once (kind, token, data, expires, used) " +
			"SELECT kind, token, data, CAST(expires AS TEXT), used FROM ngc_once_old",
		"DROP TABLE ngc_once_old",
		"CREATE INDEX ngc_once_expires ON ngc_once (expires)",
	} {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	var typeof string
	var stillGood int
	if err := db.QueryRowContext(ctx,
		"SELECT typeof(expires), (expires > ?) FROM ngc_once WHERE kind = ? AND token = ?",
		int64(expiredNow), []byte(kind), value).Scan(&typeof, &stillGood); err != nil {
		t.Fatalf("reading the effect back: %v", err)
	}
	if typeof != "text" || stillGood != 1 {
		t.Fatalf("the bend did not take: typeof(expires)=%q and (expires > %d) = %d, "+
			"so the comparison this test is about did not turn around",
			typeof, int64(expiredNow), stillGood)
	}
}

const (
	onceGoodUntil = 1790000100
	expiredNow    = 1790000200 // past onceGoodUntil
)

// TestOnceUseRefusesAnExpiredValueOnABentColumn is the case for the second
// bolt in OnceUse, and what it holds is a SECURITY promise: an expired
// one-time value is never handed out, not even on a database whose expires
// column carries the wrong type.
//
// Without that bolt the value comes back WITH ITS DATA and no error. Measured
// on 30.10.2026: the UPDATE decides with "expires > ?" in SQL, and SQLite
// orders every text after every number, so the comparison does not fail - it
// INVERTS. All four turn around on a text column, measured at one row whose
// value is 1000: ">5000" and ">=5000" answer 1 where they owe 0, "<5000" and
// "<=5000" answer 0 where they owe 1. A gate that means "allowed" as "greater
// than" therefore lets everything through.
//
// MariaDB 11.8.6 converts for the comparison and refused correctly in the same
// measurement, so this is one database's behaviour and not a rule of SQL - and
// SQLite is the side that matters most, because it is what a single-machine
// installation runs on.
func TestOnceUseRefusesAnExpiredValueOnABentColumn(t *testing.T) {
	s, db := openDB(t)
	value := bytes.Repeat([]byte{0x7e}, 30)
	if err := s.OnceCreate(ctx, dbapi.OnceTask, value, "u-admin", onceGoodUntil); err != nil {
		t.Fatal(err)
	}
	// The way to the line this test is about has to be open: on an untouched
	// column an expired value is already refused, so a green run here would
	// otherwise measure that earlier wall instead of the bolt.
	if _, err := s.OnceUse(ctx, dbapi.OnceTask, value, expiredNow); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("on an untouched column an expired value gives %v, want ErrNotFound", err)
	}

	// A value of its own, because the call above spent nothing but must not
	// be the one the real case runs on either way.
	bent := bytes.Repeat([]byte{0x7f}, 30)
	if err := s.OnceCreate(ctx, dbapi.OnceTask, bent, "u-admin", onceGoodUntil); err != nil {
		t.Fatal(err)
	}
	bendExpires(t, db, dbapi.OnceTask, bent)

	data, err := s.OnceUse(ctx, dbapi.OnceTask, bent, expiredNow)
	if !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("an expired value on a bent column gives data %q and error %v, want ErrNotFound", data, err)
	}
	if data != "" {
		t.Errorf("it handed out %q, and that is the whole point of the bolt", data)
	}
	// And a second time, because the bolt rolls the mark back: a value it
	// caught must stay caught rather than become usable once.
	if data, err := s.OnceUse(ctx, dbapi.OnceTask, bent, expiredNow); !errors.Is(err, dbapi.ErrNotFound) || data != "" {
		t.Errorf("second call gives data %q and error %v, want ErrNotFound and nothing", data, err)
	}
}

// TestOnceUseStillWorksOnABentColumn is the other side of the bolt: it refuses
// what is expired and nothing else. A bolt that also broke the value still in
// time would be a worse fault than the one it fixes, and a case that only
// measured the refusal could not tell the two apart.
func TestOnceUseStillWorksOnABentColumn(t *testing.T) {
	s, db := openDB(t)
	value := bytes.Repeat([]byte{0x7d}, 30)
	if err := s.OnceCreate(ctx, dbapi.OnceTask, value, "u-admin", onceGoodUntil); err != nil {
		t.Fatal(err)
	}
	bendExpires(t, db, dbapi.OnceTask, value)

	// now is before onceGoodUntil, so this value is good.
	const inTime = onceGoodUntil - 100
	data, err := s.OnceUse(ctx, dbapi.OnceTask, value, inTime)
	if err != nil || data != "u-admin" {
		t.Errorf("a value in time on a bent column gives data %q and error %v, want u-admin and no error", data, err)
	}
	// Spent exactly once, as ever.
	if _, err := s.OnceUse(ctx, dbapi.OnceTask, value, inTime); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("second use gives %v, want ErrUsed", err)
	}
}
