package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// webRef is the ref the real tree is measured at. A name and not a commit on
// purpose: this case is meant to follow the branch while it is still moving.
// It SKIPS when the ref is absent, because apps/web is empty on main - a skip
// says "not measured here", a green would lie.
const webRef = "origin/web"

// TestRealArchiveIsClosed walks the graph over an archive of the actual web
// interface. It is the only case that can find a file apps/web gained without
// anybody adding it anywhere, and it is why the fixture cases are not enough.
//
// Measured on origin/web (fe5a0e4), 02.10.2026, by running this very code in a
// throwaway module: 67 files, 59 reached, 0 missing, 0 bare, 8 orphans - the
// seven of extra-web.txt and the one of open-web.txt.
func TestRealArchiveIsClosed(t *testing.T) {
	needTools(t)
	root := repoRoot(t)
	cmd := exec.Command("git", "cat-file", "-e", webRef+":apps/web/VERSION")
	cmd.Dir = root
	cmd.Env = gitEnv(t)
	if err := cmd.Run(); err != nil {
		t.Skipf("%s carries no apps/web; nothing measured here", webRef)
	}

	out, log, err := pack(t, root, webRef, "1")
	if err != nil {
		t.Fatalf("pack-release.sh on %s: %v\n%s", webRef, err, log)
	}
	dir := unpack(t, filepath.Join(out, "ngc-web-all.tar"))
	got, err := Closure(dir, lines(t, "entrypoints-web.txt"))
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	for _, m := range got.Missing {
		t.Errorf("%s refers to %s, which the archive does not carry", m.From, m.To)
	}
	for _, b := range got.Bare {
		t.Errorf("%s imports %q, which no import map resolves", b.From, b.To)
	}

	extra := lines(t, "extra-web.txt")
	open := lines(t, "open-web.txt")
	for _, rel := range extra {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("extra-web.txt names %s, which %s does not carry", rel, webRef)
		}
		if slices.Contains(got.Reached, rel) {
			t.Errorf("extra-web.txt names %s, but a reference reaches it; take the line out", rel)
		}
	}

	// EVERY file of the archive is reached, explained, or a KNOWN open
	// question. The third bucket is REPORTED and does not fail: tools/check.sh
	// runs "go test ./...", so failing here would close the commit gate of
	// every task over a file nobody has decided about yet. It does fail for an
	// UNKNOWN orphan - that one nobody has looked at - and for an open question
	// that became reachable, because then the question is answered and the line
	// belongs gone.
	var orphans, known []string
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, dir+string(filepath.Separator)))
		switch {
		case slices.Contains(got.Reached, rel), slices.Contains(extra, rel):
		case slices.Contains(open, rel):
			known = append(known, rel)
		default:
			orphans = append(orphans, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	for _, rel := range open {
		if slices.Contains(got.Reached, rel) {
			t.Errorf("open-web.txt still asks about %s, but a reference reaches it now; answer the question and take the line out", rel)
		}
	}
	if len(known) != 0 {
		t.Logf("still open, see tools/release/open-web.txt:\n  %s", strings.Join(known, "\n  "))
	}
	if len(orphans) != 0 {
		t.Errorf("these files are in the archive, no reference reaches them, and neither extra-web.txt nor open-web.txt says anything about them:\n  %s",
			strings.Join(orphans, "\n  "))
	}
}
