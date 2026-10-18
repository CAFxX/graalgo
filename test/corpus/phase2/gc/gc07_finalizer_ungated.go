package main

// R-P0-3 (2026-10-18; DIV-M4-10 retired): finalizers are NOT gated on an
// explicit runtime.GC(). The registration (Cleaner phantom + weak map)
// holds no strong reference to the object, so once the object is
// unreachable, JVM collection finalizes it with no explicit GC call —
// like gc.
//
// NOTE on the struct shape: T carries a pointer field so it is NOT
// tiny-allocated. gc1.27.1 never runs finalizers for tiny-allocated
// objects (< 16 bytes, noscan) — verified empirically 2026-10-18 — so a
// tiny T would make this differential test vacuous on the gc side.
//
// JMM note: the finalizer runs on the JVM Cleaner thread and writes the
// Go global; the main goroutine polls it. Racy in the strict JMM sense
// but reliable in practice (same pattern as gc01).

import (
	"fmt"
	"runtime"
)

var finalized = false

type T struct {
	v int
	p *int // non-nil-able pointer field: keeps T out of the tiny allocator
}

func main() {
	x := &T{v: 42}
	runtime.SetFinalizer(x, func(o *T) { finalized = true })
	x = nil
	// Allocation pressure WITHOUT runtime.GC(): the finalizer must run.
	// (Pre-fix this printed UNEXPECTED: PINNED pinned the object until
	// an explicit runtime.GC(), leaking it forever.)
	for i := 0; i < 500 && !finalized; i++ {
		_ = make([]byte, 1<<20)
	}
	if finalized {
		fmt.Println("released")
	} else {
		fmt.Println("UNEXPECTED: not finalized without runtime.GC()")
	}
}
