// cycle: the program graalgo.launcher.ContextLeakProbe runs in many
// successive contexts of one JVM (test/m1r/leak.sh). It touches the runtime
// state a context owns — goroutines (some left parked at exit on a channel, a
// Mutex, a WaitGroup and a Cond), channels, select, maps, fmt, unsafe and
// reflect conversions, weak pointers, finalizers and cleanups (one run, one
// still registered at exit, which starts the finalizer watcher), timers (a
// package-level one and an AfterFunc left pending at exit) — and exits 0
// only when its results are right, so the probe can check every run
// without reading its output.
package main

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
	"weak"
)

type pair struct {
	a, b int64
	s    string
}

var pending = time.NewTimer(time.Hour) // a package variable that is a timer, pending at exit

func main() {
	ch := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ch <- i * j
			}
		}(i)
	}
	go func() { wg.Wait(); close(ch) }()
	sum := 0
	counts := map[string]int{}
	for v := range ch {
		sum += v
		counts[fmt.Sprint(v%50)]++
	}
	var mu sync.Mutex
	hits := 0
	tick := time.NewTicker(time.Millisecond)
	for i := 0; i < 3; i++ {
		select {
		case <-tick.C:
			mu.Lock()
			hits++
			mu.Unlock()
		case <-time.After(10 * time.Second):
		}
	}
	tick.Stop()
	finalized := make(chan string, 2)
	func() {
		runtime.SetFinalizer(&pair{s: "f"}, func(p *pair) { finalized <- p.s })
		runtime.AddCleanup(&pair{s: "c"}, func(s string) { finalized <- s }, "c")
	}()
	kept := &pair{s: "kept"}
	runtime.SetFinalizer(kept, func(*pair) { os.Exit(6) }) // registered at exit
	runtime.GC()
	for range 2 {
		select {
		case <-finalized:
		case <-time.After(10 * time.Second):
			os.Exit(7)
		}
	}
	time.AfterFunc(time.Hour, func() { os.Exit(4) }) // still pending at exit
	block := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() { <-block }() // still parked at exit
	}
	var held sync.Mutex
	held.Lock()
	var never sync.WaitGroup
	never.Add(1)
	cond := sync.NewCond(&sync.Mutex{})
	ready := make(chan struct{}, 3)
	go func() { ready <- struct{}{}; held.Lock() }()                // parked on the Mutex at exit
	go func() { ready <- struct{}{}; never.Wait() }()               // on the WaitGroup
	go func() { cond.L.Lock(); ready <- struct{}{}; cond.Wait() }() // on the Cond
	for i := 0; i < 3; i++ {
		<-ready
	}
	p := &pair{a: 7, b: 9, s: "x"}
	b := (*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b)))
	wp := weak.Make(p)
	if *b != 9 || reflect.ValueOf(*p).Field(2).String() != "x" || wp.Value() != p {
		os.Exit(5)
	}
	_ = pending
	var sb strings.Builder
	fmt.Fprintf(&sb, "sum=%d keys=%d hits=%d", sum, len(counts), hits)
	runtime.KeepAlive(kept)
	if sb.String() != "sum=138600 keys=50 hits=3" {
		fmt.Fprintln(os.Stderr, "cycle: wrong result:", sb.String())
		os.Exit(3)
	}
}
