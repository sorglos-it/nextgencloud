package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Every app gets a system user of its own and an encrypted data directory of
// its own, and the two belong together: the directory is the home of that user,
// which is where rootless podman keeps the image store of the app, so the
// engine finds a container again by the name alone (ofName, Container.DataDir).
// Nothing here starts a container; that is the engine's.

// paths is where this package finds the programs it runs and the files it
// writes. It is a variable and not a set of constants for one reason: a case
// has to put a program of its own in place of useradd and a file of its own in
// place of /etc/subuid, because the real ones change this machine and a test
// may not. Nothing outside this package can reach it, and nothing inside it
// changes it at run time.
var paths = struct {
	useradd, userdel string
	subuid, subgid   string
	appData          string
}{
	useradd: "/usr/sbin/useradd",
	userdel: "/usr/sbin/userdel",
	subuid:  "/etc/subuid",
	subgid:  "/etc/subgid",
	appData: config.AppDataDir,
}

// The sub id ranges of the apps: 65536 ids each, which is what a user namespace
// of a rootless container needs, and the first range begins at 524288.
//
// Why so high: the ids below it belong to the machine - its own users, and
// whatever a distribution hands out to a person who logs in (Debian starts
// those at 100000 and gives 65536 each). A range of ours that reached into
// those would map a file of an app onto a user of this server, which is the one
// thing the separation is for.
const (
	SubIDBase  = 524288
	SubIDCount = 65536
)

// maxSubIDSlots is how many ranges can be handed out: as many as there are apps
// (store.MaxApps). The last one ends below 2^32, so every id of every app still
// fits in a uid.
const maxSubIDSlots = store.MaxApps

// User is the system user of one app.
type User struct {
	// Name is the name UserName gave the app. It is the name of the container
	// as well, and that is not a convenience: every call of the engine after
	// Create carries nothing but this name and reads the user out of it.
	Name     string
	UID, GID uint32
}

var (
	// ErrApp reports an app id that is no app id (wire.ValidAppID).
	ErrApp = errors.New("runtime: app id is not an app id")
	// ErrSubID reports that no range of sub ids is free any more, or that
	// /etc/subuid or /etc/subgid says something this package cannot read.
	ErrSubID = errors.New("runtime: no free range of sub ids for this app")
)

// UserName is the name of the system user of app, and the name of its
// container.
//
// It has to be short, because a user name is, and it has to be unique, because
// two apps sharing a user would share everything the user owns - the data
// directory, the image store, the containers. So the readable part is cut to
// fit and what makes it unique is the hash of the whole app id, not the part
// that was kept: two apps whose last label is the same still get two names.
//
// It is also stable for good. The name is written into the user database, into
// /etc/subuid and into the name of every container of the app, so changing how
// it is built would orphan every app already installed.
func UserName(app string) string {
	sum := sha256.Sum256([]byte(app))
	id := hex.EncodeToString(sum[:])[:8]
	// The readable part: the last label of the reverse domain, which is what a
	// person recognises, with everything a user name may not hold taken out.
	label := app
	if i := strings.LastIndexByte(app, '.'); i >= 0 {
		label = app[i+1:]
	}
	var b strings.Builder
	for i := 0; i < len(label) && b.Len() < 18; i++ {
		if c := label[i]; (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		}
	}
	// A label of nothing usable leaves the name without that part rather than
	// with a dash hanging in the air.
	if b.Len() == 0 {
		return "ngc-" + id
	}
	return "ngc-" + b.String() + "-" + id
}

// DataDirOf is the data directory of the app: the home of its user and what the
// engine mounts as /data.
func DataDirOf(app string) string { return paths.appData + "/" + app }

