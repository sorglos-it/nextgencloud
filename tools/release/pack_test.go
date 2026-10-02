package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// repoRoot is the root of this checkout: this file sits in tools/release/.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

// gitEnv is the environment every git and every script in a test runs with.
//
// GIT_CONFIG_COUNT is THE GUARD, and it has to be here and not only in the
// shell: a worktree outside the repository needs safe.directory, "git -c" does
// not help because the test is not the caller, and without it git refuses with
// "detected dubious ownership" - measured, and tools/check.sh then exits 128.
// It is set for these child processes only; nothing global is touched.
//
// GIT_CONFIG_NOSYSTEM switches off the SYSTEM file ONLY, so a global
// core.autocrlf still reaches in. That is why webFixture sets core.autocrlf in
// its own repository instead of relying on this.
func gitEnv(t *testing.T) []string {
	t.Helper()
	return append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=safe.directory",
		"GIT_CONFIG_VALUE_0="+filepath.ToSlash(repoRoot(t)),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_DATE=@1790000000 +0000",
		"GIT_COMMITTER_DATE=@1790000000 +0000")
}

// tarCmd builds a tar command with --force-local, and that flag is not
// cosmetic: GNU tar reads "C:\\Users\\..." as the rsh syntax host:path and
// answers "tar: Cannot connect to C: resolve failed" (measured, exit 128). Every
// path a Go test hands tar is absolute and native, so every call needs it.
func tarCmd(args ...string) *exec.Cmd {
	out := []string{"--force-local"}
	for _, a := range args {
		out = append(out, filepath.ToSlash(a))
	}
	return exec.Command("tar", out...)
}

func needTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"sh", "git", "tar", "openssl", "sha256sum", "base64"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is missing", bin)
		}
	}
}

// needVerifyTools gates ONLY the cases that run
// shared/testdata/release/verify.sh, because that script needs jq and the rest
// of this file does not. Measured: putting jq into needTools skipped all twelve
// cases instead of two, and twelve skips hide more than they report.
func needVerifyTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"jq", "od", "cut", "wc", "mktemp"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is missing; verify.sh cannot run (shared/ulh.md names it)", bin)
		}
	}
}

