// Regression test for L13 (v2 selector wire must carry the method/field
// object id).
//
// The emitter used to discard the go/types Selection (`_ = sel`) and emit
// only the selector NAME. Same-name methods on different types were then
// indistinguishable on the wire, and the Java backend resolved every
// selector by struct field-name lookup — method calls threw (or, with a
// same-named field, bound wrong).
//
// The test is demonstrably capable of failing: with the fix reverted, the
// emitted selector nodes carry no "obj" field and every assertion below on
// obj identity fails.
package emit

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// selectorObjIDSources declares two types with a same-named method plus
// ordinary field accesses: the minimal program that proves selectors carry
// identity, not just names.
var selectorObjIDSources = map[string]string{
	"main.go": `package demo

type T1 struct{ X int }
type T2 struct{ Y int }

func (t T1) M() int { return t.X }
func (t T2) M() int { return t.Y * 2 }

func Use(a T1, b T2) int {
	return a.M() + b.M() + a.X
}
`,
}

type selectorWire struct {
	sel string
	obj int
}

func TestEmitSelectorCarriesObjectID(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, len(selectorObjIDSources))
	for name, src := range selectorObjIDSources {
		fn := filepath.Join(dir, name)
		if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, fn)
	}
	sort.Strings(paths)

	pres, err := parser.ParseFiles(paths)
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

	var selectors []selectorWire
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["kind"] == "selector" {
				f := n["fields"].(map[string]any)
				obj, ok := f["obj"].(int)
				if !ok {
					t.Fatalf("selector %q has no int obj id (got %v)", f["sel"], f["obj"])
				}
				selectors = append(selectors, selectorWire{sel: f["sel"].(string), obj: obj})
			}
			for _, val := range n {
				walk(val)
			}
		case []any:
			for _, val := range n {
				walk(val)
			}
		}
	}
	for _, f := range doc.Funcs {
		walk(f)
	}

	objAt := func(id int) map[string]any {
		if id < 0 || id >= len(doc.Objects) {
			t.Fatalf("obj id %d out of range (objects=%d)", id, len(doc.Objects))
		}
		m, ok := doc.Objects[id].(map[string]any)
		if !ok {
			t.Fatalf("object %d is not a map", id)
		}
		return m
	}

	// Partition: 3 method selections (a.M, b.M) and field selections (a.X twice).
	var methodObjs []int
	fieldObjs := map[string][]int{}
	for _, s := range selectors {
		obj := objAt(s.obj)
		switch obj["kind"] {
		case "func":
			if s.sel != "M" {
				t.Fatalf("func selector has unexpected name %q", s.sel)
			}
			if obj["name"] != "M" {
				t.Fatalf("method obj %d name = %v, want M", s.obj, obj["name"])
			}
			methodObjs = append(methodObjs, s.obj)
		case "var":
			if obj["name"] != s.sel {
				t.Fatalf("field obj %d name = %v, selector = %q", s.obj, obj["name"], s.sel)
			}
			fieldObjs[s.sel] = append(fieldObjs[s.sel], s.obj)
		default:
			t.Fatalf("selector %q resolved to object of kind %v", s.sel, obj["kind"])
		}
	}

	// The two M selections must point at DISTINCT method objects with
	// DISTINCT signature types (T1.M vs T2.M) — the exact identity the
	// name-only wire used to drop.
	if len(methodObjs) != 2 {
		t.Fatalf("got %d method selections, want 2", len(methodObjs))
	}
	if methodObjs[0] == methodObjs[1] {
		t.Fatalf("a.M() and b.M() share obj id %d: methods indistinguishable", methodObjs[0])
	}
	sig0 := objAt(methodObjs[0])["type"]
	sig1 := objAt(methodObjs[1])["type"]
	if sig0 == sig1 {
		t.Fatalf("T1.M and T2.M share signature type id %v", sig0)
	}

	// Field selections resolve to var objects, consistently: both a.X
	// selections must carry the same obj id.
	if len(fieldObjs["X"]) != 2 || fieldObjs["X"][0] != fieldObjs["X"][1] {
		t.Fatalf("a.X selections resolved to %v, want same obj id twice", fieldObjs["X"])
	}
	if len(fieldObjs["Y"]) != 1 {
		t.Fatalf("t.Y selections resolved to %v, want exactly one", fieldObjs["Y"])
	}
}
