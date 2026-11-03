package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// discoverTests returns the names of TestXxx functions in a _test.go file.
// TestMain is NOT a test: it is reported separately (see discoveredFile)
// and never returned here, so it can never be called with a *graalgoT
// argument the way the old 1-param selection did (P1-T1).
func discoverTests(path string) ([]string, error) {
	df, err := discoverTestFile(path)
	if err != nil {
		return nil, err
	}
	return df.tests, nil
}

// discoveredExample is one ExampleXxx function in a _test.go file.
type discoveredExample struct {
	name      string
	output    string // expected stdout, without a trailing newline
	hasOutput bool   // false when the example has no Output: comment
	unordered bool   // true for "Unordered output:" comments
}

// discoveredFile describes one _test.go file's entry points: its tests,
// whether it declares TestMain, and its examples.
type discoveredFile struct {
	tests       []string
	hasTestMain bool
	examples    []discoveredExample
}

// discoverTestFile parses one _test.go file and reports its tests,
// TestMain presence, and examples (with output comments). A function
// named TestMain is excluded from tests even though it matches the
// Test* prefix: its parameter is *testing.M, not *testing.T, and the
// driver synthesises a dedicated graalgoM path for it instead.
func discoverTestFile(path string) (discoveredFile, error) {
	var df discoveredFile
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return df, fmt.Errorf("parse: %w", err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if name == "TestMain" {
			df.hasTestMain = true
			continue
		}
		if strings.HasPrefix(name, "Test") {
			// Must take *testing.T (or be a simple TestXxx with one param).
			if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
				continue
			}
			df.tests = append(df.tests, name)
			continue
		}
		if strings.HasPrefix(name, "Example") {
			// Examples take no parameters and return nothing.
			if fn.Type.Params != nil && len(fn.Type.Params.List) != 0 {
				continue
			}
			if fn.Type.Results != nil && len(fn.Type.Results.List) != 0 {
				continue
			}
			ex, err := discoverExample(f, fn)
			if err != nil {
				return df, err
			}
			df.examples = append(df.examples, ex)
		}
	}
	return df, nil
}

// discoverExample extracts one example's expected output from the
// Output: (or Unordered output:) comment inside its body, following
// gc's rule that the output comment is a comment group within the
// function body whose first line is the Output: marker. The marker line
// itself, and a "Output: <text>" first line's trailing text, are not
// part of the expected output; remaining lines are, joined with "\n".
// A second, separate Output: comment group in the same body is an
// error under gc ("there can only be one output comment block per
// example") and is rejected here instead of being silently ignored
// while the first block's output is used. Note the distinction: lines
// after the marker WITHIN the same comment group that happen to read
// "Output: ..." are literal expected output, not a second block.
func discoverExample(f *ast.File, fn *ast.FuncDecl) (discoveredExample, error) {
	ex := discoveredExample{name: fn.Name.Name}
	found := false
	for _, cg := range f.Comments {
		if cg.Pos() < fn.Body.Pos() || cg.End() > fn.Body.End() {
			continue
		}
		text := cg.Text()
		lines := strings.Split(text, "\n")
		// CommentGroup.Text() drops a trailing empty line.
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) == 0 {
			continue
		}
		var rest []string
		unordered := false
		switch {
		case lines[0] == "Output:":
			rest = lines[1:]
		case strings.HasPrefix(lines[0], "Output: "):
			rest = append([]string{strings.TrimPrefix(lines[0], "Output: ")}, lines[1:]...)
		case lines[0] == "Unordered output:":
			unordered = true
			rest = lines[1:]
		case strings.HasPrefix(lines[0], "Unordered output: "):
			unordered = true
			rest = append([]string{strings.TrimPrefix(lines[0], "Unordered output: ")}, lines[1:]...)
		default:
			continue
		}
		if found {
			return ex, fmt.Errorf("example %s: there can only be one output comment block per example", fn.Name.Name)
		}
		found = true
		ex.unordered = unordered
		ex.hasOutput = true
		ex.output = strings.Join(rest, "\n")
	}
	return ex, nil
}

// testShimTemplate is a minimal testing.T replacement, inlined into every
// synthesized test driver. Diagnostic calls (Errorf, Fatalf, …) are
// rewritten at synthesis time to position-capturing variants
// (graalgoErrorf et al.), so failure output carries file:line exactly
// like `go test`.
//
// GRAALGO_VERBOSE_PLACEHOLDER is replaced with true/false at synthesis
// time; verbosity is baked into the driver so no runtime flag plumbing
// is needed.
const testShimTemplate = `
// --- graalgo test shim (generated) ---
const graalgoVerbose = GRAALGO_VERBOSE_PLACEHOLDER

type graalgoT struct {
	name       string
	depth      int
	failed     bool
	skipped    bool
	output     []string // buffered detail lines (flushed on failure when !verbose)
	subresults []string // buffered subtest result lines (flushed after own result)
	cleanups   []func() // registered by Cleanup, run LIFO when the test returns
}

// logLine emits a detail line: immediately in verbose mode, buffered
// otherwise (flushed after the --- FAIL line, matching go test).
func (t *graalgoT) logLine(s string) {
	if graalgoVerbose {
		println(s)
	} else {
		t.output = append(t.output, s)
	}
}

func (t *graalgoT) indent() string {
	s := ""
	for i := 0; i < t.depth; i++ {
		s += "    "
	}
	return s
}

func (t *graalgoT) graalgoErrorf(fileline, format string, args ...interface{}) {
	t.failed = true
	t.logLine("    " + fileline + ": " + sprintf(format, args...))
}

func (t *graalgoT) graalgoError(fileline string, args ...interface{}) {
	t.failed = true
	t.logLine("    " + fileline + ": " + joinArgs(args...))
}

func (t *graalgoT) graalgoFatalf(fileline, format string, args ...interface{}) {
	t.failed = true
	t.logLine("    " + fileline + ": " + sprintf(format, args...))
	panic("graalgo test fatal: " + t.name)
}

func (t *graalgoT) graalgoFatal(fileline string, args ...interface{}) {
	t.failed = true
	t.logLine("    " + fileline + ": " + joinArgs(args...))
	panic("graalgo test fatal: " + t.name)
}

func (t *graalgoT) graalgoLogf(fileline, format string, args ...interface{}) {
	t.logLine("    " + fileline + ": " + sprintf(format, args...))
}

func (t *graalgoT) graalgoLog(fileline string, args ...interface{}) {
	t.logLine("    " + fileline + ": " + joinArgs(args...))
}

func (t *graalgoT) graalgoSkip(fileline string, args ...interface{}) {
	t.skipped = true
	t.logLine("    " + fileline + ": " + joinArgs(args...))
	panic("graalgo test skip: " + t.name)
}

func (t *graalgoT) graalgoSkipf(fileline, format string, args ...interface{}) {
	t.skipped = true
	t.logLine("    " + fileline + ": " + sprintf(format, args...))
	panic("graalgo test skip: " + t.name)
}

func (t *graalgoT) graalgoSkipNow(fileline string) {
	t.skipped = true
	panic("graalgo test skip: " + t.name)
}

func (t *graalgoT) Fail() {
	t.failed = true
}

func (t *graalgoT) Failed() bool {
	return t.failed
}

func (t *graalgoT) Name() string {
	return t.name
}

func (t *graalgoT) Helper() {}

func (t *graalgoT) Skipped() bool {
	return t.skipped
}

// FailNow marks the test failed and stops it via the same panic
// mechanism Fatal uses; the enclosing Run/main recover turns it into
// a --- FAIL line without aborting siblings or the parent test.
func (t *graalgoT) FailNow() {
	t.failed = true
	panic("graalgo test fatal: " + t.name)
}

// Parallel is a no-op: this driver runs tests sequentially in
// declaration order. A test that depends on true parallelism (e.g.
// timing out unless another test runs concurrently) will not behave
// like under go test; that limitation is declared here, not hidden.
func (t *graalgoT) Parallel() {}

// Cleanup registers f to run, LIFO, when this test (or subtest)
// returns — including via FailNow/Fatal/Skip panics, because Run and
// the generated main run cleanups after recovering.
func (t *graalgoT) Cleanup(f func()) {
	t.cleanups = append(t.cleanups, f)
}

func (t *graalgoT) runCleanups() {
	for i := len(t.cleanups) - 1; i >= 0; i-- {
		t.cleanups[i]()
	}
	t.cleanups = nil
}

// TempDir creates a fresh empty directory and registers its removal
// as a cleanup, mirroring testing.T.TempDir.
func (t *graalgoT) TempDir() string {
	dir, err := os.MkdirTemp("", "graalgo-test-")
	if err != nil {
		panic("graalgo test tempdir: " + err.Error())
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// Setenv sets an environment variable for the duration of the test
// and registers restoration of the previous value (or unset state)
// as a cleanup.
func (t *graalgoT) Setenv(key, value string) {
	old, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		// gc fails the test ("cannot set environment variable");
		// silently continuing with the variable unset hid the
		// failure and let the test assert against a stale env.
		// The log line carries no file:line (the shim has none);
		// the fatal sentinel still stops exactly this test.
		t.failed = true
		t.logLine("cannot set environment variable: " + err.Error())
		panic("graalgo test fatal: " + t.name)
	}
	t.Cleanup(func() {
		if existed {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	})
}

// Chdir changes the working directory for the duration of the test
// and registers restoration of the previous directory as a cleanup.
func (t *graalgoT) Chdir(dir string) {
	old, err := os.Getwd()
	if err != nil {
		panic("graalgo test chdir: " + err.Error())
	}
	if err := os.Chdir(dir); err != nil {
		panic("graalgo test chdir: " + err.Error())
	}
	t.Cleanup(func() { os.Chdir(old) })
}

// Deadline reports that no test timeout is wired into this driver:
// there is no deadline, matching a go test run without -timeout.
func (t *graalgoT) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

// Context returns a background context: no timeout or cancellation is
// wired into this driver, so unlike go test the context is not
// cancelled when the test ends. Code that depends on post-test
// cancellation will not observe it here.
func (t *graalgoT) Context() context.Context {
	return context.Background()
}

func (t *graalgoT) Run(name string, f func(*graalgoT)) bool {
	sub := &graalgoT{name: t.name + "/" + name, depth: t.depth + 1}
	// === RUN lines are never indented, matching go test.
	if graalgoVerbose {
		println("=== RUN   " + sub.name)
	}
	t0 := time.Now()
	// Recover inside Run, mirroring the top-level recover: a Fatal,
	// FailNow, or Skip in a subtest stops only that subtest, never
	// the parent — code after t.Run(...) still runs. A skipped
	// subtest does NOT mark the parent skipped (go test semantics);
	// a failed subtest does mark the parent failed.
	//
	// Only a panic naming THIS subtest is this subtest's: the
	// graalgoSkip*/graalgoFatal* helpers already set sub.skipped /
	// sub.failed before panicking, so the exact-name branches swallow
	// without re-marking. A Fatal/Skip panic naming an ancestor (the
	// parent's own Fatal/Skip called from inside the subtest closure)
	// must NOT be swallowed here — prefix matching misattributed it
	// to the subtest and let the parent keep executing after t.Run.
	// It is re-panicked with the same sentinel string so an outer Run
	// or the top-level recover handles it as that test's.
	func() {
		defer func() {
			if r := recover(); r != nil {
				s := toString(r)
				if s == "graalgo test skip: "+sub.name {
					// Already marked skipped by graalgoSkip*.
				} else if s == "graalgo test fatal: "+sub.name {
					// Already marked failed by graalgoFatal*.
				} else if (len(s) >= 17 && s[:17] == "graalgo test skip") || (len(s) >= 18 && s[:18] == "graalgo test fatal") {
					// An ancestor's Fatal/Skip: re-panic with the
					// same sentinel string so an outer Run or the
					// top-level recover handles it as that test's.
					panic(s)
				} else {
					sub.failed = true
					sub.logLine("panic: " + s)
				}
			}
		}()
		f(sub)
	}()
	sub.runCleanups()
	dur := "(" + fmtDur2(time.Since(t0)) + ")"
	if sub.failed {
		t.failed = true
	}
	// Subtest result lines are buffered on the parent and flushed after
	// the parent's own result line, matching go test's ordering. In
	// non-verbose mode only failures are shown.
	if graalgoVerbose || sub.failed {
		var line string
		if sub.failed {
			line = "--- FAIL: " + sub.name + " " + dur
		} else if sub.skipped {
			line = "--- SKIP: " + sub.name + " " + dur
		} else {
			line = "--- PASS: " + sub.name + " " + dur
		}
		t.subresults = append(t.subresults, sub.indent()+line)
		if sub.failed && !graalgoVerbose {
			for _, l := range sub.output {
				t.subresults = append(t.subresults, sub.indent()+l)
			}
		}
	}
	// Bubble up nested subtest results (already indented).
	t.subresults = append(t.subresults, sub.subresults...)
	return !sub.failed
}

func joinArgs(args ...interface{}) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += toString(a)
	}
	return out
}

func sprintf(format string, args ...interface{}) string {
	// Minimal %v/%s/%d formatting sufficient for test diagnostics.
	out := ""
	ai := 0
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) && ai < len(args) {
			i++
			switch format[i] {
			case 'v', 's', 'd', 'q', 'x':
				out += toString(args[ai])
				ai++
			case '%':
				out += "%"
			default:
				out += "%"
				out += string(format[i])
			}
		} else {
			out += string(format[i])
		}
	}
	for ; ai < len(args); ai++ {
		out += " "
		out += toString(args[ai])
	}
	return out
}

func toString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return itoa(int64(x))
	case int64:
		return itoa(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return "<value>"
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [32]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// fmtDur2 formats a duration like go test's per-test timing: (0.00s).
func fmtDur2(d time.Duration) string {
	h := (int64(d) + 5000000) / 10000000 // hundredths, rounded
	if h < 0 {
		h = 0
	}
	return itoa(h/100) + "." + twoDigits(h%100) + "s"
}

func twoDigits(n int64) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
`

