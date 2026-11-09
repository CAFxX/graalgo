package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// backendCall records one graalgoRun invocation (the driver-to-backend
// boundary). Tests stub graalgoRun and assert on these calls instead of
// running the real JVM backend.
type backendCall struct {
	root string
	args []string
}

// stubBackend replaces graalgoRun for the duration of the test, recording
// every call and delegating the exit code to fn. The original is restored
// on cleanup.
func stubBackend(t *testing.T, fn func(root string, args []string) int) *[]backendCall {
	t.Helper()
	calls := new([]backendCall)
	old := graalgoRun
	graalgoRun = func(root string, args []string) int {
		*calls = append(*calls, backendCall{root: root, args: append([]string(nil), args...)})
		return fn(root, args)
	}
	t.Cleanup(func() { graalgoRun = old })
	return calls
}

// captureStdout redirects os.Stdout for the test and returns a function
// that restores it and returns everything written so far.
func captureStdout(t *testing.T) func() string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	var buf strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	var once sync.Once
	restore := func() (out string) {
		once.Do(func() {
			_ = w.Close()
			<-done
			os.Stdout = old
			out = buf.String()
		})
		return out
	}
	t.Cleanup(func() { restore() })
	return restore
}

// writeGoFiles writes each named source file into dir and returns the
// absolute path per name.
func writeGoFiles(t *testing.T, dir string, files map[string]string) map[string]string {
	t.Helper()
	paths := make(map[string]string, len(files))
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		paths[name] = p
	}
	return paths
}

// sameFileSet reports whether got and want name the same files,
// ignoring order.
func sameFileSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := make(map[string]bool, len(want))
	for _, f := range want {
		set[f] = true
	}
	for _, f := range got {
		if !set[f] {
			return false
		}
	}
	return true
}

// packageGate returns a stub backend that succeeds a --check call only
// when its file list is exactly one of the expected package file sets.
// This models the real backend's contract (internal/frontend/main.go:
// "All files must declare the same package. They are type-checked
// together"), so a driver that checks files one at a time fails the
// gate exactly like the real backend would.
func packageGate(sets ...[]string) func(root string, args []string) int {
	return func(root string, args []string) int {
		if len(args) < 1 || args[0] != "--check" {
			return 1
		}
		for _, want := range sets {
			if sameFileSet(args[1:], want) {
				return 0
			}
		}
		return 1
	}
}

// R20B-01: `graalgo vet` must check a multi-file package once, with all
// of its files together. Per-file --check fails on every multi-file
// package (cross-file references are undefined in a single file), so the
// analyzers never run.
func TestCmdVetMultiFilePackage(t *testing.T) {
	dir := t.TempDir()
	paths := writeGoFiles(t, dir, map[string]string{
		"a.go": "package main\n\nfunc helper() int { return 1 }\n",
		"b.go": "package main\n\nfunc main() { println(helper()) }\n",
	})
	want := []string{paths["a.go"], paths["b.go"]}
	calls := stubBackend(t, packageGate(want))
	out := captureStdout(t)
	code := cmdVet("test-root", []string{dir})
	got := out()
	if code != 0 {
		t.Fatalf("cmdVet = %d, want 0; output:\n%s", code, got)
	}
	if len(*calls) != 1 {
		t.Fatalf("backend called %d times, want 1 (--check once for the whole package): %+v", len(*calls), *calls)
	}
	if (*calls)[0].args[0] != "--check" || !sameFileSet((*calls)[0].args[1:], want) {
		t.Errorf("gate call = %v, want [--check %v]", (*calls)[0].args, want)
	}
	if !strings.Contains(got, "vet: no issues found") {
		t.Errorf("output missing 'vet: no issues found':\n%s", got)
	}
}

