package runtime

import (
	"context"
	"errors"
	"os"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// The runtime directory of an app, /run/ngc/<name>, is where rootless podman
// keeps what is running for that container (XDG_RUNTIME_DIR, Podman.run). It
// belongs to the app's user, because podman runs as that user and writes there.
//
// Nothing of it survives a restart, and that is not a flaw to be fixed but what
// a runtime directory is: config.RunDir is a tmpfs, so after every boot the
// whole tree is gone. The directory the engine names therefore has to be made
// again, and EnsureRun is the one place that makes it. The engine deliberately
// does not: it only names the path (runDir), so that making a directory and
// handing it to another user - two calls that need root - stay with the caller
// that already has root and already knows the app.

// ErrRun reports that the runtime directory of an app could not be made or
// could not be given to the app's user. It carries the reason of the system
// call with it, which names the path; nothing in that is secret, and nothing of
// it comes from outside this server.
var ErrRun = errors.New("runtime: the runtime directory of the app could not be prepared")

// RunDirOf is the runtime directory of the app's container. The name is the
// name of the system user of the app, which is the name of its container, so
// this is the same path the engine hands podman as XDG_RUNTIME_DIR.
func RunDirOf(name string) string { return paths.runDir + "/" + name }

// EnsureRun makes the runtime directory of this app's container and answers
// with its path.
//
// It may be called again at any time, and it has to be: after every restart of
// the machine the directory is gone with the tmpfs, and after a half-finished
// activation it may be there with the wrong owner or the wrong mode. So a
// directory that is already there is tightened rather than believed - the mode
// back to 0700 BEFORE it is handed over, then the owner to the app's user - and
// only a directory that cannot be made or cannot be handed over is an error.
//
// The owner is the whole point: a directory of root under /run/ngc would make
// every podman call of that app fail, and a directory of another app's user
// would let that other app read what is running here.
func EnsureRun(ctx context.Context, u User) (string, error) {
	// The name is what becomes a path and an argument of the engine, so it is
	// held against the same grammar the engine holds it against - not against
	// "it is not empty". Whoever hands in a name from somewhere else lands
	// here and not in a directory called "..".
	if !validName.MatchString(u.Name) {
		return "", ErrName
	}
	// The same line EnsureData and the engine hold: a runtime directory that
	// belongs to root is a directory an app writes into as root.
	if u.UID == 0 || u.GID == 0 {
		return "", errors.Join(ErrIdentity, errors.New("uid or gid 0 for "+u.Name))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := RunDirOf(u.Name)
	// config.RunDir itself stays root's and 0711: every app has its own
	// directory below it, and nobody has to list the others to find his own.
	// 0711 and not 0755 because the list of installed apps is nothing a user
	// of this machine needs to read off a directory.
	if err := paths.mkdirAll(paths.runDir, 0o711); err != nil {
		return "", errors.Join(ErrRun, err)
	}
	// And it is tightened as well, not only made: os.MkdirAll leaves the mode
	// of a directory that is already there alone, and one that another program
	// or a remnant on the tmpfs left at 0777 would make every app's runtime
	// directory listable by anyone on this machine.
	if err := paths.chmod(paths.runDir, 0o711); err != nil {
		return "", errors.Join(ErrRun, err)
	}
	if err := paths.mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", errors.Join(ErrRun, err)
	}
	// The mode BEFORE the handover, and that order is the whole of it: while
	// the directory is still this process's, a mode that is too wide harms
	// nobody, because nobody else may enter it. From the chown on it belongs
	// to the app's user, and a 0777 directory of that user is a door standing
	// open - for every moment between the two calls, and no tightening after
	// the chown closes it for that time.
	//
	// It is not a line for the fresh directory, which os.Mkdir already made
	// 0700. It is for the one an earlier, half-finished run left behind, whose
	// mode os.Mkdir does not touch - and /run is a tmpfs, so that case comes
	// up after every restart that leaves a remnant. Measured: with the chmod
	// after the chown such a directory was 0777 in the moment it was handed
	// over (TestEnsureRunTightensTheModeBeforeItHandsTheDirectoryOver).
	if err := paths.chmod(dir, 0o700); err != nil {
		return "", errors.Join(ErrRun, err)
	}
	if err := paths.chown(dir, int(u.UID), int(u.GID)); err != nil {
		return "", errors.Join(ErrRun, err)
	}
	return dir, nil
}

// DropRun takes the runtime directory of the app away. A directory that is not
// there is no error: cleaning up after an activation that failed is what this
// call is for, and the tmpfs may have taken it already.
func DropRun(name string) error {
	if !validName.MatchString(name) {
		return ErrName
	}
	if err := os.RemoveAll(RunDirOf(name)); err != nil {
		return errors.Join(ErrRun, err)
	}
	return nil
}

// runDirDefault is config.RunDir, held here so paths has one place for it.
const runDirDefault = config.RunDir
