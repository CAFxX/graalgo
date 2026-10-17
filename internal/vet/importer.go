package vet

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"sync"
)

// sourceImporter is a types.Importer that resolves imports from Go source
// code instead of compiled export data.
//
// Design decision (C2/QN-13): importer.Default() is the gc export-data
// importer, which looks for compiled package archives under $GOROOT/pkg.
// Those archives are no longer shipped (that directory now holds only
// include/ and tool/), so with importer.Default() no import ever
// resolved: every import-dependent analyzer silently degraded to a no-op,
// and type-gated checks false-positived on the resulting nil types.
// The alternative of shelling out to `go list -export` (what go vet does)
// would hard-wire a dependency on the reference toolchain into
// `graalgo vet`, which must work as a drop-in `go` replacement without
// `go` installed. Type-checking from source is hermetic and offline, and
// yields identical type information for vet purposes (the compiler
// type-checks these same sources).
//
// Resolution is purely by import path via go/build (GOROOT for the
// standard library, GOPATH otherwise; GOOS/GOARCH and build constraints
// honored; test files excluded). No package names are special-cased: the
// only fixed names are "unsafe" (which go/types itself special-cases via
// types.Unsafe) and "C" (handled by types.Config.FakeImportC).
//
// Imports that cannot be resolved are recorded and surfaced by Run as
// diagnostics ("could not import ...") instead of silently degrading
// (QN-13). Analyzers then run on partial type info, and type-gated checks
// must treat unresolvable types as unknown rather than as violations.
type sourceImporter struct {
	fset  *token.FileSet
	sizes types.Sizes
	def   types.Importer // export-data fast path (may resolve nothing)

	mu       sync.Mutex
	pkgs     map[string]*types.Package
	inflight map[string]bool // recursion guard for import cycles
	failed   map[string]error
}

func newSourceImporter(fset *token.FileSet) *sourceImporter {
	return &sourceImporter{
		fset:     fset,
		sizes:    types.SizesFor("gc", runtime.GOARCH),
		def:      importer.Default(),
		pkgs:     make(map[string]*types.Package),
		inflight: make(map[string]bool),
		failed:   make(map[string]error),
	}
}

// Import implements types.Importer.
func (s *sourceImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	s.mu.Lock()
	if pkg, ok := s.pkgs[path]; ok {
		s.mu.Unlock()
		return pkg, nil
	}
	if err, ok := s.failed[path]; ok {
		s.mu.Unlock()
		return nil, err
	}
	if s.inflight[path] {
		s.mu.Unlock()
		return nil, fmt.Errorf("import cycle detected at %q", path)
	}
	s.inflight[path] = true
	s.mu.Unlock()

	pkg, err := s.importOne(path)

	s.mu.Lock()
	delete(s.inflight, path)
	if err != nil {
		s.failed[path] = err
	} else {
		s.pkgs[path] = pkg
	}
	s.mu.Unlock()
	return pkg, err
}

// failures returns a snapshot of the recorded import failures.
func (s *sourceImporter) failures() map[string]error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]error, len(s.failed))
	for p, err := range s.failed {
		out[p] = err
	}
	return out
}

// importOne resolves a single import path: compiled export data first,
// then source via go/build.
func (s *sourceImporter) importOne(path string) (*types.Package, error) {
	// Fast path: pre-compiled export data (e.g. GOPATH/pkg archives),
	// when present.
	if pkg, err := s.def.Import(path); err == nil {
		return pkg, nil
	}
	// Source path: locate the package directory. go/build resolves the
	// standard library via GOROOT and third-party packages via GOPATH.
	bp, err := build.Default.Import(path, "", build.IgnoreVendor)
	if err != nil {
		return nil, err
	}
	if len(bp.GoFiles) == 0 {
		return nil, fmt.Errorf("no buildable Go files in %s", bp.Dir)
	}
	files := make([]*ast.File, 0, len(bp.GoFiles))
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(s.fset, filepath.Join(bp.Dir, name), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	conf := types.Config{
		Importer: s, // recursive: dependencies resolve the same way
		Sizes:    s.sizes,
		// Function bodies of dependencies cannot affect their package's
		// type information; skipping them keeps import resolution fast.
		IgnoreFuncBodies:         true,
		DisableUnusedImportCheck: true,
		Error:                    func(error) {}, // best effort; partial type info is still useful
	}
	pkg, _ := conf.Check(path, s.fset, files, nil)
	if pkg == nil {
		return nil, fmt.Errorf("type-check of %q produced no package", path)
	}
	return pkg, nil
}
