package browser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The two sentences of link.Table that no single run can see all at once are
// checked here over the package's syntax tree.
//
// Why a syntax tree and not a search for text, measured on the version that did
// search for text: six ways past it, and the first one our own commit gate
// builds. gofmt lines up the keys of a composite literal, so the field reads
// "Type:    wire.TypeGrant" with four spaces where a needle holds one -- and
// tools/check.sh refuses any other spelling. Then: a field written later
// (f.Type = wire.TypeGrant), a closure at package level that is counted as part
// of the function above it, a .Reserve( inside a string such as "wss://x",
// .Reserve taken as a method value, and a body check that "_ = \"s.Close(ctx,\""
// already satisfies. A tree sees all six, and it sees no text inside a string.
//
// One line of the walk is about reading and not about writing: feed has to name
// wire.TypeGrant in a case label, because the browser sends GRANTs too, and a
// label is no frame. The walk leaves case labels out of the count and keeps
// everything else -- a composite literal of any shape, a field set later, a
// method value, a closure at package level.
//
// What this walk is NOT, and both limits are named because a lint that is taken
// for a proof is worse than none.
//
// It is a lint, not the holder of the first sentence. It reads the files on disk,
// so a build that puts another file in their place (go test -overlay) is
// invisible to it -- a mutant of the built program stays green here.
//
// And it matches a NAME, not a VALUE. wire.TypeGrant is the number 5, so
// "Type: 5", a constant of this package set to it, "wire.Type(5)" and a dot
// import of package wire all go past this walk. None of those is exotic. Closing
// it would take go/types, that is, type checking the package rather than parsing
// it -- and it is not done, because the compiler already holds the sentence and
// this walk is only here for what the compiler cannot say.
//
// So: the claim of this file is exactly "a tree sees the six shapes a text search
// let through, and a case label is not one of them", and nothing more than that.
// The first sentence is held by the compiler, because Session.link is unexported
// and the only ways out of this package are SendGrant, SendData, SendControl and
// Close.

// site is one place in the package's own source.
type site struct {
	fn  string // the top-level function it sits in, or "" outside one
	pos token.Position
	at  token.Pos
}

func (s site) String() string {
	if s.fn == "" {
		return fmt.Sprintf("%s (outside any function)", s.pos)
	}
	return s.fn
}

// pkg is the parsed source of this package, test files left out.
type pkg struct {
	fset  *token.FileSet
	files map[string]*ast.File
}

// loadPkg parses every non-test Go file of a directory. Files a build tag would
// exclude are parsed too: for a lint, looking at more than the build does is the
// safe direction -- a GRANT behind a tag is still a GRANT this package can grow.
func loadPkg(t *testing.T, dir string) pkg {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	p := pkg{fset: token.NewFileSet(), files: map[string]*ast.File{}}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		// Mode 0: no comments in the tree, so a sentence about wire.TypeGrant in
		// a comment is never counted as a use of it.
		f, err := parser.ParseFile(p.fset, n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		p.files[n] = f
	}
	if len(p.files) == 0 {
		t.Fatalf("no source parsed in %s -- a lint that reads nothing is green for ever", dir)
	}
	return p
}

// selectors finds every selector expression of the package whose field or method
// name is name, wherever it stands: in a call, as a method value, in a composite
// literal of any shape, in an assignment. Each hit says which top-level function
// it sits in; a hit outside every function names its file and line, so a closure
// at package level cannot be counted as part of the function above it.
func (p pkg) selectors(name string) []site {
	var out []site
	for _, f := range p.files {
		for _, d := range f.Decls {
			fn := ""
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
			}
			ast.Inspect(d, func(n ast.Node) bool {
				se, ok := n.(*ast.SelectorExpr)
				if ok && se.Sel.Name == name {
					out = append(out, site{fn: fn, pos: p.fset.Position(se.Sel.Pos()), at: se.Sel.Pos()})
				}
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pos.Offset < out[j].pos.Offset })
	return out
}

// caseLabels are the hits that stand in the label of a case clause, as in
// "case wire.TypeGrant:". Reading a type apart is not building a frame of it,
// and feed must be able to read it: the browser sends GRANTs too. The first
// version of this walk did not tell the two apart and named feed as a second
// builder -- a false finding, and the kind a text search for "Type: " hid by
// accident rather than on purpose.
func (p pkg) caseLabels(name string) map[token.Pos]bool {
	out := map[token.Pos]bool{}
	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				ast.Inspect(e, func(n ast.Node) bool {
					if se, ok := n.(*ast.SelectorExpr); ok && se.Sel.Name == name {
						out[se.Sel.Pos()] = true
					}
					return true
				})
			}
			return true
		})
	}
	return out
}

