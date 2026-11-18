package main

// A backward goto is a loop back-edge: a goroutine spinning in a goto
// loop is preempted like any other loop, so with GOMAXPROCS=1 the
// goroutine that releases it still gets to run (gc: async preemption).

import (
	"fmt"
	"sync/atomic"
	"time"
)

var flag int32

func main() {
	go func() {
		time.Sleep(20 * time.Millisecond)
		atomic.StoreInt32(&flag, 1)
	}()
	n := 0
loop:
	n++
	if atomic.LoadInt32(&flag) == 0 {
		goto loop
	}
	fmt.Println("released", n > 0)
}
