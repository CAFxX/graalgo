package main

import (
	"bytes"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// what fn printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// stubBackend records backend invocations and answers them without a
// JVM. For --emit-wire invocations it writes fake wire JSON to the
// requested path so cmdBuild can proceed to Emit.
func stubBackend(t *testing.T) *[][]string {
	t.Helper()
	old := backendRun
	var calls [][]string
	backendRun = func(root string, args []string) int {
		calls = append(calls, append([]string{}, args...))
		for _, a := range args {
			if strings.HasPrefix(a, "--emit-wire=") {
				if err := os.WriteFile(strings.TrimPrefix(a, "--emit-wire="), []byte(`{"wire":true}`), 0o644); err != nil {
					t.Error(err)
				}
			}
		}
		return 0
	}
	t.Cleanup(func() { backendRun = old })
	return &calls
}

func baseNames(paths []string) []string {
	var out []string
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// O4: the test path's build-constraint fallback must not hand the backend
// mutually exclusive, build-gated files. When ImportDir fails, the
// fallback filters by filename suffix and //go:build evaluation instead
// of returning every *.go file.

// A directory whose files are all build-excluded under the default
// context: ImportDir errors ("no buildable Go source files"), and the
// old fallback returned both files, which redeclare each other.
func TestBuildableGoFilesFallbackExcludesGatedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "taga.go", "//go:build taga\n\npackage p\n\nfunc Foo() int { return 1 }\n")
	writeFile(t, dir, "tagb.go", "//go:build tagb\n\npackage p\n\nfunc Foo() int { return 2 }\n")
	if _, err := build.Default.ImportDir(dir, 0); err == nil {
		t.Fatal("test setup: expected ImportDir to fail on an all-excluded directory")
	}
	got := buildableGoFiles(dir)
	if len(got) != 0 {
		t.Fatalf("buildableGoFiles on fallback path = %v, want empty (both files are build-excluded)", got)
	}
}

// ImportDir can also fail for a reason unrelated to constraints
// ("found packages"): the fallback must still apply constraint
// filtering to the files it returns, not return the raw glob.
func TestBuildableGoFilesFallbackStillFiltersConstraints(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "good.go", "package p\n\nfunc A() {}\n")
	writeFile(t, dir, "other.go", "package q\n\nfunc B() {}\n")
	writeFile(t, dir, "winonly.go", "//go:build windows\n\npackage p\n\nfunc W() {}\n")
	if _, err := build.Default.ImportDir(dir, 0); err == nil {
		t.Fatal("test setup: expected ImportDir to fail on a mixed-package directory")
	}
	got := buildableGoFiles(dir)
	for _, f := range got {
		if filepath.Base(f) == "winonly.go" {
			t.Fatalf("buildableGoFiles returned build-excluded winonly.go: %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("buildableGoFiles = %v, want the 2 constraint-satisfying files", got)
	}
}

// The constraint filter must honour a caller's build tags, not just
// build.Default: this is the seam the -tags forwarding (N1) threads
// through, so a tag-gated file selected by -tags is not dropped by
// the fallback. Here ImportDir under tags=[sometag] fails ("found
// packages": on.go in p and other.go in q both match), so the
// fallback runs — and it must still return the tag-selected file.
func TestBuildableGoFilesWithContextHonoursTags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "on.go", "//go:build sometag\n\npackage p\n\nfunc On() {}\n")
	writeFile(t, dir, "other.go", "package q\n\nfunc Other() {}\n")
	ctxt := build.Default
	ctxt.BuildTags = []string{"sometag"}
	if _, err := ctxt.ImportDir(dir, 0); err == nil {
		t.Fatal("test setup: expected ImportDir to fail on a mixed-package directory")
	}
	got := buildableGoFilesWithContext(dir, ctxt)
	if len(got) != 2 {
		t.Fatalf("buildableGoFilesWithContext(tags=[sometag]) = %v, want [on.go other.go]", got)
	}
	// Sanity: under the default context on.go is excluded, so
	// ImportDir succeeds and only other.go is buildable.
	got = buildableGoFiles(dir)
	if len(got) != 1 || filepath.Base(got[0]) != "other.go" {
		t.Fatalf("buildableGoFiles (default) = %v, want [other.go]", got)
	}
}

