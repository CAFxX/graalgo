// Package checker implements stage 2 of the frontend/v2 pipeline:
// untyped AST → type-checked package.
//
// This wraps the standard library's go/types. The importer resolves
// imports from the real GOROOT and module cache — no synthetic packages,
// no overlay, no shadowing. If a package cannot be found on disk, that
// is a compile error, not a signal to invent a fake.
package checker

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
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
}

// Check type-checks pkg (from stage 1) and returns the typed result.
//
// The importer is go/importer's compiled-package importer over the real
// toolchain: imports resolve to actual compiled package data, not
// synthetic stand-ins. There is deliberately no fallback to synthetic
// packages — a missing import is an error.
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

	// Real importer only. importer.Default() uses the compiled package
	// data from the toolchain that built this binary — i.e., the real
	// stdlib, not a synthetic copy.
	imp := importer.Default()

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
