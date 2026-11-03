package main

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests for REVIEW-2026-11-03 P1-T4: `graalgo run`/`build`
// silently ignored build constraints and -tags. Directory expansion
// must apply go/build filtering (platform + tags), explicitly listed
// files must bypass it (like `go run`/`go build`), and -tags must reach
// the backend instead of being accepted and dropped.

func baseSet(t *testing.T, files []string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, f := range files {
		out[filepath.Base(f)] = true
	}
	return out
}

func TestResolveFilesWithTags(t *testing.T) {
	if build.Default.GOOS == "plan9" {
		t.Skip("plan9-suffixed fixture file is buildable on a plan9 host")
	}
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n")
	writeFile(t, dir, "b_mytag.go", "//go:build mytag\n\npackage p\n")
	writeFile(t, dir, "c_ignore.go", "//go:build ignore\n\npackage p\n")
	writeFile(t, dir, "d_plan9.go", "package p\n")
	writeFile(t, dir, "e_test.go", "package p\n")

	// No tags: only the unconditional file and the test file (callers
	// filter _test.go themselves, so resolveFiles still returns it).
	files, err := resolveFilesWithTags([]string{dir}, nil)
	if err != nil {
		t.Fatalf("resolveFilesWithTags no tags: %v", err)
	}
	got := baseSet(t, files)
	for _, want := range []string{"a.go", "e_test.go"} {
		if !got[want] {
			t.Errorf("no tags: missing %s (got %v)", want, got)
		}
	}
	for _, excluded := range []string{"b_mytag.go", "c_ignore.go", "d_plan9.go"} {
		if got[excluded] {
			t.Errorf("no tags: %s must be excluded by constraints (got %v)", excluded, got)
		}
	}

	// With -tags mytag: the tagged variant joins, the ignored and
	// wrong-GOOS files stay out.
	files, err = resolveFilesWithTags([]string{dir}, []string{"mytag"})
	if err != nil {
		t.Fatalf("resolveFilesWithTags mytag: %v", err)
	}
	got = baseSet(t, files)
	if !got["b_mytag.go"] {
		t.Errorf("mytag: b_mytag.go must be included (got %v)", got)
	}
	for _, excluded := range []string{"c_ignore.go", "d_plan9.go"} {
		if got[excluded] {
			t.Errorf("mytag: %s must stay excluded (got %v)", excluded, got)
		}
	}

	// Explicitly listed files bypass constraint filtering, like gc.
	files, err = resolveFilesWithTags([]string{filepath.Join(dir, "c_ignore.go")}, nil)
	if err != nil {
		t.Fatalf("resolveFilesWithTags explicit file: %v", err)
	}
	if got := baseSet(t, files); !got["c_ignore.go"] {
		t.Errorf("explicit file must bypass constraints (got %v)", got)
	}

	// A directory whose files are all excluded is an error naming the
	// directory, not a silent empty package.
	empty := t.TempDir()
	writeFile(t, empty, "only_ignored.go", "//go:build ignore\n\npackage p\n")
	if _, err := resolveFilesWithTags([]string{empty}, nil); err == nil {
		t.Error("all-excluded directory must error")
	} else if !strings.Contains(err.Error(), empty) {
		t.Errorf("error must name the directory, got: %v", err)
	}
}

func TestSplitBuildTags(t *testing.T) {
	got := splitBuildTags("foo,bar baz,,qux")
	want := []string{"foo", "bar", "baz", "qux"}
	if len(got) != len(want) {
		t.Fatalf("splitBuildTags: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitBuildTags[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
	if len(splitBuildTags("")) != 0 {
		t.Errorf("splitBuildTags(\"\") must be empty, got %v", splitBuildTags(""))
	}
}

func TestParseBuildFlagsTags(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-tags=foo,bar", "-o", "out", "pkg"})
	if err != nil {
		t.Fatalf("parseBuildFlags: %v", err)
	}
	if bf.tags != "foo,bar" {
		t.Errorf("bf.tags = %q, want %q", bf.tags, "foo,bar")
	}
	tags := splitBuildTags(bf.tags)
	if len(tags) != 2 || tags[0] != "foo" || tags[1] != "bar" {
		t.Errorf("split tags = %v, want [foo bar]", tags)
	}
	bf, err = parseBuildFlags([]string{"-tags", "spaced tag"})
	if err != nil {
		t.Fatalf("parseBuildFlags separate value: %v", err)
	}
	if bf.tags != "spaced tag" {
		t.Errorf("bf.tags = %q, want %q", bf.tags, "spaced tag")
	}
}

func envLookup(env []string, key string) (string, bool) {
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func TestWithBuildTagsEnv(t *testing.T) {
	t.Setenv("GRAALGO_BUILD_TAGS", "inherited")
	env := withBuildTagsEnv([]string{"mytag", "other"})
	v, ok := envLookup(env, "GRAALGO_BUILD_TAGS")
	if !ok {
		t.Fatal("GRAALGO_BUILD_TAGS missing from env")
	}
	if v != "inherited mytag other" {
		t.Errorf("merged tags = %q, want %q", v, "inherited mytag other")
	}
	// Exactly one entry: the merge replaces, never duplicates.
	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, "GRAALGO_BUILD_TAGS=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("GRAALGO_BUILD_TAGS appears %d times, want 1", count)
	}

	t.Setenv("GRAALGO_BUILD_TAGS", "")
	env = withBuildTagsEnv([]string{"mytag"})
	v, ok = envLookup(env, "GRAALGO_BUILD_TAGS")
	if !ok || v != "mytag" {
		t.Errorf("no inherited value: got %q (present=%v), want %q", v, ok, "mytag")
	}
}
