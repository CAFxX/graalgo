package main

// Importing-module go.mod context for disk imports (review L4).
//
// Go's module version selection (MVS) is driven by the MAIN module's
// go.mod: a third-party import resolves to the version the importing
// module requires, not to whatever version happens to be newest on disk.
// resolveDiskPackageDir therefore consults the enclosing go.mod of the
// package being compiled (set once per ExportFiles call, from the input
// files' directory) before falling back to the highest version on disk.
// The fallback still exists — a compile with no enclosing go.mod, or a
// requirement missing from the cache, cannot pin anything — but it warns
// on stderr so the substitution is visible instead of silent.
//
// This is a general mechanism: no import path, module path, or version is
// special-cased. The parser below handles the go.mod directives disk
// resolution needs (module, go, toolchain, require, replace); it is
// deliberately not a full go.mod implementation (no exclude/retract/tool
// semantics — those never change which directory a module version lives
// in, and version-specific replaces are honored only when the required
// version matches).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// goModuleReplace is one replace directive: old module path (plus an
// optional version pin) replaced by a new module path at a version, or by
// a local directory (version == "" and local == true).
type goModuleReplace struct {
	oldVersion string
	newPath    string
	newVersion string
	local      bool
}

// goModule is the parsed subset of one go.mod file.
type goModule struct {
	dir       string // directory containing the go.mod file
	path      string // module directive
	goVersion string // go directive, e.g. "1.27" ("" if absent)
	toolchain string // toolchain directive, e.g. "go1.27.1" ("" if absent)
	requires  map[string]string
	replaces  map[string]goModuleReplace
}

var (
	importingModuleMu sync.RWMutex
	importingModule   *goModule
)

// setImportingModule locates and parses the go.mod enclosing dir (walking
// up, like cmd/go) and records it as the disk-import context for the
// current export. A nil result (no enclosing go.mod, or an unreadable one)
// means "unpinned": disk resolution falls back to highest-version-with-
// warning. Called by ExportFiles before any import is resolved.
func setImportingModule(dir string) {
	importingModuleMu.Lock()
	defer importingModuleMu.Unlock()
	importingModule = findEnclosingGoMod(dir)
}

// currentImportingModule returns the module context set by the most recent
// ExportFiles call, or nil when there is none. The frontend processes one
// main module per ExportFiles call in a process (gofrontend is a
// single-export binary; the test suite runs exports sequentially), so a
// process-scoped context matches the existing process-scoped disk caches.
func currentImportingModule() *goModule {
	importingModuleMu.RLock()
	defer importingModuleMu.RUnlock()
	return importingModule
}

// findEnclosingGoMod walks up from dir to the first directory containing
// a go.mod file and parses it. It returns nil when no go.mod is found.
func findEnclosingGoMod(dir string) *goModule {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	d := abs
	for {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			m := parseGoMod(data)
			m.dir = d
			return m
		}
		parent := filepath.Dir(d)
		if parent == d {
			return nil
		}
		d = parent
	}
}

// parseGoMod parses the require/replace-relevant subset of go.mod data.
func parseGoMod(data []byte) *goModule {
	m := &goModule{
		requires: map[string]string{},
		replaces: map[string]goModuleReplace{},
	}
	block := "" // "require", "replace", or "other" while inside a (...) block
	for _, raw := range strings.Split(string(data), "\n") {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		// Normalize whitespace (tabs included) so "require\t(" and
		// "replace\ta\t=>\tb" parse like their gofmt'd forms; directive
		// parsing below only uses whitespace-separated fields.
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if block != "" {
			if line == ")" {
				block = ""
				continue
			}
			switch block {
			case "require":
				if f := strings.Fields(line); len(f) >= 2 {
					m.requires[f[0]] = f[1]
				}
			case "replace":
				m.addReplace(line)
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "module "):
			m.path = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		case strings.HasPrefix(line, "go "):
			m.goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		case strings.HasPrefix(line, "toolchain "):
			m.toolchain = strings.TrimSpace(strings.TrimPrefix(line, "toolchain "))
		case line == "require (" || strings.HasPrefix(line, "require ("):
			block = "require"
		case strings.HasPrefix(line, "require "):
			if f := strings.Fields(strings.TrimPrefix(line, "require ")); len(f) >= 2 {
				m.requires[f[0]] = f[1]
			}
		case line == "replace (" || strings.HasPrefix(line, "replace ("):
			block = "replace"
		case strings.HasPrefix(line, "replace "):
			m.addReplace(strings.TrimPrefix(line, "replace "))
		case strings.HasSuffix(line, "("):
			// exclude, retract, tool, godebug, ... blocks: parsed as
			// opaque so their contents are never mistaken for requires.
			block = "other"
		}
	}
	return m
}

