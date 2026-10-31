// Package srctest implements the GOROOT/test differential adapter
// (conformance layer: docs/arch/phases/phase3/PLAN.md §5 layer 2, GG-0029/GG-0032).
//
// Go's test/ tree (356 *.go files directly under GOROOT/test) is the
// language spec-semantics anchor: conformance programs run by gc's own
// test driver (test/run.go). The adapter classifies every file and runs
// the included ones differential gc-a vs graalgo through the shared
// harness backends. The graalgo-run launcher is the frontend's
// test consumer (same backend the typeparam adapter uses).
//
// Classification (docs/arch/phases/phase1/PLAN.md §5 item I: "GOROOT/test 356 files
// via adapter (filtered: no imports beyond builtins; exclusions
// versioned)"):
//
//	run                 package main + func main, imports ⊆ {fmt, runtime,
//	                    unsafe} — the language-conformance subset. fmt
//	                    resolves via the frontend's phase-1 import overlay
//	                    stub (D4); runtime/unsafe are overlay-supported.
//	                    Programs importing sync/time/context/sync-atomic
//	                    test stdlib semantics, not language semantics;
//	                    they belong to layer 3's wave corpora.
//	errorcheck          negative tests (gc itself rejects them — detected
//	                    by a gc build probe at scan time — plus negative
//	                    syntax tests): build-failure parity — both backends
//	                    must reject.
//	exclude:<reason>    versioned YAML exclusions (srctest-exclusions.yaml,
//	                    GG-0033) + structural exclusions:
//	                    - //go:build ignore or otherwise unsatisfiable in
//	                      this environment -> backend-specific
//	                    - not package main, or no func main -> not-runnable
//	                      (backend-specific)
//	                    - import "C" -> backend-specific (graalgo has no cgo)
//	                    - imports beyond the language subset, or use of
//	                      chan/go/select -> deferred-parity
//
// The differential execution reuses typeparam.Run (same build+run parity
// operation); only the classifier is srctest-specific.
package srctest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"diffharness/internal/typeparam"
)

// ExclusionsFileName is the conventional YAML file name (GG-0033).
func ExclusionsFileName() string { return "srctest-exclusions.yaml" }

// runClass is the class string for included files (mirrors typeparam).
const runClass = "run"

// Scan classifies every *.go file directly under testDir. yamlExcl are the
// exclusions from the versioned YAML file; files they name are reported as
// exclude:<reason> regardless of content.
//
// For files that would otherwise be "run", a gc build probe distinguishes
// negative tests (gc itself rejects them) from runnable programs: the
// former become errorcheck (build-failure parity), the latter stay run.
// The probe needs a Go toolchain; when none is found, the file stays run
// and the differential surfaces the gc rejection at run time.
func Scan(testDir string, yamlExcl map[string]typeparam.Exclusion) ([]typeparam.TFile, error) {
	entries, err := os.ReadDir(testDir)
	if err != nil {
		return nil, fmt.Errorf("read test dir: %w", err)
	}
	goBin := findGo()
	var out []typeparam.TFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		tf := classify(testDir, e.Name())
		tf.Srcs = []string{filepath.Join(testDir, e.Name())}
		if tf.Class == runClass && goBin != "" {
			if !gcBuilds(goBin, tf.Srcs[0]) {
				tf.Class = "errorcheck"
				tf.Detail = "negative test: gc rejects it; build-failure parity"
			}
		}
		if x, ok := yamlExcl[tf.Name]; ok {
			tf.Class = "exclude:" + x.Reason
			tf.Detail = "excluded by " + ExclusionsFileName() + ": " + x.Detail
		}
		out = append(out, tf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// findGo locates a Go toolchain for the build probe (DH_GO, then PATH).
func findGo() string {
	if dh := os.Getenv("DH_GO"); dh != "" {
		if p := filepath.Join(dh, "bin", "go"); isExec(p) {
			return p
		}
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return ""
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// gcBuilds reports whether gc can build the single-file program.
func gcBuilds(goBin, src string) bool {
	dir, err := os.MkdirTemp("", "srctest-probe-*")
	if err != nil {
		return true // probe inconclusive: don't reclassify
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(goBin, "build", "-o", filepath.Join(dir, "prog"), src)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GO111MODULE=off")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// languageImports is the import set the conformance layer covers: the
// language-conformance subset (see package doc). Everything else is
// stdlib semantics (layer 3) or unsupported (backend-specific).
var languageImports = map[string]bool{
	"fmt": true, "runtime": true, "unsafe": true,
}

// classify determines a file's class from its build constraints, package
// clause, func main presence, and import set.
func classify(testDir, name string) typeparam.TFile {
	tf := typeparam.TFile{Name: name}
	path := filepath.Join(testDir, name)

	constraint := readBuildConstraint(path)
	if constraint != "" {
		if !satisfiable(constraint) {
			tf.Class = "exclude:backend-specific"
			tf.Detail = "//go:build " + constraint + " unsatisfiable on linux/amd64+gc"
			return tf
		}
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		// Negative syntax test (e.g. bombad.go, char_lit1.go): the file is
		// intentionally invalid Go. Differential expectation is
		// build-failure parity — both backends must reject it.
		tf.Class = "errorcheck"
		tf.Detail = "negative syntax test: both backends must fail to build (" + firstErrLine(err) + ")"
		return tf
	}
	if f.Name.Name != "main" {
		tf.Class = "exclude:backend-specific"
		tf.Detail = "not-runnable: package " + f.Name.Name + " (not package main)"
		return tf
	}
	// Need the full AST for func main detection.
	f2, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// Negative syntax test: build-failure parity (see above).
		tf.Class = "errorcheck"
		tf.Detail = "negative syntax test: both backends must fail to build (" + firstErrLine(err) + ")"
		return tf
	}
	if !hasFuncMain(f2) {
		tf.Class = "exclude:backend-specific"
		tf.Detail = "not-runnable: package main without func main"
		return tf
	}
	for _, imp := range f.Imports {
		ipath := strings.Trim(imp.Path.Value, "\"`")
		if ipath == "C" {
			tf.Class = "exclude:backend-specific"
			tf.Detail = "import \"C\": graalgo has no cgo support"
			return tf
		}
		if !languageImports[ipath] {
			tf.Class = "exclude:deferred-parity"
			tf.Detail = "imports " + ipath + ": stdlib semantics, covered by layer-3 wave corpora"
			return tf
		}
	}
	// Concurrency primitives (chan types, go statements, select, channel
	// send/receive) need the phase-2 scheduler; the phase-1 pipeline is
	// single-threaded. These programs belong to layer 3 (P3SyncMain).
	if usesConcurrency(f2) {
		tf.Class = "exclude:deferred-parity"
		tf.Detail = "needs concurrency (chan/go/select): phase-2 scheduler domain, covered by layer 3"
		return tf
	}
	tf.Class = runClass
	tf.Detail = "language-conformance subset"
	if constraint != "" {
		tf.Detail += " (//go:build " + constraint + ")"
	}
	return tf
}

// firstErrLine condenses a parse error to one line for the detail field.
func firstErrLine(err error) string {
	s := err.Error()
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// usesConcurrency reports whether the file uses channel/goroutine/select
// primitives (phase-2 scheduler domain).
func usesConcurrency(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.ChanType:
			found = true
		case *ast.GoStmt:
			found = true
		case *ast.SelectStmt:
			found = true
		case *ast.SendStmt:
			found = true
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				found = true
			}
		}
		return !found
	})
	return found
}

// hasFuncMain reports whether the file declares func main().
func hasFuncMain(f *ast.File) bool {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "main" {
			continue
		}
		return true
	}
	return false
}

var goBuildRe = regexp.MustCompile(`^//go:build\s+(.+)$`)
var plusBuildRe = regexp.MustCompile(`^//\s*\+build\s+(.+)$`)

// readBuildConstraint returns the //go:build expression ("" when absent),
// falling back to the legacy // +build line.
func readBuildConstraint(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var plus string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if m := goBuildRe.FindStringSubmatch(line); m != nil {
			return strings.TrimSpace(m[1])
		}
		if m := plusBuildRe.FindStringSubmatch(line); m != nil {
			plus = strings.TrimSpace(m[1])
		}
		if line != "" && !strings.HasPrefix(line, "//") {
			break
		}
	}
	return plus
}

