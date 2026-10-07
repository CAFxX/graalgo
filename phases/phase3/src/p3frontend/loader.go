// Package p3frontend compiles multi-file Go package directories into
// frontend/v1 JSON documents.
//
// Architecture (see DESIGN.md): the loader selects buildable files and
// resolves imports; the importer type-checks local packages with go/types
// (synthetic runtime/fmt/sync retained from the frozen overlay); the flattener
// merges every dependency into one package-main source file; export.go
// hands the merged file to the frozen gofrontend binary, whose stdout is
// the frontend/v1 document.
//
// Only the main package may be named "main"; every other local package is
// flattened under a content-derived identifier prefix. Anything the M1
// scope cannot compile (stdlib packages, func init, import cycles) fails
// fast with a named error — never silently.
package p3frontend

import (
	"bytes"
	"fmt"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Known GOOS/GOARCH tables (copied from go1.27.1's internal/syslist; the
// internal package is not importable, and the tables are part of the
// toolchain's stable contract).
// ---------------------------------------------------------------------------

var knownOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true,
	"freebsd": true, "hurd": true, "illumos": true, "ios": true,
	"js": true, "linux": true, "nacl": true, "netbsd": true,
	"openbsd": true, "plan9": true, "solaris": true, "wasip1": true,
	"windows": true, "zos": true,
}

var knownArch = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true,
	"armbe": true, "arm64": true, "arm64be": true, "loong64": true,
	"mips": true, "mipsle": true, "mips64": true, "mips64le": true,
	"mips64p32": true, "mips64p32le": true, "ppc": true, "ppc64": true,
	"ppc64le": true, "riscv": true, "riscv64": true, "s390": true,
	"s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}

var unixOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true,
	"freebsd": true, "hurd": true, "illumos": true, "ios": true,
	"linux": true, "netbsd": true, "openbsd": true, "solaris": true,
}

// ---------------------------------------------------------------------------
// Loader
// ---------------------------------------------------------------------------

// Loader selects buildable files and resolves imports. It targets
// linux/amd64 with the gc toolchain; the release tags cover go1.1 through
// go1.27 (the pinned go1.27.1 toolchain — no language pinning, GG-0013).
type Loader struct {
	goos   string
	goarch string
	goroot string

	resolveCache map[string]*PackageRef // (abs fromDir + "\x00" + import path) -> resolution
	moduleCache  map[string]moduleInfo  // dir -> enclosing module (or none)
}

type moduleInfo struct {
	root string // module root dir; "" when no go.mod was found
	path string // module path; "" when no go.mod was found
}

// PackageRef is the resolution of one import path.
type PackageRef struct {
	ImportPath string
	Kind       refKind
	Dir        string   // local/stdlib: package directory
	Files      []string // local/stdlib: selected .go basenames, sorted
	PkgName    string   // local: package clause name
}

type refKind int

const (
	kindSynthetic refKind = iota // "runtime"/"fmt"/"sync"/"time"/"context"/"errors"/"sort": resolved by the frozen overlay
	kindStdlib                   // $GOROOT/src/<path>: resolvable, not compilable in M1
	kindLocal                    // module-local package: flattened
)

// NewLoader builds a Loader targeting linux/amd64. GOROOT defaults to
// ~/tools/go-1.27.1 and is overridable with GRAALGO_GOROOT.
func NewLoader() *Loader {
	goroot := os.Getenv("GRAALGO_GOROOT")
	if goroot == "" {
		if home, err := os.UserHomeDir(); err == nil {
			goroot = filepath.Join(home, "tools", "go-1.27.1")
		}
	}
	return &Loader{
		goos:         "linux",
		goarch:       "amd64",
		goroot:       goroot,
		resolveCache: map[string]*PackageRef{},
		moduleCache:  map[string]moduleInfo{},
	}
}

// GOROOT reports the GOROOT the loader resolves stdlib paths against.
func (l *Loader) GOROOT() string { return l.goroot }

// ---------------------------------------------------------------------------
// Build-tag evaluation (mirrors go/build's shouldBuild/matchTag/goodOSArchFile)
// ---------------------------------------------------------------------------

// tagSatisfied reports whether a build tag holds for the loader's target.
// This mirrors go/build's matchTag for GOOS/GOARCH/compiler/unix/release
// tags; cgo and user -tags are never satisfied in M1.
func (l *Loader) tagSatisfied(tag string) bool {
	switch tag {
	case l.goos, l.goarch, "gc":
		return true
	}
	if tag == "unix" && unixOS[l.goos] {
		return true
	}
	if strings.HasPrefix(tag, "go1.") {
		if n, err := strconv.Atoi(strings.TrimPrefix(tag, "go1.")); err == nil {
			return n >= 1 && n <= 27
		}
	}
	return false
}

