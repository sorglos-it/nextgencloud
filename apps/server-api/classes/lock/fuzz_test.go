package lock

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
)

// seeds are the rows FuzzLockRow starts from: rows the counting writes, rows
// a damaged database could hold, and the numbers at the ends of their range.
func seeds() [][]byte {
	return [][]byte{
		[]byte("1 1790000000"),
		[]byte("3 1790000001"),
		[]byte("0 0"),
		[]byte("20 1790000300"),
		[]byte("3 8000000000000000000"),
		[]byte(""),
		[]byte("1"),
		[]byte(" "),
		[]byte("-1 -1"),
		// The largest count a row can hold: one more failure on it is where
		// the counting would run over into a negative number, and from then
		// on every answer about the name is a refusal.
		[]byte("9223372036854775807 1790000000"),
		[]byte("9999999999999999999999 1"),
		[]byte("2 1790000000 3"),
		[]byte("1  1790000000"),
	}
}

// TestSeedsAreRowsTheCountingReads is what makes a green fuzz run mean
// something. Half the target lies behind the row parser, and a corpus of
// rows it all refuses would never walk it. So the seeds are held to bringing
// rows the counting reads, and not just rows it turns away.
func TestSeedsAreRowsTheCountingReads(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	taken := 0
	for _, row := range seeds() {
		// With the times a row of the counting carries: one past its keep
		// time is read as none whatever it says, and every seed would count
		// as one the counting reads.
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
			Data: row, Seq: uint64(now + keepFor), Updated: now}); err != nil {
			t.Fatalf("put %q: %v", row, err)
		}
		if _, _, _, err := read(ctx, db, "anna", now); err == nil {
			taken++
		}
	}
	if taken < 4 {
		t.Fatalf("only %d of %d seeds are rows the counting reads", taken, len(seeds()))
	}
}

// FuzzLockRow feeds the counting with any row a damaged or foreign database
// could hold. A row it reads gives a wait between none and the longest one,
// and a further failure never shortens it; a row it refuses is refused by
// every function alike and is never written over, so no damaged row is the
// way out of a lock. Nothing panics.
func FuzzLockRow(f *testing.F) {
	for _, row := range seeds() {
		f.Add(row)
	}
	f.Fuzz(func(t *testing.T, row []byte) {
		db := memstore.New()
		now := int64(1790000000)
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key("anna"),
			Data: row, Seq: uint64(now + keepFor), Updated: now}); err != nil {
			t.Fatalf("put: %v", err)
		}
		// What is fuzzed is the data; the two times above are the ones Fail
		// writes. So whatever the data of such a row says, it says it for
		// keepFor at most: once those times say the row may be removed, the
		// counting reads it as none. No data makes a wait that never ends, and
		// none shuts its bucket until somebody wires up a purge. Rows a store
		// hands back with times of its own are not what this walks.
		if wait, err := Check(ctx, db, "anna", now+keepFor); err != nil || wait != 0 {
			t.Fatalf("the row still answers %d after keepFor: %v", wait, err)
		}
		wait, err := Check(ctx, db, "anna", now)
		if err != nil {
			if !errors.Is(err, ErrLock) {
				t.Fatalf("check answered %v", err)
			}
			if wait != 0 {
				t.Fatalf("a row it refuses handed out a wait of %d", wait)
			}
			// A row nobody can read is not counted as no failures: it stays
			// as it is, and every answer about it stays a refusal.
			if err := Fail(ctx, db, "anna", now); !errors.Is(err, ErrLock) {
				t.Fatalf("fail on a row check refuses answered %v", err)
			}
			b, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna"))
			if err != nil || !bytes.Equal(b.Data, row) {
				t.Fatalf("the row was written over: %v", err)
			}
			return
		}
		if wait < 0 || wait > maxWait {
			t.Fatalf("wait %d for row %q", wait, row)
		}
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("fail: %v", err)
		}
		again, err := Check(ctx, db, "anna", now)
		if err != nil {
			t.Fatalf("check after fail: %v", err)
		}
		if again < wait || again > maxWait {
			t.Fatalf("a further failure turned the wait %d into %d", wait, again)
		}
		// However damaged the row was, what is written back counts and is
		// removed again: a row that outlived every purge would lock a name
		// out for good.
		b, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna"))
		if err != nil {
			t.Fatalf("row after fail: %v", err)
		}
		if b.Seq != uint64(now+keepFor) {
			t.Fatalf("row kept until %d instead of %d", b.Seq, now+keepFor)
		}
		if err := Purge(ctx, db, now+keepFor); err != nil {
			t.Fatalf("purge: %v", err)
		}
		if _, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna")); !errors.Is(err, dbapi.ErrNotFound) {
			t.Fatalf("the row survived its own purge: %v", err)
		}
	})
}
