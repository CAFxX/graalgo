package main

import (
	"fmt"
	"time"
)

func main() {
	tk := time.NewTicker(50 * time.Millisecond)
	time.Sleep(300 * time.Millisecond) // slow receiver: ticks must drop
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
	tk.Stop()
	if n < 6 {
		fmt.Println("dropped")
	} else {
		fmt.Println("no drop (bug)")
	}
}
