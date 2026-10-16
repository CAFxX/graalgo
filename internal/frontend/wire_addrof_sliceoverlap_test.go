package main

// Wire-contract regression tests for the "type" field on the addrof and
// sliceoverlap wire nodes (P1-2 follow-up, 2026-10-16).
//
// Design decision (pinned here and in the commit message): the "type"
// field is NOT dead wire weight. The per-kind builders (buildAddrOf,
// buildSliceOverlap in NodeFactory.java) don't read it, but the central
// buildExpr dispatcher stamps every expression node's fields.type via
// setTypeId — interface boxing and dynamic-type descriptors need the
// static type-table id. Removing "type" from the emitter would silently
// drop the stamp (typeId() stays -1). These tests pin the emitter side
// of the contract: every addrof/sliceoverlap node carries a numeric
// "type", in the schema matching its addressing mode.
//
// Red-green: deleting the "type" emission from any of the four addrof
// sites (export.go) or from exportAliasIntrinsic (synth_machinery.go)
// fails these tests.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// addrofNodes returns every addrof wire node in the document.
func addrofNodes(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	return findAllKind(doc, "addrof")
}

// numericTypeField asserts the node's fields carry a numeric "type" and
// returns it.
func numericTypeField(t *testing.T, kind string, n map[string]any) int {
	t.Helper()
	f, ok := n["fields"].(map[string]any)
	if !ok {
		t.Fatalf("%s node has no fields: %v", kind, n)
	}
	tid, ok := f["type"].(float64)
	if !ok {
		t.Fatalf("%s node has no numeric \"type\" field: %v", kind, f)
	}
	return int(tid)
}

// TestAddrofWireTypeContract pins the emitter side of the addrof wire
// contract: every addrof node carries a numeric "type" (the *T type-table
// id, consumed by the backend's central buildExpr type-id stamp), and the
// schema matches the addressing mode — {"array","index","type"} for &arr[i]
// (lvalue slot addressing: writes through *p must update arr[i]), versus
// {"x","type"} for &x / &s.f / implicit & on method receivers.
func TestAddrofWireTypeContract(t *testing.T) {
	src := `package main

type T struct{ v int }

func (p *T) Set(v int) { p.v = v }

func main() {
	x := 1
	_ = &x
	s := T{v: 2}
	_ = &s.v
	arr := [3]int{1, 2, 3}
	p1 := &arr[0]
	_ = p1
	sl := []int{4, 5, 6}
	p2 := &sl[1]
	_ = p2
	t := T{v: 3}
	t.Set(4)
	_ = x
	_ = s
	_ = arr
	_ = sl
	_ = t
}
`
	out := exportSrc(t, src)
	doc := decodeExport(t, out)
	nodes := addrofNodes(t, doc)
	if len(nodes) != 5 {
		t.Fatalf("want 5 addrof nodes (&x, &s.v, &arr[0], &sl[1], implicit &t for t.Set), got %d", len(nodes))
	}
	var arrayIndex, xShape int
	for _, n := range nodes {
		tid := numericTypeField(t, "addrof", n)
		if tid < 0 {
			t.Fatalf("addrof node has negative type id: %d", tid)
		}
		f := n["fields"].(map[string]any)
		_, hasArray := f["array"]
		_, hasIndex := f["index"]
		_, hasX := f["x"]
		switch {
		case hasArray && hasIndex && !hasX:
			arrayIndex++
		case hasX && !hasArray && !hasIndex:
			xShape++
		default:
			t.Fatalf("addrof node matches neither schema ({x,type} vs {array,index,type}): %v", f)
		}
	}
	// &arr[0] and &sl[1] use the slot-addressing schema; &x, &s.v and the
	// implicit method-receiver &t use the operand schema.
	if arrayIndex != 2 {
		t.Fatalf("want 2 addrof nodes with {array,index,type} schema (&arr[0], &sl[1]), got %d", arrayIndex)
	}
	if xShape != 3 {
		t.Fatalf("want 3 addrof nodes with {x,type} schema (&x, &s.v, implicit &t), got %d", xShape)
	}
}

// TestSliceOverlapWireTypeContract pins the emitter side of the
// sliceoverlap wire contract: every sliceoverlap node carries a numeric
// "type" (the bool result's type-table id, consumed by the backend's
// central buildExpr type-id stamp), plus "x", "y" and the "inexact" flag.
//
// The intrinsic is only reachable through crypto/internal/fips140/alias,
// which Go's internal-visibility rule admits solely to importers under
// crypto/ — so the driver declares itself (via ExportFiles' testPackageDir)
// as the crypto package's own test binary, exactly like `go test`.
func TestSliceOverlapWireTypeContract(t *testing.T) {
	src := `package main

import "crypto/internal/fips140/alias"

func main() {
	x := []byte{1, 2, 3, 4}
	y := x[1:3]
	_ = alias.AnyOverlap(x, y)
	_ = alias.InexactOverlap(x, y)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "prog.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	// The driver stands in for the crypto package's own test binary
	// (white-box, like `go test`): ExportFiles' testPackageDir declares
	// the package the driver belongs to, conferring crypto's
	// internal-visibility privileges for crypto/internal/fips140/alias.
	goroot := runtime.GOROOT()
	if goroot == "" {
		t.Skip("no GOROOT")
	}
	cryptoDir := filepath.Join(goroot, "src", "crypto")
	if st, err := os.Stat(cryptoDir); err != nil || !st.IsDir() {
		t.Skipf("missing std dir %s", cryptoDir)
	}
	out, err := ExportFiles([]string{p}, cryptoDir)
	if err != nil {
		t.Fatalf("ExportFiles: %v", err)
	}
	doc := decodeExport(t, out)
	nodes := findAllKind(doc, "sliceoverlap")
	if len(nodes) != 2 {
		t.Fatalf("want 2 sliceoverlap nodes (AnyOverlap, InexactOverlap), got %d", len(nodes))
	}
	var inexactSeen, exactSeen bool
	for _, n := range nodes {
		tid := numericTypeField(t, "sliceoverlap", n)
		if tid < 0 {
			t.Fatalf("sliceoverlap node has negative type id: %d", tid)
		}
		f := n["fields"].(map[string]any)
		if _, ok := f["x"].(map[string]any); !ok {
			t.Fatalf("sliceoverlap node has no x expr node: %v", f)
		}
		if _, ok := f["y"].(map[string]any); !ok {
			t.Fatalf("sliceoverlap node has no y expr node: %v", f)
		}
		inexact, ok := f["inexact"].(bool)
		if !ok {
			t.Fatalf("sliceoverlap node has no bool \"inexact\" field: %v", f)
		}
		if inexact {
			inexactSeen = true
		} else {
			exactSeen = true
		}
	}
	if !inexactSeen || !exactSeen {
		t.Fatalf("want one exact (AnyOverlap) and one inexact (InexactOverlap) node, got inexact=%v exact=%v", inexactSeen, exactSeen)
	}
}
