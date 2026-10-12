package classes

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// demoManifest is the manifest of the app writeApp lays down.
const demoManifest = `{"id":"org.sorglos.demo","name":"Demo","version":"1.2.3",` +
	`"publisher":"Sorglos IT","icon":"icon.svg","ui":{"entry":"index.html"},` +
	`"services":[{"name":"demo","command":["/usr/local/bin/demo"]}]}`

// y1980 is the modification time other packers write when they want a fixed
// one. Here it stands on the disk, to show it does not reach the archive.
var y1980 = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// writeApp lays down an app directory: manifest.json, three files below ui/
// (one of them in a subdirectory) and one image under services/.
func writeApp(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "manifest.json"), []byte(demoManifest))
	write(t, filepath.Join(dir, "ui", "index.html"), []byte("<!doctype html><title>Demo</title>"))
	write(t, filepath.Join(dir, "ui", "icon.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	write(t, filepath.Join(dir, "ui", "css", "app.css"), []byte("body{margin:0}\n"))
	write(t, filepath.Join(dir, "services", "amd64.tar"), []byte("podman image"))
	return dir
}

func write(t testing.TB, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPackWritesAReadablePackage(t *testing.T) {
	dir := writeApp(t)
	out := filepath.Join(t.TempDir(), "app.ngc")
	m, sum, err := Pack(dir, out)
	if err != nil || m.ID != "org.sorglos.demo" || len(sum) != 32 {
		t.Fatalf("pack: %v %+v", err, m)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	// The package must read exactly as store.Walk reads one, and its ui-tar
	// must match what store.PackageUI computes.
	var ui bytes.Buffer
	m2, hash, err := store.PackageUI(f, &ui)
	if err != nil || m2.Version != m.Version || len(hash) != 32 {
		t.Fatalf("package ui: %v", err)
	}
	// The ui-tar holds the files below ui/ sorted by path, and nothing else.
	var got []string
	err = store.WalkUI(bytes.NewReader(ui.Bytes()), func(f store.PackFile, body io.Reader) error {
		got = append(got, f.Path)
		_, err := io.Copy(io.Discard, body)
		return err
	})
	if err != nil {
		t.Fatalf("walk ui: %v", err)
	}
	want := "ui/css/app.css,ui/icon.svg,ui/index.html"
	if strings.Join(got, ",") != want {
		t.Errorf("ui files %q, want %q", got, want)
	}
	// The SHA-256 Pack returns is the one of the file on disk.
	if !bytes.Equal(sum, sha256Of(read(t, out))) {
		t.Error("the returned sum is not the one of the file")
	}
}

// TestPackIsReproducible: the same sources packed twice give the same bytes,
// so the SHA-256 a catalog lists says what is in the package, not when
// someone packed it.
func TestPackIsReproducible(t *testing.T) {
	dir := writeApp(t)
	outDir := t.TempDir()
	first := filepath.Join(outDir, "one.ngc")
	second := filepath.Join(outDir, "two.ngc")
	_, sumA, err := Pack(dir, first)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	// Every file gets another modification time and another mode on disk: if
	// any of that reached the archive, the two packages would differ.
	for _, p := range []string{"manifest.json", "ui/index.html", "ui/css/app.css", "services/amd64.tar"} {
		path := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, y1980, y1980); err != nil {
			t.Fatal(err)
		}
	}
	_, sumB, err := Pack(dir, second)
	if err != nil {
		t.Fatalf("pack again: %v", err)
	}
	a, b := read(t, first), read(t, second)
	if !bytes.Equal(a, b) || !bytes.Equal(sumA, sumB) {
		t.Fatalf("two runs differ: %d and %d bytes", len(a), len(b))
	}
	// The intermediate check: the preparation did reach the disk, so the two
	// equal packages are not two runs over an unchanged directory.
	info, err := os.Stat(filepath.Join(dir, "ui", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(y1980) {
		t.Fatalf("the modification time on disk is %v, so nothing was changed between the runs", info.ModTime())
	}
}

func TestPackRefusesUnsafeNames(t *testing.T) {
	dir := writeApp(t)
	// A hidden file, a name with "..", a symbolic link and a path outside ui/
	// are refused before anything is written.
	write(t, filepath.Join(dir, "ui", ".hidden"), []byte("x"))
	out := filepath.Join(t.TempDir(), "app.ngc")
	if _, _, err := Pack(dir, out); err == nil {
		t.Fatal("a hidden file was packed")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a refused app left a file behind")
	}
}

// TestPackRefusesWhatAHostileDirectoryTries walks one hostile change at a
// time. Each case first proves the way to the refusal was open — the very
// same directory without that change packs — so a green case cannot be the
// earlier wall of some other rule.
func TestPackRefusesWhatAHostileDirectoryTries(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, dir string)
	}{
		{"a hidden file below ui/", func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "ui", ".env"), []byte("SECRET=1"))
		}},
		{"a name store.ValidPath refuses", func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "ui", "a b.css"), []byte("x"))
		}},
		{"a link out of the app", func(t *testing.T, dir string) {
			write(t, filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("password"))
			link(t, filepath.Join(dir, "ui", "escape.html"), filepath.Join(filepath.Dir(dir), "secret.txt"))
		}},
		{"a link to a directory outside the app", func(t *testing.T, dir string) {
			link(t, filepath.Join(dir, "ui", "out"), filepath.Dir(dir))
		}},
		{"a file over store.MaxUIFile", func(t *testing.T, dir string) {
			truncate(t, filepath.Join(dir, "ui", "big.bin"), store.MaxUIFile+1)
		}},
		{"files over store.MaxUITotal together", func(t *testing.T, dir string) {
			for i := range store.MaxUITotal/store.MaxUIFile + 1 {
				truncate(t, filepath.Join(dir, "ui", "part"+strconv.Itoa(i)+".bin"), store.MaxUIFile)
			}
		}},
		{"more entries than store.MaxEntries", func(t *testing.T, dir string) {
			for i := range store.MaxEntries {
				write(t, filepath.Join(dir, "ui", "f"+strconv.Itoa(i)), []byte{0})
			}
		}},
		{"an image that is a directory", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "services", "arm64.tar"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Two intermediate checks on the unchanged directory, so a refusal
			// below cannot be a wall some earlier rule puts up: the very same
			// app packs, and with an output path that cannot be created it
			// gets as far as creating the file and fails with that path's
			// error, which is never ErrPack.
			openDir := writeApp(t)
			if _, _, err := Pack(openDir, filepath.Join(t.TempDir(), "open.ngc")); err != nil {
				t.Fatalf("the way was not open before the change: %v", err)
			}
			unwritable := filepath.Join(t.TempDir(), "no-such-directory", "out.ngc")
			if _, _, err := Pack(openDir, unwritable); err == nil || err == ErrPack {
				t.Fatalf("the way to the output file was not open: %v", err)
			}
			dir := writeApp(t)
			c.build(t, dir)
			// The hostile directory is refused with ErrPack although the
			// output file could not be created at all: so it was refused
			// before the packer wrote, read or even opened anything.
			if _, _, err := Pack(dir, filepath.Join(t.TempDir(), "no-such-directory", "out.ngc")); err != ErrPack {
				t.Fatalf("not refused before the output file was touched: %v", err)
			}
			out := filepath.Join(t.TempDir(), "hostile.ngc")
			if _, _, err := Pack(dir, out); err == nil {
				t.Fatal("packed it")
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("a refused app left a file behind")
			}
		})
	}
}

