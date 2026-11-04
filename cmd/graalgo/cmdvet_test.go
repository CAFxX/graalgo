package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vetWriteFile creates dir/name with content, creating parent dirs.
// (Named distinctly from the testsynth helpers to avoid any clash.)
func vetWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func vetGroupFiles(t *testing.T, groups []vetCheckGroup) [][]string {
	t.Helper()
	var got [][]string
	for _, g := range groups {
		got = append(got, g.files)
	}
	return got
}

func TestGroupVetCheckFiles(t *testing.T) {
	dir := t.TempDir()
	vetWriteFile(t, dir, "a.go", "package foo\n\nfunc Helper() int { return 1 }\n")
	vetWriteFile(t, dir, "b.go", "package foo\n\nfunc Use() int { return Helper() }\n")
	vetWriteFile(t, dir, "c_test.go", "package foo_test\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n")
	vetWriteFile(t, dir, "sub/d.go", "package foo\n\nfunc Other() int { return 2 }\n")
	// Unparseable files must each form a singleton group: an empty
	// file has no package clause, and a missing file cannot be read.
	vetWriteFile(t, dir, "broken.go", "")
	missing := filepath.Join(dir, "missing.go")

	files := []string{
		filepath.Join(dir, "a.go"),
		filepath.Join(dir, "b.go"),
		filepath.Join(dir, "c_test.go"),
		filepath.Join(dir, "sub", "d.go"),
		filepath.Join(dir, "broken.go"),
		missing,
	}
	got := vetGroupFiles(t, groupVetCheckFiles(files))
	want := [][]string{
		{filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")},
		{filepath.Join(dir, "c_test.go")},
		{filepath.Join(dir, "sub", "d.go")},
		{filepath.Join(dir, "broken.go")},
		{missing},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d groups %v, want %d groups %v", len(got), got, len(want), want)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// vetFakeRoot builds a fake graalgo root whose backend runner just logs
// its argv (one line per invocation) and exits with exitCode. It
// stands in for internal/backend/graalgo-sync-run, which cmdVet shells
// out to via graalgoRun; the grouping under test is exactly which argv
// each invocation carries, so a logging stub is the honest seam.
func vetFakeRoot(t *testing.T, exitCode int) (root, logPath string) {
	t.Helper()
	root = t.TempDir()
	logPath = filepath.Join(root, "invocations.log")
	backendDir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> \"$VET_GATE_LOG\"\nexit %d\n", exitCode)
	if err := os.WriteFile(filepath.Join(backendDir, "graalgo-sync-run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VET_GATE_LOG", logPath)
	return root, logPath
}

func vetReadInvocations(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestCmdVetMultiFilePackage(t *testing.T) {
	// Regression test for the vet gate: file B uses a symbol declared
	// in file A. The old per-file gate ran --check on B alone and
	// failed with "undefined: Helper"; the grouped gate runs one
	// --check over both files and passes.
	pkgDir := t.TempDir()
	vetWriteFile(t, pkgDir, "a.go", "package foo\n\nfunc Helper() int { return 1 }\n")
	vetWriteFile(t, pkgDir, "b.go", "package foo\n\nfunc Use() int { return Helper() }\n")
	root, logPath := vetFakeRoot(t, 0)
	// Pass the package directory: resolveFiles globs it to absolute
	// paths, so no working-directory change is needed.
	if code := cmdVet(root, []string{pkgDir}); code != 0 {
		t.Fatalf("cmdVet returned %d, want 0", code)
	}
	invocations := vetReadInvocations(t, logPath)
	want := "--check " + filepath.Join(pkgDir, "a.go") + " " + filepath.Join(pkgDir, "b.go")
	if len(invocations) != 1 {
		t.Fatalf("got %d backend invocations %v, want 1 grouped invocation", len(invocations), invocations)
	}
	if invocations[0] != want {
		t.Fatalf("invocation = %q, want %q", invocations[0], want)
	}
}

func TestCmdVetSeparatesExternalTestPackage(t *testing.T) {
	// An external test package (foo_test alongside foo) must not be
	// merged into the package's own --check: the frontend requires a
	// single package name per invocation. It gets its own group.
	pkgDir := t.TempDir()
	vetWriteFile(t, pkgDir, "a.go", "package foo\n\nfunc Helper() int { return 1 }\n")
	vetWriteFile(t, pkgDir, "b.go", "package foo\n\nfunc Use() int { return Helper() }\n")
	vetWriteFile(t, pkgDir, "c_test.go", "package foo_test\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n")
	root, logPath := vetFakeRoot(t, 0)
	if code := cmdVet(root, []string{pkgDir}); code != 0 {
		t.Fatalf("cmdVet returned %d, want 0", code)
	}
	invocations := vetReadInvocations(t, logPath)
	want := []string{
		"--check " + filepath.Join(pkgDir, "a.go") + " " + filepath.Join(pkgDir, "b.go"),
		"--check " + filepath.Join(pkgDir, "c_test.go"),
	}
	if len(invocations) != len(want) {
		t.Fatalf("got %d backend invocations %v, want %v", len(invocations), invocations, want)
	}
	for i := range want {
		if invocations[i] != want[i] {
			t.Fatalf("invocation %d = %q, want %q", i, invocations[i], want[i])
		}
	}
}

func TestCmdVetReportsAnalyzerDiagnostic(t *testing.T) {
	// Per-group analysis must still report real issues: a TestMain
	// with the wrong signature is flagged by the analyzers, so vet
	// exits 1 with a diagnostic (exit code only is asserted here;
	// cmdVet writes diagnostics to stderr, like go vet). The
	// fixture is deliberately import-free so the check cannot depend
	// on importer resolution.
	pkgDir := t.TempDir()
	vetWriteFile(t, pkgDir, "a.go", "package foo\n\nfunc Helper() int { return 1 }\n")
	vetWriteFile(t, pkgDir, "a_test.go", "package foo\n\nfunc TestMain() {}\n")
	root, _ := vetFakeRoot(t, 0)
	if code := cmdVet(root, []string{pkgDir}); code != 1 {
		t.Fatalf("cmdVet returned %d, want 1 on analyzer diagnostic", code)
	}
}

func TestCmdVetGateFailureStillFails(t *testing.T) {
	// A genuinely failing gate must still fail vet, with the grouped
	// invocation shape (one --check for the whole package, not one
	// per file), and must not fall through to the analyzers.
	pkgDir := t.TempDir()
	vetWriteFile(t, pkgDir, "a.go", "package foo\n\nfunc Helper() int { return 1 }\n")
	vetWriteFile(t, pkgDir, "b.go", "package foo\n\nfunc Use() int { return Helper() }\n")
	root, logPath := vetFakeRoot(t, 1)
	if code := cmdVet(root, []string{pkgDir}); code != 1 {
		t.Fatalf("cmdVet returned %d, want 1 on gate failure", code)
	}
	invocations := vetReadInvocations(t, logPath)
	want := "--check " + filepath.Join(pkgDir, "a.go") + " " + filepath.Join(pkgDir, "b.go")
	if len(invocations) != 1 || invocations[0] != want {
		t.Fatalf("got invocations %v, want [%q]", invocations, want)
	}
}
