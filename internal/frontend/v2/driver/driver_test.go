package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/encode"
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
	doc, err := emit.Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	// Validate the in-memory program structure (no JSON involved).
	if doc.Frontend != "v2" {
		t.Fatalf("frontend = %q, want v2", doc.Frontend)
	}
	if name := doc.Package["name"]; name != "main" {
		t.Fatalf("package name = %v, want main", name)
	}
	if len(doc.Funcs) != 1 {
		t.Fatalf("funcs has %d entries, want 1", len(doc.Funcs))
	}
}

// TestKeyedStructLiteral is the Go half of the F13-P1-1 end-to-end test
// (independent codebase review, 2026-11-09-13): a keyed struct literal
// Point{X: 1, Y: 2} must survive parse → check → emit → encode with its
// fieldname keys intact and in order, so the Java NodeFactoryV2 can
// resolve them to field indices. The Java half is
// NodeFactoryV2Test.testKeyedStructLiteralBuilds, which feeds the hex
// logged here (run with -v to regenerate it) into buildProgram.
//
// The test is demonstrably capable of failing: if the emitter ever went
// back to emitting ident keys for struct fields, the fieldname count
// check fails; if the encoder rejected the shape, Encode fails loudly.
func TestKeyedStructLiteral(t *testing.T) {
	src := `package main

type Point struct {
	X int
	Y int
}

func main() {
	p := Point{X: 1, Y: 2}
	println(p.X, p.Y)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "point.go")
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
	doc, err := emit.Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	// Find the single compositelit and check its keyed elements.
	lits := findNodes(doc.Funcs, "compositelit")
	if len(lits) != 1 {
		t.Fatalf("compositelit nodes = %d, want 1", len(lits))
	}
	fields, ok := lits[0]["fields"].(map[string]any)
	if !ok {
		t.Fatalf("compositelit has no fields map: %#v", lits[0])
	}
	elems, ok := fields["elems"].([]any)
	if !ok || len(elems) != 2 {
		t.Fatalf("compositelit elems = %v, want 2 keyvalue elements", fields["elems"])
	}
	wantNames := []string{"X", "Y"}
	for i, e := range elems {
		kv, ok := e.(map[string]any)
		if !ok || kv["kind"] != "keyvalue" {
			t.Fatalf("elems[%d] = %#v, want a keyvalue node", i, e)
		}
		kf, ok := kv["fields"].(map[string]any)
		if !ok {
			t.Fatalf("elems[%d] has no fields map", i)
		}
		key, ok := kf["key"].(map[string]any)
		if !ok {
			t.Fatalf("elems[%d] key = %#v, want a node map", i, kf["key"])
		}
		if key["kind"] != "fieldname" {
			t.Fatalf("elems[%d] key kind = %v, want fieldname", i, key["kind"])
		}
		keyFields, ok := key["fields"].(map[string]any)
		if !ok || keyFields["name"] != wantNames[i] {
			t.Fatalf("elems[%d] key name = %v, want %s", i, keyFields["name"], wantNames[i])
		}
	}

	b, err := encode.Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(b) == 0 {
		t.Fatalf("encode produced empty output")
	}
	// Hex of the encoded document: the Java-side golden in
	// NodeFactoryV2Test.testKeyedStructLiteralBuilds. Re-run with -v
	// after any emitter/encoder change and re-sync the Java golden.
	t.Logf("keyed-struct-literal golden hex: %s", hex.EncodeToString(b))
}

// findNodes returns every node map of the given kind anywhere in v,
// in encounter order.
func findNodes(v any, kind string) []map[string]any {
	var out []map[string]any
	var walk func(x any)
	walk = func(x any) {
		switch n := x.(type) {
		case map[string]any:
			if n["kind"] == kind {
				out = append(out, n)
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range n {
				walk(c)
			}
		}
	}
	walk(v)
	return out
}
