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
	driver, err := synthesizeTestDriver(dir, ftList, nonTest, verbose)
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

// Regression for FE-06: the previously-missing testing.T methods work
// in a synthesized driver — Cleanup (LIFO, incl. context still live
// during cleanups), TempDir (created, removed at test end), Setenv and
// Chdir (restored at test end), Deadline (no deadline), Context,
// testing.Short()/testing.Verbose(), and helpers taking testing.TB.
func TestMissingTMethods(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

var (
	cleanupOrder []string
	tempDirPath  string
	origWd       string
)

func helperTB(tb testing.TB) {
	tb.Helper()
	tb.Log("from TB helper")
	if got := tb.Name(); got == "" {
		tb.Errorf("TB Name empty")
	}
}

func TestMethods(t *testing.T) {
	helperTB(t)
	origWd, _ = os.Getwd()
	ctx := t.Context()
	if ctx == nil {
		t.Errorf("Context nil")
	} else if ctx.Err() != nil {
		t.Errorf("Context already cancelled during test: %v", ctx.Err())
	}
	// Registered first, so it runs last: the context must still be
	// live while cleanups run (it is cancelled after them).
	t.Cleanup(func() {
		if ctx.Err() != nil {
			cleanupOrder = append(cleanupOrder, "ctx-cancelled-early")
		}
	})
	t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "first") })
	t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "second") })
	tempDirPath = t.TempDir()
	if _, err := os.Stat(tempDirPath); err != nil {
		t.Errorf("TempDir does not exist: %v", err)
	}
	t.Setenv("GRAALGO_DRV1_TEST", "hello")
	if os.Getenv("GRAALGO_DRV1_TEST") != "hello" {
		t.Errorf("Setenv did not take effect")
	}
	if testing.Short() {
		t.Errorf("Short() true without -short")
	}
	if testing.Verbose() {
		t.Log("verbose mode")
	}
	if _, ok := t.Deadline(); ok {
		t.Errorf("Deadline ok without a deadline")
	}
	t.Chdir(tempDirPath)
	if wd, _ := os.Getwd(); wd != tempDirPath {
		t.Errorf("Chdir did not take effect: wd=%q", wd)
	}
}

func TestCleanupRan(t *testing.T) {
	// Cleanups run LIFO at the end of TestMethods; the context check
	// appends nothing (the context was still live during cleanups).
	if len(cleanupOrder) != 2 || cleanupOrder[0] != "second" || cleanupOrder[1] != "first" {
		t.Errorf("cleanup order = %v, want [second first]", cleanupOrder)
	}
	if _, err := os.Stat(tempDirPath); !os.IsNotExist(err) {
		t.Errorf("TempDir %q was not removed by cleanup", tempDirPath)
	}
	if v, ok := os.LookupEnv("GRAALGO_DRV1_TEST"); ok {
		t.Errorf("Setenv not restored: GRAALGO_DRV1_TEST=%q", v)
	}
	if wd, _ := os.Getwd(); wd != origWd {
		t.Errorf("Chdir not restored: wd=%q want %q", wd, origWd)
	}
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 0 {
			t.Errorf("verbose=%v: driver exit = %d, want 0:\n%s", verbose, code, out)
		}
		mustNotContain(t, out, "--- FAIL")
		if verbose {
			// Passing tests print nothing in non-verbose mode.
			mustContain(t, out, "--- PASS: TestMethods")
			mustContain(t, out, "--- PASS: TestCleanupRan")
		}
	}
}

// Regression for FE-06: a FailNow inside a subtest runs that subtest's
// cleanups, still fails the subtest (and the parent), and does not
// stop later tests from running.
func TestFailNowRunsCleanups(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

var cleanupOrder []string

func TestFailNowCleanup(t *testing.T) {
	t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "parent-cleanup") })
	t.Run("failnow", func(t *testing.T) {
		t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "sub-cleanup") })
		t.FailNow()
	})
	if len(cleanupOrder) == 0 || cleanupOrder[len(cleanupOrder)-1] != "sub-cleanup" {
		t.Errorf("subtest FailNow did not run its cleanup: %v", cleanupOrder)
	}
}

func TestFailNowCleanupRan(t *testing.T) {
	if len(cleanupOrder) != 2 || cleanupOrder[0] != "sub-cleanup" || cleanupOrder[1] != "parent-cleanup" {
		t.Errorf("cleanup order = %v, want [sub-cleanup parent-cleanup]", cleanupOrder)
	}
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 1 {
			t.Errorf("verbose=%v: driver exit = %d, want 1 (FailNow subtest must fail the package):\n%s", verbose, code, out)
		}
		mustContain(t, out, "--- FAIL: TestFailNowCleanup/failnow")
		mustContain(t, out, "--- FAIL: TestFailNowCleanup")
		mustNotContain(t, out, "--- FAIL: TestFailNowCleanupRan")
		if verbose {
			// Passing tests print nothing in non-verbose mode.
			mustContain(t, out, "--- PASS: TestFailNowCleanupRan")
		}
	}
}

