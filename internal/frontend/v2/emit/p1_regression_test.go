// Regression tests for the 2026-10-18 independent review P1 findings: the
// v2 emitter silently miscompiled instead of failing loudly. Each test is
// demonstrably capable of failing — it fails on the pre-fix emitter and
// passes after the fix.
package emit

import (
	"reflect"
	"testing"
)

// findGlobal returns the fields of the "globalvar" node declaring name, or
// nil.
func findGlobal(doc *Document, name string) map[string]any {
	for _, gv := range doc.Globals {
		gm, ok := gv.(map[string]any)
		if !ok {
			continue
		}
		fields, _ := gm["fields"].(map[string]any)
		names, _ := fields["names"].([]string)
		for _, n := range names {
			if n == name {
				return fields
			}
		}
	}
	return nil
}

// findNodes returns the fields of every emitted node with the given kind.
func findNodes(doc *Document, kind string) []map[string]any {
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if k, ok := t["kind"].(string); ok && k == kind {
				if f, ok := t["fields"].(map[string]any); ok {
					out = append(out, f)
				}
			}
			for _, w := range t {
				walk(w)
			}
		case []any:
			for _, w := range t {
				walk(w)
			}
		}
	}
	walk(doc.Funcs)
	walk(doc.Globals)
	return out
}

// TestGlobalsEmitted: a package-level var's initializer must be emitted,
// not silently lost (review issue 2). Pre-fix, Document.Globals was always
// empty while the program was accepted.
func TestGlobalsEmitted(t *testing.T) {
	src := `package main

var Global = 99
var Typed string = "hi"
var Multi1, Multi2 = 1, 2

func main() { println(Global, Typed, Multi1+Multi2) }
`
	doc := mustEmit(t, src)
	if len(doc.Globals) != 3 {
		t.Fatalf("globals = %d entries, want 3", len(doc.Globals))
	}
	// The untyped initializer 99 must be present as a basiclit value.
	g := findGlobal(doc, "Global")
	if g == nil {
		t.Fatal("no globalvar node declares Global")
	}
	vals, _ := g["values"].([]any)
	if len(vals) != 1 {
		t.Fatalf("Global values = %d, want 1", len(vals))
	}
	vn, _ := vals[0].(map[string]any)
	if vn["kind"] != "basiclit" {
		t.Fatalf("Global value kind = %v, want basiclit", vn["kind"])
	}
	vf, _ := vn["fields"].(map[string]any)
	if vf["value"] != "99" {
		t.Fatalf("Global value = %v, want 99 (initializer lost)", vf["value"])
	}
	// A multi-name spec keeps every name and every value.
	m := findGlobal(doc, "Multi1")
	if m == nil {
		t.Fatal("no globalvar node declares Multi1")
	}
	names, _ := m["names"].([]string)
	if !reflect.DeepEqual(names, []string{"Multi1", "Multi2"}) {
		t.Fatalf("multi names = %v, want [Multi1 Multi2]", names)
	}
	mvals, _ := m["values"].([]any)
	if len(mvals) != 2 {
		t.Fatalf("multi values = %d, want 2", len(mvals))
	}
}

// TestConstAndNilObjectsEmitted: *types.Const and *types.Nil must not be
// described as kind "var" with no value (review issue 3). Pre-fix, `true`
// and `nil` produced {"kind":"var"} with the value silently lost.
func TestConstAndNilObjectsEmitted(t *testing.T) {
	src := `package main

func f() *int { return nil }

func main() {
	t := true
	p := f()
	println(t, p == nil)
}
`
	doc := mustEmit(t, src)
	var sawConst, sawNil bool
	for _, ov := range doc.Objects {
		om, ok := ov.(map[string]any)
		if !ok {
			continue
		}
		switch om["kind"] {
		case "const":
			if om["name"] == "true" {
				sawConst = true
				if om["value"] != "true" {
					t.Fatalf("const true value = %v, want \"true\"", om["value"])
				}
			}
		case "nil":
			sawNil = true
		case "var":
			// No const/nil object may hide behind kind "var".
			if om["name"] == "true" || om["name"] == "nil" {
				t.Fatalf("object %q described as var (the silent-loss bug)", om["name"])
			}
		}
	}
	if !sawConst {
		t.Error("no const object for `true` in the object table")
	}
	if !sawNil {
		t.Error("no nil object in the object table")
	}
}

