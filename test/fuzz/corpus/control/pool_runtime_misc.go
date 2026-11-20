// Targets sync.Pool Get from empty with and without New, Put/Get round trip
// of a non-nil value counted without assuming retention, runtime.Gosched,
// runtime.GOMAXPROCS(0) not changing settings, and runtime.NumGoroutine
// returning to 1 after spawned goroutines finish.
package main

import (
	"runtime"
	"sync"
)

type buf struct{ b []byte }

func main() {
	created := 0
	p := sync.Pool{New: func() any {
		created++
		return &buf{b: make([]byte, 0, 16)}
	}}
	x := p.Get().(*buf)
	println("from New", created, cap(x.b))
	x.b = append(x.b, "abc"...)
	p.Put(x)
	y := p.Get().(*buf)
	println("got non-nil", y != nil, created >= 1)

	var empty sync.Pool
	println("empty pool nil", empty.Get() == nil)
	empty.Put(nil)
	println("put nil ignored", empty.Get() == nil)

	before := runtime.GOMAXPROCS(0)
	after := runtime.GOMAXPROCS(0)
	println("GOMAXPROCS stable", before == after, before >= 1)
	println("NumCPU positive", runtime.NumCPU() >= 1)

	println("goroutines start", runtime.NumGoroutine())
	var wg sync.WaitGroup
	block := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-block
			runtime.Gosched()
		}()
	}
	for runtime.NumGoroutine() < 11 {
		runtime.Gosched()
	}
	println("goroutines running", runtime.NumGoroutine())
	close(block)
	wg.Wait()
	for runtime.NumGoroutine() > 1 {
		runtime.Gosched()
	}
	println("goroutines end", runtime.NumGoroutine())

	// Gosched-based cooperative counter: final value deterministic
	var mu sync.Mutex
	total := 0
	for i := 0; i < 5; i++ {
		wg.Go(func() {
			for k := 0; k < 100; k++ {
				mu.Lock()
				total += i
				mu.Unlock()
				runtime.Gosched()
			}
		})
	}
	wg.Wait()
	println("total", total)

	// many pools / many gets: values come from New or Put; count is consistent
	var pp sync.Pool
	pp.New = func() any { return new(int) }
	for i := 0; i < 100; i++ {
		v := pp.Get().(*int)
		*v = i
		pp.Put(v)
	}
	println("pool loop done")
	runtime.GC()
	println("after GC get non-nil", pp.Get() != nil)
}
