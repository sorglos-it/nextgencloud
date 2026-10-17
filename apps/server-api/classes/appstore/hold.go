package appstore

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// THE PACKAGE OF AN UPLOAD IS KEPT UNTIL AN ACTIVATION FETCHES IT, and where
// it is kept is this file's whole subject. Punkt 30 named the hole and left
// the decision open on purpose: ngc.store/upload keeps only the ui-tar, and
// ActivateRequest.File belongs to the CALLER, so until now an activation of an
// uploaded app (core.LocalStore) found no file and ended E_UNREACHABLE.
// Measured on a running installation on 17.10.2026, after the upload itself
// was fixed: the package arrived, the node was offered as "ngc-2", the admin
// had the right -- and activating it answered "Dienst nicht erreichbar."
//
// WHAT IS DECIDED HERE, and the questions Punkt 30 asked are answered in order:
//
//   - WHERE. In a directory the SERVER names (Stores.Hold), not in a path a
//     handler made up. Empty means nothing is kept, which is exactly the
//     behaviour before this file existed -- a server that does not wire it
//     keeps answering E_UNREACHABLE, and no case about that changes meaning.
//
//   - WHICH FILE IS WHICH. The name is the package's own SHA-256 and nothing
//     else: an activation carries that sum in the task an admin SIGNED
//     (core.ActivateParams.SHA256), so the file is found by the one value both
//     ends already agree on. Two uploads of the same bytes are one file, an
//     upload of other bytes can never be taken for this one, and no row, no
//     clock and no name of a person is needed to find it. The sum is checked
//     again after the copy, where it always was (fetchPackage), so a file
//     somebody swapped under us is refused there rather than trusted here.
//
//   - SEVERAL API SERVERS SHARE THE DATABASE, NOT THE DISK. A package uploaded
//     to one of them is not on the other, and an activation that reaches the
//     other one answers E_UNREACHABLE just as before. That is a real limit and
//     not a bug of this file: the alternative is a 4 GiB file in a shared
//     database, and shared/dbapi has no place for one. A shell talks to ONE
//     API over its session, so upload and activate land on the same server in
//     the ordinary case; a reconnect between the two is what breaks it, and
//     then the admin uploads again.
//
//   - WHAT STAYS BEHIND AFTER A CRASH. Everything in the hold is swept by AGE,
//     at every upload, so a package nobody activated and a temporary file of
//     an upload that died halfway both go the same way. Age and nothing else:
//     a sweep by count or by size would delete the package of the admin who is
//     about to activate it, and "the newest are kept" is not a promise anybody
//     can read off the screen.
//
// THE PRICE, named rather than hidden: a package that was activated stays in
// the hold until it ages out, so a successful install leaves a copy of the
// software on the disk for up to HoldFor. That is deliberate -- an activation
// that failed halfway can be tried again without a second upload of gigabytes.
// And during an activation the package exists TWICE on disk, once in the hold
// and once in the spool, because the spool copy is what gets hashed and parsed
// (fetchPackage). Whoever sizes the data directory of an API server has to
// know both.

// DefaultHoldFor is how long a kept package waits for its activation. An admin
// uploads a package and clicks Aktivieren in the same sitting, so an hour is
// generous; what it has to cover is a person who is interrupted, not a person
// who comes back tomorrow.
const DefaultHoldFor = time.Hour

// holdPrefix names a temporary file of an upload in progress. os.CreateTemp
// puts digits behind it, so such a name is longer than a finished one -- a
// finished name is base64url of a SHA-256, 43 characters -- and a sweep needs
// no stat to tell the two apart. It does not have to: both go by age.
const holdPrefix = "ngc-upload-"

// errNoHold reports that this server keeps no uploaded packages at all
// (Stores.Hold empty). It never leaves this package: spoolPackage turns it
// into the ErrStore that was answered before the hold existed, so a server
// without a hold answers exactly as it did.
var errNoHold = errors.New("appstore: this server keeps no uploaded packages")

// SweepHold removes every kept package that is older than HoldFor. Upload
// does it too, at every upload; this is for whoever starts a server, so a
// machine that took one upload and was never asked again does not keep that
// package for ever.
//
// It is a SWEEP BY AGE and not an emptying, which is where it differs from
// the spool: a server empties the spool at every start (server.sweepSpool),
// because what lies there belongs to one call that is long over. A held
// package belongs to the next call, and a restart between upload and
// activation is exactly the case it has to survive.
func (st *Stores) SweepHold() { st.sweepHold(st.holdNow()) }

// holdFor is HoldFor, or DefaultHoldFor while nobody set it. A value of zero
// or less is not "immediately": it is a configuration nobody meant, and the
// default is the safer reading -- a sweep that deleted everything it saw would
// delete the package of the upload it runs in front of.
func (st *Stores) holdFor() time.Duration {
	if st.HoldFor <= 0 {
		return DefaultHoldFor
	}
	return st.HoldFor
}

// holdName is the file name of the package with that checksum: base64url of
// the sum, which is the same text the contract carries it as. A sum of another
// length than SHA-256 has no name, so nothing outside this package can ask for
// a file by a value it made up.
func holdName(sum []byte) (string, bool) {
	if len(sum) != sha256.Size {
		return "", false
	}
	return enc.B64.EncodeToString(sum), true
}

// newHeld opens a temporary file in the hold for an upload that is starting.
// It answers errNoHold while this server keeps nothing.
func (st *Stores) newHeld() (*os.File, error) {
	if st.Hold == "" {
		return nil, errNoHold
	}
	if err := os.MkdirAll(st.Hold, 0o700); err != nil {
		return nil, err
	}
	return os.CreateTemp(st.Hold, holdPrefix)
}

