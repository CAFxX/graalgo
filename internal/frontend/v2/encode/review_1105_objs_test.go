// Regression test for the review-1105-v2-objs remediation lane: the new
// document shapes (globals, method receivers, selector index, fieldname
// nodes) must survive the Go→Java binary encoding. The encoder only
// accepts []any lists, so a selector index emitted as []int would fail
// loudly here instead of corrupting the wire format.
package encode

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

func TestEncodeFixedShapes(t *testing.T) {
	src := `package main

var GlobalCounter int

type Point struct{ X int }

func (p Point) Get() int { return p.X }

func main() {
	xs := []int{1}
	var a int
	for a = range xs {
		println(a, GlobalCounter)
	}
	p := Point{X: 1}
	println(p.Get(), p.X)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "src.go")
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
	if len(doc.Globals) != 1 {
		t.Fatalf("Globals = %d, want 1", len(doc.Globals))
	}
	b, err := Encode(doc)
	if err != nil {
		t.Fatalf("encode fixed-shape doc: %v", err)
	}
	if len(b) == 0 {
		t.Fatalf("encode produced empty output")
	}
}
