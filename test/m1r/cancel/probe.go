// probe: the program graalgo.launcher.CancelProbe cancels or watches exit
// (test/m1r/cancel.sh; playbook §10, WP-TR-AUDIT-3).
//
// Usage: probe READY SCENARIO PARKED
//
// It starts goroutines blocked in every way a Go program blocks — PARKED
// goroutines on a channel receive, others on a send, a select, a Mutex, a
// WaitGroup, timers and sleeps; goroutines blocked in read(2) on a pipe
// nobody writes and on stdin (cancel.sh makes it a FIFO that never delivers);
// goroutines that panic and recover in a loop, one parked inside a deferred
// call while panicking; and spinners: plain loops and a backward goto loop —
// then creates
// the file READY. SCENARIO says what happens next (exit and panic create the
// file READY.exit right before they exit):
//
//	park   nothing: only Context.close(true) ends it
//	exit   os.Exit(3) while all of the above runs
//	panic  an uncaught panic in a goroutine (exit status 2)
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

var sink int

func spin(n int) {
	s := 0
	for i := 0; ; i++ {
		s += i * n
		if s == -1 {
			break
		}
	}
	sink = s
}

func gotoSpin(n int) {
	i := 0
loop:
	i += n
	if i == -1 {
		sink = i
		return
	}
	goto loop
}

func panicLoop(started *sync.WaitGroup) {
	started.Done()
	for i := 0; ; i++ {
		func() {
			defer func() {
				if recover() == nil {
					panic("no panic to recover")
				}
			}()
			defer func() {
				panic(fmt.Sprint("inner ", i)) // replaces the outer panic
			}()
			panic("outer")
		}()
	}
}

func main() {
	ready, scenario := os.Args[1], os.Args[2]
	parked, err := strconv.Atoi(os.Args[3])
	if err != nil {
		panic(err)
	}
	var started sync.WaitGroup
	block := make(chan int)
	other := make(chan int)
	var mu sync.Mutex
	var never sync.WaitGroup
	never.Add(1)
	mu.Lock()
	run := func(n int, f func()) {
		for i := 0; i < n; i++ {
			started.Add(1)
			go func() {
				started.Done()
				f()
			}()
		}
	}
	run(parked, func() { <-block })
	run(10, func() { block <- 1 })
	run(10, func() {
		select {
		case <-block:
		case other <- 1:
		}
	})
	run(10, func() { mu.Lock() })
	run(10, func() { never.Wait() })
	run(100, func() { time.Sleep(time.Hour) })
	run(10, func() { <-time.After(time.Hour) })
	run(10, func() { <-time.NewTimer(time.Hour).C })
	// read(2) on a blocking pipe nobody writes, and on stdin.
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil {
		panic(err)
	}
	reading := make(chan int, 2)
	for _, fd := range []int{p[0], 0} {
		go func(fd int) {
			reading <- fd
			var buf [64]byte
			n, err := syscall.Read(fd, buf[:])
			fmt.Println("read returned", fd, n, err) // only after the context is gone: never seen
		}(fd)
	}
	for i := 0; i < 2; i++ {
		<-reading
	}
	for i := 0; i < 2; i++ {
		started.Add(1)
		go panicLoop(&started)
	}
	started.Add(1)
	go func() {
		defer func() {
			<-block // parked inside a deferred call, mid-panic
		}()
		started.Done()
		panic("parked while panicking")
	}()
	for i := 0; i < runtime.GOMAXPROCS(0)+2; i++ {
		started.Add(1)
		go func(i int) {
			started.Done()
			if i%2 == 0 {
				spin(i + 1)
			} else {
				gotoSpin(i + 1)
			}
		}(i)
	}
	started.Wait()
	time.Sleep(50 * time.Millisecond) // let the readers enter read(2)
	if err := os.WriteFile(ready, []byte("ready\n"), 0o644); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if scenario != "park" {
		time.Sleep(100 * time.Millisecond)
		// The embedder times the exit from this file on.
		if err := os.WriteFile(ready+".exit", []byte("exit\n"), 0o644); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	switch scenario {
	case "exit":
		os.Exit(3)
	case "panic":
		go func() {
			panic("boom")
		}()
	}
	select {}
}
