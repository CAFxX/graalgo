package main

// context-pattern stress: cancellation via flag broadcast.
// Go `context` package needs imports + interfaces (WS D/G) — out of
// scope. This covers the closest expressible pattern: a cancellation
// flag broadcast to N workers; every worker must observe cancellation
// and terminate. Differential vs gc on termination + output.
import "fmt"

var cancel = false
var done [8]bool

func main() {
	for i := 0; i < 8; i++ {
		id := i
		go func() {
			var c int64
			for !cancel {
				c++
			}
			_ = c
			done[id] = true
		}()
	}
	// Let workers spin briefly, then broadcast cancellation.
	for j := 0; j < 1000000; j++ {
	}
	cancel = true
	for !(done[0] && done[1] && done[2] && done[3] && done[4] && done[5] && done[6] && done[7]) {
	}
	fmt.Println("cancelled")
}
