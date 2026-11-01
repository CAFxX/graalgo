package main

import (
	"os"
	"os/exec"
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

// TestExpandPatternDirsMatchesGoList is the gc differential for the
// walk: the directories expandPatternDirs returns for a pattern must
// equal the package directories `go list` reports for the same pattern
// on the same tree. It skips when no gc toolchain is on PATH (the
// resolver unit tests above still pin the expected sets).
func TestExpandPatternDirsMatchesGoList(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH; gc differential skipped")
	}
	for _, pat := range []string{"./...", "./sub/..."} {
		prefix, isPattern, err := splitPattern(pat)
		if err != nil || !isPattern {
			t.Fatalf("splitPattern(%q): prefix=%q isPattern=%v err=%v", pat, prefix, isPattern, err)
		}
		got, err := expandPatternDirs(prefix, pat)
		if err != nil {
			t.Fatalf("expandPatternDirs(%q): %v", pat, err)
		}
		cmd := exec.Command(goBin, "list", "-f", "{{.Dir}}", pat)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pat, err)
		}
		var want []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			rel, err := filepath.Rel(root, line)
			if err != nil {
				t.Fatalf("rel(%q): %v", line, err)
			}
			want = append(want, rel)
		}
		// expandPatternDirs spells the pattern root as given
		// ("."/"./sub") while go list reports it as "." after
		// relativizing; normalize both sides through Clean.
		norm := func(ds []string) string {
			var cleaned []string
			for _, d := range ds {
				cleaned = append(cleaned, filepath.Clean(d))
			}
			return strings.Join(cleaned, "|")
		}
		if norm(got) != norm(want) {
			t.Fatalf("pattern %s: expandPatternDirs = %v, go list = %v", pat, got, want)
		}
	}
}

// TestExpandPatternDirsNestedModuleRoot pins the veto finding F1: a
// pattern whose root IS a nested module is refused gc-style, not
// expanded — both spelled from the module root and via `..` from a
// sibling package directory.
func TestExpandPatternDirsNestedModuleRoot(t *testing.T) {
	root := patternFixture(t)
	chdir(t, root)
	for _, tc := range []struct{ prefix, pat string }{
		{"nestedmod", "./nestedmod/..."},
	} {
		if _, err := expandPatternDirs(tc.prefix, tc.pat); err == nil || !strings.Contains(err.Error(), "does not contain main module") {
			t.Fatalf("expandPatternDirs(%q): err = %v, want 'does not contain main module'", tc.pat, err)
		}
	}
	if _, err := resolveFileGroups([]string{"./nestedmod/..."}); err == nil || !strings.Contains(err.Error(), "does not contain main module") {
		t.Fatalf("resolveFileGroups(./nestedmod/...): err = %v, want 'does not contain main module'", err)
	}
	chdir(t, filepath.Join(root, "sub"))
	if _, err := expandPatternDirs("../nestedmod", "../nestedmod/..."); err == nil || !strings.Contains(err.Error(), "does not contain main module") {
		t.Fatalf("expandPatternDirs(../nestedmod/...): err = %v, want 'does not contain main module'", err)
	}
}

// TestResolveFileGroupsDotDotRoot pins the veto finding F2: a `..`
// pattern root expands the enclosing module tree, judged by the
// directory it resolves to rather than its dot-prefixed spelling.
func TestResolveFileGroupsDotDotRoot(t *testing.T) {
	root := patternFixture(t)
	chdir(t, filepath.Join(root, "sub"))
	groups, err := resolveFileGroups([]string{"../..."})
	if err != nil {
		t.Fatalf("resolveFileGroups(../...): %v", err)
	}
	wantDirs := []string{"..", filepath.Join("..", "onlytest"), filepath.Join("..", "sub"), filepath.Join("..", "sub", "nested")}
	if got := groupDirs(t, groups); strings.Join(got, "|") != strings.Join(wantDirs, "|") {
		t.Fatalf("dirs = %v, want %v", got, wantDirs)
	}
	// A single package under a `..` root, spelled through the root.
	groups, err = resolveFileGroups([]string{"../onlytest/..."})
	if err != nil {
		t.Fatalf("resolveFileGroups(../onlytest/...): %v", err)
	}
	if got := groupDirs(t, groups); len(got) != 1 || got[0] != filepath.Join("..", "onlytest") {
		t.Fatalf("dirs = %v, want [../onlytest]", got)
	}
}

