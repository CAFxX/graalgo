// Package srctest implements the GOROOT/test differential adapter
// (conformance layer: SPEC §5.2 layer 2, GG-0029/GG-0032).
//
// Go's test/ tree (356 *.go files directly under GOROOT/test) is the
// language spec-semantics anchor: conformance programs run by gc's own
// test driver (test/run.go). The adapter classifies every file and runs
// the runnable ones differential gc-a vs graalgo through the shared
// harness backends, using the product launcher.
//
// Classification:
//
//	run                 package main + func main: build+run parity under
//	                    the product launcher (P3SyncMain, backends-p3.json).
//	errorcheck          negative tests (gc itself rejects them — detected
//	                    by a gc build probe at scan time — plus negative
//	                    syntax tests): build-failure parity — both backends
//	                    must reject.
//	notrun:<why>        not a standalone program for this target (not
//	                    package main, no func main, //go:build unsatisfiable
//	                    on linux/amd64+gc): nothing to compare.
//
// There are NO automatic exclusions. A runnable file that diverges is
// listed explicitly, with a SPEC §5.6 reason code, in
// srctest-exclusions.yaml; listed files still run, and one that matches gc
// is reported STALE and fails the run (prune the entry).
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
		if (tf.Class == runClass || tf.Class == typeparam.ClassCompile) && goBin != "" {
			builds, err := gcBuilds(goBin, tf.Srcs[0])
			if err != nil {
				return nil, err
			}
			switch {
			case builds:
			case tf.Class == typeparam.ClassCompile:
				// `// compile` fragments testdir compiles with `go tool
				// compile` alone (e.g. bodyless func declarations, import.go,
				// parentype.go): `go build` links them and rejects. There is
				// no gc build to compare against: nothing to compare.
				tf.Class = typeparam.ClassNotRun
				tf.Detail = "// compile fragment: `go build` rejects it (testdir only runs `go tool compile`)"
			default:
				tf.Class = "errorcheck"
				tf.Detail = "negative test: gc rejects it; build-failure parity"
			}
		}
		if x, ok := yamlExcl[tf.Name]; ok {
			// Reviewed expected divergence: the file still runs; the
			// run verdict is EXPECTED while it diverges and STALE (a
			// failure) once it matches gc.
			tf.Expected = x.Reason
			tf.ExpectedSig = x.Signature
			tf.Detail = "expected divergence (" + ExclusionsFileName() + "): " + x.Detail
		}
		out = append(out, tf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Select keeps only the named files of a scan (a partial run); naming a
// file the scan does not have is an error, never an empty pass.
func Select(files []typeparam.TFile, names []string) ([]typeparam.TFile, error) {
	want := map[string]bool{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			want[n] = true
		}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("-files names no file")
	}
	var out []typeparam.TFile
	for _, f := range files {
		if want[f.Name] {
			out = append(out, f)
			delete(want, f.Name)
		}
	}
	if len(want) > 0 {
		var missing []string
		for n := range want {
			missing = append(missing, n)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("-files: not in the scanned directory: %s", strings.Join(missing, ","))
	}
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

// gcBuilds reports whether gc can build the single-file program. Only a
// compile REJECTION (go build exited non-zero with a diagnostic naming the
// source) makes a file a negative test; any other probe failure (no temp
// dir, toolchain cannot start, a toolchain-level error) is an
// infrastructure error, never "gc rejects it" (global review G4).
func gcBuilds(goBin, src string) (bool, error) {
	dir, err := os.MkdirTemp("", "srctest-probe-*")
	if err != nil {
		return false, fmt.Errorf("infra: srctest build probe: %w", err)
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(goBin, "build", "-o", filepath.Join(dir, "prog"), src)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GO111MODULE=off")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err == nil {
		return true, nil
	}
	if _, isExit := err.(*exec.ExitError); !isExit {
		return false, fmt.Errorf("infra: srctest build probe of %s: %w", src, err)
	}
	if !strings.Contains(out.String(), filepath.Base(src)) {
		return false, fmt.Errorf("infra: srctest build probe of %s failed without a diagnostic for it: %s", src, out.String())
	}
	return false, nil
}

// classify determines a file's class from its build constraints, package
// clause, func main presence, and import set.
func classify(testDir, name string) typeparam.TFile {
	tf := typeparam.TFile{Name: name}
	path := filepath.Join(testDir, name)

	constraint := readBuildConstraint(path)
	if constraint != "" {
		if !satisfiable(constraint) {
			tf.Class = typeparam.ClassNotRun
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
		tf.Class = typeparam.ClassNotRun
		tf.Detail = "not a program: package " + f.Name.Name + " (not package main)"
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
		tf.Class = typeparam.ClassNotRun
		tf.Detail = "not a program: package main without func main"
		return tf
	}
	// No automatic exclusions: cgo, stdlib imports and concurrency used
	// to be excluded by rule (298 of 356 files never compared). Every
	// runnable program now runs under the product launcher; a divergence
	// is either fixed or listed explicitly, reason-coded, in
	// srctest-exclusions.yaml (SPEC 5.6).
	tf.Class = runClass
	tf.Detail = "runnable program"
	// The action line gc's own test driver (cmd/internal/testdir) obeys:
	// `// skip` files are not run at all; `// compile` and `// build`
	// files are only compiled (sieve.go runs forever); `// run` words
	// after the build flags are program arguments, and `// run` programs
	// run in GOROOT/test (testdir's default working directory).
	action, words := readAction(path)
	switch action {
	case "skip":
		tf.Class = typeparam.ClassNotRun
		tf.Detail = "// skip: gc's test driver does not run it"
		return tf
	case "compile", "build":
		tf.Class = typeparam.ClassCompile
		tf.Detail = "// " + action + ": compile-only (build parity), as gc's test driver"
	case "run", "runoutput":
		tf.RunDir = testDir
		tf.Args = runArgs(words)
		if len(tf.Args) > 0 {
			tf.Detail += " (args " + strings.Join(tf.Args, " ") + ")"
		}
	}
	if constraint != "" {
		tf.Detail += " (//go:build " + constraint + ")"
	}
	return tf
}

// readAction returns the test-driver action of a GOROOT/test file — the
// first word of its first line when that line is a `//` comment, as
// cmd/internal/testdir reads it — and the words that follow it.
func readAction(path string) (string, []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	first, _, _ := strings.Cut(string(data), "\n")
	rest, ok := strings.CutPrefix(strings.TrimSpace(first), "//")
	if !ok {
		return "", nil
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return "", nil
	}
	return f[0], f[1:]
}

// runArgs extracts the program arguments from a `// run` line's words:
// testdir passes leading -flags to the go command and extra *.go files
// as sources; the remaining words are the program's arguments.
func runArgs(words []string) []string {
	i := 0
	for i < len(words) && strings.HasPrefix(words[i], "-") {
		i++
	}
	var args []string
	for _, w := range words[i:] {
		if strings.HasSuffix(w, ".go") {
			continue
		}
		args = append(args, w)
	}
	return args
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
