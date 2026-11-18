// Benchmark: slice and array indexing (bounds checks, element loads/stores).
package main

import (
	"fmt"
	"os"
	"time"
)

var arr [1024]int

func work(s []int) int {
	t := 0
	for k := 0; k < 20; k++ {
		for i := range s {
			s[i] = s[i] + i&7
		}
		for _, v := range s {
			t += v
		}
		for i := 0; i < len(arr); i++ {
			arr[i] += i
			t += arr[i] & 3
		}
	}
	return t
}

func main() {
	s := make([]int, 100000)
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 15; i++ {
		t0 := time.Now()
		r += work(s)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH slicesum %d\n", best.Nanoseconds())
}