var (
	slashSlash = []byte("//")
	slashStar  = []byte("/*")
	starSlash  = []byte("*/")
	plusBuild  = []byte("+build")
)

// headerBuildLines scans a file's leading comment block and returns the
// trimmed header region plus the //go:build lines found there. The scan
// mirrors go/build's parseFileHeader: it stops at the first non-comment,
// non-blank line, tracks /* */ comment state, and records end — the
// offset just past the most recent blank line seen while the header was
// still open (!ended). The returned region is content[:end].
//
// The two constraint syntaxes are treated exactly as in go/build:
//   - //go:build lines are honored wherever they appear in the header,
//     even with no following blank line;
//   - // +build lines are honored only inside the trimmed region —
//     shouldBuild re-scans it line by line, so a // +build line with no
//     blank line before the package clause is ignored.
func headerBuildLines(content []byte) (trimmed []byte, goBuild []string) {
	end := 0
	p := content
	ended := false
	inSlashStar := false
lines:
	for len(p) > 0 {
		raw := p
		if i := bytes.IndexByte(raw, '\n'); i >= 0 {
			raw, p = raw[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line := bytes.TrimSpace(raw)
		if len(line) == 0 && !ended {
			// Remember the offset just past the most recent blank
			// line: constraint lines at or past this offset are
			// outside the build-constraint region.
			end = len(content) - len(p)
			continue lines
		}
		if !bytes.HasPrefix(line, slashSlash) {
			ended = true
		}
		if !inSlashStar {
			if s := string(line); constraint.IsGoBuild(s) {
				goBuild = append(goBuild, s)
			}
		}
		for len(line) > 0 {
			if inSlashStar {
				if i := bytes.Index(line, starSlash); i >= 0 {
					inSlashStar = false
					line = bytes.TrimSpace(line[i+len(starSlash):])
					continue
				}
				continue lines
			}
			if bytes.HasPrefix(line, slashSlash) {
				continue lines
			}
			if bytes.HasPrefix(line, slashStar) {
				inSlashStar = true
				line = bytes.TrimSpace(line[len(slashStar):])
				continue
			}
			break lines // non-comment text: the header ends here
		}
	}
	return content[:end], goBuild
}

// shouldBuild reports whether the file content is selected for the build,
// mirroring go/build's shouldBuild: a //go:build line, when present,
// controls exclusively (multiple lines are an error, as in go/build);
// otherwise every // +build line in the trimmed header region must hold.
func (l *Loader) shouldBuild(content []byte) (bool, error) {
	trimmed, goBuild := headerBuildLines(content)
	if len(goBuild) > 1 {
		return false, fmt.Errorf("multiple //go:build lines")
	}
	if len(goBuild) == 1 {
		x, err := constraint.Parse(goBuild[0])
		if err != nil {
			return false, fmt.Errorf("parsing //go:build line: %w", err)
		}
		return x.Eval(l.tagSatisfied), nil
	}
	// Like go/build, scan the trimmed header region line by line; an
	// unparseable // +build line is ignored. (The trimmed region provably
	// contains no /* */ lines — any /* line ends the header — so no
	// comment-state tracking is needed here, exactly as in go/build.)
	for _, raw := range bytes.Split(trimmed, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if !bytes.HasPrefix(line, slashSlash) || !bytes.Contains(line, plusBuild) {
			continue
		}
		s := string(line)
		if !constraint.IsPlusBuild(s) {
			continue
		}
		// Like go/build, an unparseable // +build line is ignored.
		if x, err := constraint.Parse(s); err == nil {
			if !x.Eval(l.tagSatisfied) {
				return false, nil
			}
		}
	}
	return true, nil
}

// goodOSArchFile mirrors go/build's goodOSArchFile: a trailing
// _GOOS, _GOARCH, or _GOOS_GOARCH filename suffix must match the target.
func (l *Loader) goodOSArchFile(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	i := strings.Index(base, "_")
	if i < 0 {
		return true
	}
	l_ := strings.Split(base[i:], "_") // l_[0] == ""
	if n := len(l_); n > 0 && l_[n-1] == "test" {
		l_ = l_[:n-1]
	}
	n := len(l_)
	if n >= 3 && knownOS[l_[n-2]] && knownArch[l_[n-1]] {
		return l.tagSatisfied(l_[n-2]) && l.tagSatisfied(l_[n-1])
	}
	if n >= 2 && (knownOS[l_[n-1]] || knownArch[l_[n-1]]) {
		return l.tagSatisfied(l_[n-1])
	}
	return true
}

// SelectFiles returns the sorted basenames of the .go files in dir that the
// toolchain would build for linux/amd64: no *_test.go, no _/.-prefixed
// files, filename-suffix filtering, and build-constraint filtering.
func (l *Loader) SelectFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("p3frontend: reading dir %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !l.goodOSArchFile(name) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("p3frontend: reading %s: %w", name, err)
		}
		ok, err := l.shouldBuild(content)
		if err != nil {
			return nil, fmt.Errorf("p3frontend: %s: %w", name, err)
		}
		if !ok {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// ---------------------------------------------------------------------------
// Import resolution
// ---------------------------------------------------------------------------

// packageClause returns the package clause name of the first file that
// declares one (files must be non-empty and sorted).
func packageClause(dir string, files []string) (string, error) {
	for _, f := range files {
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, filepath.Join(dir, f), nil, parser.PackageClauseOnly)
		if err != nil {
			return "", fmt.Errorf("p3frontend: parsing package clause of %s: %w", f, err)
		}
		if af.Name != nil {
			return af.Name.Name, nil
		}
	}
	return "", fmt.Errorf("p3frontend: no package clause found in %s", dir)
}

// findModule walks up from dir to the nearest go.mod and returns the module
// root and module path. Results are cached. A missing go.mod is not an
// error here: the caller decides whether module mode is required.
func (l *Loader) findModule(dir string) moduleInfo {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if mi, ok := l.moduleCache[abs]; ok {
		return mi
	}
	mi := moduleInfo{}
	for d := abs; ; d = filepath.Dir(d) {
		if content, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "module" {
					mi = moduleInfo{root: d, path: fields[1]}
					break
				}
			}
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	l.moduleCache[abs] = mi
	return mi
}

// localRef builds a PackageRef for a module-local package directory.
func (l *Loader) localRef(importPath, dir string) (*PackageRef, error) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("p3frontend: cannot resolve import %q: directory %s does not exist", importPath, dir)
	}
	files, err := l.SelectFiles(dir)
	if err != nil {
		return nil, err
	}
	// P3-M6 (GG-0009): every buildable .s file in a compiled package must
	// resolve to an inventory category; unsupported/uncategorized files
	// fail the build here -- never silently ignored.
	if _, err := l.CheckAsmFiles(dir, importPath); err != nil {
		return nil, err
	}
	name, err := packageClause(dir, files)
	if err != nil {
		return nil, err
	}
	return &PackageRef{ImportPath: importPath, Kind: kindLocal, Dir: dir, Files: files, PkgName: name}, nil
}

