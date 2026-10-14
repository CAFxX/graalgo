package main

// DIV-M4-10: finalizers are gated on an explicit runtime.GC().
// The registration pins the object in GcInterop's PINNED set; only
// runtime.GC() (or SetFinalizer(x, nil)) releases it. Without the
// explicit call the finalizer never runs, even under allocation
// pressure — the pin is a strong root, so this is deterministic, not
// timing-dependent. In gc this program would print "released" in the
// first loop; here the gating is graalgo-specific behavior.

import (
	"fmt"
	"runtime"
)

var finalized = false

type T struct{ v int }

func main() {
	x := &T{v: 42}
	runtime.SetFinalizer(x, func(o *T) { finalized = true })
	x = nil
	// Allocation pressure WITHOUT runtime.GC(): the finalizer must NOT
	// run. Deterministic: PINNED keeps the object strongly reachable, so
	// no JVM collection can clear the phantom.
	for i := 0; i < 100; i++ {
		_ = make([]byte, 1<<20)
	}
	if finalized {
		fmt.Println("UNEXPECTED: finalized without runtime.GC()")
	} else {
		fmt.Println("gated")
	}
	// Explicit GC releases the pin; the finalizer must run.
	for i := 0; i < 200 && !finalized; i++ {
		runtime.GC()
	}
	if finalized {
		fmt.Println("released")
	} else {
		fmt.Println("UNEXPECTED: not finalized after runtime.GC()")
	}
}
