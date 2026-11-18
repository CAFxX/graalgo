// Benchmark: tight nested integer loops (back-edge safepoint, arithmetic).
package main

import (
	"fmt"
	"os"
	"time"
)

func work(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		for j := 0; j < 1000; j++ {
			s += (i ^ j) & 15
		}
	}
	return s
}

func main() {
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 15; i++ {
		t0 := time.Now()
		r = work(20000)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH loop %d\n", best.Nanoseconds())
}
