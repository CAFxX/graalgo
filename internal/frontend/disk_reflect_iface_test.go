package main

// Real (disk-imported) reflect: interface multi-result dispatch.
//
// reflect retired from synthetic to the disk importer; its multi-result
// members (Type.MethodByName/FieldByName/FieldByNameFunc, Value.TryRecv,
// Select, ...) are pair-transformed by transformDiskMultiResult (WS G).
// Calls through the reflect.Type interface desugar via
// classifyDiskIfaceMultiResultCall (the MODPROBE-005 interface specs):
// the itab maps the interface method to the concrete type's
// pair-returning __-helper (e.g. __rtype_FieldByName), so the node
// yields the pair and field extraction proceeds like the concrete
// desugar.
//
// Red-green: before the fix (static M7 reflect entries + synthetic
// reflect_m7 helpers), t.FieldByName("X") routed to the synthetic
// helper written against the synthetic Value layout and panicked at
// runtime; after, it exports an ifacemethod node typed as the disk
// pair.

import (
	"strings"
	"testing"
)

// TestDiskReflectIfaceMethod pins that reflect.Type interface
// multi-result calls desugar through the disk interface path. The
// export must complete with ifacemethod nodes for FieldByName and
// MethodByName (not a "no synthetic source" panic, not a fail-fast).
func TestDiskReflectIfaceMethod(t *testing.T) {
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

	// Both calls must desugar to ifacemethod nodes in main.
	methods := map[string]bool{}
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
	}
	if !methods["FieldByName"] {
		t.Fatal("no ifacemethod node for FieldByName in main")
	}
	if !methods["MethodByName"] {
		t.Fatal("no ifacemethod node for MethodByName in main")
	}
}

// TestDiskReflectIfaceStillStructural pins that the disk interface
// dispatch does not loosen the structural guard: a user interface with
// a FieldByName-shaped method but non-matching result types must NOT
// be dispatched through reflect's disk pair.
func TestDiskReflectIfaceStillStructural(t *testing.T) {
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
			if strings.HasPrefix(name, "reflect.") {
				t.Fatalf("user FieldByName dispatched through %q; structural guard bypassed", name)
			}
		}
	}
}

// TestDiskReflectSelectDesugar pins that reflect.Select desugars to a
// "reflectop" wire node with op "selectimpl" (mrReflectOp), not to a
// __-helper call. The real Select body needs the bodyless rselect
// runtime function; the backend implements select directly.
func TestDiskReflectSelectDesugar(t *testing.T) {
	const src = `package main

import "reflect"

func main() {
	ch := make(chan int, 1)
	ch <- 42
	cases := []reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)},
		{Dir: reflect.SelectDefault},
	}
	chosen, recv, ok := reflect.Select(cases)
	println(chosen, recv.Int(), ok)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	found := false
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "reflectop") {
			nf := n["fields"].(map[string]any)
			if op, _ := nf["op"].(string); op == "selectimpl" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no reflectop/selectimpl node for reflect.Select in main")
	}
}

// TestDiskReflectTryRecvDesugar pins that (reflect.Value).TryRecv
// desugars to a "reflectop" wire node with op "tryrecv" (mrReflectOp),
// not to the __-helper (whose real body needs bodyless chanrecv).
func TestDiskReflectTryRecvDesugar(t *testing.T) {
	const src = `package main

import "reflect"

func main() {
	ch := make(chan int, 1)
	ch <- 7
	v := reflect.ValueOf(ch)
	x, ok := v.TryRecv()
	println(x.Int(), ok)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	found := false
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != "main" {
			continue
		}
		for _, n := range findAllKind(fm, "reflectop") {
			nf := n["fields"].(map[string]any)
			if op, _ := nf["op"].(string); op == "tryrecv" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no reflectop/tryrecv node for Value.TryRecv in main")
	}
}

// TestMethodValueMultiResultScopeError pins that a bound method value
// of a multi-result method (e.g. (*big.Int).SetString) fails fast with
// a named scope error instead of emitting a dangling methodvalue node
// ("obj": 0 — the stub has no funcID).
func TestMethodValueMultiResultScopeError(t *testing.T) {
	const src = `package main

import "math/big"

func main() {
	z := new(big.Int)
	sink := z.SetString
	_ = sink
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected multi-result method-value scope error, got success")
	}
	const want = "method value of multi-result method SetString is not supported"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}