// testShimSource returns the shim with verbosity baked in.
func testShimSource(verbose bool) string {
	v := "false"
	if verbose {
		v = "true"
	}
	return strings.Replace(testShimTemplate, "GRAALGO_VERBOSE_PLACEHOLDER", v, 1)
}

// fileTests groups the selected tests of one _test.go file, plus the
// examples selected from it (examples are run as pseudo-tests with
// output comparison; see the example wrappers emitted by
// synthesizeTestDriver).
type fileTests struct {
	file     string
	tests    []string
	examples []discoveredExample
}

// testCallRewrites maps testing.T diagnostic methods to the shim's
// position-capturing variants. The rewritten call gets the original
// file:line prepended as its first argument.
var testCallRewrites = map[string]string{
	"Errorf":  "graalgoErrorf",
	"Error":   "graalgoError",
	"Fatalf":  "graalgoFatalf",
	"Fatal":   "graalgoFatal",
	"Logf":    "graalgoLogf",
	"Log":     "graalgoLog",
	"Skip":    "graalgoSkip",
	"Skipf":   "graalgoSkipf",
	"SkipNow": "graalgoSkipNow",
}

// isGraalgoTParam reports whether a field declares *graalgoT.
func isGraalgoTParam(field *ast.Field) bool {
	star, ok := field.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "graalgoT"
}

// graalgoTParamNames returns the names of *graalgoT params in a field list.
func graalgoTParamNames(fl *ast.FieldList) []string {
	var names []string
	if fl == nil {
		return names
	}
	for _, field := range fl.List {
		if !isGraalgoTParam(field) {
			continue
		}
		for _, n := range field.Names {
			names = append(names, n.Name)
		}
	}
	return names
}

// rewriteTestCalls rewrites t.Errorf-style diagnostic calls on *graalgoT
// parameters to the shim's position-capturing variants, prepending the
// call's original file:line. Runs after rewriteParam, so all testing.T
// params are *graalgoT. Nested function literals get their own scope: a
// shadowing non-graalgoT param disables rewriting for that name within
// the literal, while a nested *graalgoT param enables it.
func rewriteTestCalls(fn *ast.FuncDecl, fset *token.FileSet) {
	params := make(map[string]bool)
	for _, n := range graalgoTParamNames(fn.Type.Params) {
		params[n] = true
	}
	var walk func(n ast.Node, params, shadowed map[string]bool)
	walk = func(n ast.Node, params, shadowed map[string]bool) {
		ast.Inspect(n, func(nd ast.Node) bool {
			if fl, ok := nd.(*ast.FuncLit); ok {
				innerParams := make(map[string]bool, len(params))
				for k, v := range params {
					innerParams[k] = v
				}
				innerShadowed := make(map[string]bool, len(shadowed))
				for k, v := range shadowed {
					innerShadowed[k] = v
				}
				if fl.Type.Params != nil {
					for _, field := range fl.Type.Params.List {
						if isGraalgoTParam(field) {
							for _, nm := range field.Names {
								innerParams[nm.Name] = true
								delete(innerShadowed, nm.Name)
							}
						} else {
							for _, nm := range field.Names {
								innerShadowed[nm.Name] = true
							}
						}
					}
				}
				walk(fl.Body, innerParams, innerShadowed)
				return false
			}
			if call, ok := nd.(*ast.CallExpr); ok {
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || !params[id.Name] || shadowed[id.Name] {
					return true
				}
				rw, ok := testCallRewrites[sel.Sel.Name]
				if !ok {
					return true
				}
				pos := fset.Position(call.Pos())
				fl := filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
				lit := &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(fl)}
				call.Args = append([]ast.Expr{lit}, call.Args...)
				sel.Sel.Name = rw
			}
			return true
		})
	}
	walk(fn.Body, params, make(map[string]bool))
}

