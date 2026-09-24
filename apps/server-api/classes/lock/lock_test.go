package lock

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
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

// TestTheCountingStartsOverAfterKeepFor holds what makes the wait a wait and
// not a sentence: a bucket nothing has counted for keepFor begins at zero
// again. A row says itself how long it is kept, and the counting reads it the
// same way Purge does -- so the lock behaves the same whether a purge was ever
// wired up and however often it runs. Without it every bucket climbs to the
// longest wait once and stays there, and a dozen typos spread over months are
// enough to get it there.
func TestTheCountingStartsOverAfterKeepFor(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	// A dozen failures are more than enough for the longest wait there is.
	for i := 0; i < 12; i++ {
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != maxWait {
		t.Fatalf("after twelve failures wait %d: %v", wait, err)
	}
	// Nothing counts for keepFor. From here the bucket is as new: the first
	// freeCount failures are free again, and the next costs the first wait.
	later := now + keepFor
	for i := 0; i < freeCount; i++ {
		if err := Fail(ctx, db, "anna", later); err != nil {
			t.Fatalf("fail %d after keepFor: %v", i, err)
		}
		if wait, err := Check(ctx, db, "anna", later); err != nil || wait != 0 {
			t.Fatalf("failure %d of a bucket at zero waits %d: %v", i+1, wait, err)
		}
	}
	if err := Fail(ctx, db, "anna", later); err != nil {
		t.Fatalf("the failure after the free ones: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", later); err != nil || wait != firstWait {
		t.Fatalf("the first wait of a bucket at zero is %d: %v", wait, err)
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

// TestPurgeFreesNoRowTheCountingStillReads holds Purge to the counting: a row
// goes exactly when the counting reads it as none, because both ask the same
// question of it (see expired). Where the two could come apart is a store that
// hands Seq back empty -- dbapi leaves Seq to the caller and the contract's own
// test never reads one back through BlobGet -- and there a purge would be the
// way out of a lock that is still running. Whoever wires Purge up may run it as
// often as he likes, and this is what that rests on.
func TestPurgeFreesNoRowTheCountingStillReads(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	// A row of a store that keeps no Seq: its keep time is in Updated alone,
	// and the counting reads it as the longest wait there is.
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
		Data: []byte("9 1790000300"), Updated: now}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != maxWait {
		t.Fatalf("before the purge the row waits %d: %v", wait, err)
	}
	if err := Purge(ctx, db, now); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != maxWait {
		t.Fatalf("the purge freed a lock that was still running: %d %v", wait, err)
	}
	// And once the counting reads the row as none, the purge takes it: what is
	// kept is the row that still counts, not every row.
	later := now + keepFor
	if wait, err := Check(ctx, db, "anna", later); err != nil || wait != 0 {
		t.Fatalf("after keepFor the row still waits %d: %v", wait, err)
	}
	if err := Purge(ctx, db, later); err != nil {
		t.Fatalf("purge after keepFor: %v", err)
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna")); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("the purge kept a row the counting reads as none: %v", err)
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

// TestADamagedRowShutsItsBucketNoLongerThanKeepFor holds the other side of
// the row above. A row nobody can read shuts its bucket, and that is the safe
// direction -- but nothing in this package ever repairs it: Fail leaves it
// alone on purpose, and Clear runs after a login the shut bucket lets nobody
// make. A row is kept as long as its own two times say, and the row below
// carries the times this package writes: keepFor, so that is how long the
// damage lasts here, and the next failure writes a row over it that counts
// again. What a row of other times is kept for is not what this holds.
func TestADamagedRowShutsItsBucketNoLongerThanKeepFor(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
		Data: []byte("x y"), Seq: uint64(now + keepFor), Updated: now}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); !errors.Is(err, ErrLock) || wait != 0 {
		t.Fatalf("while it is kept the damaged row answered %d, %v", wait, err)
	}
	later := now + keepFor
	if wait, err := Check(ctx, db, "anna", later); err != nil || wait != 0 {
		t.Fatalf("the damaged row still shuts its bucket after keepFor: %d %v", wait, err)
	}
	// And the bucket counts from zero again, so nobody is left without a lock
	// either: the damage is neither a way in nor a way to shut a name for good.
	for i := 0; i <= freeCount; i++ {
		if err := Fail(ctx, db, "anna", later); err != nil {
			t.Fatalf("fail %d on the bucket of a damaged row: %v", i, err)
		}
	}
	if wait, err := Check(ctx, db, "anna", later); err != nil || wait != firstWait {
		t.Fatalf("the bucket of a damaged row waits %d: %v", wait, err)
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
	n, _, _, err := read(ctx, db, "anna", now)
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

// TestEveryNameIsCountedHoweverManyHaveFailed holds the one thing a table
// with a cap gave away: a table nobody can add to must not switch the
// counting off. Whoever fills it with names of his own would otherwise buy
// unlimited guesses on every name that has no row yet -- and no name has one
// until its first failure. Nobody has to be a user to try a name, so filling
// it costs nothing but requests.
func TestEveryNameIsCountedHoweverManyHaveFailed(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	// Far more made-up names than the counting will ever hold rows for.
	const names = 16384
	for i := 0; i < names; i++ {
		if err := Fail(ctx, db, "made-up-"+strconv.Itoa(i), now); err != nil {
			t.Fatalf("failure %d on a name of its own: %v", i, err)
		}
	}
	blobs, err := db.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// The rows are the counting's own, not one per name that was sent.
	if len(blobs)*2 >= names {
		t.Fatalf("%d rows for %d names", len(blobs), names)
	}
	// A name nobody has tried yet, on a table as full as it gets: it counts
	// like every other, and it locks. Whether "anna" shares her bucket with
	// one of the made-up names is a coincidence of the hash, and the check
	// must not rest on it -- so her bucket is emptied first and the wait
	// afterwards is the exact one her own failures ask for. Without that,
	// made-up names alone could make the last line true.
	if err := Clear(ctx, db, "anna"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != 0 {
		t.Fatalf("an empty bucket waits %d: %v", wait, err)
	}
	for i := 0; i <= freeCount; i++ {
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("a failure on a new name was not counted: %v", err)
		}
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != firstWait {
		t.Fatalf("a new name waits %d after its first failures: %v", wait, err)
	}
}

// mate returns a second name that shares the row of name. It searches, and
// the search is what it proves: a counting that gives every name a row of its
// own finds none among a million.
func mate(tb testing.TB, name string) string {
	tb.Helper()
	for i := 0; i < 1<<20; i++ {
		other := "mate-" + strconv.Itoa(i)
		if other != name && key(other) == key(name) {
			return other
		}
	}
	tb.Fatalf("no name among a million shares the row of %q", name)
	return ""
}

// TestNamesOfOneRowShareTheCounting states the price of a bounded table: two
// names can share a row, and then they share the count and the wait. That is
// what makes the table bounded, and it is why the row may hold nothing of the
// name -- whoever reads the rows learns which rows count, never which names
// were tried, and never whether any of them belongs to a user.
func TestNamesOfOneRowShareTheCounting(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	other := mate(t, "anna")
	for i := 0; i < 3; i++ {
		if err := Fail(ctx, db, other, now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	wait, err := Check(ctx, db, "anna", now)
	if err != nil || wait != 1 {
		t.Fatalf("the name sharing the row waited %d: %v", wait, err)
	}
	// One row, not two: that is the whole of what bounds the table.
	blobs, err := db.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil || len(blobs) != 1 {
		t.Fatalf("%d rows for two names: %v", len(blobs), err)
	}
}

// TestEveryNameFallsInABucket proves the table is bounded by the names alone
// and needs no cap to be: a name of any length and any bytes gets one of
// buckets many IDs, all of one size. The ID is the whole row key, so the
// rows tell nobody which names were tried -- only how many of the buckets
// count right now.
func TestEveryNameFallsInABucket(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 8*buckets; i++ {
		id := key(strings.Repeat("\x00name ", i%7) + strconv.Itoa(i))
		if !strings.HasPrefix(id, prefix) || len(id) != len(prefix)+4 {
			t.Fatalf("the ID of a name is %q", id)
		}
		seen[id] = true
	}
	if len(seen) > buckets {
		t.Fatalf("%d IDs of %d buckets", len(seen), buckets)
	}
}

// stripSeq is a store that hands a row back without the Seq it was written
// with. dbapi leaves Seq to the caller ("0 where no order is needed") and the
// contract's own test never reads one back through BlobGet, so a store may
// well do this.
type stripSeq struct{ dbapi.Local }

func (s stripSeq) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	b, err := s.Local.BlobGet(ctx, kind, id)
	b.Seq = 0
	return b, err
}

// TestTheCountingHoldsWhereAStoreKeepsNoSeq holds the one way the expiry
// above could open every lock instead of closing the old ones: a row carries
// the time it is kept twice, in Seq and in Updated, and a store that drops one
// of them must not make every row look expired -- that would count every
// failure as the first and the wait would never grow. So a row counts unless
// both of its times say it may be gone.
func TestTheCountingHoldsWhereAStoreKeepsNoSeq(t *testing.T) {
	db := stripSeq{memstore.New()}
	now := int64(1790000000)
	for i := 0; i <= freeCount; i++ {
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != firstWait {
		t.Fatalf("a store that keeps no Seq counted the failures to a wait of %d: %v", wait, err)
	}
	// And the row still expires, by the other of its two times.
	later := now + keepFor
	if err := Fail(ctx, db, "anna", later); err != nil {
		t.Fatalf("fail after keepFor: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", later); err != nil || wait != 0 {
		t.Fatalf("the first failure of a bucket at zero waits %d: %v", wait, err)
	}
}

// TestARowKeepsCountingUnlessItHasLostBothTimes pins how far the two times
// carry, and where they stop. A store that hands back one of them keeps the
// counting: the row still answers the wait it holds. A store that hands back
// neither leaves nothing to tell the row from one that is not there, and it
// reads as none -- that is the one shape the counting cannot hold, and it is
// what the word about a row that cannot be counted is narrowed to.
func TestARowKeepsCountingUnlessItHasLostBothTimes(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	row := []byte("9 1790000300")
	for _, b := range []dbapi.Blob{
		{Kind: dbapi.BlobSetting, ID: key("anna"), Data: row, Seq: uint64(now + keepFor)},
		{Kind: dbapi.BlobSetting, ID: key("anna"), Data: row, Updated: now},
	} {
		if err := db.BlobPut(ctx, b); err != nil {
			t.Fatalf("put: %v", err)
		}
		if wait, err := Check(ctx, db, "anna", now); err != nil || wait != maxWait {
			t.Fatalf("a row with one of its two times waits %d: %v", wait, err)
		}
	}
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
		Data: row}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != 0 {
		t.Fatalf("a row with neither of its two times waits %d: %v", wait, err)
	}
}

// TestTheCountingsClockNeverRunsBackwards holds a lock against the clock. A
// clock set back further than a row is kept, one failure counted at that time,
// and the clock right again: the row would otherwise carry times that lie
// behind the counting, the next read takes it for expired, and the lock that
// was running is gone together with the count. A wait must not be something
// anybody turns off by turning a clock.
func TestTheCountingsClockNeverRunsBackwards(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	for i := 0; i < 4; i++ {
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	before, err := Check(ctx, db, "anna", now)
	if err != nil || before == 0 {
		t.Fatalf("the lock before the clock moved: %d %v", before, err)
	}
	back := now - keepFor - 1
	if err := Fail(ctx, db, "anna", back); err != nil {
		t.Fatalf("fail on a clock set back: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait != before {
		t.Fatalf("the clock going back left a wait of %d instead of %d: %v", wait, before, err)
	}
	// And the counting stands where it stood, not at zero: the failures after
	// it are not free again.
	if err := Fail(ctx, db, "anna", now); err != nil {
		t.Fatalf("fail after the clock was right again: %v", err)
	}
	if wait, err := Check(ctx, db, "anna", now); err != nil || wait <= before {
		t.Fatalf("the failure after the clock went back waits %d, the one before %d: %v", wait, before, err)
	}
}
