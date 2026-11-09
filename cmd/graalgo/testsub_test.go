package main

import (
	"bytes"
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

// runSynthDriverSplit synthesizes a test driver for the package in dir
// and runs it with gc `go run`, returning its stdout, stderr and exit
// code separately. The split matters: `go test` output is stdout, so
// every result line (=== RUN, --- PASS/FAIL/SKIP, PASS/FAIL) must land
// on stdout and stderr must stay empty of them. A CombinedOutput-only
// helper masks exactly that split (R03-T2).
func runSynthDriverSplit(t *testing.T, dir string, verbose bool) (string, string, int) {
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
	driver, err := synthesizeTestDriver(dir, ftList, nonTest, verbose)
	if err != nil {
		t.Fatalf("synthesizeTestDriver: %v", err)
	}
	defer os.Remove(driver)
	cmd := exec.Command(goBinary(t), "run", driver)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("go run driver: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	}
	return stdout.String(), stderr.String(), code
}

// runSynthDriver synthesizes a test driver for the package in dir and
// runs it with gc `go run`, returning its combined output and exit code.
func runSynthDriver(t *testing.T, dir string, verbose bool) (string, int) {
	stdout, stderr, code := runSynthDriverSplit(t, dir, verbose)
	return stdout + stderr, code
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

// Regression for R03-T2: every result line the driver prints must land
// on stdout — `go test` output is stdout, while builtin println writes
// to stderr. The old driver printed all of them with println, so `go
// test pkg > out.txt` captured no per-test lines; the old
// CombinedOutput-only helper masked it.
func TestDriverResultLinesOnStdout(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestPass(t *testing.T) {
	t.Log("pass detail")
}

func TestFail(t *testing.T) {
	t.Log("fail detail")
	t.Errorf("boom")
}

func TestSkip(t *testing.T) {
	t.Skip("skipping")
}
`)
	for _, verbose := range []bool{true, false} {
		stdout, stderr, code := runSynthDriverSplit(t, dir, verbose)
		if code != 1 {
			t.Errorf("verbose=%v: driver exit = %d, want 1:\nstdout:\n%s\nstderr:\n%s", verbose, code, stdout, stderr)
		}
		// No result line may leak to stderr. (`go run` itself reports
		// the driver's non-zero exit as "exit status 1" on stderr;
		// that is the harness's line, not the driver's — the driver
		// runs under the graalgo backend in production, not `go run` —
		// so assert on the absence of result lines, not on empty
		// stderr, for the failing package.)
		for _, marker := range []string{"=== RUN", "--- PASS", "--- FAIL", "--- SKIP", "pass detail", "fail detail", "boom", "skipping", "\nPASS\n", "\nFAIL\n"} {
			mustNotContain(t, stderr, marker)
		}
		mustContain(t, stdout, "--- FAIL: TestFail ")
		mustContain(t, stdout, "fail detail")
		mustContain(t, stdout, "\nFAIL\n")
		if verbose {
			mustContain(t, stdout, "=== RUN   TestPass")
			mustContain(t, stdout, "=== RUN   TestFail")
			mustContain(t, stdout, "=== RUN   TestSkip")
			mustContain(t, stdout, "--- PASS: TestPass ")
			mustContain(t, stdout, "--- SKIP: TestSkip ")
			mustContain(t, stdout, "pass detail")
		} else {
			mustNotContain(t, stdout, "=== RUN")
			mustNotContain(t, stdout, "--- PASS: TestPass ")
			mustNotContain(t, stdout, "--- SKIP: TestSkip ")
			mustNotContain(t, stdout, "pass detail")
		}
	}
}

// Regression for R03-T2 (PASS summary): an all-pass package in verbose
// mode prints the PASS summary on stdout, with empty stderr.
func TestDriverPassSummaryOnStdout(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestPass(t *testing.T) {
	t.Log("pass detail")
}
`)
	stdout, stderr, code := runSynthDriverSplit(t, dir, true)
	if code != 0 {
		t.Errorf("driver exit = %d, want 0:\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if stderr != "" {
		t.Errorf("driver wrote to stderr:\n%s\nstdout:\n%s", stderr, stdout)
	}
	mustContain(t, stdout, "=== RUN   TestPass")
	mustContain(t, stdout, "--- PASS: TestPass ")
	mustContain(t, stdout, "pass detail")
	mustContain(t, stdout, "\nPASS\n")
}

// Regression for R03-T2 (PKG-075): when the package under test declares
// a top-level `fmt` (like fmt's own `type fmt struct`), the forced fmt
// import is aliased (fmt2) and the driver must still compile and print
// its result lines to stdout. A naive bare `fmt.Println` in the shim
// would fail to compile this driver (`fmt` names the type).
func TestDriverFmtNameCollision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/fmtcoll\n\ngo 1.21\n")
	writeFile(t, dir, "p.go", "package fmtcoll\n\ntype fmt struct{ x int }\n\nfunc (v *fmt) Get() int { return v.x }\n")
	writeFile(t, dir, "p_test.go", `package fmtcoll

import "testing"

func TestGet(t *testing.T) {
	f := &fmt{x: 7}
	if f.Get() != 7 {
		t.Fatal("bad")
	}
}
`)
	for _, verbose := range []bool{true, false} {
		stdout, stderr, code := runSynthDriverSplit(t, dir, verbose)
		if code != 0 {
			t.Errorf("verbose=%v: driver exit = %d, want 0 (driver must compile and pass):\nstdout:\n%s\nstderr:\n%s", verbose, code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("verbose=%v: driver wrote to stderr:\n%s\nstdout:\n%s", verbose, stderr, stdout)
		}
		if verbose {
			mustContain(t, stdout, "=== RUN   TestGet")
			mustContain(t, stdout, "--- PASS: TestGet ")
			mustContain(t, stdout, "\nPASS\n")
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
