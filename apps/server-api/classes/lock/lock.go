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
	freeCount = 2
	firstWait = 1
	maxWait   = 300
	// keepFor is how long a row is kept past the latest time a failure of its
	// bucket was counted at, so that a burst of tries does not clear itself at
	// once -- and no longer, so that a bucket nothing has counted for that long
	// begins at zero again. That latest time can lie ahead of the clock, and
	// what that costs stands at kept in Fail.
	keepFor = 3600
	// maxCount is the highest failure count a row holds. From a dozen on the
	// wait is maxWait whatever the number says, so counting further changes
	// nothing -- except at the end of the range, where one more would turn
	// the count negative and shut the name for good.
	maxCount = 1 << 20
)

// buckets is how many rows each of the two countings has, and every name falls
// in one of them (key). Nobody has to be a user to try a name, so a row per
// name would let anybody grow the settings table with made-up names; a cap on
// the rows would bound it, but then a name with no row of its own could not be
// counted once the table is full, and filling it would switch the counting off
// for every name that matters. Buckets bound the table by their number alone,
// so every name is countable at every moment and no walk of the table stands in
// front of a failure.
//
// 4096 of them, and a power of two so that the 16 bits below divide among
// them without a remainder -- no bucket takes more names than another.
//
//   - Two names of one installation sharing a bucket is what it costs: with
//     30 accounts the chance is about a tenth, with 10 about one in a
//     hundred, and what they share is the count and the wait of at most
//     maxWait (see Fail).
//   - The rows stay small: two countings of 4096 rows of about 40 bytes are
//     under half a megabyte together, even after somebody has hit every bucket
//     of both. Purge reads them all at once (dbapi.BlobFrom takes no limit and
//     hands back every setting there is, not only these), so twice this number
//     is what bounds this package's share of that read and nothing more.
//   - Locking every bucket out costs a dozen failures each and has to be
//     repeated every maxWait. Whoever knows the names of an installation
//     locks them out far cheaper, one by one -- which is the price of any
//     lock per name, and no number of buckets changes it.
const buckets = 4096

// The two countings this package keeps, and the whole reason they are two. A
// row ID is one of these and then exactly four hex digits out of the hash (see
// key), so two row IDs are equal only where the two spaces are: the tail is of
// one fixed length and carries no space, and these two strings differ. That is
// what keeps a row of one counting from ever being a row of the other --
// whatever the two names hash to, and whoever chose them. Nothing else does,
// and in particular Purge does not (see below).
//
// nameSpace counts login names. It is the counting of the contract
// (shared/README.md, "Vor dem WebSocket": the wait holds per login name), and
// login/start reads it by the name it was asked for.
//
// keySpace counts the key ID of a proof whose key the trust list does not
// hold. Such a proof names no user, so there is no name to count it for -- and
// the key ID comes out of the request, which means whoever sends it picks the
// string. In one counting with the names that is an open door: key hashes
// every string into the same buckets, so a few thousand tries find a key ID
// that falls into the row of any name one likes, and three failed logins then
// lay that name's wait. Kept apart, the worst such a search buys is the row of
// another key ID.
//
// Purge does not tell the two apart and does not need to: it takes the rows of
// both and leaves every other setting alone (see there). What it does need is
// that no setting ID belonging to nobody here may begin with either of them --
// it asks for both with strings.HasPrefix, so an ID that begins with one is
// taken as a row of this package and freed once its time has passed.
const (
	nameSpace = "lock."
	keySpace  = "lockkey."
)

// The two properties key rests on, held by the compiler: a number that is no
// power of two, or one the 16 bits cannot address, does not translate. No test
// can hold them -- a wrong number divides the names unevenly and counts on.
const (
	_ = uint(0 - buckets&(buckets-1)) // a power of two
	_ = uint(1<<16 - buckets)         // no more than the 16 bits give out
)

// maxNow is the largest time these functions take. No clock of this century
// comes near it, and below it now+keepFor never wraps into a negative time,
// which would write a row Purge frees at once.
const maxNow = 1 << 62