// envTags is the build-tag environment the conformance layer runs in.
var envTags = map[string]bool{
	"linux": true, "amd64": true, "unix": true, "gc": true,
}

// satisfiable evaluates a //go:build boolean expression against envTags.
// Unknown terms (other GOOS/GOARCH, cgo, race, ...) make the constraint
// unsatisfiable here. go1.N terms are satisfied for N <= 27.
func satisfiable(expr string) bool {
	toks := tokenize(expr)
	pos := 0
	v, ok := parseOr(toks, &pos)
	return ok && v && pos == len(toks)
}

func tokenize(expr string) []string {
	var toks []string
	cur := ""
	flush := func() {
		if cur != "" {
			toks = append(toks, cur)
			cur = ""
		}
	}
	for _, r := range expr {
		switch {
		case r == ' ' || r == '\t':
			flush()
		case r == '(' || r == ')':
			flush()
			toks = append(toks, string(r))
		case r == '&' || r == '|':
			if cur == string(r) {
				// Second char of && or ||.
				toks = append(toks, cur+string(r))
				cur = ""
			} else {
				flush()
				cur = string(r)
			}
		case r == '!':
			flush()
			toks = append(toks, "!")
		default:
			cur += string(r)
		}
	}
	flush()
	return toks
}

func parseOr(toks []string, pos *int) (bool, bool) {
	l, ok := parseAnd(toks, pos)
	if !ok {
		return false, false
	}
	for *pos < len(toks) && toks[*pos] == "||" {
		*pos++
		r, ok := parseAnd(toks, pos)
		if !ok {
			return false, false
		}
		l = l || r
	}
	return l, true
}

func parseAnd(toks []string, pos *int) (bool, bool) {
	l, ok := parseUnary(toks, pos)
	if !ok {
		return false, false
	}
	for *pos < len(toks) && toks[*pos] == "&&" {
		*pos++
		r, ok := parseUnary(toks, pos)
		if !ok {
			return false, false
		}
		l = l && r
	}
	return l, true
}

func parseUnary(toks []string, pos *int) (bool, bool) {
	if *pos < len(toks) && toks[*pos] == "!" {
		*pos++
		v, ok := parseUnary(toks, pos)
		return !v, ok
	}
	if *pos < len(toks) && toks[*pos] == "(" {
		*pos++
		v, ok := parseOr(toks, pos)
		if !ok || *pos >= len(toks) || toks[*pos] != ")" {
			return false, false
		}
		*pos++
		return v, true
	}
	if *pos >= len(toks) {
		return false, false
	}
	t := toks[*pos]
	*pos++
	if t == "ignore" {
		return false, true
	}
	if strings.HasPrefix(t, "go1.") {
		return goVersionSatisfied(t), true
	}
	return envTags[t], true
}

// goVersionSatisfied reports whether a go1.N tag is satisfied by go1.27.
func goVersionSatisfied(t string) bool {
	var n int
	if _, err := fmt.Sscanf(t, "go1.%d", &n); err != nil {
		return false
	}
	return n <= 27
}
