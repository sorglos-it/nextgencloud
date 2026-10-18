package release

import (
	"bytes"
	"io"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// WHAT AN ngc-web RELEASE FILE IS, measured at the programs and not at the
// decoy in shared/testdata.
//
// Two different things carry the extension ".tar" today, and the code says so
// plainly:
//
//   - ngc-web: apps/server-controller/classes/update/install.go, unpack (line
//     881) hands the file to store.WalkUI and strips the one prefix "ui/". So
//     an ngc-web release file is a tar in the ONE dialect shared/store/ui.go
//     describes at its top: names "ui/<ValidPath>", mode "0000644" (a
//     directory "0000755"), mtime "00000000000", ustar magic, the end marker.
//   - ngc-api and ngc-controller: installPart (line 527) calls copyInto,
//     chmod 0755 and os.Rename onto BinDir/ngc-api. It unpacks NOTHING. So
//     that release file is the naked program, and ".tar" is a misleading name
//     (docs/release-weg.md, D3).
//
// shared/testdata/release/ngc-web-all.tar is 18 bytes of ASCII text
// ("NGC web test file"), and nothing in the repository ever unpacks it: the
// only reader is apps/cli Check, which treats it as an opaque blob and
// compares its SHA-256 against release.json. Measured: release.json names
// size 18 and sha256 b15225b7…f3e0, which is exactly that text - so the decoy
// is not merely thin, it is NAILED DOWN by a signature. Replacing it is
// therefore not one file but three (the file, release.json, the signature
// over it), all of them under frozen shared/. It stands as BLOCKED in the
// report of this branch.
//
// WebFile below writes the real thing with the contract's own writer, so
// nothing here reimplements the dialect. The file it produces lies in
// tools/release/testdata/ - the area this session may write - and the cases
// beside it read it back the way the controller reads it.

// webEntries are the files of the smallest ngc-web release file that is a
// real surface: an entry page and one asset below a directory, so a nested
// path is exercised and not only the flat case.
var webEntries = []struct {
	path string // without the "ui/" prefix
	body string
}{
	{"index.html", "<!doctype html>\n<title>NGC</title>\n"},
	{"assets/app.js", "export const ngc = true;\n"},
}

// WebFile writes an ngc-web release file to w: a tar store.WalkUI accepts,
// rooted at "ui/", built by store.NewUIWriter.
//
// It uses the writer of the contract on purpose. A tar assembled here by hand
// would be this file's own idea of the dialect, and the measurement would
// then compare two of our guesses instead of the contract against itself.
func WebFile(w io.Writer) error {
	u := store.NewUIWriter(w)
	// No directory entries. store.PackageUI, the contract's OWN producer of
	// these archives, writes none either ("directory entries are not
	// written", shared/store/ui.go:383) and WalkUI implies a parent from the
	// path. Writing them would make this file a shape the contract never
	// produces. Measured on the way: UIWriter.Dir takes "ui", not "ui/" - it
	// appends the slash itself (ui.go:315, add) - and the first version of
	// this file got that wrong and panicked with ErrPackage.
	for _, e := range webEntries {
		if err := u.File("ui/"+e.path, int64(len(e.body)), strings.NewReader(e.body)); err != nil {
			return err
		}
	}
	return u.Close()
}

// WebFileBytes is WebFile into memory.
func WebFileBytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := WebFile(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
