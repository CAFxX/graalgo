package main

// The equality and hash functions of type descriptors (abi.Type.Equal,
// MapType.Hasher), called by upstream code such as sync.Map, panic like gc's
// efaceeq/typehash on an interface holding an uncomparable dynamic type
// (BUG-0044); runtime.NumGoroutine counts the live goroutines. Found by
// test/fuzz.

import (
	"runtime"
	"sync"
)

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			_, rt := r.(runtime.Error)
			println(name, "panic:", rt, r.(interface{ Error() string }).Error())
		}
	}()
	f()
	println(name, "no panic")
}

// fresh returns a new map per probe: a panic inside sync.Map's trie leaves
// its node lock held under gc, so the next operation would deadlock.
func fresh() *sync.Map {
	m := new(sync.Map)
	m.Store("sl", []int{1})
	m.Store("n", 1)
	return m
}

func main() {
	try("cas uncomparable old", func() { fresh().CompareAndSwap("sl", []int{1}, 6) })
	try("cas uncomparable stored", func() { fresh().CompareAndSwap("n", map[int]int{}, 6) })
	try("cad uncomparable", func() { fresh().CompareAndDelete("sl", []int{1}) })
	try("store unhashable key", func() { fresh().Store([]int{1}, 1) })
	try("load unhashable key", func() { fresh().Load(func() {}) })
	println("cas ok", fresh().CompareAndSwap("n", 1, 2))

	println("goroutines", runtime.NumGoroutine())
	var wg sync.WaitGroup
	release := make(chan struct{})
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
		}()
	}
	for runtime.NumGoroutine() != 4 {
		runtime.Gosched()
	}
	println("goroutines", runtime.NumGoroutine())
	close(release)
	wg.Wait()
	for runtime.NumGoroutine() != 1 {
		runtime.Gosched()
	}
	println("goroutines", runtime.NumGoroutine())
}