// O2: each directory operand is its own package, with its own file set.
func TestResolvePackagesTwoDirsSeparate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/a.go", "package a\n\nfunc A() {}\n")
	writeFile(t, root, "b/b.go", "package b\n\nfunc B() {}\n")
	pkgs, err := resolvePackages([]string{filepath.Join(root, "a"), filepath.Join(root, "b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("resolvePackages = %d packages, want 2", len(pkgs))
	}
	if !equalStrings(baseNames(pkgs[0].files), []string{"a.go"}) || !equalStrings(baseNames(pkgs[1].files), []string{"b.go"}) {
		t.Fatalf("resolvePackages file sets = %v, %v, want [a.go], [b.go]", pkgs[0].files, pkgs[1].files)
	}
}

// O2, silent sub-case: two directories declaring the same package name
// with disjoint declarations must NOT merge into one file list.
func TestResolvePackagesSamePackageNameSeparate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/main.go", "package main\n\nfunc main() { println(\"A\") }\n")
	writeFile(t, root, "b/main.go", "package main\n\nfunc main() { println(\"B\") }\n")
	pkgs, err := resolvePackages([]string{filepath.Join(root, "a"), filepath.Join(root, "b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 || len(pkgs[0].files) != 1 || len(pkgs[1].files) != 1 {
		t.Fatalf("resolvePackages = %v, want 2 single-file packages", pkgs)
	}
}

// A list of .go files is a single (command-line-arguments) package.
func TestResolvePackagesFileOperandsSingleGroup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package main\n\nfunc main() {}\n")
	writeFile(t, root, "b.go", "package main\n\nfunc helper() {}\n")
	pkgs, err := resolvePackages([]string{filepath.Join(root, "a.go"), filepath.Join(root, "b.go")})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].files) != 2 {
		t.Fatalf("resolvePackages = %v, want 1 package with 2 files", pkgs)
	}
}

func TestResolvePackagesMixedOperandsRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/a.go", "package a\n\nfunc A() {}\n")
	writeFile(t, root, "f.go", "package main\n\nfunc main() {}\n")
	if _, err := resolvePackages([]string{filepath.Join(root, "a"), filepath.Join(root, "f.go")}); err == nil {
		t.Fatal("resolvePackages mixing a directory and a .go file: want an error, got nil")
	}
	if _, err := resolvePackages([]string{"./..."}); err == nil {
		t.Fatal("resolvePackages ./...: want an error, got nil")
	}
}

func TestResolvePackagesNoArgs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package p\n\nfunc A() {}\n")
	writeFile(t, root, "b.go", "package p\n\nfunc B() {}\n")
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })
	pkgs, err := resolvePackages(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].files) != 2 {
		t.Fatalf("resolvePackages(nil) = %v, want 1 package with 2 files", pkgs)
	}
}

// O1: a two-file package where a.go calls a helper in b.go is vet-clean
// and must pass with a single --check over the whole file list.
func TestCmdVetMultiFilePackageSingleCheck(t *testing.T) {
	calls := stubBackend(t)
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", "package pkg\n\nfunc F() int { return Helper() }\n")
	writeFile(t, root, "pkg/b.go", "package pkg\n\nfunc Helper() int { return 42 }\n")
	var code int
	out := captureStdout(t, func() {
		code = cmdVet("test-root", []string{filepath.Join(root, "pkg")})
	})
	if code != 0 {
		t.Fatalf("cmdVet = %d, want 0; output: %s", code, out)
	}
	if !strings.Contains(out, "no issues found") {
		t.Fatalf("cmdVet output = %q, want it to contain \"no issues found\"", out)
	}
	if len(*calls) != 1 {
		t.Fatalf("backend invocations = %v, want exactly 1 --check", *calls)
	}
	args := (*calls)[0]
	if len(args) != 3 || args[0] != "--check" {
		t.Fatalf("backend invocation = %v, want [--check a.go b.go]", args)
	}
	if !equalStrings(baseNames(args[1:]), []string{"a.go", "b.go"}) {
		t.Fatalf("backend --check files = %v, want [a.go b.go]", args[1:])
	}
}

