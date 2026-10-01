package sqlstore

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The guard of this file reads the package's own source and refuses five
// things: a statement that is not a constant, a value that reaches the
// database as a byte slice from anywhere but blob, a way of reaching the
// driver that it cannot follow, the import of reflect, through which a method
// carries its name as a string and no reading of identifiers sees it, and a
// OnceUse that asks the database about the value before it writes the mark.
// That is how "SQL only with placeholders", "nothing nil where every column
// is NOT NULL" and "the one-time value is spent by the write that hands it
// out" are kept, instead of trusting everyone to remember them.
//
// It asks go/types what a name stands for, never what it is called: a local
// variable may carry the name of a constant, and only the object behind the
// name says which of the two is meant. It reads whole files, not only the
// bodies of function declarations. What it cannot resolve it reports, so a
// hole comes out as a failure and not as a pass.
//
// It reads exactly one package. TestNobodyElseUnderServerDBSpeaksToADatabase
// draws the line around that: no other package under apps/server-db touches a
// database without a guard of its own.
//
// TestTheGuardCatchesWhatSlippedPast below runs it against every package
// under testdata/: some are written the way this store writes and have to
// stay silent, the rest carry one hole each, and every one of those holes was
// measured green against an earlier guard. Which packages those are stands in
// one table there, and that table is compared against the directory.

// dbMethods are the ways a statement or a value reaches the driver.
var dbMethods = map[string]bool{
	"Exec": true, "ExecContext": true,
	"Query": true, "QueryContext": true,
	"QueryRow": true, "QueryRowContext": true,
	"Prepare": true, "PrepareContext": true,
}

// skipCtx is how many arguments of such a call stand in front of the
// statement: one when a context comes first, which is what Context in the
// name says. It is read off the name of the method, never off the name of a
// variable.
func skipCtx(method string) int {
	if strings.HasSuffix(method, "Context") {
		return 1
	}
	return 0
}

// modulePath is the import path of this module. Packages under it are read
// from source, everything else is left unresolved on purpose: every name this
// guard asks about is declared in this module, and reading the standard
// library from source as well was measured at 7.9 s against 5 ms for none of
// it.
const modulePath = "github.com/sorglos-it/nextgencloud/"

// find is one place the guard refuses. sql tells the two halves apart: the
// statement, or the values that go with it. atomic is the third question,
// about the order in which one method speaks, and it is reported on its own
// because it is neither of those two.
type find struct {
	sql    bool
	atomic bool
	pos    token.Position
	msg    string
}

func (f find) String() string { return f.pos.String() + ": " + f.msg }

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
	files, err := parseOwn(m.fset, dir)
	if err != nil {
		return nil, err
	}
	conf := types.Config{Importer: m, Error: func(error) {}, DisableUnusedImportCheck: true}
	// Checked under its import path and not under the name in its files, so a
	// guard can tell two packages of the same name apart. readPackage does the
	// same with importPath, which is this line read backwards, and parseOwn
	// gives no name back to either of them.
	pkg, _ := conf.Check(path, m.fset, files, nil)
	m.done[path] = pkg
	return pkg, nil
}

// parseOwn reads the source files of one package, without its tests: a test
// may write any SQL it likes, only the program is held to the rule. It gives
// no package name back: everything here checks a package under its import
// path, and the name in the files would say no more than that the directory
// holds a package at all.
func parseOwn(fset *token.FileSet, dir string) ([]*ast.File, error) {
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for pkgName, pkg := range pkgs {
		if strings.HasSuffix(pkgName, "_test") {
			continue
		}
		for fileName, file := range pkg.Files {
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("sqlcheck: %s holds no source of its own", dir)
	}
	// The order of a map is not the order of a message.
	slices.SortFunc(files, func(a, b *ast.File) int {
		return strings.Compare(fset.Position(a.Pos()).Filename, fset.Position(b.Pos()).Filename)
	})
	return files, nil
}

// readPackage parses and type-checks the package in dir, under its import
// path and not under the name in its files - the same way ownModule.Import
// checks the packages it resolves, and for the same reason.
func readPackage(t *testing.T, dir string) *pkgSource {
	t.Helper()
	fset := token.NewFileSet()
	files, err := parseOwn(fset, dir)
	if err != nil {
		t.Fatal(err)
	}
	root := moduleRoot(t)
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	conf := types.Config{
		Importer:                 &ownModule{root: root, fset: fset, done: map[string]*types.Package{}},
		Error:                    func(error) {},
		DisableUnusedImportCheck: true,
	}
	pkg, _ := conf.Check(importPath(t, root, dir), fset, files, info)
	if pkg == nil {
		t.Fatalf("%s could not be read at all", dir)
	}
	return &pkgSource{fset: fset, files: files, info: info, pkg: pkg}
}

// importPath is the path a directory of this module is imported under. It is
// the way back from ownModule.Import, which turns a path into a directory.
func importPath(t *testing.T, root, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		t.Fatal(err)
	}
	return modulePath + filepath.ToSlash(rel)
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

// at is one function of the package together with one of its arguments.
type at struct {
	fn *types.Func
	i  int
}

// where is a value and the functions it was written in, so that a parameter
// found in it can be traced to the right call sites.
type where struct {
	expr ast.Expr
	in   []ast.Node
}

// checker holds one run of the guard.
type checker struct {
	p      *pkgSource
	all    []callAt
	callee map[*ast.Ident]bool
	decl   map[*types.Func]*ast.FuncDecl
	made   map[*types.Var][]where // where a variable was given its value
	prep   map[*types.Var]bool    // variables holding a prepared statement
	finds  []find
	calls  int
	values int
	stmt   map[at]bool
	vals   map[at]bool
	asked  map[*types.Func]bool
	self   types.Type // the store whose OnceUse is being read; it is never the driver
}

// receiverOf is the type a method is declared on, or nil for a function.
func (c *checker) receiverOf(fn *ast.FuncDecl) types.Type {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return nil
	}
	return c.p.info.Types[fn.Recv.List[0].Type].Type
}

func (c *checker) report(sql bool, pos token.Pos, format string, a ...any) {
	c.finds = append(c.finds, find{sql: sql, pos: c.p.fset.Position(pos), msg: fmt.Sprintf(format, a...)})
}

func (c *checker) reportAtomic(pos token.Pos, format string, a ...any) {
	c.finds = append(c.finds, find{atomic: true, pos: c.p.fset.Position(pos), msg: fmt.Sprintf(format, a...)})
}

// read walks the package once and collects everything the guard needs: every
// call with the functions around it, which identifiers stand in call
// position, the declaration of every function, where each variable was given
// its value, and which variables hold a prepared statement. Whole files are
// read, so a call in a function literal at a package var counts like any
// other.
func (c *checker) read() {
	for _, file := range c.p.files {
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			stack = append(stack, n)
			var in []ast.Node
			for _, up := range stack {
				switch up.(type) {
				case *ast.FuncDecl, *ast.FuncLit:
					in = append(in, up)
				}
			}
			switch x := n.(type) {
			case *ast.CallExpr:
				c.all = append(c.all, callAt{call: x, in: in})
				if id := calleeIdent(x); id != nil {
					c.callee[id] = true
				}
			case *ast.FuncDecl:
				if fn, ok := c.p.info.Defs[x.Name].(*types.Func); ok {
					c.decl[fn] = x
				}
			case *ast.AssignStmt:
				c.made1(x.Lhs, x.Rhs, in)
			case *ast.ValueSpec:
				c.made1(idents(x.Names), x.Values, in)
			}
			return true
		})
	}
}