// synthesizeTestDriver builds a single-file Go program that runs the
// selected test functions. It extracts the TestXxx function declarations
// from the test file (rewritten to take *graalgoT) plus the package's
// non-test sources.
// synthesizeTestDriver builds a single-file Go program that runs the
// selected test functions of a package. It extracts the TestXxx function
// declarations from the test files (rewritten to take *graalgoT) plus the
// package's non-test sources.
//
// One driver per package (not per file) so the output matches `go test`:
// a single PASS/FAIL summary and per-test === RUN / --- PASS lines in
// file order. Verbosity is baked into the driver at synthesis time.
func synthesizeTestDriver(dir string, ftList []fileTests, srcFiles []string, verbose bool) (string, error) {
	// Import collection preserves every distinct (name, path) form: a
	// package can be imported plain in one test file and dot-imported
	// in another (unicode does exactly this), and collapsing to one
	// form would leave the other's references undefined. The backend
	// resolves imports via its own import machinery (the same path
	// `graalgo run` uses for packages), so we preserve them instead of
	// stripping — stripping breaks references like hash.Hash.
	type importSpec struct {
		name string // "" for default, "." / "_" / explicit otherwise
		path string
	}
	var importList []importSpec
	seenImport := make(map[importSpec]bool)
	addImport := func(name, path string) {
		s := importSpec{name, path}
		if !seenImport[s] {
			seenImport[s] = true
			importList = append(importList, s)
		}
	}

	// fileBindings records, for each parsed file whose declarations may
	// be inlined, the file's own import bindings: local qualifier name
	// -> import spec. The local name is the explicit name when the
	// import declares one, otherwise the imported package's DECLARED
	// name (resolveImportPkgName) — the identifier that file's own code
	// actually uses. Resolving qualifier references through the
	// importing file's bindings (the same rule Go name resolution uses
	// per file) is what makes import pruning precise: two distinct
	// imports that happen to share a local name — e.g. "internal/strconv"
	// and "strconv" (PKG-021) — are told apart instead of being
	// conflated by qualifier name.
	fileBindings := make(map[*ast.File]map[string]importSpec)
	bindingsFor := func(f *ast.File, name, path string) {
		if name == "." || name == "_" {
			return // dot/blank imports have no qualifier form
		}
		local := name
		if local == "" {
			local = resolveImportPkgName(dir, path)
		}
		m := fileBindings[f]
		if m == nil {
			m = make(map[string]importSpec)
			fileBindings[f] = m
		}
		m[local] = importSpec{name, path}
	}

	// usedImports records the import specs actually referenced by
	// inlined declarations, resolved per-file through fileBindings: a
	// qualifier counts as a use of the spec its own file binds it to.
	// The old name-keyed matching conflated distinct imports sharing a
	// local name and kept imports no inlined declaration references —
	// e.g. the plain "strconv" import collected from strconv's
	// example_test.go (whose Example functions are never inlined) was
	// kept because inlined sources reference the *different* import
	// "internal/strconv" through the same qualifier, producing
	// "strconv redeclared in this block" (PKG-021).
	usedImports := make(map[importSpec]bool)

	// inlineSrc reports whether the package's own sources are inlined
	// into the driver. It is set after the test files are parsed (their
	// package clauses decide), and gates both import collection and
	// source parsing below.
	inlineSrc := true
	collectImports := func(f *ast.File) {
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, "\"")
			// The testing import is rewritten to the graalgoT shim;
			// keeping it would be an unused import.
			if path == "testing" {
				continue
			}
			name := ""
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if inlineSrc && (name == "." || name == "_") && isSelfImport(dir, path) {
				// The driver's body inlines the package under test's
				// own declarations at top level, so importing the
				// package itself again is wrong: a dot self-import
				// redeclares every inlined name (PKG-002), and a blank
				// one is redundant (the inlined inits already run).
				// Plain or explicitly-named self-imports are kept:
				// external test files may reference the package's
				// exported API through the qualifier.
				continue
			}
			addImport(name, path)
			bindingsFor(f, name, path)
		}
	}

	type srcUnit struct {
		path string
		f    *ast.File
		fset *token.FileSet
	}

	// Parse each test file with its own FileSet (positions feed the
	// file:line rewriting in rewriteTestCalls). Test files are parsed
	// before the package's own sources: their package clauses decide
	// whether the sources are inlined at all (see inlineSrc below).
	type testUnit struct {
		srcUnit
		tests    map[string]bool
		ordered  []string
		examples []discoveredExample
	}
	var testUnits []testUnit
	var testPkgNames []string
	selectedFiles := make(map[string]bool)
	for _, ft := range ftList {
		selectedFiles[ft.file] = true
		fset := token.NewFileSet()
		tf, err := parser.ParseFile(fset, ft.file, nil, 0)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", ft.file, err)
		}
		testPkgNames = append(testPkgNames, tf.Name.Name)
		testSet := make(map[string]bool)
		for _, t := range ft.tests {
			testSet[t] = true
		}
		testUnits = append(testUnits, testUnit{srcUnit{ft.file, tf, fset}, testSet, ft.tests, ft.examples})
	}

	// Sibling test files (not selected): every non-entry-point
	// declaration is inlined as helpers. Like runnableTests in test.go,
	// only files satisfying the current platform's build constraints
	// are considered — the raw glob alone would inline build-tag-gated
	// siblings (e.g. math's export_s390x_test.go, constrained to s390x
	// by its filename suffix) whose helpers reference build-excluded
	// sources (hasVX from arith_s390x.go) and fail to compile on this
	// platform (PKG-037).
	var sibUnits []srcUnit
	buildable := make(map[string]bool)
	for _, f := range buildableGoFiles(dir) {
		buildable[f] = true
	}
	siblings, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	for _, sib := range siblings {
		if selectedFiles[sib] || !buildable[sib] {
			continue
		}
		sibSet := token.NewFileSet()
		sibAST, err := parser.ParseFile(sibSet, sib, nil, 0)
		if err != nil {
			// A dropped sibling could hold init functions or
			// package-level variables, so a parse failure here
			// must fail synthesis naming the file (P1-T6),
			// never silently change the package under test.
			return "", fmt.Errorf("parse %s: %w", sib, err)
		}
		testPkgNames = append(testPkgNames, sibAST.Name.Name)
		sibUnits = append(sibUnits, srcUnit{sib, sibAST, sibSet})
	}

	// The package's own sources are inlined only when some test file
	// needs white-box access — i.e. is declared in the package itself.
	// Otherwise the tests exercise the real imported package: nothing
	// is inlined, so a dot-import of the package under test cannot
	// redeclare anything (PKG-002), and the package's types keep their
	// identity where other packages' APIs mention them (e.g. strings
	// functions taking unicode.SpecialCase). When the declared package
	// name cannot be determined, inline conservatively. The package
	// name is also kept for phase B, which partitions gathered
	// declarations into the package under test and its external test
	// package.
	pkgName, pkgNameErr := packageNameInDir(dir)
	if pkgNameErr == nil {
		inlineSrc = false
		for _, n := range testPkgNames {
			if n == pkgName {
				inlineSrc = true
				break
			}
		}
	}

	for _, tu := range testUnits {
		collectImports(tu.f)
	}
	for _, u := range sibUnits {
		collectImports(u.f)
	}

	var units []srcUnit
	if inlineSrc {
		for _, sf := range srcFiles {
			sfset := token.NewFileSet()
			sfAST, err := parser.ParseFile(sfset, sf, nil, 0)
			if err != nil {
				// As for siblings above: silently dropping a
				// package source file changes behaviour, so a
				// parse failure fails synthesis naming the file.
				return "", fmt.Errorf("parse %s: %w", sf, err)
			}
			collectImports(sfAST)
			units = append(units, srcUnit{sf, sfAST, sfset})
		}
	}

	// The generated main uses time and os directly; the shim's
	// Context method uses context.
	addImport("", "time")
	addImport("", "os")
	addImport("", "context")
	usedImports[importSpec{"", "time"}] = true
	usedImports[importSpec{"", "os"}] = true
	usedImports[importSpec{"", "context"}] = true

	// Example wrappers are emitted after the gathered declarations,
	// one per selected example in ftList order, named
	// graalgoExample0, graalgoExample1, ... so they can never collide
	// with an example's own name. They are harness bindings: seed the
	// phase-B protected/taken sets with them below.
	var exampleWrappers []string // wrapper name, in ftList order
	var wrapperExample []discoveredExample
	for _, tu := range testUnits {
		for _, ex := range tu.examples {
			exampleWrappers = append(exampleWrappers, fmt.Sprintf("graalgoExample%d", len(exampleWrappers)))
			wrapperExample = append(wrapperExample, ex)
		}
	}

	// declNames returns the identifiers a top-level declaration binds
	// in the package block: plain function names (methods live in their
	// receiver's namespace; repeated init functions are legal and never
	// collide), type names, and var/const names.
	declNames := func(d ast.Decl) []string {
		var names []string
		switch t := d.(type) {
		case *ast.FuncDecl:
			if t.Recv == nil && t.Name.Name != "init" {
				names = append(names, t.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range t.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
				}
			}
		}
		return names
	}

	// A gatheredDecl is one top-level declaration selected for inlining,
	// with everything phases B/C need: the import bindings of the file
	// it came from, the qualifier->spec references it makes (resolved
	// through those bindings), whether it belongs to the external test
	// package, and the package-block names it binds.
	type gatheredDecl struct {
		decl     ast.Decl
		fset     *token.FileSet
		bindings map[string]importSpec
		quals    map[string]importSpec
		ext      bool
		names    []string
	}
	type bodyItem struct {
		comment string // section marker when g == nil
		g       *gatheredDecl
	}
	var items []bodyItem
	gatherDecl := func(f *ast.File, fset *token.FileSet, decl ast.Decl) {
		bindings := fileBindings[f]
		quals := make(map[string]importSpec)
		if len(bindings) > 0 {
			ast.Inspect(decl, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok {
						if spec, ok := bindings[id.Name]; ok {
							quals[id.Name] = spec
							usedImports[spec] = true
						}
					}
				}
				return true
			})
		}
		ext := pkgNameErr == nil && f.Name.Name != pkgName
		items = append(items, bodyItem{g: &gatheredDecl{decl, fset, bindings, quals, ext, declNames(decl)}})
	}

	// Phase A: gather the declarations to inline (no printing yet).
	// Reference recording happens here so phase B can prune and
	// disambiguate imports, and rename colliding external-test-package
	// declarations, before anything is printed.

	// Inline non-test source files (package clause stripped): funcs plus
	// type/var/const declarations. Skip imports (emitted separately) and
	// the main function (we generate our own). Skipped entirely when no
	// test file needs white-box access (see inlineSrc above).
	for _, u := range units {
		items = append(items, bodyItem{comment: fmt.Sprintf("\n// --- from %s ---\n", filepath.Base(u.path))})
		for _, decl := range u.f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.Name == "main" && d.Recv == nil {
					continue
				}
				gatherDecl(u.f, u.fset, d)
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				gatherDecl(u.f, u.fset, d)
			}
		}
	}

	// TestMain tracking for the generated main: whether any inlined
	// TestMain came from the package under test itself (white-box) or
	// only from its external test package (which phase B may rename).
	// gc rejects a package with both an internal and an external
	// TestMain ("multiple definitions of TestMain", setup failed).
	// The driver used to hide that case: phase B renamed the
	// external TestMain away and the generated main silently ran only
	// the white-box one, so the combination is rejected at synthesis
	// time below, with gc's message. (Two TestMains on the same side
	// still redeclare at driver compile time, as under gc.)
	hasTestMain := false
	testMainWhite := false
	testMainExt := false
	// inlineTestMain inlines a TestMain declaration rewritten to take
	// *graalgoM, and records which side it came from. TestMain bodies
	// may call m.Run() but nothing else testing-specific: the testing
	// import is dropped, so other testing package references fail
	// loudly at driver compile time rather than silently.
	inlineTestMain := func(f *ast.File, fset *token.FileSet, d *ast.FuncDecl) {
		rewriteTestMainParam(d)
		gatherDecl(f, fset, d)
		hasTestMain = true
		if pkgNameErr == nil && f.Name.Name != pkgName {
			testMainExt = true
		} else {
			testMainWhite = true
		}
	}

	// The test files: selected TestXxx functions (rewritten to *graalgoT
	// with position-capturing diagnostic calls) plus each file's own
	// non-test helpers and declarations. Selected ExampleXxx functions
	// are inlined too, for the example wrappers emitted after phase C;
	// a TestMain in any selected file is inlined for the TestMain path
	// of the generated main (never as a test).
	for _, tu := range testUnits {
		items = append(items, bodyItem{comment: fmt.Sprintf("\n// --- tests from %s ---\n", filepath.Base(tu.path))})
		exampleSet := make(map[string]bool)
		for _, ex := range tu.examples {
			exampleSet[ex.name] = true
		}
		for _, decl := range tu.f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				n := d.Name.Name
				if d.Recv == nil && n == "TestMain" {
					inlineTestMain(tu.f, tu.fset, d)
					continue
				}
				isTest := tu.tests[n]
				// Skip other entry points (Test/Benchmark/Fuzz/Example).
				// Methods (Recv != nil) are always inlined — tests use
				// them (e.g. badRand.Read implementing io.Reader).
				isEntry := strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "Benchmark") ||
					strings.HasPrefix(n, "Fuzz") || strings.HasPrefix(n, "Example")
				if !isTest && isEntry && !exampleSet[n] {
					continue
				}
				rewriteParam(d)
				// Helpers called from TestMain may take *testing.M
				// (pointer or bare value); rewrite those too, not
				// just TestMain's own parameter — otherwise the
				// helper's testing.M reference survives into a
				// driver whose testing import is dropped.
				rewriteTestMainParam(d)
				rewriteTestCalls(d, tu.fset)
				gatherDecl(tu.f, tu.fset, d)
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				gatherDecl(tu.f, tu.fset, d)
			}
		}
	}

	// Sibling test files: every non-entry-point declaration (helpers).
	// A TestMain in a sibling file is still inlined (for the TestMain
	// path): runPackage normally promotes the TestMain file to a
	// selected file, but synthesis must not silently drop TestMain
	// when called directly with a ftList that omits it.
	for _, u := range sibUnits {
		items = append(items, bodyItem{comment: fmt.Sprintf("\n// --- helpers from %s ---\n", filepath.Base(u.path))})
		for _, decl := range u.f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "TestMain" {
					inlineTestMain(u.f, u.fset, d)
					continue
				}
				if d.Recv == nil {
					n := d.Name.Name
					if strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "Benchmark") ||
						strings.HasPrefix(n, "Fuzz") || strings.HasPrefix(n, "Example") {
						continue
					}
				}
				rewriteParam(d)
				// As above: helpers called from TestMain may take
				// *testing.M; rewrite them, not just TestMain.
				rewriteTestMainParam(d)
				rewriteTestCalls(d, u.fset)
				gatherDecl(u.f, u.fset, d)
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				gatherDecl(u.f, u.fset, d)
			}
		}
	}

	// A white-box TestMain plus an external TestMain is rejected by
	// gc (see the tracking comment above); reject it here rather
	// than emitting a driver that silently runs only one of them.
	if testMainWhite && testMainExt {
		return "", fmt.Errorf("multiple definitions of TestMain")
	}

	// Phase B (imports): prune unreferenced imports and assign
	// collision-free local names.
	//
	// Prune imports not referenced by the inlined declarations. Helpers
	// from sibling files pull in imports this driver's tests may not use
	// — Go rejects unused imports. A reference is a real package
	// qualifier observed in an inlined declaration resolved through the
	// declaration's own file import bindings (see fileBindings), not a
	// substring: mentions in comments or string literals do not count.
	// The local package name comes from the import spec when explicit;
	// otherwise it is the package's DECLARED name, resolved from its
	// source directory — not the last path element. Those differ for
	// parent-module imports with a major-version suffix
	// (github.com/caddyserver/caddy/v2 declares package caddy), for
	// stdlib v2 paths (math/rand/v2 declares package rand), and for
	// third-party renames (gopkg.in/yaml.v2 declares package yaml).
	// Guessing from the path prunes the import and the driver fails with
	// "undefined: <name>".
	//
	// Emission then gives every kept import a unique local name. Two
	// kept imports can bind the same local name — distinct paths whose
	// declared package names coincide, or an explicit name colliding
	// with another import's name (PKG-021: "internal/strconv" and
	// "strconv"). Emitting both would redeclare the name in the import
	// block. The first (in path,name order) keeps the natural name; the
	// rest are explicitly aliased to name2, name3, ... and every
	// qualifier reference through the renamed import's file bindings is
	// rewritten to the alias before printing, so emission can never
	// redeclare.
	//
	// The uniqueness scope is the whole driver file, not just the
	// import block: a kept import can also collide with a top-level
	// declaration inlined into the driver. When the package's own
	// sources are inlined (inlineSrc), a plain self-import is kept for
	// the external test files' qualified references — but if the
	// package declares a top-level name equal to its own import name
	// (fmt's `type fmt struct`), the import and the inlined
	// declaration redeclare (PKG-075: "fmt already declared through
	// import of package fmt"). Seeding the taken set with the names the
	// driver will declare (the inlined white-box declarations and the
	// harness bindings) makes the aliasing cover this case too, and the
	// existing qualifier-rewriting pass retargets the references.
	driverDeclNames := make(map[string]bool)
	for _, it := range items {
		if it.g == nil || it.g.ext {
			continue
		}
		for _, n := range it.g.names {
			driverDeclNames[n] = true
		}
	}
	for _, n := range []string{"main", "graalgoVerbose", "graalgoT", "joinArgs", "sprintf", "toString", "itoa", "fmtDur2", "twoDigits", "graalgoM", "graalgoRunTests", "graalgoExampleLinesEqual"} {
		driverDeclNames[n] = true
	}
	for _, n := range exampleWrappers {
		driverDeclNames[n] = true
	}
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	emitName := make(map[importSpec]string)
	if len(importList) > 0 {
		ordered := make([]importSpec, len(importList))
		copy(ordered, importList)
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].path != ordered[j].path {
				return ordered[i].path < ordered[j].path
			}
			return ordered[i].name < ordered[j].name
		})
		var kept []importSpec
		for _, s := range ordered {
			if s.name == "_" || s.name == "." {
				// Side-effect and dot imports are never referenced by
				// package name, but dropping them changes semantics.
				kept = append(kept, s)
				continue
			}
			if usedImports[s] {
				kept = append(kept, s)
			}
		}
		taken := make(map[string]bool)
		for n := range driverDeclNames {
			taken[n] = true
		}
		for _, s := range kept {
			if s.name == "." || s.name == "_" {
				emitName[s] = s.name
				continue
			}
			base := s.name
			if base == "" {
				base = resolveImportPkgName(dir, s.path)
			}
			name := base
			for i := 2; taken[name]; i++ {
				name = base + strconv.Itoa(i)
			}
			taken[name] = true
			emitName[s] = name
		}
		if len(kept) > 0 {
			sb.WriteString("import (\n")
			for _, s := range kept {
				name := emitName[s]
				switch {
				case s.name == "." || s.name == "_":
					sb.WriteString(fmt.Sprintf("\t%s %q\n", s.name, s.path))
				case s.name == "":
					if name == resolveImportPkgName(dir, s.path) {
						sb.WriteString(fmt.Sprintf("\t%q\n", s.path))
					} else {
						sb.WriteString(fmt.Sprintf("\t%s %q\n", name, s.path))
					}
				default:
					sb.WriteString(fmt.Sprintf("\t%s %q\n", name, s.path))
				}
			}
			sb.WriteString(")\n")
		}
	}

	// Phase B (declarations): rename external-test-package declarations
	// that collide with the package under test's.
	//
	// Under gc the package under test and its external test package are
	// separate packages; the driver's single-file flattening merges
	// them into one scope, so a top-level name bound in both redeclares
	// (PKG-025: math's func acos vs the external test's var acos,
	// math/bits' var overflowError vs const overflowError; PKG-030:
	// bufio's const minReadBufferSize in both packages). The external
	// test package yields: its colliding declarations are renamed to a
	// unique name2/name3/... suffix and every bare-identifier reference
	// resolving to them — within the external package's own contributed
	// declarations — is rewritten (phase C). Collisions within one side
	// are genuine and left to fail loudly, exactly as `go test` would
	// reject them. No per-file exception is needed for dot-imports: an
	// external file cannot both dot-import the package under test and
	// declare the same exported name (gc rejects that), and a
	// dot-import only brings exported names, so a colliding name's bare
	// references in external declarations always mean the external
	// package's own declaration.
	whiteNames := make(map[string]bool)
	extNames := make(map[string]bool)
	for _, it := range items {
		if it.g == nil {
			continue
		}
		for _, n := range it.g.names {
			if it.g.ext {
				extNames[n] = true
			} else {
				whiteNames[n] = true
			}
		}
	}
	// Names the external test package must yield to: the package under
	// test's declarations and the driver's own harness bindings (the
	// generated main and the graalgoT shim).
	protected := make(map[string]bool)
	for n := range whiteNames {
		protected[n] = true
	}
	for _, n := range []string{"main", "graalgoVerbose", "graalgoT", "joinArgs", "sprintf", "toString", "itoa", "fmtDur2", "twoDigits", "graalgoM", "graalgoRunTests", "graalgoExampleLinesEqual"} {
		protected[n] = true
	}
	for _, n := range exampleWrappers {
		protected[n] = true
	}
	takenNames := make(map[string]bool)
	for n := range protected {
		takenNames[n] = true
	}
	for n := range extNames {
		takenNames[n] = true
	}
	for _, n := range emitName {
		takenNames[n] = true
	}
	eRenames := make(map[string]string)
	renamed := make(map[string]bool)
	for _, it := range items {
		g := it.g
		if g == nil || !g.ext {
			continue
		}
		for _, n := range g.names {
			if !protected[n] || renamed[n] {
				continue
			}
			renamed[n] = true
			newName := n
			for i := 2; takenNames[newName]; i++ {
				newName = n + strconv.Itoa(i)
			}
			takenNames[newName] = true
			eRenames[n] = newName
		}
	}

	// renameQualifiers rewrites the qualifier references of one gathered
	// declaration to the imports' final emission names. Only qualifiers
	// whose import was aliased (emission name != the file's local name)
	// are touched; everything else prints unchanged.
	renameQualifiers := func(g *gatheredDecl) {
		if len(g.quals) == 0 {
			return
		}
		renames := make(map[string]string)
		for q, spec := range g.quals {
			if n, ok := emitName[spec]; ok && n != q {
				renames[q] = n
			}
		}
		if len(renames) == 0 {
			return
		}
		ast.Inspect(g.decl, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					if nn, ok := renames[id.Name]; ok {
						id.Name = nn
					}
				}
			}
			return true
		})
	}

	// rewriteExtRefs renames an external-test-package declaration's own
	// bound names per eRenames and rewrites every bare-identifier
	// reference resolving to them. The walk is scope-aware: parameters,
	// results, and local declarations shadow the package-level name
	// within their scope and are left alone, exactly as Go resolution
	// would. Positions that can never denote the package-level
	// declaration are skipped: selector operands (field/method
	// namespace), struct field and method declarations, and labels. A
	// selector on the file's own import name is a package qualifier
	// owned by the import-alias pass, never the external declaration.
	// Composite-literal keys are rewritten: a key that is really a
	// field name then fails loudly ("unknown field") instead of
	// silently rebinding to the package under test's declaration.
	rewriteExtRefs := func(g *gatheredDecl) {
		if len(eRenames) == 0 {
			return
		}
		qualNames := make(map[string]bool)
		for q := range g.bindings {
			qualNames[q] = true
		}
		for _, n := range emitName {
			qualNames[n] = true
		}
		renameRef := func(id *ast.Ident, shadowed map[string]bool) {
			if id == nil {
				return
			}
			newName, ok := eRenames[id.Name]
			if !ok || shadowed[id.Name] {
				return
			}
			id.Name = newName
		}
		extend := func(shadowed map[string]bool, names []string) map[string]bool {
			if len(names) == 0 {
				return shadowed
			}
			m := make(map[string]bool, len(shadowed)+len(names))
			for k, v := range shadowed {
				m[k] = v
			}
			for _, n := range names {
				if n != "" && n != "_" {
					m[n] = true
				}
			}
			return m
		}
		identNames := func(ids []*ast.Ident) []string {
			var out []string
			for _, id := range ids {
				out = append(out, id.Name)
			}
			return out
		}
		fieldNames := func(fl *ast.FieldList) []string {
			var names []string
			if fl == nil {
				return names
			}
			for _, f := range fl.List {
				names = append(names, identNames(f.Names)...)
			}
			return names
		}
		var walkExpr func(e ast.Expr, shadowed map[string]bool)
		var walkStmt func(s ast.Stmt, shadowed map[string]bool) map[string]bool
		// walkFieldTypes walks the types of a field list; the field
		// names themselves are declarations in another namespace and
		// are never references (embedded fields have no names: their
		// type IS the reference).
		walkFieldTypes := func(fl *ast.FieldList, shadowed map[string]bool) {
			if fl == nil {
				return
			}
			for _, f := range fl.List {
				walkExpr(f.Type, shadowed)
			}
		}
		walkExpr = func(e ast.Expr, shadowed map[string]bool) {
			switch x := e.(type) {
			case nil:
			case *ast.Ident:
				renameRef(x, shadowed)
			case *ast.FuncLit:
				sc := extend(shadowed, fieldNames(x.Type.Params))
				sc = extend(sc, fieldNames(x.Type.Results))
				sc = extend(sc, fieldNames(x.Type.TypeParams))
				walkFieldTypes(x.Type.Params, shadowed)
				walkFieldTypes(x.Type.Results, shadowed)
				walkFieldTypes(x.Type.TypeParams, shadowed)
				if x.Body != nil {
					walkStmt(x.Body, sc)
				}
			case *ast.CompositeLit:
				walkExpr(x.Type, shadowed)
				for _, elt := range x.Elts {
					walkExpr(elt, shadowed)
				}
			case *ast.ParenExpr:
				walkExpr(x.X, shadowed)
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok {
					if !qualNames[id.Name] {
						renameRef(id, shadowed)
					}
				} else {
					walkExpr(x.X, shadowed)
				}
				// Sel is a field/method name: never a package reference.
			case *ast.IndexExpr:
				walkExpr(x.X, shadowed)
				walkExpr(x.Index, shadowed)
			case *ast.IndexListExpr:
				walkExpr(x.X, shadowed)
				for _, ix := range x.Indices {
					walkExpr(ix, shadowed)
				}
			case *ast.SliceExpr:
				walkExpr(x.X, shadowed)
				walkExpr(x.Low, shadowed)
				walkExpr(x.High, shadowed)
				walkExpr(x.Max, shadowed)
			case *ast.TypeAssertExpr:
				walkExpr(x.X, shadowed)
				walkExpr(x.Type, shadowed)
			case *ast.CallExpr:
				walkExpr(x.Fun, shadowed)
				for _, a := range x.Args {
					walkExpr(a, shadowed)
				}
			case *ast.StarExpr:
				walkExpr(x.X, shadowed)
			case *ast.UnaryExpr:
				walkExpr(x.X, shadowed)
			case *ast.BinaryExpr:
				walkExpr(x.X, shadowed)
				walkExpr(x.Y, shadowed)
			case *ast.KeyValueExpr:
				walkExpr(x.Key, shadowed)
				walkExpr(x.Value, shadowed)
			case *ast.ArrayType:
				walkExpr(x.Len, shadowed)
				walkExpr(x.Elt, shadowed)
			case *ast.StructType:
				walkFieldTypes(x.Fields, shadowed)
			case *ast.FuncType:
				walkFieldTypes(x.Params, shadowed)
				walkFieldTypes(x.Results, shadowed)
				walkFieldTypes(x.TypeParams, shadowed)
			case *ast.InterfaceType:
				walkFieldTypes(x.Methods, shadowed)
			case *ast.MapType:
				walkExpr(x.Key, shadowed)
				walkExpr(x.Value, shadowed)
			case *ast.ChanType:
				walkExpr(x.Value, shadowed)
			case *ast.Ellipsis:
				walkExpr(x.Elt, shadowed)
			}
		}
		walkStmt = func(s ast.Stmt, shadowed map[string]bool) map[string]bool {
			switch x := s.(type) {
			case nil:
				return shadowed
			case *ast.DeclStmt:
				if gd, ok := x.Decl.(*ast.GenDecl); ok {
					for _, spec := range gd.Specs {
						switch sp := spec.(type) {
						case *ast.ValueSpec:
							// The initializer sees the outer scope.
							walkExpr(sp.Type, shadowed)
							for _, v := range sp.Values {
								walkExpr(v, shadowed)
							}
							shadowed = extend(shadowed, identNames(sp.Names))
						case *ast.TypeSpec:
							sc := extend(shadowed, fieldNames(sp.TypeParams))
							walkFieldTypes(sp.TypeParams, shadowed)
							walkExpr(sp.Type, sc)
							shadowed = extend(shadowed, []string{sp.Name.Name})
						}
					}
				}
				return shadowed
			case *ast.ExprStmt:
				walkExpr(x.X, shadowed)
			case *ast.SendStmt:
				walkExpr(x.Chan, shadowed)
				walkExpr(x.Value, shadowed)
			case *ast.IncDecStmt:
				walkExpr(x.X, shadowed)
			case *ast.AssignStmt:
				if x.Tok == token.DEFINE {
					for _, r := range x.Rhs {
						walkExpr(r, shadowed)
					}
					var names []string
					for _, l := range x.Lhs {
						if id, ok := l.(*ast.Ident); ok {
							names = append(names, id.Name)
						} else {
							walkExpr(l, shadowed)
						}
					}
					return extend(shadowed, names)
				}
				for _, l := range x.Lhs {
					walkExpr(l, shadowed)
				}
				for _, r := range x.Rhs {
					walkExpr(r, shadowed)
				}
			case *ast.GoStmt:
				walkExpr(x.Call, shadowed)
			case *ast.DeferStmt:
				walkExpr(x.Call, shadowed)
			case *ast.ReturnStmt:
				for _, r := range x.Results {
					walkExpr(r, shadowed)
				}
			case *ast.BranchStmt:
				// Label target: label namespace, never a package ref.
			case *ast.IfStmt:
				sc := shadowed
				if x.Init != nil {
					sc = walkStmt(x.Init, sc)
				}
				walkExpr(x.Cond, sc)
				walkStmt(x.Body, sc)
				if x.Else != nil {
					walkStmt(x.Else, sc)
				}
			case *ast.CaseClause:
				for _, e := range x.List {
					walkExpr(e, shadowed)
				}
				sc := shadowed
				for _, b := range x.Body {
					sc = walkStmt(b, sc)
				}
			case *ast.SwitchStmt:
				sc := shadowed
				if x.Init != nil {
					sc = walkStmt(x.Init, sc)
				}
				walkExpr(x.Tag, sc)
				walkStmt(x.Body, sc)
			case *ast.TypeSwitchStmt:
				sc := shadowed
				if x.Init != nil {
					sc = walkStmt(x.Init, sc)
				}
				var guard []string
				if as, ok := x.Assign.(*ast.AssignStmt); ok {
					for _, l := range as.Lhs {
						if id, ok := l.(*ast.Ident); ok {
							guard = append(guard, id.Name)
						}
					}
					if len(as.Rhs) == 1 {
						if ta, ok := as.Rhs[0].(*ast.TypeAssertExpr); ok {
							walkExpr(ta.X, sc)
						}
					}
				} else if es, ok := x.Assign.(*ast.ExprStmt); ok {
					walkExpr(es.X, sc)
				}
				walkStmt(x.Body, extend(sc, guard))
			case *ast.SelectStmt:
				walkStmt(x.Body, shadowed)
			case *ast.CommClause:
				sc := shadowed
				if x.Comm != nil {
					sc = walkStmt(x.Comm, sc)
				}
				for _, b := range x.Body {
					sc = walkStmt(b, sc)
				}
			case *ast.ForStmt:
				sc := shadowed
				if x.Init != nil {
					sc = walkStmt(x.Init, sc)
				}
				walkExpr(x.Cond, sc)
				if x.Post != nil {
					walkStmt(x.Post, sc)
				}
				walkStmt(x.Body, sc)
			case *ast.RangeStmt:
				walkExpr(x.X, shadowed)
				sc := shadowed
				if x.Tok == token.DEFINE {
					var names []string
					for _, kv := range []ast.Expr{x.Key, x.Value} {
						if id, ok := kv.(*ast.Ident); ok {
							names = append(names, id.Name)
						} else {
							walkExpr(kv, shadowed)
						}
					}
					sc = extend(sc, names)
				} else {
					walkExpr(x.Key, shadowed)
					walkExpr(x.Value, shadowed)
				}
				walkStmt(x.Body, sc)
			case *ast.LabeledStmt:
				// The label is in the label namespace; the statement
				// keeps the ambient scope.
				walkStmt(x.Stmt, shadowed)
			case *ast.BlockStmt:
				sc := shadowed
				for _, b := range x.List {
					sc = walkStmt(b, sc)
				}
				// Block scope does not leak.
			}
			return shadowed
		}
		top := make(map[string]bool)
		switch d := g.decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				renameRef(d.Name, top) // the binding itself
			}
			// The receiver's names shadow the body, but its type is
			// resolved in the outer scope.
			walkFieldTypes(d.Recv, top)
			sc := extend(top, fieldNames(d.Recv))
			if d.Type != nil {
				walkFieldTypes(d.Type.Params, sc)
				walkFieldTypes(d.Type.Results, sc)
				walkFieldTypes(d.Type.TypeParams, sc)
				sc = extend(sc, fieldNames(d.Type.Params))
				sc = extend(sc, fieldNames(d.Type.Results))
				sc = extend(sc, fieldNames(d.Type.TypeParams))
			}
			if d.Body != nil {
				walkStmt(d.Body, sc)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						renameRef(n, top) // the bindings themselves
					}
					walkExpr(sp.Type, top)
					for _, v := range sp.Values {
						walkExpr(v, top)
					}
				case *ast.TypeSpec:
					renameRef(sp.Name, top)
					sc := extend(top, fieldNames(sp.TypeParams))
					walkFieldTypes(sp.TypeParams, top)
					walkExpr(sp.Type, sc)
				}
			}
		}
	}

	// Phase C: print the gathered declarations. Qualifier references
	// whose import was aliased are rewritten to the alias; renamed
	// external-test-package declarations (and the references resolving
	// to them) to their new names.
	var body strings.Builder
	for _, it := range items {
		if it.g == nil {
			body.WriteString(it.comment)
			continue
		}
		renameQualifiers(it.g)
		if it.g.ext {
			rewriteExtRefs(it.g)
		}
		printDecl(&body, it.g.fset, it.g.decl)
		body.WriteString("\n")
	}

	// Shim + main runner. Per-test output mirrors `go test -v`:
	// === RUN / --- PASS/FAIL/SKIP with (0.00s) timings, a single
	// PASS/FAIL summary, and exit code 1 (no panic trace) on failure.
	//
	// The runner lives in graalgoRunTests so TestMain can drive it:
	// graalgoM.Run calls it and records the exit code, and main
	// either calls TestMain (when the package declares one) or runs
	// the tests directly. Tests run first, then examples — the order
	// go test's generated main uses.
	body.WriteString(testShimSource(verbose))
	body.WriteString(`
// --- graalgo test main (generated) ---
type graalgoM struct {
	code int
}

func (m *graalgoM) Run() int {
	m.code = graalgoRunTests()
	return m.code
}

func graalgoRunTests() int {
	failed := 0
`)
	for _, tu := range testUnits {
		for _, t := range tu.ordered {
			// If this test file's TestXxx was renamed by phase B (its
			// name collided with the package under test's), call the
			// renamed function — otherwise the driver would silently
			// run the wrong package's test.
			callName := t
			if pkgNameErr == nil && tu.f.Name.Name != pkgName {
				if nn, ok := eRenames[t]; ok {
					callName = nn
				}
			}
			body.WriteString("\t{\n")
			body.WriteString(fmt.Sprintf("\t\ttt := &graalgoT{name: %q}\n", t))
			if verbose {
				body.WriteString(fmt.Sprintf("\t\tprintln(\"=== RUN   %s\")\n", t))
			}
			body.WriteString("\t\tt0 := time.Now()\n")
			body.WriteString("\t\tfunc() {\n")
			body.WriteString("\t\t\tdefer func() {\n")
			body.WriteString("\t\t\t\tif r := recover(); r != nil {\n")
			body.WriteString("\t\t\t\t\ts := toString(r)\n")
			body.WriteString("\t\t\t\t\tif len(s) >= 17 && s[:17] == \"graalgo test skip\" {\n")
			body.WriteString("\t\t\t\t\t\ttt.skipped = true\n")
			body.WriteString("\t\t\t\t\t} else if len(s) >= 18 && s[:18] == \"graalgo test fatal\" {\n")
			body.WriteString("\t\t\t\t\t\ttt.failed = true\n")
			body.WriteString("\t\t\t\t\t} else {\n")
			body.WriteString("\t\t\t\t\t\ttt.failed = true\n")
			body.WriteString("\t\t\t\t\t\ttt.logLine(\"panic: \" + s)\n")
			body.WriteString("\t\t\t\t\t}\n")
			body.WriteString("\t\t\t\t}\n")
			body.WriteString("\t\t\t}()\n")
			body.WriteString(fmt.Sprintf("\t\t\t%s(tt)\n", callName))
			body.WriteString("\t\t}()\n")
			// Cleanups run after the test returns (or panics and is
			// recovered above), before the result line — as in go test.
			body.WriteString("\t\ttt.runCleanups()\n")
			body.WriteString("\t\tdur := \"(\" + fmtDur2(time.Since(t0)) + \")\"\n")
			if verbose {
				body.WriteString("\t\tif tt.skipped {\n")
				body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- SKIP: %s \" + dur)\n", t))
				body.WriteString("\t\t} else if tt.failed {\n")
				body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- FAIL: %s \" + dur)\n", t))
				body.WriteString("\t\t\tfailed++\n")
				body.WriteString("\t\t} else {\n")
				body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- PASS: %s \" + dur)\n", t))
				body.WriteString("\t\t}\n")
				body.WriteString("\t\tfor _, l := range tt.subresults {\n")
				body.WriteString("\t\t\tprintln(l)\n")
				body.WriteString("\t\t}\n")
			} else {
				body.WriteString("\t\tif tt.failed {\n")
				body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- FAIL: %s \" + dur)\n", t))
				body.WriteString("\t\t\tfor _, l := range tt.output {\n")
				body.WriteString("\t\t\t\tprintln(l)\n")
				body.WriteString("\t\t\t}\n")
				body.WriteString("\t\t\tfor _, l := range tt.subresults {\n")
				body.WriteString("\t\t\t\tprintln(l)\n")
				body.WriteString("\t\t\t}\n")
				body.WriteString("\t\t\tfailed++\n")
				body.WriteString("\t\t}\n")
			}
			body.WriteString("\t}\n")
		}
	}
	// Examples run after all tests, each through its wrapper (which
	// captures stdout and compares it against the Output: comment).
	// The display name is the example's own name, as go test prints.
	// The example wrappers are invoked through the same per-test
	// block as tests, so their === RUN / --- PASS/FAIL lines, panic
	// recovery, and cleanup handling are identical. (Wrapper names
	// are positional: exampleWrappers[i] is the i-th selected example
	// in ftList order, matching the wrapper definitions emitted below.)
	{
		wi := 0
		for _, tu := range testUnits {
			for _, ex := range tu.examples {
				wrapper := exampleWrappers[wi]
				wi++
				body.WriteString("\t{\n")
				body.WriteString(fmt.Sprintf("\t\ttt := &graalgoT{name: %q}\n", ex.name))
				if verbose {
					body.WriteString(fmt.Sprintf("\t\tprintln(\"=== RUN   %s\")\n", ex.name))
				}
				body.WriteString("\t\tt0 := time.Now()\n")
				body.WriteString("\t\tfunc() {\n")
				body.WriteString("\t\t\tdefer func() {\n")
				body.WriteString("\t\t\t\tif r := recover(); r != nil {\n")
				body.WriteString("\t\t\t\t\ts := toString(r)\n")
				body.WriteString("\t\t\t\t\tif len(s) >= 17 && s[:17] == \"graalgo test skip\" {\n")
				body.WriteString("\t\t\t\t\t\ttt.skipped = true\n")
				body.WriteString("\t\t\t\t\t} else if len(s) >= 18 && s[:18] == \"graalgo test fatal\" {\n")
				body.WriteString("\t\t\t\t\t\ttt.failed = true\n")
				body.WriteString("\t\t\t\t\t} else {\n")
				body.WriteString("\t\t\t\t\t\ttt.failed = true\n")
				body.WriteString("\t\t\t\t\t\ttt.logLine(\"panic: \" + s)\n")
				body.WriteString("\t\t\t\t\t}\n")
				body.WriteString("\t\t\t\t}\n")
				body.WriteString("\t\t\t}()\n")
				body.WriteString(fmt.Sprintf("\t\t\t%s(tt)\n", wrapper))
				body.WriteString("\t\t}()\n")
				body.WriteString("\t\ttt.runCleanups()\n")
				body.WriteString("\t\tdur := \"(\" + fmtDur2(time.Since(t0)) + \")\"\n")
				if verbose {
					body.WriteString("\t\tif tt.skipped {\n")
					body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- SKIP: %s \" + dur)\n", ex.name))
					body.WriteString("\t\t} else if tt.failed {\n")
					body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- FAIL: %s \" + dur)\n", ex.name))
					body.WriteString("\t\t\tfailed++\n")
					body.WriteString("\t\t} else {\n")
					body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- PASS: %s \" + dur)\n", ex.name))
					body.WriteString("\t\t}\n")
					body.WriteString("\t\tfor _, l := range tt.subresults {\n")
					body.WriteString("\t\t\tprintln(l)\n")
					body.WriteString("\t\t}\n")
				} else {
					body.WriteString("\t\tif tt.failed {\n")
					body.WriteString(fmt.Sprintf("\t\t\tprintln(\"--- FAIL: %s \" + dur)\n", ex.name))
					body.WriteString("\t\t\tfor _, l := range tt.output {\n")
					body.WriteString("\t\t\t\tprintln(l)\n")
					body.WriteString("\t\t\t}\n")
					body.WriteString("\t\t\tfor _, l := range tt.subresults {\n")
					body.WriteString("\t\t\t\tprintln(l)\n")
					body.WriteString("\t\t\t}\n")
					body.WriteString("\t\t\tfailed++\n")
					body.WriteString("\t\t}\n")
				}
				body.WriteString("\t}\n")
			}
		}
	}
	body.WriteString("\tif failed > 0 {\n")
	body.WriteString("\t\tprintln(\"FAIL\")\n")
	body.WriteString("\t\treturn 1\n")
	body.WriteString("\t}\n")
	if verbose {
		body.WriteString("\tprintln(\"PASS\")\n")
	}
	body.WriteString("\treturn 0\n")
	body.WriteString("}\n")

	// Example wrappers: run one example with stdout captured through
	// an os.Pipe, then compare against its Output: comment. An
	// example without an output comment only has to run without
	// panicking. Captured output is compared after dropping a single
	// trailing newline (go test's example runner likewise tolerates
	// the final newline); any other difference fails the example.
	// Note the pipe depends on os.Pipe, whose multi-result desugar
	// the frontend now provides; if the backend ever stops providing
	// it, the wrapper's panic is recovered by the runner above and
	// the example fails loudly instead of silently passing.
	// Two properties of the wrapper shape matter: stdout restoration
	// and the writer-end close happen in a defer, so a panicking
	// example cannot strand later output in the abandoned pipe (the
	// old straight-line restore never ran on panic, leaking the pipe
	// and letting a later large write block on it filling); and a
	// reader goroutine drains the pipe concurrently while the example
	// runs, so output over the 64KB pipe buffer cannot deadlock the
	// writer (the old read-after-return shape blocked forever past
	// 64KB). The reader reports through a buffered (size-1) channel,
	// so on the panic path — where nothing receives — the goroutine
	// still terminates instead of blocking on the send.
	anyUnordered := false
	for _, ex := range wrapperExample {
		if ex.unordered {
			anyUnordered = true
		}
	}
	if anyUnordered {
		body.WriteString(`
func graalgoExampleLinesEqual(a, b string) bool {
	split := func(s string) []string {
		var lines []string
		cur := ""
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				lines = append(lines, cur)
				cur = ""
			} else {
				cur += string(s[i])
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		return lines
	}
	counts := func(lines []string) map[string]int {
		m := make(map[string]int)
		for _, l := range lines {
			m[l]++
		}
		return m
	}
	ca := counts(split(a))
	cb := counts(split(b))
	if len(ca) != len(cb) {
		return false
	}
	for k, v := range ca {
		if cb[k] != v {
			return false
		}
	}
	return true
}
`)
	}
	{
		wi := 0
		for _, tu := range testUnits {
			for _, ex := range tu.examples {
				wrapper := exampleWrappers[wi]
				wi++
				callName := ex.name
				if pkgNameErr == nil && tu.f.Name.Name != pkgName {
					if nn, ok := eRenames[ex.name]; ok {
						callName = nn
					}
				}
				body.WriteString(fmt.Sprintf("\nfunc %s(t *graalgoT) {\n", wrapper))
				body.WriteString("\tr, w, err := os.Pipe()\n")
				body.WriteString("\tif err != nil {\n")
				body.WriteString(fmt.Sprintf("\t\tt.failed = true\n\t\tt.logLine(\"example %s: cannot capture stdout: \" + err.Error())\n\t\treturn\n\t}\n", ex.name))
				body.WriteString("\told := os.Stdout\n")
				body.WriteString("\tos.Stdout = w\n")
				// Deferred restore + writer close: runs on the panic
				// path too, so stdout can never stay redirected into
				// this example's abandoned pipe.
				body.WriteString("\tdefer func() {\n")
				body.WriteString("\t\tos.Stdout = old\n")
				body.WriteString("\t\tw.Close()\n")
				body.WriteString("\t}()\n")
				// Concurrent drain: the reader runs while the example
				// runs, so output larger than the pipe buffer cannot
				// deadlock the writer. The buffered channel lets the
				// reader finish (and exit) even when the example
				// panics and nothing receives its result.
				body.WriteString("\toutCh := make(chan string, 1)\n")
				body.WriteString("\tgo func() {\n")
				body.WriteString("\t\tvar buf [4096]byte\n")
				body.WriteString("\t\tout := \"\"\n")
				body.WriteString("\t\tfor {\n")
				body.WriteString("\t\t\tn, err := r.Read(buf[:])\n")
				body.WriteString("\t\t\tif n > 0 {\n")
				body.WriteString("\t\t\t\tout += string(buf[:n])\n")
				body.WriteString("\t\t\t}\n")
				body.WriteString("\t\t\tif err != nil {\n")
				body.WriteString("\t\t\t\tbreak\n")
				body.WriteString("\t\t\t}\n")
				body.WriteString("\t\t}\n")
				body.WriteString("\t\tr.Close()\n")
				body.WriteString("\t\toutCh <- out\n")
				body.WriteString("\t}()\n")
				body.WriteString(fmt.Sprintf("\t%s()\n", callName))
				// Closing the writer ends the reader's stream; the
				// deferred close above is then a harmless no-op.
				body.WriteString("\tw.Close()\n")
				body.WriteString("\tos.Stdout = old\n")
				body.WriteString("\tout := <-outCh\n")
				if !ex.hasOutput {
					// No Output: comment: the captured output is
					// discarded, but the receive above must still
					// be used or the driver fails to compile.
					body.WriteString("\t_ = out\n")
				}
				if ex.hasOutput {
					body.WriteString(fmt.Sprintf("\twant := %q\n", ex.output))
					body.WriteString("\tgot := out\n")
					body.WriteString("\tif len(got) > 0 && got[len(got)-1] == '\\n' {\n")
					body.WriteString("\t\tgot = got[:len(got)-1]\n")
					body.WriteString("\t}\n")
					if ex.unordered {
						body.WriteString("\tif !graalgoExampleLinesEqual(got, want) {\n")
					} else {
						body.WriteString("\tif got != want {\n")
					}
					body.WriteString("\t\tt.failed = true\n")
					body.WriteString(fmt.Sprintf("\t\tt.logLine(\"example %s: output mismatch\")\n", ex.name))
					body.WriteString("\t\tt.logLine(\"got: \" + got)\n")
					body.WriteString("\t\tt.logLine(\"want: \" + want)\n")
					body.WriteString("\t}\n")
				}
				body.WriteString("}\n")
			}
		}
	}

	// The generated main: TestMain, when present, drives the run and
	// its exit code is propagated even if it returns without calling
	// os.Exit itself (a TestMain that calls os.Exit never reaches
	// the propagation line). Without TestMain, the tests run directly.
	if hasTestMain {
		testMainCall := "TestMain"
		if !testMainWhite && testMainExt {
			if nn, ok := eRenames["TestMain"]; ok {
				testMainCall = nn
			}
		}
		body.WriteString("\nfunc main() {\n")
		body.WriteString("\tm := &graalgoM{}\n")
		body.WriteString(fmt.Sprintf("\t%s(m)\n", testMainCall))
		body.WriteString("\tos.Exit(m.code)\n")
		body.WriteString("}\n")
	} else {
		body.WriteString("\nfunc main() {\n")
		body.WriteString("\tif code := graalgoRunTests(); code != 0 {\n")
		body.WriteString("\t\tos.Exit(code)\n")
		body.WriteString("\t}\n")
		body.WriteString("}\n")
	}

	sb.WriteString(body.String())

	// Write to a temp file.
	tmp, err := os.CreateTemp("", "graalgo-test-*.go")
	if err != nil {
		return "", err
	}
	if _, err := tmp.WriteString(sb.String()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	return tmp.Name(), nil
}