// ErrLock reports a lock row the database cannot hold or that is malformed,
// and a time no clock gives. Both leave the name locked rather than free: a
// row that cannot be read must not be counted as nothing. One shape is outside
// that word, and only one: a row a store hands back without either of its two
// times is no longer to be told from a row that is not there, and from the
// clock's keepFor second on it reads as none (see expired) -- before that
// second an Updated of 0 still lies inside the keep time and the row counts on,
// which is no clock of this century but is what the word covers. Losing one of
// the two leaves the row counting.
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

// key is the setting ID of a name in one counting: the space it is counted in
// and its bucket, taken from the first two bytes of its SHA-256 and written as
// four hex digits, so every row is of one size and holds nothing of the name.
// There are only buckets many of them per space, which is what bounds the
// table; two names of one space that fall in one bucket share their count.
//
// The hash says nothing about the space, so two strings of different spaces
// that hash alike still get two rows -- the space is what keeps them apart,
// and it is the only thing that does.
func key(space, name string) string {
	n := bucketOf(name)
	const digits = "0123456789abcdef"
	return space + string([]byte{
		digits[n>>12&0xf], digits[n>>8&0xf], digits[n>>4&0xf], digits[n&0xf]})
}

// bucketOf is the row a name falls in. key writes it as hex and Hold takes its
// mutex, and both go through here: a mutex that stood for another row than the
// one the failure is counted in would guard nothing.
func bucketOf(name string) uint16 {
	h := sha256.Sum256([]byte(name))
	return binary.BigEndian.Uint16(h[:2]) % buckets
}

// holds is one mutex per row of each counting, and it is what makes "the first
// freeCount failures are free" a number instead of a starting point.
//
// counting below makes the read and the write of ONE failure a single step.
// That was not enough, and the gap was measured, not reasoned about: the
// caller reads the wait, then weighs the proof, then counts the failure, and
// the first of those is a read of its own. A burst fired at one moment all
// read the same free row before any of them had counted, so all of them were
// weighed. Measured on 17.10.2026, 20 wrong proofs against one row: 17 of them
// weighed when they came at once against 3 when they came one after the other,
// in 10 of 10 rounds; and on a real installation on Debian 13, 7 to 14 of 20.
// The free attempts an installation hands out grew with the attacker's
// concurrency.
//
// So the whole decision of one attempt - read the wait, weigh the proof, count
// the failure - belongs inside one hold of its row. Hold hands that out.
//
// What it costs, and it is a real price: two attempts in one row are now
// weighed one after the other, and a row holds every name that falls in it. A
// login of one name therefore waits for a login of another that shares its row
// - for the length of one verification, and there are buckets many rows, so
// with a few dozen accounts a pair sharing one is rare (see buckets). For the
// burst this is not a cost but the whole point: that is what the wait is for.
//
// And the same caveat as counting's: this is one process. A second API on the
// same database would read the row while this one holds its mutex, and then
// the counting would have to be the database's. The contract has no call for
// one today.
var holds [2][buckets]sync.Mutex

// Hold takes the row name is counted in and hands back its release. The caller
// holds it until the attempt is decided, failure counted and all.
func Hold(name string) func() { return hold(nameSpace, name) }

// HoldKey is Hold for the counting of the key IDs -- the other set of rows, for
// the same reason Fail and FailKey are two (see nameSpace and keySpace).
func HoldKey(keyID string) func() { return hold(keySpace, keyID) }

func hold(space, name string) func() {
	m := &holds[spaceRow(space)][bucketOf(name)]
	m.Lock()
	return m.Unlock
}

// spaceRow is which of the two sets of mutexes a space uses. The two spaces are
// the only ones there are, and a name must never reach the other's row: that is
// the same separation key rests on, and the reason for it stands at keySpace.
func spaceRow(space string) int {
	if space == keySpace {
		return 1
	}
	return 0
}

