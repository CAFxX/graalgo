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
	// Pkg is the type-checked package (nil when type checking failed;
	// see TypeCheckError, returned alongside any diagnostics).
	Pkg *types.Package
}

// TypeCheckError reports that go/types failed while checking the files
// passed to Run. Diagnostics computed from partial type information may
// still be returned alongside it, but callers must never present a run
// that produced a TypeCheckError as a clean bill of health: analyzers
// that depend on type information silently degrade on partial info
// (e.g. printf returns early when Info.Types is empty), so a clean
// diagnostic list over failed type information is not "no issues found".
type TypeCheckError struct {
	Err error
}

func (e *TypeCheckError) Error() string {
	return "vet: type checking failed: " + e.Err.Error()
}

// Unwrap lets callers inspect the underlying types error.
func (e *TypeCheckError) Unwrap() error { return e.Err }

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
// It parses with comments (for directive checks) and type-checks the
// files as a single package: callers must pass files that share one
// package clause (see cmdVet's grouping for directories that mix a
// package with its external test package). Analyzers still run when
// type checking fails, so partial diagnostics are not lost, but the
// failure is returned as a *TypeCheckError alongside them — never
// swallowed. A caller that prints "no issues found" when Run returns a
// nil diagnostic list but a non-nil error is lying about the run.
func Run(paths []string) ([]Diagnostic, error) {
	_, ds, err := Check(paths, importer.Default())
	return ds, err
}

// Check is Run plus the type-checked package, with a caller-supplied
// importer: cmdVet uses it so an external test package can resolve
// its package under test to the package already checked from the same
// directory, which importer.Default() cannot do for a module-path
// import of not-yet-compiled source. The returned package is
// usable as an import for a later check only when err is nil: on a
// type-checking failure the package is partial, and handing a partial
// package to another group would let its errors leak into — or mask
// errors in — that group's diagnostics.
func Check(paths []string, imp types.Importer) (*types.Package, []Diagnostic, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("vet: %v", err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("vet: parse %s: %v", path, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("vet: no files")
	}

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
	conf := types.Config{
		Importer:                 imp,
		IgnoreFuncBodies:         false,
		DisableUnusedImportCheck: true,
	}
	pkg, checkErr := conf.Check("vetpkg", fset, files, info)
	// Type errors are NOT tolerated silently: analyzers run on the
	// partial info (so real diagnostics are not lost), but the failure
	// is reported to the caller, which must fail loudly rather than
	// print a clean verdict over degraded type information.
	pass := &Pass{Fset: fset, Files: files, Info: info, Pkg: pkg}
	var out []Diagnostic
	for _, a := range analyzers {
		out = append(out, a.Run(pass)...)
	}
	out = SortDiagnostics(out)
	if checkErr != nil {
		return pkg, out, &TypeCheckError{Err: checkErr}
	}
	return pkg, out, nil
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
