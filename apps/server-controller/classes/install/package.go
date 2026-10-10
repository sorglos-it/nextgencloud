// Package install takes an app package or a release file from an API and
// checks it before anything of it is unpacked.
package install

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// Names inside the staged folder of an accepted package.
const (
	PackageFile = "package" // the file as it arrived; it stays only if its checksum matched
	FilesDir    = "files"   // what store.Walk let through: ui/… and services/<arch>.tar
)

var (
	// ErrSource says the package names no source this installation trusts, or
	// not the one the signed task approved: a store outside the trust list,
	// an app or a version no checked catalogue lists, a release the release
	// key does not cover.
	ErrSource = errors.New("install: no approved source for this package")
	// ErrVerify says the package is not what was approved: a checksum, a
	// size, a UI hash, an app name or a version that does not match, or an
	// NGC too old for it. A caller maps it to core.ErrVerify (E_VERIFY).
	ErrVerify = errors.New("install: package does not match what was approved")
	// ErrDisk says this node could not put the package down: no space, no
	// permission, or a name in the package that the target filesystem cannot
	// keep apart from one already written. Nothing is wrong with the package
	// or with the store, so a caller must not answer E_VERIFY to it.
	//
	// Its text names no path on purpose. This error travels to an API and can
	// end up in a message an admin reads, and the staging path would tell
	// whoever sent the package where to aim a race or a planted link. The
	// operating system own error, path and all, stays reachable with
	// errors.As for this node own log - an explicit step, not a leak.
	ErrDisk = errors.New("install: this node could not store the package")
)

// diskError is ErrDisk with the cause behind it: Error() is the fixed text
// alone, while errors.Is(err, ErrDisk) and errors.As(err, &pathErr) both hold.
type diskError struct{ cause error }

func (e diskError) Error() string   { return ErrDisk.Error() }
func (e diskError) Unwrap() []error { return []error{ErrDisk, e.cause} }

// disk marks err as a failure of this node rather than of the package.
func disk(err error) error {
	if err == nil {
		return nil
	}
	return diskError{err}
}

// markWriter tells a failed write to the disk apart from a failed read of the
// package: io.Copy returns one error for both, and the two must not get the
// same answer.
type markWriter struct {
	w      io.Writer
	failed bool
}

func (m *markWriter) Write(p []byte) (int, error) {
	n, err := m.w.Write(p)
	if err != nil {
		m.failed = true
	}
	return n, err
}

// AcceptInput is one package offered to a controller. Reader carries the
// transfer header (core.ReadTransfer) and then the file; Max bounds the file.
// List is the installation's own trust list, the only place a store key may
// come from. Store, App, Version, SHA256 and UI come from the signed task
// (core.ActivateParams); Store is core.LocalStore for an app an admin
// uploaded and empty for a release. NGC is this NGC's version, Dir the folder
// under which a fresh staging folder is made (config.StageDir in operation).
// LastSeq returns the newest catalogue number already seen from a store, "" for
// a release.
//
// ReleaseKey is this installation's own release key (config.Controller.ReleaseKey).
// Task 15's interface list names store.VerifyRelease and "the own release
// key" but left the field out of AcceptInput; without it the release branch
// cannot check a signature at all, so the field is here and the deviation is
// reported.
type AcceptInput struct {
	Reader     io.Reader
	Max        int64
	List       *trust.List
	Store      string
	App        string
	Version    string
	SHA256     []byte
	UI         []byte
	NGC        string
	Dir        string
	ReleaseKey []byte
	LastSeq    func(store string) uint64
}

// Package is an accepted package. Dir holds PackageFile, the checked file,
// and FilesDir, what store.Walk let through; for a release only PackageFile.
// Manifest and UI are nil for a release, which carries neither, and that is
// the whole answer for a release, not a half-finished one. Signer is the
// key the source was signed with (the store's key, or the release key) and
// Seq the number of that catalogue or release, so a caller can remember what
// it has seen.
type Package struct {
	Dir      string
	Manifest *store.Manifest
	UI       []byte
	Transfer core.Transfer
	Signer   []byte
	Seq      uint64
}