// builders are the hits of a name outside every case label: the places that
// build or set a value of it rather than read one apart.
func (p pkg) builders(name string) []site {
	skip := p.caseLabels(name)
	var out []site
	for _, s := range p.selectors(name) {
		if !skip[s.at] {
			out = append(out, s)
		}
	}
	return out
}

// names lists the places of a set of hits, each once, in the order they were
// found.
func names(sites []site) []string {
	var out []string
	for _, s := range sites {
		if !contains(out, s.String()) {
			out = append(out, s.String())
		}
	}
	return out
}

// inFunc keeps the hits that sit in one named function.
func inFunc(sites []site, fn string) []site {
	var out []site
	for _, s := range sites {
		if s.fn == fn {
			out = append(out, s)
		}
	}
	return out
}

// TestOnlySendGrantBuildsAGrant is the lint behind the first sentence: inside
// this package, wire.TypeGrant is named in one function only, that function is
// SendGrant, it asks Allow, and it asks it before it names the type.
func TestOnlySendGrantBuildsAGrant(t *testing.T) {
	p := loadPkg(t, ".")
	grants := p.builders("TypeGrant")
	if len(grants) == 0 {
		t.Fatal("no place that builds a GRANT was found at all -- the walk sees nothing")
	}
	if got := names(grants); len(got) != 1 || got[0] != "SendGrant" {
		t.Fatalf("a GRANT frame is built in %v, not in SendGrant alone", got)
	}
	allow := p.selectors("Allow")
	if got := names(allow); len(got) != 1 || got[0] != "SendGrant" {
		t.Fatalf("Allow is asked in %v, not in SendGrant alone", got)
	}
	// Order inside SendGrant: the first Allow stands before the first TypeGrant.
	a, g := inFunc(allow, "SendGrant"), inFunc(grants, "SendGrant")
	if len(a) == 0 || len(g) == 0 {
		t.Fatalf("SendGrant asks Allow %d times and names TypeGrant %d times", len(a), len(g))
	}
	if a[0].pos.Offset > g[0].pos.Offset {
		t.Fatalf("SendGrant builds the frame at %s before it asks Allow at %s", g[0].pos, a[0].pos)
	}
}

// TestOnlyProtocolErrorHoldsANumber is what is left of the lint behind the
// third sentence, and it is deliberately less than it was: since link.Table
// holds a number only inside FailChannelChannel, which writes the CLOSE and notes it in one
// call, the compiler holds the sentence and this walk only says that this
// package keeps the two calls in one place each -- FailChannel in protocolError, the
// note of a CLOSE in Close. Both limits of the walk from the file header stand.
func TestOnlyProtocolErrorHoldsANumber(t *testing.T) {
	p := loadPkg(t, ".")
	fail := p.selectors("FailChannel")
	if len(fail) == 0 {
		t.Fatal("no use of FailChannel was found at all -- the walk sees nothing")
	}
	if got := names(fail); len(got) != 1 || got[0] != "protocolError" {
		t.Fatalf("a number is held in %v, not in protocolError alone", got)
	}
	sent := p.selectors("SentClose")
	if got := names(sent); len(got) != 1 || got[0] != "Close" {
		t.Fatalf("SentClose is noted in %v, not in Close alone", got)
	}
}

