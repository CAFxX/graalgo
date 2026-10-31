package main

// WS G (P3-M9, module-probe lane): general third-party package import.
//
// Any import path not claimed by the synthetic overlay resolves from local
// disk, in this order (see resolveDiskPackageDir):
//  1. the version pinned by the importing module's go.mod (require, with
//     replace applied; a local-path replace resolves to that directory),
//  2. as a fallback that warns on stderr, the highest version present in
//     the module cache (used when there is no enclosing go.mod, the module
//     is not required, or the pinned version is absent from the cache),
//  3. GOPATH/src/<path>,
//  4. the pinned toolchain's GOROOT/src/<path> for non-synthetic stdlib
//     (the GOROOT tree's VERSION file must match the toolchain that built
//     gofrontend; see pinnedGoroot).
// The package's .go files are selected
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
	diskDirCache = map[string]string{} // disk cache key -> resolved source dir
	diskBuilding = map[string]bool{}   // cycle guard for recursive imports
)

// diskCacheKey keys the directory cache (diskDirCache) by import path AND
// by the importing module's go.mod directory: two modules may pin
// different versions of the same import path, and a path-only key would
// serve the first module's resolved directory to the second. It is NOT
// used for the built-package cache (diskCache): see getDiskPackage for
// why that cache stays path-keyed. With no importing-module context
// (direct getDiskPackage callers outside ExportFiles) the key degrades to
// the bare import path, matching the previous behavior.
func diskCacheKey(importPath string) string {
	if m := currentImportingModule(); m != nil {
		return importPath + "\x00" + m.dir
	}
	return importPath
}

// warnDiskFallback reports a highest-version fallback on stderr. The
// resolution result is cached, so each (module, import path) warns once
// per process, not once per import site.
func warnDiskFallback(importPath, reason, chosen string) {
	fmt.Fprintf(os.Stderr, "gofrontend: warning: import %q: %s; using highest version on disk (%s)\n", importPath, reason, chosen)
}

// pinnedGoroot returns the GOROOT whose stdlib sources disk imports may
// read: runtime.GOROOT(), verified against its own VERSION file. The
// build toolchain's version (runtime.Version()) must match the first line
// of $GOROOT/VERSION; a GOROOT environment override pointing at a
// different toolchain is refused instead of silently mixing stdlib
// versions. (Go 1.27's VERSION file carries a build timestamp on its
// second line; only the first line is the version.)
func pinnedGoroot() (string, error) {
	goroot := runtime.GOROOT()
	if goroot == "" {
		return "", fmt.Errorf("gofrontend: no GOROOT available for stdlib import (toolchain GOROOT is empty)")
	}
	data, err := os.ReadFile(filepath.Join(goroot, "VERSION"))
	if err != nil {
		return "", fmt.Errorf("gofrontend: cannot verify pinned GOROOT %s: %v (stdlib must come from the build toolchain %s)", goroot, err, runtime.Version())
	}
	ver := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if ver != runtime.Version() {
		return "", fmt.Errorf("gofrontend: GOROOT %s reports version %q, but this gofrontend was built with %q; refusing to mix toolchains", goroot, ver, runtime.Version())
	}
	return goroot, nil
}

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
// Resolution order (see the package header): the importing module's
// go.mod-pinned version first (require + replace, longest-module-prefix
// match); the highest version on disk only as a warned fallback; then
// GOPATH/src/<path>; then the pinned toolchain's GOROOT/src/<path> so
// plain stdlib packages outside the synthetic overlay (e.g.
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
	key := diskCacheKey(importPath)
	diskMu.Lock()
	if d, ok := diskDirCache[key]; ok {
		diskMu.Unlock()
		return d, nil
	}
	diskMu.Unlock()

	resolve := func() (string, error) {
		if mc := goModCache(); mc != "" {
			// The go.mod-pinned version wins when the importing
			// module requires this import (longest-module-prefix
			// match, so subpackage imports such as
			// github.com/google/go-cmp/cmp pin to the go-cmp
			// requirement). A local-path replace resolves to that
			// directory directly; a missing local directory is a
			// genuine configuration error, not a scope error, so it
			// is reported rather than silently falling through to
			// the cache.
			if mod := currentImportingModule(); mod != nil {
				if modPath, version, localDir, pinned := mod.pinnedRequirement(importPath); pinned {
					if localDir != "" {
						if st, err := os.Stat(localDir); err == nil && st.IsDir() {
							return localDir, nil
						}
						return "", fmt.Errorf("gofrontend: import %q: go.mod replaces module %q with local directory %q, which does not exist", importPath, modPath, localDir)
					}
					pinnedDir := filepath.Join(mc, escapeModulePath(modPath)+"@"+version)
					if sub := strings.TrimPrefix(importPath, modPath); sub != "" {
						pinnedDir = filepath.Join(pinnedDir, filepath.FromSlash(strings.TrimPrefix(sub, "/")))
					}
					if st, err := os.Stat(pinnedDir); err == nil && st.IsDir() {
						return pinnedDir, nil
					}
					// Pinned version absent from the cache: warn and
					// fall through to the highest-version scan below
					// rather than failing a compile the cache could
					// still satisfy. The warning names the pinned
					// version so the substitution is visible.
					if dir, ver, ok := highestModCacheDir(mc, importPath); ok {
						warnDiskFallback(importPath, fmt.Sprintf("go.mod pins %s@%s, which is absent from the module cache", modPath, version), ver)
						return dir, nil
					}
					// No cache entry at all: continue to GOPATH/GOROOT.
				}
			}
			// Fallback: highest version on disk. This runs only when
			// there is no importing-module context, the module is not
			// required, or the pinned version is absent and no other
			// cache version exists either (the pinned-missing case
			// above already returned). It warns so the choice of
			// version is visible instead of silent.
			if dir, ver, ok := highestModCacheDir(mc, importPath); ok {
				reason := "no enclosing go.mod pins a version for this import"
				if currentImportingModule() != nil {
					reason = "the importing module's go.mod does not require this import"
				}
				warnDiskFallback(importPath, reason, ver)
				return dir, nil
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
		// Standard library outside the synthetic overlay: resolve from
		// the PINNED toolchain's GOROOT (pinnedGoroot verifies the
		// tree's VERSION against the toolchain that built gofrontend,
		// so a GOROOT override pointing at another toolchain fails
		// loudly instead of silently mixing stdlib versions). A pin
		// failure is a genuine error, not a scope error: it is returned
		// directly rather than falling through to errDiskNotFound.
		if goroot, err := pinnedGoroot(); err != nil {
			recordDiskPinError(importPath, err)
			return "", err
		} else {
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
	diskDirCache[key] = dir
	diskMu.Unlock()
	diskPinErrMu.Lock()
	delete(diskPinErr, importPath)
	diskPinErrMu.Unlock()
	return dir, nil
}

// highestModCacheDir scans the module cache for importPath and returns
// the directory of the highest version present, using the P4
// (driver-harden) progressively-shorter-prefix rule: the cache stores the
// MODULE with the version (go-cmp@v0.7.0/cmp), not the package, so the
// full path is tried as a module first, then trailing elements are
// stripped as subpackage paths. The version returned is the winning
// compareSemver version, for fallback warnings.
func highestModCacheDir(mc, importPath string) (dir, version string, ok bool) {
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
				return dir, best, true
			}
		}
	}
	return "", "", false
}

