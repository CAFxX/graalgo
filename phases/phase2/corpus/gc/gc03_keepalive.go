package main

import (
	"fmt"
	"runtime"
)

var finalized = false

type T struct{ v int }

func main() {
	x := &T{v: 42}
	runtime.SetFinalizer(x, func(o *T) { finalized = true })
	y := x.v + 1
	runtime.KeepAlive(x)
	// The finalizer must not have run before the KeepAlive point:
	// x was live through the call.
	if finalized {
		fmt.Println("premature")
	} else {
		fmt.Println("alive", y)
	}
}
