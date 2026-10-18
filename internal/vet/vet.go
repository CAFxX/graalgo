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
	"sort"
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
// Files are grouped by their package clause (like go list does): a package
// and its external test package (foo and foo_test) are type-checked as
// separate packages rather than merged into one pseudo-package, so every
// type-keyed analyzer sees correct type info for every input file.
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

	imp := &groupImporter{}
	var out []Diagnostic
	for _, group := range groupByPackageClause(files) {
		out = append(out, runPackageGroup(fset, imp, group)...)
	}
	return SortDiagnostics(out), nil
}

// packageGroup is one package clause's share of the input files.
type packageGroup struct {
	name  string // package clause, e.g. "foo" or "foo_test"
	files []*ast.File
}

// groupByPackageClause groups files by their package clause name, in
// sorted clause order for deterministic output. Sorting also guarantees a
// tested package is checked before its external test package ("foo"
// sorts before "foo_test"), so the test package's imports can resolve
// against it.
func groupByPackageClause(files []*ast.File) []packageGroup {
	byName := make(map[string][]*ast.File)
	for _, f := range files {
		name := f.Name.Name
		byName[name] = append(byName[name], f)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := make([]packageGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, packageGroup{name: name, files: byName[name]})
	}
	return groups
}

// runPackageGroup type-checks one group's files as a single package and
// runs every analyzer over it. Type errors are tolerated: analyzers use
// best-effort info.
func runPackageGroup(fset *token.FileSet, imp *groupImporter, group packageGroup) []Diagnostic {
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
	pkg, _ := conf.Check(group.name, fset, group.files, info)
	// Type errors are tolerated: analyzers use best-effort info.
	if pkg != nil {
		imp.add(group.name, pkg)
	}

	pass := &Pass{Fset: fset, Files: group.files, Info: info, Pkg: pkg}
	var out []Diagnostic
	for _, a := range analyzers {
		out = append(out, a.Run(pass)...)
	}
	return out
}

// groupImporter resolves imports for a single Run call. It delegates to
// the default importer first; when that fails it falls back to packages
// already type-checked in this run, matched by package clause name. This
// lets an external test package (foo_test) resolve the package under test
// (foo) when both are passed to Run together, even though the tested
// package's real import path is unknown.
type groupImporter struct {
	byName map[string]*types.Package
}

func (g *groupImporter) Import(path string) (*types.Package, error) {
	if p, err := importer.Default().Import(path); err == nil {
		return p, nil
	}
	if p, ok := g.byName[path]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("vet: cannot import %q", path)
}

func (g *groupImporter) add(name string, pkg *types.Package) {
	if g.byName == nil {
		g.byName = make(map[string]*types.Package)
	}
	g.byName[name] = pkg
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
