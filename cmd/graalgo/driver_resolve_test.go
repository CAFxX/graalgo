package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// chdir changes the process working directory for the test, restoring
// it on cleanup. (t.Chdir needs a go1.24 language version; this
// module is go1.21, so it is spelled out here.)
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

// fileSet returns the base names of files as a set, for order-free
// comparison (go/build does not promise an order for its file lists).
func fileSet(files []string) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		out[filepath.Base(f)] = true
	}
	return out
}

func labelSet(pkgs []resolvedPackage) map[string]bool {
	out := map[string]bool{}
	for _, p := range pkgs {
		out[p.label] = true
	}
	return out
}

func TestResolvePackagesBuildConstraints(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", "package p\nfunc A() {}\n")
	writeFile(t, root, "pkg/a_linux.go", "package p\nfunc ALinux() {}\n")
	writeFile(t, root, "pkg/a_darwin.go", "package p\nfunc ADarwin() {}\n")
	writeFile(t, root, "pkg/ignored.go", "//go:build ignore\n\npackage p\nfunc Ignored() {}\n")
	writeFile(t, root, "pkg/win.go", "//go:build windows\n\npackage p\nfunc Win() {}\n")
	chdir(t, root)

	pkgs, err := resolvePackages([]string{"pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("got %d packages, want 1", len(pkgs))
	}
	got := fileSet(pkgs[0].files)
	for _, want := range []string{"a.go"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, pkgs[0].files)
		}
	}
	for _, banned := range []string{"a_darwin.go", "ignored.go", "win.go"} {
		if got[banned] {
			t.Errorf("constraint-excluded %s present in %v", banned, pkgs[0].files)
		}
	}
	// The platform-suffixed file is included only on its own platform.
	if runtime.GOOS == "linux" && !got["a_linux.go"] {
		t.Errorf("a_linux.go missing on linux in %v", pkgs[0].files)
	}
	if runtime.GOOS != "linux" && got["a_linux.go"] {
		t.Errorf("a_linux.go present on %s in %v", runtime.GOOS, pkgs[0].files)
	}
}

func TestResolvePackagesNoArgsUsesCwd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "other_darwin.go", "package main\nfunc Other() {}\n")
	chdir(t, root)

	pkgs, err := resolvePackages(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("got %d packages, want 1", len(pkgs))
	}
	got := fileSet(pkgs[0].files)
	if !got["main.go"] {
		t.Errorf("main.go missing in %v", pkgs[0].files)
	}
	if runtime.GOOS == "linux" && got["other_darwin.go"] {
		t.Errorf("constraint-excluded other_darwin.go present in %v", pkgs[0].files)
	}
}

func TestResolvePackagesDotDotDot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/a.go", "package a\n")
	writeFile(t, root, "b/b.go", "package b\n")
	writeFile(t, root, "b/sub/c.go", "package sub\n")
	writeFile(t, root, "onlytest/only_test.go", "package onlytest\n")
	writeFile(t, root, "vendor/v/v.go", "package v\n")
	writeFile(t, root, ".hidden/h.go", "package h\n")
	writeFile(t, root, "_under/u.go", "package u\n")
	writeFile(t, root, "testdata/t.go", "package t\n")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	pkgs, err := resolvePackages([]string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	got := labelSet(pkgs)
	for _, want := range []string{"a", "b", "b/sub", "onlytest"} {
		if !got[want] {
			t.Errorf("missing package %s in %v", want, got)
		}
	}
	for _, banned := range []string{"vendor", "vendor/v", ".hidden", "_under", "testdata", "empty"} {
		if got[banned] {
			t.Errorf("excluded package %s present in %v", banned, got)
		}
	}
	if len(pkgs) != 4 {
		t.Errorf("got %d packages %v, want 4", len(pkgs), got)
	}
}

func TestResolvePackagesImportPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.21\n")
	writeFile(t, root, "root.go", "package m\n")
	writeFile(t, root, "sub/s.go", "package sub\n")
	chdir(t, root)

	pkgs, err := resolvePackages([]string{"example.com/m/sub"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].files) != 1 || filepath.Base(pkgs[0].files[0]) != "s.go" {
		t.Fatalf("import path resolved to %v, want [s.go]", pkgs)
	}

	if _, err := resolvePackages([]string{"example.com/m/nope"}); err == nil {
		t.Error("unknown import path resolved without error")
	}
}