// EnsureUser makes sure the system user of app is there and answers with it.
//
// It may be called again at any time and for a user that is already there: an
// install that failed halfway, a controller that was restarted, an update that
// runs over an app from before - all of them arrive here, and all of them have
// to end with the same user.
func EnsureUser(ctx context.Context, app string) (User, error) {
	if !wire.ValidAppID(app) {
		return User{}, ErrApp
	}
	name := UserName(app)
	if u, err := ofUserDB(name, app); err == nil {
		// The user is there, the ranges may not be: a crash between the two
		// leaves exactly that, so this is not an else.
		if err := ensureSubIDs(name); err != nil {
			return User{}, err
		}
		return u, nil
	} else if !errors.Is(err, ErrUser) {
		// It is there and it is wrong - a home somewhere else, an id of 0.
		// Making it again would not help and would hide it.
		return User{}, err
	}
	// --no-create-home, because the directory is EnsureData's: it has to be
	// empty when its encryption policy is set, and it has to belong to this
	// user afterwards. --home-dir all the same, because the home is what the
	// engine reads out of the user database for every later call; without it
	// the user would get /home/<name> and no container of this app would ever
	// start. --shell nologin, because nobody logs in as an app.
	args := []string{"--system", "--no-create-home",
		"--shell", "/usr/sbin/nologin", "--home-dir", DataDirOf(app), name}
	cmd := exec.CommandContext(ctx, paths.useradd, args...)
	// No environment of this process: a controller runs as root and carries
	// whatever systemd gave it, and none of that is useradd's business.
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	if out, err := cmd.CombinedOutput(); err != nil {
		return User{}, errors.Join(ErrUser, errors.New("useradd "+name+": "+
			err.Error()+": "+strings.TrimSpace(string(out))))
	}
	// Read back rather than believe: the uid and the gid are the system's to
	// choose, and the gid in particular depends on whether useradd made a group
	// of its own for this user.
	u, err := ofUserDB(name, app)
	if err != nil {
		return User{}, err
	}
	if err := ensureSubIDs(name); err != nil {
		return User{}, err
	}
	return u, nil
}

// ofUserDB reads the user out of the user database and holds it against what it
// has to be: a numeric uid and gid, neither of them 0, and the data directory
// of this app as its home. The engine holds the same three at Create and at
// every call after it, so a mismatch found here is the same finding one call
// earlier - and a clearer one, because here the app id is still known.
func ofUserDB(name, app string) (User, error) {
	db, err := user.Lookup(name)
	if err != nil {
		return User{}, errors.Join(ErrUser, err)
	}
	uid, err := strconv.ParseUint(db.Uid, 10, 32)
	if err != nil {
		return User{}, errors.Join(ErrIdentity, err)
	}
	gid, err := strconv.ParseUint(db.Gid, 10, 32)
	if err != nil {
		return User{}, errors.Join(ErrIdentity, err)
	}
	if uid == 0 || gid == 0 {
		return User{}, errors.Join(ErrIdentity, errors.New("uid or gid 0 for "+name))
	}
	if db.HomeDir != DataDirOf(app) {
		return User{}, errors.Join(ErrHome, errors.New("the home of "+name+" is "+db.HomeDir))
	}
	return User{Name: name, UID: uint32(uid), GID: uint32(gid)}, nil
}

// DropUser takes the system user of app away. The data directory is not its to
// remove - that is DropData, and the order matters: a directory whose owner is
// gone belongs to a number, and the next user to get that number would inherit
// it.
func DropUser(ctx context.Context, app string) error {
	if !wire.ValidAppID(app) {
		return ErrApp
	}
	name := UserName(app)
	// The ranges first: a line left in /etc/subuid would hold its range against
	// the next app for as long as the file lives, and nothing would ever free
	// it again.
	if err := dropSubIDs(name); err != nil {
		return err
	}
	if _, err := user.Lookup(name); err != nil {
		// Not there is what this call is for. userdel would say "no such user"
		// and that is not a failure of anything.
		return nil
	}
	cmd := exec.CommandContext(ctx, paths.userdel, name)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Join(ErrUser, errors.New("userdel "+name+": "+
			err.Error()+": "+strings.TrimSpace(string(out))))
	}
	return nil
}

