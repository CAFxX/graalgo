package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// rt_finalizer_storm: finalizer throughput under scheduler load.
//
// Registers finalizers on N short-lived objects, then forces collection
// until the storm drains. The finalizer count is timing-dependent, so the
// stress-differential script runs this in "completion" mode: the gate is
// clean completion (exit 0, no crash/hang) on both graalgo and gc, not
// byte-identical output.
type obj struct{ v int }

var count atomic.Int64

const N = 500

func alloc() {
	for i := 0; i < N; i++ {
		o := &obj{v: i}
		runtime.SetFinalizer(o, func(o *obj) { count.Add(1) })
	}
}

func main() {
	alloc()
	for i := 0; i < 1000 && count.Load() < N; i++ {
		runtime.GC()
	}
	fmt.Println("done", count.Load())
}
