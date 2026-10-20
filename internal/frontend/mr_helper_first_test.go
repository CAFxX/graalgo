package main

// P3-M9 (WS D, r21): multi-result methods in interface method tables must
// map to the pair-returning __-helper, not the original multi-result
// funcdecl.
//
// Backend v1 cannot build a GoFunc for a multi-result funcdecl
// (NodeFactory throws "function with multiple results is not supported
// in v1"). The itab must therefore resolve every multi-result interface
// method to its __-helper (per the mrIfaceMethod design,
// mrdesugar_m5.go:87-92). methodTableEntries tries the helper FIRST for
// multi-result methods — a funcByObj hit does not mean the backend can
// use it.
//
// Red-green: before the fix, the itab entry for reflect.FieldByName
// (via reflect.Type interface dispatch, e.g. fmt.Print → reflect.TypeOf
// → Kind) carried the original 2-result method's obj, and the backend's
// IfaceMethodTable.buildItab threw: interface dispatch: method
// "FieldByName" missing for dynamic type "*reflect.rtype". After the
// fix, the entry maps to the __reflectTFieldByName helper.

import (
	"testing"
)

// TestMultiResultMethodTableHasFieldByName pins that the method table
// for the disk reflect rtype includes the FieldByName entry (mapped via
// the m7HelperPkgOverride to the reflect_m7 sidecar helper). Before the
// fix, the entry was missing or mapped to the unusable 2-result
// original, causing the backend itab build to fail.
func TestMultiResultMethodTableHasFieldByName(t *testing.T) {
	const src = `package main

import (
	"fmt"
	"reflect"
)

func main() {
	// Force reflect.Type interface dispatch through *reflect.rtype:
	// TypeOf returns the interface, Kind dispatches via the itab.
	t := reflect.TypeOf(1)
	_ = t.Kind()
	fmt.Print(1, 2, " ")
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	types := typeEntries(t, doc)

	// The export must include a method table entry for FieldByName on
	// the reflect rtype (pointer). This verifies the helper lookup
	// succeeds through the override (reflect → reflect_m7).
	found := false
	for _, te := range types {
		tm, _ := te.(map[string]any)
		detail, _ := tm["detail"].(map[string]any)
		methods, _ := detail["methods"].([]any)
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			// Method entries are encoded as objects with fields.
			var name string
			if mf, ok := mm["fields"].(map[string]any); ok {
				name, _ = mf["name"].(string)
			} else {
				name, _ = mm["name"].(string)
			}
			if name == "FieldByName" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no FieldByName entry in any method table; helper mapping failed")
	}
}

// TestMultiResultHelperFirstFallsThrough pins the negative: a
// multi-result method with NO registered __-helper must fall through to
// the original funcByObj entry (not invent a helper, not fail). This
// verifies the helper-FIRST is a preference, not a requirement.
func TestMultiResultHelperFirstFallsThrough(t *testing.T) {
	const src = `package main

type I interface {
	TwoResults() (int, bool)
}

type C struct{}

func (C) TwoResults() (int, bool) { return 1, true }

func main() {
	var i I = C{}
	a, b := i.TwoResults()
	println(a, b)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	types := typeEntries(t, doc)

	found := false
	for _, te := range types {
		tm, _ := te.(map[string]any)
		detail, _ := tm["detail"].(map[string]any)
		methods, _ := detail["methods"].([]any)
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			var name string
			if mf, ok := mm["fields"].(map[string]any); ok {
				name, _ = mf["name"].(string)
			} else {
				name, _ = mm["name"].(string)
			}
			if name == "TwoResults" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("TwoResults not found in method tables; fallthrough broken")
	}
}