// made1 writes down where the variables on the left were given their value.
// A variable written in more than one place is written down more than once,
// and the guard then says so instead of reading the first of them.
//
// An assignment to a part of a variable - vals[1] = ... - counts as a write
// of the whole variable: it stands nowhere the reading of the row could pick
// it up, so the variable becomes unreadable by it, the way append already
// makes it unreadable. Only an assignment counts. copy into a part of the row,
// a write through a second name for the same row, and a helper that writes
// into a row it was handed are writes as well and are **not** counted here -
// following them would need a reading this guard does not have.
func (c *checker) made1(lhs []ast.Expr, rhs []ast.Expr, in []ast.Node) {
	if call, ok := single(rhs); ok {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isPrepare(sel.Sel.Name) {
			if v, ok := c.varOf(lhs[0]); ok {
				c.prep[v] = true
			}
		}
	}
	for _, l := range lhs {
		if _, ok := l.(*ast.Ident); ok {
			continue
		}
		if v, ok := c.varOf(rootOf(l)); ok {
			c.made[v] = append(c.made[v], where{expr: l, in: in})
		}
	}
	if len(lhs) != len(rhs) {
		return
	}
	for i, l := range lhs {
		if v, ok := c.varOf(l); ok {
			c.made[v] = append(c.made[v], where{expr: rhs[i], in: in})
		}
	}
}

// isPrepare names the two methods of database/sql that make a prepared
// statement. The two names stand here written out because a method called
// PrepareAnything gives back whatever it likes, and taking its result for a
// prepared statement would take the statement argument off the next call.
// Whose method it is stays unchecked: a Prepare or PrepareContext of a
// foreign type that gives back something else would still be believed.
func isPrepare(name string) bool { return name == "Prepare" || name == "PrepareContext" }

// rootOf is the name a write goes into, with the indexing, the parentheses,
// the pointer, the field and the type assertion around it taken off: vals of
// vals[1] and of (vals)[1], p of (*p)[1], s of s.row[1], v of v.([]any)[1]. Where a second name stands between
// the write and the row, the name that comes out is that second one and the
// row itself stays readable - the same limit the comment at made1 names. A
// slice expression is not in the list: vals[1:2] = ... is no Go, a slice
// expression is never addressable.
func rootOf(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			e = x.X
		case *ast.TypeAssertExpr:
			e = x.X
		default:
			return e
		}
	}
}

func (c *checker) varOf(e ast.Expr) (*types.Var, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if v, ok := c.p.info.Defs[id].(*types.Var); ok {
		return v, true
	}
	v, ok := c.p.info.Uses[id].(*types.Var)
	return v, ok
}

func idents(names []*ast.Ident) []ast.Expr {
	out := make([]ast.Expr, len(names))
	for i, n := range names {
		out[i] = n
	}
	return out
}

func single(rhs []ast.Expr) (*ast.CallExpr, bool) {
	if len(rhs) != 1 {
		return nil, false
	}
	call, ok := rhs[0].(*ast.CallExpr)
	return call, ok
}

// guard reads one package and reports what it refuses. It also says how many
// database calls and how many values it measured: a guard that measured
// nothing says nothing.
func guard(p *pkgSource) (finds []find, calls, values int) {
	c := &checker{
		p: p, callee: map[*ast.Ident]bool{}, decl: map[*types.Func]*ast.FuncDecl{},
		made: map[*types.Var][]where{}, prep: map[*types.Var]bool{},
		stmt: map[at]bool{}, vals: map[at]bool{}, asked: map[*types.Func]bool{},
	}
	c.read()
	c.noMethodValues()
	c.noReflect()
	c.onceUseWritesFirst()
	for _, cl := range c.all {
		sel, ok := cl.call.Fun.(*ast.SelectorExpr)
		if !ok || !dbMethods[sel.Sel.Name] {
			continue
		}
		c.calls++
		skip := skipCtx(sel.Sel.Name)
		i := c.stmtIndex(cl.call, sel, skip)
		if i >= 0 {
			if i >= len(cl.call.Args) {
				c.report(true, cl.call.Pos(), "%s is called without a statement", sel.Sel.Name)
				continue
			}
			c.checkStmt(cl.call.Args[i], cl.in)
		}
		for j := skip; j < len(cl.call.Args); j++ {
			if j != i {
				c.scanValue(cl.call.Args[j], cl.in)
			}
		}
	}
	slices.SortFunc(c.finds, func(a, b find) int {
		if n := strings.Compare(a.pos.Filename, b.pos.Filename); n != 0 {
			return n
		}
		return a.pos.Offset - b.pos.Offset
	})
	return c.finds, c.calls, c.values
}

// noMethodValues refuses a way to the driver that no reading of arguments can
// follow: the method itself taken as a value. e := s.db.ExecContext and then
// e(ctx, built, raw) hands over a statement and its values at a call the
// guard cannot recognise as one.
//
// Which name is meant is decided on the object, not on the name alone: a
// field, a variable or a constant of this module that happens to be called
// Query is none of the driver's business and stays silent. Refused is a name
// of dbMethods that stands outside call position and is either a method - the
// shape a method value has - or something this guard cannot resolve at all.
//
// Unresolved is everything outside this module: ownModule reads this module's
// packages from source and nothing else, and the files of this package import
// fourteen foreign packages, the driver among them. So the second branch is
// wider than the driver, and a data field of a foreign type carrying one of
// these eight names would be a false alarm. It is the strict direction on
// purpose; measured in this package today: none.
func (c *checker) noMethodValues() {
	for _, file := range c.p.files {
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || !dbMethods[id.Name] || c.callee[id] || c.p.info.Defs[id] != nil {
				return true
			}
			obj, known := c.p.info.Uses[id]
			if known {
				if _, method := obj.(*types.Func); !method {
					return true // a field, a variable, a constant: not the driver
				}
			}
			c.report(true, id.Pos(), "%s is taken as a value here; what it is then given "+
				"stands nowhere the guard can read", id.Name)
			return true
		})
	}
}

// noReflect refuses reflect in this package. A name reached through reflect
// is a string, so no reading of identifiers can see it: the whole of the
// guard above would be silent on it. This is a ban, not a measurement of
// need. The same ban on every program file under apps/server-db outside
// testdata/ and vendor/ stands in TestNobodyElseUnderServerDBSpeaksToADatabase;
// testdata/ is left out of it on purpose, because one package there imports
// reflect as its hole.
func (c *checker) noReflect() {
	for _, file := range c.p.files {
		for _, imp := range file.Imports {
			if imp.Path.Value == `"reflect"` {
				c.report(true, imp.Pos(), "this package imports reflect; a method called through it "+
					"carries its name as a string, and the guard of this file reads identifiers")
			}
		}
	}
}

// writeVerbs open a statement that changes rows.
var writeVerbs = map[string]bool{"UPDATE": true, "INSERT": true, "DELETE": true, "REPLACE": true}

// mustWrite says whether a statement changes rows. It is asked only of what
// stands where a statement stands at a call to the driver, never of a string
// found somewhere in a body: a message may open with the word select, and an
// earlier reading of every expression called limits.go a statement for
// exactly that reason. Anything it cannot read as a write counts as a read,
// which is the strict direction.
//
// One that opens with WITH is decided on its words and not on its first one:
// a common table expression may end in an UPDATE just as well as in a SELECT,
// and only the whole of it tells the two apart. Whole words are compared, so
// a column called updated is no UPDATE.
func mustWrite(p *pkgSource, e ast.Expr) bool {
	tv, ok := p.info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	w := strings.Fields(strings.ToUpper(constant.StringVal(tv.Value)))
	if len(w) == 0 {
		return false
	}
	if writeVerbs[w[0]] {
		return true
	}
	if w[0] == "WITH" {
		for _, x := range w[1:] {
			if writeVerbs[x] {
				return true
			}
		}
	}
	return false
}

// ownFunc is the function a call names, when this module declares it.
func (c *checker) ownFunc(call *ast.CallExpr) *types.Func {
	id := calleeIdent(call)
	if id == nil {
		return nil
	}
	fn, _ := c.p.info.Uses[id].(*types.Func)
	return fn
}

