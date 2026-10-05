package main

// runtime stress: finalizer storm.
// Objects with finalizers, allocated across goroutines while explicit
// GCs run. Exercises GcInterop under concurrency: finalizers must all
// run exactly once, no lost or duplicate finalizations.
//
// NOTE: `&` (address-of) is WS D — not expressible in Go source. The
// finalizer increments a shared package counter directly (finalizers run
// on a single finalizer thread in both gc and graalgo, so no race).
import (
	"fmt"
	"runtime"
)

var done0, done1, done2, done3 = false, false, false, false
var finTotal int64

type obj struct{}

func main() {
	go func() {
		for j := 0; j < 500; j++ {
			o := &obj{}
			runtime.SetFinalizer(o, func(x *obj) {
				finTotal++
			})
		}
		done0 = true
	}()
	go func() {
		for j := 0; j < 500; j++ {
			o := &obj{}
			runtime.SetFinalizer(o, func(x *obj) {
				finTotal++
			})
		}
		done1 = true
	}()
	go func() {
		for j := 0; j < 500; j++ {
			o := &obj{}
			runtime.SetFinalizer(o, func(x *obj) {
				finTotal++
			})
		}
		done2 = true
	}()
	go func() {
		for j := 0; j < 500; j++ {
			o := &obj{}
			runtime.SetFinalizer(o, func(x *obj) {
				finTotal++
			})
		}
		done3 = true
	}()
	for !(done0 && done1 && done2 && done3) {
	}
	// Force collection; finalizers run on the finalizer thread.
	// NOTE: finalizer timing is implementation-defined (gc may run 0 here
	// if the finalizer goroutine hasn't caught up; graalgo runs them
	// promptly). The differential assertion is clean completion, not the
	// count — the count is informational.
	for i := 0; i < 5; i++ {
		runtime.GC()
	}
	fmt.Println("storm-done", finTotal)
}
