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
	"sort"
	"strings"
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
// The files are grouped into real packages before any type checking:
// grouping key is (directory, package clause), so files of one package
// are checked together, a package's external test package
// (`package foo_test`) gets its own pass, and files from different
// directories are never merged into one synthetic package. Within one
// directory, all files must belong to a single package (plus its
// external test package); a directory mixing two package names is
// rejected, as `go build` would reject it.
//
// Type checking is best-effort for analysis but not for the verdict:
// analyzers still run on the AST with whatever type info was gathered
// when checking fails, and their diagnostics are returned alongside the
// error — but a type-check failure is returned as an error, so a caller
// can never report "no issues found" for code that did not type-check.
func Run(paths []string) ([]Diagnostic, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("vet: no files")
	}
	fset := token.NewFileSet()
	groups, err := groupFilesByPackage(fset, paths)
	if err != nil {
		return nil, err
	}

	var out []Diagnostic
	var typeErrs []error
	for _, g := range groups {
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
		pkg, checkErr := conf.Check(g.name, fset, g.files, info)
		if checkErr != nil {
			typeErrs = append(typeErrs, fmt.Errorf("vet: type check failed for package %s in %s: %v", g.name, g.dir, checkErr))
		}
		// Analyzers run even when checking failed, on best-effort info.
		pass := &Pass{Fset: fset, Files: g.files, Info: info, Pkg: pkg}
		for _, a := range analyzers {
			out = append(out, a.Run(pass)...)
		}
	}
	return SortDiagnostics(out), errors.Join(typeErrs...)
}

// pkgGroup is one real package pass: the files of a single package
// clause in a single directory.
type pkgGroup struct {
	dir   string // absolute directory, for error messages and grouping
	name  string // package clause name (e.g. "foo" or "foo_test")
	files []*ast.File
}

// groupFilesByPackage parses paths (with comments, for directive checks)
// and groups the files by (directory, package clause), in deterministic
// order. It rejects a directory whose files belong to more than one
// package (counting a package and its external test package as one):
// merging such files into one check would either fail spuriously or
// silently check a package that does not exist.
func groupFilesByPackage(fset *token.FileSet, paths []string) ([]pkgGroup, error) {
	type parsed struct {
		path string
		file *ast.File
	}
	var parsedFiles []parsed
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("vet: %v", err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("vet: parse %s: %v", path, err)
		}
		parsedFiles = append(parsedFiles, parsed{path: path, file: f})
	}

	// baseName strips the "_test" suffix of an external test package,
	// so "foo" and "foo_test" count as one package per directory.
	baseName := func(name string) string {
		return strings.TrimSuffix(name, "_test")
	}
	basesByDir := make(map[string]map[string]bool)   // dir -> package bases
	clausesByDir := make(map[string]map[string]bool) // dir -> package clauses seen
	for _, p := range parsedFiles {
		dir, err := filepath.Abs(filepath.Dir(p.path))
		if err != nil {
			dir = filepath.Dir(p.path)
		}
		name := p.file.Name.Name
		if basesByDir[dir] == nil {
			basesByDir[dir] = make(map[string]bool)
		}
		if clausesByDir[dir] == nil {
			clausesByDir[dir] = make(map[string]bool)
		}
		basesByDir[dir][baseName(name)] = true
		clausesByDir[dir][name] = true
	}
	for dir, bases := range basesByDir {
		if len(bases) > 1 {
			names := make([]string, 0, len(clausesByDir[dir]))
			for n := range clausesByDir[dir] {
				names = append(names, n)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("vet: found packages %s in %s", strings.Join(names, " and "), dir)
		}
	}

	// pkgGroup holds a slice and is not a map key; key on dir+name text.
	byKey := make(map[string][]*ast.File)
	var keys []pkgGroup
	seenKey := make(map[string]bool)
	for _, p := range parsedFiles {
		dir, err := filepath.Abs(filepath.Dir(p.path))
		if err != nil {
			dir = filepath.Dir(p.path)
		}
		key := pkgGroup{dir: dir, name: p.file.Name.Name}
		flat := dir + "\x00" + key.name
		if !seenKey[flat] {
			seenKey[flat] = true
			keys = append(keys, key)
		}
		byKey[flat] = append(byKey[flat], p.file)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].dir != keys[j].dir {
			return keys[i].dir < keys[j].dir
		}
		return keys[i].name < keys[j].name
	})
	groups := make([]pkgGroup, 0, len(keys))
	for _, k := range keys {
		k.files = byKey[k.dir+"\x00"+k.name]
		groups = append(groups, k)
	}
	return groups, nil
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
