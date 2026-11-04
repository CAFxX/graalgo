package main

// Module-aware disk resolution (review 2026-11-04, P1-04/P1-05).
//
// The disk importer used to resolve every third-party import by scanning
// the module cache and keeping the NEWEST version present
// (resolveDiskPackageDir's compareSemver loop). Two builds on machines
// with different caches could therefore compile different dependency
// versions with no error, and the importing module's go.mod was never
// consulted.
//
// Resolution now has two modes, chosen by whether the package being
// compiled belongs to a module:
//
//   - Module mode: ExportFiles walks up from the importing package's
//     directory to the enclosing go.mod (if any) and installs it as the
//     module context for the whole export. Third-party versions then
//     come ONLY from that go.mod's require list (with replace applied):
//     the exact required version is read from the module cache, and any
//     other outcome — module not required, required version absent from
//     the cache, replaced directory missing — is a loud error, never a
//     silent fallback to another version. A package inside the main
//     module itself resolves to the module directory. Standard-library
//     imports (first path element has no dot) still resolve from the
//     toolchain GOROOT, never from the cache or GOPATH.
//   - Legacy mode: no go.mod above the importing package (standalone
//     files, corpus fixtures, stdlib-only compiles). The previous
//     behaviour is preserved verbatim: newest cached version, then
//     GOPATH/src, then GOROOT. Legacy mode is documented as such; it
//     must not be used for module builds, where the version would again
//     depend on ambient cache contents.
//
// The module cache itself is only storage: under module mode its
// location (flag, GOMODCACHE, GOPATH default) can change availability
// ("not present in cache" error) but never the selected version.
//
// go.sum is deliberately NOT consulted: version selection is go.mod's
// job, and cache directories are populated (and hash-verified) by cmd/go
// when downloaded. gofrontend only reads them.
//
// This parser covers the go.mod directives that affect resolution
// (module, require, replace); all other directives (go, toolchain,
// exclude, retract, godebug, tool) are parsed past and ignored. It is a
// general go.mod reader, not a per-module table: no package, module, or
// version is special-cased here.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// modReplace is one replace directive's right-hand side for a module.
type modReplace struct {
	oldVersion string // "" = replaces all versions of the module
	version    string // replacement version (when !isDir)
	dir        string // replacement local directory, absolute (when isDir)
	isDir      bool
}

// moduleContext is the importing module for one ExportFiles invocation.
type moduleContext struct {
	root        string                // directory containing go.mod
	goMod       string                // path to go.mod
	path        string                // module path
	requires    map[string]string     // module path -> required version
	replaces    map[string]modReplace // module path -> replacement
	fingerprint string                // root + go.mod bytes; cache invalidation key
}

// isModulePath reports whether importPath is a third-party module path
// (as opposed to a standard-library path): Go's rule is that the first
// path element contains a dot.
func isModulePath(importPath string) bool {
	first := importPath
	if i := strings.Index(importPath, "/"); i >= 0 {
		first = importPath[:i]
	}
	return strings.Contains(first, ".")
}

// findGoMod walks up from dir looking for a go.mod, returning "" when
// none exists up to the filesystem root.
func findGoMod(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for {
		cand := filepath.Join(abs, "go.mod")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}

// loadModuleContext reads the go.mod enclosing dir (if any) and returns
// the module context, or (nil, nil) when dir is not inside a module.
func loadModuleContext(dir string) (*moduleContext, error) {
	goMod := findGoMod(dir)
	if goMod == "" {
		return nil, nil
	}
	data, err := os.ReadFile(goMod)
	if err != nil {
		return nil, fmt.Errorf("gofrontend: cannot read %s: %w", goMod, err)
	}
	path, requires, replaces, err := parseGoMod(data)
	if err != nil {
		return nil, fmt.Errorf("gofrontend: cannot parse %s: %w", goMod, err)
	}
	root := filepath.Dir(goMod)
	// Replace targets spelled as relative paths are relative to the
	// go.mod that declares them.
	for m, rep := range replaces {
		if rep.isDir && !filepath.IsAbs(rep.dir) {
			rep.dir = filepath.Join(root, rep.dir)
			replaces[m] = rep
		}
	}
	return &moduleContext{
		root: root, goMod: goMod, path: path,
		requires: requires, replaces: replaces,
		fingerprint: root + "\x00" + string(data),
	}, nil
}

// stripGoModComment cuts a go.mod line at its first "//" comment. Module
// paths, versions, and local paths never contain "//", so no quoting
// subtlety arises.
func stripGoModComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// splitGoModFields splits a go.mod line into fields, honouring
// double-quoted tokens (module paths containing spaces are quoted).
func splitGoModFields(line string) []string {
	var fields []string
	i := 0
	for i < len(line) {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
			i++
		}
		if i >= len(line) {
			break
		}
		if line[i] == '"' {
			j := i + 1
			for j < len(line) && line[j] != '"' {
				if line[j] == '\\' && j+1 < len(line) {
					j++
				}
				j++
			}
			if j >= len(line) {
				fields = append(fields, line[i+1:])
				break
			}
			if uq, err := strconv.Unquote(line[i : j+1]); err == nil {
				fields = append(fields, uq)
			} else {
				fields = append(fields, line[i+1:j])
			}
			i = j + 1
			continue
		}
		j := i
		for j < len(line) && line[j] != ' ' && line[j] != '\t' && line[j] != '\r' {
			j++
		}
		fields = append(fields, line[i:j])
		i = j
	}
	return fields
}

