package vet

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunTypeCheckFailureIsLoud: R02 — a type-checking failure must be
// returned to the caller, never swallowed, so no caller can print a
// clean verdict over degraded type information.
func TestRunTypeCheckFailureIsLoud(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	src := `package main

func main() {
	undefinedFunction()
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run([]string{path})
	if err == nil {
		t.Fatal("Run on a file with an undefined identifier returned nil error, want TypeCheckError")
	}
	var tce *TypeCheckError
	if !errors.As(err, &tce) {
		t.Fatalf("Run error = %T (%v), want *TypeCheckError", err, err)
	}
	if !strings.Contains(err.Error(), "undefined") {
		t.Errorf("TypeCheckError = %v, want it to mention the undefined identifier", err)
	}
}

// TestRunMixedPackageClausesFail: R02 — files with different package
// clauses (e.g. a package plus its external test package) cannot be
// checked as one package; Run must fail loudly rather than run
// analyzers over the degraded result and report nothing.
func TestRunMixedPackageClausesFail(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "a.go")
	ext := filepath.Join(dir, "a_test.go")
	if err := os.WriteFile(pkg, []byte("package foo\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ext, []byte("package foo_test\n\nfunc Example() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run([]string{pkg, ext})
	if err == nil {
		t.Fatal("Run on mixed package clauses returned nil error, want TypeCheckError")
	}
	var tce *TypeCheckError
	if !errors.As(err, &tce) {
		t.Fatalf("Run error = %T (%v), want *TypeCheckError", err, err)
	}
}

// TestRunValidFileHasNoTypeCheckError pins the other side: a clean
// type-check still returns a nil error, so the loud-failure change
// does not turn every vet run into a failure.
func TestRunValidFileHasNoTypeCheckError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run([]string{path})
	if err != nil {
		t.Fatalf("Run on a valid file returned error: %v", err)
	}
}
