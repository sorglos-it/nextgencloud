package install

import (
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// What an activation may write into config.StageDir, and why it is not simply
// what the caller asks for.
//
// Accept takes a bound from its caller (AcceptInput.Max) and holds the package
// file against it. What lands on the disk is twice that, measured: the checked
// file stays beside the files it is unpacked into (package.go: PackageFile and
// FilesDir in the same folder), 8 393 728 bytes of package became 16 782 357
// bytes on disk, a factor of 2.00. With store.MaxPackage as the bound that is
// not 4 GiB but 8.
//
// And a package that is refused late writes all of it first: a wrong UI hash
// is known only after the whole archive has been read, because the UI hash is
// the hash of the whole thing (store.PackageUI). Measured in the same review:
// 8 394 351 of 8 394 351 bytes read and staged, and only then refused. So the disk has to carry the
// worst case of every package that is offered, not of every package that is
// good.
//
// This server has nothing else to fall back on. /var/lib/ngc holds the
// database and the data of every app, /etc/ngc holds its configuration and
// /etc/subuid the ranges every container needs, and on a single-disk machine -
// which is what a Pi is - all of them are the same disk as config.StageDir. A
// machine whose disk an app installation filled does not come back up after a
// restart.
const (
	// StageFactor is how much of the disk a package of n bytes takes while it
	// is checked: the file and its unpacked files, both at once.
	StageFactor = 2
	// StageReserve is what is left alone, whatever a caller asks for. It is a
	// decision and not a measurement, and the reason is named rather than a
	// number pulled out of the air: below about half a gigabyte a Debian or
	// Ubuntu cannot write its journal, cannot take an apt lock and cannot
	// write the database of this controller - so an installation that would
	// reach into it is refused before it starts, and the admin reads a
	// sentence instead of finding a server that does not boot.
	//
	// What nobody here can measure: how much a given server really needs
	// free. That depends on its journal, its swap and its apps, and this
	// machine is not that server.
	StageReserve = 512 << 20
)

// ErrSpace says this server has no room to check a package of this size. A
// caller maps it to wire.CodeInternal and shows SpaceText.
var ErrSpace = errors.New("install: not enough free space to check this package")

// SpaceText is what a user reads for ErrSpace. It is the one sentence of this
// file meant for a person, so it is German, and it says what to do.
const SpaceText = "Zu wenig freier Speicherplatz, um die App zu prüfen. " +
	"Bitte Speicherplatz freigeben und erneut versuchen."

// errNoSpaceCheck says this system cannot be asked how much room is left. It
// never reaches a caller: where it comes back, the bound stays the one the
// caller set (bound).
var errNoSpaceCheck = errors.New("install: free space is not a question of this system")

// diskFree answers how many bytes are free where dir is. It is a variable so
// a case can put a disk of its own behind it: the real answer is a call of
// Linux, and without a stand-in nothing that decides on it could be measured
// where this is written.
var diskFree = freeSpace

// bound is how large a package may be for this activation: the bound of the
// caller, cut down to what the disk of this server can carry twice over, and
// never above store.MaxPackage.
//
// A system that cannot be asked keeps the caller's bound. NGC runs on Debian
// and Ubuntu, so that is not the server - it is the machine this is written
// on, and the case for the arithmetic puts a disk behind diskFree rather than
// leave it unmeasured.
func bound(dir string, max int64) (int64, error) {
	if max <= 0 || max > store.MaxPackage {
		return 0, ErrInternal
	}
	free, err := diskFree(dir)
	if errors.Is(err, errNoSpaceCheck) {
		return max, nil
	}
	if err != nil {
		// A directory that cannot be asked at all is this server's to fix, and
		// going on with the caller's bound would be the one answer that can
		// fill the disk.
		return 0, errors.Join(ErrSpace, err)
	}
	room := (free - StageReserve) / StageFactor
	if room <= 0 {
		return 0, ErrSpace
	}
	if room < max {
		return room, nil
	}
	return max, nil
}
