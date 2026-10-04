// O4 Go side: timer latency under load. Sleep(10ms) loop, measure excess.
package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"
)

var stop = false

func main() {
	nSpin, _ := strconv.Atoi(os.Args[1])
	samples, _ := strconv.Atoi(os.Args[2])
	for i := 0; i < nSpin; i++ {
		go func() {
			var c int64
			var sink uint64
			for !stop {
				sink += uint64(c) * 0x9E3779B97F4A7C15
				sink ^= sink >> 29
				c++
			}
			_ = sink
		}()
	}
	excess := make([]int64, samples)
	for i := 0; i < samples; i++ {
		t0 := time.Now().UnixNano()
		time.Sleep(10 * time.Millisecond)
		excess[i] = (time.Now().UnixNano() - t0 - 10_000_000) / 1000
	}
	stop = true
	sort.Slice(excess, func(i, j int) bool { return excess[i] < excess[j] })
	pct := func(p float64) int64 { return excess[int(p/100*float64(samples))] }
	fmt.Printf("nSpin=%d samples=%d\n", nSpin, samples)
	fmt.Printf("excess_us p50=%d p99=%d max=%d\n", pct(50), pct(99), excess[samples-1])
}
