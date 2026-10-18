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
	"unicode"
	"unicode/utf8"
)

// discoverTests returns the names of TestXxx functions in a _test.go file,
// following go test's naming rule: the name is "Test" alone, or "Test"
// followed by a non-lowercase letter, and the function takes exactly one
// parameter.
//
// TestMain is never a test: it is the test binary's entry point. go test
// runs it as main with a *testing.M; the synthesized driver has no
// testing.M shim, so TestMain is excluded here and reported separately
// by detectTestMain (runPackage warns).
func discoverTests(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var tests []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		if !isTestName(name) {
			continue
		}
		// Must take *testing.T (or be a simple TestXxx with one param).
		if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
			continue
		}
		tests = append(tests, name)
	}
	return tests, nil
}

// isTestName reports whether name is a go test function name: "Test"
// alone, or "Test" followed by a non-lowercase letter (mirroring cmd/go's
// isTest — "Test" is ok, "Testfoo" is not). TestMain is the test binary's
// entry point, not a test, and is excluded.
func isTestName(name string) bool {
	if name == "TestMain" {
		return false
	}
	if name == "Test" {
		return true
	}
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name[len("Test"):])
	return !unicode.IsLower(r)
}

// detectTestMain reports whether path declares a TestMain function, the
// test binary's entry point. The synthesized driver cannot honor TestMain
// (no testing.M shim), so runPackage warns and runs the tests without it.
func detectTestMain(path string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false, fmt.Errorf("parse: %w", err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if fn.Name.Name == "TestMain" {
			return true, nil
		}
	}
	return false, nil
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

// graalgoPrintln writes a driver line to stdout. The builtin println
// writes to stderr in graalgo (PrintNode/BuiltinPrint — gc parity for the
// builtin), but go test writes its === RUN / --- PASS / --- FAIL lines to
// stdout, which scripts parse — so the driver must not use the builtin.
func graalgoPrintln(s string) {
	os.Stdout.WriteString(s + "\n")
}

// graalgoFatalPanic and graalgoSkipPanic are the sentinel panic values for
// t.Fatalf/t.Skipf. gc implements Fatalf via runtime.Goexit, which no
// guest recover() can intercept; the synthesized driver cannot emulate
// Goexit, so Fatalf/Skipf panic with these unexported sentinel types. The
// per-test boundary recognizes them by TYPE — a guest panic can no longer
// be misclassified by matching the old "graalgo test fatal/skip" message
// prefixes (kept as a fallback for string panics). A guest defer/recover
// that swallows one is a documented divergence from go test: in gc the
// test function stops at Fatalf; here it continues. See
// cmd/graalgo/README.md.
type graalgoFatalPanic struct{ name string }
type graalgoSkipPanic struct{ name string }

type graalgoT struct {
	name       string
	depth      int
	failed     bool
	skipped    bool
	output     []string // buffered detail lines (flushed on failure when !verbose)
	subresults []string // buffered subtest result lines (flushed after own result)
}

// logLine emits a detail line: immediately in verbose mode, buffered
// otherwise (flushed after the --- FAIL line, matching go test).
func (t *graalgoT) logLine(s string) {
	if graalgoVerbose {
		graalgoPrintln(s)
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
	panic(graalgoFatalPanic{t.name})
}

func (t *graalgoT) graalgoFatal(fileline string, args ...interface{}) {
	t.failed = true
	t.logLine("    " + fileline + ": " + joinArgs(args...))
	panic(graalgoFatalPanic{t.name})
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
	panic(graalgoSkipPanic{t.name})
}

func (t *graalgoT) graalgoSkipf(fileline, format string, args ...interface{}) {
	t.skipped = true
	t.logLine("    " + fileline + ": " + sprintf(format, args...))
	panic(graalgoSkipPanic{t.name})
}

func (t *graalgoT) graalgoSkipNow(fileline string) {
	t.skipped = true
	panic(graalgoSkipPanic{t.name})
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

func (t *graalgoT) Run(name string, f func(*graalgoT)) bool {
	sub := &graalgoT{name: t.name + "/" + name, depth: t.depth + 1}
	// === RUN lines are never indented, matching go test.
	if graalgoVerbose {
		graalgoPrintln("=== RUN   " + sub.name)
	}
	t0 := time.Now()
	f(sub)
	dur := "(" + fmtDur2(time.Since(t0)) + ")"
	if sub.failed {
		t.failed = true
	}
	if sub.skipped {
		t.skipped = true
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

// fileTests groups the selected tests of one _test.go file.
type fileTests struct {
	file  string
	tests []string
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
		tests   map[string]bool
		ordered []string
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
		testUnits = append(testUnits, testUnit{srcUnit{ft.file, tf, fset}, testSet, ft.tests})
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
			continue
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
				continue
			}
			collectImports(sfAST)
			units = append(units, srcUnit{sf, sfAST, sfset})
		}
	}

	// The generated main uses time and os directly.
	addImport("", "time")
	addImport("", "os")
	usedImports[importSpec{"", "time"}] = true
	usedImports[importSpec{"", "os"}] = true

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

	// The test files: selected TestXxx functions (rewritten to *graalgoT
	// with position-capturing diagnostic calls) plus each file's own
	// non-test helpers and declarations.
	for _, tu := range testUnits {
		items = append(items, bodyItem{comment: fmt.Sprintf("\n// --- tests from %s ---\n", filepath.Base(tu.path))})
		for _, decl := range tu.f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				n := d.Name.Name
				isTest := tu.tests[n]
				// Skip other entry points (Test/Benchmark/Fuzz/Example).
				// Methods (Recv != nil) are always inlined — tests use
				// them (e.g. badRand.Read implementing io.Reader).
				isEntry := strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "Benchmark") ||
					strings.HasPrefix(n, "Fuzz") || strings.HasPrefix(n, "Example")
				if !isTest && isEntry {
					continue
				}
				rewriteParam(d)
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
	for _, u := range sibUnits {
		items = append(items, bodyItem{comment: fmt.Sprintf("\n// --- helpers from %s ---\n", filepath.Base(u.path))})
		for _, decl := range u.f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					n := d.Name.Name
					if strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "Benchmark") ||
						strings.HasPrefix(n, "Fuzz") || strings.HasPrefix(n, "Example") {
						continue
					}
				}
				rewriteParam(d)
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
	for _, n := range []string{"main", "graalgoVerbose", "graalgoT", "graalgoPrintln",
		"graalgoFatalPanic", "graalgoSkipPanic",
		"joinArgs", "sprintf", "toString", "itoa", "fmtDur2", "twoDigits"} {
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
	for _, n := range []string{"main", "graalgoVerbose", "graalgoT", "joinArgs", "sprintf", "toString", "itoa", "fmtDur2", "twoDigits"} {
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
	body.WriteString(testShimSource(verbose))
	body.WriteString("\nfunc main() {\n")
	body.WriteString("\tfailed := 0\n")
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
				body.WriteString(fmt.Sprintf("\t\tgraalgoPrintln(\"=== RUN   %s\")\n", t))
			}
			body.WriteString("\t\tt0 := time.Now()\n")
			body.WriteString("\t\tfunc() {\n")
			body.WriteString("\t\t\tdefer func() {\n")
			body.WriteString("\t\t\t\tif r := recover(); r != nil {\n")
			// The sentinel panic types are recognized by TYPE, not by
			// message prefix: a guest panic can no longer be
			// misclassified as fatal/skip. The string-prefix fallback
			// keeps the old string panic values working (they are no
			// longer generated, but may appear in hand-written
			// drivers) — the boundary is strictly stronger than
			// before, not weaker.
			body.WriteString("\t\t\t\t\tswitch r.(type) {\n")
			body.WriteString("\t\t\t\t\tcase graalgoSkipPanic:\n")
			body.WriteString("\t\t\t\t\t\ttt.skipped = true\n")
			body.WriteString("\t\t\t\t\tcase graalgoFatalPanic:\n")
			body.WriteString("\t\t\t\t\t\ttt.failed = true\n")
			body.WriteString("\t\t\t\t\tdefault:\n")
			body.WriteString("\t\t\t\t\t\ts := toString(r)\n")
			body.WriteString("\t\t\t\t\t\tif len(s) >= 17 && s[:17] == \"graalgo test skip\" {\n")
			body.WriteString("\t\t\t\t\t\t\ttt.skipped = true\n")
			body.WriteString("\t\t\t\t\t\t} else if len(s) >= 18 && s[:18] == \"graalgo test fatal\" {\n")
			body.WriteString("\t\t\t\t\t\t\ttt.failed = true\n")
			body.WriteString("\t\t\t\t\t\t} else {\n")
			body.WriteString("\t\t\t\t\t\t\ttt.failed = true\n")
			body.WriteString("\t\t\t\t\t\t\ttt.logLine(\"panic: \" + s)\n")
			body.WriteString("\t\t\t\t\t\t}\n")
			body.WriteString("\t\t\t\t\t}\n")
			body.WriteString("\t\t\t\t}\n")
			body.WriteString("\t\t\t}()\n")
			body.WriteString(fmt.Sprintf("\t\t\t%s(tt)\n", callName))
			body.WriteString("\t\t}()\n")
			body.WriteString("\t\tdur := \"(\" + fmtDur2(time.Since(t0)) + \")\"\n")
			if verbose {
				body.WriteString("\t\tif tt.skipped {\n")
				body.WriteString(fmt.Sprintf("\t\t\tgraalgoPrintln(\"--- SKIP: %s \" + dur)\n", t))
				body.WriteString("\t\t} else if tt.failed {\n")
				body.WriteString(fmt.Sprintf("\t\t\tgraalgoPrintln(\"--- FAIL: %s \" + dur)\n", t))
				body.WriteString("\t\t\tfailed++\n")
				body.WriteString("\t\t} else {\n")
				body.WriteString(fmt.Sprintf("\t\t\tgraalgoPrintln(\"--- PASS: %s \" + dur)\n", t))
				body.WriteString("\t\t}\n")
				body.WriteString("\t\tfor _, l := range tt.subresults {\n")
				body.WriteString("\t\t\tgraalgoPrintln(l)\n")
				body.WriteString("\t\t}\n")
			} else {
				body.WriteString("\t\tif tt.failed {\n")
				body.WriteString(fmt.Sprintf("\t\t\tgraalgoPrintln(\"--- FAIL: %s \" + dur)\n", t))
				body.WriteString("\t\t\tfor _, l := range tt.output {\n")
				body.WriteString("\t\t\t\tgraalgoPrintln(l)\n")
				body.WriteString("\t\t\t}\n")
				body.WriteString("\t\t\tfor _, l := range tt.subresults {\n")
				body.WriteString("\t\t\t\tgraalgoPrintln(l)\n")
				body.WriteString("\t\t\t}\n")
				body.WriteString("\t\t\tfailed++\n")
				body.WriteString("\t\t}\n")
			}
			body.WriteString("\t}\n")
		}
	}
	body.WriteString("\tif failed > 0 {\n")
	body.WriteString("\t\tgraalgoPrintln(\"FAIL\")\n")
	body.WriteString("\t\tos.Exit(1)\n")
	body.WriteString("\t}\n")
	if verbose {
		body.WriteString("\tgraalgoPrintln(\"PASS\")\n")
	}
	body.WriteString("}\n")

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
