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

// TypeError is a go/types failure for one package group. Analyzers still
// ran on the group with whatever partial type info was gathered, so the
// diagnostics for that group may miss findings or, where an analyzer
// falls back to syntactic matching without type info, over-report.
type TypeError struct {
	Package string   // package clause name (e.g. "foo", "foo_test")
	Files   []string // source files in the group
	Err     error    // error returned by types.Config.Check
}

// Result is the outcome of analyzing one or more Go source files.
type Result struct {
	Diagnostics []Diagnostic
	// TypeErrors lists the type-checking errors encountered, one entry
	// per package group that failed to check. Empty when every group
	// type-checked cleanly.
	TypeErrors []TypeError
}

// Run analyzes the named Go source files and returns sorted diagnostics.
// It parses with comments (for directive checks) and type-checks on a
// best-effort basis: if type checking fails, analyzers still run on the
// AST with whatever type info was gathered.
//
// Files are grouped by package clause and each group is type-checked
// separately (see RunDetailed), so a package and its external test
// package no longer degrade each other's type info.
func Run(paths []string) ([]Diagnostic, error) {
	res, err := RunDetailed(paths)
	if err != nil {
		return nil, err
	}
	return res.Diagnostics, nil
}

// RunDetailed is Run with the per-package type-checking errors exposed.
// Callers that report results to a user should surface TypeErrors as
// warnings: a group listed there was analyzed with partial type info.
func RunDetailed(paths []string) (*Result, error) {
	fset := token.NewFileSet()
	groups, err := parseAndGroup(fset, paths)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("vet: no files")
	}

	// Check non-test groups first so that an external test package
	// (package foo_test) can import the already-checked package under
	// test, the way `go vet` does.
	ordered := orderGroups(groups)

	res := &Result{}
	// checked maps a derived import path to the package checked for
	// it, so external test files can import the package under test.
	// Only module-derived paths are recorded: a bare package name
	// must never shadow a real import (e.g. a local package named
	// "fmt" must not intercept imports of the standard "fmt").
	checked := make(map[string]*types.Package)
	for _, g := range ordered {
		info := &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Scopes:     make(map[ast.Node]*types.Scope),
		}
		conf := types.Config{
			Importer:                 &localImporter{fallback: importer.Default(), pkgs: checked},
			IgnoreFuncBodies:         false,
			DisableUnusedImportCheck: true,
		}
		pkgPath, derived := deriveImportPath(g.dir)
		if !derived {
			pkgPath = g.name
		}
		pkg, checkErr := conf.Check(pkgPath, fset, g.files, info)
		if checkErr != nil {
			res.TypeErrors = append(res.TypeErrors, TypeError{
				Package: g.name,
				Files:   g.paths,
				Err:     checkErr,
			})
		}
		if pkg != nil && derived {
			if _, ok := checked[pkgPath]; !ok {
				checked[pkgPath] = pkg
			}
		}
		pass := &Pass{Fset: fset, Files: g.files, Info: info, Pkg: pkg}
		for _, a := range analyzers {
			res.Diagnostics = append(res.Diagnostics, a.Run(pass)...)
		}
	}
	res.Diagnostics = SortDiagnostics(res.Diagnostics)
	return res, nil
}

// pkgGroup is one package clause's worth of parsed files. Files from a
// package and its external test package (package foo vs package
// foo_test) are different groups: go/types requires one package per
// Check call, and a directory with tests always supplies two.
type pkgGroup struct {
	name    string // package clause name
	files   []*ast.File
	paths   []string // source paths, parallel to files
	dir     string   // directory of the first file
	hasTest bool     // any file is a _test.go file
}

// parseAndGroup parses the named files and groups them by package
// clause, preserving first-seen group order.
func parseAndGroup(fset *token.FileSet, paths []string) ([]*pkgGroup, error) {
	var groups []*pkgGroup
	byName := make(map[string]*pkgGroup)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("vet: %v", err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("vet: parse %s: %v", path, err)
		}
		g, ok := byName[f.Name.Name]
		if !ok {
			g = &pkgGroup{name: f.Name.Name, dir: filepath.Dir(path)}
			byName[f.Name.Name] = g
			groups = append(groups, g)
		}
		g.files = append(g.files, f)
		g.paths = append(g.paths, path)
		if strings.HasSuffix(path, "_test.go") {
			g.hasTest = true
		}
	}
	return groups, nil
}

// orderGroups returns groups with non-test groups first (in first-seen
// order), then test groups (in first-seen order), so the package under
// test is checked before any external test package that imports it.
func orderGroups(groups []*pkgGroup) []*pkgGroup {
	ordered := make([]*pkgGroup, 0, len(groups))
	for _, g := range groups {
		if !g.hasTest {
			ordered = append(ordered, g)
		}
	}
	for _, g := range groups {
		if g.hasTest {
			ordered = append(ordered, g)
		}
	}
	return ordered
}

// localImporter serves already-checked sibling packages — the package
// under test, for an external test package that imports it — before
// delegating everything else to the default importer.
type localImporter struct {
	fallback types.Importer
	pkgs     map[string]*types.Package
}

func (l *localImporter) Import(path string) (*types.Package, error) {
	if pkg, ok := l.pkgs[path]; ok {
		return pkg, nil
	}
	return l.fallback.Import(path)
}

// deriveImportPath returns the import path of the package in dir, from
// the enclosing go.mod: the module path plus the path of dir relative
// to the module root. It reports false when no enclosing go.mod exists
// or it has no module line.
func deriveImportPath(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for d := abs; ; {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			mod := parseModulePath(string(data))
			if mod == "" {
				return "", false
			}
			rel, err := filepath.Rel(d, abs)
			if err != nil {
				return "", false
			}
			if rel == "." {
				return mod, true
			}
			return mod + "/" + filepath.ToSlash(rel), true
		}
		if !os.IsNotExist(err) {
			return "", false
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// parseModulePath returns the module path declared in go.mod content,
// or "" when there is no module line.
func parseModulePath(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strings.Trim(fields[1], `"`)
			}
		}
	}
	return ""
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
