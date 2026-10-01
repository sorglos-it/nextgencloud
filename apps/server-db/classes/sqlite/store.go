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
