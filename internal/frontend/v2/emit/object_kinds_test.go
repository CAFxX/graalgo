// L12: object-kind truthfulness.
//
// describeObject must label every types.Object with its true go/types
// kind — constants as "const" (with their value payload), type names as
// "typename", nil as "nil" — never as "var". Type conversions must emit
// "conversion" nodes, never calls to a fake "var" holding a type name.
// Undescribable object kinds (*types.PkgName, *types.Label, *types.Builtin)
// must fail loudly instead of being mislabeled.
package emit

import (
	"encoding/hex"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

func emitDocForKinds(t *testing.T, src string) *Document {
	t.Helper()
	fset := token.NewFileSet()
	pkg, err := parser.ParseSources(fset, map[string]string{"kinds.go": src})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(fset, pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	doc, err := Emit(cres, pkg, []string{"kinds.go"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

// objectsByName indexes the document's object table by object name.
func objectsByName(doc *Document) map[string]map[string]any {
	m := map[string]map[string]any{}
	for _, o := range doc.Objects {
		om, ok := o.(map[string]any)
		if !ok {
			continue
		}
		name, _ := om["name"].(string)
		m[name] = om
	}
	return m
}

// walkNodes calls f for every node map in the tree. Node maps are the
// maps carrying "kind" and "fields"; the walk recurses through all nested
// maps and lists.
func walkNodes(v any, f func(map[string]any)) {
	switch v := v.(type) {
	case map[string]any:
		if _, ok := v["kind"]; ok {
			f(v)
		}
		for _, c := range v {
			walkNodes(c, f)
		}
	case []any:
		for _, c := range v {
			walkNodes(c, f)
		}
	}
}

func constValue(t *testing.T, om map[string]any) map[string]any {
	t.Helper()
	v, ok := om["value"].(map[string]any)
	if !ok {
		t.Fatalf("object %q has no value map: %#v", om["name"], om)
	}
	return v
}

func TestObjectKindsAreTruthful(t *testing.T) {
	src := `package main

const (
	answer   = 42
	ratio    = 3.25
	greeting = "hi"
	flag     = true
	cval     = 1 + 2i
)

type Celsius float64

func main() {
	x := answer
	f := ratio
	s := greeting
	b := flag
	c := cval
	var p *int = nil
	y := int(f)
	t := Celsius(f)
	r := string(65)
	println(x, y)
	_ = s
	_ = b
	_ = c
	_ = p
	_ = t
	_ = r
}
`
	doc := emitDocForKinds(t, src)
	objs := objectsByName(doc)

	// Constants are "const" with faithful value payloads — not "var".
	intObj := objs["answer"]
	if intObj["kind"] != "const" {
		t.Errorf("answer kind = %v, want const", intObj["kind"])
	}
	iv := constValue(t, intObj)
	if iv["ckind"] != "int" || iv["bits"] != int64(42) || iv["exact"] != "42" {
		t.Errorf("answer value = %#v, want int/42", iv)
	}

	floatObj := objs["ratio"]
	if floatObj["kind"] != "const" {
		t.Errorf("ratio kind = %v, want const", floatObj["kind"])
	}
	fv := constValue(t, floatObj)
	if fv["ckind"] != "float" ||
		fv["f64bits"] != int64(math.Float64bits(3.25)) ||
		fv["f32bits"] != int64(int32(math.Float32bits(3.25))) {
		t.Errorf("ratio value = %#v, want correctly-rounded float bits", fv)
	}

	strObj := objs["greeting"]
	if strObj["kind"] != "const" {
		t.Errorf("greeting kind = %v, want const", strObj["kind"])
	}
	sv := constValue(t, strObj)
	if sv["ckind"] != "string" || sv["sbytes"] != hex.EncodeToString([]byte("hi")) {
		t.Errorf("greeting value = %#v, want hex of %q", sv, "hi")
	}

	boolObj := objs["flag"]
	if boolObj["kind"] != "const" {
		t.Errorf("flag kind = %v, want const", boolObj["kind"])
	}
	bv := constValue(t, boolObj)
	if bv["ckind"] != "bool" || bv["value"] != true {
		t.Errorf("flag value = %#v, want bool/true", bv)
	}

	cplxObj := objs["cval"]
	if cplxObj["kind"] != "const" {
		t.Errorf("cval kind = %v, want const", cplxObj["kind"])
	}
	cv := constValue(t, cplxObj)
	if cv["ckind"] != "complex" ||
		cv["creal64"] != int64(math.Float64bits(1)) ||
		cv["cimag64"] != int64(math.Float64bits(2)) {
		t.Errorf("cval value = %#v, want complex 1+2i bit patterns", cv)
	}

	// nil is "nil" — not a mutable "var".
	if objs["nil"]["kind"] != "nil" {
		t.Errorf("nil kind = %v, want nil", objs["nil"]["kind"])
	}

	// Nothing may be mislabeled "var": constants, nil, and type names
	// are not variables.
	for name, om := range objs {
		switch name {
		case "answer", "ratio", "greeting", "flag", "cval", "nil", "int", "Celsius", "string":
			if om["kind"] == "var" {
				t.Errorf("object %q mislabeled as var: %#v", name, om)
			}
		}
	}

	// Conversions are "conversion" nodes with operand + target type —
	// never calls to a "var" named int/Celsius/string.
	var conversions, calls int
	walkNodes(doc.Funcs, func(n map[string]any) {
		switch n["kind"] {
		case "conversion":
			conversions++
			fields, ok := n["fields"].(map[string]any)
			if !ok {
				t.Errorf("conversion node has no fields: %#v", n)
				return
			}
			if _, ok := fields["x"].(map[string]any); !ok {
				t.Errorf("conversion node has no x operand: %#v", n)
			}
			if _, ok := fields["type"].(int); !ok {
				t.Errorf("conversion node has no type id: %#v", n)
			}
		case "call":
			calls++
			fields, _ := n["fields"].(map[string]any)
			fun, _ := fields["fun"].(map[string]any)
			ffields, _ := fun["fields"].(map[string]any)
			if name, ok := ffields["name"].(string); ok {
				if name == "int" || name == "Celsius" || name == "string" {
					t.Errorf("conversion %s(x) emitted as call to %q", name, name)
				}
			}
		}
	})
	if conversions != 3 {
		t.Errorf("found %d conversion nodes, want 3 (int(f), Celsius(f), string(65))", conversions)
	}
	if calls != 1 {
		t.Errorf("found %d call nodes, want 1 (println)", calls)
	}
}

// TestDescribeObjectKinds unit-covers every describeObject arm, including
// "typename", which the pipeline no longer declares (conversions skip fun
// emission, so type names never enter the object table through them).
func TestDescribeObjectKinds(t *testing.T) {
	newEmitter := func() *Emitter {
		return &Emitter{
			typeIDs: make(map[types.Type]int),
			types:   []any{},
			objIDs:  make(map[types.Object]int),
			objects: []any{},
		}
	}

	cases := []struct {
		name string
		obj  types.Object
		kind string
	}{
		{"var", types.NewVar(0, nil, "x", types.Typ[types.Int]), "var"},
		{"func", types.NewFunc(0, nil, "fn",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)), "func"},
		{"const", types.NewConst(0, nil, "k",
			types.Typ[types.Int], constant.MakeInt64(7)), "const"},
		{"typename", types.NewTypeName(0, nil, "MyInt", types.Typ[types.Int]), "typename"},
		{"nil", types.Universe.Lookup("nil"), "nil"},
	}
	for _, tc := range cases {
		e := newEmitter()
		got, err := e.describeObject(tc.obj, false)
		if err != nil {
			t.Errorf("describeObject(%s): %v", tc.name, err)
			continue
		}
		if got["kind"] != tc.kind {
			t.Errorf("describeObject(%s) kind = %v, want %q", tc.name, got["kind"], tc.kind)
		}
	}

	// The const arm carries the value payload the backend needs.
	e := newEmitter()
	got, err := e.describeObject(
		types.NewConst(0, nil, "k", types.Typ[types.Int], constant.MakeInt64(7)), false)
	if err != nil {
		t.Fatalf("describeObject(const): %v", err)
	}
	v, ok := got["value"].(map[string]any)
	if !ok || v["ckind"] != "int" || v["bits"] != int64(7) {
		t.Errorf("const value payload = %#v, want int/7", got["value"])
	}
}

// TestDescribeObjectRejectsUndescribable proves the loud-failure arm:
// package names, labels, and builtins must error, never be mislabeled.
func TestDescribeObjectRejectsUndescribable(t *testing.T) {
	newEmitter := func() *Emitter {
		return &Emitter{
			typeIDs: make(map[types.Type]int),
			types:   []any{},
			objIDs:  make(map[types.Object]int),
			objects: []any{},
		}
	}
	for _, obj := range []types.Object{
		types.NewPkgName(0, nil, "fmt", nil),
		types.NewLabel(0, nil, "loop"),
		types.Universe.Lookup("len"), // *types.Builtin
	} {
		e := newEmitter()
		if _, err := e.describeObject(obj, false); err == nil {
			t.Errorf("describeObject(%T) = nil error, want loud failure", obj)
		} else if !strings.Contains(err.Error(), "unsupported object kind") {
			t.Errorf("describeObject(%T) error = %q, want 'unsupported object kind'", obj, err)
		}
		// declareObject must propagate the failure, not swallow it.
		e2 := newEmitter()
		if _, err := e2.declareObject(obj); err == nil {
			t.Errorf("declareObject(%T) = nil error, want loud failure", obj)
		}
	}
}
