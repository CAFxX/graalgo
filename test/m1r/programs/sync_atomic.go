package main

import "sync/atomic"

type counters struct {
	pad  int64
	hits atomic.Int64
	i32  int32
	u32  uint32
	flag atomic.Bool
	u64  atomic.Uint64
}

type node struct {
	val  int
	next *node
}

func main() {
	// The functions: wrap-around at the type's width, old/new results.
	var i32 int32 = 0x7ffffffe
	println(atomic.AddInt32(&i32, 1), atomic.AddInt32(&i32, 1), atomic.AddInt32(&i32, -3))
	println(i32)
	var u32 uint32 = 0xfffffffe
	println(atomic.AddUint32(&u32, 1), atomic.AddUint32(&u32, 1), atomic.AddUint32(&u32, ^uint32(0)))
	println(u32)
	var i64 int64 = 1<<63 - 1
	println(atomic.AddInt64(&i64, 1), atomic.LoadInt64(&i64), atomic.SwapInt64(&i64, 42))
	println(i64)
	var u64 uint64
	println(atomic.AddUint64(&u64, ^uint64(0)), atomic.CompareAndSwapUint64(&u64, 1, 2), atomic.CompareAndSwapUint64(&u64, ^uint64(0), 7))
	println(u64)
	var up uintptr = 10
	println(atomic.AddUintptr(&up, 5), atomic.SwapUintptr(&up, 1), atomic.LoadUintptr(&up), atomic.CompareAndSwapUintptr(&up, 1, 99))
	println(up)
	i32 = -8
	println(atomic.AndInt32(&i32, 0x7fffffff), atomic.LoadInt32(&i32), atomic.OrInt32(&i32, -1<<31), atomic.LoadInt32(&i32))
	u32 = 0xf0f0f0f0
	println(atomic.OrUint32(&u32, 0x0f), atomic.LoadUint32(&u32), atomic.AndUint32(&u32, 0xff), atomic.LoadUint32(&u32))
	println(atomic.AndInt64(&i64, 0xf), atomic.OrUint64(&u64, 1<<63), atomic.AndUintptr(&up, 3), atomic.OrUintptr(&up, 4))
	println(i64, u64, up)
	atomic.StoreInt32(&i32, -5)
	atomic.StoreUint32(&u32, 5)
	atomic.StoreInt64(&i64, -6)
	atomic.StoreUint64(&u64, 6)
	atomic.StoreUintptr(&up, 7)
	println(atomic.LoadInt32(&i32), atomic.LoadUint32(&u32), atomic.LoadInt64(&i64), atomic.LoadUint64(&u64), atomic.LoadUintptr(&up))
	println(atomic.SwapInt32(&i32, 1), atomic.SwapUint32(&u32, 2), atomic.SwapUint64(&u64, 3),
		atomic.CompareAndSwapInt32(&i32, 1, -1), atomic.CompareAndSwapUint32(&u32, 9, 1), atomic.CompareAndSwapInt64(&i64, -6, 0))
	println(i32, u32, i64, u64)

	// Words inside a struct, and upstream's typed wrappers on top of the functions.
	c := &counters{pad: -1}
	var ai atomic.Int32
	var au atomic.Uint32
	var aptr atomic.Uintptr
	for i := 0; i < 1000; i++ {
		c.hits.Add(1)
		atomic.AddInt32(&c.i32, 2)
		atomic.AddUint32(&c.u32, 3)
		c.u64.Add(1 << 40)
		ai.Add(-1)
		au.Add(1)
		aptr.Add(1)
	}
	c.flag.Store(true)
	println(c.hits.Load(), c.i32, c.u32, c.u64.Load(), ai.Load(), au.Load(), aptr.Load(), c.flag.Load(), c.pad)
	println(c.flag.Swap(false), c.flag.CompareAndSwap(false, true), c.flag.Load(), ai.CompareAndSwap(-1000, 0), ai.Swap(5), au.And(0xff), au.Or(1<<31), au.Load())
	var u atomic.Uint64
	u.Store(1 << 63)
	println(u.Load(), u.Add(1<<63), u.Swap(3), u.CompareAndSwap(3, 4), u.And(6), u.Or(8), u.Load())

	// atomic.Pointer[T]: pointers live in reference slots, and
	// compare-and-swap compares locations.
	var head atomic.Pointer[node]
	println(head.Load() == nil)
	for i := 0; i < 100; i++ {
		n := &node{val: i}
		for {
			old := head.Load()
			n.next = old
			if head.CompareAndSwap(old, n) {
				break
			}
		}
	}
	count, sum := 0, 0
	for n := head.Load(); n != nil; n = n.next {
		count++
		sum += n.val
	}
	old := head.Swap(nil)
	println(count, sum, old != nil, head.Load() == nil, head.CompareAndSwap(old, old), head.CompareAndSwap(nil, old), head.Load() == old)
	arr := [4]node{}
	var interior atomic.Pointer[node]
	interior.Store(&arr[2])
	println(interior.CompareAndSwap(&arr[1], nil), interior.CompareAndSwap(&arr[2], &arr[3]), interior.Load() == &arr[3])
}
