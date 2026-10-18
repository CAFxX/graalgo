package main

import (
	"fmt"
	"runtime"
)

func main() {
	x := new(int)
	// An explicit Go panic inside the finalizer. gc parity (go1.27.1
	// src/runtime/mfinal.go: no recover on the finalizer goroutine): the
	// program dies with exit 2 printing "panic: boom" — it must not print
	// "survived" and continue. The differential test asserts exactly that.
	runtime.SetFinalizer(x, func(_ *int) { panic("boom") })
	x = nil
	for i := 0; i < 200; i++ {
		runtime.GC()
	}
	fmt.Println("survived")
}
