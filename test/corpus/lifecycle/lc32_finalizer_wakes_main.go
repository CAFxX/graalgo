package main

// A finalizer is the only goroutine that can wake main: no false
// "all goroutines are asleep" while it is queued, and it may block
// (sleep) like any goroutine (gc runs finalizers on a goroutine).

import (
	"fmt"
	"runtime"
	"time"
)

type T struct{ n [64]int }

func main() {
	done := make(chan bool)
	p := &T{}
	runtime.SetFinalizer(p, func(*T) {
		time.Sleep(10 * time.Millisecond)
		close(done)
	})
	p = nil
	runtime.GC()
	<-done
	fmt.Println("finalized")
}
