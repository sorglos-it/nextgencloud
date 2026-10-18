// Package release decides what a release archive must carry. It does that from
// the archive's own references and not from a list somebody keeps up to date:
// a list is wrong the moment a file gains an import, and nothing notices.
package release

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Reference is one pointer from one file to another, both as slash paths
// relative to the root of the archive.
type Reference struct{ From, To string }

// Result is what Closure found.
type Result struct {
	Reached []string    // sorted; every file the graph arrives at and that exists
	Missing []Reference // referenced and not in the tree
	Bare    []Reference // a live module specifier that is not relative
}

// A reference resolves against one of two bases. Getting this wrong was the
// whole of finding N1: require_once __DIR__ . '/Url.php' begins with a slash
// and is still relative TO THE FILE, while href="/assets/…" is a URL and is
// relative to the web root. One rule for both resolves one of them wrongly.
type base int

const (
	fromFile base = iota // relative to the folder of the file that names it
	fromWeb              // a URL path, relative to the web root
)

type ref struct {
	spec string
	base base
}

var (
	// jsImport catches "import x from '…'", "import '…'", "export … from '…'"
	// and a dynamic import('…').
	jsImport = regexp.MustCompile(`(?:\bfrom|\bimport)\s*\(?\s*['"]([^'"]+)['"]`)
	// jsWorker catches new Worker('…') and new SharedWorker('…') with a literal.
	jsWorker = regexp.MustCompile(`new\s+(?:Shared)?Worker\s*\(\s*['"]([^'"]+)['"]`)
	// jsModuleURL catches new URL("…", import.meta.url): the idiom for naming a
	// FILE beside a module, and the only new URL( that means a file. Measured on
	// origin/web: four occurrences of new URL(, and only this one has
	// import.meta.url as its base - the other three build runtime addresses
	// from location.href. A pattern without that condition would have invented
	// three references.
	jsModuleURL = regexp.MustCompile(`new\s+URL\s*\(\s*['"]([^'"]+)['"]\s*,\s*import\.meta\.url\s*\)`)
	// phpInclude catches require_once __DIR__ . '…' and its three siblings.
	phpInclude = regexp.MustCompile(`(?:require_once|require|include_once|include)\s*\(?\s*__DIR__\s*\.\s*['"]([^'"]+)['"]`)
	// phpRender catches Page::render(__DIR__ . '…') and Page::frame(__DIR__ . '…').
	phpRender = regexp.MustCompile(`(?:render|frame)\s*\(\s*__DIR__\s*\.\s*['"]([^'"]+)['"]`)
	// markupRef catches src="…" and href="…" in markup and in a PHP template.
	markupRef = regexp.MustCompile(`(?:src|href)\s*=\s*["']([^"']+)["']`)
	// cssURL catches url(…) in a stylesheet.
	cssURL = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)

	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineComment  = regexp.MustCompile(`(?m)(^|\s)//.*$`)
	phpComment   = regexp.MustCompile(`(?m)(^|\s)#.*$`)
)

// webRoot is the folder Apache serves, so a URL path resolves against it:
// apache-vhost.conf has DocumentRoot "/var/www/ngc/public".
const webRoot = "public"

// Closure walks the references of dir from entrypoints outwards. Both the
// entrypoints and every path in the result are slash paths relative to dir.
//
// It returns an error only when a reference would leave the tree, because that
// is a broken archive and not a missing file: nothing below the root may point
// above it (safe unpacking, shared/README.md).
func Closure(dir string, entrypoints []string) (Result, error) {
	var res Result
	seen := map[string]bool{}
	queue := slices.Clone(entrypoints)
	for len(queue) > 0 {
		from := queue[0]
		queue = queue[1:]
		if seen[from] {
			continue
		}
		seen[from] = true

		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(from)))
		if err != nil {
			continue
		}
		res.Reached = append(res.Reached, from)

		refs, bare := references(from, string(body))
		for _, b := range bare {
			res.Bare = append(res.Bare, Reference{From: from, To: b})
		}
		for _, r := range refs {
			to, err := resolve(from, r)
			if err != nil {
				return Result{}, fmt.Errorf("%s points out of the archive: %s", from, r.spec)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(to))); err != nil {
				res.Missing = append(res.Missing, Reference{From: from, To: to})
				continue
			}
			queue = append(queue, to)
		}
	}
	sort.Strings(res.Reached)
	sortRefs(res.Missing)
	sortRefs(res.Bare)
	return res, nil
}

