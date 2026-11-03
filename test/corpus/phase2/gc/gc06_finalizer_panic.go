package main

import (
	"fmt"
	"runtime"
)

type T struct{ v int }

func main() {
	x := &T{v: 42}
	// The finalizer dereferences its argument. In graalgo the argument is
	// always nil (DIV-M4-1), so this panics. In gc a panicking finalizer
	// is a fatal crash (exit status 2); graalgo matches since
	// REVIEW-2026-11-03-16 P1-B5: the finalizer thread prints
	// "graalgo: finalizer panicked:" and terminates the program with
	// exit status 2. "NOT finalized" must never print: if the loop
	// completes, the finalizer never fired and the test has failed.
	runtime.SetFinalizer(x, func(o *T) { fmt.Println(o.v) })
	x = nil
	for i := 0; i < 200; i++ {
		runtime.GC()
	}
	fmt.Println("NOT finalized")
}
