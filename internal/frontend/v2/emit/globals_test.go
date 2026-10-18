// Tests for L10: top-level `var` declarations are rejected loudly because
// v2 has no globals support yet. Import, const, and type declarations are
// unaffected by the rejection — this file pins that scoping.
package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

func emitProgram(t *testing.T, src string) (*Document, error) {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "p.go")
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return Emit(cres, pres.Pkg, pres.Files)
}

// TestTopLevelNonVarDeclsEmitFine pins the L10 scoping: a package with
// top-level import, const, and type declarations — but no var — still
// emits successfully. Only token.VAR GenDecls are rejected.
func TestTopLevelNonVarDeclsEmitFine(t *testing.T) {
	src := `package main

import "fmt"

const answer = 42

type Point struct {
	X int
	Y int
}

func main() {
	fmt.Println(answer)
	p := Point{X: 1, Y: 2}
	fmt.Println(p.X)
}
`
	doc, err := emitProgram(t, src)
	if err != nil {
		t.Fatalf("emit with import/const/type top-level decls failed: %v", err)
	}
	if len(doc.Globals) != 0 {
		t.Fatalf("Globals has %d entries, want 0 (no globals support yet)", len(doc.Globals))
	}
	if len(doc.Funcs) != 1 {
		t.Fatalf("Funcs has %d entries, want 1", len(doc.Funcs))
	}
}

// TestTopLevelVarErrorNamesTheVar checks the rejection names the offending
// declaration, so the user knows what to move or remove.
func TestTopLevelVarErrorNamesTheVar(t *testing.T) {
	src := `package main

var (
	alpha = 1
	beta  = 2
)

func main() {}
`
	_, err := emitProgram(t, src)
	if err == nil {
		t.Fatal("expected loud failure for top-level var block, got success")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Fatalf("error %q does not name the rejected vars", err.Error())
	}
}
