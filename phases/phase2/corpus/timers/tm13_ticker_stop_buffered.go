package main

import (
	"fmt"
	"time"
)

// tm13: Stop on a live ticker with a buffered tick drains the tick
// (Go 1.23+ timerchandrain parity). After Stop, no stale tick is
// observable; a second Stop reports inactive.
func main() {
	tk := time.NewTicker(20 * time.Millisecond)
	time.Sleep(300 * time.Millisecond) // ticks fire; 1 buffered (cap 1)
	tk.Stop()
	fmt.Println("stopped")
	// Drain: must be empty (Stop drained the buffered tick).
	n := 0
	for {
		select {
		case <-tk.C:
			n++
		default:
			goto drained
		}
	}
drained:
	fmt.Println("buffered after stop:", n)
	fmt.Println("done")
}
