package sqlstore

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The guard of this file reads the package's own source and refuses two
// things: a statement that is not a constant, and a value that reaches the
// database as a byte slice from anywhere but blob. That is how "SQL only with
// placeholders" and "nothing nil where every column is NOT NULL" are kept,
// instead of trusting everyone to remember them.
//
// It asks go/types what a name stands for, never what it is called: a local
// variable may carry the name of a constant, and only the object behind the
// name says which of the two is meant. It reads whole files, not only the
// bodies of function declarations, so a call in a function literal at a
// package var is read like any other. What it cannot resolve it reports, so a
// hole comes out as a failure and not as a pass.
//
// TestTheGuardCatchesWhatSlippedPast below runs it against testdata/, where
// four packages carry one hole each. Three of those four were measured green
// against the guard this one replaces.

// dbMethods are the ways a statement reaches the driver.
var dbMethods = map[string]bool{
	"Exec": true, "ExecContext": true,
	"Query": true, "QueryContext": true,
	"QueryRow": true, "QueryRowContext": true,
	"Prepare": true, "PrepareContext": true,
}

// stmtPlace is where the statement stands in a call of one of dbMethods:
// second when a context comes first, which is exactly what Context in the
// name says. The place is read off the name of the method, never off the name
// of a variable.
func stmtPlace(method string) int {
	if strings.HasSuffix(method, "Context") {
		return 1
	}
	return 0
}

// modulePath is the import path of this module. Packages under it are read
// from source, everything else is left unresolved on purpose: every name this
// guard asks about is declared either in this package or in a struct of the
// contract, and reading the standard library from source as well was measured
// at 7.9 s against 5 ms for none of it.
const modulePath = "github.com/sorglos-it/nextgencloud/"

// find is one place the guard refuses. sql tells the two guards apart.
type find struct {
	sql bool
	pos token.Position
	msg string
}

// pkgSource is one package, parsed and type-checked.
type pkgSource struct {
	fset  *token.FileSet
	files []*ast.File
	info  *types.Info
	pkg   *types.Package
}

// callAt is one call together with the functions it stands in, innermost
// last.
type callAt struct {
	call *ast.CallExpr
	in   []ast.Node
}

// atDB is a call that gives the database a statement, and where the statement
// stands in it. A helper of this package may carry two, as put does.
type atDB struct {
	callAt
	stmt []int
}

// ownModule resolves the packages of this module from source and nothing
// else.
type ownModule struct {
	root string
	fset *token.FileSet
	done map[string]*types.Package
}

func (m *ownModule) Import(path string) (*types.Package, error) {
	if pkg, ok := m.done[path]; ok {
		return pkg, nil
	}
	if !strings.HasPrefix(path, modulePath) {
		return nil, fmt.Errorf("sqlcheck: %s is outside this module", path)
	}
	dir := filepath.Join(m.root, filepath.FromSlash(strings.TrimPrefix(path, modulePath)))
	files, name, err := parseOwn(m.fset, dir)
	if err != nil {
		return nil, err
	}
	conf := types.Config{Importer: m, Error: func(error) {}, DisableUnusedImportCheck: true}
	pkg, _ := conf.Check(name, m.fset, files, nil)
	m.done[path] = pkg
	return pkg, nil
}

// parseOwn reads the source files of one package, without its tests: a test
// may write any SQL it likes, only the program is held to the rule.
func parseOwn(fset *token.FileSet, dir string) ([]*ast.File, string, error) {
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, "", err
	}
	var files []*ast.File
	name := ""
	for pkgName, pkg := range pkgs {
		if strings.HasSuffix(pkgName, "_test") {
			continue
		}
		for fileName, file := range pkg.Files {
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}
			files = append(files, file)
			name = pkgName
		}
	}
	if len(files) == 0 {
		return nil, "", fmt.Errorf("sqlcheck: %s holds no source of its own", dir)
	}
	// The order of a map is not the order of a message.
	slices.SortFunc(files, func(a, b *ast.File) int {
		return strings.Compare(fset.Position(a.Pos()).Filename, fset.Position(b.Pos()).Filename)
	})
	return files, name, nil
}

