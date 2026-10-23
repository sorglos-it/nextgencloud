// Handing the image of a package to the app it belongs to.
//
// It exists because of who runs the engine. `podman load --input <path>` runs
// UNPRIVILEGED, as the app's own system user (runtime.Podman.run), and the path
// it is given lies in the staging directory of this activation - which this
// package writes as root and for root alone: config.StageDir is 0700
// (Accept), the accept-* directory under it comes from os.MkdirTemp and is
// 0700, the services directory in it is 0700, and the tar itself is 0600
// (unpackApp). Four doors, all shut, and the engine stands outside every one
// of them.
//
// Measured on 20.10.2026 on the Debian machine: the same package loads when it
// lies where the app's user can read it ("Loaded image:
// localhost/ngc/terminal:1.0.0") and answers
// "Error: faccessat …/services/amd64.tar: permission denied", exit 125, when it
// lies in a root:root 0700 copy of the staged layout. Before that the
// activation never got this far - it died one step earlier, on the directory
// above the apps (runtime.EnsureData), for the same kind of reason.
//
// What is opened, and what is NOT: the directories on the way get 0711, so the
// app's user can WALK to its own file and can list none of them - the other
// packages being staged at the same time are not its business, and their names
// are random (os.MkdirTemp). The tar is handed over by OWNER, not by mode:
// chown to the app's user and 0400. So exactly one user on this machine can
// read exactly that one file, and 0644 - which would have done the job too -
// is not used, because then every user of the machine could read every app's
// image.
package install

import (
	"os"
	"path/filepath"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
)

// files are the two calls of this file: the mode of a directory and the owner
// of a file. They are held in a variable for the reason package runtime holds
// its own (runtime.paths): os.Chown is a call of Linux, and on Windows - where
// this is written - it answers "not supported by windows" for every path. So
// without a stand-in nothing this file does could be measured here at all, and
// a case would have to be read on a machine nobody writes on. A case records
// what each call was asked for and holds it against the numbers the comments
// above name; the real two keep their own case on Linux
// (TestTheStagedImageIsHandedToTheAppOnDisk). Nothing outside this package can
// reach it, and nothing inside it changes it at run time.
var files = struct {
	chmod func(path string, mode os.FileMode) error
	chown func(path string, uid, gid int) error
}{os.Chmod, os.Chown}

// imageDirMode is the mode of the directories on the way to the image:
// walk through, list nothing.
const imageDirMode = 0o711

// imageFileMode is the mode of the image itself once it belongs to the app's
// user: read, and nothing else, for that user alone.
const imageFileMode = 0o400

// handImage opens the way from stageDir down to tarPath for u and gives u the
// file.
//
// The way is walked UP from the file to stageDir, and stageDir is what stops
// it. The first draft of this named the directories instead - the staging
// directory, the files directory of the activation, the one the tar lies in -
// and that draft was wrong: between the first two lies the accept-* directory
// of os.MkdirTemp, which it would have left shut. A list of names is a list
// that can miss one; the walk cannot, as long as it has a floor, and stageDir
// is a floor every caller already has.
//
// A path that is not under stageDir never had a floor and is refused rather
// than walked to the root of the disk - it cannot happen (the caller builds
// tarPath out of a directory it made under stageDir) and it is the one way
// this loop could ever chmod something that is not ours.
//
// It is called for every activation, also when the modes are already right:
// what stands there is not believed, it is set - the same reason
// runtime.EnsureData chmods the directory above the apps rather than asking
// first. Nothing is taken back afterwards, and nothing has to be: the accept-*
// directory of this activation is removed whether it worked or not (Activate),
// and config.StageDir keeps the one mode it should have.
func handImage(stageDir, tarPath string, u runtime.User) error {
	// uid or gid 0 would hand the image to root and say it was handed over.
	// The three Ensure calls of package runtime hold the same line for the
	// same reason, and this one runs after them, so it can only be reached
	// with a user they already answered for - which is why it is an error
	// here and not a question.
	if u.UID == 0 || u.GID == 0 {
		return ErrInternal
	}
	floor := filepath.Clean(stageDir)
	dirs := []string{}
	for dir := filepath.Dir(filepath.Clean(tarPath)); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == floor {
			break
		}
		// filepath.Dir of a root answers the root itself, so this is where a
		// path outside stageDir ends - and it ends without a chmod, because
		// nothing was called yet.
		if dir == filepath.Dir(dir) {
			return ErrInternal
		}
	}
	// From the outside in: stageDir first, the directory of the tar last. The
	// other order would open the inner ones while the way to them is still
	// shut, which works and reads backwards.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := files.chmod(dirs[i], imageDirMode); err != nil {
			return err
		}
	}
	// The owner, then the mode. The order is not a security statement here and
	// is not written as one: the file arrives 0600 (unpackApp) and leaves
	// 0400, and neither of those grants anybody but the owner anything, so no
	// moment between the two calls is open to a third user. The order is this
	// way round because the failure that matters is the chown - without it the
	// file stays root's and the engine still cannot read it - and a call that
	// fails should fail before it has changed a mode.
	if err := files.chown(tarPath, int(u.UID), int(u.GID)); err != nil {
		return err
	}
	return files.chmod(tarPath, imageFileMode)
}
