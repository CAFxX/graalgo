package main

import (
	"fmt"
	"runtime"
)

// R-2: the launcher --gmp flag must be visible through
// runtime.GOMAXPROCS(0), and GOMAXPROCS(n) must round-trip the previous
// value while moving the scheduler's effective cap. Run with --gmp=2.
func main() {
	if got := runtime.GOMAXPROCS(0); got != 2 {
		fmt.Println("FAIL: initial GOMAXPROCS(0) =", got, "want 2 (--gmp=2)")
		return
	}
	if prev := runtime.GOMAXPROCS(4); prev != 2 {
		fmt.Println("FAIL: GOMAXPROCS(4) returned", prev, "want 2")
		return
	}
	if got := runtime.GOMAXPROCS(0); got != 4 {
		fmt.Println("FAIL: after set, GOMAXPROCS(0) =", got, "want 4")
		return
	}
	if prev := runtime.GOMAXPROCS(-1); prev != 4 {
		fmt.Println("FAIL: GOMAXPROCS(-1) returned", prev, "want 4 (no change)")
		return
	}
	if got := runtime.GOMAXPROCS(0); got != 4 {
		fmt.Println("FAIL: negative set changed the value to", got)
		return
	}
	fmt.Println("ok")
}
