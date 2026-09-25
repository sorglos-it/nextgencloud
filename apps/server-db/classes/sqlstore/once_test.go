package sqlstore_test

import (
	"bytes"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
)

// TestLockTakeAgainWithTheSameTime is the trap of the UPDATE-then-INSERT
// shape: MariaDB counts changed rows by default, so taking a lock again with
// the very same time would look like "no row" and end in an INSERT that
// fails. The driver has to be set to count matched rows instead, and no
// driver in this repository is: no MariaDB connection is opened anywhere yet,
// so there is nowhere for the flag to stand. The task that writes that DSN
// owes it.
//
// Until then this test holds nothing of the sort, and that is all this run
// measures: SQLite counts every row its UPDATE went through, so the second
// take passes here whatever MariaDB would answer.
func TestLockTakeAgainWithTheSameTime(t *testing.T) {
	s := open(t)
	const now, until = 1790000000, 1790000060
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, until); err != nil || !ok {
		t.Fatalf("first: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, until); err != nil || !ok {
		t.Errorf("same owner, same time: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", now, until); err != nil || ok {
		t.Errorf("another owner: %v, %v", ok, err)
	}
}

// TestLockFreeOnlyTheOwner: the DELETE of LockFree names the name and the
// owner, and that second half is the whole of the method - one that dropped
// every row of a name would take the rollout lock away from whoever holds it,
// from another API and on another node. Only one other call in this package
// reaches an open database with it, and it gives the lock back as the very
// owner that took it (TestOnceAndLockBounds); the three in
// TestALongValueNeverReachesTheDatabase run against a closed one. None of
// them can tell the owner half from nothing, so without this test it is free
// to fall away unnoticed.
//
// It is measured the only way a lock can be: by asking a third owner whether
// it is still held.
func TestLockFreeOnlyTheOwner(t *testing.T) {
	s := open(t)
	const now, until = 1790000000, 1790000060
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, until); err != nil || !ok {
		t.Fatalf("take: %v, %v", ok, err)
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, "api-2"); err != nil {
		t.Fatalf("another owner gives it back: %v", err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-3", now, until); err != nil || ok {
		t.Errorf("after another owner gave it back: %v, %v, want false", ok, err)
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, "api-1"); err != nil {
		t.Fatalf("the owner gives it back: %v", err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-3", now, until); err != nil || !ok {
		t.Errorf("after the owner gave it back: %v, %v, want true", ok, err)
	}
}

// TestLockTakeWhenTheTimeIsReached: a lock is free again exactly when its time
// is reached, not a second later. That is the until <= ? of the statement, and
// it is measured here on a row that is really there - LockTake refuses an
// until at or below the now it is given, so both cases of that bound in
// TestOnceAndLockBounds, the one exactly at the now and the one below it, are
// answered before the database ever sees them.
func TestLockTakeWhenTheTimeIsReached(t *testing.T) {
	s := open(t)
	const now, until = 1790000000, 1790000060
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, until); err != nil || !ok {
		t.Fatalf("take: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", until-1, until+60); err != nil || ok {
		t.Errorf("one second before the time is reached: %v, %v, want false", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", until, until+60); err != nil || !ok {
		t.Errorf("exactly when the time is reached: %v, %v, want true", ok, err)
	}
}

// TestOnceUseTellsApart: an unknown or expired value is ErrNotFound, a second
// use is ErrUsed. The two must not be mixed up, because the API answers a
// login with them.
func TestOnceUseTellsApart(t *testing.T) {
	s := open(t)
	value := bytes.Repeat([]byte{0x53}, 32)
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, value, "u-admin", 1790000100); err != nil {
		t.Fatal(err)
	}
	if data, err := s.OnceUse(ctx, dbapi.OnceNonce, value, 1790000000); err != nil || data != "u-admin" {
		t.Fatalf("use: %q, %v", data, err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, value, 1790000000); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("second use: %v", err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, bytes.Repeat([]byte{1}, 32), 1790000000); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	other := bytes.Repeat([]byte{2}, 32)
	if err := s.OnceCreate(ctx, dbapi.OnceTask, other, "", 1790000100); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceTask, other, 1790000100); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("expired: %v", err)
	}
}

// TestOnceUseTellsTheKindsApart: the key is (kind, token), so one token may
// stand under two kinds at once, and both reads after the UPDATE have to name
// the kind as well - the one that gives the data back (onceRead) and the one
// that says why nothing matched (onceState). A value that exists only once
// measures neither of them: take the kind out of either statement and every
// other test of this package still comes through green.
//
// onceState is read only when the UPDATE matched nothing, so it needs an
// expired row of one kind beside a valid row of the other; with two rows in
// the same state both answers are the same and the kind cannot be seen at
// all. Each kind takes the expired part in turn below, and the nonce row is
// written first both times, so no order a read without the kind might fall
// back on lets both cases through.
func TestOnceUseTellsTheKindsApart(t *testing.T) {
	s := open(t)
	const now, late, early int64 = 1790000150, 1790000200, 1790000100
	value := bytes.Repeat([]byte{4}, 32)
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, value, "u-nonce", late); err != nil {
		t.Fatal(err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceTask, value, "u-task", late); err != nil {
		t.Fatal(err)
	}
	// onceRead: each use gives back its own kind's data, not the other one's.
	if data, err := s.OnceUse(ctx, dbapi.OnceTask, value, now); err != nil || data != "u-task" {
		t.Errorf("the task value: %q, %v, want u-task", data, err)
	}
	if data, err := s.OnceUse(ctx, dbapi.OnceNonce, value, now); err != nil || data != "u-nonce" {
		t.Errorf("the nonce value: %q, %v, want u-nonce", data, err)
	}
	// onceState: the reason for a miss comes from the row of its own kind.
	for _, c := range []struct {
		spent       string
		token       byte
		nonce, task int64
	}{
		{dbapi.OnceTask, 5, late, early},
		{dbapi.OnceNonce, 6, early, late},
	} {
		token := bytes.Repeat([]byte{c.token}, 32)
		if err := s.OnceCreate(ctx, dbapi.OnceNonce, token, "n", c.nonce); err != nil {
			t.Fatal(err)
		}
		if err := s.OnceCreate(ctx, dbapi.OnceTask, token, "t", c.task); err != nil {
			t.Fatal(err)
		}
		if _, err := s.OnceUse(ctx, c.spent, token, now); !errors.Is(err, dbapi.ErrNotFound) {
			t.Errorf("the expired %s value beside a valid one of the other kind: %v, want ErrNotFound", c.spent, err)
		}
	}
}

// TestAnExpiredAndSpentValueIsNotFound pins what the contract leaves open. It
// names both answers for OnceUse and does not order them, so a value that is
// spent and past its time could be either; two stores of one contract that
// disagree here would make the conformance test of task 7 impossible to
// write. The order is taken from memstore, the reference beside this store,
// and taken by running it instead of by reading it.
//
// The two do not agree everywhere, and this test says nothing about the rest:
// memstore has no columns and so no bounds, so an input past MaxKey or
// MaxText is refused here and taken there. That difference belongs to the
// stores and not to the contract, and it stands on the same list as this case.
//
// This one test came with the code and not before it, unlike the rest of this
// file: the question only turned up while the switch of OnceUse was read back
// against its own comment.
func TestAnExpiredAndSpentValueIsNotFound(t *testing.T) {
	const expires, after = 1790000100, 1790000200
	for name, s := range map[string]dbapi.Store{"sqlstore": open(t), "memstore": memstore.New()} {
		value := bytes.Repeat([]byte{7}, 32)
		if err := s.OnceCreate(ctx, dbapi.OnceNonce, value, "u-admin", expires); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if data, err := s.OnceUse(ctx, dbapi.OnceNonce, value, expires-1); err != nil || data != "u-admin" {
			t.Fatalf("%s, first use: %q, %v", name, data, err)
		}
		if _, err := s.OnceUse(ctx, dbapi.OnceNonce, value, after); !errors.Is(err, dbapi.ErrNotFound) {
			t.Errorf("%s, spent and expired: %v, want ErrNotFound", name, err)
		}
	}
}

// TestOnlyOneUseGetsTheValue: of concurrent OnceUse calls for one value
// exactly one may come back with the data. OnceUse decides in a single
// UPDATE, so this holds; a store that reads the row first and writes after it
// hands the same one-time value to several callers at once - a login taken
// twice, a task run twice.
//
// It measures that here and now, on SQLite, without -race and without
// MariaDB. The size of the pool is not what decides: in that shape the read
// and the write are two separate calls through database/sql, each takes a
// connection and gives it back again, and against such a store the probe is
// red at a pool of one just as at the four this store opens.
//
// How wide the machine is does decide, and that is why there are 32 racers
// and 400 rounds and not 8 and 40. Against such a store, ten runs each, the
// smaller numbers caught none of ten at GOMAXPROCS 1 and 2 and three of ten
// at 4 - on a two-core CI runner, and on the Pi that sqlite.go builds this
// store for, the very shape this test is here to catch would have come
// through silently. With these numbers it is five of five from two
// processors upwards, at a cost of about three seconds.
//
// The floor, so that nobody reads more into a green run than is there: at
// GOMAXPROCS 1 even 32 by 400 caught none of five here and one of five for
// a second reader. These are counts of a scattering, not a bound, and no run
// of this may be leaned on at that width. A single-threaded runner
// measures none of this, whatever the numbers say.
//
// What it does not catch, said plainly so nobody takes it for more: a store
// that puts those same two steps inside one transaction comes through green
// here - measured, against exactly such a store. SQLite's own transactions
// save it, and nothing this test does can tell that apart from a store that
// is right.
//
// That shape, and this one on a machine too narrow for it, are held instead
// by TestOnceUseDecidesWithItsWrite, which reads the source and asks whether
// the first statement OnceUse gives the database writes. Both were measured
// against it: the transactional one it caught while every other test of this
// package stayed green, and the one without a transaction it caught at a
// single processor, where this test measures nothing.
func TestOnlyOneUseGetsTheValue(t *testing.T) {
	const racers, rounds = 32, 400
	s := open(t)
	for round := range rounds {
		value := append(bytes.Repeat([]byte{0x5a}, 30), byte(round>>8), byte(round))
		if err := s.OnceCreate(ctx, dbapi.OnceTask, value, "u-admin", 1790000100); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		got := make([]string, racers)
		errs := make([]error, racers)
		for i := range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				got[i], errs[i] = s.OnceUse(ctx, dbapi.OnceTask, value, 1790000000)
			}()
		}
		close(start)
		wg.Wait()
		ok, used, other := 0, 0, 0
		for i := range racers {
			switch {
			case errs[i] == nil:
				ok++
				if got[i] != "u-admin" {
					t.Errorf("round %d: data %q, want u-admin", round, got[i])
				}
			case errors.Is(errs[i], dbapi.ErrUsed):
				used++
			default:
				other++
				t.Logf("round %d, racer %d: %v", round, i, errs[i])
			}
		}
		if ok != 1 || used != racers-1 {
			t.Fatalf("round %d: %d got the value, %d ErrUsed, %d other - want 1 and %d",
				round, ok, used, other, racers-1)
		}
	}
}

