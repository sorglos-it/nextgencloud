package sqlstore_test

import (
	"bytes"
	"errors"
	"strings"
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
// from another API and on another node. Nothing else in this package runs
// LockFree against an open database, so without this the second half of that
// statement is free to fall away unnoticed.
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
// until below the now it is given, so the other side of the same comparison in
// TestOnceAndLockBounds never reaches the database at all.
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
