// Select/send/close/timeout races (BUG-0038, WP-SCHED): a goroutine
// blocked in a select on several channels races a plain send, a close, a
// select send and a timer on every round. A lost wake-up between a select's
// enqueue on its channels and its park leaves both sides parked — the run
// hangs and run.sh's timeout fails it; gc is the oracle for every count.
// Bounded: about 100k channel round trips in all.
package main

import (
	"sync"
	"time"
)

// pingPong: the select receives on ping or quit; the partner sends ping,
// waits for pong, and ends the pair by sending on quit (even pairs) or
// closing it (odd pairs). This is torture leg F6's shape.
func pingPong(pairs, rounds int) (sum int64, closedSeen int) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	for p := 0; p < pairs; p++ {
		ping := make(chan int64)
		pong := make(chan int64)
		quit := make(chan int)
		closeQuit := p%2 == 1
		wg.Add(2)
		go func() {
			defer wg.Done()
			for {
				select {
				case v := <-ping:
					pong <- v + 1
				case _, ok := <-quit:
					if !ok {
						mu.Lock()
						closedSeen++
						mu.Unlock()
					}
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			var s int64
			for k := 0; k < rounds; k++ {
				ping <- int64(k)
				s += <-pong
			}
			if closeQuit {
				close(quit)
			} else {
				quit <- 1
			}
			mu.Lock()
			sum += s
			mu.Unlock()
		}()
	}
	wg.Wait()
	return sum, closedSeen
}

// selectSenders: producers select between sending on out and a done
// channel; consumers receive a fixed total, then done is closed while the
// producers are blocked in their select send.
func selectSenders(producers, consumers, perConsumer int) (got int, stopped int) {
	out := make(chan int)
	done := make(chan struct{})
	var pwg, cwg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < producers; i++ {
		pwg.Add(1)
		go func(i int) {
			defer pwg.Done()
			for {
				select {
				case out <- i:
				case <-done:
					mu.Lock()
					stopped++
					mu.Unlock()
					return
				}
			}
		}(i)
	}
	for i := 0; i < consumers; i++ {
		cwg.Add(1)
		go func() {
			defer cwg.Done()
			for k := 0; k < perConsumer; k++ {
				<-out
				mu.Lock()
				got++
				mu.Unlock()
			}
		}()
	}
	cwg.Wait()
	close(done)
	pwg.Wait()
	return got, stopped
}

// timeoutRaces: a select with a long timeout must take the value every
// round (the timer never wins); a select on a channel nobody sends to must
// take the short timeout.
func timeoutRaces(rounds int) (values, timeouts int) {
	ch := make(chan int)
	go func() {
		for k := 0; k < rounds; k++ {
			ch <- k
		}
	}()
	for k := 0; k < rounds; k++ {
		select {
		case <-ch:
			values++
		case <-time.After(30 * time.Second):
			timeouts += 1000
		}
	}
	never := make(chan int)
	for k := 0; k < 5; k++ {
		select {
		case <-never:
			values += 1000
		case <-time.After(time.Millisecond):
			timeouts++
		}
	}
	return values, timeouts
}

// multiSelect: every receiver selects over all channels; one sender per
// channel; each value is received exactly once.
func multiSelect(chans, receivers, perChan int) (total int, count int) {
	cs := make([]chan int, chans)
	for i := range cs {
		cs[i] = make(chan int)
	}
	results := make(chan int, receivers)
	quit := make(chan struct{})
	var rwg sync.WaitGroup
	for r := 0; r < receivers; r++ {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			s := 0
			for {
				select {
				case v := <-cs[0]:
					s += v
				case v := <-cs[1]:
					s += v
				case v := <-cs[2]:
					s += v
				case v := <-cs[3]:
					s += v
				case <-quit:
					results <- s
					return
				}
			}
		}()
	}
	var swg sync.WaitGroup
	for i := 0; i < chans; i++ {
		swg.Add(1)
		go func(c chan int) {
			defer swg.Done()
			for k := 1; k <= perChan; k++ {
				c <- k
			}
		}(cs[i])
	}
	swg.Wait()
	close(quit)
	rwg.Wait()
	for r := 0; r < receivers; r++ {
		total += <-results
		count++
	}
	return total, count
}

func main() {
	for rep := 0; rep < 10; rep++ {
		sum, closed := pingPong(32, 300)
		println("pingpong sum", sum, "closed", closed)
	}
	got, stopped := selectSenders(16, 8, 250)
	println("selectsend got", got, "stopped", stopped)
	values, timeouts := timeoutRaces(2000)
	println("timeouts values", values, "timeouts", timeouts)
	total, count := multiSelect(4, 12, 1000)
	println("multiselect total", total, "receivers", count)
}
