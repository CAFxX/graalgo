// After main calls runtime.Goexit, a program whose only other goroutine is
// asleep on a timer is not deadlocked: the timer fires, the goroutine runs
// and exits the process (m3-plan G6 mutant M-SCHED-01: the deadlock check
// ignoring pending timers once main has Goexited).
package main

import (
	"os"
	"runtime"
	"time"
)

func main() {
	go func() {
		time.Sleep(20 * time.Millisecond)
		println("worker woke after main's Goexit")
		os.Exit(3)
	}()
	runtime.Goexit()
}
