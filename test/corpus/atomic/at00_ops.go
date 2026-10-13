package main

import (
	"fmt"
	"sync/atomic"
)

func main() {
	// Int64: the full op set, sequential (deterministic).
	var a atomic.Int64
	a.Store(41)
	fmt.Println(a.Load(), a.Add(1), a.Swap(42), a.CompareAndSwap(42, 100))
	fmt.Println(a.Load())

	// Uint64 / Int32 / Uint32.
	var u atomic.Uint64
	u.Store(7)
	fmt.Println(u.Add(3), u.Swap(1), u.CompareAndSwap(1, 9), u.Load())
	var i32 atomic.Int32
	i32.Store(-5)
	fmt.Println(i32.Add(10), i32.Swap(0), i32.CompareAndSwap(0, -1), i32.Load())
	var u32 atomic.Uint32
	u32.Store(100)
	fmt.Println(u32.Add(23), u32.Swap(4), u32.CompareAndSwap(4, 5), u32.Load())

	// Bool.
	var b atomic.Bool
	fmt.Println(b.Load())
	b.Store(true)
	fmt.Println(b.Load(), b.Swap(false), b.CompareAndSwap(false, true))
	fmt.Println(b.Load())

	// Uintptr.
	var p atomic.Uintptr
	p.Store(0x1000)
	fmt.Println(p.Add(0x10), p.Swap(0x2000), p.CompareAndSwap(0x2000, 0x3000), p.Load())

	// Failed CAS leaves the value alone.
	var c atomic.Int64
	c.Store(1)
	fmt.Println(c.CompareAndSwap(2, 3), c.Load())
}