// expired reports whether a row may be removed, which is the moment the
// counting reads it as none. It is the one question Purge asks of a row too, so
// the two cannot come apart over what they ask -- only over what a store hands
// each of them, which is stated at Purge.
// A row carries that time twice: Seq, which dbapi leaves to the caller ("0
// where no order is needed"), and Updated, which holds when the row was last
// written. The contract's own test reads Data and Updated back through BlobGet
// and a Seq only through BlobFrom, so a store that hands Seq back empty must
// not turn every row into an expired one. That would count every failure as
// the first, and no wait would ever grow. A row is gone only when both of its
// times say so. now is a time validNow has taken, so it fits an unsigned
// number, and now-keepFor cannot run over either.
func expired(b dbapi.Blob, now int64) bool {
	return b.Seq <= uint64(now) && b.Updated <= now-keepFor
}

// read returns the number of failures, the time the wait ends, and the time
// the row was last written at (0 where there is no row the counting reads). A
// row past its keep time reads as none, before anything of it is read: it is
// the row Purge would have removed, and the lock must behave the same whether
// a purge was ever wired up and however often it runs. That is what lets the
// counting of a bucket fall back to zero -- and it is the only thing that ever
// ends the lock a damaged row lays on its bucket.
func read(ctx context.Context, db dbapi.Local, space, name string, now int64) (int, int64, int64, error) {
	b, err := db.BlobGet(ctx, dbapi.BlobSetting, key(space, name))
	if errors.Is(err, dbapi.ErrNotFound) {
		return 0, 0, 0, nil
	}
	if err != nil {
		return 0, 0, 0, err
	}
	if expired(b, now) {
		return 0, 0, 0, nil
	}
	count, until, ok := strings.Cut(string(b.Data), " ")
	n, err1 := strconv.Atoi(count)
	u, err2 := strconv.ParseInt(until, 10, 64)
	if !ok || err1 != nil || err2 != nil || n < 0 || u < 0 {
		return 0, 0, 0, ErrLock
	}
	return n, u, b.Updated, nil
}

// Check returns how many seconds name still has to wait, 0 when it is free.
// The answer is never longer than maxWait: no failure this package counts
// asks for more, so a row that lies further ahead -- a clock that jumped
// forward and came back, a number that went wrong -- would otherwise shut a
// name out for as long as that number happens to say.
func Check(ctx context.Context, db dbapi.Local, name string, now int64) (int64, error) {
	return check(ctx, db, nameSpace, name, now)
}

// CheckKey is Check for the key ID of a proof whose key the trust list does
// not hold. It reads the other counting, and nothing it is ever asked about
// can reach a login name's row -- see nameSpace and keySpace for why that
// matters.
func CheckKey(ctx context.Context, db dbapi.Local, keyID string, now int64) (int64, error) {
	return check(ctx, db, keySpace, keyID, now)
}

func check(ctx context.Context, db dbapi.Local, space, name string, now int64) (int64, error) {
	if !validNow(now) {
		return 0, ErrLock
	}
	_, until, _, err := read(ctx, db, space, name, now)
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
//
// The counting climbs, and it falls back on its own: a bucket that has not
// counted for keepFor starts at zero, because the row it left behind is past
// its keep time and reads as none (see read). Nothing outside this package has
// to run for that, and a first failure after a quiet hour costs what a first
// failure costs -- not the longest wait there is. The hour is counted from the
// latest time a failure of that bucket was counted at, so setting a clock back
// does not shorten it. A time that lies ahead of the clock stretches it by as
// much as it lies ahead: that is the price of the same line, and it stands with
// the line (see kept below).
func Fail(ctx context.Context, db dbapi.Local, name string, now int64) error {
	return fail(ctx, db, nameSpace, name, now)
}

// FailKey is Fail for the key ID of a proof whose key the trust list does not
// hold. It counts in the other counting, so no failure it is ever given lays
// a wait on a login name -- see nameSpace and keySpace.
func FailKey(ctx context.Context, db dbapi.Local, keyID string, now int64) error {
	return fail(ctx, db, keySpace, keyID, now)
}

func fail(ctx context.Context, db dbapi.Local, space, name string, now int64) error {
	if !validNow(now) {
		return ErrLock
	}
	counting.Lock()
	defer counting.Unlock()
	n, until, seen, err := read(ctx, db, space, name, now)
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
	// A failure never moves the keep time of a row backwards: of the time this
	// failure is counted at and the one the row already carries, the later one
	// stands. A clock set back further than keepFor would otherwise leave a row
	// whose two times both say expired as soon as the clock is right again, and
	// the lock that was running would be gone with the count -- a wait must not
	// be something anybody turns off by turning a clock. seen is a time out of
	// the row, so it is taken only where it is one a clock gives, and kept +
	// keepFor cannot run over.
	//
	// The price of that line, chosen and not overlooked: a failure counted
	// while the clock ran ahead leaves a row whose keep time lies that far
	// ahead, and the bucket stays shut until the clock has caught up with it --
	// for as long as the clock was ahead, which is no wait any failure here
	// asks for. It heals by itself and needs nobody to run anything. The other
	// side was measured and is worse: cap the time a row brings along at keepFor
	// ahead, and a clock set back by 2*keepFor or more drops a lock that is
	// running to no wait at all and its count to zero -- whoever can turn the
	// clock turns the lock off. Out of one reading the two cannot be told apart:
	// "the row lies ahead because the clock ran ahead" and "the clock is behind
	// right now" are the same shape, a keep time above now. So this takes the
	// side that keeps the lock. Telling them apart needs a time this package is
	// not given -- a source of its own in dbapi, which is on the list for V1.2 --
	// and no further line here does it.
	kept := now
	if validNow(seen) && seen > kept {
		kept = seen
	}
	// Seq and Updated are the time the row may be removed. A walk of the rows
	// gets both (dbapi.BlobFrom), so Purge asks them the same question the
	// counting asks and needs nothing of the data (see expired).
	return db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key(space, name),
		Seq: uint64(kept + keepFor), Data: []byte(strconv.Itoa(n) + " " + strconv.FormatInt(until, 10)),
		Updated: kept})
}

