// Regression tests for R2315-13-P1-1: package-level sync/atomic pointer
// ops with an UNTYPED nil value argument must store/compare the nil
// word (0), not die with an internal error.
//
// Mechanism: the frontend types untyped nil as the empty interface, so
// the wire value arg is an INTERFACE-kind NilNode, which has no long
// value; the atomicptrop node read it with a raw executeLong. gc
// stores/compares the nil word. A typed nil already worked; only the
// untyped-nil literal crashed, which is why the earlier tests missed it.
//
// Each test below FAILS on the rebased 8a7a4dc9 tip without the
// handleArg helper (UnsupportedOperationException) and PASSES with it.
// The StorePointer case is the regression pin: it WORKED on master via
// the plain-assign path and the approved 8a7a4dc9 fix would have broken
// it by routing the store through the same raw executeLong.
package atomicptr

import (
	"sync/atomic"
	"testing"
	"unsafe"
)

// TestStorePointerNilValue: StorePointer(&p, nil) then LoadPointer
// returns nil (regression pin for the 8a7a4dc9 store path).
func TestStorePointerNilValue(t *testing.T) {
	var p unsafe.Pointer
	atomic.StorePointer(&p, nil)
	if got := atomic.LoadPointer(&p); got != nil {
		t.Fatalf("LoadPointer after StorePointer(nil) = %v, want nil", got)
	}
}

// TestSwapPointerNilValue: SwapPointer(&p, nil) returns the old pointer
// and stores nil.
func TestSwapPointerNilValue(t *testing.T) {
	x := 1
	var p unsafe.Pointer = unsafe.Pointer(&x)
	old := atomic.SwapPointer(&p, nil)
	if old != unsafe.Pointer(&x) {
		t.Fatalf("SwapPointer(nil) old = %v, want %v", old, unsafe.Pointer(&x))
	}
	if got := atomic.LoadPointer(&p); got != nil {
		t.Fatalf("LoadPointer after SwapPointer(nil) = %v, want nil", got)
	}
}

// TestCompareAndSwapPointerNilOld: CompareAndSwapPointer(&p, nil, x)
// succeeds when the word is nil.
func TestCompareAndSwapPointerNilOld(t *testing.T) {
	var p unsafe.Pointer
	x := 2
	if !atomic.CompareAndSwapPointer(&p, nil, unsafe.Pointer(&x)) {
		t.Fatal("CompareAndSwapPointer(nil, x) from nil word = false, want true")
	}
	if got := atomic.LoadPointer(&p); got != unsafe.Pointer(&x) {
		t.Fatalf("LoadPointer after CAS(nil, x) = %v, want %v", got, unsafe.Pointer(&x))
	}
}

// TestCompareAndSwapPointerNilNew: CompareAndSwapPointer(&p, x, nil)
// stores nil when the old value matches.
func TestCompareAndSwapPointerNilNew(t *testing.T) {
	x := 3
	var p unsafe.Pointer = unsafe.Pointer(&x)
	if !atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&x), nil) {
		t.Fatal("CompareAndSwapPointer(x, nil) on match = false, want true")
	}
	if got := atomic.LoadPointer(&p); got != nil {
		t.Fatalf("LoadPointer after CAS(x, nil) = %v, want nil", got)
	}
}
