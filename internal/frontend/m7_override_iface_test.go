package main

// P3-M8 (wave 2, reflect): the m7HelperPkgOverride gap in the interface
// multi-result classifiers.
//
// classifyIfaceMultiResultCall and classifyIfaceFallbackMultiResultCall
// must consult m7HelperPkgOverride when resolving the pair type's
// package: reflect retired from synthetic to the disk importer, but its
// M7 __-helpers and pair types still live in the synthetic reflect_m7
// package. The other three classifiers (classifyM7MultiResultCall,
// classifyIoMultiResultCall, classifyM7MultiResultCallIfaceFallback)
// already did; the two interface classifiers did not, so any
// reflect.Type interface multi-result call (e.g. t.FieldByName("X"))
// panicked with "gofrontend: no synthetic source for reflect".
//
// Red-green: before the fix, TestM7OverrideIfaceReflectMethod panics
// with that message; after, it exports an ifacemethod node typed as the
// reflect_m7 pair.
//
// fix-redmaster-r43: MethodByName on reflect.Type no longer exports
// as ifacemethod. Guest MethodByName via ifacemethod dispatch always
// returns false at runtime (shells carry no uncommon data), so real
// Type.MethodByName calls are intercepted to the TypeInfo-based
// reflectop path (typeMethodByName); FieldByName keeps the
// ifacemethod desugar pinned below. The test still pins the M7
// requirement (no exporter panic) plus the new division of labor.

import (
	"strings"
	"testing"
)

// TestM7OverrideIfaceReflectMethod pins that reflect.Type interface
// multi-result calls no longer panic the exporter. Before the fix,
// classifyIfaceMultiResultCall called getSyntheticPackage("reflect")
// for the m8IfaceMultiResultMethods reflect entries, panicking with
// "gofrontend: no synthetic source for reflect" (reflect retired from
// synthetic to the disk importer). After the fix, the classifier
// consults m7HelperPkgOverride ("reflect" -> "reflect_m7") and the
// export completes with an ifacemethod desugar.
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
	doc := decodeExport(t, out)

	// FieldByName must desugar to an ifacemethod node in main (the
	// M7 override); MethodByName on reflect.Type must desugar to
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

// TestM7OverrideIfaceStillStructural pins that the override does not
// loosen the structural guard: a user interface with a FieldByName-shaped
// method but non-matching result types must NOT be dispatched through
// the reflect_m7 pair.
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
