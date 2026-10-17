// Package vet implements static analyzers for `graalgo vet`.
//
// Analyzers inspect the parsed AST and type information, reporting
// diagnostics in the same style as `go vet`.
package vet

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
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
// best-effort basis: imports are resolved from source (see sourceImporter),
// and if type checking fails, analyzers still run on the AST with whatever
// type info was gathered. Imports that cannot be resolved are reported as
// diagnostics rather than silently degrading import-dependent checks.
func Run(paths []string) ([]Diagnostic, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("vet: %v", err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("vet: parse %s: %v", path, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("vet: no files")
	}

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
	imp := newSourceImporter(fset)
	conf := types.Config{
		Importer:                 imp,
		IgnoreFuncBodies:         false,
		DisableUnusedImportCheck: true,
		FakeImportC:              true, // cgo files get usable type info instead of a failed "C" import
		Sizes:                    imp.sizes,
	}
	pkg, _ := conf.Check("vetpkg", fset, files, info)
	// Type errors are tolerated: analyzers use best-effort info.
	// Import failures are not silent (QN-13): each is reported so the
	// user knows which import-dependent checks were degraded.

	pass := &Pass{Fset: fset, Files: files, Info: info, Pkg: pkg}
	var out []Diagnostic
	out = append(out, importFailureDiagnostics(fset, files, imp.failures())...)
	for _, a := range analyzers {
		out = append(out, a.Run(pass)...)
	}
	return SortDiagnostics(out), nil
}

// importFailureDiagnostics reports unresolvable imports as diagnostics
// positioned at the corresponding import spec, so a degraded run is
// visible instead of silent (QN-13).
func importFailureDiagnostics(fset *token.FileSet, files []*ast.File, failed map[string]error) []Diagnostic {
	var out []Diagnostic
	for _, f := range files {
		for _, spec := range f.Imports {
			path := importPath(spec)
			err, ok := failed[path]
			if !ok {
				continue
			}
			out = append(out, Diagnostic{
				Pos:     fset.Position(spec.Pos()),
				Message: fmt.Sprintf("could not import %q (%v); import-dependent checks skipped for this package", path, firstLine(err.Error())),
			})
		}
	}
	return out
}

// firstLine truncates s at the first newline (go/build failure reasons
// list every search path; the diagnostic keeps the summary).
func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// isPointerToNamed reports whether t is *pkgPath.name, compared
// structurally rather than by string rendering.
func isPointerToNamed(t types.Type, pkgPath, name string) bool {
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := p.Elem().(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == name && obj.Pkg() != nil && obj.Pkg().Path() == pkgPath
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
