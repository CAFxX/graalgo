// L01 regression: v2 emitter basic-class classification.
//
// basicClass (emit.go) used to switch on `b.Info() & types.IsNumeric`, but
// types.IsNumeric = IsInteger|IsFloat|IsComplex — the mask strips exactly
// the IsBoolean and IsUnsigned bits, so `case types.IsBoolean` and
// `case types.IsUnsigned` were dead code. Typed bool emitted class
// "unknown" and uint8..uint64/uint/uintptr emitted class "int"; the Java
// backend's GoKind.fromBasic throws IllegalArgumentException on both, so
// ANY program using bool or unsigned ints failed at decode.
//
// The class strings asserted here are the cross-language contract consumed
// by GoKind.fromBasic (internal/backend/truffle/.../values/GoKind.java):
// "int", "uint", "float", "complex", "bool", "string" (+ "untyped" for
// untyped nil only). A class outside that set makes the decoder throw.
package emit

import (
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// TestBasicClassAllBasicKinds pins the wire class for every go/types basic
// kind. It is demonstrably capable of failing: on the pre-L01 code,
// "bool" produced "unknown" and every unsigned width produced "int".
func TestBasicClassAllBasicKinds(t *testing.T) {
	cases := []struct {
		name string
		typ  *types.Basic
		want string
	}{
		{"bool", types.Typ[types.Bool], "bool"},
		{"untyped bool", types.Typ[types.UntypedBool], "bool"},
		{"int", types.Typ[types.Int], "int"},
		{"int8", types.Typ[types.Int8], "int"},
		{"int16", types.Typ[types.Int16], "int"},
		{"int32", types.Typ[types.Int32], "int"},
		{"int64", types.Typ[types.Int64], "int"},
		{"untyped int", types.Typ[types.UntypedInt], "int"},
		{"untyped rune", types.Typ[types.UntypedRune], "int"},
		{"uint", types.Typ[types.Uint], "uint"},
		{"uint8", types.Typ[types.Uint8], "uint"},
		{"uint16", types.Typ[types.Uint16], "uint"},
		{"uint32", types.Typ[types.Uint32], "uint"},
		{"uint64", types.Typ[types.Uint64], "uint"},
		{"uintptr", types.Typ[types.Uintptr], "uint"},
		{"float32", types.Typ[types.Float32], "float"},
		{"float64", types.Typ[types.Float64], "float"},
		{"untyped float", types.Typ[types.UntypedFloat], "float"},
		{"complex64", types.Typ[types.Complex64], "complex"},
		{"complex128", types.Typ[types.Complex128], "complex"},
		{"untyped complex", types.Typ[types.UntypedComplex], "complex"},
		{"string", types.Typ[types.String], "string"},
		{"untyped string", types.Typ[types.UntypedString], "string"},
		{"untyped nil", types.Typ[types.UntypedNil], "untyped"},
	}
	for _, c := range cases {
		if got := basicClass(c.typ); got != c.want {
			t.Errorf("basicClass(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

// basicClassProgram exercises bool and every unsigned int kind (plus one of
// each other family as controls) through the real parse→check→emit
// pipeline, the way a user program reaches the decoder.
const basicClassProgram = `package main

func main() {
	var b bool = true
	var u uint = 1
	var u8 uint8 = 2
	var u16 uint16 = 3
	var u32 uint32 = 4
	var u64 uint64 = 5
	var up uintptr = 6
	var by byte = 7
	var i int = -1
	var i8 int8 = -2
	var i16 int16 = -3
	var i32 int32 = -4
	var i64 int64 = -5
	var r rune = 'a'
	var f32 float32 = 1.5
	var f64 float64 = 2.5
	var c64 complex64 = 1 + 2i
	var c128 complex128 = 3 + 4i
	var s string = "hi"
	_ = b
	_ = u
	_ = u8
	_ = u16
	_ = u32
	_ = u64
	_ = up
	_ = by
	_ = i
	_ = i8
	_ = i16
	_ = i32
	_ = i64
	_ = r
	_ = f32
	_ = f64
	_ = c64
	_ = c128
	_ = s
}
`

// wantBasicClasses is the (name → class) contract the decoder accepts for
// the types declared in basicClassProgram. Names are the go/types basic
// names the emitter writes into the type table.
var wantBasicClasses = map[string]string{
	"bool":       "bool",
	"uint":       "uint",
	"uint8":      "uint",
	"uint16":     "uint",
	"uint32":     "uint",
	"uint64":     "uint",
	"uintptr":    "uint",
	"int":        "int",
	"int8":       "int",
	"int16":      "int",
	"int32":      "int",
	"int64":      "int",
	"float32":    "float",
	"float64":    "float",
	"complex64":  "complex",
	"complex128": "complex",
	"string":     "string",
}

// acceptedClasses are the basic classes GoKind.fromBasic accepts; any other
// class makes the Java decoder throw IllegalArgumentException.
var acceptedClasses = map[string]bool{
	"int": true, "uint": true, "float": true, "complex": true,
	"bool": true, "string": true, "untyped": true,
}

// TestBasicClassEmittedProgram emits basicClassProgram and asserts every
// basic type-table entry carries the decoder-accepted class. Pre-L01 this
// failed: bool entries carried "unknown", uint* entries carried "int".
func TestBasicClassEmittedProgram(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "main.go")
	if err := os.WriteFile(fn, []byte(basicClassProgram), 0644); err != nil {
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

	got := map[string]string{}
	for _, te := range doc.Types {
		m, ok := te.(map[string]any)
		if !ok {
			t.Fatalf("type entry is %T, want map[string]any", te)
		}
		if m["kind"] != "basic" {
			continue
		}
		detail, ok := m["detail"].(map[string]any)
		if !ok {
			t.Fatalf("basic detail is %T, want map[string]any", m["detail"])
		}
		name, _ := detail["name"].(string)
		class, _ := detail["class"].(string)
		if !acceptedClasses[class] {
			t.Errorf("type %q: class %q is rejected by the decoder (GoKind.fromBasic throws)", name, class)
		}
		got[name] = class
	}
	for name, want := range wantBasicClasses {
		class, ok := got[name]
		if !ok {
			t.Errorf("type %q missing from emitted type table", name)
			continue
		}
		if class != want {
			t.Errorf("type %q: class = %q, want %q (decoder rejects %q)", name, class, want, class)
		}
	}
}
