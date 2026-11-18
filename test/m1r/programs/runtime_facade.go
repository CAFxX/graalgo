package main

import (
	"math/bits"
	"runtime"
	_ "unsafe"
)

//go:linkname nanotime runtime.nanotime
func nanotime() int64

func classify(f func()) {
	defer func() {
		r := recover()
		re, isRuntime := r.(runtime.Error)
		err, isError := r.(error)
		switch {
		case isRuntime:
			println("runtime.Error:", re.Error())
		case isError:
			println("error:", err.Error())
		default:
			println("not an error")
		}
	}()
	f()
}

type myErr struct{}

func (myErr) Error() string { return "mine" }

func main() {
	println(runtime.GOOS, runtime.GOARCH, runtime.Compiler)
	println(runtime.GOMAXPROCS(0) >= 1, runtime.NumCPU() >= 1)
	prev := runtime.GOMAXPROCS(2)
	println(runtime.GOMAXPROCS(prev) == 2, runtime.GOMAXPROCS(-1) == prev)

	t0 := nanotime()
	done := make(chan int)
	turns := 0
	go func() {
		for i := 0; i < 5; i++ {
			turns++
			runtime.Gosched()
		}
		done <- turns
	}()
	for i := 0; i < 5; i++ {
		runtime.Gosched()
	}
	println(<-done)
	runtime.GC()
	println(nanotime() >= t0)

	var p *int
	var m map[string]int
	var s []int
	zero := 0
	classify(func() { _ = *p })
	classify(func() { m["a"] = 1 })
	classify(func() { _ = s[3] })
	classify(func() { _ = 1 / zero })
	classify(func() { bits.Div64(1, 0, 0) })
	classify(func() { bits.Div64(9, 0, 3) })
	classify(func() { panic(myErr{}) })
	classify(func() { panic("text") })
	classify(func() { var e error; _ = e.(myErr) })

	// The values math/bits panics with are the runtime's own.
	var first, second any
	func() {
		defer func() { first = recover() }()
		bits.Div64(9, 0, 3)
	}()
	func() {
		defer func() { second = recover() }()
		bits.Div32(9, 0, 3)
	}()
	println(first == second, first != nil)
}
