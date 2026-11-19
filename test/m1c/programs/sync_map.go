//m1c:requires descriptors

// sync.Map (internal/sync.HashTrieMap, upstream's source): Store, Load,
// LoadOrStore, LoadAndDelete, Swap, CompareAndSwap, CompareAndDelete, Range,
// Clear, under concurrent writers.
package main

import "sync"

func main() {
	var m sync.Map
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				m.Store(g*1000+i, i)
			}
		}()
	}
	wg.Wait()
	n := 0
	m.Range(func(k, v any) bool { n++; return true })
	println("entries", n)
	v, loaded := m.LoadOrStore("a", 1)
	println(v.(int), loaded)
	v, loaded = m.LoadOrStore("a", 2)
	println(v.(int), loaded)
	old, ok := m.Swap("a", 3)
	println(old.(int), ok)
	println(m.CompareAndSwap("a", 3, 4), m.CompareAndSwap("a", 3, 5))
	println(m.CompareAndDelete("a", 5), m.CompareAndDelete("a", 4))
	v, loaded = m.LoadAndDelete(7)
	println(v.(int), loaded)
	_, ok = m.Load(7)
	println(ok)
	m.Clear()
	n = 0
	m.Range(func(k, v any) bool { n++; return true })
	println("after clear", n)
}
