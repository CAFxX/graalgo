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

func (t *graalgoT) Fatalf(format string, args ...interface{}) {
	t.failed = true
	println("    ", t.name, ": ", sprintf(format, args...))
	panic("graalgo test fatal: " + t.name)
}

func (t *graalgoT) Logf(format string, args ...interface{}) {
	println("    ", t.name, ": ", sprintf(format, args...))
}

func (t *graalgoT) Skip(args ...interface{}) {
	panic("graalgo test skip: " + t.name)
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
func synthesizeTestDriver(dir, testFile string, srcFiles, tests []string) (string, error) {
	// Parse the test file.
	fset := token.NewFileSet()
	tf, err := parser.ParseFile(fset, testFile, nil, 0)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", testFile, err)
	}

	var sb strings.Builder
	sb.WriteString("package main\n\n")

	// Inline non-test source files (package clause stripped).
	// For each source file, extract only func declarations (no imports,
	// to avoid conflicts). Skip the main function (we generate our own).
	for _, sf := range srcFiles {
		sfset := token.NewFileSet()
		sfAST, err := parser.ParseFile(sfset, sf, nil, 0)
		if err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n// --- from %s ---\n", filepath.Base(sf)))
		for _, decl := range sfAST.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				// Skip main (we generate our own test runner main).
				if fn.Name.Name == "main" && fn.Recv == nil {
					continue
				}
				printDecl(&sb, fset, fn)
				sb.WriteString("\n")
			}
		}
	}

	// Extract TestXxx functions from the test file, rewriting *testing.T
	// to *graalgoT.
	sb.WriteString(fmt.Sprintf("\n// --- tests from %s ---\n", filepath.Base(testFile)))
	testSet := make(map[string]bool)
	for _, t := range tests {
		testSet[t] = true
	}
	for _, decl := range tf.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if !testSet[fn.Name.Name] {
			continue
		}
		// Rewrite the *testing.T param to *graalgoT.
		rewriteParam(fn)
		printDecl(&sb, fset, fn)
		sb.WriteString("\n")
	}

	// Shim + main runner.
	sb.WriteString(testShim)
	sb.WriteString("\nfunc main() {\n")
	sb.WriteString("\tfailed := 0\n")
	for _, t := range tests {
		sb.WriteString(fmt.Sprintf("\tfunc() {\n"))
		sb.WriteString(fmt.Sprintf("\t\ttt := &graalgoT{name: %q}\n", t))
		sb.WriteString(fmt.Sprintf("\t\tdefer func() {\n"))
		sb.WriteString(fmt.Sprintf("\t\t\tif r := recover(); r != nil {\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\ts := toString(r)\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\tif len(s) >= 17 && s[:17] == \"graalgo test skip\" {\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\t\tprintln(\"    --- SKIP:\", tt.name)\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\t} else if !tt.failed {\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\t\ttt.failed = true\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\t\tprintln(\"    --- FAIL:\", tt.name, \"(panic:\", s, \")\")\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\t}\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t}\n"))
		sb.WriteString(fmt.Sprintf("\t\t\tif tt.failed {\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\tprintln(\"    --- FAIL:\", tt.name)\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\tfailed++\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t} else {\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t\tprintln(\"    --- PASS:\", tt.name)\n"))
		sb.WriteString(fmt.Sprintf("\t\t\t}\n"))
		sb.WriteString(fmt.Sprintf("\t\t}()\n"))
		sb.WriteString(fmt.Sprintf("\t\t%s(tt)\n", t))
		sb.WriteString(fmt.Sprintf("\t}()\n"))
	}
	sb.WriteString("\tif failed > 0 {\n")
	sb.WriteString("\t\tprintln(\"FAIL\")\n")
	sb.WriteString("\t\tpanic(\"tests failed\")\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\tprintln(\"PASS\")\n")
	sb.WriteString("}\n")

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

// printDecl prints a single AST declaration to the builder.
func printDecl(sb *strings.Builder, fset *token.FileSet, decl ast.Decl) {
	var buf bytes.Buffer
	printer.Fprint(&buf, fset, decl)
	sb.WriteString(buf.String())
}

// rewriteParam rewrites a *testing.T parameter to *graalgoT in a FuncDecl.
func rewriteParam(fn *ast.FuncDecl) {
	if fn.Type.Params == nil {
		return
	}
	for _, param := range fn.Type.Params.List {
		// Look for *testing.T or testing.T
		switch t := param.Type.(type) {
		case *ast.StarExpr:
			if sel, ok := t.X.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "testing" && sel.Sel.Name == "T" {
					// Replace with graalgoT identifier
					param.Type = &ast.StarExpr{X: &ast.Ident{Name: "graalgoT"}}
				}
			}
		case *ast.SelectorExpr:
			if ident, ok := t.X.(*ast.Ident); ok && ident.Name == "testing" && t.Sel.Name == "T" {
				param.Type = &ast.Ident{Name: "graalgoT"}
			}
		}
		// Also rewrite testing.X references in the body to graalgo equivalents
		// (best effort; the shim covers the common methods)
	}
}
