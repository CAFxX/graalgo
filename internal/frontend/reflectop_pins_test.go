package main

// NEW-P1-2 (REVIEW-2026-11-05-22): pins for the fix-redmaster-r43
// interceptions that route real-`reflect` calls to the table-backed
// "reflectop" wire node instead of compiling them normally. Each pin
// asserts the exporter emits the expected reflectop op in `main`.
// The `reflect.Type` interception (`typeMethodByName`) is pinned
// separately in m7_override_iface_test.go
// (TestM7OverrideIfaceReflectMethod); this file pins the other two.
//
// Red-green: deleting any of the three interceptions in export.go
// makes the corresponding test fail (no reflectop node with that op
// in main). The interceptions exist because guest dispatch of these
// calls is wrong at runtime: guest MakeFunc cannot build its
// trampoline (no reflectcall equivalent), guest Value.Call traps on
// func values, Interface on func values traps in buildInterfaceView,
// and IsValid misreads Method.Func values on the synthetic layout.
// Retirement plan: docs/arch/bridge-inventory.md B-3.

import "testing"

// reflectopOpsInMain exports src and returns the set of "op" values on
// reflectop nodes in func main.
func reflectopOpsInMain(t *testing.T, src string) map[string]bool {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	ops := map[string]bool{}
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "reflectop") {
			nf := n["fields"].(map[string]any)
			op, _ := nf["op"].(string)
			ops[op] = true
		}
	}
	return ops
}

// TestReflectopMakeFunc pins reflect.MakeFunc -> reflectop op "makefuncval".
func TestReflectopMakeFunc(t *testing.T) {
	const src = `package main

import "reflect"

type F func(int) int

func impl(args []reflect.Value) []reflect.Value {
	return []reflect.Value{reflect.ValueOf(42)}
}

func main() {
	var f F
	fn := reflect.MakeFunc(reflect.TypeOf(f), impl)
	_ = fn
}
`
	if ops := reflectopOpsInMain(t, src); !ops["makefuncval"] {
		t.Fatalf("no reflectop makefuncval node for MakeFunc in main (ops = %v)", ops)
	}
}

// TestReflectopValueMethods pins reflect.Value Call/CallSlice/Interface/
// IsValid -> reflectop ops "call"/"callslice"/"interface"/"isvalid".
func TestReflectopValueMethods(t *testing.T) {
	const src = `package main

import "reflect"

func add(a, b int) int { return a + b }

func main() {
	v := reflect.ValueOf(add)
	out := v.Call([]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)})
	_ = out
	out2 := v.CallSlice([]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)})
	_ = out2
	if v.IsValid() {
		println("valid")
	}
	_ = v.Interface()
}
`
	ops := reflectopOpsInMain(t, src)
	for _, want := range []string{"call", "callslice", "interface", "isvalid"} {
		if !ops[want] {
			t.Fatalf("no reflectop %q node for Value method in main (ops = %v)", want, ops)
		}
	}
}

// TestReflectopDoesNotInterceptNonReflect pins the discriminators:
// same-named methods on non-reflect types, and a MakeFunc in another
// package, must NOT route to reflectop. The Value discriminator is
// the receiver's named type (reflect.Value); the MakeFunc
// discriminator is the resolved reflect package.
func TestReflectopDoesNotInterceptNonReflect(t *testing.T) {
	const src = `package main

type Caller struct{}

func (Caller) Call()              {}
func (Caller) IsValid() bool     { return true }
func (Caller) Interface() any    { return nil }
func (Caller) CallSlice()        {}

func MakeFunc() {}

func main() {
	c := Caller{}
	c.Call()
	c.CallSlice()
	_ = c.IsValid()
	_ = c.Interface()
	MakeFunc()
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	if nodes := findAllKind(doc, "reflectop"); len(nodes) != 0 {
		t.Fatalf("reflectop nodes = %d for non-reflect calls, want 0", len(nodes))
	}
}
