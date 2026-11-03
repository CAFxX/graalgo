package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunSeparatesTestPackage is the regression test for review
// REVIEW-2026-11-03-12 P1-1(a): a directory holding a package and its
// external test package must vet as two passes, not as one merged
// synthetic package. Both passes must type-check cleanly (no error)
// and findings from both files must be reported — the test-package
// finding (copylocks) depends on valid type info for the test package
// alone, which a merged check cannot provide.
func TestRunSeparatesTestPackage(t *testing.T) {
	dir := t.TempDir()
	fooSrc := `package foo

import "fmt"

func Greet() {
	fmt.Printf("%d", "hello")
}
`
	testSrc := `package foo_test

import (
	"fmt"
	"sync"
)

func copyLock(m sync.Mutex) {
	_ = m
}

func Check() {
	fmt.Printf("%d", "hello")
}
`
	fooPath := filepath.Join(dir, "foo.go")
	testPath := filepath.Join(dir, "foo_test.go")
	if err := os.WriteFile(fooPath, []byte(fooSrc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{fooPath, testPath})
	if err != nil {
		t.Fatalf("Run(foo + foo_test) returned error, want clean separate passes: %v", err)
	}
	var filesSeen []string
	var msgs []string
	for _, d := range ds {
		filesSeen = append(filesSeen, d.Pos.Filename)
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, wantFile := range []string{fooPath, testPath} {
		found := false
		for _, f := range filesSeen {
			if f == wantFile {
				found = true
			}
		}
		if !found {
			t.Errorf("no diagnostic from %s, got diagnostics in %v:\n%s", wantFile, filesSeen, joined)
		}
	}
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("expected printf diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "copies lock") {
		t.Errorf("expected copylocks diagnostic from the test package, got:\n%s", joined)
	}
}

// TestRunCrossFileTypeError is the regression test for review
// REVIEW-2026-11-03-12 P1-1(b): a package whose type error spans two
// files must not vet clean. Run must return the type-check failure so
// cmdVet can never print "vet: no issues found" for it.
func TestRunCrossFileTypeError(t *testing.T) {
	dir := t.TempDir()
	aSrc := `package p

func Get() int {
	return Value
}
`
	bSrc := `package p

var Value = "not an int"
`
	aPath := filepath.Join(dir, "a.go")
	bPath := filepath.Join(dir, "b.go")
	if err := os.WriteFile(aPath, []byte(aSrc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte(bSrc), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Run([]string{aPath, bPath})
	if err == nil {
		t.Fatal("Run() returned nil error for a package with a cross-file type error, want a type-check failure")
	}
	if !strings.Contains(err.Error(), "type check") {
		t.Errorf("error should identify a type-check failure, got: %v", err)
	}
}

// TestRunMixedPackagesInOneDir rejects a directory mixing two package
// names: such files are not one package, and checking them together
// would silently vet a package that does not exist.
func TestRunMixedPackagesInOneDir(t *testing.T) {
	dir := t.TempDir()
	fooPath := filepath.Join(dir, "foo.go")
	barPath := filepath.Join(dir, "bar.go")
	if err := os.WriteFile(fooPath, []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(barPath, []byte("package bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Run([]string{fooPath, barPath})
	if err == nil {
		t.Fatal("Run() returned nil error for one directory with two packages, want an error")
	}
	if !strings.Contains(err.Error(), "found packages") {
		t.Errorf("error should report mixed packages, got: %v", err)
	}
}

// TestRunTwoDirsSamePackageName checks that files from two different
// directories are never merged, even when they share a package name:
// each directory is its own package pass.
func TestRunTwoDirsSamePackageName(t *testing.T) {
	src := `package foo

import "fmt"

func Greet() {
	fmt.Printf("%d", "hello")
}
`
	var paths []string
	var dirs []string
	for i := 0; i < 2; i++ {
		dir := t.TempDir()
		dirs = append(dirs, dir)
		p := filepath.Join(dir, "foo.go")
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	ds, err := Run(paths)
	if err != nil {
		t.Fatalf("Run(two dirs, same package name) returned error, want clean separate passes: %v", err)
	}
	seen := make(map[string]bool)
	for _, d := range ds {
		seen[d.Pos.Filename] = true
	}
	for _, p := range paths {
		if !seen[p] {
			t.Errorf("no diagnostic from %s", p)
		}
	}
}