// isSelfImport reports whether importPath denotes the package under
// test (dir) itself. Detection is by resolved source directory, which
// works without module context — e.g. unicode's external test files
// import ". \"unicode\"" while the enclosing module path is
// "std/unicode" — with a textual import-path comparison as backup.
func isSelfImport(dir, importPath string) bool {
	if importPathForDir(dir) == importPath {
		return true
	}
	d, err := importDirForPath(dir, importPath)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absD, err := filepath.Abs(d)
	if err != nil {
		return false
	}
	// Resolve symlinks: GOROOT and dir itself may be reached through
	// different spellings of the same directory.
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absD); err == nil {
		absD = resolved
	}
	return absD == absDir
}

// pathBase returns the last element of an import path.
func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// importPkgNameCache memoizes resolveImportPkgName: the declared name of
// an import path cannot change within one synthesis run.
var importPkgNameCache = map[string]string{}

// resolveImportPkgName returns the declared package name for an import
// path — the identifier the synthesized driver's body uses to reference
// it. Resolution order:
//  1. packages inside the tested module (parent/sibling imports such as
//     github.com/caddyserver/caddy/v2 from within that module),
//  2. standard library via GOROOT,
//  3. third-party modules via the module cache, then GOPATH/src.
//
// Falls back to the last path element (the previous behavior) when the
// source directory cannot be found.
func resolveImportPkgName(dir, importPath string) string {
	if n, ok := importPkgNameCache[importPath]; ok {
		return n
	}
	name := pathBase(importPath)
	if d, err := importDirForPath(dir, importPath); err == nil {
		if pn, err := packageNameInDir(d); err == nil && pn != "" {
			name = pn
		}
	}
	importPkgNameCache[importPath] = name
	return name
}

