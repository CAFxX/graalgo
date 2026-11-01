package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBackendRoot builds a stand-in graalgo installation whose
// graalgo-sync-run records every invocation to a file and, for
// --emit-wire=PATH arguments, writes a minimal wire payload so
// cmdBuild's frontend gate succeeds. It lets command-level tests pin
// driver behavior (rejections, per-group checking, output naming)
// without a real backend.
func fakeBackendRoot(t *testing.T) (root, record string) {
	t.Helper()
	fake := t.TempDir()
	binDir := filepath.Join(fake, "internal", "backend")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	record = filepath.Join(fake, "record.txt")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    --emit-wire=*) printf '{}' > \"${a#--emit-wire=}\" ;;\n" +
		"  esac\n" +
		"done\n" +
		"echo \"$*\" >> \"$GRAALGO_TEST_RECORD\"\n" +
		"exit 0\n"
	runner := filepath.Join(binDir, "graalgo-sync-run")
	if err := os.WriteFile(runner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAALGO_TEST_RECORD", record)
	return fake, record
}

// captureStderr redirects os.Stderr to a temp file for the duration of
// f and returns what f printed there.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	tmp, err := os.CreateTemp("", "graalgo-stderr-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpName := tmp.Name()
	os.Stderr = tmp
	f()
	os.Stderr = old
	tmp.Close()
	out, err := os.ReadFile(tmpName)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(tmpName)
	return string(out)
}

// TestCmdRunMultiPackageRejection pins cmdRun's one-package rule: a
// pattern matching several packages is rejected before any backend
// invocation, so no backend root is needed on this path.
func TestCmdRunMultiPackageRejection(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	var code int
	stderr := captureStderr(t, func() { code = cmdRun("unused-root", []string{"./..."}) })
	if code != 1 {
		t.Fatalf("cmdRun(./...): code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "matches 4 packages") {
		t.Fatalf("cmdRun(./...): stderr = %q, want 'matches 4 packages'", stderr)
	}
}

// TestCmdBuildOutputWithMultiplePackages pins cmdBuild's -o rule: -o
// with several matched packages is an error, again before any backend
// invocation.
func TestCmdBuildOutputWithMultiplePackages(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	var code int
	stderr := captureStderr(t, func() { code = cmdBuild("unused-root", []string{"-o", "out", "./..."}) })
	if code != 1 {
		t.Fatalf("cmdBuild(-o, ./...): code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "-o with multiple packages") {
		t.Fatalf("cmdBuild(-o, ./...): stderr = %q, want '-o with multiple packages'", stderr)
	}
}

// TestCmdBuildSinglePatternNaming pins cmdBuild's single-pattern
// naming end to end (with the fake backend standing in for the
// frontend gate): a pattern matching exactly one package names the
// artifact after that package's directory, not after the "..." base.
func TestCmdBuildSinglePatternNaming(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH; artifact compilation skipped")
	}
	fake, _ := fakeBackendRoot(t)
	root := patternFixture(t)
	chdir(t, root)
	code := cmdBuild(fake, []string{"./sub/nested/..."})
	if code != 0 {
		t.Fatalf("cmdBuild(./sub/nested/...): code = %d, want 0", code)
	}
	fi, err := os.Stat(filepath.Join(root, "nested"))
	if err != nil {
		t.Fatalf("artifact 'nested' missing: %v", err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("artifact 'nested' is not executable: %v", fi.Mode())
	}
}

// TestCmdVetPerGroup pins cmdVet's per-group checking: the frontend
// check runs once per matched package, over that group's files only —
// never once over files from several packages merged together.
func TestCmdVetPerGroup(t *testing.T) {
	fake, record := fakeBackendRoot(t)
	root := patternFixture(t)
	chdir(t, root)
	code := cmdVet(fake, []string{"./sub/..."})
	if code != 0 {
		t.Fatalf("cmdVet(./sub/...): code = %d, want 0", code)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("backend record unreadable: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("backend invocations = %d (%q), want 2 (one per group)", len(lines), raw)
	}
	if !strings.Contains(lines[0], "--check") || !strings.Contains(lines[0], filepath.Join("sub", "sub.go")) || strings.Contains(lines[0], "nested") {
		t.Fatalf("group 1 invocation = %q, want --check over sub/sub.go only", lines[0])
	}
	if !strings.Contains(lines[1], "--check") || !strings.Contains(lines[1], "nested.go") || strings.Contains(lines[1], filepath.Join("sub", "sub.go")) {
		t.Fatalf("group 2 invocation = %q, want --check over nested files only", lines[1])
	}
}
