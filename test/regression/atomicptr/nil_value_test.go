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
	"fmt"
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

// --- R2315-14-P1-3: SwapPointer / CompareAndSwapPointer with a NIL
// ADDRESS must panic with gc's nil-dereference panic, never silently
// succeed. ---
//
// Mechanism: load/store already panic (storage() on a Java-null address,
// requireNonNil on a nil GoPointer), but swap/CAS had no nil-address
// check. The hole is reachable only through the unsafe round-trip
// (*unsafe.Pointer)(unsafe.Pointer(nil)): that conversion yields a nil
// GoPointer (a non-null Java object with a null referent), so
// storage() returns it; readPlain then returns 0 for the nil pointer
// and setReferent writes into the detached holder object — a silent
// no-op that returns 0 / true as if the operation had succeeded. A
// plain nil *unsafe.Pointer variable or a nil literal as the address
// already panicked pre-fix (the address node yields Java null, so
// storage() panics); those forms are pinned here as non-triggers that
// must keep panicking. gc panics for every form (verified with gc
// 1.27.1); the recovered value is "runtime error: invalid memory
// address or nil pointer dereference" in all cases.
//
// Each round-trip test below FAILS on 9d72e541 (no panic: the op
// silently returns) and PASSES with the requireNonNil fix in
// AtomicPtrOpNode's swap/CAS paths.

const nilDerefPanic = "runtime error: invalid memory address or nil pointer dereference"

// expectNilDerefPanic runs f and fails unless it panics with gc's
// nil-dereference panic. The recovered value is compared with
// fmt.Sprint, exactly as the E2E probes print it: the recovered
// value's concrete type differs across runtimes (under graalgo it is
// neither a string nor an error value a type switch would catch —
// a hand-rolled string/error switch formats it as ""), but its
// fmt.Sprint rendering is the gc message on both.
func expectNilDerefPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("%s: no panic, want nil-dereference panic", name)
		}
		if got := fmt.Sprint(r); got != nilDerefPanic {
			t.Fatalf("%s: recovered %q, want %q", name, got, nilDerefPanic)
		}
	}()
	f()
}

// nilAddrRoundTrip is the R2315-14-P1-3 trigger: a nil *unsafe.Pointer
// produced by an unsafe round-trip, not by a nil literal or a nil
// variable (both of which already panic via storage()).
func nilAddrRoundTrip() *unsafe.Pointer {
	return (*unsafe.Pointer)(unsafe.Pointer(nil))
}

// TestSwapPointerNilAddressPanics: SwapPointer on a nil address panics.
func TestSwapPointerNilAddressPanics(t *testing.T) {
	x := 1
	expectNilDerefPanic(t, "SwapPointer(round-trip nil addr)", func() {
		atomic.SwapPointer(nilAddrRoundTrip(), unsafe.Pointer(&x))
	})
	expectNilDerefPanic(t, "SwapPointer(nil *unsafe.Pointer var)", func() {
		var addr *unsafe.Pointer
		atomic.SwapPointer(addr, unsafe.Pointer(&x))
	})
	expectNilDerefPanic(t, "SwapPointer(nil literal addr)", func() {
		atomic.SwapPointer(nil, unsafe.Pointer(&x))
	})
}

// TestCompareAndSwapPointerNilAddressPanics: CompareAndSwapPointer on a
// nil address panics.
func TestCompareAndSwapPointerNilAddressPanics(t *testing.T) {
	x := 2
	expectNilDerefPanic(t, "CAS(round-trip nil addr)", func() {
		atomic.CompareAndSwapPointer(nilAddrRoundTrip(), nil, unsafe.Pointer(&x))
	})
	expectNilDerefPanic(t, "CAS(nil *unsafe.Pointer var)", func() {
		var addr *unsafe.Pointer
		atomic.CompareAndSwapPointer(addr, nil, unsafe.Pointer(&x))
	})
	expectNilDerefPanic(t, "CAS(nil literal addr)", func() {
		atomic.CompareAndSwapPointer(nil, nil, unsafe.Pointer(&x))
	})
}

// TestSwapPointerNilAddressEvaluatesValueFirst: gc evaluates the value
// argument before the nil-address panic fires (verified with gc
// 1.27.1: the side effect runs, then the panic). The requireNonNil
// check therefore sits AFTER the value-argument evaluation in the
// swap/CAS paths, exactly as in the store path — a check placed before
// it would skip the side effect (the R2315-14-P2-4 shape).
func TestSwapPointerNilAddressEvaluatesValueFirst(t *testing.T) {
	evaluated := false
	valFn := func() unsafe.Pointer {
		evaluated = true
		x := 3
		return unsafe.Pointer(&x)
	}
	expectNilDerefPanic(t, "SwapPointer(round-trip nil addr, side-effecting value)", func() {
		atomic.SwapPointer(nilAddrRoundTrip(), valFn())
	})
	if !evaluated {
		t.Fatal("SwapPointer(nil addr): value arg not evaluated before panic")
	}
}

// TestCompareAndSwapPointerNilAddressEvaluatesValueFirst: same ordering
// pin for CAS (both value arguments are evaluated before the panic).
func TestCompareAndSwapPointerNilAddressEvaluatesValueFirst(t *testing.T) {
	oldEvaluated := false
	newEvaluated := false
	oldFn := func() unsafe.Pointer {
		oldEvaluated = true
		return nil
	}
	newFn := func() unsafe.Pointer {
		newEvaluated = true
		x := 4
		return unsafe.Pointer(&x)
	}
	expectNilDerefPanic(t, "CAS(round-trip nil addr, side-effecting values)", func() {
		atomic.CompareAndSwapPointer(nilAddrRoundTrip(), oldFn(), newFn())
	})
	if !oldEvaluated || !newEvaluated {
		t.Fatalf("CAS(nil addr): old evaluated=%v new evaluated=%v, want both true",
			oldEvaluated, newEvaluated)
	}
}