// TestOnceAndLockBounds nails every bound of these two from both sides: the
// longest key and the shortest span still go through, one byte more and one
// second less do not. A bound measured only from the far side could sit
// anywhere.
func TestOnceAndLockBounds(t *testing.T) {
	s := open(t)
	key := strings.Repeat("k", sqlstore.MaxKey)
	text := strings.Repeat("o", sqlstore.MaxText)
	const now, expires = 1790000000, 1790000100
	if err := s.OnceCreate(ctx, key, []byte(key), key, expires); err != nil {
		t.Errorf("kind, value and data of %d bytes each: %v", sqlstore.MaxKey, err)
	}
	// The shortest span a lock may be taken for is one second.
	if ok, err := s.LockTake(ctx, key, text, now, now+1); err != nil || !ok {
		t.Errorf("name of %d and owner of %d bytes: %v, %v", sqlstore.MaxKey, sqlstore.MaxText, ok, err)
	}
	// LockFree on the bound too, and it has to reach the database: the row is
	// really deleted, so another owner gets the lock at the same now, which a
	// call that quietly gave nil would not do.
	if err := s.LockFree(ctx, key, text); err != nil {
		t.Errorf("LockFree, name of %d and owner of %d bytes: %v", sqlstore.MaxKey, sqlstore.MaxText, err)
	}
	if ok, err := s.LockTake(ctx, key, "api-1", now, now+1); err != nil || !ok {
		t.Errorf("after LockFree on the bound: %v, %v, want true", ok, err)
	}
	for name, err := range map[string]error{
		"empty kind":     s.OnceCreate(ctx, "", []byte("v"), "", expires),
		"empty value":    s.OnceCreate(ctx, dbapi.OnceNonce, nil, "", expires),
		"kind too long":  s.OnceCreate(ctx, key+"x", []byte("v"), "", expires),
		"value too long": s.OnceCreate(ctx, dbapi.OnceNonce, []byte(key+"x"), "", expires),
		"data too long":  s.OnceCreate(ctx, dbapi.OnceNonce, []byte("v"), key+"x", expires),
	} {
		if !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("OnceCreate, %s: %v, want ErrInvalid", name, err)
		}
	}
	for name, c := range map[string]struct {
		name, owner string
		now, until  int64
	}{
		"empty name":       {"", "api-1", now, now + 60},
		"empty owner":      {dbapi.LockUpdate, "", now, now + 60},
		"name too long":    {key + "x", "api-1", now, now + 60},
		"owner too long":   {dbapi.LockUpdate, text + "x", now, now + 60},
		"until is now":     {dbapi.LockUpdate, "api-1", now, now},
		"until before now": {dbapi.LockUpdate, "api-1", now, now - 1},
	} {
		if ok, err := s.LockTake(ctx, c.name, c.owner, c.now, c.until); ok || !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("LockTake, %s: %v, %v, want false and ErrInvalid", name, ok, err)
		}
	}
}

