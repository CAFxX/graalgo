package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// chdir changes the working directory for the test, restoring it on
// cleanup. t.Chdir needs go1.24 but this module declares go1.21, so
// tests use this helper (same pattern as os.Chdir in driver tests).
func chdir(t *testing.T, dir string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

// TestParseBuildFlagsTags: R01 — -tags must be retained by the parser in
// both -tags value and -tags=value forms; a dropped value here is the
// exact regression that silently built the wrong variant.
func TestParseBuildFlagsTags(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-tags", "foo bar", "pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo bar" {
		t.Errorf("-tags value form: tags = %q, want %q", bf.tags, "foo bar")
	}
	if !reflect.DeepEqual(bf.pkgs, []string{"pkg"}) {
		t.Errorf("pkgs = %v, want [pkg]", bf.pkgs)
	}

	bf, err = parseBuildFlags([]string{"-tags=foo", "-o", "out"})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo" || bf.output != "out" {
		t.Errorf("-tags= form: tags = %q output = %q, want foo/out", bf.tags, bf.output)
	}
}

func TestParseBuildFlagsErrors(t *testing.T) {
	if _, err := parseBuildFlags([]string{"-tags"}); err == nil {
		t.Error("-tags without a value: got nil error, want missing-argument error")
	}
	if _, err := parseBuildFlags([]string{"-bogus"}); err == nil {
		t.Error("unknown flag: got nil error, want error")
	}
	if _, err := parseBuildFlags([]string{"-o"}); err == nil {
		t.Error("-o without a value: got nil error, want error")
	}
}

func TestParseBuildFlagsIgnored(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-a", "-x", "-work", "-n", "pkg"})
	if err != nil {
		t.Fatalf("accepted diagnostic flags rejected: %v", err)
	}
	if !reflect.DeepEqual(bf.pkgs, []string{"pkg"}) {
		t.Errorf("pkgs = %v, want [pkg]", bf.pkgs)
	}
}

// TestBackendCheckArgsForwardsTags: R01 — the backend invocation built
// for a tagged build must carry the tags (via GRAALGO_BUILD_TAGS env)
// and the wire/check argv. This is the test the original bug would
// have failed: tags parsed but never forwarded.
func TestBackendCheckArgsForwardsTags(t *testing.T) {
	args, env := backendCheckArgs("foo bar", "/tmp/w.json", []string{"a.go", "b.go"})
	wantArgs := []string{"--emit-wire=/tmp/w.json", "--check", "a.go", "b.go"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("backend argv = %v, want %v", args, wantArgs)
	}
	wantEnv := []string{"GRAALGO_BUILD_TAGS=foo bar"}
	if !reflect.DeepEqual(env, wantEnv) {
		t.Errorf("backend env = %v, want %v", env, wantEnv)
	}

	args, env = backendCheckArgs("", "/tmp/w.json", []string{"a.go"})
	if env != nil {
		t.Errorf("untagged build: backend env = %v, want nil (no tags to forward)", env)
	}
	if !reflect.DeepEqual(args, []string{"--emit-wire=/tmp/w.json", "--check", "a.go"}) {
		t.Errorf("untagged build: backend argv = %v", args)
	}
}

func TestTagsEnv(t *testing.T) {
	if got := tagsEnv(""); got != nil {
		t.Errorf("tagsEnv(\"\") = %v, want nil", got)
	}
	if got := tagsEnv("foo"); !reflect.DeepEqual(got, []string{"GRAALGO_BUILD_TAGS=foo"}) {
		t.Errorf("tagsEnv(\"foo\") = %v", got)
	}
}

// TestFilterFilesByBuildTags: R01 — files gated by a tag not in -tags
// are excluded; files gated by a tag in -tags and unconstrained files
// are kept; a tag set excluding everything is a loud error.
func TestFilterFilesByBuildTags(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	plain := write("plain.go", "package p\n")
	gated := write("gated.go", "//go:build mytag\n\npackage p\n")
	other := write("other.go", "//go:build othertag\n\npackage p\n")

	kept, err := filterFilesByBuildTags([]string{plain, gated, other}, "mytag")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, []string{plain, gated}) {
		t.Errorf("tags=mytag: kept = %v, want [plain gated]", kept)
	}

	kept, err = filterFilesByBuildTags([]string{plain, gated, other}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 3 {
		t.Errorf("no tags: kept = %v, want all files unchanged", kept)
	}

	if _, err := filterFilesByBuildTags([]string{gated, other}, "nomatch"); err == nil {
		t.Error("tags excluding all files: got nil error, want loud error")
	}
}

