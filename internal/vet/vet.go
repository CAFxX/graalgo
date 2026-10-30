// Package vet implements static analyzers for `graalgo vet`.
//
// Analyzers inspect the parsed AST and type information, reporting
// diagnostics in the same style as `go vet`.
package vet

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
)

// Analyzer is a single static check.
type Analyzer struct {
	Name string
	Doc  string
	Run  func(pass *Pass) []Diagnostic
}

// Pass carries the state for a single analyzer run.
type Pass struct {
	Fset  *token.FileSet
	Files []*ast.File
	Info  *types.Info
	// Pkg is the type-checked package (may be nil if checking failed).
	Pkg *types.Package
}

// registered analyzers, in the order they run.
var analyzers []*Analyzer

func register(a *Analyzer) {
	analyzers = append(analyzers, a)
}

// Analyzers returns the registered analyzers.
func Analyzers() []*Analyzer {
	return analyzers
}

// Run analyzes the named Go source files and returns sorted diagnostics.
// It parses with comments (for directive checks) and type-checks on a
// best-effort basis: if type checking fails, analyzers still run on the
// AST with whatever type info was gathered.
//
// Files are grouped by (directory, declared package name) and each group
// is type-checked and analyzed separately. Grouping matters: `go vet`
// analyzes the internal package and an external `foo_test` package as
// two packages, and merging them into one synthetic package (the old
// behaviour, a single conf.Check over every file as "vetpkg") lets
// declarations from one package pollute the other's type info and makes
// type-dependent analyzers silently degrade to syntactic fallbacks.
// Internal test files (package foo) join the internal group, matching
// `go vet`'s augmented-package semantics.
func Run(paths []string) ([]Diagnostic, error) {
	fset := token.NewFileSet()
	type group struct {
		files []*ast.File
	}
	var order []string
	groups := make(map[string]*group)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("vet: %v", err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("vet: parse %s: %v", path, err)
		}
		key := filepath.Dir(path) + "\x00" + f.Name.Name
		g, ok := groups[key]
		if !ok {
			g = &group{}
			groups[key] = g
			order = append(order, key)
		}
		g.files = append(g.files, f)
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("vet: no files")
	}

	var out []Diagnostic
	for _, key := range order {
		g := groups[key]
		pkgName := g.files[0].Name.Name
		info := &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Scopes:     make(map[ast.Node]*types.Scope),
		}
		conf := types.Config{
			Importer:                 importer.Default(),
			IgnoreFuncBodies:         false,
			DisableUnusedImportCheck: true,
		}
		pkg, _ := conf.Check(pkgName, fset, g.files, info)
		// Type errors are tolerated: analyzers use best-effort info.

		pass := &Pass{Fset: fset, Files: g.files, Info: info, Pkg: pkg}
		for _, a := range analyzers {
			out = append(out, a.Run(pass)...)
		}
	}
	return SortDiagnostics(out), nil
}

// position returns the source position of n.
func (p *Pass) position(n ast.Node) token.Position {
	return p.Fset.Position(n.Pos())
}

// isPkgFunc reports whether call is a call to pkgPath.FuncName,
// resolved via type info when available, falling back to the
// syntactic package name.
func (p *Pass) isPkgFunc(call *ast.CallExpr, pkgPath, funcName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != funcName {
		return false
	}
	// Prefer type info.
	if obj := p.Info.Uses[sel.Sel]; obj != nil {
		if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path() == pkgPath
		}
	}
	// Fallback: match on the qualifier's package name.
	if id, ok := sel.X.(*ast.Ident); ok {
		if obj := p.Info.Uses[id]; obj != nil {
			if pn, ok := obj.(*types.PkgName); ok {
				return pn.Imported().Path() == pkgPath
			}
		}
		// Last resort: compare import path suffix against the
		// qualifier name for the common stdlib packages.
		for _, f := range p.Files {
			for _, imp := range f.Imports {
				path := importPath(imp)
				name := importName(imp)
				if path == pkgPath && name == id.Name {
					return true
				}
			}
		}
	}
	return false
}

func importPath(imp *ast.ImportSpec) string {
	s := imp.Path.Value
	if len(s) >= 2 && s[0] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func importName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	p := importPath(imp)
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
