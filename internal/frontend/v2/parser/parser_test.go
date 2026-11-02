// Stage-1 tests (R23): the parser accepts well-formed packages and
// rejects malformed input loudly, through both entry points. ParseFiles
// (file-reading) and ParseSources (in-memory) must agree — ParseFiles
// delegates to ParseSources, so the two paths cannot drift apart.
package parser

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	fn := filepath.Join(dir, name)
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return fn
}

func TestParseFilesAccept(t *testing.T) {
	dir := t.TempDir()
	// Deliberately non-alphabetical input order: Result.Files must
	// preserve the caller's order, not sort it.
	b := writeFile(t, dir, "b.go", "package demo\n\nfunc B() {}\n")
	a := writeFile(t, dir, "a.go", "package demo\n\nfunc A() {}\n")
	res, err := ParseFiles([]string{b, a})
	if err != nil {
		t.Fatalf("ParseFiles: %v", err)
	}
	if res.Pkg.Name != "demo" {
		t.Fatalf("package name = %q, want demo", res.Pkg.Name)
	}
	if len(res.Files) != 2 {
		t.Fatalf("Files has %d entries, want 2", len(res.Files))
	}
	if res.Files[0] != b || res.Files[1] != a {
		t.Fatalf("Files = %v, want input order [%s %s]", res.Files, b, a)
	}
	if len(res.Pkg.Files) != 2 {
		t.Fatalf("Pkg.Files has %d entries, want 2", len(res.Pkg.Files))
	}
}

func TestParseFilesReject(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if _, err := ParseFiles(nil); err == nil {
			t.Fatalf("expected error for no input files")
		}
	})
	t.Run("missing", func(t *testing.T) {
		if _, err := ParseFiles([]string{filepath.Join(t.TempDir(), "nope.go")}); err == nil {
			t.Fatalf("expected error for a missing file")
		}
	})
	t.Run("syntax", func(t *testing.T) {
		dir := t.TempDir()
		fn := writeFile(t, dir, "bad.go", "package demo\n\nfunc Broken( {\n")
		if _, err := ParseFiles([]string{fn}); err == nil {
			t.Fatalf("expected error for a syntax error")
		} else if !strings.Contains(err.Error(), "parser:") {
			t.Fatalf("error %q lacks the parser: prefix", err.Error())
		}
	})
	t.Run("mixed packages", func(t *testing.T) {
		dir := t.TempDir()
		a := writeFile(t, dir, "a.go", "package one\n")
		b := writeFile(t, dir, "b.go", "package two\n")
		if _, err := ParseFiles([]string{a, b}); err == nil {
			t.Fatalf("expected error for mixed package clauses")
		} else if !strings.Contains(err.Error(), "declares package") {
			t.Fatalf("error %q does not name the package mismatch", err.Error())
		}
	})
}

func TestParseSourcesAcceptAndReject(t *testing.T) {
	fset := token.NewFileSet()
	pkg, err := ParseSources(fset, map[string]string{
		"a.go": "package demo\n\nfunc A() {}\n",
		"b.go": "package demo\n\nfunc B() {}\n",
	})
	if err != nil {
		t.Fatalf("ParseSources: %v", err)
	}
	if pkg.Name != "demo" || len(pkg.Files) != 2 {
		t.Fatalf("package = %q with %d files, want demo with 2", pkg.Name, len(pkg.Files))
	}

	if _, err := ParseSources(token.NewFileSet(), map[string]string{
		"a.go": "package one\n",
		"b.go": "package two\n",
	}); err == nil {
		t.Fatalf("expected error for mixed package clauses")
	}
	if _, err := ParseSources(token.NewFileSet(), map[string]string{
		"bad.go": "package demo\n\nfunc Broken( {\n",
	}); err == nil {
		t.Fatalf("expected error for a syntax error")
	}
}

// TestParseFilesDelegatesToParseSources pins the wiring: the same source
// text parsed from disk and from memory must yield the same package
// name and file count. If ParseFiles ever stops delegating to
// ParseSources, a drift between the two paths shows up here first.
func TestParseFilesDelegatesToParseSources(t *testing.T) {
	src := "package demo\n\nfunc A() int { return 1 }\n"
	dir := t.TempDir()
	fn := writeFile(t, dir, "a.go", src)
	fromFiles, err := ParseFiles([]string{fn})
	if err != nil {
		t.Fatalf("ParseFiles: %v", err)
	}
	fromSources, err := ParseSources(token.NewFileSet(), map[string]string{"a.go": src})
	if err != nil {
		t.Fatalf("ParseSources: %v", err)
	}
	if fromFiles.Pkg.Name != fromSources.Name {
		t.Fatalf("package names differ: %q vs %q", fromFiles.Pkg.Name, fromSources.Name)
	}
	if len(fromFiles.Pkg.Files) != len(fromSources.Files) {
		t.Fatalf("file counts differ: %d vs %d", len(fromFiles.Pkg.Files), len(fromSources.Files))
	}
}
