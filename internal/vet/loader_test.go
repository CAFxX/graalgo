package vet

// Regression tests for R05-V-01: the vet loader must load packages
// properly instead of type-checking all files as one package with
// importer.Default() and discarding the error.
//
// The old loader silently degraded on the default invocation:
//   - a tested package (foo.go + foo_test.go) failed the single-package
//     check, so analyzers ran on incomplete type info;
//   - module-local imports did not resolve, so analyzers that need
//     Info.Types (printf, copylocks, lostcancel, ...) skipped silently;
//   - one syntax error aborted the whole run with zero diagnostics.
//
// These tests run the loader through Run, which shells out to `go list`
// via golang.org/x/tools/go/packages; the nested `go list` inherits the
// test process environment (PATH, GOCACHE, GOMODCACHE), as with any
// go/packages consumer.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLoaderFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func loaderDiagnostics(t *testing.T, paths ...string) []Diagnostic {
	t.Helper()
	ds, err := Run(paths)
	if err != nil {
		t.Fatalf("Run(%v) returned error: %v", paths, err)
	}
	return ds
}

func diagnosticsForFile(ds []Diagnostic, name string) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if filepath.Base(d.Pos.Filename) == name {
			out = append(out, d)
		}
	}
	return out
}

func joinMessages(ds []Diagnostic) string {
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	return strings.Join(msgs, "\n")
}

// TestLoaderTestedPackage: a normal tested package (package clause foo
// plus an external test package foo_test) must be vetted as a whole:
// findings from both files, with no duplicate for the shared file.
func TestLoaderTestedPackage(t *testing.T) {
	dir := t.TempDir()
	writeLoaderFile(t, dir, "go.mod", "module example.com/foo\n\ngo 1.21\n")
	foo := writeLoaderFile(t, dir, "foo.go", `package foo

import "fmt"

func F() {
	fmt.Printf("%d", "hello")
}
`)
	writeLoaderFile(t, dir, "foo_test.go", `package foo_test

import (
	"fmt"
	"testing"
)

func TestF(t *testing.T) {
	fmt.Printf("%d", "world")
}
`)

	ds := loaderDiagnostics(t, foo, filepath.Join(dir, "foo_test.go"))
	joined := joinMessages(ds)
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("expected printf diagnostic, got:\n%s", joined)
	}
	if got := diagnosticsForFile(ds, "foo.go"); len(got) == 0 {
		t.Errorf("expected diagnostics for foo.go, got none (all):\n%s", joined)
	}
	if got := diagnosticsForFile(ds, "foo_test.go"); len(got) == 0 {
		t.Errorf("expected diagnostics for foo_test.go, got none (all):\n%s", joined)
	}
	// The non-test file is shared by the plain package and its test
	// variant; its finding must be reported exactly once.
	count := 0
	for _, d := range diagnosticsForFile(ds, "foo.go") {
		if strings.Contains(d.Message, "expects integer") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 printf diagnostic for foo.go, got %d:\n%s", count, joined)
	}
}

// TestLoaderModuleLocalImport: an import of a module-local package must
// resolve, so analyzers see the real argument types.
func TestLoaderModuleLocalImport(t *testing.T) {
	dir := t.TempDir()
	writeLoaderFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.21\n")
	writeLoaderFile(t, dir, "sub/sub.go", `package sub

func Hello() string { return "hi" }
`)
	m := writeLoaderFile(t, dir, "m.go", `package m

import (
	"fmt"

	"example.com/m/sub"
)

func F() {
	fmt.Printf("%d", sub.Hello())
}
`)

	ds := loaderDiagnostics(t, m)
	if joined := joinMessages(ds); !strings.Contains(joined, "expects integer") {
		t.Errorf("expected printf diagnostic for %%d with sub.Hello() string, got:\n%s", joined)
	}
}

// TestLoaderSyntaxErrorContinues: one file with a syntax error must not
// abort the run: the parse error is reported per file and the remaining
// files are still vetted.
func TestLoaderSyntaxErrorContinues(t *testing.T) {
	dir := t.TempDir()
	writeLoaderFile(t, dir, "go.mod", "module example.com/broken\n\ngo 1.21\n")
	good := writeLoaderFile(t, dir, "good.go", `package broken

import "fmt"

func Good() {
	fmt.Printf("%d", "good")
}
`)
	writeLoaderFile(t, dir, "broken.go", `package broken

import "fmt"

func Broken() {
	fmt.Printf("%d", "broken"
}
`)

	ds := loaderDiagnostics(t, good, filepath.Join(dir, "broken.go"))
	joined := joinMessages(ds)
	if got := diagnosticsForFile(ds, "broken.go"); len(got) == 0 {
		t.Errorf("expected a diagnostic for the syntax error in broken.go, got none:\n%s", joined)
	}
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("expected printf diagnostic for good.go despite broken.go, got:\n%s", joined)
	}
}

// TestLoaderMissingFile: a nonexistent input file is a hard error, never
// a silent "no issues found".
func TestLoaderMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Run([]string{filepath.Join(dir, "nope.go")}); err == nil {
		t.Errorf("expected an error for a nonexistent file, got nil")
	}
}

// TestLoaderImportErrorSurfaced: a type-check failure (unresolvable
// import) is surfaced as a diagnostic instead of being discarded.
func TestLoaderImportErrorSurfaced(t *testing.T) {
	dir := t.TempDir()
	writeLoaderFile(t, dir, "go.mod", "module example.com/badimp\n\ngo 1.21\n")
	m := writeLoaderFile(t, dir, "m.go", `package badimp

import "example.com/nonexistent/pkg"

var _ = pkg.Thing
`)

	ds := loaderDiagnostics(t, m)
	if joined := joinMessages(ds); !strings.Contains(joined, "could not import") {
		t.Errorf("expected a diagnostic surfacing the import failure, got:\n%s", joined)
	}
}

// TestLoaderNoGoBinary: without a `go` binary to drive package loading,
// the run fails loudly instead of silently degrading.
func TestLoaderNoGoBinary(t *testing.T) {
	dir := t.TempDir()
	p := writeLoaderFile(t, dir, "m.go", `package main

func main() {}
`)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if _, err := Run([]string{p}); err == nil {
		t.Errorf("expected an error when no `go` binary is available, got nil")
	}
}
