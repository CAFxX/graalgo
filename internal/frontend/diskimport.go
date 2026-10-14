package main

// WS G (P3-M9, module-probe lane): general third-party package import.
//
// Any import path not claimed by the synthetic overlay resolves from local
// disk: the module cache ($GOMODCACHE/<path>@<highest version>) first, then
// GOPATH/src/<path>. The package's .go files are selected
// constraint-aware via go/build (GOOS/GOARCH build tags, so e.g.
// node_js.go is skipped on linux), parsed and type-checked with the same
// overlayImporter (its own imports resolve recursively through the same
// mechanism), and merged into the export document through the exact
// syntheticPackage machinery (importSyntheticPackage): function bodies,
// package vars, init() functions, and generic instantiations all flow
// through the same path as synthetic packages. Nothing here is
// module-specific: this is the general import overlay WS G always owned.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// errDiskNotFound reports that an import path resolves to no source
// directory on disk. The overlay importer maps this to the pre-existing
// WS G scope error (failImporter); any other build error is a genuine
// defect in the imported package and is reported as such.
var errDiskNotFound = errors.New("not found on disk")

var (
	diskMu       sync.Mutex
	diskCache    = map[string]*syntheticPackage{}
	diskDirCache = map[string]string{} // import path -> resolved source dir
	diskBuilding = map[string]bool{}   // cycle guard for recursive imports
)

// goModCache returns the local module cache directory.
func goModCache() string {
	if v := os.Getenv("GOMODCACHE"); v != "" {
		return v
	}
	gp := os.Getenv("GOPATH")
	if gp == "" {
		if h, err := os.UserHomeDir(); err == nil {
			gp = filepath.Join(h, "go")
		}
	}
	if gp == "" {
		return ""
	}
	return filepath.Join(strings.Split(gp, string(os.PathListSeparator))[0], "pkg", "mod")
}