// source is what the checked catalogue or release says about this one file,
// gathered before anything is written.
type source struct {
	signer []byte
	seq    uint64
	size   int64
	sha    []byte
	ui     []byte // catalogue entry only; nil for a release and an upload
	minNGC string
	app    bool // an app package: it has a manifest and a UI, a release has not
}

// Accept reads the transfer header, checks that the file was approved, and
// only then writes it. What stands before the first byte reaches the disk is
// the signature, the catalogue or release membership, the rollback rule, the
// NGC version, and the IDENTITY of the three checksums - that the signed
// task, the header and the checked entry all name the same one. The checksum
// of the bytes themselves cannot be had before them, so it comes after.
//
// Then the file is written and its SHA-256 and size compared, a paid app is
// decrypted, and the ui-tar is hashed (store.PackageUI) against the task and
// the catalogue entry. That last one is a LATE wall by its nature: the UI
// hash is only known after the last byte, so a package with a wrong UI hash
// is read and staged in full before it is refused. Finally store.Walk writes
// the files. Every failure removes the whole staged folder, so nothing of a
// refused package stays behind.
func Accept(ctx context.Context, in AcceptInput) (Package, error) {
	if err := validInput(in); err != nil {
		return Package{}, err
	}
	tr, err := core.ReadTransfer(in.Reader)
	if err != nil {
		return Package{}, err
	}
	// The signed task names the checksum, so the header has to repeat it: a
	// header is not signed, and nothing below would notice a swapped file
	// whose own catalogue entry matched it.
	if !bytes.Equal(tr.SHA256, in.SHA256) || tr.Size > in.Max {
		return Package{}, ErrVerify
	}
	src, err := approved(in, tr)
	if err != nil {
		return Package{}, err
	}
	if tr.Size != src.size || !bytes.Equal(tr.SHA256, src.sha) {
		return Package{}, ErrVerify
	}
	if !store.SupportsMinNGC(in.NGC, src.minNGC) {
		return Package{}, ErrVerify
	}
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}

	if err := os.MkdirAll(in.Dir, 0o700); err != nil {
		return Package{}, disk(err)
	}
	dir, err := os.MkdirTemp(in.Dir, "accept-")
	if err != nil {
		return Package{}, disk(err)
	}
	p, err := stage(ctx, dir, in, tr, src)
	if err != nil {
		// Walk checks a package only after its last file, so nothing written
		// so far was ever fully valid on its own.
		os.RemoveAll(dir)
		return Package{}, err
	}
	return p, nil
}

func validInput(in AcceptInput) error {
	ok := in.Reader != nil && in.Max > 0 && in.Max <= store.MaxPackage && in.List != nil &&
		in.App != "" && in.Version != "" && len(in.SHA256) == sha256.Size && len(in.UI) == sha256.Size &&
		in.Dir != "" && in.LastSeq != nil
	if !ok {
		return ErrVerify
	}
	return nil
}

// approved checks where the file comes from: exactly one source, and that
// source approved by this installation alone. Nothing here touches the disk.
func approved(in AcceptInput, tr core.Transfer) (source, error) {
	switch {
	case tr.Catalog != nil:
		return fromCatalog(in, tr)
	case tr.Release != nil:
		return fromRelease(in, tr)
	default:
		// An uploaded app: no catalogue binds the file to a name, so the
		// signed task's checksum is the whole proof, and the manifest's own ID
		// and version are checked against the task after unpacking.
		if in.Store != core.LocalStore {
			return source{}, ErrSource
		}
		return source{size: tr.Size, sha: tr.SHA256, app: true}, nil
	}
}

func fromCatalog(in AcceptInput, tr core.Transfer) (source, error) {
	// The store comes from the installation's own trust list, never from the
	// header: the header only says which of the listed stores this is.
	if in.Store != tr.Store {
		return source{}, ErrSource
	}
	i := slices.IndexFunc(in.List.Stores, func(s trust.Store) bool { return s.ID == tr.Store })
	if i < 0 {
		return source{}, ErrSource
	}
	st := in.List.Stores[i]
	c, err := store.VerifyCatalog(*tr.Catalog, st, in.LastSeq(st.ID))
	if err != nil {
		return source{}, err
	}
	a := slices.IndexFunc(c.Apps, func(e store.Entry) bool { return e.ID == in.App })
	if a < 0 {
		return source{}, ErrSource
	}
	v := slices.IndexFunc(c.Apps[a].Versions, func(p store.Package) bool { return p.Version == in.Version })
	if v < 0 {
		return source{}, ErrSource
	}
	e := c.Apps[a].Versions[v]
	return source{signer: st.Key, seq: c.Seq, size: e.Size, sha: e.SHA256, ui: e.UI,
		minNGC: e.MinNGC, app: true}, nil
}

