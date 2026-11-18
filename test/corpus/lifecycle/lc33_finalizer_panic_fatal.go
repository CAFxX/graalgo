package main

// A panicking finalizer is fatal to the program (exit 2), like a panic
// in any goroutine.

import (
	"fmt"
	"runtime"
	"time"
)

type T struct{ n [64]int }

func main() {
	p := &T{}
	runtime.SetFinalizer(p, func(*T) { panic("finalizer boom") })
	p = nil
	runtime.GC()
	time.Sleep(2 * time.Second)
	fmt.Println("not reached")
}
