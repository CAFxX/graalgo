// Regression test for REVIEW-2026-11-03-15 P1-2: clear() on a slice of
// strings, structs, or complex numbers filled the backing array with
// Java null instead of the Go zero value, so the next read gave a host
// NullPointerException (struct field read, string comparison) or nil
// instead of 0+0i (complex). The fix carries the slice element type on
// the "clear" wire node and zero-fills per element kind. Map clear and
// primitive-slice clear were already correct and are covered as controls.
package p1clearzero

import "testing"

type Inner struct{ Y int }
type Outer struct {
	X  int
	In Inner
	S  string
	C  complex64
}
type NamedOuter struct{ X int }
type StrSlice []string

func TestClearStringSlice(t *testing.T) {
	s := []string{"a", "b", "c"}
	clear(s)
	for i, v := range s {
		if v != "" {
			t.Fatalf("clear([]string): s[%d] = %q, want \"\"", i, v)
		}
	}
	if len(s) != 3 {
		t.Fatalf("clear([]string): len = %d, want 3", len(s))
	}
}

func TestClearNamedStringSlice(t *testing.T) {
	s := StrSlice{"a", "b"}
	clear(s)
	for i, v := range s {
		if v != "" {
			t.Fatalf("clear(StrSlice): s[%d] = %q, want \"\"", i, v)
		}
	}
}

func TestClearStringSubSlice(t *testing.T) {
	s := []string{"a", "b", "c", "d"}
	clear(s[1:3])
	want := []string{"a", "", "", "d"}
	for i, v := range want {
		if s[i] != v {
			t.Fatalf("clear(s[1:3]): s[%d] = %q, want %q", i, s[i], v)
		}
	}
}

func TestClearStructSlice(t *testing.T) {
	s := []Outer{
		{X: 5, In: Inner{Y: 6}, S: "x", C: 1 + 2i},
		{X: 7, In: Inner{Y: 8}, S: "y", C: 3 + 4i},
	}
	clear(s)
	// A nested-struct field read after clear must yield the zero value,
	// not crash: pre-fix the slot was Java null.
	if s[0].X != 0 || s[0].In.Y != 0 || s[0].S != "" || s[0].C != 0 {
		t.Fatalf("clear([]Outer): s[0] = %+v, want zero", s[0])
	}
	if s[1].X != 0 || s[1].In.Y != 0 {
		t.Fatalf("clear([]Outer): s[1] = %+v, want zero", s[1])
	}
}

func TestClearStructSlotsNotAliased(t *testing.T) {
	s := make([]NamedOuter, 2)
	s[0].X, s[1].X = 1, 2
	clear(s)
	s[0].X = 42
	if s[1].X != 0 {
		t.Fatalf("clear([]NamedOuter): writing s[0].X leaked into s[1].X = %d (shared zero instance)", s[1].X)
	}
}

func TestClearComplexSlices(t *testing.T) {
	c64 := []complex64{1 + 2i, 3 + 4i}
	clear(c64)
	for i, v := range c64 {
		if v != 0 {
			t.Fatalf("clear([]complex64): c64[%d] = %v, want 0", i, v)
		}
	}
	c128 := []complex128{1 + 2i, 3 + 4i}
	clear(c128)
	for i, v := range c128 {
		if v != 0 {
			t.Fatalf("clear([]complex128): c128[%d] = %v, want 0", i, v)
		}
	}
}

func TestClearControls(t *testing.T) {
	is := []int{1, 2, 3}
	clear(is)
	for i, v := range is {
		if v != 0 {
			t.Fatalf("clear([]int): is[%d] = %d, want 0", i, v)
		}
	}
	an := []any{1, "x", 2.5}
	clear(an)
	for i, v := range an {
		if v != nil {
			t.Fatalf("clear([]any): an[%d] = %v, want nil", i, v)
		}
	}
	x, y := 1, 2
	ps := []*int{&x, &y}
	clear(ps)
	for i, v := range ps {
		if v != nil {
			t.Fatalf("clear([]*int): ps[%d] = %v, want nil", i, v)
		}
	}
	m := map[string]int{"a": 1}
	clear(m)
	if len(m) != 0 {
		t.Fatalf("clear(map): len = %d, want 0", len(m))
	}
}

func TestClearNilNoop(t *testing.T) {
	var ss []string
	clear(ss)
	var os []Outer
	clear(os)
	var cs []complex128
	clear(cs)
	var m map[string]int
	clear(m)
}
