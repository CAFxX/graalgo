package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestClearElemField (REV-2026-11-03-15 P1-2): a clear() on a slice must
// carry the slice element type id ("elem") on the wire node, so the
// backend can zero-fill with the Go zero value (not Java null) for
// string/struct/complex elements. A clear() on a map carries no "elem".
// A named slice type still resolves to its element type.
func TestClearElemField(t *testing.T) {
	const src = `package main

type Point struct{ X, Y int }
type StrSlice []string

func main() {
	var ss []string
	clear(ss)
	var ps []Point
	clear(ps)
	var ns StrSlice
	clear(ns)
	var m map[string]int
	clear(m)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "clear.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	var clears []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch vv := v.(type) {
		case map[string]any:
			if vv["kind"] == "clear" {
				clears = append(clears, vv)
			}
			for _, child := range vv {
				walk(child)
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(doc)
	if len(clears) != 4 {
		t.Fatalf("found %d clear nodes, want 4", len(clears))
	}
	withElem := 0
	elemCounts := map[float64]int{}
	for _, c := range clears {
		fields, _ := c["fields"].(map[string]any)
		if fields == nil {
			t.Fatalf("clear node has no fields: %v", c)
		}
		if _, ok := fields["x"]; !ok {
			t.Fatalf("clear node has no \"x\": %v", c)
		}
		elem, ok := fields["elem"]
		if !ok {
			continue
		}
		withElem++
		num, ok := elem.(float64)
		if !ok {
			t.Fatalf("clear \"elem\" is not a type id number: %T %v", elem, elem)
		}
		elemCounts[num]++
	}
	// Three slice clears carry "elem"; the map clear carries none.
	if withElem != 3 {
		t.Fatalf("%d clear nodes carry \"elem\", want 3 (the map clear must not)", withElem)
	}
	// The two string-ish clears (plain []string and named StrSlice)
	// share one element type id; the struct clear has a different one.
	if len(elemCounts) != 2 {
		t.Fatalf("distinct \"elem\" values = %v, want 2 (string, struct)", elemCounts)
	}
	for id, n := range elemCounts {
		if n != 1 && n != 2 {
			t.Fatalf("elem id %v appears %d times, want 1 or 2", id, n)
		}
	}
}
