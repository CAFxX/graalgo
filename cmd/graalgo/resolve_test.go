package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// TestResolveFilesTagsBuildGated (N2): a package directory holding two
// //go:build-gated files that declare the SAME symbol must never be
// handed to the backend as one unit — that is the duplicate-declaration
// failure build/run/vet hit with the old bare-glob expansion. With a
// tag, only that tag's variant is returned; without a tag, neither
// variant is returned (go build semantics: a tag-gated file belongs to
// the package only when its tag is requested).
func TestResolveFilesTagsBuildGated(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plain.go", "package p\n\nfunc Plain() string { return \"plain\" }\n")
	write("variant_a.go", "//go:build taga\n\npackage p\n\nfunc Which() string { return \"a\" }\n")
	write("variant_b.go", "//go:build tagb\n\npackage p\n\nfunc Which() string { return \"b\" }\n")

	base := func(files []string) []string {
		var names []string
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		return names
	}

	for _, tc := range []struct {
		tags string
		want []string
	}{
		{"taga", []string{"plain.go", "variant_a.go"}},
		{"tagb", []string{"plain.go", "variant_b.go"}},
		// No tags: both gated variants are excluded, so the two
		// definitions of Which can never collide.
		{"", []string{"plain.go"}},
	} {
		files, err := resolveFilesTags([]string{dir}, tc.tags)
		if err != nil {
			t.Fatalf("tags=%q: resolveFilesTags: %v", tc.tags, err)
		}
		got := base(files)
		slices.Sort(got)
		slices.Sort(tc.want)
		if !slices.Equal(got, tc.want) {
			t.Errorf("tags=%q: files = %v, want %v", tc.tags, got, tc.want)
		}
	}
}

// TestResolveFilesTagsPlatformVariants (N2): filename-suffix platform
// variants (foo_linux.go / foo_windows.go) declaring the same symbol
// are filtered to the current platform even with no -tags.
func TestResolveFilesTagsPlatformVariants(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plat_linux.go", "package p\n\nfunc Platform() string { return \"linux\" }\n")
	write("plat_windows.go", "package p\n\nfunc Platform() string { return \"windows\" }\n")
	write("plat_darwin.go", "package p\n\nfunc Platform() string { return \"darwin\" }\n")

	files, err := resolveFilesTags([]string{dir}, "")
	if err != nil {
		t.Fatalf("resolveFilesTags: %v", err)
	}
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	want := "plat_" + runtime.GOOS + ".go"
	if len(names) != 1 || names[0] != want {
		t.Errorf("platform files = %v, want [%s] on GOOS=%s", names, want, runtime.GOOS)
	}
}

// TestResolveFilesTagsNoArg (N2): the no-arg current-directory case
// gets the same constraint filtering as an explicit directory.
func TestResolveFilesTagsNoArg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("//go:build taga\n\npackage p\n\nfunc Which() string { return \"a\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("//go:build tagb\n\npackage p\n\nfunc Which() string { return \"b\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// t.Chdir needs go1.24; the root module is go1.21, so chdir
	// manually (restored on return).
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	files, err := resolveFilesTags(nil, "tagb")
	if err != nil {
		t.Fatalf("resolveFilesTags(nil, tagb): %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "b.go" {
		t.Errorf("no-arg tagged files = %v, want [b.go]", files)
	}
}

// TestResolveFilesTagsExplicitFilesUnfiltered (N2): explicit .go file
// arguments are never constraint-filtered, matching `go build` —
// otherwise a user could not build a single gated file on purpose.
func TestResolveFilesTagsExplicitFilesUnfiltered(t *testing.T) {
	dir := t.TempDir()
	gated := filepath.Join(dir, "gated.go")
	if err := os.WriteFile(gated, []byte("//go:build taga\n\npackage p\n\nfunc Which() string { return \"a\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := resolveFilesTags([]string{gated}, "")
	if err != nil {
		t.Fatalf("resolveFilesTags(explicit): %v", err)
	}
	if len(files) != 1 || files[0] != gated {
		t.Errorf("explicit files = %v, want [%s]", files, gated)
	}
}
