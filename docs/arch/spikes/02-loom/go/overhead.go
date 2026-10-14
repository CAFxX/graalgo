// Overhead Go side: tight loop baseline, ns/iteration (no safepoint concept;
// Go's preemption check is ~free in compiled code — this is the reference).
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	iters, _ := strconv.ParseInt(os.Args[1], 10, 64)
	var x uint64
	for i := int64(0); i < iters/10; i++ {
		x += uint64(i) * 0x9E3779B97F4A7C15
	}
	t0 := time.Now().UnixNano()
	for i := int64(0); i < iters; i++ {
		x += uint64(i) * 0x9E3779B97F4A7C15
	}
	dt := time.Now().UnixNano() - t0
	_ = x
	fmt.Printf("mode=GO iters=%d ns_per_iter=%.2f\n", iters, float64(dt)/float64(iters))
}
