package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// chdirTo switches the process working directory to dir for the rest of
// the test, restoring it afterwards. (No test in this package runs in
// parallel, so a process-wide chdir is safe here.)
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// fixtureModule builds a small module exercising the ./... expansion
// rules: nested packages, a package with no Go files, a test-only
// package, a vendored directory, a nested module, hidden / "_"-prefixed /
// testdata directories, and a directory whose only Go file is
// "_"-prefixed (ignored by the go tool).
func fixtureModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fix\n\ngo 1.21\n")
	writeFile(t, root, "root.go", "package fix\n")
	writeFile(t, root, "a/a.go", "package a\n")
	writeFile(t, root, "a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n")
	writeFile(t, root, "a/b/b.go", "package b\n")
	writeFile(t, root, "empty/README.txt", "no Go files here\n")
	writeFile(t, root, "onlytest/only_test.go", "package onlytest\n\nimport \"testing\"\n\nfunc TestY(t *testing.T) {}\n")
	writeFile(t, root, "vendor/vend/v.go", "package vend\n")
	writeFile(t, root, "submod/go.mod", "module example.com/submod\n\ngo 1.21\n")
	writeFile(t, root, "submod/pkg/p.go", "package pkg\n")
	writeFile(t, root, ".hidden/h.go", "package hidden\n")
	writeFile(t, root, "_ignored/i.go", "package ignored\n")
	writeFile(t, root, "testdata/data/d.go", "package data\n")
	writeFile(t, root, "ignoredfile/_foo.go", "package ignoredfile\n")
	return root
}

// findGoBinary locates a go binary for the differential check against
// `go list`: the go on PATH, else GOROOT/bin/go, else the repo's
// pinned toolchain. Returns "" when none is found.
func findGoBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		if p := filepath.Join(goroot, "bin", "go"); isExec(p) {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, "tools", "go", "bin", "go"); isExec(p) {
			return p
		}
	}
	return ""
}

// goListDirs runs `go list -f {{.Dir}} ./...` in dir and returns the
// matched directories relative to dir ("." for dir itself), sorted.
func goListDirs(t *testing.T, goBin, dir string) []string {
	t.Helper()
	cmd := exec.Command(goBin, "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = dir
	// Hermetic, offline: no network, throwaway cache dirs.
	cmd.Env = append(os.Environ(),
		"GOPROXY=off",
		"GOCACHE="+filepath.Join(t.TempDir(), "gocache"),
		"GOPATH="+filepath.Join(t.TempDir(), "gopath"),
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list ./...: %v: %s", err, ee.Stderr)
		}
		t.Fatalf("go list ./...: %v", err)
	}
	var rels []string
	for _, line := range strings.Fields(string(out)) {
		rel, err := filepath.Rel(dir, line)
		if err != nil {
			t.Fatal(err)
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	sort.Strings(rels)
	return rels
}

// TestExpandDotDotDotMatchesGoList is the differential test for R-07:
// our ./... expansion over a fixture module must equal `go list ./...`
// over the same module. Any divergence (a vendored, hidden, testdata,
// nested-module, empty, or ignored-file directory leaking in, or a
// test-only package dropped) fails the test.
func TestExpandDotDotDotMatchesGoList(t *testing.T) {
	goBin := findGoBinary(t)
	if goBin == "" {
		t.Skip("no go binary found for the go list differential check")
	}
	root := fixtureModule(t)
	chdirTo(t, root)

	got, err := expandDotDotDot()
	if err != nil {
		t.Fatalf("expandDotDotDot: %v", err)
	}
	want := goListDirs(t, goBin, root)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandDotDotDot() = %v, go list ./... = %v", got, want)
	}
	// Explicit negative assertions, so a failure names the rule broken
	// even before the diff above is read.
	for _, banned := range []string{"empty", "vendor/vend", "submod/pkg", ".hidden", "_ignored", "testdata/data", "ignoredfile"} {
		for _, d := range got {
			if d == banned {
				t.Fatalf("expandDotDotDot() wrongly includes %q", banned)
			}
		}
	}
	// Explicit positive assertion: test-only packages are packages.
	found := false
	for _, d := range got {
		if d == "onlytest" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expandDotDotDot() dropped test-only package onlytest: %v", got)
	}
}

func TestResolveTestDirsDotDotDot(t *testing.T) {
	root := fixtureModule(t)
	chdirTo(t, root)

	got, err := resolveTestDirs([]string{"./..."})
	if err != nil {
		t.Fatalf("resolveTestDirs(./...): %v", err)
	}
	want := []string{".", "a", "a/b", "onlytest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveTestDirs(./...) = %v, want %v", got, want)
	}

	// Mixed explicit + pattern args: each package once, in
	// first-match order (the explicit arg keeps its position; the
	// expansion dedups against it). The expansion itself is sorted;
	// the mixed order is not.
	got, err = resolveTestDirs([]string{"a", "./..."})
	if err != nil {
		t.Fatalf("resolveTestDirs(a, ./...): %v", err)
	}
	wantMixed := []string{"a", ".", "a/b", "onlytest"}
	if !reflect.DeepEqual(got, wantMixed) {
		t.Fatalf("resolveTestDirs(a, ./...) = %v, want %v", got, wantMixed)
	}
}

