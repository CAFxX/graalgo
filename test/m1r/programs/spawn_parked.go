// spawn_parked: many goroutines alive at once. 20,000 goroutines are spawned
// in batches of 1,000 and stay parked on one channel while the next batches
// are created; a broadcast (close) then wakes them all and each adds its index
// to a sum. Functional only, so the output is the same on any host: the
// spawn-cost scaling (ADR-0018: creating a goroutine costs the same however
// many are alive) is a wall-clock property and is timed by the benchmark tier,
// test/bench/scaling/spawn_flat.go (make bench-spawn-flat, weekly), not here.
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	batch   = 1000
	batches = 20
)

func main() {
	release := make(chan struct{})
	var started, done sync.WaitGroup
	var parked, sum atomic.Int64
	for b := 0; b < batches; b++ {
		started.Add(batch)
		done.Add(batch)
		for i := 0; i < batch; i++ {
			id := int64(b*batch + i)
			go func() {
				parked.Add(1)
				started.Done()
				<-release
				sum.Add(id)
				done.Done()
			}()
		}
		started.Wait()
		fmt.Printf("batch %d: %d alive\n", b, parked.Load())
	}
	close(release)
	done.Wait()
	n := int64(batch * batches)
	fmt.Println("released", n, "sum", sum.Load(), "want", n*(n-1)/2)
}
