package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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
	if err := checkTarget(cfg.Path, target); err != nil {
		return err
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
// What the lock does, and what it does NOT do. The transaction writes nothing
// and still holds the write lock (_txlock=immediate makes it a BEGIN
// IMMEDIATE, so the lock is taken when the transaction begins and not at a
// first write), so from then on no other connection commits. Measured on
// 10.10.2026 under eight writers, 1 MiB each, eight copying windows: the
// JOURNAL stood still in 8 of 8 windows, and the FILE changed in 7 of 8 -
// with the lock held. A control run with the writers quiet showed the file
// unchanged in 3 of 3, so the measurement sees what it claims to see.
//
// So the lock freezes the journal, not the file. Suspicion, not measured: the
// automatic checkpoint runs after the commit, that is after the writer has
// given the lock back, and it then moves pages into the file while this reads
// it. An earlier version of this comment claimed the opposite - that no other
// connection moves a page out of the journal - and that was wrong.
//
// Therefore the promise "a copy that is consistent in itself" does NOT rest
// on the file standing still. It rests on the file and the journal being
// copied TOGETHER: replaying every frame of the journal over the file repairs
// a file that a checkpoint tore while it was read, and fold below does that
// replay on the copy. Take the journal away and the copy is torn - that is
// the mutant that dies (m4 of the report), not the one that takes the lock
// out.
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
// Why the lock stays although no test here falls without it: taking it out
// leaves TestBackupHoldsUnderWriteLoad green, measured 03.10.2026 and
// confirmed 10.10.2026 (mutant m1, alive, code 0). What it rules out is the
// one case no test here can schedule: a writer restarting the journal from
// its first frame between the two copies below, which would leave the copy
// with a file missing pages and a journal that no longer carries them. That
// case was looked for and is out of reach here - 14 680 probes in twelve
// seconds showed one single WAL header and a journal growing monotonically
// from 20 to 169 MB, so it never started over. The lock stays because it
// costs one short transaction, not because a test demands it.
func freeze(ctx context.Context, conn *sql.Conn, path, tmp string) (bool, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	// Nothing to commit: the transaction is only here for its lock.
	defer tx.Rollback()
	// One buffer for both files: the copies run one after the other, and the
	// whole point of copyBuf is that this number does not grow with the
	// database.
	buf := make([]byte, copyBuf)
	if _, err := copyFile(path, tmp, buf); err != nil {
		return false, err
	}
	// SQLite names the journal after the file.
	journal, err := copyFile(path+"-wal", tmp+"-wal", buf)
	if err != nil {
		return false, err
	}
	if !journal {
		// An empty journal is no journal, and an empty file beside the copy
		// would survive the rename under the backup's own name.
		os.Remove(tmp + "-wal")
	}
	return journal, nil
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
	// Closed here and not only by the defer above: removeBeside may run only
	// once the last connection is gone, or SQLite writes the journal again
	// behind it. The defer then closes a pool that is already closed, which
	// sql.DB answers with nil - said here because a second Close reads like
	// an oversight.
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

// beside names a database and everything that belongs to it: the file itself,
// the journal and the shared-memory file SQLite keeps next to it. Writing any
// of the three while the database is open is writing the database.
func beside(path string) []string {
	return []string{path, path + "-wal", path + "-shm"}
}

// checkTarget refuses a target that would write the database it is copying.
//
// Comparing the two strings is not enough, and that was measured on
// 10.10.2026: cfg.Path goes through validPath (absolute and filepath.Clean),
// the target went through nothing, so "/dir/./state.db" and
// "/dir/sub/../state.db" named the database and did not equal its path. They
// reached os.Rename; what refused them was Windows, not this code, and on
// linux/amd64 and linux/arm64 - the targets NGC builds for - os.Rename would
// have replaced the running database. So the target goes through the same
// check as the path, and both are compared only once they are clean.
//
// The journal and the shared-memory file count as the database: a copy
// landing on "state.db-wal" destroys the transactions the file does not carry
// yet. The ".part" name belongs in the comparison too, because that is the
// name the copy carries while it is written, before the rename.
//
// A name is not the only way to reach the same file, so two more questions
// follow the comparison.
//
// A target that is a symbolic link is refused outright, and that is the safer
// of the two readings: following it would have to answer where it points at
// the moment of the write, which is not the moment it was looked at. The
// price, in one sentence: a backup cannot be written through a link, not even
// one that points somewhere harmless. NOT MEASURED HERE - this machine does
// not let a test make a symbolic link ("Dem Client fehlt ein erforderliches
// Recht"), and no Linux Go toolchain is on it either, which was looked for.
// TestBackupRefusesALinkAsItsTarget says so and skips loudly instead of
// passing.
//
// And a target that IS the database without being a link to it: a hard link
// carries no mark of its own, Lstat calls it a plain file, and writing
// through it writes the database. os.SameFile answers that from what the
// operating system knows about the two files instead of from their names, and
// it is measured, on a hard link, in
// TestBackupRefusesAnotherNameOfTheSameFile.
func checkTarget(path, target string) error {
	if target == "" {
		return errors.New("sqlite: die Sicherung braucht einen eigenen Dateinamen, nicht den der Datenbank")
	}
	if !validPath(path) {
		return errors.New("sqlite: der Pfad der Datenbank ist kein absoluter, einfacher Dateiname")
	}
	if !validPath(target) {
		return errors.New("sqlite: der Name der Sicherung muss ein absoluter, einfacher Dateiname sein")
	}
	// Every name the copy ever carries against every name the database owns.
	var mine []string
	for _, p := range beside(target) {
		mine = append(mine, p)
	}
	for _, p := range beside(target + ".part") {
		mine = append(mine, p)
	}
	for _, a := range mine {
		for _, b := range beside(path) {
			if a == b {
				return errors.New("sqlite: die Sicherung braucht einen eigenen Dateinamen, nicht den der Datenbank")
			}
		}
	}
	// Lstat, not Stat: Stat follows the link and would answer about the
	// database at the other end of it.
	if st, err := os.Lstat(target); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return errors.New("sqlite: der Name der Sicherung zeigt auf einen Verweis, nicht auf eine Datei")
	}
	// Here Stat is the right one: the question is no longer how the target is
	// written but which file it ends up being. A target that is not there yet
	// is the normal case and answers an error, which is not a finding.
	to, err := os.Stat(target)
	if err != nil {
		return nil
	}
	from, err := os.Stat(path)
	if err != nil {
		return nil // the missing database is reported by Backup itself, with its own sentence
	}
	if os.SameFile(to, from) {
		return errors.New("sqlite: der Name der Sicherung meint dieselbe Datei wie die Datenbank")
	}
	return nil
}

// copyBuf is how much of a file is in memory at a time. It is a fixed size
// and not the size of the file on purpose: the file and its journal together
// were measured at 49 MB on 03.10.2026, and the journal alone grew to 169 MB
// in twelve seconds under eight writers. Reading either one whole would go
// around the 512 MiB this package allows a connection (maxMemory) and would
// grow with the installation; a controller with 512 MB of RAM must not die of
// its own backup. 256 KiB holds the number of write calls down to four per
// mebibyte and is nothing next to that ceiling.
const copyBuf = 256 << 10

// copyFile writes src to dst byte for byte, in pieces of copyBuf. Nothing of
// the content is held longer than one piece, so what this costs does not
// depend on how big the database has become.
//
// It reports whether the source held anything: a journal that is not there
// and one of zero length both mean the file is the whole database already. A
// source that is there but empty is still copied, so the database itself
// always arrives under the copy's name, however small it is.
func copyFile(src, dst string, buf []byte) (bool, error) {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return false, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return false, fmt.Errorf("sqlite: die Sicherung ließ sich nicht schreiben: %w", err)
	}
	// Close before the caller goes on: the bytes of the last piece are in the
	// file only once this answered, and the copy is read again right after.
	if _, err := io.CopyBuffer(out, in, buf); err != nil {
		out.Close()
		return false, fmt.Errorf("sqlite: die Sicherung ließ sich nicht schreiben: %w", err)
	}
	if err := out.Close(); err != nil {
		return false, fmt.Errorf("sqlite: die Sicherung ließ sich nicht schreiben: %w", err)
	}
	return st.Size() > 0, nil
}
