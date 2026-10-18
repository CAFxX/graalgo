package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiscoverTestsExcludesTestMain is the regression test for RV18-P1-22a:
// TestMain(m *testing.M) used to be discovered as a test (name starts with
// "Test", one param), and rewriteParam only handles *testing.T, so the
// synthesized driver failed to compile the whole package. TestMain is the
// test binary's entry point, not a test — it is excluded (and reported by
// detectTestMain so runPackage can warn). This also pins go test's naming
// rule: "Test" alone and "TestXxx" with a non-lowercase X are tests;
// "Testfoo" (lowercase after Test) is not.
func TestDiscoverTestsExcludesTestMain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tm\n\ngo 1.21\n")
	writeFile(t, root, "a_test.go", `package tm

import "testing"

func TestMain(m *testing.M) {
	m.Run()
}

func TestFoo(t *testing.T) {}

func Testfoo(t *testing.T) {}

func Test(t *testing.T) {}

func BenchmarkFoo(b *testing.B) {}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestFoo", "Test"}
	if len(tests) != len(want) {
		t.Fatalf("discoverTests = %v, want %v", tests, want)
	}
	for i := range want {
		if tests[i] != want[i] {
			t.Fatalf("discoverTests = %v, want %v", tests, want)
		}
	}
	hasMain, err := detectTestMain(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMain {
		t.Errorf("detectTestMain = false, want true for a file declaring TestMain")
	}

	// A file without TestMain reports false.
	writeFile(t, root, "b_test.go", "package tm\n\nimport \"testing\"\n\nfunc TestBar(t *testing.T) {}\n")
	hasMain, err = detectTestMain(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if hasMain {
		t.Errorf("detectTestMain = true, want false for a file without TestMain")
	}
}

// TestSynthDriverOmitsTestMain pins the end-to-end effect of the RV18-P1-22a
// fix: a package whose test file declares TestMain still synthesizes a
// valid driver (it parses), the TestMain declaration is not inlined, no
// dangling testing.M reference survives (the testing import is stripped),
// and the real test is still wired into main.
func TestSynthDriverOmitsTestMain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tm\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package tm\n\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, root, "p_test.go", `package tm

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	println("setup")
	os.Exit(m.Run())
}

func TestHello(t *testing.T) {
	if Hello() != "hi" {
		t.Fatal("bad")
	}
}
`)
	testFile := filepath.Join(root, "p_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestHello" {
		t.Fatalf("discoverTests = %v, want [TestHello]", tests)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "p.go")})

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "driver.go", src, 0)
	if err != nil {
		t.Fatalf("generated driver does not parse: %v\n%s", err, src)
	}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "TestMain" {
			t.Errorf("generated driver inlines TestMain; it must be excluded:\n%s", src)
		}
	}
	if strings.Contains(src, "testing.M") {
		t.Errorf("generated driver references testing.M (the testing import is stripped):\n%s", src)
	}
	if !strings.Contains(src, "TestHello(tt)") {
		t.Errorf("generated driver lost the TestHello call:\n%s", src)
	}
}

// noBuiltinPrintlnCalls fails the test if the driver's own harness code
// (the graalgoT shim methods, the graalgoPrintln helper, and the generated
// main) calls the builtin println: harness output must go to stdout via
// graalgoPrintln (the builtin writes to stderr in graalgo, breaking
// scripts that parse `graalgo test` stdout). Guest test bodies are exempt:
// a guest println is gc-parity (stderr in gc too).
func noBuiltinPrintlnCalls(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "driver.go", src, 0)
	if err != nil {
		t.Fatalf("generated driver does not parse: %v\n%s", err, src)
	}
	isGraalgoTMethod := func(fn *ast.FuncDecl) bool {
		if fn.Recv == nil || len(fn.Recv.List) != 1 {
			return false
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			return false
		}
		id, ok := star.X.(*ast.Ident)
		return ok && id.Name == "graalgoT"
	}
	harness := map[string]bool{
		"main": true, "graalgoPrintln": true, "joinArgs": true,
		"sprintf": true, "toString": true, "itoa": true,
		"fmtDur2": true, "twoDigits": true,
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if !harness[fn.Name.Name] && !isGraalgoTMethod(fn) {
			continue // guest code: println is gc-parity there
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "println" {
				t.Errorf("harness function %s calls builtin println (stderr); use graalgoPrintln (stdout)", fn.Name.Name)
				return false
			}
			return true
		})
	}
}

// synthDriverVerbose is synthDriver with verbosity selectable.
func synthDriverVerbose(t *testing.T, dir string, ftList []fileTests, srcFiles []string, verbose bool) string {
	t.Helper()
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(dir, ftList, srcFiles, verbose)
	if err != nil {
		t.Fatalf("synthesizeTestDriver: %v", err)
	}
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// TestDriverOutputGoesToStdout is the regression test for RV18-P1-22b: the
// synthesized driver's === RUN / --- PASS / --- FAIL / PASS / FAIL lines
// and buffered detail lines must go to stdout (os.Stdout), not the builtin
// println (stderr in graalgo). Checked in both verbose and quiet modes,
// since the generated main emits different println sites per mode.
func TestDriverOutputGoesToStdout(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/out\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package out\n\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, root, "p_test.go", `package out

