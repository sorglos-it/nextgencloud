package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// Backup writes a copy of the encrypted file to target.
//
// The copy is the bytes of the file, not a dump: it is encrypted exactly like
// the original, with the same key from the configuration. Whoever keeps the
// backup has to keep that configuration somewhere else, or the copy can never
// be opened again - and whoever steals the backup alone has nothing.
//
// It does open the file, and it has to: a plain copy of a database in use is
// half of two states. What it does not do is go through the database for the
// content - it reads no row, applies no migration and adds no table, so what a
// backup holds is what lay in the file. It also never creates the file it is
// asked to copy: opening a missing path would make an empty database, and a
// backup of that, played back, is an erased installation.
func Backup(cfg config.DB, target string) error {
	if target == "" || target == cfg.Path {
		return errors.New("sqlite: die Sicherung braucht einen eigenen Dateinamen, nicht den der Datenbank")
	}
	// The file has to be there already. OpenDB below would create it.
	st, err := os.Stat(cfg.Path)
	if err != nil {
		return fmt.Errorf("sqlite: die Datenbank ist nicht da, es gibt nichts zu sichern: %w", err)
	}
	if !st.Mode().IsRegular() {
		return errors.New("sqlite: der Pfad der Datenbank zeigt nicht auf eine Datei")
	}
	db, err := OpenDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Written beside the target and renamed at the end, so a backup is never
	// half a file: under the target's own name a reader finds either the whole
	// copy or nothing.
	tmp := target + ".part"
	journal, err := freeze(ctx, conn, cfg.Path, tmp)
	if err != nil {
		removeCopy(tmp)
		return err
	}
	if journal {
		if err := fold(ctx, cfg, tmp); err != nil {
			removeCopy(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		removeCopy(tmp)
		return fmt.Errorf("sqlite: die Sicherung ließ sich nicht ablegen: %w", err)
	}
	return nil
}

// freeze copies the file, and the journal beside it where there is one, to tmp
// while nobody may change either of them. It reports whether a journal came
// along.
//
// Why the two of them stand still: the transaction writes nothing and still
// holds the write lock (_txlock=immediate makes it a BEGIN IMMEDIATE, so the
// lock is taken when the transaction begins and not at a first write). From
// then on no other connection commits - and because only a commit starts an
// automatic checkpoint, no other connection moves a page from the journal into
// the file either.
//
// Why the journal is copied instead of being folded into the file first, which
// would make the copy one file right away: every mode of PRAGMA wal_checkpoint
// that empties the journal wants that same write lock, and the passive mode,
// which does not want it, does not get through either while writers keep
// coming. Both measured on 03.10.2026 under eight writers of 1 MiB each:
// TRUNCATE answers busy while the lock is held, and PASSIVE answered busy in
// all twenty tries over two seconds. A file and the journal beside it are a
// database together, and fold below turns the pair into one file afterwards,
// on the copy, where no other writer is in the way.
//
// Measured on the same day, and it is the reason this paragraph says "why" and
// not "or else": taking the lock out leaves TestBackupHoldsUnderWriteLoad
// green. Copying the pair is consistent even without it, because replaying
// every frame of the journal over the file repairs a file that a checkpoint
// tore while it was read. What the lock still rules out is the one case no
// test here can schedule: a writer restarting the journal from its first frame
// between the two reads below, which would leave the copy with a file missing
// pages and a journal that no longer carries them. It stays, because it costs
// one short transaction.
func freeze(ctx context.Context, conn *sql.Conn, path, tmp string) (bool, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	// Nothing to commit: the transaction is only here for its lock.
	defer tx.Rollback()
	body, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	// SQLite names the journal after the file. One that is not there and one of
	// zero length both mean the file is the whole database already.
	journal, err := os.ReadFile(path + "-wal")
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return false, fmt.Errorf("sqlite: die Sicherung ließ sich nicht schreiben: %w", err)
	}
	if len(journal) == 0 {
		return false, nil
	}
	if err := os.WriteFile(tmp+"-wal", journal, 0o600); err != nil {
		return false, fmt.Errorf("sqlite: die Sicherung ließ sich nicht schreiben: %w", err)
	}
	return true, nil
}

// fold makes the copy stand on its own: opening the pair with the same key
// replays the journal into the copy, and the checkpoint empties it. It runs on
// the copy and not on the database, so no other writer is in the way and the
// mode that wants the write lock gets it.
//
// Nothing of the content passes through here either: SQLite moves its own
// pages, with the key from the configuration, so the copy stays encrypted
// exactly as the original is.
func fold(ctx context.Context, cfg config.DB, tmp string) error {
	cfg.Path = tmp
	db, err := OpenDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	var busy, frames, folded int64
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &folded); err != nil {
		return err
	}
	// SQLite's own answer is what says it worked: whether it had to give up,
	// and how many frames are left in the journal. A copy whose journal still
	// holds frames is not a database on its own, and renaming it into place
	// would hand out a backup missing its newest transactions - or half of one.
	//
	// No test here makes this answer come back: the copy is a file nothing else
	// has a connection to, so there is nobody to be busy against and nothing
	// to hold a frame back. Taking the check out therefore leaves every case
	// green (measured 03.10.2026). It is the answer of the one step that is
	// allowed to fail silently, and it is two comparisons.
	if busy != 0 || frames != 0 {
		return fmt.Errorf("sqlite: die Sicherung ließ sich nicht abschließen: %d Einträge blieben im Journal (busy %d)", frames, busy)
	}
	// Closing the last connection takes the journal and the shared-memory file
	// away by itself; both are removed anyway, so the copy is one file and the
	// rename leaves nothing beside it.
	if err := db.Close(); err != nil {
		return err
	}
	removeBeside(tmp)
	return nil
}

// removeCopy takes away a copy that never got its name, journal and all.
// os.Remove's own errors are dropped on purpose: they would hide the error that
// led here, and what stays behind carries the ".part" name, which is never
// handed out as a backup.
func removeCopy(tmp string) {
	os.Remove(tmp)
	removeBeside(tmp)
}

// removeBeside takes away the two files SQLite keeps next to a database.
func removeBeside(tmp string) {
	os.Remove(tmp + "-wal")
	os.Remove(tmp + "-shm")
}
