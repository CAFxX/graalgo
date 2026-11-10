package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeMainPkg creates a temp module with the given files (name ->
// content) for TestMain end-to-end tests.
func writeMainPkg(t *testing.T, module string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+module+"\n\ngo 1.21\n")
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	return dir
}

// TestTestMainRunsSetupTeardownAndTest is the A-2 end-to-end
// regression: a package with TestMain must run setup, the tests, and
// teardown, and exit 0 — instead of failing the whole run with a
// frontend type error (the pre-fix behavior).
func TestTestMainRunsSetupTeardownAndTest(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainpass", map[string]string{
		"a.go": "package mainpass\n",
		"a_test.go": `package mainpass

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	println("setup-ran")
	code := m.Run()
	println("teardown-ran")
	os.Exit(code)
}

func TestOK(t *testing.T) {
	println("test-ran")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Fatalf("driver exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "setup-ran")
	mustContain(t, out, "test-ran")
	mustContain(t, out, "teardown-ran")
	mustContain(t, out, "=== RUN   TestOK")
	mustContain(t, out, "--- PASS: TestOK")
	mustContain(t, out, "PASS")
}

// TestTestMainFailingTestExitsOne: a failing test under TestMain must
// fail the package (exit 1) and teardown must still run.
func TestTestMainFailingTestExitsOne(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainfail", map[string]string{
		"a.go": "package mainfail\n",
		"a_test.go": `package mainfail

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	println("setup-ran")
	code := m.Run()
	println("teardown-ran")
	os.Exit(code)
}

func TestBad(t *testing.T) {
	t.Errorf("bad thing")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 1 {
		t.Fatalf("driver exit = %d, want 1:\n%s", code, out)
	}
	mustContain(t, out, "setup-ran")
	mustContain(t, out, "teardown-ran")
	mustContain(t, out, "--- FAIL: TestBad")
}

