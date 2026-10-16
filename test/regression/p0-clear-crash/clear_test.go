// Regression test for the clear(m) P0: statement-position clear(m) on a
// map crashed the backend with UnsupportedOperationException because the
// interop ClearNode declared kind INT64 (placeholder) but only implemented
// executeObject(), while ExprStmtNode dispatches on kind().slotKind()
// (INT64 -> executeLong).
//
// Also covers the latent reflectClear bug fixed in the same change:
// ReflectOpNode.reflectClear() cast mapIterNext() results to Object[] as if
// they were key-value pairs, but mapIterInit/mapIterNext return KEYS
// (GoMap.keysSnapshot()). It now calls GoMap.clear() directly.
//
// And the synthetic reflect.Value.Clear() divergence fixed alongside:
// the synthetic wrapper called __mustBeAssignable, panicking on
// unaddressable values (e.g. reflect.ValueOf(m)) where gc 1.27.1 clears
// fine (src/reflect/value.go switches on Kind only).
package p0clearcrash

import (
	"reflect"
	"testing"
)

// TestClearMapStatement is the P0 reproducer: statement-position clear(m)
// on a non-empty map. Pre-fix this crashed with
// UnsupportedOperationException("ClearNode (kind INT64) has no long value").
func TestClearMapStatement(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	clear(m) // statement position: must not crash
	if len(m) != 0 {
		t.Fatalf("clear(m): len = %d, want 0", len(m))
	}
	if _, ok := m["a"]; ok {
		t.Fatalf("clear(m): key %q still present after clear", "a")
	}
}

// TestClearSliceStatement covers clear(s) on a slice: length/capacity are
// preserved, elements are zeroed.
func TestClearSliceStatement(t *testing.T) {
	s := []int{1, 2, 3, 4}
	clear(s)
	if len(s) != 4 || cap(s) != 4 {
		t.Fatalf("clear(s): len/cap = %d/%d, want 4/4", len(s), cap(s))
	}
	for i, v := range s {
		if v != 0 {
			t.Fatalf("clear(s): s[%d] = %d, want 0", i, v)
		}
	}
}

// TestClearNilNoop: clear on nil map/slice is a no-op and must not panic.
func TestClearNilNoop(t *testing.T) {
	var nm map[string]int
	clear(nm)
	var ns []int
	clear(ns)
}

// TestReflectValueClearMap exercises the fixed reflectClear path via
// reflect.Value.Clear on a map obtained from ValueOf (unaddressable).
// Pre-fix this panicked two ways: first the synthetic wrapper demanded
// assignability (divergence from gc 1.27.1), then reflectClear cast keys
// to Object[] and failed.
func TestReflectValueClearMap(t *testing.T) {
	m := map[string]int{"x": 9, "y": 8}
	reflect.ValueOf(m).Clear()
	if len(m) != 0 {
		t.Fatalf("reflect.ValueOf(m).Clear(): len = %d, want 0", len(m))
	}
}

// TestReflectValueClearSlice: reflect.Value.Clear zeroes slice elements.
func TestReflectValueClearSlice(t *testing.T) {
	s := []int{7, 8, 9}
	reflect.ValueOf(s).Clear()
	for i, v := range s {
		if v != 0 {
			t.Fatalf("reflect.ValueOf(s).Clear(): s[%d] = %d, want 0", i, v)
		}
	}
}
