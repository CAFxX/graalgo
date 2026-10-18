package main

import (
	"fmt"
	"runtime"
)

type T struct{ v int }

func main() {
	x := &T{v: 42}
	// The finalizer dereferences its argument. In graalgo the argument is
	// always nil (DIV-M4-1), so this panics with a nil-pointer dereference.
	// gc parity (go1.27.1 src/runtime/mfinal.go: no recover on the finalizer
	// goroutine): a panicking finalizer is FATAL — the program dies with
	// exit 2 and "survived" is never printed. The differential test asserts
	// exactly that (exit code, empty stdout, the panic on stderr).
	runtime.SetFinalizer(x, func(o *T) { fmt.Println(o.v) })
	x = nil
	for i := 0; i < 200; i++ {
		runtime.GC()
	}
	fmt.Println("survived")
}
