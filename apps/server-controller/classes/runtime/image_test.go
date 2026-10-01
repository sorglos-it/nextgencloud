package runtime

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// entry is one file in an image tar.
type entry struct {
	name string
	body string
	kind byte
}

// archive is what an image tar of podman looks like in the small: the layers
// first, the manifest last. The order is the one podman writes.
func archive(manifest string) []entry {
	return []entry{
		{name: "c.json", body: `{}`},
		{name: "l/layer.tar", body: "layer"},
		{name: ManifestFile, body: manifest},
	}
}

func tarBytes(t testing.TB, es []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range es {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: kind, Mode: 0o644, Size: int64(len(e.body))}
		if kind == tar.TypeSymlink {
			h.Linkname = e.body
			h.Size = 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTar(t testing.TB, path string, es []entry) {
	t.Helper()
	if err := os.WriteFile(path, tarBytes(t, es), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A tar as podman writes it carries members beside RepoTags. Reading it must
// not fail over them: the members of a foreign format are not ours to name,
// and a tar of a newer podman would otherwise make the app unloadable.
func TestImageTagTakesATarAsPodmanWritesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amd64.tar")
	writeTar(t, path, archive(`[{"Config":"c.json","RepoTags":["ngc/terminal:1.0.0"],`+
		`"Layers":["l/layer.tar"],"LayerSources":{"sha256:aa":{"mediaType":"x","size":1,"digest":"sha256:aa"}}}]`))
	ref, err := ImageTag(path)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "ngc/terminal:1.0.0" {
		t.Fatalf("ref = %q", ref)
	}
}

// A refusal of this package answers with ErrManifest, a refusal of strict
// decoding with its own error. The difference is the cause an admin gets to
// see: "this tar names no single image" is one sentence, a syntax error of JSON
// is another, and the caller maps them to different answers.
type refusal struct {
	manifest string
	ours     bool // ErrManifest, not an error of the decoder
}

func TestImageTagRefusesMoreThanOneRepoTag(t *testing.T) {
	cases := map[string]refusal{
		"two tags in one entry":      {`[{"RepoTags":["ngc/a:1.0.0","ngc/b:1.0.0"]}]`, true},
		"no tag at all":              {`[{"Config":"c.json","Layers":["l/layer.tar"]}]`, true},
		"an empty tag list":          {`[{"RepoTags":[]}]`, true},
		"two entries":                {`[{"RepoTags":["ngc/a:1.0.0"]},{"RepoTags":["ngc/b:1.0.0"]}]`, true},
		"no entry":                   {`[]`, true},
		"a tag that is empty":        {`[{"RepoTags":[""]}]`, true},
		"a tag without a tag":        {`[{"RepoTags":["ngc/a"]}]`, true},
		"a tag with a space":         {`[{"RepoTags":["ngc/a:1.0.0 --all"]}]`, true},
		"a tag with a dash in front": {`[{"RepoTags":["-v:1.0.0"]}]`, true},
		"a tag in capitals":          {`[{"RepoTags":["NGC/a:1.0.0"]}]`, true},
		"not a list":                 {`{"RepoTags":["ngc/a:1.0.0"]}`, false},
		"the same member twice":      {`[{"RepoTags":["ngc/a:1.0.0"],"RepoTags":["ngc/a:1.0.0"]}]`, false},
		"something after the list":   {`[{"RepoTags":["ngc/a:1.0.0"]}] {}`, false},
		"a tag that is no string":    {`[{"RepoTags":[1]}]`, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "amd64.tar")
			writeTar(t, path, archive(c.manifest))
			ref, err := ImageTag(path)
			if err == nil {
				t.Fatalf("took %q as %q", c.manifest, ref)
			}
			if got := errors.Is(err, ErrManifest); got != c.ours {
				t.Fatalf("err = %v, ErrManifest = %v, want %v", err, got, c.ours)
			}
		})
	}
}

// The tar itself is read strictly as well: the manifest must be there exactly
// once and be a file. Two of them would let podman keep the image under another
// name than the one this engine answers with, because a later member of a tar
// replaces an earlier one of the same name.
func TestImageTagRefusesATarThatNamesTheManifestTwiceOrNotAtAll(t *testing.T) {
	good := `[{"RepoTags":["ngc/a:1.0.0"]}]`
	cases := map[string]struct {
		es   []entry
		ours bool
	}{
		"no manifest": {[]entry{{name: "c.json", body: `{}`}}, true},
		"twice": {[]entry{
			{name: ManifestFile, body: good},
			{name: ManifestFile, body: `[{"RepoTags":["ngc/b:1.0.0"]}]`},
		}, true},
		"as a link":     {[]entry{{name: ManifestFile, body: "c.json", kind: tar.TypeSymlink}}, true},
		"as a folder":   {[]entry{{name: ManifestFile, kind: tar.TypeDir}}, true},
		"in a folder":   {[]entry{{name: "sub/" + ManifestFile, body: good}}, true},
		"above the tar": {[]entry{{name: "../" + ManifestFile, body: good}}, true},
		"empty":         {[]entry{{name: ManifestFile, body: ""}}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "amd64.tar")
			writeTar(t, path, c.es)
			ref, err := ImageTag(path)
			if err == nil {
				t.Fatalf("took it as %q", ref)
			}
			if got := errors.Is(err, ErrManifest); got != c.ours {
				t.Fatalf("err = %v, ErrManifest = %v, want %v", err, got, c.ours)
			}
		})
	}
}

// A manifest written to fill the memory is refused by what it says about its
// own size, before a byte of it is read. That it is this refusal and not the
// one of strict decoding is the whole point: nothing of such an archive is ever
// held here.
func TestImageTagRefusesAManifestThatIsTooBig(t *testing.T) {
	big := `[{"RepoTags":["ngc/a:1.0.0"],"Pad":"` + strings.Repeat("x", 4<<20) + `"}]`
	path := filepath.Join(t.TempDir(), "amd64.tar")
	writeTar(t, path, archive(big))
	_, err := ImageTag(path)
	if err == nil {
		t.Fatal("the manifest was decoded")
	}
	if !errors.Is(err, ErrManifest) {
		t.Fatalf("err = %v, want ErrManifest - the size was read before it was refused", err)
	}
}

func TestImageTagRefusesWhatIsNoTar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amd64.tar")
	if err := os.WriteFile(path, []byte("this is not a tar"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImageTag(path); err == nil {
		t.Fatal("it was read as a tar")
	}
	if _, err := ImageTag(filepath.Join(t.TempDir(), "gone.tar")); err == nil {
		t.Fatal("a file that is not there was read")
	}
}

func FuzzImageTag(f *testing.F) {
	f.Add([]byte("")) // no valid tar
	f.Add(tarBytes(f, archive(`[{"RepoTags":["ngc/a:1.0.0"]}]`)))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = imageTagFrom(bytes.NewReader(b)) })
}
