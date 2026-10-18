// Regression tests for the unsafeptradd wire node and nested-struct zero
// initialization (fix-redmaster-exitcode-drift).
//
// RC-2: unsafe.Pointer(uintptr(p) + k*unsafe.Sizeof(T)) — the standard Go
// idiom for &p[k] — must compute the correct element address via the
// unsafeptradd wire node, not trap (D5).
//
// RC-1: make([]T,n) must recursively zero-initialize STRUCT-kind fields
// (including embedded structs); pre-fix they stayed Java null, causing a
// Go nil-dereference panic on promoted-field access.
package ptradd

import (
	"testing"
	"unsafe"
)

// TestPtrAddPositive is the RC-2 reproducer: the canonical idiom must yield
// the correct element value. Pre-fix this trapped with D5
// (UnsafeEscapeException: uintptr -> unsafe.Pointer conversion traps).
func TestPtrAddPositive(t *testing.T) {
	arr := [5]int64{10, 20, 30, 40, 50}
	p := unsafe.Pointer(&arr[0])
	q := unsafe.Pointer(uintptr(p) + 2*unsafe.Sizeof(int64(0)))
	v := *(*int64)(q)
	if v != 30 {
		t.Fatalf("*(p+2) = %d, want 30", v)
	}
}

// TestPtrAddSub covers the SUB form: uintptr(p) - k*Sizeof.
func TestPtrAddSub(t *testing.T) {
	arr := [5]int64{10, 20, 30, 40, 50}
	p := unsafe.Pointer(&arr[4])
	q := unsafe.Pointer(uintptr(p) - 2*unsafe.Sizeof(int64(0)))
	v := *(*int64)(q)
	if v != 30 {
		t.Fatalf("*(p-2) = %d, want 30", v)
	}
}

// TestPtrAddBareSizeof covers the bare-Sizeof form (k=1).
func TestPtrAddBareSizeof(t *testing.T) {
	arr := [5]int64{10, 20, 30, 40, 50}
	p := unsafe.Pointer(&arr[0])
	q := unsafe.Pointer(uintptr(p) + unsafe.Sizeof(int64(0)))
	v := *(*int64)(q)
	if v != 20 {
		t.Fatalf("*(p+1) = %d, want 20", v)
	}
}

// TestPtrAddZeroIndex covers the identity form (k=0).
func TestPtrAddZeroIndex(t *testing.T) {
	arr := [5]int64{10, 20, 30, 40, 50}
	p := unsafe.Pointer(&arr[2])
	q := unsafe.Pointer(uintptr(p) + 0*unsafe.Sizeof(int64(0)))
	v := *(*int64)(q)
	if v != 30 {
		t.Fatalf("*(p+0) = %d, want 30", v)
	}
}

type ptraddInner struct {
	x int
}

type ptraddOuter struct {
	ptraddInner // embedded: pre-fix, make([]ptraddOuter,n)[i].ptraddInner was null
	y           int
}

// TestNestedStructZero is the RC-1 reproducer: make([]T,n) with an embedded
// struct field must not panic on promoted-field access. Pre-fix this panicked
// with a Go nil-dereference (deref of null walking the promotion path).
func TestNestedStructZero(t *testing.T) {
	s := make([]ptraddOuter, 2)
	// Read the promoted field: must be zero, not a panic.
	if s[0].x != 0 {
		t.Fatalf("s[0].x = %d, want 0", s[0].x)
	}
	// Write through the promoted field: must stick.
	s[0].x = 42
	if s[0].x != 42 {
		t.Fatalf("s[0].x = %d, want 42", s[0].x)
	}
	// The second element must be independent.
	if s[1].x != 0 {
		t.Fatalf("s[1].x = %d, want 0", s[1].x)
	}
}
