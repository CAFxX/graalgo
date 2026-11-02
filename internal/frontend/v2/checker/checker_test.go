package checker_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// These tests exercise the module-aware importer. They require a Go
// toolchain on PATH: packages.Load resolves imports by running
// `go list`, exactly as the `go` tool itself does.

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func checkFile(t *testing.T, path string) *checker.Result {
	t.Helper()
	pres, err := parser.ParseFiles([]string{path})
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	res, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check %s: %v", path, err)
	}
	return res
}

// importedPaths returns the import paths of pkg's direct imports.
func importedPaths(t *testing.T, res *checker.Result) []string {
	t.Helper()
	var paths []string
	for _, imp := range res.Pkg.Imports() {
		paths = append(paths, imp.Path())
	}
	return paths
}

func contains(t *testing.T, paths []string, want string) {
	t.Helper()
	for _, p := range paths {
		if p == want {
			return
		}
	}
	t.Fatalf("imports %v do not contain %q", paths, want)
}

// TestCheckModuleLocalImport is the regression test for the v2 checker
// failing on module-local imports with "can't find import": package
// main imports a package from its own module, which the old
// importer.Default() (gc importer, nil lookup) could never resolve
// because module packages have no prebuilt .a files in GOPATH/pkg.
func TestCheckModuleLocalImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/repro\n\ngo 1.27\n")
	writeFile(t, dir, "greet/greet.go", "package greet\n\n// Hello returns a greeting.\nfunc Hello() string { return \"hi\" }\n")
	main := writeFile(t, dir, "main.go",
		"package main\n\nimport \"example.com/repro/greet\"\n\nfunc main() { println(greet.Hello()) }\n")

	res := checkFile(t, main)
	if res.Pkg.Name() != "main" {
		t.Fatalf("package name = %q, want %q", res.Pkg.Name(), "main")
	}
	contains(t, importedPaths(t, res), "example.com/repro/greet")
}

// TestCheckTransitiveModuleLocalImport covers an imported package that
// itself imports another module-local package: the imported package's
// exported API mentions the second package's type, so the load must
// resolve the whole module-local import graph, not just the direct edge.
func TestCheckTransitiveModuleLocalImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/repro\n\ngo 1.27\n")
	writeFile(t, dir, "words/words.go",
		"package words\n\n// Word is a greeting word.\ntype Word string\n\n// Hi is a greeting.\nconst Hi Word = \"hi\"\n")
	writeFile(t, dir, "greet/greet.go",
		"package greet\n\nimport \"example.com/repro/words\"\n\n// Hello returns a greeting.\nfunc Hello() words.Word { return words.Hi }\n")
	main := writeFile(t, dir, "main.go",
		"package main\n\nimport \"example.com/repro/greet\"\n\nvar _ = greet.Hello()\n")

	res := checkFile(t, main)
	contains(t, importedPaths(t, res), "example.com/repro/greet")
	// The imported greet package must itself have resolved words.
	var greetPaths []string
	for _, imp := range res.Pkg.Imports() {
		if imp.Path() == "example.com/repro/greet" {
			for _, dep := range imp.Imports() {
				greetPaths = append(greetPaths, dep.Path())
			}
		}
	}
	contains(t, greetPaths, "example.com/repro/words")
}

// TestCheckStdlibImportOutsideModule checks that stdlib imports still
// resolve when the checked file lives outside any module.
func TestCheckStdlibImportOutsideModule(t *testing.T) {
	dir := t.TempDir()
	main := writeFile(t, dir, "main.go",
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"ok\") }\n")

	res := checkFile(t, main)
	contains(t, importedPaths(t, res), "fmt")
}

// TestCheckMissingImportErrors checks the no-synthetic-packages rule:
// an import that does not exist anywhere must fail type-checking with
// an error naming the import, not silently resolve to a fake package.
func TestCheckMissingImportErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/repro\n\ngo 1.27\n")
	main := writeFile(t, dir, "main.go",
		"package main\n\nimport \"example.com/repro/nosuch\"\n\nvar _ = nosuch.X\n")

	pres, err := parser.ParseFiles([]string{main})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err == nil {
		t.Fatal("check succeeded for a missing import, want an error")
	}
	if !strings.Contains(err.Error(), "example.com/repro/nosuch") {
		t.Fatalf("error %v does not name the missing import", err)
	}
}
