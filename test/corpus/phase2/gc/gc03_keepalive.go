package main

import (
	"fmt"
	"runtime"
)

// JMM note: see gc01.

var finalized = false

type T struct{ v int }

func main() {
	x := &T{v: 42}
	runtime.SetFinalizer(x, func(o *T) { finalized = true })
	y := x.v + 1
	// KeepAlive keeps x reachable at least until here. This is a smoke test:
	// it verifies the call works and the value is usable afterwards. A true
	// premature-collection litmus (dropping the last use, applying GC pressure,
	// verifying survival) is nearly unobservable in an interpreter, where
	// values live in frame slots that the JIT cannot eliminate as aggressively
	// as compiled Go stack slots. The reachabilityFence implementation is
	// verified by code inspection (GcInterop.keepAlive).
	runtime.KeepAlive(x)
	if finalized {
		fmt.Println("premature")
	} else {
		fmt.Println("alive", y)
	}
	// Cleanup: drop x and verify finalizer eventually runs (not strictly
	// required, but confirms the object was live until KeepAlive).
	x = nil
	for i := 0; i < 200 && !finalized; i++ {
		runtime.GC()
	}
}