// webFixture builds a throwaway git repository shaped like apps/web. The tree
// is written out HERE, by hand, and NOT derived from entrypoints-web.txt or
// extra-web.txt: a fixture built from the list it is used to check can only
// ever agree with itself, which was the mistake of the first draft.
//
// Three shapes in here are copied from the real tree because getting them wrong
// was measured, not imagined:
//   - classes/Config.php uses __DIR__ . '/Url.php', a leading slash that is
//     still relative to the file.
//   - assets/apps/*/index.html use src="app.js", a name with no "./".
//   - proof.js reaches its worker INDIRECTLY, through
//     new URL("...", import.meta.url) held in a const - which is how
//     public/assets/js/contracts/proof.js:175/254 really does it.
func webFixture(t *testing.T, change func(files map[string]string)) string {
	t.Helper()
	files := map[string]string{
		"VERSION": "1.2.3\n",
		"public/index.php": "<?php\n" +
			"require_once __DIR__ . '/../classes/Config.php';\n" +
			"require_once __DIR__ . '/../classes/Page.php';\n" +
			"$raw = @file_get_contents(__DIR__ . '/../VERSION', false, null, 0, 32);\n" +
			"Page::render(__DIR__ . '/../templates/desktop.php', []);\n" +
			"Page::frame(__DIR__ . '/assets/js/shell/frame-boot.js');\n",
		"classes/Config.php":                   "<?php require_once __DIR__ . '/Url.php';\n",
		"classes/Url.php":                      "<?php # end of the chain\n",
		"classes/Page.php":                     "<?php self::render(__DIR__ . '/../templates/frame.php', []);\n",
		"templates/desktop.php":                "<link rel=\"stylesheet\" href=\"/assets/css/shell.css\">\n",
		"templates/frame.php":                  "<script type=\"module\" src=\"/assets/js/shell/main.js\"></script>\n",
		"templates/apache-vhost.conf":          "# the virtual host\n",
		"templates/apache-apis.conf.example":   "# the balancer\n",
		"README.md":                            "# web\n",
		"CLAUDE.md":                            "# web\n",
		"public/assets/css/shell.css":          "body { background: url('../img/bg.png'); }\n",
		"public/assets/img/bg.png":             "not really a png\n",
		"public/assets/js/shell/frame-boot.js": "import { p } from '../contracts/proof.js';\n",
		"public/assets/js/shell/main.js":       "import { d } from './dom.js';\n",
		"public/assets/js/shell/dom.js":        "export const d = 1;\n",
		"public/assets/js/contracts/proof.js": "const ARGON2_WORKER = new URL(\"../auth/argon2-worker.js\", import.meta.url);\n" +
			"export function p() { return new Worker(ARGON2_WORKER, { type: \"module\" }); }\n",
		"public/assets/js/auth/argon2-worker.js":      "import { h } from '../../vendor/noble-hashes/argon2.js';\n",
		"public/assets/vendor/noble-hashes/argon2.js": "/**\n * import { x } from '@noble/hashes/sha2.js';\n */\nexport const h = 1;\n",
		"public/assets/vendor/noble-hashes/LICENSE":   "MIT\n",
		"public/assets/vendor/noble-hashes/SOURCE.md": "from npm\n",
		"public/assets/ngc.js":                        "// the library the shell writes into an app\n",
		"assets/apps/calculator/index.html":           "<script src=\"app.js\"></script><link href=\"app.css\">\n",
		"assets/apps/calculator/app.js":               "// calc\n",
		"assets/apps/calculator/app.css":              "/* calc */\n",
		"assets/apps/terminal/index.html":             "<script src=\"app.js\"></script><link href=\"app.css\">\n",
		"assets/apps/terminal/app.js":                 "// term\n",
		"assets/apps/terminal/app.css":                "/* term */\n",
		// Must not travel.
		"tests/shell/boot.spec.js": "// must not travel\n",
		"tests/package.json":       "{}\n",
	}
	if change != nil {
		change(files)
	}
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, "apps", "web", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}
	// The same rule the repository has, so the fixture meets the same trap.
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"),
		[]byte("* text=auto eol=lf\n"), 0o644); err != nil {
		t.Fatalf("gitattributes: %v", err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = gitEnv(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	// THE RIGGING THAT MAKES THE CRLF CASE REAL. core.autocrlf=true is what
	// rewrites the bytes, and on Linux it is off, so without this line
	// TestPackWritesLFWhateverTheCheckoutDoes could not fall there - it would
	// pass for the wrong reason. Measured: git archive of a SUBTREE does not see
	// the root .gitattributes, so eol=lf above does not save it.
	git("config", "core.autocrlf", "true")
	git("config", "user.name", "t")
	git("config", "user.email", "t@example.org")
	git("config", "commit.gpgsign", "false")
	git("add", "--", ".")
	git("commit", "-q", "-m", "fixture")
	return dir
}

// pack runs the script and returns the output directory, the combined output
// and the error, so a test can demand red.
func pack(t *testing.T, repo, ref, seq string) (string, string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	cmd := exec.Command("sh", filepath.Join(repoRoot(t), "tools", "pack-release.sh"),
		ref, seq, out)
	cmd.Dir = repo
	// The guard has to name THIS repository, not the checkout: the script runs
	// git inside repo.
	env := append(gitEnv(t), "TZ=UTC", "LC_ALL=C",
		"GIT_CONFIG_VALUE_0="+filepath.ToSlash(repo))
	cmd.Env = env
	b, err := cmd.CombinedOutput()
	return out, string(b), err
}

// unpack extracts an archive so the graph can be walked over it.
func unpack(t *testing.T, tarball string) string {
	t.Helper()
	dir := t.TempDir()
	if b, err := tarCmd("-xf", tarball, "-C", dir).CombinedOutput(); err != nil {
		t.Fatalf("tar -xf: %v\n%s", err, b)
	}
	return dir
}

// lines reads one of the three short files beside this test.
func lines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "release", name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	// open-web.txt may legitimately be empty - that is the goal state.
	if len(out) == 0 && name != "open-web.txt" {
		t.Fatalf("%s lists nothing", name)
	}
	return out
}

// TestArchiveIsClosed is the assertion this plan exists for: every file the
// archive refers to is in the archive. The authority is the graph, not a list.
func TestArchiveIsClosed(t *testing.T) {
	needTools(t)
	repo := webFixture(t, nil)
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
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
}

// TestArchiveCarriesTheFilesNoReferenceReaches holds the short list, and it
// falls for its own subject because the fixture writes those files without
// consulting the list.
func TestArchiveCarriesTheFilesNoReferenceReaches(t *testing.T) {
	needTools(t)
	repo := webFixture(t, nil)
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	dir := unpack(t, filepath.Join(out, "ngc-web-all.tar"))
	for _, rel := range lines(t, "extra-web.txt") {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("the archive does not carry %s", rel)
		}
	}
}