// parseGoMod parses go.mod source, returning the module path, the require
// map, and the replace map. Unknown directives are ignored; unknown
// block directives are skipped in full so their contents are never
// mistaken for top-level directives.
func parseGoMod(data []byte) (string, map[string]string, map[string]modReplace, error) {
	requires := map[string]string{}
	replaces := map[string]modReplace{}
	modulePath := ""
	lines := strings.Split(string(data), "\n")
	i := 0
	// skipBlock advances i past the closing ")" line of a block that
	// starts at lines[i-1] (the directive line itself is consumed).
	skipBlock := func() error {
		for i < len(lines) {
			f := splitGoModFields(stripGoModComment(lines[i]))
			i++
			if len(f) == 1 && f[0] == ")" {
				return nil
			}
		}
		return fmt.Errorf("unterminated block in go.mod")
	}
	for i < len(lines) {
		fields := splitGoModFields(stripGoModComment(lines[i]))
		i++
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "module":
			if len(fields) != 2 {
				return "", nil, nil, fmt.Errorf("malformed module directive: %q", strings.Join(fields, " "))
			}
			modulePath = fields[1]
		case "require":
			if len(fields) == 2 && fields[1] == "(" {
				closed := false
				for i < len(lines) {
					f := splitGoModFields(stripGoModComment(lines[i]))
					i++
					if len(f) == 0 {
						continue
					}
					if len(f) == 1 && f[0] == ")" {
						closed = true
						break
					}
					if len(f) < 2 {
						return "", nil, nil, fmt.Errorf("malformed require line: %q", strings.Join(f, " "))
					}
					requires[f[0]] = f[1]
				}
				if !closed {
					return "", nil, nil, fmt.Errorf("unterminated require block in go.mod")
				}
			} else if len(fields) >= 3 {
				requires[fields[1]] = fields[2]
			} else {
				return "", nil, nil, fmt.Errorf("malformed require directive: %q", strings.Join(fields, " "))
			}
		case "replace":
			if len(fields) == 2 && fields[1] == "(" {
				closed := false
				for i < len(lines) {
					f := splitGoModFields(stripGoModComment(lines[i]))
					i++
					if len(f) == 0 {
						continue
					}
					if len(f) == 1 && f[0] == ")" {
						closed = true
						break
					}
					rep, err := parseReplaceArgs(f)
					if err != nil {
						return "", nil, nil, err
					}
					replaces[rep.oldPath] = rep.replace
				}
				if !closed {
					return "", nil, nil, fmt.Errorf("unterminated replace block in go.mod")
				}
			} else if len(fields) >= 2 {
				rep, err := parseReplaceArgs(fields[1:])
				if err != nil {
					return "", nil, nil, err
				}
				replaces[rep.oldPath] = rep.replace
			} else {
				return "", nil, nil, fmt.Errorf("malformed replace directive: %q", strings.Join(fields, " "))
			}
		default:
			// go, toolchain, exclude, retract, godebug, tool, ...:
			// irrelevant to resolution. A block form must still be
			// skipped so its lines are not misread as directives.
			if len(fields) == 2 && fields[1] == "(" {
				if err := skipBlock(); err != nil {
					return "", nil, nil, err
				}
			}
		}
	}
	if modulePath == "" {
		return "", nil, nil, fmt.Errorf("go.mod has no module directive")
	}
	return modulePath, requires, replaces, nil
}

type parsedReplace struct {
	oldPath string
	replace modReplace
}

// parseReplaceArgs parses the arguments after "replace": either
// "old [oldver] => new [newver]" or "old [oldver] => localpath".
func parseReplaceArgs(args []string) (parsedReplace, error) {
	arrow := -1
	for k, a := range args {
		if a == "=>" {
			arrow = k
			break
		}
	}
	if arrow < 0 {
		return parsedReplace{}, fmt.Errorf("malformed replace (no =>): %q", strings.Join(args, " "))
	}
	left, right := args[:arrow], args[arrow+1:]
	if len(left) < 1 || len(left) > 2 || len(right) < 1 || len(right) > 2 {
		return parsedReplace{}, fmt.Errorf("malformed replace: %q", strings.Join(args, " "))
	}
	rep := modReplace{}
	if len(left) == 2 {
		rep.oldVersion = left[1]
	}
	if len(right) == 1 {
		// A bare target is a local path (a module target always
		// carries a version).
		if !strings.HasPrefix(right[0], ".") && !strings.HasPrefix(right[0], "/") {
			return parsedReplace{}, fmt.Errorf("malformed replace target (module without version): %q", strings.Join(args, " "))
		}
		rep.isDir = true
		rep.dir = right[0]
	} else {
		rep.version = right[1]
	}
	return parsedReplace{oldPath: left[0], replace: rep}, nil
}

