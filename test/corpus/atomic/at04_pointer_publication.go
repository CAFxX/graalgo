// verifies: GG-0020
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// fix-p0-1-atomic-pointer: package-level sync/atomic pointer ops must
// carry gc's acquire/release semantics, not lower to plain deref
// reads/writes. The publication phase is the litmus test: a writer
// goroutine writes plain data, then publishes its address with
// StorePointer; the reader spins on LoadPointer and then reads the
// data. Under gc the reader always observes the published value; a
// plain-deref lowering carries no ordering guarantee, so the reader
// could observe stale data. (Memory-model litmus tests cannot fail
// deterministically — a stale read is timing-dependent — so the
// deterministic regression gate for this fix is the frontend wire
// test TestAtomicLoadPointerLowersToSyncop /
// TestAtomicStorePointerStmtRenamedImport, which fail on any return
// to the deref lowering. This program is the behavioral guard: it
// must print exactly the gc output, every run.)
//
// The remaining phases pin the surrounding behavior the new lowering
// must not regress: sequential roundtrips, nil, the typed
// atomic.Pointer[T] path (whose real method bodies call LoadPointer /
// StorePointer unqualified on &x.v — the AtomicReference cell shape),
// and Load-after-Swap / Load-after-CAS coherence on a plain field
// (all plain-field atomic ops share one lock).

func main() {
	// Phase 1: sequential roundtrip and nil.
	var p unsafe.Pointer
	x := 11
	y := 22
	atomic.StorePointer(&p, unsafe.Pointer(&x))
	if atomic.LoadPointer(&p) == unsafe.Pointer(&x) {
		fmt.Println("roundtrip ok")
	} else {
		fmt.Println("roundtrip MISMATCH")
	}
	atomic.StorePointer(&p, nil)
	if atomic.LoadPointer(&p) == nil {
		fmt.Println("nil ok")
	} else {
		fmt.Println("nil MISMATCH")
	}

	// Phase 2: publication across goroutines.
	var flag unsafe.Pointer
	data := 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data = 42
		atomic.StorePointer(&flag, unsafe.Pointer(&data))
	}()
	for atomic.LoadPointer(&flag) == nil {
	}
	got := *(*int)(atomic.LoadPointer(&flag))
	if got == 42 && data == 42 {
		fmt.Println("published", got, data)
	} else {
		fmt.Println("published MISMATCH", got, data)
	}
	wg.Wait()

	// Phase 3: typed atomic.Pointer[T] through its real method bodies.
	var pp atomic.Pointer[int]
	z := 7
	pp.Store(&z)
	if pp.Load() == &z && *pp.Load() == 7 {
		fmt.Println("typed ok", *pp.Load())
	} else {
		fmt.Println("typed MISMATCH")
	}

	// Phase 4: Load observes Swap and CAS on a plain field.
	old := atomic.SwapPointer(&p, unsafe.Pointer(&x))
	if old == nil && atomic.LoadPointer(&p) == unsafe.Pointer(&x) {
		fmt.Println("swap-load ok")
	} else {
		fmt.Println("swap-load MISMATCH")
	}
	if atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&x), unsafe.Pointer(&y)) &&
		atomic.LoadPointer(&p) == unsafe.Pointer(&y) {
		fmt.Println("cas-load ok")
	} else {
		fmt.Println("cas-load MISMATCH")
	}
	if !atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&x), unsafe.Pointer(&x)) {
		fmt.Println("cas-false ok")
	} else {
		fmt.Println("cas-false MISMATCH")
	}
}