// TestPackRefusesATreeWithoutTheEntryPoint: public/index.php is the one file
// ngc-web.sh checks itself (line 619), so it must never leave the build.
func TestPackRefusesATreeWithoutTheEntryPoint(t *testing.T) {
	needTools(t)
	repo := webFixture(t, func(f map[string]string) { delete(f, "public/index.php") })
	_, log, err := pack(t, repo, "main", "3")
	if err == nil {
		t.Fatalf("pack-release.sh accepted a tree without public/index.php\n%s", log)
	}
	if !strings.Contains(log, "public/index.php") {
		t.Errorf("the message does not name the missing file:\n%s", log)
	}
}

func TestArchiveCarriesNoTests(t *testing.T) {
	needTools(t)
	repo := webFixture(t, nil)
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	b, err := tarCmd("-tf", filepath.Join(out, "ngc-web-all.tar")).Output()
	if err != nil {
		t.Fatalf("tar -tf: %v", err)
	}
	for _, e := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(e), "tests/") {
			t.Errorf("the archive carries a test file: %s", e)
		}
	}
}

func TestPackIsReproducible(t *testing.T) {
	needTools(t)
	repo := webFixture(t, nil)
	sum := func() [32]byte {
		out, log, err := pack(t, repo, "main", "3")
		if err != nil {
			t.Fatalf("pack-release.sh: %v\n%s", err, log)
		}
		b, err := os.ReadFile(filepath.Join(out, "ngc-web-all.tar"))
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		return sha256.Sum256(b)
	}
	if a, b := sum(), sum(); a != b {
		t.Errorf("two runs over the same source gave different archives: %x != %x", a, b)
	}
}

// TestPackWritesLFWhateverTheCheckoutDoes. The fixture sets core.autocrlf=true
// in its own repository, so this falls on Linux too - without that it would
// pass there for the wrong reason, because the trap is a setting of THIS
// machine and not of the source.
func TestPackWritesLFWhateverTheCheckoutDoes(t *testing.T) {
	needTools(t)
	repo := webFixture(t, nil)
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	b, err := tarCmd("-xOf", filepath.Join(out, "ngc-web-all.tar"), "VERSION").Output()
	if err != nil {
		t.Fatalf("tar -xOf VERSION: %v", err)
	}
	if strings.Contains(string(b), "\r") {
		t.Errorf("VERSION in the archive holds a CR; on %s the eol conversion was not switched off", runtime.GOOS)
	}
}