// litOf is the function literal a called name stands for, when the name was
// given one in exactly one place. More than one place and the guard says
// nothing: it cannot know which of them runs.
func (c *checker) litOf(call *ast.CallExpr) *ast.FuncLit {
	v, ok := c.varOf(call.Fun)
	if !ok {
		return nil
	}
	ws := c.made[v]
	if len(ws) != 1 {
		return nil
	}
	lit, _ := ws[0].expr.(*ast.FuncLit)
	return lit
}

// argLits are the literals that run where they stand: one handed to a call as
// an argument, and one called on the spot. A literal given a name is meant to
// be read where the name is called instead, and litOf is what reaches it -
// but only where the name was written in exactly one place. Written in two,
// litOf gives up and no one else looks, so a read in such a literal, called
// before the write, goes through. The comment at litOf names that limit; here
// it is what this way of reading costs. Whether a function runs the literal
// it is handed is its own business and is not read here; taking it for one
// that runs is the strict direction, and litarg is the hole that rests on it.
// What that direction costs is a right store refused: one that hands the read
// to a collector before the write and runs the collected functions only after
// it. The read is marked where it is handed over, and the store comes out
// wrong. Both prices stand beside the rule.
//
// Under a defer or a go that direction is the wrong one, and only a literal
// called on the spot counts. What such a call settles there is the function
// it keeps for later, so a literal handed over inside it is taken for that
// later body. What the call it goes to does with it is not read: a helper
// that hands it back for the defer to call and one that runs it where it
// stands are the same shape here, and both are left out. A deferred body
// runs after the whole method and is no statement in front of the write; a
// started one is not ordered at all, and leaving that one out is the gap the
// note beside the rule names. deferwrap is the right store that was refused
// for this, deferlit the hole that stays caught, because a literal called on
// the spot does speak at the defer. What it costs is a helper that runs the
// literal on the spot and hands back an empty function: that read really is
// the first the method gives the database, and it goes through. The note
// beside the rule names that shape too. A defer or a go is reached through
// onTheSpot, the same way firstStmtIn reads the two.
//
// A literal called on the spot inside such a later body is marked as well,
// and nothing reads it: to reach one, firstStmtIn would have to step into the
// literal the defer hands on, and that one is never marked.
func argLits(n ast.Node) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	held := false // inside what a defer or a go hands on to be called later
	var visit func(ast.Node) bool
	later := func(call *ast.CallExpr) bool {
		was := held
		held = true
		onTheSpot(call, visit)
		held = was
		return false
	}
	visit = func(x ast.Node) bool {
		switch s := x.(type) {
		case *ast.DeferStmt:
			return later(s.Call)
		case *ast.GoStmt:
			return later(s.Call)
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !held {
			for _, a := range call.Args {
				if lit, ok := a.(*ast.FuncLit); ok {
					out[lit] = true
				}
			}
		}
		if lit, ok := call.Fun.(*ast.FuncLit); ok {
			out[lit] = true
		}
		return true
	}
	ast.Inspect(n, visit)
	return out
}

// bound is what the parameters of the function being read stand for at the
// call that led into it, already resolved. Without it a helper that is handed
// its statement would carry no statement the guard can see.
type bound map[*types.Var]ast.Expr

// resolve follows a name through the parameters it was bound to, so that the
// expression handed back is the one the caller really wrote. It gives up on a
// name that is written to anywhere: a parameter a helper overwrites with a
// statement of its own still leads back to the caller's, and that one is then
// the wrong answer. Giving up here means the place is reported, which is what
// the head of this file asks for - a hole comes out as a failure.
func (c *checker) resolve(e ast.Expr, b bound) (ast.Expr, bool) {
	for range 8 { // a chain of helpers, not a cycle
		v, ok := c.varOf(e)
		if !ok {
			return e, true // a constant, or something with no name of its own
		}
		if len(c.made[v]) > 0 {
			return e, false // written somewhere; which value stands here is not said
		}
		next, ok := b[v]
		if !ok || next == e {
			return e, false // a name the guard did not follow into this body
		}
		e = next
	}
	return e, false
}

// bind maps the parameters of fn to the arguments of call, each of them
// resolved in the binding the call itself stands in. A variadic tail is left
// out: every helper here takes its statement in a place of its own, and one
// handed in the tail would be reported where it reaches the driver, the same
// as any statement the guard cannot follow.
func (c *checker) bind(fn *types.Func, call *ast.CallExpr, b bound) bound {
	out := bound{}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return out
	}
	ps := sig.Params()
	n := ps.Len()
	if sig.Variadic() {
		n--
	}
	for i := range n {
		if i >= len(call.Args) {
			break
		}
		if e, ok := c.resolve(call.Args[i], b); ok {
			out[ps.At(i)] = e
		}
	}
	return out
}

// atDriver says whether this call hands a statement to the driver. The name
// alone does not say it: a method of the store itself may be called Query
// without any database behind it, and reading the name on its own made the
// rule red on such a package. The receiver decides - the store never is the
// driver, whatever its methods are called. A receiver go/types could not
// place is taken for the driver, which is the strict direction.
func (c *checker) atDriver(sel *ast.SelectorExpr) bool {
	if !dbMethods[sel.Sel.Name] {
		return false
	}
	t := c.p.info.Types[sel.X].Type
	return t == nil || !sameNamed(t, c.self)
}

// onTheSpot reads what a deferred or started call works out where it stands:
// the expression that names the function, and the arguments. Go settles both
// at the defer or the go and keeps only the call for later, so a statement in
// either runs before everything below. defer s.trace()() is the everyday
// shape of that, and defer closers[s.pick()]() another.
//
// Nothing is skipped here. What is finally called is left out a step later:
// firstStmtIn walks into a function literal standing in a defer or a go only
// where argLits marks it, and argLits marks no literal that one of the two
// hands on. The note beside the rule says what that leaves uncovered. A
// literal written straight into the defer was skipped here once as well;
// taking that skip out left all twenty-five packages as they were, because
// such a literal is dropped a step later either way.
func onTheSpot(call *ast.CallExpr, visit func(ast.Node) bool) {
	ast.Inspect(call.Fun, visit)
	for _, a := range call.Args {
		ast.Inspect(a, visit)
	}
}

// sameNamed compares two types without minding how many pointers stand in
// front of them.
func sameNamed(a, b types.Type) bool {
	if a == nil || b == nil {
		return false
	}
	for {
		p, ok := a.(*types.Pointer)
		if !ok {
			break
		}
		a = p.Elem()
	}
	for {
		p, ok := b.(*types.Pointer)
		if !ok {
			break
		}
		b = p.Elem()
	}
	return types.Identical(a, b)
}

// firstStmtIn is the first statement n gives the database, read in the order
// the source has it. Only what stands where a statement stands at a call of
// dbMethods is read - never a string found somewhere in a body, because a
// message may open with the word select and is no statement.
//
// A call of a function of this package, and a call of a name that holds a
// function literal, are read at the place they stand, so a statement moved
// into a helper counts where the helper is called and not where it is
// written; a statement handed to that helper as a parameter is followed
// through bind. Each is read once: a loop through the same helper adds
// nothing, and a cycle would not end.
//
// What stands at the statement and cannot be followed to a constant is
// reported, not passed over. An earlier reading passed it over, and a helper
// that overwrote the statement it was handed then came through both this rule
// and TestEverySQLIsAConstant - through the second one because what it gave
// the driver was a constant, only not the one it was handed.
//
// Of a defer or a go, what it settles where it stands is read there - the
// expression naming the function, and the arguments - and the body of what is
// finally called is left out: see the note on the rule.
type says int

const (
	saysNothing says = iota
	saysWrite
	saysRead
	saysUnknown
)