// TestALongValueNeverReachesTheDatabase: a value the column cannot hold was
// never written by OnceCreate, so OnceUse answers it in Go, and LockFree
// gives a lock back that is not there. Otherwise the two databases would part
// company over it - SQLite compares any length and finds nothing, MariaDB is
// handed a value past its packet - and a bound that is looked at after the
// work is no bound.
//
// The database is closed first, so the measurement is not "the same answer"
// but "without asking": a value of the right size reaches the closed database
// and comes back as an error, one past the bound never gets that far.
func TestALongValueNeverReachesTheDatabase(t *testing.T) {
	s := open(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", sqlstore.MaxKey+1)
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, []byte("v"), 1790000000); err == nil || errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("a value of the right size did not reach the database: %v", err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, []byte(long), 1790000000); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("OnceUse, long value: %v, want ErrNotFound", err)
	}
	if _, err := s.OnceUse(ctx, long, []byte("v"), 1790000000); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("OnceUse, long kind: %v, want ErrNotFound", err)
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, "api-1"); err == nil {
		t.Fatal("a name of the right size did not reach the database")
	}
	if err := s.LockFree(ctx, long, "api-1"); err != nil {
		t.Errorf("LockFree, long name: %v", err)
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, strings.Repeat("x", sqlstore.MaxText+1)); err != nil {
		t.Errorf("LockFree, long owner: %v", err)
	}
}

