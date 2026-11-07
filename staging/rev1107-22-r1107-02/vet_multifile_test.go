package main

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// installFakeRunner installs a fake graalgo-sync-run under root that
// records every invocation's argv (one line per call, args joined by
// spaces) to logPath and exits with exitCode. This lets cmdVet tests
// observe exactly how the frontend gate is invoked without building
// the Truffle backend.
func installFakeRunner(t *testing.T, root, logPath string, exitCode int) {
	t.Helper()
	dir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + strconv.Quote(logPath) + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	runner := filepath.Join(dir, "graalgo-sync-run")
	if err := os.WriteFile(runner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// readInvocations returns the recorded backend invocations, one argv
// line per call. A missing log means zero invocations.
func readInvocations(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// captureStdout runs f with os.Stdout redirected to a pipe and returns
// everything f printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestVetMultiFilePackageSingleCheck is the regression test for
// R-1107-02: `graalgo vet` false-failed on multi-file packages because
// cmdVet ran `--check` once per file, and gofrontend type-checks only
// the files it is given — so b.go using a.go's declaration failed the
// gate before the analyzers ever ran. The fix checks all of a
// package's files in one --check invocation. The old per-file loop
// would record two invocations here and fail this test.
func TestVetMultiFilePackageSingleCheck(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "invocations.log")
	installFakeRunner(t, root, logPath, 0)
	pkgDir := filepath.Join(root, "pkg")
	writeFile(t, pkgDir, "a.go", "package p\n\nfunc Foo() int { return 1 }\n")
	writeFile(t, pkgDir, "b.go", "package p\n\nfunc Bar() int { return Foo() }\n")

	var code int
	out := captureStdout(t, func() { code = cmdVet(root, []string{pkgDir}) })
	if code != 0 {
		t.Fatalf("cmdVet returned %d, want 0 (output: %s)", code, out)
	}
	invocations := readInvocations(t, logPath)
	if len(invocations) != 1 {
		t.Fatalf("got %d --check invocations, want exactly 1: %v", len(invocations), invocations)
	}
	got := invocations[0]
	for _, want := range []string{"--check", filepath.Join(pkgDir, "a.go"), filepath.Join(pkgDir, "b.go")} {
		if !strings.Contains(got, want) {
			t.Errorf("single --check invocation %q does not contain %q", got, want)
		}
	}
	if !strings.Contains(out, "vet: no issues found") {
		t.Errorf("cmdVet output %q, want it to contain %q", out, "vet: no issues found")
	}
}

// TestVetCheckFailureShortCircuits verifies a failing --check gate
// fails vet without running the analyzers.
func TestVetCheckFailureShortCircuits(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "invocations.log")
	installFakeRunner(t, root, logPath, 1)
	pkgDir := filepath.Join(root, "pkg")
	writeFile(t, pkgDir, "a.go", "package p\n\nfunc Foo() int { return 1 }\n")

	var code int
	out := captureStdout(t, func() { code = cmdVet(root, []string{pkgDir}) })
	if code != 1 {
		t.Fatalf("cmdVet returned %d, want 1 (output: %s)", code, out)
	}
	if got := readInvocations(t, logPath); len(got) != 1 {
		t.Fatalf("got %d --check invocations, want exactly 1: %v", len(got), got)
	}
	if strings.Contains(out, "vet: no issues found") {
		t.Errorf("analyzers ran despite a failing --check gate (output: %s)", out)
	}
}

// TestVetTwoPackagesCheckedSeparately verifies `graalgo vet dirA dirB`
// checks each package in its own --check invocation instead of
// failing on the package-name mismatch a single combined invocation
// would produce.
func TestVetTwoPackagesCheckedSeparately(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "invocations.log")
	installFakeRunner(t, root, logPath, 0)
	dirA := filepath.Join(root, "pkga")
	dirB := filepath.Join(root, "pkgb")
	writeFile(t, dirA, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	writeFile(t, dirB, "b.go", "package b\n\nfunc B() int { return 2 }\n")

	var code int
	out := captureStdout(t, func() { code = cmdVet(root, []string{dirA, dirB}) })
	if code != 0 {
		t.Fatalf("cmdVet returned %d, want 0 (output: %s)", code, out)
	}
	invocations := readInvocations(t, logPath)
	if len(invocations) != 2 {
		t.Fatalf("got %d --check invocations, want exactly 2: %v", len(invocations), invocations)
	}
	if !strings.Contains(invocations[0], filepath.Join(dirA, "a.go")) || strings.Contains(invocations[0], filepath.Join(dirB, "b.go")) {
		t.Errorf("first invocation should check only pkga: %q", invocations[0])
	}
	if !strings.Contains(invocations[1], filepath.Join(dirB, "b.go")) || strings.Contains(invocations[1], filepath.Join(dirA, "a.go")) {
		t.Errorf("second invocation should check only pkgb: %q", invocations[1])
	}
}

// TestVetTestFilesExcludedFromCheck verifies _test.go files are
// excluded from the --check gate (an external test package declares a
// different package name and cannot be type-checked together with the
// package) while still being parsed and analyzed by vet.Run.
func TestVetTestFilesExcludedFromCheck(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "invocations.log")
	installFakeRunner(t, root, logPath, 0)
	pkgDir := filepath.Join(root, "pkg")
	writeFile(t, pkgDir, "a.go", "package p\n\nfunc Foo() int { return 1 }\n")
	writeFile(t, pkgDir, "a_test.go", "package p_test\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n")

	var code int
	out := captureStdout(t, func() { code = cmdVet(root, []string{pkgDir}) })
	if code != 0 {
		t.Fatalf("cmdVet returned %d, want 0 (output: %s)", code, out)
	}
	invocations := readInvocations(t, logPath)
	if len(invocations) != 1 {
		t.Fatalf("got %d --check invocations, want exactly 1: %v", len(invocations), invocations)
	}
	if !strings.Contains(invocations[0], filepath.Join(pkgDir, "a.go")) {
		t.Errorf("--check invocation should contain a.go: %q", invocations[0])
	}
	if strings.Contains(invocations[0], "a_test.go") {
		t.Errorf("--check invocation should not contain _test.go files: %q", invocations[0])
	}
}

// TestVetDuplicateFileCheckedOnce verifies a file named twice on the
// command line is passed to --check once, not twice (a duplicate file
// in one --check invocation would fail on duplicate declarations).
// Only the invocation shape is asserted: vet.Run still receives the
// duplicated file list, which is pre-existing behavior.
func TestVetDuplicateFileCheckedOnce(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "invocations.log")
	installFakeRunner(t, root, logPath, 0)
	pkgDir := filepath.Join(root, "pkg")
	writeFile(t, pkgDir, "a.go", "package p\n\nfunc Foo() int { return 1 }\n")
	target := filepath.Join(pkgDir, "a.go")

	captureStdout(t, func() { _ = cmdVet(root, []string{target, target}) })
	invocations := readInvocations(t, logPath)
	if len(invocations) != 1 {
		t.Fatalf("got %d --check invocations, want exactly 1: %v", len(invocations), invocations)
	}
	if got := strings.Count(invocations[0], target); got != 1 {
		t.Errorf("--check invocation contains %q %d times, want exactly once: %q", target, got, invocations[0])
	}
}
