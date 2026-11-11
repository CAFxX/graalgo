package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goBinary locates the gc toolchain for running synthesized drivers.
// The synthesized driver is plain Go (shim + generated main), so running
// it under gc exercises exactly the shim and driver result logic fixed
// here; the Truffle backend is not involved.
func goBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, "tools", "go", "bin", "go"); fileExists(p) {
			return p
		}
	}
	t.Fatal("go toolchain not found on PATH or at ~/tools/go/bin/go")
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// runSynthDriver synthesizes a test driver for the package in dir and
// runs it with gc `go run`, returning its combined output and exit code.
func runSynthDriver(t *testing.T, dir string, verbose bool) (string, int) {
	t.Helper()
	// Reset the package-name cache so temp-dir paths from other tests
	// cannot leak in (same reset as synthDriver in testsynth_test.go).
	importPkgNameCache = map[string]string{}
	testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	var ftList []fileTests
	for _, tf := range testFiles {
		tests, err := discoverTests(tf)
		if err != nil {
			t.Fatal(err)
		}
		if len(tests) > 0 {
			ftList = append(ftList, fileTests{file: tf, tests: tests})
		}
	}
	srcFiles, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var nonTest []string
	for _, f := range srcFiles {
		if !strings.HasSuffix(f, "_test.go") {
			nonTest = append(nonTest, f)
		}
	}
	driver, err := synthesizeTestDriver(dir, ftList, nonTest, verbose, nil)
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

func writeSubtestPkg(t *testing.T, testSrc string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/subtest\n\ngo 1.21\n")
	writeFile(t, dir, "a.go", "package subtest\n\nvar SiblingRan bool\n")
	writeFile(t, dir, "a_test.go", testSrc)
	return dir
}

func mustContain(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("driver output missing %q:\n%s", want, out)
	}
}

func mustNotContain(t *testing.T, out, want string) {
	t.Helper()
	if strings.Contains(out, want) {
		t.Errorf("driver output must not contain %q:\n%s", want, out)
	}
}

// Regression for NEW-P1-1 (verbose fail-masking): a test that fails one
// subtest via Errorf and then skips a later subtest must report FAIL —
// in verbose mode as well as non-verbose — and the failing subtests
// after the skip must still run.
func TestSubtestSkipThenFail(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestParent(t *testing.T) {
	t.Run("A", func(t *testing.T) { t.Errorf("A failed") })
	t.Run("B", func(t *testing.T) { t.SkipNow() })
	t.Run("C", func(t *testing.T) { t.Errorf("C failed") })
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 1 {
			t.Errorf("verbose=%v: driver exit = %d, want 1 (package must fail):\n%s", verbose, code, out)
		}
		mustContain(t, out, "--- FAIL: TestParent ")
		mustContain(t, out, "--- FAIL: TestParent/A")
		mustContain(t, out, "--- FAIL: TestParent/C")
		mustNotContain(t, out, "--- SKIP: TestParent ")
		if verbose {
			mustContain(t, out, "--- SKIP: TestParent/B")
		}
	}
}

// Regression for NEW-P1-1 (sibling abort): a Fatal in one subtest must
// not prevent later siblings from running.
func TestSubtestFatalSiblingStillRuns(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestParent(t *testing.T) {
	t.Run("fatal", func(t *testing.T) { t.Fatal("fatal subtest") })
	t.Run("sibling", func(t *testing.T) { SiblingRan = true })
	if !SiblingRan {
		t.Errorf("sibling did not run")
	}
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 1 {
			t.Errorf("verbose=%v: driver exit = %d, want 1 (fatal subtest must fail the package):\n%s", verbose, code, out)
		}
		mustContain(t, out, "--- FAIL: TestParent/fatal")
		mustNotContain(t, out, "sibling did not run")
		if verbose {
			mustContain(t, out, "--- PASS: TestParent/sibling")
		}
	}
}

// Regression for NEW-P1-1 (sentinel fragility): a user panic whose
// message starts with the old string sentinels must report FAIL with
// the panic line, never SKIP — in a subtest and at top level.
func TestUserPanicWithSentinelPrefix(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestSentinelSub(t *testing.T) {
	t.Run("spoof", func(t *testing.T) { panic("graalgo test skip: not a real skip") })
}

func TestSentinelTop(t *testing.T) {
	panic("graalgo test fatal: not a real fatal")
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 1 {
			t.Errorf("verbose=%v: driver exit = %d, want 1:\n%s", verbose, code, out)
		}
		mustContain(t, out, "--- FAIL: TestSentinelSub/spoof")
		mustContain(t, out, "panic: graalgo test skip: not a real skip")
		mustContain(t, out, "--- FAIL: TestSentinelTop")
		mustContain(t, out, "panic: graalgo test fatal: not a real fatal")
		mustNotContain(t, out, "--- SKIP: TestSentinelSub")
		mustNotContain(t, out, "--- SKIP: TestSentinelTop")
	}
}
