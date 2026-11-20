// Targets sync/atomic typed values and functions: Int32/Int64/Uint32/
// Uint64/Uintptr/Bool/Pointer[T]/Value (Load/Store/Swap/CAS/Add/And/Or),
// wraparound, concurrent Add totals, and atomic.Value panic messages for
// nil Store and inconsistently typed Store/Swap/CompareAndSwap.
package main

import (
	"math"
	"sync"
	"sync/atomic"
)

type node struct{ v int }

func catch(name string, f func()) {
	defer func() {
		switch r := recover().(type) {
		case nil:
			println(name, "no panic")
		case string:
			println(name, "string:", r)
		case error:
			println(name, "error:", r.Error())
		default:
			println(name, "other")
		}
	}()
	f()
}

func main() {
	var i32 atomic.Int32
	println("i32", i32.Add(5), i32.Add(-10), i32.Load())
	i32.Store(math.MaxInt32)
	println("i32 wrap", i32.Add(1))
	println("i32 swap", i32.Swap(7), i32.Load())
	println("i32 cas", i32.CompareAndSwap(6, 1), i32.CompareAndSwap(7, 1), i32.Load())
	i32.Store(0b1100)
	println("i32 and/or", i32.And(0b1010), i32.Load(), i32.Or(0b0001), i32.Load())

	var i64 atomic.Int64
	i64.Store(math.MinInt64)
	println("i64 wrap", i64.Add(-1))
	var u32 atomic.Uint32
	println("u32 underflow", u32.Add(^uint32(0)))
	println("u32 or/and", u32.Or(0xF0), u32.And(0x0F), u32.Load())
	var u64 atomic.Uint64
	u64.Store(1 << 63)
	println("u64", u64.Add(1<<63), u64.Load())
	var up atomic.Uintptr
	up.Store(40)
	println("uintptr", up.Add(2), up.Swap(1), up.CompareAndSwap(1, 9), up.Load())

	var b atomic.Bool
	println("bool", b.Load(), b.Swap(true), b.Load(), b.CompareAndSwap(false, true), b.CompareAndSwap(true, false), b.Load())

	var p atomic.Pointer[node]
	println("ptr nil", p.Load() == nil)
	n1 := &node{1}
	n2 := &node{2}
	p.Store(n1)
	println("ptr cas wrong", p.CompareAndSwap(n2, n2), p.Load().v)
	println("ptr cas right", p.CompareAndSwap(n1, n2), p.Load().v)
	old := p.Swap(nil)
	println("ptr swap", old.v, p.Load() == nil)

	// legacy functions
	var raw int64
	atomic.AddInt64(&raw, 10)
	println("raw", atomic.LoadInt64(&raw), atomic.CompareAndSwapInt64(&raw, 10, 11), atomic.SwapInt64(&raw, 3), raw)
	var r32 uint32 = 0xFF
	println("raw and", atomic.AndUint32(&r32, 0x0F), r32, atomic.OrUint32(&r32, 0x30), r32)

	// concurrency
	var cnt atomic.Int64
	var flags atomic.Uint32
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Go(func() {
			for k := 0; k < 1000; k++ {
				cnt.Add(1)
			}
			flags.Or(1 << g)
		})
	}
	wg.Wait()
	println("concurrent", cnt.Load(), flags.Load())

	// atomic.Value
	var v atomic.Value
	println("value empty nil", v.Load() == nil)
	catch("store nil", func() { v.Store(nil) })
	v.Store("first")
	println("value", v.Load().(string))
	catch("store int", func() { v.Store(1) })
	catch("swap int", func() { v.Swap(2) })
	catch("swap nil", func() { v.Swap(nil) })
	catch("cas int", func() { v.CompareAndSwap("first", 3) })
	catch("cas nil new", func() { v.CompareAndSwap("first", nil) })
	println("cas ok", v.CompareAndSwap("first", "second"), v.Load().(string))
	println("cas stale", v.CompareAndSwap("first", "third"), v.Load().(string))
	println("swap", v.Swap("x").(string), v.Load().(string))
	var v2 atomic.Value
	println("cas on empty", v2.CompareAndSwap(nil, 5), v2.Load().(int))
	var v3 atomic.Value
	catch("cas uncomparable old", func() {
		v3.Store([]int{1})
		v3.CompareAndSwap([]int{1}, []int{2})
	})
	type named string
	var v4 atomic.Value
	v4.Store("s")
	catch("named string", func() { v4.Store(named("n")) })
}
