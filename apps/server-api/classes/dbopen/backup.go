package dbopen

import (
	"context"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// Backuper writes a copy of the database cfg names to target. apps/server-db
// implements it on its own copier -- the write lock, the journal beside the
// file and the fold of the two into one -- and registers it here, exactly as
// it registers Opener and Migrator.
//
// It is a registration and not a call into apps/server-db for the reason the
// other two are: the API holds no driver and no SQL, and whoever builds a
// program decides with one blank import whether the database part is in it.
//
// What a Backuper writes is the ENCRYPTED file, not a dump. The key stays in
// the configuration and never travels with the copy, so a backup alone opens
// nothing -- which is the point when it is stolen, and the trap when it is
// the only thing that was kept. Whoever calls this has to say that sentence
// to the admin; the copy itself cannot.
//
// owner is deliberately not an argument, unlike with Opener and Migrator: a
// backup takes no migration lock and writes no row, so there is no ledger for
// a name to go into. Handing one in would promise a trace that nothing keeps.
type Backuper func(ctx context.Context, cfg config.DB, target string) error

// ErrNoBackuper reports that no part registered a backuper, so
// "ngc-api backup" cannot run.
//
// It is its own value and neither ErrNoOpener nor ErrNoMigrator: a program may
// well open a database, even migrate it, and still carry no way to copy it.
// The one answer that must never come out of a program built that way is
// "done" with nothing written -- an admin then keeps a backup plan that has
// never made a file.
var ErrNoBackuper = errors.New("dbopen: no database backup registered")

var backuper Backuper

// RegisterBackuper makes b the backuper. apps/server-db calls it once.
//
// It shares the mutex of Register for the reason RegisterMigrator does: all
// three are set in the same init phase and read later, and one lock over the
// three is one order to think about.
func RegisterBackuper(b Backuper) {
	mu.Lock()
	defer mu.Unlock()
	backuper = b
}

// Backup runs the registered backuper and hands its error back BARE.
//
// Bare, because every sentence that part produces already names the step that
// gave way -- the target, the missing database, the journal mode, the write
// lock another program holds -- and in German, for the admin who has to act
// on it. A sentence of this file in front of it would say nothing that one
// does not and would push the one that matters to the end of the line.
func Backup(ctx context.Context, cfg config.DB, target string) error {
	mu.Lock()
	b := backuper
	mu.Unlock()
	if b == nil {
		return ErrNoBackuper
	}
	return b(ctx, cfg, target)
}
