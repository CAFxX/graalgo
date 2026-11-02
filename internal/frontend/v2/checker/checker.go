// Package checker implements stage 2 of the frontend/v2 pipeline:
// untyped AST → type-checked package.
//
// This wraps the standard library's go/types. Imports resolve through
// golang.org/x/tools/go/packages, i.e. through the real `go list`
// driver in module mode: stdlib, module-local packages, and module
// dependencies all resolve from their real sources, in the importing
// file's own module context. There is no synthetic package, no overlay,
// and no shadowing. If a package cannot be found, that is a compile
// error, not a signal to invent a fake.
package checker

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Result is the output of stage 2.
type Result struct {
	// Pkg is the type-checked package.
	Pkg *types.Package
	// Info carries per-node type information: definitions, uses,
	// selections, and types.
	Info *types.Info
	// Fset is the file set from stage 1 (positions are stable across stages).
	Fset *token.FileSet
}

// Config controls stage 2 behavior.
type Config struct {
	// GoVersion is the language version to type-check against, e.g. "go1.27".
	// Empty means the toolchain default.
	GoVersion string
	// Dir is the directory in which imports are resolved (the module
	// context). Empty means derive it from the checked package's files:
	// go/types reports the importing file's directory on every import,
	// and that directory is used first; Dir is only the fallback for
	// in-memory sources whose file names carry no directory. Empty
	// throughout means the process working directory.
	Dir string
}

// Check type-checks pkg (from stage 1) and returns the typed result.
//
// Imports resolve through a real, module-aware importer (see
// moduleImporter): every import is loaded from its real source via
// `go list`, in the importing file's module context. There is
// deliberately no fallback to synthetic packages — a missing import
// is an error.
func Check(fset *token.FileSet, pkg *ast.Package, conf *Config) (*Result, error) {
	if conf == nil {
		conf = &Config{}
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}

	imp := &moduleImporter{
		fallbackDir: conf.Dir,
		cache:       make(map[importKey]*types.Package),
	}
	// Pre-seed the fallback directory from the checked files so that
	// in-memory sources (whose names carry no directory) still resolve
	// when they live in a real directory. The per-file directory
	// reported by go/types takes precedence at import time.
	if imp.fallbackDir == "" {
		imp.fallbackDir = dirOfFirstFile(pkg)
	}

	files := make([]*ast.File, 0, len(pkg.Files))
	for _, f := range pkg.Files {
		files = append(files, f)
	}

	tconf := &types.Config{
		Importer: imp,
		// go/types ignores //go:linkname for type-checking purposes, which
		// is correct: linkname is a linker-level mechanism, not a type-level
		// one. The backend resolves the actual symbol; if it cannot, that
		// is a backend gap, not a frontend workaround.
		IgnoreFuncBodies: false,
	}
	if conf.GoVersion != "" {
		tconf.GoVersion = conf.GoVersion
	}

	tpkg, err := tconf.Check(pkg.Name, fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("checker: type-check failed: %v", err)
	}
	return &Result{Pkg: tpkg, Info: info, Fset: fset}, nil
}

// dirOfFirstFile returns the directory of the first file of pkg in
// sorted file-name order (sorted so the choice is deterministic despite
// pkg.Files being a map), or "" if no file name carries a directory.
func dirOfFirstFile(pkg *ast.Package) string {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if dir := filepath.Dir(name); dir != "" && dir != "." {
			return dir
		}
	}
	return ""
}

type importKey struct {
	path   string
	srcDir string
}

// moduleImporter is a types.ImporterFrom that resolves each import with
// packages.Load, i.e. with the real `go list` driver in module mode.
//
// Why not importer.Default(): that is the gc importer with a nil
// lookup, which resolves stdlib only (via `go list -export` against the
// checker's own GOROOT) and anything else only from prebuilt .a files
// in GOPATH/pkg — which module packages never have. Any program
// importing a module-local package therefore failed with "can't find
// import". packages.Load instead asks `go list` to resolve the import
// path in the importing file's directory, so module membership, replace
// directives, and vendoring are all honored exactly as the `go` tool
// itself honors them.
//
// The importer loads only real packages: a load error, a package with
// errors, or a package without type information is returned as an
// import error. Nothing is synthesized, stubbed, or faked.
type moduleImporter struct {
	fallbackDir string
	cache       map[importKey]*types.Package
}

func (m *moduleImporter) Import(path string) (*types.Package, error) {
	return m.ImportFrom(path, "", 0)
}

func (m *moduleImporter) ImportFrom(path, srcDir string, mode types.ImportMode) (*types.Package, error) {
	if mode != 0 {
		return nil, fmt.Errorf("checker: unsupported import mode %d for %q", mode, path)
	}
	if srcDir == "" {
		srcDir = m.fallbackDir
	}
	key := importKey{path: path, srcDir: srcDir}
	if tpkg, ok := m.cache[key]; ok {
		return tpkg, nil
	}
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName |
			packages.NeedTypes |
			packages.NeedDeps |
			packages.NeedImports,
		Dir:   srcDir,
		Tests: false,
	}, path)
	if err != nil {
		return nil, fmt.Errorf("checker: could not load import %q: %w", path, err)
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("checker: could not load import %q: %d packages returned", path, len(loaded))
	}
	pkg := loaded[0]
	if len(pkg.Errors) > 0 {
		errs := make([]string, 0, len(pkg.Errors))
		for _, e := range pkg.Errors {
			errs = append(errs, e.Error())
		}
		return nil, fmt.Errorf("checker: could not load import %q: %s", path, strings.Join(errs, "; "))
	}
	if pkg.Types == nil {
		return nil, fmt.Errorf("checker: could not load import %q: no type information", path)
	}
	if pkg.Types.Name() == "" || pkg.Types.Name() == "_" {
		return nil, fmt.Errorf("checker: could not load import %q: invalid package name %q", path, pkg.Types.Name())
	}
	m.cache[key] = pkg.Types
	return pkg.Types, nil
}