// moduleForDir walks up from dir to the enclosing go.mod, returning the
// module root directory and the declared module path.
func moduleForDir(dir string) (root, modulePath string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	d := abs
	for {
		if modData, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			if module := parseModulePath(modData); module != "" {
				return d, module
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", ""
}

// importDirForPath maps an import path to its source directory on disk,
// relative to the module containing dir.
func importDirForPath(dir, importPath string) (string, error) {
	// Same module: parent and sibling package imports (e.g. the caddy
	// root package imported by caddyconfig's tests).
	if modRoot, modulePath := moduleForDir(dir); modulePath != "" {
		if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
			rel := strings.TrimPrefix(importPath, modulePath)
			rel = strings.TrimPrefix(rel, "/")
			d := filepath.Join(modRoot, filepath.FromSlash(rel))
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d, nil
			}
		}
	}
	// Standard library.
	if isStdImportPath(importPath) {
		if goroot := goRoot(); goroot != "" {
			d := filepath.Join(goroot, "src", filepath.FromSlash(importPath))
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d, nil
			}
		}
	} else {
		// Third-party: module cache first, then GOPATH/src.
		if mc := goModCacheDir(); mc != "" {
			if d, ok := findInModCache(mc, importPath); ok {
				return d, nil
			}
		}
		for _, gp := range goPathDirs() {
			d := filepath.Join(gp, "src", filepath.FromSlash(importPath))
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d, nil
			}
		}
	}
	return "", fmt.Errorf("no source directory for import %q", importPath)
}

