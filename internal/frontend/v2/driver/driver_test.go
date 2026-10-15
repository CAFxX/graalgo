package main

import (
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

func TestHelloWorld(t *testing.T) {
	src := `package main

func main() {
	x := 42
	println(x)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	// Stage 1.
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pres.Pkg.Name != "main" {
		t.Fatalf("package name = %q, want main", pres.Pkg.Name)
	}

	// Stage 2.
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	// Stage 3.
	docBytes, err := emit.Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	// Validate the JSON structure.
	var doc map[string]any
	if err := json.Unmarshal(docBytes, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc["frontend"] != "v1" {
		t.Fatalf("frontend = %v, want v1", doc["frontend"])
	}
	funcs, ok := doc["funcs"].([]any)
	if !ok || len(funcs) != 1 {
		t.Fatalf("funcs has %d entries, want 1", len(funcs))
	}
	_ = token.NoPos
}
