// Regression tests for REVIEW-2026-11-01-21 P0-1: sync/atomic
// LoadPointer/StorePointer lowered to plain deref reads/writes, with
// no atomicity and no ordering guarantee. The fix routes them through
// the atomicaddr+syncop wire nodes (a "atomic.Uintptr" Load/Store),
// like the package-level word ops.
//
// Failability note (honest): a memory-ordering defect cannot be made
// to fail deterministically from a black-box program — a stale read
// is timing-dependent, and single-threaded programs pass under the
// old lowering too. The deterministic gate for this fix is the
// frontend wire test (TestAtomicLoadPointerLowersToSyncop /
// TestAtomicStorePointerStmtRenamedImport in internal/frontend),
// which fails on any return to the deref lowering. These tests are
// the behavioral guard: they exercise every shape the new lowering
// must serve (plain field, AtomicReference cell via atomic.Pointer[T],
// Load-after-Swap/CAS coherence, repeated cross-goroutine
// publication) and fail loudly on a wrong value, a panic, or a hang
// (via the outer timeout in the test-regression Makefile target).
package p0atomicpointer

import (
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

// TestLoadStorePointerRoundtrip pins the sequential behavior: store
// then load returns the stored pointer, nil roundtrips, and a load
// after SwapPointer / CompareAndSwapPointer observes the RMW result
// on a plain field.
func TestLoadStorePointerRoundtrip(t *testing.T) {
	var p unsafe.Pointer
	x := 11
	y := 22
	atomic.StorePointer(&p, unsafe.Pointer(&x))
	if got := atomic.LoadPointer(&p); got != unsafe.Pointer(&x) {
		t.Fatalf("LoadPointer after StorePointer: got %v, want pointer to x", got)
	}
	atomic.StorePointer(&p, nil)
	if got := atomic.LoadPointer(&p); got != nil {
		t.Fatalf("LoadPointer after StorePointer(nil): got %v, want nil", got)
	}
	if old := atomic.SwapPointer(&p, unsafe.Pointer(&x)); old != nil {
		t.Fatalf("SwapPointer old: got %v, want nil", old)
	}
	if got := atomic.LoadPointer(&p); got != unsafe.Pointer(&x) {
		t.Fatalf("LoadPointer after SwapPointer: got %v, want pointer to x", got)
	}
	if !atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&x), unsafe.Pointer(&y)) {
		t.Fatalf("CompareAndSwapPointer with matching old returned false")
	}
	if got := atomic.LoadPointer(&p); got != unsafe.Pointer(&y) {
		t.Fatalf("LoadPointer after CompareAndSwapPointer: got %v, want pointer to y", got)
	}
	if atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&x), unsafe.Pointer(&x)) {
		t.Fatalf("CompareAndSwapPointer with mismatching old returned true")
	}
}

// TestLoadPointerPublication is the litmus test: in each of 50 rounds
// a writer goroutine writes plain data and then publishes its address
// with StorePointer; the reader spins on LoadPointer and then reads
// the data. Under gc's acquire/release semantics the reader always
// observes the published value. Any stale observation, torn pointer,
// or lost store fails the test.
func TestLoadPointerPublication(t *testing.T) {
	var flag unsafe.Pointer
	data := 0
	for round := 1; round <= 50; round++ {
		want := round * 10
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			data = want
			atomic.StorePointer(&flag, unsafe.Pointer(&data))
		}()
		for atomic.LoadPointer(&flag) == nil {
		}
		if got := *(*int)(atomic.LoadPointer(&flag)); got != want || data != want {
			t.Fatalf("round %d: published=%d data=%d, want %d", round, got, data, want)
		}
		atomic.StorePointer(&flag, nil)
		wg.Wait()
	}
}

// TestTypedPointerLoadStore exercises the in-package shape: the real
// atomic.Pointer[T] method bodies call LoadPointer / StorePointer
// unqualified on &x.v, where the address evaluates to the
// AtomicReference cell itself. That shape must serve through the same
// syncop Uintptr arms — a backend that only accepts LongCell
// receivers fails here with a loud error, not a silent value.
func TestTypedPointerLoadStore(t *testing.T) {
	var pp atomic.Pointer[int]
	z := 7
	pp.Store(&z)
	if got := pp.Load(); got != &z {
		t.Fatalf("Pointer.Load after Store: got %v, want pointer to z", got)
	}
	if got := *pp.Load(); got != 7 {
		t.Fatalf("Pointer.Load value: got %d, want 7", got)
	}
	w := 9
	if old := pp.Swap(&w); old != &z {
		t.Fatalf("Pointer.Swap old: got %v, want pointer to z", old)
	}
	if got := pp.Load(); got != &w {
		t.Fatalf("Pointer.Load after Swap: got %v, want pointer to w", got)
	}
	pp.Store(nil)
	if got := pp.Load(); got != nil {
		t.Fatalf("Pointer.Load after Store(nil): got %v, want nil", got)
	}
}
