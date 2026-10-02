package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// stubbedSignerGlob matches every copy stubbedSigner can leave behind. It is in
// .gitignore under the same spelling.
const stubbedSignerGlob = "zz-sign-release-selfcheck-off-*.sh"

// TestMain sweeps those copies away before and after the package runs, because
// t.Cleanup does not run when a case is killed - measured with a blocked signer
// and -test.timeout: the copy stayed, and before this round it was not even
// ignored. A copy of the signer with its self-check switched off, harmlessly
// named, in the tools folder, is exactly the thing that gets committed by
// accident.
func TestMain(m *testing.M) {
	sweepStubbedSigners()
	code := m.Run()
	sweepStubbedSigners()
	os.Exit(code)
}

func sweepStubbedSigners() {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	// This file sits in tools/release/, so tools/ is one up.
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(wd), stubbedSignerGlob))
	if err != nil {
		return
	}
	for _, p := range paths {
		os.Remove(p)
	}
}

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

// TestArchiveCarriesNoTestOnlyLink. public/assets/js/net/testlink.js is a
// shortcut the browser tests import, and a release has no business carrying it.
// Decided on 02.10.2026 by the web strand on this measurement of the real tree:
// every mention of the file in SHIPPED code is a comment (apps/js/store.js:239,
// auth/keys.js:37, auth/login.js:50) - the graph reaches it from nowhere - while
// the live "await import(\"/assets/js/net/testlink.js\")" sit only under
// apps/web/tests/ (attacks.spec.js:361, world.spec.js:606/896 and channels.spec.js
// at :20 :31 :48 :65 :83 :110 :134 and on), and tests/ does not travel.
//
// So it is a second ':(exclude)' in pack-release.sh and NOT a line in
// extra-web.txt: an entry there would need a reason, and the only reason
// available would be made up.
func TestArchiveCarriesNoTestOnlyLink(t *testing.T) {
	needTools(t)
	const link = "public/assets/js/net/testlink.js"
	repo := webFixture(t, func(f map[string]string) {
		f[link] = "export function connectForTest() {}\n"
	})
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	b, err := tarCmd("-tf", filepath.Join(out, "ngc-web-all.tar")).Output()
	if err != nil {
		t.Fatalf("tar -tf: %v", err)
	}
	for _, e := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.TrimPrefix(strings.TrimSpace(e), "./") == link {
			t.Errorf("the archive carries %s, which only the browser tests import", link)
		}
	}
	// And the exclusion must not take the folder's other files with it: the
	// pathspec names one file, not net/.
	keep := "public/assets/js/net/http.js"
	repo = webFixture(t, func(f map[string]string) {
		f[link] = "export function connectForTest() {}\n"
		f[keep] = "export const h = 1;\n"
	})
	out, log, err = pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	b, err = tarCmd("-tf", filepath.Join(out, "ngc-web-all.tar")).Output()
	if err != nil {
		t.Fatalf("tar -tf: %v", err)
	}
	var found bool
	for _, e := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.TrimPrefix(strings.TrimSpace(e), "./") == keep {
			found = true
		}
	}
	if !found {
		t.Errorf("the exclusion took %s with it; it must name one file, not the folder", keep)
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

// TestSignScriptMakesAReleaseVerifyShAccepts. It signs with a key made here,
// never with the real release key: that key is not on this machine, and a test
// that needed it would be a test nobody can run.
func TestSignScriptMakesAReleaseVerifyShAccepts(t *testing.T) {
	needTools(t)
	needVerifyTools(t)
	repo := webFixture(t, nil)
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	key := newTestKey(t)

	cmd := exec.Command("sh", filepath.Join(repoRoot(t), "tools", "sign-release.sh"), out, key)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sign-release.sh: %v\n%s", err, b)
	}

	// Both sides must accept it, and the shell side is the one the user runs.
	env := readEnvelope(t, filepath.Join(out, "release.json"))
	pub := readPub(t, filepath.Join(out, "release-key.pem"))
	if _, err := store.VerifyRelease(env, pub, 3); err != nil {
		t.Fatalf("VerifyRelease: %v", err)
	}
	vs := filepath.Join(repoRoot(t), "shared", "testdata", "release", "verify.sh")
	b, err := exec.Command("sh", vs, out, "ngc-web", "all").CombinedOutput()
	if err != nil {
		t.Fatalf("verify.sh: %v\n%s", err, b)
	}
	if !strings.Contains(string(b), "release ok") {
		t.Errorf("verify.sh did not say release ok:\n%s", b)
	}
}

