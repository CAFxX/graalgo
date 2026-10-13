package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	count := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Println(count) // 8000, deterministic
	var once sync.Once
	v := 0
	for i := 0; i < 5; i++ {
		once.Do(func() { v = 42 })
	}
	fmt.Println(v)
	var rw sync.RWMutex
	rw.RLock()
	fmt.Println("rlock ok")
	rw.RUnlock()
}
