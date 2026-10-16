// Positive tests for composite literals, index, slice, and key-value expressions.
//
// These constructs are now HANDLED (moved from loudlyRejected to handledNodes
// in coverage_test.go). Each test feeds a minimal Go program through the full
// pipeline (parse → check → emit) and asserts the emitter succeeds and produces
// the expected node kinds.
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitSrc runs the full pipeline on src and returns the document.
func emitSrc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "prog.go")
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
	doc, err := Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

// findNodeKind searches the document funcs for a node with the given kind.
func findNodeKind(t *testing.T, doc *Document, kind string) bool {
	t.Helper()
	var search func(n any) bool
	search = func(n any) bool {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == kind {
				return true
			}
			for _, val := range v {
				if search(val) {
					return true
				}
			}
		case []any:
			for _, item := range v {
				if search(item) {
					return true
				}
			}
		}
		return false
	}
	for _, fn := range doc.Funcs {
		if search(fn) {
			return true
		}
	}
	return false
}

func TestCompositeLitStruct(t *testing.T) {
	doc := emitSrc(t, `package main
type Point struct { X, Y int }
func main() {
	p := Point{X: 1, Y: 2}
	println(p.X + p.Y)
}
`)
	if !findNodeKind(t, doc, "compositelit") {
		t.Error("expected compositelit node for struct literal")
	}
}

func TestCompositeLitArray(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	a := [3]int{1, 2, 3}
	println(a[0] + a[1] + a[2])
}
`)
	if !findNodeKind(t, doc, "compositelit") {
		t.Error("expected compositelit node for array literal")
	}
}

func TestCompositeLitSlice(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	s := []int{1, 2, 3}
	println(len(s))
}
`)
	if !findNodeKind(t, doc, "compositelit") {
		t.Error("expected compositelit node for slice literal")
	}
}

func TestCompositeLitMap(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	m := map[string]int{"a": 1, "b": 2}
	println(m["a"] + m["b"])
}
`)
	if !findNodeKind(t, doc, "compositelit") {
		t.Error("expected compositelit node for map literal")
	}
}

func TestCompositeLitNested(t *testing.T) {
	doc := emitSrc(t, `package main
type Inner struct { V int }
type Outer struct { In Inner; Name string }
func main() {
	o := Outer{In: Inner{V: 42}, Name: "test"}
	println(o.In.V)
}
`)
	if !findNodeKind(t, doc, "compositelit") {
		t.Error("expected compositelit node for nested struct literal")
	}
}

func TestIndexExprArray(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	a := [3]int{1, 2, 3}
	println(a[1])
}
`)
	if !findNodeKind(t, doc, "index") {
		t.Error("expected index node for array index")
	}
}

func TestIndexExprMap(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	m := map[string]int{"x": 99}
	println(m["x"])
}
`)
	if !findNodeKind(t, doc, "index") {
		t.Error("expected index node for map index")
	}
}

func TestIndexExprSlice(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	s := []int{10, 20, 30}
	println(s[2])
}
`)
	if !findNodeKind(t, doc, "index") {
		t.Error("expected index node for slice index")
	}
}

func TestSliceExpr(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	s := []int{1, 2, 3, 4, 5}
	t := s[1:4]
	println(len(t))
}
`)
	if !findNodeKind(t, doc, "slice") {
		t.Error("expected slice node for slice expression")
	}
}

func TestSliceExprFull(t *testing.T) {
	doc := emitSrc(t, `package main
func main() {
	s := [5]int{1, 2, 3, 4, 5}
	t := s[1:4:5]
	println(len(t), cap(t))
}
`)
	if !findNodeKind(t, doc, "slice") {
		t.Error("expected slice node for full slice expression")
	}
}

func TestCompositeLitEmpty(t *testing.T) {
	// Empty literals must also work.
	doc := emitSrc(t, `package main
func main() {
	s := []int{}
	m := map[string]int{}
	println(len(s), len(m))
}
`)
	if !findNodeKind(t, doc, "compositelit") {
		t.Error("expected compositelit node for empty literals")
	}
}
