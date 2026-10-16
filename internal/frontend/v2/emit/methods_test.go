// Method declaration tests.
//
// Methods (*ast.FuncDecl with Recv) must emit their receiver — previously
// the receiver was silently dropped, emitting the method as a plain
// function. Each test feeds a minimal program through the full pipeline
// (parse → check → emit) and asserts on the in-memory document.
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitDoc is a test helper: parse, check, and emit one source file.
func emitDoc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "m.go")
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

// funcByName returns the funcdecl node fields for the named function.
func funcByName(t *testing.T, doc *Document, name string) map[string]any {
	t.Helper()
	for _, f := range doc.Funcs {
		fm := f.(map[string]any)
		fields := fm["fields"].(map[string]any)
		if fields["name"] == name {
			return fields
		}
	}
	t.Fatalf("func %q not found in document", name)
	return nil
}

// recvOf extracts the receiver param node from funcdecl fields.
func recvOf(t *testing.T, fields map[string]any) map[string]any {
	t.Helper()
	recv, ok := fields["recv"]
	if !ok {
		t.Fatalf("funcdecl has no recv field")
	}
	if recv == nil {
		return nil
	}
	rn := recv.(map[string]any)
	if rn["kind"] != "param" {
		t.Fatalf("recv kind = %v, want param", rn["kind"])
	}
	return rn["fields"].(map[string]any)
}

func TestMethodValueReceiver(t *testing.T) {
	doc := emitDoc(t, `package main

type Point struct{ X, Y int }

func (p Point) Norm() int { return p.X*p.X + p.Y*p.Y }

func main() {}
`)
	fields := funcByName(t, doc, "Norm")
	recv := recvOf(t, fields)
	if recv == nil {
		t.Fatal("value-receiver method has nil recv (receiver dropped)")
	}
	if recv["name"] != "p" {
		t.Errorf("recv name = %v, want p", recv["name"])
	}
	if recv["pointer"] != false {
		t.Errorf("recv pointer = %v, want false (value receiver)", recv["pointer"])
	}
	// The receiver object must be a declared param-like object.
	if _, ok := recv["obj"].(int); !ok {
		t.Errorf("recv obj is not an int object id: %v", recv["obj"])
	}
}

func TestMethodPointerReceiver(t *testing.T) {
	doc := emitDoc(t, `package main

type Counter struct{ n int }

func (c *Counter) Inc() { c.n++ }

func main() {}
`)
	fields := funcByName(t, doc, "Inc")
	recv := recvOf(t, fields)
	if recv == nil {
		t.Fatal("pointer-receiver method has nil recv (receiver dropped)")
	}
	if recv["pointer"] != true {
		t.Errorf("recv pointer = %v, want true (pointer receiver)", recv["pointer"])
	}
}

func TestMethodBlankReceiver(t *testing.T) {
	// Blank receiver: legal Go, receiver is not addressable by name.
	doc := emitDoc(t, `package main

type T struct{}

func (_ T) M() {}

func main() {}
`)
	fields := funcByName(t, doc, "M")
	recv := recvOf(t, fields)
	if recv == nil {
		t.Fatal("blank-receiver method has nil recv")
	}
	if recv["name"] != "_" {
		t.Errorf("recv name = %v, want _", recv["name"])
	}
}

func TestPlainFuncHasNilRecv(t *testing.T) {
	// Plain functions must have nil recv (not an empty struct).
	doc := emitDoc(t, `package main

func Add(a, b int) int { return a + b }

func main() {}
`)
	fields := funcByName(t, doc, "Add")
	recv, ok := fields["recv"]
	if !ok {
		t.Fatal("funcdecl has no recv field")
	}
	if recv != nil {
		t.Errorf("plain func has non-nil recv: %v", recv)
	}
}

func TestMethodSetPopulated(t *testing.T) {
	doc := emitDoc(t, `package main

type Point struct{ X int }

func (p Point) Get() int { return p.X }

func (p *Point) Set(x int) { p.X = x }

func main() {}
`)
	// Find the Named type for main.Point and check its methods list.
	found := false
	for _, ty := range doc.Types {
		tm := ty.(map[string]any)
		if tm["kind"] != "named" {
			continue
		}
		detail := tm["detail"].(map[string]any)
		if detail["name"] != "main.Point" {
			continue
		}
		found = true
		methods, ok := detail["methods"].([]any)
		if !ok {
			t.Fatalf("named type methods is not a list: %v", detail["methods"])
		}
		if len(methods) != 2 {
			t.Errorf("main.Point methods = %d, want 2 (Get, Set)", len(methods))
		}
		// Each method id must resolve to a func object in the object table.
		for _, mid := range methods {
			id, ok := mid.(int)
			if !ok || id < 0 || id >= len(doc.Objects) {
				t.Errorf("method id %v out of range", mid)
			}
		}
	}
	if !found {
		t.Error("named type main.Point not found in type table")
	}
}

func TestMethodCallEmits(t *testing.T) {
	// End-to-end: a method call must emit (selector on a value).
	// This is a positive control that methods integrate with call emission.
	doc := emitDoc(t, `package main

type Point struct{ X int }

func (p Point) Get() int { return p.X }

func main() {
	pt := Point{X: 42}
	println(pt.Get())
}
`)
	// The method itself must have a receiver.
	fields := funcByName(t, doc, "Get")
	if recvOf(t, fields) == nil {
		t.Fatal("method Get has nil recv")
	}
	// main must exist and have a body (the call emitted without error
	// is the assertion — Emit would have failed loudly otherwise).
	mf := funcByName(t, doc, "main")
	if mf["body"] == nil {
		t.Error("main has nil body")
	}
}
