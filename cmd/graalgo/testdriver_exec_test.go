package main

// Behavioral fidelity tests for the generated test driver.
//
// These tests synthesize a driver from a fixture package and execute it
// under the gc toolchain (go run), asserting exit codes, output, and
// side effects. That is the honest proof: the shim's Cleanup/TempDir/
// Setenv/Parallel/TestMain semantics are validated by real execution,
// not by string-matching the generator. Every assertion below is
// capable of failing: removing the corresponding shim behavior breaks
// the test.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goBinary resolves the gc toolchain for driver execution.
func goBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GRAALGO_TEST_GO"); p != "" {
		return p
	}
	if h, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(h, "tools", "go", "bin", "go")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	t.Skip("no Go toolchain found (set GRAALGO_TEST_GO)")
	return ""
}

// runDriver executes driver source with `go run` and returns its combined
// output and exit code. extraEnv adds environment variables for the
// driver process.
func runDriver(t *testing.T, src string, extraEnv map[string]string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	drv := filepath.Join(dir, "driver.go")
	if err := os.WriteFile(drv, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBinary(t), "run", drv)
	// A hermetic build: no module context, no toolchain switching, no
	// ambient build flags leaking in. GOTMPDIR is left for the caller:
	// TempDir tests set it deliberately.
	cmd.Env = []string{"GOTOOLCHAIN=local", "GOFLAGS=", "GOWORK=off", "PATH=" + os.Getenv("PATH")}
	for _, kv := range os.Environ() {
		k := kv
		if i := strings.Index(kv, "="); i >= 0 {
			k = kv[:i]
		}
		switch k {
		case "GOTOOLCHAIN", "GOFLAGS", "GOWORK", "PATH", "GOTMPDIR",
			"GRAALGO_FIXTURE_ORDER_FILE", "GRAALGO_SHIM_FIXTURE_SETENV":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("go run: %v\n%s", err, out)
		}
	}
	return string(out), code
}

// fixtureDir writes a fixture package (go.mod + sources) and returns its
// directory.
func fixtureDir(t *testing.T, module string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+module+"\n\ngo 1.21\n")
	for name, content := range files {
		writeFile(t, root, name, content)
	}
	return root
}

// fixtureTests discovers all tests of one fixture file, failing the test
// on error.
func fixtureTests(t *testing.T, path string) []string {
	t.Helper()
	tests, err := discoverTests(path)
	if err != nil {
		t.Fatal(err)
	}
	return tests
}

const shimFixtureMethods = `package fid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var cleanupOrder []string

func TestCleanupLIFO(t *testing.T) {
	t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "a") })
	t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "b") })
	t.Cleanup(func() { cleanupOrder = append(cleanupOrder, "c") })
}

func TestCleanupLIFOCheck(t *testing.T) {
	if got, want := strings.Join(cleanupOrder, ","), "c,b,a"; got != want {
		t.Fatalf("cleanup order = %q, want %q (LIFO)", got, want)
	}
}

func TestTempDirLifecycle(t *testing.T) {
	var dir string
	t.Cleanup(func() {
		// Registered first, so it runs after TempDir's own removal:
		// verifies the directory is really gone at test end.
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("TempDir %q was not removed", dir)
		}
	})
	dir = t.TempDir()
	if dir == "" {
		t.Fatal("TempDir returned an empty path")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("TempDir %q does not exist: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("TempDir %q is not writable: %v", dir, err)
	}
	if gotmp := os.Getenv("GOTMPDIR"); gotmp != "" {
		if rel, err := filepath.Rel(gotmp, dir); err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("TempDir %q is not under GOTMPDIR %q", dir, gotmp)
		}
	}
}

var nestedParentDir string

func TestTempDirNested(t *testing.T) {
	nestedParentDir = t.TempDir()
	t.Run("sub", func(t *testing.T) {
		var sub string
		t.Cleanup(func() {
			if _, err := os.Stat(sub); !os.IsNotExist(err) {
				t.Errorf("subtest TempDir %q was not removed at subtest end", sub)
			}
			if _, err := os.Stat(nestedParentDir); err != nil {
				t.Errorf("parent TempDir %q removed before the parent ended: %v", nestedParentDir, err)
			}
		})
		sub = t.TempDir()
		if sub == nestedParentDir {
			t.Fatal("subtest TempDir collides with the parent's")
		}
	})
}

func TestTempDirNestedCheck(t *testing.T) {
	if _, err := os.Stat(nestedParentDir); !os.IsNotExist(err) {
		t.Errorf("parent TempDir %q was not removed at test end", nestedParentDir)
	}
}

const setenvKey = "GRAALGO_SHIM_FIXTURE_SETENV"

func TestSetenvRestore(t *testing.T) {
	os.Unsetenv(setenvKey)
	t.Cleanup(func() {
		// Registered before Setenv's own cleanup, so it runs after the
		// restore: verifies the variable is really gone.
		if v, ok := os.LookupEnv(setenvKey); ok {
			t.Errorf("env not restored, still %q=%q", setenvKey, v)
		}
	})
	t.Setenv(setenvKey, "hello")
	if got := os.Getenv(setenvKey); got != "hello" {
		t.Fatalf("Setenv did not take effect: got %q", got)
	}
}

func TestSetenvRestoreExisting(t *testing.T) {
	os.Setenv(setenvKey, "outer")
	t.Cleanup(func() {
		if got := os.Getenv(setenvKey); got != "outer" {
			t.Errorf("env not restored to previous value: got %q", got)
		}
	})
	t.Setenv(setenvKey, "inner")
	if got := os.Getenv(setenvKey); got != "inner" {
		t.Fatalf("Setenv did not take effect: got %q", got)
	}
}

func TestParallelNoop(t *testing.T) {
	t.Parallel()
	// Parallel is a documented scheduling no-op on the sequential
	// driver: the test still runs exactly once, to completion.
}

func TestParallelDoublePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("second t.Parallel did not panic")
		}
	}()
	t.Parallel()
}

func TestSetenvAfterParallelPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("t.Setenv after t.Parallel did not panic")
		} else if s, ok := r.(string); !ok || !strings.Contains(s, "can not use t.Parallel") {
			t.Errorf("unexpected panic: %v", r)
		}
	}()
	t.Setenv(setenvKey, "x")
}

func TestParallelAfterSetenvPanics(t *testing.T) {
	t.Setenv(setenvKey, "x")
	defer func() {
		if recover() == nil {
			t.Error("t.Parallel after t.Setenv did not panic")
		}
	}()
	t.Parallel()
}

func TestSetenvParallelAncestorPanics(t *testing.T) {
	t.Parallel()
	t.Run("sub", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("t.Setenv with a parallel ancestor did not panic")
			}
		}()
		t.Setenv(setenvKey, "x")
	})
}
`

