// O5 Go side: GOMAXPROCS throughput scaling. 16 workers, fixed digest.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	secs, _ := strconv.Atoi(os.Args[1])
	const workers = 16
	var total atomic.Int64
	var wg sync.WaitGroup
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local int64
			x := uint64(0x12345678)
			for time.Now().Before(deadline) {
				for k := 0; k < 1000; k++ {
					x ^= x << 13
					x ^= x >> 7
					x ^= x << 17
					local++
				}
			}
			total.Add(local)
			_ = x
		}()
	}
	wg.Wait()
	ops := float64(total.Load())
	fmt.Printf("gomaxprocs=%d workers=%d secs=%d mops=%.1f mops_per_sec=%.1f\n",
		runtime.GOMAXPROCS(0), workers, secs, ops/1e6, ops/1e6/float64(secs))
}
