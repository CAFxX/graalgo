package main

// Implicit method-receiver & on index expressions: wire-shape tests.
//
// x.M() with a pointer receiver on an addressable receiver x is (&x).M()
// per the Go spec. When x is an index expression (arr[i], s[i]), the
// exporter must emit the lvalue {"array","index","type"} addrof schema
// (the same schema explicit &arr[i] emits), so the backend addresses the
// element slot. Emitting the operand-addressing {"x","type"} schema makes
// buildAddrOf snapshot the element value, silently losing receiver
// mutations for value-copied element types (scalars) — a P0 correctness
// bug (differential corpus: test/differential/corpus/49_recv_addr_index).
//
// Red-green: the index-receiver cases fail before the addrofNode fix (the
// addrof node carries "x", not "array"/"index") and pass after. The ident
// case passes before and after (guards the helper's fallthrough).

import (
	"testing"
)

// addrofFields returns the fields of the single addrof node wrapping the
// methodref receiver in doc.
func addrofFields(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	mr := singleMethodRef(t, doc)
	recv := methodRefRecv(t, mr)
	if recv["kind"] != "addrof" {
		t.Fatalf("methodref recv kind = %v, want addrof (implicit &)", recv["kind"])
	}
	f, ok := recv["fields"].(map[string]any)
	if !ok {
		t.Fatal("addrof has no fields")
	}
	return f
}

func requireLvalueAddrof(t *testing.T, f map[string]any) {
	t.Helper()
	if _, ok := f["x"]; ok {
		t.Fatal("implicit & on index receiver emitted operand schema {\"x\"}: " +
			"backend would snapshot the element and lose writes")
	}
	arr, ok := f["array"].(map[string]any)
	if !ok {
		t.Fatalf("addrof has no array field: %v", f)
	}
	idx, ok := f["index"].(map[string]any)
	if !ok {
		t.Fatalf("addrof has no index field: %v", f)
	}
	if arr["kind"] == "index" {
		t.Fatalf("array operand is itself an index node (double-index not flattened): %v", arr["kind"])
	}
	_ = idx
}

// TestImplicitAddrOfIndexArray: arr[i].M() with pointer-receiver M emits
// the lvalue addrof schema.
func TestImplicitAddrOfIndexArray(t *testing.T) {
	const src = `package main

type I int

func (i *I) Inc() { *i++ }

func main() {
	arr := [3]I{10, 20, 30}
	arr[1].Inc()
	println(int(arr[1]))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	requireLvalueAddrof(t, addrofFields(t, decodeExport(t, out)))
}

// TestImplicitAddrOfIndexSlice: s[i].M() with pointer-receiver M emits the
// lvalue addrof schema.
func TestImplicitAddrOfIndexSlice(t *testing.T) {
	const src = `package main

type I int

func (i *I) Inc() { *i++ }

func main() {
	s := []I{10, 20, 30}
	s[2].Inc()
	println(int(s[2]))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	requireLvalueAddrof(t, addrofFields(t, decodeExport(t, out)))
}

// TestImplicitAddrOfIndexMethodValue: f := arr[i].M binds the slot — the
// methodvalue receiver is the lvalue addrof schema.
func TestImplicitAddrOfIndexMethodValue(t *testing.T) {
	const src = `package main

type I int

func (i *I) Inc() { *i++ }

func main() {
	arr := [3]I{10, 20, 30}
	f := arr[1].Inc
	f()
	println(int(arr[1]))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	mvs := findAllKind(doc, "methodvalue")
	if len(mvs) != 1 {
		t.Fatalf("want exactly 1 methodvalue node, got %d", len(mvs))
	}
	f, ok := mvs[0]["fields"].(map[string]any)
	if !ok {
		t.Fatal("methodvalue has no fields")
	}
	recv, ok := f["recv"].(map[string]any)
	if !ok {
		t.Fatal("methodvalue has no recv node")
	}
	if recv["kind"] != "addrof" {
		t.Fatalf("methodvalue recv kind = %v, want addrof (implicit &)", recv["kind"])
	}
	rf, ok := recv["fields"].(map[string]any)
	if !ok {
		t.Fatal("addrof has no fields")
	}
	requireLvalueAddrof(t, rf)
}

// TestImplicitAddrOfIdentUnchanged: v.M() on a variable receiver keeps the
// operand-addressing {"x"} addrof schema (the common path is untouched).
func TestImplicitAddrOfIdentUnchanged(t *testing.T) {
	const src = `package main

type I int

func (i *I) Inc() { *i++ }

func main() {
	var v I = 10
	v.Inc()
	println(int(v))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	f := addrofFields(t, decodeExport(t, out))
	x, ok := f["x"].(map[string]any)
	if !ok {
		t.Fatalf("ident-receiver addrof lost its x field: %v", f)
	}
	if x["kind"] != "ident" {
		t.Fatalf("addrof x kind = %v, want ident", x["kind"])
	}
	if _, ok := f["array"]; ok {
		t.Fatalf("ident-receiver addrof wrongly emitted the lvalue schema: %v", f)
	}
}
