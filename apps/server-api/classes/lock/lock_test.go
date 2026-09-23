package lock

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
)

var ctx = context.Background()

func TestLockGrowsAndClears(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	// The first two failures cost nothing: a typo must not lock anyone out.
	for i := 0; i < 2; i++ {
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
		if wait, err := Check(ctx, db, "anna", now); err != nil || wait != 0 {
			t.Fatalf("after %d failures wait %d: %v", i+1, wait, err)
		}
	}
	if err := Fail(ctx, db, "anna", now); err != nil {
		t.Fatalf("third: %v", err)
	}
	wait, err := Check(ctx, db, "anna", now)
	if err != nil || wait != 1 {
		t.Fatalf("third failure wait %d: %v", wait, err)
	}
	if err := Fail(ctx, db, "anna", now); err != nil {
		t.Fatalf("fourth: %v", err)
	}
	if wait, _ := Check(ctx, db, "anna", now); wait != 2 {
		t.Fatalf("fourth failure wait %d", wait)
	}
	// Once the time has passed the name is free again.
	if wait, _ := Check(ctx, db, "anna", now+2); wait != 0 {
		t.Fatalf("still locked after the wait: %d", wait)
	}
	if err := Clear(ctx, db, "anna"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna")); err == nil {
		t.Fatal("clear left the row")
	}
}

func TestLockIsTheSameForNamesThatDoNotExist(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	for i := 0; i < 3; i++ {
		if err := Fail(ctx, db, "nobody", now); err != nil {
			t.Fatalf("fail: %v", err)
		}
	}
	// Nothing about the row says whether the name belongs to a user.
	if wait, _ := Check(ctx, db, "nobody", now); wait != 1 {
		t.Fatalf("wait %d", wait)
	}
}

func TestPurgeRemovesOnlyExpiredLocks(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	for i := 0; i < 3; i++ {
		_ = Fail(ctx, db, "anna", now)
	}
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte("x")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := Purge(ctx, db, now+3600); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna")); err == nil {
		t.Fatal("expired lock kept")
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation); err != nil {
		t.Fatalf("purge removed another setting: %v", err)
	}
}

// TestPurgeFreesNothingOnATimeNoClockGives holds the one number that would
// free every lock at once: a time below 1 read as unsigned lies above every
// row, and the rows that still count are exactly the ones above it.
func TestPurgeFreesNothingOnATimeNoClockGives(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	for i := 0; i < 3; i++ {
		_ = Fail(ctx, db, "anna", now)
	}
	for _, bad := range []int64{-1, 0, -1790000000} {
		err := Purge(ctx, db, bad)
		if _, kept := db.BlobGet(ctx, dbapi.BlobSetting, key("anna")); kept != nil {
			t.Fatalf("purge at %d freed the lock: %v", bad, kept)
		}
		if !errors.Is(err, ErrLock) {
			t.Fatalf("purge at %d answered %v", bad, err)
		}
	}
	// The same time counts nothing either, so no one buys a free try with it.
	if err := Fail(ctx, db, "anna", 0); !errors.Is(err, ErrLock) {
		t.Fatalf("fail at 0 answered %v", err)
	}
	if wait, err := Check(ctx, db, "anna", 0); !errors.Is(err, ErrLock) || wait != 0 {
		t.Fatalf("check at 0 answered %d, %v", wait, err)
	}
}

// TestWaitIsNeverLongerThanTheLongestOne proves that no row shuts a name out
// for longer than the counting itself ever asks for.
func TestWaitIsNeverLongerThanTheLongestOne(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
		Data: []byte("3 8000000000000000000"), Seq: uint64(now + keepFor)}); err != nil {
		t.Fatalf("put: %v", err)
	}
	wait, err := Check(ctx, db, "anna", now)
	if err != nil || wait != maxWait {
		t.Fatalf("wait %d: %v", wait, err)
	}
}

// TestAFurtherFailureNeverShortensTheWait proves that trying again while a
// name waits is no way out of the wait. The count alone decides how long the
// next wait is, so a row that waits longer than the count asks for -- a clock
// that jumped forward and came back -- would be shortened by one more try.
func TestAFurtherFailureNeverShortensTheWait(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
		Data: []byte("1 1790000200"), Seq: uint64(now + keepFor)}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := Fail(ctx, db, "anna", now); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != 200 {
		t.Fatalf("a further failure shortened the wait to %d: %v", wait, err)
	}
}