func TestResolvePackagesSeparationAndFileLists(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/a.go", "package a\n")
	writeFile(t, root, "b/b.go", "package b\n")
	writeFile(t, root, "x.go", "package x\n")
	writeFile(t, root, "y.go", "//go:build windows\n\npackage x\n")
	chdir(t, root)

	// Two directories stay two packages; files are never merged.
	pkgs, err := resolvePackages([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2", len(pkgs))
	}
	if got := fileSet(pkgs[0].files); !got["a.go"] || got["b.go"] {
		t.Errorf("package a files = %v, want only a.go", pkgs[0].files)
	}
	if got := fileSet(pkgs[1].files); !got["b.go"] || got["a.go"] {
		t.Errorf("package b files = %v, want only b.go", pkgs[1].files)
	}

	// Explicit .go files form one file-list package and bypass
	// constraint filtering (the user named them).
	pkgs, err = resolvePackages([]string{"x.go", "y.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].files) != 2 {
		t.Fatalf("file list resolved to %v, want one package of 2 files", pkgs)
	}

	// Mixing file and package arguments is rejected, not merged.
	if _, err := resolvePackages([]string{"x.go", "a"}); err == nil {
		t.Error("mixed file/package arguments accepted")
	}
	// Files from different directories are rejected.
	if _, err := resolvePackages([]string{"a/a.go", "b/b.go"}); err == nil {
		t.Error("files from different directories accepted")
	}
	// Unknown arguments are rejected.
	if _, err := resolvePackages([]string{"nosuchdir"}); err == nil {
		t.Error("unknown argument accepted")
	}
}

// fakeBackend installs an executable graalgo-sync-run under root that
// logs every invocation to the FAKE_BACKEND_LOG file, writes '{}' to
// any --emit-wire= path, and exits FAKE_BACKEND_CHECK_EXIT for
// invocations containing --check, FAKE_BACKEND_RUN_EXIT otherwise.
func fakeBackend(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/usr/bin/env bash\n" +
		"echo \"$*\" >> \"$FAKE_BACKEND_LOG\"\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    --emit-wire=*) printf '{}' > \"${a#--emit-wire=}\" ;;\n" +
		"  esac\n" +
		"done\n" +
		"if [[ \" $* \" == *\" --check \"* ]]; then exit \"${FAKE_BACKEND_CHECK_EXIT:-0}\"; fi\n" +
		"exit \"${FAKE_BACKEND_RUN_EXIT:-0}\"\n"
	p := filepath.Join(dir, "graalgo-sync-run")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func backendLog(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "backend.log")
	t.Setenv("FAKE_BACKEND_LOG", log)
	return log
}

func readLog(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), "\n")
}