// TestSignScriptLeaksNoKey holds the one rule that cannot be repaired after the
// fact. "Not even shortened" means every PIECE of the key, so this slides a
// window across the whole body instead of only testing prefixes - the first
// draft tested prefixes, and the second half of a line would have walked
// straight past it. The raw key bytes are looked for in three spellings as
// well, because a leak does not have to arrive as PEM.
//
// IT DOES NOT ASK FOR jq, AND THAT IS THE POINT. The first draft gated this on
// needVerifyTools, which meant the one assertion that cannot be repaired after
// the fact - a leaked key is leaked - never ran on a machine without jq, and
// this one has none, in Windows or in WSL. The check itself needs no jq: it
// looks for strings in outputs and files. What needed jq was the script's own
// closing self-check with verify.sh, so this case simply does not demand that
// the script SUCCEED. It measures both ways out instead:
//
//   - the way that really happens without jq: the script aborts at the
//     self-check, prints verify.sh's complaint plus its own, and removes
//     release.json again;
//   - the successful way, through a copy of the script whose self-check is
//     switched off, so the lines after the signature are measured as well.
//
// Both are scanned, and so is every file left in the directory.
func TestSignScriptLeaksNoKey(t *testing.T) {
	needTools(t)
	repo := webFixture(t, nil)
	out, log, err := pack(t, repo, "main", "3")
	if err != nil {
		t.Fatalf("pack-release.sh: %v\n%s", err, log)
	}
	key := newTestKey(t)
	pem, err := os.ReadFile(key)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	// needles: every 12-character window of the PEM body, plus the raw private
	// bytes as hex, base64 and base64url. 12 and not 8: a shorter window meets
	// a base64 coincidence now and then, and a case that cries wolf gets
	// switched off by the next person.
	var body strings.Builder
	for _, line := range strings.Split(string(pem), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "-----") {
			body.WriteString(line)
		}
	}
	needles := map[string]bool{}
	s := body.String()
	if len(s) < 12 {
		t.Fatal("the test key has no body to look for")
	}
	for i := 0; i+12 <= len(s); i++ {
		needles[s[i:i+12]] = true
	}
	der, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("the key is no base64: %v", err)
	}
	// The last 32 bytes of a PKCS#8 Ed25519 key are the seed. It is looked for
	// in four spellings, because a leak does not have to arrive as PEM - and the
	// fourth is the one a surviving mutant found:
	//
	//	openssl pkey -in <key> -text
	//
	// prints the seed as COLON-SEPARATED hex, fifteen bytes to a line. That is
	// openssl's own output form, so it is what somebody debugging actually
	// types, and no window of the PEM body and no plain hex matches it. Measured
	// on a throwaway key:
	//
	//	priv:
	//	    4f:1c:...:e3:
	//	    ...
	//
	// The needle is therefore the colon form with every space and line break
	// taken out, and every haystack is searched in that squeezed shape as well.
	if len(der) < 32 {
		t.Fatalf("the test key is %d bytes of DER, too short to hold a seed", len(der))
	}
	seed := der[len(der)-32:]
	needles[hexOf(seed)] = true
	needles[colonHexOf(seed)] = true
	needles[base64.StdEncoding.EncodeToString(seed)] = true
	needles[base64.RawURLEncoding.EncodeToString(seed)] = true

	// Said out loud, because a scan that found nothing is worth nothing unless
	// it is known how much it looked for.
	t.Logf("looking for %d distinct pieces of the throwaway key", len(needles))

	// The way out that happens here: without jq the self-check cannot run, the
	// script says so and takes release.json back. Its exit code is deliberately
	// NOT demanded - a run that fails prints MORE, not less, so it is the more
	// interesting haystack of the two.
	cmd := exec.Command("sh", filepath.Join(repoRoot(t), "tools", "sign-release.sh"), out, key)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	printed, _ := cmd.CombinedOutput()

	// And the successful way, so the lines after the signature are measured too.
	cmd = exec.Command("sh", stubbedSigner(t), out, key)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	stubbed, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the signer with its self-check switched off: %v\n%s", err, stubbed)
	}

	// What both runs printed, and everything they left behind - EVERY file, at
	// any depth. os.ReadDir was the second hole a surviving mutant found: it
	// lists one level and skips folders, so a key written to out/zz/leak.pem went
	// unseen. WalkDir closes that and the "-text" form above in one go.
	haystacks := map[string]string{
		"the output of the real run":    string(printed),
		"the output of the stubbed run": string(stubbed),
	}
	if err := filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(out, p)
		if err != nil {
			rel = p
		}
		haystacks["file "+filepath.ToSlash(rel)] = string(b)
		return nil
	}); err != nil {
		t.Fatalf("walking the release directory: %v", err)
	}
	t.Logf("searched %d haystacks", len(haystacks))
	for where, hay := range haystacks {
		for n := range needles {
			if holds(hay, n) {
				t.Fatalf("%s holds a piece of the private key", where)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(out, filepath.Base(key))); err == nil {
		t.Error("the script copied the key into the release directory")
	}
}

// stubbedSigner writes a copy of tools/sign-release.sh whose closing self-check
// is switched off and returns its path, so the SUCCESSFUL way out of the script
// can be measured where verify.sh cannot run for want of jq.
//
// It HAS to live in tools/ beside the original: the script finds verify.sh as
// $self/../shared/testdata/release/verify.sh, so a copy in a temp folder dies
// with "verify.sh is missing" and measures nothing. The name carries the process
// id so two runs in one checkout do not fight over it, and it is removed again
// when the case ends.
//
// Only the self-check line is replaced. Everything the key ever touches - the
// public key, the key id, the message, the signature - is the original's.
func stubbedSigner(t *testing.T) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "tools", "sign-release.sh")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read sign-release.sh: %v", err)
	}
	const call = `sh "$verify" "$dir" ngc-web all >"$work/verify.out" 2>&1 || {`
	if !strings.Contains(string(b), call) {
		t.Fatal("the self-check line of sign-release.sh was not found; this helper is stale")
	}
	s := strings.Replace(string(b), call,
		`echo "release ok (self-check switched off for a test)" >"$work/verify.out"; true || {`, 1)
	dst := filepath.Join(repoRoot(t), "tools",
		fmt.Sprintf("zz-sign-release-selfcheck-off-%d.sh", os.Getpid()))
	// The name must stay inside stubbedSignerGlob, which is what TestMain sweeps
	// and what .gitignore holds. If it ever drifts out, the copy survives a
	// killed run and is not ignored.
	if ok, _ := filepath.Match(stubbedSignerGlob, filepath.Base(dst)); !ok {
		t.Fatalf("%s does not match %s; TestMain and .gitignore would both miss it",
			filepath.Base(dst), stubbedSignerGlob)
	}
	if err := os.WriteFile(dst, []byte(s), 0o644); err != nil {
		t.Fatalf("write the stubbed signer: %v", err)
	}
	t.Cleanup(func() { os.Remove(dst) })
	return dst
}

