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
	x = nil
	// NOTE: record .expected with GOMAXPROCS=1. Back-to-back runtime.GC()
	// calls starve the finalizer goroutine under multi-P scheduling
	// (the world is almost always stopped); single-P scheduling is
	// deterministic. The graalgo test runs with --gmp=1 for the same reason.
	for i := 0; i < 200 && !finalized; i++ {
		runtime.GC()
	}
	if finalized {
		fmt.Println("finalized")
	} else {
		fmt.Println("NOT finalized")
	}
}