// addReplace records one replace directive body: "old [oldver] => new
// [newver]". A new side with no version is a local directory replacement.
func (m *goModule) addReplace(line string) {
	parts := strings.SplitN(line, "=>", 2)
	if len(parts) != 2 {
		return
	}
	oldF := strings.Fields(strings.TrimSpace(parts[0]))
	newF := strings.Fields(strings.TrimSpace(parts[1]))
	if len(oldF) == 0 || len(newF) == 0 {
		return
	}
	rep := goModuleReplace{newPath: newF[0]}
	if len(oldF) >= 2 {
		rep.oldVersion = oldF[1]
	}
	if len(newF) >= 2 {
		rep.newVersion = newF[1]
	} else {
		rep.local = true
	}
	m.replaces[oldF[0]] = rep
}

// pinnedRequirement resolves importPath against the module's requirements
// using longest-module-prefix matching (the module cache stores the
// MODULE at <module>@<version>, with the package as a subdirectory, so a
// subpackage import pins to its enclosing module's requirement). It
// returns the module path, the version to use, and whether the import is
// pinned at all. A local-path replacement resolves to that directory
// instead of a version: localDir is non-empty in that case.
func (m *goModule) pinnedRequirement(importPath string) (modPath, version, localDir string, pinned bool) {
	best := ""
	for req := range m.requires {
		if importPath == req || strings.HasPrefix(importPath, req+"/") {
			if len(req) > len(best) {
				best = req
			}
		}
	}
	if best == "" {
		return "", "", "", false
	}
	version = m.requires[best]
	if rep, ok := m.replaces[best]; ok && (rep.oldVersion == "" || rep.oldVersion == version) {
		if rep.local {
			sub := strings.TrimPrefix(importPath, best)
			sub = strings.TrimPrefix(sub, "/")
			dir := rep.newPath
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(m.dir, dir)
			}
			if sub != "" {
				dir = filepath.Join(dir, filepath.FromSlash(sub))
			}
			return best, "", dir, true
		}
		version = rep.newVersion
	}
	return best, version, "", true
}

// langMajorMinor reduces a Go version string ("go1.27.1", "1.27",
// "v1.27.0") to its "major.minor" form for language-level comparison: a
// go directive names a language floor, not an exact toolchain.
func langMajorMinor(v string) string {
	v = strings.TrimPrefix(v, "go")
	v = strings.TrimPrefix(v, "v")
	if i := strings.Index(v, "-"); i >= 0 {
		v = v[:i]
	}
	if i := strings.Index(v, "+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// checkModuleToolchain fails when the importing module's declared
// toolchain/go version is incompatible with the pinned build toolchain
// (toolchainVersion, e.g. runtime.Version()).
//
//   - A toolchain directive pins an exact toolchain: any difference from
//     the toolchain doing the compiling means the stdlib sources the
//     disk importer would read are not the ones the program asked for.
//   - A go directive is a language floor, not an exact pin: a program
//     declaring "go 1.23" compiles fine under go1.27, so only a go
//     directive NEWER than the toolchain fails (the toolchain cannot
//     provide that language version).
//
// A nil module (no enclosing go.mod) or absent directives impose no
// constraint: single-file programs without a go.mod keep compiling.
func checkModuleToolchain(m *goModule, toolchainVersion string) error {
	if m == nil {
		return nil
	}
	if m.toolchain != "" && m.toolchain != toolchainVersion {
		return fmt.Errorf("gofrontend: module %q declares toolchain %q, but this gofrontend was built with %q; refusing to compile against a different toolchain's stdlib", m.path, m.toolchain, toolchainVersion)
	}
	if m.goVersion != "" {
		if compareSemver(langMajorMinor(m.goVersion), langMajorMinor(toolchainVersion)) > 0 {
			return fmt.Errorf("gofrontend: module %q requires go %s, but this gofrontend was built with %s; refusing to compile against an older toolchain's stdlib", m.path, m.goVersion, toolchainVersion)
		}
	}
	return nil
}
