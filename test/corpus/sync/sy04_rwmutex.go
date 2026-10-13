package main

import (
	"fmt"
	"sync"
)

// Port of SyncStressTest.testRWMutex (Java level): 16 readers x 500
// reads concurrent with 2 writers x 100 writes; every read sees a
// consistent value (no torn writes), final value exact.
func main() {
	var mu sync.RWMutex
	var wg sync.WaitGroup
	value := 0
	torn := 0
	nReaders := 16
	nWriters := 2
	reads := 500
	writes := 100
	wg.Add(nReaders + nWriters)
	for i := 0; i < nReaders; i++ {
		go func() {
			defer wg.Done()
			for k := 0; k < reads; k++ {
				mu.RLock()
				v := value
				mu.RUnlock()
				if v < 0 {
					torn++
				}
			}
		}()
	}
	for i := 0; i < nWriters; i++ {
		go func() {
			defer wg.Done()
			for k := 0; k < writes; k++ {
				mu.Lock()
				value++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Println("value:", value, "torn:", torn)
}