// TestTestMainWithoutRun: a TestMain that returns without calling Run
// runs no tests and the driver exits 0, matching gc.
func TestTestMainWithoutRun(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainnorun", map[string]string{
		"a.go": "package mainnorun\n",
		"a_test.go": `package mainnorun

import "testing"

func TestMain(m *testing.M) {
	println("main-only")
}

func TestOK(t *testing.T) {
	println("test-ran")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Fatalf("driver exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "main-only")
	mustNotContain(t, out, "test-ran")
	mustNotContain(t, out, "=== RUN   TestOK")
	mustNotContain(t, out, "testing: warning: no tests to run")
}

// TestTestMainDotImport: a dot-imported TestMain (*M) is an entry
// point, and the dot-imported test param (*T) is rewritten.
func TestTestMainDotImport(t *testing.T) {
	dir := writeMainPkg(t, "example.com/maindot", map[string]string{
		"a.go": "package maindot\n",
		"a_test.go": `package maindot

import (
	. "testing"
	"os"
)

func TestMain(m *M) {
	println("dot-main-ran")
	os.Exit(m.Run())
}

func TestOK(t *T) {
	println("dot-test-ran")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Fatalf("driver exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "dot-main-ran")
	mustContain(t, out, "dot-test-ran")
	mustContain(t, out, "--- PASS: TestOK")
}

// TestTestMainAliased: an aliased TestMain (*tt.M) is an entry point.
func TestTestMainAliased(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainalias", map[string]string{
		"a.go": "package mainalias\n",
		"a_test.go": `package mainalias

import (
	tt "testing"
	"os"
)

func TestMain(m *tt.M) {
	println("alias-main-ran")
	os.Exit(m.Run())
}

func TestOK(t *tt.T) {
	println("alias-test-ran")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Fatalf("driver exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "alias-main-ran")
	mustContain(t, out, "alias-test-ran")
	mustContain(t, out, "--- PASS: TestOK")
}

// TestTestMainExternalPackage: a TestMain in an external test package
// (package foo_test) is the entry point, as under gc.
func TestTestMainExternalPackage(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainext", map[string]string{
		"a.go": "package mainext\n",
		"a_test.go": `package mainext_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	println("ext-main-ran")
	os.Exit(m.Run())
}

func TestOK(t *testing.T) {
	println("ext-test-ran")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Fatalf("driver exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "ext-main-ran")
	mustContain(t, out, "ext-test-ran")
	mustContain(t, out, "--- PASS: TestOK")
}

// TestTestMainRepeatedRun: gc allows m.Run() to be called more than
// once; each call re-runs the tests.
func TestTestMainRepeatedRun(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainrepeat", map[string]string{
		"a.go": "package mainrepeat\n",
		"a_test.go": `package mainrepeat

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	c1 := m.Run()
	c2 := m.Run()
	println("codes", c1, c2)
	os.Exit(c1)
}

func TestOK(t *testing.T) {
	println("test-ran")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Fatalf("driver exit = %d, want 0:\n%s", code, out)
	}
	if n := strings.Count(out, "=== RUN   TestOK"); n != 2 {
		t.Errorf("TestOK ran %d times, want 2:\n%s", n, out)
	}
	mustContain(t, out, "codes 0 0")
}

// runSynthDriverWithList synthesizes a driver for the given file list
// (unlike runSynthDriver, the caller supplies the selection, so an
// empty selection with a TestMain can be exercised) and runs it with
// gc `go run`, returning combined output and exit code.
func runSynthDriverWithList(t *testing.T, dir string, ftList []fileTests, srcFiles []string, verbose bool) (string, int) {
	t.Helper()
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(dir, ftList, srcFiles, verbose)
	if err != nil {
		t.Fatalf("synthesizeTestDriver: %v", err)
	}
	defer os.Remove(driver)
	cmd := exec.Command(goBinary(t), "run", driver)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("go run driver: %v\noutput:\n%s", err, out)
		}
	}
	return string(out), code
}

// TestTestMainRunNoTestsSelected is the -run counterpart of the A-2
// end-to-end test: gc runs TestMain with -run matching nothing, and
// its m.Run() reports zero tests (verified against go1.27.1:
// "testing: warning: no tests to run", PASS, exit 0). The driver must
// do the same when the selection is empty but a TestMain exists.
func TestTestMainRunNoTestsSelected(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainnoselect", map[string]string{
		"a.go": "package mainnoselect\n",
		"a_test.go": `package mainnoselect

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	println("setup-ran")
	code := m.Run()
	println("teardown-ran")
	os.Exit(code)
}

func TestOK(t *testing.T) {
	println("test-ran")
}
`,
	})
	testFile := filepath.Join(dir, "a_test.go")
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriverWithList(t, dir,
			[]fileTests{{file: testFile, tests: nil, testMain: true}},
			[]string{filepath.Join(dir, "a.go")}, verbose)
		if code != 0 {
			t.Fatalf("verbose=%v: driver exit = %d, want 0:\n%s", verbose, code, out)
		}
		mustContain(t, out, "setup-ran")
		mustContain(t, out, "teardown-ran")
		mustNotContain(t, out, "test-ran")
		mustNotContain(t, out, "=== RUN   TestOK")
		if verbose {
			mustContain(t, out, "testing: warning: no tests to run")
			mustContain(t, out, "PASS")
		} else {
			mustNotContain(t, out, "testing: warning: no tests to run")
		}
	}
}

// TestRunPackageTestMainRedeclared pins runPackage's redeclaration
// checks: any package with two TestMain functions — two entry points,
// an entry point plus an ordinary TestMain test, or two ordinary
// TestMain tests — must fail before the backend is invoked (gc:
// "multiple definitions of TestMain" / "TestMain redeclared in this
// block", verified against go1.27.1).
func TestRunPackageTestMainRedeclared(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "two entry points",
			files: map[string]string{
				"a.go": "package mainredecl\n",
				"a_test.go": `package mainredecl

import "testing"

func TestMain(m *testing.M) {}
`,
				"b_test.go": `package mainredecl

import "testing"

func TestMain(m *testing.M) {}
`,
			},
		},
		{
			name: "entry point plus as-test",
			files: map[string]string{
				"a.go": "package mainredecl\n",
				"a_test.go": `package mainredecl

import "testing"

func TestMain(m *testing.M) {}
`,
				"b_test.go": `package mainredecl

import "testing"

func TestMain(m *testing.T) {}
`,
			},
		},
		{
			name: "two as-test",
			files: map[string]string{
				"a.go": "package mainredecl\n",
				"a_test.go": `package mainredecl

import "testing"

func TestMain(m *testing.T) {}
`,
				"b_test.go": `package mainredecl

import "testing"

func TestMain(m *testing.T) {}
`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeMainPkg(t, "example.com/mainredecl", tc.files)
			r := newTestRunner(dir, false, nil, 1, 0)
			r.runBackend = func(ctx context.Context, root string, args []string) (int, bool) {
				t.Error("backend must not be called for a redeclared TestMain")
				return 0, false
			}
			if r.runPackage(dir) {
				t.Error("runPackage = true, want false for redeclared TestMain")
			}
		})
	}
}

// TestTestMainAsTest: func TestMain(m *testing.T) is an ordinary test
// named TestMain under gc, not an entry point — the driver must run
// it as a test.
func TestTestMainAsTest(t *testing.T) {
	dir := writeMainPkg(t, "example.com/mainastest", map[string]string{
		"a.go": "package mainastest\n",
		"a_test.go": `package mainastest

import "testing"

func TestMain(m *testing.T) {
	println("as-test-ran")
}
`,
	})
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Fatalf("driver exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "as-test-ran")
	mustContain(t, out, "=== RUN   TestMain")
	mustContain(t, out, "--- PASS: TestMain")
}
