// Package lock counts failed logins per login name and says how long a name
// has to wait. It keeps the count in the database, so the wait holds at every
// API of an installation. The row is kept for names that do not exist too,
// and it holds nothing of the name but which of a fixed number of buckets it
// falls in: an answer never tells whether a user exists, and the rows tell no
// one which names were tried.
package lock

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// Counting: the first freeCount failures are free (a typo costs nothing),
// then the wait doubles from firstWait up to maxWait.
const (
	prefix    = "lock."
	freeCount = 2
	firstWait = 1
	maxWait   = 300
	// keepFor is how long a row with no further failure is kept, so that a
	// burst of tries does not clear itself at once.
	keepFor = 3600
	// maxCount is the highest failure count a row holds. From a dozen on the
	// wait is maxWait whatever the number says, so counting further changes
	// nothing -- except at the end of the range, where one more would turn
	// the count negative and shut the name for good.
	maxCount = 1 << 20
)

// buckets is how many rows the counting has, and every name falls in one of
// them (key). Nobody has to be a user to try a name, so a row per name would
// let anybody grow the settings table with made-up names; a cap on the rows
// would bound it, but then a name with no row of its own could not be counted
// once the table is full, and filling it would switch the counting off for
// every name that matters. Buckets bound the table by their number alone, so
// every name is countable at every moment and no walk of the table stands in
// front of a failure.
//
// 4096 of them, and a power of two so that the 16 bits below divide among
// them without a remainder -- no bucket takes more names than another.
//
//   - Two names of one installation sharing a bucket is what it costs: with
//     30 accounts the chance is about a tenth, with 10 about one in a
//     hundred, and what they share is the count and the wait of at most
//     maxWait (see Fail).
//   - The rows stay small: 4096 of about 40 bytes is well under a quarter of
//     a megabyte, even after somebody has hit every bucket. Purge reads them
//     all at once (dbapi.BlobFrom takes no limit), and this number is what
//     bounds that read.
//   - Locking every bucket out costs a dozen failures each and has to be
//     repeated every maxWait. Whoever knows the names of an installation
//     locks them out far cheaper, one by one -- which is the price of any
//     lock per name, and no number of buckets changes it.
const buckets = 4096

// maxNow is the largest time these functions take. No clock of this century
// comes near it, and below it now+keepFor never wraps into a negative time,
// which would write a row Purge frees at once.
const maxNow = 1 << 62

// ErrLock reports a lock row the database cannot hold or that is malformed,
// and a time no clock gives. Both leave the name locked rather than free:
// what cannot be counted must not be counted as nothing.
var ErrLock = errors.New("lock: malformed lock row")

// counting makes reading a row and writing it back one step within this
// process, and removing rows a step of the same kind. Without it a burst of
// tries fired at once all read the same count and all write the same one
// back, and the wait would never grow however many were tried in parallel;
// and a walk of the rows would go on deleting by a picture that a failure
// counted since has made wrong. There is one API today, so this is the whole
// step; a second one on the same database would need the database to count,
// which the contract has no call for.
var counting sync.Mutex

// validNow reports whether now is a time a clock of this API gives.
func validNow(now int64) bool { return now >= 1 && now <= maxNow }

// key is the setting ID of a name: its bucket, taken from the first two bytes
// of its SHA-256 and written as four hex digits, so every row is of one size
// and holds nothing of the name. There are only buckets many of them, which
// is what bounds the table; two names that fall in one share their count.
func key(name string) string {
	h := sha256.Sum256([]byte(name))
	n := binary.BigEndian.Uint16(h[:2]) % buckets
	const digits = "0123456789abcdef"
	return prefix + string([]byte{
		digits[n>>12&0xf], digits[n>>8&0xf], digits[n>>4&0xf], digits[n&0xf]})
}

