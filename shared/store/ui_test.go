package store

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
)

// uiVector is one case of ui.json: a UI tar and whether WalkUI accepts it,
// and for a valid one the files fn gets, in order.
type uiVector struct {
	Name  string    `json:"name"`
	Tar   enc.Bytes `json:"tar"`
	Valid bool      `json:"valid"`
	Files []string  `json:"files,omitempty"`
}

// uiSpec is an entry a test writes with UIWriter: a directory, or a file and
// its data.
type uiSpec struct {
	path string
	dir  bool
	data []byte
}

func uiDir(path string) uiSpec        { return uiSpec{path: path, dir: true} }
func uiFile(path, data string) uiSpec { return uiSpec{path: path, data: []byte(data)} }

// writeUI writes entries and the end marker with UIWriter.
func writeUI(t testing.TB, entries ...uiSpec) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := NewUIWriter(&buf)
	for _, e := range entries {
		var err error
		if e.dir {
			err = w.Dir(e.path)
		} else {
			err = w.File(e.path, int64(len(e.data)), bytes.NewReader(e.data))
		}
		if err != nil {
			t.Fatalf("%s: %v", e.path, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// header is a header block as UIWriter writes it, changed by patch, with its
// checksum computed again.
func header(flag byte, name string, size int64, patch func(*[blockSize]byte)) []byte {
	b := uiHeader(flag, name, size)
	if patch != nil {
		patch(b)
		copy(b[148:156], octalField(int64(headerSum(b)), 7)+" ")
	}
	return b[:]
}

// rawFile is a file entry: its header, changed by patch, and its data.
func rawFile(name, data string, patch func(*[blockSize]byte)) []byte {
	return append(header('0', name, int64(len(data)), patch), pad512([]byte(data))...)
}

// rawPAX is a PAX header, changed by patch, whose data is record.
func rawPAX(record string, patch func(*[blockSize]byte)) []byte {
	return append(header('x', "", int64(len(record)), patch), pad512([]byte(record))...)
}

// afterPAX is the header of a file of data "x" that follows a PAX header.
func afterPAX() []byte { return rawFile("", "x", nil) }

// deepFiles returns files whose paths imply dirs directories in all, "ui"
// included: "ui/dN" and 97 levels below it per file, as many as needed.
func deepFiles(dirs int) []uiSpec {
	var files []uiSpec
	for i, left := 0, dirs-1; left > 0; i++ {
		depth := min(left, 98)
		files = append(files, uiFile("ui/d"+strconv.Itoa(i)+strings.Repeat("/a", depth-1)+"/f", "x"))
		left -= depth
	}
	return files
}

// uiCases returns the cases of ui.json. Each invalid one breaks exactly one
// rule of WalkUI, "PAX path empty, the header keeps its name" aside, a case
// in which other tar readers disagree.
func uiCases(t testing.TB) []uiVector {
	t.Helper()
	end := endMarker
	cat := slices.Concat[[]byte]
	// The one file "ui/x.txt" of data "x", its header changed by patch.
	file := func(patch func(*[blockSize]byte)) []byte { return cat(rawFile("ui/x.txt", "x", patch), end) }
	// The same file, its checksum plus delta written by format.
	sumAs := func(format string, delta int) []byte {
		b := uiHeader('0', "ui/x.txt", 1)
		copy(b[148:156], fmt.Sprintf(format, headerSum(b)+delta))
		return cat(b[:], pad512([]byte("x")), end)
	}
	long := "ui/" + strings.Repeat("c", 150) // needs a PAX header
	record := paxPathRecord(long)
	rest := record[strings.IndexByte(record, ' '):] // " path=…\n"
	withPAX := func(data string) []byte { return cat(rawPAX(data, nil), afterPAX(), end) }
	n100 := "ui/" + strings.Repeat("n", 97)
	n101 := "ui/" + strings.Repeat("n", 98)
	p200 := "ui/" + strings.Repeat("a", 100) + "/" + strings.Repeat("b", 99)
	dLong := "ui/" + strings.Repeat("d", 98) // "ui/ddd…/" has 102 bytes
	deep := deepFiles(MaxDirs)
	var deepPaths []string
	for _, f := range deep {
		deepPaths = append(deepPaths, f.path)
	}

	return []uiVector{
		{Name: "valid", Tar: writeUI(t, uiDir("ui"), uiDir("ui/css"),
			uiFile("ui/index.html", "<!doctype html><title>Terminal</title>"), uiFile("ui/icon.svg", "<svg/>"),
			uiFile("ui/css/app.css", "body{margin:0}\n")),
			Valid: true, Files: []string{"ui/index.html", "ui/icon.svg", "ui/css/app.css"}},
		{Name: "one file", Tar: file(nil), Valid: true, Files: []string{"ui/x.txt"}},
		{Name: "only the end marker", Tar: slices.Clone(end), Valid: true},
		{Name: "empty file", Tar: writeUI(t, uiFile("ui/empty.txt", "")), Valid: true, Files: []string{"ui/empty.txt"}},
		{Name: "file of one block", Tar: writeUI(t, uiFile("ui/b.bin", strings.Repeat("z", blockSize))), Valid: true, Files: []string{"ui/b.bin"}},
		{Name: "name of 100 bytes in the header", Tar: writeUI(t, uiFile(n100, "x")), Valid: true, Files: []string{n100}},
		{Name: "name of 101 bytes in a PAX header", Tar: writeUI(t, uiFile(n101, "x")), Valid: true, Files: []string{n101}},
		{Name: "path of 200 bytes", Tar: writeUI(t, uiFile(p200, "x")), Valid: true, Files: []string{p200}},
		{Name: "directory with a long name", Tar: writeUI(t, uiDir(dLong), uiFile(dLong+"/f", "x")), Valid: true, Files: []string{dLong + "/f"}},
		// A directory may be named after a path has implied it, as long as no
		// entry names it twice.
		{Name: "directory after a file below it", Tar: writeUI(t, uiFile("ui/a/b", "x"), uiDir("ui/a"), uiDir("ui")), Valid: true, Files: []string{"ui/a/b"}},
		{Name: "MaxDirs directories", Tar: writeUI(t, deep...), Valid: true, Files: deepPaths},

		// The header layout, one field at a time.
		{Name: "name with bytes after its NUL", Tar: file(func(b *[blockSize]byte) { copy(b[9:], "junk") })},
		{Name: "name field empty", Tar: cat(rawFile("", "x", nil), end)},
		{Name: "file mode 0600", Tar: file(func(b *[blockSize]byte) { copy(b[100:108], "0000600\x00") })},
		{Name: "mode not octal", Tar: file(func(b *[blockSize]byte) { copy(b[100:108], "0000999\x00") })},
		{Name: "mode with setuid", Tar: file(func(b *[blockSize]byte) { copy(b[100:108], "0004644\x00") })},
		{Name: "directory with the mode of a file", Tar: cat(header('5', "ui/d/", 0, func(b *[blockSize]byte) { copy(b[100:108], "0000644\x00") }), end)},
		{Name: "uid 1", Tar: file(func(b *[blockSize]byte) { copy(b[108:116], "0000001\x00") })},
		{Name: "gid 1", Tar: file(func(b *[blockSize]byte) { copy(b[116:124], "0000001\x00") })},
		{Name: "size with a leading space", Tar: file(func(b *[blockSize]byte) { copy(b[124:136], " 0000000001\x00") })},
		{Name: "size 1, then NUL bytes", Tar: file(func(b *[blockSize]byte) { copy(b[124:136], "1\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00") })},
		{Name: "size without its NUL", Tar: file(func(b *[blockSize]byte) { b[135] = ' ' })},
		{Name: "mtime 1", Tar: file(func(b *[blockSize]byte) { copy(b[136:148], "00000000001\x00") })},
		{Name: "wrong checksum", Tar: sumAs("%06o\x00 ", 1)},
		{Name: "checksum of 7 digits and NUL", Tar: sumAs("%07o\x00", 0)},
		{Name: "checksum with leading spaces", Tar: sumAs("%7o ", 0)},
		{Name: "checksum without its space", Tar: sumAs("%06o\x00\x00", 0)},
		{Name: "checksum with its NUL and space swapped", Tar: sumAs("%06o \x00", 0)},
		{Name: "typeflag NUL", Tar: file(func(b *[blockSize]byte) { b[156] = 0 })},
		{Name: "typeflag NUL with a trailing slash", Tar: cat(header('5', "ui/nd/", 0, func(b *[blockSize]byte) { b[156] = 0 }), end)},
		{Name: "symlink", Tar: cat(header('2', "ui/link", 0, nil), end)},
		{Name: "hard link", Tar: cat(header('1', "ui/hardlink", 0, nil), end)},
		{Name: "fifo", Tar: cat(header('6', "ui/fifo", 0, nil), end)},
		{Name: "contiguous file", Tar: file(func(b *[blockSize]byte) { b[156] = '7' })},
		{Name: "global PAX header", Tar: cat(rawPAX(record, func(b *[blockSize]byte) { b[156] = 'g' }), afterPAX(), end)},
		{Name: "linkname on a file", Tar: file(func(b *[blockSize]byte) { copy(b[157:], "/etc/shadow") })},
		{Name: "magic of GNU tar", Tar: file(func(b *[blockSize]byte) { copy(b[257:265], "ustar  \x00") })},
		{Name: "version xx", Tar: file(func(b *[blockSize]byte) { copy(b[263:265], "xx") })},
		{Name: "version NUL NUL", Tar: file(func(b *[blockSize]byte) { copy(b[263:265], "\x00\x00") })},
		{Name: "uname and gname", Tar: file(func(b *[blockSize]byte) { copy(b[265:], "root"); copy(b[297:], "root") })},
		{Name: "devmajor", Tar: file(func(b *[blockSize]byte) { copy(b[329:337], "0000007\x00") })},
		{Name: "prefix", Tar: file(func(b *[blockSize]byte) { copy(b[345:], "ui") })},
		{Name: "STAR trailer", Tar: file(func(b *[blockSize]byte) { copy(b[508:], "tar\x00") })},
		{Name: "bytes 500 to 507", Tar: file(func(b *[blockSize]byte) { copy(b[500:], "GARBAGE!") })},
		{Name: "directory with a size", Tar: cat(header('5', "ui/d/", 1, nil), pad512([]byte("x")), end)},
		// With nothing after it but the end marker. A reader without the rule
		// that skips a directory's data accepts the case above and refuses
		// this one; one that reads no data for a directory does the opposite.
		{Name: "directory with a size, header only", Tar: cat(header('5', "ui/d/", 1, nil), end)},

		// PAX headers.
		{Name: "PAX header with a name", Tar: cat(rawPAX(record, func(b *[blockSize]byte) { copy(b[:], "PaxHeaders/0") }), afterPAX(), end)},
		{Name: "PAX header with mode 0000", Tar: cat(rawPAX(record, func(b *[blockSize]byte) { copy(b[100:108], "0000000\x00") }), afterPAX(), end)},
		{Name: "name after a PAX header", Tar: cat(rawPAX(record, nil), rawFile("ui/y.txt", "x", nil), end)},
		{Name: "PAX uid record", Tar: withPAX(record + paxRecord("uid", "abc"))},
		{Name: "PAX mtime record", Tar: withPAX(record + paxRecord("mtime", "x"))},
		{Name: "PAX comment record", Tar: withPAX(record + paxRecord("comment", "hi"))},
		{Name: "PAX linkpath record", Tar: withPAX(record + paxRecord("linkpath", "/etc/shadow"))},
		{Name: "PAX size record", Tar: cat(rawPAX(record+paxRecord("size", "1"), nil), header('0', "", 0, nil), pad512([]byte("x")), end)},
		{Name: "PAX size record with a plus", Tar: cat(rawPAX(record+paxRecord("size", "+1"), nil), header('0', "", 0, nil), pad512([]byte("x")), end)},
		{Name: "PAX size record below 0", Tar: withPAX(record + paxRecord("size", "-1"))},
		{Name: "PAX path twice", Tar: withPAX(record + paxPathRecord("ui/"+strings.Repeat("e", 150)))},
		{Name: "PAX path empty", Tar: withPAX(paxPathRecord(""))},
		{Name: "PAX path empty, the header keeps its name", Tar: cat(rawPAX(paxPathRecord(""), nil), rawFile("ui/keep.txt", "x", nil), end)},
		{Name: "PAX path for a short name", Tar: withPAX(paxPathRecord("ui/p.txt"))},
		{Name: "PAX path of 100 bytes", Tar: withPAX(paxPathRecord(n100))},
		{Name: "PAX length with a leading zero", Tar: withPAX("0" + strconv.Itoa(len(record)+1) + rest)},
		{Name: "PAX length with a plus sign", Tar: withPAX("+" + strconv.Itoa(len(record)+1) + rest)},
		{Name: "PAX length off by one", Tar: withPAX(strconv.Itoa(len(record)+1) + rest)},
		{Name: "PAX record without its newline", Tar: withPAX(strconv.Itoa(len(record)-1) + rest[:len(rest)-1])},
		{Name: "PAX size longer than its record", Tar: cat(header('x', "", int64(len(record)+7), nil), pad512([]byte(record)), afterPAX(), end)},
		{Name: "PAX size shorter than its record", Tar: cat(header('x', "", int64(len(record)-1), nil), pad512([]byte(record)), afterPAX(), end)},
		{Name: "two PAX headers in a row", Tar: cat(rawPAX(record, nil), rawPAX(paxPathRecord("ui/"+strings.Repeat("e", 150)), nil), afterPAX(), end)},
		// The header after a PAX header is the entry's own; one of typeflag x
		// is refused there even with data a file could have. The case above
		// fails a reader that takes it for the entry anyway, at the header
		// after it.
		{Name: "PAX header, then an x header with data", Tar: cat(rawPAX(record, nil), rawPAX("x", nil), end)},
		{Name: "PAX header before the end marker", Tar: cat(rawPAX(record, nil), end)},
		{Name: "PAX padding not NUL", Tar: cat(func() []byte {
			b := rawPAX(record, nil)
			b[len(b)-1] = 'x'
			return b
		}(), afterPAX(), end)},
		// Declared too long and nothing after it: WalkUI refuses the header
		// before it reads the data; one with its data is in TestWalkUILimits.
		{Name: "PAX data longer than a block, header only", Tar: cat(header('x', "", blockSize+1, nil), end)},

		// Data and the end of the archive.
		{Name: "data padding not NUL", Tar: cat(func() []byte {
			b := rawFile("ui/x.txt", "x", nil)
			b[len(b)-1] = 'x'
			return b
		}(), end)},
		{Name: "truncated archive", Tar: rawFile("ui/x.txt", "0123456789", nil)[:blockSize+5]},
		{Name: "empty archive", Tar: []byte{}},
		{Name: "no end marker", Tar: rawFile("ui/x.txt", "x", nil)},
		{Name: "one zero block", Tar: cat(rawFile("ui/x.txt", "x", nil), end[:blockSize])},
		{Name: "an entry after one zero block", Tar: cat(rawFile("ui/x.txt", "x", nil), end[:blockSize], rawFile("ui/y.txt", "y", nil), end)},
		{Name: "data after the end marker", Tar: cat(rawFile("ui/x.txt", "x", nil), end, end[:blockSize])},

		// Names, the tree and the limits.
		{Name: "parent directory", Tar: cat(rawFile("ui/../x", "x", nil), end)},
		{Name: "absolute path", Tar: cat(rawFile("/ui/x", "x", nil), end)},
		{Name: "outside ui", Tar: cat(rawFile(ManifestFile, "{}", nil), end)},
		{Name: "hidden file", Tar: cat(rawFile("ui/.htaccess", "x", nil), end)},
		{Name: "non-ASCII name", Tar: cat(rawFile("ui/\xc3\xa4.txt", "x", nil), end)},
		{Name: "path of 201 bytes", Tar: withPAX(paxPathRecord(p200 + "b"))},
		{Name: "file named ui", Tar: cat(rawFile("ui", "x", nil), end)},
		{Name: "directory without its slash", Tar: cat(header('5', "ui/d", 0, nil), end)},
		{Name: "directory ui/../", Tar: cat(header('5', "ui/../", 0, nil), end)},
		{Name: "directory ../", Tar: cat(header('5', "../", 0, nil), end)},
		{Name: "directory ui/.git/", Tar: cat(header('5', "ui/.git/", 0, nil), end)},
		{Name: "directory ui//", Tar: cat(header('5', "ui//", 0, nil), end)},
		{Name: "file with a trailing slash", Tar: cat(rawFile("ui/t.txt/", "x", nil), end)},
		{Name: "duplicate", Tar: cat(rawFile("ui/x.txt", "x", nil), rawFile("ui/x.txt", "y", nil), end)},
		{Name: "directory twice", Tar: cat(header('5', "ui/a/", 0, nil), header('5', "ui/a/", 0, nil), end)},
		{Name: "directory with the name of a file", Tar: cat(rawFile("ui/a", "x", nil), header('5', "ui/a/", 0, nil), end)},
		{Name: "entry under a file", Tar: cat(rawFile("ui/x.txt", "x", nil), rawFile("ui/x.txt/y", "y", nil), end)},
		{Name: "file where a directory is needed", Tar: cat(rawFile("ui/a/b", "x", nil), rawFile("ui/a", "y", nil), end)},
		{Name: "file where a directory entry needs a directory", Tar: cat(header('5', "ui/a/b/", 0, nil), rawFile("ui/a", "y", nil), end)},
		// Declared too large and nothing after it: WalkUI refuses the header
		// before it reads the data; one with its data is in TestWalkUILimits.
		{Name: "file too large, header only", Tar: cat(header('0', "ui/big.bin", MaxUIFile+1, nil), end)},
		{Name: "one directory too many", Tar: cat(withoutEnd(writeUI(t, deep...)), rawFile("ui/extra/f", "x", nil), end)},
	}
}

// walkUIFiles runs WalkUI over b and returns the paths and data of the files
// fn got.
func walkUIFiles(b []byte) ([]string, [][]byte, error) {
	var paths []string
	var data [][]byte
	err := WalkUI(bytes.NewReader(b), func(f PackFile, body io.Reader) error {
		d, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		if f.Kind != KindUI || f.Arch != "" || int64(len(d)) != f.Size {
			return errors.New("file described wrongly")
		}
		paths, data = append(paths, f.Path), append(data, d)
		return nil
	})
	return paths, data, err
}

// rewrite reads b with walkUI and writes what it read again with UIWriter.
func rewrite(t testing.TB, b []byte) ([]byte, error) {
	var entries []uiSpec
	err := walkUI(bytes.NewReader(b), func(f PackFile, body io.Reader) error {
		d, err := io.ReadAll(body)
		entries = append(entries, uiSpec{path: f.Path, data: d})
		return err
	}, func(path string) { entries = append(entries, uiDir(path)) })
	if err != nil {
		return nil, err
	}
	return writeUI(t, entries...), nil
}

func TestUIVectors(t *testing.T) {
	cases := uiCases(t)
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.Name] {
			t.Errorf("%s twice", c.Name)
		}
		names[c.Name] = true
		paths, _, err := walkUIFiles(c.Tar)
		if (err == nil) != c.Valid || (err != nil && err != ErrPackage) {
			t.Errorf("%s: %v", c.Name, err)
		}
		if c.Valid && !slices.Equal(paths, c.Files) {
			t.Errorf("%s: files %q, want %q", c.Name, paths, c.Files)
		}
		// One form only: what WalkUI accepts is exactly what UIWriter writes.
		if again, err := rewrite(t, c.Tar); c.Valid && (err != nil || !bytes.Equal(again, c.Tar)) {
			t.Errorf("%s: UIWriter writes it otherwise", c.Name)
		}
	}
	// The header cases change what UIWriter writes for one file.
	if !bytes.Equal(slices.Concat(rawFile("ui/x.txt", "x", nil), endMarker), writeUI(t, uiFile("ui/x.txt", "x"))) {
		t.Error("rawFile differs from UIWriter")
	}
	golden.Check(t, *update, "ui.json", cases)
}

func TestUIWriter(t *testing.T) {
	// Write, then walk: the same files with the same data.
	big := strings.Repeat("z", 3*blockSize+7)
	b := writeUI(t, uiDir("ui"), uiFile("ui/index.html", "<!doctype html>"), uiFile("ui/big.bin", big),
		uiFile("ui/"+strings.Repeat("p", 150), "long"), uiDir("ui/"+strings.Repeat("q", 120)), uiFile("ui/empty", ""))
	paths, data, err := walkUIFiles(b)
	if err != nil || len(paths) != 4 || paths[1] != "ui/big.bin" || string(data[1]) != big || string(data[2]) != "long" || len(data[3]) != 0 {
		t.Fatalf("round trip: %q, %v", paths, err)
	}
	refuse := map[string]func(w *UIWriter) error{
		"bad name":              func(w *UIWriter) error { return w.File("ui/../x", 1, strings.NewReader("x")) },
		"file named ui":         func(w *UIWriter) error { return w.File("ui", 1, strings.NewReader("x")) },
		"directory outside ui":  func(w *UIWriter) error { return w.Dir("x") },
		"file too large":        func(w *UIWriter) error { return w.File("ui/x", MaxUIFile+1, strings.NewReader("x")) },
		"negative size":         func(w *UIWriter) error { return w.File("ui/x", -1, strings.NewReader("x")) },
		"fewer bytes than size": func(w *UIWriter) error { return w.File("ui/x", 2, strings.NewReader("x")) },
		"duplicate": func(w *UIWriter) error {
			_ = w.File("ui/x", 1, strings.NewReader("x"))
			return w.File("ui/x", 1, strings.NewReader("x"))
		},
		"under a file": func(w *UIWriter) error {
			_ = w.File("ui/x", 1, strings.NewReader("x"))
			return w.Dir("ui/x/y")
		},
		"one directory too many": func(w *UIWriter) error {
			for _, f := range deepFiles(MaxDirs) {
				_ = w.File(f.path, 1, strings.NewReader("x"))
			}
			return w.File("ui/extra/f", 1, strings.NewReader("x"))
		},
		"after Close": func(w *UIWriter) error {
			_ = w.Close()
			return w.Dir("ui")
		},
	}
	for name, f := range refuse {
		w := NewUIWriter(io.Discard)
		if err := f(w); err != ErrPackage {
			t.Errorf("%s: got %v, want %v", name, err, ErrPackage)
		}
		if err := w.Dir("ui"); err == nil {
			t.Errorf("%s: the writer goes on after an error", name)
		}
	}
}

// TestUIFromPackage builds the UI tar of a package as the API does: Walk the
// package, write its UI files with UIWriter, and WalkUI reads them back.
func TestUIFromPackage(t *testing.T) {
	var buf bytes.Buffer
	w := NewUIWriter(&buf)
	_, err := Walk(bytes.NewReader(buildTar(t, goodEntries(t))), func(m *Manifest, f PackFile, body io.Reader) error {
		if f.Kind != KindUI {
			return nil
		}
		return w.File(f.Path, f.Size, body)
	})
	if err != nil || w.Close() != nil {
		t.Fatal(err)
	}
	paths, _, err := walkUIFiles(buf.Bytes())
	if err != nil || !slices.Equal(paths, []string{"ui/index.html", "ui/icon.svg"}) {
		t.Errorf("%q, %v", paths, err)
	}
}

func TestWalkUI(t *testing.T) {
	valid := writeUI(t, uiDir("ui"), uiFile("ui/a", "aa"), uiFile("ui/b", "bb"))
	// fn's own error comes back as it is, and fn never sees a directory.
	stop := errors.New("stop")
	var seen []string
	err := WalkUI(bytes.NewReader(valid), func(f PackFile, _ io.Reader) error { seen = append(seen, f.Path); return stop })
	if err != stop || !slices.Equal(seen, []string{"ui/a"}) {
		t.Errorf("fn's error: %v after %q", err, seen)
	}
	// Data cut short reaches fn as ErrPackage.
	var readErr error
	err = WalkUI(bytes.NewReader(rawFile("ui/x.txt", "0123456789", nil)[:blockSize+5]), func(_ PackFile, body io.Reader) error {
		_, readErr = io.ReadAll(body)
		return readErr
	})
	if err != ErrPackage || readErr != ErrPackage {
		t.Errorf("cut data: %v, fn read %v", err, readErr)
	}
	// fn may leave data unread; WalkUI skips it, and finds it cut short too.
	big := writeUI(t, uiFile("ui/a", strings.Repeat("a", 3*blockSize+1)), uiFile("ui/b", "bb"))
	for _, b := range [][]byte{valid, big} {
		if err := WalkUI(bytes.NewReader(b), func(PackFile, io.Reader) error { return nil }); err != nil {
			t.Errorf("unread data: %v", err)
		}
	}
	if err := WalkUI(bytes.NewReader(big[:2*blockSize+7]), func(PackFile, io.Reader) error { return nil }); err != ErrPackage {
		t.Errorf("unread data cut short: %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// zeros reads as n NUL bytes.
func zeros(n int64) io.Reader { return io.LimitReader(zeroReader{}, n) }

// uiPipe streams a UI tar: n files "ui/f<i>" of size NUL bytes each, written
// by UIWriter, then raw instead of the end marker if raw is set. The caller
// closes the reader, which ends the writer.
func uiPipe(n int, size int64, raw []byte) *io.PipeReader {
	pr, pw := io.Pipe()
	go func() {
		w := NewUIWriter(pw)
		var err error
		for i := 0; i < n && err == nil; i++ {
			err = w.File("ui/f"+strconv.Itoa(i), size, zeros(size))
		}
		switch {
		case err == nil && raw != nil:
			_, err = pw.Write(raw)
		case err == nil:
			err = w.Close()
		}
		pw.CloseWithError(err)
	}()
	return pr
}

// TestWalkUILimits covers the limits too large for ui.json, on both sides,
// with the data in the archive: WalkUI refuses from the header, while a
// reader that reads the data first could pass the header-only vectors.
func TestWalkUILimits(t *testing.T) {
	bigRecord := paxPathRecord("ui/" + strings.Repeat("x", 1<<20+1-17))
	if len(bigRecord) != 1<<20+1 {
		t.Fatalf("record of %d bytes", len(bigRecord))
	}
	cases := []struct {
		name  string
		tar   func() io.Reader
		valid bool
	}{
		{"MaxUIFile", func() io.Reader { return uiPipe(1, MaxUIFile, nil) }, true},
		{"file too large, with its data", func() io.Reader {
			return io.MultiReader(bytes.NewReader(header('0', "ui/big.bin", MaxUIFile+1, nil)), zeros(MaxUIFile+blockSize), bytes.NewReader(endMarker))
		}, false},
		{"MaxUITotal in all", func() io.Reader { return uiPipe(MaxUITotal/MaxUIFile, MaxUIFile, nil) }, true},
		{"total too large", func() io.Reader {
			return uiPipe(MaxUITotal/MaxUIFile, MaxUIFile, slices.Concat(rawFile("ui/one-more", "x", nil), endMarker))
		}, false},
		{"MaxEntries entries", func() io.Reader { return uiPipe(MaxEntries, 0, nil) }, true},
		{"too many entries", func() io.Reader {
			return uiPipe(MaxEntries, 0, slices.Concat(rawFile("ui/one-more", "", nil), endMarker))
		}, false},
		{"PAX data of 1 MiB and 1 byte", func() io.Reader {
			return io.MultiReader(bytes.NewReader(rawPAX(bigRecord, nil)), bytes.NewReader(afterPAX()), bytes.NewReader(endMarker))
		}, false},
	}
	for _, c := range cases {
		r := c.tar()
		files := 0
		err := WalkUI(r, func(_ PackFile, body io.Reader) error {
			files++
			_, err := io.Copy(io.Discard, body)
			return err
		})
		if pr, ok := r.(*io.PipeReader); ok {
			pr.Close()
		}
		if (err == nil) != c.valid || (err != nil && err != ErrPackage) {
			t.Errorf("%s: %v after %d files", c.name, err, files)
		}
	}
}

// TestWalkUITreeCost: paths of distinct directories 97 levels deep would
// make the tree keep a directory per level, over 100 MiB for MaxEntries of
// them; MaxDirs refuses them after ten files. The most the tree keeps is
// MaxEntries names of 200 bytes.
func TestWalkUITreeCost(t *testing.T) {
	var deep bytes.Buffer
	for i := range MaxEntries - 1 {
		deep.Write(rawPAX(paxPathRecord(fmt.Sprintf("ui/x%05d%s/f", i, strings.Repeat("/a", 96))), nil))
		deep.Write(afterPAX())
	}
	deep.Write(endMarker)
	var long []uiSpec
	for i := range MaxEntries {
		long = append(long, uiFile(fmt.Sprintf("ui/%05d%s", i, strings.Repeat("n", 192)), ""))
	}
	cases := []struct {
		name  string
		tar   []byte
		valid bool
		max   uint64
	}{
		{"deep distinct paths", deep.Bytes(), false, 256 << 10},
		{"MaxEntries long names", writeUI(t, long...), true, 16 << 20},
	}
	for _, c := range cases {
		files := 0
		var err error
		n := allocated(func() {
			err = WalkUI(bytes.NewReader(c.tar), func(PackFile, io.Reader) error { files++; return nil })
		})
		t.Logf("%s: %d bytes, %d files, %d bytes allocated", c.name, len(c.tar), files, n)
		if (err == nil) != c.valid || n > c.max {
			t.Errorf("%s: %v after %d files, %d bytes allocated", c.name, err, files, n)
		}
	}
}

func FuzzWalkUI(f *testing.F) {
	for _, c := range uiCases(f) {
		if len(c.Tar) < 64<<10 {
			f.Add([]byte(c.Tar))
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		paths, data, err := walkUIFiles(b)
		if err != nil {
			return
		}
		// What passes keeps the limits, and is exactly what UIWriter writes
		// for the same entries: the form has one encoding.
		var total int64
		for i, p := range paths {
			if !uiPath(p) || len(data[i]) > MaxUIFile {
				t.Fatalf("file %q of %d bytes", p, len(data[i]))
			}
			total += int64(len(data[i]))
		}
		if total > MaxUITotal || len(paths) > MaxEntries {
			t.Fatalf("%d files, %d bytes", len(paths), total)
		}
		again, err := rewrite(t, b)
		if err != nil || !bytes.Equal(again, b) {
			t.Fatalf("accepted, but UIWriter writes it otherwise: %v", err)
		}
	})
}

// packFile is the entry of a package for a UI file of a test.
func packFile(s uiSpec) entry { return entry{s.path, s.data, tar.TypeReg} }

// uiTypeflags returns the typeflag of every header block of a ui-tar, in
// order, up to the end marker.
func uiTypeflags(b []byte) string {
	var flags []byte
	for pos := 0; pos+blockSize <= len(b) && !allNUL(b[pos:pos+blockSize]); {
		h := b[pos : pos+blockSize]
		size, _ := octalValue(h[124:136])
		flags = append(flags, h[156])
		pos += blockSize + int((size+blockSize-1)/blockSize)*blockSize
	}
	return string(flags)
}

// failingWriter refuses every write with err.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestPackageUI: the ui-tar of a package holds its UI files in the order of
// the package and no directory, WalkUI reads it, and the hash is its
// SHA-256. What is not a plain valid package is ErrPackage; an error of w
// comes back as it is.
func TestPackageUI(t *testing.T) {
	index := uiFile("ui/index.html", "<!doctype html><title>Terminal</title>")
	pngB, icon, pngA := uiFile("ui/img/b.png", "b"), uiFile("ui/icon.svg", "<svg/>"), uiFile("ui/img/a.png", "a")
	pkg := buildTar(t, []entry{manifestEntry(t), {"ui/", nil, tar.TypeDir}, packFile(index), {"services/", nil, tar.TypeDir},
		{"services/amd64.tar", []byte("image"), tar.TypeReg}, {"ui/img/", nil, tar.TypeDir}, packFile(pngB), packFile(icon), packFile(pngA)})
	var out bytes.Buffer
	m, hash, err := PackageUI(bytes.NewReader(pkg), &out)
	if err != nil || m.ID != "org.sorglos.terminal" {
		t.Fatalf("%+v, %v", m, err)
	}
	// The package's order, not sorted, and exactly what UIWriter writes for
	// these files alone.
	if !bytes.Equal(out.Bytes(), writeUI(t, index, pngB, icon, pngA)) {
		t.Error("not the UI files of the package in its order")
	}
	var paths, dirs []string
	err = walkUI(bytes.NewReader(out.Bytes()), func(f PackFile, _ io.Reader) error {
		paths = append(paths, f.Path)
		return nil
	}, func(path string) { dirs = append(dirs, path) })
	if err != nil || len(dirs) != 0 || !slices.Equal(paths, []string{"ui/index.html", "ui/img/b.png", "ui/icon.svg", "ui/img/a.png"}) {
		t.Errorf("WalkUI: %q, directories %q, %v", paths, dirs, err)
	}
	if sum := sha256.Sum256(out.Bytes()); !bytes.Equal(hash, sum[:]) {
		t.Error("hash is not the SHA-256 of the ui-tar")
	}

	var sealed bytes.Buffer
	if err := Encrypt(&sealed, bytes.NewReader(pkg), contentKey, fixedRand(0x24)); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{
		"encrypted package": sealed.Bytes(),
		// The header of the last file, then nothing: archive/tar reports
		// io.ErrUnexpectedEOF while its data is read.
		"cut short in the data of a ui file": pkg[:len(pkg)-len(endMarker)-blockSize],
		// Refused only after every file went to w.
		"ui entry missing": smallBadPackages(t)["ui entry missing"],
	} {
		if m, hash, err := PackageUI(bytes.NewReader(bad), io.Discard); err != ErrPackage || m != nil || hash != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	full := errors.New("disk full")
	if _, _, err := PackageUI(bytes.NewReader(pkg), failingWriter{full}); err != full {
		t.Errorf("error of w: %v", err)
	}
}

// packUIVector is one case of packui.json: an app package as ngc-store pack
// writes it (archive/tar with the writer's defaults), the ui-tar PackageUI
// makes of it and that ui-tar's SHA-256, the app's UI hash.
type packUIVector struct {
	Name    string    `json:"name"`
	Package enc.Bytes `json:"package"`
	UI      enc.Bytes `json:"ui"`
	Hash    enc.Bytes `json:"hash"`
}

// longUIPath is a UI file name of 150 bytes: archive/tar splits it into the
// prefix and name fields of a USTAR header, a ui-tar needs a PAX header.
var longUIPath = "ui/" + strings.Repeat("d", 99) + "/" + strings.Repeat("f", 43) + ".css"

// packUICases returns the cases of packui.json. Each ui-tar is written from
// the files it must hold, not by PackageUI, so the vector states what
// PackageUI has to make.
func packUICases(t testing.TB) []packUIVector {
	t.Helper()
	dir := func(name string) entry { return entry{name, nil, tar.TypeDir} }
	image := func(name string) entry { return entry{name, []byte("podman image"), tar.TypeReg} }
	index := uiFile("ui/index.html", `<!doctype html><title>Terminal</title><link rel="stylesheet" href="css/app.css">`)
	css := uiFile("ui/css/app.css", "body{margin:0}\n")
	icon := uiFile("ui/icon.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	calcIndex := uiFile("ui/index.html", "<!doctype html><title>Rechner</title>")
	long := uiFile(longUIPath, "p{margin:0}\n")
	calculator := &Manifest{ID: "org.sorglos.calculator", Name: "Rechner", Version: "1.0.0", Publisher: "Sorglos IT",
		UI: &UI{Entry: "index.html"}}
	backup := &Manifest{ID: "org.sorglos.backup", Name: "Sicherung", Version: "1.0.0", Publisher: "Sorglos IT",
		Services: []Service{{Name: "backup", Command: []string{"/usr/local/bin/ngc-backup"}}}}

	cases := []struct {
		name    string
		entries []entry
		ui      []uiSpec // the files of the ui-tar, in order
	}{
		{"files, directories and an image", []entry{manifestEntry(t), dir("ui/"), packFile(index), dir("ui/css/"), packFile(css),
			dir("services/"), image("services/amd64.tar"), packFile(icon)}, []uiSpec{index, css, icon}},
		{"long file name", []entry{manifestOf(t, calculator), packFile(calcIndex), packFile(long)}, []uiSpec{calcIndex, long}},
		{"no ui files", []entry{manifestOf(t, backup), image("services/amd64.tar"), image("services/arm64.tar")}, nil},
	}
	var out []packUIVector
	for _, c := range cases {
		ui := writeUI(t, c.ui...)
		sum := sha256.Sum256(ui)
		out = append(out, packUIVector{Name: c.name, Package: buildTar(t, c.entries), UI: ui, Hash: sum[:]})
	}
	return out
}

func TestPackageUIVectors(t *testing.T) {
	cases := packUICases(t)
	flags := map[string]string{}
	long := 0
	for _, c := range cases {
		var out bytes.Buffer
		m, hash, err := PackageUI(bytes.NewReader(c.Package), &out)
		if err != nil || m == nil || !bytes.Equal(out.Bytes(), c.UI) || !bytes.Equal(hash, c.Hash) {
			t.Errorf("%s: PackageUI differs: %v", c.Name, err)
		}
		if _, _, err := walkUIFiles(c.UI); err != nil {
			t.Errorf("%s: WalkUI: %v", c.Name, err)
		}
		flags[c.Name] = uiTypeflags(c.UI)
		// archive/tar wrote every header as USTAR, so the long name went into
		// the prefix and name fields.
		tr := tar.NewReader(bytes.NewReader(c.Package))
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil || h.Format != tar.FormatUSTAR {
				t.Fatalf("%s: %+v, %v", c.Name, h, err)
			}
			if h.Name == longUIPath {
				long++
			}
		}
	}
	if long != 1 {
		t.Errorf("the long name in %d packages", long)
	}
	// Directories are left out, and only the long name has a PAX header.
	want := map[string]string{"files, directories and an image": "000", "long file name": "0x0", "no ui files": ""}
	if !maps.Equal(flags, want) {
		t.Errorf("typeflags %q, want %q", flags, want)
	}
	golden.Check(t, *update, "packui.json", cases)
}

// FuzzPackageUI: whatever Walk accepts, PackageUI turns into a ui-tar WalkUI
// accepts, with the UI files Walk hands out, in the same order and with the
// same data, and the hash is its SHA-256. What Walk refuses, PackageUI
// refuses as ErrPackage.
func FuzzPackageUI(f *testing.F) {
	for _, c := range packUICases(f) {
		f.Add([]byte(c.Package))
	}
	for _, b := range smallBadPackages(f) {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var paths []string
		var data [][]byte
		_, walkErr := Walk(bytes.NewReader(b), func(_ *Manifest, pf PackFile, body io.Reader) error {
			d, err := io.ReadAll(body)
			if pf.Kind == KindUI {
				paths, data = append(paths, pf.Path), append(data, d)
			}
			return err
		})
		var out bytes.Buffer
		m, hash, err := PackageUI(bytes.NewReader(b), &out)
		if walkErr != nil {
			if err != ErrPackage || m != nil || hash != nil {
				t.Fatalf("Walk refuses (%v), PackageUI: %v", walkErr, err)
			}
			return
		}
		if err != nil || m == nil {
			t.Fatalf("Walk accepts, PackageUI refuses: %v", err)
		}
		if sum := sha256.Sum256(out.Bytes()); !bytes.Equal(hash, sum[:]) {
			t.Fatal("hash is not the SHA-256 of the ui-tar")
		}
		gotPaths, gotData, err := walkUIFiles(out.Bytes())
		if err != nil || !slices.Equal(gotPaths, paths) || !slices.EqualFunc(gotData, data, bytes.Equal) {
			t.Fatalf("ui-tar holds %q, Walk handed out %q: %v", gotPaths, paths, err)
		}
	})
}
