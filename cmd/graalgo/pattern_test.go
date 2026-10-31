package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// patternFixture builds a small multi-package module tree exercising
// every `./...` walk rule: nested packages, a test-only package, and
// directories gc excludes from `...` expansion (underscore-prefixed,
// testdata, vendor, nested module).
func patternFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/probe\n\ngo 1.21\n")
	writeFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, root, "main_test.go", "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n")
	writeFile(t, root, "sub/sub.go", "package sub\n")
	writeFile(t, root, "sub/nested/nested.go", "package nested\n")
	writeFile(t, root, "sub/nested/nested_test.go", "package nested\n\nimport \"testing\"\n\nfunc TestNested(t *testing.T) {}\n")
	writeFile(t, root, "onlytest/only_test.go", "package onlytest\n\nimport \"testing\"\n\nfunc TestOnly(t *testing.T) {}\n")
	writeFile(t, root, "_hidden/h.go", "package hidden\n")
	writeFile(t, root, "testdata/x/x.go", "package x\n")
	writeFile(t, root, "vendor/vendpkg/v.go", "package vendpkg\n")
	writeFile(t, root, "nestedmod/go.mod", "module example.com/nested\n\ngo 1.21\n")
	writeFile(t, root, "nestedmod/n.go", "package nestedmod\n")
	writeFile(t, root, "assets/readme.txt", "not go\n")
	return root
}

