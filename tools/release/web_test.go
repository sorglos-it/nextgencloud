package release

import (
	"bytes"
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

// refPresent answers whether webRef carries apps/web, and it keeps two causes
// apart that the first draft of this case ran together - at the price of the
// only measurement there is.
//
// THE TWO CAUSES. "The thing is not there" is a skip. "The tool refused" is an
// ERROR and has to go red: a worktree outside its repository makes git leave
// with "fatal: detected dubious ownership" unless safe.directory is set for the
// call, and a case that reads that as "the ref is absent" turns itself off in
// the very moment somebody wants to know whether it still measures - green, not
// one line of log, and the one case that can catch a file apps/web gained
// silently stops looking.
//
// WHY THE EXIT CODE ALONE CANNOT DO IT, measured in this worktree:
//
//	git cat-file -e main:apps/web/nope          -> 128  "path ... does not exist in 'main'"
//	git cat-file -e no-such-branch:.../VERSION  -> 128  "invalid object name"
//	git cat-file -e <any> with no safe.directory -> 128 "detected dubious ownership"
//
// All three are 128. So the split is made by ASKING IN AN ORDER where each step
// can only fail for one reason, instead of reading one code several ways.
//
// A FOURTH STATE, and it is the reason this function has four questions and not
// three: a BROKEN repository - the ref file is there, the commit object it names
// is not. Built on purpose in a throwaway repository (commit, then delete
// .git/objects/<xx>/<rest>) and measured:
//
//	rev-parse --git-dir                -> 0    ".git"            repository opens
//	rev-parse --verify -q main^{commit} -> 1   (no output)        LIKE AN UNKNOWN REF
//	rev-parse --verify    main^{commit} -> 128 "Needed a single revision"
//	rev-parse --verify    no-such^{commit} -> 128 "Needed a single revision"
//
// So dropping the -q does NOT separate them: an unknown ref and a broken
// repository give the same code AND the same sentence. What does separate them is
// asking about the NAME instead of the object, which touches no object at all:
//
//	rev-parse --symbolic-full-name main       -> 0   "refs/heads/main"  (even broken)
//	rev-parse --symbolic-full-name no-such    -> 128 "unknown revision"
//	rev-parse --symbolic-full-name <raw sha>  -> 0   (empty output, existing or not)
//
// Hence:
//
//  1. "rev-parse --git-dir" - the guard pack-release.sh checks first. It touches
//     no ref, so a failure here is git REFUSING. RED.
//  2. "rev-parse --symbolic-full-name <ref>" - 128 means this name is not known
//     here. That is an answer: SKIP. A non-empty answer is a ref that exists; an
//     empty one means git read the argument as a raw object id.
//  3. "rev-parse --verify <ref>^{commit}" - after 2 this can only fail because
//     the object is missing. For a REF THAT EXISTS that is a broken repository:
//     RED. For a raw object id nothing ever claimed it is here, so: SKIP.
//  4. "cat-file -e <ref>:apps/web/VERSION" - readable repository, resolvable
//     commit, so a failure means this commit carries no apps/web (it is nearly
//     empty on main). SKIP.
func refPresent(t *testing.T, root string) bool {
	t.Helper()
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = gitEnv(t)
		return cmd.CombinedOutput()
	}
	// 1. Does git work here at all?
	if out, err := run("rev-parse", "--git-dir"); err != nil {
		t.Fatalf("git refused to work in %s, so NOTHING here is measured - and that is an\n"+
			"error, not a skip. A worktree outside its repository needs the guard, for the\n"+
			"call only:\n"+
			"  export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=\"$PWD\"\n"+
			"Never set safe.directory globally. git said: %v\n%s", root, err, out)
	}
	// 2. Is the NAME known here? This asks the ref store, not the objects.
	name, err := run("rev-parse", "--symbolic-full-name", webRef)
	if err != nil {
		return false
	}
	isRef := len(bytes.TrimSpace(name)) > 0
	// 3. Can the commit be resolved? After 2 the only reason it cannot is a
	// missing object.
	if out, err := run("rev-parse", "--verify", webRef+"^{commit}"); err != nil {
		if !isRef {
			// webRef was a raw object id, and nothing ever promised this clone
			// holds it. Not measured here.
			return false
		}
		t.Fatalf("%s is a ref that exists (%s) but its commit cannot be resolved, so this\n"+
			"repository is BROKEN - and that is an error, not a skip. Nothing here is\n"+
			"measured. git said: %v\n%s", webRef, bytes.TrimSpace(name), err, out)
	}
	// 4. Does that commit carry the web interface?
	_, err = run("cat-file", "-e", webRef+":apps/web/VERSION")
	return err == nil
}

// TestRealArchiveIsClosed walks the graph over an archive of the actual web
// interface. It is the only case that can find a file apps/web gained without
// anybody adding it anywhere, and it is why the fixture cases are not enough.
//
// Measured on origin/web (fe5a0e4), 02.10.2026, by running this very code in a
// throwaway module: 67 files, 59 reached, 0 missing, 0 bare, 8 orphans - the
// seven of extra-web.txt and the one of open-web.txt.
func TestRealArchiveIsClosed(t *testing.T) {
	t.Parallel()
	needTools(t)
	root := repoRoot(t)
	if !refPresent(t, root) {
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

	// BOTH free-note lists are held against the archive, not just extra-web.txt.
	// A line naming a file that is not in the archive any more is a line nobody
	// rechecks, and the lists are exactly the place where that rots unnoticed:
	// they are the part of this check that is NOT derived from anything. Whoever
	// writes "every" builds the loop - so the loop runs over both.
	extra := lines(t, "extra-web.txt")
	open := lines(t, "open-web.txt")
	for name, list := range map[string][]string{
		"extra-web.txt": extra,
		"open-web.txt":  open,
	} {
		for _, rel := range list {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				t.Errorf("%s names %s, which %s does not carry any more; the line is stale",
					name, rel, webRef)
			}
			if slices.Contains(got.Reached, rel) {
				t.Errorf("%s names %s, but a reference reaches it; take the line out", name, rel)
			}
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
	if len(known) != 0 {
		t.Logf("still open, see tools/release/open-web.txt:\n  %s", strings.Join(known, "\n  "))
	}
	if len(orphans) != 0 {
		t.Errorf("these files are in the archive, no reference reaches them, and neither extra-web.txt nor open-web.txt says anything about them:\n  %s",
			strings.Join(orphans, "\n  "))
	}
}
