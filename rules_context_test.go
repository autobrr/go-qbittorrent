package qbittorrent

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"
)

func parseGoFiles(t *testing.T, pattern string, keep func(name string) bool) (*token.FileSet, []*ast.File) {
	t.Helper()
	names, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range names {
		if !keep(name) {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return fset, files
}

func isSelector(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && sel.Sel.Name == name
}

func ctxParam(fn *ast.FuncType) string {
	for _, field := range fn.Params.List {
		if isSelector(field.Type, "context", "Context") && len(field.Names) > 0 {
			return field.Names[0].Name
		}
	}
	return ""
}

func calledFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return nil
	}
	fn, _ := info.Uses[id].(*types.Func)
	return fn
}

func ctxSibling(fn *types.Func) *types.Func {
	if fn == nil || strings.HasSuffix(fn.Name(), "Ctx") {
		return nil
	}
	name := fn.Name() + "Ctx"
	sig := fn.Signature()
	if sig.Recv() == nil {
		if fn.Pkg() == nil {
			return nil
		}
		sibling, _ := fn.Pkg().Scope().Lookup(name).(*types.Func)
		return sibling
	}
	obj, _, _ := types.LookupFieldOrMethod(sig.Recv().Type(), true, fn.Pkg(), name)
	sibling, _ := obj.(*types.Func)
	return sibling
}

func findDroppedContexts(fset *token.FileSet, files []*ast.File, info *types.Info) []string {
	var found []string
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ctx := ctxParam(fn.Type)
			if ctx == "" || ctx == "_" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				pos := fset.Position(call.Pos())
				where := fmt.Sprintf("%s:%d in %s", filepath.Base(pos.Filename), pos.Line, fn.Name.Name)
				if isSelector(call.Fun, "context", "Background") || isSelector(call.Fun, "context", "TODO") {
					found = append(found, fmt.Sprintf("%s: pass %s, or context.WithoutCancel(%s) to outlive the caller", where, ctx, ctx))
				} else if called := calledFunc(info, call); ctxSibling(called) != nil {
					found = append(found, fmt.Sprintf("%s: calls %s, which uses context.Background(); call %sCtx(%s, ...)", where, called.Name(), called.Name(), ctx))
				}
				return true
			})
		}
	}
	return found
}

func TestLibraryCodePassesTheCallersContext(t *testing.T) {
	fset, files := parseGoFiles(t, "*.go", func(name string) bool { return !strings.HasSuffix(name, "_test.go") })
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check("github.com/autobrr/go-qbittorrent", fset, files, info); err != nil {
		t.Fatal(err)
	}
	for _, msg := range findDroppedContexts(fset, files, info) {
		t.Error(msg)
	}
}
