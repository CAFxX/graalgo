package main

import (
	"fmt"
	"runtime"
)

type T struct{ v int }

func main() {
	x := &T{v: 42}
	// The finalizer dereferences its argument. In graalgo the argument is
	// always nil (DIV-M4-1), so this panics. The panic must be LOUD (stderr),
	// not silent: GcInterop prints "graalgo: finalizer panicked:".
	// In gc this would be a fatal crash; here the program continues.
	runtime.SetFinalizer(x, func(o *T) { fmt.Println(o.v) })
	x = nil
	for i := 0; i < 200; i++ {
		runtime.GC()
	}
	fmt.Println("survived")
}
