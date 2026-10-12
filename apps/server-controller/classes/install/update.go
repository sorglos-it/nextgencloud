// Installing an update of an app that is already activated: the second half of
// an acceptance, run over a file that was staged earlier.
//
// WHY THIS AND NOT A FIELD MORE IN state.Staged. Two ways were on the table.
// The other one was to let the staged row carry the UI hash and the needs out
// of the checked catalogue entry, so that installing would not have to open
// the package at all. It was measured and dropped, for two reasons that are
// the same reason:
//
//   - The needs of a CATALOGUE ENTRY (store.Package.Needs) are not the needs
//     of the MANIFEST. Nothing holds the two against each other - not
//     store.VerifyCatalog, not store.Walk, and not Accept, which holds the
//     manifest against the entry's id, version and minNgc and against nothing
//     else. The container is built out of the manifest (activate.go: Network
//     and Root out of store.Manifest.Needs), so a check against the catalogue
//     copy would be a wall in front of a door: a package whose manifest asks
//     for root while its entry says nothing would pass it and get root.
//   - The UI hash of the entry names the SAME package as the SHA-256 of that
//     entry, and the SHA-256 is what the staged row already carries and what
//     this file holds the bytes against. A second comparison against a second
//     copy can therefore only ever hold. It would be a line no case can kill -
//     the shape the db session measured as W-1: two lines that lean on each
//     other and together carry nothing.
//
// So nothing checked is written down twice. The staged row keeps the one value
// a verified signature produced - the SHA-256 - and everything else is read
// out of the package those bytes are.
//
// WHAT IS STILL CARRIED, AND WHY IT CANNOT DRIFT. The caller has to refuse an
// update whose needs the app was not granted BEFORE anything is changed
// (store.UpdateNeedsGrants), so it asks Needs first and Update afterwards -
// two reads of the same file. Update does not believe the first one: it reads
// the manifest again and refuses unless the needs it was handed are the needs
// it finds (ErrVerify). So either the two reads agree, and then one of them
// answers for both, or the update does not happen.
package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// AppUpdate is one staged app update to set up. App, Version and SHA256 are
// what the signed catalogue entry said when the file was laid ready, File is
// that file, and Needs is what the caller read out of its manifest (Needs) and
// held against the grants of the app.
//
// There is no Store and no licence in it on purpose. The store of the app
// stands in its row and is written back from there, so there is one place that
// says which store an app belongs to; and the licence of a paid app was
// checked and kept at the activation, which an update does not repeat.
type AppUpdate struct {
	App     string
	Version string
	SHA256  []byte
	File    string
	Needs   []string
}

