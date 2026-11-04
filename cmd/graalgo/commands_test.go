package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Regression tests for R11-CMD (NEW-P0-9: -tags silently dropped;
// NEW-P1-7: build/run ignore build constraints). All fixtures are
// minimal packages; only file headers matter to go/build, except the
// package clause itself.

func writePkgFile(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func baseNames(t *testing.T, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		out = append(out, filepath.Base(f))
	}
	sort.Strings(out)
	return out
}

func assertBaseNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	gotB := baseNames(t, got)
	sort.Strings(want)
	if strings.Join(gotB, " ") != strings.Join(want, " ") {
		t.Fatalf("got files %v, want %v", gotB, want)
	}
}

func TestSplitBuildTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo bar", []string{"foo", "bar"}},
		{"foo,bar", []string{"foo", "bar"}},
		{"foo, bar  baz", []string{"foo", "bar", "baz"}},
		{"foo,,bar", []string{"foo", "bar"}},
	}
	for _, c := range cases {
		got := splitBuildTags(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("splitBuildTags(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("splitBuildTags(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestParseBuildFlagsTags(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-tags", "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo" {
		t.Fatalf("tags = %q, want %q", bf.tags, "foo")
	}
	bf, err = parseBuildFlags([]string{"-tags=foo,bar", "pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo,bar" || len(bf.pkgs) != 1 || bf.pkgs[0] != "pkg" {
		t.Fatalf("tags = %q pkgs = %v", bf.tags, bf.pkgs)
	}
	if _, err := parseBuildFlags([]string{"-tags"}); err == nil {
		t.Fatal("expected error for -tags with no argument")
	}
}

func TestEffectiveBuildTagsAndEnv(t *testing.T) {
	t.Setenv("GRAALGO_BUILD_TAGS", "env1 env2")
	got := effectiveBuildTags("flag1,env1")
	want := []string{"env1", "env2", "flag1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("effectiveBuildTags = %v, want %v", got, want)
	}
	if env := backendTagsEnv(got); len(env) != 1 || env[0] != "GRAALGO_BUILD_TAGS=env1 env2 flag1" {
		t.Fatalf("backendTagsEnv = %v", env)
	}
	if env := backendTagsEnv(nil); env != nil {
		t.Fatalf("backendTagsEnv(nil) = %v, want nil", env)
	}
}

func tagFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writePkgFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	writePkgFile(t, dir, "t_foo.go", "//go:build foo\n\npackage main\nfunc tagged() string { return \"foo\" }\n")
	writePkgFile(t, dir, "t_nofoo.go", "//go:build !foo\n\npackage main\nfunc tagged() string { return \"nofoo\" }\n")
	return dir
}

func TestResolveBuildFilesTags(t *testing.T) {
	dir := tagFixture(t)
	files, err := resolveBuildFiles([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNames(t, files, "main.go", "t_nofoo.go")
	files, err = resolveBuildFiles([]string{dir}, []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNames(t, files, "main.go", "t_foo.go")
}

func TestImportDirWithTags(t *testing.T) {
	dir := tagFixture(t)
	pkg, err := importDirWithTags(dir, []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	got := append(append([]string{}, pkg.GoFiles...), pkg.TestGoFiles...)
	sort.Strings(got)
	if strings.Join(got, " ") != "main.go t_foo.go" {
		t.Fatalf("GoFiles = %v, want [main.go t_foo.go]", got)
	}
}

func TestResolveBuildFilesGoos(t *testing.T) {
	dir := t.TempDir()
	writePkgFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	writePkgFile(t, dir, "v_linux.go", "package main\nfunc variant() string { return \"linux\" }\n")
	writePkgFile(t, dir, "v_windows.go", "package main\nfunc variant() string { return \"windows\" }\n")
	writePkgFile(t, dir, "v_darwin.go", "//go:build darwin\n\npackage main\nfunc variantD() string { return \"darwin\" }\n")
	writePkgFile(t, dir, "v_linux2.go", "//go:build linux\n\npackage main\nfunc variantL() string { return \"linux2\" }\n")
	files, err := resolveBuildFiles([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// This test runs on linux (the driver's platform); the windows and
	// darwin variants must be excluded, the linux ones included.
	assertBaseNames(t, files, "main.go", "v_linux.go", "v_linux2.go")
}

func TestResolveBuildFilesExplicitFilesPassThrough(t *testing.T) {
	dir := t.TempDir()
	writePkgFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	writePkgFile(t, dir, "v_windows.go", "package main\nfunc variant() string { return \"windows\" }\n")
	// Explicitly naming a file compiles it as given (gc file mode),
	// even when its constraints exclude it for the platform.
	files, err := resolveBuildFiles([]string{filepath.Join(dir, "v_windows.go")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNames(t, files, "v_windows.go")
	// Mixed: the directory expansion is filtered, the explicit file kept.
	files, err = resolveBuildFiles([]string{dir, filepath.Join(dir, "v_windows.go")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNames(t, files, "main.go", "v_windows.go")
}

func TestResolveBuildFilesNoBuildable(t *testing.T) {
	dir := t.TempDir()
	writePkgFile(t, dir, "v_windows.go", "package main\nfunc variant() string { return \"windows\" }\n")
	files, err := resolveBuildFiles([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("got %v, want empty (no buildable files on this platform)", files)
	}
}

func TestResolveBuildFilesBrokenDir(t *testing.T) {
	dir := t.TempDir()
	writePkgFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	writePkgFile(t, dir, "bad.go", "//go:build !!\n\npackage main\n")
	// No tags: fall back to the unfiltered set so the backend reports
	// the real error, as before.
	files, err := resolveBuildFiles([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNames(t, files, "bad.go", "main.go")
	// Tags requested: falling back would silently ignore the tags.
	if _, err := resolveBuildFiles([]string{dir}, []string{"foo"}); err == nil {
		t.Fatal("expected error for unloadable directory with tags requested")
	}
}

func TestResolveBuildFilesNoArgs(t *testing.T) {
	dir := tagFixture(t)
	// Manual chdir/restore: t.Chdir needs go1.24, but this module is go1.21.
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(old) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	files, err := resolveBuildFiles(nil, []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNames(t, files, "main.go", "t_foo.go")
}

func TestGraalgoRunEnvPassesTags(t *testing.T) {
	root := t.TempDir()
	scriptDir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "env.out")
	script := "#!/bin/sh\nprintf '%s' \"$GRAALGO_BUILD_TAGS\" > '" + out + "'\n"
	scriptPath := filepath.Join(scriptDir, "graalgo-sync-run")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Flag tags override the inherited environment in the child.
	t.Setenv("GRAALGO_BUILD_TAGS", "envtag")
	if code := graalgoRunEnv(root, []string{"--check"}, []string{"GRAALGO_BUILD_TAGS=flagtag"}); code != 0 {
		t.Fatalf("graalgoRunEnv exit = %d, want 0", code)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "flagtag" {
		t.Fatalf("child saw GRAALGO_BUILD_TAGS=%q, want %q", got, "flagtag")
	}
	if code := graalgoRunProgramEnv(root, []string{"--check"}, []string{"GRAALGO_BUILD_TAGS=flagtag"}); code != 0 {
		t.Fatalf("graalgoRunProgramEnv exit = %d, want 0", code)
	}
}
