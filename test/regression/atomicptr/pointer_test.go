// Regression tests for review23 P1-1: atomic.LoadPointer /
// atomic.StorePointer over plain unsafe.Pointer storage must be real
// atomic operations with gc's publication ordering, not plain deref
// reads and plain *addr = val assigns.
//
// The deterministic gate for this fix is the frontend export test
// (internal/frontend/atomicptr_review23_test.go): pre-fix, LoadPointer
// lowered to a "deref" node and StorePointer to "assign"/"deref", so
// that test fails pre-fix and passes post-fix regardless of timing.
// The concurrency test below is a smoke test on top of it: a 50-round
// guest publication program passed pre-fix on x86 (plain loads usually
// read fresh there), so it cannot by itself gate the fix — but it must
// keep passing, and it exercises the fixed path end to end under
// graalgo, including the typed atomic.Pointer[T] path whose Store body
// routes through the same lowering.
package atomicptr

import (
	"sync/atomic"
	"testing"
	"unsafe"
)

// TestLoadStorePointerRoundTrip covers the single-threaded contract on
// plain storage: nil default, store/load round-trip, overwrite, and
// coherence with SwapPointer/CompareAndSwapPointer on the same address
// (all four ops share one lock on plain storage post-fix).
func TestLoadStorePointerRoundTrip(t *testing.T) {
	var p unsafe.Pointer
	if got := atomic.LoadPointer(&p); got != nil {
		t.Fatalf("LoadPointer(zero) = %v, want nil", got)
	}
	x := 1
	atomic.StorePointer(&p, unsafe.Pointer(&x))
	if got := atomic.LoadPointer(&p); got != unsafe.Pointer(&x) {
		t.Fatalf("LoadPointer after Store = %v, want %v", got, unsafe.Pointer(&x))
	}
	if got := *(*int)(atomic.LoadPointer(&p)); got != 1 {
		t.Fatalf("payload via LoadPointer = %d, want 1", got)
	}
	y := 2
	atomic.StorePointer(&p, unsafe.Pointer(&y))
	if got := *(*int)(atomic.LoadPointer(&p)); got != 2 {
		t.Fatalf("payload after overwrite = %d, want 2", got)
	}
	old := atomic.SwapPointer(&p, unsafe.Pointer(&x))
	if old != unsafe.Pointer(&y) {
		t.Fatalf("SwapPointer old = %v, want %v", old, unsafe.Pointer(&y))
	}
	if !atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&x), unsafe.Pointer(&y)) {
		t.Fatal("CompareAndSwapPointer(expected match) = false, want true")
	}
	if got := atomic.LoadPointer(&p); got != unsafe.Pointer(&y) {
		t.Fatalf("LoadPointer after CAS = %v, want %v", got, unsafe.Pointer(&y))
	}
	atomic.StorePointer(&p, nil)
	if got := atomic.LoadPointer(&p); got != nil {
		t.Fatalf("LoadPointer after Store(nil) = %v, want nil", got)
	}
}

// TestTypedPointerRoundTrip covers atomic.Pointer[T], whose Store body
// routes through the same StorePointer lowering and whose Load body
// routes through the same LoadPointer lowering.
func TestTypedPointerRoundTrip(t *testing.T) {
	var tp atomic.Pointer[int]
	if got := tp.Load(); got != nil {
		t.Fatalf("Pointer.Load(zero) = %v, want nil", got)
	}
	x := 7
	tp.Store(&x)
	if got := tp.Load(); got != &x || *got != 7 {
		t.Fatalf("Pointer.Load after Store = %v, want &x (7)", got)
	}
	y := 8
	if old := tp.Swap(&y); old != &x {
		t.Fatalf("Pointer.Swap old = %v, want &x", old)
	}
	if !tp.CompareAndSwap(&y, &x) {
		t.Fatal("Pointer.CompareAndSwap(expected match) = false, want true")
	}
	if got := tp.Load(); got != &x {
		t.Fatalf("Pointer.Load after CAS = %v, want &x", got)
	}
	tp.Store(nil)
	if got := tp.Load(); got != nil {
		t.Fatalf("Pointer.Load after Store(nil) = %v, want nil", got)
	}
}

// TestPublicationPointer is the publication smoke test: the writer
// writes the payload, then publishes its address with StorePointer;
// the reader spins on LoadPointer and must then observe the payload.
// Pre-fix this passed on x86 by luck (see the package doc comment);
// its value here is end-to-end coverage of the fixed path, not gating.
func TestPublicationPointer(t *testing.T) {
	var data int
	var p unsafe.Pointer
	for i := 0; i < 200; i++ {
		data = 0
		atomic.StorePointer(&p, nil)
		done := make(chan bool)
		go func() {
			data = 42
			atomic.StorePointer(&p, unsafe.Pointer(&data))
			done <- true
		}()
		for atomic.LoadPointer(&p) == nil {
		}
		if got := *(*int)(atomic.LoadPointer(&p)); got != 42 {
			t.Fatalf("round %d: stale payload: got %d, want 42", i, got)
		}
		<-done
	}
}