import "testing"

func TestHello(t *testing.T) {
	println("guest debug") // guest println is gc-parity (stderr); must not trip the harness check
	t.Log("detail")
	if Hello() != "hi" {
		t.Fatal("bad")
	}
}
`)
	testFile := filepath.Join(root, "p_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	ftList := []fileTests{{file: testFile, tests: tests}}
	srcFiles := []string{filepath.Join(root, "p.go")}
	for _, verbose := range []bool{false, true} {
		src := synthDriverVerbose(t, root, ftList, srcFiles, verbose)
		noBuiltinPrintlnCalls(t, src)
		if !strings.Contains(src, "func graalgoPrintln(s string)") {
			t.Errorf("verbose=%v: driver missing the graalgoPrintln shim:\n%s", verbose, src)
		}
		if !strings.Contains(src, "os.Stdout.WriteString") {
			t.Errorf("verbose=%v: driver does not write to os.Stdout:\n%s", verbose, src)
		}
		// The go-test-format lines must flow through the stdout shim.
		// Quiet mode prints only failures; verbose mode prints RUN and
		// PASS lines too.
		want := []string{
			`graalgoPrintln("--- FAIL: TestHello " + dur)`,
			`graalgoPrintln("FAIL")`,
			`graalgoPrintln(l)`,
		}
		if verbose {
			want = append(want,
				`graalgoPrintln("=== RUN   TestHello")`,
				`graalgoPrintln("--- PASS: TestHello " + dur)`,
				`graalgoPrintln("PASS")`,
			)
		}
		for _, line := range want {
			if !strings.Contains(src, line) {
				t.Errorf("verbose=%v: driver missing %q:\n%s", verbose, line, src)
			}
		}
	}
}

// TestFatalSkipSentinelPanics is the regression test for RV-P1-28: Fatalf
// and Skipf panic with unexported sentinel types (graalgoFatalPanic /
// graalgoSkipPanic) recognized by TYPE at the per-test boundary — not with
// string values matched by message prefix, which a guest panic could
// spoof. The old string-prefix match is kept as a fallback (the boundary
// is strictly stronger, not weaker). This also pins the deterministic
// behavior: fatal sets failed=true, and the non-verbose driver flushes the
// buffered output after the --- FAIL line.
func TestFatalSkipSentinelPanics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fs\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package fs\n")
	writeFile(t, root, "p_test.go", `package fs

import "testing"

func TestFatal(t *testing.T) {
	t.Fatalf("boom %d", 1)
}

func TestSkip(t *testing.T) {
	t.Skipf("skip %d", 2)
}
`)
	testFile := filepath.Join(root, "p_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "p.go")})

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "driver.go", src, 0); err != nil {
		t.Fatalf("generated driver does not parse: %v\n%s", err, src)
	}
	for _, want := range []string{
		"panic(graalgoFatalPanic{",
		"panic(graalgoSkipPanic{",
		"type graalgoFatalPanic struct",
		"type graalgoSkipPanic struct",
		"case graalgoFatalPanic:",
		"case graalgoSkipPanic:",
		// The legacy string-prefix fallback is retained.
		`"graalgo test skip"`,
		`"graalgo test fatal"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated driver missing %q:\n%s", want, src)
		}
	}
	// The old spoofable string panics must be gone.
	for _, gone := range []string{`panic("graalgo test fatal`, `panic("graalgo test skip`} {
		if strings.Contains(src, gone) {
			t.Errorf("generated driver still panics with a spoofable string %q:\n%s", gone, src)
		}
	}
	// Deterministic behavior pin: the fatal sentinel case marks the test
	// failed...
	if !strings.Contains(src, "case graalgoFatalPanic:\n\t\t\t\t\t\ttt.failed = true") {
		t.Errorf("fatal sentinel case does not set tt.failed = true:\n%s", src)
	}
	// ...and the non-verbose driver flushes buffered output after the
	// --- FAIL line.
	failIdx := strings.Index(src, `graalgoPrintln("--- FAIL: TestFatal " + dur)`)
	flushIdx := strings.Index(src, "for _, l := range tt.output")
	if failIdx < 0 || flushIdx < 0 || flushIdx < failIdx {
		t.Errorf("non-verbose driver does not flush tt.output after the --- FAIL line:\n%s", src)
	}
}
