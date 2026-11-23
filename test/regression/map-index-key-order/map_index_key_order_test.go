// Regression test for G-P1-5 (REVIEW-2026-11-23-06): IndexNode's MAP
// branch evaluated the container before the key, while gc evaluates the
// key first. A key expression that reassigns (or otherwise mutates) the
// map variable must therefore see the lookup run against the NEW map.
package mapindexkeyorder

import "testing"

// TestMapIndexKeyEvaluatedBeforeContainer is the G-P1-5 reproducer: k()
// reassigns m before returning "a". gc evaluates k() first, so the
// lookup runs against the new map (which lacks "a") and yields 0.
// Pre-fix graalgo evaluated the container first and returned 1.
func TestMapIndexKeyEvaluatedBeforeContainer(t *testing.T) {
	m := map[string]int{"a": 1}
	k := func() string { m = map[string]int{"b": 2}; return "a" }
	if got := m[k()]; got != 0 {
		t.Fatalf("m[k()]: got %d, want 0 (key must be evaluated before the container)", got)
	}
}

// TestMapIndexNilMapKeyStillEvaluated: a nil map read yields the zero
// value, but the key expression must still be evaluated — gc evaluates
// the key first regardless of the map value. Pre-fix the nil-map
// short-circuit returned without ever evaluating the key.
func TestMapIndexNilMapKeyStillEvaluated(t *testing.T) {
	var m map[string]int
	called := false
	k := func() string { called = true; return "a" }
	if got := m[k()]; got != 0 {
		t.Fatalf("m[k()] on nil map: got %d, want 0", got)
	}
	if !called {
		t.Fatal("m[k()] on nil map: key expression was not evaluated")
	}
}

// TestMapIndexNormalLookup is the control: ordinary present/absent
// lookups are unaffected by the reorder.
func TestMapIndexNormalLookup(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	if got := m["a"]; got != 1 {
		t.Fatalf("m[%q]: got %d, want 1", "a", got)
	}
	if got := m["zz"]; got != 0 {
		t.Fatalf("m[%q]: got %d, want 0", "zz", got)
	}
}

// TestMapIndexWriteVarContainer is the control for the write path:
// m[k()] = v with k() reassigning m must land in the NEW map (gc).
// The general assignment path re-reads a simple-variable container at
// store time (bug-030 rule), so this already passed pre-fix; it guards
// that rule for maps.
func TestMapIndexWriteVarContainer(t *testing.T) {
	m := map[string]int{"a": 1}
	old := m
	k := func() string { m = map[string]int{"b": 2}; return "a" }
	m[k()] = 9
	if old["a"] != 1 || m["a"] != 9 {
		t.Fatalf("m[k()] = 9: old[a]=%d new[a]=%d, want 1/9", old["a"], m["a"])
	}
}