// captureStderr runs fn with os.Stderr redirected to a temp file and
// returns what fn wrote there.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp("", "graalgo-test-stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	old := os.Stderr
	os.Stderr = f
	fn()
	os.Stderr = old
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCmdBuildSeparatesPackages(t *testing.T) {
	root := t.TempDir()
	fakeBackend(t, root)
	log := backendLog(t)
	work := t.TempDir()
	writeFile(t, work, "a/a.go", "package main\nfunc main() {}\n")
	writeFile(t, work, "b/b.go", "package main\nfunc main() {}\n")
	// Build from a separate directory: artifact names ("a", "b")
	// would otherwise collide with the package directories themselves.
	outdir := t.TempDir()
	chdir(t, outdir)

	pkgA := filepath.Join(work, "a")
	pkgB := filepath.Join(work, "b")
	if code := cmdBuild(root, []string{pkgA, pkgB}); code != 0 {
		t.Fatalf("cmdBuild(a, b) = %d, want 0", code)
	}
	lines := readLog(t, log)
	if len(lines) != 2 {
		t.Fatalf("backend invoked %d times %v, want 2 (one per package)", len(lines), lines)
	}
	if !strings.Contains(lines[0], "a/a.go") || strings.Contains(lines[0], "b/b.go") {
		t.Errorf("first build invocation = %q, want only package a's files", lines[0])
	}
	if !strings.Contains(lines[1], "b/b.go") || strings.Contains(lines[1], "a/a.go") {
		t.Errorf("second build invocation = %q, want only package b's files", lines[1])
	}
	for _, name := range []string{"a", "b"} {
		fi, err := os.Stat(filepath.Join(outdir, name))
		if err != nil || fi.IsDir() {
			t.Errorf("artifact %s missing or not a file after build (stat=%v, isDir=%v)", name, err, err == nil && fi.IsDir())
		}
	}

	// -o naming an existing directory with multiple packages: like
	// `go build`, each package's artifact lands in that directory.
	outdir2 := t.TempDir()
	if code := cmdBuild(root, []string{"-o", outdir2, pkgA, pkgB}); code != 0 {
		t.Fatalf("cmdBuild -o dir with multiple packages = %d, want 0", code)
	}
	for _, name := range []string{"a", "b"} {
		fi, err := os.Stat(filepath.Join(outdir2, name))
		if err != nil || fi.IsDir() {
			t.Errorf("artifact %s missing in -o dir after build (stat=%v, isDir=%v)", name, err, err == nil && fi.IsDir())
		}
	}

	// -o naming a non-directory path with multiple packages is
	// rejected with exit 1 (go's convention), before any backend call.
	before := len(readLog(t, log))
	var code int
	stderr := captureStderr(t, func() { code = cmdBuild(root, []string{"-o", "out", pkgA, pkgB}) })
	if code != 1 {
		t.Errorf("cmdBuild -o file with multiple packages = %d, want 1", code)
	}
	if !strings.Contains(stderr, "cannot write multiple packages to non-directory") {
		t.Errorf("stderr = %q, want cannot-write-multiple-packages message", stderr)
	}
	if got := len(readLog(t, log)); got != before {
		t.Errorf("backend invoked for rejected -o build: %d lines, want %d", got, before)
	}
}

func TestCmdBuildOutputIsDirectory(t *testing.T) {
	root := t.TempDir()
	fakeBackend(t, root)
	log := backendLog(t)
	work := t.TempDir()
	writeFile(t, work, "p/p.go", "package main\nfunc main() {}\n")
	chdir(t, work)

	// Default output "p" collides with the package directory itself.
	var code int
	stderr := captureStderr(t, func() { code = cmdBuild(root, []string{"p"}) })
	if code == 0 {
		t.Error("cmdBuild with directory-colliding output succeeded")
	}
	if !strings.Contains(stderr, "already exists and is a directory") {
		t.Errorf("stderr = %q, want directory-collision error", stderr)
	}
	if lines := readLog(t, log); len(lines) != 0 {
		t.Errorf("backend invoked %v for a rejected build, want no invocations", lines)
	}

	// Same for an explicit -o naming a directory.
	stderr = captureStderr(t, func() { code = cmdBuild(root, []string{"-o", "p", "p"}) })
	if code == 0 {
		t.Error("cmdBuild -o with directory output succeeded")
	}
	if !strings.Contains(stderr, "already exists and is a directory") {
		t.Errorf("stderr = %q, want directory-collision error", stderr)
	}
}

func TestCmdVetWholePackageAndStderr(t *testing.T) {
	root := t.TempDir()
	fakeBackend(t, root)
	log := backendLog(t)
	t.Setenv("FAKE_BACKEND_CHECK_EXIT", "1")
	work := t.TempDir()
	writeFile(t, work, "p/p.go", "package p\nfunc F() {}\n")
	writeFile(t, work, "p/q.go", "package p\nfunc G() {\n\treturn\n\tprintln(\"dead\")\n}\n")
	chdir(t, work)

	var code int
	stderr := captureStderr(t, func() { code = cmdVet(root, []string{"p"}) })
	if code != 1 {
		t.Fatalf("cmdVet = %d, want 1", code)
	}
	lines := readLog(t, log)
	if len(lines) != 1 {
		t.Fatalf("backend invoked %d times %v, want 1 whole-package --check", len(lines), lines)
	}
	if !strings.Contains(lines[0], "p/p.go") || !strings.Contains(lines[0], "p/q.go") {
		t.Errorf("--check invocation = %q, want both package files in one call", lines[0])
	}
	// Analyzers ran despite the failing check, and their diagnostics
	// went to stderr, not stdout.
	if !strings.Contains(stderr, "unreachable code") {
		t.Errorf("stderr = %q, want unreachable-code diagnostic despite failing check", stderr)
	}
}

