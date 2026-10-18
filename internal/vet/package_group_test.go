package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageClauseGrouping is the regression test for R23-P1-2: input
// files with different package clauses (a package and its external test
// package) must be type-checked as separate packages, not merged into one
// pseudo-package.
//
// Before the fix, Run passed all files to a single types.Config.Check.
// go/types then ignored every file whose package clause did not match the
// first file's, leaving those files with no type info at all — so every
// type-keyed analyzer silently missed findings in them.
func TestPackageClauseGrouping(t *testing.T) {
	dir := t.TempDir()
	fooSrc := `package foo

import "fmt"

func Foo() {
	fmt.Printf("%d", "not an int")
}
`
	testSrc := `package foo_test

import (
	"fmt"
	"testing"
)

func TestFoo(t *testing.T) {
	fmt.Printf("%s", 42)
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
		t.Fatal(err)
	}
	t.Logf("diagnostics:")
	for _, d := range ds {
		t.Logf("  %s: %s", d.Pos, d.Message)
	}

	// The type-keyed printf check must fire in package foo ...
	assertDiagAt(t, ds, fooPath, 6, "expects integer")
	// ... and separately in the external test package foo_test ...
	assertDiagAt(t, ds, testPath, 9, "expects string")
	// ... with nothing leaking across the package boundary: exactly one
	// diagnostic per file.
	if len(ds) != 2 {
		t.Errorf("expected exactly 2 diagnostics (one per package), got %d", len(ds))
	}
}

// assertDiagAt fails unless ds contains a diagnostic whose message
// contains want, positioned at path:line.
func assertDiagAt(t *testing.T, ds []Diagnostic, path string, line int, want string) {
	t.Helper()
	for _, d := range ds {
		if d.Pos.Filename == path && d.Pos.Line == line && strings.Contains(d.Message, want) {
			return
		}
	}
	t.Errorf("expected diagnostic containing %q at %s:%d, got:", want, path, line)
	for _, d := range ds {
		t.Errorf("  %s: %s", d.Pos, d.Message)
	}
}

// TestPackageClauseGroupingReversedOrder pins that the grouping does not
// depend on input file order: whichever file comes first, both packages
// are still checked independently.
func TestPackageClauseGroupingReversedOrder(t *testing.T) {
	dir := t.TempDir()
	fooSrc := `package foo

import "fmt"

func Foo() {
	fmt.Printf("%d", "not an int")
}
`
	testSrc := `package foo_test

import (
	"fmt"
	"testing"
)

func TestFoo(t *testing.T) {
	fmt.Printf("%s", 42)
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

	// Deliberately pass the test file first: before the fix, go/types
	// adopted its package clause ("foo_test") and ignored foo.go instead.
	ds, err := Run([]string{testPath, fooPath})
	if err != nil {
		t.Fatal(err)
	}
	assertDiagAt(t, ds, fooPath, 6, "expects integer")
	assertDiagAt(t, ds, testPath, 9, "expects string")
	if len(ds) != 2 {
		t.Errorf("expected exactly 2 diagnostics (one per package), got %d", len(ds))
	}
}

// TestExternalTestPackageImportsTestedPackage covers the groupImporter
// fallback: when the external test package imports the package under test
// by its clause name and both were passed to Run together, the import must
// resolve so type-keyed checks see through it.
func TestExternalTestPackageImportsTestedPackage(t *testing.T) {
	dir := t.TempDir()
	fooSrc := `package foo

func Answer() int { return 42 }
`
	testSrc := `package foo_test

import (
	"fmt"
	foo "foo"
	"testing"
)

func TestAnswer(t *testing.T) {
	fmt.Printf("%s", foo.Answer())
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
		t.Fatal(err)
	}
	// %s with an int only fires when foo.Answer()'s type resolves to int
	// through the cross-package import.
	assertDiagAt(t, ds, testPath, 10, "expects string")
	if len(ds) != 1 {
		t.Errorf("expected exactly 1 diagnostic, got %d", len(ds))
		for _, d := range ds {
			t.Errorf("  %s: %s", d.Pos, d.Message)
		}
	}
}
