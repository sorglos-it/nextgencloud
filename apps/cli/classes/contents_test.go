package classes

import (
	"encoding/json"
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// tarOf writes a tar at path with the entries given, in the order given.
// A name ending in "/" becomes a directory entry, so a case can lay down
// the shape a packer leaves behind and not only the files.
func tarOf(t testing.TB, path string, names ...string) {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, n := range names {
		h := &tar.Header{Name: n, Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(len(n))}
		if strings.HasSuffix(n, "/") {
			h = &tar.Header{Name: n, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := w.Write([]byte(n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestContentsNamesWhatIsMissing is the assertion of P3: a release archive
// that does not hold what the installation script takes out of it is
// refused here, with the missing name in the sentence. Before this, every
// one of these archives was signed and called "release ok" — measured on
// 17.10.2026, exit status 0 for a web archive without the vhost template
// and for an api archive without ngc-store.
func TestContentsNamesWhatIsMissing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		part  string
		files []string
		want  string // substring of the reason; empty means it must hold
	}{
		{name: "web complete", part: store.ReleaseWeb,
			files: []string{"public/", "public/index.php", "templates/apache-vhost.conf", "assets/app.js"}},
		{name: "web without the vhost template", part: store.ReleaseWeb,
			files: []string{"public/index.php"}, want: "templates/apache-vhost.conf"},
		{name: "web without index.php", part: store.ReleaseWeb,
			files: []string{"templates/apache-vhost.conf"}, want: "public/index.php"},
		{name: "api complete", part: store.ReleaseAPI, files: []string{"ngc-api", "ngc-store"}},
		{name: "api without ngc-store", part: store.ReleaseAPI,
			files: []string{"ngc-api"}, want: "ngc-store"},
		{name: "api in a directory", part: store.ReleaseAPI,
			files: []string{"usr/", "usr/ngc-api", "usr/ngc-store"}, want: "ngc-api, ngc-store"},
		{name: "api with a file too much", part: store.ReleaseAPI,
			files: []string{"ngc-api", "ngc-store", "README.md"}, want: "README.md"},
		{name: "controller complete", part: store.ReleaseController, files: []string{"ngc-controller"}},
		{name: "controller with a directory", part: store.ReleaseController,
			files: []string{"bin/", "bin/ngc-controller"}, want: "ngc-controller"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "x.tar")
			tarOf(t, p, c.files...)
			err := CheckContents(p, c.part)
			if c.want == "" {
				if err != nil {
					t.Fatalf("this archive holds what the script takes out, CheckContents says %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("this archive is wrongly packed and CheckContents passes it")
			}
			if !errors.Is(err, ErrContents) {
				t.Errorf("reason %v, want it to be ErrContents", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("reason %q, want it to name %q", err, c.want)
			}
		})
	}
}

// TestContentsRefusesWhatIsNoArchive separates the three mistakes a packer
// makes that are not a missing file: a file that is no tar, a name no
// installation unpacks, and an entry that is neither a file nor a
// directory. Each is its own sentence, because an exit status does not
// tell them apart (docs: messen.md).
func TestContentsRefusesWhatIsNoArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	noTar := filepath.Join(dir, "no.tar")
	if err := os.WriteFile(noTar, []byte("NGC web test file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckContents(noTar, store.ReleaseWeb); err == nil || !strings.Contains(err.Error(), "no tar") {
		t.Errorf("a file of 17 bytes of text: %v, want it named as no tar", err)
	}

	escape := filepath.Join(dir, "escape.tar")
	tarOf(t, escape, "../etc/passwd", "public/index.php", "templates/apache-vhost.conf")
	err := CheckContents(escape, store.ReleaseWeb)
	if err == nil {
		t.Fatalf("an entry walking out of its directory is passed")
	}
	if !strings.Contains(err.Error(), "which no installation unpacks") {
		t.Errorf("reason %q, want the name refused before anything is compared", err)
	}

	link := filepath.Join(dir, "link.tar")
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	if err := w.WriteHeader(&tar.Header{Name: "ngc-controller", Typeflag: tar.TypeSymlink,
		Linkname: "/bin/sh", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	err = CheckContents(link, store.ReleaseController)
	if err == nil || !strings.Contains(err.Error(), "neither a file nor a directory") {
		t.Errorf("a symlink named ngc-controller: %v, want it refused as no file", err)
	}
}

// TestContentsOfADirectoryJudgesEveryFileOrSaysSo holds the one answer a
// check must never give: a directory in which nothing was looked at must
// not read like a directory in which nothing was wrong.
func TestContentsOfADirectoryJudgesEveryFileOrSaysSo(t *testing.T) {
	t.Parallel()
	empty := t.TempDir()
	seen, err := CheckDirContents(empty)
	if err != nil || len(seen) != 0 {
		t.Fatalf("empty directory: seen %v, err %v", seen, err)
	}
	var out, errOut bytes.Buffer
	if code := runContents(empty, &out, &errOut); code != 1 {
		t.Errorf("exit %d for a directory with no release file, want 1", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout %q for a directory with no release file, want nothing", out.String())
	}
	for _, name := range ReleaseFileNames() {
		if !strings.Contains(errOut.String(), name) {
			t.Errorf("the message does not name %s: %q", name, errOut.String())
		}
	}

	dir := t.TempDir()
	tarOf(t, filepath.Join(dir, "ngc-web-all.tar"), "public/index.php", "templates/apache-vhost.conf")
	tarOf(t, filepath.Join(dir, "ngc-controller-amd64.tar"), "ngc-controller")
	seen, err = CheckDirContents(dir)
	if err != nil {
		t.Fatalf("two archives that hold: %v", err)
	}
	if len(seen) != 2 {
		t.Errorf("judged %v, want both archives", seen)
	}
	out.Reset()
	errOut.Reset()
	if code := runContents(dir, &out, &errOut); code != 0 {
		t.Errorf("exit %d, want 0; %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ngc-web-all.tar") {
		t.Errorf("stdout %q, want it to name what was judged", out.String())
	}

	tarOf(t, filepath.Join(dir, "ngc-api-amd64.tar"), "ngc-api")
	if _, err := CheckDirContents(dir); err == nil {
		t.Errorf("a wrongly packed api archive beside two good ones is passed")
	}
}

// TestPartListNamesEveryPart holds the lists a message is built from
// against releaseParts, the one place the set is written down. A message
// that names the valid values is only worth something while it names all
// of them.
func TestPartListNamesEveryPart(t *testing.T) {
	t.Parallel()
	for _, p := range releaseParts {
		name, ok := ReleaseFileName(p.part, p.arch)
		if !ok || name != p.part+"-"+p.arch+".tar" {
			t.Errorf("ReleaseFileName(%q, %q) = %q, %v", p.part, p.arch, name, ok)
		}
		if !KnownPart(p.part) {
			t.Errorf("%q is a part of releaseParts and KnownPart says no", p.part)
		}
		if !strings.Contains(PartList(), p.part) || !strings.Contains(PartList(), p.arch) {
			t.Errorf("PartList %q misses %s %s", PartList(), p.part, p.arch)
		}
		if _, ok := partContents[p.part]; !ok {
			t.Errorf("no content rule for the part %q", p.part)
		}
	}
	if KnownPart("web") {
		t.Errorf("%q is the wrong name and KnownPart says yes", "web")
	}
	if _, ok := ReleaseFileName(store.ReleaseWeb, "amd64"); ok {
		t.Errorf("ngc-web has no architecture amd64")
	}
	if names := ReleaseFileNames(); len(names) != len(releaseParts) {
		t.Errorf("%d file names for %d parts", len(names), len(releaseParts))
	}
}

// TestCheckNamesTheValidParts is the assertion for the afternoon of
// 16.10.2026: "ngc-store check <dir> web all" answered "no single valid
// entry for web all" and never said that the part is called "ngc-web".
// Three mistakes, three sentences, and each one names the closed set the
// tool already knows.
func TestCheckNamesTheValidParts(t *testing.T) {
	t.Parallel()
	f := newReleaseFixture(t, "ngc-web-all")
	env, err := Release(f.dir, f.keyFile, releaseOpts(1, ""))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.dir, ReleaseFile), b)
	pem, err := PublicKeyPEM(f.pub)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.dir, ReleaseKeyFile), pem)

	if err := Check(f.dir, store.ReleaseWeb, "all"); err != nil {
		t.Fatalf("the release this fixture signed does not check: %v", err)
	}
	cases := []struct {
		name, part, arch string
		want             []string
	}{
		{name: "part without the ngc- prefix", part: "web", arch: "all",
			want: []string{"no single valid entry for web all", "is no part", "ngc-web", "ngc-api", "ngc-controller"}},
		{name: "part with an architecture it has not", part: store.ReleaseWeb, arch: "amd64",
			want: []string{"no single valid entry", "no architecture", "ngc-web (all)"}},
		{name: "part this release does not carry", part: store.ReleaseAPI, arch: "amd64",
			want: []string{"no single valid entry", "this release lists", "ngc-web all"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Check(f.dir, c.part, c.arch)
			if err == nil {
				t.Fatalf("Check passes %s %s", c.part, c.arch)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("reason %q, want it to hold %q", err, w)
				}
			}
		})
	}
}

// TestReleaseNamesTheShapeOfAVersion is the assertion for the second
// afternoon: "release -version 0.0.1.0" answered "store: invalid release",
// the contract's one sentinel for every defect of a release, and the
// four-digit number is this house's own numbering, so it keeps arriving.
// The rule is still the contract's (store.ValidVersion); only the sentence
// is ours.
func TestReleaseNamesTheShapeOfAVersion(t *testing.T) {
	t.Parallel()
	f := newReleaseFixture(t, "ngc-web-all")
	for _, v := range []string{"0.0.1.0", "1.1", "01.1.0", "", "v1.1.0"} {
		opts := releaseOpts(1, "")
		opts.Version = v
		_, err := Release(f.dir, f.keyFile, opts)
		if err == nil {
			t.Fatalf("version %q is signed", v)
		}
		if !errors.Is(err, ErrVersion) {
			t.Errorf("version %q: %v, want ErrVersion", v, err)
		}
		if !strings.Contains(err.Error(), "MAJOR.MINOR.PATCH") || !strings.Contains(err.Error(), v) {
			t.Errorf("version %q: reason %q, want the shape and the value", v, err)
		}
	}
}

// TestNoReleaseFileNamesTheNames: a directory with one wrongly named
// archive looks exactly like an empty one, and the names lived only in
// releaseParts.
func TestNoReleaseFileNamesTheNames(t *testing.T) {
	t.Parallel()
	f := newReleaseFixture(t, "ngc-web")   // one dash short of ngc-web-all.tar
	_, err := Release(f.dir, f.keyFile, releaseOpts(1, ""))
	if !errors.Is(err, ErrNoReleaseFile) {
		t.Fatalf("%v, want ErrNoReleaseFile", err)
	}
	for _, name := range ReleaseFileNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the message does not name %s: %q", name, err)
		}
	}
}