// TestGroupFilesByPackage: R02 — a package and its external test
// package must form separate groups; internal test files in the same
// package stay with it.
func TestGroupFilesByPackage(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.go", "package foo\n")
	b := write("b.go", "package foo\n")
	ext := write("c_test.go", "package foo_test\n")
	in := write("d_test.go", "package foo\n")

	groups, err := groupFilesByPackage([]string{a, b, ext, in})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (foo, foo_test)", len(groups))
	}
	if !reflect.DeepEqual(groups[0], []string{a, b, in}) {
		t.Errorf("group 0 = %v, want [a b d_test]", groups[0])
	}
	if !reflect.DeepEqual(groups[1], []string{ext}) {
		t.Errorf("group 1 = %v, want [c_test]", groups[1])
	}
}

func TestGroupFilesByPackageParseError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.go")
	if err := os.WriteFile(p, []byte("this is not go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := groupFilesByPackage([]string{p}); err == nil {
		t.Error("unparsable file: got nil error, want loud error")
	}
}

func TestResolveFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	chdir(t, dir)
	files, err := resolveFiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "a.go" {
		t.Errorf("no-arg resolve = %v, want [a.go]", files)
	}
	files, err = resolveFiles([]string{"sub"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "c.go") {
		t.Errorf("dir resolve = %v, want [sub/c.go]", files)
	}
	files, err = resolveFiles([]string{"missing.go"})
	if err != nil || len(files) != 1 {
		t.Errorf("missing .go passthrough = %v, %v, want [missing.go] nil", files, err)
	}
	if _, err := resolveFiles([]string{"missing-dir"}); err == nil {
		t.Error("missing non-go path: got nil error, want error")
	}
	if _, err := resolveFiles([]string{"./..."}); err == nil {
		t.Error("./...: got nil error, want not-yet-supported error")
	}
	empty := t.TempDir()
	chdir(t, empty)
	if _, err := resolveFiles(nil); err == nil {
		t.Error("empty dir, no args: got nil error, want error")
	}
}

func TestFindGraalgoRoot(t *testing.T) {
	// Success via GRAALGO_HOME.
	root := t.TempDir()
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAALGO_HOME", root)
	got, err := findGraalgoRoot()
	if err != nil || got != root {
		t.Errorf("GRAALGO_HOME root: got %q, %v, want %q nil", got, err, root)
	}

	// GRAALGO_HOME pointing at a non-installation is a loud error.
	t.Setenv("GRAALGO_HOME", t.TempDir())
	if _, err := findGraalgoRoot(); err == nil {
		t.Error("GRAALGO_HOME non-root: got nil error, want error")
	}

	// No GRAALGO_HOME and no installation above cwd or the test
	// binary: root discovery must fail loudly, not return "".
	t.Setenv("GRAALGO_HOME", "")
	chdir(t, t.TempDir())
	if _, err := findGraalgoRoot(); err == nil {
		t.Error("no installation discoverable: got nil error, want error")
	}
}

// TestDispatchErrorPaths: R23 — build/run/vet/fmt must fail loudly on
// bad flags and unresolvable operands without ever reaching the
// backend (root is bogus on purpose: any backend call would fail
// differently, so these tests prove validation happens first).
func TestDispatchErrorPaths(t *testing.T) {
	const bogusRoot = "/nonexistent-graalgo-root"
	chdir(t, t.TempDir())

	cases := []struct {
		name string
		code int
	}{
		{"build unknown flag", cmdBuild(bogusRoot, []string{"-bogus"})},
		{"build -ldflags", cmdBuild(bogusRoot, []string{"-ldflags", "-X=x"})},
		{"build -o -", cmdBuild(bogusRoot, []string{"-o", "-"})},
		{"build missing dir", cmdBuild(bogusRoot, []string{"./no-such-dir"})},
		{"build -tags no files", cmdBuild(bogusRoot, []string{"-tags", "foo"})},
		{"run no files", cmdRun(bogusRoot, nil)},
		{"run unknown flag", cmdRun(bogusRoot, []string{"-bogus"})},
		{"run -ldflags", cmdRun(bogusRoot, []string{"-ldflags", "x"})},
		{"run -o", cmdRun(bogusRoot, []string{"-o", "out", "x.go"})},
		{"vet missing dir", cmdVet(bogusRoot, []string{"./no-such-dir"})},
		{"fmt missing dir", cmdFmt(bogusRoot, []string{"./no-such-dir"})},
	}
	for _, tc := range cases {
		if tc.code == 0 {
			t.Errorf("%s: exit 0, want non-zero", tc.name)
		}
	}
	// Usage errors are specifically exit 2 for build flag misuse.
	if code := cmdBuild(bogusRoot, []string{"-bogus"}); code != 2 {
		t.Errorf("build unknown flag: exit %d, want 2", code)
	}
	if code := cmdRun(bogusRoot, nil); code != 2 {
		t.Errorf("run no files: exit %d, want 2", code)
	}
}
