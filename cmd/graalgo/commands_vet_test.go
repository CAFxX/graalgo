package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeVetRoot builds a directory tree with an executable
// internal/backend/graalgo-sync-run stand-in that logs each invocation's
// argv (one line per call) to logPath and exits with $VETEXIT (default 0).
// cmdVet locates the backend as <root>/internal/backend/graalgo-sync-run,
// so pointing it at this root exercises the real gate-invocation code
// without the JVM backend.
func fakeVetRoot(t *testing.T, logPath string) string {
	t.Helper()
	root := t.TempDir()
	backendDir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> \"$VETLOG\"\n" +
		"exit \"${VETEXIT:-0}\"\n"
	backend := filepath.Join(backendDir, "graalgo-sync-run")
	if err := os.WriteFile(backend, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VETLOG", logPath)
	return root
}

// chdir switches the test process into dir for the rest of the test,
// restoring the original directory on cleanup. (t.Chdir needs go1.24;
// this module is go1.21.)
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	})
}

func writeVetFile(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readVetLog(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
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
	out := make([]byte, 0, 4096)
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			break
		}
	}
	r.Close()
	return string(out)
}

// TestCmdVetChecksAllFilesTogether is the regression test for the vet
// gate running --check once per file: a two-file package whose files
// reference each other must reach the analyzers via a single --check
// over both files. The old per-file loop invoked the backend twice
// (once per file), so it fails this test's invocation-shape assertion.
func TestCmdVetChecksAllFilesTogether(t *testing.T) {
	pkgDir := t.TempDir()
	writeVetFile(t, pkgDir, "a.go", "package p\n\nfunc Hello() string { return greet() }\n")
	writeVetFile(t, pkgDir, "b.go", "package p\n\nfunc greet() string { return \"hi\" }\n")
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	root := fakeVetRoot(t, logPath)
	chdir(t, pkgDir)

	if code := cmdVet(root, nil); code != 0 {
		t.Fatalf("cmdVet = %d, want 0 for a clean two-file package", code)
	}
	invocations := readVetLog(t, logPath)
	if len(invocations) != 1 {
		t.Fatalf("backend invoked %d times (%q), want exactly 1 invocation over all files",
			len(invocations), invocations)
	}
	if invocations[0] != "--check a.go b.go" {
		t.Fatalf("backend invocation = %q, want %q", invocations[0], "--check a.go b.go")
	}
}

// TestCmdVetReportsDiagnosticInSecondFile: the single combined --check
// must not swallow analyzer output — a genuine vet diagnostic in the
// second file must still be reported (and exit non-zero).
func TestCmdVetReportsDiagnosticInSecondFile(t *testing.T) {
	pkgDir := t.TempDir()
	writeVetFile(t, pkgDir, "a.go", "package p\n\nfunc Hello() string { return \"hi\" }\n")
	writeVetFile(t, pkgDir, "b.go", "package p\n\nimport \"fmt\"\n\nfunc Bad() { fmt.Printf(\"%d\", \"s\") }\n")
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	root := fakeVetRoot(t, logPath)
	chdir(t, pkgDir)

	var code int
	out := captureStdout(t, func() { code = cmdVet(root, nil) })
	if code == 0 {
		t.Fatalf("cmdVet = 0, want non-zero for a package with a printf mismatch in b.go")
	}
	if !strings.Contains(out, "b.go") {
		t.Fatalf("cmdVet output %q does not mention b.go, want the diagnostic for the second file", out)
	}
	if invocations := readVetLog(t, logPath); len(invocations) != 1 {
		t.Fatalf("backend invoked %d times, want exactly 1", len(invocations))
	}
}

// TestCmdVetSingleFileUnchanged: single-file packages keep the same
// gate shape as before — one --check over the one file.
func TestCmdVetSingleFileUnchanged(t *testing.T) {
	pkgDir := t.TempDir()
	writeVetFile(t, pkgDir, "only.go", "package p\n\nfunc Hello() string { return \"hi\" }\n")
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	root := fakeVetRoot(t, logPath)
	chdir(t, pkgDir)

	if code := cmdVet(root, nil); code != 0 {
		t.Fatalf("cmdVet = %d, want 0 for a clean single-file package", code)
	}
	invocations := readVetLog(t, logPath)
	if len(invocations) != 1 || invocations[0] != "--check only.go" {
		t.Fatalf("backend invocations = %q, want exactly [--check only.go]", invocations)
	}
}
