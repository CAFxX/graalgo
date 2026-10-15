package main

// PKG-024 regression: a comma-ok index (v, ok := m[k]) must be accepted for
// ANY operand whose underlying/core type is a map — not just for operands
// whose go/types Underlying() is literally *types.Map. In particular, for a
// type parameter constrained by ~map[K]V (e.g. maps.Equal's M2), Underlying()
// is the constraint interface (~map[K]V), not a map, and the old check
// rejected valid code with "gofrontend: comma-ok index on non-map type M2".
//
// The fix takes the value type from the two-valued tuple go/types records
// on the index expression itself, which is authoritative: the program has
// already type-checked, so a comma-ok index here is necessarily map-like.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportSrc writes src to a temp file and runs the frontend exporter on it.
func exportSrc(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "prog.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	return out
}

// commaOkIndexTypeIDs returns the "type" field ids of every index node with
// commaOk=true in the wire document, plus the decoded types table.
func commaOkIndexTypeIDs(t *testing.T, doc string) ([]int, []map[string]any) {
	t.Helper()
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	typesRaw, _ := raw["types"].([]any)
	types := make([]map[string]any, len(typesRaw))
	for i, tr := range typesRaw {
		m, _ := tr.(map[string]any)
		types[i] = m
	}
	var ids []int
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "index" {
				if f, ok := v["fields"].(map[string]any); ok {
					if cok, _ := f["commaOk"].(bool); cok {
						if tid, ok := f["type"].(float64); ok {
							ids = append(ids, int(tid))
						} else {
							t.Fatalf("comma-ok index node has no numeric type field: %v", f)
						}
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	return ids, types
}

func typeKindName(types []map[string]any, id int) (kind, name string) {
	if id < 0 || id >= len(types) {
		return "", ""
	}
	e := types[id]
	kind, _ = e["kind"].(string)
	if det, ok := e["detail"].(map[string]any); ok {
		name, _ = det["name"].(string)
	}
	return kind, name
}

// TestCommaOkDefinedMapType: comma-ok on a defined type with a map
// underlying type (the non-generic baseline).
func TestCommaOkDefinedMapType(t *testing.T) {
	out := exportSrc(t, `package main

type M2 map[int]bool

func main() {
	m := M2{1: true}
	v, ok := m[1]
	_, _ = v, ok
}
`)
	ids, types := commaOkIndexTypeIDs(t, out)
	if len(ids) != 1 {
		t.Fatalf("want 1 comma-ok index node, got %d", len(ids))
	}
	if kind, name := typeKindName(types, ids[0]); kind != "basic" || name != "bool" {
		t.Fatalf("elem type = %s %q, want basic bool", kind, name)
	}
}

// TestCommaOkGenericDefinedMapType: comma-ok on an instantiated generic
// defined map type.
func TestCommaOkGenericDefinedMapType(t *testing.T) {
	out := exportSrc(t, `package main

type GM[K comparable, V any] map[K]V

func get[K comparable, V any](m GM[K, V], k K) (V, bool) {
	v, ok := m[k]
	return v, ok
}

func main() {
	m := make(GM[string, int])
	m["a"] = 1
	v, ok := get(m, "a")
	_, _ = v, ok
}
`)
	ids, types := commaOkIndexTypeIDs(t, out)
	if len(ids) == 0 {
		t.Fatal("no comma-ok index node emitted")
	}
	for _, id := range ids {
		kind, name := typeKindName(types, id)
		// Inside get: the instantiated GM[K]V body exports with K,V bound
		// (typeparam entries); monomorphized instantiations substitute
		// concrete types (basic int). Both are correct value types.
		ok := (kind == "typeparam" && name == "V") || (kind == "basic" && name == "int")
		if !ok {
			t.Fatalf("elem type = %s %q, want typeparam V or basic int", kind, name)
		}
	}
}

// TestCommaOkTypeParamMap: comma-ok on a type parameter constrained by
// ~map[K]V — the exact shape that broke `graalgo test maps`
// (maps.Equal does v2, ok := m2[k] with m2 M2, M2 ~map[K]V).
// Pre-fix this failed with "gofrontend: comma-ok index on non-map type M2".
func TestCommaOkTypeParamMap(t *testing.T) {
	out := exportSrc(t, `package main

func Equal[M2 ~map[K]V, K, V comparable](m2 M2, k K) (V, bool) {
	v, ok := m2[k]
	return v, ok
}

func main() {
	m := map[string]int{"a": 1}
	v, ok := Equal(m, "a")
	_, _ = v, ok
}
`)
	ids, types := commaOkIndexTypeIDs(t, out)
	if len(ids) == 0 {
		t.Fatal("no comma-ok index node emitted")
	}
	for _, id := range ids {
		kind, name := typeKindName(types, id)
		// Generic body: typeparam V; monomorphized instantiation at the
		// call site: basic int. Both are correct value types.
		ok := (kind == "typeparam" && name == "V") || (kind == "basic" && name == "int")
		if !ok {
			t.Fatalf("elem type = %s %q, want typeparam V or basic int", kind, name)
		}
	}
}

// TestSingleValueIndexDefinedMapType: the single-value index path must keep
// working on defined map types (it never had the Underlying check, but it
// shares exportIndex — guard against regressions).
func TestSingleValueIndexDefinedMapType(t *testing.T) {
	out := exportSrc(t, `package main

type M2 map[int]bool

func main() {
	m := M2{1: true}
	v := m[1]
	_ = v
}
`)
	if strings.Contains(out, `"commaOk": true`) {
		t.Fatal("single-value index must not be marked commaOk")
	}
	if !strings.Contains(out, `"kind": "index"`) {
		t.Fatal("no index node emitted")
	}
}

// TestSingleValueIndexTypeParamMap: single-value index on a type parameter
// constrained by ~map[K]V.
func TestSingleValueIndexTypeParamMap(t *testing.T) {
	exportSrc(t, `package main

func get[M2 ~map[K]V, K, V comparable](m2 M2, k K) V {
	return m2[k]
}

func main() {
	m := map[string]int{"a": 1}
	v := get(m, "a")
	_ = v
}
`)
}