// isStdImportPath reports whether importPath is a standard-library path:
// the first element contains no dot (no hostname).
func isStdImportPath(importPath string) bool {
	first := importPath
	if i := strings.Index(first, "/"); i >= 0 {
		first = first[:i]
	}
	return !strings.Contains(first, ".")
}

// goRoot returns GOROOT for the toolchain building this driver.
func goRoot() string {
	if gr := os.Getenv("GOROOT"); gr != "" {
		return gr
	}
	return runtime.GOROOT()
}

// goModCacheDir returns the module cache directory.
func goModCacheDir() string {
	if mc := os.Getenv("GOMODCACHE"); mc != "" {
		return mc
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		return filepath.Join(strings.Split(gopath, string(os.PathListSeparator))[0], "pkg", "mod")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "go", "pkg", "mod")
	}
	return ""
}

// goPathDirs returns the GOPATH entries.
func goPathDirs() []string {
	if gp := os.Getenv("GOPATH"); gp != "" {
		return strings.Split(gp, string(os.PathListSeparator))
	}
	if h, err := os.UserHomeDir(); err == nil {
		return []string{filepath.Join(h, "go")}
	}
	return nil
}

// findInModCache locates importPath under the module cache, trying
// progressively shorter module prefixes (the cache stores the module at
// <escaped>@<version>, with the package as a subdirectory). Any version
// will do: the declared package name does not vary by version.
func findInModCache(mc, importPath string) (string, bool) {
	escaped := escapeModulePath(importPath)
	elems := strings.Split(escaped, "/")
	for i := len(elems); i >= 1; i-- {
		modEscaped := strings.Join(elems[:i], "/")
		subPath := strings.Join(elems[i:], "/")
		parent := filepath.Join(mc, filepath.Dir(modEscaped))
		base := filepath.Base(modEscaped)
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), base+"@") {
				continue
			}
			d := filepath.Join(parent, e.Name())
			if subPath != "" {
				d = filepath.Join(d, filepath.FromSlash(subPath))
			}
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d, true
			}
		}
	}
	return "", false
}

