package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakeGraalgoRoot creates a directory tree that findGraalgoRoot accepts,
// with internal/backend/graalgo-sync-run as a shell script that exits with
// the given code. It lets the command tests exercise the backend-invocation
// paths without the real Truffle backend.
func fakeGraalgoRoot(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "internal", "backend")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexit " + strconv.Itoa(exitCode) + "\n"
	p := filepath.Join(binDir, "graalgo-sync-run")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// captureStream redirects *stream (os.Stdout or os.Stderr) for the duration
// of f and returns what was written.
func captureStream(t *testing.T, stream **os.File, f func()) string {
	t.Helper()
	old := *stream
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	*stream = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	*stream = old
	return <-done
}

func TestParseBuildFlags(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-o", "out", "-ldflags", "-X=a", "-tags", "t1", "-a", "-x", "pkg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bf.output != "out" || bf.ldflags != "-X=a" || bf.tags != "t1" {
		t.Errorf("wrong parse: %+v", bf)
	}
	if len(bf.pkgs) != 1 || bf.pkgs[0] != "pkg" {
		t.Errorf("wrong pkgs: %+v", bf)
	}

	bf, err = parseBuildFlags([]string{"-o=out2", "-tags=t2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bf.output != "out2" || bf.tags != "t2" {
		t.Errorf("wrong =-form parse: %+v", bf)
	}

	if _, err := parseBuildFlags([]string{"--bogus"}); err == nil {
		t.Error("expected error for unknown flag, got nil")
	}
	if _, err := parseBuildFlags([]string{"-ldflags"}); err == nil {
		t.Error("expected error for -ldflags with missing value, got nil")
	}
	// A bare "-" is not a flag; it goes to the package list.
	bf, err = parseBuildFlags([]string{"-"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bf.pkgs) != 1 || bf.pkgs[0] != "-" {
		t.Errorf("bare - should be a package arg: %+v", bf)
	}
}

func TestCmdBuildFlagErrors(t *testing.T) {
	// Unknown flag: usage error, exit 2, before any backend use.
	if code := cmdBuild("", []string{"--bogus"}); code != 2 {
		t.Errorf("unknown flag: got exit %d, want 2", code)
	}
	// -o - is rejected: exit 2.
	if code := cmdBuild("", []string{"-o", "-"}); code != 2 {
		t.Errorf("-o -: got exit %d, want 2", code)
	}
	// -ldflags is recognized but unsupported: exit 1.
	if code := cmdBuild("", []string{"-ldflags", "x"}); code != 1 {
		t.Errorf("-ldflags: got exit %d, want 1", code)
	}
}

func TestCmdBuildBackendFailure(t *testing.T) {
	root := fakeGraalgoRoot(t, 1)
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\n\nfunc main() {}\n")
	// The frontend gate (fake backend) fails: build must fail non-zero.
	if code := cmdBuild(root, []string{filepath.Join(dir, "a.go")}); code != 1 {
		t.Errorf("backend failure: got exit %d, want 1", code)
	}
}

func TestCmdRunNoFiles(t *testing.T) {
	if code := cmdRun("", nil); code != 2 {
		t.Errorf("no files: got exit %d, want 2", code)
	}
}

func TestCmdRunFlagRejections(t *testing.T) {
	// -ldflags is recognized but not supported for run: exit 1.
	if code := cmdRun("", []string{"-ldflags", "x", "a.go"}); code != 1 {
		t.Errorf("-ldflags: got exit %d, want 1", code)
	}
	// -o is meaningless for run: exit 1.
	if code := cmdRun("", []string{"-o", "out", "a.go"}); code != 1 {
		t.Errorf("-o: got exit %d, want 1", code)
	}
	// Unknown build flag: usage error, exit 2.
	if code := cmdRun("", []string{"--bogus", "a.go"}); code != 2 {
		t.Errorf("unknown flag: got exit %d, want 2", code)
	}
}

func TestCmdRunProgramExitPropagation(t *testing.T) {
	root := fakeGraalgoRoot(t, 3)
	dir := t.TempDir()
	writeFile(t, dir, "prog.go", "package main\n\nfunc main() {}\n")
	// Like `go run`: a non-zero program exit becomes exit 1 with an
	// "exit status N" line on stderr.
	var code int
	stderr := captureStream(t, &os.Stderr, func() {
		code = cmdRun(root, []string{filepath.Join(dir, "prog.go")})
	})
	if code != 1 {
		t.Errorf("program exit 3: got exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "exit status 3") {
		t.Errorf("missing exit status line on stderr, got: %q", stderr)
	}
}

func TestCmdRunMissingBackend(t *testing.T) {
	// Root without the launcher script: exec fails, exit 1 (not 2).
	root := t.TempDir()
	dir := t.TempDir()
	writeFile(t, dir, "prog.go", "package main\n\nfunc main() {}\n")
	if code := cmdRun(root, []string{filepath.Join(dir, "prog.go")}); code != 1 {
		t.Errorf("missing backend: got exit %d, want 1", code)
	}
}

func TestCmdVetResolveError(t *testing.T) {
	if code := cmdVet("", []string{"/nonexistent-dir-graalgo-test"}); code != 1 {
		t.Errorf("unresolvable path: got exit %d, want 1", code)
	}
}

func TestCmdVet(t *testing.T) {
	root := fakeGraalgoRoot(t, 0) // --check passes; analyzers decide.
	dir := t.TempDir()
	writeFile(t, dir, "bad.go", "package main\n\nfunc f() {\n\tx := 1\n\tx = x\n\t_ = x\n}\n")
	if code := cmdVet(root, []string{filepath.Join(dir, "bad.go")}); code != 1 {
		t.Errorf("vet findings: got exit %d, want 1", code)
	}

	writeFile(t, dir, "good.go", "package main\n\nfunc g() {\n\tx := 1\n\t_ = x\n}\n")
	stdout := captureStream(t, &os.Stdout, func() {
		if code := cmdVet(root, []string{filepath.Join(dir, "good.go")}); code != 0 {
			t.Errorf("clean file: got exit %d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "no issues found") {
		t.Errorf("clean file: missing confirmation, got: %q", stdout)
	}
}

func TestCmdFmtNoGoFiles(t *testing.T) {
	dir := t.TempDir() // empty: no .go files
	if code := cmdFmt("", []string{dir}); code != 1 {
		t.Errorf("no Go files: got exit %d, want 1", code)
	}
}

func TestCmdFmtFormats(t *testing.T) {
	if findGofmt() == "" {
		t.Skip("gofmt not found")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	writeFile(t, dir, "a.go", "package main\n\nfunc f( ){}\n")
	if code := cmdFmt("", []string{p}); code != 0 {
		t.Errorf("fmt: got exit %d, want 0", code)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc f() {}\n" {
		t.Errorf("file not formatted, got: %q", got)
	}
}

func TestCmdTestFlagHandling(t *testing.T) {
	if code := cmdTest("", []string{"--bogus"}); code != 2 {
		t.Errorf("unknown flag: got exit %d, want 2", code)
	}
	if code := cmdTest("", []string{"-count=abc"}); code != 2 {
		t.Errorf("bad -count: got exit %d, want 2", code)
	}
	if code := cmdTest("", []string{"-cover"}); code != 1 {
		t.Errorf("-cover: got exit %d, want 1", code)
	}
	if code := cmdTest("", []string{"-run=["}); code != 1 {
		t.Errorf("invalid -run regexp: got exit %d, want 1", code)
	}
	if code := cmdTest("", []string{"/nonexistent-dir-graalgo-test"}); code != 1 {
		t.Errorf("unresolvable dir: got exit %d, want 1", code)
	}
	if code := cmdTest("", []string{"-h"}); code != 0 {
		t.Errorf("-h: got exit %d, want 0", code)
	}
}

func TestUsageAndHelpFor(t *testing.T) {
	out := captureStream(t, &os.Stdout, usage)
	if !strings.Contains(out, "graalgo <command>") {
		t.Errorf("usage missing command synopsis, got: %q", out)
	}
	out = captureStream(t, &os.Stdout, func() { helpFor("run") })
	if !strings.Contains(out, "usage: graalgo run") {
		t.Errorf("help run missing, got: %q", out)
	}
	out = captureStream(t, &os.Stdout, func() { helpFor("bogus") })
	if !strings.Contains(out, "unknown command") {
		t.Errorf("help bogus should report unknown command, got: %q", out)
	}
}

func TestFindGraalgoRoot(t *testing.T) {
	root := fakeGraalgoRoot(t, 0)
	t.Setenv("GRAALGO_HOME", root)
	got, err := findGraalgoRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}

	t.Setenv("GRAALGO_HOME", filepath.Join(root, "nonexistent"))
	if _, err := findGraalgoRoot(); err == nil {
		t.Error("expected error for bad GRAALGO_HOME, got nil")
	}
}

func TestResolveFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n")
	writeFile(t, dir, "b_test.go", "package p\n")
	files, err := resolveFiles([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %v", files)
	}
	if _, err := resolveFiles([]string{"./..."}); err == nil {
		t.Error("expected error for ./..., got nil")
	}

	// No args: *.go in the current directory. (Manual chdir: the module is
	// go1.21, so testing.T.Chdir is unavailable; no parallel tests exist in
	// this package, so a process-wide chdir with restore is safe.)
	empty := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(empty); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := resolveFiles(nil); err == nil {
		t.Error("expected error with no Go files in cwd, got nil")
	}
	writeFile(t, empty, "c.go", "package p\n")
	files, err = resolveFiles(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "c.go" {
		t.Errorf("expected [c.go], got %v", files)
	}
}

// TestDispatch builds the driver binary and exercises the real main()
// dispatch (which calls os.Exit and cannot be tested in-process).
func TestDispatch(t *testing.T) {
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "graalgo")
	// Repo root: two levels up from this test file (cmd/graalgo/).
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	build := exec.Command("go", "build", "-o", bin, "./cmd/graalgo/")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	root := fakeGraalgoRoot(t, 0)
	run := func(t *testing.T, env []string, dir string, args ...string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append([]string{}, env...)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("run failed: %v", err)
			}
		}
		return code, stdout.String(), stderr.String()
	}

	workdir := t.TempDir() // outside the repo tree: no root discovery
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}

	// No command: usage error, exit 2.
	if code, _, _ := run(t, env, workdir); code != 2 {
		t.Errorf("no args: got exit %d, want 2", code)
	}
	// version is handled before root lookup.
	if code, out, _ := run(t, env, workdir, "version"); code != 0 || !strings.Contains(out, "graalgo version ") {
		t.Errorf("version: got exit %d, out %q", code, out)
	}
	if code, out, _ := run(t, env, workdir, "help", "run"); code != 0 || !strings.Contains(out, "usage: graalgo run") {
		t.Errorf("help run: got exit %d, out %q", code, out)
	}

	// Unknown command dispatches to the default branch: exit 2.
	// GRAALGO_HOME points at a valid fake root so root lookup succeeds.
	envRoot := append(env, "GRAALGO_HOME="+root)
	if code, _, stderr := run(t, envRoot, workdir, "boguscmd"); code != 2 || !strings.Contains(stderr, "unknown command") {
		t.Errorf("unknown command: got exit %d, stderr %q", code, stderr)
	}
	// build with an unknown flag: exit 2 from cmdBuild.
	if code, _, _ := run(t, envRoot, workdir, "build", "--bogus"); code != 2 {
		t.Errorf("build --bogus: got exit %d, want 2", code)
	}
	// No graalgo installation discoverable: exit 1.
	if code, _, stderr := run(t, env, workdir, "boguscmd"); code != 1 || !strings.Contains(stderr, "cannot locate graalgo installation") {
		t.Errorf("missing root: got exit %d, stderr %q", code, stderr)
	}
}
