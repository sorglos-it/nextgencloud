package sqlite

import (
	"context"
	"database/sql"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// New wraps an open file as a store. It creates nothing.
func New(db *sql.DB) dbapi.Store { return sqlstore.New(db, sqlstore.SQLite) }

// Open opens the file, brings the schema up to date and returns the store.
// The file belongs to whoever holds the key, so the migrations run here and
// need no separate account, unlike MariaDB. owner shows up in the migration
// lock, now is the time it records.
//
// So "the migrations of an update from one place" - the heading of the commit
// that built migraterun - holds for MariaDB and NOT here (Kl-5 of the review
// of 10.10.2026). There, one controller with the role db runs them through
// ngc_migrate; on SQLite EVERY caller of this function migrates at its own
// start, with a context of its own. Decided and named: the file has no second
// account to run them under and no second machine to run them from, the
// migration lock (ngc_lock) is what keeps two callers of this function apart,
// and owner is in it so that whoever looks can see which one held it.
func Open(cfg config.DB, owner string, now int64) (dbapi.Store, error) {
	db, err := OpenDB(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := migrate.Apply(context.Background(), db, sqlstore.SQLite, owner, now); err != nil {
		db.Close()
		return nil, err
	}
	return New(db), nil
}