// TestAliasTypesEmitted: *types.Alias must get its own kind instead of
// {"kind":"basic","class":"unknown"} (review issue 4). In go1.27 the
// predeclared `any` is an alias; unsigned basics (uint..uintptr, byte) also
// previously fell through basicClass to "unknown" because their go/types
// info flag composes IsInteger|IsUnsigned.
func TestAliasTypesEmitted(t *testing.T) {
	src := `package main

func main() {
	var x any
	var b byte
	var r rune
	var u uint
	x, b, r, u = 1, 2, 3, 4
	println(x, b, r, u)
}
`
	doc := mustEmit(t, src)
	aliases := map[string]bool{}
	classes := map[string]string{}
	for _, tv := range doc.Types {
		tm, ok := tv.(map[string]any)
		if !ok {
			continue
		}
		detail, _ := tm["detail"].(map[string]any)
		name, _ := detail["name"].(string)
		switch tm["kind"] {
		case "basic":
			if detail["class"] == "unknown" {
				t.Fatalf("type %q mis-emitted as basic/unknown", name)
			}
			classes[name] = detail["class"].(string)
		case "alias":
			aliases[name] = true
			if _, ok := detail["rhs"]; !ok {
				t.Fatalf("alias %q has no rhs", name)
			}
		}
	}
	if !aliases["any"] {
		t.Error(`no alias type entry for "any"`)
	}
	for name, want := range map[string]string{"byte": "uint", "rune": "int", "uint": "uint"} {
		if got := classes[name]; got != want {
			t.Errorf("basic %q class = %q, want %q", name, got, want)
		}
	}
}

// TestSelectorCarriesSelection: a selector node must carry the go/types
// Selection — kind (field vs method), the embedded-field index path, and
// whether pointer indirection applied (review issue 5). Pre-fix the
// selection was discarded (`_ = sel`), silently miscompiling embedded-field
// access and pointer adjustments.
func TestSelectorCarriesSelection(t *testing.T) {
	src := `package main

import "bytes"

func main() {
	b := struct {
		bytes.Buffer
	}{}
	b.WriteString("hi")
	p := &struct{ X int }{X: 7}
	println(b.String(), p.X)
}
`
	doc := mustEmit(t, src)
	sels := findNodes(doc, "selector")
	if len(sels) == 0 {
		t.Fatal("no selector nodes emitted")
	}
	var sawEmbeddedMethod, sawIndirectField bool
	for _, f := range sels {
		sel, _ := f["sel"].(string)
		kind, _ := f["selkind"].(string)
		index, _ := f["index"].([]int)
		indirect, _ := f["indirect"].(bool)
		if f["obj"] == nil {
			t.Fatalf("selector %q has no obj", sel)
		}
		switch sel {
		case "String":
			// Method promoted through the embedded bytes.Buffer: the
			// index path has one entry per embedding level plus the
			// method's position in the method set — [0, 2] here. What
			// matters is that the path is carried, not dropped.
			if kind != "method" {
				t.Errorf("String selkind = %q, want method", kind)
			}
			if len(index) != 2 || index[0] != 0 {
				t.Errorf("String index = %v, want [0 <method>] (embedded path lost)", index)
			}
			sawEmbeddedMethod = true
		case "X":
			// Field through a pointer: indirection applied.
			if kind != "field" {
				t.Errorf("X selkind = %q, want field", kind)
			}
			if !indirect {
				t.Errorf("X indirect = false, want true (pointer adjustment lost)")
			}
			sawIndirectField = true
		}
	}
	if !sawEmbeddedMethod {
		t.Error("no selector node for the embedded method call b.String()")
	}
	if !sawIndirectField {
		t.Error("no selector node for the pointer field access p.X")
	}
}
