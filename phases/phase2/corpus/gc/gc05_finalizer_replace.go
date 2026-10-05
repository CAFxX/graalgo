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

var fired = ""

type T struct{ v int }

func main() {
	x := &T{v: 42}
	runtime.SetFinalizer(x, func(o *T) { fired = "first" })
	runtime.SetFinalizer(x, nil)
	runtime.SetFinalizer(x, func(o *T) { fired = "second" })
	// See gc02 for the KeepAlive rationale.
	runtime.KeepAlive(x)
	x = nil
	for i := 0; i < 200 && fired == ""; i++ {
		runtime.GC()
	}
	if fired == "" {
		fmt.Println("none fired")
	} else {
		fmt.Println(fired)
	}
}
