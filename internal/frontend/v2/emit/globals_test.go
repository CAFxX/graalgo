// Golden tests for the P1-2 emitter defects (review 2026-10-30):
// package-level variables must be emitted (not silently dropped),
// struct field offsets must be real (not hardcoded 0), named types
// must survive, and unhandled object/type kinds must fail loudly
// instead of being mislabelled.
//
// Each test feeds a minimal program through the full pipeline
// (parse → check → emit) and inspects the resulting Document.
package emit

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

func emitSrc(t *testing.T, src string) (*Document, error) {
	t.Helper()
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
	return Emit(cres, pres.Pkg, pres.Files)
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T (%v)", v, v)
	}
	return m
}

func asList(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T (%v)", v, v)
	}
	return l
}

// findObject returns the first object entry with the given kind and name.
func findObject(t *testing.T, doc *Document, kind, name string) map[string]any {
	t.Helper()
	for _, o := range doc.Objects {
		m := asMap(t, o)
		if m["kind"] == kind && m["name"] == name {
			return m
		}
	}
	t.Fatalf("no object kind=%q name=%q in %d objects", kind, name, len(doc.Objects))
	return nil
}

// findType returns the first type entry with the given kind.
func findType(t *testing.T, doc *Document, kind string) map[string]any {
	t.Helper()
	for _, ty := range doc.Types {
		m := asMap(t, ty)
		if m["kind"] == kind {
			return m
		}
	}
	t.Fatalf("no type kind=%q in %d types", kind, len(doc.Types))
	return nil
}

