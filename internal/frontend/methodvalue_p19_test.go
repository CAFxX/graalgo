package main

// P1-9 (review-2026-10-31-22): exporter tests for method values that
// used to fail at export time.
// Red-green: before the fix, the interface programs fail with
// "method value X is not supported: method value via interface
// (P3-M3 scope)", and the multi-result programs fail with
// "multi-valued call result is not supported in frontend/v1 scope
// (WS D)" at the call site through the variable. After the fix they
// export: interface method values as "ifacemethodvalue" wire nodes,
// and multi-result method values with pair-linked call sites.
// The end-to-end behavior (values, mutation, Builder via io.Writer)
// is covered by test/regression/pkg031/builder_iface_test.go.

import (
	"strings"
	"testing"
)

const p19IfaceMethodValueSrc = `package main

type Sizer interface {
	Size() int
}

type Pair interface {
	Two() (int, error)
}

type C struct{ x int }

func (c C) Size() int { return c.x }
func (c C) Two() (int, error) { return c.x, nil }

func main() {
	var s Sizer = C{42}
	f := s.Size
	println(f())
	var p Pair = C{7}
	g := p.Two
	v, err := g()
	println(v, err == nil)
}
`

// TestIfaceMethodValueExports: interface method values (single- and
// multi-result) export as "ifacemethodvalue" wire nodes, not scope
// errors.
func TestIfaceMethodValueExports(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, p19IfaceMethodValueSrc))
	if err != nil {
		t.Fatalf("ExportFile with interface method values: %v", err)
	}
	doc := decodeExport(t, out)
	nodes := findAllKind(doc, "ifacemethodvalue")
	if len(nodes) != 2 {
		t.Fatalf("ifacemethodvalue nodes = %d, want 2 (Size, Two)", len(nodes))
	}
	byMethod := map[string]map[string]any{}
	for _, n := range nodes {
		f := fieldsOf(n)
		m, _ := f["method"].(string)
		byMethod[m] = f
	}
	for _, want := range []string{"Size", "Two"} {
		if _, ok := byMethod[want]; !ok {
			t.Fatalf("no ifacemethodvalue node for %s", want)
		}
	}
	// The multi-result call through the variable must desugar to a
	// pair-temp extraction, not reach the backend as a multi-valued
	// call: the export carries the synthesized pair temp.
	if !strings.Contains(out, "__p5mr_") {
		t.Fatal("export has no pair temp for the multi-result method value call")
	}
}

const concreteMRMethodValueSrc = `package main

type C struct{ x int }

func (c C) DivMod(a, b int) (int, int) { return a / b, a % b }

var F func(int, int) (int, int)

func main() {
	c := C{0}
	f := c.DivMod
	q, r := f(7, 3)
	println(q, r)
	F = c.DivMod
	s, t := F(7, 3)
	println(s, t)
}
`

// TestConcreteMRMethodValueExports: a multi-result method value stored
// in a local (:=) and in a package variable (=) exports, and calls
// through both variables desugar to pair-temp extractions.
func TestConcreteMRMethodValueExports(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, concreteMRMethodValueSrc))
	if err != nil {
		t.Fatalf("ExportFile with multi-result method values: %v", err)
	}
	doc := decodeExport(t, out)
	nodes := findAllKind(doc, "methodvalue")
	if len(nodes) != 2 {
		t.Fatalf("methodvalue nodes = %d, want 2 (local f, package F)", len(nodes))
	}
	if !strings.Contains(out, "__p5mr_") {
		t.Fatal("export has no pair temp for the multi-result method value calls")
	}
}

const p19IfaceMethodExprSrc = `package main

type Sizer interface {
	Size() int
}

type C struct{ x int }

func (c C) Size() int { return c.x }

func main() {
	var s Sizer = C{42}
	_ = s
	println(Sizer.Size)
}
`

// TestIfaceMethodExprStillScopeError: method *expressions* on
// interface receivers stay out of scope (there is no receiver value
// to bind); only method *values* were fixed. Sizer.Size here is a
// valid Go method expression (func(Sizer) int); it must fail export
// with the P3-M3 scope error, not export an ifacemethodvalue node.
func TestIfaceMethodExprStillScopeError(t *testing.T) {
	_, err := ExportFile(writeIfaceProg(t, p19IfaceMethodExprSrc))
	if err == nil {
		t.Fatal("interface method expression exported without error, want P3-M3 scope error")
	}
	if !strings.Contains(err.Error(), "method expression") {
		t.Fatalf("error = %v, want a method-expression scope error", err)
	}
}
