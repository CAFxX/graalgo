package main

// WS G (P3-M9, module-probe lane): general third-party package import.
//
// Any import path not claimed by the synthetic overlay resolves from local
// disk: when the importing module's go.mod pins the import's module,
// exactly that version from the module cache (with go.sum verification
// when a go.sum is present, and a loud error when the pinned version is
// absent — never a silent upgrade); otherwise the module cache
// ($GOMODCACHE/<path>@<highest version>) first, then GOPATH/src/<path>.
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
	"crypto/sha256"
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

// compareSemver compares "v1.6.0"-style versions per semver precedence
// (https://semver.org): the numeric core first, then prerelease
// identifiers — a version without a prerelease outranks the same core
// with one, numeric prerelease identifiers compare numerically and rank
// below alphanumeric ones, and a shorter prerelease list that is a prefix
// of a longer one ranks below it. Build metadata (after "+") is ignored.
// The previous implementation truncated at "-", so a pseudo-version such
// as v1.2.3-0.2024… compared as plain v1.2.3 and only a lexicographic
// fallback ordered it — wrong whenever the prerelease actually decides
// the order (e.g. v1.2.3-0.2024… must rank BELOW v1.2.3, not equal to
// it). Used only by the legacy highest-cached-version fallback; pinned
// go.mod resolution never consults it.
func compareSemver(a, b string) int {
	split := func(v string) (core []string, pre []string) {
		v = strings.TrimPrefix(v, "v")
		if i := strings.Index(v, "+"); i >= 0 {
			v = v[:i]
		}
		if i := strings.Index(v, "-"); i >= 0 {
			pre = strings.Split(v[i+1:], ".")
			v = v[:i]
		}
		core = strings.Split(v, ".")
		return core, pre
	}
	ca, pa := split(a)
	cb, pb := split(b)
	for i := 0; i < len(ca) || i < len(cb); i++ {
		var sa, sb string
		if i < len(ca) {
			sa = ca[i]
		} else {
			sa = "0"
		}
		if i < len(cb) {
			sb = cb[i]
		} else {
			sb = "0"
		}
		na, ea := strconv.Atoi(sa)
		nb, eb := strconv.Atoi(sb)
		if ea == nil && eb == nil {
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if sa != sb {
			if sa < sb {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa) == 0 && len(pb) == 0:
		return 0
	case len(pa) == 0:
		// A release outranks the same core with a prerelease.
		return 1
	case len(pb) == 0:
		return -1
	}
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, y := pa[i], pb[i]
		if x == y {
			continue
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if nx < ny {
				return -1
			}
			return 1
		case ex == nil:
			// Numeric identifiers rank below alphanumeric ones.
			return -1
		case ey == nil:
			return 1
		default:
			if x < y {
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
	return 0
}

// ---------------------------------------------------------------------------
// Importing-module resolution (REVIEW-2026-11-03-16, P1-S6/F4).
//
// The importing package's own go.mod — found by walking up from the
// importing directory (ExportFiles' importingDir, which honours the
// --test-package-dir flag for test drivers) — is authoritative for every
// module it requires. The longest required module path that is a prefix
// of the import path commits the resolution: exactly that version (after
// replace directives) is used, and any failure — version absent from the
// module cache, package missing from that version, go.sum entry missing
// or disagreeing with the cache download hash — is a loud, distinct
// error, never a silent upgrade to another cached version. Those errors
// deliberately do NOT wrap errDiskNotFound, so the overlay importer and
// the import gate report them as genuine failures instead of degrading
// to the pre-existing "not in frontend/v1 scope" error.
//
// When no go.mod is found, or the import's module is not among its
// requirements, the previous behaviour is preserved unchanged (highest
// cached version, then GOPATH/src, then GOROOT): the probe harness and
// go.mod-less compiles keep working, and an unlisted module is not
// newly rejected. The pinned case is the correctness fix; the unpinned
// case stays a documented compatibility fallback, not a silent claim of
// go.mod fidelity.
//
// go.sum verification, when a go.sum sits beside the importing go.mod:
// the pinned module@version must have an entry, and when the module
// cache carries download metadata (cache/download/<path>/@v/<ver>.ziphash)
// its hash must match a go.sum h1 entry. No go.sum file means no
// verification is possible and none is claimed. The extracted module
// tree itself carries no stored hash to check against, so verification
// is entry presence plus download-hash agreement, not a full tree hash
// — stated here so the claim is never overstated.
// ---------------------------------------------------------------------------

// goModReplace is one parsed replace directive.
type goModReplace struct {
	oldPath  string
	oldVer   string // "" means "all versions of oldPath"
	newPath  string
	newVer   string
	newLocal string // "" unless the replacement is a filesystem path
}

// goModContext is the parsed go.mod (plus optional go.sum) of the module
// being compiled, together with the fingerprint that keys the disk caches.
type goModContext struct {
	dir        string // directory containing go.mod
	goModPath  string
	goSumPath  string
	hasGoSum   bool
	modulePath string
	requires   map[string]string
	replaces   []goModReplace
	// goSum keeps the module-zip hashes and the go.mod hashes DISTINCT:
	// a go.sum line "module version h1:..." attests the module zip,
	// while "module version/go.mod h1:..." attests only the go.mod file.
	// Merging them (as a single hash set) let a go.sum carrying only a
	// go.mod hash produce a misleading "disagrees with the download
	// hash" error for the zip; verifyGoSum compares the cache ziphash
	// against the zip set only, and reports a go.mod-only entry as
	// missing-module-hash, never as disagreement.
	goSum       map[string]goSumHashes // "module@version" -> hash sets
	fingerprint string
}

// goSumHashes is one go.sum entry's hashes, split by what they attest.
type goSumHashes struct {
	zip map[string]bool // "module version h1:..." lines (module zip)
	mod map[string]bool // "module version/go.mod h1:..." lines (go.mod file)
}

var diskModuleCtx *goModContext // set by ExportFiles for one export; see below

// diskExportMu serializes ExportFiles: the importing-module context is
// process-global (diskModuleCtx, restore-on-return), so two concurrent
// exports would resolve imports under each other's pins. No concurrent
// caller exists today — the only production caller is the single
// gofrontend invocation (main.go), and no test in this package uses
// t.Parallel — but serializing here makes any future concurrent caller
// correct by construction instead of silently wrong. ExportFiles never
// re-enters itself (disk packages build via buildDiskPackage, not via
// ExportFiles), so holding this across the whole export cannot deadlock.
var diskExportMu sync.Mutex

// exportTestGate, when non-nil, is called by ExportFiles after installing
// the importing-module context and before resolving any import. Tests
// use it as a rendezvous to force two exports to overlap at exactly the
// point where a missing serialization would cross their contexts (see
// TestDiskImportConcurrentExportsKeepOwnPins). Nil in production, where
// the call is a single nil check.
var exportTestGate func()

// currentDiskModuleContext returns the importing-module context for this
// export, or nil when no go.mod governs the current compile.
func currentDiskModuleContext() *goModContext {
	diskMu.Lock()
	defer diskMu.Unlock()
	return diskModuleCtx
}

// diskCacheKey keys a disk resolution by importing-module context, so a
// package resolved under one module's pins can never be served from a
// cache entry resolved under another module's pins (or none) in the same
// process. The fingerprint covers the go.mod path, its requirements and
// replaces, and the go.sum bytes, so an edited go.mod/go.sum re-resolves
// instead of hitting a stale entry. The resolved cache root is part of
// the key too: the same go.mod resolving against a different GOMODCACHE
// (tests re-point it per test; a user may move the cache) must not hit
// entries resolved from the old cache.
func diskCacheKey(ctx *goModContext, importPath string) string {
	root := goModCache()
	if ctx == nil {
		return root + "\x00" + importPath
	}
	return root + "\x00" + ctx.fingerprint + "\x00" + importPath
}

// setDiskModuleContextFromDir loads the go.mod governing dir (walking up
// from it) and installs it as the current importing-module context,
// returning a restore function that reinstates the previous context.
// A malformed go.mod is a loud error, not a silent fallback to
// no-context resolution. When no go.mod is found the context becomes
// nil (legacy resolution) and restore still applies.
func setDiskModuleContextFromDir(dir string) (func(), error) {
	var ctx *goModContext
	if dir != "" {
		var err error
		ctx, err = loadGoModContext(dir)
		if err != nil {
			return nil, err
		}
	}
	diskMu.Lock()
	prev := diskModuleCtx
	diskModuleCtx = ctx
	diskMu.Unlock()
	return func() {
		diskMu.Lock()
		diskModuleCtx = prev
		diskMu.Unlock()
	}, nil
}

// loadGoModContext walks up from dir to the nearest go.mod and parses it
// (plus a sibling go.sum if present). It returns (nil, nil) when no
// go.mod is found at or above dir.
func loadGoModContext(dir string) (*goModContext, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for d := abs; ; d = filepath.Dir(d) {
		cand := filepath.Join(d, "go.mod")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			data, err := os.ReadFile(cand)
			if err != nil {
				return nil, fmt.Errorf("go.mod at %s: cannot read: %v", cand, err)
			}
			ctx, err := parseGoMod(cand, data)
			if err != nil {
				return nil, err
			}
			// B2 (veto fix): a go.sum that exists but cannot be read
			// (permissions, I/O error, a directory in its place) is a
			// LOUD error, never the sum:absent path. Treating it as
			// absent would silently downgrade a pinned build to no
			// verification — indistinguishable from a genuinely absent
			// go.sum, while an unreadable go.mod is loud in this same
			// function. Only os.IsNotExist takes the absent path.
			sumPath := filepath.Join(d, "go.sum")
			if sumData, err := os.ReadFile(sumPath); err == nil {
				ctx.hasGoSum = true
				ctx.goSumPath = sumPath
				ctx.goSum = parseGoSum(sumData)
				ctx.fingerprint += "\x00sum:" + fmt.Sprintf("%x", sha256.Sum256(sumData))
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("go.sum at %s: cannot read: %v", sumPath, err)
			} else {
				ctx.fingerprint += "\x00sum:absent"
			}
			return ctx, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return nil, nil
		}
	}
}

// parseGoSum parses go.sum lines into "module@version" -> hash sets,
// keeping module-zip hashes and go.mod hashes in separate sets (see the
// goSum field comment). The "/go.mod" suffix on a version is stripped
// for the key: both lines attest the same module@version, but what they
// attest differs.
func parseGoSum(data []byte) map[string]goSumHashes {
	out := map[string]goSumHashes{}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		isMod := strings.HasSuffix(f[1], "/go.mod")
		ver := strings.TrimSuffix(f[1], "/go.mod")
		key := f[0] + "@" + ver
		entry := out[key]
		if isMod {
			if entry.mod == nil {
				entry.mod = map[string]bool{}
			}
			entry.mod[f[2]] = true
		} else {
			if entry.zip == nil {
				entry.zip = map[string]bool{}
			}
			entry.zip[f[2]] = true
		}
		out[key] = entry
	}
	return out
}

// parseGoMod parses the subset of go.mod syntax this resolver needs:
// module, require (single-line and block), and replace (single-line and
// block, including version-specific and local-path replacements). Other
// directives are skipped. Anything malformed in the parsed subset is an
// error — a half-parsed go.mod must never silently govern resolution.
func parseGoMod(path string, data []byte) (*goModContext, error) {
	ctx := &goModContext{goModPath: path, dir: filepath.Dir(path), requires: map[string]string{}}
	parseRequire := func(f []string) error {
		if len(f) != 2 {
			return fmt.Errorf("go.mod at %s: malformed require line %q", path, strings.Join(f, " "))
		}
		ctx.requires[f[0]] = f[1]
		return nil
	}
	lines := strings.Split(string(data), "\n")
	inBlock := "" // "" | "require" | "replace"
	for _, raw := range lines {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if inBlock != "" {
			if line == ")" {
				inBlock = ""
				continue
			}
			switch inBlock {
			case "require":
				if err := parseRequire(strings.Fields(line)); err != nil {
					return nil, err
				}
			case "replace":
				if err := ctx.parseReplaceLine(path, line); err != nil {
					return nil, err
				}
			}
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "module":
			if len(f) != 2 {
				return nil, fmt.Errorf("go.mod at %s: malformed module line %q", path, line)
			}
			ctx.modulePath = f[1]
		case "require":
			if len(f) == 1 {
				// "require (" handled below via the "(" token.
				return nil, fmt.Errorf("go.mod at %s: malformed require line %q", path, line)
			}
			if f[1] == "(" {
				inBlock = "require"
				continue
			}
			if err := parseRequire(f[1:]); err != nil {
				return nil, err
			}
		case "replace":
			if len(f) >= 2 && f[1] == "(" {
				inBlock = "replace"
				continue
			}
			if err := ctx.parseReplaceLine(path, strings.Join(f[1:], " ")); err != nil {
				return nil, err
			}
		default:
			// go, toolchain, exclude, retract, etc.: not needed here.
		}
	}
	if inBlock != "" {
		return nil, fmt.Errorf("go.mod at %s: unterminated %s block", path, inBlock)
	}
	// Fingerprint: go.mod path + sorted requirements + replaces. The
	// go.sum contribution is appended by loadGoModContext.
	var sb strings.Builder
	sb.WriteString(path)
	reqs := make([]string, 0, len(ctx.requires))
	for m, v := range ctx.requires {
		reqs = append(reqs, m+"@"+v)
	}
	sort.Strings(reqs)
	for _, r := range reqs {
		sb.WriteString("\x00")
		sb.WriteString(r)
	}
	for _, rp := range ctx.replaces {
		sb.WriteString("\x00replace:")
		sb.WriteString(rp.oldPath + "@" + rp.oldVer + "=>" + rp.newPath + "@" + rp.newVer + rp.newLocal)
	}
	ctx.fingerprint = sb.String()
	return ctx, nil
}

// parseReplaceLine parses one replace spec (without the leading
// "replace" keyword): "old [oldver] => new [newver]" or
// "old [oldver] => local/path".
func (ctx *goModContext) parseReplaceLine(goModPath, line string) error {
	parts := strings.Split(line, "=>")
	if len(parts) != 2 {
		return fmt.Errorf("go.mod at %s: malformed replace line %q", goModPath, line)
	}
	lhs := strings.Fields(strings.TrimSpace(parts[0]))
	rhs := strings.Fields(strings.TrimSpace(parts[1]))
	if len(lhs) < 1 || len(lhs) > 2 || len(rhs) < 1 || len(rhs) > 2 {
		return fmt.Errorf("go.mod at %s: malformed replace line %q", goModPath, line)
	}
	r := goModReplace{oldPath: lhs[0]}
	if len(lhs) == 2 {
		r.oldVer = lhs[1]
	}
	if len(rhs) == 1 && (strings.HasPrefix(rhs[0], "./") || strings.HasPrefix(rhs[0], "../") || filepath.IsAbs(rhs[0])) {
		r.newLocal = rhs[0]
		if !filepath.IsAbs(r.newLocal) {
			r.newLocal = filepath.Join(ctx.dir, r.newLocal)
		}
	} else {
		if len(rhs) != 2 {
			return fmt.Errorf("go.mod at %s: non-local replace %q needs a version", goModPath, line)
		}
		r.newPath, r.newVer = rhs[0], rhs[1]
	}
	ctx.replaces = append(ctx.replaces, r)
	return nil
}

// pinnedModule applies the context's requirements and replaces to a
// module path. It reports (modulePath, version, localDir, ok): when ok
// and localDir != "" the module is replaced by a local directory;
// when ok and localDir == "" the module must resolve from the cache at
// exactly version. When !ok the module is not pinned by this go.mod.
func (ctx *goModContext) pinnedModule(modulePath string) (string, string, string, bool) {
	ver, ok := ctx.requires[modulePath]
	if !ok {
		return "", "", "", false
	}
	for _, r := range ctx.replaces {
		if r.oldPath != modulePath {
			continue
		}
		if r.oldVer != "" && r.oldVer != ver {
			continue
		}
		if r.newLocal != "" {
			return modulePath, "", r.newLocal, true
		}
		return r.newPath, r.newVer, "", true
	}
	return modulePath, ver, "", true
}

// pinnedPrefix returns the longest required module path that is a prefix
// of importPath (the module itself, or the module plus "/" plus a
// subpackage), or "" when no requirement governs the import.
func (ctx *goModContext) pinnedPrefix(importPath string) string {
	best := ""
	for m := range ctx.requires {
		if importPath == m || strings.HasPrefix(importPath, m+"/") {
			if len(m) > len(best) {
				best = m
			}
		}
	}
	return best
}

// verifyGoSum enforces the go.sum contract for a pinned module@version:
// with a go.sum present, an entry must exist, and a cache download hash
// (when the cache carries one) must agree with a go.sum h1 entry.
func (ctx *goModContext) verifyGoSum(modulePath, version, modCache string) error {
	if !ctx.hasGoSum {
		return nil
	}
	key := modulePath + "@" + version
	entry, ok := ctx.goSum[key]
	if !ok || (len(entry.zip) == 0 && len(entry.mod) == 0) {
		return fmt.Errorf("go.mod at %s requires %s %s, but %s has no entry for it; run 'go mod download %s' to record it instead of building against an unverified version", ctx.goModPath, modulePath, version, ctx.goSumPath, modulePath)
	}
	ziphashPath := filepath.Join(modCache, "cache", "download", escapeModulePath(modulePath), "@v", version+".ziphash")
	if data, err := os.ReadFile(ziphashPath); err == nil {
		zh := strings.TrimSpace(string(data))
		// A go.mod-only entry attests no module zip: it cannot agree
		// or disagree with the download hash, so report it as a
		// missing module hash — never as a disagreement.
		if len(entry.zip) == 0 {
			return fmt.Errorf("go.mod at %s requires %s %s, but %s carries only a go.mod hash for it (no module hash); run 'go mod download %s' to record it instead of building against an unverified version", ctx.goModPath, modulePath, version, ctx.goSumPath, modulePath)
		}
		hasH1 := false
		for h := range entry.zip {
			if strings.HasPrefix(h, "h1:") {
				hasH1 = true
				if h == zh {
					return nil
				}
			}
		}
		if hasH1 {
			return fmt.Errorf("go.mod at %s requires %s %s, but its go.sum entry disagrees with the module cache download hash in %s; refusing to build against a tampered or mismatched cache entry", ctx.goModPath, modulePath, version, ziphashPath)
		}
	}
	return nil
}

// resolveDiskPackageDir maps an import path to a source directory on disk.
// When the importing module's go.mod pins the import's module, exactly
// that version resolves (see the go.mod section above); failures there
// are loud errors that deliberately do NOT wrap errDiskNotFound.
// Otherwise the legacy order applies: module cache
// ($GOMODCACHE/<escaped path>@<highest version> — the probe harness
// pins the cache to the intended version), then GOPATH/src/<path>,
// then the toolchain's GOROOT/src/<path> so plain stdlib packages
// outside the synthetic overlay (e.g. database/sql/driver) resolve
// through the same general mechanism.
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
	ctx := currentDiskModuleContext()
	key := diskCacheKey(ctx, importPath)
	diskMu.Lock()
	if d, ok := diskDirCache[key]; ok {
		diskMu.Unlock()
		return d, nil
	}
	diskMu.Unlock()

	resolve := func() (string, error) {
		// Pinned first: when the importing go.mod governs this import,
		// the pinned module commits the resolution. No fallback to
		// another cached version, GOPATH, or GOROOT is attempted from
		// here — in module mode those are not the importing module's
		// dependency, and falling back would be the silent upgrade
		// this fix removes.
		if ctx != nil {
			if mod := ctx.pinnedPrefix(importPath); mod != "" {
				return resolvePinnedDir(ctx, mod, importPath)
			}
		}
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
	diskDirCache[key] = dir
	diskMu.Unlock()
	return dir, nil
}

// resolvePinnedDir resolves importPath against exactly the version the
// importing go.mod pins for module mod (after replaces). Every failure
// is a loud error that does not wrap errDiskNotFound, so callers report
// it as a genuine failure rather than a scope error.
func resolvePinnedDir(ctx *goModContext, mod, importPath string) (string, error) {
	resolvedPath, ver, localDir, ok := ctx.pinnedModule(mod)
	if !ok {
		return "", fmt.Errorf("%w: %q", errDiskNotFound, importPath)
	}
	subPath := strings.TrimPrefix(importPath, mod)
	subPath = strings.TrimPrefix(subPath, "/")
	if localDir != "" {
		dir := localDir
		if subPath != "" {
			dir = filepath.Join(dir, subPath)
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir, nil
		}
		return "", fmt.Errorf("go.mod at %s replaces %s with %s, but package %q is not there (no directory %s)", ctx.goModPath, mod, localDir, importPath, dir)
	}
	mc := goModCache()
	if mc == "" {
		return "", fmt.Errorf("go.mod at %s requires %s %s, but no module cache is configured (GOMODCACHE/GOPATH unset and no home directory); cannot resolve %q", ctx.goModPath, resolvedPath, ver, importPath)
	}
	modDir := filepath.Join(mc, filepath.FromSlash(escapeModulePath(resolvedPath))+"@"+ver)
	if st, err := os.Stat(modDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("go.mod at %s requires %s %s, but that version is not in the module cache (%s); run 'go mod download %s' to fetch it instead of silently building against another cached version", ctx.goModPath, resolvedPath, ver, mc, resolvedPath)
	}
	if err := ctx.verifyGoSum(resolvedPath, ver, mc); err != nil {
		return "", err
	}
	dir := modDir
	if subPath != "" {
		dir = filepath.Join(dir, subPath)
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("go.mod at %s requires %s %s, but package %q is not in that module version (no directory %s)", ctx.goModPath, resolvedPath, ver, importPath, dir)
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
	// Keyed by importing-module context (see diskCacheKey): the same
	// import path may legitimately build from different module versions
	// under different go.mods in one process.
	key := diskCacheKey(currentDiskModuleContext(), importPath)
	diskMu.Lock()
	if sp, ok := diskCache[key]; ok {
		diskMu.Unlock()
		return sp, nil
	}
	if diskBuilding[key] {
		diskMu.Unlock()
		return nil, fmt.Errorf("gofrontend: import cycle involving %q", importPath)
	}
	diskBuilding[key] = true
	diskMu.Unlock()

	sp, err := buildDiskPackage(importPath)

	diskMu.Lock()
	delete(diskBuilding, key)
	if err != nil {
		diskMu.Unlock()
		return nil, err
	}
	if prev, ok := diskCache[key]; ok {
		diskMu.Unlock()
		return prev, nil
	}
	diskCache[key] = sp
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