// TestOncePurgeFreesTheValueAgain measures both sides of the purge: the value
// that expires exactly at now goes, the one a second later stays. That the
// row is really gone is measured by writing the same value again - a value
// still there gives ErrUsed although nobody ever used it.
func TestOncePurgeFreesTheValueAgain(t *testing.T) {
	s := open(t)
	const now = 1790000100
	gone, stays := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	if err := s.OnceCreate(ctx, dbapi.OnceTask, gone, "a", now); err != nil {
		t.Fatal(err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceTask, stays, "b", now+1); err != nil {
		t.Fatal(err)
	}
	if err := s.OncePurge(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceTask, gone, "c", now+60); err != nil {
		t.Errorf("the value that expired at now was not freed: %v", err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceTask, stays, "d", now+60); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("the value that expires later: %v, want ErrUsed", err)
	}
	if data, err := s.OnceUse(ctx, dbapi.OnceTask, stays, now); err != nil || data != "b" {
		t.Errorf("use: %q, %v", data, err)
	}
}

// onceSeed is one input of the fuzz target below.
type onceSeed struct {
	kind  string
	value []byte
	data  string
}

// onceSeeds are the fixed inputs of FuzzOnceCreate. Five of them are values a
// caller really writes, the other five are what the bounds are there for.
func onceSeeds() []onceSeed {
	edge := strings.Repeat("y", sqlstore.MaxKey)
	long := strings.Repeat("x", sqlstore.MaxKey+1)
	return []onceSeed{
		{dbapi.OnceNonce, bytes.Repeat([]byte{0x53}, 32), "u-admin"},
		{dbapi.OnceTask, bytes.Repeat([]byte{1}, 32), ""},
		{dbapi.OncePair, bytes.Repeat([]byte{2}, 32), "ctl-1"},
		{dbapi.OnceJoin, []byte{0}, "\x00"},
		{edge, []byte(edge), edge},
		{"", []byte("v"), ""},
		{dbapi.OnceNonce, nil, ""},
		{long, []byte("v"), ""},
		{dbapi.OnceNonce, []byte(long), ""},
		{dbapi.OnceNonce, []byte("v"), long},
	}
}

// TestOnceCreateTakesTheSeeds counts how many of the seeds are taken, and it
// does that here over the fixed list - never inside the fuzz target. A share
// counted while the target runs says only what the fuzzer chose to send, and
// it sends what widens the coverage, which are the refusals.
func TestOnceCreateTakesTheSeeds(t *testing.T) {
	s := open(t)
	taken := 0
	for _, c := range onceSeeds() {
		switch err := s.OnceCreate(ctx, c.kind, c.value, c.data, 1790000100); {
		case err == nil:
			taken++
		case !errors.Is(err, dbapi.ErrInvalid):
			t.Errorf("%.20q: %v", c.kind, err)
		}
	}
	if taken != 5 {
		t.Errorf("%d of the ten seeds were taken, and five of them are meant to be", taken)
	}
}

// FuzzOnceCreate: whatever comes in, OnceCreate refuses it as ErrInvalid, or
// it takes it - the first time by writing a row that OnceUse finds again and
// gives back unchanged, a second time with ErrUsed. Which of the two it has
// to be is decided from the lengths alone and measured on every input, so
// nothing the fuzzer sends can make the target say less.
func FuzzOnceCreate(f *testing.F) {
	for _, c := range onceSeeds() {
		f.Add(c.kind, c.value, c.data)
	}
	// One store for the whole run: opening an encrypted file and migrating it
	// per input would measure the migrations, not this.
	s := open(f)
	f.Fuzz(func(t *testing.T, kind string, value []byte, data string) {
		const expires = 1790000100
		fits := kind != "" && len(value) > 0 && len(kind) <= sqlstore.MaxKey &&
			len(value) <= sqlstore.MaxKey && len(data) <= sqlstore.MaxKey
		err := s.OnceCreate(ctx, kind, value, data, expires)
		if errors.Is(err, dbapi.ErrInvalid) == fits {
			t.Fatalf("kind %d, value %d, data %d bytes: fits is %v and the answer was %v",
				len(kind), len(value), len(data), fits, err)
		}
		if !fits {
			return
		}
		if errors.Is(err, dbapi.ErrUsed) {
			return // the same kind and value came a second time
		}
		if err != nil {
			t.Fatal(err)
		}
		if got, err := s.OnceUse(ctx, kind, value, expires-1); err != nil || got != data {
			t.Fatalf("%d bytes of data came back as %d bytes: %v", len(data), len(got), err)
		}
	})
}

// onceLosesItsRow takes away the row an UPDATE on ngc_once has just touched,
// so that everything the same transaction wants to read of it afterwards
// finds nothing. It is SQL of the test's own, planted on a database of its
// own, the same way countChangedRows in user_test.go plants the other way of
// counting: no driver has to be rebuilt to put a store in front of a failure
// it cannot otherwise be put in front of.
const onceLosesItsRow = `CREATE TRIGGER ngc_once_loses_its_row AFTER UPDATE ON ngc_once
FOR EACH ROW BEGIN DELETE FROM ngc_once WHERE kind = NEW.kind AND token = NEW.token; END`

// onceRow is how many rows of ngc_once carry that value and what stands in
// their used column, asked of the database. used is -1 where there is no row,
// so a missing row and an unused one can never be read for one another.
func onceRow(t *testing.T, db *sql.DB, value []byte) (rows int, used int64) {
	t.Helper()
	used = -1
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM ngc_once WHERE kind = ? AND token = ?",
		[]byte(dbapi.OnceTask), value).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows == 1 {
		if err := db.QueryRowContext(ctx, "SELECT used FROM ngc_once WHERE kind = ? AND token = ?",
			[]byte(dbapi.OnceTask), value).Scan(&used); err != nil {
			t.Fatal(err)
		}
	}
	return rows, used
}