func (c *checker) firstStmtIn(n ast.Node, b bound, seen map[*types.Func]bool, seenLit map[*ast.FuncLit]bool) (verdict says, pos token.Pos) {
	here := argLits(n)
	var visit func(ast.Node) bool
	visit = func(x ast.Node) bool {
		if verdict != saysNothing || x == nil {
			return false
		}
		switch s := x.(type) {
		case *ast.DeferStmt:
			onTheSpot(s.Call, visit)
			return false
		case *ast.GoStmt:
			onTheSpot(s.Call, visit)
			return false
		}
		if lit, ok := x.(*ast.FuncLit); ok && !here[lit] {
			return false // it runs where its name is called
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && c.atDriver(sel) {
			i := c.stmtIndex(call, sel, skipCtx(sel.Sel.Name))
			if i >= 0 && i < len(call.Args) {
				pos = call.Args[i].Pos()
				stmt, ok := c.resolve(call.Args[i], b)
				switch {
				case !ok:
					verdict = saysUnknown
				case mustWrite(c.p, stmt):
					verdict = saysWrite
				default:
					verdict = saysRead
				}
				return false
			}
			return true
		}
		var body ast.Node
		next := bound{}
		if fn := c.ownFunc(call); fn != nil && !seen[fn] {
			seen[fn] = true
			if d := c.decl[fn]; d != nil && d.Body != nil {
				body, next = d.Body, c.bind(fn, call, b)
			}
		} else if lit := c.litOf(call); lit != nil && !seenLit[lit] {
			seenLit[lit] = true
			body = lit.Body
		}
		if body != nil {
			if v, p := c.firstStmtIn(body, next, seen, seenLit); v != saysNothing {
				verdict, pos = v, p
				return false
			}
		}
		return true
	}
	ast.Inspect(n, visit)
	return verdict, pos
}

// onceUseWritesFirst refuses a OnceUse that asks the database about the value
// before it writes the mark. What it hands out has to be decided by the write
// itself - one UPDATE that matches only a row still unused and still in time,
// and the count of rows it changed - because two callers that read the same
// unused row both hand the same one-time value out. A login taken twice and a
// task run twice are what that costs, so this is a rule and not a taste.
//
// A read after the write is what the method is for and says nothing here: the
// order alone is refused. UPDATE ... RETURNING passes as well, although it
// comes through QueryRow - it is the statement that is read, never the name
// of the method.
//
// LockTake is left out although it would pass this rule today: which owner
// holds a lock does not rest on the same property, and a rule that nails down
// a property nobody needs refuses an allowed change one day - and is then
// switched off, together with the half that was needed.
//
// Why from the source and not from a run: two steps without a transaction
// are caught by TestOnlyOneUseGetsTheValue, but only on a machine of two
// processors or more; two steps inside one transaction are caught by no run
// here at all, because SQLite's own transactions save such a store and
// MariaDB is in no run of this repository. Read from the source, both are
// caught on any machine, and neither answer flutters.
//
// What it does not hold, so that nobody takes it for more. Each of these was
// measured against a store built for it, none of them is a guess:
//
//   - It reads the order, not that the count decides. A OnceUse that writes
//     first and throws the row count away comes through it. Four other tests
//     of this package catch that one, TestOnlyOneUseGetsTheValue among them
//     even at a single processor, because such a store is not sometimes
//     wrong but always.
//   - A read through an interface method of this package comes through: the
//     method has no body here to read, the way Dialect has none. So does one
//     through a function held in a struct field, and one through a method
//     taken as a value - for those two nothing says which body runs. Only a
//     name of dbMethods taken as a value is refused, and that by
//     noMethodValues, not here.
//   - It holds the one method called OnceUse. A second, two-step OnceUseV2
//     beside a correct OnceUse passes. dbapi.Store keeps the name in place,
//     but it forbids no method beside it: what keeps a second one out today
//     is that nothing calls it, not this rule.
//   - Of a defer or a go, what it settles where it stands is read, and the
//     body of what is finally called is left out. Go settles the expression
//     naming the function and the arguments right there, so a statement in
//     either runs before everything under it - eight stores that hide the
//     read that way are refused, one with the call to the driver written out
//     in the argument, one behind defer s.trace()() and one behind an index.
//     What is left out runs later: a deferred body after the whole method,
//     so a statement in it is none in front of the write. Every function
//     literal handed over there is left out with it, and what the call it
//     goes to does with the literal is not read - so this too is a gap and
//     not a deduction. Where a helper hands the literal back for the defer
//     to call, leaving it out is right, and one right store that reads the
//     row that way stays silent, while a literal called on the spot in an
//     argument speaks at the defer and is read. Where the helper runs the
//     literal instead and hands back a function that does nothing, leaving
//     it out is wrong: the read is then the first statement the method gives
//     the database, and it comes through. Both shapes were refused
//     together, and one change turned both silent at once: without reading
//     the helper they cannot be told apart, so closing the false alarm on
//     the right store paid for it with this hole. That was measured on a
//     store built for it, and no package under testdata holds it, so
//     nothing here keeps it measured. A started one is not ordered at all
//     and may run before the write, so leaving that one out is a gap and
//     not a deduction. Uncovered with it: the order among several defers,
//     which Go runs backwards, and a OnceUse that speaks only from those
//     bodies - that one is refused for what it is, with the guard saying it
//     can find no statement rather than that the store is wrong. Both faces
//     of it were measured: the right one and the wrong one are refused
//     alike, on that ground.
//   - Outside a defer or a go, a function literal handed to a call as an
//     argument is read as if it ran at that call. Whether it does is the
//     called function's business and is not read, and the strict direction
//     costs a right store. One that hands the read to a collector before the
//     write, and runs the collected functions only after it, is refused all
//     the same: the guard names that read as the first statement. It was
//     measured on a store built for it, before the false alarm on the
//     deferred shape was closed and after, and refused both times - so that
//     change neither made it nor mended it. No package under testdata holds
//     it, so nothing here keeps it measured.
//   - A literal given a name is followed only where the name was written in
//     exactly one place. Written in two, litOf gives up: the guard cannot say
//     which of the two bodies runs, so it reads neither, and a read in such a
//     literal, called before the write, comes through without a word. This
//     was measured on a store built for it, silent before that same change
//     and after it. No package under testdata holds it.
//   - Preparing is not speaking. stmtIndex counts a PrepareContext as the
//     place a statement is given, so a prepared read counts where it is
//     prepared and not where it is run. Nothing in this package prepares a
//     statement at all, and in the two shapes measured for it the prepared
//     statement was the one run as well, so the two places fell together.
func (c *checker) onceUseWritesFirst() {
	for _, file := range c.p.files {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "OnceUse" || fn.Body == nil {
				continue
			}
			c.self = c.receiverOf(fn)
			verdict, pos := c.firstStmtIn(fn.Body, bound{}, map[*types.Func]bool{}, map[*ast.FuncLit]bool{})
			switch verdict {
			case saysNothing:
				c.reportAtomic(fn.Pos(), "OnceUse gives the database no statement the guard can find, "+
					"so nothing here says the value is spent by the write that hands it out")
			case saysRead:
				c.reportAtomic(pos, "the first statement it gives the database only reads, "+
					"so what OnceUse hands out was decided by that read and not by the write")
			case saysUnknown:
				c.reportAtomic(pos, "the first statement OnceUse gives the database cannot be "+
					"followed to the one written here, so nothing says it is the write that decides")
			}
		}
	}
}

// stmtIndex says where the statement of a database call stands, or -1 when
// the call carries none. A prepared statement carries none: sql.Stmt has
// Exec, Query and QueryRow without a statement of their own, and that is the
// textbook form for a row written many times. Two signs say so, and one is
// enough: the receiver is a variable that came out of Prepare or
// PrepareContext - those two names and no other - or the argument where a
// statement would stand is of a type that is no string.
func (c *checker) stmtIndex(call *ast.CallExpr, sel *ast.SelectorExpr, skip int) int {
	if isPrepare(sel.Sel.Name) {
		return skip // this one always carries a statement
	}
	if v, ok := c.varOf(sel.X); ok && c.prep[v] {
		return -1
	}
	if skip >= len(call.Args) {
		return -1
	}
	if t := c.p.info.Types[call.Args[skip]].Type; t != nil {
		b, ok := t.Underlying().(*types.Basic)
		if !ok || (b.Kind() != types.Invalid && b.Info()&types.IsString == 0) {
			return -1
		}
	}
	return skip
}

