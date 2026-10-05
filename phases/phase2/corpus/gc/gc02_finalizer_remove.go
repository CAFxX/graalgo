package main

// JMM note: the finalizer runs on the JVM Cleaner thread and writes the
// Go global; the main goroutine polls it. Visibility relies on the
// synchronization in runtime.GC() (MXBean poll with sleep) creating a
// happens-before edge. This is racy in the strict JMM sense but reliable
// in practice; a production implementation would need volatile globals.

import (
	"fmt"
	"runtime"
)

var finalized = false

type T struct{ v int }

func main() {
	x := &T{v: 42}
	runtime.SetFinalizer(x, func(o *T) { finalized = true })
	runtime.SetFinalizer(x, nil)
	// KeepAlive: without it, the JIT may consider x dead immediately after
	// the SetFinalizer call (it is overwritten by x=nil without being read),
	// and the finalizer could run before the cancellation is processed.
	// KeepAlive ensures x stays alive until the cancellation is definite.
	runtime.KeepAlive(x)
	x = nil
	for i := 0; i < 50 && !finalized; i++ {
		runtime.GC()
	}
	if finalized {
		fmt.Println("finalized")
	} else {
		fmt.Println("not finalized")
	}
}
