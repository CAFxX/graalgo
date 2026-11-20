// spawn_flat: goroutine creation costs the same however many goroutines are
// alive (ADR-0018). 20,000 goroutines are spawned in batches of 1,000 and stay
// parked on a channel; the fastest of the two last batches (19,000 alive) must
// be within 3x the fastest of the two first, and the whole round must take
// under 2 s (after a warm-up round). gc prints "spawn flat" and "spawn fast"
// (~5 ms); a creation path that walks every live goroutine (one polyglot
// thread per goroutine: ~5x per spawn at 20,000, ~15 s for the round) prints
// "spawn grows" and "spawn slow".
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	batch   = 1000
	batches = 20
)

func spawnBatch(release chan struct{}, started, done *sync.WaitGroup) time.Duration {
	t0 := time.Now()
	started.Add(batch)
	done.Add(batch)
	for i := 0; i < batch; i++ {
		go func() {
			started.Done()
			<-release
			done.Done()
		}()
	}
	started.Wait()
	return time.Since(t0)
}

func round() []time.Duration {
	release := make(chan struct{})
	var started, done sync.WaitGroup
	ts := make([]time.Duration, batches)
	for b := range ts {
		ts[b] = spawnBatch(release, &started, &done)
	}
	close(release)
	done.Wait()
	return ts
}

func main() {
	round() // warm-up: the spawn path compiled
	t0 := time.Now()
	ts := round()
	total := time.Since(t0)
	early := min(ts[0], ts[1])
	late := min(ts[batches-2], ts[batches-1])
	if late <= 3*early {
		fmt.Println("spawn flat")
	} else {
		fmt.Println("spawn grows: first batches", early, "last batches", late)
	}
	if total < 2*time.Second {
		fmt.Println("spawn fast")
	} else {
		fmt.Println("spawn slow:", total)
	}
}
