package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVetModule creates a temp module (module example.com/vettest)
// with the given files and returns its directory.
func writeVetModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/vettest\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func diagnosticsByFile(ds []Diagnostic) map[string][]string {
	byFile := make(map[string][]string)
	for _, d := range ds {
		byFile[filepath.Base(d.Pos.Filename)] = append(byFile[filepath.Base(d.Pos.Filename)], d.Message)
	}
	return byFile
}

// TestRunSplitsPackageAndTestFiles is the regression test for the
// mixed-package vet defect: a directory holding package foo, an
// internal test file (package foo) and an external test file (package
// foo_test) must be type-checked as two packages, each with full type
// info — not fed to go/types as one package, which failed the check
// and left every type-dependent analyzer running on partial info.
func TestRunSplitsPackageAndTestFiles(t *testing.T) {
	dir := writeVetModule(t, map[string]string{
		"foo.go": `package foo

import "fmt"

type MyInt32 int32

type Local struct {
	A int
}

func F() {
	fmt.Sprintf("%d", "hello")
	var n MyInt32
	_ = n << 40
	_ = Local{1}
}
`,
		"foo_test.go": `package foo

import "testing"

func TestF(t *testing.T) {
	F()
}
`,
		"foo_external_test.go": `package foo_test

import (
	"testing"

	foo "example.com/vettest"
)

func TestShift(t *testing.T) {
	var n foo.MyInt32
	_ = n << 40
}
`,
	})
	res, err := RunDetailed([]string{
		filepath.Join(dir, "foo.go"),
		filepath.Join(dir, "foo_test.go"),
		filepath.Join(dir, "foo_external_test.go"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both groups must type-check cleanly: no degraded group, so no
	// loud warning is owed in the common package-with-tests case.
	if len(res.TypeErrors) != 0 {
		t.Fatalf("expected no type errors, got: %v", res.TypeErrors)
	}
	byFile := diagnosticsByFile(res.Diagnostics)
	var all []string
	for _, d := range res.Diagnostics {
		all = append(all, d.Message)
	}
	joined := strings.Join(all, "\n")

	// printf in the main package file must fire: before the split the
	// failed Check left Info.Types empty and it was silently missed.
	if !strings.Contains(strings.Join(byFile["foo.go"], "\n"), "expects integer") {
		t.Errorf("expected printf diagnostic in foo.go, got:\n%s", joined)
	}
	// shift on a locally declared int32 type must fire in the main
	// package file.
	if !strings.Contains(strings.Join(byFile["foo.go"], "\n"), "too small for shift") {
		t.Errorf("expected shift diagnostic in foo.go, got:\n%s", joined)
	}
	// shift on the same type used through the package import in the
	// external test file must fire too: the foo_test group imports
	// the already-checked foo package.
	if !strings.Contains(strings.Join(byFile["foo_external_test.go"], "\n"), "too small for shift") {
		t.Errorf("expected shift diagnostic in foo_external_test.go, got:\n%s", joined)
	}
	// A local unkeyed literal must NOT be flagged: composites only
	// flags imported types, which needs a non-nil Pass.Pkg — the
	// exact thing the failed single Check destroyed.
	if strings.Contains(joined, "composite literal uses unkeyed fields") {
		t.Errorf("false positive: local unkeyed literal flagged:\n%s", joined)
	}
	// The internal test file's valid Test function must not be
	// flagged by the tests analyzer.
	if msgs := byFile["foo_test.go"]; len(msgs) != 0 {
		t.Errorf("unexpected diagnostics in foo_test.go: %v", msgs)
	}
	if msgs := byFile["foo_external_test.go"]; len(msgs) != 1 {
		t.Errorf("expected exactly the shift diagnostic in foo_external_test.go, got: %v", msgs)
	}
}

// TestRunDetailedReportsTypeErrors checks the loud path: a genuine
// type error is reported (not discarded) while analyzers still run on
// best-effort info.
func TestRunDetailedReportsTypeErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	src := `package main

import "fmt"

func main() {
	fmt.Printf("%d", "hello")
	undefinedIdent()
}
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := RunDetailed([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TypeErrors) != 1 {
		t.Fatalf("expected 1 type error, got: %v", res.TypeErrors)
	}
	if res.TypeErrors[0].Package != "main" {
		t.Errorf("expected type error for package main, got %q", res.TypeErrors[0].Package)
	}
	if !strings.Contains(res.TypeErrors[0].Err.Error(), "undefinedIdent") {
		t.Errorf("expected type error to name undefinedIdent, got: %v", res.TypeErrors[0].Err)
	}
	// Best-effort is preserved: the printf misuse in the same file
	// is still reported even though type checking failed.
	joined := ""
	for _, d := range res.Diagnostics {
		joined += d.Message + "\n"
	}
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("expected printf diagnostic despite type error, got:\n%s", joined)
	}
}

func TestDeriveImportPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m // trailing comment\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub", "pkg")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if got, ok := deriveImportPath(sub); !ok || got != "example.com/m/sub/pkg" {
		t.Errorf("deriveImportPath(sub) = %q, %v; want %q, true", got, ok, "example.com/m/sub/pkg")
	}
	if got, ok := deriveImportPath(root); !ok || got != "example.com/m" {
		t.Errorf("deriveImportPath(root) = %q, %v; want %q, true", got, ok, "example.com/m")
	}
	// No enclosing go.mod: a fresh temp dir has none above it.
	other := t.TempDir()
	if got, ok := deriveImportPath(other); ok || got != "" {
		t.Errorf("deriveImportPath(no go.mod) = %q, %v; want %q, false", got, ok, "")
	}
	// A go.mod with no module line derives nothing.
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "go.mod"), []byte("go 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, ok := deriveImportPath(empty); ok || got != "" {
		t.Errorf("deriveImportPath(no module line) = %q, %v; want %q, false", got, ok, "")
	}
}

func TestParseModulePath(t *testing.T) {
	if got := parseModulePath("module example.com/m\n\ngo 1.21\n"); got != "example.com/m" {
		t.Errorf("parseModulePath = %q; want %q", got, "example.com/m")
	}
	if got := parseModulePath("module example.com/m // trailing comment\n"); got != "example.com/m" {
		t.Errorf("parseModulePath with comment = %q; want %q", got, "example.com/m")
	}
	if got := parseModulePath("go 1.21\n"); got != "" {
		t.Errorf("parseModulePath without module line = %q; want %q", got, "")
	}
	if got := parseModulePath(""); got != "" {
		t.Errorf("parseModulePath empty = %q; want %q", got, "")
	}
}