func fromRelease(in AcceptInput, tr core.Transfer) (source, error) {
	// A release belongs to no store, and only the installation's own release
	// key may cover it.
	if in.Store != "" || len(in.ReleaseKey) != ed25519.PublicKeySize {
		return source{}, ErrSource
	}
	r, err := store.VerifyRelease(*tr.Release, ed25519.PublicKey(in.ReleaseKey), in.LastSeq(""))
	if err != nil {
		return source{}, err
	}
	if r.Version != in.Version {
		return source{}, ErrSource
	}
	// The task names the part, the header's checksum names the file: a
	// release lists one file per part and architecture, and nothing in the
	// task says which architecture, so the checksum picks the entry.
	i := slices.IndexFunc(r.Files, func(f store.File) bool {
		return f.Part == in.App && bytes.Equal(f.SHA256, tr.SHA256)
	})
	if i < 0 {
		return source{}, ErrSource
	}
	return source{signer: in.ReleaseKey, seq: r.Seq, size: r.Files[i].Size, sha: r.Files[i].SHA256}, nil
}

// stage writes the file into dir, checks it, and unpacks an app package.
func stage(ctx context.Context, dir string, in AcceptInput, tr core.Transfer, src source) (Package, error) {
	sum, err := writeFile(filepath.Join(dir, PackageFile), in.Reader, tr.Size)
	if err != nil {
		return Package{}, err
	}
	// tr.SHA256 is already the one the signed task named and the one the
	// checked catalogue entry carries, so this one comparison binds the bytes
	// on disk to all three.
	if !bytes.Equal(sum, tr.SHA256) {
		return Package{}, ErrVerify
	}
	p := Package{Dir: dir, Transfer: tr, Signer: src.signer, Seq: src.seq}
	if !src.app {
		// A release ends here, and Manifest and UI stay nil: a release file is
		// an NGC component, not an app package, so it has no manifest.json and
		// no ui/ for store.Walk and store.PackageUI to find. nil is the
		// finished answer for this branch, not a failure a caller must handle
		// — the signature, the release number, the part, the version, the size
		// and the checksum are all already checked above. Unpacking it belongs
		// to whoever installs an update.
		return p, nil
	}
	// No check here that the catalogue's "encrypted" and a content key agree:
	// store.NewDecrypter refuses a plain file (no NGCENC1 magic) with
	// store.ErrEncrypted, and store.Walk refuses a sealed file read without a
	// key with store.ErrPackage. A check of its own was measured to be
	// unkillable — both directions already fall one layer down
	// (TestAcceptDecryptsAPaidApp).
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}

	// PackageUI walks the whole package, so the UI hash is known only after a
	// pass of its own; that is why the file is on disk and read twice.
	plain, err := open(dir, tr.Key)
	if err != nil {
		return Package{}, err
	}
	broken := &sticky{r: plain}
	m, uiHash, err := store.PackageUI(broken, io.Discard)
	plain.Close()
	if err != nil {
		// store.Walk answers every unreadable archive with ErrPackage, so a
		// wrong content key would otherwise look like a damaged package.
		return Package{}, broken.first(err)
	}
	if !bytes.Equal(uiHash, in.UI) || (src.ui != nil && !bytes.Equal(uiHash, src.ui)) {
		return Package{}, ErrVerify
	}
	// For an uploaded app the manifest is the only place the app's name, its
	// version and its minNgc stand; for a store app the catalogue said the
	// same already, and the two must not disagree.
	if m.ID != in.App || m.Version != in.Version || !store.SupportsMinNGC(in.NGC, m.MinNGC) {
		return Package{}, ErrVerify
	}
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}

	plain, err = open(dir, tr.Key)
	if err != nil {
		return Package{}, err
	}
	defer plain.Close()
	broken = &sticky{r: plain}
	files := filepath.Join(dir, FilesDir)
	// store.Walk checks every entry before handing it over: only ui/<ValidPath>
	// and services/{amd64,arm64}.tar, no absolute path, no "..", no link, no
	// sparse entry, at most store.MaxDirs directories, and every size within
	// its limit (MaxUIFile, MaxUITotal, MaxPackage) - it BOUNDS sizes, it does
	// not fix them. So the name is safe to join, and a second path check here
	// would be a copy that can drift from the one in Walk.
	if _, err := store.Walk(broken, func(_ *store.Manifest, f store.PackFile, body io.Reader) error {
		path := filepath.Join(files, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return disk(err)
		}
		// O_EXCL: a name that already exists, or a link someone slipped in, is
		// never followed. The package can reach this line on its own - two ui
		// names that differ only in case are two entries to store.Walk and one
		// name to a filesystem that folds case (TestAcceptMapsAStorageFailure).
		w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return disk(err)
		}
		m := &markWriter{w: w}
		n, err := io.Copy(m, io.LimitReader(body, f.Size+1))
		cerr := w.Close()
		switch {
		case m.failed || cerr != nil:
			// The write failed, not the package: a full disk must never be
			// answered as a bad package. Measured: dropping m.failed keeps the
			// suite green, because no case here can fill a disk or revoke a
			// permission mid-write. Named, not removed - it is the half of
			// this branch that covers the failure nobody can stage.
			if err == nil {
				err = cerr
			}
			return disk(err)
		case err != nil:
			return store.ErrPackage // the archive could not be read
		}
		// More than promised is impossible while store.Walk refuses sparse
		// entries, and less only on a clean EOF - which the package checksum
		// already caught before any of this ran. Kept and named: this is the
		// line that holds if either of those two ever changes.
		if n != f.Size {
			return store.ErrPackage
		}
		return nil
	}); err != nil {
		return Package{}, broken.first(err)
	}
	p.Manifest, p.UI = m, uiHash
	return p, nil
}

