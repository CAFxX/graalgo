// Benchmark: recursive calls (call overhead, function-entry safepoint).
package main

import (
	"fmt"
	"os"
	"time"
)

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 15; i++ {
		t0 := time.Now()
		r = fib(27)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH fib %d\n", best.Nanoseconds())
}