// newTestKey makes an Ed25519 key in PEM with openssl itself, so the script
// meets the same shape it will meet on the key machine.
func newTestKey(t *testing.T) string {
	t.Helper()
	key := filepath.Join(t.TempDir(), "test-release.pem")
	if b, err := exec.Command("openssl", "genpkey", "-algorithm", "ed25519",
		"-out", key).CombinedOutput(); err != nil {
		t.Fatalf("genpkey: %v\n%s", err, b)
	}
	return key
}

// colonHexOf is the spelling "openssl pkey -text" prints, with the line breaks
// and the indent taken out: 4f:1c:...:e3. Measured on a throwaway key, fifteen
// bytes to a line.
func colonHexOf(b []byte) string {
	var out strings.Builder
	for i, c := range b {
		if i > 0 {
			out.WriteByte(':')
		}
		out.WriteString(hexOf([]byte{c}))
	}
	return out.String()
}

// squeeze removes every space, tab and line break. A secret printed over several
// lines - which is what openssl's own -text form does - is one string again
// afterwards, and that is the shape the needles are built in.
func squeeze(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n', '\v', '\f':
			return -1
		}
		return r
	}, s)
}

// holds reports whether hay carries needle - as it stands, squeezed, and in
// either case of hex. Case is only an issue for the hex spellings; base64 is
// case-significant, and lowering it would invent matches.
func holds(hay, needle string) bool {
	if strings.Contains(hay, needle) || strings.Contains(squeeze(hay), needle) {
		return true
	}
	if isHexish(needle) {
		low, up := strings.ToLower(hay), strings.ToUpper(hay)
		n := strings.ToUpper(needle)
		if strings.Contains(low, needle) || strings.Contains(up, n) ||
			strings.Contains(squeeze(low), needle) || strings.Contains(squeeze(up), n) {
			return true
		}
	}
	return false
}

// isHexish answers whether a needle is one of the hex spellings, so that only
// those are compared without regard to case.
func isHexish(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r == ':':
		default:
			return false
		}
	}
	return s != ""
}

func hexOf(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, d[c>>4], d[c&0x0f])
	}
	return string(out)
}

// readEnvelope reads a sign.Envelope the way a consumer does.
func readEnvelope(t *testing.T, path string) sign.Envelope {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	env, err := sign.ParseEnvelope(b, sign.MaxPayload)
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}
	return env
}

// readPub turns the PEM "PUBLIC KEY" beside the release into the 32 bytes of an
// Ed25519 key: the last 32 bytes of the 44-byte DER, the 12 in front being
// Ed25519's fixed algorithm header.
func readPub(t *testing.T, path string) ed25519.PublicKey {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var body strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "-----") {
			body.WriteString(line)
		}
	}
	der, err := base64.StdEncoding.DecodeString(body.String())
	if err != nil {
		t.Fatalf("the public key is no base64: %v", err)
	}
	if len(der) != 44 {
		t.Fatalf("the public key is %d bytes of DER, not 44", len(der))
	}
	return ed25519.PublicKey(der[12:])
}
