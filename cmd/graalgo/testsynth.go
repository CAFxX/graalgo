package main

import (
	"bytes"
	"fmt"
	"go/ast"
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
		if !strings.HasPrefix(name, "Test") {
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

func (t *graalgoT) Run(name string, f func(*graalgoT)) bool {
	sub := &graalgoT{name: t.name + "/" + name, depth: t.depth + 1}
	// === RUN lines are never indented, matching go test.
	if graalgoVerbose {
		println("=== RUN   " + sub.name)
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
	// Collect imports from all inlined files (deduplicated). The backend
	// resolves imports via its own import machinery (the same path
	// `graalgo run` uses for packages), so we preserve them instead of
	// stripping — stripping breaks references like hash.Hash.
	imports := make(map[string]string) // path -> name ("" for default)
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
			if _, ok := imports[path]; !ok {
				imports[path] = name
			}
		}
	}

	type srcUnit struct {
		path string
		f    *ast.File
		fset *token.FileSet
	}
	var units []srcUnit
	for _, sf := range srcFiles {
		sfset := token.NewFileSet()
		sfAST, err := parser.ParseFile(sfset, sf, nil, 0)
		if err != nil {
			continue
		}
		collectImports(sfAST)
		units = append(units, srcUnit{sf, sfAST, sfset})
	}

	// Parse each test file with its own FileSet (positions feed the
	// file:line rewriting in rewriteTestCalls).
	type testUnit struct {
		srcUnit
		tests   map[string]bool
		ordered []string
	}
	var testUnits []testUnit
	selectedFiles := make(map[string]bool)
	for _, ft := range ftList {
		selectedFiles[ft.file] = true
		fset := token.NewFileSet()
		tf, err := parser.ParseFile(fset, ft.file, nil, 0)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", ft.file, err)
		}
		collectImports(tf)
		testSet := make(map[string]bool)
		for _, t := range ft.tests {
			testSet[t] = true
		}
		testUnits = append(testUnits, testUnit{srcUnit{ft.file, tf, fset}, testSet, ft.tests})
	}

	// Sibling test files (not selected): every non-entry-point
	// declaration is inlined as helpers.
	var sibUnits []srcUnit
	siblings, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	for _, sib := range siblings {
		if selectedFiles[sib] {
			continue
		}
		sibSet := token.NewFileSet()
		sibAST, err := parser.ParseFile(sibSet, sib, nil, 0)
		if err != nil {
			continue
		}
		collectImports(sibAST)
		sibUnits = append(sibUnits, srcUnit{sib, sibAST, sibSet})
	}

	// The generated main uses time and os directly.
	if _, ok := imports["time"]; !ok {
		imports["time"] = ""
	}
	if _, ok := imports["os"]; !ok {
		imports["os"] = ""
	}

	// Build the body first; imports are pruned against it below.
	var body strings.Builder

	// Inline non-test source files (package clause stripped): funcs plus
	// type/var/const declarations. Skip imports (emitted separately) and
	// the main function (we generate our own).
	for _, u := range units {
		body.WriteString(fmt.Sprintf("\n// --- from %s ---\n", filepath.Base(u.path)))
		for _, decl := range u.f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.Name == "main" && d.Recv == nil {
					continue
				}
				printDecl(&body, u.fset, d)
				body.WriteString("\n")
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				printDecl(&body, u.fset, d)
				body.WriteString("\n")
			}
		}
	}

	// The test files: selected TestXxx functions (rewritten to *graalgoT
	// with position-capturing diagnostic calls) plus each file's own
	// non-test helpers and declarations.
	for _, tu := range testUnits {
		body.WriteString(fmt.Sprintf("\n// --- tests from %s ---\n", filepath.Base(tu.path)))
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
				printDecl(&body, tu.fset, d)
				body.WriteString("\n")
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				printDecl(&body, tu.fset, d)
				body.WriteString("\n")
			}
		}
	}

	// Sibling test files: every non-entry-point declaration (helpers).
	for _, u := range sibUnits {
		body.WriteString(fmt.Sprintf("\n// --- helpers from %s ---\n", filepath.Base(u.path)))
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
				printDecl(&body, u.fset, d)
				body.WriteString("\n")
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				printDecl(&body, u.fset, d)
				body.WriteString("\n")
			}
		}
	}

	// Shim + main runner. Per-test output mirrors `go test -v`:
	// === RUN / --- PASS/FAIL/SKIP with (0.00s) timings, a single
	// PASS/FAIL summary, and exit code 1 (no panic trace) on failure.
	body.WriteString(testShimSource(verbose))
	body.WriteString("\nfunc main() {\n")
	body.WriteString("\tfailed := 0\n")
	for _, tu := range testUnits {
		for _, t := range tu.ordered {
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
			body.WriteString(fmt.Sprintf("\t\t\t%s(tt)\n", t))
			body.WriteString("\t\t}()\n")
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
	body.WriteString("\tif failed > 0 {\n")
	body.WriteString("\t\tprintln(\"FAIL\")\n")
	body.WriteString("\t\tos.Exit(1)\n")
	body.WriteString("\t}\n")
	if verbose {
		body.WriteString("\tprintln(\"PASS\")\n")
	}
	body.WriteString("}\n")

	bodyStr := body.String()

	// Prune imports not referenced in the body. Helpers from sibling
	// files pull in imports this driver's tests may not use — Go
	// rejects unused imports. The local package name comes from the
	// import spec when explicit; otherwise it is the package's DECLARED
	// name, resolved from its source directory — not the last path
	// element. Those differ for parent-module imports with a major-version
	// suffix (github.com/caddyserver/caddy/v2 declares package caddy),
	// for stdlib v2 paths (math/rand/v2 declares package rand), and for
	// third-party renames (gopkg.in/yaml.v2 declares package yaml).
	// Guessing from the path prunes the import and the driver fails with
	// "undefined: <name>".
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	if len(imports) > 0 {
		var paths []string
		for p := range imports {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		var kept []string
		for _, p := range paths {
			name := imports[p]
			if name == "_" || name == "." {
				// Side-effect and dot imports are never referenced by
				// package name, but dropping them changes semantics.
				kept = append(kept, p)
				continue
			}
			pkgName := name
			if pkgName == "" {
				pkgName = resolveImportPkgName(dir, p)
			}
			if pkgName != "" && strings.Contains(bodyStr, pkgName+".") {
				kept = append(kept, p)
			}
		}
		if len(kept) > 0 {
			sb.WriteString("import (\n")
			for _, p := range kept {
				if name := imports[p]; name != "" {
					sb.WriteString(fmt.Sprintf("\t%s %q\n", name, p))
				} else {
					sb.WriteString(fmt.Sprintf("\t%q\n", p))
				}
			}
			sb.WriteString(")\n")
		}
	}
	sb.WriteString(bodyStr)

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
// directory d, parsing only the package clause of one source file.
func packageNameInDir(d string) (string, error) {
	entries, err := os.ReadDir(d)
	if err != nil {
		return "", err
	}
	var fallback string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if fallback == "" {
			fallback = name
		}
		// Prefer a non-test file; the package clause is identical either way.
		if !strings.HasSuffix(name, "_test.go") {
			fallback = name
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
	return f.Name.Name, nil
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
