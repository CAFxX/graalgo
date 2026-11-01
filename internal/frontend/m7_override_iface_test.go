package main

// P3-M8 (wave 2, reflect): reflect.Type interface multi-result calls
// after the synthetic twin's deletion (review R7, 2026-11-01).
//
// History: reflect retired from synthetic to the disk importer, but
// its M7 __-helpers and pair types lingered in the synthetic reflect_m7
// package, reached through the m7HelperPkgOverride ("reflect" ->
// "reflect_m7") consulted by classifyIfaceMultiResultCall and
// classifyIfaceFallbackMultiResultCall. Before that override fix, any
// reflect.Type interface multi-result call (e.g. t.FieldByName("X"))
// panicked with "gofrontend: no synthetic source for reflect".
//
// R7 completed the retirement: synth_reflect.go is deleted, the
// override map and the m8 reflect rows are gone, and reflect.Type
// interface calls desugar through the general disk-interface
// machinery (mr_disk.go interface specs: __Type_<Method>Pair{f1, f2}
// with disk result types) or the user-interface fallback. The tests
// below pin that division of labor and that no reflect_m7 reference
// survives anywhere in the exported document.
//
// fix-redmaster-r43: MethodByName on reflect.Type no longer exports
// as ifacemethod. Guest MethodByName via ifacemethod dispatch always
// returns false at runtime (shells carry no uncommon data), so real
// Type.MethodByName calls are intercepted to the TypeInfo-based
// reflectop path (typeMethodByName); FieldByName keeps the
// ifacemethod desugar pinned below.

import (
	"strings"
	"testing"
)

// TestM7OverrideIfaceReflectMethod pins that reflect.Type interface
// multi-result calls export without the deleted synthetic twin. Before
// the override fix, classifyIfaceMultiResultCall called
// getSyntheticPackage("reflect") for the m8IfaceMultiResultMethods
// reflect entries, panicking with "gofrontend: no synthetic source
// for reflect" (reflect retired from synthetic to the disk importer).
// After review R7 the twin is gone entirely: the export completes
// with an ifacemethod desugar typed by the disk-interface pair, and
// no "reflect_m7" reference survives in the exported document.
func TestM7OverrideIfaceReflectMethod(t *testing.T) {
	const src = `package main

import "reflect"

func main() {
	var s struct{ X int }
	t := reflect.TypeOf(s)
	f, ok := t.FieldByName("X")
	println(f.Name, ok)
	m, ok2 := t.MethodByName("String")
	println(m.Name, ok2)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	// R7: the deleted twin must leave no trace in the export.
	if strings.Contains(string(out), "reflect_m7") {
		t.Fatal("exported document still references the deleted reflect_m7 twin")
	}
	doc := decodeExport(t, out)

	// FieldByName must desugar to an ifacemethod node in main (via
	// the disk-interface pair now, not the deleted twin);
	// MethodByName on reflect.Type must desugar to
	// a reflectop typeMethodByName node (the fix-redmaster-r43
	// interception — guest dispatch always returns false).
	methods := map[string]bool{}
	ops := map[string]bool{}
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "ifacemethod") {
			nf := n["fields"].(map[string]any)
			method, _ := nf["method"].(string)
			methods[method] = true
		}
		for _, n := range findAllKind(fm, "reflectop") {
			nf := n["fields"].(map[string]any)
			op, _ := nf["op"].(string)
			ops[op] = true
		}
	}
	if !methods["FieldByName"] {
		t.Fatal("no ifacemethod node for FieldByName in main")
	}
	if !ops["typeMethodByName"] {
		t.Fatal("no reflectop typeMethodByName node for MethodByName in main")
	}
}

// TestM7OverrideIfaceStillStructural pins that the structural guard
// survives the twin's deletion: a user interface with a
// FieldByName-shaped method but non-matching result types must NOT be
// dispatched through a reflect pair (the deleted reflect_m7 pair is
// the regression shape this guards against).
func TestM7OverrideIfaceStillStructural(t *testing.T) {
	const src = `package main

type T interface {
	FieldByName(name string) (int, bool)
}

type B struct{}

func (B) FieldByName(name string) (int, bool) { return 0, false }

func main() {
	var t T = B{}
	n, ok := t.FieldByName("X")
	println(n, ok)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	types := typeEntries(t, doc)
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "ifacemethod") {
			nf := n["fields"].(map[string]any)
			if nf["method"] != "FieldByName" {
				continue
			}
			typeID, _ := nf["type"].(float64)
			te, ok := types[int(typeID)].(map[string]any)
			if !ok {
				t.Fatalf("ifacemethod type id %v out of range", typeID)
			}
			detail, _ := te["detail"].(map[string]any)
			name, _ := detail["name"].(string)
			if strings.HasPrefix(name, "reflect_m7.") {
				t.Fatalf("user FieldByName dispatched through %q; structural guard bypassed", name)
			}
		}
	}
}