// chdir changes the test process's working directory, restoring it on
// cleanup. (t.Chdir needs go1.24; this module declares go1.21.)
func chdir(t *testing.T, dir string) {
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

func groupDirs(t *testing.T, groups []fileGroup) []string {
	t.Helper()
	var dirs []string
	for _, g := range groups {
		dirs = append(dirs, g.dir)
	}
	return dirs
}

func TestResolveFileGroupsDotDotDot(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	groups, err := resolveFileGroups([]string{"./..."})
	if err != nil {
		t.Fatalf("resolveFileGroups(./...): %v", err)
	}
	// Root first, then lexical: onlytest, sub, sub/nested. The
	// underscore, testdata, vendor, and nested-module trees are out,
	// and the assets dir holds no .go files.
	wantDirs := []string{".", "onlytest", "sub", filepath.Join("sub", "nested")}
	if got := groupDirs(t, groups); strings.Join(got, "|") != strings.Join(wantDirs, "|") {
		t.Fatalf("dirs = %v, want %v", got, wantDirs)
	}
	// Groups carry every .go file of the package, test files
	// included; _test.go filtering is cmdBuild's job, not the
	// resolver's (the test path needs the test files).
	rootFiles := map[string]bool{}
	for _, f := range groups[0].files {
		rootFiles[filepath.Base(f)] = true
	}
	if !rootFiles["main.go"] || !rootFiles["main_test.go"] {
		t.Fatalf("root group files = %v, want main.go + main_test.go", groups[0].files)
	}
}

func TestResolveFileGroupsSubPattern(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	groups, err := resolveFileGroups([]string{"./sub/..."})
	if err != nil {
		t.Fatalf("resolveFileGroups(./sub/...): %v", err)
	}
	// The pattern root keeps its "./sub" spelling; children are
	// cleaned by filepath.Join.
	wantDirs := []string{"./sub", filepath.Join("sub", "nested")}
	if got := groupDirs(t, groups); strings.Join(got, "|") != strings.Join(wantDirs, "|") {
		t.Fatalf("dirs = %v, want %v", got, wantDirs)
	}
}

func TestResolveFileGroupsOverlappingPatternsDeduped(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	groups, err := resolveFileGroups([]string{"./...", "./sub/..."})
	if err != nil {
		t.Fatalf("resolveFileGroups: %v", err)
	}
	seen := map[string]int{}
	for _, g := range groups {
		seen[filepath.Clean(g.dir)]++
	}
	for d, n := range seen {
		if n > 1 {
			t.Fatalf("directory %q appears in %d groups", d, n)
		}
	}
}

func TestResolveFileGroupsNoMatchAndBadPatterns(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	if _, err := resolveFileGroups([]string{"./assets/..."}); err == nil || !strings.Contains(err.Error(), "matched no packages") {
		t.Fatalf("assets pattern: err = %v, want 'matched no packages'", err)
	}
	if _, err := resolveFileGroups([]string{"./nope/..."}); err == nil || !strings.Contains(err.Error(), "./nope/...") {
		t.Fatalf("missing-dir pattern: err = %v, want pattern named", err)
	}
	// A bare "..." is gc's all-packages pattern, not "./...": it must
	// fail loudly, never silently expand to the local tree.
	if _, err := resolveFileGroups([]string{"..."}); err == nil || !strings.Contains(err.Error(), "./...") {
		t.Fatalf("bare ...: err = %v, want redirection to ./...", err)
	}
	if _, err := resolveFileGroups([]string{"./.../sub"}); err == nil || !strings.Contains(err.Error(), "invalid package pattern") {
		t.Fatalf("mid-pattern ...: err = %v, want 'invalid package pattern'", err)
	}
}

// TestResolveFileGroupsIgnoredRoots pins gc's root rule: a pattern
// whose root is itself ignored (testdata, underscore, dot) matches no
// packages, and a vendor root fails loudly instead of matching.
func TestResolveFileGroupsIgnoredRoots(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	for _, pat := range []string{"./testdata/...", "./_hidden/..."} {
		if _, err := resolveFileGroups([]string{pat}); err == nil || !strings.Contains(err.Error(), "matched no packages") {
			t.Fatalf("%s: err = %v, want 'matched no packages'", pat, err)
		}
	}
	if _, err := resolveFileGroups([]string{"./vendor/..."}); err == nil || !strings.Contains(err.Error(), "vendor") {
		t.Fatalf("./vendor/...: err = %v, want vendor error", err)
	}
}

func TestResolveFileGroupsExplicitArgsUnchanged(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	// A single directory still resolves to one flat group, as before.
	groups, err := resolveFileGroups([]string{"sub"})
	if err != nil {
		t.Fatalf("resolveFileGroups(sub): %v", err)
	}
	if len(groups) != 1 || len(groups[0].files) != 1 {
		t.Fatalf("groups = %+v, want one group with sub.go", groups)
	}
	// Explicit .go files still form one group.
	groups, err = resolveFileGroups([]string{"main.go", "sub/sub.go"})
	if err != nil {
		t.Fatalf("resolveFileGroups(files): %v", err)
	}
	if len(groups) != 1 || len(groups[0].files) != 2 {
		t.Fatalf("groups = %+v, want one group with two files", groups)
	}
}

func TestResolveTestDirsDotDotDot(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	dirs, err := resolveTestDirs([]string{"./..."})
	if err != nil {
		t.Fatalf("resolveTestDirs(./...): %v", err)
	}
	// Test expansion keeps the test-only package (it has a .go file)
	// and the same exclusions as the build expansion.
	want := []string{".", "onlytest", "sub", filepath.Join("sub", "nested")}
	if strings.Join(dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	// Overlapping pattern + explicit dir: each directory once.
	dirs, err = resolveTestDirs([]string{"./...", "sub"})
	if err != nil {
		t.Fatalf("resolveTestDirs overlapping: %v", err)
	}
	if strings.Join(dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	if _, err := resolveTestDirs([]string{"./assets/..."}); err == nil || !strings.Contains(err.Error(), "matched no packages") {
		t.Fatalf("assets pattern: err = %v, want 'matched no packages'", err)
	}
}

// TestExpandPatternDirsSkipsUnreadableAndSymlinkDirs pins two walk
// rules that the fixture cannot express via plain files: symlinked
// directories are not followed, and a nested go.mod stops descent.
func TestExpandPatternDirsSymlinkAndNestedModule(t *testing.T) {
	root := patternFixture(t)
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "linksub")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	chdir(t, root)
	dirs, err := expandPatternDirs(".", "./...")
	if err != nil {
		t.Fatalf("expandPatternDirs: %v", err)
	}
	for _, d := range dirs {
		if filepath.Clean(d) == "linksub" {
			t.Fatalf("symlinked dir followed: %v", dirs)
		}
		if strings.HasPrefix(filepath.Clean(d), "nestedmod") {
			t.Fatalf("nested module included: %v", dirs)
		}
	}
}
