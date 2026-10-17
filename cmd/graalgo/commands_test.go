package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

// stubBackend replaces graalgoRun for the duration of a test, recording
// every invocation. Returns the recorded argument lists.
func stubBackend(t *testing.T, code int) *[][]string {
	t.Helper()
	var invocations [][]string
	old := graalgoRun
	graalgoRun = func(root string, args []string) int {
		cp := append([]string{}, args...)
		invocations = append(invocations, cp)
		return code
	}
	t.Cleanup(func() { graalgoRun = old })
	return &invocations
}

// TestCmdVetSingleCheckInvocation pins the C1 fix: `graalgo vet` must run
// the frontend --check ONCE over the whole file set (like cmdBuild), not
// once per file. Per-file invocations fail with undefined-symbol
// artifacts for cross-file references before the analyzers ever run.
func TestCmdVetSingleCheckInvocation(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	writeFile(t, pkg, "a.go", "package pkg\n\nfunc A() int { return 1 }\n")
	writeFile(t, pkg, "b.go", "package pkg\n\nfunc B() int { return A() }\n")

	invocations := stubBackend(t, 0)

	if code := cmdVet(root, []string{pkg}); code != 0 {
		t.Fatalf("cmdVet = %d, want 0", code)
	}
	if len(*invocations) != 1 {
		t.Fatalf("graalgoRun invoked %d times, want exactly 1: %v",
			len(*invocations), *invocations)
	}
	got := (*invocations)[0]
	want := []string{"--check",
		filepath.Join(pkg, "a.go"),
		filepath.Join(pkg, "b.go"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("check invocation = %v, want %v", got, want)
	}
}

// TestCmdVetCheckFailureShortCircuits pins the failure contract: a
// failing frontend gate returns 1 and the analyzers never run, so a
// broken package cannot produce analyzer diagnostics from partial
// translation output.
func TestCmdVetCheckFailureShortCircuits(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	writeFile(t, pkg, "a.go", "package pkg\n\nfunc A() int { return 1 }\n")
	writeFile(t, pkg, "b.go", "package pkg\n\nfunc B() int { return A() }\n")

	invocations := stubBackend(t, 1)

	if code := cmdVet(root, []string{pkg}); code != 1 {
		t.Fatalf("cmdVet = %d, want 1", code)
	}
	if len(*invocations) != 1 {
		t.Fatalf("graalgoRun invoked %d times, want exactly 1: %v",
			len(*invocations), *invocations)
	}
}

// TestCmdVetCheckIncludesTestFiles pins the file-set contract: `graalgo
// vet` checks the test files too (like `go vet`), not just the build
// files — resolveFiles returns them, and the single --check must cover
// all of them. (The _test.go fixture declares no Test* function so the
// test stays independent of the tests analyzer's import-resolution
// behavior, which is rev-vet-core's lane.)
func TestCmdVetCheckIncludesTestFiles(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	writeFile(t, pkg, "a.go", "package pkg\n\nfunc A() int { return 1 }\n")
	writeFile(t, pkg, "a_test.go", "package pkg\n\nfunc helper() int { return 1 }\n")

	invocations := stubBackend(t, 0)

	if code := cmdVet(root, []string{pkg}); code != 0 {
		t.Fatalf("cmdVet = %d, want 0", code)
	}
	if len(*invocations) != 1 {
		t.Fatalf("graalgoRun invoked %d times, want exactly 1: %v",
			len(*invocations), *invocations)
	}
	got := (*invocations)[0]
	want := []string{"--check",
		filepath.Join(pkg, "a.go"),
		filepath.Join(pkg, "a_test.go"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("check invocation = %v, want %v", got, want)
	}
}