// escapeModulePath applies the module cache escaping: uppercase letters
// become ! plus the lowercase letter.
func escapeModulePath(p string) string {
	var sb strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			sb.WriteByte('!')
			sb.WriteRune(r - 'A' + 'a')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// packageNameInDir returns the declared package name of the Go package in
// directory d. Resolution goes through go/build so that files excluded
// from the build are never mistaken for the package's own sources: the
// old first-.go-file heuristic picked sort's gen_sort_variants.go —
// package main with //go:build ignore — and reported "main" for import
// "sort", which made the import pruner drop "sort" as unreferenced
// (PKG-003).
func packageNameInDir(d string) (string, error) {
	if pkg, err := build.Default.ImportDir(d, 0); err == nil && pkg.Name != "" {
		return pkg.Name, nil
	}
	// Fallback for directories go/build cannot import: parse the package
	// clause of one source file, preferring a non-test file. An external
	// test file's package foo_test clause means the importable name foo.
	entries, err := os.ReadDir(d)
	if err != nil {
		return "", err
	}
	var fallback string
	fallbackIsTest := true
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		isTest := strings.HasSuffix(name, "_test.go")
		if fallback == "" {
			fallback, fallbackIsTest = name, isTest
		}
		if !isTest {
			fallback, fallbackIsTest = name, false
			break
		}
	}
	if fallback == "" {
		return "", fmt.Errorf("no Go files in %s", d)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(d, fallback), nil, parser.PackageClauseOnly)
	if err != nil {
		return "", err
	}
	name := f.Name.Name
	if fallbackIsTest {
		name = strings.TrimSuffix(name, "_test")
	}
	return name, nil
}