// ResolveImport maps an import path to a PackageRef. fromDir anchors module
// mode (the importing package's directory); results are cached by
// (fromDir, path) because resolution is anchored at fromDir — the same
// path can resolve differently (or fail) from different modules.
// "runtime"/"fmt"/"sync" resolve to synthetic markers; stdlib paths resolve to
// their GOROOT directory (proving the mapping — compiling them is a scope
// error raised by the importer); module-path-prefixed imports resolve to
// directories; anything else is an error. "time" and "context" resolve to
// synthetic markers (never GOROOT): the phase-1 exporter type-checks them
// against its synthetic time/context packages (ADR-0005), so the p3frontend
// mirror must not see the real GOROOT sources. "errors" and "sort" resolve
// to synthetic markers likewise (P3-M7 wave 1: real-Go synthetic packages
// in phases/phase1/frontend/synth_errors.go / synth_sort.go). "strconv"
// and "encoding/json" resolve to synthetic markers likewise (P3-M7 wave 1:
// real-Go synthetic packages in phases/phase1/frontend/synth_strconv.go /
// synth_json.go).
func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
	// Synthetic packages resolve independently of fromDir; keep them out
	// of the anchored cache.
	switch path {
	case "runtime", "fmt", "sync", "time", "context", "errors", "sort", "bytes", "math", "strings", "strconv", "encoding/json":
		return &PackageRef{ImportPath: path, Kind: kindSynthetic}, nil
	}
	anchor, err := filepath.Abs(fromDir)
	if err != nil {
		anchor = fromDir
	}
	key := anchor + "\x00" + path
	if ref, ok := l.resolveCache[key]; ok {
		return ref, nil
	}
	resolve := func() (*PackageRef, error) {
		if l.goroot != "" {
			if dir := filepath.Join(l.goroot, "src", path); isDir(dir) {
				files, err := l.SelectFiles(dir)
				if err != nil {
					return nil, err
				}
				return &PackageRef{ImportPath: path, Kind: kindStdlib, Dir: dir, Files: files}, nil
			}
		}
		mi := l.findModule(fromDir)
		if mi.path == "" {
			return nil, fmt.Errorf("p3frontend: cannot resolve import %q: not stdlib, and no go.mod found from %s", path, fromDir)
		}
		rel, ok := strings.CutPrefix(path, mi.path)
		if !ok || (rel != "" && !strings.HasPrefix(rel, "/")) {
			return nil, fmt.Errorf("p3frontend: cannot resolve import %q: not stdlib, not under module %q", path, mi.path)
		}
		return l.localRef(path, filepath.Join(mi.root, filepath.FromSlash(rel)))
	}
	ref, err := resolve()
	if err != nil {
		return nil, err
	}
	l.resolveCache[key] = ref
	return ref, nil
}

