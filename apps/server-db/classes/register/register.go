// Package register is the seam between the API and this part: it hands
// apps/server-api/classes/dbopen the two functions that package declares but
// deliberately does not contain -- the opener and the migrator.
//
// It exists because of a measured hole. dbopen declares Register and
// RegisterMigrator and says in its own head that "apps/server-db ... registers
// its opener here"; until 12.10.2026 nothing but tests ever called either, so
// server.Build ran into ErrNoOpener and ngc-api could not start at all
// (docs/migrationen-wer-spielt-sie-ein.md, "Die Naht dbopen.RegisterMigrator").
// Every part was built right and the registration was nobody's task.
//
// It registers in init and is meant to be imported blank:
//
//	import _ ".../apps/server-db/classes/register"
//
// That is what dbopen asks for in so many words -- "whoever builds a program
// decides with one blank import whether the database part is in it" -- and it
// is what keeps the API free of SQL: nothing here is called from the API's
// code, so a program that leaves the import out carries neither driver nor
// migration and says so with ErrNoOpener instead of running half way.
//
// What this package does NOT do: it does not decide WHEN migrations run. On
// SQLite sqlite.Open migrates at every open, on MariaDB mariadb.Open migrates
// never -- both of those are decisions of those two files and are left exactly
// as they stand. This file only names which function belongs to which driver.
package register

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/mariadb"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migraterun"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

func init() {
	dbopen.Register(open)
	dbopen.RegisterMigrator(migrate)
}

// open picks the driver and opens the store.
//
// The driver is NAMED and not guessed, and an unknown one comes back as the
// error below rather than as a nil store: config.DB.Validate refuses a driver
// it does not know already, so this is the second bolt for a configuration
// that reached here without going through it. Returning nil, nil here would
// turn a wrong line in api.json into a nil dereference in the first request
// that touches the database -- and this repository has no recover anywhere in
// production code (Verträge V1.2, Punkt 38), so that would be the whole
// process.
//
// ctx is not passed on, and that is the signature of the two Opens and not an
// oversight: database/sql opens its connections from a context of its own
// (see the head of sqlite.OpenDB's connector), so neither Open takes one.
func open(_ context.Context, cfg config.DB, owner string) (dbapi.Store, error) {
	switch cfg.Driver {
	case config.DriverSQLite:
		// now is read here, at the open, because the migration lock this
		// start may take belongs to this one start.
		return sqlite.Open(cfg, owner, time.Now().Unix())
	case config.DriverMariaDB:
		// mariadb.Open runs no migration: that is the ngc_migrate account's
		// job and the API does not have it. owner is therefore unused on this
		// side, and that asymmetry is the contract of the two files, not a
		// gap here.
		return mariadb.Open(cfg)
	}
	return nil, fmt.Errorf("register: the configuration names the database driver %q, and there is none of that name", cfg.Driver)
}

// migrate is ngc-api migrate. It fits migraterun.Run, which answers how many
// migrations it played, to dbopen.Migrator, which answers nothing and writes
// to out: the number is what a human reads, so it goes to out as a German
// line, and nothing else does.
//
// out may be nil -- dbopen.Migrate makes no promise about it -- and a nil
// io.Writer would be a nil dereference in Fprintf. The count is then dropped
// and the error is not.
func migrate(ctx context.Context, cfg config.DB, owner string, out io.Writer) error {
	n, err := migraterun.Run(ctx, cfg, owner, time.Now().Unix())
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if n == 1 {
		_, err = fmt.Fprintln(out, "1 Migration eingespielt.")
		return err
	}
	_, err = fmt.Fprintf(out, "%d Migrationen eingespielt.\n", n)
	return err
}
