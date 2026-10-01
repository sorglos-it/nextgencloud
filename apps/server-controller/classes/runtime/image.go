package runtime

import (
	"archive/tar"
	"encoding/json"
	"io"
	"os"
	"path"
	"regexp"

	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// ManifestFile is the member of an image tar that names the image, and
// MaxImageManifest is the most of it that is read. Both belong to the archive
// format of podman and docker, not to NGC: an app package carries the tar as
// podman wrote it (services/<GOARCH>.tar).
const (
	ManifestFile     = "manifest.json"
	MaxImageManifest = 1 << 20
)

// validRef is the reference of an image: a repository and a tag. Nothing else
// becomes the name of an image here, so no reference can turn into an option
// of the engine, carry a space that would make two arguments out of one, or
// hold a second colon that would make the tag part of the repository.
var validRef = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,127}:[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ImageTag answers with the one reference the image tar at tarPath names.
//
// The reference is read out of the tar instead of out of what podman prints
// while it loads the image: an output that has to be parsed is an output whose
// form can change, and this name is what every later call about this container
// uses.
func ImageTag(tarPath string) (string, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return imageTagFrom(f)
}

// imageTagFrom is ImageTag on an archive that is already open.
//
// It takes the tag only when the archive leaves no room for a second answer:
// exactly one manifest member, exactly one entry in it, exactly one RepoTags
// value, and that value a reference. Two manifest members would be the opening
// to aim at - a later member of a tar replaces an earlier one of the same name,
// so podman would keep the image under the second name while this answered
// with the first - which is why the archive is read to its end even after the
// manifest was found.
func imageTagFrom(r io.Reader) (string, error) {
	tr := tar.NewReader(r)
	var manifest []byte
	seen := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		// Only a file of exactly this name at the top of the archive counts.
		// path.Clean leaves a name that climbs out of the archive as it is, so
		// such a name matches nothing here. The type is asked because a member
		// that is no file has no body to read: a link names another member and
		// is not followed. Without this question such a member would still be
		// refused - by its empty body, further down, with a cause that says
		// nothing about why.
		if h.Typeflag != tar.TypeReg || path.Clean(h.Name) != ManifestFile {
			continue
		}
		if seen {
			return "", ErrManifest
		}
		seen = true
		// A manifest that says it is bigger than is allowed is refused before a
		// byte of it is read. This is the one member of an archive from outside
		// that is held in memory here, and the reader hands over no more than
		// the size the member announces, so this is also the whole bound.
		if h.Size > MaxImageManifest {
			return "", ErrManifest
		}
		manifest, err = io.ReadAll(tr)
		if err != nil {
			return "", err
		}
	}
	if !seen {
		return "", ErrManifest
	}

	// The entries are read as members by name, not into a struct of named
	// fields: the manifest of a docker archive carries Config, Layers and more
	// beside RepoTags, those members belong to a foreign format, and naming
	// them here would mean guessing which ones a newer podman writes - a guess
	// that makes an app unloadable the day it is wrong. Everything else that
	// strict decoding holds still holds: the size, a member named twice at any
	// depth, invalid UTF-8, and anything after the one JSON value.
	var entries []map[string]json.RawMessage
	if err := enc.DecodeStrict(manifest, MaxImageManifest, &entries); err != nil {
		return "", err
	}
	if len(entries) != 1 {
		return "", ErrManifest
	}
	raw, ok := entries[0]["RepoTags"]
	if !ok {
		return "", ErrManifest
	}
	var tags []string
	if err := enc.DecodeStrict(raw, MaxImageManifest, &tags); err != nil {
		return "", err
	}
	if len(tags) != 1 || !validRef.MatchString(tags[0]) {
		return "", ErrManifest
	}
	return tags[0], nil
}
