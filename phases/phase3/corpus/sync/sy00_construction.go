package main

import (
	"fmt"
	"sync"
)

// All four construction forms, package-level and local vars.
var gMu sync.Mutex

func main() {
	var m1 sync.Mutex     // implicit syncnew (exporter-normalized zero)
	m2 := sync.Mutex{}    // composite literal
	m3 := &sync.Mutex{}   // address-of literal (pointer form)
	m4 := new(sync.Mutex) // new (pointer form)
	m1.Lock()
	m1.Unlock()
	m2.Lock()
	m2.Unlock()
	m3.Lock()
	m3.Unlock()
	m4.Lock()
	m4.Unlock()
	gMu.Lock()
	gMu.Unlock()

	var rw sync.RWMutex
	rw.RLock()
	rw.RUnlock()
	rw.Lock()
	rw.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	wg.Done()
	wg.Wait()

	var o sync.Once
	called := 0
	o.Do(func() { called++ })
	o.Do(func() { called++ })

	// defer + syncop
	func() {
		m1.Lock()
		defer m1.Unlock()
	}()

	fmt.Println("construction ok", called)
}
