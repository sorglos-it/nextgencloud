package state

import (
	"fmt"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

// Backup writes a copy of this controller's own database to target.
//
// It stands beside Open in this one file for the reason the head of the file
// gives: exactly one place names the implementation of this store, and a copy
// of the store is as much a part of that as opening it. A second place would
// be a second answer to "which file format is a controller's database".
//
// The copy is the encrypted bytes, not a dump. The key stays in
// controller.json (config.Controller.DBKey) and nothing of it is in the file,
// so this copy alone opens on no machine - which is the point when it is
// stolen and the trap when it is the only thing that was kept. Whoever calls
// this has to say that to the admin; the file cannot.
//
// It runs WHILE the controller runs, and that is measured rather than hoped:
// two programs on one SQLite file are safe, only slow (Verträge V1.2,
// Punkt 48), and the copier takes the write lock for one short transaction,
// reads no row and plays no migration. So a backup does not ask an admin to
// stop the server first - and before this existed, stopping it and copying
// the file by hand was the only way there was.
//
// What it does NOT do is migrate. Open does that at every open, because a
// controller that comes up has to be on this release's schema; a copy has to
// be what lay in the file, and a backup that migrated on its way out would
// hand back something the installation never had.
func Backup(cfg config.DB, target string) error {
	// Before the file, and in the same order as Open: a key of the wrong
	// length is this server's configuration and has its own name, so that an
	// admin reads "the key of this server is wrong" and not "your database is
	// broken". Without this the store answers that it cannot use the key, and
	// that sentence does not name the server.
	if len(cfg.Key.Bytes()) != KeySize {
		return ErrKey
	}
	// And the driver, for the reason Open refuses another one: a controller's
	// own database is this file format. A MariaDB configuration must not be
	// copied as though its data lay in a file of this machine - they lie on
	// the database servers, and ngc-db copies them there.
	//
	// What this check is really FOR, measured and not supposed: the copier
	// underneath refuses a wrong driver by itself with
	// `sqlite: driver "mariadb", want "sqlite"`, so a mutant that takes these
	// lines out is refused all the same (m4 of this round, which first LIVED
	// against a case that had no file on disk and was therefore turned away
	// one wall earlier). So this earns its place by the NAME of the answer
	// and by nothing else: an admin of a cluster reads which program does it
	// instead of a sentence about a driver, and the sentence underneath
	// carries no such name. The same shape as ErrKey in Open.
	if cfg.Driver != config.DriverSQLite {
		return fmt.Errorf("state: die eigene Datenbank dieses Controllers ist eine SQLite-Datei, "+
			"die Konfiguration nennt aber %q - gesichert wird ein MariaDB-Verbund auf den "+
			"Datenbankservern mit \"ngc-db backup\"", cfg.Driver)
	}
	if err := sqlite.Backup(cfg, target); err != nil {
		// Wrapped for the reason Open wraps: whoever reads this knows which
		// of the stores of this installation it was. Nothing of the key is in
		// it - config.DB prints every secret as "[secret]".
		return fmt.Errorf("state: die eigene Datenbank dieses Controllers ließ sich nicht sichern: %w", err)
	}
	return nil
}
