package main

import (
	"fmt"
	"runtime"
)

type T struct{ v int }

func main() {
	x := &T{v: 42}
	// The finalizer dereferences its argument. In graalgo the argument is
	// always nil (DIV-M4-1), so this panics. L2: the finalizer body runs
	// on a scheduler goroutine, so the panic is recorded exactly like
	// any other goroutine's uncaught Go panic (traceback on stderr,
	// launcher exit 2) — not merely printed by the Cleaner thread.
	// Graalgo-specific nuance vs gc's fatal crash: a panic in one
	// goroutine does not kill the others, so main still reaches the
	// "survived" print; the process exit code (2) is what records the
	// panic.
	runtime.SetFinalizer(x, func(o *T) { fmt.Println(o.v) })
	x = nil
	for i := 0; i < 200; i++ {
		runtime.GC()
	}
	fmt.Println("survived")
}