// TestAFailedWriteTakesBackWhatItWrote: store.write rolls the transaction
// back when its body answers an error, so a method that wrote and then failed
// leaves nothing behind.
//
// OnceUse is the one method in this package where that rollback has any work
// to do, and that is why it is the one under test here. UserPut cannot show
// it: all seven of its ways out answer before anything of theirs is written,
// so there is never anything to take back - measured, one exit at a time.
// OnceUse writes the mark first, on purpose, because the UPDATE is what
// decides; only then does it read the row for the data it hands out. Between
// those two the transaction can fail, and then the mark may not stand: the
// caller was told the value was not his, and the next caller has to be able
// to get it.
//
// The failure is planted, because nothing a caller may pass reaches that gap.
// The trigger removes the row the UPDATE just marked, so the SELECT behind it
// finds none, OnceUse hands that error up, and store.write is left with a
// transaction that has written. The fixture is measured first, on a second
// value of its own: marked by hand, the row has to be gone, or the trigger
// never fired and this test would hold nothing.
//
// Measured, and this is the whole point of the test: with write rolling back,
// the row is there again and unused (rows left: 1, used: 0). With write
// committing in the error case instead, the row is gone for good (rows left:
// 0, used: -1) - the value is spent and nobody ever got it. That mutation was
// run against the whole package on aa718c6 and again here, and it is this
// test alone that goes red with it; before it, nothing in the package did.
func TestAFailedWriteTakesBackWhatItWrote(t *testing.T) {
	s, db := openDB(t)
	value := bytes.Repeat([]byte{0x5a}, 32)
	fixture := bytes.Repeat([]byte{0x5b}, 32)
	for _, v := range [][]byte{value, fixture} {
		if err := s.OnceCreate(ctx, dbapi.OnceTask, v, "u-admin", 1790000100); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, onceLosesItsRow); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE ngc_once SET used = ? WHERE kind = ? AND token = ?",
		int64(1), []byte(dbapi.OnceTask), fixture); err != nil {
		t.Fatal(err)
	}
	if rows, _ := onceRow(t, db, fixture); rows != 0 {
		t.Fatalf("a marked row left %d rows standing, so the trigger never fired and this "+
			"test would hold nothing", rows)
	}
	if data, err := s.OnceUse(ctx, dbapi.OnceTask, value, 1790000000); err == nil {
		t.Fatalf("OnceUse handed out %q from the row it had just lost", data)
	}
	if rows, used := onceRow(t, db, value); rows != 1 || used != 0 {
		t.Errorf("after the failed OnceUse: rows left: %d, used: %d - want 1 and 0; "+
			"the mark of a transaction that failed is still standing", rows, used)
	}
}
