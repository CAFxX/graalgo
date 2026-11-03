package main

import (
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Regression tests for REVIEW-2026-11-03-12 P1-2: `graalgo run`/`build`
// on a package directory used to compile every *.go file regardless of
// build constraints, so a package with foo_linux.go + foo_darwin.go
// (same package name, disjoint symbols) built on Linux with darwin-only
// code included, or failed on duplicate declarations where `go build`
// succeeds. Directory expansion must now return only the files `go
// build` would compile for the current platform.
//
// Each excluded fixture declares a DUPLICATE of a symbol in main.go plus
// an init with an observable effect, so a pre-fix file list (which
// includes the fixture) fails these assertions — and, end to end, would
// fail the backend with a redeclaration or run the excluded init. The
// assertions are on the resolved file list because file selection is
// the layer this fix owns: the backend compiles exactly the files it is
// given, which existing tests already cover.

func TestResolveFilesFiltersFilenameSuffixExcludedFile(t *testing.T) {
	if build.Default.GOOS == "darwin" {
		t.Skip("the _darwin fixture is buildable on darwin; nothing to exclude")
	}
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc Shared() int { return 1 }\n\nfunc main() {}\n")
	// Excluded by the _darwin filename suffix alone (no //go:build line).
	writeFile(t, dir, "foo_darwin.go", "package main\n\nfunc Shared() int { return 2 }\n\nfunc init() { panic(\"darwin-only init ran\") }\n")

	files, err := resolveFiles([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "main.go")}
	if !slices.Equal(files, want) {
		t.Fatalf("resolveFiles(%q) = %v, want %v", dir, files, want)
	}
}

func TestResolveFilesFiltersGoBuildExcludedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc Shared() int { return 1 }\n\nfunc main() {}\n")
	// Excluded by a //go:build line alone (neutral filename, no suffix).
	writeFile(t, dir, "never.go", "//go:build ignore\n\npackage main\n\nfunc Shared() int { return 2 }\n\nfunc init() { panic(\"build-excluded init ran\") }\n")

	files, err := resolveFiles([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "main.go")}
	if !slices.Equal(files, want) {
		t.Fatalf("resolveFiles(%q) = %v, want %v", dir, files, want)
	}
}

func TestResolveFilesNoArgsFiltersExcludedFile(t *testing.T) {
	if build.Default.GOOS == "darwin" {
		t.Skip("the _darwin fixture is buildable on darwin; nothing to exclude")
	}
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "foo_darwin.go", "package main\n\nfunc init() { panic(\"darwin-only init ran\") }\n")
	// t.Chdir needs go1.24 (module is go1.21); os.Chdir with restore
	// instead. This package runs no test in parallel, so the process
	// cwd switch is safe here.
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	files, err := resolveFiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"main.go"}
	if !slices.Equal(files, want) {
		t.Fatalf("resolveFiles(nil) in %q = %v, want %v", dir, files, want)
	}
}

func TestResolveFilesAllExcludedDirErrors(t *testing.T) {
	if build.Default.GOOS == "darwin" {
		t.Skip("the _darwin fixture is buildable on darwin; nothing to exclude")
	}
	dir := t.TempDir()
	writeFile(t, dir, "only_darwin.go", "package main\n\nfunc main() {}\n")

	files, err := resolveFiles([]string{dir})
	if err == nil {
		t.Fatalf("resolveFiles(%q) = %v, want an error: no buildable files", dir, files)
	}
	if files != nil {
		t.Fatalf("resolveFiles(%q) = %v, want no files", dir, files)
	}
}

func TestResolveFilesExplicitFileUnfiltered(t *testing.T) {
	// Explicitly named files compile as listed, like `go run a.go`:
	// constraint filtering applies to directory expansion only.
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	darwin := filepath.Join(dir, "foo_darwin.go")
	writeFile(t, dir, "foo_darwin.go", "package main\n\nfunc init() {}\n")

	files, err := resolveFiles([]string{darwin})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files, []string{darwin}) {
		t.Fatalf("resolveFiles(%q) = %v, want %v", darwin, files, []string{darwin})
	}
}

func TestResolveFilesMixedPackageFallsBackToAllFiles(t *testing.T) {
	// A directory with two packages is not importable, so constraint
	// evaluation fails and the raw file list is returned for the backend
	// to report the real (mixed-package) error — not silently filtered.
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "b.go", "package other\n\nfunc X() {}\n")

	files, err := resolveFiles([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")}
	if !slices.Equal(files, want) {
		t.Fatalf("resolveFiles(%q) = %v, want %v", dir, files, want)
	}
}

func TestBuildableGoFilesIncludesCgoFile(t *testing.T) {
	if !build.Default.CgoEnabled {
		t.Skip("cgo disabled: cgo files are IgnoredGoFiles, not CgoFiles")
	}
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	// Classification only needs the `import "C"` clause; cgo itself
	// never runs here.
	writeFile(t, dir, "cgo.go", "package main\n\nimport \"C\"\n\nfunc CgoHelper() {}\n")

	files := buildableGoFiles(dir)
	if !slices.Contains(files, filepath.Join(dir, "cgo.go")) {
		t.Fatalf("buildableGoFiles(%q) = %v, want it to contain cgo.go", dir, files)
	}
}