// TestExpandPatternDirsOutsideModuleRoot pins roots that resolve
// outside the enclosing module: they are refused, whether the escape
// is from a package directory (`../../...` from sub) or from inside a
// nested module (`../...` from nestedmod, whose main module is itself).
func TestExpandPatternDirsOutsideModuleRoot(t *testing.T) {
	root := patternFixture(t)
	chdir(t, filepath.Join(root, "sub"))
	if _, err := expandPatternDirs("../..", "../../..."); err == nil || !strings.Contains(err.Error(), "does not contain main module") {
		t.Fatalf("expandPatternDirs(../../...): err = %v, want 'does not contain main module'", err)
	}
	chdir(t, filepath.Join(root, "nestedmod"))
	if _, err := expandPatternDirs("..", "../..."); err == nil || !strings.Contains(err.Error(), "does not contain main module") {
		t.Fatalf("expandPatternDirs(../...) from nestedmod: err = %v, want 'does not contain main module'", err)
	}
}

// TestExpandPatternDirsNestedModuleIsMainModuleFromInside pins the
// anchor of the containment rule: from inside a nested module, that
// module is the main module, so `./...` expands it — the rule is
// anchored at the cwd's module, not at the pattern root's ancestors.
func TestExpandPatternDirsNestedModuleIsMainModuleFromInside(t *testing.T) {
	root := patternFixture(t)
	chdir(t, filepath.Join(root, "nestedmod"))
	dirs, err := expandPatternDirs(".", "./...")
	if err != nil {
		t.Fatalf("expandPatternDirs(./...) from nestedmod: %v", err)
	}
	if len(dirs) != 1 || filepath.Clean(dirs[0]) != "." {
		t.Fatalf("dirs = %v, want [.]", dirs)
	}
}

// TestExpandPatternDirsMatchesGoListRoots extends the gc differential
// to the divergent roots: a `..` root must match `go list` run from
// the same subdirectory, and nested-module / outside-module roots
// must fail on both sides (gc fails, the driver fails).
func TestExpandPatternDirsMatchesGoListRoots(t *testing.T) {
	root := patternFixture(t)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH; gc differential skipped")
	}
	goList := func(dir, pat string) ([]string, error) {
		cmd := exec.Command(goBin, "list", "-f", "{{.Dir}}", pat)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		var dirs []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			rel, err := filepath.Rel(root, line)
			if err != nil {
				t.Fatalf("rel(%q): %v", line, err)
			}
			dirs = append(dirs, rel)
		}
		return dirs, nil
	}
	// Success case: ../... from sub matches go list's whole tree.
	sub := filepath.Join(root, "sub")
	chdir(t, sub)
	got, err := expandPatternDirs("..", "../...")
	if err != nil {
		t.Fatalf("expandPatternDirs(../...): %v", err)
	}
	var gotRel []string
	for _, d := range got {
		abs, err := filepath.Abs(d)
		if err != nil {
			t.Fatalf("abs(%q): %v", d, err)
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("rel(%q): %v", abs, err)
		}
		gotRel = append(gotRel, rel)
	}
	want, err := goList(sub, "../...")
	if err != nil {
		t.Fatalf("go list ../...: %v", err)
	}
	if strings.Join(gotRel, "|") != strings.Join(want, "|") {
		t.Fatalf("../...: expandPatternDirs = %v, go list = %v", gotRel, want)
	}
	// Failure cases: gc fails and the driver fails, same class.
	for _, tc := range []struct{ dir, prefix, pat string }{
		{root, "nestedmod", "./nestedmod/..."},
		{sub, "../..", "../../..."},
	} {
		if _, err := goList(tc.dir, tc.pat); err == nil {
			t.Fatalf("go list %s from %s: unexpectedly succeeded", tc.pat, tc.dir)
		}
		chdir(t, tc.dir)
		if _, err := expandPatternDirs(tc.prefix, tc.pat); err == nil || !strings.Contains(err.Error(), "does not contain main module") {
			t.Fatalf("expandPatternDirs(%q) from %s: err = %v, want 'does not contain main module'", tc.pat, tc.dir, err)
		}
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