// checkStmt refuses anything but a constant. A constant expression is one
// go/types has a value for while it reads the source, so nothing a run puts
// together can be one - and that holds however the expression is named. A
// parameter is not refused; it is followed to the call sites of its function
// instead, and from there on outwards, because a helper is exactly where a
// statement built at run time would be handed in.
func (c *checker) checkStmt(arg ast.Expr, in []ast.Node) {
	if c.p.constString(arg) {
		return
	}
	id, ok := arg.(*ast.Ident)
	if !ok {
		c.report(true, arg.Pos(), "the statement is put together at run time")
		return
	}
	v, ok := c.p.info.Uses[id].(*types.Var)
	if !ok {
		c.report(true, arg.Pos(), "the statement is %s, and that is no constant", id.Name)
		return
	}
	fn, i, lit := c.p.paramOf(v, in)
	switch {
	case lit:
		c.report(true, arg.Pos(), "the statement is a parameter of a function literal, "+
			"and a literal has no call sites the guard could read")
	case fn == nil:
		c.report(true, arg.Pos(), "the statement is the variable %s, and that is no constant", id.Name)
	default:
		c.traceStmt(fn, i)
	}
}

// traceStmt asks the same question of every call of fn that was asked where
// fn hands its parameter to the driver. A use of fn that is not a call is
// refused: taken as a value it is run where the guard cannot see.
func (c *checker) traceStmt(fn *types.Func, i int) {
	if c.stmt[at{fn, i}] {
		return
	}
	c.stmt[at{fn, i}] = true
	sites := 0
	for _, cl := range c.all {
		id := calleeIdent(cl.call)
		if id == nil || c.p.info.Uses[id] != types.Object(fn) {
			continue
		}
		sites++
		if i >= len(cl.call.Args) {
			c.report(true, cl.call.Pos(), "%s is called without the statement", fn.Name())
			continue
		}
		c.checkStmt(cl.call.Args[i], cl.in)
	}
	if sites == 0 {
		c.report(true, fn.Pos(), "%s hands its argument to the driver, and nobody calls it", fn.Name())
	}
	c.noValueUse(fn)
}

// noValueUse refuses a function of this package that reaches the driver being
// taken as a value instead of called.
func (c *checker) noValueUse(fn *types.Func) {
	if c.asked[fn] {
		return
	}
	c.asked[fn] = true
	for _, file := range c.p.files {
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || c.p.info.Uses[id] != types.Object(fn) || c.callee[id] {
				return true
			}
			c.report(true, id.Pos(), "%s is taken as a value here, and it reaches the driver; "+
				"what it is given then stands nowhere the guard can read", fn.Name())
			return true
		})
	}
}

// scanValue opens up everything a row of values can be written as, and
// measures what is left at the end of it: a []any written out at the call, a
// []any in a variable of its own, a []any handed on as a parameter, and a
// []any a function of this package returns. All four are good code and all
// four are the near form for a row of ten columns, so all four are followed
// instead of refused.
func (c *checker) scanValue(e ast.Expr, in []ast.Node) {
	switch x := e.(type) {
	case *ast.CompositeLit:
		if c.anySlice(c.p.info.Types[x].Type) {
			for _, el := range x.Elts {
				c.scanValue(el, in)
			}
			return
		}
	case *ast.CallExpr:
		if fn, ok := c.p.info.Uses[calleeIdentOr(x)].(*types.Func); ok && c.anySlice(result(fn)) {
			c.traceReturns(fn)
			return
		}
	case *ast.Ident:
		if v, ok := c.p.info.Uses[x].(*types.Var); ok && c.anySlice(v.Type()) {
			if fn, i, lit := c.p.paramOf(v, in); fn != nil && !lit {
				c.traceValues(fn, i)
				return
			}
			if made := c.made[v]; len(made) == 1 {
				c.scanValue(made[0].expr, made[0].in)
				return
			}
			c.report(false, x.Pos(), "the guard cannot see which values stand in %s: "+
				"it is written in %d places", x.Name, len(c.made[v]))
			return
		}
	}
	c.values++
	c.checkValue(e)
}

// traceValues measures the values at every call of fn that fills its
// argument i.
func (c *checker) traceValues(fn *types.Func, i int) {
	if c.vals[at{fn, i}] {
		return
	}
	c.vals[at{fn, i}] = true
	sig, _ := fn.Type().(*types.Signature)
	spread := sig != nil && sig.Variadic() && i == sig.Params().Len()-1
	sites := 0
	for _, cl := range c.all {
		id := calleeIdent(cl.call)
		if id == nil || c.p.info.Uses[id] != types.Object(fn) {
			continue
		}
		sites++
		if i >= len(cl.call.Args) {
			if !spread { // a variadic call may carry no value at all
				c.report(false, cl.call.Pos(), "%s is called without the values", fn.Name())
			}
			continue
		}
		if spread && cl.call.Ellipsis == token.NoPos {
			// The values were written out one by one at this call.
			for j := i; j < len(cl.call.Args); j++ {
				c.scanValue(cl.call.Args[j], cl.in)
			}
			continue
		}
		c.scanValue(cl.call.Args[i], cl.in)
	}
	if sites == 0 {
		c.report(false, fn.Pos(), "%s gives the driver values, and nobody calls it", fn.Name())
	}
	c.noValueUse(fn)
}

// traceReturns measures the values a function of this package puts together
// and gives back as a row.
func (c *checker) traceReturns(fn *types.Func) {
	if c.vals[at{fn, -1}] {
		return
	}
	c.vals[at{fn, -1}] = true
	decl := c.decl[fn]
	if decl == nil || decl.Body == nil {
		c.report(false, fn.Pos(), "the values come out of %s, and the guard cannot read it", fn.Name())
		return
	}
	found := 0
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			if c.anySlice(c.p.info.Types[r].Type) {
				found++
				c.scanValue(r, []ast.Node{decl})
			}
		}
		return true
	})
	if found == 0 {
		c.report(false, fn.Pos(), "%s is read for the values it gives back, and it gives none", fn.Name())
	}
}

// calleeIdentOr is calleeIdent for a call, and a name nothing resolves for
// anything else, so that a lookup on it comes back empty instead of panicking.
func calleeIdentOr(call *ast.CallExpr) *ast.Ident {
	if id := calleeIdent(call); id != nil {
		return id
	}
	return &ast.Ident{}
}

// result is the single type a function gives back, or nil.
func result(fn *types.Func) types.Type {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Results().Len() != 1 {
		return nil
	}
	return sig.Results().At(0).Type()
}

// anySlice reports whether the type is a slice of an interface - the shape a
// row of values travels in.
func (c *checker) anySlice(t types.Type) bool {
	if t == nil {
		return false
	}
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	_, ok = s.Elem().Underlying().(*types.Interface)
	return ok
}

// constString reports whether an expression is a string constant. go/types
// works its value out while it reads the source, so no value of a run can be
// part of it; a name that stands for a variable never has one, however the
// variable is called. A constant of another package of this module counts as
// well - it is resolved from source, and a constant is a constant.
func (p *pkgSource) constString(e ast.Expr) bool {
	tv, ok := p.info.Types[e]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String
}