func TestPackageLevelVarsEmitted(t *testing.T) {
	doc, err := emitSrc(t, `package main

var Counter int = 42
var Name string

func main() {
	println(Counter)
	_ = Name
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Globals) != 2 {
		t.Fatalf("globals has %d entries, want 2 (Counter, Name)", len(doc.Globals))
	}
	g0 := asMap(t, doc.Globals[0])
	if g0["kind"] != "global" {
		t.Fatalf("globals[0] kind = %v, want global", g0["kind"])
	}
	f0 := asMap(t, g0["fields"])
	names0, ok := f0["names"].([]string)
	if !ok || len(names0) != 1 || names0[0] != "Counter" {
		t.Fatalf("globals[0] names = %v, want [Counter]", f0["names"])
	}
	vals0 := asList(t, f0["values"])
	if len(vals0) != 1 {
		t.Fatalf("globals[0] values has %d entries, want 1 (the 42 initializer)", len(vals0))
	}
	lit := asMap(t, vals0[0])
	if lit["kind"] != "basiclit" || asMap(t, lit["fields"])["value"] != "42" {
		t.Fatalf("globals[0] value = %v, want basiclit 42", vals0[0])
	}
	// The global's object id must resolve to the var object, and the
	// func body's reference to Counter must use that same id.
	objs0 := asList(t, f0["objs"])
	oid, ok := objs0[0].(int)
	if !ok {
		t.Fatalf("globals[0] obj id = %T %v, want int", objs0[0], objs0[0])
	}
	obj := asMap(t, doc.Objects[oid])
	if obj["kind"] != "var" || obj["name"] != "Counter" {
		t.Fatalf("global obj = %v, want kind var name Counter", obj)
	}
	// Second global: no initializer, so no values — but it must exist.
	g1 := asMap(t, doc.Globals[1])
	f1 := asMap(t, g1["fields"])
	if len(asList(t, f1["values"])) != 0 {
		t.Fatalf("globals[1] values = %v, want empty (no initializer)", f1["values"])
	}
	names1, ok := f1["names"].([]string)
	if !ok || len(names1) != 1 || names1[0] != "Name" {
		t.Fatalf("globals[1] names = %v, want [Name]", f1["names"])
	}
}

func TestStructFieldOffsetsReal(t *testing.T) {
	// gc/amd64 layout: int8 at 0, int64 aligned to 8, int8 at 16.
	// Hardcoded offset:0 would fail the B and C assertions.
	doc, err := emitSrc(t, `package main

type Padded struct {
	A int8
	B int64
	C int8 `+"`json:\"c\"`"+`
}

var P Padded

func main() {
	println(P.B)
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	st := findType(t, doc, "struct")
	detail := asMap(t, st["detail"])
	fields := asList(t, detail["fields"])
	if len(fields) != 3 {
		t.Fatalf("struct has %d fields, want 3", len(fields))
	}
	want := []struct {
		name   string
		offset int64
		tag    string
	}{
		{"A", 0, ""},
		{"B", 8, ""},
		{"C", 16, `json:"c"`},
	}
	for i, w := range want {
		f := asMap(t, fields[i])
		if f["name"] != w.name {
			t.Errorf("field %d name = %v, want %s", i, f["name"], w.name)
		}
		off, ok := f["offset"].(int64)
		if !ok {
			t.Fatalf("field %s offset = %T %v, want int64", w.name, f["offset"], f["offset"])
		}
		if off != w.offset {
			t.Errorf("field %s offset = %d, want %d (hardcoded 0 is the P1-2 bug)", w.name, off, w.offset)
		}
		if f["tag"] != w.tag {
			t.Errorf("field %s tag = %q, want %q", w.name, f["tag"], w.tag)
		}
	}
	// The named type itself must be recorded honestly, not as a var.
	findObject(t, doc, "typename", "Padded")
}

func TestNamedTypeEmitted(t *testing.T) {
	doc, err := emitSrc(t, `package main

type Celsius float64

var Temp Celsius = 21.5

func main() {
	println(Temp)
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	named := findType(t, doc, "named")
	detail := asMap(t, named["detail"])
	name, _ := detail["name"].(string)
	if !strings.HasSuffix(name, "Celsius") {
		t.Fatalf("named type name = %q, want suffix Celsius", name)
	}
	under, ok := detail["underlying"].(int)
	if !ok {
		t.Fatalf("named underlying = %T %v, want int type id", detail["underlying"], detail["underlying"])
	}
	basic := asMap(t, doc.Types[under])
	if basic["kind"] != "basic" {
		t.Fatalf("named underlying kind = %v, want basic", basic["kind"])
	}
	if asMap(t, basic["detail"])["name"] != "float64" {
		t.Fatalf("named underlying = %v, want float64", basic["detail"])
	}
	// The package-level var of the named type must be in Globals.
	if len(doc.Globals) != 1 {
		t.Fatalf("globals has %d entries, want 1 (Temp)", len(doc.Globals))
	}
}

func TestTopLevelConstAndTypeRecorded(t *testing.T) {
	doc, err := emitSrc(t, `package main

const Answer = 42

type Point struct {
	X int
	Y int
}

func main() {
	println(Answer)
	p := Point{X: 1, Y: 2}
	println(p.X)
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	c := findObject(t, doc, "const", "Answer")
	if c["val"] != "42" {
		t.Fatalf("const Answer val = %v, want 42", c["val"])
	}
	findObject(t, doc, "typename", "Point")
}

func TestUnhandledTypeKindFailsLoudly(t *testing.T) {
	// A generic function parameter has a *types.TypeParam type, which
	// the old default branch silently encoded as basic/unknown.
	// (No instantiation call: generic instantiation is already
	// rejected by emitIndex, which would mask this defect.)
	_, err := emitSrc(t, `package main

func id[T any](x T) T { return x }

func main() {}
`)
	if err == nil {
		t.Fatal("expected loud failure for unhandled type-param type, got success (silent mis-encoding)")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error %q does not contain 'unsupported'", err.Error())
	}
}

func TestNilIdentEmitted(t *testing.T) {
	// The predeclared nil has no go/types type; asking the type
	// table for one used to panic. It must emit as its own node.
	doc, err := emitSrc(t, `package main

var P *int = nil

func main() {
	if P == nil {
		println("nil")
	}
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Globals) != 1 {
		t.Fatalf("globals has %d entries, want 1 (P)", len(doc.Globals))
	}
	f := asMap(t, asMap(t, doc.Globals[0])["fields"])
	vals := asList(t, f["values"])
	if len(vals) != 1 {
		t.Fatalf("global P values has %d entries, want 1 (nil)", len(vals))
	}
	if asMap(t, vals[0])["kind"] != "nil" {
		t.Fatalf("global P value kind = %v, want nil", asMap(t, vals[0])["kind"])
	}
}

func TestAliasTypeEmitted(t *testing.T) {
	// An alias names its right-hand side; it must not collapse
	// into the aliased type silently, nor fail as unhandled.
	doc, err := emitSrc(t, `package main

type MyInt = int

var N MyInt = 7

func main() {
	println(N)
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	al := findType(t, doc, "alias")
	detail := asMap(t, al["detail"])
	name, _ := detail["name"].(string)
	if !strings.HasSuffix(name, "MyInt") {
		t.Fatalf("alias name = %q, want suffix MyInt", name)
	}
	rhs, ok := detail["rhs"].(int)
	if !ok {
		t.Fatalf("alias rhs = %T %v, want int type id", detail["rhs"], detail["rhs"])
	}
	if asMap(t, asMap(t, doc.Types[rhs])["detail"])["name"] != "int" {
		t.Fatalf("alias rhs = %v, want int", doc.Types[rhs])
	}
}

func TestUnhandledObjectKindFailsLoudly(t *testing.T) {
	// White-box: a label object has no honest object kind in the
	// emitter, so declaring it must record an error, not a fake var.
	e := &Emitter{
		typeIDs: make(map[types.Type]int),
		objIDs:  make(map[types.Object]int),
	}
	pkg := types.NewPackage("example.com/p", "p")
	lbl := types.NewLabel(0, pkg, "loop")
	e.declareObject(lbl)
	if e.err == nil {
		t.Fatal("expected loud failure for unhandled label object, got none (silent mis-labelling)")
	}
	if !strings.Contains(e.err.Error(), "unsupported") {
		t.Fatalf("error %q does not contain 'unsupported'", e.err.Error())
	}
}