// Needs reads what the new version of the app needs out of the manifest in the
// staged package file. It changes nothing on this machine and writes nothing:
// a caller asks it to refuse an update before the first system call
// (store.UpdateNeedsGrants).
//
// It does NOT say the file is the approved one. Whether these bytes are the
// ones a signature covered is Update's line, and it stands there and not here
// so that it is directly in front of the setup.
func (m *Manager) Needs(ctx context.Context, file string) ([]string, error) {
	if m == nil || file == "" {
		return nil, ErrInternal
	}
	f, err := openPackage(file, nil)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	broken := &sticky{r: f}
	// PackageUI and not store.Walk with a callback of its own: it is the one
	// call of shared/store that hands back the parsed manifest without writing
	// anything, and what is thrown away here is the ui-tar, not a check.
	mf, _, err := store.PackageUI(broken, io.Discard)
	if err != nil {
		return nil, broken.first(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.Clone(mf.Needs), nil
}

// Update sets a staged update of an activated app up: the same setup an
// activation does, with the grants cut down to the needs of the new version
// and the data of the app left where it is.
//
// The order, and every refusal in it stands before the installation is
// changed:
//
//	1 the row of the app; an app this controller does not run has no update
//	2 the bytes of the staged file against the SHA-256 the signed catalogue
//	  entry named (state.Staged)
//	3 the package unpacked, and its manifest held against app, version and
//	  this NGC (unpackApp, the same code an acceptance walks)
//	4 the needs of that manifest against the needs the caller checked the
//	  grants with
//	5 services only with the role services
//	-- only now is the installation changed ---------------------------------
//	6 the setup of an activation, with installed = true: the user and the
//	  data directory of the app are never dropped on a failure, and every
//	  other step is taken back (setUp)
//
// Steps 2 to 5 are not free of the disk, and saying they were would be wrong:
// step 3 needs a folder to write the package's files into, so one is made
// under config.StageDir before it and removed again whichever way this call
// ends. That folder is not part of the installation - no user, no container
// and no row is touched by it, and nothing of this server reads it afterwards
// (TestUpdateRefusesBytesThatAreNotTheStagedOnes counts what is left).
//
// A failure anywhere leaves the row of the app naming the version that was
// installed BEFORE, so a broken-off update never counts as a finished one, and
// running it again sets the same version up over what is there
// (TestUpdateLeavesTheOldVersionInTheRowAtEveryStep).
//
// A paid app cannot be updated this way, and that is NOT a check here but a
// consequence named: the content key of a sealed package arrives in the
// transfer header (core.Transfer.Key), and laying an update ready keeps no
// header - so the file is read without a key and store.Walk answers
// store.ErrPackage for it, the same as for a damaged package. REPORTED, not
// built: keeping that key would mean writing a content key onto this server's
// disk, which is a decision above this task.
func (m *Manager) Update(ctx context.Context, in AppUpdate) error {
	if m == nil || m.d.State == nil || m.d.Engine == nil || m.d.Sys == nil ||
		m.d.StageDir == "" || m.d.Now == nil {
		return ErrInternal
	}
	if in.App == "" || in.Version == "" || len(in.SHA256) != sha256.Size || in.File == "" {
		// Everything in it comes out of this controller's own rows, so an
		// empty field is this program wired wrong and not a peer's doing.
		return ErrInternal
	}
	old, err := m.d.State.App(ctx, in.App)
	switch {
	case err == nil:
	case errors.Is(err, dbapi.ErrNotFound):
		return ErrForbidden
	default:
		return err
	}
	// An empty version is a row that holds kept data and nothing running
	// (dbapi.App), and there is nothing to update about it: that is an
	// activation, which checks a signed task and a licence.
	if old.Version == "" || old.Store == "" {
		return ErrForbidden
	}

	if err := os.MkdirAll(m.d.StageDir, 0o700); err != nil {
		return disk(err)
	}
	dir, err := os.MkdirTemp(m.d.StageDir, "update-")
	if err != nil {
		return disk(err)
	}
	// The unpacked files are this call's alone: the image is in the store of
	// the app by the time this returns, and nothing else of a package outlives
	// an update - whether it worked or not.
	defer os.RemoveAll(dir)

	if err := sameFile(in.File, in.SHA256); err != nil {
		return err
	}
	files := filepath.Join(dir, FilesDir)
	mf, _, err := unpackApp(ctx, unpack{Pkg: in.File, Files: files,
		App: in.App, Version: in.Version, NGC: m.d.NGC})
	if err != nil {
		return err
	}
	// The join between the caller's read of this package and this one. Without
	// it the grants were checked against one answer and the container built
	// out of another.
	if !sameSet(mf.Needs, in.Needs) {
		return ErrVerify
	}
	if len(mf.Services) > 0 && !slices.Contains(m.d.Roles, tunnel.RoleServices) {
		return ErrForbidden
	}

	// The grants of the new row are the needs of the NEW version, not the ones
	// of the old row: an update that needs less loses the rest here, and one
	// that needs more never reaches this line (the caller's store.Granted).
	// The store comes out of the old row, which is the one place that says it.
	set := appSetup{app: in.App, version: in.Version, store: old.Store,
		sha256: in.SHA256, grants: mf.Needs, files: files}
	// installed = true, always: this is an update of a row that names a
	// version, so the user of the app and its encrypted data directory are
	// never taken away by a failure of this call.
	//
	// WHAT THIS COSTS, MEASURED AND REPORTED, NOT FIXED: the image of the new
	// version is loaded into the store of the app's own user, and that store
	// lies INSIDE the data directory - runtime.DataDirOf(app) is the home of
	// that user (runtime.EnsureUser, --home-dir), and rootless podman is
	// started with HOME set to it (runtime.Podman.run). So every update adds a
	// set of image layers under config.AppDataDir/<app>, and
	// runtime.DataSize(app) counts them as the data of the user. The image of
	// the version before is NOT removed either, for the reason activate.go
	// names at the same place: nothing writes down the reference an image has
	// (dbapi.App has no field for it), so there is no name to hand
	// Engine.RemoveImage. Both would be fixed by one field in shared/dbapi,
	// which is frozen - BLOCKED, not built.
	return m.setUp(ctx, set, mf, old, true, nil, nil)
}

// sameFile holds the bytes of path against the SHA-256 a signature covered. It
// reads the file and hashes it: the file was written by this server when the
// update was laid ready, and between then and now it lay on a disk that other
// programs can reach.
func sameFile(path string, want []byte) error {
	f, err := os.Open(path)
	if err != nil {
		return disk(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return disk(err)
	}
	if !bytes.Equal(h.Sum(nil), want) {
		return ErrVerify
	}
	return nil
}