func TestTheLintSeesThroughTheSixWaysPastATextSearch(t *testing.T) {
	dir := t.TempDir()
	// Every line here is one of the six measured ways past a text search.
	bad := "package x\n" +
		"\n" +
		"import \"go/token\"\n" +
		"\n" +
		"// 3. a closure at package level, after SendGrant in the file.\n" +
		"var elsewhere = func() {\n" +
		"\t_ = wire.Frame{\n" +
		"\t\tChannel: 1,\n" +
		// 1. gofmt lines the keys up, so the needle with one space misses.
		"\t\tType:    wire.TypeGrant,\n" +
		"\t}\n" +
		"}\n" +
		"\n" +
		"func SendGrant(c chan int, f *wire.Frame) {\n" +
		"\t// 2. the field written later, not in a literal.\n" +
		"\tf.Type = wire.TypeGrant\n" +
		"\tc.Allow(1)\n" +
		"}\n" +
		"\n" +
		"func protocolError(s *S, t *T) {\n" +
		"\t// 4. a string that reads like a call, and 6. a body check satisfied\n" +
		"\t// by a string.\n" +
		"\t_ = \"wss://x\" + \"s.Close(ctx,\"\n" +
		"\t_ = token.NoPos\n" +
		"}\n" +
		"\n" +
		"func reads(f wire.Frame) {\n" +
		"\t// 7. reading the type apart is not building a frame of it.\n" +
		"\tswitch f.Type {\n" +
		"\tcase wire.TypeGrant:\n" +
		"\t}\n" +
		"}\n" +
		"\n" +
		"func other(t *T) {\n" +
		"\t// 5. Reserve as a method value, never called here.\n" +
		"\tg := t.Reserve\n" +
		"\t_ = g\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(bad), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p := loadPkg(t, dir)

	// 1 and 2 and 3: two places build a GRANT, and one of them is no function.
	// 7: the case label of reads is not one of them.
	got := names(p.builders("TypeGrant"))
	if len(got) != 2 {
		t.Fatalf("the walk found %v, want two places", got)
	}
	if contains(got, "reads") {
		t.Fatalf("a case label was taken for a place that builds a GRANT: %v", got)
	}
	if all := names(p.selectors("TypeGrant")); len(all) != 3 || !contains(all, "reads") {
		t.Fatalf("every use of the name should still be findable, found %v", all)
	}
	if !strings.Contains(got[0], "outside any function") {
		t.Fatalf("the package-level closure was counted as a function: %v", got)
	}
	if !contains(got, "SendGrant") {
		t.Fatalf("the field written later went unseen: %v", got)
	}
	// 5: Reserve as a method value.
	if r := names(p.selectors("Reserve")); len(r) != 1 || r[0] != "other" {
		t.Fatalf("Reserve as a method value was not seen: %v", r)
	}
	// 4 and 6: neither the host in a URL nor the string that reads like a call
	// counts, so protocolError does not call s.Close here.
	if p.callsSessionClose("protocolError") {
		t.Fatal("a string that reads like s.Close(ctx, ...) was taken for a call")
	}
	// And a name nobody holds finds nothing, so a green run means something.
	if n := names(p.selectors("NothingHereIsCalledThis")); len(n) != 0 {
		t.Fatalf("a name nobody holds was found in %v", n)
	}
}

// callsSessionClose reports whether a function really calls s.Close(...), as a
// call and not as a string that happens to look like one. Since link.Table.Fail
// notes the CLOSE itself, only the synthetic case below still asks it, and there
// it shows that neither a host in a URL nor a string that reads like a call
// counts as one.
func (p pkg) callsSessionClose(fn string) bool {
	for _, f := range p.files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != fn {
				continue
			}
			found := false
			ast.Inspect(fd, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				se, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || se.Sel.Name != "Close" {
					return true
				}
				if id, ok := se.X.(*ast.Ident); ok && id.Name == "s" {
					found = true
				}
				return true
			})
			if found {
				return true
			}
		}
	}
	return false
}

// TestTheLintSeesThroughTheSixWaysPastATextSearch is the self-test of the walk,
// and it covers both sentences, not only the GRANT one: six shapes that a search
// for text let through, in one package it has to report.

// contains says whether a list holds a string.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
