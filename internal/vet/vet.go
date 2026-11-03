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
//
// Diagnostics are returned sorted and deduplicated on identical
// (position, message): directory-scoped analyzers scan a directory's
// .s files once per pass, so without deduplication a directory holding
// both a package and its external test package would report each
// assembly finding twice. External test passes are checked after the
// other packages with an importer scoped to the test pass's own
// directory's package under test, so a test file can import the
// package under test by its module import path (or package clause
// name outside a module) even though that package has no installed
// export data — and never a same-named package from another
// directory, which outside a module would share its import path.
func Run(paths []string) ([]Diagnostic, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("vet: no files")
	}
	fset := token.NewFileSet()
	groups, err := groupFilesByPackage(fset, paths)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		groups[i].importPath = moduleImportPath(groups[i].dir, groups[i].name)
	}

	// Check non-test packages first, then external test packages, so a
	// test pass can import the package under test (see below). Output
	// is sorted at the end, so check order never affects diagnostic
	// order.
	var regular, testGroups []pkgGroup
	for _, g := range groups {
		if strings.HasSuffix(g.name, "_test") {
			testGroups = append(testGroups, g)
		} else {
			regular = append(regular, g)
		}
	}

	var out []Diagnostic
	var typeErrs []error
	// checked holds successfully type-checked non-test packages,
	// keyed by directory and package clause name, for the test-pass
	// importer below. It is deliberately NOT keyed by import path:
	// outside a module the import path falls back to the bare package
	// clause name, so two directories with the same clause would
	// collide on one key and the second package would silently shadow
	// the first for every test pass — and a clause colliding with a
	// stdlib name would shadow the real stdlib import. Keying by
	// directory keeps each test pass scoped to its own package.
	checked := make(map[string]*types.Package)
	check := func(g pkgGroup, imp types.Importer) {
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
		pkg, checkErr := conf.Check(g.importPath, fset, g.files, info)
		if checkErr != nil {
			typeErrs = append(typeErrs, fmt.Errorf("vet: type check failed for package %s in %s: %v", g.name, g.dir, checkErr))
		} else if pkg != nil && !strings.HasSuffix(g.name, "_test") {
			checked[g.dir+"\x00"+g.name] = pkg
		}
		// Analyzers run even when checking failed, on best-effort info.
		pass := &Pass{Fset: fset, Files: g.files, Info: info, Pkg: pkg}
		for _, a := range analyzers {
			out = append(out, a.Run(pass)...)
		}
	}
	for _, g := range regular {
		check(g, importer.Default())
	}
	// An external test package imports the package under test by its
	// module import path, but importer.Default() resolves only installed
	// export data, so that import always failed here. Feed the package
	// under test to each test pass's importer instead: a test pass
	// sees exactly its own directory's package under test as checked
	// from source in this same Run, and everything else still resolves
	// via Default. The package under test is the non-test group in the
	// same directory whose clause is the test group's base name; a
	// test pass never resolves a same-named package from another
	// directory, which outside a module shares its import path.
	// Only successfully checked packages are fed in: if the package
	// under test itself failed to type-check, the test pass honestly
	// fails too instead of importing partial type info.
	for _, g := range testGroups {
		base := strings.TrimSuffix(g.name, "_test")
		var underTest *types.Package
		var underTestPath string
		for _, r := range regular {
			if r.dir == g.dir && r.name == base {
				underTestPath = r.importPath
				underTest = checked[r.dir+"\x00"+r.name]
				break
			}
		}
		check(g, scopedImporter{path: underTestPath, pkg: underTest, fallback: importer.Default()})
	}
	return deduplicateDiagnostics(SortDiagnostics(out)), errors.Join(typeErrs...)
}

// scopedImporter resolves exactly one import — the package under test
// for a single external test pass, as checked from source in this Run
// — before falling back to the default (installed) importer for
// everything else, including stdlib imports. It never answers for a
// package from another directory, even one sharing the under-test
// package's clause name and hence its no-module import path.
type scopedImporter struct {
	path     string
	pkg      *types.Package
	fallback types.Importer
}

func (imp scopedImporter) Import(path string) (*types.Package, error) {
	if imp.pkg != nil && path == imp.path {
		return imp.pkg, nil
	}
	return imp.fallback.Import(path)
}

// moduleImportPath derives a group's import path: the enclosing go.mod's
// module path plus the group's directory relative to the module root,
// or the package clause name when no go.mod encloses the directory.
// This is the path a test file in the same directory imports the
// package under test by, and the path types.Config.Check records on
// the checked package, so the test pass's importer can match it.
func moduleImportPath(dir, clause string) string {
	d := dir
	for {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "//") {
					continue
				}
				f := strings.Fields(line)
				if len(f) >= 2 && f[0] == "module" {
					mod := strings.Trim(f[1], `"`)
					rel, err := filepath.Rel(d, dir)
					if err != nil || rel == "." {
						return mod
					}
					return mod + "/" + filepath.ToSlash(rel)
				}
			}
			// A go.mod without a module line is malformed; fall
			// through to the clause-name fallback rather than
			// inventing a path.
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return clause
}

// deduplicateDiagnostics drops identical (position, message) diagnostics,
// keeping the first in sorted order. It exists because directory-scoped
// analyzers (framepointer, asmdecl) scan a directory's .s files once per
// pass: a directory with both a package and its external test package
// otherwise reports each assembly finding twice, once per pass, where
// the pre-per-package vet reported it once. Deduplicating at the Run
// boundary — rather than skipping directory-scoped analyzers in later
// passes — preserves per-pass analysis (each pass still sees the .s
// files with its own type info) and covers any analyzer that can emit
// the same finding from two passes over one directory.
func deduplicateDiagnostics(ds []Diagnostic) []Diagnostic {
	seen := make(map[string]bool, len(ds))
	out := ds[:0]
	for _, d := range ds {
		key := d.Pos.Filename + "\x00" + fmt.Sprint(d.Pos.Line) + "\x00" + fmt.Sprint(d.Pos.Column) + "\x00" + d.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// pkgGroup is one real package pass: the files of a single package
// clause in a single directory.
type pkgGroup struct {
	dir        string // absolute directory, for error messages and grouping
	name       string // package clause name (e.g. "foo" or "foo_test")
	files      []*ast.File
	importPath string // module import path, set by Run (see moduleImportPath)
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
