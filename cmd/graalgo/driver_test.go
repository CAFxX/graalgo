package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// what it printed. Tests in this package do not run in parallel, so
// swapping os.Stdout here is safe.
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
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func baseSet(files []string) map[string]bool {
	m := make(map[string]bool, len(files))
	for _, f := range files {
		m[filepath.Base(f)] = true
	}
	return m
}

func equalStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func TestParseBuildFlagsErrorPaths(t *testing.T) {
	for _, args := range [][]string{
		{"-unknown"},
		{"-unknown=value"},
		{"-o"},
		{"-ldflags"},
		{"-tags"},
	} {
		if _, err := parseBuildFlags(args); err == nil {
			t.Errorf("parseBuildFlags(%v): got no error", args)
		}
	}
	bf, err := parseBuildFlags([]string{"-o", "out", "-tags=linux,amd64", "-a", "-x", "pkg.go"})
	if err != nil {
		t.Fatalf("parseBuildFlags(valid): %v", err)
	}
	if bf.output != "out" || bf.tags != "linux,amd64" {
		t.Errorf("parseBuildFlags(valid) = %+v", bf)
	}
	if len(bf.pkgs) != 1 || bf.pkgs[0] != "pkg.go" {
		t.Errorf("parseBuildFlags(valid) pkgs = %v", bf.pkgs)
	}
	// A lone "-" is a package/file argument, not a flag.
	bf, err = parseBuildFlags([]string{"-"})
	if err != nil {
		t.Fatalf("parseBuildFlags([-]): %v", err)
	}
	if len(bf.pkgs) != 1 || bf.pkgs[0] != "-" {
		t.Errorf("parseBuildFlags([-]) pkgs = %v", bf.pkgs)
	}
}

func TestParseCount(t *testing.T) {
	for _, s := range []string{"0", "-2", "abc", ""} {
		if _, err := parseCount(s); err == nil {
			t.Errorf("parseCount(%q): got no error", s)
		}
	}
	if n, err := parseCount("3"); err != nil || n != 3 {
		t.Errorf("parseCount(3) = %d, %v", n, err)
	}
}

func TestResolveTestDirs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := resolveTestDirs(nil)
	if err != nil || len(dirs) != 1 || dirs[0] != cwd {
		t.Errorf("resolveTestDirs(nil) = %v, %v", dirs, err)
	}
	dir := t.TempDir()
	dirs, err = resolveTestDirs([]string{dir})
	if err != nil || len(dirs) != 1 || dirs[0] != dir {
		t.Errorf("resolveTestDirs(dir) = %v, %v", dirs, err)
	}
	f := filepath.Join(dir, "x.go")
	if err := os.WriteFile(f, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs, err = resolveTestDirs([]string{f})
	if err != nil || len(dirs) != 1 || dirs[0] != dir {
		t.Errorf("resolveTestDirs(file) = %v, %v", dirs, err)
	}
	for _, bad := range [][]string{{filepath.Join(dir, "missing")}, {"./..."}} {
		if _, err := resolveTestDirs(bad); err == nil {
			t.Errorf("resolveTestDirs(%v): got no error", bad)
		}
	}
}

func TestCmdTestArgHandling(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"-bogus"}, 2},
		{[]string{"-count=abc"}, 2},
		{[]string{"-count", "0"}, 2},
		{[]string{"-count", "-1"}, 2},
		{[]string{"-cover"}, 1},
		{[]string{"-h"}, 0},
		{[]string{filepath.Join(t.TempDir(), "missing")}, 1},
	}
	for _, c := range cases {
		if got := cmdTest("", c.args); got != c.want {
			t.Errorf("cmdTest(%v) = %d, want %d", c.args, got, c.want)
		}
	}
	// Invalid -run regexp is a hard error, after dirs resolve.
	if got := cmdTest("", []string{"-run=[", t.TempDir()}); got != 1 {
		t.Errorf("cmdTest(-run=[) = %d, want 1", got)
	}
}

