package store

import (
	"archive/tar"
	"io"
	"strings"
)

// uiSlack is what a UI tar may hold besides its files' data: headers, PAX
// records and padding.
const uiSlack = 16 << 20

// WalkUI reads the tar of an app's UI that the API keeps (dbapi.BlobUI) and
// sends the shell (ngc.desktop/ui), and calls fn for every regular file with
// its data. The shell implements the same rules (vectors: ui.json):
//
//   - Walk's rules for ui/: entries "ui" or "ui/<ValidPath>" after removing
//     one trailing "/" (a file is never "ui"), directories of size 0 or
//     regular files only, no links, no sparse entries, no duplicates, no
//     entry under a file and no file where a directory is needed, at most
//     MaxEntries entries, MaxUIFile per file and MaxUITotal in all (checked
//     from the header, before reading).
//   - Headers as tar.Writer writes them by default: USTAR (magic "ustar\0",
//     version "00", only ASCII bytes, the fields mode, uid, gid, size, mtime,
//     devmajor and devminor ending in NUL), each perhaps after a PAX header
//     ('x') whose records override it (path, size).
//   - The archive ends with its end marker, two zero blocks, and nothing
//     follows it; with the marker it has at most MaxUITotal+16 MiB, and
//     WalkUI reads at most one byte more from r.
//
// Every failure is ErrPackage or fn's own error; after one the caller
// discards what fn received.
func WalkUI(r io.Reader, fn func(f PackFile, body io.Reader) error) error {
	const limit = MaxUITotal + uiSlack
	lr := &io.LimitedReader{R: r, N: limit + 1}
	read := func() int64 { return limit + 1 - lr.N }
	tr := tar.NewReader(lr)
	entries := newTree()
	var total, end int64 // end: where the data of the last entry ends, padding included
	for n := 1; ; n++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil || n > MaxEntries || sparse(h) {
			return ErrPackage
		}
		// Only what tar.Writer writes by default: USTAR headers, with PAX
		// records where USTAR has no room. archive/tar reads more dialects,
		// each one more thing the shell would have to read the same way.
		if h.Format != tar.FormatUSTAR && h.Format != tar.FormatPAX {
			return ErrPackage
		}
		name := strings.TrimSuffix(h.Name, "/")
		file := h.Typeflag == tar.TypeReg
		switch {
		case h.Typeflag == tar.TypeDir:
			if h.Size != 0 || (name != "ui" && !uiPath(name)) {
				return ErrPackage
			}
		case file:
			total += h.Size
			if !uiPath(name) || h.Size > MaxUIFile || total > MaxUITotal {
				return ErrPackage
			}
		default:
			return ErrPackage
		}
		// Checked, so short: the copy keeps no PAX buffer of the reader alive.
		name = strings.Clone(name)
		if !entries.add(name, file) {
			return ErrPackage
		}
		end = read() + (h.Size+511)&^511
		if !file {
			continue
		}
		if err := fn(PackFile{Path: name, Size: h.Size, Kind: KindUI}, uiBody{tr}); err != nil {
			return err
		}
	}
	// archive/tar reports the end just as quietly when the marker is missing
	// or one block short, and never reads what follows it; the shell and Go
	// must agree on all three.
	if read() != end+2*512 || lr.N == 0 {
		return ErrPackage
	}
	var one [1]byte
	if n, err := io.ReadFull(lr, one[:]); n != 0 || err != io.EOF {
		return ErrPackage
	}
	return nil
}

// uiBody is the data of one file for fn; data cut short is ErrPackage.
type uiBody struct{ r io.Reader }

func (b uiBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && err != io.EOF {
		err = ErrPackage
	}
	return n, err
}