// R20B-01: with the gate fixed, the analyzers actually run: a printf
// mismatch spread across two files is reported.
func TestCmdVetAnalyzersRun(t *testing.T) {
	dir := t.TempDir()
	paths := writeGoFiles(t, dir, map[string]string{
		"a.go": "package main\n\nimport \"fmt\"\n\nfunc greet() { fmt.Printf(\"%d\", \"hello\") }\n",
		"b.go": "package main\n\nfunc main() { greet() }\n",
	})
	want := []string{paths["a.go"], paths["b.go"]}
	stubBackend(t, packageGate(want))
	out := captureStdout(t)
	code := cmdVet("test-root", []string{dir})
	got := out()
	if code != 1 {
		t.Fatalf("cmdVet = %d, want 1 (printf diagnostic); output:\n%s", code, got)
	}
	if !strings.Contains(got, "expects integer") {
		t.Errorf("output missing the printf diagnostic ('expects integer'):\n%s", got)
	}
}

// R20B-01: a package that fails the frontend gate still fails vet, and
// the analyzers are not run on a package that did not type-check.
func TestCmdVetBrokenPackageGateFails(t *testing.T) {
	dir := t.TempDir()
	writeGoFiles(t, dir, map[string]string{
		"a.go": "package main\n\nfunc helper() int { return 1 }\n",
		"b.go": "package main\n\nfunc main() { println(missing()) }\n",
	})
	stubBackend(t, func(root string, args []string) int { return 1 })
	out := captureStdout(t)
	code := cmdVet("test-root", []string{dir})
	got := out()
	if code != 1 {
		t.Fatalf("cmdVet = %d, want 1 (gate failed); output:\n%s", code, got)
	}
	if strings.Contains(got, "vet: no issues found") {
		t.Errorf("gate failure must not reach the 'no issues found' report:\n%s", got)
	}
}

// R20B-01: vetting two packages runs one --check per package, not one
// per file and not one merged call across packages.
func TestCmdVetMultiplePackages(t *testing.T) {
	dir1 := t.TempDir()
	p1 := writeGoFiles(t, dir1, map[string]string{
		"a.go": "package p1\n\nfunc A() int { return 1 }\n",
		"b.go": "package p1\n\nfunc B() int { return A() }\n",
	})
	dir2 := t.TempDir()
	p2 := writeGoFiles(t, dir2, map[string]string{
		"c.go": "package p2\n\nfunc C() int { return 2 }\n",
		"d.go": "package p2\n\nfunc D() int { return C() }\n",
	})
	set1 := []string{p1["a.go"], p1["b.go"]}
	set2 := []string{p2["c.go"], p2["d.go"]}
	calls := stubBackend(t, packageGate(set1, set2))
	out := captureStdout(t)
	code := cmdVet("test-root", []string{dir1, dir2})
	got := out()
	if code != 0 {
		t.Fatalf("cmdVet = %d, want 0; output:\n%s", code, got)
	}
	if len(*calls) != 2 {
		t.Fatalf("backend called %d times, want 2 (one --check per package): %+v", len(*calls), *calls)
	}
}

// R20B-01: _test.go files are not part of the --check gate (an external
// test package declares a different package name, which the frontend
// rejects), but they are still analyzed by vet.Run. This test asserts
// only the gate calls, not cmdVet's exit code: vet.Run type-checks the
// mixed foo + foo_test file set as one package (the known F4
// franken-package issue, out of scope here), which yields a spurious
// diagnostic and a non-zero exit regardless of the gate.
func TestCmdVetTestFilesExcludedFromGate(t *testing.T) {
	dir := t.TempDir()
	paths := writeGoFiles(t, dir, map[string]string{
		"a.go":      "package foo\n\nfunc Helper() int { return 1 }\n",
		"a_test.go": "package foo_test\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) {}\n",
	})
	calls := stubBackend(t, func(root string, args []string) int { return 0 })
	out := captureStdout(t)
	_ = cmdVet("test-root", []string{dir})
	_ = out()
	var gateCalls [][]string
	for _, c := range *calls {
		if len(c.args) > 0 && c.args[0] == "--check" {
			gateCalls = append(gateCalls, c.args[1:])
		}
	}
	if len(gateCalls) != 1 {
		t.Fatalf("want exactly 1 --check call, got %d: %+v", len(gateCalls), *calls)
	}
	if !sameFileSet(gateCalls[0], []string{paths["a.go"]}) {
		t.Errorf("gate files = %v, want only [%s] (no _test.go)", gateCalls[0], paths["a.go"])
	}
}
