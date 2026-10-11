package main

// P3-M8-leftovers: structural dedup for anonymous interfaces (ifaceStructKey).
// go/types mints a fresh *types.Interface per syntactic occurrence; without
// dedup, a type assertion x.(interface{M()}) and a separate occurrence of the
// same anonymous interface shape get different wire type ids → the itab lookup
// misses and method dispatch returns garbage.
// Red-green: TestAnonInterfaceStructuralDedup fails before the dedup (two
// interface entries for one shape) and passes after (exactly one).

import (
	"testing"
)

// Two syntactic occurrences of interface{ WriteSingle([]byte) int } — a
// parameter type and a type-assertion target — must share one wire type id.
func TestAnonInterfaceStructuralDedup(t *testing.T) {
	const src = `package main

type hasher struct{ n int }

func (h *hasher) WriteSingle(p []byte) int { return 5 }

func use(w interface{ WriteSingle([]byte) int }) int {
	return w.WriteSingle([]byte("x"))
}

func main() {
	var h interface{} = &hasher{}
	if w, ok := h.(interface{ WriteSingle([]byte) int }); ok {
		println(use(w))
	}
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	count := 0
	for _, e := range entries {
		em, _ := e.(map[string]any)
		if em["kind"] != "interface" {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		methods, _ := d["methods"].([]any)
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			if mm["name"] == "WriteSingle" {
				count++
				break
			}
		}
	}
	if count != 1 {
		t.Fatalf("got %d interface type entries with WriteSingle, want exactly 1 (structural dedup by sorted method implkeys)", count)
	}
}

// Negative case: structurally DIFFERENT anonymous interfaces must not share
// a wire id — interface{ A() } vs interface{ A(); B() } are different shapes.
func TestAnonInterfaceNoDedupAcrossShapes(t *testing.T) {
	const src = `package main

type impl struct{}

func (impl) A() int { return 1 }
func (impl) B() int { return 2 }

func useA(x interface{ A() int }) int { return x.A() }
func useAB(x interface {
	A() int
	B() int
}) int {
	return x.A() + x.B()
}

func main() {
	var v interface{} = impl{}
	if a, ok := v.(interface{ A() int }); ok {
		println(useA(a))
	}
	if ab, ok := v.(interface {
		A() int
		B() int
	}); ok {
		println(useAB(ab))
	}
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	// Count interface entries by method-set shape: {A} vs {A,B}.
	shapes := map[string]int{}
	for _, e := range entries {
		em, _ := e.(map[string]any)
		if em["kind"] != "interface" {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		methods, _ := d["methods"].([]any)
		var names []string
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			if n, ok := mm["name"].(string); ok {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			continue // empty interface{} — not under test
		}
		key := ""
		for _, n := range names {
			key += n + ";"
		}
		shapes[key]++
	}
	if shapes["A;"] != 1 {
		t.Fatalf("shape {A}: got %d entries, want 1", shapes["A;"])
	}
	if shapes["A;B;"] != 1 {
		t.Fatalf("shape {A,B}: got %d entries, want 1", shapes["A;B;"])
	}
}
