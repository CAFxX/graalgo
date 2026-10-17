// Regression tests for the RV-P0 runtime NPE: GoStruct.zero left nested
// STRUCT-kind fields as Java null, so &z.Nested built a cell pointer over
// nothing and the first selector through it NPE'd in SelectorWriteNode
// (s.descriptor() on a null receiver) instead of operating on the zero
// struct. The fix materializes nested struct fields recursively in
// GoStruct.zero; selector sites additionally convert any residual null
// struct reference to Go's nil-dereference panic (never a JVM NPE).
//
// Pre-fix, TestNestedStructCellWrite/Read crashed the backend with a
// NullPointerException; post-fix they pass. TestNilPointerSelectorWrite
// pins the negative direction: a genuine nil pointer still raises Go's
// nil-dereference panic (recoverable), not a JVM crash.
package p0runtimenpe

import "testing"

type Inner struct{ X int }
type Outer struct{ In Inner }
type Deep struct {
	Mid struct {
		In Inner
	}
}

// TestNestedStructCellWrite is the minimal §2 reproducer: a [2]Outer array
// zero-materializes through GoStruct.zero (Boxing.newArray), &a[0].In must
// address a real zero Inner, and the write must round-trip.
func TestNestedStructCellWrite(t *testing.T) {
	var a [2]Outer
	p := &a[0].In
	p.X = 42
	if a[0].In.X != 42 {
		t.Fatalf("a[0].In.X = %d, want 42", a[0].In.X)
	}
	if a[1].In.X != 0 {
		t.Fatalf("a[1].In.X = %d, want 0 (untouched element stays zero)", a[1].In.X)
	}
}

// TestNestedStructCellRead is the read-path variant: SelectorNode's
// readFieldPath NPE'd at the same hole.
func TestNestedStructCellRead(t *testing.T) {
	var a [2]Outer
	a[0].In.X = 7
	q := &a[0].In
	if q.X != 7 {
		t.Fatalf("q.X = %d, want 7", q.X)
	}
}

// TestCompositeLiteralElidedNestedField covers Outer{} with the nested
// field elided: the missing field must still be a usable zero struct.
func TestCompositeLiteralElidedNestedField(t *testing.T) {
	o := Outer{}
	p := &o.In
	p.X = 7
	if o.In.X != 7 {
		t.Fatalf("o.In.X = %d, want 7", o.In.X)
	}
}

// TestDeepNesting pins multi-level materialization through mixed
// zero-materialization paths (var + composite literal).
func TestDeepNesting(t *testing.T) {
	var d Deep
	p := &d.Mid.In
	p.X = 11
	if d.Mid.In.X != 11 {
		t.Fatalf("d.Mid.In.X = %d, want 11", d.Mid.In.X)
	}
	d2 := Deep{}
	d2.Mid.In.X = 13
	if d2.Mid.In.X != 13 {
		t.Fatalf("d2.Mid.In.X = %d, want 13", d2.Mid.In.X)
	}
}

// TestMapMissNestedStructCellWrite covers the map-miss zero path
// (IndexNode -> Boxing.zero -> GoStruct.zero).
func TestMapMissNestedStructCellWrite(t *testing.T) {
	m := map[string]Outer{}
	e := m["missing"]
	p := &e.In
	p.X = 5
	if e.In.X != 5 {
		t.Fatalf("e.In.X = %d, want 5", e.In.X)
	}
}

// TestSliceMakeNestedStructCellWrite covers make([]Outer, n) element
// zeroing followed by a cell write.
func TestSliceMakeNestedStructCellWrite(t *testing.T) {
	s := make([]Outer, 2)
	p := &s[1].In
	p.X = 9
	if s[1].In.X != 9 {
		t.Fatalf("s[1].In.X = %d, want 9", s[1].In.X)
	}
}

// NOTE: a Go-level negative test (recover() from a nil-pointer selector
// write asserting the Go panic message) is not committable: the frontend
// rejects `defer func() { r := recover(); ... }()` with
// "assign.fields: lhs/rhs length mismatch" (independent frontend
// limitation, reproduced on the unmodified base). The negative direction
// (Go nil-dereference panic, never a JVM NPE) is verified instead as a
// main-program differential — see the lane report (repro_nilpanic.go:
// graalgo's stderr matches gc's "nil pointer dereference" panic).

// TestPointerToNilIntDereference pins Hole-A preservation: dereferencing a
// pointer to a nil *int must yield the nil *int without panicking — only
// selector *use* sites panic, not deref itself.
func TestPointerToNilIntDereference(t *testing.T) {
	var x *int
	p := &x
	q := *p
	if q != nil {
		t.Fatalf("q = %v, want nil", q)
	}
}

// TestNestedWriteDoesNotAliasZeroTemplate guards the value semantics:
// writing through one zero-materialized struct must not be observable
// through another zero of the same type.
func TestNestedWriteDoesNotAliasZeroTemplate(t *testing.T) {
	var a, b Outer
	(&a.In).X = 21
	if b.In.X != 0 {
		t.Fatalf("b.In.X = %d, want 0 (independent zero values)", b.In.X)
	}
}