func TestDispatchUnknownAndMissing(t *testing.T) {
	if got := dispatchCommand("", "bogus", nil); got != 2 {
		t.Errorf("dispatchCommand(bogus) = %d, want 2", got)
	}
	if got := dispatchCommand("", "bogus", []string{"x"}); got != 2 {
		t.Errorf("dispatchCommand(bogus, args) = %d, want 2", got)
	}
	var code int
	out := captureStdout(t, func() { code = runMain(nil) })
	if code != 2 {
		t.Errorf("runMain(nil) = %d, want 2", code)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("runMain(nil) printed no usage:\n%s", out)
	}
	out = captureStdout(t, func() { code = runMain([]string{"version"}) })
	if code != 0 || !strings.Contains(out, "graalgo version") {
		t.Errorf("runMain(version) = %d, out %q", code, out)
	}
	out = captureStdout(t, func() { code = runMain([]string{"help", "run"}) })
	if code != 0 || !strings.Contains(out, "graalgo run") {
		t.Errorf("runMain(help run) = %d, out %q", code, out)
	}
}

// platFixture builds a multi-OS package directory: one untagged file,
// one file per GOOS suffix, one //go:build-ignored file, and one test
// file, all in the same package.
func platFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/plat\n\ngo 1.21\n")
	writeFile(t, dir, "a.go", "package plat\n")
	writeFile(t, dir, "foo_linux.go", "package plat\n")
	writeFile(t, dir, "foo_darwin.go", "package plat\n")
	writeFile(t, dir, "foo_windows.go", "package plat\n")
	writeFile(t, dir, "bar_ignore.go", "//go:build ignore\n\npackage plat\n")
	writeFile(t, dir, "z_test.go", "package plat\n")
	return dir
}

func TestResolveFilesBuildConstraints(t *testing.T) {
	dir := platFixture(t)
	files, err := resolveFiles([]string{dir})
	if err != nil {
		t.Fatalf("resolveFiles: %v", err)
	}
	got := baseSet(files)
	// Untagged and test files are always in; other platforms and
	// //go:build-ignored files are always out.
	for _, want := range []string{"a.go", "z_test.go"} {
		if !got[want] {
			t.Errorf("resolveFiles missing %s: %v", want, files)
		}
	}
	for _, excluded := range []string{"bar_ignore.go"} {
		if got[excluded] {
			t.Errorf("resolveFiles wrongly includes %s: %v", excluded, files)
		}
	}
	for _, other := range []string{"linux", "darwin", "windows"} {
		name := "foo_" + other + ".go"
		if other == runtime.GOOS {
			if !got[name] {
				t.Errorf("resolveFiles missing current-platform %s: %v", name, files)
			}
		} else if got[name] {
			t.Errorf("resolveFiles wrongly includes other-platform %s: %v", name, files)
		}
	}
	// Explicitly listed files bypass constraint filtering.
	darwin := filepath.Join(dir, "foo_darwin.go")
	files, err = resolveFiles([]string{darwin})
	if err != nil || len(files) != 1 || files[0] != darwin {
		t.Errorf("resolveFiles(explicit darwin file) = %v, %v", files, err)
	}
	// The unfiltered expansion (fmt's path) keeps every file.
	files, err = resolveAllFiles([]string{dir})
	if err != nil {
		t.Fatalf("resolveAllFiles: %v", err)
	}
	if !equalStringSet(baseSet(files), map[string]bool{
		"a.go": true, "foo_linux.go": true, "foo_darwin.go": true,
		"foo_windows.go": true, "bar_ignore.go": true, "z_test.go": true,
	}) {
		t.Errorf("resolveAllFiles = %v", files)
	}
}

func TestResolveFilesNoArgsBuildConstraints(t *testing.T) {
	dir := platFixture(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	files, err := resolveFiles(nil)
	if err != nil {
		t.Fatalf("resolveFiles(nil): %v", err)
	}
	got := baseSet(files)
	if got["foo_darwin.go"] || got["foo_windows.go"] || got["bar_ignore.go"] {
		t.Errorf("resolveFiles(nil) in package dir includes excluded files: %v", files)
	}
	if !got["a.go"] {
		t.Errorf("resolveFiles(nil) missing a.go: %v", files)
	}
}

func TestGroupFilesByPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package foo\n")
	writeFile(t, dir, "b.go", "package foo\n")
	writeFile(t, dir, "c_test.go", "package foo\n")
	writeFile(t, dir, "d_test.go", "package foo_test\n")
	writeFile(t, dir, "bad.go", "this is not go at all\n")
	a, b, c, d, bad := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go"),
		filepath.Join(dir, "c_test.go"), filepath.Join(dir, "d_test.go"),
		filepath.Join(dir, "bad.go")
	groups := groupFilesByPackage([]string{a, b, c, d, bad})
	want := [][]string{{a, b, c}, {d}, {bad}}
	if len(groups) != len(want) {
		t.Fatalf("groupFilesByPackage = %v, want %v", groups, want)
	}
	for i := range want {
		if strings.Join(groups[i], ",") != strings.Join(want[i], ",") {
			t.Errorf("group %d = %v, want %v", i, groups[i], want[i])
		}
	}
	// Same package name in two directories stays two groups.
	dir2 := t.TempDir()
	writeFile(t, dir2, "a.go", "package foo\n")
	a2 := filepath.Join(dir2, "a.go")
	groups = groupFilesByPackage([]string{a, a2})
	if len(groups) != 2 {
		t.Errorf("groupFilesByPackage across dirs = %v, want 2 groups", groups)
	}
}

