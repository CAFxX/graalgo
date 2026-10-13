package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Reviewer-2 P1-4: go/types models every builtin's type as Typ[Invalid]
// (NewBuiltin sets typ: Typ[Invalid] — builtins have no signature). The
// exporter must never serialize types.Invalid: exportIdent used to call
// typeID(obj.Type()) for *types.Builtin, minting a bogus "invalid type"
// wire entry that the Java side laundered into a fake INTERFACE kind.
// Regression: a program calling builtins must export no "invalid type"
// entry, and builtin ident nodes must not carry a "type" field.
func TestBuiltinIdentNeverExportsInvalidType(t *testing.T) {
	src := `package main

func main() {
	xs := []int{3, 1, 2}
	n := len(xs)
	m := cap(xs)
	if n != 3 || m != 3 {
		panic("bad len/cap")
	}
	ys := make([]int, n)
	copy(ys, xs)
	println(n, len(ys))
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "builtin.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(path)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	types, ok := doc["types"].([]any)
	if !ok {
		t.Fatal("export has no types array")
	}
	for i, v := range types {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if m["kind"] == "basic" && d["name"] == "invalid type" {
			t.Errorf("types[%d] is a serialized types.Invalid (builtin ident leak)", i)
		}
	}
	// Every builtin ident node must lack the "type" field (builtins have
	// no go/types signature; the factory dispatches via the call node's
	// "builtin" marker).
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "ident" {
				if f, ok := v["fields"].(map[string]any); ok {
					if _, isBuiltin := f["builtin"]; isBuiltin {
						if _, hasType := f["type"]; hasType {
							t.Errorf("builtin ident %q carries a type field", f["name"])
						}
					}
				}
			}
			for _, e := range v {
				walk(e)
			}
		case []any:
			for _, e := range v {
				walk(e)
			}
		}
	}
	walk(doc)
}