// Regression for FE-06: t.Parallel cannot run in the sequential
// synthesized driver, so it must fail loudly with an explicit message
// — never silently no-op (which could deadlock a test waiting for a
// sibling parallel test) and never fail driver compilation.
func TestParallelFailsLoudly(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestParallel(t *testing.T) {
	t.Parallel()
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 1 {
			t.Errorf("verbose=%v: driver exit = %d, want 1 (Parallel must fail loudly):\n%s", verbose, code, out)
		}
		mustContain(t, out, "t.Parallel is not supported")
		mustContain(t, out, "--- FAIL: TestParallel")
	}
}

// Regression for FE-08: diagnostics render real values — floats,
// errors, slices, byte slices, maps, %q, %x, %T, %.2f — and a value
// with no rendering prints the explicit "<unsupported value>" marker,
// never the old "<value>" placeholder.
func TestDiagnosticsRendering(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import (
	"errors"
	"testing"
)

type point struct{ X, Y int }

func TestRender(t *testing.T) {
	t.Logf("float=%v err=%v slice=%v bytes=%v m=%v", 1.5, errors.New("boom"), []int{1, 2}, []byte{104, 105}, map[string]int{"b": 2, "a": 1})
	t.Logf("quoted=%q hexint=%x hexstr=%x type=%T precise=%.2f", "hi", 255, "AB", 1.5, 1.234)
	t.Logf("str=%v struct=%v", "s", point{1, 2})
	t.Errorf("got %v, want %v", 1.5, 2.5)
}
`)
	out, code := runSynthDriver(t, dir, true)
	if code != 1 {
		t.Errorf("driver exit = %d, want 1 (Errorf must fail):\n%s", code, out)
	}
	mustContain(t, out, "float=1.5")
	mustContain(t, out, "err=boom")
	mustContain(t, out, "slice=[1 2]")
	mustContain(t, out, "bytes=[104 105]")
	mustContain(t, out, "m=map[a:1 b:2]")
	mustContain(t, out, `quoted="hi"`)
	mustContain(t, out, "hexint=ff")
	mustContain(t, out, "hexstr=4142")
	mustContain(t, out, "type=float64")
	mustContain(t, out, "precise=1.23")
	mustContain(t, out, "str=s")
	mustContain(t, out, "struct=<unsupported value>")
	mustContain(t, out, "got 1.5, want 2.5")
	mustNotContain(t, out, "<value>")
}

// Regression for DRV-1b finding 1: a typed-nil error — a non-nil error
// interface holding a nil pointer, here (*os.PathError)(nil), whose
// Error method dereferences its receiver — must render as "<nil>" in
// diagnostics, never panic the driver. The old toString called
// Error() unguarded, so the panic unwound to the test's deferred
// recover and failed an otherwise passing test.
func TestTypedNilErrorRendersNil(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

func TestTypedNil(t *testing.T) {
	t.Logf("%v", (*os.PathError)(nil))
}
`)
	out, code := runSynthDriver(t, dir, true)
	if code != 0 {
		t.Errorf("driver exit = %d, want 0 (a Logf of a typed-nil error must not fail the test):\n%s", code, out)
	}
	mustContain(t, out, "<nil>")
	mustContain(t, out, "--- PASS: TestTypedNil")
	mustNotContain(t, out, "panic:")
}

// Regression for DRV-1b finding 2: a cleanup registered by another
// cleanup must run too — runCleanups drains until empty, matching
// testing — instead of being silently dropped by a single-pass drain.
func TestCleanupRegisteredByCleanup(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

var cleanupOrder []string

func TestChain(t *testing.T) {
	t.Cleanup(func() {
		cleanupOrder = append(cleanupOrder, "outer")
		t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "inner") })
	})
}

func TestChainRan(t *testing.T) {
	if len(cleanupOrder) != 2 || cleanupOrder[0] != "outer" || cleanupOrder[1] != "inner" {
		t.Errorf("cleanup order = %v, want [outer inner]", cleanupOrder)
	}
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 0 {
			t.Errorf("verbose=%v: driver exit = %d, want 0:\n%s", verbose, code, out)
		}
		mustNotContain(t, out, "--- FAIL")
		if verbose {
			mustContain(t, out, "--- PASS: TestChain")
			mustContain(t, out, "--- PASS: TestChainRan")
		}
	}
}