// paramOf says which function around the call has v as its parameter, and in
// which place, counted the way a call writes them. A parameter of a function
// literal is reported as such.
func (p *pkgSource) paramOf(v *types.Var, in []ast.Node) (fn *types.Func, i int, lit bool) {
	for k := len(in) - 1; k >= 0; k-- {
		var sig *types.Signature
		var obj *types.Func
		switch f := in[k].(type) {
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

// checkValue refuses every value that goes to the database as a byte slice
// from anywhere but blob or a conversion of a string. A nil byte slice binds
// as NULL on MariaDB, where every column is NOT NULL; the SQLite driver of
// this project writes an empty blob for it instead, so a forgotten blob can
// go red nowhere but here.
func (c *checker) checkValue(v ast.Expr) {
	t := c.p.info.Types[v].Type
	if t == nil {
		c.report(false, v.Pos(), "the guard cannot tell what %s is", types.ExprString(v))
		return
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Invalid:
			c.report(false, v.Pos(), "the guard cannot tell what %s is", types.ExprString(v))
		case types.UntypedNil:
			c.report(false, v.Pos(), "%s is nil, and every column is NOT NULL", types.ExprString(v))
		}
		return
	case *types.Slice:
		b, ok := u.Elem().Underlying().(*types.Basic)
		if !ok || b.Kind() != types.Uint8 {
			break
		}
		if c.fromBlob(v) || c.fromString(v) {
			return
		}
		c.report(false, v.Pos(), "%s goes to the database as a byte slice that neither blob nor a conversion "+
			"of a string made; a nil one binds as NULL, and every column is NOT NULL", types.ExprString(v))
		return
	}
	c.report(false, v.Pos(), "%s is no value this store gives the database: a number, a bool, a string, "+
		"or a byte slice out of blob or a conversion of a string", types.ExprString(v))
}

// fromBlob reports whether the value is blob(...) - the function of this
// package, not one that happens to be called that.
func (c *checker) fromBlob(v ast.Expr) bool {
	call, ok := v.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	obj := c.p.info.Uses[id]
	return obj != nil && obj == c.p.pkg.Scope().Lookup("blob")
}

// fromString reports whether the value is a conversion of a string into
// bytes. Such a conversion never gives nil.
func (c *checker) fromString(v ast.Expr) bool {
	call, ok := v.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !c.p.info.Types[call.Fun].IsType() {
		return false
	}
	t := c.p.info.Types[call.Args[0]].Type
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// TestEverySQLIsAConstant: every statement this package gives the database is
// a constant, at the call to the driver and at every call site of the helpers
// that take one as a parameter, however many levels deep. No way to the
// driver is left that the guard cannot follow.
func TestEverySQLIsAConstant(t *testing.T) {
	finds, calls, _ := guard(readPackage(t, "."))
	for _, f := range finds {
		if f.sql && !f.atomic {
			t.Errorf("%s", f)
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
// the one place it can be measured - see checkValue. Task 5 brought one
// nillable field, the value of OnceCreate and OnceUse; task 6 brings seven
// more: User.TOTP, Session.Token, Cert, Proof, APIKey, APICert and
// Pending.Key. Five of those seven have nothing but blob in front of them,
// because okDoc(nil) is true; only Session.Token and Pending.Key are kept out
// by a length check as well, and user.go says what that is worth.
func TestEveryByteSliceComesFromBlob(t *testing.T) {
	finds, _, values := guard(readPackage(t, "."))
	for _, f := range finds {
		if !f.sql && !f.atomic {
			t.Errorf("%s", f)
		}
	}
	if values < 15 {
		t.Errorf("only %d values given to the database were measured", values)
	}
}

// TestOnceUseDecidesWithItsWrite: the value OnceUse hands out is spent by the
// write that hands it out, and not by a read in front of it. The two shapes
// that break it - a read and a write without a transaction, and the same two
// inside one - are both refused here, and testdata/twostep carries the first
// of them so that this rule is measured and not only written down.
//
// It is read from the source, so it holds on a machine of one processor and
// without MariaDB, where TestOnlyOneUseGetsTheValue measures nothing.
func TestOnceUseDecidesWithItsWrite(t *testing.T) {
	finds, _, _ := guard(readPackage(t, "."))
	for _, f := range finds {
		if f.atomic {
			t.Errorf("%s", f)
		}
	}
	// This package has a OnceUse, so a run that found none read past it.
	if !hasOnceUse(readPackage(t, ".")) {
		t.Error("no OnceUse was read at all, so this test measured nothing")
	}
}

// hasOnceUse says whether the package declares the method this rule is about.
func hasOnceUse(p *pkgSource) bool {
	for _, file := range p.files {
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "OnceUse" && fn.Body != nil {
				return true
			}
		}
	}
	return false
}

// TestTheNameCheckSitsInTheTransaction: UserPut asks whose the name is on the
// transaction it writes in, and in front of the write. It is read from the
// source, because no answer of UserPut says it any more.
//
// That is the whole reason this test exists. The check was measured once: on
// f048e83, taking its refusal out turned TestUserNameIsUnique and dbtest's
// users group red. Since the index error is translated, both ways give
// ErrInvalid, and the check can be deleted whole - statement, constant and all
// group of the contract going red with it. A second line of defence had made
// the first one indistinguishable, and a line no test holds is a line the next
// tidy-up removes, with the comment that turns on it left standing over
// nothing.
//
// Every count in this comment that names a group of the contract came by hand,
// the two above and the one below, taken while dbtest.Run had one call site in
// the whole tree, against memstore. Task 7 gave it a second one in
// classes/sqlite, so go test ./... drives the contract against this store from
// here on. The note at TestOneWriteAndOneDelete in user_test.go says it in
// full.
//
// What this test reads, exactly. userIDOf is named once in the package, and
// three things are read about that one place: what the call it is an argument
// of runs on - the very *sql.Tx this literal was handed, which go/types says
// by the object behind the name and not by the name tx; where that call
// stands - a statement of the literal's own body, neither inside a function
// literal of its own nor behind defer or go; and what stands between it and
// the first write of the literal - a return of dbapi.ErrInvalid belonging to
// the literal itself. Place, receiver, form. Three properties of the file.
//
// What it does not read: whether that statement ever runs, and whether its
// answer has anything to do with the write. It cannot. That is a property of
// the run, and no walk over a syntax tree decides it. Two shapes past this
// guard were measured, both of which compile, both of which run, and both of
// which left all 139 tests of this package green and every group of the
// contract with them:
//
//   - the whole check wrapped in a condition UserPut has already refused
//     above, if len(u.ID) == 0. Every word of it stands where this test wants
//     it, and it never runs.
//   - the question asked of the transaction and its answer thrown away,
//     QueryContext and rows.Close(), with the refusal left standing behind it
//     over a variable nothing ever writes.
//
// They were the second and the third way past this guard, in three rounds of
// review, each a shape the round before had not thought of. So this test is
// not what holds the property. TestTheCheckAloneRefusesATakenName and
// TestOnlyOneWriterGetsAName hold it, by asking UserPut itself on a database
// whose ngc_user_name has been dropped; both shapes above are red there at
// once. This one stays because it is cheap and because it catches the plain
// relapses in the source, before anything has to run: the check deleted, the
// refusal deleted, the question moved to the pool, the question deferred -
// measured, each of those red here.
//
// Where it is stricter than the property. That is a debt and not a virtue: a
// guard that goes red on good code gets switched off, and then it guards
// nothing, so both places where this one does are written down.
//
//   - tx2 := tx, and the question asked of tx2. The same transaction under a
//     second name, and the store is right; this test refuses it, because
//     following a value would mean following every way a value can take.
//     Measured: this test goes red on it and the two behaviour tests above
//     stay green, which is the two of them saying the store is still right.
//   - the check pulled out into a method of its own that takes the *sql.Tx as
//     a parameter. userIDOf is then not named inside the literal at all, so
//     the first switch below answers "the name is asked about outside the
//     transaction UserPut writes in" - which it is not. Nothing here follows
//     a call into another function, and a tidy-up that reaches for that shape
//     is not wrong.
//
// Both are refusals of this test and not of the store. Should either be
// wanted, this test is what has to be rebuilt, and the two behaviour tests
// are what say whether the store is still right while that happens.
//
// One more thing it does not read: the condition the refusal is written with,
// only that a refusal stands between the question and the write. Two writers
// can pass the check together on a database that does not certify them apart,
// and the index behind it is what holds the promise then.
func TestTheNameCheckSitsInTheTransaction(t *testing.T) {
	p := readPackage(t, ".")
	lit, found := txBodyOf(p, "UserPut")
	if !found {
		t.Fatal("no UserPut was read at all, so this test measured nothing")
	}
	if lit == nil {
		t.Fatal("UserPut gives write no function literal, so there is no transaction to read here")
	}
	at := func(pos token.Pos) string { return p.fset.Position(pos).String() }
	inside := func(pos token.Pos) bool { return pos > lit.Pos() && pos < lit.End() }
	if !constDeclared(p, "userIDOf") {
		t.Fatal("userIDOf is not declared any more: nothing asks whose the name is")
	}
	read, update, insert := constUses(p, "userIDOf"), constUses(p, "userUpdate"), constUses(p, "userInsert")
	if len(update) == 0 || len(insert) == 0 || !inside(update[0]) || !inside(insert[0]) {
		t.Fatalf("%s: the literal UserPut gives write holds neither of its two writes, "+
			"so this test read the wrong one", at(lit.Pos()))
	}
	switch {
	case len(read) == 0:
		t.Fatalf("%s: userIDOf is named nowhere, so nothing asks whose the name is before the write",
			at(lit.Pos()))
	case len(read) > 1:
		t.Fatalf("%s: userIDOf is asked in %d places, and this test can say of none of them "+
			"that it is the one in front of the write", at(read[0]), len(read))
	case !inside(read[0]):
		t.Fatalf("%s: the name is asked about outside the transaction UserPut writes in, "+
			"so a second writer fits between the question and the write", at(read[0]))
	case read[0] > update[0]:
		t.Fatalf("%s: the name is asked about after the write, not in front of it", at(read[0]))
	}
	tx := txParamOf(p, lit)
	if tx == nil {
		t.Fatalf("%s: the literal UserPut gives write takes no transaction of its own under a name, "+
			"so nothing here can say what the question is asked of", at(lit.Pos()))
	}
	call := callCarrying(lit, read[0])
	switch {
	case call == nil:
		t.Fatalf("%s: userIDOf is no argument of a call, so nothing asks it", at(read[0]))
	case receiverOf(p, call) != tx:
		t.Fatalf("%s: the name is asked of %s and not of the *sql.Tx this literal was handed. "+
			"Anything else runs on a connection of its own and cannot even see this transaction, "+
			"so every other writer fits between the question and the write; a second name "+
			"holding this very transaction is refused here too, because this reads the "+
			"parameter and follows nothing",
			at(call.Pos()), types.ExprString(call.Fun))
	}
	switch st := statementOf(lit, read[0]); {
	case st == nil:
		t.Fatalf("%s: the question stands inside a function literal of its own, so where it is "+
			"written says nothing about when it runs", at(read[0]))
	case isLaterThanItLooks(st):
		t.Fatalf("%s: the question is deferred, so it runs after the write however far in front "+
			"of it it is written", at(st.Pos()))
	}
	for _, pos := range invalidReturns(p, lit) {
		if pos > read[0] && pos < update[0] {
			return
		}
	}
	t.Errorf("%s: nothing between the question and the write answers dbapi.ErrInvalid, "+
		"so the answer to a name that belongs to another ID is left to the index alone", at(read[0]))
}

// txBodyOf is the transaction of a method: the function literal it gives
// store.write. It says as well whether the method was found at all, so a
// renamed one comes out as a failure and not as a pass.
func txBodyOf(p *pkgSource, method string) (*ast.FuncLit, bool) {
	for _, file := range p.files {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != method || fn.Body == nil {
				continue
			}
			var lit *ast.FuncLit
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || lit != nil {
					return lit == nil
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				f, isFunc := p.info.Uses[sel.Sel].(*types.Func)
				if !isFunc || f.Name() != "write" || f.Pkg() != p.pkg || len(call.Args) == 0 {
					return true
				}
				if l, ok := call.Args[len(call.Args)-1].(*ast.FuncLit); ok {
					lit = l
				}
				return true
			})
			return lit, true
		}
	}
	return nil, false
}

// constDeclared says whether the package declares a constant of this name.
func constDeclared(p *pkgSource, name string) bool {
	_, ok := p.pkg.Scope().Lookup(name).(*types.Const)
	return ok
}

// constUses gives every place a constant of this package is named, in the
// order of the source. It asks go/types what the name stands for, so a local
// variable that carries the same name is not one of them.
func constUses(p *pkgSource, name string) []token.Pos {
	var at []token.Pos
	for _, file := range p.files {
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if c, ok := p.info.Uses[id].(*types.Const); ok && c.Name() == name && c.Pkg() == p.pkg {
				at = append(at, id.Pos())
			}
			return true
		})
	}
	slices.Sort(at)
	return at
}

// txParamOf is the transaction a function literal was handed, as the object
// go/types made of its parameter. That object is what a call is compared
// against and never the name, so a variable that merely carries the name tx
// is not this transaction. A name that really holds it is refused as well -
// tx2 := tx and the call on tx2 - and on purpose: following where a value
// travelled would mean following every way it can travel, while the statement
// this guard is for is written on the parameter itself. store.write hands its
// literal one thing and nothing else, so one parameter under one name is the
// shape; anything else is no transaction this test can follow.
func txParamOf(p *pkgSource, lit *ast.FuncLit) *types.Var {
	if lit.Type.Params == nil || len(lit.Type.Params.List) != 1 ||
		len(lit.Type.Params.List[0].Names) != 1 {
		return nil
	}
	v, _ := p.info.Defs[lit.Type.Params.List[0].Names[0]].(*types.Var)
	return v
}

// callCarrying is the innermost call that takes what stands at pos as one of
// its own arguments.
func callCarrying(n ast.Node, pos token.Pos) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			if arg.Pos() <= pos && pos < arg.End() {
				found = call // outermost first, so the last one written down is the innermost
			}
		}
		return true
	})
	return found
}

