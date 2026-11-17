// Unit tests for stage 1 (parser) of the frontend/v2 pipeline.
//
// The parser is a thin wrapper over go/parser: syntax errors are fatal
// here, and every file must declare the same package. These tests pin
// that contract directly instead of only exercising it end-to-end via
// the driver.
package parser

import (
	"go/token"
	"strings"
	"testing"
)

// TestParseSourcesSamePackage pins the success contract: several files
// declaring one package assemble into one *ast.Package with all files.
func TestParseSourcesSamePackage(t *testing.T) {
	fset := token.NewFileSet()
	pkg, err := ParseSources(fset, map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
		"b.go": "package main\n\nfunc B() {}\n",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pkg.Name != "main" {
		t.Errorf("package name = %q, want main", pkg.Name)
	}
	if len(pkg.Files) != 2 {
		t.Errorf("files = %d, want 2", len(pkg.Files))
	}
}

// TestParseSourcesMixedPackagesRejected pins the same-package
// enforcement: files declaring different packages are an error, never
// a silently merged package.
func TestParseSourcesMixedPackagesRejected(t *testing.T) {
	fset := token.NewFileSet()
	_, err := ParseSources(fset, map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
		"b.go": "package other\n\nfunc B() {}\n",
	})
	if err == nil {
		t.Fatalf("expected an error for mixed packages, got success")
	}
	if !strings.Contains(err.Error(), "parser:") {
		t.Errorf("error %q does not carry the parser prefix", err.Error())
	}
}

// TestParseSourcesSyntaxError pins that a syntax error is fatal at this
// stage, not deferred to the checker.
func TestParseSourcesSyntaxError(t *testing.T) {
	fset := token.NewFileSet()
	_, err := ParseSources(fset, map[string]string{
		"a.go": "package main\n\nfunc {\n",
	})
	if err == nil {
		t.Fatalf("expected an error for a syntax error, got success")
	}
}

// TestParseFilesNoInput pins that parsing zero files is an error.
func TestParseFilesNoInput(t *testing.T) {
	_, err := ParseFiles(nil)
	if err == nil {
		t.Fatalf("expected an error for no input files, got success")
	}
}

// TestParseFilesMissingFile pins that an unreadable file is an error,
// not an empty package.
func TestParseFilesMissingFile(t *testing.T) {
	_, err := ParseFiles([]string{"/nonexistent/no-such-file-1707.go"})
	if err == nil {
		t.Fatalf("expected an error for a missing file, got success")
	}
}