// O1, second half: the same shape of package with a real vet violation
// must still report it.
func TestCmdVetReportsRealViolation(t *testing.T) {
	stubBackend(t)
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", "package pkg\n\nfunc F() int { return 1; println(\"unreachable\") }\n")
	var code int
	out := captureStdout(t, func() {
		code = cmdVet("test-root", []string{filepath.Join(root, "pkg")})
	})
	if code != 1 {
		t.Fatalf("cmdVet = %d, want 1; output: %s", code, out)
	}
	if !strings.Contains(out, "unreachable code") {
		t.Fatalf("cmdVet output = %q, want it to report \"unreachable code\"", out)
	}
}

// O2 for vet: two packages are checked and analyzed separately, each
// with only its own files.
func TestCmdVetTwoPackagesCheckedSeparately(t *testing.T) {
	calls := stubBackend(t)
	root := t.TempDir()
	writeFile(t, root, "a/a.go", "package a\n\nfunc A() {}\n")
	writeFile(t, root, "b/b.go", "package b\n\nfunc B() {}\n")
	code := cmdVet("test-root", []string{filepath.Join(root, "a"), filepath.Join(root, "b")})
	if code != 0 {
		t.Fatalf("cmdVet = %d, want 0", code)
	}
	if len(*calls) != 2 {
		t.Fatalf("backend invocations = %v, want 2 (one per package)", *calls)
	}
	for _, args := range *calls {
		if len(args) != 2 || args[0] != "--check" {
			t.Fatalf("backend invocation = %v, want [--check <single file>]", args)
		}
	}
}

// O2 for build: two directories in one invocation produce two
// artifacts, each named after its own package. The build runs from a
// work directory separate from the sources, and the artifacts must be
// regular executable files there — a bare Stat would also match a
// source directory of the same name, which is not an artifact.
func TestCmdBuildTwoPackagesSeparateArtifacts(t *testing.T) {
	calls := stubBackend(t)
	root := t.TempDir()
	writeFile(t, root, "src/pkgA/main.go", "package main\n\nfunc main() { println(\"A\") }\n")
	writeFile(t, root, "src/pkgB/main.go", "package main\n\nfunc main() { println(\"B\") }\n")
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })
	code := cmdBuild("test-root", []string{"../src/pkgA", "../src/pkgB"})
	if code != 0 {
		t.Fatalf("cmdBuild = %d, want 0", code)
	}
	if len(*calls) != 2 {
		t.Fatalf("backend invocations = %v, want 2 (one per package)", *calls)
	}
	for _, name := range []string{"pkgA", "pkgB"} {
		fi, err := os.Stat(filepath.Join(work, name))
		if err != nil {
			t.Fatalf("artifact %s missing: %v", name, err)
		}
		if fi.IsDir() || fi.Mode()&0o111 == 0 {
			t.Fatalf("artifact %s is not an executable file: %v", name, fi.Mode())
		}
	}
}

// O2: -o names a single artifact, so it cannot be used when building
// multiple packages.
func TestCmdBuildMultiplePackagesRejectsDashO(t *testing.T) {
	calls := stubBackend(t)
	root := t.TempDir()
	writeFile(t, root, "pkgA/main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, root, "pkgB/main.go", "package main\n\nfunc main() {}\n")
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })
	code := cmdBuild("test-root", []string{"-o", "out", "pkgA", "pkgB"})
	if code != 2 {
		t.Fatalf("cmdBuild -o with two packages = %d, want 2", code)
	}
	if len(*calls) != 0 {
		t.Fatalf("backend invocations = %v, want none (rejected before building)", *calls)
	}
}
