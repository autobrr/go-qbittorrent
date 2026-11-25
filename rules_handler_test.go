package qbittorrent

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"testing"
)

var goroutineUnsafeTestCalls = []string{"Fatal", "Fatalf", "FailNow", "Skip", "Skipf", "SkipNow"}

func isHandlerFunc(fn *ast.FuncType) bool {
	var types []ast.Expr
	for _, field := range fn.Params.List {
		for range max(len(field.Names), 1) {
			types = append(types, field.Type)
		}
	}
	if len(types) != 2 || !isSelector(types[0], "http", "ResponseWriter") {
		return false
	}
	star, ok := types[1].(*ast.StarExpr)
	return ok && isSelector(star.X, "http", "Request")
}

func findHandlerFailNows(fset *token.FileSet, files []*ast.File) []string {
	var found []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok || !isHandlerFunc(lit.Type) {
				return true
			}
			ast.Inspect(lit.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				recv, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if recv.Name == "require" || slices.Contains(goroutineUnsafeTestCalls, sel.Sel.Name) {
					pos := fset.Position(call.Pos())
					found = append(found, fmt.Sprintf("%s:%d: %s.%s runs on the server goroutine, where FailNow cannot stop the test; use t.Errorf or assert, then write an error response and return",
						filepath.Base(pos.Filename), pos.Line, recv.Name, sel.Sel.Name))
				}
				return true
			})
			return false
		})
	}
	return found
}

func TestHTTPHandlersInTestsDoNotFailNow(t *testing.T) {
	fset, files := parseGoFiles(t, "*_test.go", func(string) bool { return true })
	for _, msg := range findHandlerFailNows(fset, files) {
		t.Error(msg)
	}
}