// readPackage parses and type-checks the package in dir.
func readPackage(t *testing.T, dir string) *pkgSource {
	t.Helper()
	fset := token.NewFileSet()
	files, name, err := parseOwn(fset, dir)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	conf := types.Config{
		Importer:                 &ownModule{root: moduleRoot(t), fset: fset, done: map[string]*types.Package{}},
		Error:                    func(error) {},
		DisableUnusedImportCheck: true,
	}
	pkg, _ := conf.Check(name, fset, files, info)
	if pkg == nil {
		t.Fatalf("%s could not be read at all", dir)
	}
	return &pkgSource{fset: fset, files: files, info: info, pkg: pkg}
}

// moduleRoot is the directory that holds go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("sqlcheck: no go.mod stands above this package")
		}
		dir = up
	}
}

// calls are every call of the package, each with the functions it stands in,
// and callee holds the identifier that names the function of a call. Whole
// files are read: a call in a function literal at a package var counts like
// any other.
func (p *pkgSource) calls() ([]callAt, map[*ast.Ident]bool) {
	var out []callAt
	callee := map[*ast.Ident]bool{}
	for _, file := range p.files {
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var in []ast.Node
			for _, up := range stack {
				switch up.(type) {
				case *ast.FuncDecl, *ast.FuncLit:
					in = append(in, up)
				}
			}
			out = append(out, callAt{call: call, in: in})
			if id := calleeIdent(call); id != nil {
				callee[id] = true
			}
			return true
		})
	}
	return out, callee
}

// calleeIdent is the identifier that names the function of a call, be it
// plain or a method.
func calleeIdent(call *ast.CallExpr) *ast.Ident {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f
	case *ast.SelectorExpr:
		return f.Sel
	}
	return nil
}

// guard reads one package and reports every statement that is not a constant
// and every value that reaches the database as a byte slice from anywhere but
// blob. It also says how many database calls and how many values it measured:
// a guard that measured nothing says nothing.
func guard(p *pkgSource) (finds []find, calls, values int) {
	all, callee := p.calls()
	report := func(sql bool, pos token.Pos, format string, a ...any) {
		finds = append(finds, find{sql: sql, pos: p.fset.Position(pos), msg: fmt.Sprintf(format, a...)})
	}
	// handed holds the functions of this package that give one of their own
	// parameters to the driver, and which parameter that is.
	handed := map[*types.Func][]int{}
	var given []atDB
	for _, c := range all {
		sel, ok := c.call.Fun.(*ast.SelectorExpr)
		if !ok || !dbMethods[sel.Sel.Name] {
			continue
		}
		at := stmtPlace(sel.Sel.Name)
		if at >= len(c.call.Args) {
			report(true, c.call.Pos(), "%s is called without a statement", sel.Sel.Name)
			continue
		}
		calls++
		given = append(given, atDB{callAt: c, stmt: []int{at}})
		p.checkStmt(report, c, c.call.Args[at], handed)
	}
	// Every call of a function that hands its parameter to the driver is
	// asked the same question. A use of such a function that is not a call is
	// refused: taken as a value it is run where the guard cannot follow.
	for _, fn := range slices.SortedFunc(maps(handed), func(a, b *types.Func) int {
		return strings.Compare(a.Name(), b.Name())
	}) {
		places := handed[fn]
		sites := 0
		for _, c := range all {
			id := calleeIdent(c.call)
			if id == nil || p.info.Uses[id] != types.Object(fn) {
				continue
			}
			sites++
			for _, at := range places {
				if at >= len(c.call.Args) {
					report(true, c.call.Pos(), "%s is called without the statement", fn.Name())
					continue
				}
				if !p.constString(c.call.Args[at]) {
					report(true, c.call.Args[at].Pos(), "%s is given a statement that is no constant", fn.Name())
				}
			}
			given = append(given, atDB{callAt: c, stmt: places})
		}
		if sites == 0 {
			report(true, fn.Pos(), "%s hands its argument to the driver, and nobody calls it", fn.Name())
		}
		for _, file := range p.files {
			ast.Inspect(file, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || p.info.Uses[id] != types.Object(fn) || callee[id] {
					return true
				}
				report(true, id.Pos(), "%s is taken as a value here, and it hands its argument to the driver; "+
					"what it is given then stands nowhere the guard can read", fn.Name())
				return true
			})
		}
	}
	// Every value that goes with a statement.
	for _, g := range given {
		first := slices.Min(g.stmt)
		for i := first + 1; i < len(g.call.Args); i++ {
			if slices.Contains(g.stmt, i) {
				continue
			}
			arg := g.call.Args[i]
			if g.call.Ellipsis != token.NoPos && i == len(g.call.Args)-1 && p.forwards(arg, g.in, handed) {
				continue // the caller's values, measured at the caller
			}
			for _, v := range flatten(arg) {
				values++
				p.checkValue(report, v)
			}
		}
	}
	slices.SortFunc(finds, func(a, b find) int {
		if c := strings.Compare(a.pos.Filename, b.pos.Filename); c != 0 {
			return c
		}
		return a.pos.Offset - b.pos.Offset
	})
	return finds, calls, values
}