// escapeModulePath applies the module-cache path escaping: every uppercase
// letter becomes '!' followed by its lowercase form.
func escapeModulePath(p string) string {
	var sb strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			sb.WriteRune('!')
			sb.WriteRune(r - 'A' + 'a')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// compareSemver compares "v1.6.0"-style versions numerically; a
// lexicographic fallback orders pseudo-versions deterministically.
func compareSemver(a, b string) int {
	norm := func(v string) []string {
		v = strings.TrimPrefix(v, "v")
		if i := strings.Index(v, "-"); i >= 0 {
			v = v[:i]
		}
		if i := strings.Index(v, "+"); i >= 0 {
			v = v[:i]
		}
		return strings.Split(v, ".")
	}
	pa, pb := norm(a), norm(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea == nil && eb == nil && na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// resolveDiskPackageDir maps an import path to a source directory on disk.
// Module cache wins ($GOMODCACHE/<escaped path>@<highest version>); the
// probe harness pins the cache to the intended version. Falls back to
// GOPATH/src/<path>, then to the toolchain's GOROOT/src/<path> so plain
// stdlib packages outside the synthetic overlay (e.g.
// database/sql/driver) resolve through the same general mechanism.
// Synthetic-overlay paths never reach here (the importer's cases claim
// them first), so GOROOT can never shadow a synthetic package.
func resolveDiskPackageDir(importPath string) (string, error) {
	diskMu.Lock()
	if d, ok := diskDirCache[importPath]; ok {
		diskMu.Unlock()
		return d, nil
	}
	diskMu.Unlock()

	resolve := func() (string, error) {
		if mc := goModCache(); mc != "" {
			// P4 (driver-harden): subpackage imports (e.g.
			// github.com/google/go-cmp/cmp). The module cache stores
			// the MODULE with the version (go-cmp@v0.7.0/cmp), not the
			// package. Try progressively shorter prefixes: the full
			// path as a module first, then stripping trailing elements
			// as subpackage paths.
			escaped := escapeModulePath(importPath)
			elems := strings.Split(escaped, string(filepath.Separator))
			for i := len(elems); i >= 1; i-- {
				modEscaped := strings.Join(elems[:i], string(filepath.Separator))
				subPath := strings.Join(elems[i:], string(filepath.Separator))
				parent := filepath.Join(mc, filepath.Dir(modEscaped))
				base := filepath.Base(modEscaped)
				entries, err := os.ReadDir(parent)
				if err != nil {
					continue
				}
				best := ""
				for _, e := range entries {
					name := e.Name()
					if !e.IsDir() || !strings.HasPrefix(name, base+"@") {
						continue
					}
					ver := strings.TrimPrefix(name, base+"@")
					if best == "" || compareSemver(ver, best) > 0 {
						best = ver
					}
				}
				if best != "" {
					dir := filepath.Join(parent, base+"@"+best)
					if subPath != "" {
						dir = filepath.Join(dir, subPath)
					}
					if st, err := os.Stat(dir); err == nil && st.IsDir() {
						return dir, nil
					}
				}
			}
		}
		gp := os.Getenv("GOPATH")
		if gp == "" {
			if h, err := os.UserHomeDir(); err == nil {
				gp = filepath.Join(h, "go")
			}
		}
		for _, root := range strings.Split(gp, string(os.PathListSeparator)) {
			if root == "" {
				continue
			}
			dir := filepath.Join(root, "src", importPath)
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				return dir, nil
			}
		}
		// Standard library outside the synthetic overlay: resolve from the
		// toolchain that built gofrontend (matches the gc leg's version).
		if goroot := runtime.GOROOT(); goroot != "" {
			dir := filepath.Join(goroot, "src", importPath)
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				return dir, nil
			}
		}
		return "", fmt.Errorf("%w: %q", errDiskNotFound, importPath)
	}

	dir, err := resolve()
	if err != nil {
		return "", err
	}
	diskMu.Lock()
	diskDirCache[importPath] = dir
	diskMu.Unlock()
	return dir, nil
}

// isDiskImportable reports whether importPath resolves to a source
// directory on disk (cheap: directory lookup only, no parsing).
func isDiskImportable(importPath string) bool {
	_, err := resolveDiskPackageDir(importPath)
	return err == nil
}

// getDiskPackage returns the built *syntheticPackage for a third-party
// import path, building and caching it on first use. Recursive imports
// (a disk package importing another disk package) work: the type-check
// below runs outside diskMu, so the overlay importer's recursive call
// cannot deadlock. Import cycles are illegal Go; the in-progress guard
// turns them into an error instead of infinite recursion.
func getDiskPackage(importPath string) (*syntheticPackage, error) {
	diskMu.Lock()
	if sp, ok := diskCache[importPath]; ok {
		diskMu.Unlock()
		return sp, nil
	}
	if diskBuilding[importPath] {
		diskMu.Unlock()
		return nil, fmt.Errorf("gofrontend: import cycle involving %q", importPath)
	}
	diskBuilding[importPath] = true
	diskMu.Unlock()

	sp, err := buildDiskPackage(importPath)

	diskMu.Lock()
	delete(diskBuilding, importPath)
	if err != nil {
		diskMu.Unlock()
		return nil, err
	}
	if prev, ok := diskCache[importPath]; ok {
		diskMu.Unlock()
		return prev, nil
	}
	diskCache[importPath] = sp
	diskMu.Unlock()
	return sp, nil
}

// buildDiskPackage parses and type-checks the third-party package at its
// resolved source directory, returning it in the syntheticPackage shape so
// the export machinery consumes it unchanged.
func buildDiskPackage(importPath string) (*syntheticPackage, error) {
	dir, err := resolveDiskPackageDir(importPath)
	if err != nil {
		return nil, err
	}
	// Constraint-aware file selection (GOOS/GOARCH build tags, including
	// the legacy // +build lines): _test.go files are never in GoFiles.
	bp, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		return nil, fmt.Errorf("gofrontend: cannot load package %q from %s: %w", importPath, dir, err)
	}
	if len(bp.GoFiles) == 0 {
		return nil, fmt.Errorf("gofrontend: package %q has no buildable Go files in %s", importPath, dir)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var raws [][]byte
	for _, name := range bp.GoFiles {
		full := filepath.Join(dir, name)
		src, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("gofrontend: cannot read %s: %w", full, err)
		}
		// P3-M8 (wave 2): ParseComments, like ExportFile and
		// buildSyntheticPackage, so //go: directives stay visible.
		f, err := parser.ParseFile(fset, full, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("gofrontend: parse error in %q (%s): %w", importPath, name, err)
		}
		files = append(files, f)
		raws = append(raws, src)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	// The same overlay importer: the package's own imports resolve
	// recursively (synthetic, stub, or further disk packages).
	// WS G (multi-result): rewrite multi-result FuncDecls to the
	// __-helper + stub shape BEFORE type-checking, so call sites
	// type-check against the stub's original signature.
	mrSpecs := transformDiskMultiResult(importPath, files)
	conf := types.Config{Importer: overlayImporter{}}
	pkg, err := conf.Check(importPath, fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("gofrontend: type error in %q: %w", importPath, err)
	}
	// Stubs served type-checking; keep them for method resolution
	// (the desugar rewrites all calls, so stubs are never exported).
	// Mark them via the spec so the exporter can skip them.
	registerDiskMRSpecs(mrSpecs)
	return &syntheticPackage{
		path: importPath, pkg: pkg,
		files: files, info: info, fset: fset, raws: raws,
	}, nil
}

// importedPackage resolves an importedSynthetic key to its
// *syntheticPackage, whether from the synthetic overlay or built from
// disk (WS G). Disk packages are cached by getDiskPackage, so this
// returns the exact package importSyntheticPackage merged.
func (e *exporter) importedPackage(p string) (*syntheticPackage, error) {
	if _, ok := syntheticSources[p]; ok {
		return getSyntheticPackage(p), nil
	}
	return getDiskPackage(p)
}

// importPkgTree merges path and its transitive dependencies into the
// export document, dependencies first (post-order: valid Go initialization
// order), sorted for determinism. Synthetic paths reuse the existing
// importSyntheticPackage; signature-only stub paths (runtime, fmt, sync,
// sync/atomic, unsafe — no bodies to merge, their call sites are handled
// by existing wire logic) are walked with nothing to merge; anything else
// resolves from disk. Idempotent via e.importedPkgTree (and
// e.importedSynthetic for overlap with the needX direct-import flow), so
// sharing the tree with the synthetic flow is safe.
func (e *exporter) importPkgTree(path string) error {
	if e.importedSynthetic[path] || e.importedPkgTree[path] {
		return nil
	}
	e.importedPkgTree[path] = true
	sp, err := resolvePkgTree(path)
	if err != nil {
		return err
	}
	if sp == nil {
		return nil
	}
	imports := sp.pkg.Imports()
	ipaths := make([]string, 0, len(imports))
	for _, imp := range imports {
		ipaths = append(ipaths, imp.Path())
	}
	sort.Strings(ipaths)
	for _, ip := range ipaths {
		if err := e.importPkgTree(ip); err != nil {
			return err
		}
	}
	return e.importSyntheticPackage(sp)
}

// resolvePkgTree maps an import path to its *syntheticPackage for body
// merging, or (nil, nil) for signature-only stubs with no bodies.
func resolvePkgTree(path string) (*syntheticPackage, error) {
	if _, ok := syntheticSources[path]; ok {
		return getSyntheticPackage(path), nil
	}
	switch path {
	case "runtime", "fmt", "sync", "sync/atomic", "unsafe":
		return nil, nil
	}
	return getDiskPackage(path)
}