// open returns the plain package staged in dir, decrypted with key if a paid
// app brought one. The content key never reaches an error text or a log.
func open(dir string, key []byte) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(dir, PackageFile))
	if err != nil {
		return nil, disk(err)
	}
	if len(key) == 0 {
		return f, nil
	}
	r, err := store.NewDecrypter(f, key)
	if err != nil {
		f.Close()
		return nil, err
	}
	return readCloser{r, f}, nil
}

// sticky keeps the first error of the decrypted stream. store.Walk turns
// every unreadable archive into store.ErrPackage, which would hide the one
// cause a caller must tell apart: the wrong content key.
type sticky struct {
	r   io.Reader
	err error
}

func (s *sticky) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF && s.err == nil {
		s.err = err
	}
	return n, err
}

// first returns the stream's own error if it had one, else err.
func (s *sticky) first(err error) error {
	if s.err != nil {
		return s.err
	}
	return err
}

type readCloser struct {
	io.Reader
	c io.Closer
}

func (r readCloser) Close() error { return r.c.Close() }

// writeFile copies exactly size bytes of r to path and returns their
// SHA-256. One byte more or less than the header promised is a refusal: a
// package cut short in transit parses as a shorter, complete archive, and
// bytes past the promise were never covered by any checksum.
func writeFile(path string, r io.Reader, size int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, disk(err)
	}
	h := sha256.New()
	m := &markWriter{w: f}
	n, err := io.Copy(io.MultiWriter(m, h), io.LimitReader(r, size))
	cerr := f.Close()
	if m.failed || cerr != nil {
		if err == nil {
			err = cerr
		}
		return nil, disk(err)
	}
	if err != nil {
		return nil, ErrVerify // the transfer broke, so this is not the approved file
	}
	// Measured redundant and kept on purpose: a short file also fails the
	// checksum, so no case can kill this line alone (mutant "n != size" ->
	// "n < 0" stays green). It says the size rule out loud, and it is the
	// only one of the two that a reader can see without the hash.
	if n != size {
		return nil, ErrVerify
	}
	var one [1]byte
	if k, _ := r.Read(one[:]); k != 0 {
		return nil, ErrVerify
	}
	return h.Sum(nil), nil
}