// keepHeld gives the temporary file its final name, the one an activation
// looks for. The file is closed either way: what is kept is a name on the
// disk, never an open handle.
//
// os.Rename and not a copy: it is the same directory, so the name appears
// whole or not at all and an activation can never open a half-written
// package. A name that is already there is REPLACED, and that is right -- the
// name is the checksum, so the bytes under it are the same bytes.
// TWO SYNCS AND NOT ONE, and they promise different things -- the same two
// shared/config.writeAtomic makes, and this had only the first until a second
// pair of eyes read it against that function:
//
//   - f.Sync() BEFORE the rename sures the CONTENT. Without it a machine that
//     loses power between the two can leave the right name over an empty or
//     half-written file, and the next activation reads a package that passes no
//     checksum.
//   - the directory sync AFTER the rename sures the RENAME. Without it the
//     crash leaves the OLD state: the file under its temporary name, which the
//     next sweep takes away. That is the harmless one of the two, and it is
//     tolerated where a system cannot sync a directory at all -- the file
//     itself is whole either way.
//
// WHAT IS MEASURED IS THE ORDER, not the durability: no case can cut the power,
// so nothing here can be held against a crash. But "sync, then rename, then
// sync the directory" is observable, and holdSteps below is what makes it so --
// a mutant that drops either sync loses a line in that list. Measured on
// 17.10.2026: before holdSteps existed, a mutant that dropped f.Sync() lived
// through the whole set.
func (st *Stores) keepHeld(f *os.File, sum []byte) error {
	name, ok := holdName(sum)
	if !ok {
		dropHeld(f)
		return ErrStore
	}
	if err := f.Sync(); err != nil {
		dropHeld(f)
		return err
	}
	holdStep("sync")
	tmp := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	dir := filepath.Dir(tmp)
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	holdStep("rename")
	// The error is not reported, as in writeAtomic: a system that cannot sync
	// a directory has still got the whole file under the right name, and
	// failing the upload over the weaker of the two promises would throw away
	// a package that is there.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
		holdStep("dirsync")
	}
	return nil
}

// holdSteps records the durability steps of keepHeld in the order they run. It
// is nil in operation and set by a case, and it exists because of what can and
// cannot be measured here: a crash cannot be staged, an ORDER can be read.
//
// A package-level variable and not a field of Stores, because what it watches
// is not a property of one installation but of this one function; the cases
// that set it do not run in parallel, and holdStep is what every writer goes
// through so there is one place to look.
var holdSteps func(step string)

func holdStep(step string) {
	if holdSteps != nil {
		holdSteps(step)
	}
}

// dropHeld throws a temporary file away, name and all. An upload that was
// refused leaves nothing: the package of a refused upload is a copy of
// software nobody accepted.
func dropHeld(f *os.File) {
	if f == nil {
		return
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
}

// heldPackage opens the kept package with that checksum and answers its size.
// A file that is not there, a sum of the wrong length and a server without a
// hold all answer an error: for the caller they are one case, "this server
// does not have that file".
func (st *Stores) heldPackage(sum []byte) (*os.File, int64, error) {
	if st.Hold == "" {
		return nil, 0, errNoHold
	}
	name, ok := holdName(sum)
	if !ok {
		return nil, 0, errNoHold
	}
	f, err := os.Open(filepath.Join(st.Hold, name))
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
		_ = f.Close()
		return nil, 0, ErrStore
	}
	return f, fi.Size(), nil
}

// sweepHold removes everything in the hold that is older than holdFor. It
// reports nothing: a sweep that failed must not cost the upload it ran in
// front of, and what it could not remove it sees again at the next one.
//
// The AGE is the modification time, and for a file written in one pass that is
// the moment it was finished. A package being written right now is therefore
// young, not old -- which is why a sweep may run at the start of an upload and
// not only at its end.
func (st *Stores) sweepHold(now time.Time) {
	if st.Hold == "" {
		return
	}
	es, err := os.ReadDir(st.Hold)
	if err != nil {
		return
	}
	cut := now.Add(-st.holdFor())
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().After(cut) {
			continue
		}
		_ = os.Remove(filepath.Join(st.Hold, e.Name()))
	}
}

// holdNow is the clock of the sweep, and it is the WALL CLOCK and not the
// state's own (state.State.Now) on purpose -- which is the opposite of what
// every other time in this tree does, so it is written out here.
//
// What the sweep compares is a file's modification time, and that comes from
// the FILE SYSTEM. A comparison between two clocks that may disagree is no
// comparison: with the state's clock behind the real one, as a case sets it,
// the cut lies before every mtime and nothing is ever swept -- measured
// exactly so while this function used it, and the case that found it moves
// mtimes instead of the clock now.
//
// The price is that a case cannot move this clock. It does not have to: an age
// is set with os.Chtimes, which writes the same value the sweep reads.
func (st *Stores) holdNow() time.Time { return time.Now() }

// copyHeld writes the kept package into w and answers how much it wrote. It is
// the one place that reads a held file, so the bound of an upload applies to it
// as well: a file that is larger than the bound -- which on this disk means
// somebody else put it there -- is refused instead of copied.
func (st *Stores) copyHeld(sum []byte, w io.Writer, max int64) (int64, error) {
	f, size, err := st.heldPackage(sum)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	if size > max {
		return 0, ErrTooLarge
	}
	n, err := io.Copy(w, io.NewSectionReader(f, 0, size))
	if err != nil {
		return 0, err
	}
	if n != size {
		return 0, ErrStore
	}
	return n, nil
}
