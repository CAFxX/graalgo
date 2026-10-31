package main

// L2: finalizers are NOT gated on an explicit runtime.GC(). An earlier
// design kept every registered object strongly reachable until
// runtime.GC() (DIV-M4-10), so a finalizer could never fire without
// one and the objects leaked forever in programs that never call it.
// Registration now pins only for the setFinalizer call itself; after
// that, lifetime is natural (tied to Go variables, like gc). This
// program drops the object and applies allocation pressure WITHOUT
// any runtime.GC() call: the finalizer must run. (Under the old
// design this printed "gated" then required the explicit call.)

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
	// Allocation pressure WITHOUT runtime.GC(): the finalizer must run.
	// The loop's back-edge quantum-parks main at GOMAXPROCS=1, giving
	// the spawned finalizer goroutine a slot when it is ready.
	for i := 0; i < 2000 && !finalized; i++ {
		_ = make([]byte, 1<<20)
	}
	if finalized {
		fmt.Println("finalized")
	} else {
		fmt.Println("UNEXPECTED: not finalized without runtime.GC()")
	}
}
