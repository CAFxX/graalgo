// Package loading for vet.Run.
//
// The default invocation vets real packages, so files are loaded as
// packages — with module resolution and test variants — instead of being
// type-checked as a single synthetic package. Loading uses
// golang.org/x/tools/go/packages ("go list" under the hood); files that
// lie outside any Go module fall back to a best-effort single-package
// check with the standard library importer, so ad-hoc files keep working.
//
// In both paths, load and type-check errors are surfaced as diagnostics
// instead of being discarded: a vet run that silently says nothing on
// ordinary input is worse than one that errors.

package vet

import (
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// vetLoadMode requests syntax and type information for the matched
// packages and their dependencies. NeedDeps is set deliberately:
// without it, dependency types are read from export data, and the
// export-data reader in this x/tools version supports only export
// data up to version 2 while go1.27.1 emits version 4, so loading any
// package that imports a standard-library package fails fatally
// ("package ... without types was imported"). With NeedDeps the
// dependencies are type-checked from source instead, exactly as
// go vet does; that costs a full type-check of the import graph,
// but it is insensitive to export-data format changes.
var vetLoadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo

// loadPasses loads the named files and returns one Pass per loaded
// package variant, plus a diagnostic for every load error encountered.
// Files are grouped by directory so each directory costs one
// `go list` invocation; directories outside any Go module use the
// legacy single-package fallback instead.
func loadPasses(paths []string) ([]*Pass, []Diagnostic, error) {
	groups := make(map[string][]string)
	var dirs []string
	for _, p := range paths {
		d := filepath.Dir(p)
		if _, ok := groups[d]; !ok {
			dirs = append(dirs, d)
		}
		groups[d] = append(groups[d], p)
	}
	sort.Strings(dirs)

	var passes []*Pass
	var diags []Diagnostic
	for _, dir := range dirs {
		files := groups[dir]
		if findGoMod(dir) == "" {
			ps, ds, err := loadLegacy(files)
			if err != nil {
				return nil, nil, err
			}
			passes = append(passes, ps...)
			diags = append(diags, ds...)
			continue
		}
		ps, ds, err := loadPackages(dir, files)
		if err != nil {
			return nil, nil, err
		}
		passes = append(passes, ps...)
		diags = append(diags, ds...)
	}
	return passes, diags, nil
}

// findGoMod returns the go.mod enclosing dir, or "" when dir is outside
// any Go module.
func findGoMod(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return filepath.Join(d, "go.mod")
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// loadPackages loads files in dir as Go packages (with test variants),
// returning one Pass per package that has syntax plus a diagnostic for
// every load error (parse errors, type errors, unresolvable imports).
func loadPackages(dir string, files []string) ([]*Pass, []Diagnostic, error) {
	cfg := &packages.Config{
		Mode:  vetLoadMode,
		Dir:   dir,
		Tests: true,
	}
	patterns := make([]string, 0, len(files))
	for _, f := range files {
		patterns = append(patterns, "file="+f)
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, nil, fmt.Errorf("vet: loading packages in %s: %w", dir, err)
	}
	var passes []*Pass
	var diags []Diagnostic
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			diags = append(diags, packageErrorDiagnostic(e))
		}
		if len(pkg.Syntax) == 0 {
			continue
		}
		info := pkg.TypesInfo
		if info == nil {
			info = emptyTypesInfo()
		}
		fset := pkg.Fset
		if fset == nil {
			fset = token.NewFileSet()
		}
		passes = append(passes, &Pass{
			Fset:  fset,
			Files: pkg.Syntax,
			Info:  info,
			Pkg:   pkg.Types,
		})
	}
	return passes, diags, nil
}

// packageErrorDiagnostic converts a go/packages load error into a
// diagnostic. The error position is "file:line:col", "file:line", or
// empty; unpositioned errors keep an empty position.
func packageErrorDiagnostic(e packages.Error) Diagnostic {
	return Diagnostic{Pos: parseErrorPos(e.Pos), Message: e.Msg}
}

