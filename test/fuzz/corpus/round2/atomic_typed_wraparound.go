// sync/atomic typed and function forms: wraparound of Add on signed and
// unsigned widths, And/Or returning the old value, Bool/Pointer Swap and
// CAS, atomic.Pointer to arrays and structs mutated through the loaded
// pointer, and atomic.Value holding funcs (CAS compares with ==: panics).
package main

import (
	"math"
	"sync"
	"sync/atomic"
)

type cfg struct {
	limits [4]int32
	name   string
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case string:
				println(name, "-> string:", v)
			case interface{ Error() string }:
				println(name, "-> error:", v.Error())
			}
		}
	}()
	f()
}

func main() {
	var i32 atomic.Int32
	i32.Store(math.MaxInt32)
	println("i32 wrap", i32.Add(1), i32.Add(-1), i32.Load())
	var u32 atomic.Uint32
	println("u32 wrap", u32.Add(^uint32(0)), u32.Add(2))
	var i64 atomic.Int64
	i64.Store(math.MinInt64)
	println("i64 wrap", i64.Add(-1), i64.Swap(5), i64.CompareAndSwap(5, -5), i64.Load())
	var u64 atomic.Uint64
	u64.Store(1 << 63)
	println("u64", u64.Add(1<<63), u64.Or(0xf0), u64.And(0x30), u64.Load())
	println("u32 and/or", u32.Or(0xff00), u32.And(0x0f0f), u32.Load())
	var raw int32 = -8
	println("func forms", atomic.AddInt32(&raw, 3), atomic.OrInt32(&raw, 0x10), atomic.AndInt32(&raw, 0xff), raw)
	var ru uintptr = 10
	println("uintptr", atomic.AddUintptr(&ru, ^uintptr(0)), atomic.CompareAndSwapUintptr(&ru, 9, 1), ru)
	var su uint64
	atomic.AddUint64(&su, ^uint64(3-1))
	println("sub via add", su == math.MaxUint64-2)

	var b atomic.Bool
	println("bool", b.Swap(true), b.CompareAndSwap(false, true), b.CompareAndSwap(true, false), b.Load())

	var ap atomic.Pointer[[8]byte]
	println("nil ptr load", ap.Load() == nil)
	buf := &[8]byte{1}
	ap.Store(buf)
	ap.Load()[7] = 7
	println("ptr aliases", buf[7], ap.Load()[0])
	cp := *ap.Load()
	cp[0] = 50
	println("deref copy", buf[0], cp[0])
	other := &[8]byte{1}
	println("cas other equal content", ap.CompareAndSwap(other, other), ap.CompareAndSwap(buf, other), ap.Load() == other)

	var cfgp atomic.Pointer[cfg]
	c1 := &cfg{name: "one"}
	cfgp.Store(c1)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				for {
					old := cfgp.Load()
					nc := *old
					nc.limits[w]++
					if cfgp.CompareAndSwap(old, &nc) {
						break
					}
				}
			}
			mu.Lock()
			mu.Unlock()
		}()
	}
	wg.Wait()
	fin := cfgp.Load()
	println("cas loop", fin.limits[0], fin.limits[1], fin.limits[2], fin.limits[3], fin.name, c1.limits[0])

	var cnt atomic.Int64
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 500; k++ {
				cnt.Add(int64(w))
			}
		}()
	}
	wg.Wait()
	println("parallel add", cnt.Load())

	var fv atomic.Value
	f := func() int { return 1 }
	fv.Store(f)
	println("func value", fv.Load().(func() int)())
	try("func cas", func() { fv.CompareAndSwap(f, f) })
	try("func swap type", func() { fv.Swap(func() string { return "" }) })
	old := fv.Swap(func() int { return 2 })
	println("func swap", old.(func() int)(), fv.Load().(func() int)())

	var mv atomic.Value
	mv.Store(map[string]int{"a": 1})
	mv.Load().(map[string]int)["b"] = 2
	println("map value shared", len(mv.Load().(map[string]int)))
	var av atomic.Value
	av.Store(cfg{name: "v"})
	got := av.Load().(cfg)
	got.limits[0] = 9
	println("struct value copied", av.Load().(cfg).limits[0])
	println("cas struct", av.CompareAndSwap(cfg{name: "v"}, cfg{name: "w"}), av.Load().(cfg).name)
	var ev atomic.Value
	try("cas empty with value", func() { println("empty cas", ev.CompareAndSwap(1, 2)) })
	println("swap empty", ev.Swap(3) == nil, ev.Load().(int))
}