// maps gives the keys of a map as a sequence, so the report comes out in the
// same order every run.
func maps(m map[*types.Func][]int) func(func(*types.Func) bool) {
	return func(yield func(*types.Func) bool) {
		for fn := range m {
			if !yield(fn) {
				return
			}
		}
	}
}

// checkStmt refuses anything but a constant. A constant expression is one
// go/types has a value for while it reads the source, so nothing a run puts
// together can be one - and that holds however the expression is named. A
// parameter is not refused; it is followed to the call sites of its function
// instead, because a helper is exactly where a statement built at run time
// would be handed in.
func (p *pkgSource) checkStmt(report func(bool, token.Pos, string, ...any), c callAt, arg ast.Expr, handed map[*types.Func][]int) {
	if p.constString(arg) {
		return
	}
	id, ok := arg.(*ast.Ident)
	if !ok {
		report(true, arg.Pos(), "the statement is put together at run time")
		return
	}
	v, ok := p.info.Uses[id].(*types.Var)
	if !ok {
		report(true, arg.Pos(), "the statement is %s, and that is no constant of this package", id.Name)
		return
	}
	fn, at, lit := p.paramOf(v, c.in)
	switch {
	case lit:
		report(true, arg.Pos(), "the statement is a parameter of a function literal, and a literal has no call sites the guard could read")
	case fn == nil:
		report(true, arg.Pos(), "the statement is the variable %s, and that is no constant of this package", id.Name)
	case !slices.Contains(handed[fn], at):
		handed[fn] = append(handed[fn], at)
	}
}

// constString reports whether an expression is a string constant. go/types
// works its value out while it reads the source, so no value of a run can be
// part of it; a name that stands for a variable never has one, however the
// variable is called.
func (p *pkgSource) constString(e ast.Expr) bool {
	tv, ok := p.info.Types[e]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String
}

// paramOf says which function around the call has v as its parameter, and in
// which place, counted the way a call writes them. A parameter of a function
// literal is reported as such.
func (p *pkgSource) paramOf(v *types.Var, in []ast.Node) (fn *types.Func, at int, lit bool) {
	for i := len(in) - 1; i >= 0; i-- {
		var sig *types.Signature
		var obj *types.Func
		switch f := in[i].(type) {
		case *ast.FuncDecl:
			obj, _ = p.info.Defs[f.Name].(*types.Func)
			if obj == nil {
				continue
			}
			sig, _ = obj.Type().(*types.Signature)
		case *ast.FuncLit:
			sig, _ = p.info.Types[f].Type.(*types.Signature)
		}
		if sig == nil {
			continue
		}
		for j := range sig.Params().Len() {
			if sig.Params().At(j) == v {
				return obj, j, obj == nil
			}
		}
	}
	return nil, 0, false
}

// forwards reports whether the value spread at a call is the caller's own
// variadic parameter, handed on by a function whose call sites are measured
// anyway.
func (p *pkgSource) forwards(e ast.Expr, in []ast.Node, handed map[*types.Func][]int) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := p.info.Uses[id].(*types.Var)
	if !ok {
		return false
	}
	fn, _, _ := p.paramOf(v, in)
	return fn != nil && handed[fn] != nil
}

// flatten opens a []any{...} written at a call: the values of the store stand
// in one of those.
func flatten(e ast.Expr) []ast.Expr {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return []ast.Expr{e}
	}
	return lit.Elts
}

