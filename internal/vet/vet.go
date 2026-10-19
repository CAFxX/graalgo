// Package vet implements static analyzers for `graalgo vet`.
//
// Analyzers inspect the parsed AST and type information, reporting
// diagnostics in the same style as `go vet`.
package vet

import (
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
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
// It parses with comments (for directive checks).
//
// Files are grouped by directory and package clause, and each group is
// type-checked and analyzed as its own package, mirroring `go vet`'s
// per-package behavior. Files from different packages are never merged
// into a single fake package (which would produce redeclarations, wrong
// resolution, and garbage diagnostics).
//
// Type checking is best-effort: if it fails, the failure is reported as a
// warning on stderr (naming any unresolvable imports) and the analyzers
// still run on the AST with whatever type info was gathered. Parse
// failures are collected and returned; analysis continues with the files
// that did parse.
func Run(paths []string) ([]Diagnostic, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("vet: no files")
	}
	fset := token.NewFileSet()
	groups, gerr := groupFiles(fset, paths)

	var out []Diagnostic
	for i, g := range groups {
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
		pkg, checkErr := conf.Check(fmt.Sprintf("vetpkg%d", i), fset, g.files, info)
		if checkErr != nil {
			reportTypeCheckError(g, checkErr)
		}
		// Type errors are tolerated: analyzers use best-effort info.

		pass := &Pass{Fset: fset, Files: g.files, Info: info, Pkg: pkg}
		for _, a := range analyzers {
			out = append(out, a.Run(pass)...)
		}
	}
	return SortDiagnostics(out), gerr
}

// fileGroup is a set of files that form one package: same directory and
// same package clause.
type fileGroup struct {
	dir   string // absolute directory containing the files
	name  string // package clause name
	files []*ast.File
}

// groupFiles parses paths and groups the resulting files by (directory,
// package clause). Files that fail to parse are skipped; their errors are
// collected and returned so the caller can report them.
func groupFiles(fset *token.FileSet, paths []string) ([]*fileGroup, error) {
	var groups []*fileGroup
	byKey := make(map[string]*fileGroup)
	var errs []error
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("vet: %v", err))
			continue
		}
		f, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			errs = append(errs, fmt.Errorf("vet: parse %s: %v", path, err))
			continue
		}
		dir, derr := filepath.Abs(filepath.Dir(path))
		if derr != nil {
			dir = filepath.Dir(path)
		}
		key := dir + "\x00" + f.Name.Name
		g := byKey[key]
		if g == nil {
			g = &fileGroup{dir: dir, name: f.Name.Name}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.files = append(g.files, f)
	}
	return groups, errors.Join(errs...)
}

// unresolvableImportRe matches go/types "could not import" errors, e.g.
// `could not import "example.com/mod/pkg" (can't find import: ...)`.
var unresolvableImportRe = regexp.MustCompile(`could not import ("[^"]+"|\S+)`)

// reportTypeCheckError surfaces a failed package type-check as a warning
// on stderr instead of silently discarding it. An unresolvable import is
// named explicitly so the user knows why analysis is degraded.
func reportTypeCheckError(g *fileGroup, err error) {
	fmt.Fprintf(os.Stderr, "graalgo vet: type-check of package %s in %s failed (%d file(s)); analyzers run with best-effort type info\n",
		g.name, g.dir, len(g.files))
	if m := unresolvableImportRe.FindStringSubmatch(err.Error()); m != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: unresolvable import: %s\n", m[1])
	}
	fmt.Fprintf(os.Stderr, "graalgo vet: type error: %v\n", err)
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
