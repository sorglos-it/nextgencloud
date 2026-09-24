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

// argLits are the literals that stand as an argument of a call, or are called
// on the spot. Those run where they stand; a literal given a name runs where
// the name is called, and it is read there instead. A defer or a go is read
// the same way as in firstStmtIn, through onTheSpot: a literal invoked inside
// one of its arguments runs at the defer, the one the defer carries does not.
func argLits(n ast.Node) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	var visit func(ast.Node) bool
	visit = func(x ast.Node) bool {
		switch s := x.(type) {
		case *ast.DeferStmt:
			onTheSpot(s.Call, visit)
			return false
		case *ast.GoStmt:
			onTheSpot(s.Call, visit)
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, a := range call.Args {
			if lit, ok := a.(*ast.FuncLit); ok {
				out[lit] = true
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
// A literal standing in the defer itself is the one thing skipped, because
// nothing of it is worked out there. What is left out is the body of whatever
// is finally called, and the note beside the rule says so.
func onTheSpot(call *ast.CallExpr, visit func(ast.Node) bool) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		ast.Inspect(sel.X, visit)
	}
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
//     What is left out runs later: a deferred body
//     after the whole method, so a statement in it is none in front of the
//     write. A started one is not ordered at all and may run before the
//     write, so leaving that one out is a gap and not a deduction. Uncovered
//     with it: the order among several defers, which Go runs backwards, and
//     a OnceUse that speaks only from those bodies - that one is refused for
//     what it is, with the guard saying it can find no statement rather than
//     that the store is wrong. Both faces of it were measured: the right one
//     and the wrong one are refused alike, on that ground.
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
// the one place it can be measured - see checkValue - and tasks 5 to 7 bring
// five more nillable fields: User.TOTP, Session.Cert, Proof, APIKey, APICert
// and Pending.Key.
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

// atTheDatabase are the packages under apps/server-db that may touch a
// database, with what holds them. The guard above reads one package; without
// this list its promise would stop at its own directory, because a call in
// another package of this module is not read here at all.
var atTheDatabase = map[string]string{
	"sqlstore": "the guard of this file",
	"migrate":  "checkStatement, fill and the tests beside them: it runs the statements of the migrations, and they are measured there",
	"sqlite":   "it opens the encrypted file and sets its PRAGMAs on the driver's own connection; it runs no statement of the store",
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
// under testdata. The ones with an empty want are written the way this store
// writes and have to stay silent: a guard that is red on good code is
// switched off, and then it guards nothing. The others carry one hole each,
// and every one of those was measured green against an earlier guard;
// namesake was measured red against one, although nothing is wrong with it.
//
// How many there are is not written down anywhere: the table is compared
// against the directory, so a package added there without a line here is a
// failure, and a line here without a package there is one too.
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
