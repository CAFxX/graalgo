// 06_parallel: t.Parallel subtests run after sequential ones, with
// === PAUSE / === CONT markers, bounded by -test.parallel=2 (see
// 06_parallel.args). Done-indices are collected and printed sorted at
// parent cleanup (parallel completion order is nondeterministic).
// Marked .timing (parallel scheduling is timing-sensitive by construction).
package main

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestParallel(t *testing.T) {
	var mu sync.Mutex
	var running, maxSeen int
	var done []int
	for i := 0; i < 4; i++ {
		i := i
		t.Run(fmt.Sprintf("p%d", i), func(t *testing.T) {
			t.Parallel()
			mu.Lock()
			running++
			if running > maxSeen {
				maxSeen = running
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			running--
			done = append(done, i)
			mu.Unlock()
		})
	}
	// Parent cleanup runs after all parallel subtests complete.
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if maxSeen > 2 {
			fmt.Println("BAD: maxSeen =", maxSeen, "> 2")
			return
		}
		sort.Ints(done)
		fmt.Println("PARALLEL-DONE", done, "maxSeen<=", 2)
	})
}

func main() {
	tests := []testing.InternalTest{
		{"TestParallel", TestParallel},
	}
	testing.Main(nil, tests, nil, nil)
}

