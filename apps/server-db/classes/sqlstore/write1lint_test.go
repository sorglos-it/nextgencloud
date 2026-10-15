package sqlstore

// The one way the write channel could be turned into a deadlock, held by a
// case instead of by care: a write that waits for another write.
//
// The channel holds one turn, and it is held for the whole operation
// including its repeats. A write (or a statement without a transaction)
// started INSIDE a write closure would wait for a turn its own caller is
// holding, and nothing would ever come back - not an error, not a deadline of
// the database, just a goroutine that stands still until the caller's context
// ends. There is no such call today; this case is what keeps it that way,
// because the next hand that writes one would otherwise learn it from a
// hanging server.

import (
	"go/ast"
	"testing"
)

// TestNoWriteWaitsForAnotherWrite reads this package and refuses a call to
// write or exec that stands inside the closure of a write.
//
// It counts what it really looked at before it says anything: a reading that
// found no write closures at all would be green for the wrong reason.
func TestNoWriteWaitsForAnotherWrite(t *testing.T) {
	src := readPackage(t, ".")

	closures := 0
	for _, f := range src.files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isStoreCall(call, "write") || len(call.Args) < 2 {
				return true
			}
			lit, ok := call.Args[len(call.Args)-1].(*ast.FuncLit)
			if !ok {
				return true
			}
			closures++
			ast.Inspect(lit.Body, func(in ast.Node) bool {
				inner, ok := in.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, name := range []string{"write", "exec"} {
					if isStoreCall(inner, name) {
						t.Errorf("%s: %s is called inside the closure of a write; "+
							"it would wait for the turn its own caller holds",
							src.fset.Position(inner.Pos()), name)
					}
				}
				return true
			})
			return true
		})
	}
	// A reading that saw no write closure measured nothing.
	if closures < 4 {
		t.Errorf("only %d write closures were read; the reading has gone past them", closures)
	}
}

// isStoreCall reports whether call is x.<name>(...) - the receiver is not
// asked about, because in this package there is one store and nothing else
// carries these two names.
func isStoreCall(call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}
