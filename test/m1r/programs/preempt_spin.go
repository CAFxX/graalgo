// Quantum preemption (GG-SCHED-0001, WP-POLLCOST): at GOMAXPROCS(1), two
// CPU-bound goroutines that never block hold the only run slot — one whose
// only safepoint is a loop back-edge (no call in the loop), one whose only
// safepoints are function entries (recursion, no loop). The worker main
// waits for runs only if both are preempted at their quantum; a build whose
// back-edge or function-entry poll is missing hangs here, and run.sh's
// timeout fails it. Main's return must then freeze both spinners (exit at
// the next poll).
package main

import "runtime"

var sink int

func spinLoop() {
	n := 0
	for {
		n += n&3 + 1
		if n == -1 {
			break
		}
	}
	sink = n
}

func spinRec(d int) int {
	if d == 0 {
		return 1
	}
	return spinRec(d-1) + spinRec(d-1)
}

func main() {
	runtime.GOMAXPROCS(1)
	go spinLoop()
	go func() { sink = spinRec(62) }()
	done := make(chan int)
	go func() {
		s := 0
		for i := 0; i < 1000; i++ {
			s += i
		}
		done <- s
	}()
	println(<-done)
	println("preempted")
}
