// Benchmark: map insert, lookup, delete, and range.
package main

import (
	"fmt"
	"os"
	"time"
)

func work(n int) int {
	m := make(map[int]int)
	for i := 0; i < n; i++ {
		m[i*7] = i
	}
	t := 0
	for i := 0; i < 2*n; i++ {
		if v, ok := m[i]; ok {
			t += v
		}
	}
	for i := 0; i < n; i += 2 {
		delete(m, i*7)
	}
	for k, v := range m {
		t += k - v
	}
	return t + len(m)
}

func main() {
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 15; i++ {
		t0 := time.Now()
		r = work(50000)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH maps %d\n", best.Nanoseconds())
}