// diskPinErr records, per import path, the latest GOROOT-pin failure from
// resolveDiskPackageDir. isDiskImportable can only report a boolean, so
// the import gate would otherwise degrade a loud toolchain-mismatch error
// into a generic scope error; checkImportAllowed consults this map to
// surface the real error instead. Entries are cleared when the path later
// resolves successfully.
var (
	diskPinErrMu sync.Mutex
	diskPinErr   = map[string]error{}
)

func recordDiskPinError(importPath string, err error) {
	diskPinErrMu.Lock()
	diskPinErr[importPath] = err
	diskPinErrMu.Unlock()
}

// diskPinError returns the recorded GOROOT-pin failure for importPath, if
// any.
func diskPinError(importPath string) error {
	diskPinErrMu.Lock()
	defer diskPinErrMu.Unlock()
	return diskPinErr[importPath]
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
	// DELIBERATELY keyed by bare import path, NOT by diskCacheKey:
	// building a disk package has process-global side effects that are
	// not idempotent — buildDiskPackage appends the package's
	// multi-result specs to the global diskMRSpecs table with no dedup
	// (mr_disk.go, registerDiskMRSpecs), so building the same path twice
	// in one process (once per importing module) duplicates those specs
	// and shifts later exports' type tables (observed as a
	// TestGolden/pkgvar_mrarg mismatch after the alias/bisect tests).
	// gofrontend runs one export per process in production, so a
	// process-singleton built package matches production semantics;
	// per-module correctness still holds for directory RESOLUTION
	// (diskDirCache is module-qualified, so a fresh build uses the
	// pinned directory). Fully isolating built packages per module
	// requires making that global registration idempotent first — that
	// lives in mr_disk.go, owned by another lane, and is flagged as
	// follow-up rather than worked around here.
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
	// P3-driver-harden (jsonlib lane): the GRAALGO_BUILD_TAGS env var
	// (space-separated) adds extra build tags, e.g. purego to select
	// pure-Go fallbacks over assembly stubs (segmentio/asm).
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
	ctxt.BuildTags = append(append([]string{"purego"}, ctxt.BuildTags...), strings.Fields(os.Getenv("GRAALGO_BUILD_TAGS"))...)
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
// importSyntheticPackage; dedicated-stub paths (runtime, unsafe — no
// bodies to merge, their call sites are handled by existing wire logic)
// are walked with nothing to merge; anything else resolves from disk
// (fmt and sync/sync/atomic included: fmt was never a dedicated stub,
// and 503608ea retired the synthetic sync/sync/atomic packages to disk).
// Idempotent via e.importedPkgTree (and e.importedSynthetic for overlap
// with the needX direct-import flow), so sharing the tree with the
// synthetic flow is safe.
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
