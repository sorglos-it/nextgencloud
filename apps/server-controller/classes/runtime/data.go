package runtime

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The data directory of an app is encrypted by the Linux kernel itself
// (fscrypt, AES-256-XTS for the contents and AES-256-CTS for the names), set
// through two ioctls out of package syscall. No extra module and no other
// program: a container of an app is the one place where a missing dependency
// cannot be installed afterwards, and the kernel is already there.
//
// The key of an app never lies on a disk. It is derived from the key of the
// controller (config.Controller.DBKey, 64 bytes) and the app id each time it is
// needed, so a stolen disk carries the encrypted directories and nothing that
// opens them - what opens them is in /etc/ngc/controller.json, which is root
// and 0600, and in the kernel's own keyring while the server runs.
//
// The other way out of the design draft - one encrypted container file per app -
// is in the open decisions. It needs no filesystem feature, and it costs a loop
// device, a mount and a fixed size per app.

const (
	// keyInfo separates this use of the controller's key from every other use
	// it may ever have. It carries a version because the day it changes, every
	// directory already written stays readable only under the old one.
	keyInfo = "ngc/appdata/v1"
	// keyBytes is what AES-256-XTS takes: two 256 bit halves.
	keyBytes = 64
	// minServerKey is the shortest server key this will stretch. HKDF makes 64
	// bytes out of anything, including out of nothing, and a key derived from
	// four bytes protects nothing at all - so a key that is no key is refused
	// instead of stretched.
	minServerKey = 32
)

// NoFscryptText is what a user reads when the disk cannot do it. It is the one
// sentence of this package meant for a person, so it is German, and it says
// what to do instead of what failed.
const NoFscryptText = "Der Datenträger kann keine verschlüsselten Ordner (fscrypt). " +
	"Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden."

var (
	// ErrKey reports a server key that is too short to derive from.
	ErrKey = errors.New("runtime: the key of this server is too short")
	// ErrNoFscrypt reports a filesystem that cannot encrypt a directory. The
	// caller shows NoFscryptText for it and activates no app with data: an
	// unencrypted data directory is the one thing this must never make quietly.
	ErrNoFscrypt = errors.New("runtime: this filesystem cannot encrypt a directory (fscrypt)")
	// ErrPolicy reports a data directory that already carries an encryption
	// policy other than the one of its app. Nothing is changed for it: the
	// directory holds data that only the other key opens.
	ErrPolicy = errors.New("runtime: the data directory carries another encryption policy")
)

// appKey is the key of this app, derived from the key of this server and the
// app id. The app id is the salt and not part of the info string so that two
// apps cannot be made to collide by a name that happens to end in the info.
func appKey(serverKey []byte, app string) ([]byte, error) {
	if len(serverKey) < minServerKey {
		return nil, ErrKey
	}
	k, err := hkdf.Key(sha256.New, serverKey, []byte(app), keyInfo, keyBytes)
	if err != nil {
		return nil, errors.Join(ErrKey, err)
	}
	return k, nil
}

