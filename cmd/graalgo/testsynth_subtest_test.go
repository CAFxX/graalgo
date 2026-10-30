package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests for P1-11 (review 2026-10-30): a subtest's Skip/Fatal
// must end only that subtest, mirroring go test semantics. Before the
// fix, the Skip/Fatal panic unwound through the parent into the
// per-test recover, so the parent's remaining code never ran and a
// failure after a skipped subtest was reported as ok.
//
// These tests execute the synthesized driver under the stock Go
// toolchain: the driver is self-contained (shim + inlined test bodies,
// stdlib imports only), so `go run` exercises the real shim Run/main
// verdict logic rather than asserting on generated source text.

// findGoToolchain locates a stock go binary for executing drivers.
func findGoToolchain(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, "tools", "go", "bin", "go")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no stock go toolchain available to execute the synthesized driver")
	return ""
}

// synthAndRunDriver synthesizes a driver for a single-test package and
// runs it under the stock toolchain, returning combined output and exit
// code.
func synthAndRunDriver(t *testing.T, testSrc string, verbose bool) (string, int) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/subtestfix\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package p\n")
	testFile := filepath.Join(root, "p_test.go")
	writeFile(t, root, "p_test.go", testSrc)
	importPkgNameCache = map[string]string{}
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 {
		t.Fatalf("discoverTests = %v, want exactly one test", tests)
	}
	driver, err := synthesizeTestDriver(root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "p.go")}, verbose)
	if err != nil {
		t.Fatalf("synthesizeTestDriver: %v", err)
	}
	defer os.Remove(driver)
	cmd := exec.Command(findGoToolchain(t), "run", driver)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("go run %s: %v\n%s", driver, err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// TestSubtestSkipThenParentFailure: a parent whose skipped subtest is
// followed by a failing assertion must report the failure, not ok.
func TestSubtestSkipThenParentFailure(t *testing.T) {
	out, code := synthAndRunDriver(t, `package p

import "testing"

func TestParent(t *testing.T) {
	t.Run("skipped", func(t *testing.T) {
		t.Skip("not today")
	})
	t.Errorf("parent failure after skipped subtest")
}
`, false)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero; output:\n%s", out)
	}
	if !strings.Contains(out, "--- FAIL: TestParent") {
		t.Errorf("driver output lacks parent FAIL line:\n%s", out)
	}
}

// TestSubtestFatalContinuesParent: a subtest's Fatal must fail the
// subtest (and hence the parent) but the parent must keep running.
func TestSubtestFatalContinuesParent(t *testing.T) {
	out, code := synthAndRunDriver(t, `package p

import (
	"fmt"
	"testing"
)

func TestParent(t *testing.T) {
	t.Run("fatal", func(t *testing.T) {
		t.Fatal("sub fatal")
	})
	fmt.Println("PARENT-CONTINUED")
}
`, false)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero; output:\n%s", out)
	}
	if !strings.Contains(out, "PARENT-CONTINUED") {
		t.Errorf("parent did not continue after fatal subtest; output:\n%s", out)
	}
	if !strings.Contains(out, "--- FAIL: TestParent/fatal") {
		t.Errorf("driver output lacks subtest FAIL line:\n%s", out)
	}
}

// TestSubtestSkipOnlyParentPasses: a parent whose only subtest skips
// passes in go test (the subtest alone is SKIP). The old code marked
// the parent itself skipped.
func TestSubtestSkipOnlyParentPasses(t *testing.T) {
	out, code := synthAndRunDriver(t, `package p

import "testing"

func TestParent(t *testing.T) {
	t.Run("skipped", func(t *testing.T) {
		t.Skip("not today")
	})
}
`, true)
	if code != 0 {
		t.Errorf("driver exit code = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "--- PASS: TestParent ") {
		t.Errorf("driver output lacks parent PASS line:\n%s", out)
	}
	if !strings.Contains(out, "--- SKIP: TestParent/skipped ") {
		t.Errorf("driver output lacks subtest SKIP line:\n%s", out)
	}
}

// TestParentErrorThenSkipReportsFailure: in go test, a test that records
// an error and then skips still FAILs. The old driver main checked the
// skipped flag before the failed flag in verbose mode, hiding the
// failure behind a SKIP verdict.
func TestParentErrorThenSkipReportsFailure(t *testing.T) {
	out, code := synthAndRunDriver(t, `package p

import "testing"

func TestParent(t *testing.T) {
	t.Errorf("boom")
	t.Skip("skip")
}
`, true)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero; output:\n%s", out)
	}
	if !strings.Contains(out, "--- FAIL: TestParent ") {
		t.Errorf("driver output lacks parent FAIL line:\n%s", out)
	}
}