func TestResolveTestDirsRejectsOtherPatterns(t *testing.T) {
	// Only "..." patterns are rejected here: the driver's bare
	// arguments are filesystem paths (a bare "all" is a path lookup
	// that fails loudly as "no such file or directory", which is the
	// pre-existing path-based behavior, not a pattern to narrow).
	for _, p := range []string{"./a/...", "...", "example.com/...", "./.../x"} {
		if _, err := resolveTestDirs([]string{p}); err == nil {
			t.Fatalf("resolveTestDirs(%q): expected loud rejection, got nil error", p)
		} else if !strings.Contains(err.Error(), "not yet supported") {
			t.Fatalf("resolveTestDirs(%q): error %v does not say 'not yet supported'", p, err)
		}
	}
}

func TestResolveFilesDotDotDot(t *testing.T) {
	root := fixtureModule(t)
	chdirTo(t, root)

	got, err := resolveFiles([]string{"./..."})
	if err != nil {
		t.Fatalf("resolveFiles(./...): %v", err)
	}
	want := []string{
		"root.go",
		filepath.Join("a", "a.go"),
		filepath.Join("a", "a_test.go"),
		filepath.Join("a", "b", "b.go"),
		filepath.Join("onlytest", "only_test.go"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveFiles(./...) = %v, want %v", got, want)
	}
}

// TestResolveFilesIgnoresIgnoredFiles pins the go/build rule that
// files starting with "." or "_" are not part of a package: they must
// not be returned by directory expansion (or they would be gated by
// build/run although gc ignores them), while _test.go files are still
// returned here — the callers that must exclude them filter later.
func TestResolveFilesIgnoresIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package p\n")
	writeFile(t, root, "_foo.go", "package p\n")
	writeFile(t, root, ".dot.go", "package p\n")
	writeFile(t, root, "b_test.go", "package p\n")
	chdirTo(t, root)

	got, err := resolveFiles([]string{"."})
	if err != nil {
		t.Fatalf("resolveFiles(.): %v", err)
	}
	want := []string{"a.go", "b_test.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveFiles(.) = %v, want %v", got, want)
	}
	if got := nonTestGoFiles("."); !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Fatalf("nonTestGoFiles(.) = %v, want [a.go]", got)
	}
}

// TestCmdBuildExpandedAllTestOnly pins the gc behaviour verified in
// the lane's differential run: `go build ./...` over a tree whose only
// packages are test-only succeeds (exit 0, nothing to build), unlike
// a single explicitly named test-only package. No backend is involved:
// the empty-buildable return happens before any frontend gating.
func TestCmdBuildExpandedAllTestOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "only_test.go", "package t\n\nimport \"testing\"\n\nfunc TestT(t *testing.T) {}\n")
	writeFile(t, root, "sub/only_test.go", "package sub\n\nimport \"testing\"\n\nfunc TestS(t *testing.T) {}\n")
	chdirTo(t, root)
	if code := cmdBuildExpanded("", buildFlags{pkgs: []string{"./..."}}); code != 0 {
		t.Fatalf("cmdBuildExpanded over all-test-only ./...: exit %d, want 0 (gc succeeds)", code)
	}
}

// TestCmdBuildExpandedRejectsMixedFileArgs pins the loud rejection of
// `graalgo build ./... file.go`: widening the file to its directory
// would silently build something the user did not ask for. This fails
// before any frontend gating, so no backend is needed.
func TestCmdBuildExpandedRejectsMixedFileArgs(t *testing.T) {
	root := fixtureModule(t)
	chdirTo(t, root)
	if code := cmdBuildExpanded("", buildFlags{pkgs: []string{"./...", "root.go"}}); code != 2 {
		t.Fatalf("cmdBuildExpanded(./... + file): exit %d, want 2 (loud rejection)", code)
	}
}

func TestExpandDotDotDotNoPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.txt", "no Go files\n")
	chdirTo(t, root)
	if _, err := expandDotDotDot(); err == nil {
		t.Fatal("expandDotDotDot over a directory with no Go files: expected error, got nil")
	}
}
