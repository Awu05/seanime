package plugin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromisesSettleOnTheScheduler guards against settling a goja promise from a background
// goroutine. The goja runtime isn't safe for concurrent use, so a resolve or reject called
// directly inside a `go` statement can crash the plugin runtime. They must go through a
// Schedule* call, which runs them on the runtime's own goroutine.
func TestPromisesSettleOnTheScheduler(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || (id.Name != "resolve" && id.Name != "reject") {
				return true
			}
			// Walk outwards: a Schedule* call before any `go` statement means it's scheduled.
			for i := len(stack) - 2; i >= 0; i-- {
				if p, ok := stack[i].(*ast.CallExpr); ok {
					if sel, ok := p.Fun.(*ast.SelectorExpr); ok && strings.Contains(sel.Sel.Name, "Schedule") {
						return true
					}
				}
				if _, ok := stack[i].(*ast.GoStmt); ok {
					t.Errorf("%s: promise settled from a goroutine without the scheduler", fset.Position(call.Pos()))
					return true
				}
			}
			return true
		})
	}
}