// subIDRange is one line of /etc/subuid or /etc/subgid.
type subIDRange struct {
	name  string
	start uint64
	count uint64
}

// ensureSubIDs gives this user one range in /etc/subuid and one in /etc/subgid,
// or leaves the ones it already has alone.
func ensureSubIDs(name string) error {
	for _, file := range []string{paths.subuid, paths.subgid} {
		if err := ensureSubIDFile(file, name); err != nil {
			return err
		}
	}
	return nil
}

func ensureSubIDFile(file, name string) error {
	lines, rs, err := readSubIDs(file)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.name == name {
			return nil
		}
	}
	start, err := freeSubIDStart(rs)
	if err != nil {
		return err
	}
	lines = append(lines, name+":"+strconv.FormatUint(start, 10)+":"+strconv.Itoa(SubIDCount))
	return writeWhole(file, strings.Join(lines, "\n")+"\n")
}

// dropSubIDs takes every line of this user out of both files.
func dropSubIDs(name string) error {
	for _, file := range []string{paths.subuid, paths.subgid} {
		lines, _, err := readSubIDs(file)
		if err != nil {
			return err
		}
		kept := make([]string, 0, len(lines))
		for _, l := range lines {
			if n, _, ok := strings.Cut(l, ":"); !ok || n != name {
				kept = append(kept, l)
			}
		}
		if len(kept) == len(lines) {
			continue
		}
		body := ""
		if len(kept) > 0 {
			body = strings.Join(kept, "\n") + "\n"
		}
		if err := writeWhole(file, body); err != nil {
			return err
		}
	}
	return nil
}

// readSubIDs is the file as lines, and the ranges it could read out of them.
//
// A line it cannot read is kept and not counted, which is the only safe way
// round: dropping it would take a range away from whoever owns it, and counting
// it as a range of ours would hand that range to an app.
func readSubIDs(file string) ([]string, []subIDRange, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			// A machine without the file has nothing handed out yet.
			return nil, nil, nil
		}
		return nil, nil, errors.Join(ErrSubID, err)
	}
	var lines []string
	var rs []subIDRange
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines, l)
		f := strings.Split(l, ":")
		if len(f) != 3 {
			continue
		}
		start, err1 := strconv.ParseUint(f[1], 10, 64)
		count, err2 := strconv.ParseUint(f[2], 10, 64)
		if err1 != nil || err2 != nil || count == 0 {
			continue
		}
		rs = append(rs, subIDRange{name: f[0], start: start, count: count})
	}
	return lines, rs, nil
}

// freeSubIDStart is the lowest range of ours that overlaps nothing already
// written down - not another app's, and not a range the machine handed to one
// of its own users.
func freeSubIDStart(rs []subIDRange) (uint64, error) {
	for n := range maxSubIDSlots {
		start := uint64(SubIDBase) + uint64(n)*SubIDCount
		free := true
		for _, r := range rs {
			if start < r.start+r.count && r.start < start+SubIDCount {
				free = false
				break
			}
		}
		if free {
			return start, nil
		}
	}
	return 0, ErrSubID
}

// writeWhole replaces a file completely or leaves it as it was. Half a line in
// /etc/subuid would lock an app out of its own range for good and would not be
// noticed until a container failed to start, so the new text is written beside
// the file and put in its place in one step.
func writeWhole(file, body string) error {
	dir := filepath.Dir(file)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(file)+".ngc-")
	if err != nil {
		return errors.Join(ErrSubID, err)
	}
	name := tmp.Name()
	// Nothing of this is secret - these files are world readable by design,
	// because every program that maps a user namespace reads them - but a
	// half-written one must never be left behind under its own name.
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		return errors.Join(ErrSubID, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return errors.Join(ErrSubID, err)
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(ErrSubID, err)
	}
	if err := os.Rename(name, file); err != nil {
		return errors.Join(ErrSubID, err)
	}
	return nil
}