func TestCmdRunCheckPhaseConvention(t *testing.T) {
	root := t.TempDir()
	fakeBackend(t, root)
	log := backendLog(t)
	work := t.TempDir()
	writeFile(t, work, "prog.go", "package main\nfunc main() {}\n")
	chdir(t, work)

	// Failing check: driver exit 1, no run phase, no "exit status" line.
	t.Setenv("FAKE_BACKEND_CHECK_EXIT", "1")
	t.Setenv("FAKE_BACKEND_RUN_EXIT", "0")
	var code int
	stderr := captureStderr(t, func() { code = cmdRun(root, []string{"prog.go"}) })
	if code != 1 {
		t.Fatalf("cmdRun with failing check = %d, want 1", code)
	}
	if lines := readLog(t, log); len(lines) != 1 {
		t.Fatalf("backend invoked %d times %v, want only the check phase", len(lines), lines)
	}
	if strings.Contains(stderr, "exit status") {
		t.Errorf("stderr = %q, want no exit-status line for a check failure", stderr)
	}

	// Passing check, program exits 3: driver exit 1 with "exit status 3".
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_BACKEND_CHECK_EXIT", "0")
	t.Setenv("FAKE_BACKEND_RUN_EXIT", "3")
	stderr = captureStderr(t, func() { code = cmdRun(root, []string{"prog.go"}) })
	if code != 1 {
		t.Fatalf("cmdRun with program exit 3 = %d, want 1", code)
	}
	if lines := readLog(t, log); len(lines) != 2 {
		t.Fatalf("backend invoked %d times %v, want check + run phases", len(lines), lines)
	}
	if !strings.Contains(stderr, "exit status 3") {
		t.Errorf("stderr = %q, want 'exit status 3' for a program exit", stderr)
	}
}

