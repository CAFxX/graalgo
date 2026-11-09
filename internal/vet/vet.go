// Package vet implements static analyzers for `graalgo vet`.
//
// Analyzers inspect the parsed AST and type information, reporting
// diagnostics in the same style as `go vet`.
package vet

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"os/exec"
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
//
// Files are loaded as packages (see loader.go): module files get full
// module resolution with test variants, files outside any module get a
// best-effort single-package check. Load and type-check errors are
// reported as diagnostics instead of being discarded, so a run never
// silently reports "no issues found" on input it could not understand.
// A nonexistent input file, or a missing `go` binary to drive package
// loading, is a hard error.
func Run(paths []string) ([]Diagnostic, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("vet: no files")
	}
	abs := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		ap, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("vet: %v", err)
		}
		if _, err := os.Stat(ap); err != nil {
			return nil, fmt.Errorf("vet: %v", err)
		}
		if !seen[ap] {
			seen[ap] = true
			abs = append(abs, ap)
		}
	}
	// Package loading shells out to `go list`; without a `go` binary
	// the run must fail loudly instead of silently degrading.
	if _, err := exec.LookPath("go"); err != nil {
		return nil, fmt.Errorf("vet: cannot load packages: no `go` binary on PATH")
	}
	passes, loadDiags, err := loadPasses(abs)
	if err != nil {
		return nil, err
	}
	var out []Diagnostic
	out = append(out, loadDiags...)
	for _, pass := range passes {
		for _, a := range analyzers {
			out = append(out, a.Run(pass)...)
		}
	}
	return SortDiagnostics(dedupeDiagnostics(out)), nil
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