// read returns the number of failures and the time the wait ends.
func read(ctx context.Context, db dbapi.Local, name string) (int, int64, error) {
	b, err := db.BlobGet(ctx, dbapi.BlobSetting, key(name))
	if errors.Is(err, dbapi.ErrNotFound) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	count, until, ok := strings.Cut(string(b.Data), " ")
	n, err1 := strconv.Atoi(count)
	u, err2 := strconv.ParseInt(until, 10, 64)
	if !ok || err1 != nil || err2 != nil || n < 0 || u < 0 {
		return 0, 0, ErrLock
	}
	return n, u, nil
}

// Check returns how many seconds name still has to wait, 0 when it is free.
// The answer is never longer than maxWait: no failure this package counts
// asks for more, so a row that lies further ahead -- a clock that jumped
// forward and came back, a number that went wrong -- would otherwise shut a
// name out for as long as that number happens to say.
func Check(ctx context.Context, db dbapi.Local, name string, now int64) (int64, error) {
	if !validNow(now) {
		return 0, ErrLock
	}
	_, until, err := read(ctx, db, name)
	if err != nil || until <= now {
		return 0, err
	}
	if wait := until - now; wait < maxWait {
		return wait, nil
	}
	return maxWait, nil
}

// Fail counts one failed attempt for name and sets the new wait. A row it
// cannot read stays as it is: writing a fresh count over it would make a
// damaged row the way out of every lock. Every name has its bucket, so every
// attempt is counted -- however many names have failed before it, and
// whether or not the name belongs to a user. The whole of it is one read and
// one write, so counting a name that has failed before costs exactly what
// counting one nobody has ever sent costs: a difference there would say over
// the clock what the answers keep quiet.
func Fail(ctx context.Context, db dbapi.Local, name string, now int64) error {
	if !validNow(now) {
		return ErrLock
	}
	counting.Lock()
	defer counting.Unlock()
	n, until, err := read(ctx, db, name)
	if err != nil {
		return err
	}
	if n < maxCount {
		n++
	}
	wait := int64(0)
	if n > freeCount {
		wait = firstWait << min(n-freeCount-1, 16)
		if wait > maxWait {
			wait = maxWait
		}
	}
	// Trying again while a name waits is no way out of the wait: the count
	// alone says how long the next one is, so the wait that is running stands
	// wherever it is the longer of the two.
	if until < now+wait {
		until = now + wait
	}
	// Seq is the time the row may be removed, so Purge can skip the rows that
	// still count without reading them.
	return db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key(name),
		Seq: uint64(now + keepFor), Data: []byte(strconv.Itoa(n) + " " + strconv.FormatInt(until, 10)),
		Updated: now})
}

// Clear removes the row after a successful login. It goes through the same
// step as the counting: between reading a row and writing it back, a delete
// would be undone by the write that follows it. The row is the bucket of the
// name, so a name that shares it is cleared along with it -- see buckets.
func Clear(ctx context.Context, db dbapi.Local, name string) error {
	counting.Lock()
	defer counting.Unlock()
	return db.BlobDelete(ctx, dbapi.BlobSetting, key(name))
}

// Purge removes lock rows whose time has passed. Other settings stay. A time
// no clock gives removes nothing at all: every row still counting lies ahead
// of it, and freeing them all at once is exactly what a wrong number must
// never do.
//
// It holds counting over the whole walk, so nothing is counted between the
// walk and the deletes that follow it: a row that the walk saw expired would
// otherwise be a lock somebody has just set, and removing it would set that
// lock to none. The walk is bounded by buckets rows and the other settings,
// and no failure of a login waits on more than that.
func Purge(ctx context.Context, db dbapi.Local, now int64) error {
	if !validNow(now) {
		return ErrLock
	}
	counting.Lock()
	defer counting.Unlock()
	blobs, err := db.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil {
		return err
	}
	for _, b := range blobs {
		if !strings.HasPrefix(b.ID, prefix) || b.Seq > uint64(now) {
			continue
		}
		if err := db.BlobDelete(ctx, dbapi.BlobSetting, b.ID); err != nil {
			return err
		}
	}
	return nil
}
