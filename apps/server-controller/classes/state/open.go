// Where this controller's own store is named. Exactly one file in the whole
// controller names an implementation of it, and this is that file: everything
// else works on *Store, and *Store works on dbapi.Local, so another store
// takes one change here and none anywhere else.
package state

import (
	"errors"
	"fmt"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

// KeySize is the key of the file: 64 bytes, two AES-256 keys for AES-256-XTS
// (config.Controller.DBKey carries exactly this many).
const KeySize = 64

// ErrKey says the key of the local database is not a key of this file format.
//
// What the check in front of it is FOR, measured and not supposed: the store
// refuses a key of the wrong length by itself (sqlite.OpenDB, "the key must
// be 64 bytes for AES-256-XTS") and leaves no file behind either, so a mutant
// that takes the length check out of Open still fails every open - it LIVED
// in the first round of mutants, which is how this was found. The check earns
// its place by the NAME of the answer and by nothing else: a caller of this
// package can ask errors.Is(err, ErrKey) and tell "this server's key is
// wrong" from "this server's database is broken", and the plain error of the
// store underneath carries no such name. TestOpenNamesAKeyThatIsNoKey holds
// exactly that, and a mutant without the check now dies there.
var ErrKey = errors.New("state: the database key needs 64 bytes for AES-256-XTS")

// Open opens the controller's own encrypted SQLite and brings its schema up to
// date. owner names who is migrating and goes into the migration lock; now is
// the time that lock records, in seconds.
//
// Two things about those migrations, and both come from the head of
// sqlite.Open rather than from a guess here:
//
//   - On SQLite EVERY caller of this function migrates at its own start. The
//     rule "the migrations of an update from one place" holds for MariaDB,
//     where one controller with the role db runs them through ngc_migrate -
//     not here: this file has no second account to run them under and no
//     second machine to run them from.
//   - What keeps two callers apart is the migration lock (ngc_lock), and owner
//     stands in it so that whoever looks can see which one held it. A second
//     process that meets the lock does NOT wait and does NOT migrate: this
//     call comes back with an error, and a controller that gets one does not
//     come up. Measured in TestASecondControllerOnTheSameFileIsTurnedAway -
//     "it holds according to the comment" is no measurement, and a pair of
//     lines that lean on each other and hold nothing together is exactly the
//     finding W-1 of the db package.
//
// What this does NOT do is run the migrations of an UPDATE. Those are
// ngc-api migrate, a program of its own (docs/vertraege-v1.2.md, point 28).
// This is a file opening its own schema, which is a different thing: it has no
// ledger to write and no second server to wait for.
func Open(cfg config.DB, owner string, now int64) (*Store, error) {
	// Before the file: a key of the wrong length is this server's
	// configuration and nothing a peer brings about.
	if len(cfg.Key.Bytes()) != KeySize {
		return nil, ErrKey
	}
	db, err := sqlite.Open(cfg, owner, now)
	if err != nil {
		// Wrapped and not passed through: whoever reads this knows which of
		// the stores of this installation it was. Nothing of the key is in
		// it - config.DB prints every secret as "[secret]" (config.DB.String).
		return nil, fmt.Errorf("state: the local database of this controller did not open: %w", err)
	}
	return New(db), nil
}
