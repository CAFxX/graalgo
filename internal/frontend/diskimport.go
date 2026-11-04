package main

// WS G (P3-M9, module-probe lane): general third-party package import.
//
// Any import path not claimed by the synthetic overlay resolves from local
// disk. The package's .go files are selected constraint-aware via go/build
// (GOOS/GOARCH build tags, so e.g. node_js.go is skipped on linux), parsed
// and type-checked with the same overlayImporter (its own imports resolve
// recursively through the same mechanism), and merged into the export
// document through the exact syntheticPackage machinery
// (importSyntheticPackage): function bodies, package vars, init()
// functions, and generic instantiations all flow through the same path as
// synthetic packages. Nothing here is module-specific: this is the general
// import overlay WS G always owned.
//
// Version selection is module-aware (review 2026-11-04, P1-04/P1-05; see
// diskmodule.go): when the package being compiled belongs to a module,
// the version of every third-party import comes ONLY from that module's
// go.mod (with replace applied), and any other outcome is a loud error.
// Only compilations outside any module (standalone files, corpus
// fixtures) keep the legacy order — newest version in the module cache,
// then GOPATH/src, then GOROOT — because there is no go.mod to select
// from. Standard-library imports always resolve from the toolchain
// GOROOT, never from the cache or GOPATH.
//
// Build inputs vs ambient environment: the module-cache LOCATION is a
// machine input (explicit --modcache flag, else GOMODCACHE, else the
// GOPATH default), but under module mode it can only affect availability,
// never the selected version. GOROOT is pinned by the explicit --goroot
// flag, else the toolchain that built gofrontend: the CLI clears the
// ambient GOROOT variable at startup (see main.go) because
// runtime.GOROOT() consults it first. GRAALGO_BUILD_TAGS is a build input
// in the same sense as -tags — extra build tags selecting which files
// compile — not an ambient default: it is honoured only when the
// --build-tags flag is absent, and a build with different tags is a
// different build. The backend launcher may pass --modcache/--goroot/
// --build-tags explicitly to pin CI builds; when it does not, the flag
// defaults above apply.

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

// Explicit argv inputs (see main.go) for disk resolution. Empty means
// "not given": the flag fallbacks below apply. buildTagsSet distinguishes
// "--build-tags=" (explicitly no extra tags, env ignored) from the flag
// being absent (GRAALGO_BUILD_TAGS honoured).
var (
	diskFlagModCache     string
	diskFlagGoroot       string
	diskFlagBuildTags    []string
	diskFlagBuildTagsSet bool
)

// setDiskFlags installs the explicit argv inputs for disk resolution,
// as parsed by main (or by tests, which assign the globals directly).
// Changing the cache or GOROOT location invalidates the disk caches:
// a directory resolved under one location must never be reused for
// another.
func setDiskFlags(modCache, goroot string, buildTags []string, buildTagsSet bool) {
	diskMu.Lock()
	defer diskMu.Unlock()
	if modCache != diskFlagModCache || goroot != diskFlagGoroot {
		diskDirCache = map[string]string{}
		diskCache = map[string]*syntheticPackage{}
	}
	diskFlagModCache = modCache
	diskFlagGoroot = goroot
	diskFlagBuildTags = buildTags
	diskFlagBuildTagsSet = buildTagsSet
}