// parseErrorPos parses a "file:line:col" or "file:line" position string.
func parseErrorPos(s string) token.Position {
	if s == "" || s == "-" {
		return token.Position{}
	}
	fields := strings.Split(s, ":")
	// Collect trailing numeric fields (line, then column).
	nums := 0
	for i := len(fields) - 1; i >= 0; i-- {
		if _, err := strconv.Atoi(fields[i]); err != nil {
			break
		}
		nums++
	}
	if nums == 0 {
		return token.Position{Filename: s}
	}
	filename := strings.Join(fields[:len(fields)-nums], ":")
	pos := token.Position{Filename: filename}
	if nums >= 1 {
		pos.Line, _ = strconv.Atoi(fields[len(fields)-nums])
	}
	if nums >= 2 {
		pos.Column, _ = strconv.Atoi(fields[len(fields)-1])
	}
	return pos
}

// emptyTypesInfo is a non-nil *types.Info with initialized maps, so
// analyzers can read from it without a nil check.
func emptyTypesInfo() *types.Info {
	return &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
}

// loadLegacy type-checks files outside any Go module, one package per
// package clause, using the compiled export data of the standard
// library. Parse errors are reported per file and the remaining files
// are still checked; type-check errors are surfaced as diagnostics
// instead of being discarded.
func loadLegacy(files []string) ([]*Pass, []Diagnostic, error) {
	fset := token.NewFileSet()
	var parsed []*ast.File
	var diags []Diagnostic
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("vet: %v", err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			diags = append(diags, parseErrorDiagnostic(err))
			continue
		}
		parsed = append(parsed, f)
	}
	if len(parsed) == 0 && len(diags) == 0 {
		return nil, nil, fmt.Errorf("vet: no files")
	}

	// Split by package clause: a tested package's foo.go (package foo)
	// and foo_test.go (package foo_test) are two packages, not one.
	groups := make(map[string][]*ast.File)
	var names []string
	for _, f := range parsed {
		name := f.Name.Name
		if _, ok := groups[name]; !ok {
			names = append(names, name)
		}
		groups[name] = append(groups[name], f)
	}
	sort.Strings(names)

	var passes []*Pass
	for _, name := range names {
		groupFiles := groups[name]
		info := emptyTypesInfo()
		conf := types.Config{
			Importer:                 importer.Default(),
			IgnoreFuncBodies:         false,
			DisableUnusedImportCheck: true,
		}
		pkg, err := conf.Check(name, fset, groupFiles, info)
		if err != nil {
			diags = append(diags, typeErrorDiagnostic(fset, err))
		}
		passes = append(passes, &Pass{Fset: fset, Files: groupFiles, Info: info, Pkg: pkg})
	}
	return passes, diags, nil
}

// parseErrorDiagnostic converts a parser error into a diagnostic
// positioned at the first reported error.
func parseErrorDiagnostic(err error) Diagnostic {
	var el scanner.ErrorList
	if errors.As(err, &el) {
		for _, e := range el {
			return Diagnostic{Pos: e.Pos, Message: e.Msg}
		}
	}
	return Diagnostic{Message: err.Error()}
}

// typeErrorDiagnostic converts a go/types check error into a diagnostic
// positioned where the checker reported it.
func typeErrorDiagnostic(fset *token.FileSet, err error) Diagnostic {
	var terr types.Error
	if errors.As(err, &terr) {
		pos := terr.Pos
		if terr.Fset != nil {
			return Diagnostic{Pos: terr.Fset.Position(pos), Message: terr.Msg}
		}
		return Diagnostic{Pos: fset.Position(pos), Message: terr.Msg}
	}
	var terrp *types.Error
	if errors.As(err, &terrp) {
		pos := terrp.Pos
		if terrp.Fset != nil {
			return Diagnostic{Pos: terrp.Fset.Position(pos), Message: terrp.Msg}
		}
		return Diagnostic{Pos: fset.Position(pos), Message: terrp.Msg}
	}
	return Diagnostic{Message: err.Error()}
}

// dedupeDiagnostics drops repeated diagnostics (same position and
// message), keeping the first occurrence. A file shared by a package
// and its test variant is analyzed twice, so without this its findings
// would be reported twice.
func dedupeDiagnostics(ds []Diagnostic) []Diagnostic {
	seen := make(map[Diagnostic]bool, len(ds))
	out := ds[:0]
	for _, d := range ds {
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