// mainRef resolves the main package directory itself (not via an import
// path). mainImportPath computes its pseudo import path for cycle naming.
func (l *Loader) mainRef(dir string) (*PackageRef, error) {
	files, err := l.SelectFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("p3frontend: no buildable Go files in %s", dir)
	}
	// P3-M6 (GG-0009): same .s fail-fast as localRef -- see above.
	if _, err := l.CheckAsmFiles(dir, l.mainImportPath(dir)); err != nil {
		return nil, err
	}
	name, err := packageClause(dir, files)
	if err != nil {
		return nil, err
	}
	if name != "main" {
		return nil, fmt.Errorf("p3frontend: %s: expected package main, found %q", dir, name)
	}
	return &PackageRef{ImportPath: l.mainImportPath(dir), Kind: kindLocal, Dir: dir, Files: files, PkgName: name}, nil
}

// mainImportPath is the main package's import path when it lives in a
// module, or the pseudo-path "main" otherwise (used for cycle naming and
// the type-checker's package path only; it never appears in output).
func (l *Loader) mainImportPath(dir string) string {
	mi := l.findModule(dir)
	if mi.path == "" {
		return "main"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return mi.path
	}
	rel, err := filepath.Rel(mi.root, abs)
	if err != nil || rel == "." {
		return mi.path
	}
	return mi.path + "/" + filepath.ToSlash(rel)
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// ---------------------------------------------------------------------------
// Dependency closure
// ---------------------------------------------------------------------------

// importSpec is one parsed import: path plus blank/dot/name form.
type importSpec struct {
	Path  string
	Blank bool // import _ "x"
	Dot   bool // import . "x"
}

// importsOf parses the import specs of every selected file in ref (files
// sorted, specs in source order), deduplicated by path. Only the path and
// the blank/dot form matter to the closure; renames are handled by go/types.
func importsOf(ref *PackageRef) ([]importSpec, error) {
	var out []importSpec
	seen := map[string]bool{}
	for _, f := range ref.Files {
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, filepath.Join(ref.Dir, f), nil, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("p3frontend: parsing imports of %s: %w", f, err)
		}
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, "\"`")
			if seen[path] {
				continue
			}
			seen[path] = true
			spec := importSpec{Path: path}
			if imp.Name != nil {
				switch imp.Name.Name {
				case "_":
					spec.Blank = true
				case ".":
					spec.Dot = true
				}
			}
			out = append(out, spec)
		}
	}
	// Sorted for a deterministic closure order (and canonical cycle paths).
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// LoadClosure returns the main package's local dependencies in dependency
// order (dependencies before dependents; ties broken by import path).
// Synthetic (runtime/fmt/sync) imports are not part of the closure. An import
// cycle is a fail-fast error containing "import cycle" and naming the
// full path — the walk is iterative over a visited set plus an explicit
// stack, so it cannot hang.
func (l *Loader) LoadClosure(mainDir string, main *PackageRef) ([]*PackageRef, error) {
	var order []*PackageRef
	visited := map[string]bool{}
	var stack []string

	var visit func(ref *PackageRef) error
	visit = func(ref *PackageRef) error {
		for _, p := range stack {
			if p == ref.ImportPath {
				cycle := append(append([]string{}, stack...), ref.ImportPath)
				return fmt.Errorf("p3frontend: import cycle: %s", strings.Join(cycle, " -> "))
			}
		}
		if visited[ref.ImportPath] {
			return nil
		}
		visited[ref.ImportPath] = true
		stack = append(stack, ref.ImportPath)
		specs, err := importsOf(ref)
		if err != nil {
			return err
		}
		for _, spec := range specs {
			dep, err := l.ResolveImport(ref.Dir, spec.Path)
			if err != nil {
				return err
			}
			if dep.Kind != kindLocal {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		if ref != main {
			order = append(order, ref)
		}
		return nil
	}

	if err := visit(main); err != nil {
		return nil, err
	}
	return order, nil
}