// TestPackRefusesAVersionStoreWouldReject. store.ValidVersion is
// ^(0|[1-9][0-9]{0,5})\.(…)\.(…)$ (shared/store/manifest.go:224) and verify.sh
// does not check the version at all, so a version that only looks like one is
// packed, signed, published and rejected at the user as ErrRelease.
func TestPackRefusesAVersionStoreWouldReject(t *testing.T) {
	needTools(t)
	for _, bad := range []string{"1.2.3.4", "01.2.3", "1234567.0.0", "1.2", "v1.2.3", "1.2.3-rc1"} {
		if store.ValidVersion(bad) {
			t.Fatalf("%q is a valid version after all; the case is measuring nothing", bad)
		}
		repo := webFixture(t, func(f map[string]string) { f["VERSION"] = bad + "\n" })
		_, log, err := pack(t, repo, "main", "3")
		if err == nil {
			t.Errorf("pack-release.sh accepted the version %q\n%s", bad, log)
		}
	}
	// And a good one still passes, so the gate is not simply closed.
	repo := webFixture(t, func(f map[string]string) { f["VERSION"] = "0.12.0\n" })
	if _, log, err := pack(t, repo, "main", "3"); err != nil {
		t.Errorf("pack-release.sh refused the valid version 0.12.0: %v\n%s", err, log)
	}
}

// TestArchiveKeepsTheExecutableBit measures what the later archives of ngc-api
// and ngc-controller live on: git archive takes the mode from the git entry and
// not from the file system, so an executable stays executable even when the
// archive is built on Windows. apps/web has no 100755 file at all (measured),
// so the fixture makes one in the INDEX, which is why this works here.
func TestArchiveKeepsTheExecutableBit(t *testing.T) {
	needTools(t)
	repo := webFixture(t, func(f map[string]string) {
		f["ngc-probe"] = "#!/bin/sh\nexit 0\n"
	})
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = gitEnv(t)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	git("update-index", "--chmod=+x", "apps/web/ngc-probe")
	git("commit", "-q", "-m", "probe")

	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	b, err := tarCmd("-tvf", filepath.Join(out, "ngc-web-all.tar")).Output()
	if err != nil {
		t.Fatalf("tar -tvf: %v", err)
	}
	// The name is matched as the LAST FIELD, with a leading "./" allowed. tar
	// writes "./ngc-probe" when it packs a directory as ".", and a matcher that
	// demanded " ngc-probe" would then report "not in the archive" - the wrong
	// red for this subject, and the mistake the red of step 7 (b) walked into.
	var line string
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		if name := strings.TrimPrefix(f[len(f)-1], "./"); name == "ngc-probe" {
			line = l
		}
	}
	if line == "" {
		t.Fatal("ngc-probe is not in the archive")
	}
	if !strings.HasPrefix(line, "-rwx") {
		t.Errorf("the executable bit is gone: %q", line)
	}
}

func TestPayloadPassesVerifyRelease(t *testing.T) {
	needTools(t)
	repo := webFixture(t, nil)
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	payload, err := os.ReadFile(filepath.Join(out, "release.payload.json"))
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	// The real key is not on this machine and never in a test: a key made here
	// signs the payload, so what is measured is the shape of the payload.
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	env, err := sign.Sign(priv, sign.PurposeRelease, payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	rel, err := store.VerifyRelease(env, pub, 3)
	if err != nil {
		t.Fatalf("VerifyRelease: %v", err)
	}
	if rel.Seq != 3 || rel.Version != "1.2.3" || len(rel.Files) != 1 {
		t.Fatalf("payload wrong: seq=%d version=%q files=%d", rel.Seq, rel.Version, len(rel.Files))
	}
	// issued is the commit date and not the clock, because it is inside the
	// payload and the payload has to be reproducible too.
	if rel.Issued != 1790000000 {
		t.Errorf("issued is %d, not the commit date 1790000000", rel.Issued)
	}
	f := rel.Files[0]
	if f.Part != store.ReleaseWeb || f.Arch != "all" || f.URL != "ngc-web-all.tar" {
		t.Errorf("entry wrong: %+v", f)
	}
	tarball, err := os.ReadFile(filepath.Join(out, "ngc-web-all.tar"))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if got := sha256.Sum256(tarball); string(f.SHA256) != string(got[:]) {
		t.Errorf("the payload names another checksum than the archive has")
	}
	if f.Size != int64(len(tarball)) {
		t.Errorf("the payload names size %d, the archive has %d", f.Size, len(tarball))
	}
	var again store.Release
	if err := json.Unmarshal(payload, &again); err != nil {
		t.Fatalf("payload is no store.Release: %v", err)
	}
}