// printDecl prints a single AST declaration to the builder.
func printDecl(sb *strings.Builder, fset *token.FileSet, decl ast.Decl) {
	var buf bytes.Buffer
	printer.Fprint(&buf, fset, decl)
	sb.WriteString(buf.String())
}

// rewriteParam rewrites *testing.T to *graalgoT everywhere in a FuncDecl:
// the parameter list AND any nested function literals (e.g. the closure
// passed to t.Run). Uses an AST walk so no testing.T reference survives.
func rewriteParam(fn *ast.FuncDecl) {
	ast.Inspect(fn, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.StarExpr:
			if sel, ok := t.X.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "testing" && sel.Sel.Name == "T" {
					t.X = &ast.Ident{Name: "graalgoT"}
				}
			}
		case *ast.SelectorExpr:
			if ident, ok := t.X.(*ast.Ident); ok && ident.Name == "testing" && t.Sel.Name == "T" {
				// testing.T as a bare type (not pointer): replace the
				// selector with the graalgoT identifier.
				// (Handled by replacing the parent; see below.)
			}
		}
		return true
	})
	// Second pass: replace bare testing.T selector expressions (non-pointer
	// type references) which ast.Inspect can't swap in place.
	rewriteBareTestingT(fn.Type)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			rewriteBareTestingT(fl.Type)
		}
		return true
	})
}

// rewriteTestMainParam rewrites *testing.M to *graalgoM everywhere in a
// FuncDecl: the parameter list AND any nested function literals. It
// mirrors rewriteParam, for the M type instead of T. It is applied to
// TestMain itself and to every other inlined test/sibling FuncDecl,
// because a helper called from TestMain may take *testing.M. Scope
// limit (same as rewriteParam for T): only FuncDecls are rewritten —
// testing.M references in GenDecls (type aliases, struct fields) are
// left alone and are the caller's responsibility to avoid.
func rewriteTestMainParam(fn *ast.FuncDecl) {
	ast.Inspect(fn, func(n ast.Node) bool {
		if star, ok := n.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "testing" && sel.Sel.Name == "M" {
					star.X = &ast.Ident{Name: "graalgoM"}
				}
			}
		}
		return true
	})
	rewriteBareTestingM(fn.Type)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			rewriteBareTestingM(fl.Type)
		}
		return true
	})
}

// rewriteBareTestingM replaces testing.M (non-pointer) with graalgoM in
// a function type's params and results.
func rewriteBareTestingM(ft *ast.FuncType) {
	if ft == nil {
		return
	}
	rewriteBareTestingMList(ft.Params)
	rewriteBareTestingMList(ft.Results)
}

func rewriteBareTestingMList(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		if sel, ok := f.Type.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "testing" && sel.Sel.Name == "M" {
				f.Type = &ast.Ident{Name: "graalgoM"}
			}
		}
	}
}

// rewriteBareTestingT replaces testing.T (non-pointer) with graalgoT in a
// function type's params and results.
func rewriteBareTestingT(ft *ast.FuncType) {
	if ft == nil {
		return
	}
	rewriteFieldList(ft.Params)
	rewriteFieldList(ft.Results)
}

func rewriteFieldList(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		if sel, ok := f.Type.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "testing" && sel.Sel.Name == "T" {
				f.Type = &ast.Ident{Name: "graalgoT"}
			}
		}
	}
}
