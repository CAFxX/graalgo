package main

import (
	"fmt"
	"strings"
	"testing"
)

// Regression tests for A-P1-1 (REVIEW-2026-11-11-03): `graalgo test`
// failed every package that defines TestMain. discoverTests selected
// TestMain as an ordinary test, nothing handled *testing.M, and the
// synthesized driver did not compile (dropped `testing` import +
// TestMain(tt) call with a *graalgoT). The fix synthesizes a minimal
// *graalgoM shim and calls TestMain as the driver's entry point, the
// way `go test` does (its m.Run() drives the selected tests).
//
// The driver is plain Go, so these tests run it under gc via
// runSynthDriver (testsub_test.go) — exercising exactly the shim and
// driver result logic — plus one end-to-end cmdTest test with a fake
// backend that runs the driver under gc.

// TestTestMainRunsTests is the core regression test: a package whose
// TestMain calls os.Exit(m.Run()) must test green, with the test's
// result lines in the driver output.
func TestTestMainRunsTests(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestHello(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("bad")
	}
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 0 {
			t.Errorf("verbose=%v: driver exit = %d, want 0 (TestMain + passing test must pass):\n%s", verbose, code, out)
		}
		mustContain(t, out, "--- PASS: TestHello ")
		if verbose {
			mustContain(t, out, "=== RUN   TestHello")
		}
	}
}

// TestTestMainSetupBeforeRun pins that TestMain's setup runs before the
// tests: the test fails unless TestMain set the flag. (If TestMain were
// skipped, the test — and the package — would fail.)
func TestTestMainSetupBeforeRun(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

var ready bool

func TestMain(m *testing.M) {
	println("setup ran")
	ready = true
	os.Exit(m.Run())
}

func TestUsesSetup(t *testing.T) {
	if !ready {
		t.Fatal("setup did not run before the test")
	}
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 0 {
			t.Errorf("verbose=%v: driver exit = %d, want 0 (setup must run, test must pass):\n%s", verbose, code, out)
		}
		mustContain(t, out, "setup ran")
		mustContain(t, out, "--- PASS: TestUsesSetup ")
	}
}

// TestTestMainFailingTestExitCode pins that a failing test still fails
// the package when TestMain drives it: m.Run()'s exit code must
// propagate through TestMain's os.Exit(m.Run()).
func TestTestMainFailingTestExitCode(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestBad(t *testing.T) {
	t.Errorf("boom")
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 1 {
			t.Errorf("verbose=%v: driver exit = %d, want 1 (failing test must fail the package):\n%s", verbose, code, out)
		}
		mustContain(t, out, "--- FAIL: TestBad ")
		mustContain(t, out, "FAIL")
	}
}

// TestTestMainWithoutRunSkipsTests pins the go-test contract that
// tests run only when TestMain calls m.Run(): a TestMain that returns
// without calling Run must run no tests and exit 0 — even though the
// test would fail if it ran.
func TestTestMainWithoutRunSkipsTests(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestMain(m *testing.M) {
	println("setup ran")
}

func TestNeverRuns(t *testing.T) {
	t.Fatal("this test must not run")
}
`)
	for _, verbose := range []bool{true, false} {
		out, code := runSynthDriver(t, dir, verbose)
		if code != 0 {
			t.Errorf("verbose=%v: driver exit = %d, want 0 (TestMain without m.Run() runs no tests):\n%s", verbose, code, out)
		}
		mustContain(t, out, "setup ran")
		mustNotContain(t, out, "--- FAIL: TestNeverRuns")
		mustNotContain(t, out, "--- PASS: TestNeverRuns")
	}
}

// TestTestMainDriverShape pins the synthesized driver's shape: the
// generated main calls TestMain with a *graalgoM (never with a
// *graalgoT), and the shim provides M.Run.
func TestTestMainDriverShape(t *testing.T) {
	dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestHello(t *testing.T) {}
`)
	testFile := dir + "/a_test.go"
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, dir,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{dir + "/a.go"})
	if !strings.Contains(src, "type graalgoM struct") {
		t.Errorf("driver has no graalgoM shim:\n%s", src)
	}
	if !strings.Contains(src, "func (m *graalgoM) Run() int") {
		t.Errorf("driver has no graalgoM.Run:\n%s", src)
	}
	if !strings.Contains(src, "TestMain(m)") {
		t.Errorf("driver main does not call TestMain with the *graalgoM:\n%s", src)
	}
	if strings.Contains(src, "TestMain(tt)") {
		t.Errorf("driver main calls TestMain with a *graalgoT:\n%s", src)
	}
	if strings.Contains(src, "testing.M") {
		t.Errorf("driver still references *testing.M (the testing import is dropped):\n%s", src)
	}
}

