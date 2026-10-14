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
	"sort"
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

// testShim is a minimal testing.T replacement, inlined into every
// synthesized test driver. It mirrors the shape used by tools/gate1's
// shim: Errorf records failures, Fatalf aborts the test, and the driver
// reports the final tally.
const testShim = `
// --- graalgo test shim (generated) ---
type graalgoT struct {
	name   string
	failed bool
}

func (t *graalgoT) Errorf(format string, args ...interface{}) {
	t.failed = true
	println("    ", t.name, ": ", sprintf(format, args...))
}

func (t *graalgoT) Error(args ...interface{}) {
	t.failed = true
	println("    ", t.name, ": ", sprintf("", args...))
}

func (t *graalgoT) Fatalf(format string, args ...interface{}) {
	t.failed = true
	println("    ", t.name, ": ", sprintf(format, args...))
	panic("graalgo test fatal: " + t.name)
}

func (t *graalgoT) Fatal(args ...interface{}) {
	t.failed = true
	println("    ", t.name, ": ", sprintf("", args...))
	panic("graalgo test fatal: " + t.name)
}

func (t *graalgoT) Fail() {
	t.failed = true
}

func (t *graalgoT) Failed() bool {
	return t.failed
}

func (t *graalgoT) Logf(format string, args ...interface{}) {
	println("    ", t.name, ": ", sprintf(format, args...))
}

func (t *graalgoT) Log(args ...interface{}) {
	println("    ", t.name, ": ", sprintf("", args...))
}

func (t *graalgoT) Name() string {
	return t.name
}

func (t *graalgoT) Helper() {}

func (t *graalgoT) Skip(args ...interface{}) {
	panic("graalgo test skip: " + t.name)
}

func (t *graalgoT) SkipNow() {
	panic("graalgo test skip: " + t.name)
}

func (t *graalgoT) Skipped() bool {
	return false
}

func (t *graalgoT) Run(name string, f func(*graalgoT)) bool {
	sub := &graalgoT{name: t.name + "/" + name}
	f(sub)
	if sub.failed {
		t.failed = true
		println("    --- FAIL:", sub.name)
		return false
	}
	return true
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
`

// synthesizeTestDriver builds a single-file Go program that runs the
// selected test functions. It extracts the TestXxx function declarations
// from the test file (rewritten to take *graalgoT) plus the package's
// non-test sources.
// synthesizeTestDriver builds a single-file Go program that runs the
// selected test functions. It extracts the TestXxx function declarations
// from the test file (rewritten to take *graalgoT) plus the package's
// non-test sources.
func synthesizeTestDriver(dir, testFile string, srcFiles, tests []string) (string, error) {
	// Parse the test file.
	fset := token.NewFileSet()
	tf, err := parser.ParseFile(fset, testFile, nil, 0)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", testFile, err)
	}

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
	}
	var units []srcUnit
	for _, sf := range srcFiles {
		sfset := token.NewFileSet()
		sfAST, err := parser.ParseFile(sfset, sf, nil, 0)
		if err != nil {
			continue
		}
		collectImports(sfAST)
		units = append(units, srcUnit{sf, sfAST})
	}
	collectImports(tf)
	var sibUnits []srcUnit
	siblings, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	for _, sib := range siblings {
		if sib == testFile {
			continue
		}
		sibSet := token.NewFileSet()
		sibAST, err := parser.ParseFile(sibSet, sib, nil, 0)
		if err != nil {
			continue
		}
		collectImports(sibAST)
		sibUnits = append(sibUnits, srcUnit{sib, sibAST})
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
				printDecl(&body, fset, d)
				body.WriteString("\n")
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				printDecl(&body, fset, d)
				body.WriteString("\n")
			}
		}
	}

	// The test file: TestXxx functions (rewritten to *graalgoT) plus the
	// file's own non-test helpers and declarations.
	body.WriteString(fmt.Sprintf("\n// --- tests from %s ---\n", filepath.Base(testFile)))
	testSet := make(map[string]bool)
	for _, t := range tests {
		testSet[t] = true
	}
	for _, decl := range tf.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			n := d.Name.Name
			isTest := testSet[n]
			// Skip other entry points (Test/Benchmark/Fuzz/Example).
			// Methods (Recv != nil) are always inlined — tests use
			// them (e.g. badRand.Read implementing io.Reader).
			isEntry := strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "Benchmark") ||
				strings.HasPrefix(n, "Fuzz") || strings.HasPrefix(n, "Example")
			if !isTest && isEntry {
				continue
			}
			rewriteParam(d)
			printDecl(&body, fset, d)
			body.WriteString("\n")
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			printDecl(&body, fset, d)
			body.WriteString("\n")
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
				printDecl(&body, fset, d)
				body.WriteString("\n")
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				printDecl(&body, fset, d)
				body.WriteString("\n")
			}
		}
	}

	// Shim + main runner.
	body.WriteString(testShim)
	body.WriteString("\nfunc main() {\n")
	body.WriteString("\tfailed := 0\n")
	for _, t := range tests {
		body.WriteString("\tfunc() {\n")
		body.WriteString(fmt.Sprintf("\t\ttt := &graalgoT{name: %q}\n", t))
		body.WriteString("\t\tdefer func() {\n")
		body.WriteString("\t\t\tif r := recover(); r != nil {\n")
		body.WriteString("\t\t\t\ts := toString(r)\n")
		body.WriteString("\t\t\t\tif len(s) >= 17 && s[:17] == \"graalgo test skip\" {\n")
		body.WriteString("\t\t\t\t\tprintln(\"    --- SKIP:\", tt.name)\n")
		body.WriteString("\t\t\t\t} else if !tt.failed {\n")
		body.WriteString("\t\t\t\t\ttt.failed = true\n")
		body.WriteString("\t\t\t\t\tprintln(\"    --- FAIL:\", tt.name, \"(panic:\", s, \")\")\n")
		body.WriteString("\t\t\t\t}\n")
		body.WriteString("\t\t\t}\n")
		body.WriteString("\t\t\tif tt.failed {\n")
		body.WriteString("\t\t\t\tprintln(\"    --- FAIL:\", tt.name)\n")
		body.WriteString("\t\t\t\tfailed++\n")
		body.WriteString("\t\t\t} else {\n")
		body.WriteString("\t\t\t\tprintln(\"    --- PASS:\", tt.name)\n")
		body.WriteString("\t\t\t}\n")
		body.WriteString("\t\t}()\n")
		body.WriteString(fmt.Sprintf("\t\t%s(tt)\n", t))
		body.WriteString("\t}()\n")
	}
	body.WriteString("\tif failed > 0 {\n")
	body.WriteString("\t\tprintln(\"FAIL\")\n")
	body.WriteString("\t\tpanic(\"tests failed\")\n")
	body.WriteString("\t}\n")
	body.WriteString("\tprintln(\"PASS\")\n")
	body.WriteString("}\n")

	bodyStr := body.String()

	// Prune imports not referenced in the body. Helpers from sibling
	// files pull in imports this driver's tests may not use — Go
	// rejects unused imports.
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
			pkgName := name
			if pkgName == "" || pkgName == "_" || pkgName == "." {
				pkgName = pathBase(p)
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
