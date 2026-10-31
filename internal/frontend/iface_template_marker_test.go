package main

// P1-B reachability pin (2026-10-31): the "templateMethodsSkipped"
// marker survives in final exports ONLY on template (unbound) entries.
// Concrete instantiations always monomorphize, so a runtime value —
// which always has a concrete type — can never trip the backend's
// sentinel checks (boxing, type assertion, type switch). This test
// exports a program that boxes *sync/atomic.Pointer[int] into an
// interface and asserts:
//   - at least one entry IS marked (the test exercises the marker path,
//     it is not vacuous);
//   - every marked entry is the *sync/atomic.Pointer[T] template pointer
//     (never a concrete instantiation);
//   - every *sync/atomic.Pointer[int] pointer entry is unmarked and
//     carries the full method table (CompareAndSwap, Load, Store, Swap).
// A future change that marks a concrete entry re-elevates P1-B from a
// P2 label issue to a real correctness gap, and this test fails loudly.

import (
	"testing"
)

func TestIfaceTemplateMarkerOnlyOnTemplates(t *testing.T) {
	const src = `package main

import "sync/atomic"

type Loader interface{ Load() *int }

func main() {
	var p atomic.Pointer[int]
	v := 7
	p.Store(&v)
	var l Loader = &p
	println(*l.Load())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	entryName := func(id int) string {
		em, _ := entries[id].(map[string]any)
		if em == nil {
			return ""
		}
		d, _ := em["detail"].(map[string]any)
		if d == nil {
			return ""
		}
		name, _ := d["name"].(string)
		return name
	}
	methodNames := func(d map[string]any) []string {
		methods, _ := d["methods"].([]any)
		var names []string
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			if mm == nil {
				continue
			}
			n, _ := mm["name"].(string)
			names = append(names, n)
		}
		return names
	}
	marked := 0
	concretePtr := 0
	for i, e := range entries {
		em, _ := e.(map[string]any)
		if em == nil {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		if d == nil {
			continue
		}
		_, isMarked := d["templateMethodsSkipped"]
		if em["kind"] != "pointer" {
			if isMarked {
				t.Errorf("marked non-pointer entry id=%d kind=%v name=%q: marker must appear only on template pointers", i, em["kind"], d["name"])
			}
			continue
		}
		elem, _ := d["elem"].(float64)
		elemName := entryName(int(elem))
		if isMarked {
			marked++
			if elemName != "sync/atomic.Pointer[T]" {
				t.Errorf("marked pointer entry id=%d has elem %q, want the sync/atomic.Pointer[T] template", i, elemName)
			}
			if len(methodNames(d)) != 0 {
				t.Errorf("marked pointer entry id=%d carries %d methods, want 0 (template methods skipped)", i, len(methodNames(d)))
			}
			continue
		}
		if elemName == "sync/atomic.Pointer[int]" {
			concretePtr++
			want := []string{"CompareAndSwap", "Load", "Store", "Swap"}
			got := methodNames(d)
			if len(got) != len(want) {
				t.Errorf("concrete *sync/atomic.Pointer[int] entry id=%d methods=%v, want %v", i, got, want)
				continue
			}
			for j := range want {
				if got[j] != want[j] {
					t.Errorf("concrete *sync/atomic.Pointer[int] entry id=%d methods=%v, want %v", i, got, want)
					break
				}
			}
		}
	}
	if marked == 0 {
		t.Error("no marked entry in the export: the test does not exercise the marker path")
	}
	if concretePtr == 0 {
		t.Error("no *sync/atomic.Pointer[int] pointer entry in the export: the concrete control is missing")
	}
	// The named concrete entry itself must be unmarked (its value method
	// set is empty — all Pointer methods have pointer receivers — but it
	// must not carry the marker).
	for i, e := range entries {
		em, _ := e.(map[string]any)
		if em == nil || em["kind"] != "named" {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		if d == nil || d["name"] != "sync/atomic.Pointer[int]" {
			continue
		}
		if _, has := d["templateMethodsSkipped"]; has {
			t.Errorf("named sync/atomic.Pointer[int] entry id=%d is marked, want unmarked", i)
		}
	}
}
