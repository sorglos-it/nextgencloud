package release

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}
	return dir
}

func TestClosureFollowsAChainOfImports(t *testing.T) {
	dir := tree(t, map[string]string{
		"public/index.php":          "<?php require_once __DIR__ . '/../classes/Page.php';\n",
		"classes/Page.php":          "<?php require_once __DIR__ . '/Url.php';\n",
		"classes/Url.php":           "<?php\n",
		"public/assets/js/a.js":     "import { b } from './sub/b.js';\n",
		"public/assets/js/sub/b.js": "import { c } from '../c.js';\n",
		"public/assets/js/c.js":     "export const c = 1;\n",
	})
	got, err := Closure(dir, []string{"public/index.php", "public/assets/js/a.js"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	for _, want := range []string{
		"classes/Page.php", "classes/Url.php",
		"public/assets/js/sub/b.js", "public/assets/js/c.js",
	} {
		if !slices.Contains(got.Reached, want) {
			t.Errorf("the graph does not arrive at %s; reached %v", want, got.Reached)
		}
	}
	if len(got.Missing) != 0 {
		t.Errorf("nothing is missing in this tree, but got %v", got.Missing)
	}
}

func TestClosureNamesAFileThatIsReferencedAndAbsent(t *testing.T) {
	dir := tree(t, map[string]string{
		"public/assets/js/a.js": "import { b } from './b.js';\n",
	})
	got, err := Closure(dir, []string{"public/assets/js/a.js"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	if len(got.Missing) != 1 ||
		got.Missing[0].From != "public/assets/js/a.js" ||
		got.Missing[0].To != "public/assets/js/b.js" {
		t.Fatalf("the missing import was not named: %v", got.Missing)
	}
}

func TestClosureIgnoresAnImportInAComment(t *testing.T) {
	dir := tree(t, map[string]string{
		"v/utils.js": "/**\n * import { ahash } from '@noble/hashes/utils.js';\n */\n" +
			"// import { x } from '@noble/hashes/sha2.js';\n" +
			"export const u = 1;\n",
	})
	got, err := Closure(dir, []string{"v/utils.js"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	if len(got.Bare) != 0 {
		t.Errorf("a commented import was taken for a live one: %v", got.Bare)
	}
	if len(got.Missing) != 0 {
		t.Errorf("a commented import was looked for: %v", got.Missing)
	}
}

func TestClosureReportsALiveBareSpecifier(t *testing.T) {
	dir := tree(t, map[string]string{
		"v/utils.js": "import { sha256 } from '@noble/hashes/sha2.js';\nexport const u = 1;\n",
	})
	got, err := Closure(dir, []string{"v/utils.js"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	if len(got.Bare) != 1 || got.Bare[0].To != "@noble/hashes/sha2.js" {
		t.Fatalf("the live bare specifier was not named: %v", got.Bare)
	}
}

func TestClosureFollowsAWorkerAndAStylesheet(t *testing.T) {
	dir := tree(t, map[string]string{
		"public/assets/js/login.js":       "const w = new Worker('./auth/worker.js', { type: 'module' });\n",
		"public/assets/js/auth/worker.js": "import { h } from '../hash.js';\n",
		"public/assets/js/hash.js":        "export const h = 1;\n",
		"templates/frame.php":             "<link rel=\"stylesheet\" href=\"/assets/css/shell.css\">\n",
		"public/assets/css/shell.css":     "body { background: url('../img/bg.png'); }\n",
		"public/assets/img/bg.png":        "not really a png\n",
	})
	got, err := Closure(dir, []string{"public/assets/js/login.js", "templates/frame.php"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	for _, want := range []string{
		"public/assets/js/auth/worker.js", "public/assets/js/hash.js",
		"public/assets/css/shell.css", "public/assets/img/bg.png",
	} {
		if !slices.Contains(got.Reached, want) {
			t.Errorf("the graph does not arrive at %s; reached %v", want, got.Reached)
		}
	}
	if len(got.Missing) != 0 {
		t.Errorf("nothing is missing in this tree, but got %v", got.Missing)
	}
}

func TestClosureFollowsAWorkerBehindAModuleURL(t *testing.T) {
	dir := tree(t, map[string]string{
		"public/assets/js/contracts/proof.js": "const ARGON2_WORKER = new URL(\"../auth/argon2-worker.js\", import.meta.url);\n" +
			"export function f() { return new Worker(ARGON2_WORKER, { type: \"module\" }); }\n",
		"public/assets/js/auth/argon2-worker.js": "import { a } from '../../vendor/h/argon2.js';\n",
		"public/assets/vendor/h/argon2.js":       "export const a = 1;\n",
	})
	got, err := Closure(dir, []string{"public/assets/js/contracts/proof.js"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	for _, want := range []string{
		"public/assets/js/auth/argon2-worker.js",
		"public/assets/vendor/h/argon2.js",
	} {
		if !slices.Contains(got.Reached, want) {
			t.Errorf("the graph does not arrive at %s; reached %v", want, got.Reached)
		}
	}
}

func TestClosureDoesNotInventAFileFromARuntimeURL(t *testing.T) {
	dir := tree(t, map[string]string{
		"public/assets/js/net/http.js": "export function s(p) { return new URL(p, location.href); }\n" +
			"const q = new URL('/api/socket', location.href);\n",
	})
	got, err := Closure(dir, []string{"public/assets/js/net/http.js"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	if len(got.Missing) != 0 {
		t.Fatalf("a runtime URL was taken for a file: %v", got.Missing)
	}
}

func TestClosureResolvesAMarkupNameWithoutADotSlash(t *testing.T) {
	dir := tree(t, map[string]string{
		"assets/apps/calc/index.html": "<script src=\"app.js\"></script><link href=\"app.css\">\n",
		"assets/apps/calc/app.js":     "// calc\n",
		"assets/apps/calc/app.css":    "/* calc */\n",
	})
	got, err := Closure(dir, []string{"assets/apps/calc/index.html"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	for _, want := range []string{"assets/apps/calc/app.js", "assets/apps/calc/app.css"} {
		if !slices.Contains(got.Reached, want) {
			t.Errorf("the graph does not arrive at %s; reached %v", want, got.Reached)
		}
	}
	if len(got.Missing) != 0 || len(got.Bare) != 0 {
		t.Errorf("missing %v, bare %v", got.Missing, got.Bare)
	}
}

func TestClosureResolvesARootPathAgainstTheWebRoot(t *testing.T) {
	dir := tree(t, map[string]string{
		"templates/frame.php":   "<script type=\"module\" src=\"/assets/js/m.js\"></script>\n",
		"public/assets/js/m.js": "export const m = 1;\n",
	})
	got, err := Closure(dir, []string{"templates/frame.php"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	if !slices.Contains(got.Reached, "public/assets/js/m.js") {
		t.Fatalf("a root path was not resolved against public/: reached %v, missing %v",
			got.Reached, got.Missing)
	}
}

func TestClosureResolvesASlashAfterDirAgainstTheFile(t *testing.T) {
	dir := tree(t, map[string]string{
		"classes/Config.php": "<?php require_once __DIR__ . '/Url.php';\n",
		"classes/Url.php":    "<?php\n",
	})
	got, err := Closure(dir, []string{"classes/Config.php"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	if !slices.Contains(got.Reached, "classes/Url.php") {
		t.Fatalf("__DIR__ . '/Url.php' was not resolved against the file: reached %v, missing %v",
			got.Reached, got.Missing)
	}
	if len(got.Missing) != 0 {
		t.Fatalf("it invented %v", got.Missing)
	}
}

func TestClosureRefusesAPathThatLeavesTheTree(t *testing.T) {
	dir := tree(t, map[string]string{
		"public/assets/js/a.js": "import { x } from '../../../../etc/passwd';\n",
	})
	if _, err := Closure(dir, []string{"public/assets/js/a.js"}); err == nil {
		t.Fatal("a reference out of the tree was accepted")
	}
}