func TestDiscoverTestsSignature(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a_test.go", `package foo

import "testing"

func TestGood(t *testing.T) {}
func TestMain(m *testing.M) {}
func TestWrongType(x int) {}
func TestWrongM(m *testing.M) {}
func TestNoParams() {}
func TestTwoNames(a, b *testing.T) {}
func TestUnnamed(*testing.T) {}
func TestNonPointer(t testing.T) {}
`)
	tests, err := discoverTests(filepath.Join(root, "a_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestGood", "TestUnnamed"}
	if strings.Join(tests, ",") != strings.Join(want, ",") {
		t.Errorf("discoverTests = %v, want %v", tests, want)
	}

	// Aliased testing import.
	writeFile(t, root, "b_test.go", `package foo

import tt "testing"

func TestAlias(t *tt.T) {}
func TestAliasWrong(t *tt.M) {}
`)
	tests, err = discoverTests(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestAlias" {
		t.Errorf("discoverTests(alias) = %v, want [TestAlias]", tests)
	}

	// Dot-imported testing.
	writeFile(t, root, "c_test.go", `package foo

import . "testing"

func TestDot(t *T) {}
func TestDotWrong(x int) {}
`)
	tests, err = discoverTests(filepath.Join(root, "c_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestDot" {
		t.Errorf("discoverTests(dot) = %v, want [TestDot]", tests)
	}

	// No testing import at all: *testing.T cannot resolve, so the
	// function is not a test (the file is invalid Go regardless).
	writeFile(t, root, "d_test.go", `package foo

func TestOrphan(t *testing.T) {}
`)
	tests, err = discoverTests(filepath.Join(root, "d_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 0 {
		t.Errorf("discoverTests(no import) = %v, want []", tests)
	}
}

func TestCmdVetSingleCheckAndSilent(t *testing.T) {
	dir := t.TempDir()
	// Cross-file reference: the N-P1-17 regression core. A per-file
	// --check fails b.go (A undefined in isolation); one --check over
	// both files succeeds.
	writeFile(t, dir, "a.go", "package foo\n\nfunc A() int { return 1 }\n")
	writeFile(t, dir, "b.go", "package foo\n\nfunc B() int { return A() }\n")
	var calls [][]string
	stub := func(root string, args []string) int {
		calls = append(calls, append([]string{}, args...))
		return 0
	}
	var code int
	out := captureStdout(t, func() {
		code = cmdVetWithRunner("fake-root", []string{dir}, stub)
	})
	if code != 0 {
		t.Fatalf("cmdVetWithRunner = %d, want 0", code)
	}
	if len(calls) != 1 {
		t.Fatalf("check calls = %d, want 1 (one --check over the whole package): %v", len(calls), calls)
	}
	if calls[0][0] != "--check" {
		t.Errorf("check call = %v, want --check first", calls[0])
	}
	got := append([]string{}, calls[0][1:]...)
	sort.Strings(got)
	want := []string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("check files = %v, want %v", calls[0][1:], want)
	}
	if out != "" {
		t.Errorf("cmdVet printed on success %q, want silence (N-P2-39)", out)
	}
	// A failing --check gates the analyzers: exit 1.
	code = cmdVetWithRunner("fake-root", []string{dir}, func(string, []string) int { return 1 })
	if code != 1 {
		t.Errorf("cmdVetWithRunner(failing check) = %d, want 1", code)
	}
}
