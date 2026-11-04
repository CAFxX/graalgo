// Regression test for REVIEW-2026-11-04-07 NEW-P1-1 (lane R07-GOROUTINES):
// the differential corpus program 14_goroutines_sum (fan-out/fan-in over a
// buffered channel with sync.WaitGroup) was recorded in
// test/snapshots/go1.27.1.json (2026-10-14, commit 10b6c507) as losing
// partial sums non-deterministically (totals 32/34 instead of 36) with no
// tracker ID and no fix, and was excluded from the snapshot baseline — so
// per-push CI did not cover the pattern at all.
//
// On current HEAD the program passes repeatedly (50/50 runs printing 36
// under graalgo-sync-run at 473e50fe), so the exclusion was removed and
// this test guards the pattern against a recurrence. A single snapshot run
// cannot catch a flaky loss (green-by-luck re-enters the baseline), so the
// test repeats the exact corpus shape in-process and asserts BOTH the
// partial-sum count and the total on every iteration: a whole-send loss
// shows as count < 4, while the recorded symptom (36 minus 2 or 4, i.e.
// element-level losses, not minus a whole partial sum 3/7/11/15) shows as
// count == 4 with total < 36. Asserting both distinguishes the two.
//
// The executing WaitGroup on this path is the real disk body
// (sync/waitgroup.go via the disk importer; synthetic sync was retired in
// 503608ea), not the GoWaitGroup cell: NodeFactory's PKG-053 rule lowers
// sync.WaitGroup to a plain GoStruct, and the wire for this program
// contains no "syncnew" node (only "syncop" atomic nodes inside the disk
// bodies). This test therefore guards the disk-body + atomic + semaphore
// path, not the cell path.
//
// NOTE: no timer-based guard (e.g. time.After) is used here on purpose:
// timer channels do not fire under graalgo yet (see the pkg031 lane note),
// so a timeout-guarded test would hang instead of failing.
package goroutinessum

import (
	"sync"
	"testing"
)

// fanOutFanIn runs the 14_goroutines_sum shape once and returns
// (partials received, total). It mirrors the corpus program exactly:
// 4 workers summing pairs of 1..8, buffered channel cap 4, a closer
// goroutine doing wg.Wait() then close(ch), main ranging over ch.
func fanOutFanIn() (int, int) {
	nums := []int{1, 2, 3, 4, 5, 6, 7, 8}
	ch := make(chan int, 4)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(part []int) {
			defer wg.Done()
			sum := 0
			for _, v := range part {
				sum += v
			}
			ch <- sum
		}(nums[w*2 : w*2+2])
	}
	go func() { wg.Wait(); close(ch) }()
	total := 0
	count := 0
	for s := range ch {
		total += s
		count++
	}
	return count, total
}

func TestFanOutFanInRepeated(t *testing.T) {
	const iterations = 25
	for i := 0; i < iterations; i++ {
		count, total := fanOutFanIn()
		if count != 4 {
			t.Fatalf("iteration %d: received %d partial sums, want 4 (whole-send loss)", i, count)
		}
		if total != 36 {
			t.Fatalf("iteration %d: total = %d, want 36", i, total)
		}
	}
}
