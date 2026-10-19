package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two packages in different directories must be type-checked and analyzed
// as separate packages, never merged into one fake package. (Regression
// test: the old code ran conf.Check over all files at once, producing
// redeclarations and wrong resolution whenever more than one package was
// passed.)
func TestMultiPackage(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	// Both packages declare a top-level `helper` — legal in separate
	// packages, a redeclaration if merged into one fake package.
	for i, pkg := range []string{"p1", "p2"} {
		d := filepath.Join(dir, "pkg"+string(rune('a'+i)))
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		src := "package " + pkg + "\n\nimport \"fmt\"\n\nfunc helper() int { return 1 }\n\nfunc F() {\n\tfmt.Printf(\"%d\", \"hello\")\n}\n"
		p := filepath.Join(d, "bad.go")
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	ds, err := Run(paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Each package must produce its own printf diagnostic, attributed to
	// its own file.
	seen := map[string]bool{}
	for _, d := range ds {
		if strings.Contains(d.Message, "expects integer") {
			seen[d.Pos.Filename] = true
		}
	}
	for _, p := range paths {
		if !seen[p] {
			t.Errorf("expected printf diagnostic for %s, diagnostics:\n%v", p, ds)
		}
	}
}

// A package with an unresolvable import must still be analyzed
// best-effort, and the type-check failure must be surfaced (as a stderr
// warning) rather than silently discarded. Note: go/types records no type
// info at all when an import fails to resolve, so this exercises a purely
// syntactic analyzer (unreachable), which must still fire.
func TestBestEffortUnresolvableImport(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import _ "example.com/does/not/exist"

func main() {
	return
	println("unreachable")
}
`
	path := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, d := range ds {
		if strings.Contains(d.Message, "unreachable") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unreachable diagnostic despite unresolvable import, got:\n%v", ds)
	}
}

// Files with different package clauses in the same directory must not be
// merged into one package either.
func TestSameDirMixedPackages(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package foo\n\nimport \"fmt\"\n\nfunc A() { fmt.Printf(\"%d\", \"x\") }\n")
	write("b.go", "package bar\n\nimport \"fmt\"\n\nfunc B() { fmt.Printf(\"%d\", \"y\") }\n")

	ds, err := Run([]string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	count := 0
	for _, d := range ds {
		if strings.Contains(d.Message, "expects integer") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 printf diagnostics (one per package), got %d:\n%v", count, ds)
	}
}
