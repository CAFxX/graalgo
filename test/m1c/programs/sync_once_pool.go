// Once (including a panicking function), OnceFunc/OnceValue/OnceValues
// (re-panic with the same value), and Pool with runtime.GC's cleanup.
package main

import (
	"runtime"
	"sync"
)

func main() {
	var once sync.Once
	func() {
		defer func() { println("recovered:", recover().(string)) }()
		once.Do(func() { panic("first") })
	}()
	once.Do(func() { println("not run: Do counts a panicking call") })
	println("once done")

	calls := 0
	f := sync.OnceFunc(func() { calls++; panic("oncefunc") })
	for i := 0; i < 2; i++ {
		func() {
			defer func() { println("oncefunc panic", i, recover().(string)) }()
			f()
		}()
	}
	println("calls", calls)
	v := sync.OnceValue(func() int { calls++; return 42 })
	println(v(), v(), calls)
	a, b := sync.OnceValues(func() (string, error) { return "x", nil })()
	println(a, b == nil)

	// Once under contention: exactly one execution, every caller returns after it.
	var o sync.Once
	var wg sync.WaitGroup
	ran := 0
	var mu sync.Mutex
	seen := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.Do(func() { runtime.Gosched(); ran++ })
			mu.Lock()
			seen += ran
			mu.Unlock()
		}()
	}
	wg.Wait()
	println("ran", ran, "seen", seen)

	p := sync.Pool{New: func() any { return "new" }}
	p.Put("put")
	println(p.Get().(string), p.Get().(string))
	p.Put("again")
	runtime.GC() // moves the cache to the victim
	runtime.GC() // drops the victim
	println(p.Get().(string))
	var empty sync.Pool
	println(empty.Get() == nil)
}
