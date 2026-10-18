package main

import (
	"fmt"
	"sync"
)

// R-7: a negative WaitGroup counter is a *recoverable* Go panic in gc
// 1.27.1 (unlike mutex misuse, which is a fatal error) — a deferred
// recover() must catch it.
func main() {
	var wg sync.WaitGroup
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("recovered:", r)
		} else {
			fmt.Println("FAIL: no panic")
		}
	}()
	wg.Add(-1)
	fmt.Println("FAIL: Add(-1) did not panic")
}