// receiverOf is the variable a method is called on, where the call is a method
// call on a plain name. A field, the result of another call or a function of
// the package gives nothing, and the caller says so instead of guessing.
func receiverOf(p *pkgSource, call *ast.CallExpr) *types.Var {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil
	}
	v, _ := p.info.Uses[id].(*types.Var)
	return v
}

// statementOf is the statement of the literal's own body that holds pos, and
// nothing where pos stands inside a function literal nested in it: there the
// place in the source says nothing about when it runs, because what runs is
// the call of that literal and not its text.
func statementOf(lit *ast.FuncLit, pos token.Pos) ast.Stmt {
	for _, st := range lit.Body.List {
		if pos < st.Pos() || pos >= st.End() {
			continue
		}
		nested := false
		ast.Inspect(st, func(n ast.Node) bool {
			if inner, ok := n.(*ast.FuncLit); ok && pos > inner.Pos() && pos < inner.End() {
				nested = true
			}
			return !nested
		})
		if nested {
			return nil
		}
		return st
	}
	return nil
}

// isLaterThanItLooks reports whether a statement runs at another time than the
// one its place gives it: defer runs it when the literal is done, go runs it
// beside everything.
func isLaterThanItLooks(st ast.Stmt) bool {
	switch st.(type) {
	case *ast.DeferStmt, *ast.GoStmt:
		return true
	}
	return false
}