// EnsureData makes the encrypted data directory of app and answers with its
// path. It may be called again for a directory that is already there: an
// install that failed halfway and an update both arrive here.
//
// The order is the whole of it. The directory has to be empty when its policy
// is set - the kernel refuses otherwise - and it has to belong to the app's
// user afterwards, because podman runs as that user and keeps the image store
// of the app in there. So: make it, add the key, bind the policy, hand it over.
func EnsureData(ctx context.Context, app string, u User, serverKey []byte) (string, error) {
	if !wire.ValidAppID(app) {
		return "", ErrApp
	}
	// The same line the engine holds, for the same reason: a data directory
	// that belongs to root is a directory an app writes into as root.
	if u.UID == 0 || u.GID == 0 {
		return "", errors.Join(ErrIdentity, errors.New("uid or gid 0 for "+app))
	}
	key, err := appKey(serverKey, app)
	if err != nil {
		return "", err
	}
	// An ioctl cannot be cut short, so the context is asked here, where there
	// is still nothing to undo, and nowhere else. Everything below is a handful
	// of system calls.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := DataDirOf(app)
	// THE FOLDER THAT HOLDS THEM ALL IS root:ngc-apps 0710.
	//
	// It was 0700 root until 18.10.2026, and then no app could be set up at
	// all: podman runs as the app's own user (podman.go, run), its whole image
	// store lies under this folder, and a user who may not TRAVERSE the folder
	// above cannot reach its own. Measured on the real Debian-13 machine that
	// day, out of podman's own mouth once somebody finally read its stderr:
	//
	//   cannot resolve /var/lib/ngc/apps/org.sorglos.terminal:
	//   lstat /var/lib/ngc/apps/org.sorglos.terminal: permission denied
	//
	// Every activation ended there, rolled the app's user back and answered
	// E_INTERNAL -- with the reason thrown away twice over, because p.run was
	// handed nil for stderr.
	//
	// 0711 AND THE GROUP, both, and the order of that sentence is a measured
	// decision (Thomas, 19.10.2026). The tighter 0710 was tried first and is
	// REFUTED: with the folder at 0710 root:ngc-apps and the app user really
	// in that group, the image loaded and the container was built, and crun
	// then died on
	//
	//   open `/var/lib/ngc/apps/<app>/.local/share/containers/storage/
	//   overlay/<id>/merged`: Permission denied: OCI permission denied
	//
	// because a rootless container runs in a user namespace and a
	// SUPPLEMENTARY group is not mapped into it: inside, the process holds its
	// own gid and nothing else, so a folder that only a group may traverse
	// stops it. Measured A/B on the real machine the same night, the same
	// build with this one mode changed: 0710 refused, 0711 ran - and that is
	// the first app NGC has ever run.
	//
	// So the mode is 0711: execute and no read. A local user may step THROUGH
	// this folder into a path it already knows, and may list nothing - the
	// names of the installed apps stay unreadable, each app's own directory
	// keeps its 0700 and its owner below, and its own fscrypt key is what
	// really closes it.
	//
	// THE GROUP STAYS ALL THE SAME, and not out of stubbornness: it costs one
	// system group, it says in the user database which users these are, and
	// it is the bolt that is already in place on the day podman maps that
	// membership into the namespace (or an app runs rootful). The gid is read
	// back from the system and never written down.
	gid, err := paths.appsGID(ctx)
	if err != nil {
		return "", err
	}
	if err := paths.mkdirAll(paths.appData, 0o711); err != nil {
		return "", err
	}
	// For a folder an older build already made with 0700: mkdirAll leaves
	// what is there alone, so these two lines are what mends an installation
	// standing today. The group first and the mode second: between the two the
	// folder is 0700 and reaches nobody, which is the safe half-way.
	// ONLY THE GROUP, and the owner is left exactly as it is: -1 is "do not
	// change" for chown(2). The folder belongs to root and has to go on
	// belonging to root, so there is nothing to set - and a call that names an
	// owner would be a call only root could make, for a change that was never
	// needed.
	if err := paths.chown(paths.appData, -1, gid); err != nil {
		return "", err
	}
	if err := paths.chmod(paths.appData, 0o711); err != nil {
		return "", err
	}
	made := false
	if err := paths.mkdir(dir, 0o700); err != nil {
		if !os.IsExist(err) {
			return "", err
		}
	} else {
		made = true
	}
	// A directory this call made and could not encrypt is taken away again. It
	// is empty, so nothing is lost - and left behind it would be exactly what
	// must not exist: a data directory of an app without encryption, which a
	// later call would find and take for ready.
	ok := false
	defer func() {
		if !ok && made {
			_ = os.Remove(dir)
		}
	}()

	id, err := paths.addKey(paths.appData, key)
	if err != nil {
		return "", err
	}
	if err := paths.setPolicy(dir, id); err != nil {
		return "", err
	}
	if err := os.Chown(dir, int(u.UID), int(u.GID)); err != nil {
		return "", err
	}
	// Said again after the chown, and this is what it really does: this call is
	// repeatable, so dir may be one an earlier, half-finished run left behind
	// with another mode, and os.Mkdir above does not touch the mode of a
	// directory that is already there. Such a directory is tightened here.
	//
	// For a directory this call just made the line changes nothing - os.Mkdir
	// made it 0700 - and it is not here for the set-user-id and set-group-id
	// bits either: a directory made 0700 carries neither, so there is nothing
	// for the chown above to clear. Kept because the repeat case is real and
	// measured (TestEnsureDataTightensTheModeOfADirectoryAlreadyThere).
	if err := paths.chmod(dir, 0o700); err != nil {
		return "", err
	}
	ok = true
	return dir, nil
}

// UnlockData puts the key of app back into the keyring of the filesystem. The
// keys live in the kernel and not on the disk, so this is what has to happen
// after every restart of the machine, before any container of the app starts.
//
// It changes no policy and makes no directory: a directory that is not there
// yet is EnsureData's, and a directory that carries another policy is not made
// readable by adding a key - the key simply is not the one its policy names.
func UnlockData(ctx context.Context, app string, serverKey []byte) error {
	if !wire.ValidAppID(app) {
		return ErrApp
	}
	key, err := appKey(serverKey, app)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = paths.addKey(paths.appData, key)
	return err
}

// DropData takes the data directory of app away and the key of it out of the
// kernel.
//
// The key identifier is read off the directory before it goes, not derived from
// the server key: this call has no key, and it must work for a directory whose
// policy names something else - after a half-finished install, or on a disk
// from another installation.
//
// A directory that is not there is no error. Cleaning up after a failed install
// is one of the two things this call is for.
func DropData(ctx context.Context, app string) error {
	if !wire.ValidAppID(app) {
		return ErrApp
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := DataDirOf(app)
	id, idErr := policyOf(dir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if idErr != nil {
		// No policy to read means no key of ours to take out, and that is the
		// normal case on a filesystem without encryption and for a directory
		// that was never there. The removal above is what the caller asked for
		// and it happened.
		return nil
	}
	// The key may still be in use by another directory of the same policy; the
	// kernel says so and keeps it, which is right. What must not happen is that
	// this call fails over it.
	if err := removeKey(paths.appData, id); err != nil && !errors.Is(err, ErrNoFscrypt) {
		return err
	}
	return nil
}

// DataSize is how many bytes the app keeps. It counts the files under the
// directory of this app and follows no link out of it: a link is a way to make
// the size of one app answer for the files of something else.
//
// An app without a directory has no data, and that is 0 and not an error - it
// is the answer before the first install.
func DataSize(app string) (int64, error) {
	if !wire.ValidAppID(app) {
		return 0, ErrApp
	}
	dir := DataDirOf(app)
	var total int64
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Only what really holds bytes. A directory, a link, a socket or a
		// device counts as nothing: what is asked for is how much the app
		// wrote.
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			// The file went away while this was walking. That is not a failure
			// of the count; it is a file that no longer takes any room.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		total += fi.Size()
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return total, nil
}
