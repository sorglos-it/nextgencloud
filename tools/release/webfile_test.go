package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// readWeb walks ar the way the controller walks an ngc-web release file
// (install.go, unpack: store.WalkUI, then the one prefix "ui/" stripped) and
// gives what would land under the document root.
func readWeb(ar []byte) (map[string]string, error) {
	out := map[string]string{}
	err := store.WalkUI(bytes.NewReader(ar), func(f store.PackFile, body io.Reader) error {
		b, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		out[f.Path] = string(b)
		return nil
	})
	return out, err
}

// TestTheRealWebFileIsOneTheControllerCanUnpack is the answer to P3 as a
// running case: the file WebFile writes goes through the very reader the
// controller uses, and the names that come out are rooted at "ui/".
func TestTheRealWebFileIsOneTheControllerCanUnpack(t *testing.T) {
	ar, err := WebFileBytes()
	if err != nil {
		t.Fatalf("WebFile: %v", err)
	}
	got, err := readWeb(ar)
	if err != nil {
		t.Fatalf("store.WalkUI refused the file WebFile wrote: %v", err)
	}
	for _, e := range webEntries {
		body, ok := got["ui/"+e.path]
		if !ok {
			t.Errorf("ui/%s did not come through", e.path)
			continue
		}
		if body != e.body {
			t.Errorf("ui/%s came through as %q, want %q", e.path, body, e.body)
		}
	}
	if len(got) != len(webEntries) {
		t.Errorf("%d files came through, want %d: %v", len(got), len(webEntries), got)
	}
}

// TestTheDecoyIsNotAWebFile is the point of P3 measured from the other side:
// the 18 bytes in shared/testdata/release are refused by the same reader. A
// fixture that passes for the thing would measure its own thinness.
//
// The file is found by its place in the repository and a missing one is a
// Fatalf, never a skip (messen.md).
func TestTheDecoyIsNotAWebFile(t *testing.T) {
	path := filepath.Join("..", "..", "shared", "testdata", "release", "ngc-web-all.tar")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the decoy is not there: %v", err)
	}
	if len(b) != 18 {
		t.Fatalf("the decoy is %d bytes, not the 18 this case was written against: %q", len(b), b)
	}
	if _, err := readWeb(b); err == nil {
		t.Error("store.WalkUI accepted 18 bytes of text as a web release file")
	}
	// Why replacing it is not one file: release.json names this size and this
	// checksum, and release.json is signed. Measured here so the sentence in
	// webfile.go cannot go stale without something going red.
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != "b15225b711e0700e6876d784c6ef561375ab17948a2bc89a49f8deccfb50f3e0" {
		t.Errorf("the decoy's checksum changed to %s; release.json in shared/testdata names the old one", hex.EncodeToString(sum[:]))
	}
}

// TestTheWebFileOnDiskIsTheOneWebFileWrites holds the checked-in file against
// the generator, so the two can never drift: a changed generator and an
// unchanged file go red here.
func TestTheWebFileOnDiskIsTheOneWebFileWrites(t *testing.T) {
	want, err := WebFileBytes()
	if err != nil {
		t.Fatalf("WebFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("testdata", "ngc-web-all.tar"))
	if err != nil {
		t.Fatalf("the real web release file is not there: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("testdata/ngc-web-all.tar is %d bytes, WebFile writes %d; regenerate it", len(got), len(want))
	}
}

// TestTheWebFileIsReproducible is the property a release file needs: the same
// input gives the same bytes, so two builds of one commit are one file. The
// dialect has no clock in it (mtime is the string "00000000000"), and this is
// the case that keeps it that way.
func TestTheWebFileIsReproducible(t *testing.T) {
	a, err := WebFileBytes()
	if err != nil {
		t.Fatalf("WebFile: %v", err)
	}
	b, err := WebFileBytes()
	if err != nil {
		t.Fatalf("WebFile: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two runs of WebFile wrote different bytes")
	}
}