// invalidReturns gives every place inside n where dbapi.ErrInvalid is
// returned. It stops at a function literal nested in n: a return there ends
// that literal and not the one asked about, so it answers the caller nothing.
//
// dbapi is told by the path its package was imported under and not by its
// name, because a package of that name can stand anywhere.
func invalidReturns(p *pkgSource, n ast.Node) []token.Pos {
	var at []token.Pos
	ast.Inspect(n, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok && node != n {
			return false
		}
		ret, ok := node.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			sel, ok := r.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if obj := p.info.Uses[sel.Sel]; obj != nil && obj.Name() == "ErrInvalid" &&
				obj.Pkg() != nil && obj.Pkg().Path() == modulePath+"shared/dbapi" {
				at = append(at, ret.Pos())
			}
		}
		return true
	})
	return at
}

// atTheDatabase are the packages under apps/server-db that may touch a
// database, with what holds them. The guard above reads one package; without
// this list its promise would stop at its own directory, because a call in
// another package of this module is not read here at all.
var atTheDatabase = map[string]string{
	"sqlstore": "the guard of this file",
	"migrate":  "checkStatement, fill and the tests beside them: it runs the statements of the migrations, and they are measured there",
	"sqlite":   "it opens the encrypted file and sets its PRAGMAs on the driver's own connection, and since task 7 Open also applies the migrations and wraps the result as a store; it runs no statement of its own - migrate and sqlstore run theirs, and each of those two has a guard here",
}

// TestNobodyElseUnderServerDBSpeaksToADatabase draws the line the guard above
// needs. Every package under apps/server-db that imports database/sql or
// names one of the driver's methods has to stand in atTheDatabase with the
// reason why it is allowed to. A new package that speaks to the database
// without a guard is a failure here, not a quiet hole.
//
// It carries the same ban as noReflect, over every package instead of one:
// no program file under apps/server-db outside testdata/ and vendor/ may
// import reflect. Through reflect the name of a method is a string, and no
// reading of identifiers - here or in a sister package - would see it.
// testdata/ is skipped on purpose, because the package mirror there imports
// reflect as its hole; test files are not read here either. It is a ban, not
// a measurement of need: no program file outside testdata/ imports reflect
// today, and an exception would belong in a table with a reason, the way
// atTheDatabase carries one.
func TestNobodyElseUnderServerDBSpeaksToADatabase(t *testing.T) {
	root := filepath.Join(moduleRoot(t), "apps", "server-db")
	seen, looked := map[string]bool{}, 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		looked++
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		pkg := file.Name.Name
		for _, imp := range file.Imports {
			if imp.Path.Value == `"reflect"` {
				t.Errorf("%s: package %s imports reflect; a method called through it carries its "+
					"name as a string, and no guard under apps/server-db reads strings", path, pkg)
			}
		}
		if !atDatabase(file) {
			return nil
		}
		seen[pkg] = true
		if _, ok := atTheDatabase[pkg]; !ok {
			t.Errorf("%s: package %s touches a database, and nothing says what guards it; "+
				"give it one and write it into atTheDatabase", path, pkg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if looked < 3 {
		t.Errorf("only %d source files were read under %s", looked, root)
	}
	for pkg := range atTheDatabase {
		if !seen[pkg] {
			t.Errorf("atTheDatabase names %s, and no package of that name touches a database any more", pkg)
		}
	}
}

// atDatabase reports whether a file imports database/sql or names one of the
// driver's methods. The second catches a file that got its handle through an
// interface of somebody else.
func atDatabase(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path.Value == `"database/sql"` {
			return true
		}
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok && dbMethods[sel.Sel.Name] {
			found = true
		}
		return !found
	})
	return found
}

// TestTheGuardCatchesWhatSlippedPast runs the guard against every package
// under testdata. A want says what the guard does with that package and
// nothing else: empty means it has to stay silent, a text means it has to
// say that much. Whether the store in the package is right is a second
// question, and the package comment there answers it.
//
// For most lines the two answers agree. An empty want then means a store
// written the way this store writes: a guard that is red on good code is
// switched off, and then it guards nothing. A text then means one hole cut
// in, and every one of those was measured green against an earlier guard;
// namesake was measured red against one, although nothing is wrong with it.
//
// Three lines are here because the two answers do not agree. Each of the
// three was measured against the guard, not guessed:
//
//   - runnow is silent and the store is wrong. A helper works the read out
//     at the defer, and without reading that helper the guard cannot tell it
//     from the one in deferwrap, which only hands the body on. This gap is
//     what closing the false alarm on deferwrap cost.
//   - litmulti is silent and the store is wrong. The read stands in a
//     literal whose name was written in two places, and litOf follows a name
//     to its literal only where it was written in exactly one.
//   - armlater is red and the store is right. The read is handed to a
//     collector before the write and runs only after it, and a literal in an
//     argument is read as if it ran at that call. Only the held-back rebuild
//     of argLits mends that one.
//
// So a red here is read against the line, never against the store. Should
// runnow or litmulti go red one day, or armlater go silent, the guard got
// better and the line is what has to follow.
//
// How many there are is not written down anywhere: the table is compared
// against the directory, so a package added there without a line here is a
// failure, and a line here without a package there is one too.
//
// clean holds no line of its own. Its whole body stands, in order and
// unchanged, in prepared, in namesake and in valueslice as well - those three
// are clean plus more, and only its own doc comment and its package line are
// not in them. So there is nothing in it that is only in it, and nobody need
// look for a change that turns clean red and leaves those three silent. That
// is how it is built: the smallest shape, kept to show that the guard is not
// red on everything.
func TestTheGuardCatchesWhatSlippedPast(t *testing.T) {
	table := []struct{ dir, want string }{
		{"clean", ""},
		{"prepared", ""},
		{"valueslice", ""},
		{"namesake", ""},
		{"shadow", "the statement is the variable blobDelete"},
		{"literal", "the statement is the variable stmt"},
		{"handoff", "exec is taken as a value"},
		{"methodvalue", "ExecContext is taken as a value"},
		{"rawbytes", "data goes to the database as a byte slice"},
		{"slot", "cannot see which values stand in vals"},
		{"prepname", "the statement is put together at run time"},
		{"mirror", "imports reflect"},
		{"twostep", "the first statement it gives the database only reads"},
		{"sentence", ""},
		{"deferarg", "the first statement it gives the database only reads"},
		{"deferhelp", "the first statement it gives the database only reads"},
		{"deferrecv", "the first statement it gives the database only reads"},
		{"deferlit", "the first statement it gives the database only reads"},
		{"goarg", "the first statement it gives the database only reads"},
		{"defercall", "the first statement it gives the database only reads"},
		{"gocall", "the first statement it gives the database only reads"},
		{"deferindex", "the first statement it gives the database only reads"},
		{"defertimer", ""},
		{"deferwrap", ""},
		{"litarg", "the first statement it gives the database only reads"},
		{"runnow", ""},
		{"litmulti", ""},
		{"armlater", "the first statement it gives the database only reads"},
	}
	listed := map[string]bool{}
	for _, c := range table {
		if listed[c.dir] {
			t.Errorf("testdata/%s stands twice in the table", c.dir)
		}
		listed[c.dir] = true
	}
	dirs, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	there := map[string]bool{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		there[d.Name()] = true
		if !listed[d.Name()] {
			t.Errorf("testdata/%s is not in the table, so nothing runs against it", d.Name())
		}
	}
	for dir := range listed {
		if !there[dir] {
			t.Errorf("the table names testdata/%s, and no such package stands there", dir)
		}
	}
	for _, c := range table {
		t.Run(c.dir, func(t *testing.T) {
			if !there[c.dir] {
				t.Skip("the line above says what is missing")
			}
			finds, calls, values := guard(readPackage(t, filepath.Join("testdata", c.dir)))
			if calls < 5 || values < 10 {
				t.Fatalf("the package under test is too small to say anything: %d calls, %d values", calls, values)
			}
			if c.want == "" {
				for _, f := range finds {
					t.Errorf("%s", f)
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
