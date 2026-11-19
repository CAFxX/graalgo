//m1c:requires descriptors

// time on the runtime timer heap: Sleep ordering across goroutines, Timer
// Stop/Reset results with Go 1.23's synchronous channels (no stale value after
// Stop/Reset, an unreceived fired value counts as pending), After, AfterFunc
// and its Stop, Ticker ticks and Reset, and a program blocked only on a timer
// is not a deadlock.
package main

import (
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	var order []int
	var wg sync.WaitGroup
	for _, ms := range []int{40, 10, 30, 20} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(ms) * time.Millisecond)
			mu.Lock()
			order = append(order, ms)
			mu.Unlock()
		}()
	}
	wg.Wait()
	println("sleep order", order[0], order[1], order[2], order[3])

	start := time.Now()
	time.Sleep(15 * time.Millisecond)
	println("slept at least 15ms:", time.Since(start) >= 15*time.Millisecond)
	time.Sleep(-1)
	time.Sleep(0)

	t := time.NewTimer(time.Hour)
	println("stop pending timer", t.Stop(), "again", t.Stop())
	println("reset stopped timer", t.Reset(5*time.Millisecond))
	<-t.C
	println("received after reset")

	t2 := time.NewTimer(time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	println("stop fired, unreceived timer", t2.Stop())
	select {
	case <-t2.C:
		println("stale value after Stop")
	default:
		println("no stale value after Stop")
	}

	t3 := time.NewTimer(time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	println("reset fired, unreceived timer", t3.Reset(time.Hour))
	select {
	case <-t3.C:
		println("stale value after Reset")
	default:
		println("no stale value after Reset")
	}
	t3.Stop()

	v := <-time.After(5 * time.Millisecond)
	println("After delivers a time:", !v.IsZero())

	done := make(chan string)
	time.AfterFunc(5*time.Millisecond, func() { done <- "afterfunc ran" })
	println(<-done)
	f := time.AfterFunc(time.Hour, func() { println("must not run") })
	println("stop afterfunc", f.Stop())

	tk := time.NewTicker(2 * time.Millisecond)
	for i := 0; i < 3; i++ {
		<-tk.C
	}
	println("three ticks")
	tk.Reset(time.Hour)
	select {
	case <-tk.C:
		println("tick after Reset")
	case <-time.After(20 * time.Millisecond):
		println("no tick after Reset to an hour")
	}
	tk.Stop()

	// Only a timer pending: not a deadlock.
	c := make(chan int)
	time.AfterFunc(10*time.Millisecond, func() { c <- 7 })
	println("value from a timer goroutine", <-c)
}
