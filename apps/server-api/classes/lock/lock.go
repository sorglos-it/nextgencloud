// Package lock counts failed logins per login name and says how long a name
// has to wait. It keeps the count in the database, so the wait holds at every
// API of an installation. The row is kept for names that do not exist too,
// and it holds only the hash of the name: an answer never tells whether a
// user exists, and the rows tell no one which names were tried.
package lock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// maxRows is how many names the counting keeps at once. Every name that
	// fails writes a row, and nobody has to be a user to try one: without a
	// cap the settings table grows with every made-up name somebody sends. A
	// name that already counts counts on beyond it -- only names that are
	// new find no room, and only until the rows of the hour have passed.
	maxRows = 1024
	// maxCount is the highest failure count a row holds. From a dozen on the
	// wait is maxWait whatever the number says, so counting further changes
	// nothing -- except at the end of the range, where one more would turn
	// the count negative and shut the name for good.
	maxCount = 1 << 20
)

// maxNow is the largest time these functions take. No clock of this century
// comes near it, and below it now+keepFor never wraps into a negative time,
// which would write a row Purge frees at once.
const maxNow = 1 << 62

// ErrLock reports a lock row the database cannot hold or that is malformed,
// and a time no clock gives. Both leave the name locked rather than free:
// what cannot be counted must not be counted as nothing.
var ErrLock = errors.New("lock: malformed lock row")

// ErrFull reports that the counting holds as many names as it may (maxRows)
// and this one is not among them. The attempt is turned away rather than let
// through: an attempt nobody counts is a free guess, and a table somebody
// filled with names of his own must not buy him those.
var ErrFull = errors.New("lock: no room for another name")

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

// key is the setting ID of a name: the hex of its SHA-256, so the row is of
// fixed size and holds no name.
func key(name string) string {
	h := sha256.Sum256([]byte(name))
	return prefix + hex.EncodeToString(h[:])
}

// read returns the number of failures, the time the wait ends, and whether
// the name has a row at all. The last one decides whether counting this
// failure adds a row to the table, which is what maxRows bounds.
func read(ctx context.Context, db dbapi.Local, name string) (int, int64, bool, error) {
	b, err := db.BlobGet(ctx, dbapi.BlobSetting, key(name))
	if errors.Is(err, dbapi.ErrNotFound) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	count, until, ok := strings.Cut(string(b.Data), " ")
	n, err1 := strconv.Atoi(count)
	u, err2 := strconv.ParseInt(until, 10, 64)
	if !ok || err1 != nil || err2 != nil || n < 0 || u < 0 {
		return 0, 0, true, ErrLock
	}
	return n, u, true, nil
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
	_, until, _, err := read(ctx, db, name)
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
// damaged row the way out of every lock. A name with no row yet gets one
// while the table has room for it, and ErrFull once it has not: an attempt
// that cannot be counted is turned away, never let through.
func Fail(ctx context.Context, db dbapi.Local, name string, now int64) error {
	if !validNow(now) {
		return ErrLock
	}
	counting.Lock()
	defer counting.Unlock()
	n, until, found, err := read(ctx, db, name)
	if err != nil {
		return err
	}
	// A name with no row of its own adds one, and only so many fit.
	if !found {
		if err := room(ctx, db, now); err != nil {
			return err
		}
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
// would be undone by the write that follows it.
func Clear(ctx context.Context, db dbapi.Local, name string) error {
	counting.Lock()
	defer counting.Unlock()
	return db.BlobDelete(ctx, dbapi.BlobSetting, key(name))
}

// Purge removes lock rows whose time has passed. Other settings stay. A time
// no clock gives removes nothing at all: every row still counting lies ahead
// of it, and freeing them all at once is exactly what a wrong number must
// never do.
func Purge(ctx context.Context, db dbapi.Local, now int64) error {
	if !validNow(now) {
		return ErrLock
	}
	counting.Lock()
	defer counting.Unlock()
	_, err := sweep(ctx, db, now)
	return err
}

// room reports whether the table takes another name, and is asked before a
// name gets a row of its own. The rows nobody counts any more go on the way,
// so a table full of hours past turns nobody away and no purge has to have
// run for the counting to keep working.
func room(ctx context.Context, db dbapi.Local, now int64) error {
	live, err := sweep(ctx, db, now)
	if err != nil {
		return err
	}
	if live >= maxRows {
		return ErrFull
	}
	return nil
}

// sweep walks the lock rows once: it removes those whose time has passed and
// returns how many still count. The caller holds counting, so nothing is
// counted between the walk and the deletes that follow it -- a row that the
// walk saw expired would otherwise be a lock somebody has just set, and
// removing it would set that lock to none.
func sweep(ctx context.Context, db dbapi.Local, now int64) (int, error) {
	blobs, err := db.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil {
		return 0, err
	}
	live := 0
	for _, b := range blobs {
		if !strings.HasPrefix(b.ID, prefix) {
			continue
		}
		if b.Seq > uint64(now) {
			live++
			continue
		}
		if err := db.BlobDelete(ctx, dbapi.BlobSetting, b.ID); err != nil {
			return 0, err
		}
	}
	return live, nil
}