func TestResolveTestDirsDotDotDotAndImportPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.21\n")
	writeFile(t, root, "p1/p1.go", "package p1\n")
	writeFile(t, root, "p1/p1_test.go", "package p1\n")
	writeFile(t, root, "p2/p2.go", "package p2\n")
	writeFile(t, root, "vendor/v/v.go", "package v\n")
	chdir(t, root)

	dirs, err := resolveTestDirs([]string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range dirs {
		got[d] = true
	}
	for _, want := range []string{"p1", "p2"} {
		if !got[want] {
			t.Errorf("missing test dir %s in %v", want, dirs)
		}
	}
	if got["vendor/v"] {
		t.Errorf("vendor dir present in %v", dirs)
	}

	dirs, err = resolveTestDirs([]string{"example.com/m/p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || filepath.Base(dirs[0]) != "p1" {
		t.Errorf("import path resolved to %v, want [p1]", dirs)
	}
}

func TestCmdFmtFormatsAllFilesRegardlessOfConstraints(t *testing.T) {
	// gofmt formats all files regardless of build constraints; fmt
	// must therefore not use the constraint-filtered build file list.
	t.Setenv("GOROOT", runtime.GOROOT())
	root := t.TempDir()
	work := t.TempDir()
	writeFile(t, work, "pkg/a.go", "package p\n\nfunc   A()  {}\n")
	writeFile(t, work, "pkg/foo_windows.go", "//go:build windows\n\npackage p\n\nfunc   Win()  {}\n")
	writeFile(t, work, "pkg/ignored.go", "//go:build ignore\n\npackage p\n\nfunc   Ignored()  {}\n")
	if code := cmdFmt(root, []string{filepath.Join(work, "pkg")}); code != 0 {
		t.Fatalf("cmdFmt(mixed dir) = %d, want 0", code)
	}
	for _, name := range []string{"a.go", "foo_windows.go", "ignored.go"} {
		data, err := os.ReadFile(filepath.Join(work, "pkg", name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "func   ") {
			t.Errorf("%s not formatted: %q", name, data)
		}
	}

	// A directory of only constraint-gated files must format, not
	// error with "no Go files".
	writeFile(t, work, "gated/only_windows.go", "//go:build windows\n\npackage p\n\nfunc   OnlyWin()  {}\n")
	if code := cmdFmt(root, []string{filepath.Join(work, "gated")}); code != 0 {
		t.Fatalf("cmdFmt(gated-only dir) = %d, want 0", code)
	}
	data, err := os.ReadFile(filepath.Join(work, "gated", "only_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "func   ") {
		t.Errorf("gated-only file not formatted: %q", data)
	}
}

func TestCmdBuildTestOnlyPackageOrigin(t *testing.T) {
	root := t.TempDir()
	fakeBackend(t, root)
	log := backendLog(t)
	work := t.TempDir()
	writeFile(t, work, "onlytest/only_test.go", "package onlytest\nimport \"testing\"\nfunc TestX(t *testing.T) {}\n")
	writeFile(t, work, "real/r.go", "package main\nfunc main() {}\n")
	outdir := t.TempDir()
	chdir(t, outdir)

	// An explicitly named test-only package in a multi-build errors,
	// like `go build`: it is skipped only when ./... expanded it.
	var code int
	stderr := captureStderr(t, func() {
		code = cmdBuild(root, []string{filepath.Join(work, "onlytest"), filepath.Join(work, "real")})
	})
	if code != 1 {
		t.Errorf("cmdBuild explicit test-only + real = %d, want 1", code)
	}
	if !strings.Contains(stderr, "no non-test Go files") {
		t.Errorf("stderr = %q, want no-non-test-Go-files error", stderr)
	}
	if lines := readLog(t, log); len(lines) != 0 {
		t.Errorf("backend invoked %v for a rejected build, want none", lines)
	}

	// The same test-only package reached via ./... is skipped and
	// the real package still builds.
	proj := t.TempDir()
	writeFile(t, proj, "proj/onlytest/only_test.go", "package onlytest\nimport \"testing\"\nfunc TestX(t *testing.T) {}\n")
	writeFile(t, proj, "proj/real/r.go", "package main\nfunc main() {}\n")
	chdir(t, proj)
	if code := cmdBuild(root, []string{"./proj/..."}); code != 0 {
		t.Fatalf("cmdBuild ./proj/... with test-only package = %d, want 0", code)
	}
	if lines := readLog(t, log); len(lines) != 1 {
		t.Fatalf("backend invoked %d times %v, want 1 (real package only)", len(lines), lines)
	}
}

func TestResolvePackagesDotDotDotPrunesNestedModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/a.go", "package a\n")
	writeFile(t, root, "nested/n.go", "package nested\n")
	writeFile(t, root, "nested/go.mod", "module example.com/nested\n\ngo 1.21\n")
	writeFile(t, root, "nested/inner/i.go", "package inner\n")
	chdir(t, root)

	pkgs, err := resolvePackages([]string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	got := labelSet(pkgs)
	if !got["a"] {
		t.Errorf("missing package a in %v", got)
	}
	for _, banned := range []string{"nested", "nested/inner"} {
		if got[banned] {
			t.Errorf("nested-module package %s present in %v", banned, got)
		}
	}
}

func TestResolvePackagesCgoIsLoud(t *testing.T) {
	// A package mixing plain and cgo files must not resolve by
	// silently dropping the cgo file: that would build a package
	// missing a file the user wrote.
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", "package p\nfunc A() {}\n")
	writeFile(t, root, "pkg/c.go", "package p\nimport \"C\"\nfunc F() {}\n")
	chdir(t, root)

	if _, err := resolvePackages([]string{"pkg"}); err == nil {
		t.Error("cgo package resolved without error; its CgoFiles were silently dropped")
	} else if !strings.Contains(err.Error(), "cgo") {
		t.Errorf("cgo package error = %q, want it to name cgo", err)
	}
}

func TestVersionStringFormat(t *testing.T) {
	s := versionString()
	matched, err := regexp.MatchString(`^go version go[0-9]+\.[0-9]+(\.[0-9]+)? graalgo/0\.4\.0$`, s)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Errorf("versionString() = %q, want go-version format with graalgo suffix", s)
	}
}
