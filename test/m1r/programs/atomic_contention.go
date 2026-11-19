// Eight goroutines hammering sync/atomic words, then a lock-free stack on
// atomic.Pointer. Also the reproducer of a startup hang (about one run in
// six) that was the runtime's, not the intrinsics': Truffle's virtual-thread
// mount hook re-entering MethodHandle.customize (see
// internal/backend/graalgo-bundle-run for the cause and the flag that removes
// the trigger).
package main

import (
	"runtime"
	"sync/atomic"
)

type counters struct {
	pad  int64
	hits atomic.Int64
	i32  int32
	u32  uint32
	u64  atomic.Uint64
}

type node struct {
	val  int
	next *node
}

func main() {
	c := &counters{}
	var ai atomic.Int32
	done := make(chan bool)
	const G, N = 8, 2000
	for g := 0; g < G; g++ {
		go func() {
			for i := 0; i < N; i++ {
				c.hits.Add(1)
				atomic.AddInt32(&c.i32, 2)
				atomic.AddUint32(&c.u32, 3)
				c.u64.Add(1 << 40)
				ai.Add(-1)
				if i%100 == 0 {
					runtime.Gosched()
				}
			}
			done <- true
		}()
	}
	for g := 0; g < G; g++ {
		<-done
	}
	println(c.hits.Load(), c.i32, c.u32, c.u64.Load(), ai.Load(), c.pad)

	// A lock-free stack.
	var head atomic.Pointer[node]
	for g := 0; g < G; g++ {
		go func() {
			for i := 0; i < 200; i++ {
				n := &node{val: g*1000 + i}
				for {
					old := head.Load()
					n.next = old
					if head.CompareAndSwap(old, n) {
						break
					}
				}
			}
			done <- true
		}()
	}
	for g := 0; g < G; g++ {
		<-done
	}
	count, sum := 0, 0
	for n := head.Load(); n != nil; n = n.next {
		count++
		sum += n.val
	}
	println(count, sum)
}