// link makes path a link to target. Windows gives an unprivileged process no
// symbolic link, so there the test falls back to a directory junction: Go
// reports it as a non-regular entry just the same, which is what collect has
// to refuse. Only if neither works does the case skip, and then it says so.
func link(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Symlink(target, path); err == nil {
		return
	}
	info, statErr := os.Stat(target)
	if statErr == nil && info.IsDir() && runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput(); err == nil {
			return
		} else {
			t.Logf("mklink: %v %s", err, out)
		}
	}
	t.Skipf("LOUD SKIP, NO RESULT: this system lets the test make no link at %s", path)
}

// truncate makes a file of size bytes without writing them: on NTFS and on
// ext4 this is a sparse file, so the size limits can be driven without
// spending minutes on disk.
func truncate(t *testing.T, path string, size int64) {
	t.Helper()
	write(t, path, nil)
	if err := os.Truncate(path, size); err != nil {
		t.Fatal(err)
	}
}

// TestValidEntryRefusesNamesNoDirectoryCanHold covers the names a directory
// on this system cannot carry but a tar entry could: an absolute path, a ".."
// step and a control character. Those are the names that would let a package
// write outside the app's folder when it is unpacked.
func TestValidEntryRefusesNamesNoDirectoryCanHold(t *testing.T) {
	for _, name := range []string{"manifest.json", "ui/index.html", "ui/css/app.css",
		"services/amd64.tar", "services/arm64.tar"} {
		if !validEntry(name) {
			t.Errorf("%q refused", name)
		}
	}
	for _, name := range []string{"", "/etc/passwd", "ui//etc/passwd", "ui/../../etc/passwd",
		"ui/..", "ui/../x", "ui/a/../../b", "ui/\x01", "ui/\x00x", "ui/.hidden", "../manifest.json",
		"services/x86.tar", "services/amd64.tar/x", "etc/passwd", "ui/", "ui", "manifest.json/x"} {
		if validEntry(name) {
			t.Errorf("%q accepted", name)
		}
	}
}