// TestDamagedRowKeepsTheNameShut proves that a row nobody can read is no way
// out: it is not counted as no failures, and it is not written over.
func TestDamagedRowKeepsTheNameShut(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	row := []byte("x y")
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
		Data: row, Seq: uint64(now + keepFor)}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); !errors.Is(err, ErrLock) || wait != 0 {
		t.Fatalf("check answered %d, %v", wait, err)
	}
	if err := Fail(ctx, db, "anna", now); !errors.Is(err, ErrLock) {
		t.Fatalf("fail answered %v", err)
	}
	b, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna"))
	if err != nil || !bytes.Equal(b.Data, row) {
		t.Fatalf("the damaged row was replaced by %q: %v", b.Data, err)
	}
}

// TestFailuresFiredAtOnceAllCount proves the counting against the way an
// attacker tries: not one guess after the other but all of them at once. Read
// and write have to be one step, or every one of them reads the same count
// and writes the same one back, and the wait never grows.
func TestFailuresFiredAtOnceAllCount(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	const tries = 50
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < tries; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := Fail(ctx, db, "anna", now); err != nil {
				t.Errorf("fail: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	n, _, _, err := read(ctx, db, "anna")
	if err != nil || n != tries {
		t.Fatalf("%d of %d failures counted: %v", n, tries, err)
	}
	if wait, _ := Check(ctx, db, "anna", now); wait != maxWait {
		t.Fatalf("wait after %d failures at once: %d", tries, wait)
	}
}

// guard is a store that holds every write of a lock row to the mutex which
// makes reading a row and writing it back one step. TryLock tells it apart:
// while the counting is held it cannot be taken, and a write that takes it is
// one that runs beside a failure being counted.
type guard struct {
	dbapi.Local
	tb testing.TB
}

func (g *guard) held(what string) {
	if counting.TryLock() {
		counting.Unlock()
		g.tb.Errorf("%s ran without the counting", what)
	}
}

func (g *guard) BlobPut(ctx context.Context, b dbapi.Blob) error {
	g.held("a put of " + b.ID)
	return g.Local.BlobPut(ctx, b)
}

func (g *guard) BlobDelete(ctx context.Context, kind, id string) error {
	g.held("a delete of " + id)
	return g.Local.BlobDelete(ctx, kind, id)
}

func (g *guard) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	g.held("a walk of the rows")
	return g.Local.BlobFrom(ctx, kind, seq)
}

// TestEveryWriteOfALockRowRunsUnderTheCounting proves that removing rows is
// held to the same step as counting them. A walk hands out a picture of a
// moment; a failure counted after it makes one of those rows a lock that is
// running, and a delete that follows the old picture sets that lock to none.
func TestEveryWriteOfALockRowRunsUnderTheCounting(t *testing.T) {
	db := &guard{Local: memstore.New(), tb: t}
	now := int64(1790000000)
	for i := 0; i < 3; i++ {
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	if err := Clear(ctx, db, "anna"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := Fail(ctx, db, "anna", now); err != nil {
		t.Fatalf("fail after the clear: %v", err)
	}
	if err := Purge(ctx, db, now+keepFor); err != nil {
		t.Fatalf("purge: %v", err)
	}
}

// TestNoMoreNamesThanTheTableTakes proves the rows are bounded. Every name
// that fails writes one, and nobody has to be a user to try a name: without a
// cap the settings table grows with every made-up name somebody sends.
func TestNoMoreNamesThanTheTableTakes(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	for i := 0; i < maxRows; i++ {
		if err := Fail(ctx, db, "name-"+strconv.Itoa(i), now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	if err := Fail(ctx, db, "one name too many", now); !errors.Is(err, ErrFull) {
		t.Fatalf("the name beyond the cap answered %v", err)
	}
	// A name that already counts counts on: whoever fills the table with
	// names of his own must not buy free tries for the name he is after.
	if err := Fail(ctx, db, "name-0", now); err != nil {
		t.Fatalf("a name that already counts: %v", err)
	}
	// Rows nobody counts any more turn nobody away: once their time has
	// passed the table takes names again, without waiting for a purge.
	if err := Fail(ctx, db, "one name too many", now+keepFor+1); err != nil {
		t.Fatalf("after the rows had expired: %v", err)
	}
	blobs, err := db.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("%d rows left of %d", len(blobs), maxRows)
	}
}
