package main

import "runtime"

// R2 adv11 (review remediation): Goexit in a NESTED call — defers in g run,
// then defers in main run, then the fatal deadlock error. pn10/pn11 cover
// Goexit-in-main only. Fatal: exit 2, no post-mortem observation possible.
func main() {
	g := func() {
		defer func() { _ = 1 }()
		runtime.Goexit()
	}
	defer func() { _ = 2 }()
	g()
}

// panic: fatal error: no goroutines (main called runtime.Goexit) - deadlock!
