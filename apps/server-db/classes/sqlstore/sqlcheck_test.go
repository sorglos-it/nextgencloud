package sqlstore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestEverySQLIsAConstant reads this package's own source and refuses any
// call that hands the database something other than a constant of this
// package or a plain string: that is how "SQL only with placeholders" is
// kept, instead of trusting everyone to remember it.
//
// A parameter counts as well, but only as far as its call sites do. put and
// exec take the statement as an argument, so the test follows it back and
// asks the same question of every call of theirs; otherwise the guard would
// stop where the helper begins, and a helper is exactly where a statement
// put together at run time would be handed in.
func TestEverySQLIsAConstant(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]bool{
		"ExecContext": true, "QueryContext": true, "QueryRowContext": true,
		"Exec": true, "Query": true, "QueryRow": true, "PrepareContext": true, "Prepare": true,
	}
	files := ownFiles(pkgs)
	consts := constNames(files)
	// handed maps the name of a function of this package to the places where
	// it hands one of its own parameters to the database.
	handed := map[string]map[int]bool{}
	checked := 0
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			params := paramIndexes(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !methods[sel.Sel.Name] || len(call.Args) == 0 {
					return true
				}
				// The SQL is the first argument, or the second when a context
				// comes first.
				arg := call.Args[0]
				if len(call.Args) > 1 && isCtx(arg) {
					arg = call.Args[1]
				}
				checked++
				switch a := arg.(type) {
				case *ast.Ident:
					switch i, isParam := params[a.Name]; {
					case consts[a.Name]: // a constant of this package
					case isParam:
						if handed[fn.Name.Name] == nil {
							handed[fn.Name.Name] = map[int]bool{}
						}
						handed[fn.Name.Name][i] = true
					default:
						t.Errorf("%s: the SQL is the variable %s, neither a constant of this package nor a parameter",
							fset.Position(a.Pos()), a.Name)
					}
				case *ast.BasicLit:
					if a.Kind != token.STRING {
						t.Errorf("%s: the SQL is no string", fset.Position(a.Pos()))
					}
				default:
					t.Errorf("%s: the SQL is put together at run time", fset.Position(arg.Pos()))
				}
				return true
			})
		}
	}
	// A test that measured nothing says nothing: the store speaks to the
	// database in more places than this, and if none of them is found here,
	// the reading above has gone past them.
	if checked < 5 {
		t.Errorf("only %d calls to the database were measured", checked)
	}
	for fn, where := range handed {
		for i := range where {
			checkCallers(t, fset, files, consts, fn, i)
		}
	}
}

// checkCallers asks the same question of every call of fn: what stands in the
// place i, which fn hands to the database, has to be a constant of this
// package. A function nobody calls is reported too - then the guard above
// measured a statement that never runs.
func checkCallers(t *testing.T, fset *token.FileSet, files map[string]*ast.File, consts map[string]bool, fn string, i int) {
	t.Helper()
	calls := 0
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || called(call) != fn {
				return true
			}
			if i >= len(call.Args) {
				t.Errorf("%s: %s is called without the statement", fset.Position(call.Pos()), fn)
				return true
			}
			calls++
			if id, ok := call.Args[i].(*ast.Ident); !ok || !consts[id.Name] {
				t.Errorf("%s: %s is given a statement that is no constant of this package",
					fset.Position(call.Args[i].Pos()), fn)
			}
			return true
		})
	}
	if calls == 0 {
		t.Errorf("%s hands its argument %d to the database, and nobody calls it", fn, i)
	}
}

// called gives the name of the function a call names, be it plain or a method
// of the store.
func called(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// ownFiles are the source files of the package itself, without the tests: a
// test may write any SQL it likes, only the program is held to the rule.
func ownFiles(pkgs map[string]*ast.Package) map[string]*ast.File {
	out := map[string]*ast.File{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if !strings.HasSuffix(name, "_test.go") {
				out[name] = file
			}
		}
	}
	return out
}

// constNames are the names of every constant the package declares.
func constNames(files map[string]*ast.File) map[string]bool {
	out := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range v.Names {
						out[name.Name] = true
					}
				}
			}
		}
	}
	return out
}

// paramIndexes gives the place of every parameter of a function, counted the
// way a call writes them.
func paramIndexes(fn *ast.FuncDecl) map[string]int {
	out := map[string]int{}
	i := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			i++
			continue
		}
		for _, name := range field.Names {
			out[name.Name] = i
			i++
		}
	}
	return out
}

func isCtx(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "ctx"
}
