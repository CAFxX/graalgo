// Unit tests for stage 2 (checker) of the frontend/v2 pipeline.
//
// The checker is a thin wrapper over go/types with a real importer and
// deliberately no fallback to synthetic packages: a missing import is a
// compile error, never a signal to invent a package. These tests pin
// that contract directly instead of only exercising it end-to-end via
// the driver.
package checker

import (
	"go/token"
	"strings"
	"testing"

	"graalgo-frontend-v2/parser"
)

// checkSrc parses one in-memory source file and type-checks it.
func checkSrc(t *testing.T, src string) (*Result, error) {
	t.Helper()
	fset := token.NewFileSet()
	pkg, err := parser.ParseSources(fset, map[string]string{"src.go": src})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Check(fset, pkg, &Config{GoVersion: "go1.27"})
}

// TestCheckValidPackage pins the success contract: a valid package
// yields a named package, populated Info, and the stage-1 FileSet.
func TestCheckValidPackage(t *testing.T) {
	res, err := checkSrc(t, `package main

func main() { println(1) }
`)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if res.Pkg == nil || res.Pkg.Name() != "main" {
		t.Fatalf("package = %v, want main", res.Pkg)
	}
	if res.Info == nil || len(res.Info.Defs) == 0 {
		t.Fatalf("Info.Defs is empty")
	}
	if res.Fset == nil {
		t.Fatalf("Fset is nil")
	}
	if res.Info.Defs == nil {
		t.Fatalf("Info.Defs is nil")
	}
}

// TestCheckMissingImportIsError pins the no-synthetic-fallback contract:
// an import that cannot be found on disk is an error, never an invented
// package.
func TestCheckMissingImportIsError(t *testing.T) {
	_, err := checkSrc(t, `package main

import "example.com/nonexistent"

var _ = nonexistent.X
`)
	if err == nil {
		t.Fatalf("expected an error for a missing import, got success")
	}
	if !strings.Contains(err.Error(), "checker:") {
		t.Errorf("error %q does not carry the checker prefix", err.Error())
	}
}

// TestCheckTypeErrorIsError pins that a type error fails the stage:
// a string assigned to an int is not a program the emitter may see.
func TestCheckTypeErrorIsError(t *testing.T) {
	_, err := checkSrc(t, `package main

var x int = "s"
`)
	if err == nil {
		t.Fatalf("expected an error for int = string, got success")
	}
}

// TestCheckNilConfigUsesDefault pins that a nil Config is accepted and
// checks with the toolchain default.
func TestCheckNilConfigUsesDefault(t *testing.T) {
	fset := token.NewFileSet()
	pkg, err := parser.ParseSources(fset, map[string]string{"src.go": "package main\n\nfunc main() {}\n"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res, err := Check(fset, pkg, nil)
	if err != nil {
		t.Fatalf("check with nil config: %v", err)
	}
	if res.Pkg == nil || res.Pkg.Name() != "main" {
		t.Fatalf("package = %v, want main", res.Pkg)
	}
}