// gcBackendRoot returns a fake backend root whose launcher runs the
// driver under gc: the launcher receives
// --test-package-dir=<abs> <driver.go> and execs `go run` on the .go
// argument. This exercises cmdTest end to end (discovery, synthesis,
// exit code) without the Truffle backend.
func gcBackendRoot(t *testing.T) string {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  case "$a" in
    *.go) exec %s run "$a" ;;
  esac
done
exit 1
`, goBinary(t))
	return fakeBackendRoot(t, script)
}

// TestCmdTestTestMainEndToEnd runs `graalgo test` with the fake gc
// backend over packages with a TestMain: pass, fail, -run matching
// nothing (TestMain must still run), and multiple TestMains (loud
// error, like `go test`).
func TestCmdTestTestMainEndToEnd(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		root := gcBackendRoot(t)
		dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestHello(t *testing.T) {}
`)
		var code int
		out := captureOutput(t, func() {
			code = cmdTest(root, []string{dir})
		})
		if code != 0 {
			t.Errorf("cmdTest exit = %d, want 0:\n%s", code, out)
		}
		mustContain(t, out, "ok  \t")
	})
	t.Run("fail", func(t *testing.T) {
		root := gcBackendRoot(t)
		dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestBad(t *testing.T) {
	t.Errorf("boom")
}
`)
		var code int
		out := captureOutput(t, func() {
			code = cmdTest(root, []string{dir})
		})
		if code != 1 {
			t.Errorf("cmdTest exit = %d, want 1:\n%s", code, out)
		}
		mustContain(t, out, "--- FAIL: TestBad ")
		mustContain(t, out, "FAIL")
	})
	t.Run("run-matches-nothing", func(t *testing.T) {
		root := gcBackendRoot(t)
		dir := writeSubtestPkg(t, `package subtest

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	println("setup ran")
	os.Exit(m.Run())
}

func TestHello(t *testing.T) {}
`)
		var code int
		out := captureOutput(t, func() {
			code = cmdTest(root, []string{"-run=Nomatch", dir})
		})
		if code != 0 {
			t.Errorf("cmdTest exit = %d, want 0:\n%s", code, out)
		}
		// Like `go test`, TestMain still runs when -run matches no
		// test — its setup must execute even though no test runs.
		mustContain(t, out, "setup ran")
		mustContain(t, out, "[no tests to run]")
	})
	t.Run("multiple", func(t *testing.T) {
		root := gcBackendRoot(t)
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", "module example.com/subtest\n\ngo 1.21\n")
		writeFile(t, dir, "a.go", "package subtest\n")
		writeFile(t, dir, "a_test.go", `package subtest

import "testing"

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestA(t *testing.T) {}
`)
		writeFile(t, dir, "b_test.go", `package subtest

import "testing"

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestB(t *testing.T) {}
`)
		var code int
		out := captureOutput(t, func() {
			code = cmdTest(root, []string{dir})
		})
		if code != 1 {
			t.Errorf("cmdTest exit = %d, want 1 (multiple TestMain must fail):\n%s", code, out)
		}
		mustContain(t, out, "multiple definitions of TestMain")
	})
}