func TestPackRefusesADirectoryThatIsNoApp(t *testing.T) {
	out := filepath.Join(t.TempDir(), "app.ngc")
	if _, _, err := Pack(t.TempDir(), out); err != ErrPack {
		t.Errorf("without a manifest: %v", err)
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "manifest.json"), []byte(`{"id":"org.sorglos.demo"}`))
	if _, _, err := Pack(dir, out); err != store.ErrManifest {
		t.Errorf("with a broken manifest: %v", err)
	}
	// A manifest that promises a ui entry the directory does not hold: that is
	// caught by store.Walk reading the package back, after its last file.
	dir = t.TempDir()
	write(t, filepath.Join(dir, "manifest.json"), []byte(demoManifest))
	write(t, filepath.Join(dir, "ui", "other.html"), []byte("x"))
	write(t, filepath.Join(dir, "services", "amd64.tar"), []byte("image"))
	if _, _, err := Pack(dir, out); err != store.ErrPackage {
		t.Errorf("without the ui entry: %v", err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a refused app left a file behind")
	}
}

func TestPackMatchesTheVectors(t *testing.T) {
	// packui.json holds the packages ngc-store pack writes, with their ui-tar
	// and its SHA-256: whatever this packer writes must read back into exactly
	// those bytes, or the shell, the API and the controller would judge an app
	// differently.
	raw, err := os.ReadFile("../../../shared/testdata/packui.json")
	if err != nil {
		t.Fatalf("vectors: %v", err)
	}
	// packui.json is a list of {name, package, ui, hash}, each of the last
	// three base64url (enc.Bytes); shared/store/ui_test.go writes it.
	var cases []struct {
		Name    string    `json:"name"`
		Package enc.Bytes `json:"package"`
		UI      enc.Bytes `json:"ui"`
		Hash    enc.Bytes `json:"hash"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("vectors: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("packui.json holds no case")
	}
	for _, c := range cases {
		var ui bytes.Buffer
		_, hash, err := store.PackageUI(bytes.NewReader(c.Package), &ui)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !bytes.Equal(ui.Bytes(), c.UI) || !bytes.Equal(hash, c.Hash) {
			t.Fatalf("%s: ui-tar or hash differs from the vector", c.Name)
		}
	}
}

// TestPackWritesTheHeadersOfTheVectors: a package of this packer carries the
// headers the packages in packui.json carry — USTAR, mode 0644, owner 0, time
// 0 — in the order manifest, ui files, images, and no entry besides them.
func TestPackWritesTheHeadersOfTheVectors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "app.ngc")
	if _, _, err := Pack(writeApp(t), out); err != nil {
		t.Fatalf("pack: %v", err)
	}
	var names []string
	tr := tar.NewReader(bytes.NewReader(read(t, out)))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if h.Format != tar.FormatUSTAR || h.Typeflag != tar.TypeReg || h.Mode != 0o644 ||
			h.Uid != 0 || h.Gid != 0 || h.ModTime.Unix() != 0 {
			t.Errorf("%s: %+v", h.Name, h)
		}
		names = append(names, h.Name)
	}
	want := "manifest.json,ui/css/app.css,ui/icon.svg,ui/index.html,services/amd64.tar"
	if strings.Join(names, ",") != want {
		t.Errorf("entries %q, want %q", names, want)
	}
}

// longUIPath is a ui file name of 150 bytes, as packui.json carries one: Go
// archive/tar splits it over the prefix and name fields of a USTAR header,
// and store.PackageUI then gives it a PAX header in the ui-tar.
var longUIPath = strings.Repeat("d", 99) + "/" + strings.Repeat("f", 43) + ".css"

func TestPackWritesALongNameAsUSTAR(t *testing.T) {
	dir := writeApp(t)
	write(t, filepath.Join(dir, "ui", filepath.FromSlash(longUIPath)), []byte("p{margin:0}\n"))
	out := filepath.Join(t.TempDir(), "app.ngc")
	if _, _, err := Pack(dir, out); err != nil {
		t.Fatalf("pack: %v", err)
	}
	long := 0
	tr := tar.NewReader(bytes.NewReader(read(t, out)))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil || h.Format != tar.FormatUSTAR {
			t.Fatalf("%+v, %v", h, err)
		}
		if h.Name == "ui/"+longUIPath {
			long++
		}
	}
	if long != 1 {
		t.Fatalf("the long name is in the package %d times", long)
	}
	// The ui-tar of such a package is still one store.WalkUI reads.
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ui bytes.Buffer
	if _, _, err := store.PackageUI(f, &ui); err != nil {
		t.Fatalf("package ui: %v", err)
	}
	if err := store.WalkUI(bytes.NewReader(ui.Bytes()), func(store.PackFile, io.Reader) error { return nil }); err != nil {
		t.Fatalf("walk ui: %v", err)
	}
}

// TestPackPacksAnAppWithoutAUI: an app that is only a service has no ui/
// directory, and both its images are in the package.
func TestPackPacksAnAppWithoutAUI(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "manifest.json"), []byte(`{"id":"org.sorglos.backup","name":"Sicherung",`+
		`"version":"1.0.0","publisher":"Sorglos IT","services":[{"name":"backup","command":["/usr/bin/backup"]}]}`))
	write(t, filepath.Join(dir, "services", "amd64.tar"), []byte("amd64 image"))
	write(t, filepath.Join(dir, "services", "arm64.tar"), []byte("arm64 image"))
	out := filepath.Join(t.TempDir(), "app.ngc")
	m, _, err := Pack(dir, out)
	if err != nil || m.ID != "org.sorglos.backup" {
		t.Fatalf("pack: %v %+v", err, m)
	}
	var names []string
	tr := tar.NewReader(bytes.NewReader(read(t, out)))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		names = append(names, h.Name)
	}
	want := "manifest.json,services/amd64.tar,services/arm64.tar"
	if strings.Join(names, ",") != want {
		t.Errorf("entries %q, want %q", names, want)
	}
}

// TestPackRefusesANameNoUSTARHeaderHolds: store.ValidPath allows a path of up
// to 200 bytes, but a USTAR header splits a name only at a "/" with at most
// 100 bytes behind it. Such a name is refused rather than written as a PAX
// entry, because packui.json holds ngc-store's packages as USTAR throughout.
// Whoever packs the app renames the file.
func TestPackRefusesANameNoUSTARHeaderHolds(t *testing.T) {
	dir := writeApp(t)
	name := strings.Repeat("g", 120) + ".css"
	if !store.ValidPath(name) {
		t.Fatalf("the way was not open: store.ValidPath already refuses %d bytes", len(name))
	}
	write(t, filepath.Join(dir, "ui", name), []byte("p{margin:0}\n"))
	out := filepath.Join(t.TempDir(), "app.ngc")
	if _, _, err := Pack(dir, out); err == nil {
		t.Fatal("packed a name no USTAR header holds")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a refused app left a file behind")
	}
}
