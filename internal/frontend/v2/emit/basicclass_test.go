// Regression tests for the review-1106-p0-basicclass remediation lane
// (independent codebase review, 2026-11-06, R-P0-1).
//
// basicClass switched on `b.Info() & types.IsNumeric`, which strips the
// IsBoolean/IsUnsigned bits before the switch ever sees them, so bool came
// out class "unknown" and every unsigned int came out "int"; unsafe.Pointer
// (Info 0) could never reach the "pointer" class. GoKind.fromBasic throws
// on all three, so any v2 program using a bool, an unsigned int, or an
// unsafe.Pointer failed at decode. This test pins the emitted class for
// every basic kind against the vocabulary GoKind.fromBasic accepts. It
// fails on the pre-fix emitter (bool "unknown", unsigned "int",
// unsafe.Pointer "int", untyped bool "untyped").
package emit

import "testing"

// basicClassOf returns the emitted class for the basic type with the given
// name in the document's type table, or "" if no such type is present.
func basicClassOf(t *testing.T, doc *Document, name string) string {
	t.Helper()
	id := basicTypeID(doc, name)
	if id < 0 {
		return ""
	}
	m, ok := doc.Types[id].(map[string]any)
	if !ok {
		return ""
	}
	d, ok := m["detail"].(map[string]any)
	if !ok {
		return ""
	}
	c, _ := d["class"].(string)
	return c
}

// TestBasicClass asserts the emitted class for every basic type the v2
// emitter can produce, typed and untyped. The expected classes are exactly
// the vocabulary GoKind.fromBasic accepts ("int", "uint", "float",
// "complex", "bool", "string", "pointer"); any other class throws at
// decode. The unsafe.Pointer entry is keyed by its go/types name
// ("Pointer"); fromBasic ignores the name for class "pointer".
func TestBasicClass(t *testing.T) {
	doc := emitTestSrc(t, `package main

import "unsafe"

var b bool
var i int
var i8 int8
var i16 int16
var i32 int32
var i64 int64
var u uint
var u8 uint8
var u16 uint16
var u32 uint32
var u64 uint64
var up uintptr
var by byte
var rn rune
var f32 float32
var f64 float64
var c64 complex64
var c128 complex128
var s string
var p unsafe.Pointer

const ub = true
const ui = 42
const ur = 'a'
const uf = 1.5
const uc = 1 + 2i
const us = "x"

func main() {
	println(b, i, i8, i16, i32, i64, u, u8, u16, u32, u64, up, by, rn, f32, f64, c64, c128, s, ub, ui, ur, uf, uc, us)
	_ = p
}
`)
	want := map[string]string{
		"bool":            "bool",
		"int":             "int",
		"int8":            "int",
		"int16":           "int",
		"int32":           "int",
		"int64":           "int",
		"uint":            "uint",
		"uint8":           "uint",
		"uint16":          "uint",
		"uint32":          "uint",
		"uint64":          "uint",
		"uintptr":         "uint",
		"byte":            "uint",
		"rune":            "int",
		"float32":         "float",
		"float64":         "float",
		"complex64":       "complex",
		"complex128":      "complex",
		"string":          "string",
		"Pointer":         "pointer",
		"untyped bool":    "bool",
		"untyped int":     "int",
		"untyped rune":    "int",
		"untyped float":   "float",
		"untyped complex": "complex",
		"untyped string":  "string",
	}
	for name, class := range want {
		if got := basicClassOf(t, doc, name); got != class {
			t.Errorf("basic %q: class = %q, want %q", name, got, class)
		}
	}
}