// Clear removes the bucket row of one name. It goes through the same step as
// the counting: between reading a row and writing it back, a delete would be
// undone by the write that follows it. The row is the bucket of the name, so a
// name that shares it is cleared along with it -- see buckets.
//
// NOT after a successful login, which is what this sentence used to say.
// Measured (grep -rn '\.Clear(' over apps and shared without tests): one
// production caller, setup.finish -- the setup is over for good at that point,
// so its bucket has nothing left to count. login/finish.go says the opposite
// in so many words ("a login that worked clears no bucket") and gives the
// reason: a bucket holds every name that falls into it, so a login clearing
// its own bucket would let anybody wipe a stranger's count by logging in.
func Clear(ctx context.Context, db dbapi.Local, name string) error {
	counting.Lock()
	defer counting.Unlock()
	return db.BlobDelete(ctx, dbapi.BlobSetting, key(nameSpace, name))
}

// Purge removes lock rows whose time has passed, of both countings at once.
// Other settings stay. It never tells the two apart -- it takes the union of
// them, which is all a walk that frees space has to do. A time no clock gives
// removes nothing at all: every row still counting lies ahead of it, and
// freeing them all at once is exactly what a wrong number must never do.
//
// No lock depends on it, for every row this package writes. Purge asks a row
// the same question the counting asks (see expired), and such a row carries
// Seq = Updated + keepFor, which makes the two halves of that question the one
// question -- so it takes that row exactly when the counting reads it as none.
// The two reach a row by different calls, the counting through BlobGet and this
// walk through BlobFrom, and dbapi's own test pins Updated only through the
// first and Seq only through the second; with a row of other times, where the
// two halves do not fall together, a store that hands one of them back empty on
// one of the two paths can therefore free a row the counting still reads. What
// Purge frees for the rows written here is the space they take, not a name:
// whoever wires it up may run it as often or as seldom as he likes.
//
// It holds counting over the whole walk, so nothing is counted between the
// walk and the deletes that follow it: a row that the walk saw expired would
// otherwise be a lock somebody has just set, and removing it would set that
// lock to none. The walk is bounded by twice buckets rows -- the two countings
// have that many each -- and the other settings, and no failure of a login
// waits on more than that.
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
		ours := strings.HasPrefix(b.ID, nameSpace) || strings.HasPrefix(b.ID, keySpace)
		if !ours || !expired(b, now) {
			continue
		}
		if err := db.BlobDelete(ctx, dbapi.BlobSetting, b.ID); err != nil {
			return err
		}
	}
	return nil
}