// TestDriverShimMethods executes a fixture exercising Cleanup, TempDir,
// Setenv and Parallel under gc and asserts gc-faithful behavior.
func TestDriverShimMethods(t *testing.T) {
	root := fixtureDir(t, "example.com/shimfix", map[string]string{
		"p.go":      "package fid\n",
		"p_test.go": shimFixtureMethods,
	})
	testFile := filepath.Join(root, "p_test.go")
	driver := synthDriverFile(t, root,
		[]fileTests{{file: testFile, tests: fixtureTests(t, testFile)}},
		[]string{filepath.Join(root, "p.go")}, true, "")
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	gotmp := t.TempDir()
	out, code := runDriver(t, string(src), map[string]string{"GOTMPDIR": gotmp})
	if code != 0 {
		t.Fatalf("driver exited %d, want 0:\n%s", code, out)
	}
	for _, name := range []string{
		"TestCleanupLIFO", "TestCleanupLIFOCheck",
		"TestTempDirLifecycle", "TestTempDirNested", "TestTempDirNestedCheck",
		"TestSetenvRestore", "TestSetenvRestoreExisting",
		"TestParallelNoop", "TestParallelDoublePanics",
		"TestSetenvAfterParallelPanics", "TestParallelAfterSetenvPanics",
		"TestSetenvParallelAncestorPanics",
	} {
		if !strings.Contains(out, "--- PASS: "+name) {
			t.Errorf("missing --- PASS for %s:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "\nPASS\n") {
		t.Errorf("missing PASS summary:\n%s", out)
	}
	// The temp dirs must be gone: TempDir's removal is unconditional.
	entries, _ := os.ReadDir(gotmp)
	if len(entries) != 0 {
		t.Errorf("GOTMPDIR not cleaned: %v", entries)
	}
}

const shimFixtureFatalPanic = `package fid

import "testing"

var fatalCleanupRan = false
var panicCleanupRan = false

func TestCleanupOnFatal(t *testing.T) {
	t.Cleanup(func() { fatalCleanupRan = true })
	t.Fatal("boom")
}

func TestCleanupOnPanic(t *testing.T) {
	t.Cleanup(func() { panicCleanupRan = true })
	panic("kaboom")
}

func TestCleanupChecks(t *testing.T) {
	if !fatalCleanupRan {
		t.Error("cleanup did not run when the test called Fatal")
	}
	if !panicCleanupRan {
		t.Error("cleanup did not run when the test panicked")
	}
}
`

// TestDriverCleanupOnFatalAndPanic pins that cleanups drain when a test
// ends via Fatal or via a panic, and that the package continues with the
// remaining tests (exit 1 overall, but the check test passes).
func TestDriverCleanupOnFatalAndPanic(t *testing.T) {
	root := fixtureDir(t, "example.com/shimfatal", map[string]string{
		"p.go":      "package fid\n",
		"p_test.go": shimFixtureFatalPanic,
	})
	testFile := filepath.Join(root, "p_test.go")
	driver := synthDriverFile(t, root,
		[]fileTests{{file: testFile, tests: fixtureTests(t, testFile)}},
		[]string{filepath.Join(root, "p.go")}, true, "")
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	out, code := runDriver(t, string(src), nil)
	if code != 1 {
		t.Errorf("driver exited %d, want 1 (two tests fail):\n%s", code, out)
	}
	for _, want := range []string{
		"--- FAIL: TestCleanupOnFatal",
		"--- FAIL: TestCleanupOnPanic",
		"--- PASS: TestCleanupChecks",
		"panic: kaboom",
		"\nFAIL\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

const shimFixtureTestMain = `package fidmain

import (
	"os"
	"strings"
	"testing"
)

var order []string

func TestMain(m *testing.M) {
	order = append(order, "setup")
	code := m.Run()
	order = append(order, "teardown")
	os.WriteFile(os.Getenv("GRAALGO_FIXTURE_ORDER_FILE"), []byte(strings.Join(order, ",")), 0o644)
	os.Exit(code)
}

func TestUsesSetup(t *testing.T) {
	if len(order) == 0 || order[0] != "setup" {
		t.Fatal("TestMain setup did not run before the test")
	}
	order = append(order, "test")
}
`

// TestDriverTestMain pins the TestMain contract: setup runs before the
// tests, m.Run()'s exit code propagates, teardown runs after, and the
// output matches go test.
func TestDriverTestMain(t *testing.T) {
	root := fixtureDir(t, "example.com/tmmain", map[string]string{
		"p.go":      "package fidmain\n",
		"p_test.go": shimFixtureTestMain,
	})
	testFile := filepath.Join(root, "p_test.go")
	has, err := hasTestMain(testFile)
	if err != nil || !has {
		t.Fatalf("hasTestMain = %v, %v", has, err)
	}
	driver := synthDriverFile(t, root,
		[]fileTests{{file: testFile, tests: fixtureTests(t, testFile)}},
		[]string{filepath.Join(root, "p.go")}, true, testFile)
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	orderFile := filepath.Join(t.TempDir(), "order")
	out, code := runDriver(t, string(src),
		map[string]string{"GRAALGO_FIXTURE_ORDER_FILE": orderFile})
	if code != 0 {
		t.Fatalf("driver exited %d, want 0:\n%s", code, out)
	}
	for _, want := range []string{
		"=== RUN   TestUsesSetup",
		"--- PASS: TestUsesSetup",
		"\nPASS\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	ord, err := os.ReadFile(orderFile)
	if err != nil {
		t.Fatalf("TestMain did not record the order file: %v", err)
	}
	if string(ord) != "setup,test,teardown" {
		t.Errorf("TestMain order = %q, want setup,test,teardown", ord)
	}
}

const shimFixtureTestMainFail = `package fidmain

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(m.Run()) }

func TestFails(t *testing.T) { t.Error("boom") }
`

// TestDriverTestMainFailure pins that a failing test under TestMain
// yields exit code 1 with the FAIL summary.
func TestDriverTestMainFailure(t *testing.T) {
	root := fixtureDir(t, "example.com/tmmainfail", map[string]string{
		"p.go":      "package fidmain\n",
		"p_test.go": shimFixtureTestMainFail,
	})
	testFile := filepath.Join(root, "p_test.go")
	driver := synthDriverFile(t, root,
		[]fileTests{{file: testFile, tests: fixtureTests(t, testFile)}},
		[]string{filepath.Join(root, "p.go")}, true, testFile)
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	out, code := runDriver(t, string(src), nil)
	if code != 1 {
		t.Errorf("driver exited %d, want 1:\n%s", code, out)
	}
	for _, want := range []string{"--- FAIL: TestFails", "\nFAIL\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

const shimFixtureTestMainNoRun = `package fidmain

import "testing"

func TestMain(m *testing.M) {}

func TestNeverRuns(t *testing.T) { t.Fatal("must not run") }
`

// TestDriverTestMainWithoutRun pins testing's contract: a TestMain that
// never calls m.Run() runs no tests and exits 0.
func TestDriverTestMainWithoutRun(t *testing.T) {
	root := fixtureDir(t, "example.com/tmmainnorun", map[string]string{
		"p.go":      "package fidmain\n",
		"p_test.go": shimFixtureTestMainNoRun,
	})
	testFile := filepath.Join(root, "p_test.go")
	driver := synthDriverFile(t, root,
		[]fileTests{{file: testFile, tests: fixtureTests(t, testFile)}},
		[]string{filepath.Join(root, "p.go")}, true, testFile)
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	out, code := runDriver(t, string(src), nil)
	if code != 0 {
		t.Errorf("driver exited %d, want 0:\n%s", code, out)
	}
	if strings.Contains(out, "=== RUN") {
		t.Errorf("TestMain without m.Run() ran tests:\n%s", out)
	}
}
