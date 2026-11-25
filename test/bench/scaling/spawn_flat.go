// spawn_flat: goroutine creation costs the same however many goroutines are
// alive (ADR-0018). 20,000 goroutines are spawned in batches of 1,000 and stay
// parked on a channel; the fastest of the two last batches (19,000 alive) must
// be within 3x the fastest of the two first (or under 50 ms), after a warm-up
// round. gc prints "spawn flat"; a creation path that walks every live
// goroutine (one polyglot thread per goroutine: ~5x per spawn at 20,000)
// prints "spawn grows". A wall-clock check, so it lives in the benchmark tier
// (make bench-spawn-flat: compared with gc's output by test/m1r/run.sh;
// weekly in ci.yml's full-suite job), never in a per-push gate; the per-push
// structural guard is CarriersTest (no polyglot thread per goroutine), and
// test/m1r/programs/spawn_parked.go keeps the functional workload per push.
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
	ts := round()
	early := min(ts[0], ts[1])
	late := min(ts[batches-2], ts[batches-1])
	// A batch under 50 ms (50 µs a spawn) is flat whatever the ratio: gc's
	// batches take ~0.2 ms, where host noise alone exceeds 3x.
	if late <= 3*early || late < 50*time.Millisecond {
		fmt.Println("spawn flat")
	} else {
		fmt.Println("spawn grows: first batches", early, "last batches", late)
	}
}