func sortRefs(r []Reference) {
	sort.Slice(r, func(i, j int) bool {
		if r[i].From != r[j].From {
			return r[i].From < r[j].From
		}
		return r[i].To < r[j].To
	})
}

// skip reports whether a specifier names nothing in the archive.
func skip(s string) bool {
	switch {
	case s == "", strings.HasPrefix(s, "data:"), strings.HasPrefix(s, "http://"),
		strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "//"),
		strings.HasPrefix(s, "#"), strings.HasPrefix(s, "mailto:"),
		strings.HasPrefix(s, "javascript:"), strings.HasPrefix(s, "<"):
		return true
	}
	return false
}

// references returns the references of one file and, separately, the live
// module specifiers that are not relative. Comments come out first: the
// vendored noble-hashes files carry "@noble/hashes/…" imports inside JSDoc
// (measured in utils.js:118, _md.js:53, blake2.js:428), and taking those for
// real would refuse every release.
func references(from, body string) (refs []ref, bare []string) {
	kind := path.Ext(from)
	switch kind {
	case ".js", ".css":
		body = lineComment.ReplaceAllString(blockComment.ReplaceAllString(body, ""), "")
	case ".php":
		// A PHP file may use // and # as well as /* */.
		body = phpComment.ReplaceAllString(
			lineComment.ReplaceAllString(blockComment.ReplaceAllString(body, ""), ""), "")
	}

	// addFile takes specifiers that are relative TO THIS FILE, whatever they
	// begin with. A leading slash after __DIR__ is still a file path.
	addFile := func(specs []string) {
		for _, s := range specs {
			if !skip(s) {
				refs = append(refs, ref{spec: s, base: fromFile})
			}
		}
	}
	// addURL takes specifiers out of markup: a leading "/" is a URL and goes to
	// the web root, anything else is relative to the file. "app.js" without a
	// "./" is relative too - it is NOT a module specifier, and dropping it
	// silently was finding N2: both assets/apps/*/index.html carry exactly
	// src="app.js" and href="app.css".
	addURL := func(specs []string) {
		for _, s := range specs {
			if skip(s) {
				continue
			}
			b := fromFile
			if strings.HasPrefix(s, "/") {
				b = fromWeb
			}
			refs = append(refs, ref{spec: s, base: b})
		}
	}
	// addModule takes JS module specifiers: relative ones are references, a
	// bare one resolves to nothing without an import map and is reported.
	addModule := func(specs []string) {
		for _, s := range specs {
			switch {
			case skip(s):
			case strings.HasPrefix(s, "./"), strings.HasPrefix(s, "../"),
				strings.HasPrefix(s, "/"):
				b := fromFile
				if strings.HasPrefix(s, "/") {
					b = fromWeb
				}
				refs = append(refs, ref{spec: s, base: b})
			default:
				bare = append(bare, s)
			}
		}
	}
	first := func(re *regexp.Regexp) []string {
		var out []string
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	switch kind {
	case ".js":
		addModule(first(jsImport))
		// A worker and a new URL(…, import.meta.url) are addresses relative to
		// the module, not module specifiers: a bare name there is not an import
		// map's business, so they go through addURL and not addModule.
		addURL(first(jsWorker))
		addURL(first(jsModuleURL))
	case ".php":
		addFile(first(phpInclude))
		addFile(first(phpRender))
		addURL(first(markupRef))
	case ".html", ".htm":
		addURL(first(markupRef))
	case ".css":
		addURL(first(cssURL))
	}
	return refs, bare
}

// resolve turns one reference into a slash path relative to the archive root.
// A result that climbs above the root is an error.
func resolve(from string, r ref) (string, error) {
	spec := r.spec
	// A query or fragment belongs to a URL, not to a file name.
	if i := strings.IndexAny(spec, "?#"); i >= 0 {
		spec = spec[:i]
	}
	if spec == "" {
		return "", fmt.Errorf("empty reference")
	}
	var joined string
	if r.base == fromWeb {
		joined = path.Join(webRoot, spec)
	} else {
		joined = path.Join(path.Dir(from), spec)
	}
	clean := path.Clean(joined)
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("leaves the archive: %s", r.spec)
	}
	return clean, nil
}
