package main

// fix-1446d8f9 (reflect M7 desugaring for disk-imported packages):
// reflect retired from synthetic packages to the disk importer, but the
// m7MultiResultFuncs table still carried reflect entries. The static M7
// classifiers (classifyM7MultiResultCall, classifyIoMultiResultCall,
// classifyM7MultiResultCallIfaceFallback) called
// getSyntheticPackage("reflect"), which panics with
// "gofrontend: no synthetic source for reflect". The general fix adds
// m7HelperPkgOverride (package path -> synthetic package holding its M7
// __-helpers and pair types; "reflect" -> "reflect_m7", the retired
// synth_reflect.go helpers registered under a distinct name) and routes
// all three static classifiers through it. Any future retired synthetic
// package with M7 entries uses the same mechanism.
//
// Red-green: before the fix, TestReflectStaticM7FieldByIndexErr panics
// with that message; after, the call desugars to the reflect_m7
// __reflectFieldByIndexErr helper.

import (
	"strings"
	"testing"
)

// TestReflectStaticM7FieldByIndexErr pins the static (non-interface) M7
// classifier path: a multi-result method call on a concrete reflect.Value
// must desugar through the reflect_m7 override package, not panic
// looking up a synthetic "reflect" package.
func TestReflectStaticM7FieldByIndexErr(t *testing.T) {
	const src = `package main

import "reflect"

func main() {
	var s struct{ X int }
	v := reflect.ValueOf(s)
	x, err := v.FieldByIndexErr([]int{0})
	println(x.IsValid(), err != nil)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if !strings.Contains(out, "__reflectFieldByIndexErr") {
		t.Errorf("static reflect M7 call did not desugar to the __reflectFieldByIndexErr helper;\n%s", out[:2000])
	}
}
