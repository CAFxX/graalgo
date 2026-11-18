package main

// A finalizer on a still-reachable object cannot wake anyone: the
// deadlock is still reported (the pending finalizer only delays it).

import (
	"fmt"
	"runtime"
)

type T struct{ n [64]int }

var keep *T

func main() {
	keep = &T{}
	runtime.SetFinalizer(keep, func(*T) { fmt.Println("finalized?!") })
	runtime.GC()
	c := make(chan int)
	<-c
}