// moduleCacheDir renders a module-cache package directory:
// $GOMODCACHE/<escaped module path>@<version>[/subpackage path].
func moduleCacheDir(modCache, modPath, version, subPath string) string {
	dir := filepath.Join(modCache, escapeModulePath(modPath)+"@"+version)
	if subPath != "" {
		dir = filepath.Join(dir, filepath.FromSlash(subPath))
	}
	return dir
}

// resolveModuleDir resolves importPath under this module context.
// It returns handled=false only for standard-library paths (no dot in
// the first element), which the caller resolves from GOROOT instead.
// For every third-party path it returns handled=true and either the
// directory or a loud error: version selection comes from go.mod alone,
// never from a scan of whatever the cache happens to hold.
func (ctx *moduleContext) resolveModuleDir(importPath, modCache string) (string, bool, error) {
	if !isModulePath(importPath) {
		return "", false, nil
	}
	isDir := func(dir string) bool {
		st, err := os.Stat(dir)
		return err == nil && st.IsDir()
	}
	// A package of the main module itself resolves to the module tree.
	if ctx.path != "" && (importPath == ctx.path || strings.HasPrefix(importPath, ctx.path+"/")) {
		sub := strings.TrimPrefix(importPath, ctx.path)
		dir := filepath.Join(ctx.root, filepath.FromSlash(sub))
		if !isDir(dir) {
			return "", true, fmt.Errorf("gofrontend: import %q: package is in the main module %q but %s is not a directory", importPath, ctx.path, dir)
		}
		return dir, true, nil
	}
	// The providing module is the longest required module path that is
	// a prefix of the import path (Go's module-membership rule).
	best := ""
	for m := range ctx.requires {
		if importPath == m || strings.HasPrefix(importPath, m+"/") {
			if len(m) > len(best) {
				best = m
			}
		}
	}
	if best == "" {
		return "", true, fmt.Errorf("gofrontend: import %q: no module providing it is required by %s (add the requirement, e.g. via `go mod tidy`, instead of relying on the module cache)", importPath, ctx.goMod)
	}
	sub := strings.TrimPrefix(importPath, best)
	if rep, ok := ctx.replaces[best]; ok && (rep.oldVersion == "" || rep.oldVersion == ctx.requires[best]) {
		if rep.isDir {
			dir := filepath.Join(rep.dir, filepath.FromSlash(sub))
			if !isDir(dir) {
				return "", true, fmt.Errorf("gofrontend: import %q: replacement directory %s (from replace in %s) is not a directory", importPath, rep.dir, ctx.goMod)
			}
			return dir, true, nil
		}
		dir := moduleCacheDir(modCache, best, rep.version, sub)
		if !isDir(dir) {
			return "", true, fmt.Errorf("gofrontend: import %q: module %s@%s (replacement version from %s) is not present in the module cache %s", importPath, best, rep.version, ctx.goMod, modCache)
		}
		return dir, true, nil
	}
	version := ctx.requires[best]
	dir := moduleCacheDir(modCache, best, version, sub)
	if !isDir(dir) {
		return "", true, fmt.Errorf("gofrontend: import %q: module %s@%s required by %s is not present in the module cache %s (download it with `go mod download` instead of relying on whatever the cache holds)", importPath, best, version, ctx.goMod, modCache)
	}
	return dir, true, nil
}

// The module context for the export currently in progress, set by
// ExportFiles from the importing package's directory. gofrontend
// compiles one package per process, and frontend tests run
// sequentially, so process-wide state matches the pre-existing
// diskCache/diskDirCache discipline. Changing the context clears both
// caches: a directory resolved under one module must never be reused
// for another module's (possibly different) version of the same import.
var (
	curModuleCtx         *moduleContext
	curModuleFingerprint string
)

// setModuleContext installs ctx as the active module context, clearing
// the disk caches when the context changes.
func setModuleContext(ctx *moduleContext) {
	diskMu.Lock()
	defer diskMu.Unlock()
	fp := ""
	if ctx != nil {
		fp = ctx.fingerprint
	}
	if fp != curModuleFingerprint {
		diskDirCache = map[string]string{}
		diskCache = map[string]*syntheticPackage{}
		curModuleFingerprint = fp
	}
	curModuleCtx = ctx
}

// activeModuleContext returns the module context installed by the
// current ExportFiles call, or nil when compiling outside a module.
func activeModuleContext() *moduleContext {
	diskMu.Lock()
	defer diskMu.Unlock()
	return curModuleCtx
}
