package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestVarGenDeclMultiSpecSplit: a `var (...)` block with multiple ValueSpecs
// must emit one vardecl node per spec, not a single merged node.
//
// Before the fix, exportVarGenDecl accumulated names/objs/values across all
// specs in the GenDecl and emitted one vardecl node. For:
//
//	var (
//		hash       = fnv1(0)
//		size       uintptr  // no initializer
//		comparable = true
//	)
//
// it emitted names=[hash,size,comparable] (3) with values=[fnv1-call, true]
// (2) — a values/objs length mismatch that the v1 backend's buildVarDecl
// rejects ("values/objs length mismatch"). This pattern appears in
// reflect/type.go (StructOf).
//
// After the fix, each ValueSpec gets its own vardecl node.
func TestVarGenDeclMultiSpecSplit(t *testing.T) {
	src := `package p

func main() {
	var (
		hash       = 42
		size       uintptr
		comparable = true
	)
	_, _, _ = hash, size, comparable
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(path)
	if err != nil {
		t.Fatalf("ExportFile failed: %v", err)
	}
	var doc struct {
		Funcs []struct {
			Kind   string `json:"kind"`
			Fields struct {
				Body json.RawMessage `json:"body"`
			} `json:"fields"`
		} `json:"funcs"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Count vardecl nodes and check for mismatches by scanning raw JSON.
	vardeclCount := 0
	mismatches := 0
	var scan func(v any)
	scan = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["kind"] == "vardecl" {
				vardeclCount++
				f := n["fields"].(map[string]any)
				objs := f["objs"].([]any)
				values := f["values"].([]any)
				nv, no := len(values), len(objs)
				if nv > 0 && nv != no && !(nv == 1 && no == 2) {
					mismatches++
					t.Errorf("vardecl mismatch: %d objs, %d values", no, nv)
				}
			}
			for _, v := range n {
				scan(v)
			}
		case []any:
			for _, v := range n {
				scan(v)
			}
		}
	}
	var raw any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	scan(raw)
	if mismatches > 0 {
		t.Fatalf("found %d vardecl mismatches", mismatches)
	}
	// The three specs must produce three separate vardecl nodes (one per
	// spec), not one merged node.
	if vardeclCount < 3 {
		t.Errorf("expected at least 3 vardecl nodes (one per spec), got %d", vardeclCount)
	}
}

// TestVarGenDeclSingleSpecUnchanged: a single-spec `var x = 1` still emits
// exactly one vardecl node (not wrapped in a block).
func TestVarGenDeclSingleSpecUnchanged(t *testing.T) {
	src := `package p

func main() {
	var x = 42
	_ = x
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(path)
	if err != nil {
		t.Fatalf("ExportFile failed: %v", err)
	}
	var raw any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	// Find the vardecl for x; verify it has 1 obj and 1 value.
	found := false
	var scan func(v any)
	scan = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["kind"] == "vardecl" {
				f := n["fields"].(map[string]any)
				names := f["names"].([]any)
				for _, nm := range names {
					if nm == "x" {
						found = true
						objs := f["objs"].([]any)
						values := f["values"].([]any)
						if len(objs) != 1 || len(values) != 1 {
							t.Errorf("single-spec vardecl: got %d objs, %d values; want 1, 1",
								len(objs), len(values))
						}
					}
				}
			}
			for _, v := range n {
				scan(v)
			}
		case []any:
			for _, v := range n {
				scan(v)
			}
		}
	}
	scan(raw)
	if !found {
		t.Error("did not find vardecl for x")
	}
}

// TestVarGenDeclNoInitSpecs: specs without initializers (`var b int`) emit
// vardecl nodes with empty values (zero-value initialization), which the
// backend handles.
func TestVarGenDeclNoInitSpecs(t *testing.T) {
	src := `package p

func main() {
	var (
		a int
		b string
	)
	_, _ = a, b
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(path)
	if err != nil {
		t.Fatalf("ExportFile failed: %v", err)
	}
	var raw any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	emptyValues := 0
	var scan func(v any)
	scan = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["kind"] == "vardecl" {
				f := n["fields"].(map[string]any)
				values := f["values"].([]any)
				if len(values) == 0 {
					emptyValues++
				}
			}
			for _, v := range n {
				scan(v)
			}
		case []any:
			for _, v := range n {
				scan(v)
			}
		}
	}
	scan(raw)
	if emptyValues < 2 {
		t.Errorf("expected at least 2 vardecl nodes with empty values, got %d", emptyValues)
	}
}
