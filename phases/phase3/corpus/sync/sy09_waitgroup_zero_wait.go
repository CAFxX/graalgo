package main

import (
	"fmt"
	"sync"
)

// Wait on a zero counter returns immediately (no park). Port of
// SyncStressTest.testWaitGroupZeroAwaitImmediate.
func main() {
	var wg sync.WaitGroup
	wg.Wait()
	fmt.Println("zero wait ok")
}
