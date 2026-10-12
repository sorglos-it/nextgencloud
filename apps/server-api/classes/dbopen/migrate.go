package dbopen

import (
	"context"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// Migrator brings the schema of cfg to the state this release needs and
// writes what it did to out. apps/server-db implements it on top of its
// migraterun package -- the lock, the ledger and the linter that already sit
// there -- and registers it here, exactly as it registers its Opener.
//
// It is a registration and not a call into apps/server-db for the same reason
// Opener is: the API holds no driver and no SQL, and whoever builds a program
// decides with one blank import whether the database part is in it.
type Migrator func(ctx context.Context, cfg config.DB, out io.Writer) error

// ErrNoMigrator reports that no part registered a migrator, so ngc-api migrate
// cannot run. It is its own value and not ErrNoOpener: a program may well be
// able to open a database and still not carry the migrations.
var ErrNoMigrator = errors.New("dbopen: no database migrations registered")

var migrator Migrator

// RegisterMigrator makes m the migrator. apps/server-db calls it once.
//
// It shares the mutex of Register because both are set in the same init phase
// and read later; one lock over both is one order to think about.
func RegisterMigrator(m Migrator) {
	mu.Lock()
	defer mu.Unlock()
	migrator = m
}

// Migrate runs the registered migrator. This is the whole of what
// "ngc-api migrate" does, and the decision behind it is that there is exactly
// one runner over one schema: the controller starts this program and gets no
// runner of its own, because two runners over one schema is the finding W-1 of
// the db part (Verträge V1.2, Punkt 28).
func Migrate(ctx context.Context, cfg config.DB, out io.Writer) error {
	mu.Lock()
	m := migrator
	mu.Unlock()
	if m == nil {
		return ErrNoMigrator
	}
	return m(ctx, cfg, out)
}