// checkValue refuses every value that goes to the database as a byte slice
// from anywhere but blob or a conversion of a string. A nil byte slice binds
// as NULL on MariaDB, where every column is NOT NULL; the SQLite driver of
// this project writes an empty blob for it instead, so a forgotten blob can
// go red nowhere but here.
func (p *pkgSource) checkValue(report func(bool, token.Pos, string, ...any), v ast.Expr) {
	t := p.info.Types[v].Type
	if t == nil {
		report(false, v.Pos(), "the guard cannot tell what %s is", types.ExprString(v))
		return
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Invalid:
			report(false, v.Pos(), "the guard cannot tell what %s is", types.ExprString(v))
		case types.UntypedNil:
			report(false, v.Pos(), "%s is nil, and every column is NOT NULL", types.ExprString(v))
		}
		return
	case *types.Slice:
		b, ok := u.Elem().Underlying().(*types.Basic)
		if !ok || b.Kind() != types.Uint8 {
			break
		}
		if p.fromBlob(v) || p.fromString(v) {
			return
		}
		report(false, v.Pos(), "%s goes to the database as a byte slice that neither blob nor a conversion "+
			"of a string made; a nil one binds as NULL, and every column is NOT NULL", types.ExprString(v))
		return
	}
	report(false, v.Pos(), "%s is no value this store gives the database: a number, a bool, a string, "+
		"or a byte slice out of blob or a conversion of a string", types.ExprString(v))
}

// fromBlob reports whether the value is blob(...) - the function of this
// package, not one that happens to be called that.
func (p *pkgSource) fromBlob(v ast.Expr) bool {
	call, ok := v.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	obj := p.info.Uses[id]
	return obj != nil && obj == p.pkg.Scope().Lookup("blob")
}

// fromString reports whether the value is a conversion of a string into
// bytes. Such a conversion never gives nil.
func (p *pkgSource) fromString(v ast.Expr) bool {
	call, ok := v.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !p.info.Types[call.Fun].IsType() {
		return false
	}
	t := p.info.Types[call.Args[0]].Type
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// TestEverySQLIsAConstant: every statement this package gives the database is
// a constant, at the call to the driver and at every call site of the two
// helpers that take one as a parameter.
func TestEverySQLIsAConstant(t *testing.T) {
	finds, calls, _ := guard(readPackage(t, "."))
	for _, f := range finds {
		if f.sql {
			t.Errorf("%s: %s", f.pos, f.msg)
		}
	}
	// A test that measured nothing says nothing: the store speaks to the
	// database in more places than this, and if none of them is found here,
	// the reading above has gone past them.
	if calls < 5 {
		t.Errorf("only %d calls to the database were measured", calls)
	}
}

// TestEveryByteSliceComesFromBlob: every value that goes with a statement is
// a number, a string turned into bytes, or a byte slice out of blob. This is
// the one place it can be measured - see checkValue - and tasks 5 to 7 bring
// five more nillable fields: User.TOTP, Session.Cert, Proof, APIKey, APICert
// and Pending.Key.
func TestEveryByteSliceComesFromBlob(t *testing.T) {
	finds, _, values := guard(readPackage(t, "."))
	for _, f := range finds {
		if !f.sql {
			t.Errorf("%s: %s", f.pos, f.msg)
		}
	}
	if values < 15 {
		t.Errorf("only %d values given to the database were measured", values)
	}
}

// TestTheGuardCatchesWhatSlippedPast runs the guard against five small
// packages under testdata: one written the way this store writes, and four
// that are that one with a single hole cut into it. shadow, literal and
// rawbytes were measured green against the guard this one replaces, handoff
// is the hole this one would have had. The clean one has to stay silent -
// four holes found by a guard that is red on everything would prove nothing.
func TestTheGuardCatchesWhatSlippedPast(t *testing.T) {
	for _, c := range []struct{ dir, want string }{
		{"clean", ""},
		{"shadow", "the statement is the variable blobDelete"},
		{"literal", "the statement is the variable stmt"},
		{"handoff", "exec is taken as a value"},
		{"rawbytes", "data goes to the database as a byte slice"},
	} {
		t.Run(c.dir, func(t *testing.T) {
			finds, calls, values := guard(readPackage(t, filepath.Join("testdata", c.dir)))
			if calls < 5 || values < 10 {
				t.Fatalf("the package under test is too small to say anything: %d calls, %d values", calls, values)
			}
			if c.want == "" {
				for _, f := range finds {
					t.Errorf("%s: %s", f.pos, f.msg)
				}
				return
			}
			for _, f := range finds {
				if strings.Contains(f.msg, c.want) {
					return
				}
			}
			t.Errorf("the hole was not found; the guard said: %v", finds)
		})
	}
}