// goModCache returns the local module cache directory: the explicit
// --modcache flag wins, then GOMODCACHE, then the GOPATH default.
func goModCache() string {
	if diskFlagModCache != "" {
		return diskFlagModCache
	}
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

// diskGoroot returns the toolchain GOROOT disk resolution uses: the
// explicit --goroot flag wins, else runtime.GOROOT(). The CLI pins the
// latter to the toolchain that built the binary by clearing the ambient
// GOROOT variable at startup (see main.go); direct API callers that
// need the same pin must pass --goroot or clear GOROOT themselves.
func diskGoroot() string {
	if diskFlagGoroot != "" {
		return diskFlagGoroot
	}
	return runtime.GOROOT()
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
// Under a module context (see diskmodule.go) the version comes ONLY from
// the importing module's go.mod and anything else fails loudly; without
// one (legacy mode) the module cache wins
// ($GOMODCACHE/<escaped path>@<highest version>) — the probe harness
// pins the cache to the intended version — falling back to
// GOPATH/src/<path>, then to the toolchain's GOROOT/src/<path> so plain
// stdlib packages outside the synthetic overlay (e.g.
// database/sql/driver) resolve through the same general mechanism.
// Synthetic-overlay paths never resolve from disk, wherever the call
// comes from: the disk fallback exists for stdlib outside the overlay
// and third-party modules. GOROOT must never shadow a synthetic package —
// building e.g. the real GOROOT bytes as a disk package registers its
// multi-result members in the global disk-MR table, which then shadows
// the synthetic fail-fast/desugar contracts for every later export in
// the process (TestSyntheticBytesCutFailsFast isolation). The importer's
// cases claim synthetic paths first, but direct getDiskPackage callers
// (multiResultHelperFunc) bypass the importer, so the guard lives here.
func resolveDiskPackageDir(importPath string) (string, error) {
	if _, ok := syntheticSources[importPath]; ok {
		return "", fmt.Errorf("%w: %q (synthetic overlay)", errDiskNotFound, importPath)
	}
	diskMu.Lock()
	if d, ok := diskDirCache[importPath]; ok {
		diskMu.Unlock()
		return d, nil
	}
	diskMu.Unlock()

	resolve := func() (string, error) {
		// Module mode (P1-04): when ExportFiles installed a module
		// context, third-party versions come ONLY from its go.mod.
		// A module path that go.mod does not require is a loud error
		// here, never a scan of whatever the cache happens to hold;
		// a standard-library path resolves from the toolchain GOROOT
		// only, never from the cache or GOPATH.
		if ctx := activeModuleContext(); ctx != nil {
			if dir, handled, err := ctx.resolveModuleDir(importPath, goModCache()); handled {
				return dir, err
			}
			if goroot := diskGoroot(); goroot != "" {
				dir := filepath.Join(goroot, "src", importPath)
				if st, err := os.Stat(dir); err == nil && st.IsDir() {
					return dir, nil
				}
			}
			return "", fmt.Errorf("%w: %q", errDiskNotFound, importPath)
		}
		// Legacy mode: no module context (standalone files, corpus
		// fixtures). Newest cached version first; the probe harness
		// pins the cache to the intended version.
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
		if goroot := diskGoroot(); goroot != "" {
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
	// P3-driver-harden (jsonlib lane): extra build tags select pure-Go
	// fallbacks over assembly stubs (e.g. purego for segmentio/asm).
	// GRAALGO_BUILD_TAGS (space-separated) is a BUILD INPUT, not an
	// ambient default: it changes which files compile, so a build with
	// different tags is a different build. The explicit --build-tags
	// flag wins when given; otherwise the env var is honoured for
	// backward compatibility with the launcher, which is expected to
	// pass the flag to pin CI builds.
	// ASM Phase 1 (pure-Go fallback selection): graalgo cannot process Go
	// assembly (.s files), so the purego tag is always on. This selects
	// pure-Go fallbacks (*_generic.go, `|| purego`) over assembly
	// declarations (`!purego`) using Go's own general mechanism for "no
	// assembly" — upstream-validated, no per-package lists. Bodyless
	// functions with no pure-Go fallback (e.g. internal/cpu.cpuid) get
	// the general "unimplemented" marker (exportLinknameDecl); they are
	// never invoked because graalgo's runtime does not call
	// internal/cpu.Initialize and the cpu.X86 feature flags stay zero,
	// forcing callers onto their generic paths.
	ctxt := build.Default
	extraTags := diskFlagBuildTags
	if !diskFlagBuildTagsSet {
		extraTags = strings.Fields(os.Getenv("GRAALGO_BUILD_TAGS"))
	}
	ctxt.BuildTags = append(append([]string{"purego"}, ctxt.BuildTags...), extraTags...)
	bp, err := ctxt.ImportDir(dir, 0)
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
	// General (real-x509 lane): eta-expand `var x = pkg.F` / `x := pkg.F`
	// where F is multi-result (e.g. path/filepath's `var lstat = os.Lstat`).
	// For package-level vars, records aliases instead of expanding.
	etaVarLits, varAliases := etaExpandMRFuncValues(files, info, pkg, true)
	// Stubs served type-checking; keep them for method resolution
	// (the desugar rewrites all calls, so stubs are never exported).
	// Mark them via the spec so the exporter can skip them.
	registerDiskMRSpecs(mrSpecs)
	return &syntheticPackage{
		path: importPath, pkg: pkg,
		files: files, info: info, fset: fset, raws: raws,
		etaVarLits: etaVarLits, varFuncAliases: varAliases,
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
// importSyntheticPackage; signature-only stub paths (runtime, fmt,
// unsafe — no bodies to merge, their call sites are handled
// by existing wire logic) are walked with nothing to merge; anything else
// resolves from disk (sync and sync/atomic included, since 503608ea
// retired their synthetic packages). Idempotent via e.importedPkgTree (and
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
	if isDedicatedSyntheticStub(path) {
		return nil, nil
	}
	return getDiskPackage(path)
}
