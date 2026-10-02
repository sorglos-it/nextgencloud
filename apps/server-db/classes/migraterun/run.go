// Package migraterun is the runner of the migrations: it picks the driver out
// of the configuration, opens the database, plays the migrations in and
// closes again. That is the whole of what "ngc-api migrate" does - one
// controller with role db starts it before the new API, with the migrate
// account from its controller.json.
//
// It is a package of its own and not part of migrate, and the reason is
// measured: apps/server-db/classes/sqlite imports migrate (sqlite.Open plays
// the migrations in), so an import of sqlite from inside migrate is a ring -
// "import cycle not allowed". migrate stays a library that knows SQL and no
// driver; the runner, which needs a concrete one, sits above both.
package migraterun

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/mariadb"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

// open picks the driver from the configuration and gives back the dialect
// that belongs to it. A driver neither opener knows is named, not guessed:
// config.DB.Validate refuses it already, and this is the second bolt for a
// configuration that reached here without going through it.
func open(cfg config.DB) (*sql.DB, sqlstore.Dialect, error) {
	switch cfg.Driver {
	case config.DriverMariaDB:
		db, err := mariadb.OpenDB(cfg)
		return db, sqlstore.MariaDB, err
	case config.DriverSQLite:
		db, err := sqlite.OpenDB(cfg)
		return db, sqlstore.SQLite, err
	}
	return nil, nil, fmt.Errorf("migraterun: unknown driver %q", cfg.Driver)
}

// Run plays every missing migration in and returns how many it played. It is
// the one call ngc-api migrate needs. owner shows up in the migration lock so
// an admin sees which server is migrating, now is the time it records.
func Run(ctx context.Context, cfg config.DB, owner string, now int64) (int, error) {
	db, d, err := open(cfg)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return migrate.Apply(ctx, db, d, owner, now)
}

// Needed reports whether anything is missing, without changing a thing. The
// API asks before it starts: a database that is behind means the update did
// not finish.
//
// The one branch worth reading twice is the error of migrate.Level. A
// database that never saw a migration has no ngc_schema, so Level fails
// there - and a database that cannot be read at all fails there too, with a
// wrong key or a file that is no database. Answering "yes, migrate" to both
// would hand a broken database to the migrations and let the real reason
// disappear, so the connection is asked first: if the database answers at
// all, the missing table is the only reason left and it means none ever ran.
func Needed(ctx context.Context, cfg config.DB) (bool, error) {
	files, err := migrate.List()
	if err != nil {
		return false, err
	}
	// A bolt that cannot be reached today, and NO case holds it: the
	// migrations are built into the program, so no test can make List come
	// back empty, and the mutation round says so - this line survives being
	// changed. It stays because the last line of this function indexes the
	// slice, and an index without a bound in front of it is how a program
	// dies instead of reporting.
	if len(files) == 0 {
		return false, fmt.Errorf("migraterun: there is no migration at all")
	}
	db, _, err := open(cfg)
	if err != nil {
		return false, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return false, fmt.Errorf("migraterun: the database does not answer: %w", err)
	}
	level, err := migrate.Level(ctx, db)
	if err != nil {
		return true, nil
	}
	return level < files[len(files)-1].Version, nil
}
