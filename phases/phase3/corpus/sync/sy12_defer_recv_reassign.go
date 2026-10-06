package main

import (
	"fmt"
	"sync"
)

// Defer receiver address semantics: `defer m.Unlock()` uses &m, so a
// receiver reassigned before return unlocks the CURRENT object (which is
// unlocked -> misuse). Verified against gc 1.27.1: fatal error, not a
// silent unlock of the old object.
func main() {
	m := sync.Mutex{}
	m.Lock()
	func() {
		defer m.Unlock() // targets &m -> the reassigned (unlocked) mutex
		m = sync.Mutex{}
	}()
	fmt.Println("unreachable")
}
