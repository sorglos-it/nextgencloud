// Package classes holds the subcommands of ngc-store: packing apps,
// encrypting paid ones and signing catalogs, licences and test releases.
package classes

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// ErrPack reports a directory that is no app.
var ErrPack = errors.New("ngc-store: the directory is no app")

// images are the two names a package may carry an image under, in this order.
var images = []string{"services/amd64.tar", "services/arm64.tar"}

// Pack writes the package of the app in dir to out: manifest.json first, then
// the files below ui/ sorted by path, then the images under services/. It
// returns the manifest and the SHA-256 of the file it wrote.
//
// Nothing of the file's metadata comes from the disk: mode 0644, owner 0 and
// the modification time 0 (1970-01-01) for every entry. A time taken from the
// disk, the clock or a fixed 1980 of other packers would make two packages
// built from the same sources differ, and then the SHA-256 a catalog lists
// would depend on when someone packed, not on what is in the package. The
// readers ignore the time anyway: store.WalkUI accepts only the fixed
// "00000000000" in a ui-tar.
//
// Everything else is what Go archive/tar writes in USTAR form, so the shell,
// the API and the controller read the same bytes (vectors: packui.json). A
// name of more than 100 bytes goes into the prefix field, and one that does
// not fit there either — store.ValidPath allows up to 200 bytes — is refused
// instead of being written as a PAX entry: every header of a package of
// ngc-store is a USTAR header. Measured with a name of 150 bytes: asking for
// tar.FormatPAX instead gives the same bytes, so the form is pinned here for
// what it refuses, not for what it writes.
//
// A name or a size the contract refuses stops the run before the first byte
// is written, and a failure part way through removes the file again: a
// half-written package must never look like one.
func Pack(dir, out string) (*store.Manifest, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(dir, store.ManifestFile))
	if err != nil {
		return nil, nil, ErrPack
	}
	m, err := store.ParseManifest(raw)
	if err != nil {
		return nil, nil, err
	}
	files, err := collect(dir)
	if err != nil {
		return nil, nil, err
	}
	// The manifest counts as an entry, as it does in store.Walk.
	if 1+len(files) > store.MaxEntries {
		return nil, nil, ErrPack
	}
	sum, err := writePackage(dir, out, raw, files)
	if err != nil {
		os.Remove(out)
		return nil, nil, err
	}
	return m, sum, nil
}

// packEntry is one file of the package: its name in the archive and its size,
// both already checked against the contract.
type packEntry struct {
	name string
	size int64
}

// writePackage writes the archive and returns the SHA-256 of the whole file.
func writePackage(dir, out string, manifest []byte, files []packEntry) ([]byte, error) {
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(f, h))
	err = func() error {
		if err := writeFile(tw, store.ManifestFile, int64(len(manifest)), bytes.NewReader(manifest)); err != nil {
			return err
		}
		for _, e := range files {
			src, err := os.Open(filepath.Join(dir, filepath.FromSlash(e.name)))
			if err != nil {
				return err
			}
			err = writeFile(tw, e.name, e.size, src)
			src.Close()
			if err != nil {
				return err
			}
		}
		return tw.Close()
	}()
	if err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		return nil, err
	}
	// The package must read back exactly as store.Walk reads one; a packer
	// that writes something else is a bug, not a package. This is also where
	// a manifest that promises a ui entry or an icon the directory does not
	// hold is caught, because store.Walk checks that after the last file.
	if err := verify(out); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// collect lists the files of the package in the order they are written:
// everything below ui/ sorted by path, then the images that exist. A name,
// a type or a size the contract refuses stops the whole run, so nothing
// half-safe is ever written.
//
// The sizes come from the directory entries and are checked here; the writer
// then copies exactly that many bytes.
func collect(dir string) ([]packEntry, error) {
	var ui []packEntry
	var total int64
	root := filepath.Join(dir, "ui")
	// An app without a ui/ directory is a service-only app. Only the missing
	// root is allowed to be missing: a file that disappears during the walk
	// is a refusal, not a reason to pack the rest of the app.
	if _, err := os.Stat(root); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return withImages(dir, ui)
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// WalkDir reports the entry itself, never what a link points to:
		// links, devices, sockets and pipes are refused here, and store.Walk
		// would refuse them too. Only files belong in a package.
		if !d.Type().IsRegular() {
			return ErrPack
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := "ui/" + filepath.ToSlash(rel)
		if !validEntry(name) {
			return ErrPack
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size := info.Size()
		total += size
		if size > store.MaxUIFile || total > store.MaxUITotal {
			return ErrPack
		}
		ui = append(ui, packEntry{name, size})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(ui, func(a, b packEntry) int { return strings.Compare(a.name, b.name) })
	return withImages(dir, ui)
}

// withImages appends the images the directory holds to the ui files. An image
// that is no regular file or larger than store.MaxPackage is a refusal; one
// that is simply not there is how an app without a service looks.
func withImages(dir string, files []packEntry) ([]packEntry, error) {
	for _, name := range images {
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > store.MaxPackage {
			return nil, ErrPack
		}
		files = append(files, packEntry{name, info.Size()})
	}
	return files, nil
}

// validEntry reports whether name is a name a package may carry: the
// manifest, an image, or a file below ui/ whose path store.ValidPath allows.
// store.ValidPath is the one rule for those paths — it is what store.Walk
// reads the package back with — so "..", a leading slash, a hidden name and a
// control character are refused by it, not by a second rule of this packer.
func validEntry(name string) bool {
	if name == store.ManifestFile || slices.Contains(images, name) {
		return true
	}
	rest, ok := strings.CutPrefix(name, "ui/")
	return ok && store.ValidPath(rest)
}

// writeFile puts one regular file of size bytes into the archive. Every name
// passes validEntry again right before it is written, so no path can reach
// the archive that was not checked on the way the bytes take.
func writeFile(tw *tar.Writer, name string, size int64, r io.Reader) error {
	if !validEntry(name) || size < 0 {
		return ErrPack
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name,
		Mode: 0o644, Size: size, Format: tar.FormatUSTAR}); err != nil {
		return err
	}
	// CopyN, not Copy: exactly as many bytes as the header promises. A file
	// that shrank since collect measured it ends the run; one that grew is
	// written only as far as it was measured, so the header never lies about
	// the archive and no size limit is passed behind collect's back.
	if _, err := io.CopyN(tw, r, size); err != nil {
		return err
	}
	return nil
}

// verify reads the package back with the contract's own reader.
func verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = store.Walk(f, func(*store.Manifest, store.PackFile, io.Reader) error { return nil })
	return err
}
