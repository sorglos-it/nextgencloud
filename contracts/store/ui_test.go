package store

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

// uiVector is one case of ui.json: a UI tar and whether WalkUI accepts it,
// and for a valid one the files fn gets, in order.
type uiVector struct {
	Name  string    `json:"name"`
	Tar   enc.Bytes `json:"tar"`
	Valid bool      `json:"valid"`
	Files []string  `json:"files,omitempty"`
}

// goodUI returns the entries of a valid UI tar.
func goodUI() []entry {
	return []entry{
		{"ui/", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Terminal</title>"), tar.TypeReg},
		{"ui/icon.svg", []byte("<svg/>"), tar.TypeReg},
		{"ui/css/app.css", []byte("body{margin:0}\n"), tar.TypeReg},
	}
}

// uiCases returns the cases of ui.json. Each invalid one differs from the
// valid one by breaking exactly one rule of WalkUI.
func uiCases(t testing.TB) []uiVector {
	t.Helper()
	good := goodUI()
	valid := buildTar(t, good)
	entries := withoutEnd(valid)
	with := func(extra ...entry) []byte { return buildTar(t, append(goodUI(), extra...)) }
	goodFiles := []string{"ui/index.html", "ui/icon.svg", "ui/css/app.css"}
	// 200 bytes below ui/, split by USTAR into prefix and name; one byte more
	// is no ValidPath.
	path200 := "ui/" + strings.Repeat("a", 100) + "/" + strings.Repeat("b", 99)
	path201 := "ui/" + strings.Repeat("a", 100) + "/" + strings.Repeat("b", 100)
	// A name too long for USTAR, so tar.Writer puts it in a PAX record.
	paxName := "ui/" + strings.Repeat("c", 150)
	cssData := bytes.Index(valid, good[3].body)
	return []uiVector{
		{Name: "valid", Tar: valid, Valid: true, Files: goodFiles},
		{Name: "only the end marker", Tar: slices.Clone(endMarker), Valid: true},
		{Name: "path of 200 bytes", Tar: with(entry{path200, []byte("x"), tar.TypeReg}), Valid: true, Files: append(slices.Clone(goodFiles), path200)},
		{Name: "file name in a PAX header", Tar: with(entry{paxName, []byte("x"), tar.TypeReg}), Valid: true, Files: append(slices.Clone(goodFiles), paxName)},

		{Name: "symlink", Tar: with(entry{"ui/link", nil, tar.TypeSymlink})},
		{Name: "hard link", Tar: with(entry{"ui/hardlink", nil, tar.TypeLink})},
		{Name: "fifo", Tar: with(entry{"ui/fifo", nil, tar.TypeFifo})},
		{Name: "parent directory", Tar: with(entry{"ui/../x", []byte("x"), tar.TypeReg})},
		{Name: "absolute path", Tar: with(entry{"/ui/x", []byte("x"), tar.TypeReg})},
		{Name: "outside ui", Tar: with(entry{ManifestFile, []byte("{}"), tar.TypeReg})},
		{Name: "hidden file", Tar: with(entry{"ui/.htaccess", []byte("x"), tar.TypeReg})},
		{Name: "path of 201 bytes", Tar: with(entry{path201, []byte("x"), tar.TypeReg})},
		{Name: "duplicate", Tar: with(good[1])},
		{Name: "entry under a file", Tar: with(entry{"ui/index.html/x", []byte("x"), tar.TypeReg})},
		{Name: "file where a directory is needed", Tar: with(entry{"ui/css", []byte("x"), tar.TypeReg})},
		{Name: "file where a directory entry needs a directory", Tar: with(entry{"ui/img/icons/", nil, tar.TypeDir}, entry{"ui/img", []byte("x"), tar.TypeReg})},
		{Name: "directory with a size", Tar: slices.Concat(entries, rawHeader("ui/img/", 1, tar.TypeDir), endMarker)},
		// Declared too large and nothing after it: WalkUI refuses the header
		// before it reads any data.
		{Name: "file too large, header only", Tar: slices.Concat(entries, rawHeader("ui/big.bin", MaxUIFile+1, tar.TypeReg))},
		{Name: "sparse file", Tar: slices.Concat(entries, sparseEntry("ui/x.bin", 4096), endMarker)},
		{Name: "truncated archive", Tar: slices.Clone(valid[:cssData+5])},
		{Name: "no end marker", Tar: entries},
		{Name: "one zero block", Tar: slices.Concat(entries, endMarker[:512])},
		{Name: "data after the end marker", Tar: slices.Concat(valid, endMarker[:512])},
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

func TestUIVectors(t *testing.T) {
	cases := uiCases(t)
	for _, c := range cases {
		paths, _, err := walkUIFiles(c.Tar)
		if (err == nil) != c.Valid {
			t.Errorf("%s: %v", c.Name, err)
		}
		if err != nil && err != ErrPackage {
			t.Errorf("%s: got %v, want %v", c.Name, err, ErrPackage)
		}
		if c.Valid && !slices.Equal(paths, c.Files) {
			t.Errorf("%s: files %q, want %q", c.Name, paths, c.Files)
		}
	}
	golden.Check(t, *update, "ui.json", cases)
}

func TestWalkUI(t *testing.T) {
	paths, data, err := walkUIFiles(buildTar(t, goodUI()))
	if err != nil || !slices.Equal(paths, []string{"ui/index.html", "ui/icon.svg", "ui/css/app.css"}) ||
		string(data[2]) != "body{margin:0}\n" {
		t.Fatalf("%q, %q, %v", paths, data, err)
	}
	// fn's own error comes back as it is.
	stop := errors.New("stop")
	calls := 0
	err = WalkUI(bytes.NewReader(buildTar(t, goodUI())), func(PackFile, io.Reader) error { calls++; return stop })
	if err != stop || calls != 1 {
		t.Errorf("fn's error: %v after %d calls", err, calls)
	}
	// Data cut short reaches fn as ErrPackage.
	valid := buildTar(t, goodUI())
	cut := valid[:bytes.Index(valid, goodUI()[3].body)+5]
	var readErr error
	err = WalkUI(bytes.NewReader(cut), func(f PackFile, body io.Reader) error {
		_, readErr = io.ReadAll(body)
		return readErr
	})
	if err != ErrPackage || readErr != ErrPackage {
		t.Errorf("cut data: %v, fn read %v", err, readErr)
	}
}

// bigUITar returns a UI tar of n files named ui/f<i> of size bytes each.
func bigUITar(t testing.TB, n int, size int) []byte {
	t.Helper()
	entries := []entry{{"ui/", nil, tar.TypeDir}}
	body := make([]byte, size)
	for i := range n {
		entries = append(entries, entry{"ui/f" + strconv.Itoa(i), body, tar.TypeReg})
	}
	return buildTar(t, entries)
}

// TestWalkUILimits covers the limits too large for ui.json, on both sides.
func TestWalkUILimits(t *testing.T) {
	cases := []struct {
		name  string
		tar   []byte
		valid bool
	}{
		{"MaxUIFile", bigUITar(t, 1, MaxUIFile), true},
		{"file too large, with its data", bigUITar(t, 1, MaxUIFile+1), false},
		{"MaxUITotal in all", bigUITar(t, MaxUITotal/MaxUIFile, MaxUIFile), true},
		{"total too large", buildTar(t, append(slices.Clone(goodUI()), func() []entry {
			var e []entry
			for i := range MaxUITotal / MaxUIFile {
				e = append(e, entry{"ui/big" + strconv.Itoa(i), make([]byte, MaxUIFile), tar.TypeReg})
			}
			return e
		}()...)), false},
		{"MaxEntries entries", bigUITar(t, MaxEntries-1, 1), true},
		{"too many entries", bigUITar(t, MaxEntries, 1), false},
	}
	for _, c := range cases {
		_, _, err := walkUIFiles(c.tar)
		if (err == nil) != c.valid || (err != nil && err != ErrPackage) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// uiStream serves an archive chunk by chunk, so a test can feed WalkUI more
// than it may read without holding all of it.
type uiStream struct {
	next func() []byte // the next chunk; nil at the end
	buf  []byte
}

func (s *uiStream) Read(p []byte) (int, error) {
	for len(s.buf) == 0 {
		if s.next == nil {
			return 0, io.EOF
		}
		if s.buf = s.next(); s.buf == nil {
			s.next = nil
		}
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

// paxCommentRecord is a PAX comment record of exactly n bytes.
func paxCommentRecord(n int) []byte {
	return []byte(paxRecord("comment", strings.Repeat("c", n-len(" comment=\n")-len(strconv.Itoa(n)))))
}

// TestWalkUIReadLimit streams archives whose files keep every limit but whose
// PAX comments make them long: one of exactly MaxUITotal+16 MiB with its end
// marker passes, one block longer does not.
func TestWalkUIReadLimit(t *testing.T) {
	const limit = MaxUITotal + uiSlack
	const full = 1 << 20 // the longest PAX header archive/tar reads
	perEntry := 512 + full + 512
	n := (limit - len(endMarker)) / perEntry
	last := limit - len(endMarker) - n*perEntry - 1024
	if last <= 0 || last%512 != 0 || last > full {
		t.Fatalf("no last entry fits: %d", last)
	}
	fullRecord := paxCommentRecord(full)
	for _, c := range []struct {
		name  string
		extra int
		valid bool
	}{{"exactly the limit", 0, true}, {"one block more", 512, false}} {
		var chunks [][]byte
		for i := range n + 1 {
			record := fullRecord
			if i == n {
				record = paxCommentRecord(last + c.extra)
			}
			if len(record) != full && len(record) != last+c.extra {
				t.Fatalf("record of %d bytes", len(record))
			}
			chunks = append(chunks, rawHeader("PaxHeaders/0", int64(len(record)), tar.TypeXHeader), record,
				rawHeader("ui/f"+strconv.Itoa(i), 0, tar.TypeReg))
		}
		chunks = append(chunks, endMarker)
		size := 0
		for _, ch := range chunks {
			size += len(ch)
		}
		if size != limit+c.extra {
			t.Fatalf("%s: archive of %d bytes", c.name, size)
		}
		files := 0
		err := WalkUI(&uiStream{next: func() []byte {
			if len(chunks) == 0 {
				return nil
			}
			ch := chunks[0]
			chunks = chunks[1:]
			return ch
		}}, func(PackFile, io.Reader) error { files++; return nil })
		if (err == nil) != c.valid || (err != nil && err != ErrPackage) {
			t.Errorf("%s: %v after %d files", c.name, err, files)
		}
	}
}

func FuzzWalkUI(f *testing.F) {
	for _, c := range uiCases(f) {
		f.Add([]byte(c.Tar))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		paths, data, err := walkUIFiles(b)
		if err != nil {
			return
		}
		// What passes keeps the limits, and the same files written again by
		// tar.Writer pass too.
		var total int64
		entries := make([]entry, len(paths))
		for i, p := range paths {
			if !uiPath(p) || len(data[i]) > MaxUIFile {
				t.Fatalf("file %q of %d bytes", p, len(data[i]))
			}
			total += int64(len(data[i]))
			entries[i] = entry{p, data[i], tar.TypeReg}
		}
		if total > MaxUITotal || len(paths) > MaxEntries {
			t.Fatalf("%d files, %d bytes", len(paths), total)
		}
		again, _, err := walkUIFiles(buildTar(t, entries))
		if err != nil || !slices.Equal(again, paths) {
			t.Fatalf("written again: %q, %v; was %q", again, err, paths)
		}
	})
}
